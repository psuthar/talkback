package transcript

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// TranscriptSegmentInput is one normalized segment from VTT (before DB persistence).
type TranscriptSegmentInput struct {
	StartMs      int
	EndMs        int
	Text         string
	SpeakerLabel *string // optional; often nil for Zoom VTT
	SourceRef    *string // optional cue identifier
}

// ParseVTT reads WebVTT from r and returns normalized segments:
// - monotonic order, start_ms < end_ms
// - cleaned text (trim, collapse spaces, no empty lines)
// - micro-cues (< 800ms) merged into previous segment
// - overlaps clamped (start >= previous end)
// - idx is implied by slice order (0, 1, 2, ...)
func ParseVTT(r io.Reader) ([]TranscriptSegmentInput, error) {
	if r == nil {
		return nil, nil
	}
	scanner := bufio.NewScanner(r)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	raw, err := parseCues(lines)
	if err != nil {
		return nil, err
	}
	normalized := normalizeSegments(raw)
	return normalized, nil
}

// cue has start/end in ms and optional identifier
type cue struct {
	startMs   int
	endMs     int
	text      string
	sourceRef *string
}

var (
	vttTimestampLong  = regexp.MustCompile(`(\d{2}):(\d{2}):(\d{2})\.(\d{3})\s*-->\s*(\d{2}):(\d{2}):(\d{2})\.(\d{3})`)
	vttTimestampShort = regexp.MustCompile(`(\d{2}):(\d{2})\.(\d{3})\s*-->\s*(\d{2}):(\d{2})\.(\d{3})`)
)

func parseCues(lines []string) ([]cue, error) {
	var cues []cue
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "WEBVTT") || strings.HasPrefix(line, "NOTE") {
			i++
			continue
		}
		// Optional cue identifier (single line, no -->)
		var cueRef *string
		if line != "" && !strings.Contains(line, "-->") {
			ref := line
			cueRef = &ref
			i++
			if i >= len(lines) {
				break
			}
			line = strings.TrimSpace(lines[i])
		}
		if !strings.Contains(line, "-->") {
			i++
			continue
		}
		startMs, endMs, ok := parseTimestampLine(line)
		if !ok {
			i++
			continue
		}
		i++
		var textLines []string
		for i < len(lines) {
			next := strings.TrimSpace(lines[i])
			if next == "" {
				i++
				break
			}
			textLines = append(textLines, next)
			i++
		}
		text := normalizeText(strings.Join(textLines, " "))
		if text == "" {
			continue
		}
		cues = append(cues, cue{startMs: startMs, endMs: endMs, text: text, sourceRef: cueRef})
	}
	return cues, nil
}

func parseTimestampLine(line string) (startMs, endMs int, ok bool) {
	if m := vttTimestampLong.FindStringSubmatch(line); len(m) == 9 {
		startMs = parseMs(m[1], m[2], m[3], m[4])
		endMs = parseMs(m[5], m[6], m[7], m[8])
		return startMs, endMs, true
	}
	if m := vttTimestampShort.FindStringSubmatch(line); len(m) == 7 {
		startMs = parseMsShort(m[1], m[2], m[3])
		endMs = parseMsShort(m[4], m[5], m[6])
		return startMs, endMs, true
	}
	return 0, 0, false
}

func parseMs(h, m, s, ms string) int {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	ss, _ := strconv.Atoi(s)
	msec, _ := strconv.Atoi(ms)
	return hh*3600000 + mm*60000 + ss*1000 + msec
}

func parseMsShort(m, s, ms string) int {
	mm, _ := strconv.Atoi(m)
	ss, _ := strconv.Atoi(s)
	msec, _ := strconv.Atoi(ms)
	return mm*60000 + ss*1000 + msec
}

func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	// Collapse multiple spaces/newlines to single space
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

const microCueThresholdMs = 800

// normalizeSegments merges micro-cues, enforces monotonicity, and produces TranscriptSegmentInput list.
func normalizeSegments(cues []cue) []TranscriptSegmentInput {
	if len(cues) == 0 {
		return nil
	}
	// Merge micro-cues: duration < 800ms → append to previous
	var merged []cue
	merged = append(merged, cues[0])
	for j := 1; j < len(cues); j++ {
		c := cues[j]
		dur := c.endMs - c.startMs
		if dur < microCueThresholdMs && len(merged) > 0 {
			prev := &merged[len(merged)-1]
			prev.endMs = c.endMs
			if prev.text != "" && c.text != "" {
				prev.text = prev.text + " " + c.text
			} else if c.text != "" {
				prev.text = c.text
			}
			continue
		}
		merged = append(merged, c)
	}
	// Enforce monotonicity: clamp start to previous end on overlap
	for i := 1; i < len(merged); i++ {
		prevEnd := merged[i-1].endMs
		if merged[i].startMs < prevEnd {
			merged[i].startMs = prevEnd
		}
		if merged[i].endMs <= merged[i].startMs {
			merged[i].endMs = merged[i].startMs + 1
		}
	}
	if len(merged) > 0 {
		c := &merged[0]
		if c.endMs <= c.startMs {
			c.endMs = c.startMs + 1
		}
	}
	// Build output with idx implied by order
	out := make([]TranscriptSegmentInput, 0, len(merged))
	for _, c := range merged {
		if c.text == "" {
			continue
		}
		out = append(out, TranscriptSegmentInput{
			StartMs:      c.startMs,
			EndMs:        c.endMs,
			Text:         c.text,
			SpeakerLabel: nil,
			SourceRef:    c.sourceRef,
		})
	}
	return out
}
