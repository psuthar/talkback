package prrisk

import (
	"os"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

func contextInputFromSignals(s Signals) riskcontext.Input {
	isTest := make([]bool, len(s.Files))
	for i, f := range s.Files {
		isTest[i] = IsTestPath(f.Path)
	}
	files := make([]riskcontext.FileChange, len(s.Files))
	for i, f := range s.Files {
		files[i] = riskcontext.FileChange{Path: f.Path, Added: f.Added, Deleted: f.Deleted}
	}
	title := os.Getenv("PRRISK_PR_TITLE")
	body := os.Getenv("PRRISK_PR_BODY")
	return riskcontext.Input{
		RepoRoot:           s.RepoRoot,
		GitError:           s.GitError,
		Files:              files,
		IsTest:             isTest,
		DomainHits:         s.DomainHits,
		TestUnitDomainHits: s.TestUnitDomainHits,
		TestE2EDomainHits:  s.TestE2EDomainHits,
		PRTitle:            title,
		PRBody:             body,
	}
}

func riskContextWeights(w ScoreWeights) riskcontext.Weights {
	return riskcontext.Weights{
		ProximityLowPoints:   w.ContextProximityLowPoints,
		ScatteredPoints:      w.ContextScatteredPoints,
		HotspotPoints:        w.ContextHotspotPoints,
		IntentMismatchPoints: w.ContextIntentMismatchPoints,
	}
}
