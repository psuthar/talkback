package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolishSpokenQuestion(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \n  ", ""},
		{"fillers um uh", "um so what was uh the point", "so what was the point"},
		{"fillers and repetition", "the the main topic  was was discussed", "the main topic was discussed"},
		{"like and you know", "like what is you know the answer", "what is the answer"},
		{"no change", "What is the main topic discussed?", "What is the main topic discussed?"},
		{"multiple spaces", "what   was   said", "what was said"},
		{"leading trailing", "  um what  ", "what"},
		{"ah and er", "ah er can you explain", "can you explain"},
		{"triple repeat", "the the the point", "the point"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PolishSpokenQuestion(tt.raw)
			assert.Equal(t, tt.expected, got)
		})
	}
}
