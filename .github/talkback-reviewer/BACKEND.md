# TalkBack Reviewer — Backend Choice (Budget Store)

Owner: SCRUM-515 (Phase 1c of Epic SCRUM-512).

## Decision

The reviewer's daily token-budget counter (SCRUM-511) lives as a JSON blob on a dedicated tracking branch `reviewer-state`, read and updated via the GitHub Contents API's SHA-based optimistic locking.

```
reviewer-state branch
└── state.json
    {
      "reviewer-budget:2026-05-20": { "value": 12345, "expires_at": 1716326400 },
      "reviewer-budget:2026-05-19": { "value": 67890, "expires_at": 1716240000 }
    }
```

Each daily key carries a `value` (tokens consumed so far) and `expires_at` (Unix-seconds; 48h after creation per `REVIEWER_BUDGET_TTL_HOURS`).

## Why this and not other options

| Option | Atomicity | New infra | Cost | Verdict |
|---|---|---|---|---|
| **Tracking branch + Contents API CAS** (chosen) | Yes — SHA-CAS via PUT `If-Match`-equivalent | None | Free | Wins on infra cost and simplicity |
| GitHub repo variable | **No** — concurrent setters race | None | Free | Unsafe for Phase 2 volume |
| Render KV (Redis-compatible) | Yes — atomic primitives | Yes — provision + manage | Modest | Defer until Phase 2 volume justifies |
| Postgres single-row counter | Yes — transactions | None (we already have Postgres) | Free | Adds CI→prod-DB security surface; defer |
| SQLite-on-runner-FS | No — runner is ephemeral | None | Free | **Resets every run** — cap bypassable |

The Phase 1 calibration period is manual-invoke only — volume is sparse. The tracking-branch approach is well within capacity. If Phase 2 auto-trigger pushes volume past ~50 writes/day, revisit the choice (the `BudgetStore` Protocol means swapping is a single-file change).

## Bootstrap (one-time, maintainer)

These steps must be done by a repo admin before the workflow can succeed:

1. **Create the tracking branch.**
   ```sh
   git checkout --orphan reviewer-state
   git rm -rf .
   printf '{}\n' > state.json
   cat > README.md <<'EOF'
   # reviewer-state branch (do not edit by hand)
   This branch is owned by `.github/workflows/talkback-reviewer.yml`.
   It holds `state.json`, the daily token-budget counter for the
   talkback-reviewer agent (SCRUM-511 / SCRUM-515).
   EOF
   git add state.json README.md
   git commit -m "Initialise reviewer-state branch (SCRUM-515)"
   git push origin reviewer-state
   ```
2. **Protect the branch.** Repo Settings → Branches → Add rule for `reviewer-state`:
   - Require pull request reviews before merging: **off** (the workflow needs unrestricted write).
   - Restrict pushes that create matching branches: **on**.
   - Allowed actors: just `github-actions[bot]` (the workflow identity).
   This prevents accidental human pushes from corrupting the counter.
3. **Wire the secret.** Repo Settings → Secrets and variables → Actions → New repository secret:
   - Name: `ANTHROPIC_API_KEY`
   - Value: a key with `messages:write` permission.
4. **(Optional) override the defaults.** Repository variables:
   - `REVIEWER_MODEL` — defaults to `claude-sonnet-4-6`.
   - `REVIEWER_DAILY_TOKEN_CAP` — defaults to `500000`.
   - `REVIEWER_MAX_INPUT_TOKENS` — defaults to `100000` (caps the diff size).

## Smoke test (after bootstrap)

On any open PR, comment `/talkback-review`. The workflow should:

1. Post an acknowledgement comment within ~10 seconds.
2. Within ~60 seconds, either post a `## Findings` review or stay silent (if the model emitted the refusal token).
3. Push a new commit to `reviewer-state` updating today's counter.

If the acknowledgement appears but the review never lands, check the workflow logs — the most common failure is a stale SCOPE.md pin (Phase 1a guarantee) or a missing `ANTHROPIC_API_KEY` secret.

## Manual invoke deliberately bypasses the skip filter

SCOPE.md's "Escalation path" specifies that `/talkback-review` always runs, regardless of the skip filter (SCRUM-510). This is so the filter can be aggressive (Phase 2) without permanently locking the reviewer out of edge cases. If a maintainer wants the reviewer on a small/docs-only/draft PR, the slash command provides that escape hatch.

## When to revisit this choice

- Phase 2 auto-trigger volume produces CAS-conflict noise (visible as 409 retries in the workflow logs).
- The team wants a real metrics surface for budget consumption — at which point Postgres + Grafana wins.
- A future "shared budget across multiple repos" requirement appears — at which point Render KV wins.

Until one of those triggers, the tracking-branch approach is the right level of complexity.
