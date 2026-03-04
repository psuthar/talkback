package obsworker

import (
	"fmt"
	"math"
)

// ExtractKeyMetrics reads txn_summary, throughput, and error_count from the bundle into BaselineMetrics.
// Missing queries or empty rows are treated as zero / missing; type assertion failures return an actionable error.
func ExtractKeyMetrics(bundle *Bundle) (BaselineMetrics, error) {
	var m BaselineMetrics
	if bundle == nil {
		return m, nil
	}

	for _, qr := range bundle.Results {
		switch qr.Name {
		case "txn_summary":
			if len(qr.Results) > 0 {
				row := qr.Results[0]
				if n, err := floatFrom(row, "n"); err != nil {
					return m, fmt.Errorf("query %q key %q: %w", qr.Name, "n", err)
				} else {
					m.TxnN = int(math.Round(n))
				}
				if v, err := floatFrom(row, "p95_ms"); err == nil {
					m.P95ms = v
				}
				if v, err := floatFrom(row, "avg_ms"); err == nil {
					m.AvgMs = v
				}
				if v, err := floatFrom(row, "max_ms"); err == nil {
					m.MaxMs = v
				}
			}
		case "throughput":
			if len(qr.Results) > 0 {
				if v, err := floatFrom(qr.Results[0], "req_per_min"); err != nil {
					return m, fmt.Errorf("query %q key %q: %w", qr.Name, "req_per_min", err)
				} else {
					m.ReqPerMin = v
				}
			}
		case "error_count":
			if len(qr.Results) > 0 {
				if v, err := floatFrom(qr.Results[0], "errors"); err != nil {
					return m, fmt.Errorf("query %q key %q: %w", qr.Name, "errors", err)
				} else {
					m.Errors = int(math.Round(v))
				}
			}
		}
	}
	return m, nil
}

func floatFrom(row map[string]interface{}, key string) (float64, error) {
	v, ok := row[key]
	if !ok || v == nil {
		return 0, nil
	}
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	}
	return 0, fmt.Errorf("expected number, got %T", v)
}
