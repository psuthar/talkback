package obsworker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	Metadata        BundleMetadata `json:"metadata"`
	Delta           *Delta         `json:"delta,omitempty"`
	Results         []QueryResult  `json:"results"`           // base queries only
	DeepDiveResults []QueryResult  `json:"deep_dive,omitempty"` // only when status YELLOW/RED or OBS_FORCE_DEEP_DIVE
}

const maxResultsPreview = 20
const maxCellLen = 120

// titleCaseQueryName returns a human-readable title for a query section (e.g. "latency_p95" -> "Latency p95").
func titleCaseQueryName(name string) string {
	if name == "" {
		return name
	}
	// Replace underscores with spaces and capitalize first letter of each word loosely.
	s := strings.ReplaceAll(name, "_", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

// formatResultValue formats a value for markdown: _ms -> "%.2f ms", req_per_min -> "%.2f req/min", counts as int.
func formatResultValue(key string, v interface{}) string {
	switch val := v.(type) {
	case float64:
		if strings.HasSuffix(key, "_ms") || key == "avg_ms" || key == "p95_ms" || key == "max_ms" {
			return fmt.Sprintf("%.2f ms", val)
		}
		if key == "req_per_min" {
			return fmt.Sprintf("%.2f req/min", val)
		}
		// Counts as integers
		if key == "n" || key == "errors" || key == "count" || strings.HasSuffix(key, "_count") {
			return fmt.Sprintf("%d", int64(val))
		}
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.2f", val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > maxCellLen {
		return s[:maxCellLen] + "..."
	}
	return s
}

// facetLabel returns the preferred display label for a FACET row (name or facet key).
func facetLabel(row map[string]interface{}) string {
	if n, ok := row["name"]; ok && n != nil && fmt.Sprint(n) != "" {
		return fmt.Sprint(n)
	}
	if f, ok := row["facet"]; ok && f != nil && fmt.Sprint(f) != "" {
		return fmt.Sprint(f)
	}
	return ""
}

// renderResultRow formats one result row for markdown: FACET-style (label: k=v) or plain k=v.
func renderResultRow(row map[string]interface{}) string {
	label := facetLabel(row)
	var parts []string
	for k, v := range row {
		if k == "name" || k == "facet" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, formatResultValue(k, v)))
	}
	if label != "" && len(parts) > 0 {
		return label + ": " + strings.Join(parts, ", ")
	}
	if label != "" {
		return label
	}
	// No label: emit all k=v
	all := make([]string, 0, len(row))
	for k, v := range row {
		all = append(all, fmt.Sprintf("%s=%s", k, formatResultValue(k, v)))
	}
	return strings.Join(all, ", ")
}

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

	// Summary + Key Deltas + Recommended Next Queries (from delta)
	if b.Delta != nil {
		d := *b.Delta
		sb.WriteString("## Summary\n\n")
		sb.WriteString(fmt.Sprintf("- **Status:** %s\n", d.Status))
		sb.WriteString(fmt.Sprintf("- **Confidence:** %s\n", d.Confidence))
		for i, r := range d.Reasons {
			if strings.HasPrefix(r, "FORCED STATUS=") {
				simLine := "- **Simulation:** " + r
				if i+1 < len(d.Reasons) && !strings.HasPrefix(d.Reasons[i+1], "FORCED STATUS=") && strings.TrimSpace(d.Reasons[i+1]) != "" {
					simLine += fmt.Sprintf(" (reason=%s)", d.Reasons[i+1])
				}
				sb.WriteString(simLine + "\n")
				break
			}
		}
		sb.WriteString("- **Reasons:**\n")
		if len(d.Reasons) == 0 {
			sb.WriteString("  - (none)\n")
		} else {
			for _, r := range d.Reasons {
				sb.WriteString(fmt.Sprintf("  - %s\n", r))
			}
		}
		sb.WriteString("\n")

		// Expected Noise (401 treated as expected when below threshold)
		sb.WriteString("## Expected Noise\n\n")
		if d.ExpectedAuth401 > 0 {
			sb.WriteString(fmt.Sprintf("- Unauthorized (401): %d\n", d.ExpectedAuth401))
		} else {
			sb.WriteString("- (none)\n")
		}
		sb.WriteString("\n")

		// Unexpected Errors (non-401)
		sb.WriteString("## Unexpected Errors\n\n")
		if d.UnexpectedErrorSummary != "" {
			sb.WriteString(fmt.Sprintf("- %s\n", d.UnexpectedErrorSummary))
		} else {
			sb.WriteString("- (none)\n")
		}
		sb.WriteString("\n")

		// Auth Failure Hotspots (usernames with high 401 attempts)
		sb.WriteString("## Auth Failure Hotspots\n\n")
		if len(d.AuthHotspots) > 0 {
			for _, h := range d.AuthHotspots {
				sb.WriteString(fmt.Sprintf("- %s: %d\n", h.Username, h.Count))
			}
		} else {
			sb.WriteString("- (none)\n")
		}
		sb.WriteString("\n")

		sb.WriteString("## Key Deltas\n\n")
		deltaLines := DeltaSummaryLines(d)
		if len(deltaLines) == 0 {
			sb.WriteString("N/A (first run or no previous baseline)\n\n")
		} else {
			for _, line := range deltaLines {
				sb.WriteString(fmt.Sprintf("- %s\n", line))
			}
			sb.WriteString("\n")
		}
		rec := RecommendedNextQueries(d)
		if len(rec) > 0 {
			sb.WriteString("## Recommended Next Queries\n\n")
			for _, q := range rec {
				sb.WriteString(fmt.Sprintf("- `%s`\n", q))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("---\n\n")
	}

	for _, qr := range b.Results {
		sectionTitle := titleCaseQueryName(qr.Name)
		sb.WriteString(fmt.Sprintf("## %s\n\n", sectionTitle))
		sb.WriteString("**NRQL:**\n```\n")
		sb.WriteString(qr.NRQL)
		sb.WriteString("\n```\n\n")

		if len(qr.Results) == 0 {
			// When errors_by_endpoint (transactionName) is empty but we have errors, show request.uri fallback with a note
			if qr.Name == "errors_by_endpoint" {
				var uriResults []map[string]interface{}
				for _, r := range b.Results {
					if r.Name == "errors_by_endpoint_uri" && len(r.Results) > 0 {
						uriResults = r.Results
						break
					}
				}
				if len(uriResults) > 0 {
					sb.WriteString("*transactionName facet empty; showing request.uri facet:*\n\n")
					preview := uriResults
					if len(preview) > maxResultsPreview {
						preview = preview[:maxResultsPreview]
					}
					for i, row := range preview {
						sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, renderResultRow(row)))
					}
					if len(uriResults) > maxResultsPreview {
						sb.WriteString(fmt.Sprintf("\n*... and %d more rows.*\n", len(uriResults)-maxResultsPreview))
					}
					sb.WriteString("\n")
					continue
				}
			}
			sb.WriteString("*No rows.*\n\n")
			continue
		}

		sb.WriteString("**Results:**\n\n")
		preview := qr.Results
		if len(preview) > maxResultsPreview {
			preview = preview[:maxResultsPreview]
		}
		for i, row := range preview {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, renderResultRow(row)))
		}
		if len(qr.Results) > maxResultsPreview {
			sb.WriteString(fmt.Sprintf("\n*... and %d more rows.*\n", len(qr.Results)-maxResultsPreview))
		}
		sb.WriteString("\n")
	}

	if len(b.DeepDiveResults) > 0 {
		sb.WriteString("## Automatic Deep Dive\n\n")
		triggerLine := "Triggered by status=YELLOW"
		if b.Delta != nil {
			triggerLine = "Triggered by status=" + b.Delta.Status
			for _, r := range b.Delta.Reasons {
				if strings.HasPrefix(r, "FORCED STATUS=") {
					triggerLine = "Triggered by simulation override"
					break
				}
			}
		}
		sb.WriteString(triggerLine + "\n\n")
		for _, qr := range b.DeepDiveResults {
			sectionTitle := titleCaseQueryName(qr.Name)
			sb.WriteString(fmt.Sprintf("### %s\n\n", sectionTitle))
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
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, renderResultRow(row)))
			}
			if len(qr.Results) > maxResultsPreview {
				sb.WriteString(fmt.Sprintf("\n*... and %d more rows.*\n", len(qr.Results)-maxResultsPreview))
			}
			sb.WriteString("\n")
		}
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
