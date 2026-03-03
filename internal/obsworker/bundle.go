package obsworker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// QueryResult holds one query's name, NRQL, and results.
type QueryResult struct {
	Name   string                   `json:"name"`
	NRQL   string                   `json:"nrql"`
	Results []map[string]interface{} `json:"results"`
}

// BundleMetadata is metadata attached to the bundle.
type BundleMetadata struct {
	Timestamp  string `json:"timestamp"`
	WindowMins int    `json:"window_minutes"`
	GitSHA     string `json:"git_sha,omitempty"`
	GitBranch  string `json:"git_branch,omitempty"`
	AppName    string `json:"app_name,omitempty"`
}

// Bundle is the full diagnostic bundle.
type Bundle struct {
	Metadata BundleMetadata `json:"metadata"`
	Results  []QueryResult  `json:"results"`
}

const maxResultsPreview = 20
const maxCellLen = 120

// RenderMarkdown produces a readable markdown summary of the bundle.
func (b *Bundle) RenderMarkdown() string {
	var sb strings.Builder

	sb.WriteString("# New Relic Diagnostic Bundle\n\n")
	sb.WriteString(fmt.Sprintf("- **Timestamp:** %s\n", b.Metadata.Timestamp))
	sb.WriteString(fmt.Sprintf("- **Window:** %d minutes\n", b.Metadata.WindowMins))
	if b.Metadata.AppName != "" {
		sb.WriteString(fmt.Sprintf("- **App name:** %s\n", b.Metadata.AppName))
	}
	if b.Metadata.GitSHA != "" {
		sb.WriteString(fmt.Sprintf("- **Git SHA:** %s\n", b.Metadata.GitSHA))
	}
	if b.Metadata.GitBranch != "" {
		sb.WriteString(fmt.Sprintf("- **Git branch:** %s\n", b.Metadata.GitBranch))
	}
	sb.WriteString("\n---\n\n")

	for _, qr := range b.Results {
		sb.WriteString(fmt.Sprintf("## %s\n\n", qr.Name))
		sb.WriteString("**NRQL:**\n```\n")
		sb.WriteString(qr.NRQL)
		sb.WriteString("\n```\n\n")

		if len(qr.Results) == 0 {
			sb.WriteString("*No rows.*\n\n")
			continue
		}

		sb.WriteString("**Results:**\n\n")
		preview := qr.Results
		if len(preview) > maxResultsPreview {
			preview = preview[:maxResultsPreview]
		}
		for i, row := range preview {
			sb.WriteString(fmt.Sprintf("%d. ", i+1))
			parts := make([]string, 0, len(row))
			for k, v := range row {
				valStr := fmt.Sprintf("%v", v)
				if len(valStr) > maxCellLen {
					valStr = valStr[:maxCellLen] + "..."
				}
				parts = append(parts, fmt.Sprintf("%s=%s", k, valStr))
			}
			sb.WriteString(strings.Join(parts, ", "))
			sb.WriteString("\n")
		}
		if len(qr.Results) > maxResultsPreview {
			sb.WriteString(fmt.Sprintf("\n*... and %d more rows.*\n", len(qr.Results)-maxResultsPreview))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// WriteBundle writes the bundle to ops/bundles/<timestamp>-bundle.json and .md.
// Returns the paths written and any error.
func WriteBundle(b *Bundle, outDir string) (jsonPath, mdPath string, err error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", "", fmt.Errorf("create output dir %s: %w", outDir, err)
	}

	ts := b.Metadata.Timestamp
	base := filepath.Join(outDir, ts+"-bundle")
	jsonPath = base + ".json"
	mdPath = base + ".md"

	fJSON, err := os.Create(jsonPath)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", jsonPath, err)
	}
	defer fJSON.Close()
	enc := json.NewEncoder(fJSON)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return "", "", fmt.Errorf("write JSON: %w", err)
	}

	md := b.RenderMarkdown()
	if err := os.WriteFile(mdPath, []byte(md), 0644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", mdPath, err)
	}

	return jsonPath, mdPath, nil
}

// Timestamp returns a compact timestamp for bundle filenames.
func Timestamp(t time.Time) string {
	return t.Format("20060102-150405")
}
