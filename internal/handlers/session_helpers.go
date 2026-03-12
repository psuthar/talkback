package handlers

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// resolveEffectivePrimaryAndAdditional returns the session's effective primary video (explicit primary, else first ready, else first) and all other videos as additional. Backward compatible when video_role is nil.
func resolveEffectivePrimaryAndAdditional(videoSources []*models.VideoSource) (primary *models.VideoSource, additional []*models.VideoSource) {
	if len(videoSources) == 0 {
		return nil, nil
	}
	// 1) Explicit primary
	for _, vs := range videoSources {
		if vs != nil && vs.VideoRole != nil && *vs.VideoRole == models.VideoRolePrimary {
			primary = vs
			break
		}
	}
	// 2) First ready
	if primary == nil {
		for _, vs := range videoSources {
			if vs != nil && vs.TranscriptStatus == models.VideoTranscriptStatusReady {
				primary = vs
				break
			}
		}
	}
	// 3) First video
	if primary == nil {
		primary = videoSources[0]
	}
	// Additional = all except primary (by ID)
	for _, vs := range videoSources {
		if vs != nil && (primary == nil || vs.ID != primary.ID) {
			additional = append(additional, vs)
		}
	}
	return primary, additional
}

// ensurePrimaryVideoIfNone sets the given video as primary for the session when no video in the session is yet marked primary (e.g. first video uploaded or Zoom import).
func (h *Handlers) ensurePrimaryVideoIfNone(ctx context.Context, sessionID, videoSourceID uuid.UUID) {
	sources, err := h.DB.GetVideoSourcesBySessionID(ctx, sessionID)
	if err != nil || len(sources) == 0 {
		return
	}
	for _, vs := range sources {
		if vs != nil && vs.VideoRole != nil && *vs.VideoRole == models.VideoRolePrimary {
			return // already have a primary
		}
	}
	if err := h.DB.SetVideoSourceVideoRole(ctx, sessionID, videoSourceID, models.VideoRolePrimary); err != nil {
		log.Printf("Warning: set first video as primary: %v", err)
	}
}

// enrichAnswersWithDisplayNames sets AnsweredByDisplayName on each answer when AnsweredBy (email) is set.
func (h *Handlers) enrichAnswersWithDisplayNames(ctx context.Context, answers []*models.Answer) {
	for _, a := range answers {
		if a == nil || a.AnsweredBy == nil || *a.AnsweredBy == "" {
			continue
		}
		u, err := h.DB.GetUserByEmail(ctx, *a.AnsweredBy)
		if err != nil || u == nil {
			continue
		}
		a.AnsweredByDisplayName = &u.DisplayName
	}
}
