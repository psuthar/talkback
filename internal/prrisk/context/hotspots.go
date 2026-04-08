package riskcontext

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	hotspotSampleCommits = 50
	// hotspotMinCommits is how many distinct commits (within the sampled log) must touch
	// a two-segment path prefix for it to count as a "hotspot". We count commits, not
	// raw file lines — a single large commit must not inflate the score.
	hotspotMinCommits = 5
)

// AnalyzeHotspots finds path prefixes that show up in several recent commits and overlap the current diff.
func AnalyzeHotspots(in Input) ([]HotspotInsight, string) {
	if in.GitError != "" || in.RepoRoot == "" {
		return nil, ""
	}

	cmd := exec.Command("git", "-C", in.RepoRoot, "log", "-n", fmt.Sprint(hotspotSampleCommits), "--name-only", "--pretty=format:%H", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, strings.TrimSpace(stderr.String())
	}

	commitCountByPrefix := prefixCommitsFromNameOnlyLog(stdout.String())

	diffPref := make(map[string]struct{})
	for _, f := range in.Files {
		p := filepath.ToSlash(strings.TrimSpace(f.Path))
		diffPref[twoSegmentPrefix(p)] = struct{}{}
	}

	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range commitCountByPrefix {
		if v >= hotspotMinCommits {
			list = append(list, kv{k, v})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})

	var out []HotspotInsight
	for _, e := range list {
		if _, ok := diffPref[e.k]; ok {
			out = append(out, HotspotInsight{
				Prefix:      e.k,
				RecentCount: e.v,
				Detail: fmt.Sprintf(
					"Prefix touched in %d of the last %d sampled commits — sustained activity; extra regression care.",
					e.v, hotspotSampleCommits),
			})
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out, ""
}

// prefixCommitsFromNameOnlyLog parses output of:
//
//	git log -n N --name-only --pretty=format:%H
//
// and returns, for each two-segment path prefix, how many distinct commits listed at least one file under that prefix.
func prefixCommitsFromNameOnlyLog(stdout string) map[string]int {
	prefixCommits := make(map[string]map[string]struct{}) // prefix -> commit hash set
	var curHash string
	seenPrefixInCommit := make(map[string]struct{})

	flushCommit := func() {
		if curHash == "" {
			return
		}
		for p := range seenPrefixInCommit {
			if prefixCommits[p] == nil {
				prefixCommits[p] = make(map[string]struct{})
			}
			prefixCommits[p][curHash] = struct{}{}
		}
	}

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isGitObjectID(line) {
			flushCommit()
			curHash = line
			seenPrefixInCommit = make(map[string]struct{})
			continue
		}
		if curHash == "" {
			continue
		}
		line = filepath.ToSlash(line)
		seenPrefixInCommit[twoSegmentPrefix(line)] = struct{}{}
	}
	flushCommit()

	out := make(map[string]int)
	for p, commits := range prefixCommits {
		out[p] = len(commits)
	}
	return out
}

func isGitObjectID(s string) bool {
	// SHA-1 (40) or SHA-256 (64); git log %H emits full hash.
	switch len(s) {
	case 40, 64:
	default:
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
			// ok
		case c >= 'A' && c <= 'F':
			// ok (defensive; git usually lowercases)
		default:
			return false
		}
	}
	return true
}
