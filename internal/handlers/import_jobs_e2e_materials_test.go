package handlers

import (
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

// SCRUM-361: materials parity assertion + every v1 content-type variant.
//
// SCRUM-355's TestTemplateImport_E2E_HappyPath declares a single
// `application/pdf` material element but never asserts that a corresponding
// `materials` row was actually created on the destination. A regression that
// silently dropped materials would still pass CI today. This file extends the
// regression sweep with:
//
//  1. Materials-count parity: when the template declares N material elements,
//     the destination must have exactly N rows in `materials`. Each row's
//     `content_type` must match the template's declared value.
//  2. Coverage of all nine content types in the SCRUM-351 v1 allowlist
//     (defined in internal/sessionimport/template/template.go's
//     materialContentTypes map).
//
// We use a table-driven structure with one element per content type in a
// single template so the worker fetch + materialize path is exercised end-
// to-end in one job. The httptest server returns the matching `Content-Type`
// header for each path so preflight is happy.

// v1MaterialAllowlist mirrors `materialContentTypes` in
// internal/sessionimport/template/template.go. Pinned here so a regression
// that silently drops a content type is caught at this test boundary.
var v1MaterialAllowlist = []struct {
	contentType string
	pathSuffix  string
	body        string
}{
	{"application/pdf", "agenda.pdf", "PDFCONTENT"},
	{"application/vnd.openxmlformats-officedocument.presentationml.presentation", "deck.pptx", "PPTXCONTENT"},
	{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "memo.docx", "DOCXCONTENT"},
	{"text/csv", "rows.csv", "a,b,c\n1,2,3\n"},
	{"text/plain", "notes.txt", "plain notes"},
	{"application/vnd.ms-excel", "legacy.xls", "XLSCONTENT"},
	{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "modern.xlsx", "XLSXCONTENT"},
	{"image/png", "diagram.png", "PNGCONTENT"},
	{"image/jpeg", "photo.jpg", "JPEGCONTENT"},
}

// TestTemplateImport_E2E_MaterialsParity_AllContentTypes is SCRUM-361's
// primary regression. It builds a template with one material element per
// content type from the v1 allowlist and asserts:
//
//   - exactly len(v1MaterialAllowlist) materials rows on the destination;
//   - each row's content_type matches the template's declared value;
//   - each element ends in elements_state[id].Status == succeeded.
//
// A single test (vs nine independent ones) keeps DB churn low and makes the
// "every variant in one job" property explicit.
func TestTemplateImport_E2E_MaterialsParity_AllContentTypes(t *testing.T) {
	// No t.Parallel(): row-count assertions reference a freshly-created session.
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	// httptest server with one path per content type. The Content-Type header
	// matches the v1 allowlist entry so worker preflight + fetch are happy.
	mux := http.NewServeMux()
	for _, e := range v1MaterialAllowlist {
		e := e // pin
		mux.HandleFunc("/"+e.pathSuffix, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", e.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(e.body)))
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(e.body))
			}
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-mats@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-mats@example.com")
	require.NoError(t, err)
	require.NotNil(t, creator)

	// Build one material element per content type.
	elements := make([]template.Element, 0, len(v1MaterialAllowlist))
	declaredByID := make(map[string]string, len(v1MaterialAllowlist))
	for i, e := range v1MaterialAllowlist {
		id := fmt.Sprintf("mat-%02d", i)
		ct := e.contentType
		title := fmt.Sprintf("Material %d (%s)", i, e.contentType)
		url := srv.URL + "/" + e.pathSuffix
		elements = append(elements, template.Element{
			Kind:        template.KindMaterial,
			ID:          id,
			Title:       &title,
			URL:         &url,
			ContentType: &ct,
		})
		declaredByID[id] = e.contentType
	}

	tmpl := &template.Template{
		Version:  1,
		Title:    "SCRUM-361 — All v1 content-type variants",
		Elements: elements,
	}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors, "preflight must pass: %+v", pre.Errors)

	descriptorJSON, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptorJSON,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))

	deps := template.WorkerDeps{
		DB:         h.DB,
		Storage:    nil, // no R2 in this test; uploadMaterialBody synthesizes a key
		HTTPClient: srv.Client(),
	}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.SessionID, "worker must set session_id")
	assert.Equal(t, models.ImportJobStateSucceeded, got.State,
		"job must be succeeded when every material fetch succeeds: state=%s err=%v", got.State, got.ErrorMessage)

	// Every element must be marked succeeded in elements_state.
	for id := range declaredByID {
		st, ok := got.ElementsState[id]
		require.True(t, ok, "element %s must have a state entry", id)
		assert.Equal(t, models.ImportElementStatusSucceeded, st.Status,
			"element %s must succeed: code=%s msg=%s", id, st.ErrorCode, st.Message)
	}

	// Materials parity: count + content_type per row.
	dstID := *got.SessionID
	mats, err := h.DB.GetActiveMaterialsBySessionID(ctx, dstID)
	require.NoError(t, err)
	require.Len(t, mats, len(v1MaterialAllowlist),
		"materials count must equal declared material elements: declared=%d got=%d",
		len(v1MaterialAllowlist), len(mats))

	gotContentTypes := make(map[string]int, len(mats))
	for _, m := range mats {
		gotContentTypes[m.ContentType]++
	}
	for _, e := range v1MaterialAllowlist {
		assert.Equal(t, 1, gotContentTypes[e.contentType],
			"exactly one materials row expected with content_type=%q; got %d", e.contentType, gotContentTypes[e.contentType])
	}
}

// TestTemplateImport_E2E_MaterialsParity_Count is the focused materials-count
// regression. It declares 3 material elements with the same content type to
// exercise the parity assertion independent of the allowlist coverage above:
// any future change that drops one of N materials must fail this test.
func TestTemplateImport_E2E_MaterialsParity_Count(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		body := "PDF" + r.URL.Path
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(body))
		}
	}))
	defer srv.Close()

	addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "tmpl-count@example.com")
	creator, err := h.DB.GetUserByEmail(ctx, "tmpl-count@example.com")
	require.NoError(t, err)

	const declaredN = 3
	elements := make([]template.Element, 0, declaredN)
	for i := 0; i < declaredN; i++ {
		id := fmt.Sprintf("doc-%02d", i)
		ct := "application/pdf"
		url := fmt.Sprintf("%s/file-%d.pdf", srv.URL, i)
		title := fmt.Sprintf("Doc %d", i)
		elements = append(elements, template.Element{
			Kind:        template.KindMaterial,
			ID:          id,
			Title:       &title,
			URL:         &url,
			ContentType: &ct,
		})
	}
	tmpl := &template.Template{Version: 1, Title: "SCRUM-361 parity count", Elements: elements}

	pre := template.Run(ctx, tmpl, template.Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.Empty(t, pre.Errors, "preflight: %+v", pre.Errors)

	descriptorJSON, _ := json.Marshal(tmpl)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &creator.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptorJSON,
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))

	deps := template.WorkerDeps{DB: h.DB, HTTPClient: srv.Client()}
	require.NoError(t, template.RunImportJob(ctx, deps, job.ID, tmpl, pre, creator.Email))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SessionID)

	mats, err := h.DB.GetActiveMaterialsBySessionID(ctx, *got.SessionID)
	require.NoError(t, err)
	assert.Len(t, mats, declaredN,
		"materials count must equal declared count (%d); a regression that dropped a row would fail here", declaredN)

	for _, m := range mats {
		assert.Equal(t, "application/pdf", m.ContentType, "every row's content_type must match declaration")
	}
}
