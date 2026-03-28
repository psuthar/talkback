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
	{"e2e", []string{"web"}},       // e2e implies web-layer changes; "tests" excluded from domainsPresent
	{"playwright", []string{"web"}}, // same rationale as e2e
	{"test", nil},                   // too generic — skip domain inference
	{"fix", nil}, // too generic — only used with another keyword
	{"refactor", nil},
}

// AnalyzeIntent compares PR title/body keywords to domains present in the diff.
func AnalyzeIntent(in Input) IntentInsight {
	title := strings.TrimSpace(in.PRTitle)
	body := strings.TrimSpace(in.PRBody)
	if title == "" && in.RepoRoot != "" && in.GitError == "" {
		title, body = gitHeadMessage(in.RepoRoot)
	}

	combined := strings.ToLower(title + " " + body)
	if combined == "" {
		return IntentInsight{
			Aligned:  true,
			Mismatch: false,
			Detail:   "No PR title/body or git subject available; intent alignment skipped.",
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

	inDiff := domainsPresent(in.DomainHits)
	if len(expected) == 0 {
		return IntentInsight{
			Title:           title,
			KeywordsMatched: matched,
			Aligned:         true,
			Mismatch:        false,
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

	return IntentInsight{
		Title:           title,
		KeywordsMatched: matched,
		DomainsExpected: expected,
		DomainsInDiff:   inDiff,
		Aligned:         !mismatch,
		Mismatch:        mismatch,
		Detail:          detail,
	}
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
	// web E2E often only touches web/tests — accept web keyword if tests present
	if domain == "web" {
		for _, d := range inDiff {
			if d == "web" || d == "tests" {
				return true
			}
		}
	}
	return false
}
