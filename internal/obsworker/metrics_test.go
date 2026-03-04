package obsworker

import (
	"testing"
)

func TestExtractKeyMetrics_EmptyBundle(t *testing.T) {
	b := &Bundle{Results: nil}
	m, err := ExtractKeyMetrics(b)
	if err != nil {
		t.Fatalf("ExtractKeyMetrics: %v", err)
	}
	if m.TxnN != 0 || m.P95ms != 0 || m.ReqPerMin != 0 || m.Errors != 0 {
		t.Errorf("empty bundle: got %+v", m)
	}
}

func TestExtractKeyMetrics_FromResults(t *testing.T) {
	b := &Bundle{
		Results: []QueryResult{
			{
				Name:    "txn_summary",
				Results: []map[string]interface{}{{"n": 120.0, "p95_ms": 180.2, "avg_ms": 95.0, "max_ms": 400.0}},
			},
			{
				Name:    "throughput",
				Results: []map[string]interface{}{{"req_per_min": 3.4}},
			},
			{
				Name:    "error_count",
				Results: []map[string]interface{}{{"errors": 0.0}},
			},
		},
	}
	m, err := ExtractKeyMetrics(b)
	if err != nil {
		t.Fatalf("ExtractKeyMetrics: %v", err)
	}
	if m.TxnN != 120 {
		t.Errorf("txn_n: got %d, want 120", m.TxnN)
	}
	if m.P95ms != 180.2 {
		t.Errorf("p95_ms: got %.2f, want 180.2", m.P95ms)
	}
	if m.ReqPerMin != 3.4 {
		t.Errorf("req_per_min: got %.2f, want 3.4", m.ReqPerMin)
	}
	if m.Errors != 0 {
		t.Errorf("errors: got %d, want 0", m.Errors)
	}
	if m.AvgMs != 95.0 || m.MaxMs != 400.0 {
		t.Errorf("avg_ms/max_ms: got %.2f / %.2f", m.AvgMs, m.MaxMs)
	}
}

func TestExtractKeyMetrics_NoRowsZero(t *testing.T) {
	b := &Bundle{
		Results: []QueryResult{
			{Name: "txn_summary", Results: []map[string]interface{}{}},
			{Name: "throughput", Results: []map[string]interface{}{}},
			{Name: "error_count", Results: []map[string]interface{}{}},
		},
	}
	m, err := ExtractKeyMetrics(b)
	if err != nil {
		t.Fatalf("ExtractKeyMetrics: %v", err)
	}
	if m.TxnN != 0 || m.P95ms != 0 || m.ReqPerMin != 0 || m.Errors != 0 {
		t.Errorf("no rows should yield zeros: got %+v", m)
	}
}

func TestExtractKeyMetrics_BadTypeReturnsError(t *testing.T) {
	b := &Bundle{
		Results: []QueryResult{
			{Name: "txn_summary", Results: []map[string]interface{}{{"n": "not-a-number"}}},
		},
	}
	_, err := ExtractKeyMetrics(b)
	if err == nil {
		t.Fatal("expected error for bad n type")
	}
}
