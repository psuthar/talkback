package googlemeet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// withStubServer redirects meetAPIBase + driveAPIBase at a test server and
// restores them after the test. Returns the server's URL prefix.
func withStubServer(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	prevMeet, prevDrive := meetAPIBase, driveAPIBase
	meetAPIBase = srv.URL + "/v2"
	driveAPIBase = srv.URL + "/drive/v3"
	t.Cleanup(func() {
		meetAPIBase = prevMeet
		driveAPIBase = prevDrive
	})
	return srv.URL
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func TestIsRecordingReady(t *testing.T) {
	require.True(t, IsRecordingReady("FILE_GENERATED"))
	require.True(t, IsRecordingReady("file_generated"))
	require.False(t, IsRecordingReady("STARTED"))
	require.False(t, IsRecordingReady(""))
}

func TestBackoffWaitingFromState(t *testing.T) {
	require.Greater(t, BackoffWaitingFromState("STARTED").Seconds(), float64(60))
	require.Greater(t, BackoffWaitingFromState("ENDED").Seconds(), float64(30))
	require.Greater(t, BackoffWaitingFromState("UNKNOWN").Seconds(), float64(0))
}

func TestTranscriptStateFromTranscripts(t *testing.T) {
	require.Equal(t, "none", TranscriptStateFromTranscripts(nil))
	require.Equal(t, "none", TranscriptStateFromTranscripts([]Transcript{}))
	require.Equal(t, "pending", TranscriptStateFromTranscripts([]Transcript{
		{State: "STARTED"},
		{State: "ENDED"},
	}))
	require.Equal(t, "ready", TranscriptStateFromTranscripts([]Transcript{
		{State: "STARTED"},
		{State: "FILE_GENERATED"},
	}))
}

func TestClassifyHTTPError_Branches(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{500, "transient"},
		{502, "transient"},
		{503, "transient"},
		{429, "google_quota"},
		{401, "permanent"},
		{403, "permanent"},
		{404, "permanent"},
		{400, "permanent"},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("err body"))
			})
			withStubServer(t, h)
			err := doJSON(context.Background(), nil, "tok", meetAPIBase+"/conferenceRecords", nil)
			require.Error(t, err)
			var ae *APIError
			require.True(t, errors.As(err, &ae), "want APIError, got %T", err)
			require.Equal(t, tc.want, ae.Code, "status %d should classify as %s", tc.status, tc.want)
			require.Equal(t, tc.status, ae.StatusCode)
		})
	}
}

func TestErrorClassifierHelpers(t *testing.T) {
	require.True(t, IsPermanent(&APIError{Code: "permanent"}))
	require.False(t, IsPermanent(&APIError{Code: "transient"}))
	require.True(t, IsTransient(&APIError{Code: "transient"}))
	require.True(t, IsTransient(&APIError{Code: "google_quota"}))
	require.True(t, IsQuota(&APIError{Code: "google_quota"}))
	require.False(t, IsQuota(&APIError{Code: "transient"}))
	// Non-APIError values return false.
	require.False(t, IsPermanent(errors.New("boom")))
	require.False(t, IsTransient(errors.New("boom")))
}

func TestListConferenceRecords_Paginates_And_AuthHeader(t *testing.T) {
	var requests []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.Equal(t, "/v2/conferenceRecords", r.URL.Path)
		switch r.URL.Query().Get("pageToken") {
		case "":
			writeJSON(t, w, conferenceRecordsPage{
				ConferenceRecords: []ConferenceRecord{{Name: "conferenceRecords/A", StartTime: "2026-05-01T10:00:00Z"}},
				NextPageToken:     "tok2",
			})
		case "tok2":
			writeJSON(t, w, conferenceRecordsPage{
				ConferenceRecords: []ConferenceRecord{{Name: "conferenceRecords/B"}},
			})
		default:
			t.Fatalf("unexpected pageToken: %q", r.URL.Query().Get("pageToken"))
		}
	})
	withStubServer(t, h)
	confs, err := ListConferenceRecords(context.Background(), "test-token", nil)
	require.NoError(t, err)
	require.Len(t, confs, 2)
	require.Equal(t, "conferenceRecords/A", confs[0].Name)
	require.Equal(t, "conferenceRecords/B", confs[1].Name)
	require.Len(t, requests, 2)
	require.Contains(t, requests[1], "pageToken=tok2")
}

func TestListRecordings_Paginates(t *testing.T) {
	pages := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/conferenceRecords/A/recordings", r.URL.Path)
		pages++
		if r.URL.Query().Get("pageToken") == "" {
			writeJSON(t, w, recordingsPage{
				Recordings:    []Recording{{Name: "conferenceRecords/A/recordings/1", State: "FILE_GENERATED"}},
				NextPageToken: "p2",
			})
		} else {
			writeJSON(t, w, recordingsPage{
				Recordings: []Recording{{Name: "conferenceRecords/A/recordings/2", State: "ENDED"}},
			})
		}
	})
	withStubServer(t, h)
	recs, err := ListRecordings(context.Background(), "tok", "conferenceRecords/A", nil)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	require.Equal(t, 2, pages)
}

func TestListTranscriptEntries_Paginates(t *testing.T) {
	pages := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/conferenceRecords/A/transcripts/T/entries", r.URL.Path)
		pages++
		if r.URL.Query().Get("pageToken") == "" {
			writeJSON(t, w, entriesPage{
				TranscriptEntries: []TranscriptEntry{
					{Name: "e1", Text: "hello", StartTime: "2026-05-01T10:00:00Z", EndTime: "2026-05-01T10:00:02Z", Participant: "participants/P1"},
				},
				NextPageToken: "p2",
			})
		} else {
			writeJSON(t, w, entriesPage{
				TranscriptEntries: []TranscriptEntry{
					{Name: "e2", Text: "world", StartTime: "2026-05-01T10:00:03Z", EndTime: "2026-05-01T10:00:05Z", Participant: "participants/P2"},
				},
			})
		}
	})
	withStubServer(t, h)
	entries, err := ListTranscriptEntries(context.Background(), "tok", "conferenceRecords/A/transcripts/T", nil)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "hello", entries[0].Text)
	require.Equal(t, "world", entries[1].Text)
	require.Equal(t, 2, pages)
}

func TestGetParticipant_404ReturnsNil(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	})
	withStubServer(t, h)
	p, err := GetParticipant(context.Background(), "tok", "conferenceRecords/A/participants/X", nil)
	require.NoError(t, err)
	require.Nil(t, p)
}

func TestParticipantDisplayName(t *testing.T) {
	require.Equal(t, "", (*Participant)(nil).DisplayName())
	signed := &Participant{SignedInUser: &struct {
		User        string `json:"user,omitempty"`
		DisplayName string `json:"displayName,omitempty"`
	}{DisplayName: "Alice"}}
	require.Equal(t, "Alice", signed.DisplayName())
	anon := &Participant{AnonymousUser: &struct {
		DisplayName string `json:"displayName,omitempty"`
	}{DisplayName: "Guest"}}
	require.Equal(t, "Guest", anon.DisplayName())
	phone := &Participant{PhoneUser: &struct {
		DisplayName string `json:"displayName,omitempty"`
	}{DisplayName: "+1 555"}}
	require.Equal(t, "+1 555", phone.DisplayName())
	require.Equal(t, "", (&Participant{}).DisplayName())
}

func TestDownloadDriveFile_StreamsBodyAndAuth(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/drive/v3/files/file-123", r.URL.Path)
		require.Equal(t, "media", r.URL.Query().Get("alt"))
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MP4-BYTES"))
	})
	withStubServer(t, h)
	resp, err := DownloadDriveFile(context.Background(), "tok", "file-123", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "MP4-BYTES", string(body))
	require.Equal(t, "video/mp4", resp.Header.Get("Content-Type"))
}

func TestListRecordingsAll_FlattenFilterAndTranscriptState(t *testing.T) {
	// Two conference records:
	//   A: 2 recordings, one FILE_GENERATED, one ENDED; transcript FILE_GENERATED → state ready
	//   B: 1 recording FILE_GENERATED; transcripts list empty → state none
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/conferenceRecords":
			writeJSON(t, w, conferenceRecordsPage{ConferenceRecords: []ConferenceRecord{
				{Name: "conferenceRecords/A", StartTime: "2026-05-01T09:00:00Z"},
				{Name: "conferenceRecords/B", StartTime: "2026-05-02T14:30:00Z"},
			}})
		case r.URL.Path == "/v2/conferenceRecords/A/recordings":
			writeJSON(t, w, recordingsPage{Recordings: []Recording{
				{
					Name:      "conferenceRecords/A/recordings/r1",
					State:     "FILE_GENERATED",
					StartTime: "2026-05-01T09:00:00Z",
					DriveDestination: &struct {
						File      string `json:"file,omitempty"`
						ExportURI string `json:"exportUri,omitempty"`
					}{File: "drive-1", ExportURI: "https://drive.google.com/file/d/drive-1/view"},
				},
				{Name: "conferenceRecords/A/recordings/r2", State: "ENDED"},
			}})
		case r.URL.Path == "/v2/conferenceRecords/A/transcripts":
			writeJSON(t, w, transcriptsPage{Transcripts: []Transcript{
				{Name: "conferenceRecords/A/transcripts/t1", State: "FILE_GENERATED"},
			}})
		case r.URL.Path == "/v2/conferenceRecords/B/recordings":
			writeJSON(t, w, recordingsPage{Recordings: []Recording{
				{
					Name:      "conferenceRecords/B/recordings/r1",
					State:     "FILE_GENERATED",
					StartTime: "2026-05-02T14:30:00Z",
					DriveDestination: &struct {
						File      string `json:"file,omitempty"`
						ExportURI string `json:"exportUri,omitempty"`
					}{File: "drive-B"},
				},
			}})
		case r.URL.Path == "/v2/conferenceRecords/B/transcripts":
			writeJSON(t, w, transcriptsPage{Transcripts: nil})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	withStubServer(t, h)
	items, err := ListRecordingsAll(context.Background(), "tok", nil)
	require.NoError(t, err)
	require.Len(t, items, 2, "ENDED recordings should be filtered out")
	require.Equal(t, "conferenceRecords/A/recordings/r1", items[0].RecordingName)
	require.Equal(t, "drive-1", items[0].DriveFileID)
	require.Equal(t, "https://drive.google.com/file/d/drive-1/view", items[0].ExportURI)
	require.Equal(t, "ready", items[0].TranscriptState)
	require.True(t, strings.HasPrefix(items[0].Subject, "Google Meet — 2026-05-01"))
	require.Equal(t, "conferenceRecords/B/recordings/r1", items[1].RecordingName)
	require.Equal(t, "none", items[1].TranscriptState)
}

func TestListRecordingsAll_NoReadyRecordings_SkipsTranscriptsCall(t *testing.T) {
	transcriptsCalled := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/conferenceRecords":
			writeJSON(t, w, conferenceRecordsPage{ConferenceRecords: []ConferenceRecord{
				{Name: "conferenceRecords/X", StartTime: "2026-05-01T09:00:00Z"},
			}})
		case "/v2/conferenceRecords/X/recordings":
			writeJSON(t, w, recordingsPage{Recordings: []Recording{
				{Name: "conferenceRecords/X/recordings/r1", State: "STARTED"},
			}})
		case "/v2/conferenceRecords/X/transcripts":
			transcriptsCalled = true
			writeJSON(t, w, transcriptsPage{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	withStubServer(t, h)
	items, err := ListRecordingsAll(context.Background(), "tok", nil)
	require.NoError(t, err)
	require.Empty(t, items)
	require.False(t, transcriptsCalled, "transcripts endpoint should not be hit when no recording is FILE_GENERATED")
}

func TestListConferenceRecords_PageCap(t *testing.T) {
	pageCount := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		writeJSON(t, w, conferenceRecordsPage{
			ConferenceRecords: []ConferenceRecord{{Name: "conferenceRecords/X"}},
			NextPageToken:     "always-more",
		})
	})
	withStubServer(t, h)
	confs, err := ListConferenceRecords(context.Background(), "tok", nil)
	require.NoError(t, err)
	require.Len(t, confs, maxConferenceRecordPages, "should cap at maxConferenceRecordPages")
	require.Equal(t, maxConferenceRecordPages, pageCount)
}

func TestDoJSON_DecodeError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json {{"))
	})
	withStubServer(t, h)
	var out conferenceRecordsPage
	err := doJSON(context.Background(), nil, "tok", meetAPIBase+"/conferenceRecords", &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "googlemeet decode response")
}
