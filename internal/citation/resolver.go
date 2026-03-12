package citation

import (
	"strings"

	"github.com/psuthar/talkback/internal/models"
)

// CitationTarget describes where to navigate when the user clicks a citation.
// Never error hard — degrade gracefully (e.g. Type="text" when video not available).
type CitationTarget struct {
	Type     string `json:"type"`               // "video" | "pdf" | "doc" | "text" | "url"
	URL      string `json:"url"`                // deep link or base URL (for materials or link)
	Fragment string `json:"fragment,omitempty"`  // optional hash for url (e.g. #section-id)
	SeekMs   *int64 `json:"seek_ms,omitempty"`
	Page     *int   `json:"page,omitempty"`
	Block    *int   `json:"block,omitempty"`
}

// ResolveCitationTarget maps a citation to a navigation target given session context.
// videoSources, materials, and links are the session's sources; if nil, resolution degrades to "text" where needed.
func ResolveCitationTarget(
	c models.Citation,
	videoSources []*models.VideoSource,
	materials []*models.Material,
	links []*models.SessionLink,
) CitationTarget {
	out := CitationTarget{Type: "text"}

	switch c.SourceType {
	case "link":
		for _, link := range links {
			if link.ID.String() != c.SourceID {
				continue
			}
			out.Type = "url"
			out.URL = link.URL
			if c.Anchor != nil && c.Anchor.Section != "" {
				out.Fragment = c.Anchor.Section
			}
			return out
		}
		return out
	case "transcript":
		if c.Anchor != nil && c.Anchor.Type == "time_range" && c.Anchor.StartMs != nil {
			seek := *c.Anchor.StartMs
			out.SeekMs = &seek
			if len(videoSources) > 0 {
				out.Type = "video"
				return out
			}
		}
		return out
	case "material":
		// Previewable: PDF or doc with page/block
		for _, m := range materials {
			if m.ID.String() != c.SourceID {
				continue
			}
			ct := strings.ToLower(m.ContentType)
			if strings.Contains(ct, "pdf") || strings.Contains(ct, "image") {
				out.Type = "pdf"
				if c.Anchor != nil {
					if c.Anchor.Page != nil {
						out.Page = c.Anchor.Page
					}
					if c.Anchor.Block != nil {
						out.Block = c.Anchor.Block
					}
				}
				return out
			}
			out.Type = "doc"
			if c.Anchor != nil && c.Anchor.Block != nil {
				out.Block = c.Anchor.Block
			}
			return out
		}
		return out
	default:
		return out
	}
}
