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
  └─ Step: "Notify Claude PR Gate routine"
        │
        │  POST https://claude.ai/api/v1/code/triggers/<id>/run
        │  payload: { pr_number, head_sha, repository, final_gate,
        │             mergeable_state, top_risk_factors }
        ▼
  Claude routine (claude.ai/code/triggers/<id>)
        │
        │  reads payload, decides terminal action
        ▼
  Slice 1 (now):  posts a "received" comment on the PR
  Slice 2:        + push notification
  Slice 3:        + structured halt comment on Jira (WARN/BLOCK)
  Slice 4:        + auto-merge + Done transition (PASS+clean)
```

## Slice status

| Slice | Ticket | Routine capability | State |
|-------|--------|--------------------|-------|
| 1 | SCRUM-382 | Logs receipt by commenting on the PR. No decisions, no merge, no Jira write. | Current |
| 2 | SCRUM-383 | + push notification on every terminal outcome | Pending |
| 3 | SCRUM-384 | + structured halt comment on linked Jira ticket (WARN/BLOCK) | Pending |
| 4 | SCRUM-385 | + auto-merge and Done transition on PASS+clean | Pending |
| 5 | SCRUM-386 | Cut over FULL_AUTO / epic-run docs; remove polling | Pending |

## One-time setup (repo admin)

### 1. Create the Claude routine

Either via the claude.ai UI (Settings → Code → Triggers → New) or via the
`RemoteTrigger create` API. Use these settings:

- **Name:** `TalkBack PR Gate handler (psuthar/talkback)`
- **Schedule:** none (manual / API-triggered only)
- **Source:** `https://github.com/psuthar/talkback`
- **Model:** `claude-sonnet-4-6` (or whatever is current)
- **Allowed tools (Slice 1 only):** `mcp__github__add_issue_comment` — nothing else.
  Subsequent slices expand this list deliberately.
- **Prompt:** copy verbatim from [Slice 1 prompt](#slice-1-routine-prompt) below.

After create, capture the routine ID (format: `trig_xxxxxxxxxxxxxxxxxxxxxxxx`).

### 2. Mint a trigger token

In claude.ai Settings → Code → API tokens, create a token scoped to
**Run triggers only**. Copy the token — it won't be shown again.

### 3. Configure GitHub repo

In **Settings → Secrets and variables → Actions**:

- **Repository variable** `CLAUDE_PR_GATE_ROUTINE_ID` = the routine ID from step 1.
- **Repository secret** `CLAUDE_TRIGGER_TOKEN` = the token from step 2.

### 4. Verify

Open any PR; let `release-readiness` complete. Inside ~30 seconds of the gate
finishing, you should see a comment on the PR posted by the routine confirming
receipt of the event. If you don't see one:

- Workflow logs → expand **Notify Claude PR Gate routine** step. The HTTP status
  and any response body are echoed there.
- A `notice::Skipping Claude PR Gate notify` line means the var or secret isn't
  set on this repo yet (graceful skip — the workflow itself doesn't fail).

## Slice 1 routine prompt

Copy this verbatim into the routine. The placeholders in braces are populated
by the run-body payload (see [Payload contract](#payload-contract)).

```
You received a TalkBack PR Gate event. The run body contains:
  pr_number, head_sha, repository, final_gate, mergeable_state, top_risk_factors

Post a single comment on PR {pr_number} in repository {repository} with this body
(substituting the actual values):

> TalkBack PR Gate webhook received.
> final_gate: {final_gate}
> mergeable_state: {mergeable_state}
> head_sha: {head_sha}
> top_risk_factors: {top_risk_factors}

Do not merge the PR.
Do not transition any Jira ticket.
Do not send any notification.
After the comment is posted, exit.
```

## Payload contract

The workflow step (`Notify Claude PR Gate routine` in
`.github/workflows/release-readiness.yml`) POSTs this JSON shape:

```json
{
  "pr_number": "382",
  "head_sha": "abc123…",
  "repository": "psuthar/talkback",
  "final_gate": "PASS | WARN | BLOCK | unknown",
  "mergeable_state": "clean | behind | blocked | unstable | unknown | …",
  "top_risk_factors": ["Large diff", "Large frontend change"]
}
```

Field sources:

- `pr_number` — `github.event.number` (workflow context)
- `head_sha` — `github.sha`
- `repository` — `github.repository`
- `final_gate` — `final_gate.status` from `artifacts/pr-gate-summary.json`
- `mergeable_state` — `gh pr view <pr> --json mergeStateStatus` (lowercase string)
- `top_risk_factors` — `pr_risk.top_risk_factors[]` from `pr-gate-summary.json`

When the summary file isn't present (e.g. an early gate failure), the workflow
step skips with `::notice` and the routine is not invoked.

## Manual test (curl)

Once the routine and secrets are configured, test the trigger without waiting
for a real PR:

```bash
curl --silent --show-error \
  -H "Authorization: Bearer $CLAUDE_TRIGGER_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST "https://claude.ai/api/v1/code/triggers/$CLAUDE_PR_GATE_ROUTINE_ID/run" \
  -d '{
    "pr_number": "999",
    "head_sha": "deadbeef",
    "repository": "psuthar/talkback",
    "final_gate": "WARN",
    "mergeable_state": "blocked",
    "top_risk_factors": ["Large diff", "Large frontend change"]
  }'
```

A 2xx response confirms the trigger fired. The routine itself runs
asynchronously; you should see a comment on PR #999 within ~30 seconds. (If PR
#999 doesn't exist, the routine will error in its own session log — that's fine
for a smoke test.)

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| `::notice::Skipping Claude PR Gate notify — … not configured` | The `CLAUDE_TRIGGER_TOKEN` secret or `CLAUDE_PR_GATE_ROUTINE_ID` variable is missing on the repo. |
| `HTTP 401` in the workflow step | Token is invalid or revoked. Mint a new one and update the secret. |
| `HTTP 404` from the trigger endpoint | The routine ID is wrong, or the routine was deleted. Re-run `RemoteTrigger list` or check claude.ai → Code → Triggers. |
| Workflow step succeeds but no PR comment appears | The routine ran but the prompt or tool allow-list is wrong. Check the routine's session log in claude.ai. |
| Comment appears on every push, not just gate completion | Misconfiguration — the trigger step should only run after `Enforce TalkBack PR gate outcome`. Check the workflow's `if:` guard. |

## Authorization scope

The Slice 1 routine has a deliberately tight allow-list — comment-write on
PRs, nothing else. Each subsequent slice expands the list:

| Slice | Adds |
|-------|------|
| 2 | `PushNotification` |
| 3 | `mcp__atlassian__jira_add_comment`, `mcp__github__pull_request_read` |
| 4 | `mcp__github__merge_pull_request`, `mcp__atlassian__jira_transition_issue` |

Keep the allow-list as narrow as possible at each slice so a misbehaving
routine cannot exceed the slice's intended blast radius.

## References

- Epic: SCRUM-381 (PR Gate Automation)
- Workflow change: `.github/workflows/release-readiness.yml` (step `Notify Claude PR Gate routine`)
- FULL_AUTO policy: `docs/agent/workflow-full-auto.md`
- Epic-run policy: `docs/agent/workflow-epic-run.md` (will be updated in SCRUM-386)
