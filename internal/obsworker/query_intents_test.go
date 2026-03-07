package obsworker

import (
	"strings"
	"testing"
)

func TestBuildQueriesFromIntents_ContainsAppNameAndNoPlaceholders(t *testing.T) {
	appName := "MyApp"
	summary := IncidentSummary{
		DominantTransaction: "WebTransaction/Go/POST /debug/fault/error",
	}
	intents := []string{"inspect_failing_transaction_by_name", "inspect_top_error_messages"}
	got := BuildQueriesFromIntents(summary, intents, appName, 30)
	if len(got) == 0 {
		t.Fatal("expected at least one generated query")
	}
	for _, iq := range got {
		if strings.Contains(iq.NRQL, "YourAppName") || strings.Contains(iq.NRQL, "{{") {
			t.Errorf("NRQL must not contain placeholders: %s", iq.NRQL)
		}
		if appName != "" && !strings.Contains(iq.NRQL, appName) {
			t.Errorf("NRQL should contain appName %q when set: %s", appName, iq.NRQL)
		}
	}
	// First intent should use dominant transaction
	var foundTxn bool
	for _, iq := range got {
		if iq.Intent == "inspect_failing_transaction_by_name" {
			if !strings.Contains(iq.NRQL, "transactionName") {
				t.Errorf("inspect_failing_transaction_by_name NRQL should filter by transactionName: %s", iq.NRQL)
			}
			if !strings.Contains(iq.NRQL, "/debug/fault/error") {
				t.Errorf("inspect_failing_transaction_by_name NRQL should contain dominant transaction path: %s", iq.NRQL)
			}
			foundTxn = true
			break
		}
	}
	if !foundTxn {
		t.Error("expected intent inspect_failing_transaction_by_name in results")
	}
}

func TestBuildQueriesFromIntents_UnknownIntentSkipped(t *testing.T) {
	summary := IncidentSummary{}
	intents := []string{"inspect_top_error_messages", "nonexistent_intent"}
	got := BuildQueriesFromIntents(summary, intents, "App", 15)
	if len(got) != 1 {
		t.Errorf("expected 1 result (unknown intent skipped), got %d", len(got))
	}
	if len(got) > 0 && got[0].Intent != "inspect_top_error_messages" {
		t.Errorf("expected only inspect_top_error_messages, got %s", got[0].Intent)
	}
}

func TestBuildQueriesFromIntents_SinceClause(t *testing.T) {
	got := BuildQueriesFromIntents(IncidentSummary{}, []string{"inspect_error_timeseries"}, "MyApp", 60)
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if !strings.Contains(got[0].NRQL, "60 minutes ago") {
		t.Errorf("expected SINCE 60 minutes ago in NRQL: %s", got[0].NRQL)
	}
}
