package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/utils"
)

const (
	warmupEnabledEnv      = "TALKBACK_WARM_LIBREOFFICE_ENABLED"
	warmupDelaySecondsEnv = "TALKBACK_WARM_LIBREOFFICE_DELAY_SECONDS"
)

type warmupConfig struct {
	Enabled bool
	Delay   time.Duration
}

func parseWarmupConfig(getenv func(string) string) warmupConfig {
	isProduction := strings.EqualFold(strings.TrimSpace(getenv("ENV")), "production")

	enabled := !isProduction
	if raw := strings.TrimSpace(getenv(warmupEnabledEnv)); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			enabled = parsed
		} else {
			log.Printf("LibreOffice warm-up: invalid %s=%q (expected true/false), using default %t", warmupEnabledEnv, raw, enabled)
		}
	}

	delay := time.Duration(0)
	if isProduction {
		delay = 90 * time.Second
	}
	if raw := strings.TrimSpace(getenv(warmupDelaySecondsEnv)); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil {
			if seconds < 0 {
				log.Printf("LibreOffice warm-up: %s=%d is negative; clamping to 0", warmupDelaySecondsEnv, seconds)
				seconds = 0
			}
			delay = time.Duration(seconds) * time.Second
		} else {
			log.Printf("LibreOffice warm-up: invalid %s=%q (expected integer seconds), using default %s", warmupDelaySecondsEnv, raw, delay)
		}
	}

	return warmupConfig{
		Enabled: enabled,
		Delay:   delay,
	}
}

func startLibreOfficeWarmup() {
	cfg := parseWarmupConfig(os.Getenv)
	log.Printf("LibreOffice warm-up config: enabled=%t delay=%s env=%q", cfg.Enabled, cfg.Delay, strings.TrimSpace(os.Getenv("ENV")))
	scheduleWarmup(cfg, utils.WarmLibreOffice, time.After)
}

func scheduleWarmup(cfg warmupConfig, warmupFn func(), afterFn func(time.Duration) <-chan time.Time) {
	if !cfg.Enabled {
		log.Printf("LibreOffice warm-up: skipped (%s=false)", warmupEnabledEnv)
		return
	}

	if cfg.Delay <= 0 {
		log.Printf("LibreOffice warm-up: starting immediately")
		go warmupFn()
		return
	}

	log.Printf("LibreOffice warm-up: delayed start scheduled after %s", cfg.Delay)
	go func() {
		<-afterFn(cfg.Delay)
		log.Printf("LibreOffice warm-up: delay elapsed, starting now")
		warmupFn()
	}()
}
