package handlers

import (
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
