package mcpserver

import (
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MCP operational logs go to stderr via the standard library logger (see cmd/talkback-mcp / docs/mcp-server.md).
// Lines are single-line, space-separated key=value pairs for easy grepping. We never log API keys, bearer tokens,
// or full _meta payloads (SCRUM-40).

const (
	mcpLogEventToolComplete = "tool_complete"
	mcpLogEventAuthFailed   = "auth_failed"
)

var mcpLogAllowedExtraKeys = map[string]struct{}{
	"session_id": {},
	"reason":     {},
}

func logMCPToolComplete(tool string, elapsed time.Duration, extras map[string]string) {
	line := buildMCPLogLine(mcpLogEventToolComplete, tool, elapsed.Milliseconds(), extras)
	log.Print(line)
}

func logMCPAuthFailed(tool string) {
	line := buildMCPLogLine(mcpLogEventAuthFailed, tool, 0, map[string]string{
		"reason": "missing_or_invalid_key",
	})
	log.Print(line)
}

func buildMCPLogLine(event, tool string, durationMs int64, extras map[string]string) string {
	tn := sanitizeMCPToolName(tool)
	var b strings.Builder
	b.Grow(64)
	b.WriteString("mcp event=")
	b.WriteString(event)
	b.WriteString(" tool=")
	b.WriteString(tn)
	b.WriteString(" duration_ms=")
	b.WriteString(strconv.FormatInt(durationMs, 10))

	type pair struct {
		k, v string
	}
	pairs := make([]pair, 0, len(extras))
	for k, raw := range extras {
		if _, ok := mcpLogAllowedExtraKeys[k]; !ok {
			continue
		}
		v, ok := sanitizeMCPLogExtra(k, raw)
		if !ok {
			continue
		}
		pairs = append(pairs, pair{k: k, v: v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	for _, p := range pairs {
		b.WriteByte(' ')
		b.WriteString(p.k)
		b.WriteByte('=')
		b.WriteString(p.v)
	}
	return b.String()
}

func sanitizeMCPToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown_tool"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "unknown_tool"
	}
	return name
}

func sanitizeMCPLogExtra(key, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	switch key {
	case "session_id":
		id, err := uuid.Parse(raw)
		if err != nil {
			return "", false
		}
		return id.String(), true
	case "reason":
		raw = strings.ReplaceAll(raw, "\n", " ")
		raw = strings.ReplaceAll(raw, "\r", " ")
		if len(raw) > 64 {
			raw = raw[:64]
		}
		return raw, true
	default:
		return "", false
	}
}
