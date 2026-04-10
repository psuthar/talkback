# Skill: epic-run

## Purpose

Execute all child tickets of a Jira Epic sequentially using the `implement SCRUM-XX FULL_AUTO`
workflow. Stop immediately if any ticket's merge gate does not reach `clean` within the polling
budget, **or if the unified PR gate Final Gate is not `PASS`**, or if the gate cannot be read.
Require explicit human instruction to resume.

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

1. When the ticket changes **product code** (Go, frontend, etc.), add or update **automated tests** in the same change set: cover new behavior, regressions, and critical branches per existing repo patterns (`go test`, package tests, etc.). **Documentation-only** tickets are exempt. Do not merge implementation without meaningful test coverage where the codebase normally tests similar code.
2. Run `implement SCRUM-XX FULL_AUTO` for the ticket **with epic constraints** (see **Merge gate + Final Gate**): do **not** call `merge_pull_request` until **`mergeable_state: clean`** **and** **`final_gate.status` is `PASS`** (see **Final Gate**). If either fails or times out → **HALT**.
3. Observe terminal outcome:
   - `PASS` — PR merged, `mergeable_state` was `clean`, and **`final_gate.status`** was **`PASS`** at merge time → record in state file, continue to next item.
   - Any other outcome → **HALT** (see **Halt behavior**).

**Parallel batch (two or more tickets all marked `parallel-ok`):**

1. Same **tests-with-code** rule as sequential tickets (each batch item).
2. Run `implement SCRUM-XX FULL_AUTO` concurrently for each ticket in the batch (**with epic constraints** per **Merge gate + Final Gate**).
3. Wait for all to terminate.
4. If all PASS (merged with **`final_gate.status: PASS`**) → record all in state file, continue.
5. If any HALT → **HALT** the entire epic run, recording which tickets passed and which halted.

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

## Merge gate + Final Gate (epic is stricter than default FULL_AUTO)

Default **FULL_AUTO** (CLAUDE.md §8) may merge when **`mergeable_state: clean`**. **Epic runs must not:** merge until **both** are true:

1. **Merge gate:** `mergeable_state` from `pull_request_read (method: get)` is **`clean`** (same polling rules and 40-minute budget as `.cursor/rules/full-auto-github-polling.mdc`).
2. **Final Gate:** `final_gate.status` from the TalkBack unified gate is exactly **`PASS`**.

If **`final_gate.status`** is **`WARN`**, **`BLOCK`**, or **missing / unreadable** after a reasonable wait aligned with the merge-gate budget → **HALT** without merging. Treat “cannot determine Final Gate” as a halt (same as non-`PASS`).

**IDE anti-loop warnings:** While waiting on CI and gate artifacts, the agent will repeat **30s sleep** + PR read / artifact fetch many times. That is **correct** behavior — do not abort early because the host flags “looping.”

---

## Final Gate (how to read it)

**Source of truth:** `pr-gate-summary.json` written by `scripts/pr_gate.py` in the **`release-readiness`** GitHub Actions workflow (unified PR Risk + Release Readiness). Read the field **`final_gate.status`**.

**How to obtain it for the open PR (in order of preference):**

1. **Workflow artifact** — Download **`pr-gate-summary.json`** from the **`release-readiness`** workflow run associated with the PR head commit or branch (GitHub MCP or API: run listing + artifact download).
2. **TalkBack PR Gate check** — If artifacts are awkward, use **`pull_request_read` `get_check_runs`** on the PR head and find the check named **`TalkBack PR Gate`**: conclusion **`success`** corresponds to Final Gate **PASS**; **`action_required`** or **`failure`** means Final Gate is **not** PASS (treat as halt for epic; do not merge).

**Semantics:**

| `final_gate.status` (or equivalent check) | Epic action |
|---|---|
| **`PASS`** | Eligible to merge **if** `mergeable_state` is also **`clean`**. |
| **`WARN`** | **HALT** — do not merge. |
| **`BLOCK`** | **HALT** — do not merge. |
| Missing / parse error / timeout | **HALT** — do not merge. |

**Note:** Final Gate already combines PR Risk and Release Readiness (`scripts/pr_gate.py`). There is **no** separate epic step to re-read `ops/bundles/report.json` after merge if Final Gate was **PASS** before merge.

---

## Relation to standalone FULL_AUTO

When the user invokes **`implement SCRUM-XX FULL_AUTO` outside an epic**, CLAUDE.md §8 applies as written. When **`run epic` / `continue epic`** is active, the agent **overlays** the **Final Gate `PASS`** requirement above — epic and standalone FULL_AUTO are **not** identical.

---

## Halt behavior

On any halt condition:

1. Write halt state to `.epic-run/SCRUM-XX.json` (set `status: "halted"`, populate
   `halted_at`, `halt_reason`, `awaiting_human: true`).
2. Post a Jira comment on the **epic** with:
   - Tickets completed so far (key, PR URL, merge SHA)
   - Halted ticket + reason (merge gate / **`final_gate.status` not `PASS`** / timeout / parse error)
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
      "final_gate": "PASS"
    },
    {
      "key": "SCRUM-46",
      "status": "halted",
      "pr": 79,
      "merged_sha": null,
      "halt_reason": "final_gate_warn"
    }
  ],
  "next_pending": ["SCRUM-47", "SCRUM-48"]
}
```

---

## Constraints

- **Tests with code:** Any implementation work must include **corresponding tests** (new or updated) unless the ticket is strictly docs/config with no executable behavior. Validate with the same commands the project uses for CI (e.g. `go test ./...` for touched packages).
- Never skip a ticket silently. If a ticket cannot be implemented (missing description,
  unresolvable dependency), HALT and report.
- Never merge without **`mergeable_state: clean`** and **`final_gate.status: PASS`** (epic); do not use default FULL_AUTO merge rules alone during an epic run.
- Never self-resume after a halt, even if the reason appears transient.
- Parallel batches must all complete before the next sequential ticket starts.
- Do not modify already-Done tickets (idempotent on restart).
