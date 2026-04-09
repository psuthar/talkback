package mcpserver

import (
	"context"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// primaryVideoIDForRetrieval matches HTTP session ask: prefer explicit primary transcript, else first ready, else first source.
func primaryVideoIDForRetrieval(ctx context.Context, db *database.DB, sessionID uuid.UUID) (*uuid.UUID, error) {
	sources, err := db.GetVideoSourcesBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	primary, _ := resolveEffectivePrimaryAndAdditional(sources)
	if primary == nil {
		return nil, nil
	}
	return &primary.ID, nil
}

// resolveEffectivePrimaryAndAdditional mirrors internal/handlers/session_helpers.go (kept here to avoid mcpserver → handlers).
func resolveEffectivePrimaryAndAdditional(videoSources []*models.VideoSource) (primary *models.VideoSource, additional []*models.VideoSource) {
	if len(videoSources) == 0 {
		return nil, nil
	}
	for _, vs := range videoSources {
		if vs != nil && vs.VideoRole != nil && *vs.VideoRole == models.VideoRolePrimary {
			primary = vs
			break
		}
	}
	if primary == nil {
		for _, vs := range videoSources {
			if vs != nil && vs.TranscriptStatus == models.VideoTranscriptStatusReady {
				primary = vs
				break
			}
		}
	}
	if primary == nil {
		primary = videoSources[0]
	}
	for _, vs := range videoSources {
		if vs != nil && (primary == nil || vs.ID != primary.ID) {
			additional = append(additional, vs)
		}
	}
	return primary, additional
}
