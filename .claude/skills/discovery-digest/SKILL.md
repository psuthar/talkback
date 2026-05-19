# Skill: discovery-digest

Policy source: this skill owns its own execution policy. Related docs: `docs/agent/overview.md` (loop description, SCRUM-497), `docs/agent/ticket-lint.md` (lint applies to digest-created tickets, SCRUM-490), `.claude/skills/jira-ticket-authoring/SKILL.md` (Labels mandatory rule, SCRUM-491).

Companion workflow: `.github/workflows/discovery-digest.yml` (SCRUM-497) runs this skill on a weekly cron and via `workflow_dispatch`.

## Purpose

Convert observability signals (YELLOW / RED issues from `cmd/obsworker/`) into proposed Jira tickets without losing dedup, clustering, or human approval discipline. The skill closes the LEARN → DEFINE loop the DevExAI scorecard calls out as the primary lift opportunity for Stages 1 and 10.

The skill never auto-creates tickets. Every Jira create requires explicit user y/n on a rendered proposal.

## Invocation

```
discovery digest
run discovery digest
```

Equivalent for behaviour. Both invoke the algorithm below. The companion cron also invokes the skill non-interactively — in that mode, the skill produces a Markdown proposal and stops at the approval gate, leaving creation for a human follow-up.

## Inputs

The skill is a pure orchestrator over the GitHub and Atlassian MCPs. No new scripts or services; no Jira credentials in CI beyond what the workflow already provides.

| Source | Read via | What we use |
|---|---|---|
| GitHub issues with labels `observability, agent` | `mcp__github__list_issues` (or `search_issues` for state filtering) | Open issues today; closed last 30d as dedup negative-cases |
| Existing Jira tickets with label `source:obs-agent` | `mcp__atlassian__jira_search_issues` (`labels = "source:obs-agent"`) | Dedup key — every linked obs-issue carries the issue number in the body or remote link |
| Existing Jira remote links | `mcp__atlassian__jira_get_issue` per ticket of interest | Secondary dedup check |

`cmd/obsworker/main.go` is **not** modified. The Go worker stays a signal producer; this skill is the only new Jira-aware code in the loop.

## Algorithm

### 1. Fetch open obs signals

```
mcp__github__list_issues(
    owner="psuthar",
    repo="talkback",
    labels=["observability", "agent"],
    state="open",
)
```

Filter the result to the last **28 days** (4 weeks) by `created_at`. Older still-open signals are stale; the obs agent files a fresh issue per status-change day, so anything older than four weeks indicates the signal is either acknowledged or has self-resolved without action.

### 2. Dedup against Jira

For each obs issue, build the dedup key as the GitHub issue's `html_url` plus the issue number.

```
mcp__atlassian__jira_search_issues(
    jql='labels = "source:obs-agent" AND statusCategory != Done',
    fields=["summary", "labels", "description", "issuelinks"],
)
```

An obs issue is **already linked** if any Jira ticket in the result:

- carries a label of the shape `obs:<number>` (e.g. `obs:42`), **or**
- has a remote issue link to the obs issue's `html_url`, **or**
- mentions the obs URL anywhere in its description (last-resort fallback for legacy ad-hoc links).

Skip already-linked obs issues. Track the skip in a dry-run summary for audit but do not list them in the proposal.

### 3. Cluster by theme

Within the unlinked set, group obs issues by **observed endpoint that is actually slow**. The clustering rule (v5 — SCRUM-498 → SCRUM-502 recalibration cycle: v1 scored 0%, v2 inert, v3 33%, v4 50%, v5 expected ~100%. See `docs/agent/discovery-digest-calibration-2026-05.md`):

**Result-row scoping (mandatory).** Extract an endpoint identifier (e.g. `/api/foo/{id}`, `WebTransaction/Go/POST /api/bar`) only when it appears on a line that *also* contains one of these signal markers:

- `p95_ms=`
- `count=`
- `error_rate=`
- `request.uri=`
- `endpoint_id=`
- Or sits inside a numbered result list (e.g. `1. WebTransaction/Go/POST /api/foo  p95_ms=1234`).

**Exclude NRQL template lines.** Any line containing `SELECT ` or `FACET ` (case-sensitive — NRQL keywords) is template noise and does NOT contribute endpoints. The obs-agent's NRQL queries hard-code endpoint names in `name LIKE 'WebTransaction/%/POST /api/auth/login'`-style filters; those mentions are not signal. **Note:** v2 also excluded all lines inside fenced code blocks, but the obs-agent wraps the entire diagnostic bundle (NRQL AND result rows) in one giant fence — so fence-exclusion swallowed signal too. v3 dropped the fence rule and relies solely on the SELECT/FACET line-level filter, which catches every NRQL line independently.

**p95 threshold filter (v4).** Each extracted endpoint carries the `p95_ms=N` value parsed from the same line. An endpoint contributes to clustering only when its p95 meets the configured threshold (**default 100 ms**). Endpoints below the threshold are top-N baseline noise — high-traffic routes that appear in every issue's latency ranking whether or not they're actually slow that day — and are filtered out. Override per-invocation via `scripts/discovery_digest_score.py --min-p95-ms N` if the corpus calls for a different value. SCRUM-500's empirical re-score showed v3 (no threshold) produced 33% precision; v4's threshold filter is expected to push precision to ~100% on the same corpus.

**Status-colour gate.** Two obs issues are eligible to cluster only if their `Triggered by status=` colour matches (RED with RED, YELLOW with YELLOW). Mixing colours implies different urgency / different incident.

**Date-proximity gate.** Two obs issues are eligible to cluster only if their `createdAt` timestamps are within **≤ 7 calendar days** of each other. Wider windows cluster unrelated incidents that happened to touch the same endpoint weeks apart.

**Cluster membership rule (v5 — per-endpoint grouping).** For each above-threshold endpoint, find the set of issues that contain it (passing colour + date-proximity gates). Each connected group within that set forms one cluster anchored on that endpoint. A bridge issue with two slow endpoints appears in two clusters — one per slow endpoint — each with its own concrete anchor. This replaces v4's union-find transitivity, which could create 3-member chains where no single endpoint was shared across all members (a structural false positive). Under v5, every multi-member cluster has a non-empty `shared_endpoints` field by construction. Member sets that arise multiple times (the same pair sharing two above-threshold endpoints) are deduplicated.

Clusters with **≥ 2 obs issues** become a single Jira proposal. Single-element clusters also become proposals — they're still candidate tickets — but rendered separately so the human can see clustering effectiveness over time.

Re-calibration: re-run the procedure in `docs/agent/discovery-digest-calibration-2026-05.md` quarterly (or after any change to `cmd/obsworker/main.go`'s issue template) and update both this section and that doc with revised thresholds if precision drifts.

### 4. Render the proposal

For each cluster (or single-element candidate), render a Markdown proposal:

```
Discovery digest — week of YYYY-MM-DD

Cluster 1 (3 obs issues, endpoint /api/sessions/import):
  - obs#142 (RED, 2026-05-12): "p95 latency 8.2s vs 2.1s baseline"
  - obs#147 (YELLOW, 2026-05-14): "error rate 3.1% (was 0.4%)"
  - obs#151 (RED, 2026-05-18): "throughput drop 40% vs 7d"

  Suggested ticket:
    Type: Bug
    Title: /api/sessions/import latency + error-rate regression (week of 2026-05-18)
    Body (draft): <agent-generated, lint-clean per Phase 1>

Candidate 2 (1 obs issue, endpoint /api/qa/ask):
  - obs#149 (YELLOW, 2026-05-15): "answer latency p95 4.5s (target 2.5s)"
  ...

Approve any or all (y / y1,y3 / n)?
```

The body draft for each suggested ticket MUST be lint-clean per `docs/agent/ticket-lint.md` — the Phase 1 lint applies to digest-created tickets exactly as to any other agent-authored ticket. If the draft fails lint on a dry-run, the proposal annotates the failure for the reviewer.

### 5. Approval gate

| User response | Action |
|---|---|
| `y` | Create every proposed ticket (see step 6). |
| `y1,y3` (subset) | Create only the listed clusters/candidates. |
| `n` | Record dismissal in `ops/define-kpis/discovery-dismissed.log` (one JSONL row per skipped cluster: `{ts, obs_issue_numbers, dismissal_reason}` if the user supplied one). No Jira state created. |

### 6. Create + link

For each approved cluster:

1. `mcp__atlassian__jira_create_issue` with:
   - `projectKey="SCRUM"`, `issueType="Bug"` (or `Task` if the agent classifies it as work rather than regression)
   - `summary` and `description` from the lint-clean draft
   - `labels=["source:obs-agent", "agent-authored", *one label per obs issue number: "obs:142", "obs:147", "obs:151"*]`
2. `mcp__atlassian__jira_create_issue_link` (remote variant) to each obs issue's `html_url`. The label is the queryable handle (JQL `labels = "source:obs-agent"`); the remote link is the human-clickable backreference.
3. Update each obs GitHub issue body via `mcp__github__create_or_update_file`-equivalent (or `update_issue` if the MCP exposes a direct write) appending the single line: `Linked Jira: SCRUM-XX`. The update is idempotent — if the line already exists, skip the body update (this matters when the workflow re-runs before a previous run's tickets transition to Done).

### 7. Run summary

After all approved clusters are created, post a single comment on the parent DEFINE-uplift Epic (SCRUM-487 or its successor) summarizing:

- Obs issues considered, skipped (already linked), proposed, dismissed, created
- Cluster sizes (median, max)
- Lint-fail count on drafts (zero is the goal)

The summary is the input to the Phase 4 rule-effectiveness review.

## Cadence

- **Weekly default** (Monday morning) via `.github/workflows/discovery-digest.yml` (SCRUM-497).
- **On-demand** via `discovery digest` invocation by an operator.
- **Not hourly.** Hourly obs signals are noisy; many self-resolve within 24h. Weekly forces clustering and gives signal time to stabilise.

## Constraints

- **Never auto-create tickets.** Every Jira create requires explicit y/n on a rendered proposal. This applies in cron mode too — the workflow renders the proposal as a comment on a tracking issue and stops; a human invokes the skill interactively to approve.
- **Dedup is mandatory.** Skipping already-linked obs issues prevents the loop from creating duplicate Jira tickets when the workflow re-runs while a previous ticket is still open or In Review.
- **Lint applies.** Every digest-created ticket carries `agent-authored` per `jira-ticket-authoring/SKILL.md` Labels (mandatory) section; the Phase 1 lint runs on transition to In Progress.
- **No edits to `cmd/obsworker/main.go`.** The Go worker stays a signal producer; this skill is the layer that bridges to Jira. Wrong layering would mean adding Jira credentials to the Go binary's runtime, which contradicts the Phase 3 design decision documented in `.cursor/plans/define-domain-uplift.plan.md`.
- **No PostHog or analytics signals in v1.** SCRUM-498 calibrates the obs-only flow; Phase 3b (PostHog) is gated on 6-week acceptance KPI per the plan.
- **Dismissal log is durable.** `ops/define-kpis/discovery-dismissed.log` is tracked in git (alongside `lint-runs.log` and KPI snapshots) so Phase 4 reviews can examine dismissed clusters for missed signal patterns.
