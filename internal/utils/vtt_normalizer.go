package utils

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/psuthar/talkback/internal/models"
)

// ParseVTT parses WebVTT content into normalized segments (start_time, end_time, text)
func ParseVTT(content string) ([]models.TranscriptSegment, error) {
	lines := strings.Split(content, "\n")
	var segments []models.TranscriptSegment
	var i int
	// Skip optional WEBVTT header and empty lines
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "WEBVTT") || strings.HasPrefix(line, "NOTE") {
			i++
			continue
		}
		// Cue: timestamp line --> 00:00:00.000 --> 00:00:05.000
		if strings.Contains(line, "-->") {
			start, end, ok := parseVTTTimestampLine(line)
			if !ok {
				i++
				continue
			}
			// Next non-empty line(s) are the cue text
			var textLines []string
			i++
			for i < len(lines) {
				next := strings.TrimSpace(lines[i])
				if next == "" {
					i++
					break
				}
				textLines = append(textLines, next)
				i++
			}
			text := strings.TrimSpace(strings.Join(textLines, " "))
			if text != "" {
				segments = append(segments, models.TranscriptSegment{
					StartTime: start,
					EndTime:   end,
					Text:      text,
				})
			}
			continue
		}
		i++
	}
	return segments, nil
}

var vttTimestampLongRe = regexp.MustCompile(`(\d{2}):(\d{2}):(\d{2})\.(\d{3})\s*-->\s*(\d{2}):(\d{2}):(\d{2})\.(\d{3})`)
var vttTimestampShortRe = regexp.MustCompile(`(\d{2}):(\d{2})\.(\d{3})\s*-->\s*(\d{2}):(\d{2})\.(\d{3})`)

func parseVTTTimestampLine(line string) (startSec, endSec float64, ok bool) {
	if matches := vttTimestampLongRe.FindStringSubmatch(line); len(matches) == 9 {
		startSec = parseVTTTimeParts(matches[1], matches[2], matches[3], matches[4])
		endSec = parseVTTTimeParts(matches[5], matches[6], matches[7], matches[8])
		return startSec, endSec, true
	}
	if matches := vttTimestampShortRe.FindStringSubmatch(line); len(matches) == 7 {
		startSec = parseVTTTimePartsShort(matches[1], matches[2], matches[3])
		endSec = parseVTTTimePartsShort(matches[4], matches[5], matches[6])
		return startSec, endSec, true
	}
	return 0, 0, false
}

func parseVTTTimeParts(h, m, s, ms string) float64 {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	ss, _ := strconv.Atoi(s)
	msec, _ := strconv.Atoi(ms)
	return float64(hh*3600+mm*60+ss) + float64(msec)/1000
}

func parseVTTTimePartsShort(m, s, ms string) float64 {
	mm, _ := strconv.Atoi(m)
	ss, _ := strconv.Atoi(s)
	msec, _ := strconv.Atoi(ms)
	return float64(mm*60+ss) + float64(msec)/1000
}

// FullTextFromSegments returns concatenated text from segments (for transcript_text)
func FullTextFromSegments(segments []models.TranscriptSegment) string {
	var b strings.Builder
	for i, s := range segments {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.Text)
	}
	return b.String()
}
