package models

import (
	"time"

	"github.com/google/uuid"
)

// OrchestrationRecommendationStatusAudit is an append-only record of a creator-or-admin status change on a recommendation.
type OrchestrationRecommendationStatusAudit struct {
	ID               uuid.UUID                          `json:"id"`
	SessionID        uuid.UUID                          `json:"session_id"`
	RecommendationID uuid.UUID                          `json:"recommendation_id"`
	ActorUserID      uuid.UUID                          `json:"actor_user_id"`
	FromStatus       *OrchestrationRecommendationStatus `json:"from_status,omitempty"`
	ToStatus         OrchestrationRecommendationStatus  `json:"to_status"`
	ClientSurface    *string                            `json:"client_surface,omitempty"`
	CreatedAt        time.Time                          `json:"created_at"`
}
