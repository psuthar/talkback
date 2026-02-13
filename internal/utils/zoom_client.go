package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ZoomAPIError is returned when a Zoom API call fails; use for retry/waiting/permanent classification (Mission #4).
type ZoomAPIError struct {
	StatusCode int    // HTTP status (429, 404, 401, 403, 5xx)
	Message    string // response body or description
	Code       string // stable code: zoom_429, zoom_5xx, recording_not_found, transcript_not_ready, zoom_auth
	NotReady   bool   // true if transcript not available yet (processing or missing) → state=waiting
}

func (e *ZoomAPIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("zoom api: %s (%d): %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("zoom api (%d): %s", e.StatusCode, e.Message)
}

// Retryable returns true for 429, 5xx, network/timeouts → failed_transient.
func (e *ZoomAPIError) Retryable() bool {
	return e.StatusCode == 429 || (e.StatusCode >= 500 && e.StatusCode < 600)
}

// Permanent returns true for auth (401/403) or recording not found (404) after retries → failed_permanent.
func (e *ZoomAPIError) Permanent() bool {
	return e.StatusCode == 401 || e.StatusCode == 403 || e.Code == "recording_not_found"
}

// IsZoomAPIError returns the *ZoomAPIError if err wraps one.
func IsZoomAPIError(err error) (*ZoomAPIError, bool) {
	var z *ZoomAPIError
	ok := errors.As(err, &z)
	return z, ok
}

// ZoomRecordingFile represents a file in a Zoom cloud recording
type ZoomRecordingFile struct {
	ID             string `json:"id"`
	MeetingID      string `json:"meeting_id"`
	RecordingStart string `json:"recording_start"`
	RecordingEnd   string `json:"recording_end"`
	FileType       string `json:"file_type"`      // e.g. "VTT", "CC", "MP4", "TRANSCRIPT"
	FileExtension  string `json:"file_extension"` // e.g. "VTT", "MP4" (Zoom uses TRANSCRIPT + VTT for transcripts)
	FileSize       int64  `json:"file_size"`
	DownloadURL    string `json:"download_url"`
	Status         string `json:"status"`
}

// ZoomMeetingsListResponse from GET /users/{userId}/recordings (list recordings)
type ZoomMeetingsListResponse struct {
	Meetings      []ZoomRecordingResponse `json:"meetings"`
	PageCount     int                     `json:"page_count"`
	PageSize      int                     `json:"page_size"`
	TotalRecords  int                     `json:"total_records"`
	NextPageToken string                  `json:"next_page_token,omitempty"`
}

// ZoomRecordingResponse from GET /meetings/{id}/recordings
type ZoomRecordingResponse struct {
	UUID           string              `json:"uuid"`
	ID             int64               `json:"id"`
	AccountID      string              `json:"account_id"`
	HostID         string              `json:"host_id"`
	Topic          string              `json:"topic"`
	StartTime      string              `json:"start_time"`
	Duration       int                 `json:"duration"`
	TotalSize      int64               `json:"total_size"`
	RecordingCount int                 `json:"recording_count"`
	RecordingFiles []ZoomRecordingFile `json:"recording_files"`
}

// ZoomPastMeetingInstance from past_meetings/instances
type ZoomPastMeetingInstance struct {
	UUID      string `json:"uuid"`
	StartTime string `json:"start_time"`
	Status    string `json:"status"`
}

// ZoomPastMeetingsResponse from GET /past_meetings/{id}/instances
type ZoomPastMeetingsResponse struct {
	Meetings []ZoomPastMeetingInstance `json:"meetings"`
}

// IsZoomShareLink returns true if the URL is a Zoom rec/share link. Share links
// cannot be used for transcript import; use recording detail URL instead.
func IsZoomShareLink(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".zoom.us") && host != "zoom.us" {
		return false
	}
	return strings.Contains(u.Path, "/rec/share/")
}

// ParseZoomRecordingURL extracts meeting identifier from a Zoom recording URL.
// Supports: zoom.us/recording/detail?meeting_id=... (URL-decoded once),
// zoom.us/rec/play/..., zoom.us/rec/share/... (path UUID), us02web.zoom.us/rec/...
func ParseZoomRecordingURL(rawURL string) (meetingUUID string, err error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	path := u.Path

	// Recording detail URL: /recording/detail?meeting_id=... (Query().Get decodes once)
	if strings.Contains(path, "/recording/detail") {
		meetingID := strings.TrimSpace(u.Query().Get("meeting_id"))
		if meetingID == "" {
			return "", fmt.Errorf("recording detail URL missing meeting_id query parameter")
		}
		return meetingID, nil
	}

	// Pattern: /rec/play/UUID or /rec/share/UUID or /rec/share/xxx?xxx
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "play" || p == "share" {
			if i+1 < len(parts) {
				candidate := parts[i+1]
				// Remove query if present
				if idx := strings.Index(candidate, "?"); idx >= 0 {
					candidate = candidate[:idx]
				}
				// UUID-like (with or without hyphens)
				if len(candidate) >= 32 {
					return candidate, nil
				}
			}
			break
		}
	}
	// Fallback: try to find UUID in path (regex)
	uuidRe := regexp.MustCompile(`[a-zA-Z0-9_-]{20,}`)
	if m := uuidRe.FindString(path); m != "" {
		return m, nil
	}
	return "", fmt.Errorf("could not extract meeting identifier from Zoom URL")
}

// pathEncodeMeetingID encodes meetingID for use in a URL path. doubleEncode=true uses
// double encoding (e.g. for APIs that expect %252B instead of %2B for +).
func pathEncodeMeetingID(meetingID string, doubleEncode bool) string {
	encoded := url.PathEscape(meetingID)
	if doubleEncode {
		encoded = url.PathEscape(encoded)
	}
	return encoded
}

// GetMeetingRecordings returns cloud recording for a meeting (by UUID or numeric ID).
// Uses path-encoded meeting ID (url.PathEscape). Callers may retry on 404 with
// url.QueryUnescape(meetingID) when the ID came from a recording/detail URL that
// might be double-encoded.
func GetMeetingRecordings(accessToken, meetingID string) (*ZoomRecordingResponse, error) {
	return getMeetingRecordingsWithEncoding(accessToken, meetingID, false)
}

// GetMeetingRecordingsWithRetry calls GetMeetingRecordings and on 404 retries with
// double-encoded ID, QueryUnescaped ID, and past_meetings/instances (for recurring meetings).
func GetMeetingRecordingsWithRetry(accessToken, meetingID string) (*ZoomRecordingResponse, error) {
	rec, err := getMeetingRecordingsWithEncoding(accessToken, meetingID, false)
	if err == nil {
		return rec, nil
	}
	var z *ZoomAPIError
	if errors.As(err, &z) && z.Code != "recording_not_found" {
		return nil, err
	}
	// Retry with double encoding (e.g. + in UUID)
	rec, err = getMeetingRecordingsWithEncoding(accessToken, meetingID, true)
	if err == nil {
		return rec, nil
	}
	// Retry with unescaped ID in case the stored ID was encoded
	if unescaped, uErr := url.QueryUnescape(meetingID); uErr == nil && unescaped != meetingID {
		rec, err = getMeetingRecordingsWithEncoding(accessToken, unescaped, false)
		if err == nil {
			return rec, nil
		}
	}
	// Fallback: ID might be recurring meeting UUID; get instance UUIDs and try each
	instances, iErr := GetPastMeetingInstances(accessToken, meetingID)
	if iErr == nil && len(instances.Meetings) > 0 {
		for _, inst := range instances.Meetings {
			rec, err = getMeetingRecordingsWithEncoding(accessToken, inst.UUID, false)
			if err == nil {
				return rec, nil
			}
		}
	}
	return nil, &ZoomAPIError{StatusCode: 404, Message: "recording not found", Code: "recording_not_found"}
}

// GetMeetingRecordingsAndTranscript returns recordings and the transcript file if ready.
// If transcript is missing or still processing, returns (rec, nil, *ZoomAPIError{NotReady: true}).
func GetMeetingRecordingsAndTranscript(accessToken, meetingID string) (*ZoomRecordingResponse, *ZoomRecordingFile, error) {
	rec, err := GetMeetingRecordingsWithRetry(accessToken, meetingID)
	if err != nil {
		return nil, nil, err
	}
	file, status := FindTranscriptFileWithStatus(rec.RecordingFiles)
	if file == nil {
		return rec, nil, &ZoomAPIError{StatusCode: 200, Code: "transcript_not_ready", Message: "no transcript file available", NotReady: true}
	}
	if status == TranscriptStatusProcessing {
		return rec, nil, &ZoomAPIError{StatusCode: 200, Code: "transcript_not_ready", Message: "transcript still processing", NotReady: true}
	}
	return rec, file, nil
}

func getMeetingRecordingsWithEncoding(accessToken, meetingID string, doubleEncode bool) (*ZoomRecordingResponse, error) {
	encoded := pathEncodeMeetingID(meetingID, doubleEncode)
	apiURL := "https://api.zoom.us/v2/meetings/" + encoded + "/recordings"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 404 {
		return nil, &ZoomAPIError{StatusCode: 404, Message: "recording not found", Code: "recording_not_found"}
	}
	if resp.StatusCode == 429 {
		return nil, &ZoomAPIError{StatusCode: 429, Message: string(body), Code: "zoom_429"}
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, &ZoomAPIError{StatusCode: resp.StatusCode, Message: string(body), Code: "zoom_auth"}
	}
	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		return nil, &ZoomAPIError{StatusCode: resp.StatusCode, Message: string(body), Code: "zoom_5xx"}
	}
	if resp.StatusCode != 200 {
		return nil, &ZoomAPIError{StatusCode: resp.StatusCode, Message: string(body), Code: "zoom_api"}
	}
	var out ZoomRecordingResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecordingListItem is a simplified recording item for UI listing.
type RecordingListItem struct {
	MeetingTopic    string `json:"meeting_topic"`
	StartTime       string `json:"start_time"`
	DurationMinutes int    `json:"duration_minutes"`
	MeetingUUID     string `json:"meeting_uuid"`
	InstanceUUID    string `json:"instance_uuid,omitempty"`
	HasVideo        bool   `json:"has_video"`
	HasTranscript   bool   `json:"has_transcript"`
	RecordingCount  int    `json:"recording_count"`
}

// ListUserRecordings fetches cloud recordings for a user (GET /users/{userId}/recordings).
// from, to: YYYY-MM-DD; default last 14 days if omitted.
func ListUserRecordings(accessToken, zoomUserID, from, to string) ([]RecordingListItem, error) {
	if from == "" || to == "" {
		// default last 14 days
		// handled in caller or we can set here
	}
	encoded := pathEncodeMeetingID(zoomUserID, false) // zoom user id typically no special chars
	apiURL := "https://api.zoom.us/v2/users/" + encoded + "/recordings?page_size=100"
	if from != "" {
		apiURL += "&from=" + from
	}
	if to != "" {
		apiURL += "&to=" + to
	}
	var all []RecordingListItem
	for {
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("zoom api error (%d): %s", resp.StatusCode, string(body))
		}
		var list ZoomMeetingsListResponse
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		for _, m := range list.Meetings {
			_, transcriptStatus := FindTranscriptFileWithStatus(m.RecordingFiles)
			hasTranscript := transcriptStatus != TranscriptStatusNotAvailable
			hasVideo := FindMP4RecordingFile(m.RecordingFiles) != nil
			all = append(all, RecordingListItem{
				MeetingTopic:    m.Topic,
				StartTime:       m.StartTime,
				DurationMinutes: m.Duration,
				MeetingUUID:     m.UUID,
				InstanceUUID:    m.UUID, // same as meeting for single instance; recurring uses instance UUID
				HasVideo:        hasVideo,
				HasTranscript:   hasTranscript,
				RecordingCount:  m.RecordingCount,
			})
		}
		if list.NextPageToken == "" {
			break
		}
		apiURL = "https://api.zoom.us/v2/users/" + encoded + "/recordings?page_size=100&next_page_token=" + url.QueryEscape(list.NextPageToken)
		if from != "" {
			apiURL += "&from=" + from
		}
		if to != "" {
			apiURL += "&to=" + to
		}
	}
	return all, nil
}

// GetPastMeetingInstances returns past meeting instances for a meeting UUID.
// Tries single-encoded path first; on non-200 retries with double-encoded UUID.
func GetPastMeetingInstances(accessToken, meetingUUID string) (*ZoomPastMeetingsResponse, error) {
	out, err := getPastMeetingInstancesWithEncoding(accessToken, meetingUUID, false)
	if err != nil {
		out2, err2 := getPastMeetingInstancesWithEncoding(accessToken, meetingUUID, true)
		if err2 == nil {
			return out2, nil
		}
	}
	return out, err
}

func getPastMeetingInstancesWithEncoding(accessToken, meetingUUID string, doubleEncode bool) (*ZoomPastMeetingsResponse, error) {
	encoded := pathEncodeMeetingID(meetingUUID, doubleEncode)
	apiURL := "https://api.zoom.us/v2/past_meetings/" + encoded + "/instances"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("zoom api error: %s", string(body))
	}
	var out ZoomPastMeetingsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TranscriptStatus is the availability of a Zoom transcript for a recording.
const (
	TranscriptStatusReady        = "ready"         // transcript file exists and is completed
	TranscriptStatusProcessing   = "processing"    // transcript file exists but still processing
	TranscriptStatusNotAvailable = "not_available" // no transcript file (not enabled for recording)
)

// FindMP4RecordingFile returns the first MP4 recording file (video) for in-app playback.
func FindMP4RecordingFile(files []ZoomRecordingFile) *ZoomRecordingFile {
	for i := range files {
		f := &files[i]
		if strings.ToUpper(strings.TrimSpace(f.FileType)) == "MP4" && f.DownloadURL != "" {
			return f
		}
	}
	return nil
}

// FindTranscriptFile returns the first recording file that is a transcript (VTT/CC)
func FindTranscriptFile(files []ZoomRecordingFile) *ZoomRecordingFile {
	f, _ := FindTranscriptFileWithStatus(files)
	return f
}

// FindTranscriptFileWithStatus returns the best transcript file (prefer VTT over CC) and its status.
// Treats file_type "VTT", "CC", or "TRANSCRIPT" (with file_extension "VTT"/"CC") as transcript.
// Status is TranscriptStatusReady (file status "completed"), TranscriptStatusProcessing (file status "processing"),
// or TranscriptStatusNotAvailable if no transcript file exists.
func FindTranscriptFileWithStatus(files []ZoomRecordingFile) (*ZoomRecordingFile, string) {
	var firstVTT, firstCC *ZoomRecordingFile
	var statusVTT, statusCC string
	for i := range files {
		f := &files[i]
		ft := strings.ToUpper(strings.TrimSpace(f.FileType))
		ext := strings.ToUpper(strings.TrimSpace(f.FileExtension))
		// Zoom can return file_type "TRANSCRIPT" with file_extension "VTT"
		isVTT := ft == "VTT" || (ft == "TRANSCRIPT" && ext == "VTT")
		isCC := ft == "CC" || (ft == "TRANSCRIPT" && (ext == "CC" || ext == ""))
		if !isVTT && !isCC {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(f.Status))
		var st string
		switch status {
		case "completed":
			st = TranscriptStatusReady
		case "processing":
			st = TranscriptStatusProcessing
		default:
			st = TranscriptStatusProcessing
		}
		if isVTT && firstVTT == nil {
			firstVTT = f
			statusVTT = st
		}
		if isCC && firstCC == nil {
			firstCC = f
			statusCC = st
		}
	}
	// Prefer VTT over CC
	if firstVTT != nil {
		return firstVTT, statusVTT
	}
	if firstCC != nil {
		return firstCC, statusCC
	}
	return nil, TranscriptStatusNotAvailable
}

// DownloadTranscript fetches the transcript file content from download_url (with token).
// Returns *ZoomAPIError with NotReady=true on 404 (transcript not ready yet), Retryable on 429/5xx.
func DownloadTranscript(downloadURL, accessToken string) ([]byte, error) {
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return nil, &ZoomAPIError{StatusCode: 404, Code: "transcript_not_ready", Message: string(body), NotReady: true}
	}
	if resp.StatusCode == 429 {
		return nil, &ZoomAPIError{StatusCode: 429, Message: string(body), Code: "zoom_429"}
	}
	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		return nil, &ZoomAPIError{StatusCode: resp.StatusCode, Message: string(body), Code: "zoom_5xx"}
	}
	if resp.StatusCode != 200 {
		return nil, &ZoomAPIError{StatusCode: resp.StatusCode, Message: string(body), Code: "zoom_download"}
	}
	return body, nil
}
