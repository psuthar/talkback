# Skill: epic-run

Policy source: `docs/agent/workflow-epic-run.md`, `docs/agent/workflow-full-auto.md`, `docs/agent/workflow-jira.md`, and `docs/agent/testing-validation.md`.
This skill contains execution tactics and run-state handling, not policy ownership.

## Purpose

Execute all child tickets of a Jira Epic sequentially. **Every** ticket uses **`implement SCRUM-XX FULL_AUTO`** (see **FULL_AUTO mandatory** below)—not plain `implement SCRUM-XX`. That includes **mergeable_state** polling, squash merge when gates pass, and post-merge steps per **CLAUDE.md §8**, overlaid with epic **Final Gate** rules here.

Stop immediately if any ticket's merge gate does not reach `clean` within the polling
budget, **or if the unified PR gate Final Gate is not `PASS`**, or if the gate cannot be read.
Require explicit human instruction to resume.

### Definition of done (per ticket)

For each child: **PR merged to `main`**, Jira issue **Done**, **`mergeable_state`** was **`clean`** at merge time, and **Final Gate** was **`PASS`** before merge (epic). “Production deploy” is **not** part of this skill unless the user explicitly adds it.

### Anti-patterns (common “fix-ups” to avoid)

| Mistake | Why it breaks epic automation | Correct behavior |
|--------|-------------------------------|------------------|
| Stopping after **one** ticket when user said **`continue epic`** | User expects **all** remaining children processed in one go | **Drain** the full remaining list (see **Drain remaining work**) until Jira shows no non-Done children or **HALT** |
| Merging on **`mergeable_state: clean`** only | Standalone FULL_AUTO; epic is stricter | Merge only when **`clean`** **and** **Final Gate `PASS`** (**Merge gate + Final Gate**) |
| Skipping **Final Gate** because checks “look green” | WARN/BLOCK still block epic | Read **`final_gate.status`** or **TalkBack PR Gate** check; non-PASS → **HALT** |
| Starting ticket **N+1** before **N** merged + **Done** | Violates **Sequential close-out** | Wait for merge + Jira **Done**, then new `feat/<next>` from updated **`main`** |
| **`run epic`** when `.epic-run/<EPIC>.json` already exists (non-complete) | State conflict | Use **`continue epic`**, or delete state file only if abandoning |
| Polling **merge gate** too few times | Host “looping” warnings are not a signal to stop | **30s** interval, up to **40 min** per `.cursor/rules/full-auto-github-polling.mdc` |
| After user **squash-merges** (e.g. WARN override), stopping without **Jira Done** + **git cleanup** | User expects **`continue epic`** to finish close-out | Follow **User override: manual squash merge** (merge already done → reconcile only) |
| Not transitioning the **epic issue** to **In Progress** at run start | Epic stays “To Do” while child work is actively running | Transition the epic to **In Progress** before executing the first child ticket (see **Start** algorithm) |
| Not transitioning the **epic issue** to **Done** when all children complete | Epic stays “In Progress” even after full completion | Transition the epic to **Done** as part of **Finish** (see **Finish** section) |

---

## Invocation

Natural-language examples (all equivalent for **behavior**; the user does **not** need to type **`FULL_AUTO`**):

```
run epic SCRUM-XX
continue epic SCRUM-XX
continue epic run for SCRUM-XX
```

- `run epic SCRUM-XX` — start a fresh run (errors if a non-complete state file already exists)
- `continue epic SCRUM-XX` / **`continue epic run for SCRUM-XX`** — continue the epic using the **default epic mode** below (**FULL_AUTO** + **drain remaining work**).

### Default epic mode (no `FULL_AUTO` keyword required)

**Epic runs are always FULL_AUTO in behavior** (merge gate polling, squash merge when allowed, Jira **Done**, sequential close-out). The user **never** has to type **`FULL_AUTO`** alongside `run epic` / `continue epic`; that is implied.

Internally, each child ticket is still executed as **`implement <KEY> FULL_AUTO`** per **CLAUDE.md §8**, plus epic **Final Gate** rules.

### Drain remaining work on every `continue`

When the user says **`continue epic SCRUM-XX`** (or **`continue epic run for SCRUM-XX`**, etc.), **keep going** until one of these is true:

1. **Jira** has **no** child issues of that epic with **`statusCategory != Done`** (all children **Done** — epic work list for open children is empty), or  
2. The run **HALT**s (merge gate / Final Gate / timeout / unrecoverable error).

Do **not** stop after a single merged ticket if **more non-Done children** remain—unless the run **HALT**ed. One user message should **finish all remaining tickets** that can proceed under gates and sequential close-out (subject to session/time limits; if interrupted, user says **`continue epic`** again).

**HALT is always epic-wide and strictly sequential.** When the run halts on ticket N (WARN/BLOCK/timeout), the entire epic stops at that point. `continue epic` resumes **at ticket N**—it does **not** skip ticket N to start ticket N+1. If gates are still WARN/BLOCK on resume, **HALT again immediately** before touching any subsequent ticket.

### FULL_AUTO mandatory (every `implement`)

While **`run epic`** / **`continue epic`** is active, **each** child ticket MUST be executed with **`FULL_AUTO` in the command and in behavior**:

- Use the invocation **`implement <TICKET-KEY> FULL_AUTO`** (e.g. `implement SCRUM-72 FULL_AUTO`).
- **Do not** run **`implement <TICKET-KEY>`** without **`FULL_AUTO`** during an epic—no “standard mode” stop at “PR opened.” Polling **`mergeable_state`**, merge when **`clean`** + **Final Gate PASS**, Jira **Done**, and local cleanup follow **CLAUDE.md §8** FULL_AUTO, plus **Merge gate + Final Gate** below.

The only exception is if the **user explicitly** cancels epic mode or directs a one-off non–FULL_AUTO run; default for epic is always **FULL_AUTO** (whether or not the user typed the word **`FULL_AUTO`**).

---

## Algorithm

### Start (`run epic SCRUM-XX`)

1. Check for `.epic-run/SCRUM-XX.json`. If it exists and `status != "complete"`, refuse to
   start and instruct the user to use `continue epic SCRUM-XX` or delete the file manually.

2. Query Jira for remaining work: `parent = SCRUM-XX AND statusCategory != Done ORDER BY created ASC` (use **`statusCategory`** so “Done” is consistent across workflows).

3. Check each ticket for the parallel marker (see **Sequencing** below) and group into an
   ordered work list of items, where each item is either a single ticket or a parallel batch.

4. Write initial state file (see **State file**).

5. **Jira — epic In Progress** — Transition the **epic issue itself** (e.g. SCRUM-XX) to **In Progress** using `jira_get_transitions` + `jira_transition_issue`. Do this once, immediately before the first child ticket begins executing. If the epic is already **In Progress** (e.g. resumed run), skip this step (idempotent).

6. Execute work list items in order (see **Execution loop**).

### Execution loop (per work-list item)

**Sequential ticket:**

1. **Jira — In Progress** — Transition this ticket to **In Progress** before any code changes (see **In Progress before code** above). Then create/checkout **`feat/<ticket-key>`** from **`main`** so implementation commits land only on that branch.
2. **Test analysis + implementation** — Follow the **Testing** section of CLAUDE.md §8 in full: perform the pre-implementation test analysis, write the identified tests in the same PR, and run affected packages locally before pushing. This applies to every product-code ticket; docs/config-only tickets are exempt. Run `implement SCRUM-XX FULL_AUTO` for the ticket **with epic constraints** (see **Merge gate + Final Gate**): do **not** call `merge_pull_request` until **`mergeable_state: clean`** **and** **`final_gate.status` is `PASS`** (see **Final Gate**). If either fails or times out → **HALT**.

3. Observe terminal outcome:
   - `PASS` — PR merged, `mergeable_state` was `clean`, and **`final_gate.status`** was **`PASS`** at merge time → record in state file, transition the Jira ticket to **Done**, then **only after** **Sequential close-out** is satisfied → continue to the next item (new branch for the next ticket).
   - Any other outcome → **HALT** (see **Halt behavior**).

**Parallel batch (two or more tickets all marked `parallel-ok`):**

1. For each ticket in the batch: transition to **In Progress** and use a dedicated **`feat/<ticket-key>`** branch **before** code changes (**In Progress before code**).
2. For each ticket in the batch: follow the **Testing** section of CLAUDE.md §8 (test analysis, write tests, run locally) before implementation — same rule as sequential tickets.
3. Run `implement SCRUM-XX FULL_AUTO` concurrently for each ticket in the batch (**with epic constraints** per **Merge gate + Final Gate**).
4. Wait for all to terminate.
5. If all PASS (merged with **`final_gate.status: PASS`**) → record all in state file, continue.
6. If any HALT → **HALT** the entire epic run, recording which tickets passed and which halted.

### Finish

When all work-list items complete with PASS:

1. Mark state file `status: "complete"`.
2. **Jira — epic Done** — Transition the **epic issue itself** to **Done** using `jira_get_transitions` + `jira_transition_issue`. This is a mandatory step; do not skip it even if all child issues are already Done.
3. Post a Jira comment on the epic summarizing all merged tickets (keys, PRs, merge SHAs).
4. Report completion to the user.

---

## Sequencing

**Default: sequential.** Every ticket is run one at a time, in creation order.

**Parallel opt-in:** A ticket may carry the label `parallel-ok` in Jira, OR include the
line `Parallel: yes` anywhere in its description. When two or more *consecutive* tickets are
all marked `parallel-ok`, they form a parallel batch and run concurrently.

The agent never infers parallelism. If the ticket doesn't say it, it's sequential.

### In Progress before code (mandatory)

Transition the active child ticket to **In Progress** in Jira **before** any implementation work on that ticket:

- **Not allowed before In Progress:** edits to product code, new or changed tests written for this ticket, implementation commits, or running implementation tests to validate the ticket’s behavior.
- **Allowed before In Progress:** read-only analysis (search, read files, plan).

Use the Jira MCP (`jira_get_transitions`, `jira_transition_issue`) or equivalent. Same rule as **CLAUDE.md §8** (“hard-stop”: no product code or implementation commits until **In Progress** is applied).

### Sequential close-out (mandatory)

Before **`git checkout -b feat/<next-ticket>`** or any other **new implementation branch** for the next child ticket:

1. The **previous** ticket’s PR must be **merged** to `main` (not merely open or “In Review”).
2. The **previous** Jira issue must be **closed out** — transitioned to **Done** (or your project’s equivalent terminal state).

Until both are true, do not start the **next** ticket’s branch or implementation. For the **current** ticket, **`implement … FULL_AUTO`** is responsible for **polling `mergeable_state`**, waiting on checks, and merging when epic gates pass—not stopping at “PR opened.” If FULL_AUTO **HALT**s or the user must intervene, **wait** until the prior ticket is merged and **Done** before the next **`feat/<ticket>`**.

Do not start the next ticket’s implementation in parallel unless the user **explicitly** opts into overlap; default is **strict sequencing**.

---

## Merge gate + Final Gate (epic is stricter than default FULL_AUTO)

Default **FULL_AUTO** (CLAUDE.md §8) may merge when **`mergeable_state: clean`**. **Epic runs must not:** merge until **both** are true:

1. **Merge gate:** `mergeable_state` from `pull_request_read (method: get)` is **`clean`** (same polling rules and 40-minute budget as `.cursor/rules/full-auto-github-polling.mdc`).
2. **Final Gate:** `final_gate.status` from the TalkBack unified gate is exactly **`PASS`**.

If **`final_gate.status`** is **`WARN`**, **`BLOCK`**, or **missing / unreadable** after a reasonable wait aligned with the merge-gate budget → **HALT** without merging. Treat “cannot determine Final Gate” as a halt (same as non-`PASS`).

**Pre-merge guard (mandatory — applies inside epics exactly as in CLAUDE.md §8):** Immediately before calling `merge_pull_request`, perform one final `pull_request_read (method: get)`. Merge **only if** that read returns **`mergeable_state: clean`**. If the final read is anything other than `clean` — including `blocked`, `null`, `unknown`, `unstable`, `behind`, or field absent — **do not merge**; continue polling or HALT per the table. A poll that showed `clean` minutes earlier is not sufficient — the immediate pre-merge read is required every time.

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
| **`WARN`** | **HALT** — agent does **not** auto-merge (epic policy). |
| **`BLOCK`** | **HALT** — agent does **not** auto-merge. |
| Missing / parse error / timeout | **HALT** — do not merge. |

**Note:** Final Gate already combines PR Risk and Release Readiness (`scripts/pr_gate.py`). There is **no** separate epic step to re-read `ops/bundles/report.json` after merge if Final Gate was **PASS** before merge.

**Human override:** The **user** may still **squash-merge** the PR themselves after a **WARN** (or otherwise accept risk and merge despite gate policy). The agent **must not** do that merge during epic HALT; the user does it in GitHub. Afterward, **`continue epic`** must **reconcile** close-out—see **User override: manual squash merge** below—not re-attempt `merge_pull_request` for that PR.

---

## Relation to standalone FULL_AUTO

**Outside an epic**, the user may invoke **`implement SCRUM-XX`** (standard) or **`implement SCRUM-XX FULL_AUTO`**; CLAUDE.md §8 applies.

**Inside an epic**, every ticket is **`implement SCRUM-XX FULL_AUTO`** (mandatory; see **FULL_AUTO mandatory**). The agent **overlays** the **Final Gate `PASS`** requirement and other epic rules here — epic FULL_AUTO is **stricter** than standalone FULL_AUTO, not weaker.

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

## User override: manual squash merge (e.g. after WARN)

When the user **overrides** epic policy and **squash-merges** a PR themselves (typically after a **WARN** halted automation), they will run **`continue epic SCRUM-XX`** and expect **full close-out** for that ticket **without** asking them to click through Jira or clean up git by hand.

On **`continue epic`**, for each **non-Done** child, **first determine whether its PR is already merged** (GitHub MCP: `pull_request_read` / list PRs for the branch or link from Jira/comment).

**If the PR is merged** (user squash-merged):

1. **Do not** call **`merge_pull_request`** for that PR.
2. **Jira:** Transition the child issue to **Done** (or the project’s terminal done state) if it is not already **Done**—including moving past **In Review** when the merge is already on **`main`**.
3. **Remote branch:** If the head branch still exists on the origin (e.g. auto-delete disabled), delete it via **GitHub MCP** when the tool supports it; otherwise instruct the user once.
4. **Local git (mandatory for that ticket’s feature branch):**  
   `git fetch origin && git checkout main && git pull --ff-only origin main` → `git branch -D feat/<TICKET-KEY>` when the local branch exists (same spirit as **CLAUDE.md §8** FULL_AUTO local cleanup).
5. Update **`.epic-run/<EPIC>.json`** for that ticket: set `status: "done"`, `merged_sha` if known, `final_gate: "manual_override"`.
6. **Treat this ticket as fully complete for the purpose of the epic** — the manual merge is accepted as equivalent to a PASS for continuing. **Do not halt or pause here.** Proceed immediately to **Sequential close-out** and then to the **next** child (new `feat/<next>` from current **`main`**). The remaining tickets run under normal epic automation (Final Gate still applies to each new PR).

**If the PR is still open** (user has not merged yet): follow normal **Resume** behavior—poll **`mergeable_state`** + **Final Gate** if pursuing automated merge, or **HALT** again until the user merges or fixes gates.

---

## Resume (`continue epic SCRUM-XX`)

**Treat `continue epic` as a full automation pass**, not a single-ticket retry—same as **Drain remaining work**.

1. **Git:** `git fetch origin && git checkout main && git pull --ff-only origin main` (or equivalent) before creating the next **`feat/<ticket>`** branch so work is based on latest **`main`**.
2. Read `.epic-run/SCRUM-XX.json` if present (if missing, still proceed using Jira as truth).
3. **Jira — epic In Progress guard:** Ensure the epic issue itself is **In Progress** before processing any child ticket. Use `jira_get_transitions` + `jira_transition_issue` if it is still **To Do**. If it is already **In Progress** (or **Done** — which would be unexpected here), skip (idempotent).
4. Re-query Jira: `parent = SCRUM-XX AND statusCategory != Done ORDER BY created ASC` — **source of truth** for remaining work (covers manual merges, manual **Done**, or user-fixed CI).
5. For **each** child in order:
   - If **Done** in Jira → skip (idempotent).
   - If **not Done** but **PR is already merged** (including user **squash merge after WARN**) → run **User override: manual squash merge** close-out (**Jira Done**, branch cleanup, local **`main`**, state file)—**do not** merge again.
   - If **not Done**, PR **open**, and **Final Gate is still WARN/BLOCK** (gates not fixed) → **HALT again immediately.** Do not proceed to any subsequent ticket. Post Jira halt comment. Stop and await human instruction.
   - If **not Done**, PR **open**, and **Final Gate is now PASS** (user fixed CI/gate config) → resume **`mergeable_state`** polling + **Final Gate** check, then merge when allowed (epic rules).
   - If **not Done**, no merged PR, no implementation yet → run **`implement <KEY> FULL_AUTO`** with epic constraints.
6. **HALT** if any step fails per **Halt behavior**; otherwise repeat until no children match the JQL or epic marked complete.

**Reconcile Jira with GitHub:** If **`main`** already contains the change but Jira lags (**In Review** / not **Done**), transition via Jira MCP—**do not** re-implement or open a duplicate PR.

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

- **FULL_AUTO every ticket (default):** During epic execution, always use **`implement SCRUM-XX FULL_AUTO`** per child ticket (see **FULL_AUTO mandatory**). Do not substitute standard **`implement SCRUM-XX`**. The user does **not** need to repeat **`FULL_AUTO`** in chat—**`continue epic`** already implies it.
- **Continue = drain:** On **`continue epic SCRUM-XX`** (or **`continue epic run for SCRUM-XX`**), process **all** remaining non-Done children in order until done or **HALT** (see **Drain remaining work**).
- **Epic In Progress at run start:** Transition the **epic issue itself** to **In Progress** (via `jira_get_transitions` + `jira_transition_issue`) before the first child ticket begins executing. On `continue epic`, re-apply if the epic is still **To Do** (idempotent if already **In Progress**).
- **Epic Done at finish:** Transition the **epic issue itself** to **Done** as part of the **Finish** step, after all child tickets are Done and before posting the summary comment. This is mandatory — do not skip even if children are already Done.
- **In Progress first (child tickets):** Transition each child Jira ticket to **In Progress** before product code edits or implementation commits (see **In Progress before code**).
- **Tests with code:** Every product-code ticket requires a pre-implementation test analysis (see **Execution loop** step 2) and must ship with the identified tests written and passing locally before the PR is pushed. Documentation-only or config-only tickets are exempt. CI is not a substitute for running tests locally first.
- Never skip a ticket silently. If a ticket cannot be implemented (missing description,
  unresolvable dependency), HALT and report.
- Never **agent-merge** without **`mergeable_state: clean`** and **`final_gate.status: PASS`** (epic). **Exception:** the user may merge manually (e.g. squash after **WARN**); then **`continue epic`** must **reconcile** (**User override: manual squash merge**)—no second merge, but **Jira Done** + **git cleanup** are still required.
- Never self-resume after a halt, even if the reason appears transient.
- Parallel batches must all complete before the next sequential ticket starts.
- **No new implementation branch for ticket N+1** until ticket N’s PR is **merged** and ticket N is **Done** in Jira (see **Sequential close-out** above).
- Do not modify already-Done tickets (idempotent on restart).
