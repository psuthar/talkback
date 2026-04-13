package main

import "testing"

func TestResolveHTTPAddr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		httpAddr          string
		port              string
		want              string
	}{
		{
			name: "neither set — stdio mode",
			want: "",
		},
		{
			name:     "TALKBACK_MCP_HTTP_ADDR set — used as-is",
			httpAddr: ":9000",
			want:     ":9000",
		},
		{
			name: "PORT set — converted to colon-prefixed address",
			port: "10000",
			want: ":10000",
		},
		{
			name:     "both set — TALKBACK_MCP_HTTP_ADDR takes precedence",
			httpAddr: ":9000",
			port:     "10000",
			want:     ":9000",
		},
		{
			name:     "TALKBACK_MCP_HTTP_ADDR with whitespace — trimmed",
			httpAddr: "  :8080  ",
			want:     ":8080",
		},
		{
			name: "PORT with whitespace — trimmed, colon-prefixed",
			port: "  10000  ",
			want: ":10000",
		},
		{
			name:     "TALKBACK_MCP_HTTP_ADDR empty, PORT set — falls back to PORT",
			httpAddr: "",
			port:     "10000",
			want:     ":10000",
		},
		{
			name:     "TALKBACK_MCP_HTTP_ADDR whitespace-only, PORT set — falls back to PORT",
			httpAddr: "   ",
			port:     "10000",
			want:     ":10000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				switch key {
				case "TALKBACK_MCP_HTTP_ADDR":
					return tc.httpAddr
				case "PORT":
					return tc.port
				}
				return ""
			}
			got := resolveHTTPAddr(getenv)
			if got != tc.want {
				t.Errorf("resolveHTTPAddr() = %q, want %q", got, tc.want)
			}
		})
	}
}
