package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/transcript"
	"github.com/psuthar/talkback/internal/utils"
)

// SessionImportZoomRequest body for POST /api/sessions/:id/import/zoom
type SessionImportZoomRequest struct {
	MeetingUUID  string `json:"meeting_uuid"`
	InstanceUUID string `json:"instance_uuid,omitempty"`
}

// SessionImportZoomResponse for 202 response
type SessionImportZoomResponse struct {
	JobID string `json:"job_id"`
	State string `json:"state"`
}

// IngestionStatusResponse for GET /api/sessions/:id/ingestion
type IngestionStatusResponse struct {
	Source       string `json:"source"`
	State        string `json:"state"`
	LastError    string `json:"last_error,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	MeetingUUID  string `json:"meeting_uuid,omitempty"`  // For Retry
	InstanceUUID string `json:"instance_uuid,omitempty"` // For Retry
}

// SessionImportZoom handles POST /api/sessions/:id/import/zoom
func (h *Handlers) SessionImportZoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "creator_identity required"})
		return
	}
	// Parse session ID from path: /api/sessions/:id/import/zoom or /sessions/:id/import/zoom
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "import" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[3] == "import" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Session not found"})
		return
	}
	var req SessionImportZoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Invalid request body"})
		return
	}
	if strings.TrimSpace(req.MeetingUUID) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "meeting_uuid required"})
		return
	}
	meetingUUID := strings.TrimSpace(req.MeetingUUID)
	instanceUUID := strings.TrimSpace(req.InstanceUUID)
	if instanceUUID == "" {
		instanceUUID = meetingUUID
	}
	accessToken, _, err := h.GetValidZoomAccessToken(r, creatorIdentity)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "Zoom not connected. Connect Zoom first."})
		return
	}
	jobID := uuid.New()
	meetingUUIDPtr := &meetingUUID
	instanceUUIDPtr := &instanceUUID
	job := &models.IngestionJob{
		ID:           jobID,
		SessionID:    sessionID,
		Source:       "zoom",
		State:        "queued",
		MeetingUUID:  meetingUUIDPtr,
		InstanceUUID: instanceUUIDPtr,
	}
	if err := h.DB.CreateIngestionJob(r.Context(), job); err != nil {
		log.Printf("Create ingestion job error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create import job"})
		return
	}
	// Run ingestion asynchronously
	go runZoomIngestion(context.Background(), h, sessionID, jobID, instanceUUID, accessToken, creatorIdentity)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(SessionImportZoomResponse{
		JobID: jobID.String(),
		State: "queued",
	})
}

func runZoomIngestion(ctx context.Context, h *Handlers, sessionID, jobID uuid.UUID, meetingUUID, accessToken, creatorIdentity string) {
	_ = h.DB.UpdateIngestionJobState(ctx, jobID, "fetching", nil)
	rec, err := utils.GetMeetingRecordingsWithRetry(accessToken, meetingUUID)
	if err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion fetch error: %v", err)
		return
	}
	transcriptFile := utils.FindTranscriptFile(rec.RecordingFiles)
	if transcriptFile == nil {
		errStr := "No transcript file available for this recording"
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		return
	}
	content, err := utils.DownloadTranscript(transcriptFile.DownloadURL, accessToken)
	if err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion download transcript error: %v", err)
		return
	}
	vttContent := string(content)
	parsed, err := transcript.ParseVTT(bytes.NewReader(content))
	if err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion parse VTT error: %v", err)
		return
	}
	// Create transcript row (parsing) — idempotent via unique(session_id, source)
	transcriptRow := &models.Transcript{
		SessionID: sessionID,
		Source:    "zoom",
		Status:    models.TranscriptStatusParsing,
	}
	if err := h.DB.CreateTranscript(ctx, transcriptRow); err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion create transcript error: %v", err)
		return
	}
	// Build segment rows and video-source segment slice
	segmentRows := make([]models.TranscriptSegmentRow, 0, len(parsed))
	videoSegments := make([]models.TranscriptSegment, 0, len(parsed))
	var rawParts []string
	for i, s := range parsed {
		segmentRows = append(segmentRows, models.TranscriptSegmentRow{
			TranscriptID: transcriptRow.ID,
			SessionID:    sessionID,
			Idx:          i,
			StartMs:      s.StartMs,
			EndMs:        s.EndMs,
			Text:         s.Text,
			SpeakerLabel: s.SpeakerLabel,
			SourceRef:    s.SourceRef,
		})
		videoSegments = append(videoSegments, models.TranscriptSegment{
			StartTime: float64(s.StartMs) / 1000,
			EndTime:   float64(s.EndMs) / 1000,
			Text:      s.Text,
		})
		rawParts = append(rawParts, s.Text)
	}
	rawText := strings.Join(rawParts, "\n")
	// Idempotent: delete existing segments then insert
	if err := h.DB.DeleteSegmentsByTranscriptID(ctx, transcriptRow.ID); err != nil {
		log.Printf("Zoom ingestion delete segments (non-fatal): %v", err)
	}
	if err := h.DB.InsertTranscriptSegments(ctx, transcriptRow.ID, sessionID, segmentRows); err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateTranscriptStatus(ctx, transcriptRow.ID, models.TranscriptStatusFailed, nil, &errStr)
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion insert segments error: %v", err)
		return
	}
	if err := h.DB.UpdateTranscriptStatus(ctx, transcriptRow.ID, models.TranscriptStatusReady, &rawText, nil); err != nil {
		log.Printf("Zoom ingestion update transcript status (non-fatal): %v", err)
	}
	title := rec.Topic
	if title == "" {
		title = "Zoom Recording"
	}
	artifact, err := h.DB.CreateArtifact(ctx, sessionID, title, nil)
	if err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion create artifact error: %v", err)
		return
	}
	// Encode meeting_id so + and other chars are not corrupted when URL is parsed later (e.g. when streaming)
	zoomURL := "https://zoom.us/recording/detail?meeting_id=" + url.QueryEscape(meetingUUID)
	videoID := uuid.New()
	zoomAPI := "zoom_api"
	videoSource := &models.VideoSource{
		ID:                  videoID,
		ArtifactID:          artifact.ID,
		SessionID:           sessionID,
		Provider:            "zoom",
		VideoURL:            zoomURL,
		PlaybackMode:        "embed",
		OriginalURL:         &zoomURL,
		TranscriptStatus:    models.VideoTranscriptStatusReady,
		TranscriptionSource: &zoomAPI,
		SourceType:          models.VideoSourceTypeEmbedURL,
	}
	if err := h.DB.CreateVideoSource(ctx, videoSource); err != nil {
		errStr := err.Error()
		_ = h.DB.UpdateIngestionJobState(ctx, jobID, "failed", &errStr)
		log.Printf("Zoom ingestion create video source error: %v", err)
		return
	}
	if err := h.DB.UpdateVideoSourceZoomTranscript(ctx, videoID, rawText, &vttContent, videoSegments); err != nil {
		log.Printf("Zoom ingestion update video source transcript (non-fatal): %v", err)
	}
	_ = h.DB.UpdateIngestionJobState(ctx, jobID, "ready", nil)
	rag.IndexSessionAsync(sessionID, h.DB)
}

// SessionIngestionStatus handles GET /api/sessions/:id/ingestion
func (h *Handlers) SessionIngestionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "ingestion" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[3] == "ingestion" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	job, err := h.DB.GetIngestionJobBySessionID(r.Context(), sessionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	if job == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"source": "", "state": "", "updated_at": ""})
		return
	}
	lastError := ""
	if job.LastError != nil {
		lastError = *job.LastError
	}
	meetingUUID := ""
	if job.MeetingUUID != nil {
		meetingUUID = *job.MeetingUUID
	}
	instanceUUID := ""
	if job.InstanceUUID != nil {
		instanceUUID = *job.InstanceUUID
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(IngestionStatusResponse{
		Source:       job.Source,
		State:        job.State,
		LastError:    lastError,
		UpdatedAt:    job.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		MeetingUUID:  meetingUUID,
		InstanceUUID: instanceUUID,
	})
}

// SessionTranscriptResponse for GET /api/sessions/:id/transcript
type SessionTranscriptResponse struct {
	Status       string                     `json:"status"` // "parsing" | "ready" | "failed" | "none"
	Source       string                     `json:"source,omitempty"`
	UpdatedAt    string                     `json:"updated_at,omitempty"`
	ErrorMessage *string                    `json:"error_message,omitempty"`
	Segments     []SessionTranscriptSegment `json:"segments"`
}

// SessionTranscriptSegment is one segment in the API response
type SessionTranscriptSegment struct {
	Idx     int    `json:"idx"`
	StartMs int    `json:"start_ms"`
	EndMs   int    `json:"end_ms"`
	Text    string `json:"text"`
}

// SessionTranscript handles GET /api/sessions/:id/transcript
func (h *Handlers) SessionTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 4 && pathParts[0] == "api" && pathParts[1] == "sessions" && pathParts[3] == "transcript" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 4 && pathParts[0] == "sessions" && pathParts[3] == "transcript" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	t, err := h.DB.GetTranscriptBySessionID(ctx, sessionID, "")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	if t == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(SessionTranscriptResponse{
			Status:   "none",
			Segments: []SessionTranscriptSegment{},
		})
		return
	}
	resp := SessionTranscriptResponse{
		Status:   string(t.Status),
		Source:   t.Source,
		Segments: []SessionTranscriptSegment{},
	}
	if !t.UpdatedAt.IsZero() {
		resp.UpdatedAt = t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	resp.ErrorMessage = t.ErrorMessage
	segments, err := h.DB.ListSegmentsByTranscriptID(ctx, t.ID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}
	for _, s := range segments {
		resp.Segments = append(resp.Segments, SessionTranscriptSegment{
			Idx:     s.Idx,
			StartMs: s.StartMs,
			EndMs:   s.EndMs,
			Text:    s.Text,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
