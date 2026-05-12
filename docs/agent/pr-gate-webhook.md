# PR Gate Webhook → Claude Routine

> **Optional path — opt-in via `FULL_AUTO_WEBHOOK` (SCRUM-392).** This path is **not** the default. Each `pull_request.labeled` and `pull_request.closed` event the routine subscribes to consumes one of your daily claude.ai routine runs (~15/day on the default plan), which is below normal dev volume. The default `implement SCRUM-XX FULL_AUTO` uses the **polling path** documented in [`workflow-full-auto.md`](workflow-full-auto.md) and consumes no claude.ai quota.
>
> Use this path by invoking `implement SCRUM-XX FULL_AUTO_WEBHOOK` (note the trailing `_WEBHOOK`). The agent then skips polling, lets the routine merge in the cloud, and only runs local cleanup + a brief closure comment. Same PR / Jira outputs as the polling path; different execution surface.
>
> To enable cleanly: in claude.ai → Routines → "TalkBack PR Gate handler" → set status to **Active**. To disable cleanly (avoid accidental quota consumption while you're on the polling default): set status to **Inactive**.

Push-based handling of TalkBack PR Gate outcomes via Claude routines, originally introduced as the default in SCRUM-381–SCRUM-391 and demoted to opt-in in SCRUM-392 once the daily-quota cost was understood.

This document covers Slices 1–6 (SCRUM-382, SCRUM-383, SCRUM-384, SCRUM-385, SCRUM-386, plus the SCRUM-387 correction, SCRUM-390 enrichment, and SCRUM-391 CLOSE FLOW). It is updated in place as later slices land — see the **Slice status** table.

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
  Slice 3:        + on WARN/BLOCK only: downloads pr-gate-summary.json from
                  the release-readiness workflow's artifacts, extracts the
                  gate signals, posts a structured halt comment on the Jira
                  ticket linked from the PR title's SCRUM-XXX key.
                  PASS / UNKNOWN skip the Jira step.
  Slice 4:        + on PASS + mergeable_state=clean only: pre-merge guard
                  (re-read PR state at merge moment), then `gh pr merge
                  --squash`, then post a Jira completion comment, then
                  transition the linked Jira ticket to Done via the
                  Atlassian connector's transition-issue tool.
                  WARN / BLOCK / UNKNOWN never merge.
  Slice 6 (now):  + on `pull_request.closed` with `merged=true`: the
                  routine also subscribes to the close event so that
                  manual squash-merges of WARN/BLOCK PRs (the documented
                  override path) auto-close Jira. Posts a "FULL_AUTO
                  COMPLETE (manual override)" Jira comment naming the
                  bypassed gate outcome and the merging user, then
                  transitions Jira to Done. Idempotency: a Slice 4
                  fingerprint on the head_sha comment short-circuits
                  the close-event handler so PASS auto-merges don't
                  double-write.
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
| 3 | SCRUM-384 | + on WARN/BLOCK only: downloads `pr-gate-summary.json` from the workflow artifacts and posts a structured halt comment on the linked Jira ticket (Atlassian connector required) | Done |
| 4 | SCRUM-385 | + on PASS only: pre-merge guards, squash-merges the PR, posts a Jira completion comment, transitions the linked Jira ticket to Done (Atlassian "Transition issue" tool required) | Done |
| 5 | SCRUM-386 | Cut over FULL_AUTO / epic-run docs; remove polling | Done |
| 5b | SCRUM-390 | + Step 1 rich PR comment mirroring `release-readiness.yml` "PR Gate Summary" format (signals table, top risks, required-before-merge, analysis link); minimal fallback when no artifact | Done |
| 6 | SCRUM-391 | + subscribe to `pull_request.closed` (merged=true) and handle WARN/BLOCK manual-override squash-merges: post Jira "FULL_AUTO COMPLETE (manual override)" comment + transition to Done. Slice 4 fingerprint short-circuits PASS-path double-writes. | Current |

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
- **Trigger:** the routine subscribes to **two** GitHub event types in
  the same trigger config:
  1. **`Pull request labeled`** (preset row, or Custom → event
     `Pull request labeled`). Add a Filter on the `Labels` property.
     If a `starts with` operator is available, use it with value
     `pr-gate:`. Otherwise switch the operator to `is one of` and add
     all four labels as discrete values: `pr-gate:pass`, `pr-gate:warn`,
     `pr-gate:block`, `pr-gate:unknown`.
  2. **`Pull request closed`** (preset row, or Custom → event
     `Pull request closed`). Add a Filter on the `Merged` property =
     `true` if the UI exposes it (recommended); if not, the prompt
     short-circuits on `merged == false` so an unfiltered subscription
     is still safe.

  Both event types fire the same prompt; the prompt's event router
  dispatches to the LABEL FLOW or CLOSE FLOW based on `action`. The
  CLOSE FLOW (SCRUM-391) handles manual-override squash-merges of
  PRs the routine had halted at WARN or BLOCK — the operator only has
  to click "Squash and merge" in GitHub and the routine closes out the
  Jira ticket (override comment + Done transition).
- **Permissions:** the per-repo permissions panel — leave
  "Allow unrestricted git push" **off**.
- **Tool allow-list:** Slice 3 requires `Bash` (for `gh pr comment`,
  `gh pr view`, `gh run list`, `gh run download`) and the Atlassian
  connector (see below). The routine UI doesn't expose per-routine tool
  toggles; the cloud-routine default allow-list is broader than the
  minimum but fine — none of the unused tools are exercised by the
  prompt below.
- **Connectors (NEW for Slice 3, expanded for Slice 4):** add the
  **Atlassian** (Rovo) connector via claude.ai → Routines → routine →
  **Connectors** tab → **Add connector** → Atlassian. Authorize the
  routine for the Jira instance at `suthar-team.atlassian.net` (or
  whatever Jira host your tickets live in). Once added, Atlassian Rovo
  exposes Jira tools to the routine. For Slice 3 the routine needs
  **Add comment** (find it in the connector's Read-only tools — enable
  with auto-allow). For Slice 4 the routine additionally needs
  **Transition issue** (Interactive tools, auto-allowed by default).
  **Without the Atlassian connector, both Slice 3 (Jira halt comment)
  and Slice 4 (Jira Done transition) fall through with a notice** —
  the PR comment from Slice 1+2 is still posted, and Slice 4's
  squash-merge still completes via `gh pr merge` (the Jira transition
  is the only step that depends on the connector for Slice 4).
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

1. A PR comment with `<!-- pr-gate-routine head=… -->` (first line) followed
   by a body that mirrors the `github-actions[bot]` "PR Gate Summary" comment
   that `release-readiness.yml` already auto-posts: a `## 🤖 PR Gate Summary`
   header, a signals table (PR Risk / Release Readiness / Final Gate with
   colored emojis), a deterministic-gate caveat, top risk signals + required-
   before-merge bullet lists, and a `Full analysis: pr-gate-summary.md` link.
   When `pr-gate-summary.json` isn't present (docs-skip-hint path), the
   routine falls back to a minimal headline-only body.
2. A claude.ai run-completion notification carrying the outcome headline
   (configured channels per Settings → General → Notifications).
3. **On WARN or BLOCK only:** a structured halt comment on the Jira
   ticket whose key appears in the PR title (e.g. `SCRUM-388` from
   `SCRUM-388: …`). The Jira comment lists the gate signals (pr_risk
   band/score, top risk factors, release_readiness status,
   mergeable_state) and three resume options. PASS and UNKNOWN do NOT
   produce a Jira comment.
4. **On PASS + mergeable_state=clean only:** the routine pre-merge-guards
   the PR (re-reads via `gh pr view` and confirms still clean), then
   squash-merges via `gh pr merge --squash --delete-branch`, posts a
   Jira completion comment on the linked ticket, and transitions that
   ticket to Done via the Atlassian connector's `Transition issue`
   tool. WARN / BLOCK / UNKNOWN never merge.
5. **On manual squash-merge of a WARN/BLOCK PR (Slice 6):** the
   `pull_request.closed` (merged=true) event fires the routine's
   CLOSE FLOW. It posts a "FULL_AUTO COMPLETE (manual override)" Jira
   comment naming the bypassed gate outcome and the merging user, then
   transitions Jira to Done. A PASS auto-merge from Slice 4 also fires
   the close event but is silenced by an idempotency check that looks
   for the Slice 4 fingerprint on the head_sha PR comment. Local
   cleanup (FF main, worktree remove, branch delete) is unchanged —
   the cloud routine can't touch the developer's filesystem.

A re-run of `release-readiness` against the same head_sha should NOT
produce a second PR comment, a second notification, a second Jira
comment, a second merge attempt, or a second Done transition — the
idempotency marker on the PR comment is the dedupe key for the whole
routine.

**Primary-tree FF gap (Slice 4 vs SCRUM-388 rule):** When the routine
auto-merges, the user's primary working tree does NOT auto fast-forward.
The SCRUM-388 FF rule lives in the Claude Code FULL_AUTO close-out
(executed by a developer-side agent), not in this cloud routine. After
a routine-driven auto-merge, the developer's primary tree will be one
commit behind until they `git pull --ff-only origin main` manually
(or until their next `implement SCRUM-XX FULL_AUTO` close-out catches
up). Out of cloud-routine reach — documented as a known gap.

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

This is the live prompt (Slices 1+2+3+4+6). Copy verbatim into the routine.

The prompt routes on event type first — `pull_request.labeled` takes the
LABEL FLOW (Pre-step + Steps 1–3); `pull_request.closed` with
`merged=true` takes the CLOSE FLOW (Steps M1–M6). The LABEL FLOW does up
to five things; the CLOSE FLOW does two (Jira override comment + Jira
Done transition):

1. **Always (Pre-step):** attempt to download `pr-gate-summary.json` from
   the `release-readiness` workflow's artifact. If present, the routine
   has rich gate signals to compose downstream comments; if absent, it
   falls back to minimal bodies. Performed early so the gate data is
   available to every subsequent step.
2. **Always:** post a PR comment. With the artifact present, the body
   mirrors `release-readiness.yml`'s "PR Gate Summary" format (signals
   table, top risk signals, required-before-merge, analysis link). Without
   the artifact, falls back to the minimal headline-only body.
3. **WARN or BLOCK only:** post a structured halt comment on the linked
   Jira ticket via the Atlassian connector, reusing the artifact from
   the Pre-step.
4. **PASS only (and mergeable_state=clean):** pre-merge guard, then
   squash-merge via `gh pr merge --squash --delete-branch`, then post a
   Jira completion comment, then transition the Jira ticket to Done via
   the Atlassian connector's transition-issue tool.
5. **Always:** emit the outcome headline as the final sentence so the
   run-completion notification carries it.

The CLOSE FLOW (event B, SCRUM-391):

6. **`pull_request.closed` with `merged=true`:** dedupe against the
   Slice 4 PASS auto-merge fingerprint (M1); read the prior gate outcome
   from the most recent head_sha PR comment (M2); extract the Jira key
   from the PR title (M3); post a "FULL_AUTO COMPLETE (manual override)"
   Jira comment naming the bypassed gate outcome and the merging user
   (M4); transition Jira to Done (M5); emit the headline (M6 / Final
   exit). On `merged=false` (user closed without merging), exit silently.

The PR comment format (Step 1 with artifact present) is intentionally
the same as the comment posted by `.github/workflows/release-readiness.yml`'s
"Post PR gate comment" step. The workflow's bot comment and the routine's
comment will look near-identical (one extra marker line on the routine's,
plus the routine's comment is `psuthar`-authored rather than
`github-actions[bot]`-authored). If the workflow's comment format
changes in the future, update the routine prompt to match.

```
You are the TalkBack PR Gate webhook handler (SCRUM-382 + SCRUM-383 +
SCRUM-384 + SCRUM-385, revised by SCRUM-387, extended by SCRUM-391).
You're invoked by GitHub webhooks from `psuthar/talkback`. Two event
types fan into this routine:

  A. `pull_request.labeled` where the applied label name starts with
     `pr-gate:` — the normal gate-signal flow (post PR comment; on
     WARN/BLOCK post Jira halt; on PASS auto-merge + Jira Done).
  B. `pull_request.closed` where `pull_request.merged == true` — the
     manual-override close-out flow. Triggered when a human squash-merges
     a PR the routine had halted at WARN or BLOCK. Posts a Jira override
     comment and transitions Jira to Done so the operator's only step is
     clicking "Squash and merge" in GitHub.

Your job:

  Event router (decide first, before any other work):
    - action == "labeled" → execute LABEL FLOW below (Pre-step + Steps 1–3 + Final exit).
    - action == "closed" AND merged == true → execute CLOSE FLOW below (Steps M1–M6 + Final exit).
    - action == "closed" AND merged == false → silently exit (user closed without merging; respect that).
    - anything else → silently exit.

  LABEL FLOW (event A):
    Pre-step (always): attempt to download pr-gate-summary.json from the
    release-readiness workflow's artifact for this head_sha. Used by every
    step below. If unavailable, fall through to minimal-body fallbacks.

    1. Always: post a PR comment. With the artifact, build a rich body that
       mirrors release-readiness.yml's "Post PR gate comment" output (signal
       table + top risk signals + required-before-merge + analysis link).
       Without the artifact, fall back to a minimal headline-only body.
    2. On WARN or BLOCK only: post a structured halt comment on the linked
       Jira ticket via the Atlassian connector's add-comment tool, using
       the gate signals from the Pre-step.
    3. On PASS only (and only if mergeable_state is still clean at the
       moment of merge): pre-merge guard, squash-merge the PR, post a
       Jira completion comment, transition the Jira ticket to Done via
       the Atlassian connector's transition-issue tool.

  CLOSE FLOW (event B): see Steps M1–M6 below — post a Jira "manual
  override" completion comment, transition Jira to Done. No PR comment
  is posted on the close event (the prior gate's PR comment from
  LABEL FLOW remains the gate signal of record).

Filter rules (silently exit if any fails — the routine's trigger filter
should already enforce these, but defense in depth):

  For LABEL FLOW:
    1. The event's `action` must equal "labeled".
    2. The event's `label.name` must start with the prefix "pr-gate:".
  For CLOSE FLOW:
    3. The event's `action` must equal "closed".
    4. `pull_request.merged` must equal `true` (otherwise silent exit —
       the user closed the PR without merging).

Extract from the payload (both events share the first five fields):

  pr_number  = pull_request.number
  pr_title   = pull_request.title
  pr_url     = pull_request.html_url
  head_sha   = pull_request.head.sha
  short_sha  = first 7 chars of head_sha

  LABEL FLOW only:
    outcome  = the part of label.name after "pr-gate:"
               (one of: pass | warn | block | unknown)

  CLOSE FLOW only:
    merge_sha       = pull_request.merge_commit_sha
    short_merge_sha = first 7 chars of merge_sha
    merged_by       = sender.login  (the GitHub user who clicked merge)

Idempotency check (LABEL FLOW only — CLOSE FLOW dedupes via M1). Re-runs
of release-readiness re-apply the same label and would otherwise produce
duplicate comments. Before posting:

  existing=$(gh pr view <pr_number> -R psuthar/talkback \
    --json comments \
    --jq '.comments[] | select(.body | contains("<!-- pr-gate-routine head=<head_sha> -->")) | .id' \
    | head -1)
  if [ -n "$existing" ]; then
    echo "Already processed head_sha=<head_sha>; skipping."
    exit 0
  fi

Pick the fallback headline and guidance line by outcome (LABEL FLOW
only — used when the gate artifact isn't available for Step 1's body,
and for the LABEL FLOW Final exit notification):

  pass    → headline: "PR Gate: PASS — PR #<pr_number> (<short_sha>)"
            guidance: "Ready to merge."
  warn    → headline: "PR Gate: WARN — PR #<pr_number> (<short_sha>)"
            guidance: "Review warnings before merge."
  block   → headline: "PR Gate: BLOCK — PR #<pr_number> (<short_sha>)"
            guidance: "Fix CI; do not merge."
  unknown → headline: "PR Gate: UNKNOWN — PR #<pr_number> (<short_sha>)"
            guidance: "Gate status not determined; see workflow run."

Pre-step — Download the gate artifact (always attempt; flag the result
for use in every step below).

  run_id=$(gh run list -R psuthar/talkback --commit <head_sha> \
    --workflow "Release Readiness" --json databaseId \
    --jq '.[0].databaseId')
  artifact_available=false
  workflow_run_url=""
  if [ -n "$run_id" ]; then
    workflow_run_url="https://github.com/psuthar/talkback/actions/runs/${run_id}"
    rm -rf /tmp/gate-<head_sha> && mkdir -p /tmp/gate-<head_sha>
    if gh run download "$run_id" -R psuthar/talkback \
         -n release-readiness -D /tmp/gate-<head_sha> 2>/dev/null; then
      summary_path="/tmp/gate-<head_sha>/artifacts/pr-gate-summary.json"
      [ -f "$summary_path" ] && artifact_available=true
    fi
  fi

  if [ "$artifact_available" = "true" ]; then
    final_gate_status=$(jq -r '.final_gate.status // "UNKNOWN"' "$summary_path" | tr '[:lower:]' '[:upper:]')
    pr_risk_status=$(jq -r '.pr_risk.status // "UNKNOWN"' "$summary_path" | tr '[:lower:]' '[:upper:]')
    pr_risk_label=$(jq -r '.pr_risk.label // empty' "$summary_path")
    pr_risk_band=$(jq -r '.pr_risk.band // "unknown"' "$summary_path")
    pr_risk_score=$(jq -r '.pr_risk.score // "?"' "$summary_path")
    pr_risk_confidence=$(jq -r '.pr_risk.confidence // "?"' "$summary_path")
    top_risk_factors_json=$(jq -c '.pr_risk.top_risk_factors // []' "$summary_path")
    required_actions_json=$(jq -c '.required_actions // []' "$summary_path")
    rr_status=$(jq -r '.release_readiness.status // "UNKNOWN"' "$summary_path" | tr '[:lower:]' '[:upper:]')
    rr_score=$(jq -r '.release_readiness.score // "?"' "$summary_path")
    rr_warnings=$(jq -r '.release_readiness.warnings // 0' "$summary_path")
    rr_blockers=$(jq -r '.release_readiness.blockers // 0' "$summary_path")
  fi

Step 1 — Post the PR comment. The marker MUST be the first line so the
idempotency check above finds it on re-runs.

  Status emoji map (used in 1a):
    PASS=🟢, WARN=🟡, BLOCK=🔴, anything else=⚪
  REC_DISPLAY map:
    PASS="PASS (low risk)", WARN="WARN", BLOCK="BLOCK"

  1a. If artifact_available == true, compose the rich body (mirrors
      release-readiness.yml's "Post PR gate comment" output):

      risk_emoji = STATUS_EMOJI[<pr_risk_status>] or '⚪'
      rr_emoji   = STATUS_EMOJI[<rr_status>] or '⚪'
      gate_emoji = STATUS_EMOJI[<final_gate_status>] or '⚪'
      risk_label_display = <pr_risk_label> if non-empty else
                           REC_DISPLAY[<pr_risk_status>] or <pr_risk_status>
      rr_label_display   = "<rr_status> (<rr_score>/100)"

      Body (in this exact line order — the marker is line 1):

        <!-- pr-gate-routine head=<head_sha> -->
        ## 🤖 PR Gate Summary

        | Signal | Result |
        |--------|--------|
        | PR Risk | <risk_emoji> <risk_label_display> |
        | Release Readiness | <rr_emoji> <rr_label_display> |
        | **Final Gate** | **<gate_emoji> <final_gate_status>** |

        > _This gate is deterministic. PASS does not bypass branch protection or required code review._

      If top_risk_factors_json has 1+ entries: append a blank line, the
      header `**Top risk signals:**`, and the first 4 entries as
      `- <factor>` bullets.

      If required_actions_json has 1+ entries: append a blank line, the
      header `**Required before merge:**`, and the first 5 entries as
      `- <action>` bullets.

      Append: blank line, then
        `_Full analysis: [pr-gate-summary.md](<workflow_run_url>)_`

  1b. Else (artifact_available == false — typically docs-skip-hint or
      a repo without the gate), fall back to the minimal body:

        <!-- pr-gate-routine head=<head_sha> -->
        <headline>

        <guidance>

        Details: label=<label.name>, head_sha=<head_sha>
        (no pr-gate-summary.json available; rich body skipped)

  1c. Post via:
        gh pr comment <pr_number> -R psuthar/talkback --body "<body>"

Step 2 — Jira halt comment (WARN or BLOCK only). On PASS or UNKNOWN,
skip this entire step and go to Step 3.

  2a. Extract the Jira key from the PR title — first match of the regex
      `SCRUM-[0-9]+`. If no match, log a notice ("no Jira key in PR
      title; skipping Jira step") and continue to Step 3. The PR comment
      from Step 1 is still the user's signal.

      jira_key=$(echo "<pr_title>" | grep -oE 'SCRUM-[0-9]+' | head -1)

  2b. If artifact_available is false (gate signals not loaded in the
      Pre-step), log "pr-gate-summary.json not available; skipping Jira
      step" and continue to Step 3. The PR comment from Step 1 has the
      fallback headline; that's the user's signal for this case.

  2c. mergeable_state (computed live; not in artifact):

      mergeable=$(gh pr view <pr_number> -R psuthar/talkback \
        --json mergeStateStatus --jq .mergeStateStatus)

      Convert top_risk_factors_json to a comma-separated string for
      the comment body:
        top_risk_factors=$(echo "$top_risk_factors_json" | jq -r 'join(", ")')

  2d. Post the Jira halt comment via mcp__atlassian__jira_add_comment.
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
      log the error and continue to Step 3 / final exit — do NOT crash
      the routine. The PR comment is still the fallback signal.

  2e. (Step 2e renumbered out — the previous prompt had artifact-download
      inline here; now handled in the Pre-step. Skip directly to Step 3.)

Step 3 — Auto-merge + Jira Done (PASS only). On WARN / BLOCK / UNKNOWN,
skip this entire step and go to the final exit. Outcome must equal
"pass" — exact match.

  3a. Pre-merge guard. Re-read the PR state right now to confirm it's
      still mergeable and not already merged:

        pr_state=$(gh pr view <pr_number> -R psuthar/talkback \
          --json mergeStateStatus,state,mergeCommit \
          --jq '{state, ms: .mergeStateStatus, merged_sha: .mergeCommit.oid}')
        ms=$(echo "$pr_state" | jq -r '.ms')
        state=$(echo "$pr_state" | jq -r '.state')
        existing_merge_sha=$(echo "$pr_state" | jq -r '.merged_sha // ""')

      Branch:
        - If state == "MERGED": skip 3b (merge already happened — manual
          override or another fire); use existing_merge_sha as the
          merge_sha and continue to 3c/3d/3e.
        - If state != "OPEN": log "PR not in OPEN state ($state); skipping
          merge" and exit. (CLOSED unmerged means the user closed without
          merging; respect that.)
        - If ms != "CLEAN": log "mergeable_state=$ms; skipping merge —
          gate is stale, push or re-run release-readiness" and exit.
          (The PR comment from Step 1 already documents the PASS outcome;
          the operator can re-trigger.)

  3b. Merge:

        merge_resp=$(gh pr merge <pr_number> -R psuthar/talkback \
          --squash --delete-branch 2>&1)
        if [ $? -ne 0 ]; then
          echo "Merge failed: $merge_resp"
          # Post an error comment on the PR and exit without transitioning Jira.
          gh pr comment <pr_number> -R psuthar/talkback --body \
            "<!-- pr-gate-routine merge-failed head=<head_sha> -->
TalkBack PR Gate auto-merge failed.
gh pr merge returned: $merge_resp
Manual squash-merge required; Jira ticket remains In Review."
          exit 1
        fi
        merge_sha=$(gh pr view <pr_number> -R psuthar/talkback \
          --json mergeCommit --jq .mergeCommit.oid)

  3c. Compose the Jira completion comment body. Use the gate signals
      from the Pre-step if artifact_available; otherwise post a minimal
      completion comment.

      With artifact:

        FULL_AUTO COMPLETE — TalkBack PR Gate: PASS

        PR: <pr_url> merged at <merge_sha>
        Head SHA at merge: <head_sha>

        Gate signals (from artifacts/pr-gate-summary.json):
        - final_gate: PASS
        - pr_risk: band=<pr_risk_band>, score=<pr_risk_score>
        - release_readiness: <rr_status> (<rr_score>/100,
          <rr_warnings> warnings, <rr_blockers> blockers)
        - mergeable_state at merge: clean

        Auto-merged and transitioned to Done by Claude routine (Slice 4,
        SCRUM-385; PR comment format enriched by SCRUM-390).

      Without artifact (artifact_available == false):

        FULL_AUTO COMPLETE — TalkBack PR Gate: PASS

        PR: <pr_url> merged at <merge_sha>
        Head SHA at merge: <head_sha>

        (pr-gate-summary.json not available; gate signal detail omitted —
        likely a docs-skip-hint path.)

        Auto-merged and transitioned to Done by Claude routine (Slice 4,
        SCRUM-385; PR comment format enriched by SCRUM-390).

  3d. Extract the Jira key from PR title (same regex as Step 2a). If no
      key found, log "no Jira key in PR title; skipping Jira step
      (merge already happened)" and exit. The PR is merged, just not
      ticket-tracked.

  3e. Post the completion comment via the Atlassian connector's
      add-comment tool (same one Step 2 uses), then transition the
      ticket to Done via the Atlassian connector's transition-issue
      tool (target status name "Done"). Both calls handle errors
      gracefully — log and continue. The merge is complete regardless.

CLOSE FLOW — Manual-override close-out (event B). Runs when a human
squash-merges a PR outside of the LABEL FLOW's Step 3 auto-merge. Goal:
post the Jira completion comment + transition to Done so the operator
doesn't have to remember any post-merge steps. The local agent still
owns laptop-side cleanup (FF main, worktree remove, branch -D) on the
developer's next "finish SCRUM-XXX" / continue — that boundary is
unchanged.

  M1. Idempotency / dedupe against LABEL FLOW Step 3 auto-merges. When
      LABEL FLOW merges, it triggers `pull_request.closed` as a side
      effect — without dedupe we'd double-write to Jira.

        existing=$(gh pr view <pr_number> -R psuthar/talkback \
          --json comments \
          --jq '.comments[] | select(.body | contains("<!-- pr-gate-routine head=<head_sha> -->")) | select(.body | contains("Auto-merged and transitioned to Done by Claude routine")) | .id' \
          | head -1)
        if [ -n "$existing" ]; then
          echo "PASS auto-merge already closed out by LABEL FLOW; skipping CLOSE FLOW."
          exit 0
        fi

      The dedupe key is the *combination* of the head_sha marker AND
      the Slice 4 fingerprint text ("Auto-merged and transitioned to
      Done by Claude routine") in the same PR comment. Either alone is
      not sufficient — a user who happens to copy the marker string
      into a manual comment shouldn't suppress this flow.

  M2. Read the prior gate outcome from the most recent PR comment with
      the `<!-- pr-gate-routine head=` marker. Headlines follow the
      "PR Gate: <OUTCOME>" pattern so we can grep for the outcome.

        prior_body=$(gh pr view <pr_number> -R psuthar/talkback \
          --json comments \
          --jq '[.comments[] | select(.body | startswith("<!-- pr-gate-routine head="))] | last | .body // ""')
        prior_outcome=$(echo "$prior_body" | grep -oE 'PR Gate: (PASS|WARN|BLOCK|UNKNOWN)' | head -1 | awk '{print $3}')
        [ -z "$prior_outcome" ] && prior_outcome="UNKNOWN (no prior gate signal)"

  M3. Extract the Jira key from the PR title (same regex as Step 2a):

        jira_key=$(echo "<pr_title>" | grep -oE 'SCRUM-[0-9]+' | head -1)
        if [ -z "$jira_key" ]; then
          echo "No Jira key in PR title; merged but not ticket-tracked. Skipping M4 + M5."
          # Continue to Final exit so the run-completion notification still fires.
        fi

  M4. Post the Jira override comment via the Atlassian connector's
      add-comment tool. Call with issueKey=<jira_key> and a body of
      this form:

          FULL_AUTO COMPLETE (manual override) — TalkBack PR Gate: <prior_outcome>

          PR: <pr_url>
          Merged at: <merge_sha>
          Head SHA at merge: <head_sha>
          Merged by: <merged_by>

          The routine had halted at <prior_outcome> (see the prior Jira
          halt comment for the gate signals). <merged_by> squash-merged
          the PR manually, accepting the gate risk.

          Local cleanup still pending (cloud routines can't touch the
          developer's laptop): FF the primary tree's main, remove the
          worktree, delete the local branch. The developer's agent
          will run these on the next "finish SCRUM-XXX" / continue.

          Auto-posted by Claude routine (Slice 6, SCRUM-391). Idempotency
          marker on the corresponding PR comment dedupes against
          LABEL FLOW auto-merges, so this comment is posted at most
          once per manual override per head_sha.

      If the connector call errors (Atlassian down / auth expired), log
      the error and continue to M5. The merge already happened; the
      operator can transition Jira by hand from the override notice on
      the PR.

  M5. Transition the Jira ticket to Done via the Atlassian connector's
      transition-issue tool (target status name "Done"). Same handling
      as Step 3e: if the call errors, log and continue to Final exit.

  M6. (No further action — proceed to Final exit.)

Final exit:

  Emit the headline as the routine's last sentence and exit. The final
  sentence is what claude.ai's run-completion notification displays on
  the user's device.

  LABEL FLOW headline format:
    "PR Gate: <OUTCOME> — PR #<N> (<short_sha>)"
    For PASS, append " — merged at <short_merge_sha>" so the
    notification reflects that the merge actually happened.

  CLOSE FLOW headline format:
    "PR Gate: <prior_outcome> — PR #<N> (<short_sha>) — manually merged by <merged_by> at <short_merge_sha>"

  Examples:
    "PR Gate: PASS — PR #348 (887d7be) — merged at a1b2c3d"
    "PR Gate: WARN — PR #344 (803b903)"
    "PR Gate: BLOCK — PR #342 (f4dc764)"
    "PR Gate: UNKNOWN — PR #999 (deadbee)"
    "PR Gate: WARN — PR #350 (20da230) — manually merged by psuthar at 7e254b6"
```

## Payload contract

The routine receives GitHub's standard `pull_request` webhook payload.
Two `action` values fan into the same prompt: `"labeled"` (LABEL FLOW)
and `"closed"` (CLOSE FLOW, SCRUM-391). Schema:
<https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request>.

Fields the current prompt uses:

| Field | Used in | Source |
|-------|---------|--------|
| `action` | router | must be `"labeled"` or `"closed"` |
| `label.name` | LABEL FLOW | the freshly-applied label, e.g. `pr-gate:warn` |
| `pull_request.number` | both | the PR number |
| `pull_request.title` | both | parsed for the `SCRUM-XXX` Jira key |
| `pull_request.html_url` | both | included in the Jira comment |
| `pull_request.head.sha` | both | head SHA at the moment the event fired; also the idempotency dedupe key |
| `pull_request.merged` | CLOSE FLOW filter | must be `true` for CLOSE FLOW; `false` exits silently |
| `pull_request.merge_commit_sha` | CLOSE FLOW M4 / Final exit | the squash-merge commit on `main` |
| `sender.login` | CLOSE FLOW M4 / Final exit | the GitHub user who clicked merge |

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
| Routine's PR comment is the minimal "Details: label=…" body instead of the rich signal table | The Pre-step couldn't download `pr-gate-summary.json` for this `head_sha`. Most common cause: PR went through the `Release Readiness (docs skip-hint)` workflow rather than `Release Readiness`, so no artifact was produced. The fallback minimal body is intentional in that case; the bot's own gate comment is also missing for the same reason. To fix at the gate level, scope the docs-skip-hint workflow to actually emit a `pr-gate-summary.json` stub. Out of scope here. |
| Routine's PR comment has rich format but differs from the bot's comment | The two comment formats should match because both derive from `pr-gate-summary.json` via the same logic. If they diverge, the workflow's "Post PR gate comment" step changed and the routine prompt is stale. Update the routine's Step 1 body template to match. The source of truth is `.github/workflows/release-readiness.yml`'s `lines` array. |
| Jira halt comment never appears on WARN/BLOCK | One of: (a) Atlassian connector not added to the routine — claude.ai → Routines → routine → Connectors tab → Add connector → Atlassian, then authorize the Jira host; (b) authorization expired — reconnect; (c) PR title doesn't contain a `SCRUM-XXX` key — the routine logs "no Jira key in PR title; skipping Jira step" and falls through; (d) the release-readiness artifact wasn't produced this run (e.g., docs-skip-hint workflow ran instead) — the routine logs "No release-readiness run found" or "pr-gate-summary.json not in artifact" and falls through. In every fall-through case the PR comment from Step 1 is still posted. |
| Jira halt comment posted on PASS | Shouldn't happen — Slice 3 only posts on WARN or BLOCK. Inspect the routine's Run log: if `outcome` was parsed as `warn`/`block` for a PR that actually passed, the label-application step in the workflow may have mis-classified. Check `artifacts/pr-gate-summary.json` for the run and the workflow's `case "$final_gate"` branch. |
| PR didn't auto-merge on PASS | Pre-merge guard failed: re-read `mergeable_state` was not `CLEAN` at the moment of merge. Most common cause: the gate fired with `pr-gate:pass` label but a required reviewer requirement was added/changed between gate completion and the routine firing, or branch protection requires up-to-date branch and the PR went stale. Routine logs `"mergeable_state=<state>; skipping merge"` and exits without merging — the PR comment from Step 1 already announces PASS, so the operator can push a fixup or merge manually. |
| Routine fired on a PASS PR but the merge command errored | Inspect the routine's Run log for the `gh pr merge` exit code and stderr. Common causes: branch protection rejected the merge (e.g., requires linear history but the PR isn't rebased); the routine's GitHub App scope doesn't include `contents:write` on this repo (unlikely if Slice 1 PR comment worked); or a race with another merge. The routine posts a `<!-- pr-gate-routine merge-failed -->` comment on the PR documenting the error and exits without transitioning Jira. The Jira ticket stays In Review until manually closed out. |
| Jira didn't transition to Done after a successful merge | The Atlassian connector's transition-issue call failed. Most common: ticket has a non-standard workflow that doesn't expose "Done" as a transition (some projects use "Closed" or "Resolved" instead). Check the routine's Run log for the connector error. Fix: either align the project's workflow to expose `Done` as a transition name, or update the routine prompt to use the project's actual terminal-state name. The merge itself is unaffected; the ticket just needs a manual transition. |
| I manually squash-merged a WARN/BLOCK PR and Jira stayed In Review | The routine's trigger isn't subscribed to `pull_request.closed` events — the CLOSE FLOW (Slice 6, SCRUM-391) never fired. Add the second event-type to the routine's Trigger config: claude.ai → Routines → routine → **Trigger** → add `Pull request closed` alongside `Pull request labeled`. Filter on `Merged = true` if the UI exposes it; the prompt also short-circuits on `merged=false` so an unfiltered subscription is still safe. After the subscription is added, manual override merges will auto-post the override comment and transition Jira within ~60s. |
| CLOSE FLOW posted a Jira override comment after a PASS auto-merge | The M1 idempotency check didn't match the Slice 4 fingerprint. The check requires BOTH the head_sha marker `<!-- pr-gate-routine head=<sha> -->` AND the literal string `Auto-merged and transitioned to Done by Claude routine` in the same PR comment. If Slice 4's comment body wording changes, update M1's contains-string in the prompt to match. |

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
