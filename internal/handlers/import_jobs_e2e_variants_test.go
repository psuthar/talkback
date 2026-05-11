package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/sessionimport/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-363: element-shape variants + multi-instance coverage for the
// template-import sweep.
//
// Covers:
//  - video_source with video_url (instead of embed_url)
//  - video_source with provider, duration_seconds populated
//  - transcript with segments_url → parsed segments inserted as
//    transcript_segments rows (worker change in this ticket)
//  - multi-instance: multiple materials with different content types,
//    multiple transcripts with different source values, multiple links
//  - negative case: duplicate transcript source rejected by the schema
//    validator at submit time

func TestTemplateImport_E2E_VideoSource_Variants(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-vs-variants@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-vs-variants@example.com")
	require.NoError(t, err)

	// One video_source with video_url + provider + duration_seconds.
	provider := "other"
	dur := 1800
	videoURL := srv.URL + "/video/abc123"
	tmpl := &template.Template{
		Version: 1,
		Title:   "Video Source Variants",
		Elements: []template.Element{
			{
				Kind:            template.KindVideoSource,
				ID:              "video-rec",
				VideoURL:        &videoURL,
				Provider:        &provider,
				DurationSeconds: &dur,
			},
		},
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors, "preflight: %+v", pre.Errors)

	descriptor, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))
	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	gotJob, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, gotJob.SessionID)

	dstVS, err := h.DB.GetVideoSourcesBySessionID(ctx, *gotJob.SessionID)
	require.NoError(t, err)
	require.Len(t, dstVS, 1)
	vs := dstVS[0]
	assert.Equal(t, videoURL, vs.VideoURL, "video_url must round-trip when set instead of embed_url")
	assert.Equal(t, provider, vs.Provider, "provider must round-trip")
	require.NotNil(t, vs.DurationSeconds)
	assert.Equal(t, dur, *vs.DurationSeconds, "duration_seconds must round-trip")
}

func TestTemplateImport_E2E_Transcript_SegmentsURL(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	segments := []map[string]interface{}{
		{"idx": 0, "start_ms": 0, "end_ms": 1500, "text": "Welcome to the meeting"},
		{"idx": 1, "start_ms": 1500, "end_ms": 3200, "text": "Let's start with the agenda"},
		{"idx": 2, "start_ms": 3200, "end_ms": 5000, "text": "First item is the budget review"},
	}
	segmentsBody, _ := json.Marshal(segments)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/transcript.json":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", strconv.Itoa(len(segmentsBody)))
			if r.Method == http.MethodGet {
				_, _ = w.Write(segmentsBody)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-segments@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-segments@example.com")
	require.NoError(t, err)

	segmentsURL := srv.URL + "/transcript.json"
	tmpl := &template.Template{
		Version: 1,
		Title:   "Transcript Segments URL",
		Elements: []template.Element{
			{
				Kind:        template.KindTranscript,
				ID:          "whisper-segs",
				Source:      strPtr("whisper"),
				SegmentsURL: &segmentsURL,
			},
		},
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors, "preflight: %+v", pre.Errors)

	descriptor, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))
	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	gotJob, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, gotJob.SessionID)

	transcripts, err := h.DB.ListTranscriptsBySessionID(ctx, *gotJob.SessionID)
	require.NoError(t, err)
	require.Len(t, transcripts, 1)
	dstTranscript := transcripts[0]

	dstSegments, err := h.DB.ListSegmentsByTranscriptID(ctx, dstTranscript.ID)
	require.NoError(t, err)
	require.Len(t, dstSegments, 3, "all 3 segments from the JSON body must be inserted as transcript_segments rows")
	assert.Equal(t, 0, dstSegments[0].Idx)
	assert.Equal(t, 0, dstSegments[0].StartMs)
	assert.Equal(t, 1500, dstSegments[0].EndMs)
	assert.Equal(t, "Welcome to the meeting", dstSegments[0].Text)
	assert.Equal(t, *gotJob.SessionID, dstSegments[0].SessionID)
}

func TestTemplateImport_E2E_MultiInstance_AllKinds(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/agenda.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "10")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte("PDFCONTENT"))
			}
		case r.URL.Path == "/spec.docx":
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			w.Header().Set("Content-Length", "12")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte("DOCXCONTENT0"))
			}
		case r.URL.Path == "/diagram.png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "11")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte("PNGCONTENT0"))
			}
		case r.URL.Path == "/transcript-whisper.txt":
			body := "whisper transcript body"
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(body))
			}
		case r.URL.Path == "/transcript-zoom.txt":
			body := "zoom transcript body"
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(body))
			}
		default:
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-multi@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-multi@example.com")
	require.NoError(t, err)

	tmpl := &template.Template{
		Version: 1,
		Title:   "Multi-Instance Template",
		Elements: []template.Element{
			// 3 materials of different content types
			{Kind: template.KindMaterial, ID: "agenda", Title: strPtr("Agenda"), URL: strPtr(srv.URL + "/agenda.pdf"), ContentType: strPtr("application/pdf")},
			{Kind: template.KindMaterial, ID: "spec", Title: strPtr("Spec"), URL: strPtr(srv.URL + "/spec.docx"), ContentType: strPtr("application/vnd.openxmlformats-officedocument.wordprocessingml.document")},
			{Kind: template.KindMaterial, ID: "diagram", Title: strPtr("Diagram"), URL: strPtr(srv.URL + "/diagram.png"), ContentType: strPtr("image/png")},
			// 3 links
			{Kind: template.KindLink, ID: "link-a", URL: strPtr(srv.URL + "/a")},
			{Kind: template.KindLink, ID: "link-b", URL: strPtr(srv.URL + "/b")},
			{Kind: template.KindLink, ID: "link-c", URL: strPtr(srv.URL + "/c")},
			// 2 transcripts with DIFFERENT source values to satisfy UNIQUE(session_id, source)
			{Kind: template.KindTranscript, ID: "tx-w", Source: strPtr("whisper"), TextURL: strPtr(srv.URL + "/transcript-whisper.txt")},
			{Kind: template.KindTranscript, ID: "tx-z", Source: strPtr("zoom"), TextURL: strPtr(srv.URL + "/transcript-zoom.txt")},
		},
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors, "preflight: %+v", pre.Errors)

	descriptor, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))
	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	gotJob, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, gotJob.SessionID)

	// 3 distinct materials.
	mats, err := h.DB.GetActiveMaterialsBySessionID(ctx, *gotJob.SessionID)
	require.NoError(t, err)
	require.Len(t, mats, 3, "all 3 materials must land")
	titles := map[string]bool{}
	for _, m := range mats {
		require.NotNil(t, m.Title)
		titles[*m.Title] = true
	}
	assert.Equal(t, map[string]bool{"Agenda": true, "Spec": true, "Diagram": true}, titles, "materials must be distinct by title")

	// 3 distinct links.
	links, err := h.DB.GetSessionLinksBySessionID(ctx, *gotJob.SessionID)
	require.NoError(t, err)
	require.Len(t, links, 3)
	urls := map[string]bool{}
	for _, l := range links {
		urls[l.URL] = true
	}
	assert.Equal(t, map[string]bool{srv.URL + "/a": true, srv.URL + "/b": true, srv.URL + "/c": true}, urls, "links must be distinct by URL")

	// 2 transcripts with different sources.
	transcripts, err := h.DB.ListTranscriptsBySessionID(ctx, *gotJob.SessionID)
	require.NoError(t, err)
	require.Len(t, transcripts, 2, "both transcripts must land — UNIQUE(session_id, source) is satisfied by different source values")
	sources := map[string]bool{}
	for _, tr := range transcripts {
		sources[tr.Source] = true
	}
	assert.Equal(t, map[string]bool{"whisper": true, "zoom": true}, sources, "transcripts must be distinct by source")
}

// TestTemplateImport_API_DuplicateTranscriptSource asserts the schema
// validator rejects two transcripts with the same `source` at submit
// time (no DB writes). This is the API-level form of the
// transcript_source_duplicate reason already covered by the validator
// unit test in template/template_test.go.
func TestTemplateImport_API_DuplicateTranscriptSource(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	body := []byte(`{
		"version": 1,
		"title": "Duplicate Source",
		"elements": [
			{"kind":"transcript","id":"a","source":"whisper","text_url":"https://example.com/a.txt"},
			{"kind":"transcript","id":"b","source":"whisper","text_url":"https://example.com/b.txt"}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/import-jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addUserSessionCookie(t, h, req, "tmpl-dup-source@example.com")
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateImportJob)(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "duplicate transcript source must be a 400")
	var resp ImportJobSubmitResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Errors)
	got := false
	for _, e := range resp.Errors {
		if e.ReasonCode == template.ReasonTranscriptSourceDuplicate {
			got = true
			break
		}
	}
	assert.True(t, got, "must surface transcript_source_duplicate from the validator")
	_ = fmt.Sprintf // keep fmt alive for potential future expansion
}
