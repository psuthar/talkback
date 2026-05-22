# FULL_AUTO orchestration scripts — runbook

Owner: SCRUM-529 (Epic). Phase 0 ticket: SCRUM-528.

This is the operational runbook for `scripts/full_auto/`, the Python package that extracts deterministic chunks of the `implement SCRUM-XX FULL_AUTO` workflow so they can be called from Claude Code today and from a webhook listener later.

> **Phase 0 only ships the credential plumbing.** No behaviour change. Subsequent phases (SCRUM-530 `close.py`, SCRUM-531 dry-run validation, SCRUM-532 CLAUDE.md cut-over) build on this foundation.

---

## Deployment-target matrix

The same Python code runs in three different environments. The only difference is how the environment variables are populated.

| Where | When | `GITHUB_TOKEN` from | `ATLASSIAN_EMAIL` / `ATLASSIAN_API_TOKEN` from |
|---|---|---|---|
| Local laptop (you) | Today, replacing manual close-out steps | Auto-fallback to `gh auth token` (already logged in) | Auto-loaded from `.env.local` at import time (SCRUM-533); shell-source / `direnv` also work and take precedence |
| Webhook listener (future) | Cloudflare tunnel → local Flask app | Either `gh auth token` (if the listener runs as you) OR an env var | Listener's environment, populated from disk |
| GitHub Actions (future) | Scheduled / event-triggered runs | `secrets.GITHUB_TOKEN` (auto-issued, repo-scoped) | Repo secrets injected as env vars |

All three call `scripts/full_auto/lib/auth.py` with the same code path.

---

## Local setup

### 1. Copy the example file

```sh
cp .env.local.example .env.local
```

`.env.local` is explicitly listed in `.gitignore` (added at SCRUM-528 — the bare `.env` rule is a literal-match and does NOT cover `.env.local`). **Never commit it.**

### 2. Generate an Atlassian API token

Visit https://id.atlassian.com/manage-profile/security/api-tokens.

**Recommendations:**
- Use the **scoped** form (not the legacy full-access form).
- Grant only `read:jira-work` + `write:jira-work`. The FULL_AUTO scripts need to read tickets, post comments, and transition issues — nothing more.
- Set an expiry of **90 days**. Atlassian doesn't auto-rotate; you'll get an email at expiry.

Copy the token (only shown once at creation).

### 3. Populate `.env.local`

```
ATLASSIAN_EMAIL=paresh+talkback_ai@suthar.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...
# ATLASSIAN_BASE_URL=  (optional; defaults to TalkBack's tenant)
# GITHUB_TOKEN=        (optional; gh auth token fallback handles this)
```

### 4. Run scripts

`.env.local` is auto-loaded from the repo root at import time (SCRUM-533) — no shell sourcing required:

```sh
python -m scripts.full_auto.close SCRUM-XX --pr N --path polling
```

Shell-sourced values, CI repo secrets, and webhook env injection always win over `.env.local`, so the file is a zero-friction *fallback*, not an override.

**Optional: `direnv`.** If you want the same env vars available outside the FULL_AUTO scripts (for example, ad-hoc `curl` against the Jira API from your shell), keep using `direnv`:

```sh
brew install direnv   # if needed
echo "dotenv .env.local" > .envrc
direnv allow
```

---

## Verifying the auth module

```sh
python3 -c "from scripts.full_auto.lib.auth import github_token, jira_auth, atlassian_base_url; \
            print('github_token: present' if github_token() else 'missing'); \
            print('jira_auth:', jira_auth()[0]); \
            print('atlassian_base_url:', atlassian_base_url())"
```

If credentials are wrong, the helpers raise with an actionable error message pointing at the right fix.

---

## Token rotation

| Token | Auto-rotation? | When you'll know it expired |
|---|---|---|
| `ATLASSIAN_API_TOKEN` | No | Scripts return 401 on next Jira call |
| `gh auth login` token | Refreshes on use; ~no expiry in practice | N/A |
| `GITHUB_TOKEN` (CI) | Per-workflow-run | Doesn't apply outside CI |

**Rotation procedure for Atlassian:**
1. Generate a new scoped token at the link above (set new 90-day expiry).
2. Update `.env.local` (or your repo secret).
3. Revoke the old token in the Atlassian token list.

---

## How Claude uses close.py in the FULL_AUTO flow (Phase 3 cut-over)

Once the gate reaches PASS + mergeable clean, Claude runs **one command** instead of the ~14 individual tool calls it used to make:

```sh
python -m scripts.full_auto.close <TICKET> --pr <N> --path polling
```

`close.py` performs the full close-out sequence in order:

1. **Pre-merge guard** — re-reads the PR via the GitHub API. If `mergeable_state` is not `clean` (and the PR is not already merged), aborts with a structured reason; nothing else runs. Output: `actions_taken` lists the abort.
2. **Merge** — `PUT /pulls/<N>/merge` with `merge_method: squash`. Skipped when the PR is already merged at entry (manual-override case below).
3. **Local git** — `fetch origin --prune` → `checkout main` → `pull --ff-only origin main` → `branch -D feat/<TICKET>`. The new main SHA is captured into `result.main_sha_after` for the closure comment.
4. **State file** — looks for `.epic-run/<EPIC>.json` referencing the ticket; if found, marks the entry `status: done` + `merged_sha` + `final_gate`. No-op if no state file references this ticket.
5. **Jira → Done** — resolves the Done transition id dynamically (by name, not hardcoded id) and posts the transition.
6. **Closure comment** — renders the polling-path template with all collected values, posts it.

`close.py` returns a `CloseResult` dataclass. Claude relays its `actions_taken` list to the user as the chat-surface confirmation. The user sees one structured summary, not 14 separate "did X" lines.

### Path indicators

| `--path` value | When to use | Closure-template shape |
|---|---|---|
| `polling` | Default for `implement SCRUM-XX FULL_AUTO` — Claude observed PASS + clean and is performing the merge. | "FULL_AUTO complete — polling path (default). PR #N squash-merged at ..." |
| `manual-override` | User squash-merged manually (typically after a structural WARN they accepted). Claude is reconciling, not merging. | "Closure (user-override squash-merge path). PR #N squash-merged by user at ..." |
| `webhook` | Future — when the Cloudflare-tunnel webhook listener invokes close.py. | "FULL_AUTO complete — webhook path (FULL_AUTO_WEBHOOK). Local cleanup skipped..." |

### Failure modes

| Symptom | Most likely cause | Fix |
|---|---|---|
| `RuntimeError: Missing Jira credentials.` | `.env.local` missing at repo root, malformed, or values still set to the example placeholders | Check the file exists at the repo root, that keys are spelled `ATLASSIAN_EMAIL` / `ATLASSIAN_API_TOKEN` exactly, and that values are real (not `you@example.com`) |
| `RuntimeError: PUT /pulls/N/merge -> 405: ...` | PR is not mergeable (closed, has conflicts, branch protection failed) | Check `gh pr view N` for state; manual override may be required |
| `RuntimeError: No 'Done' transition available on SCRUM-X` | Ticket workflow doesn't expose a Done transition (e.g., already Done, or in a non-standard state) | Check current Jira status; transition manually if needed |
| `aborted_reason: "mergeable_state was 'blocked' not 'clean'"` | Pre-merge guard fired; PR state changed between polling and the guard re-read | Investigate the PR; usually a CI flake or a required check that re-triggered |

### What stays Claude's responsibility (NOT delegated to close.py)

- The **WARN/BLOCK halt** path. close.py is the PASS path; halt.py is a future Epic.
- The **worktree-based cleanup** (ExitWorktree + SCRUM-388 primary-tree FF). close.py only handles flat-checkout cleanup; worktree runs follow the prose in `workflow-full-auto.md`.
- **Test analysis and implementation** (steps 4-5 of FULL_AUTO). The agentic part of the loop.
- **PR body + completion-comment content.** `review.py` (below) accepts these via `--body-file` / `--completion-comment-file`; the agent composes the prose, the script passes it through.

---

## Companion scripts: start.py, review.py, poll.py (SCRUM-541)

Three sibling scripts cover the rest of the `implement SCRUM-XX FULL_AUTO` workflow. All four scripts share a uniform contract:

- Structured JSON output (a dataclass dumped to stdout by `main()`).
- `actions_taken: list[str]` narrating each step.
- Final entry of `actions_taken` is always a SCRUM-536 summary line — `<script>.py succeeded: N actions, no aborts` / `<script>.py aborted: <reason>` / `<script>.py dry-run: N actions previewed`. Single grep target for chat surface + log scrapers.
- Enum-string fields (e.g. `lint_status`, `state_file_status`, `terminal_state`) replace ambiguous booleans.
- `--dry-run` previews without mutations.
- Auth shared via `lib/auth.py` — `.env.local` auto-load at module import (SCRUM-533).

### start.py — pre-implementation orchestration (SCRUM-542)

```sh
python -m scripts.full_auto.start <TICKET> [--dry-run]
```

Steps: REST `GET /issue/<KEY>` → ADF → Markdown (via `lib/adf.py`) → `scripts/jira_ticket_lint.py` subprocess → auto-fix section-patch loop on agent-authored exit-2 (single retry) → resolve+apply "In Progress" transition → `git fetch origin --prune` + idempotent checkout of `feat/<TICKET>` from `origin/main`.

`StartResult` fields: `summary`, `issue_type`, `labels`, `description_md`, `lint_status` (enum: `pass` / `patched_then_pass` / `halted_gaps` / `halted_unfixable`), `branch_name`, `jira_transitioned`.

Halt conditions: lint exit 1 (unfixable structural), lint exit 2 without `agent-authored` label (human-authored gap), lint exit 2 after the single patch retry. The script never auto-patches human prose.

### review.py — PR creation + completion comment + In Review transition (SCRUM-543)

```sh
python -m scripts.full_auto.review <TICKET> \
    --title "<pr title>" \
    --body-file <path-to-pr-body.md> \
    --completion-comment-file <path-to-jira-comment.md> \
    [--dry-run]
```

Steps: branch-mismatch guard (current branch must start with `feat/<TICKET>` — tolerates worktree-style suffixes like `feat/<TICKET>-worktree`) → REST `POST /pulls` → `scripts/jira_ticket_lint.py --issue-type PR` subprocess → auto-fix loop on agent-authored exit-2 with REST `PATCH /pulls/<N>` (single retry) → REST `add_comment` to Jira → resolve+apply "In Review" transition.

`ReviewResult` fields: `pr_number`, `pr_url`, `pr_body_lint_status` (same enum as start.py), `comment_id`, `jira_transitioned`.

Content vs. mechanism split: the agent owns the **content** of the PR body and the completion comment (composed in chat history). The script passes the files through verbatim; Atlassian converts Markdown → ADF on receipt.

### poll.py — silent gate + mergeable_state polling (SCRUM-544)

```sh
python -m scripts.full_auto.poll --pr <N> [--interval 30] [--budget 2400] [--verbose]
```

Silent by default — no stdout until terminal. `--verbose` adds per-tick stderr lines for diagnostics.

Steps per tick: REST `GET /pulls/<N>` → REST `GET /commits/<head_ref>/check-runs` (queries against `pr.head_ref`, NOT `merge_commit_sha` — SCRUM-547 fixed an earlier bug where the synthetic test-merge SHA was used) → find the `TalkBack PR Gate` check → classify.

Terminal states: `pass` (gate `success` + mergeable `clean`, exit 0), `warn` (gate `action_required`, exit 2), `block` (gate `failure`, exit 2), `mergeable_blocked` (gate success but mergeable not clean, exit 2), `timeout` (budget exhausted, exit 2), `error` (API failure, exit 1).

`--interval` clamped [10, 300]; `--budget` clamped [60, 3600] to prevent both rate-limit foot-guns and infinite loops.

`PollResult` fields: `terminal_state` (enum), `gate_conclusion`, `mergeable_state`, `elapsed_seconds`, `ticks`.

---

## Ad-hoc scripts: comment.py, children.py, get_issue.py (SCRUM-549)

The four scripts above cover the inside-`implement-SCRUM-XX-FULL_AUTO` flow. SCRUM-549 added three more covering the **ad-hoc** Jira operations the agent does outside that flow — halt comments, JQL queries during epic-run drains, single-ticket investigation reads. Same contract as the four-script suite (structured JSON, SCRUM-536 summary line, enum-string fields, `--dry-run` where mutating).

### comment.py — non-completion-comment posting (SCRUM-550)

```sh
python -m scripts.full_auto.comment <TICKET> --body-file <path> [--dry-run]
```

Posts a single Jira comment via REST. The agent owns the body content (read from `--body-file`); the script passes it through verbatim. Atlassian converts Markdown → ADF on receipt.

`CommentResult` fields: `comment_id`, `body_chars`. Empty / whitespace-only body aborts cleanly with `comment.py aborted: comment body is empty or whitespace-only`.

Used for epic halt comments, epic-summary Finish comments, manual audit notes, ad-hoc one-offs. The `jira_add_comment` MCP response — full ADF body + author + updateAuthor objects each with 8 avatar URLs — was the worst single echo in the workflow; this drops it from ~2k tokens to ~200.

### children.py — lean-JSON wrapper for jira_search_issues (SCRUM-551)

```sh
# Preset: non-Done children of an epic (the shape epic-run drains use)
python -m scripts.full_auto.children --epic SCRUM-XXX [--include-done]

# Generic JQL passthrough (with soft SCRUM-guard)
python -m scripts.full_auto.children --jql "..." [--max-results 50]
```

Wraps `lib/jira.py::HttpJiraAPI.search_issues` (new helper added at SCRUM-551, via `POST /rest/api/3/search/jql`). Projects each result to `{key, summary, status, issuetype, priority, labels}` strings — drops the per-issue iconUrls, statusCategory objects, avatar URLs that the MCP includes.

`ChildrenResult` fields: `jql_used`, `count`, `children` (list of lean dicts). `--epic` and `--jql` are mutually exclusive; both missing or both present aborts.

`--jql` soft guard: the string must reference `project = SCRUM` or `parent = SCRUM-N`. Sophisticated bypasses (`OR project = OTHER` etc.) aren't blocked; catches the accidental footgun.

### get_issue.py — standalone CLI for ad-hoc Jira reads (SCRUM-552)

```sh
python -m scripts.full_auto.get_issue <TICKET> [--description] [--field <name>]
```

Thinnest of the three — reuses `lib/jira.py::HttpJiraAPI.get_issue` (added in SCRUM-542 for start.py) and the ADF→MD converter at `lib/adf.adf_to_md`. Default output drops the description (large + ADF-shaped); `--description` adds the Markdown-converted body. `--field <name>` restricts output to a single named field; repeatable.

`GetIssueResult` fields: `summary`, `issuetype`, `status`, `labels`, `description_md` (only with `--description`). A 404 / API error classifies as `aborted_reason` with the underlying error text; the script exits non-zero.

Used for "what's the status of SCRUM-XXX", "what's the parent epic", "check the description before filing a follow-up" — investigation reads that don't enter the implement flow.

### Combined token savings vs the pre-extraction MCP flow

| Script | What it replaces | Approx tokens saved/use |
|---|---|---|
| `close.py` (SCRUM-529) | merge + lib calls + add_comment + transition Done | ~5,000/ticket |
| `start.py` (SCRUM-542) | get_issue + get_transitions + transition In Progress + branch | ~3,000/ticket |
| `review.py` (SCRUM-543) | create_pull_request + lint orchestration + add_comment + transition In Review | ~4,000/ticket |
| `poll.py` (SCRUM-544) | per-tick `gh pr checks` output during the polling loop | ~2,000/ticket |
| **Implement-flow total** | | **~14,000/ticket** |
| `comment.py` (SCRUM-550) | `jira_add_comment` for halt / epic-summary / audit comments | ~2,000/call |
| `children.py` (SCRUM-551) | `jira_search_issues` for epic-drain JQL queries | ~1,000/call |
| `get_issue.py` (SCRUM-552) | ad-hoc `jira_get_issue` investigation reads | ~1,000/call |
| **Ad-hoc per-session total** | | **~21-29k/session** |

Measured against this session's chat history (see SCRUM-541 + SCRUM-549 closure comments for the per-call breakdown).

## End-to-end flow — `implement SCRUM-XX FULL_AUTO`

Visual map of what runs where (local vs remote) and where the agent's irreducible work lands between scripts. The five phases below correspond to the five script + agent steps that make up the workflow.

```
LOCAL  (user's machine)                            │   REMOTE
═══════════════════════════════════════════════════╪═════════════════════════════════════════
                                                   │
  Claude Code (chat) — orchestrates                │
        │                                          │
        │  "implement SCRUM-XX FULL_AUTO"          │
        ▼                                          │
  ╔═══════════════════════════════════════════════╗│
  ║ ① start.py (Python)                          ║│
  ║   • fetch ticket ─────────────────────────────╫┼──▶ Atlassian REST  (GET /issue)
  ║   • subprocess: jira_ticket_lint.py          ║│
  ║   • PUT /issue if auto-fix patch fires ──────╫┼──▶ Atlassian REST
  ║   • In Progress transition ───────────────────╫┼──▶ Atlassian REST  (POST /transitions)
  ║   • git fetch origin --prune ─────────────────╫┼──▶ GitHub git-over-https
  ║   • git checkout -b feat/<K> origin/main      ║│       ◀── branch CREATED locally
  ╚════════════╤══════════════════════════════════╝│
               │                                   │
  ╔════════════▼══════════════════════════════════╗│
  ║ ② Claude implementation phase                ║│   (irreducibly agent work — pure
  ║   • Read codebase (Read, Grep, Bash find)    ║│    local except the final push)
  ║   • Edit/Write source files                   ║│
  ║   • Compose PR body + completion comment      ║│
  ║     to /tmp/pr-N-*.md                         ║│
  ║   • Run tests locally (Bash):                 ║│
  ║       pytest / vitest / go test / go vet      ║│
  ║   • git add + commit (Bash, local)           ║│
  ║   • git push -u origin feat/<K> ──────────────╫┼──▶ GitHub git-over-https
  ║                                               ║│       ◀── branch PUBLISHED to origin
  ╚════════════╤══════════════════════════════════╝│
               │                                   │
  ╔════════════▼══════════════════════════════════╗│
  ║ ③ review.py (Python)                         ║│
  ║   • branch-mismatch guard (local)            ║│
  ║   • Create PR ────────────────────────────────╫┼──▶ GitHub REST  (POST /pulls)
  ║   • subprocess: jira_ticket_lint.py --PR     ║│
  ║   • PATCH PR body if auto-fix fires ─────────╫┼──▶ GitHub REST  (PATCH /pulls/N)
  ║   • Post completion comment ──────────────────╫┼──▶ Atlassian REST  (POST /comment)
  ║   • In Review transition ─────────────────────╫┼──▶ Atlassian REST  (POST /transitions)
  ╚════════════╤══════════════════════════════════╝│        │
               │                                   │        │ triggers (push + label events)
               │                                   │        ▼
               │                                   │  ┌──────────────────────────────┐
               │                                   │  │ GitHub Actions               │
               │                                   │  │ release-readiness.yml         │
               │                                   │  │   • PR Risk (pr_risk_run.py) │
               │                                   │  │   • Release Readiness        │
               │                                   │  │   • Combine                  │
               │                                   │  │     → TalkBack PR Gate check │
               │                                   │  └──────────────────────────────┘
               │                                   │              ▲
  ╔════════════▼══════════════════════════════════╗│              │ observes
  ║ ④ poll.py (Python)                           ║│              │
  ║   • Silent 30s loop, 40-min budget           ║│              │
  ║   • GET /pulls/N + check-runs ─────────────────╫┼─────────────┘
  ║   • Classify pass/warn/block/mergeable_      ║│
  ║     blocked (2-tick confirm) /timeout/error   ║│
  ║   • Exits when terminal                       ║│
  ╚════════════╤══════════════════════════════════╝│
               │                                   │
  ╔════════════▼══════════════════════════════════╗│
  ║ ⑤ close.py (Python)                          ║│
  ║   • Pre-merge GET /pulls/N ───────────────────╫┼──▶ GitHub REST
  ║   • PUT /pulls/N/merge (squash) ──────────────╫┼──▶ GitHub REST
  ║                                               ║│       ◀── if repo "auto-delete head"
  ║                                               ║│           on: remote branch DELETED
  ║   • auto-stash lint-runs.log (SCRUM-534)      ║│
  ║   • git fetch + checkout main + ──────────────╫┼──▶ GitHub git-over-https
  ║     git pull --ff-only                        ║│
  ║   • git branch -D feat/<K>                    ║│       ◀── local branch DELETED
  ║   • State-file update (.epic-run/<E>.json)    ║│
  ║   • Jira Done transition ─────────────────────╫┼──▶ Atlassian REST
  ║   • Post closure comment ─────────────────────╫┼──▶ Atlassian REST
  ╚════════════╤══════════════════════════════════╝│
               ▼                                   │
       "merged at <sha>"                           │
                                                   │
  ┌─────────────────────────────────────────────┐  │
  │ Shared lib/ (no network — pure Python):     │  │
  │   auth.py     credentials + .env.local       │  │
  │   adf.py      ADF → Markdown                 │  │
  │   state.py    .epic-run/<E>.json read/write  │  │
  │   templates.py closure-comment rendering     │  │
  │   git_ops.py  subprocess wrappers + stash    │  │
  │   jira.py     HttpJiraAPI (REST client)      │  │
  │   github.py   HttpGitHubAPI (REST client)    │  │
  └─────────────────────────────────────────────┘  │
                                                   │
═══════════════════════════════════════════════════╪═════════════════════════════════════════
```

### Branch lifecycle for `feat/SCRUM-XX`

| Step | Where | When | How |
|---|---|---|---|
| **CREATE (local)** | Local working tree | Phase ① (start.py) | `git checkout -b feat/<K> origin/main` |
| **PUBLISH (origin)** | GitHub remote | Phase ② (agent) | Agent runs `git push -u origin feat/<K>` via Bash |
| **DELETE (remote)** | GitHub remote | After PR squash-merge | GitHub auto-deletes IF the repo's "Automatically delete head branches" setting is on. `psuthar/talkback` has it enabled. |
| **DELETE (local)** | Local working tree | Phase ⑤ (close.py) | `git branch -D feat/<K>` via `lib/git_ops.delete_branch` |

### Local vs remote at a glance

| Component | Where | Notes |
|---|---|---|
| Claude Code agent (chat) | Local | Composes prompts, code, prose, decisions |
| `scripts/full_auto/*.py` (6 scripts + lib/) | Local | Python invoked via `python -m scripts.full_auto.<name>` |
| `scripts/jira_ticket_lint.py` | Local | Subprocess from start.py + review.py |
| Local git working tree | Local | Read+write by start.py, agent, close.py |
| `git fetch` / `push` / `checkout` / `merge` | Local + git-over-https to remote | git protocol, distinct from REST |
| Atlassian Jira REST | Remote | `suthar-team.atlassian.net` |
| GitHub REST API | Remote | `api.github.com` |
| `release-readiness.yml` workflow | Remote | GitHub Actions |
| TalkBack PR Gate check | Remote | Produced by `release-readiness-combine` step |

### What the agent does that no script does

Phase ② (between start.py and review.py) is irreducibly the agent's work:

- **Understanding scope** from the lint-clean description (start.py returns `description_md` so the agent doesn't re-fetch).
- **Code edits** via Read/Edit/Write tools.
- **Test runs** via Bash (`pytest`, `vitest`, `go test`, `go vet`).
- **Iteration** on test failures until clean.
- **Composing the PR body and completion comment** — these aren't auto-generated; the agent writes them based on the actual diff and context. The scripts only pass them through.
- **Committing + pushing** via Bash. The agent decides commit boundaries; no script makes commits on the agent's behalf.

## What's coming next (forward links)

- **SCRUM-530 / Phase 1** ✓ done — `scripts/full_auto/close.py` shipped.
- **SCRUM-531 / Phase 2** ✓ done — dry-run validation against 5 historical FULL_AUTOs; zero unresolved drift; see `ops/full-auto-validation/SUMMARY.md`.
- **SCRUM-532 / Phase 3** — this section. CLAUDE.md / workflow docs updated.
- **SCRUM-541 Epic** ✓ done — start.py / review.py / poll.py shipped (SCRUM-542 / 543 / 544).
- **SCRUM-549 Epic** ✓ done — comment.py / children.py / get_issue.py shipped (SCRUM-550 / 551 / 552).
- **(Future, separate Epic)** — Cloudflare-tunnel webhook listener that imports `close.py` for the post-gate close-out path. De-risked by Phase 0-3.

---

## Troubleshooting

**`RuntimeError: No GitHub credentials available.`**

The script tried `GITHUB_TOKEN` env var (unset) and `gh auth token` (failed). Run `gh auth status` to diagnose. Re-authenticate with `gh auth login` if needed.

**`RuntimeError: Missing Jira credentials.`**

Either `ATLASSIAN_EMAIL` or `ATLASSIAN_API_TOKEN` is unset or empty. The auth module auto-loads `.env.local` from the repo root at import time (SCRUM-533); check the file exists, is at the repo root (next to `.git`), and that the keys are spelled exactly. If using `direnv`, run `direnv reload`. Note that any value already set in your shell takes precedence over `.env.local`, so a stale shell export can mask a corrected file.

**Token works locally but not in CI.**

Repo secrets are scoped per environment. Verify the secret name matches (`ATLASSIAN_API_TOKEN`, exact case) and is granted to the workflow's environment.

---

## Phase 0 scope guarantee

This ticket lands:
- `scripts/full_auto/__init__.py`
- `scripts/full_auto/lib/__init__.py`
- `scripts/full_auto/lib/auth.py` (~80 LOC)
- `scripts/test_full_auto_auth.py` (~150 LOC, 12 tests)
- `.env.local.example` (committed template)
- This runbook (`docs/agent/full-auto-scripts.md`)

**Zero behaviour change.** Claude continues to do close-out via MCPs + bash tool calls. The auth module is foundation only; nothing imports it yet.

That changes in Phase 1.
