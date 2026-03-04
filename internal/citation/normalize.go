package citation

import (
	"fmt"
	"strings"

	"github.com/psuthar/talkback/internal/models"
)

const maxExcerptLen = 200

// ChunkByID maps chunk ID (uuid string) to session chunk for anchor lookup.
type ChunkByID map[string]models.SessionChunk

// MaterialLabels maps material source ID (uuid string) to display name (e.g. filename "Resume.docx").
// When set, citation labels for material chunks use this name instead of generic "Slide N" or "Document p. N".
type MaterialLabels map[string]string

// NormalizeCitations converts LLM citations into the canonical structure with
// citation_id, anchor, label, and excerpt. chunkMap is chunk_id -> SessionChunk
// so we can read AnchorJSON for each cited chunk. materialLabels is optional (can be nil):
// when provided, material citations are labeled with the document name (e.g. "Paresh Suthar Resume v5a.docx")
// instead of generic "Slide 1" / "Document p. 1".
func NormalizeCitations(citations []models.Citation, chunkMap ChunkByID, materialLabels MaterialLabels) []models.Citation {
	if len(citations) == 0 {
		return citations
	}
	out := make([]models.Citation, 0, len(citations))
	for i := range citations {
		c := citations[i]
		chunk, hasChunk := chunkMap[c.ChunkID]
		var sourceLabel string
		if hasChunk && chunk.SourceType == "material" && chunk.SourceID != nil && materialLabels != nil {
			sourceLabel = materialLabels[chunk.SourceID.String()]
		}
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
			c.Label = labelFromAnchor(c.SourceType, c.Anchor, sourceLabel)
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

// sourceLabel is the display name for the source (e.g. material filename); used so citations show
// "Paresh Suthar Resume v5a.docx (block 1)" instead of generic "Slide 1" or "Document block 1".
func labelFromAnchor(sourceType string, anchor *models.CitationAnchor, sourceLabel string) string {
	if anchor == nil {
		if sourceLabel != "" {
			return sourceLabel
		}
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
			if sourceLabel != "" {
				return fmt.Sprintf("%s p. %d", sourceLabel, *anchor.Page)
			}
			return fmt.Sprintf("Document p. %d", *anchor.Page)
		}
	case "block":
		if anchor.Block != nil {
			blockNum := *anchor.Block + 1 // 1-based for display
			if sourceLabel != "" {
				return fmt.Sprintf("%s (block %d)", sourceLabel, blockNum)
			}
			return fmt.Sprintf("Document block %d", blockNum)
		}
		if sourceLabel != "" {
			return sourceLabel
		}
		return "Document"
	case "section":
		if anchor.Section != "" {
			if sourceLabel != "" {
				return sourceLabel + " § " + anchor.Section
			}
			return "Document § " + anchor.Section
		}
	}
	if sourceLabel != "" {
		return sourceLabel
	}
	return sourceType
}
