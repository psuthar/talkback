# PR Gate Webhook → Claude Routine

Push-based handling of TalkBack PR Gate outcomes. Replaces 30-second polling
in `implement SCRUM-XXX FULL_AUTO`. Epic: SCRUM-381.

This document covers Slice 1 (SCRUM-382). It will be updated in place as later
slices land — see the **Slice status** table.

## How it fits together

```
GitHub release-readiness workflow
  │
  ├─ writes  artifacts/pr-gate-summary.json
  ├─ publishes  TalkBack PR Gate check
  └─ emits  workflow_run.completed event
                  │
                  │ (GitHub webhook fan-out via Claude GitHub App)
                  ▼
          Claude routine (subscribed to workflow_run events on this repo)
                  │
                  │ reads webhook payload, decides terminal action
                  ▼
  Slice 1 (now):  posts a "received" comment on the PR
  Slice 2:        + push notification
  Slice 3:        + downloads pr-gate-summary.json from the run's artifacts;
                    posts structured halt comment on Jira (WARN/BLOCK)
  Slice 4:        + auto-merge + Done transition (PASS+clean)
```

### Why GitHub event, not API trigger

Slice 1 was originally designed to use the routine's "Call via API" trigger,
POSTed from a step in `release-readiness.yml`. That doesn't work: claude.ai's
API is fronted by Cloudflare managed challenge, and GitHub Actions runners
cannot pass the challenge — the curl returns HTTP 403 with the CF block page.

Pivoted to **GitHub event trigger** on the routine: Claude's GitHub App
subscribes to repository webhooks directly, bypassing the public claude.ai
API path entirely. No workflow step, no shared secret, no curl.

## Slice status

| Slice | Ticket | Routine capability | State |
|-------|--------|--------------------|-------|
| 1 | SCRUM-382 | Logs receipt by commenting on the PR. No decisions, no merge, no Jira write. | Current |
| 2 | SCRUM-383 | + push notification on every terminal outcome | Pending |
| 3 | SCRUM-384 | + structured halt comment on linked Jira ticket (WARN/BLOCK) | Pending |
| 4 | SCRUM-385 | + auto-merge and Done transition on PASS+clean | Pending |
| 5 | SCRUM-386 | Cut over FULL_AUTO / epic-run docs; remove polling | Pending |

## One-time setup (repo admin)

### 1. Install the Claude GitHub App on `psuthar/talkback`

Go to <https://github.com/apps/claude> (or whichever app name appears on the
Authorized GitHub Apps tab of your account) and install it on the
`psuthar/talkback` repo. The Claude Code "Routines" UI cannot list repos
until the GitHub App has actual installation access on them.

### 2. Create the Claude routine

Via the claude.ai UI at <https://claude.ai/code/routines> (form-driven path)
or `RemoteTrigger create` (API path — see the troubleshooting note below).

- **Name:** `TalkBack PR Gate handler (psuthar/talkback) — Slice 1`
- **Source / repository:** `psuthar/talkback`
- **Model:** any (Slice 1 is tiny; Sonnet is sufficient)
- **Trigger:** **GitHub event** — filter so it fires on `workflow_run`
  completed events. If the UI exposes finer event-type controls, scope it
  to `workflow_run` only.
- **Permissions:** the per-repo permissions panel — leave
  "Allow unrestricted git push" **off**. Slice 1 only posts a PR comment.
- **Prompt:** copy verbatim from [Slice 1 prompt](#slice-1-routine-prompt) below.

After save, capture the routine ID (format: `trig_xxxxxxxxxxxxxxxxxxxxxxxx`).

### 3. Verify

Open any PR; let `release-readiness` complete. Within ~60 seconds of the
workflow finishing, you should see a comment on the PR posted by the routine
confirming receipt of the `workflow_run.completed` event. If you don't see
one:

- Routine session log in claude.ai → Routines → your routine → **Runs**.
  Failures and stdout/stderr from the routine's session are captured there.
- If no run was triggered, the GitHub App probably doesn't have webhook
  delivery enabled for `workflow_run` events on this repo. Re-check the
  app's repository access in GitHub Settings.

## Slice 1 routine prompt

Copy this verbatim into the routine.

```
You are the TalkBack PR Gate webhook handler (Slice 1, SCRUM-382). You're
invoked by a GitHub webhook delivery — specifically a `workflow_run.completed`
event from `psuthar/talkback`.

This slice's only job is to prove the trigger plumbing works by posting a
"webhook received" comment on the PR. No decisions, no merge, no Jira write,
no notifications. Subsequent slices (SCRUM-383/384/385) replace this prompt
with progressively richer logic.

Filter rules (silently exit if any fails — many workflow_run events are
unrelated):

1. The event's `workflow_run.name` must equal "Release Readiness".
2. The event's `workflow_run.event` must equal "pull_request".
3. The event's `workflow_run.pull_requests` array must have at least one
   entry. Use the first entry's number as the PR number.

Extract from the payload:

  pr_number  = workflow_run.pull_requests[0].number
  head_sha   = workflow_run.head_sha
  conclusion = workflow_run.conclusion   (success | failure | neutral | …)
  run_id     = workflow_run.id
  run_url    = workflow_run.html_url

Run exactly one command (substituting the actual values):

  gh pr comment <pr_number> -R psuthar/talkback --body \
    "TalkBack PR Gate webhook received (via workflow_run event).
workflow_run.conclusion: <conclusion>
head_sha: <head_sha>
run_id: <run_id>
run_url: <run_url>"

After the comment is posted (or the gh command errors), report the outcome
in one short sentence and exit. Do not merge the PR. Do not transition any
Jira ticket. Do not send notifications.
```

## Payload contract

The routine receives GitHub's standard `workflow_run` webhook payload. The
fields the Slice 1 prompt uses are all on the top-level `workflow_run`
object — full GitHub schema:
<https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_run>.

Future slices will additionally call:

```bash
gh run download <workflow_run.id> -R psuthar/talkback -n release-readiness
jq '.final_gate.status, .pr_risk.top_risk_factors' artifacts/pr-gate-summary.json
```

…to extract the same fields the original API-trigger payload contained.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| No comment on any PR after `release-readiness` completes | Routine isn't subscribed to `workflow_run` events on this repo, or the Claude GitHub App lost installation. Check claude.ai → Routines → your routine → Runs tab. |
| Routine runs but errors with "Could not resolve workflow_run.name" | The event payload schema changed, or the routine prompt's filter is too strict. Inspect the Run page for the actual payload received. |
| Routine posts on every PR including ones where the gate hasn't completed | The `workflow_run.conclusion` filter wasn't checked. Tighten the routine's filter logic. |
| Comment appears but on a stale PR | `workflow_run.pull_requests[0]` doesn't always point at the open PR for the head_sha — particularly for force-pushed branches. Cross-reference `workflow_run.head_branch` with the PR's head ref before commenting. (Out of Slice 1 scope; address in Slice 3 if observed.) |

## Authorization scope

The Slice 1 routine has a deliberately tight scope — comment-write on PRs
in `psuthar/talkback`, nothing else. Each subsequent slice expands the scope
explicitly:

| Slice | Adds |
|-------|------|
| 2 | Push notification capability |
| 3 | Artifact download from workflow runs (`gh run download`), Jira comment write |
| 4 | PR merge (`gh pr merge`), Jira transition write |

Keep the scope as narrow as possible at each slice so a misbehaving routine
cannot exceed its intended blast radius.

## Why not the workflow-step approach we shipped originally?

The original Slice 1 design (revision-1 of SCRUM-382) had a step in
`.github/workflows/release-readiness.yml` that posted a pre-extracted payload
to `https://claude.ai/api/v1/code/triggers/<id>/run`. That step has been
removed. The pivot rationale:

1. **Cloudflare managed challenge** on claude.ai blocks headless curl from
   GitHub Actions runners. The step worked locally and got HTTP 403 + a
   challenge page in CI.
2. **No shared secret needed.** Removing the workflow step removes the
   `CLAUDE_TRIGGER_TOKEN` secret and `CLAUDE_PR_GATE_ROUTINE_ID` variable from
   the repo's config surface. The Claude GitHub App authorization is the
   single source of trust.
3. **Simpler workflow.** One less step to maintain in
   `release-readiness.yml`.

The trade-off — the routine must fetch the gate summary itself in later
slices, rather than receiving it pre-extracted — is small (one
`gh run download` call) and is captured in the Slice 3 ticket.

## References

- Epic: SCRUM-381 (PR Gate Automation)
- FULL_AUTO policy: `docs/agent/workflow-full-auto.md`
- Epic-run policy: `docs/agent/workflow-epic-run.md` (updated in SCRUM-386)
- GitHub `workflow_run` event schema: <https://docs.github.com/en/webhooks/webhook-events-and-payloads#workflow_run>
