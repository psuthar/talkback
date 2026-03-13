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

// ChunkURLByChunkID maps chunk ID to URL for link chunks when the citation's source_id cannot be resolved (e.g. LLM returned a wrong id). Pass nil to skip chunk-URL fallback.
type ChunkURLByChunkID map[string]string

// ResolveCitationTarget maps a citation to a navigation target given session context.
// videoSources, materials, and links are the session's sources; if nil, resolution degrades to "text" where needed.
// chunkURLByChunkID is optional: when set, link citations that don't match a session link by source_id will use the URL from the chunk's anchor so the citation still opens (e.g. when source_id is wrong).
func ResolveCitationTarget(
	c models.Citation,
	videoSources []*models.VideoSource,
	materials []*models.Material,
	links []*models.SessionLink,
	chunkURLByChunkID ChunkURLByChunkID,
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
		// Fallback: use URL from chunk anchor when source_id didn't match (e.g. stored as "source_1")
		if c.ChunkID != "" && chunkURLByChunkID != nil {
			if u := chunkURLByChunkID[c.ChunkID]; u != "" {
				out.Type = "url"
				out.URL = u
				if c.Anchor != nil && c.Anchor.Section != "" {
					out.Fragment = c.Anchor.Section
				}
				return out
			}
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
		// Previewable: PDF, slides, or doc with page/block
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
			// Slides/PPTX: use page anchor so frontend opens correct slide (SlideDeckViewer uses initialSlide=page)
			if strings.Contains(ct, "presentation") || strings.Contains(ct, "powerpoint") || strings.HasSuffix(strings.ToLower(m.Filename), ".pptx") || strings.HasSuffix(strings.ToLower(m.Filename), ".ppt") {
				out.Type = "doc"
				if c.Anchor != nil && c.Anchor.Page != nil {
					out.Page = c.Anchor.Page
				}
				if c.Anchor != nil && c.Anchor.Block != nil {
					out.Block = c.Anchor.Block
				}
				return out
			}
			out.Type = "doc"
			if c.Anchor != nil && c.Anchor.Block != nil {
				out.Block = c.Anchor.Block
			}
			if c.Anchor != nil && c.Anchor.Page != nil {
				out.Page = c.Anchor.Page
			}
			return out
		}
		return out
	default:
		return out
	}
}
