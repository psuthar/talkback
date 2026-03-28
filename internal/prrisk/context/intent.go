package riskcontext

import (
	"os/exec"
	"sort"
	"strings"
)

// keywordRule maps a lowercase keyword to expected domain labels (prrisk Domain* strings).
var keywordRules = []struct {
	kw      string
	domains []string
}{
	{"auth", []string{"auth"}},
	{"login", []string{"auth"}},
	{"invite", []string{"auth"}},
	{"session", []string{"auth", "api"}},
	{"migration", []string{"migrations"}},
	{"rag", []string{"rag"}},
	{"qa", []string{"rag"}},
	{"ask", []string{"rag"}},
	{"workflow", []string{"workflows"}},
	{"github", []string{"workflows"}},
	{"ci", []string{"workflows"}},
	{"deploy", []string{"deploy"}},
	{"docker", []string{"deploy"}},
	{"render", []string{"deploy"}},
	{"e2e", []string{"web"}},
	{"playwright", []string{"web"}},
	{"test", nil},
	{"fix", nil},
	{"refactor", nil},
}

// AnalyzeIntent compares PR title/body keywords to domains present in the diff.
func AnalyzeIntent(in Input) IntentInsight {
	title := strings.TrimSpace(in.PRTitle)
	body := strings.TrimSpace(in.PRBody)
	if title == "" && in.RepoRoot != "" && in.GitError == "" {
		title, body = gitHeadMessage(in.RepoRoot)
	}

	combined := strings.TrimSpace(strings.ToLower(title + " " + body))
	weakTitle := isWeakTitle(title)

	if combined == "" {
		return IntentInsight{
			IntentStrength: "unknown",
			Aligned:        true,
			Mismatch:       false,
			Detail:         "No PR title/body or git subject available; intent alignment skipped.",
		}
	}

	var matched []string
	var expected []string
	seen := make(map[string]struct{})
	for _, rule := range keywordRules {
		if rule.kw == "" {
			continue
		}
		if !strings.Contains(combined, rule.kw) {
			continue
		}
		if rule.domains == nil {
			continue
		}
		matched = append(matched, rule.kw)
		for _, d := range rule.domains {
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			expected = append(expected, d)
		}
	}

	strength := "strong"
	if weakTitle {
		strength = "weak"
	}
	if len(expected) == 0 && len(matched) == 0 {
		strength = "unknown"
	}

	inDiff := domainsPresent(in.DomainHits)

	// Weak/generic title with no domain keywords: do not report as "aligned".
	if len(expected) == 0 && weakTitle {
		return IntentInsight{
			Title:           title,
			IntentStrength:  "weak",
			KeywordsMatched: matched,
			Aligned:         false,
			Mismatch:        false,
			DomainsInDiff:   inDiff,
			Detail:          "PR title/body is weak or generic; intent alignment not inferred. Use a descriptive title (area + change).",
		}
	}

	if len(expected) == 0 {
		return IntentInsight{
			Title:           title,
			IntentStrength:  "unknown",
			KeywordsMatched: matched,
			Aligned:         true,
			Mismatch:        false,
			DomainsInDiff:   inDiff,
			Detail:          "No strong intent keywords matched; alignment not scored.",
		}
	}

	mismatch := false
	for _, exp := range expected {
		if !containsDomain(inDiff, exp) {
			mismatch = true
			break
		}
	}

	detail := "Keywords in the title/body align with domains touched in the diff."
	if mismatch {
		detail = "Title/body suggests certain areas (keywords) but corresponding paths may be missing from this diff — confirm scope or update the PR description."
	}
	if weakTitle {
		detail += " (Title is still weak; prefer a clearer summary for reviewers.)"
	}

	return IntentInsight{
		Title:           title,
		IntentStrength:  strength,
		KeywordsMatched: matched,
		DomainsExpected: expected,
		DomainsInDiff:   inDiff,
		Aligned:         !mismatch,
		Mismatch:        mismatch,
		Detail:          detail,
	}
}

func isWeakTitle(title string) bool {
	t := strings.TrimSpace(strings.ToLower(title))
	if t == "" {
		return true
	}
	if len(t) <= 4 {
		return true
	}
	generic := map[string]bool{
		"wip": true, "fix": true, "update": true, "bump": true, "patch": true,
		"misc": true, "draft": true, "changes": true, "temp": true, "test": true,
	}
	if generic[t] {
		return true
	}
	// Very short single-token titles (e.g. "cleanup")
	if !strings.Contains(t, " ") && len(t) <= 8 {
		return true
	}
	return false
}

func gitHeadMessage(repoRoot string) (subject, body string) {
	cmd := exec.Command("git", "-C", repoRoot, "log", "-1", "--format=%s%n%b")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(out), "\n", 2)
	subject = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		body = strings.TrimSpace(parts[1])
		if len(body) > 800 {
			body = body[:800]
		}
	}
	return subject, body
}

func domainsPresent(hits map[string]int) []string {
	if hits == nil {
		return nil
	}
	var keys []string
	for k, n := range hits {
		if n > 0 && k != "tests" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func containsDomain(inDiff []string, domain string) bool {
	for _, d := range inDiff {
		if d == domain {
			return true
		}
	}
	if domain == "web" {
		for _, d := range inDiff {
			if d == "web" || d == "tests" {
				return true
			}
		}
	}
	return false
}
