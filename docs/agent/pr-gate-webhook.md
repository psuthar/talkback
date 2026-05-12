# PR Gate Webhook → Claude Routine

Push-based handling of TalkBack PR Gate outcomes. Replaces 30-second polling
in `implement SCRUM-XXX FULL_AUTO`. Epic: SCRUM-381.

This document covers Slices 1–2 (SCRUM-382, SCRUM-383, plus the
SCRUM-387 correction). It is updated in place as later slices land — see
the **Slice status** table.

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
  Slice 2 (now):  + comment body restructured so its second line is the
                    outcome headline; claude.ai's run-completion notification
                    (configured at Settings → General → Notifications)
                    carries that headline to the user's device
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
| 2 | SCRUM-383 / SCRUM-387 | + comment body formatted for notification surface (claude.ai run-completion delivers the alert); + idempotency dedup keyed on `head_sha` | Current |
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
- **Tool allow-list:** Slice 2 requires `Bash` only (for `gh`). The
  routine UI doesn't expose per-routine tool toggles; the cloud-routine
  default allow-list (`Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`,
  `WebFetch`, `WebSearch`) is broader than the minimum but fine — none of
  the unused tools are exercised by the prompt below.
- **Notifications:** ensure the relevant toggles are on at
  <https://claude.ai/settings/general> → Notifications section, especially
  **Response completions** and **Code notifications**. See the
  [Notifications](#notifications) section below for the full delivery
  channel map.
- **Prompt:** copy verbatim from [Current routine prompt](#current-routine-prompt) below.

After save, capture the routine ID (format: `trig_xxxxxxxxxxxxxxxxxxxxxxxx`).

### 3. Verify

Open any PR; let `release-readiness` complete. After the gate decides, the
workflow's last step applies one of `pr-gate:pass` / `pr-gate:warn` /
`pr-gate:block` / `pr-gate:unknown` to the PR — visible immediately in the
PR's labels strip. Within ~60 seconds of that label appearing, two things
should happen:

1. A comment with `<!-- pr-gate-routine head=… -->` (first line) appears
   on the PR. Its second line is the outcome headline, e.g.
   `PR Gate: WARN — PR #344 (803b903)`.
2. claude.ai surfaces the run-completion notification on whatever channels
   you have enabled (browser push, mobile app, email — see
   <https://claude.ai/settings/general>). The routine's last message in
   that notification is the outcome headline above.

A re-run of `release-readiness` against the same head_sha should NOT
produce a second comment or a second notification — the idempotency marker
on the comment is the dedupe key.

If you don't see the expected outputs:

- Routine session log in claude.ai → Routines → your routine → **Runs**.
  Failures and stdout/stderr from the routine's session are captured there.
- If no run was triggered, the GitHub App probably doesn't have webhook
  delivery enabled for `pull_request.labeled` events on this repo. Re-check
  the app's repository access in GitHub Settings.
- If the comment posted but no claude.ai notification arrived, the
  notification toggles at <https://claude.ai/settings/general> are off, or
  your device doesn't have the Anthropic mobile app / browser push
  configured. See the [Notifications](#notifications) section below.

## Current routine prompt

This is the live prompt (Slices 1+2). Copy verbatim into the routine.

The prompt does one thing — post a PR comment whose headline (second
line) is the outcome summary. claude.ai's run-completion notification
surfaces the routine's final emitted sentence; the prompt instructs the
routine to emit the same headline at exit so the notification's preview
text matches the PR comment. There is no separate `PushNotification`
step — cloud routines don't have access to that tool, and the comment
body + run-completion notification cover the delivery surface.

```
You are the TalkBack PR Gate webhook handler (SCRUM-382 + SCRUM-383,
revised by SCRUM-387). You're invoked by a GitHub webhook — specifically
a `pull_request.labeled` event from `psuthar/talkback` where the applied
label name starts with `pr-gate:`.

Your job: post a PR comment that confirms the webhook fired and surfaces
the gate outcome. The comment body's headline (second line) doubles as
the notification text — claude.ai's run-completion notification carries
the routine's final sentence to the user's enabled channels.

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
and would otherwise produce duplicate comments. Before posting:

  existing=$(gh pr view <pr_number> -R psuthar/talkback \
    --json comments \
    --jq '.comments[] | select(.body | contains("<!-- pr-gate-routine head=<head_sha> -->")) | .id' \
    | head -1)
  if [ -n "$existing" ]; then
    echo "Already processed head_sha=<head_sha>; skipping."
    exit 0
  fi

Pick the headline and guidance line by outcome:

  pass    → headline: "PR Gate: PASS — PR #<pr_number> (<short_sha>)"
            guidance: "Ready to merge."
  warn    → headline: "PR Gate: WARN — PR #<pr_number> (<short_sha>)"
            guidance: "Review warnings before merge."
  block   → headline: "PR Gate: BLOCK — PR #<pr_number> (<short_sha>)"
            guidance: "Fix CI; do not merge."
  unknown → headline: "PR Gate: UNKNOWN — PR #<pr_number> (<short_sha>)"
            guidance: "Gate status not determined; see workflow run."

Post the comment. The marker MUST be the first line of the body so the
idempotency check above finds it on re-runs.

  gh pr comment <pr_number> -R psuthar/talkback --body \
    "<!-- pr-gate-routine head=<head_sha> -->
<headline>

<guidance>

Details: label=<label.name>, head_sha=<head_sha>"

Then emit the headline verbatim as your final sentence and exit. The
final sentence is what claude.ai's run-completion notification displays
on the user's device. Do not merge the PR. Do not transition any Jira
ticket.
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
| Comment appears but no claude.ai notification arrives | The notification toggles at <https://claude.ai/settings/general> are off (Response completions + Code notifications), or no delivery target is configured (browser push not granted, mobile app not installed, email disabled). See [Notifications](#notifications). |
| GitHub notifications never arrive for routine comments | Expected. GitHub does not notify users about their own actions, and the routine runs as the routine owner. Rely on claude.ai notifications. |
| Comment fires twice on the same head_sha | The idempotency check's `gh pr view --json comments --jq …` didn't match the marker. Common cause: the marker line isn't the first line of the body, or the head_sha format drifted (use full 40-char SHA on both sides, never the short form). Inspect the routine's Run log for the value of `existing`. |

## Authorization scope

Each slice expands the routine's reach explicitly so a misbehaving routine
can't exceed its slice's intended blast radius.

| Slice | State | Tools required (cumulative) | New capability granted |
|-------|-------|------------------------------|------------------------|
| 1 | Done | `Bash` (for `gh pr comment`, `gh pr view`) | Comment-write on PRs in `psuthar/talkback`. |
| 2 | Current | (no new tools) | Notification surface via the existing comment-write tool + claude.ai's run-completion notification setting. See [Notifications](#notifications). |
| 3 | Pending | (still `Bash`; uses `gh run download` + `mcp__atlassian__jira_*` if configured) | Artifact download from workflow runs; Jira comment write. |
| 4 | Pending | + `gh pr merge`, Jira transition write | PR merge + Jira Done transition. |

The routine UI doesn't expose per-tool toggles. The deployed routine's
allow-list (`Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`, `WebFetch`,
`WebSearch`) is broader than the minimum, but only `Bash` is exercised by
the current prompt; the rest are dormant.

## Notifications

The routine's act of posting a PR comment is the message; *delivery* to
the user happens via one of these channels.

| Channel | Status | Notes |
|---------|--------|-------|
| **claude.ai run-completion** | Primary channel. Toggles at <https://claude.ai/settings/general> → Notifications section. **Response completions** + **Code notifications** must both be on. | Surfaces the routine's last emitted sentence; the prompt instructs the routine to emit the outcome headline verbatim at exit (e.g. `PR Gate: WARN — PR #344 (803b903)`). Carries to whatever the user has configured: browser web push, the Anthropic mobile app, email. |
| **GitHub PR-comment** | Configured but ineffective for the routine owner. | GitHub does not notify users about their own actions. The routine runs as the routine owner's identity, so PR comments it posts won't fire a GitHub notification to that owner. For other repo subscribers, GitHub notifications work as usual. |
| **Third-party connector** (Slack, etc.) | Not configured; future option. | Routines support adding connectors (claude.ai → Routines → routine → Connectors tab). A Slack connector + an additional prompt step could deliver to a channel/DM. Tracked as a future enhancement, not part of any Slice 1–5. |

If `claude.ai` notifications aren't reaching you, check:

1. The two toggles (Response completions + Code notifications) are on.
2. At least one delivery target is configured (browser push permission
   granted, mobile app installed and signed in, or email enabled).
3. The routine actually ran — claude.ai → Routines → your routine → Runs.
   A failed/skipped run won't trigger the completion notification.

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
