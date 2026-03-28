package riskcontext

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const hotspotMinHits = 6

// AnalyzeHotspots finds path prefixes that appear often in recent history and overlap the current diff.
func AnalyzeHotspots(in Input) ([]HotspotInsight, string) {
	if in.GitError != "" || in.RepoRoot == "" {
		return nil, ""
	}

	cmd := exec.Command("git", "-C", in.RepoRoot, "log", "-n", "50", "--name-only", "--pretty=format:", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, strings.TrimSpace(stderr.String())
	}

	counts := make(map[string]int)
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = filepath.ToSlash(line)
		prefix := twoSegmentPrefix(line)
		counts[prefix]++
	}

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
	for k, v := range counts {
		if v >= hotspotMinHits {
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
				Detail:      "This prefix appears frequently in recent commits; extra care on regressions.",
			})
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out, ""
}
