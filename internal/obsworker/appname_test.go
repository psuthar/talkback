package obsworker

import (
	"strings"
	"testing"
)

func TestResolveAppName_EnvSet(t *testing.T) {
	cfg := Config{AppName: "Talkback-NewRelic"}
	name, source, warning, err := ResolveAppName(cfg, nil)
	if err != nil {
		t.Fatalf("ResolveAppName: %v", err)
	}
	if name != "Talkback-NewRelic" {
		t.Errorf("appName: got %q, want Talkback-NewRelic", name)
	}
	if source != AppNameSourceEnv {
		t.Errorf("source: got %q, want ENV", source)
	}
	if warning != "" {
		t.Errorf("warning: got %q, want empty", warning)
	}
}

func TestResolveAppName_RejectsSingleQuoteInEnv(t *testing.T) {
	cfg := Config{AppName: "App'Name"}
	_, _, _, err := ResolveAppName(cfg, nil)
	if err == nil {
		t.Fatal("expected error when app name contains single quote")
	}
	if err != nil && !strings.Contains(err.Error(), "single quote") {
		t.Errorf("error should mention single quote, got: %v", err)
	}
}
