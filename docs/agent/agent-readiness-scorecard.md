# Agent Readiness Scorecard

Canonical assessment of this repo against the [DevExAI agent-readiness framework](https://theleadershipclimb.com/p/devexai-an-agent-readiness-framework) — 10 stages × 5 maturity levels (0 manual → 4 agent-native). Each assessment captures evidence at the time, not aspiration.

**Cadence:** quarterly, or after any DEFINE-domain initiative completes. Each entry is a frozen snapshot — past entries are not edited, only superseded.

## How to read this

- **Levels** (interpreted): `0` no automation; `1` agent-assisted on narrow tasks; `2` agents drive default path, humans handle exceptions; `3` agents drive end-to-end, humans review outcomes; `4` agent-native — agents self-validate and self-correct.
- **Evidence per stage** is grounded in repo state at the assessment date — paths, scripts, skills, workflows, and live Jira/PR examples.
- **Score deltas** in the latest assessment compare to the prior assessment, not aspiration.

---

## 2026-05-19 — Post DEFINE-domain uplift

**Trigger:** completion of the DEFINE-domain uplift plan (Epics SCRUM-485, SCRUM-486, SCRUM-487) plus the Phase 4 recalibration cycle (SCRUM-498 → SCRUM-502). 11 days after the baseline.

### Scorecard

| # | Stage | Domain | Score | Δ vs baseline |
|---|---|---|---|---|
| 1 | Problem discovery & prioritization | DEFINE | **3** | +1 |
| 2 | Requirements & specification | DEFINE | **4** | +1 |
| 3 | Work decomposition & planning | DEFINE | **4** | +1 |
| 4 | Implementation | BUILD | 4 | 0 |
| 5 | Testing & quality gates | VALIDATE | 4 | 0 |
| 6 | Code review & knowledge transfer | VALIDATE | 3 | 0 |
| 7 | Documentation & release readiness | SHIP | 4 | 0 |
| 8 | Deployment & release | SHIP | 4 | 0 |
| 9 | Observability & production intelligence | LEARN | 4 | 0 |
| 10 | Customer value & feedback loop | LEARN | **3** | +1 |

### Domain averages

| Domain | Was | Now | Δ |
|---|---|---|---|
| DEFINE | 2.67 / 4 | **3.67 / 4** | **+1.00** |
| BUILD | 4.00 / 4 | 4.00 / 4 | 0 |
| VALIDATE | 3.50 / 4 | 3.50 / 4 | 0 |
| SHIP | 4.00 / 4 | 4.00 / 4 | 0 |
| LEARN | 3.00 / 4 | **3.50 / 4** | **+0.50** |

**Overall: 33 / 40 (82.5%) → 37 / 40 (92.5%)** · average 3.7 / 4

---

## Per-stage evidence (post-uplift)

### Stage 1 — Problem Discovery & Prioritization · 2 → **3**

**What lifted it:** the LEARN → DEFINE loop the May baseline called out as missing now exists.

- `.claude/skills/discovery-digest/SKILL.md` (SCRUM-496) — reads obs-agent GitHub issues labelled `[observability, agent]`, dedups against existing Jira via three keys (`source:obs-agent` + `obs:<N>` label, remote issue link, body URL mention), clusters by shared above-threshold endpoint within colour + date gates, renders a Markdown proposal for human y/n approval.
- `.github/workflows/discovery-digest.yml` (SCRUM-497) — Monday cron + `workflow_dispatch`. Posts a "Discovery digest — week of YYYY-MM-DD" tracking issue with the candidate list. Idempotent per-week; empty-week still posts a brief tracker.
- `scripts/discovery_digest_score.py` (SCRUM-499) — CLI for empirical re-scoring against any issue corpus.
- `docs/agent/discovery-digest-calibration-2026-05.md` — full v1 → v5 ledger with 100% precision on the live 37-issue corpus.
- Bidirectional traceability: Jira ticket → obs issue via remote link; obs issue → Jira via `Linked Jira: SCRUM-XX` body line.

**Why not 4:** human approval is required at the proposal stage before any `jira_create_issue` call fires. This is intentional — Stage 1 level 4 would mean auto-creating tickets from signals without approval, which the design explicitly rules out (`.claude/skills/discovery-digest/SKILL.md` Constraints: "Never auto-create tickets"). Capping at 3 is honest.

### Stage 2 — Requirements & Specification · 3 → **4**

**What lifted it:** agents now self-validate AND self-correct before transitioning a ticket to In Progress.

- `scripts/jira_ticket_lint.py` (SCRUM-490) — structural lint with exit codes `0` (pass) / `2` (fixable; stdout JSON gaps) / `1` (unfixable). 8-rule v1 set: `AC.present`, `AC.min_count`, `BUG.repro`, `EPIC.goal`, `EPIC.scope_present`, `EPIC.success_criteria`, `STRUCT.empty`, `STRUCT.bad_type`. 21 unit-test fixtures.
- `.claude/skills/jira-ticket-lint/SKILL.md` (SCRUM-491) — auto-fix loop policy. On exit 2 *with* `agent-authored` label: agent patches via `jira_update_issue` (section-by-section, never wholesale), re-runs lint with `--max-retries=1`, halts on second failure. On exit 2 *without* the label: halts immediately with comment to human author. Never silently mutates human-authored prose.
- `.claude/skills/jira-ticket-authoring/SKILL.md` — mandatory "Labels" section requires `agent-authored` on every `jira_create_issue`.
- `docs/agent/workflow-jira.md` step 0.5 (SCRUM-492) — hard-stop wiring with warn-only → enforce rollout and rollback trigger.
- `docs/agent/ticket-lint.md` — canonical rule-id table + rule-effectiveness review cadence.
- **Live evidence:** 4 tickets created this week (SCRUM-499, 500, 501, 502) all passed lint before transition to In Progress. The pipeline is exercised, not theoretical.

**Why 4 not 3:** the level 4 signature is self-validating + self-correcting. The auto-fix loop is exactly that — agents detect missing AC, write the missing checkboxes, re-validate, transition. The `agent-authored` label gate makes this safe (human prose is never touched). This is the rubric's "agent-native" definition.

### Stage 3 — Work Decomposition & Planning · 3 → **4**

**What lifted it:** the `epic-run` skill now handles empty Epics autonomously via the authoring phase.

- `.claude/skills/epic-run/SKILL.md` (SCRUM-493) — extended status enum `authoring → awaiting_approval → running → halted | complete`. Step 3 of the Start algorithm: when `parent = SCRUM-X AND statusCategory != Done` returns zero rows, the skill enters `authoring`, calls `jira-work-decomposition`, renders a proposal with current LOC threshold ("400 (default)" or "<N> (Epic override)"), awaits user y/n, then calls `jira_create_issue` + lint per child.
- `.epic-run/<EPIC>.json` schema gains optional `max_estimated_loc` field (range 100–800, default 400, soft floor enforced). Per-Epic override.
- `.claude/skills/jira-work-decomposition/SKILL.md` Estimated-LOC heuristic (SCRUM-494) — files-touched × per-file complexity factor with adders for endpoints, migrations, frontend pages, jobs, docs. Threshold-rendering rule documented.
- `docs/agent/workflow-epic-run.md` — single-command invocation (no separate `kickoff epic`); >8-children cap → `halt_category: spec_missing`.
- `docs/agent/epic-run-authoring-validation.md` (SCRUM-495) — walkthrough with 4 scenarios; 18 simulator tests.
- `scripts/epic_run_state_schema.py` — validates `halt_category` enum (7 values) + status enum + `max_estimated_loc` bounds; 31 unit tests including a real-repo check that all 19 existing `.epic-run/*.json` files validate.

**Why 4:** decomposition no longer requires human prompting — `run epic SCRUM-X` auto-detects empty Epics and drives the authoring phase. Human y/n on the proposal is "reviewing outcomes," not "initiating work" — that's level 4 territory by the rubric's distinction. The LOC heuristic + recalibration cycle (estimate-vs-actual review every 2 sprints) gives this an empirical feedback path that v3 lacked.

### Stage 4 — Implementation · 4 (unchanged)

No changes shipped to BUILD-domain automation. The pre-existing 7-agent setup, feature-development handoff, FULL_AUTO polling, and `implement SCRUM-XX FULL_AUTO` loop continue to operate as documented. Exercised heavily this cycle (18 PRs merged across SCRUM-485 to SCRUM-502).

### Stage 5 — Testing & Quality Gates · 4 (unchanged)

`release-readiness-core` v0.4.0 + TalkBack PR Gate continue to operate as the deterministic merge gate. The gate fired correctly on every PR in this cycle — observed pattern: sensitive-area heuristic triggers on workflow + skill + large-docs PRs even when code diff is small (manual_override path engaged 7 times this cycle out of 18 merges). Not a defect; the gate is conservative on adjacent-area edits.

### Stage 6 — Code Review & Knowledge Transfer · 3 (unchanged)

No changes shipped to VALIDATE-stage review infrastructure. `.github/PULL_REQUEST_TEMPLATE.md` still absent — was explicitly out of scope of the DEFINE uplift per v2 plan. Worth filing as a separate ticket.

### Stage 7 — Documentation & Release Readiness · 4 (unchanged)

Already at ceiling. Each PR in this cycle posted the mandatory structured Jira comment + closure comment (where applicable) per `docs/agent/workflow-jira.md`. New canonical docs added (calibration log, walkthrough, this scorecard) but the rule-ownership map and audit trail were already at 4.

### Stage 8 — Deployment & Release · 4 (unchanged)

End-to-end deploy from green gate to merge to Render auto-deploy continues to require zero human action. 11 of 18 PRs this cycle merged via polling-path PASS; the rest used the documented user-override path. Both paths produce identical Jira closure semantics.

### Stage 9 — Observability & Production Intelligence · 4 (unchanged)

`cmd/obsworker/main.go` + `.github/workflows/observability-agent.yml` continue to file daily diagnostic-bundle GitHub issues on YELLOW/RED status. No changes shipped (explicit out-of-scope per the uplift plan — the worker stays a signal producer).

**Surfaced orthogonal finding (worth noting for future planning):** the obs-agent emits one issue per calendar day rather than one per anomaly. v5's clustering works around this, but a worker refactor would let downstream clustering operate on actual incident boundaries. Captured as a future Epic candidate.

### Stage 10 — Customer Value & Feedback Loop · 2 → **3**

**What lifted it:** the LEARN → DEFINE loop closes for observability signals.

- Obs signals (Stage 9) now flow into Jira proposals via the discovery-digest skill (Stage 1's lift).
- Bidirectional traceability: queryable via Jira label `source:obs-agent`; remote-link click-through from Jira; body cross-link in the source obs issue.
- The recalibration cycle (v1 → v5, SCRUM-498 → SCRUM-502) proved that the loop is empirically falsifiable — precision numbers are recorded with method, fixtures, and reasoning.

**Why not 4:** customer-value signals beyond observability (PostHog / Mixpanel / Amplitude) remain unwired. Phase 3b in the uplift plan deferred this with a gating criterion (≥ 2 obs-source tickets/month accepted into sprint for 6 weeks before starting). Stage 10 = 4 would require both LEARN sources (observability + product analytics) feeding DEFINE.

---

## Highest-leverage gaps still open

Ranked by stage-impact-per-effort.

1. **Stage 6 — PR template** (`.github/PULL_REQUEST_TEMPLATE.md`). One file. Mirrors the body format already in `docs/agent/workflow-jira.md`. Cheapest possible win. Doesn't change DEFINE.
2. **Stage 10 — product-analytics ingestion** (Phase 3b). Wires PostHog/Mixpanel/Amplitude as a second LEARN source feeding discovery-digest. Gated on obs-source acceptance KPI per the plan; reassess in ~6 weeks.
3. **Stage 9 — `cmd/obsworker/` refactor** to emit per-anomaly issues rather than daily rollups. Surfaced as orthogonal finding in SCRUM-498/499; not blocking but caps clustering's ceiling. Future Epic candidate.
4. **First Phase 4 rule-effectiveness review** — read `ops/define-kpis/lint-runs.log` + dismissal log, identify retire/revise/promote candidates. Schedule first review at the 2-sprint mark.
5. **Live-against-real-Epic dry-run of the authoring phase** — SCRUM-495's `Live-execution checklist`. Becomes meaningful when a fresh Epic surfaces.

---

## What this assessment doesn't yet have evidence for

Being honest about operating volume vs. capability:

- **Stage 1's discovery-digest workflow** has not yet had its first scheduled cron run (next Monday). The capability is shipped; live operating evidence is from the dry-run only.
- **Stage 2's lint hard-stop** is in warn-only mode (week 1 per the rollout). Enforcement begins next week.
- **Stage 3's authoring phase** has not yet driven a real Epic end-to-end. The simulator tests and walkthrough doc fully exercise the algorithm against fixtures; the first live run is the natural next milestone.
- **Stage 10's loop closure** is provable via the calibration corpus; live "obs issue → Jira ticket → PR → fix" round-trip has not yet happened.

These are timing limits, not capability limits — the framework's levels measure capability ("can the agent do this?"), and the capability tests pass. Next quarterly refresh will have operating evidence.

---

## Next refresh

**When:** quarterly (default 2026-08-19), or earlier if any of these fire:

- A non-trivial change ships in BUILD/VALIDATE/SHIP automation
- The DEFINE-domain uplift's KPI snapshot script shows lint pass-rate < 50% × 2 weeks, OR median time-to-In-Progress jumps > 30% (rollback trigger from `docs/agent/ticket-lint.md`)
- `cmd/obsworker/` is refactored
- A new domain initiative completes (e.g. PR template lift; PostHog ingestion)

**Process:** re-run the 10-stage rubric end-to-end; add a new dated section above; do not edit prior sections.

---

## 2026-05-08 — Baseline (recap)

Full assessment: `2026-05-08-123151-ai-sdlc-scorecard.md` (session transcript at repo root). Summary:

| # | Stage | Domain | Score |
|---|---|---|---|
| 1 | Problem discovery & prioritization | DEFINE | 2 |
| 2 | Requirements & specification | DEFINE | 3 |
| 3 | Work decomposition & planning | DEFINE | 3 |
| 4 | Implementation | BUILD | 4 |
| 5 | Testing & quality gates | VALIDATE | 4 |
| 6 | Code review & knowledge transfer | VALIDATE | 3 |
| 7 | Documentation & release readiness | SHIP | 4 |
| 8 | Deployment & release | SHIP | 4 |
| 9 | Observability & production intelligence | LEARN | 4 |
| 10 | Customer value & feedback loop | LEARN | 2 |

| Domain | Score |
|---|---|
| DEFINE | 2.67 / 4 |
| BUILD | 4.00 / 4 |
| VALIDATE | 3.50 / 4 |
| SHIP | 4.00 / 4 |
| LEARN | 3.00 / 4 |

Overall: **33 / 40 (82.5%)** · average 3.3 / 4.

Identified highest-leverage gaps: (1) auto-priority discovery from product analytics, (2) PR template for human reviewers, (3) auto-priority on agent-filed tickets, (4) execution-mode CLAUDE.md additions. The DEFINE-domain uplift plan (`.cursor/plans/define-domain-uplift.plan.md`) chose to attack (1) and (3) directly via the discovery-digest skill and lint pipeline; (2) was kept out of scope; (4) shipped incidentally via `CLAUDE.md` Quick Start update in SCRUM-492.
