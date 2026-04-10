# Skill: epic-run

## Purpose

Execute all child tickets of a Jira Epic sequentially using the `implement SCRUM-XX FULL_AUTO`
workflow. Stop immediately if any ticket's CI gate does not reach `clean` within the polling
budget, or if the release readiness check produces WARN or BLOCK. Require explicit human
instruction to resume.

---

## Invocation

```
run epic SCRUM-XX
continue epic SCRUM-XX
```

- `run epic SCRUM-XX` — start a fresh run (errors if a non-complete state file already exists)
- `continue epic SCRUM-XX` — resume from a halted run

---

## Algorithm

### Start (`run epic SCRUM-XX`)

1. Check for `.epic-run/SCRUM-XX.json`. If it exists and `status != "complete"`, refuse to
   start and instruct the user to use `continue epic SCRUM-XX` or delete the file manually.

2. Query Jira: `parent = SCRUM-XX AND status != Done ORDER BY created ASC`.

3. Check each ticket for the parallel marker (see **Sequencing** below) and group into an
   ordered work list of items, where each item is either a single ticket or a parallel batch.

4. Write initial state file (see **State file**).

5. Execute work list items in order (see **Execution loop**).

### Execution loop (per work-list item)

**Sequential ticket:**

1. Run `implement SCRUM-XX FULL_AUTO` for the ticket.
2. Observe FULL_AUTO terminal outcome:
   - `PASS` (`mergeable_state: clean`, merged) → record in state file, continue to next item.
   - Any other outcome → **HALT** (see **Halt behavior**).

**Parallel batch (two or more tickets all marked `parallel-ok`):**

1. Run `implement SCRUM-XX FULL_AUTO` concurrently for each ticket in the batch.
2. Wait for all to terminate.
3. If all PASS → record all in state file, continue.
4. If any HALT → **HALT** the entire epic run, recording which tickets passed and which halted.

### Finish

When all work-list items complete with PASS:

1. Mark state file `status: "complete"`.
2. Post a Jira comment on the epic summarizing all merged tickets (keys, PRs, merge SHAs).
3. Report completion to the user.

---

## Sequencing

**Default: sequential.** Every ticket is run one at a time, in creation order.

**Parallel opt-in:** A ticket may carry the label `parallel-ok` in Jira, OR include the
line `Parallel: yes` anywhere in its description. When two or more *consecutive* tickets are
all marked `parallel-ok`, they form a parallel batch and run concurrently.

The agent never infers parallelism. If the ticket doesn't say it, it's sequential.

---

## CI gate

The CI gate is `mergeable_state` from `pull_request_read (method: get)` via GitHub MCP —
exactly as defined in CLAUDE.md §8 FULL_AUTO. No additional review or smoke gate is added
here; FULL_AUTO already polls through `blocked` and merges on `clean`.

A FULL_AUTO outcome of anything other than `PASS` (clean merge) is treated as a halt
condition for the epic run.

---

## Release readiness

After every successful merge (`PASS`), the agent checks whether the CI run that gated the
merge produced a release-readiness WARN or BLOCK (see `ops/release-readiness/decision-flow.md`).

The release-readiness result is available in `ops/bundles/` after the GitHub Actions
`release-readiness` workflow completes. Read `ops/bundles/report.json` from the
`main` branch (or the workflow artifact) after each merge.

| Release readiness outcome | Action |
|---|---|
| `PASS` | Continue to next ticket |
| `WARN` | **HALT** — post status, require human to continue |
| `BLOCK` | **HALT** — post status, require human to continue |
| File not found / unreadable | Treat as `WARN` — HALT and note the missing report |

---

## Halt behavior

On any halt condition:

1. Write halt state to `.epic-run/SCRUM-XX.json` (set `status: "halted"`, populate
   `halted_at`, `halt_reason`, `awaiting_human: true`).
2. Post a Jira comment on the **epic** with:
   - Tickets completed so far (key, PR URL, merge SHA)
   - Halted ticket + reason (FULL_AUTO outcome or release-readiness result)
   - Remaining tickets not yet started
   - Instruction: "Resume with `continue epic SCRUM-XX` once the blocker is resolved."
3. **Stop completely.** Do not proceed to the next ticket, do not poll, do not self-resume.

---

## Resume (`continue epic SCRUM-XX`)

1. Read `.epic-run/SCRUM-XX.json`. If `awaiting_human` is not `true`, refuse and report.
2. Re-query Jira for child tickets — any now in Done are treated as complete regardless of
   state file (handles manual merges or out-of-band work).
3. Determine resume point:
   - If the halted ticket's PR was manually merged → advance to the next pending ticket.
   - Otherwise → re-run `implement SCRUM-XX FULL_AUTO` on the halted ticket.
4. Continue the execution loop from the resume point.

---

## State file

Location: `.epic-run/<EPIC-KEY>.json` (gitignored).

```json
{
  "epic": "SCRUM-29",
  "run_id": "<ISO-8601 timestamp of run start>",
  "status": "running | halted | complete",
  "awaiting_human": false,
  "halted_at": null,
  "halt_reason": null,
  "tickets": [
    {
      "key": "SCRUM-43",
      "status": "done",
      "pr": 72,
      "merged_sha": "abc123",
      "release_readiness": "PASS"
    },
    {
      "key": "SCRUM-46",
      "status": "halted",
      "pr": 79,
      "merged_sha": null,
      "halt_reason": "blocked_budget_expired"
    }
  ],
  "next_pending": ["SCRUM-47", "SCRUM-48"]
}
```

---

## Constraints

- Never skip a ticket silently. If a ticket cannot be implemented (missing description,
  unresolvable dependency), HALT and report.
- Never merge without FULL_AUTO's `mergeable_state: clean` confirmation.
- Never self-resume after a halt, even if the reason appears transient.
- Parallel batches must all complete before the next sequential ticket starts.
- Do not modify already-Done tickets (idempotent on restart).
