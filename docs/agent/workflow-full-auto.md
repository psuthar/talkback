# FULL_AUTO Post-PR Automation

Source of truth: This file owns FULL_AUTO merge-gate polling, merge rules, cleanup, and Jira Done transition requirements.

## Core Rule

Use GitHub MCP `pull_request_read` (`method: get`) and `mergeable_state` as the merge gate authority. Do not use legacy combined status as a parallel source of truth for mergeability.

## Hard Stop Conditions

Do not proceed to merge/Jira Done unless both are true:

- `TalkBack PR Gate` check run has `conclusion: success`
- `mergeable_state` is `clean`

If either condition fails after polling budget, stop FULL_AUTO: PR remains open, Jira remains In Review.

## Polling Policy (Mandatory)

- Poll every 30 seconds on one shared 40-minute budget.
- Continue polling for: `null`, `unknown`, `unstable`, `behind`, and `blocked`.
- `blocked` is not an immediate stop; continue polling.
- Stop immediately for:
  - field absent (`mergeable_state` missing): FULL_AUTO unavailable
  - terminal `dirty`
  - budget expiration without reaching `clean`

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
- Local cleanup:

```
git checkout main
git fetch --prune origin
git pull --ff-only origin main
git branch -D feat/<ticket-number>
```

- Transition Jira ticket to Done.

## Git Push Authentication Note

If HTTPS push fails non-interactively (`could not read Username ... Device not configured`):

- Configure `gh auth login` + `gh auth setup-git`, or use SSH remote.
- If needed, push from integrated terminal, then continue PR workflow through GitHub MCP.

