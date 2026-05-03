package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionPrimaryUpdate is the request body shape for PATCHing a session's
// primary content (SCRUM-272). Send {"kind":"document","id":"<uuid>"} to
// anchor the center pane on a document; send {"kind":""} (or
// {"kind":"","id":null}) to clear the explicit primary and fall back to
// legacy primary_video_artifact_id semantics (cf. SCRUM-271 resolver).
type SessionPrimaryUpdate struct {
	Kind *string    `json:"kind"`
	ID   *uuid.UUID `json:"id"`
}

// primaryUpdateError is the typed validation failure surfaced by
// applySessionPrimaryUpdate. The handler turns it into a JSON response with
// the matching HTTP status. Distinct error codes let the frontend map
// specific failures to recoverable copy without parsing prose.
type primaryUpdateError struct {
	status  int
	code    string
	message string
}

func (e *primaryUpdateError) Error() string { return e.message }

// validatedPrimaryUpdate is the parsed-and-checked form of a
// SessionPrimaryUpdate request body. Kind is a closed-set
// SessionPrimaryContentKind ("" means "clear the explicit primary"); ID is
// nil when Kind is "" and required otherwise.
type validatedPrimaryUpdate struct {
	Kind models.SessionPrimaryContentKind
	ID   *uuid.UUID
}

// applySessionPrimaryUpdate validates the request body's `primary` block and
// writes it via DB.UpdateSessionPrimary. ACL is enforced upstream by the
// handler's userIsSessionEditor guard, so any caller of this function is
// already creator-or-admin.
func (h *Handlers) applySessionPrimaryUpdate(ctx context.Context, sessionID uuid.UUID, in *SessionPrimaryUpdate) error {
	parsed, err := parseSessionPrimaryRequest(in)
	if err != nil {
		return err
	}
	if parsed.Kind != "" {
		if err := h.validatePrimaryPointerOwnership(ctx, sessionID, parsed); err != nil {
			return err
		}
	}
	return h.DB.UpdateSessionPrimary(ctx, sessionID, parsed.Kind, parsed.ID)
}

// parseSessionPrimaryRequest validates the closed-set kind and the
// kind/id consistency invariant. Pure function, easy to unit-test without
// any DB or HTTP plumbing.
func parseSessionPrimaryRequest(in *SessionPrimaryUpdate) (*validatedPrimaryUpdate, error) {
	if in == nil {
		return &validatedPrimaryUpdate{Kind: ""}, nil
	}
	kindStr := ""
	if in.Kind != nil {
		kindStr = *in.Kind
	}
	switch models.SessionPrimaryContentKind(kindStr) {
	case "":
		return &validatedPrimaryUpdate{Kind: ""}, nil
	case models.SessionPrimaryContentKindVideo,
		models.SessionPrimaryContentKindDocument,
		models.SessionPrimaryContentKindLink:
		if in.ID == nil {
			return nil, &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_MISSING_ID",
				message: "primary update with non-empty kind requires an id",
			}
		}
		return &validatedPrimaryUpdate{
			Kind: models.SessionPrimaryContentKind(kindStr),
			ID:   in.ID,
		}, nil
	default:
		return nil, &primaryUpdateError{
			status:  http.StatusBadRequest,
			code:    "PRIMARY_BAD_KIND",
			message: "primary kind must be one of video, document, link, or empty to clear",
		}
	}
}

// validatePrimaryPointerOwnership confirms that the pointed-to row exists,
// belongs to sessionID, and is in a ready-to-render state. Errors are typed
// as primaryUpdateError so the handler can produce a 4xx with a specific
// code instead of a generic 500.
//
// SCRUM-281 added the readiness arm: a creator may not designate a
// processing/failed material, an unverified link, or a non-ready video as
// primary. The UI hides the affordance for unprocessed rows
// (MaterialsTreePanel.jsx), but the API is the trust boundary, so the same
// rule has to live here too.
func (h *Handlers) validatePrimaryPointerOwnership(ctx context.Context, sessionID uuid.UUID, parsed *validatedPrimaryUpdate) error {
	switch parsed.Kind {
	case models.SessionPrimaryContentKindVideo:
		fa, err := h.DB.GetFileArtifactByID(ctx, *parsed.ID)
		if err != nil || fa == nil {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_VIDEO_NOT_FOUND",
				message: "primary video artifact not found",
			}
		}
		if fa.SessionID == nil || *fa.SessionID != sessionID {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_VIDEO_WRONG_SESSION",
				message: "primary video artifact does not belong to this session",
			}
		}
		if fa.Status != models.FileArtifactStatusReady {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_NOT_READY",
				message: "primary video artifact is not ready (status=" + string(fa.Status) + ")",
			}
		}
	case models.SessionPrimaryContentKindDocument:
		m, err := h.DB.GetMaterialByID(ctx, *parsed.ID)
		if err != nil || m == nil {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_MATERIAL_NOT_FOUND",
				message: "primary material not found",
			}
		}
		if m.SessionID != sessionID {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_MATERIAL_WRONG_SESSION",
				message: "primary material does not belong to this session",
			}
		}
		if m.DeletedAt != nil {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_MATERIAL_DELETED",
				message: "primary material has been deleted",
			}
		}
		if m.TextStatus != models.MaterialTextStatusReady {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_NOT_READY",
				message: "primary material is not ready (text_status=" + string(m.TextStatus) + ")",
			}
		}
	case models.SessionPrimaryContentKindLink:
		l, err := h.DB.GetSessionLinkByID(ctx, *parsed.ID)
		if err != nil || l == nil {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_LINK_NOT_FOUND",
				message: "primary session link not found",
			}
		}
		if l.SessionID != sessionID {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_LINK_WRONG_SESSION",
				message: "primary session link does not belong to this session",
			}
		}
		if l.Status != models.SessionLinkStatusVerified {
			return &primaryUpdateError{
				status:  http.StatusBadRequest,
				code:    "PRIMARY_NOT_READY",
				message: "primary session link is not ready (status=" + string(l.Status) + ")",
			}
		}
	}
	return nil
}
