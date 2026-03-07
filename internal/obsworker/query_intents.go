package obsworker

import (
	"fmt"
	"strings"
)

// BuildQueriesFromIntents maps diagnostic intents to valid NRQL strings.
// Uses summary for dominant transaction/URI, appName and windowMinutes for WHERE/SINCE.
func BuildQueriesFromIntents(summary IncidentSummary, intents []string, appName string, windowMinutes int) []IntentNRQL {
	window := fmt.Sprintf("%d", windowMinutes)
	appClause := ""
	if appName != "" && !strings.Contains(appName, "'") {
		appClause = " WHERE appName = '" + appName + "'"
	}
	since := fmt.Sprintf(" SINCE %s minutes ago", window)

	var out []IntentNRQL
	seen := make(map[string]bool)
	for _, intent := range intents {
		intent = strings.TrimSpace(strings.ToLower(intent))
		if intent == "" || seen[intent] {
			continue
		}
		seen[intent] = true
		var nrql string
		switch intent {
		case "inspect_failing_transaction_by_name":
			if summary.DominantTransaction != "" {
				txn := escapeNRQLString(summary.DominantTransaction)
				if appName != "" && !strings.Contains(appName, "'") {
					nrql = fmt.Sprintf("FROM TransactionError SELECT count(*) AS 'count' WHERE appName = '%s' AND transactionName = '%s' FACET error.class, error.message%s", appName, txn, since)
				} else {
					nrql = fmt.Sprintf("FROM TransactionError SELECT count(*) AS 'count' WHERE transactionName = '%s' FACET error.class, error.message%s", txn, since)
				}
			} else {
				nrql = fmt.Sprintf("FROM TransactionError SELECT count(*) AS 'count'%s FACET transactionName LIMIT 20%s", appClause, since)
			}
		case "inspect_top_error_messages":
			nrql = fmt.Sprintf("FROM TransactionError SELECT count(*) AS 'count'%s FACET error.message LIMIT 10%s", appClause, since)
		case "inspect_error_timeseries":
			nrql = fmt.Sprintf("FROM TransactionError SELECT count(*) AS 'count'%s TIMESERIES 1 minute%s", appClause, since)
		case "compare_failing_route_vs_global_latency":
			nrql = fmt.Sprintf("FROM Transaction SELECT percentile(duration,95)*1000 AS 'p95_ms'%s FACET name LIMIT 20%s", appClause, since)
		case "inspect_error_uri_breakdown":
			nrql = fmt.Sprintf("FROM TransactionError SELECT count(*) AS 'count'%s FACET request.uri LIMIT 20%s", appClause, since)
		case "inspect_auth_noise_breakdown":
			if appName != "" && !strings.Contains(appName, "'") {
				nrql = fmt.Sprintf("FROM Transaction SELECT count(*) AS 'count' WHERE appName = '%s' AND auth.outcome = 'failure' FACET auth.username LIMIT 20%s", appName, since)
			}
		default:
			continue
		}
		if nrql != "" {
			out = append(out, IntentNRQL{Intent: intent, NRQL: nrql})
		}
	}
	return out
}

// IntentNRQL pairs a diagnostic intent with its generated NRQL.
type IntentNRQL struct {
	Intent string `json:"intent"`
	NRQL   string `json:"nrql"`
}

func escapeNRQLString(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
