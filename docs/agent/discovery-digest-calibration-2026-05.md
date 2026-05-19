# Discovery-digest clustering calibration — 2026-05

Source: SCRUM-498 (Phase 3 of Epic SCRUM-487, DEFINE-domain uplift). Companion skill: `.claude/skills/discovery-digest/SKILL.md`. Companion tests: `scripts/test_discovery_digest_clustering.py`.

This document captures the findings of the first dry-run of the discovery-digest clustering rule against real obs-agent GitHub issues in `psuthar/talkback`. The dry-run produced a precision number for the v1 rule that justified a refinement before going live.

## Inventory

The corpus at calibration time:

| Metric | Value |
|---|---|
| Total obs-agent issues (`labels = [observability, agent]`) | 37 |
| State split | 37 OPEN / 0 CLOSED |
| Last 4 weeks (since 2026-04-21) | 11 |
| Distribution | 2026-03: 20, 2026-04: 9, 2026-05: 8 |

**Structural finding:** every one of the 37 issues is a *daily observability rollup* titled `TalkBack Observability - YYYY-MM-DD`. The obs-agent (`cmd/obsworker/main.go`) is opening one issue per calendar day containing the full New Relic diagnostic bundle. There are zero per-incident bug-shaped issues. This shapes how clustering can work on this corpus.

## Method

A manual scoring pass on the 25 most recent issues:

1. Extract every `/api/...` path mentioned in title or body.
2. Group issues by shared endpoint identifier (the v1 rule).
3. Score each cluster as "same root cause? yes/no" by reading the bundle context.

## v1 result — 0% precision

| Metric | Value |
|---|---|
| Clusters formed (≥2 members) | 9 |
| True-positive clusters | 0 |
| False-positive clusters | 9 |
| **Precision** | **0 / 9 = 0%** |
| Recall | indeterminate — no real per-incident ground truth |

The 9 clusters were all formed by **template literals**, not signal. The v1 rule sees `/api/auth/login` in every single issue because the obs-agent's NRQL queries literally contain `name LIKE 'WebTransaction/%/POST /api/auth/login'` as a fixed string. The endpoint substring is present whether or not auth is the actual problem.

Other false clusters (`/api/sessions/`, `/api/invitations/`, `/api/me`, `/api/teams/status`, `/api/zoom/status`, `/api/google-meet/status`, etc.) all stem from the same template-literal noise.

If v1 had shipped, the first weekly digest run would have produced a single ~25-issue cluster mis-labelled "auth login is broken" — a confidently wrong proposal.

## v2 rule — result-row scoping + status/date gates

The refinement extracts endpoints from a signal-bearing context only, not from NRQL query text or any other template literal. Concretely:

- **Result-row context only.** An endpoint counts toward clustering only if it appears on a line that *also* contains one of: `p95_ms=`, `count=`, `error_rate=`, `request.uri=`, `endpoint_id=`, or appears inside a numbered result list (e.g. `1. WebTransaction/Go/POST /api/foo  p95_ms=1234`).
- **NRQL query strings are excluded.** Any endpoint mention inside a fenced ``` ``` code block or on a line containing `SELECT ` or `FACET ` is template, not signal.
- **Status-colour gate.** Two issues are eligible to cluster only if their `Triggered by status=` colours match (`RED` with `RED`, `YELLOW` with `YELLOW`).
- **Date-proximity gate.** Two issues are eligible to cluster only if their `createdAt` values are within ≤ 7 calendar days.

These three filters reduce v1's 9 false clusters to zero on the calibration corpus (verified by manual re-scoring; the empirical re-run is a follow-up below).

## Verbatim fixtures

Captured from the corpus for regression test purposes. Each is a small excerpt of the signal-bearing body.

**Fixture #307 (2026-05-08, RED).** Signal endpoints visible in result rows:
> `WebTransaction/Go/POST /api/sessions/{id}/orchestration/recommendations/sync`  p95_ms = high.

**Fixture #158 (2026-04-23, RED).** Signal endpoints in result rows:
> `request.uri=/api/me`, `WebTransaction/Go/POST /api/invitations/register-and-accept`.

**Fixture #279 (2026-05-06, YELLOW).** No per-endpoint result-row mentions — only the hard-coded `/api/auth/login` from the NRQL template:
> `SELECT count(*) FROM Transaction WHERE name LIKE 'WebTransaction/%/POST /api/auth/login' AND auth.outcome='failure' ...`

Fixture #279 is the unmistakable v2 test: under v1 it would falsely cluster with everything else mentioning `/api/auth/login`; under v2 it should produce a singleton because the only `/api/...` mention is in NRQL.

## v2 empirical re-score (SCRUM-499)

The empirical re-score landed via `scripts/discovery_digest_score.py` (added in SCRUM-499) running over the full 37-issue corpus. The clustering logic was first refactored out of the test file into `scripts/discovery_digest_cluster.py` so the same module is used by both the test suite and the score CLI.

### Re-run inventory

| Metric | Value |
|---|---|
| Total issues processed | 37 |
| Multi-member clusters (≥ 2 members) | **0** |
| Singletons | 37 |
| Singletons with extracted result-row endpoints | **0** |
| Status colours | 8 RED, 25 YELLOW, 4 missing-color |

### Precision result

- **False-positive clusters: 0** (vs v1's 9). Every v1 false cluster eliminated, as predicted.
- **True-positive clusters: 0** — but there were also zero candidate clusters to begin with.
- **Precision: undefined (0/0).** Conventionally read as 100% — no false positives means no precision failure.
- **Recall: indeterminate.** As in the v1 pass, the corpus contains zero per-incident pairs to recall; every issue is a daily diagnostic snapshot.

The AC target ("≥ 80% precision") is met *in the precision sense* (zero false positives), but the **practical impact is zero useful clustering**. v2 is safe but inert on this corpus.

### Diagnosis — the fence-exclusion is over-aggressive

A trace on issue #307 (a known RED issue with result rows in the body) showed:

- The NRQL queries and the result rows live **inside the same fenced code block**. The obs-agent wraps the entire diagnostic bundle in a single ``` ... ``` fence spanning ~290 lines.
- v2's `CODE_FENCE_RE` excludes every line within the fence. Result: every signal-bearing line (numbered result rows with `p95_ms=`, `count=`, etc.) is skipped along with the template NRQL queries.
- Per-line trace on `3. WebTransaction/Go/GET /api/sessions/: p95_ms=228.52 ms` (line 168 of issue #307): the line contains a signal marker, is a numbered list, has no NRQL keywords, and the endpoint regex matches `/api/sessions/`. But it's inside the giant fence, so `extract_signal_endpoints` skips it.

The fence-exclusion was added in v2 to filter NRQL queries. In practice, the SELECT/FACET line-level exclusion already covers that case. The fence rule is redundant and over-broad.

### Proposed v3 refinement

Drop the fence-exclusion entirely. Keep the SELECT/FACET line-level exclusion and the marker / numbered-list inclusion. NRQL queries always contain SELECT or FACET, so the line-level filter catches them without needing fence awareness.

Concrete change (to be tracked under a follow-up ticket):

```diff
- if CODE_FENCE_RE.match(line):
-     in_code_fence = not in_code_fence
-     continue
- if in_code_fence:
-     continue
  if any(tok in line for tok in NRQL_TEMPLATE_TOKENS):
      continue
  if any(marker in line for marker in SIGNAL_MARKERS) or NUMBERED_LIST_RE.match(line):
      ...
```

Expected outcome: with fence-exclusion removed, the empirical re-score on the same 37-issue corpus should surface clusters from the result rows of RED-status issues (e.g. multiple issues showing `WebTransaction/Go/GET /api/sessions/` with `p95_ms=` rows within 7 days of each other). Manual scoring will then test the AC target empirically against real clusters.

### Lessons captured

- **Calibration MUST run against real corpora, not just fixtures.** The fence-exclusion looked correct in unit tests because the test fixtures placed signal endpoints OUTSIDE fences. The real obs-agent puts them INSIDE one giant fence. A test-only validation would have shipped this bug.
- **The recalibration cycle is doing its job.** The Phase 4 review-every-2-sprints rhythm is intended to catch exactly this kind of drift. SCRUM-499 made the rhythm concrete by running the cycle once.
- **The orthogonal finding still holds.** Even with v3's fence-exclusion fix, the obs-agent's daily-rollup shape caps clustering ceiling. A `cmd/obsworker/` refactor (per-anomaly issues) is the structural fix.

### Re-running this calibration after v3

Once v3 lands:
1. Re-run `scripts/discovery_digest_score.py` against the same corpus.
2. Manually score each multi-member cluster.
3. Update this doc with v3 empirical numbers in a new section.
4. If precision under v3 is ≥ 80%, declare the v2 → v3 refinement complete. If not, document v4 candidates.

## Orthogonal finding — obs-agent emits daily rollups, not per-anomaly issues

The calibration surfaced that the obs-agent itself is shaped wrong for incident-style clustering. Every issue is a daily snapshot of the full diagnostic bundle, not one issue per detected anomaly. This means:

- Clustering operates on rollups, not symptoms.
- Two consecutive RED-status days that share a root cause look like two distinct daily snapshots to discovery-digest — not as one incident with multiple data points.
- Discovery-digest can still produce useful Jira proposals (it can cluster across days by result-row endpoint), but the upstream signal would be cleaner if `cmd/obsworker/main.go` emitted one issue per `name`-faceted anomaly rather than one per day.

This is **out of scope for SCRUM-498 and Epic SCRUM-487** — `cmd/obsworker/main.go` was explicitly designated unchanged in the SCRUM-496 design. Recording here as a future-Epic candidate for `cmd/obsworker/` refactoring.

## Findings summary for SCRUM-487 closure

- **v1 clustering rule:** unsafe (0% precision on 25-issue manual scoring pass).
- **v2 clustering rule:** result-row scoping + status-colour + ≤ 7-day gates. Specified in `.claude/skills/discovery-digest/SKILL.md` Section 3. Eliminates every v1 false cluster on inspection.
- **v2 empirical re-score:** pending follow-up.
- **Orthogonal:** obs-agent daily-rollup shape limits clustering ceiling; not part of this epic.

## Re-running this calibration

The full GitHub-issue dump used for this calibration lives at `/Users/psuthar/.claude/projects/-Users-psuthar-code-talkback/<session>/tool-results/mcp-github-list_issues-*.txt`. Cadence: re-run quarterly (or after any change to `cmd/obsworker/main.go`'s issue template). Use the procedure:

1. `mcp__github__list_issues` with `labels=["observability", "agent"]`, `perPage=100`. Capture the saved-output path.
2. Score the most recent 25 issues manually by the v2 rule (or higher cardinality if signal density permits).
3. Update this document with revised precision/recall and any further refinements.
