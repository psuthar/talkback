// Package googlemeet provides a minimal Google Meet REST API + Drive media
// download client for importing recordings (mirrors internal/msgraph for Teams).
//
// The package speaks Meet API v2 (meet.googleapis.com/v2) and the Drive v3
// /files/{id}?alt=media endpoint. All functions take an *http.Client and an
// access token explicitly so they are easy to stub in tests.
package googlemeet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// API base URLs are package-level variables so tests can swap them.
var (
	meetAPIBase  = "https://meet.googleapis.com/v2"
	driveAPIBase = "https://www.googleapis.com/drive/v3"
)

// Recording state values returned by Meet API. Only FILE_GENERATED indicates
// the MP4 is ready for download.
const (
	stateStarted       = "STARTED"
	stateEnded         = "ENDED"
	StateFileGenerated = "FILE_GENERATED"
)

// IsRecordingReady reports whether a Meet recording is downloadable.
func IsRecordingReady(state string) bool {
	return strings.EqualFold(state, StateFileGenerated)
}

// BackoffWaitingFromState returns how long the pipeline should wait before
// re-checking a recording whose state is not yet FILE_GENERATED. The pipeline's
// generic BackoffWaiting is fine for most cases; this helper just clamps a
// reasonable Meet-specific minimum because Meet artifacts can take 5–15 minutes.
func BackoffWaitingFromState(state string) time.Duration {
	switch strings.ToUpper(state) {
	case stateStarted:
		// Meeting still going; check less aggressively.
		return 5 * time.Minute
	case stateEnded:
		// Meeting just ended; recording usually ready in a few minutes.
		return 2 * time.Minute
	default:
		return 1 * time.Minute
	}
}

// DefaultHTTPClient returns a sane default client. Drive media downloads can
// be large, so the timeout is generous.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Minute}
}

// APIError is returned when a Google API call fails with a non-2xx response.
// Code distinguishes transient (retryable) from permanent (auth/missing) and
// google_quota (429) failures so callers can pick the right backoff.
type APIError struct {
	Code       string // "transient" | "permanent" | "google_quota"
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("googlemeet api error: code=%s status=%d msg=%s", e.Code, e.StatusCode, e.Message)
}

// IsPermanent reports whether the error is a permanent (non-retryable) Google
// API failure. Network errors and unclassified errors return false.
func IsPermanent(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code == "permanent"
	}
	return false
}

// IsTransient reports whether the error is transient (retryable). Network
// errors and unclassified errors are treated as transient by callers; this
// helper only returns true for explicitly-classified APIErrors.
func IsTransient(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code == "transient" || ae.Code == "google_quota"
	}
	return false
}

// IsQuota reports whether the error is a Google quota / rate-limit error
// (HTTP 429). Callers may apply longer backoff than for generic transient
// errors.
func IsQuota(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code == "google_quota"
	}
	return false
}

// classifyHTTPError converts a non-2xx response into an APIError with a
// suitable code. The body is included in the message (truncated) for logs.
func classifyHTTPError(resp *http.Response, body []byte) *APIError {
	msg := truncate(strings.TrimSpace(string(body)), 400)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &APIError{Code: "google_quota", StatusCode: resp.StatusCode, Message: msg}
	case resp.StatusCode >= 500:
		return &APIError{Code: "transient", StatusCode: resp.StatusCode, Message: msg}
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound:
		return &APIError{Code: "permanent", StatusCode: resp.StatusCode, Message: msg}
	default:
		// Treat all other 4xx as permanent (bad request / client error).
		return &APIError{Code: "permanent", StatusCode: resp.StatusCode, Message: msg}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// doJSON executes a GET against url with bearer auth and decodes the JSON body
// into out. Non-2xx returns a classified APIError.
func doJSON(ctx context.Context, httpClient *http.Client, accessToken, url string, out interface{}) error {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyHTTPError(resp, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("googlemeet decode response: %w", err)
	}
	return nil
}

// --- Public response types ---

// ConferenceRecord is one past Google Meet conference. Resource name has the
// form "conferenceRecords/{id}".
type ConferenceRecord struct {
	Name           string `json:"name"`
	StartTime      string `json:"startTime,omitempty"`
	EndTime        string `json:"endTime,omitempty"`
	ExpireTime     string `json:"expireTime,omitempty"`
	Space          string `json:"space,omitempty"`
}

// Recording is a single MP4 recording produced for a conference. Resource name
// has the form "conferenceRecords/{c}/recordings/{r}".
type Recording struct {
	Name             string `json:"name"`
	State            string `json:"state,omitempty"`
	StartTime        string `json:"startTime,omitempty"`
	EndTime          string `json:"endTime,omitempty"`
	DriveDestination *struct {
		File      string `json:"file,omitempty"`
		ExportURI string `json:"exportUri,omitempty"`
	} `json:"driveDestination,omitempty"`
}

// Transcript is a per-conference structured transcript. Resource name has
// the form "conferenceRecords/{c}/transcripts/{t}".
type Transcript struct {
	Name             string `json:"name"`
	State            string `json:"state,omitempty"`
	StartTime        string `json:"startTime,omitempty"`
	EndTime          string `json:"endTime,omitempty"`
	DocsDestination  *struct {
		Document   string `json:"document,omitempty"`
		ExportURI  string `json:"exportUri,omitempty"`
	} `json:"docsDestination,omitempty"`
}

// TranscriptEntry is one structured line of a Meet transcript.
type TranscriptEntry struct {
	Name         string `json:"name"`
	Participant  string `json:"participant,omitempty"`
	Text         string `json:"text,omitempty"`
	LanguageCode string `json:"languageCode,omitempty"`
	StartTime    string `json:"startTime,omitempty"`
	EndTime      string `json:"endTime,omitempty"`
}

// Participant is a Meet participant resource (resolved best-effort to populate
// transcript SpeakerLabel).
type Participant struct {
	Name              string `json:"name"`
	SignedInUser      *struct {
		User        string `json:"user,omitempty"`
		DisplayName string `json:"displayName,omitempty"`
	} `json:"signedinUser,omitempty"`
	AnonymousUser *struct {
		DisplayName string `json:"displayName,omitempty"`
	} `json:"anonymousUser,omitempty"`
	PhoneUser *struct {
		DisplayName string `json:"displayName,omitempty"`
	} `json:"phoneUser,omitempty"`
}

// DisplayName returns the participant's best-available display name. Empty
// string if none can be resolved.
func (p *Participant) DisplayName() string {
	if p == nil {
		return ""
	}
	if p.SignedInUser != nil && p.SignedInUser.DisplayName != "" {
		return p.SignedInUser.DisplayName
	}
	if p.AnonymousUser != nil && p.AnonymousUser.DisplayName != "" {
		return p.AnonymousUser.DisplayName
	}
	if p.PhoneUser != nil && p.PhoneUser.DisplayName != "" {
		return p.PhoneUser.DisplayName
	}
	return ""
}

// RecordingListItem is one importable Google Meet recording (flattened from
// conferenceRecord + recording, enriched with the conference's transcript
// state). transcript_state is "ready" | "pending" | "none".
type RecordingListItem struct {
	ConferenceRecordName string `json:"conference_record_name"`
	RecordingName        string `json:"recording_name"`
	Subject              string `json:"subject"`
	StartTime            string `json:"start_time,omitempty"`
	DriveFileID          string `json:"drive_file_id"`
	ExportURI            string `json:"export_uri,omitempty"`
	State                string `json:"state"`
	TranscriptState      string `json:"transcript_state"`
}

// --- Pagination helpers ---

type conferenceRecordsPage struct {
	ConferenceRecords []ConferenceRecord `json:"conferenceRecords"`
	NextPageToken     string             `json:"nextPageToken,omitempty"`
}

type recordingsPage struct {
	Recordings    []Recording `json:"recordings"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

type transcriptsPage struct {
	Transcripts   []Transcript `json:"transcripts"`
	NextPageToken string       `json:"nextPageToken,omitempty"`
}

type entriesPage struct {
	TranscriptEntries []TranscriptEntry `json:"transcriptEntries"`
	NextPageToken     string            `json:"nextPageToken,omitempty"`
}

// pageSize for list endpoints; Meet API caps at 100.
const defaultPageSize = 50

// maxConferenceRecordPages bounds ListConferenceRecords so a power user with
// thousands of meetings doesn't pay unbounded latency.
const maxConferenceRecordPages = 4

// --- API: list conference records ---

// ListConferenceRecords returns recent past conferences for the authorized
// user. Capped at maxConferenceRecordPages * defaultPageSize records.
func ListConferenceRecords(ctx context.Context, accessToken string, httpClient *http.Client) ([]ConferenceRecord, error) {
	var out []ConferenceRecord
	pageToken := ""
	for i := 0; i < maxConferenceRecordPages; i++ {
		params := url.Values{}
		params.Set("pageSize", fmt.Sprintf("%d", defaultPageSize))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		var page conferenceRecordsPage
		u := meetAPIBase + "/conferenceRecords?" + params.Encode()
		if err := doJSON(ctx, httpClient, accessToken, u, &page); err != nil {
			return nil, err
		}
		out = append(out, page.ConferenceRecords...)
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

// --- API: list recordings under a conferenceRecord ---

// ListRecordings returns the recordings for a conferenceRecord
// (resource name "conferenceRecords/{id}").
func ListRecordings(ctx context.Context, accessToken, conferenceRecordName string, httpClient *http.Client) ([]Recording, error) {
	var out []Recording
	pageToken := ""
	for {
		params := url.Values{}
		params.Set("pageSize", fmt.Sprintf("%d", defaultPageSize))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		u := meetAPIBase + "/" + conferenceRecordName + "/recordings?" + params.Encode()
		var page recordingsPage
		if err := doJSON(ctx, httpClient, accessToken, u, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Recordings...)
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

// GetRecording fetches a single recording by resource name
// (conferenceRecords/{c}/recordings/{r}).
func GetRecording(ctx context.Context, accessToken, recordingName string, httpClient *http.Client) (*Recording, error) {
	u := meetAPIBase + "/" + recordingName
	var rec Recording
	if err := doJSON(ctx, httpClient, accessToken, u, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// --- API: list transcripts under a conferenceRecord ---

// ListTranscripts returns the transcripts for a conferenceRecord. Transcripts
// are per-conference, not per-recording.
func ListTranscripts(ctx context.Context, accessToken, conferenceRecordName string, httpClient *http.Client) ([]Transcript, error) {
	var out []Transcript
	pageToken := ""
	for {
		params := url.Values{}
		params.Set("pageSize", fmt.Sprintf("%d", defaultPageSize))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		u := meetAPIBase + "/" + conferenceRecordName + "/transcripts?" + params.Encode()
		var page transcriptsPage
		if err := doJSON(ctx, httpClient, accessToken, u, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Transcripts...)
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

// GetTranscript fetches a single transcript by resource name.
func GetTranscript(ctx context.Context, accessToken, transcriptName string, httpClient *http.Client) (*Transcript, error) {
	u := meetAPIBase + "/" + transcriptName
	var t Transcript
	if err := doJSON(ctx, httpClient, accessToken, u, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// --- API: list transcript entries ---

// ListTranscriptEntries returns all transcript entries for the given
// transcript, paginating to completion.
func ListTranscriptEntries(ctx context.Context, accessToken, transcriptName string, httpClient *http.Client) ([]TranscriptEntry, error) {
	var out []TranscriptEntry
	pageToken := ""
	for {
		params := url.Values{}
		params.Set("pageSize", fmt.Sprintf("%d", 100))
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		u := meetAPIBase + "/" + transcriptName + "/entries?" + params.Encode()
		var page entriesPage
		if err := doJSON(ctx, httpClient, accessToken, u, &page); err != nil {
			return nil, err
		}
		out = append(out, page.TranscriptEntries...)
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

// --- API: participant lookup ---

// GetParticipant resolves a participant resource (best-effort). 404 returns
// (nil, nil) so callers can fall back to an empty SpeakerLabel.
func GetParticipant(ctx context.Context, accessToken, participantName string, httpClient *http.Client) (*Participant, error) {
	u := meetAPIBase + "/" + participantName
	var p Participant
	err := doJSON(ctx, httpClient, accessToken, u, &p)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// --- Drive media download ---

// DownloadDriveFile streams the file body via Drive v3
// GET /files/{id}?alt=media. Caller must close resp.Body. The response is
// returned even if non-2xx so callers can read error bodies; non-2xx returns
// (resp, nil) with the caller responsible for inspecting StatusCode. This
// matches msgraph.DownloadHTTP's contract.
func DownloadDriveFile(ctx context.Context, accessToken, fileID string, httpClient *http.Client) (*http.Response, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	u := driveAPIBase + "/files/" + url.PathEscape(fileID) + "?alt=media"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return httpClient.Do(req)
}

// --- High-level: flatten + filter + transcript-state ---

// TranscriptStateFromTranscripts derives a UI-friendly state for the
// recordings list: "ready" if any transcript is FILE_GENERATED, "pending"
// if any transcript exists but none is ready, "none" otherwise.
func TranscriptStateFromTranscripts(transcripts []Transcript) string {
	if len(transcripts) == 0 {
		return "none"
	}
	for _, t := range transcripts {
		if strings.EqualFold(t.State, StateFileGenerated) {
			return "ready"
		}
	}
	return "pending"
}

// ListRecordingsAll walks recent conference records, lists each conference's
// recordings + transcripts, and returns one RecordingListItem per recording
// in state FILE_GENERATED. Each item carries its conference's transcript
// state. Capped by maxConferenceRecordPages.
func ListRecordingsAll(ctx context.Context, accessToken string, httpClient *http.Client) ([]RecordingListItem, error) {
	confs, err := ListConferenceRecords(ctx, accessToken, httpClient)
	if err != nil {
		return nil, err
	}
	var out []RecordingListItem
	for _, c := range confs {
		recs, err := ListRecordings(ctx, accessToken, c.Name, httpClient)
		if err != nil {
			return nil, err
		}
		// Only fetch transcripts for conferences that have at least one
		// FILE_GENERATED recording, to save API calls.
		hasReady := false
		for _, r := range recs {
			if IsRecordingReady(r.State) {
				hasReady = true
				break
			}
		}
		var tState string
		if hasReady {
			ts, err := ListTranscripts(ctx, accessToken, c.Name, httpClient)
			if err != nil {
				return nil, err
			}
			tState = TranscriptStateFromTranscripts(ts)
		} else {
			tState = "none"
		}
		subject := subjectFromConference(c)
		for _, r := range recs {
			if !IsRecordingReady(r.State) {
				continue
			}
			item := RecordingListItem{
				ConferenceRecordName: c.Name,
				RecordingName:        r.Name,
				Subject:              subject,
				StartTime:            firstNonEmpty(r.StartTime, c.StartTime),
				State:                r.State,
				TranscriptState:      tState,
			}
			if r.DriveDestination != nil {
				item.DriveFileID = r.DriveDestination.File
				item.ExportURI = r.DriveDestination.ExportURI
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if p != "" {
			return p
		}
	}
	return ""
}

// subjectFromConference returns a display subject for the conference. Meet
// has no native subject field on conferenceRecord, so we fall back to a
// timestamp-based label.
func subjectFromConference(c ConferenceRecord) string {
	if c.StartTime != "" {
		// Try to parse and reformat to a human-friendly label; on parse failure
		// fall back to the raw timestamp.
		if t, err := time.Parse(time.RFC3339, c.StartTime); err == nil {
			return "Google Meet — " + t.UTC().Format("2006-01-02 15:04")
		}
		return "Google Meet — " + c.StartTime
	}
	return "Google Meet recording"
}
