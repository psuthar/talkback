package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/obsworker"
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

	queries, err := obsworker.LoadQueries(cfg)
	if err != nil {
		return err
	}

	client := obsworker.NewNerdGraphClient(cfg)
	now := time.Now()
	ts := obsworker.Timestamp(now)

	gitSHA, gitBranch := gitInfo()

	bundle := &obsworker.Bundle{
		Metadata: obsworker.BundleMetadata{
			Timestamp:  ts,
			WindowMins: cfg.WindowMins,
			GitSHA:     gitSHA,
			GitBranch:  gitBranch,
			AppName:    cfg.AppName,
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

	// Baseline path: use OBS_BASELINE_PATH if set, else sibling of bundles dir so both persist together (e.g. CI)
	baselinePath := obsworker.BaselinePath()
	if baselinePath == obsworker.DefaultBaselinePath {
		baselinePath = filepath.Join(filepath.Dir(outDir), "baselines", "latest.json")
	} else if !filepath.IsAbs(baselinePath) {
		if cwd, err := os.Getwd(); err == nil {
			baselinePath = filepath.Join(cwd, baselinePath)
		}
	}
	prevBaseline, err := obsworker.LoadBaseline(baselinePath)
	if err != nil {
		return fmt.Errorf("load baseline: %w", err)
	}
	if prevBaseline == nil {
		log.Printf("No baseline at %s (first run)", baselinePath)
	} else {
		log.Printf("Loaded baseline from %s (timestamp %s)", baselinePath, prevBaseline.Timestamp)
	}
	thresholds := obsworker.DefaultThresholds()
	delta := obsworker.ComputeDelta(prevBaseline, currMetrics, thresholds)
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

	// Status line for scripting / logs
	reasonsStr := strings.Join(delta.Reasons, " ")
	fmt.Fprintf(os.Stdout, "STATUS=%s CONFIDENCE=%s REASONS=%q\n", delta.Status, delta.Confidence, reasonsStr)
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
