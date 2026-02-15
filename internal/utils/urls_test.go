package utils

import (
	"testing"
)

func TestRequireAbsoluteURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    string
	}{
		{"empty", "", true, ""},
		{"relative path", "/auth/zoom/callback", true, ""},
		{"relative path no leading slash", "api/callback", true, ""},
		{"http absolute", "http://localhost:8081/auth/zoom/callback", false, "http://localhost:8081/auth/zoom/callback"},
		{"https absolute", "https://talkback-895n.onrender.com/auth/zoom/callback", false, "https://talkback-895n.onrender.com/auth/zoom/callback"},
		{"trimmed with spaces", "  https://example.com/cb  ", false, "https://example.com/cb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RequireAbsoluteURL("ZOOM_REDIRECT_URL", tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequireAbsoluteURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("RequireAbsoluteURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
