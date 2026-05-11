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
  └─ "Apply pr-gate:<status> label" step
        │
        │ gh pr edit --remove-label pr-gate:<prev> --add-label pr-gate:<new>
        ▼
GitHub fires pull_request.labeled webhook
        │
        │ (Claude GitHub App fan-out to subscribed routines)
        ▼
Claude routine (subscribed to "Pull request labeled", filter:
labels starts-with "pr-gate:")
        │
        │ reads webhook payload, decides terminal action
        ▼
  Slice 1 (now):  posts a "received" comment on the PR
  Slice 2:        + push notification
  Slice 3:        + downloads pr-gate-summary.json from the run's artifacts;
                    posts structured halt comment on Jira (WARN/BLOCK)
  Slice 4:        + auto-merge + Done transition (PASS+clean)
```

### Why labels, not API or workflow_run

Two design dead-ends preceded the label-based handoff:

1. **API trigger** (curl POST from a workflow step to
   `claude.ai/api/v1/code/triggers/<id>/run`) — blocked by Cloudflare
   managed challenge from GitHub Actions runners. Returned HTTP 403 with the
   CF block page.

2. **`workflow_run` GitHub-event trigger** — not exposed in Claude's routine
   UI event list. Only `pull_request.*`, `issues.*`, and `release.*` events
   are selectable.

Labels are the only PR-event surface that CI can drive deterministically.
The workflow's `Apply pr-gate:<status> label` step applies one of
`pr-gate:pass` / `pr-gate:warn` / `pr-gate:block` / `pr-gate:unknown` to the
PR after the gate decides. The routine subscribes to `pull_request.labeled`
with a label-prefix filter and reads the outcome from the label name.

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
- **Trigger:** **GitHub event** → preset row **Pull request labeled** (or
  **Custom** → event `Pull request labeled`). Add a Filter on the `Labels`
  property. If a `starts with` operator is available, use it with value
  `pr-gate:`. Otherwise switch the operator to `is one of` and add all
  four labels as discrete values: `pr-gate:pass`, `pr-gate:warn`,
  `pr-gate:block`, `pr-gate:unknown`.
- **Permissions:** the per-repo permissions panel — leave
  "Allow unrestricted git push" **off**. Slice 1 only posts a PR comment.
- **Prompt:** copy verbatim from [Slice 1 prompt](#slice-1-routine-prompt) below.

After save, capture the routine ID (format: `trig_xxxxxxxxxxxxxxxxxxxxxxxx`).

### 3. Verify

Open any PR; let `release-readiness` complete. After the gate decides, the
workflow's last step applies one of `pr-gate:pass` / `pr-gate:warn` /
`pr-gate:block` / `pr-gate:unknown` to the PR — visible immediately in the
PR's labels strip. Within ~60 seconds of that label appearing, the routine
should post a "Webhook received" comment on the PR with the matched label
name. If you don't see one:

- Routine session log in claude.ai → Routines → your routine → **Runs**.
  Failures and stdout/stderr from the routine's session are captured there.
- If no run was triggered, the GitHub App probably doesn't have webhook
  delivery enabled for `pull_request.labeled` events on this repo. Re-check
  the app's repository access in GitHub Settings.

## Slice 1 routine prompt

Copy this verbatim into the routine.

```
You are the TalkBack PR Gate webhook handler (Slice 1, SCRUM-382). You're
invoked by a GitHub webhook — specifically a `pull_request.labeled` event
from `psuthar/talkback` where the applied label name starts with
`pr-gate:`.

This slice's only job is to prove the trigger plumbing works by posting a
"webhook received" comment on the PR. No decisions, no merge, no Jira write,
no notifications. Subsequent slices (SCRUM-383/384/385) replace this prompt
with progressively richer logic.

Filter rules (silently exit if any fails — the routine's trigger filter
should already enforce these, but defense in depth):

1. The event's `action` must equal "labeled".
2. The event's `label.name` must start with the prefix "pr-gate:".

Extract from the payload:

  pr_number  = pull_request.number
  head_sha   = pull_request.head.sha
  outcome    = the part of label.name after "pr-gate:"
               (one of: pass | warn | block | unknown)

Run exactly one command (substituting the actual values):

  gh pr comment <pr_number> -R psuthar/talkback --body \
    "TalkBack PR Gate webhook received (via pull_request.labeled event).
label: <label.name>
outcome: <outcome>
head_sha: <head_sha>"

After the comment is posted (or the gh command errors), report the outcome
in one short sentence and exit. Do not merge the PR. Do not transition any
Jira ticket. Do not send notifications.
```

## Payload contract

The routine receives GitHub's standard `pull_request` webhook payload with
`action: "labeled"`. Schema:
<https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request>.

Fields Slice 1 uses:

| Field | Source |
|-------|--------|
| `action` | top-level — must be `"labeled"` |
| `label.name` | the freshly-applied label, e.g. `pr-gate:warn` |
| `pull_request.number` | the PR number |
| `pull_request.head.sha` | head SHA at the moment the label was applied |

Future slices will additionally call:

```bash
# Look up the release-readiness run for this head SHA.
run_id=$(gh run list -R psuthar/talkback --commit <head_sha> \
  --workflow "Release Readiness" --json databaseId --jq '.[0].databaseId')
gh run download "$run_id" -R psuthar/talkback -n release-readiness
jq '.final_gate.status, .pr_risk.top_risk_factors' artifacts/pr-gate-summary.json
```

…to extract the same fields the original API-trigger payload contained.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| `pr-gate:<status>` label never appears on the PR | The workflow step `Apply pr-gate:<status> label for Claude routine` either failed or skipped. Check the `release-readiness` job log for the step (look for "Applied label pr-gate:..."). Most common skip: `artifacts/pr-gate-summary.json` wasn't produced this run. |
| Label appears but no routine comment follows | Routine isn't subscribed to `Pull request labeled` on this repo, or the Claude GitHub App lost installation, or the label-prefix filter excluded the label. Check claude.ai → Routines → your routine → **Runs** tab. |
| Routine runs but errors with "Could not resolve label.name" | The event payload schema changed, or the routine prompt's filter is too strict. Inspect the Run page for the actual payload received. |
| Routine posts on PRs where the gate didn't actually run | Some other automation added a `pr-gate:*` label. Tighten the workflow step's label set (only the 4 documented values) or scope the routine's filter to the exact 4 labels via "is one of". |
| Re-run of `release-readiness` for the same outcome doesn't fire the routine again | GitHub doesn't emit `labeled` when a label is already present. The workflow step removes the existing `pr-gate:<status>` label before re-adding it — confirm the remove step ran in the job log. |

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

## History — what was tried and discarded

| Attempt | What was tried | Why it didn't work |
|---------|----------------|--------------------|
| Rev 1 (PR #342, merged as `b24b352`) | Workflow step `Notify Claude PR Gate routine` posted to `claude.ai/api/v1/code/triggers/<id>/run` via curl, authenticated with `CLAUDE_TRIGGER_TOKEN`. | Cloudflare managed challenge on claude.ai returned HTTP 403 with a JS challenge page. Actions runners can't pass it. |
| Rev 2 (planned) | Configure the routine's trigger as a GitHub event scoped to `workflow_run`. | Claude's routine UI only exposes `pull_request.*`, `issues.*`, and `release.*` events. `workflow_run` / `check_run` aren't selectable. |
| Rev 3 (current) | Workflow step applies a `pr-gate:<status>` label to the PR after the gate decides; routine subscribes to `pull_request.labeled` filtered to that prefix. | Works. Label-add is a deterministic CI-driven PR event and the routine UI supports it natively. |

The `CLAUDE_TRIGGER_TOKEN` secret and `CLAUDE_PR_GATE_ROUTINE_ID` variable
that Rev 1 introduced are no longer used. They can be removed from
`psuthar/talkback` → Settings → Secrets and variables → Actions, or left
dormant (no workflow reads them).

## References

- Epic: SCRUM-381 (PR Gate Automation)
- FULL_AUTO policy: `docs/agent/workflow-full-auto.md`
- Epic-run policy: `docs/agent/workflow-epic-run.md` (updated in SCRUM-386)
- GitHub `pull_request` event schema (`action: labeled`): <https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request>
