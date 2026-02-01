package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/psuthar/talkback/internal/utils"
)

// ZoomTranscriptStatusResponse is the response for GET /zoom/transcript-status
type ZoomTranscriptStatusResponse struct {
	Status  string  `json:"status"`  // "ready" | "processing" | "not_available" | "recording_not_found"
	Message string  `json:"message"`
	Topic   *string `json:"topic,omitempty"`
}

// ZoomTranscriptStatus handles GET /zoom/transcript-status?zoom_url=...
// Requires X-Creator-Identity (or creator_identity query) and Zoom to be connected.
func (h *Handlers) ZoomTranscriptStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	accessToken, _, err := h.GetValidZoomAccessToken(r, creatorIdentity)
	if err != nil {
		writeJSONError(w, "Zoom not connected - connect Zoom first", http.StatusUnauthorized)
		return
	}

	zoomURL := strings.TrimSpace(r.URL.Query().Get("zoom_url"))
	if zoomURL == "" {
		writeJSONError(w, "zoom_url query parameter is required", http.StatusBadRequest)
		return
	}
	if utils.IsZoomShareLink(zoomURL) {
		writeJSONErrorWithCode(w, "zoom_share_link", zoomShareLinkMessage, http.StatusUnprocessableEntity)
		return
	}

	meetingID, err := utils.ParseZoomRecordingURL(zoomURL)
	if err != nil {
		writeJSONError(w, "Invalid Zoom URL", http.StatusBadRequest)
		return
	}

	status, rec, _, apiErrorDetail := getZoomTranscriptStatus(accessToken, meetingID)

	var message string
	switch status {
	case utils.TranscriptStatusReady:
		message = "Transcript is ready. You can create a session."
	case utils.TranscriptStatusProcessing:
		message = "Transcript is still being generated. Try again in a few minutes."
	case utils.TranscriptStatusNotAvailable:
		message = "Transcript is not enabled for this recording. Enable \"Create audio transcript\" in Zoom cloud recording settings."
	case "recording_not_found":
		message = "Recording not found."
	case "api_error":
		message = "Could not fetch recording from Zoom. Try reconnecting Zoom (Disconnect then Connect Zoom) or check the recording URL."
		if apiErrorDetail != "" {
			// Sanitize: truncate and strip newlines (Zoom returns JSON or plain text)
			detail := strings.ReplaceAll(apiErrorDetail, "\n", " ")
			if len(detail) > 400 {
				detail = detail[:400] + "..."
			}
			message = message + " Zoom returned: " + detail
		}
	default:
		message = "Unable to determine transcript status."
		status = "error"
	}

	resp := ZoomTranscriptStatusResponse{Status: status, Message: message}
	if rec != nil && rec.Topic != "" {
		resp.Topic = &rec.Topic
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
