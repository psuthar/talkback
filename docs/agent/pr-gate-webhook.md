# PR Gate Webhook → Claude Routine

Push-based handling of TalkBack PR Gate outcomes. Replaces 30-second polling
in `implement SCRUM-XXX FULL_AUTO`. Epic: SCRUM-381.

This document covers Slices 1–2 (SCRUM-382, SCRUM-383). It is updated in
place as later slices land — see the **Slice status** table.

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
  Slice 1:        posts a "received" comment on the PR (with idempotency
                  marker so re-runs for the same head_sha don't double-post)
  Slice 2 (now):  + push notification with concise outcome line
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
| 1 | SCRUM-382 | Logs receipt by commenting on the PR. No decisions, no merge, no Jira write. | Done |
| 2 | SCRUM-383 | + push notification on every terminal outcome; + idempotency dedup keyed on `head_sha` | Current |
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

- **Name:** `TalkBack PR Gate handler (psuthar/talkback)` (the per-slice
  suffix is informational only — the same routine evolves across slices via
  prompt + tool allow-list updates).
- **Source / repository:** `psuthar/talkback`
- **Model:** any (the work is tiny; Sonnet is sufficient).
- **Trigger:** **GitHub event** → preset row **Pull request labeled** (or
  **Custom** → event `Pull request labeled`). Add a Filter on the `Labels`
  property. If a `starts with` operator is available, use it with value
  `pr-gate:`. Otherwise switch the operator to `is one of` and add all
  four labels as discrete values: `pr-gate:pass`, `pr-gate:warn`,
  `pr-gate:block`, `pr-gate:unknown`.
- **Permissions:** the per-repo permissions panel — leave
  "Allow unrestricted git push" **off**.
- **Tool allow-list:** Slice 2 requires `Bash` (for `gh`) and
  `PushNotification`. Other tools (`Read`, `Write`, `Edit`, `Glob`, `Grep`,
  `WebFetch`, `WebSearch`) are not needed for this slice — narrow the
  allow-list to the minimum your routine UI permits.
- **Prompt:** copy verbatim from [Current routine prompt](#current-routine-prompt) below.

After save, capture the routine ID (format: `trig_xxxxxxxxxxxxxxxxxxxxxxxx`).

### 3. Verify

Open any PR; let `release-readiness` complete. After the gate decides, the
workflow's last step applies one of `pr-gate:pass` / `pr-gate:warn` /
`pr-gate:block` / `pr-gate:unknown` to the PR — visible immediately in the
PR's labels strip. Within ~60 seconds of that label appearing, two things
should happen:

1. A "Webhook received" comment with `<!-- pr-gate-routine head=… -->`
   appears on the PR.
2. A push notification arrives on the user's device with a one-line outcome
   summary (`PR #X PASS — abcd123`, `PR #X WARN — review needed (abcd123)`,
   etc).

A re-run of `release-readiness` against the same head_sha should NOT
produce a second comment or a second notification — the idempotency marker
on the comment is the dedupe key.

If you don't see one of the two outputs:

- Routine session log in claude.ai → Routines → your routine → **Runs**.
  Failures and stdout/stderr from the routine's session are captured there.
- If no run was triggered, the GitHub App probably doesn't have webhook
  delivery enabled for `pull_request.labeled` events on this repo. Re-check
  the app's repository access in GitHub Settings.
- If the comment posted but the notification didn't, the routine's
  allow-list is missing `PushNotification` — re-check via claude.ai → the
  routine's Permissions tab.

## Current routine prompt

This is the live prompt (Slices 1+2). Copy verbatim into the routine.

```
You are the TalkBack PR Gate webhook handler (SCRUM-382 + SCRUM-383). You're
invoked by a GitHub webhook — specifically a `pull_request.labeled` event
from `psuthar/talkback` where the applied label name starts with
`pr-gate:`.

Your job has two parts:
  1. Post a comment on the PR confirming the webhook fired (with an
     idempotency marker keyed on head_sha so re-runs don't double-post).
  2. Send a push notification with a concise outcome line.

No Jira write, no merge — those land in Slices 3+4 (SCRUM-384, SCRUM-385).

Filter rules (silently exit if any fails — the routine's trigger filter
should already enforce these, but defense in depth):

  1. The event's `action` must equal "labeled".
  2. The event's `label.name` must start with the prefix "pr-gate:".

Extract from the payload:

  pr_number  = pull_request.number
  head_sha   = pull_request.head.sha
  short_sha  = first 7 chars of head_sha
  outcome    = the part of label.name after "pr-gate:"
               (one of: pass | warn | block | unknown)

Idempotency check — re-runs of release-readiness re-apply the same label
and would otherwise produce duplicate comments and duplicate notifications.
Before doing anything else, run:

  existing=$(gh pr view <pr_number> -R psuthar/talkback \
    --json comments \
    --jq '.comments[] | select(.body | contains("<!-- pr-gate-routine head=<head_sha> -->")) | .id' \
    | head -1)
  if [ -n "$existing" ]; then
    echo "Already processed head_sha=<head_sha>; skipping."
    exit 0
  fi

Step 1 — post the comment. The marker `<!-- pr-gate-routine head=… -->`
is the dedupe key for the idempotency check above; it MUST be the first
line of the body.

  gh pr comment <pr_number> -R psuthar/talkback --body \
    "<!-- pr-gate-routine head=<head_sha> -->
TalkBack PR Gate webhook received (via pull_request.labeled event).
label: <label.name>
outcome: <outcome>
head_sha: <head_sha>"

Step 2 — send a push notification. Pick the message text by outcome:

  pass    → "PR #<pr_number> PASS — <short_sha>"
  warn    → "PR #<pr_number> WARN — review needed (<short_sha>)"
  block   → "PR #<pr_number> BLOCK — fix CI then re-push (<short_sha>)"
  unknown → "PR #<pr_number> gate unknown — see workflow"

Use the PushNotification tool. Keep the message ≤ 200 characters, single
line, no markdown.

After both steps complete (or any individual step errors), report the
outcome in one short sentence and exit. Do not merge the PR. Do not
transition any Jira ticket.
```

## Payload contract

The routine receives GitHub's standard `pull_request` webhook payload with
`action: "labeled"`. Schema:
<https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request>.

Fields the current prompt (Slices 1+2) uses:

| Field | Source |
|-------|--------|
| `action` | top-level — must be `"labeled"` |
| `label.name` | the freshly-applied label, e.g. `pr-gate:warn` |
| `pull_request.number` | the PR number |
| `pull_request.head.sha` | head SHA at the moment the label was applied; also used as the idempotency dedupe key |

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
| Comment appears but no push notification arrives | The routine's tool allow-list doesn't include `PushNotification`, or the user's notification settings on claude.ai are disabled. Check the routine's Permissions tab. |
| Comment + notification both fire twice on the same head_sha | The idempotency check's `gh pr view --json comments --jq …` didn't match the marker. Common cause: the marker line in Step 1 isn't the first line of the body, or the head_sha format drifted (use full 40-char SHA on both sides, never the short form). Inspect the routine's Run log for the value of `existing`. |

## Authorization scope

Each slice expands the routine's reach explicitly so a misbehaving routine
can't exceed its slice's intended blast radius.

| Slice | State | Tools required (cumulative) | New capability granted |
|-------|-------|------------------------------|------------------------|
| 1 | Done | `Bash` (for `gh pr comment`, `gh pr view`) | Comment-write on PRs in `psuthar/talkback`. |
| 2 | Current | + `PushNotification` | Send a push notification to the routine owner. |
| 3 | Pending | + `Bash` (no new tools, but uses `gh run download` + `mcp__atlassian__jira_*` if configured) | Artifact download from workflow runs; Jira comment write. |
| 4 | Pending | + `gh pr merge`, Jira transition write | PR merge + Jira Done transition. |

Keep the allow-list as narrow as your routine UI permits. Tools beyond
this table (e.g. `Edit`, `Write`, `WebFetch`) are not used by any slice.

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
