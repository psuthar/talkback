# FULL_AUTO Post-PR Automation

Source of truth: This file owns FULL_AUTO merge-gate handling, merge rules, cleanup, and Jira Done transition requirements for `psuthar/talkback`.

## Two invocation keywords (SCRUM-392)

| Keyword | Default? | Path | Quota cost |
|---|---|---|---|
| `implement SCRUM-XX FULL_AUTO` | **Yes** | Agent polls gate + `mergeable_state` every 30s, merges via `merge_pull_request`, transitions Jira to Done itself. | None — runs in your local Claude Code session. |
| `implement SCRUM-XX FULL_AUTO_WEBHOOK` | No (opt-in) | A deployed Claude routine subscribes to `pull_request.labeled` + `pull_request.closed`, merges in the cloud, transitions Jira. Requires the `<!-- full-auto-webhook -->` marker in the PR body (SCRUM-394) — without it `release-readiness.yml` does not apply the `pr-gate:*` label, so the routine never fires. See [`pr-gate-webhook.md`](pr-gate-webhook.md). | Each event consumes 1 routine run from your claude.ai daily quota (~15/day on the default plan). |

The two paths produce the **same PR / Jira outputs** (PR Gate comment, Jira completion comment, Done transition) — the only differences are who executes the merge and whether the close-out runs in the cloud or in your local session.

This file documents the **default polling path** first. The webhook path is documented in [`pr-gate-webhook.md`](pr-gate-webhook.md).

## Core Rule (default — polling path)

Use GitHub MCP `pull_request_read`: **`get_check_runs`** for **TalkBack PR Gate** (PASS = `conclusion: success`) and **`method: get`** for **`mergeable_state`**. Both are required for merge; see **Stop polling when the gate is not PASS** below. Do not use legacy combined status as a parallel source of truth for mergeability.

## Hard Stop Conditions

Do not proceed to merge/Jira Done unless both are true:

- `TalkBack PR Gate` check run has `conclusion: success` (this is **PASS** in the unified gate summary)
- `mergeable_state` is `clean`

If either merge condition fails, stop FULL_AUTO: PR remains open, Jira remains In Review. If the gate completes **non-PASS**, stop immediately (do not wait for the polling budget). If the gate is **PASS** but `mergeable_state` never becomes `clean`, stop when the polling budget expires.

### TalkBack PR Gate vs gate summary (PASS / WARN)

GitHub Checks use `conclusion`, not the PR comment table. In this repo, unified gate **PASS** maps to check `conclusion: success`. **WARN** maps to `conclusion: action_required` (human review / attention needed); that is **not** PASS. See `scripts/pr_gate_check_payload.py`.

## Polling Policy

Each poll cycle must read **both** check runs (for TalkBack PR Gate) and PR details (for `mergeable_state`). Order: use `pull_request_read` with `get_check_runs` first, then `method: get` for mergeability.

### Stop polling when the gate is not PASS

If the **TalkBack PR Gate** check run exists and `status` is **`completed`** with **`conclusion` other than `success`**, **stop FULL_AUTO polling immediately** — do not continue until the 40-minute budget expires. Continued polling does not help: a human must act (e.g. approve, fix BLOCK, or accept WARN risk). Leave the PR open and Jira **In Review**.

While the gate check is **missing**, **`queued`**, or **`in_progress`**, keep polling (same 30s interval, shared budget) until the gate completes or timeout.

### Mergeability after gate PASS

Only after the gate shows **`completed`** + **`conclusion: success`** does mergeability polling matter for merge:

- Poll every 30 seconds on one shared 40-minute budget.
- Continue polling for: `null`, `unknown`, `unstable`, `behind`, and `blocked`.
- `blocked` is not an immediate stop *while the gate outcome is still unknown or still PASS*; continue polling.
- Stop immediately for:
  - **TalkBack PR Gate completed with non-`success` conclusion** (see above)
  - field absent (`mergeable_state` missing): FULL_AUTO unavailable
  - terminal `dirty`
  - budget expiration without reaching `clean` (only applies while gate remains PASS)

Merge-state table:

| `mergeable_state` | Action |
|---|---|
| field absent | FULL_AUTO unavailable; hard stop |
| `null` | continue polling every 30s |
| `unknown` / `unstable` / `behind` | continue polling every 30s |
| `blocked` | continue polling every 30s until `clean`, `dirty`, or timeout |
| `clean` | continue only after confirming TalkBack PR Gate success |
| `dirty` | stop; merge conflicts |

## Pre-merge Guard (Mandatory)

Before calling `merge_pull_request`:

1. Confirm TalkBack PR Gate success via `pull_request_read` with `get_check_runs`.
2. Immediately re-read PR with `pull_request_read` (`method: get`).
3. Merge only if `mergeable_state` is still `clean`.

Never merge based on stale earlier reads. A `clean` read from minutes earlier is not sufficient — the immediate pre-merge read is required every time.

## WARN / BLOCK handling (polling path)

When the gate completes with `conclusion: action_required` (WARN) or `failure` (BLOCK):

1. **Stop polling immediately.** Do not wait for the budget to expire.
2. Post a Jira halt comment on the linked ticket summarizing: gate signals from the workflow artifacts (PR Risk band/score, top risk factors, Release Readiness status, mergeable_state), resume options (manual squash-merge to override; push fixes to address signals; cancel).
3. Leave the PR **open** and Jira in **In Review**.
4. End the session. The human decides whether to squash-merge manually (override) or push fixes.

**Resume after fixes:** on the next `implement SCRUM-XX FULL_AUTO` / `continue` / `finish` in the same branch, restart the poll loop from scratch — re-read both gate and `mergeable_state`.

**Resume after manual squash-merge** (WARN override): on the next `finish SCRUM-XXX`, the agent reads the PR state, sees `MERGED`, and runs the close-out below (Jira Done transition + local cleanup + closure comment). Under the **default polling path**, Jira Done is transitioned by the agent on the next session — there is no cloud automation watching for the close event. (The webhook path's SCRUM-391 CLOSE FLOW handles this in the cloud; you opt in via `FULL_AUTO_WEBHOOK` when you want it.)

## Merge, Cleanup, and Done Transition

After the gate reaches PASS, the post-merge close-out (merge + local cleanup + Jira → Done + closure comment) is owned by **`scripts/full_auto/close.py`** (Epic SCRUM-529). Claude invokes it instead of running each individual MCP / Bash call. See `docs/agent/full-auto-scripts.md` for the runbook + auth setup. Atlassian credentials are auto-loaded from `.env.local` at the repo root at module import time (SCRUM-533) — no shell sourcing required before invocation.

### Common case — no worktree (default for `implement SCRUM-XX FULL_AUTO`)

If implementation ran in the main checkout, run:

```sh
python -m scripts.full_auto.close <TICKET> --pr <N> --path polling
```

`close.py` performs, in order:
1. **Pre-merge guard** — re-reads the PR via the GitHub API; aborts if `mergeable_state` is not `clean` (and `--path polling` was passed for an open PR).
2. **Merge** — calls `PUT /pulls/<N>/merge` with `merge_method: squash`. Skipped automatically if the PR is already merged at entry (manual-override case).
3. **Local git** — `fetch + checkout main + pull --ff-only + git branch -D feat/<TICKET>`. If `ops/define-kpis/lint-runs.log` is the *only* dirty tracked file (the SCRUM-504 PR-body lint appends a row post-feature-commit), it is auto-stashed before the checkout and popped back after the SHA capture (SCRUM-534), so the audit row survives on main for the next PR to absorb. Any *other* dirty tracked file falls through to the original `git checkout` failure — manual recovery is still on the user.
4. **State file** — if `.epic-run/<EPIC>.json` references the ticket, updates the entry to `status: done` with the merge SHA + `final_gate`. `CloseResult.state_file_status` (SCRUM-538) records the outcome as one of `updated` / `already_done` / `no_state_file` / `would_update` (the last for dry-run); standalone tickets get `no_state_file`.
5. **Jira** — transitions to Done.
6. **Closure comment** — posts the polling-path closure template (see `scripts/full_auto/lib/templates.py`) with merge SHA, new main SHA, and deleted branch name.

`close.py` returns a `CloseResult` dataclass; Claude surfaces its `actions_taken` list back to the user. The list always ends with a single grep-able summary line — `close.py succeeded: N actions, no aborts` / `close.py aborted: <reason>` / `close.py dry-run: N actions previewed` (SCRUM-536) — so chat surface / log scraping can classify runs without parsing the whole list. On any per-step failure (auth, network, transition refused), close.py exits non-zero and Claude reports the error.

### User-override case — manual squash-merge before close.py

When the user has already squash-merged the PR (typically after a WARN halt that they accepted), Claude runs:

```sh
python -m scripts.full_auto.close <TICKET> --pr <N> --path manual-override
```

`close.py` detects the already-merged state from the GitHub API response, skips the merge call, and uses the manual-override closure template (which records that the agent did not invoke `merge_pull_request`).

### Worktree case — preserves the original prose

If implementation ran in a git worktree (visible as a `.worktrees/<ticket-number>` entry in `git worktree list`), `close.py`'s `lib/git_ops.py` does **not** handle ExitWorktree + the SCRUM-388 primary-tree FF logic. Use the worktree-specific steps below instead. (A future enhancement could extend `close.py` to detect and handle the worktree path; out of scope for SCRUM-529.)

  **If implementation ran in a git worktree** — for example, when the user explicitly asked for a worktree, when CLAUDE.md / project memory directed one, or when EnterWorktree was used (visible as a `.worktrees/<ticket-number>` entry in `git worktree list`) — run these steps in order:

  1. ExitWorktree (action: `keep` or `remove`) to return the session to the main checkout. Never run worktree-removal commands while the session is still inside the worktree.

  2. **Fast-forward the primary tree's `main` — safety-gated, mandatory for worktree runs (SCRUM-388).** The agent operated in a worktree, so the user's primary checkout is now stale relative to the merged state. Bring it up to date so no manual `git pull` is needed after close-out. All three conditions must hold; on any failure, **skip the FF and surface a notice** in the closure comment — do not force, rebase, or stash.

     a. Primary tree's current branch is `main`:
        `git -C <primary> branch --show-current` returns `main`.
     b. Primary tree has no tracked-file modifications or staged changes (untracked files are fine — they're the user's own scratch state):
        `git -C <primary> status --porcelain | grep -vE '^\?\?'` returns empty.
     c. The FF itself succeeds:
        ```
        git -C <primary> fetch origin --prune
        git -C <primary> pull --ff-only origin main
        ```

     If (a) fails, the user is on an in-progress feature branch — note "primary tree on `<branch>`; skipped FF" and move on. If (b) fails, the user has WIP on `main` — note "primary tree has tracked modifications; skipped FF" and move on. If (c) fails (rare; only happens if `main` diverged for some reason), note "FF refused (divergence); skipped" and move on. Never attempt recovery; the user can pull manually with full context.

  3. `git worktree remove .worktrees/<ticket-number>` to drop the directory and its registration.

  4. `git branch -D feat/<ticket-number>` (squash-merge orphans the local commit, so `-d` will refuse — `-D` is correct).

  If `git worktree remove` refuses because of untracked files left in the worktree (test scratch, generated artifacts, downloaded fixtures), inspect them first via `cd .worktrees/<ticket-number> && git status --short`. If every untracked path is either also present (and ignored) in the main checkout or is plainly disposable scratch, re-run with `--force` and note in the closure comment which untracked paths were force-removed so any genuinely-needed file is not lost silently. Only escalate to `--force` after that inspection — never as a reflex.

### Jira → Done and the closure comment (worktree case only)

For the worktree case above, the Jira transition + closure comment are still owned by Claude's prose (not `close.py`). Before transitioning:

- Verify the ticket already has the structured implementation comment required by `docs/agent/workflow-jira.md`.
  - If missing, post that comment first and only then continue.
- **Transition Jira ticket to Done** via `mcp__atlassian__jira_transition_issue` (target status name "Done").
- Post a final closure Jira comment confirming FULL_AUTO completion with:
  1. merged PR URL,
  2. merge/landing commit SHA on `main`,
  3. local/remote branch cleanup result,
  4. primary-tree FF outcome — one of: "FF'd to `<sha>`", "skipped — primary on `<branch>`", "skipped — primary has WIP on `main`", "skipped — `--ff-only` refused (divergence)".
  5. path indicator — `"polling path (default)"`.
  6. any residual risk or follow-up note.

For the common (non-worktree) case, `close.py` handles all of the above — Claude only relays its `CloseResult` summary to the user.

## Git Push Authentication Note

If HTTPS push fails non-interactively (`could not read Username ... Device not configured`):

- Configure `gh auth login` + `gh auth setup-git`, or use SSH remote.
- If needed, push from integrated terminal, then continue PR workflow through GitHub MCP.

## Optional path: webhook routine (opt-in via `FULL_AUTO_WEBHOOK`)

**Quota note:** Each `pull_request.labeled` and `pull_request.closed` event the routine subscribes to consumes one of your daily claude.ai routine runs (~15/day on the default plan). For normal dev volume — multiple PRs per day, each with re-runs of `release-readiness` cycling labels, plus close events — this exceeds the default quota. Default to the polling path above; opt in to the webhook path only on PRs where the cost is justified.

Invoke with `implement SCRUM-XX FULL_AUTO_WEBHOOK` (note the trailing `_WEBHOOK`). The agent's behavior changes:

- **Opt-in marker (SCRUM-394) — required.** Include the literal line `<!-- full-auto-webhook -->` (an HTML comment, invisible in the rendered PR) in the PR body when creating the PR. `release-readiness.yml` only applies the `pr-gate:<status>` label — and therefore only wakes the routine — for PRs whose body carries this marker. A PR without it is handled by the polling path only and never consumes a routine run, even though `release-readiness` still publishes the `TalkBack PR Gate` check. Do **not** add the marker on a normal `FULL_AUTO` PR; do **not** write the literal marker comment anywhere in a PR body that isn't actually opting in (describe it instead) — the workflow greps the PR body for that exact string.
- After pushing the PR and Jira → In Review, **stop active work**. Do not poll. The routine fires on the gate-applied label (typically within ~60s of `release-readiness` completing).
- Optionally do **one** confirmation read after ~5–10 min. If the PR is merged, run the local cleanup (FF + worktree remove + branch -D) and post the closure comment. If still open, the routine likely halted at WARN/BLOCK (its Jira halt comment will say so) — fall back to the polling path's WARN/BLOCK handling (above).
- The routine handles the cloud-side merge, the Jira completion comment, and the Done transition. The agent's only job is the local cleanup + a brief closure comment naming `"webhook path (FULL_AUTO_WEBHOOK)"` as the path indicator.
- Manual squash-merge override of WARN/BLOCK: the routine's CLOSE FLOW (SCRUM-391) fires on the `pull_request.closed` event and auto-posts the override Jira completion + Done transition. The agent's job on the next `finish SCRUM-XXX` is just local cleanup.

Full routine prompt, payload contract, and setup instructions: [`pr-gate-webhook.md`](pr-gate-webhook.md).

**To enable the webhook path:** in claude.ai → Routines → "TalkBack PR Gate handler" → set status to **Active**, and confirm both `Pull request labeled` and `Pull request closed` event subscriptions are present (per SCRUM-391). To disable cleanly (avoid accidental quota consumption while you're using polling), set the routine to **Inactive**.
