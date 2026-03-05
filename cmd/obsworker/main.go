package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/obsworker"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/storage/r2"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("obsworker: %v", err)
	}
}

func run() error {
	cfg, err := obsworker.LoadConfig()
	if err != nil {
		return err
	}

	client := obsworker.NewNerdGraphClient(cfg)
	appName, _, appWarning, err := obsworker.ResolveAppName(cfg, client)
	if err != nil {
		return err
	}
	if appWarning != "" {
		log.Printf("App name resolution: %s", appWarning)
	}

	queries, err := obsworker.LoadQueries(cfg, appName, cfg.RequireAppNameFilter)
	if err != nil {
		return err
	}

	now := time.Now()
	ts := obsworker.Timestamp(now)

	gitSHA, gitBranch := gitInfo()

	bundle := &obsworker.Bundle{
		Metadata: obsworker.BundleMetadata{
			Timestamp:  ts,
			WindowMins: cfg.WindowMins,
			GitSHA:     gitSHA,
			GitBranch:  gitBranch,
			AppName:    appName,
		},
		Results: make([]obsworker.QueryResult, 0, len(queries)),
	}

	for _, q := range queries {
		results, err := client.RunNRQL(q.NRQL)
		if err != nil {
			return fmt.Errorf("query %q: %w", q.Name, err)
		}
		bundle.Results = append(bundle.Results, obsworker.QueryResult{
			Name:    q.Name,
			NRQL:    q.NRQL,
			Results: results,
		})
	}

	// Extract key metrics for delta and baseline
	currMetrics, err := obsworker.ExtractKeyMetrics(bundle)
	if err != nil {
		return fmt.Errorf("extract metrics: %w", err)
	}

	outDir := "ops/bundles"
	if d := os.Getenv("OBS_BUNDLES_DIR"); d != "" {
		outDir = d
	}
	// Resolve so we can run from any CWD when OBS_BUNDLES_DIR is set
	if !filepath.IsAbs(outDir) {
		if cwd, err := os.Getwd(); err == nil {
			outDir = filepath.Join(cwd, outDir)
		}
	}

	// Baseline path: use OBS_BASELINE_PATH if set, else sibling of bundles dir so both persist together (e.g. CI).
	// Always resolve to absolute so the same path is used regardless of future cwd changes.
	baselinePath := obsworker.BaselinePath()
	if baselinePath == obsworker.DefaultBaselinePath {
		baselinePath = filepath.Join(filepath.Dir(outDir), "baselines", "latest.json")
	}
	if !filepath.IsAbs(baselinePath) {
		if cwd, err := os.Getwd(); err == nil {
			baselinePath = filepath.Join(cwd, baselinePath)
		}
	}
	baselinePath, _ = filepath.Abs(baselinePath) // best-effort absolute for consistent read/write

	// When OBS_BASELINE_R2=1 (e.g. Render ephemeral fs), load/save baseline from R2 so it persists across runs.
	// Dedicated path so observability data never mixes with application data (e.g. sessions/).
	const baselineR2Key = "observability/baselines/latest.json"
	useR2Baseline := isTruthy(os.Getenv("OBS_BASELINE_R2"))
	var r2Store storage.Interface
	if useR2Baseline {
		r2Cfg := r2.LoadConfig()
		if c, err := r2.New(r2Cfg); err != nil {
			log.Printf("OBS_BASELINE_R2 set but R2 config invalid: %v; using file baseline only", err)
			useR2Baseline = false
		} else {
			r2Store = c
		}
	}

	var prevBaseline *obsworker.Baseline
	if useR2Baseline && r2Store != nil {
		ctx := context.Background()
		rc, err := r2Store.Get(ctx, baselineR2Key)
		if err != nil {
			if !strings.Contains(err.Error(), "object not found") && !strings.Contains(err.Error(), "NotFound") {
				log.Printf("R2 baseline Get: %v; falling back to file", err)
			}
			prevBaseline, err = obsworker.LoadBaseline(baselinePath)
			if err != nil {
				return fmt.Errorf("load baseline: %w", err)
			}
			if prevBaseline != nil {
				log.Printf("Loaded baseline from file %s (timestamp %s)", baselinePath, prevBaseline.Timestamp)
			}
		} else {
			data, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr != nil {
				return fmt.Errorf("read R2 baseline body: %w", readErr)
			}
			prevBaseline, err = obsworker.LoadBaselineFromBytes(data)
			if err != nil {
				return fmt.Errorf("parse R2 baseline: %w", err)
			}
			log.Printf("Loaded baseline from R2 %s (timestamp %s)", baselineR2Key, prevBaseline.Timestamp)
		}
	} else {
		var err error
		prevBaseline, err = obsworker.LoadBaseline(baselinePath)
		if err != nil {
			return fmt.Errorf("load baseline: %w", err)
		}
	}
	if prevBaseline == nil {
		log.Printf("No baseline at %s (first run)", baselinePath)
	} else if !useR2Baseline || r2Store == nil {
		log.Printf("Loaded baseline from %s (timestamp %s)", baselinePath, prevBaseline.Timestamp)
	}
	thresholds := obsworker.DefaultThresholds()
	delta := obsworker.ComputeDelta(prevBaseline, currMetrics, thresholds, bundle, cfg, appWarning)
	bundle.Delta = &delta

	jsonPath, mdPath, err := obsworker.WriteBundle(bundle, outDir)
	if err != nil {
		return err
	}

	// Write new baseline only after bundle generation succeeded
	currBaseline := obsworker.Baseline{
		Timestamp:     ts,
		WindowMinutes: cfg.WindowMins,
		GitSHA:        gitSHA,
		Metrics:       currMetrics,
	}
	if err := obsworker.WriteBaseline(baselinePath, currBaseline); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	log.Printf("Wrote baseline to %s", baselinePath)
	if useR2Baseline && r2Store != nil {
		data, err := json.MarshalIndent(currBaseline, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal baseline for R2: %w", err)
		}
		_, _, err = r2Store.Put(context.Background(), baselineR2Key, bytes.NewReader(data), "application/json", int64(len(data)))
		if err != nil {
			return fmt.Errorf("write baseline to R2: %w", err)
		}
		log.Printf("Wrote baseline to R2 %s", baselineR2Key)
	}

	// Status line for scripting / logs
	reasonsStr := strings.Join(delta.Reasons, " ")
	fmt.Fprintf(os.Stdout, "STATUS=%s CONFIDENCE=%s REASONS=%q\n", delta.Status, delta.Confidence, reasonsStr)
	if useR2Baseline && r2Store != nil {
		fmt.Fprintf(os.Stdout, "Baseline: R2 %s\n", baselineR2Key)
	} else {
		fmt.Fprintf(os.Stdout, "Baseline: %s\n", baselinePath)
	}
	fmt.Fprintf(os.Stdout, "Bundle written:\n  %s\n  %s\n", jsonPath, mdPath)
	return nil
}

func gitInfo() (sha, branch string) {
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		sha = strings.TrimSpace(string(out))
		if len(sha) > 7 {
			sha = sha[:7]
		}
	}
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	return sha, branch
}

func isTruthy(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}
