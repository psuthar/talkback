package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestMCPToolErrJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status  int
		message string
	}{
		{400, "bad request"},
		{403, "forbidden"},
		{404, "session not found"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			err := mcpToolErr(tc.status, tc.message)
			var m map[string]any
			if e := json.Unmarshal([]byte(err.Error()), &m); e != nil {
				t.Fatalf("error text should be JSON: %v", e)
			}
			if m["http_status"] != float64(tc.status) {
				t.Fatalf("http_status: %v", m["http_status"])
			}
			if m["error"] != tc.message {
				t.Fatalf("error: %v", m["error"])
			}
			if _, has := m["error_code"]; has {
				t.Fatal("mcpToolErr should not set error_code")
			}
		})
	}
}

func TestMcpToolErrCodeJSON(t *testing.T) {
	t.Parallel()
	err := mcpToolErrCode("openai_rate_limited", 429, "slow down")
	var m map[string]any
	if e := json.Unmarshal([]byte(err.Error()), &m); e != nil {
		t.Fatal(e)
	}
	if m["error_code"] != "openai_rate_limited" {
		t.Fatalf("error_code: %v", m["error_code"])
	}
}
