# PR Gate Webhook → Claude Routine

Push-based handling of TalkBack PR Gate outcomes. Replaces 30-second polling
in `implement SCRUM-XXX FULL_AUTO`. Epic: SCRUM-381.

This document covers Slices 1–3 (SCRUM-382, SCRUM-383, SCRUM-384, plus
the SCRUM-387 correction). It is updated in place as later slices land —
see the **Slice status** table.

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
  Slice 2:        + comment body restructured so its second line is the
                  outcome headline; claude.ai's run-completion notification
                  (configured at Settings → General → Notifications)
                  carries that headline to the user's device
  Slice 3 (now):  + on WARN/BLOCK only: downloads pr-gate-summary.json from
                  the release-readiness workflow's artifacts, extracts the
                  gate signals (pr_risk band/score/factors, release_readiness
                  status), and posts a structured halt comment on the Jira
                  ticket linked from the PR title's SCRUM-XXX key (via the
                  Atlassian connector). PASS / UNKNOWN skip the Jira step.
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
| 2 | SCRUM-383 / SCRUM-387 | + comment body formatted for notification surface (claude.ai run-completion delivers the alert); + idempotency dedup keyed on `head_sha` | Done |
| 3 | SCRUM-384 | + on WARN/BLOCK only: downloads `pr-gate-summary.json` from the workflow artifacts and posts a structured halt comment on the linked Jira ticket (Atlassian connector required) | Current |
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
- **Tool allow-list:** Slice 3 requires `Bash` (for `gh pr comment`,
  `gh pr view`, `gh run list`, `gh run download`) and the Atlassian
  connector (see below). The routine UI doesn't expose per-routine tool
  toggles; the cloud-routine default allow-list is broader than the
  minimum but fine — none of the unused tools are exercised by the
  prompt below.
- **Connectors (NEW for Slice 3):** add the **Atlassian** connector via
  claude.ai → Routines → routine → **Connectors** tab → **Add connector** →
  Atlassian. Authorize the routine for the Jira instance at
  `suthar-team.atlassian.net` (or whatever Jira host your tickets live in).
  Once added, `mcp__atlassian__jira_*` tools become available to the
  routine. **Without this connector, Slice 3's Jira halt comment will
  fail gracefully** and the routine will fall through with a `notice` —
  the PR comment from Slice 1+2 is still posted.
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
PR's labels strip. Within ~60–90 seconds of that label appearing, the
routine should produce:

1. A PR comment with `<!-- pr-gate-routine head=… -->` (first line) and
   the outcome headline (second line), e.g.
   `PR Gate: WARN — PR #344 (803b903)`.
2. A claude.ai run-completion notification carrying the outcome headline
   (configured channels per Settings → General → Notifications).
3. **On WARN or BLOCK only:** a structured halt comment on the Jira
   ticket whose key appears in the PR title (e.g. `SCRUM-388` from
   `SCRUM-388: …`). The Jira comment lists the gate signals (pr_risk
   band/score, top risk factors, release_readiness status,
   mergeable_state) and three resume options. PASS and UNKNOWN do NOT
   produce a Jira comment.

A re-run of `release-readiness` against the same head_sha should NOT
produce a second PR comment, a second notification, or a second Jira
comment — the idempotency marker on the PR comment is the dedupe key.

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
- If the PR comment posted but the Jira halt comment didn't (on
  WARN/BLOCK), the Atlassian connector probably isn't added to the
  routine, or its authorization expired. claude.ai → Routines → your
  routine → **Connectors** tab → confirm Atlassian is present and active.
  See [Troubleshooting](#troubleshooting) below.

## Current routine prompt

This is the live prompt (Slices 1+2+3). Copy verbatim into the routine.

The prompt does up to three things:

1. **Always:** post a PR comment whose headline (second line) is the
   outcome summary. claude.ai's run-completion notification surfaces
   the routine's final emitted sentence; the prompt instructs the
   routine to emit the same headline at exit so the notification's
   preview text matches the PR comment.
2. **WARN or BLOCK only:** download `pr-gate-summary.json` from the
   `release-readiness` workflow's artifacts, extract gate signals, and
   post a structured halt comment on the linked Jira ticket via the
   Atlassian connector.
3. **Never:** merge the PR or transition Jira state — those land in
   Slice 4 (SCRUM-385).

```
You are the TalkBack PR Gate webhook handler (SCRUM-382 + SCRUM-383 +
SCRUM-384, revised by SCRUM-387). You're invoked by a GitHub webhook —
specifically a `pull_request.labeled` event from `psuthar/talkback` where
the applied label name starts with `pr-gate:`.

Your job:

  1. Always: post a PR comment that confirms the webhook fired and
     surfaces the gate outcome. The comment body's headline (second
     line) doubles as the notification text.
  2. On WARN or BLOCK only: download the release-readiness workflow's
     pr-gate-summary.json artifact, extract gate signals, and post a
     structured halt comment on the linked Jira ticket via the
     mcp__atlassian__jira_add_comment tool.

No merge, no Jira state transition — those land in Slice 4 (SCRUM-385).

Filter rules (silently exit if any fails — the routine's trigger filter
should already enforce these, but defense in depth):

  1. The event's `action` must equal "labeled".
  2. The event's `label.name` must start with the prefix "pr-gate:".

Extract from the payload:

  pr_number  = pull_request.number
  pr_title   = pull_request.title
  pr_url     = pull_request.html_url
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

Step 1 — Post the PR comment. The marker MUST be the first line of the
body so the idempotency check above finds it on re-runs.

  gh pr comment <pr_number> -R psuthar/talkback --body \
    "<!-- pr-gate-routine head=<head_sha> -->
<headline>

<guidance>

Details: label=<label.name>, head_sha=<head_sha>"

Step 2 — Jira halt comment (WARN or BLOCK only). On PASS or UNKNOWN,
skip this entire step and go to the final exit.

  2a. Extract the Jira key from the PR title — first match of the regex
      `SCRUM-[0-9]+`. If no match, log a notice ("no Jira key in PR
      title; skipping Jira step") and continue to final exit. The PR
      comment from Step 1 is still the user's signal.

      jira_key=$(echo "<pr_title>" | grep -oE 'SCRUM-[0-9]+' | head -1)

  2b. Find the release-readiness workflow run id for this head_sha.
      Sometimes the docs-skip-hint workflow is the one that ran instead
      — try it as a fallback. If neither found, log + skip Jira step
      (PR comment already posted).

      run_id=$(gh run list -R psuthar/talkback --commit <head_sha> \
        --workflow "Release Readiness" --json databaseId \
        --jq '.[0].databaseId')
      if [ -z "$run_id" ]; then
        echo "No release-readiness run found for head_sha=<head_sha>; skipping Jira step."
        # Continue to final exit.
      fi

  2c. Download the artifact (idempotent — overwrites prior dir):

      rm -rf /tmp/gate-<head_sha> && mkdir -p /tmp/gate-<head_sha>
      gh run download "$run_id" -R psuthar/talkback \
        -n release-readiness -D /tmp/gate-<head_sha>
      summary_path="/tmp/gate-<head_sha>/artifacts/pr-gate-summary.json"
      if [ ! -f "$summary_path" ]; then
        echo "pr-gate-summary.json not in artifact; skipping Jira step."
        # Continue to final exit.
      fi

  2d. Extract fields:

      final_gate_status=$(jq -r '.final_gate.status // "unknown"' "$summary_path")
      pr_risk_band=$(jq -r '.pr_risk.band // "unknown"' "$summary_path")
      pr_risk_score=$(jq -r '.pr_risk.score // "?"' "$summary_path")
      pr_risk_confidence=$(jq -r '.pr_risk.confidence // "?"' "$summary_path")
      top_risk_factors=$(jq -r '.pr_risk.top_risk_factors // [] | join(", ")' "$summary_path")
      rr_status=$(jq -r '.release_readiness.status // "unknown"' "$summary_path")
      rr_score=$(jq -r '.release_readiness.score // "?"' "$summary_path")
      rr_warnings=$(jq -r '.release_readiness.warnings // 0' "$summary_path")
      rr_blockers=$(jq -r '.release_readiness.blockers // 0' "$summary_path")
      mergeable=$(gh pr view <pr_number> -R psuthar/talkback \
        --json mergeStateStatus --jq .mergeStateStatus)

  2e. Post the Jira halt comment via mcp__atlassian__jira_add_comment.
      Call with issueKey=<jira_key> and a body of the form:

          FULL_AUTO HALT — TalkBack PR Gate: <UPPERCASE_OUTCOME>

          PR: <pr_url>
          Head SHA: <head_sha>

          Gate signals (from artifacts/pr-gate-summary.json):
          - final_gate: <final_gate_status>
          - pr_risk: band=<pr_risk_band>, score=<pr_risk_score>, confidence=<pr_risk_confidence>
          - top_risk_factors: [<top_risk_factors>]
          - release_readiness: <rr_status> (score <rr_score>/100, <rr_warnings> warnings, <rr_blockers> blockers)
          - mergeable_state: <mergeable>

          Resume options:
          1. Manual squash-merge to accept the risk (same playbook used
             in SCRUM-366 / 367 / 370).
          2. Push additional commits to address the signals;
             release-readiness re-runs and the routine posts an updated
             comment if the outcome changes.
          3. Cancel and re-evaluate.

          Auto-posted by Claude routine (Slice 3, SCRUM-384). Re-runs on
          the same head_sha are deduped via the PR comment marker —
          this Jira comment is posted at most once per head_sha.

      If the call errors (Atlassian connector missing or auth expired),
      log the error and continue to final exit — do NOT crash the
      routine. The PR comment is still the fallback signal.

Final exit:

  Emit the headline (from the outcome map above) verbatim as the
  routine's last sentence and exit. The final sentence is what
  claude.ai's run-completion notification displays on the user's
  device. Do not merge the PR. Do not transition any Jira ticket.
```

## Payload contract

The routine receives GitHub's standard `pull_request` webhook payload with
`action: "labeled"`. Schema:
<https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request>.

Fields the current prompt (Slices 1+2+3) uses:

| Field | Source |
|-------|--------|
| `action` | top-level — must be `"labeled"` |
| `label.name` | the freshly-applied label, e.g. `pr-gate:warn` |
| `pull_request.number` | the PR number |
| `pull_request.title` | parsed for the `SCRUM-XXX` Jira key (Slice 3) |
| `pull_request.html_url` | included in the Jira halt comment (Slice 3) |
| `pull_request.head.sha` | head SHA at the moment the label was applied; also used as the idempotency dedupe key |

Slice 3 also reads from `artifacts/pr-gate-summary.json` downloaded via
`gh run download` from the `release-readiness` workflow:

| JSON path | Used for |
|-----------|----------|
| `.final_gate.status` | Echoed in the Jira halt comment. |
| `.pr_risk.band`, `.pr_risk.score`, `.pr_risk.confidence` | Same — risk-scoring summary. |
| `.pr_risk.top_risk_factors[]` | Joined comma-separated for the Jira halt comment. |
| `.release_readiness.status`, `.release_readiness.score`, `.release_readiness.warnings`, `.release_readiness.blockers` | Same. |

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
| Jira halt comment never appears on WARN/BLOCK | One of: (a) Atlassian connector not added to the routine — claude.ai → Routines → routine → Connectors tab → Add connector → Atlassian, then authorize the Jira host; (b) authorization expired — reconnect; (c) PR title doesn't contain a `SCRUM-XXX` key — the routine logs "no Jira key in PR title; skipping Jira step" and falls through; (d) the release-readiness artifact wasn't produced this run (e.g., docs-skip-hint workflow ran instead) — the routine logs "No release-readiness run found" or "pr-gate-summary.json not in artifact" and falls through. In every fall-through case the PR comment from Step 1 is still posted. |
| Jira halt comment posted on PASS | Shouldn't happen — Slice 3 only posts on WARN or BLOCK. Inspect the routine's Run log: if `outcome` was parsed as `warn`/`block` for a PR that actually passed, the label-application step in the workflow may have mis-classified. Check `artifacts/pr-gate-summary.json` for the run and the workflow's `case "$final_gate"` branch. |

## Authorization scope

Each slice expands the routine's reach explicitly so a misbehaving routine
can't exceed its slice's intended blast radius.

| Slice | State | Tools required (cumulative) | New capability granted |
|-------|-------|------------------------------|------------------------|
| 1 | Done | `Bash` (for `gh pr comment`, `gh pr view`) | Comment-write on PRs in `psuthar/talkback`. |
| 2 | Done | (no new tools) | Notification surface via the existing comment-write tool + claude.ai's run-completion notification setting. See [Notifications](#notifications). |
| 3 | Current | + `gh run list` / `gh run download` (still `Bash`); + Atlassian connector for `mcp__atlassian__jira_add_comment` | Artifact read from the `release-readiness` workflow + Jira comment-write on the linked ticket (WARN/BLOCK only). |
| 4 | Pending | + `gh pr merge`, Jira transition write | PR merge + Jira Done transition. |

The routine UI doesn't expose per-tool Bash toggles. The deployed routine's
default allow-list (`Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`,
`WebFetch`, `WebSearch`) is broader than the minimum but only `Bash` is
exercised by the current prompt. MCP tools (e.g. `mcp__atlassian__jira_*`)
become available only when the corresponding connector is added on the
routine's **Connectors** tab.

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
