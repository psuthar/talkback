package main

import (
	"os"
	"testing"
	"time"
)

func TestParseWarmupConfig_Defaults(t *testing.T) {
	cfgDev := parseWarmupConfig(func(key string) string {
		if key == "ENV" {
			return "development"
		}
		return ""
	})
	if !cfgDev.Enabled {
		t.Fatalf("development default should enable warmup")
	}
	if cfgDev.Delay != 0 {
		t.Fatalf("development default delay = %s, want 0", cfgDev.Delay)
	}

	cfgProd := parseWarmupConfig(func(key string) string {
		if key == "ENV" {
			return "production"
		}
		return ""
	})
	if cfgProd.Enabled {
		t.Fatalf("production default should disable warmup")
	}
	if cfgProd.Delay != 90*time.Second {
		t.Fatalf("production default delay = %s, want 90s", cfgProd.Delay)
	}
}

func TestParseWarmupConfig_Overrides(t *testing.T) {
	cfg := parseWarmupConfig(func(key string) string {
		switch key {
		case "ENV":
			return "production"
		case warmupEnabledEnv:
			return "true"
		case warmupDelaySecondsEnv:
			return "15"
		default:
			return ""
		}
	})

	if !cfg.Enabled {
		t.Fatalf("enabled override should set warmup on")
	}
	if cfg.Delay != 15*time.Second {
		t.Fatalf("delay override = %s, want 15s", cfg.Delay)
	}
}

func TestParseWarmupConfig_InvalidDelayClamped(t *testing.T) {
	cfg := parseWarmupConfig(func(key string) string {
		switch key {
		case warmupEnabledEnv:
			return "true"
		case warmupDelaySecondsEnv:
			return "-5"
		default:
			return ""
		}
	})
	if cfg.Delay != 0 {
		t.Fatalf("negative delay should clamp to 0, got %s", cfg.Delay)
	}
}

func TestScheduleWarmup_Disabled(t *testing.T) {
	called := make(chan struct{}, 1)
	scheduleWarmup(
		warmupConfig{Enabled: false, Delay: 0},
		func() { called <- struct{}{} },
		time.After,
	)

	select {
	case <-called:
		t.Fatalf("warmup should not be called when disabled")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestScheduleWarmup_Delayed(t *testing.T) {
	afterCh := make(chan time.Time, 1)
	called := make(chan struct{}, 1)

	scheduleWarmup(
		warmupConfig{Enabled: true, Delay: 2 * time.Second},
		func() { called <- struct{}{} },
		func(time.Duration) <-chan time.Time { return afterCh },
	)

	select {
	case <-called:
		t.Fatalf("warmup should not run before delay channel fires")
	case <-time.After(50 * time.Millisecond):
	}

	afterCh <- time.Now()

	select {
	case <-called:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("warmup should run after delay channel fires")
	}
}

func TestStartLibreOfficeWarmup_UsesEnvConfig(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv(warmupEnabledEnv, "false")
	t.Setenv(warmupDelaySecondsEnv, "120")

	// Ensure helper reads env and does not panic.
	startLibreOfficeWarmup()

	// Give async scheduling path a tiny moment; with enabled=false it should do nothing.
	time.Sleep(10 * time.Millisecond)
}

func TestMain(m *testing.M) {
	// Keep tests deterministic if host env accidentally sets these.
	_ = os.Unsetenv(warmupEnabledEnv)
	_ = os.Unsetenv(warmupDelaySecondsEnv)
	os.Exit(m.Run())
}
