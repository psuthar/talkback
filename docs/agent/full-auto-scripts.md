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

### Combined token savings vs the pre-extraction MCP flow

| Script | What it replaces | Approx tokens saved/ticket |
|---|---|---|
| `close.py` (SCRUM-529) | merge + lib calls + add_comment + transition Done | ~5,000 |
| `start.py` | get_issue + get_transitions + transition In Progress + branch | ~3,000 |
| `review.py` | create_pull_request + lint orchestration + add_comment + transition In Review | ~4,000 |
| `poll.py` | per-tick `gh pr checks` output during the polling loop | ~2,000 |
| **Total** | | **~14,000** |

Measured against this session's chat history (see SCRUM-541 closure comment for the full breakdown).

## What's coming next (forward links)

- **SCRUM-530 / Phase 1** ✓ done — `scripts/full_auto/close.py` shipped.
- **SCRUM-531 / Phase 2** ✓ done — dry-run validation against 5 historical FULL_AUTOs; zero unresolved drift; see `ops/full-auto-validation/SUMMARY.md`.
- **SCRUM-532 / Phase 3** — this section. CLAUDE.md / workflow docs updated.
- **SCRUM-541 Epic** ✓ done — start.py / review.py / poll.py shipped (SCRUM-542 / 543 / 544).
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
