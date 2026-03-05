package obsworker

import (
	"strings"
	"testing"
)

func TestLoadConfig_MissingAPIKey(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "12345")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when NEW_RELIC_API_KEY is missing")
	}
	if !strings.Contains(err.Error(), "NEW_RELIC_API_KEY") {
		t.Errorf("error should mention NEW_RELIC_API_KEY, got: %v", err)
	}
}

func TestLoadConfig_MissingAccountID(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "test-key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when NEW_RELIC_ACCOUNT_ID is missing")
	}
	if !strings.Contains(err.Error(), "NEW_RELIC_ACCOUNT_ID") {
		t.Errorf("error should mention NEW_RELIC_ACCOUNT_ID, got: %v", err)
	}
}

func TestLoadConfig_InvalidAccountID(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "test-key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "not-a-number")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when NEW_RELIC_ACCOUNT_ID is invalid")
	}
	if !strings.Contains(err.Error(), "positive integer") && !strings.Contains(err.Error(), "NEW_RELIC_ACCOUNT_ID") {
		t.Errorf("error should mention invalid account ID, got: %v", err)
	}
}

func TestLoadConfig_DefaultsApplied(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "999")
	t.Setenv("OBS_WINDOW_MINUTES", "")
	t.Setenv("NEW_RELIC_REGION", "")
	t.Setenv("OBS_APP_NAME", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.WindowMins != 30 {
		t.Errorf("OBS_WINDOW_MINUTES default: got %d, want 30", cfg.WindowMins)
	}
	if cfg.Region != RegionUS {
		t.Errorf("NEW_RELIC_REGION default: got %q, want US", cfg.Region)
	}
}

func TestLoadConfig_RegionEU(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "999")
	t.Setenv("NEW_RELIC_REGION", "EU")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Region != RegionEU {
		t.Errorf("NEW_RELIC_REGION=EU: got %q", cfg.Region)
	}
	endpoint := cfg.NerdGraphEndpoint()
	if !strings.Contains(endpoint, "eu.newrelic.com") {
		t.Errorf("EU region endpoint should contain eu.newrelic.com, got %s", endpoint)
	}
}

func TestLoadConfig_ForceStatusAndReason(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "999")
	t.Setenv("OBS_FORCE_STATUS", "RED")
	t.Setenv("OBS_FORCE_REASON", "Simulated for testing")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ForceStatus != "RED" {
		t.Errorf("ForceStatus: got %q, want RED", cfg.ForceStatus)
	}
	if cfg.ForceReason != "Simulated for testing" {
		t.Errorf("ForceReason: got %q", cfg.ForceReason)
	}
}

func TestLoadConfig_ForceDeepDive(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "999")
	t.Setenv("OBS_FORCE_DEEP_DIVE", "true")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.ForceDeepDive {
		t.Error("ForceDeepDive should be true when OBS_FORCE_DEEP_DIVE=true")
	}
}

func TestLoadConfig_InvalidForceStatus(t *testing.T) {
	t.Setenv("NEW_RELIC_API_KEY", "key")
	t.Setenv("NEW_RELIC_ACCOUNT_ID", "999")
	t.Setenv("OBS_FORCE_STATUS", "PURPLE")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when OBS_FORCE_STATUS is invalid")
	}
	if !strings.Contains(err.Error(), "OBS_FORCE_STATUS") {
		t.Errorf("error should mention OBS_FORCE_STATUS, got: %v", err)
	}
}
