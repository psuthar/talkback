package citation

import (
	"fmt"
	"strings"

	"github.com/psuthar/talkback/internal/models"
)

const maxExcerptLen = 200

// ChunkByID maps chunk ID (uuid string) to session chunk for anchor lookup.
type ChunkByID map[string]models.SessionChunk

// NormalizeCitations converts LLM citations into the canonical structure with
// citation_id, anchor, label, and excerpt. chunkMap is chunk_id -> SessionChunk
// so we can read AnchorJSON for each cited chunk.
func NormalizeCitations(citations []models.Citation, chunkMap ChunkByID) []models.Citation {
	if len(citations) == 0 {
		return citations
	}
	out := make([]models.Citation, 0, len(citations))
	for i := range citations {
		c := citations[i]
		chunk, hasChunk := chunkMap[c.ChunkID]
		if hasChunk {
			if c.SourceType == "" {
				c.SourceType = chunk.SourceType
			}
			if c.SourceID == "" && chunk.SourceID != nil {
				c.SourceID = chunk.SourceID.String()
			}
		}
		if hasChunk && chunk.AnchorJSON != nil {
			c.Anchor = anchorFromMap(chunk.AnchorJSON)
			c.Label = labelFromAnchor(c.SourceType, c.Anchor)
		} else if c.Locator != "" {
			c.Label = c.SourceType + " " + c.Locator
		} else {
			c.Label = c.SourceType + " " + c.ChunkID
		}
		excerpt := c.Snippet
		if excerpt == "" && hasChunk {
			excerpt = chunk.Text
		}
		if len(excerpt) > maxExcerptLen {
			excerpt = strings.TrimSpace(excerpt[:maxExcerptLen]) + "..."
		}
		c.Excerpt = excerpt
		c.CitationID = fmt.Sprintf("C%d", i+1)
		out = append(out, c)
	}
	return out
}

func anchorFromMap(m map[string]interface{}) *models.CitationAnchor {
	a := &models.CitationAnchor{Type: "none"}
	if m == nil {
		return a
	}
	if startMs, ok := m["start_ms"].(float64); ok {
		s := int64(startMs)
		a.StartMs = &s
	}
	if endMs, ok := m["end_ms"].(float64); ok {
		e := int64(endMs)
		a.EndMs = &e
	}
	if a.StartMs != nil || a.EndMs != nil {
		a.Type = "time_range"
		return a
	}
	if block, ok := m["block"].(float64); ok {
		b := int(block)
		a.Block = &b
		a.Type = "block"
		return a
	}
	if page, ok := m["page"].(float64); ok {
		p := int(page)
		a.Page = &p
		a.Type = "page"
		return a
	}
	if section, ok := m["section"].(string); ok && section != "" {
		a.Section = section
		a.Type = "section"
		return a
	}
	return a
}

func labelFromAnchor(sourceType string, anchor *models.CitationAnchor) string {
	if anchor == nil {
		return sourceType
	}
	switch anchor.Type {
	case "time_range":
		if anchor.StartMs != nil && anchor.EndMs != nil {
			sMin := *anchor.StartMs / 60000
			sSec := (*anchor.StartMs % 60000) / 1000
			eMin := *anchor.EndMs / 60000
			eSec := (*anchor.EndMs % 60000) / 1000
			return fmt.Sprintf("Transcript %d:%02d–%d:%02d", sMin, sSec, eMin, eSec)
		}
	case "page":
		if anchor.Page != nil {
			return fmt.Sprintf("Document p. %d", *anchor.Page)
		}
	case "block":
		if anchor.Block != nil {
			return fmt.Sprintf("Slide %d", *anchor.Block+1) // 1-based for display
		}
		return "Document"
	case "section":
		if anchor.Section != "" {
			return "Document § " + anchor.Section
		}
	}
	return sourceType
}
