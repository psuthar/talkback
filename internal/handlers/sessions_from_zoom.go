package handlers

import (
	"encoding/json"
	"fmt"
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

// CreateSessionFromZoomRequest body
type CreateSessionFromZoomRequest struct {
	ZoomURL string  `json:"zoom_url"`
	Title   *string `json:"title,omitempty"`
}

// CreateSessionFromZoomResponse matches create-session shape plus optional video_source_id
type CreateSessionFromZoomResponse struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	CreatedBy     *string `json:"created_by,omitempty"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	VideoSourceID *string `json:"video_source_id,omitempty"`
	Message       string  `json:"message,omitempty"`
}

// writeJSONError writes a JSON body {"message": msg} and sets status code
func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"message": msg})
}

// writeJSONErrorWithCode writes a JSON body {"code": code, "message": msg} and sets status code
func writeJSONErrorWithCode(w http.ResponseWriter, code, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}

const zoomShareLinkMessage = "Use the Recording detail link (zoom.us/recording/detail?meeting_id=...) instead of the share link. Share links cannot be used for transcript import."

// getZoomTranscriptStatus fetches recording(s) and returns transcript status, the recording to use, the transcript file (when ready), and optional API error detail.
// Status is one of: TranscriptStatusReady, TranscriptStatusProcessing, TranscriptStatusNotAvailable, "recording_not_found", or "api_error".
func getZoomTranscriptStatus(accessToken, meetingID string) (status string, rec *utils.ZoomRecordingResponse, transcriptFile *utils.ZoomRecordingFile, apiErrorDetail string) {
	rec, err := utils.GetMeetingRecordings(accessToken, meetingID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// Fallback 1: try once with QueryUnescape for double-encoded meeting_id
			alt, _ := url.QueryUnescape(meetingID)
			if alt != meetingID {
				rec2, err2 := utils.GetMeetingRecordings(accessToken, alt)
				if err2 == nil {
					file, st := utils.FindTranscriptFileWithStatus(rec2.RecordingFiles)
					if st == utils.TranscriptStatusReady {
						return st, rec2, file, ""
					}
					if st == utils.TranscriptStatusProcessing {
						return st, rec2, file, ""
					}
					instances, _ := utils.GetPastMeetingInstances(accessToken, alt)
					for _, inst := range instances.Meetings {
						rec3, err3 := utils.GetMeetingRecordings(accessToken, inst.UUID)
						if err3 != nil {
							continue
						}
						file3, st3 := utils.FindTranscriptFileWithStatus(rec3.RecordingFiles)
						if st3 == utils.TranscriptStatusReady {
							return st3, rec3, file3, ""
						}
						if st3 == utils.TranscriptStatusProcessing {
							return st3, rec3, file3, ""
						}
					}
					return utils.TranscriptStatusNotAvailable, rec2, nil, ""
				}
			}
			// Fallback 2: meeting_id might be recurring meeting UUID; try past_meetings/instances then recordings per instance
			instances, instErr := utils.GetPastMeetingInstances(accessToken, meetingID)
			if instErr == nil {
				for _, inst := range instances.Meetings {
					rec2, err2 := utils.GetMeetingRecordings(accessToken, inst.UUID)
					if err2 != nil {
						continue
					}
					file2, st2 := utils.FindTranscriptFileWithStatus(rec2.RecordingFiles)
					if st2 == utils.TranscriptStatusReady {
						return st2, rec2, file2, ""
					}
					if st2 == utils.TranscriptStatusProcessing {
						return st2, rec2, file2, ""
					}
				}
				// Had instances but none with transcript
				if len(instances.Meetings) > 0 {
					return utils.TranscriptStatusNotAvailable, nil, nil, ""
				}
			}
			return "recording_not_found", nil, nil, ""
		}
		// Zoom API error (e.g. 401, 403, 500) or network error – pass through for UI
		log.Printf("Zoom GetMeetingRecordings error (meetingID=%q): %v", meetingID, err)
		return "api_error", nil, nil, err.Error()
	}
	file, st := utils.FindTranscriptFileWithStatus(rec.RecordingFiles)
	if st == utils.TranscriptStatusReady {
		return st, rec, file, ""
	}
	if st == utils.TranscriptStatusProcessing {
		return st, rec, file, ""
	}
	// No transcript on main recording; try past instances
	instances, err := utils.GetPastMeetingInstances(accessToken, meetingID)
	if err != nil {
		return utils.TranscriptStatusNotAvailable, rec, nil, ""
	}
	for _, inst := range instances.Meetings {
		rec2, err2 := utils.GetMeetingRecordings(accessToken, inst.UUID)
		if err2 != nil {
			continue
		}
		file2, st2 := utils.FindTranscriptFileWithStatus(rec2.RecordingFiles)
		if st2 == utils.TranscriptStatusReady {
			return st2, rec2, file2, ""
		}
		if st2 == utils.TranscriptStatusProcessing {
			return st2, rec2, file2, ""
		}
	}
	return utils.TranscriptStatusNotAvailable, rec, nil, ""
}

// CreateSessionFromZoom handles POST /sessions/from-zoom (RequireAuth; requires admin or creator role).
func (h *Handlers) CreateSessionFromZoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.GlobalRole.CanCreateSessions() {
		writeJSONError(w, "your role does not allow creating sessions", http.StatusForbidden)
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		writeJSONError(w, "Creator identity required (X-Creator-Identity header or creator_identity query)", http.StatusUnauthorized)
		return
	}
	if creatorIdentity != user.Email {
		writeJSONError(w, "creator identity must match the authenticated user", http.StatusForbidden)
		return
	}

	accessToken, _, err := h.GetValidZoomAccessToken(r, creatorIdentity)
	if err != nil {
		writeJSONError(w, "Zoom not connected - connect Zoom first", http.StatusUnauthorized)
		return
	}

	var req CreateSessionFromZoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}
	zoomURL := strings.TrimSpace(req.ZoomURL)
	if zoomURL == "" {
		writeJSONError(w, "zoom_url is required", http.StatusBadRequest)
		return
	}
	if utils.IsZoomShareLink(zoomURL) {
		writeJSONErrorWithCode(w, "zoom_share_link", zoomShareLinkMessage, http.StatusUnprocessableEntity)
		return
	}

	meetingID, err := utils.ParseZoomRecordingURL(zoomURL)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid Zoom URL: %v", err), http.StatusBadRequest)
		return
	}

	// Resolve transcript status (ready, processing, or not_available)
	transcriptStatus, rec, transcriptFile, _ := getZoomTranscriptStatus(accessToken, meetingID)
	if rec == nil {
		// Recording not found or API error
		if transcriptStatus == "recording_not_found" {
			writeJSONError(w, "Recording not found", http.StatusNotFound)
			return
		}
		log.Printf("Zoom get meeting recordings error")
		writeJSONError(w, "Failed to fetch Zoom recording", http.StatusUnprocessableEntity)
		return
	}
	switch transcriptStatus {
	case utils.TranscriptStatusProcessing:
		writeJSONErrorWithCode(w, "transcript_processing", "Transcript is still being generated. Try again in a few minutes.", http.StatusUnprocessableEntity)
		return
	case utils.TranscriptStatusNotAvailable:
		writeJSONErrorWithCode(w, "transcript_not_available", "Transcript is not enabled for this recording. Enable \"Create audio transcript\" in Zoom cloud recording settings.", http.StatusUnprocessableEntity)
		return
	case utils.TranscriptStatusReady:
		// proceed with transcriptFile and rec
	default:
		writeJSONError(w, "Failed to get transcript status", http.StatusUnprocessableEntity)
		return
	}

	content, err := utils.DownloadTranscript(transcriptFile.DownloadURL, accessToken)
	if err != nil {
		log.Printf("Zoom download transcript error: %v", err)
		writeJSONError(w, "Failed to download transcript", http.StatusUnprocessableEntity)
		return
	}
	vttContent := string(content)
	segmentInputs, err := transcript.ParseVTT(strings.NewReader(vttContent))
	if err != nil {
		log.Printf("Zoom parse VTT error: %v", err)
		writeJSONError(w, "Failed to parse transcript", http.StatusUnprocessableEntity)
		return
	}
	if len(segmentInputs) == 0 {
		writeJSONError(w, "Transcript is empty or not ready yet - try again later", http.StatusUnprocessableEntity)
		return
	}
	// Build full text and video_sources segments (seconds) from normalized VTT segments
	var fullTextParts []string
	videoSegments := make([]models.TranscriptSegment, 0, len(segmentInputs))
	for _, s := range segmentInputs {
		if s.Text != "" {
			fullTextParts = append(fullTextParts, s.Text)
			videoSegments = append(videoSegments, models.TranscriptSegment{
				StartTime: float64(s.StartMs) / 1000,
				EndTime:   float64(s.EndMs) / 1000,
				Text:      s.Text,
			})
		}
	}
	fullText := strings.Join(fullTextParts, "\n")
	if fullText == "" {
		writeJSONError(w, "Transcript is empty or not ready yet - try again later", http.StatusUnprocessableEntity)
		return
	}

	title := "Zoom Recording"
	if req.Title != nil && strings.TrimSpace(*req.Title) != "" {
		title = strings.TrimSpace(*req.Title)
	} else if rec.Topic != "" {
		title = rec.Topic
	}

	sessionID := uuid.New()
	sourceRefURL := zoomURL
	session := &models.Session{
		ID:                 sessionID,
		Title:              title,
		CreatedBy:          &creatorIdentity,
		Status:             models.SessionStatusOpen,
		SourceProvider:     models.SessionSourceZoom,
		SourceReferenceURL: &sourceRefURL,
	}
	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("Create session from Zoom error: %v", err)
		writeJSONError(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	artifact, err := h.DB.CreateArtifact(r.Context(), sessionID, title, nil)
	if err != nil {
		log.Printf("Create artifact from Zoom error: %v", err)
		writeJSONError(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

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
	if err := h.DB.CreateVideoSource(r.Context(), videoSource); err != nil {
		log.Printf("Create video source from Zoom error: %v", err)
		writeJSONError(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	if err := h.DB.UpdateVideoSourceZoomTranscript(r.Context(), videoID, fullText, &vttContent, videoSegments); err != nil {
		log.Printf("Update video source transcript from Zoom error: %v", err)
		// non-fatal: transcript_text might already be set or we can retry
	}

	// Mission #2: session-level transcript + segments (idempotent)
	trans := &models.Transcript{
		SessionID: sessionID,
		Source:    "zoom",
		Status:    models.TranscriptStatusParsing,
	}
	if err := h.DB.CreateTranscript(r.Context(), trans); err != nil {
		log.Printf("Create transcript from Zoom error: %v", err)
		// non-fatal: session and video are already created
	} else {
		if err := h.DB.DeleteSegmentsByTranscriptID(r.Context(), trans.ID); err != nil {
			log.Printf("Delete transcript segments error: %v", err)
		}
		rows := make([]models.TranscriptSegmentRow, 0, len(segmentInputs))
		for _, s := range segmentInputs {
			if s.Text == "" {
				continue
			}
			rows = append(rows, models.TranscriptSegmentRow{
				TranscriptID: trans.ID,
				SessionID:    sessionID,
				Idx:          len(rows),
				StartMs:      s.StartMs,
				EndMs:        s.EndMs,
				Text:         s.Text,
				SpeakerLabel: s.SpeakerLabel,
				SourceRef:    s.SourceRef,
			})
		}
		if len(rows) > 0 {
			if err := h.DB.InsertTranscriptSegments(r.Context(), trans.ID, sessionID, rows); err != nil {
				log.Printf("Insert transcript segments error: %v", err)
				errMsg := err.Error()
				_ = h.DB.UpdateTranscriptStatus(r.Context(), trans.ID, models.TranscriptStatusFailed, nil, &errMsg)
			} else {
				_ = h.DB.UpdateTranscriptStatus(r.Context(), trans.ID, models.TranscriptStatusReady, &fullText, nil)
			}
		} else {
			_ = h.DB.UpdateTranscriptStatus(r.Context(), trans.ID, models.TranscriptStatusReady, &fullText, nil)
		}
	}

	rag.IndexSessionAsync(sessionID, h.DB, h.Storage)

	vsIDStr := videoID.String()
	response := CreateSessionFromZoomResponse{
		ID:            session.ID.String(),
		Title:         session.Title,
		CreatedBy:     session.CreatedBy,
		Status:        string(session.Status),
		CreatedAt:     session.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		VideoSourceID: &vsIDStr,
		Message:       "Session created from Zoom transcript",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
