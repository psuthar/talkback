package handlers

import (
	"context"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionPrimaryDescriptor is the resolved center-pane primary for a session,
// returned in GET session responses (SCRUM-271). "Resolved" means the
// resolveSessionPrimary helper has applied the legacy fallback so that an
// existing video-first session (primary_video_artifact_id set,
// primary_content_kind NULL) reports kind="video" without UI-side inference.
//
// Kind / ID are required and identify the primary content. Title and Status
// are optional metadata pulled from the underlying artifact / material / link
// row by the handler when it can fill them from data already loaded for the
// response.
type SessionPrimaryDescriptor struct {
	Kind   string    `json:"kind"`
	ID     uuid.UUID `json:"id"`
	Title  *string   `json:"title,omitempty"`
	Status *string   `json:"status,omitempty"`
}

// resolveSessionPrimary maps a session's persisted primary fields to the
// outward-facing descriptor's (kind, id) pair, applying the legacy
// precedence rule documented on the SCRUM-269 migration:
//
//   - When primary_content_kind is set, the matching pointer column carries
//     the reference (kind=video → primary_video_artifact_id, kind=document →
//     primary_material_id, kind=link → primary_session_link_id).
//   - When primary_content_kind is NULL but primary_video_artifact_id is set,
//     the session is a legacy video-first session — return kind="video".
//   - Otherwise (no primary at all, or kind set without matching pointer),
//     return nil so the handler omits the JSON field.
//
// Title and Status are left nil; the handler enriches them from data already
// loaded for the response (materials, links, file artifact). Returning the
// base descriptor here keeps the resolver pure and unit-testable without a
// DB stub.
func resolveSessionPrimary(session *models.Session) *SessionPrimaryDescriptor {
	if session == nil {
		return nil
	}
	if session.PrimaryContentKind != nil {
		switch *session.PrimaryContentKind {
		case models.SessionPrimaryContentKindVideo:
			if session.PrimaryVideoArtifactID != nil {
				return &SessionPrimaryDescriptor{Kind: "video", ID: *session.PrimaryVideoArtifactID}
			}
		case models.SessionPrimaryContentKindDocument:
			if session.PrimaryMaterialID != nil {
				return &SessionPrimaryDescriptor{Kind: "document", ID: *session.PrimaryMaterialID}
			}
		case models.SessionPrimaryContentKindLink:
			if session.PrimarySessionLinkID != nil {
				return &SessionPrimaryDescriptor{Kind: "link", ID: *session.PrimarySessionLinkID}
			}
		}
		// Explicit kind set but matching pointer missing — invalid state from
		// the DB CHECK's perspective is not blocked, so the handler should
		// behave the same as "no primary".
		return nil
	}
	if session.PrimaryVideoArtifactID != nil {
		return &SessionPrimaryDescriptor{Kind: "video", ID: *session.PrimaryVideoArtifactID}
	}
	return nil
}

// enrichSessionPrimaryFromMaterials populates Title and Status on a
// document-kind descriptor from the already-loaded materials slice. Returns
// the descriptor unchanged when the kind doesn't match or the pointer can't
// be resolved against the slice.
func enrichSessionPrimaryFromMaterials(p *SessionPrimaryDescriptor, materials []*models.Material) *SessionPrimaryDescriptor {
	if p == nil || p.Kind != "document" {
		return p
	}
	for _, m := range materials {
		if m == nil {
			continue
		}
		if m.ID == p.ID {
			// Prefer the explicit display Title; fall back to filename.
			if m.Title != nil && *m.Title != "" {
				title := *m.Title
				p.Title = &title
			} else if m.Filename != "" {
				title := m.Filename
				p.Title = &title
			}
			status := string(m.TextStatus)
			p.Status = &status
			return p
		}
	}
	return p
}

// enrichSessionPrimaryFromLinks populates Title and Status on a link-kind
// descriptor from the already-loaded session links slice. Returns the
// descriptor unchanged when the kind doesn't match or the pointer can't be
// resolved.
func enrichSessionPrimaryFromLinks(p *SessionPrimaryDescriptor, links []*models.SessionLink) *SessionPrimaryDescriptor {
	if p == nil || p.Kind != "link" {
		return p
	}
	for _, l := range links {
		if l == nil {
			continue
		}
		if l.ID == p.ID {
			if l.Title != nil {
				title := *l.Title
				p.Title = &title
			} else if l.URL != "" {
				url := l.URL
				p.Title = &url
			}
			status := string(l.Status)
			p.Status = &status
			return p
		}
	}
	return p
}

// enrichSessionPrimaryFromFileArtifact populates Title and Status on a
// video-kind descriptor from the file_artifact already loaded for video
// playback resolution. Pass nil when no fetch occurred.
func enrichSessionPrimaryFromFileArtifact(p *SessionPrimaryDescriptor, fa *models.FileArtifact) *SessionPrimaryDescriptor {
	if p == nil || p.Kind != "video" || fa == nil {
		return p
	}
	if fa.ID != p.ID {
		return p
	}
	if fa.Filename != nil {
		title := *fa.Filename
		p.Title = &title
	}
	status := string(fa.Status)
	p.Status = &status
	return p
}

// snapshotSessionPrimary returns the (kind, id) pair for the session's
// current primary as the SCRUM-283 audit row stores it: kind is the
// closed-set string (or empty for "no explicit primary"), id is the
// matching pointer or nil. Used to capture the pre-PATCH state before
// applySessionPrimaryUpdate overwrites it.
func snapshotSessionPrimary(s *models.Session) (string, *uuid.UUID) {
	if s == nil {
		return "", nil
	}
	if s.PrimaryContentKind != nil {
		switch *s.PrimaryContentKind {
		case models.SessionPrimaryContentKindVideo:
			return "video", s.PrimaryVideoArtifactID
		case models.SessionPrimaryContentKindDocument:
			return "document", s.PrimaryMaterialID
		case models.SessionPrimaryContentKindLink:
			return "link", s.PrimarySessionLinkID
		}
	}
	if s.PrimaryVideoArtifactID != nil {
		return "video", s.PrimaryVideoArtifactID
	}
	return "", nil
}

// parseRequestedPrimary mirrors snapshotSessionPrimary for the request body
// shape. An empty kind ("") plus nil id represents "clear primary"; any
// non-empty kind requires an id (parseSessionPrimaryRequest enforces this).
func parseRequestedPrimary(in *SessionPrimaryUpdate) (string, *uuid.UUID) {
	if in == nil || in.Kind == nil {
		return "", nil
	}
	return *in.Kind, in.ID
}

// actorUserID extracts the audit row's actor pointer from the request user.
// Nil when no user is in scope (shouldn't happen at this point in the
// handler — the auth guard runs upstream — but defensive against callers
// reusing the helper from a system path).
func actorUserID(user *models.User) *uuid.UUID {
	if user == nil {
		return nil
	}
	id := user.ID
	return &id
}

// resolveAndEnrichSessionPrimaryForList builds resolved+enriched primary
// descriptors for a page of session list rows in O(1) extra queries: one
// batched fetch per kind that appears in the page. Returns a slice of
// descriptors aligned by index with the input slice (entries are nil when
// the matching session has no primary).
//
// SCRUM-280: surfaces the same {kind, id, title?, status?} block the GET
// handler produces, so clients can render "Primary: <title>" in session
// listings without a per-row fetch.
func (h *Handlers) resolveAndEnrichSessionPrimaryForList(ctx context.Context, sessions []*models.Session) []*SessionPrimaryDescriptor {
	out := make([]*SessionPrimaryDescriptor, len(sessions))
	if len(sessions) == 0 {
		return out
	}
	// First pass: resolve kind+id without DB calls and bucket the pointer ids
	// per kind for batched lookup. resolveSessionPrimary handles the legacy
	// kind=NULL → video fallback and the "kind set but pointer NULL" repair
	// case (returns nil).
	resolved := make([]*SessionPrimaryDescriptor, len(sessions))
	matIDs := make([]uuid.UUID, 0)
	linkIDs := make([]uuid.UUID, 0)
	faIDs := make([]uuid.UUID, 0)
	for i, s := range sessions {
		p := resolveSessionPrimary(s)
		resolved[i] = p
		if p == nil {
			continue
		}
		switch p.Kind {
		case "document":
			matIDs = append(matIDs, p.ID)
		case "link":
			linkIDs = append(linkIDs, p.ID)
		case "video":
			faIDs = append(faIDs, p.ID)
		}
	}
	// Three batched fetches — one per kind that actually appears in the page.
	// Errors degrade to title/status nil rather than failing the whole list:
	// the descriptor's kind+id are still the useful payload for clients.
	matMap, _ := h.DB.GetMaterialsByIDs(ctx, matIDs)
	linkMap, _ := h.DB.GetSessionLinksByIDs(ctx, linkIDs)
	faMap, _ := h.DB.GetFileArtifactsByIDs(ctx, faIDs)
	for i, p := range resolved {
		if p == nil {
			continue
		}
		switch p.Kind {
		case "document":
			if m, ok := matMap[p.ID]; ok {
				p = enrichSessionPrimaryFromMaterials(p, []*models.Material{m})
			}
		case "link":
			if l, ok := linkMap[p.ID]; ok {
				p = enrichSessionPrimaryFromLinks(p, []*models.SessionLink{l})
			}
		case "video":
			if fa, ok := faMap[p.ID]; ok {
				p = enrichSessionPrimaryFromFileArtifact(p, fa)
			}
		}
		out[i] = p
	}
	return out
}

// resolveAndEnrichSessionPrimaryForResponse returns the fully-enriched primary
// descriptor for a session by fetching only the rows needed for the resolved
// kind. Use this in handler paths that don't already load materials/links/the
// primary file_artifact for other reasons (e.g. PATCH primary). GetSession
// composes the resolver + enrichers directly because it already has those rows
// in scope. Errors fetching the supplemental rows degrade to title/status nil
// rather than failing the whole response — the descriptor's kind+id are still
// useful to clients.
//
// SCRUM-279: keeps PATCH response symmetric with GET so clients no longer have
// to chase a follow-up GET to learn the title/status of the primary they just
// set.
func (h *Handlers) resolveAndEnrichSessionPrimaryForResponse(ctx context.Context, session *models.Session) *SessionPrimaryDescriptor {
	p := resolveSessionPrimary(session)
	if p == nil {
		return nil
	}
	switch p.Kind {
	case "document":
		materials, _ := h.DB.GetActiveMaterialsBySessionID(ctx, session.ID)
		return enrichSessionPrimaryFromMaterials(p, materials)
	case "link":
		links, _ := h.DB.GetSessionLinksBySessionID(ctx, session.ID)
		return enrichSessionPrimaryFromLinks(p, links)
	case "video":
		if session.PrimaryVideoArtifactID == nil {
			return p
		}
		fa, _ := h.DB.GetFileArtifactByID(ctx, *session.PrimaryVideoArtifactID)
		return enrichSessionPrimaryFromFileArtifact(p, fa)
	}
	return p
}
