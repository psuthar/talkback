# FULL_AUTO Post-PR Automation

Source of truth: This file owns FULL_AUTO merge-gate polling, merge rules, cleanup, and Jira Done transition requirements.

> **In progress (SCRUM-381):** push-based gate handling via a Claude routine is being built out as an alternative to the polling cadence described below. See [`pr-gate-webhook.md`](pr-gate-webhook.md). Until that epic's cutover ticket (SCRUM-386) lands, the polling rules in this file remain authoritative.

## Core Rule

Use GitHub MCP `pull_request_read`: **`get_check_runs`** for **TalkBack PR Gate** (PASS = `conclusion: success`) and **`method: get`** for **`mergeable_state`**. Both are required for merge; see **Stop polling when the gate is not PASS** below. Do not use legacy combined status as a parallel source of truth for mergeability.

## Hard Stop Conditions

Do not proceed to merge/Jira Done unless both are true:

- `TalkBack PR Gate` check run has `conclusion: success` (this is **PASS** in the unified gate summary)
- `mergeable_state` is `clean`

If either merge condition fails, stop FULL_AUTO: PR remains open, Jira remains In Review. If the gate completes **non-PASS**, stop immediately (do not wait for the polling budget). If the gate is **PASS** but `mergeable_state` never becomes `clean`, stop when the polling budget expires.

### TalkBack PR Gate vs gate summary (PASS / WARN)

GitHub Checks use `conclusion`, not the PR comment table. In this repo, unified gate **PASS** maps to check `conclusion: success`. **WARN** maps to `conclusion: action_required` (human review / attention needed); that is **not** PASS. See `scripts/pr_gate_check_payload.py`.

## Polling Policy (Mandatory)

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

Before `merge_pull_request`:

1. Confirm TalkBack PR Gate success via `pull_request_read` with `get_check_runs`.
2. Immediately re-read PR with `pull_request_read` (`method: get`).
3. Merge only if `mergeable_state` is still `clean`.

Never merge based on stale earlier reads.

## Merge, Cleanup, and Done Transition

On confirmed gate pass:

- Call `merge_pull_request` with `merge_method: squash`.
- Remote branch: rely on auto-delete if configured; otherwise delete manually in GitHub UI.
- Local cleanup — choose the path that matches how implementation actually happened:

  **If implementation ran in the main checkout** (no worktree was created for this ticket):

  ```
  git checkout main
  git fetch --prune origin
  git pull --ff-only origin main
  git branch -D feat/<ticket-number>
  ```

  **If implementation ran in a git worktree** — for example, when the user explicitly asked for a worktree, when CLAUDE.md / project memory directed one, or when EnterWorktree was used (visible as a `.worktrees/<ticket-number>` entry in `git worktree list`) — run these steps in order:

  1. ExitWorktree (action: `keep` or `remove`) to return the session to the main checkout. Never run worktree-removal commands while the session is still inside the worktree.

  2. **Fast-forward the primary tree's `main` — safety-gated, mandatory for worktree runs.** The agent operated in a worktree, so the user's primary checkout is now stale relative to the merged state. Bring it up to date so no manual `git pull` is needed after close-out. All three conditions must hold; on any failure, **skip the FF and surface a notice** in the closure comment — do not force, rebase, or stash.

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

- Before transitioning Jira to Done, verify the ticket already has the structured implementation comment required by `docs/agent/workflow-jira.md`.
  - If missing, post that comment first and only then continue.
- Transition Jira ticket to Done.
- Post a final closure Jira comment confirming FULL_AUTO completion with:
  1. merged PR URL,
  2. merge/landing commit SHA on `main`,
  3. local/remote branch cleanup result,
  4. primary-tree FF outcome — one of: "FF'd to `<sha>`", "skipped — primary on `<branch>`", "skipped — primary has WIP on `main`", "skipped — `--ff-only` refused (divergence)". Omit this item only if implementation ran in the main checkout (no worktree was used).
  5. any residual risk or follow-up note.

## Git Push Authentication Note

If HTTPS push fails non-interactively (`could not read Username ... Device not configured`):

- Configure `gh auth login` + `gh auth setup-git`, or use SSH remote.
- If needed, push from integrated terminal, then continue PR workflow through GitHub MCP.

