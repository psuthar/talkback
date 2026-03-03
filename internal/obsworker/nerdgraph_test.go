package obsworker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNerdGraphClient_HappyPath(t *testing.T) {
	successBody := map[string]interface{}{
		"data": map[string]interface{}{
			"actor": map[string]interface{}{
				"account": map[string]interface{}{
					"nrql": map[string]interface{}{
						"results": []interface{}{
							map[string]interface{}{"req_per_min": 42.5},
						},
					},
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(successBody)
	}))
	defer server.Close()

	client := NewNerdGraphClientWithHTTP(server.URL, "test-key", 12345, server.Client())
	results, err := client.RunNRQL("SELECT count(*) FROM Transaction SINCE 30 minutes ago")
	if err != nil {
		t.Fatalf("RunNRQL: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if v, ok := results[0]["req_per_min"]; !ok {
		t.Errorf("result missing req_per_min: %v", results[0])
	} else if f, ok := v.(float64); !ok || f != 42.5 {
		t.Errorf("req_per_min: got %v (%T)", v, v)
	}
}

func TestNerdGraphClient_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewNerdGraphClientWithHTTP(server.URL, "key", 1, server.Client())
	_, err := client.RunNRQL("SELECT 1")
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Errorf("error should include body snippet, got: %v", err)
	}
}

func TestNerdGraphClient_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":[{"message":"NRQL syntax error"}]}`))
	}))
	defer server.Close()

	client := NewNerdGraphClientWithHTTP(server.URL, "key", 1, server.Client())
	_, err := client.RunNRQL("bad nrql")
	if err == nil {
		t.Fatal("expected error on GraphQL errors")
	}
	if !strings.Contains(err.Error(), "GraphQL errors") || !strings.Contains(err.Error(), "NRQL syntax") {
		t.Errorf("error should mention GraphQL errors and message, got: %v", err)
	}
}

func TestNerdGraphClient_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewNerdGraphClientWithHTTP(server.URL, "key", 1, server.Client())
	_, err := client.RunNRQL("SELECT 1")
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should mention decode, got: %v", err)
	}
}

// Ensure testdata fixtures are valid JSON (load them).
func TestTestdataFixtures_ValidJSON(t *testing.T) {
	for _, name := range []string{"nerdgraph_success.json", "nerdgraph_error.json"} {
		path := filepath.Join("testdata", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Errorf("%s: invalid JSON: %v", name, err)
		}
	}
}
