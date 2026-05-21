# FULL_AUTO orchestration scripts — runbook

Owner: SCRUM-529 (Epic). Phase 0 ticket: SCRUM-528.

This is the operational runbook for `scripts/full_auto/`, the Python package that extracts deterministic chunks of the `implement SCRUM-XX FULL_AUTO` workflow so they can be called from Claude Code today and from a webhook listener later.

> **Phase 0 only ships the credential plumbing.** No behaviour change. Subsequent phases (SCRUM-530 `close.py`, SCRUM-531 dry-run validation, SCRUM-532 CLAUDE.md cut-over) build on this foundation.

---

## Deployment-target matrix

The same Python code runs in three different environments. The only difference is how the environment variables are populated.

| Where | When | `GITHUB_TOKEN` from | `ATLASSIAN_EMAIL` / `ATLASSIAN_API_TOKEN` from |
|---|---|---|---|
| Local laptop (you) | Today, replacing manual close-out steps | Auto-fallback to `gh auth token` (already logged in) | `.env.local` sourced into the shell (or via `direnv`) |
| Webhook listener (future) | Cloudflare tunnel → local Flask app | Either `gh auth token` (if the listener runs as you) OR an env var | Listener's environment, populated from disk |
| GitHub Actions (future) | Scheduled / event-triggered runs | `secrets.GITHUB_TOKEN` (auto-issued, repo-scoped) | Repo secrets injected as env vars |

All three call `scripts/full_auto/lib/auth.py` with the same code path.

---

## Local setup

### 1. Copy the example file

```sh
cp .env.local.example .env.local
```

`.env.local` is explicitly listed in `.gitignore` (added at SCRUM-528 — the bare `.env` rule is a literal-match and does NOT cover `.env.local`). **Never commit it.**

### 2. Generate an Atlassian API token

Visit https://id.atlassian.com/manage-profile/security/api-tokens.

**Recommendations:**
- Use the **scoped** form (not the legacy full-access form).
- Grant only `read:jira-work` + `write:jira-work`. The FULL_AUTO scripts need to read tickets, post comments, and transition issues — nothing more.
- Set an expiry of **90 days**. Atlassian doesn't auto-rotate; you'll get an email at expiry.

Copy the token (only shown once at creation).

### 3. Populate `.env.local`

```
ATLASSIAN_EMAIL=paresh+talkback_ai@suthar.com
ATLASSIAN_API_TOKEN=ATATT3xFfGF0...
# ATLASSIAN_BASE_URL=  (optional; defaults to TalkBack's tenant)
# GITHUB_TOKEN=        (optional; gh auth token fallback handles this)
```

### 4. Source before running scripts

Two options:

**Manual:**

```sh
set -a; source .env.local; set +a
python -m scripts.full_auto.close SCRUM-XX --pr N    # not yet shipped — Phase 1
```

**direnv (recommended):**

```sh
brew install direnv   # if needed
echo "dotenv .env.local" > .envrc
direnv allow
```

Then any time you `cd` into the repo, the env loads automatically.

---

## Verifying the auth module

```sh
python3 -c "from scripts.full_auto.lib.auth import github_token, jira_auth, atlassian_base_url; \
            print('github_token: present' if github_token() else 'missing'); \
            print('jira_auth:', jira_auth()[0]); \
            print('atlassian_base_url:', atlassian_base_url())"
```

If credentials are wrong, the helpers raise with an actionable error message pointing at the right fix.

---

## Token rotation

| Token | Auto-rotation? | When you'll know it expired |
|---|---|---|
| `ATLASSIAN_API_TOKEN` | No | Scripts return 401 on next Jira call |
| `gh auth login` token | Refreshes on use; ~no expiry in practice | N/A |
| `GITHUB_TOKEN` (CI) | Per-workflow-run | Doesn't apply outside CI |

**Rotation procedure for Atlassian:**
1. Generate a new scoped token at the link above (set new 90-day expiry).
2. Update `.env.local` (or your repo secret).
3. Revoke the old token in the Atlassian token list.

---

## How Claude uses close.py in the FULL_AUTO flow (Phase 3 cut-over)

Once the gate reaches PASS + mergeable clean, Claude runs **one command** instead of the ~14 individual tool calls it used to make:

```sh
python -m scripts.full_auto.close <TICKET> --pr <N> --path polling
```

`close.py` performs the full close-out sequence in order:

1. **Pre-merge guard** — re-reads the PR via the GitHub API. If `mergeable_state` is not `clean` (and the PR is not already merged), aborts with a structured reason; nothing else runs. Output: `actions_taken` lists the abort.
2. **Merge** — `PUT /pulls/<N>/merge` with `merge_method: squash`. Skipped when the PR is already merged at entry (manual-override case below).
3. **Local git** — `fetch origin --prune` → `checkout main` → `pull --ff-only origin main` → `branch -D feat/<TICKET>`. The new main SHA is captured into `result.main_sha_after` for the closure comment.
4. **State file** — looks for `.epic-run/<EPIC>.json` referencing the ticket; if found, marks the entry `status: done` + `merged_sha` + `final_gate`. No-op if no state file references this ticket.
5. **Jira → Done** — resolves the Done transition id dynamically (by name, not hardcoded id) and posts the transition.
6. **Closure comment** — renders the polling-path template with all collected values, posts it.

`close.py` returns a `CloseResult` dataclass. Claude relays its `actions_taken` list to the user as the chat-surface confirmation. The user sees one structured summary, not 14 separate "did X" lines.

### Path indicators

| `--path` value | When to use | Closure-template shape |
|---|---|---|
| `polling` | Default for `implement SCRUM-XX FULL_AUTO` — Claude observed PASS + clean and is performing the merge. | "FULL_AUTO complete — polling path (default). PR #N squash-merged at ..." |
| `manual-override` | User squash-merged manually (typically after a structural WARN they accepted). Claude is reconciling, not merging. | "Closure (user-override squash-merge path). PR #N squash-merged by user at ..." |
| `webhook` | Future — when the Cloudflare-tunnel webhook listener invokes close.py. | "FULL_AUTO complete — webhook path (FULL_AUTO_WEBHOOK). Local cleanup skipped..." |

### Failure modes

| Symptom | Most likely cause | Fix |
|---|---|---|
| `RuntimeError: Missing Jira credentials.` | `.env.local` not sourced | `set -a; source .env.local; set +a` or set up direnv |
| `RuntimeError: PUT /pulls/N/merge -> 405: ...` | PR is not mergeable (closed, has conflicts, branch protection failed) | Check `gh pr view N` for state; manual override may be required |
| `RuntimeError: No 'Done' transition available on SCRUM-X` | Ticket workflow doesn't expose a Done transition (e.g., already Done, or in a non-standard state) | Check current Jira status; transition manually if needed |
| `aborted_reason: "mergeable_state was 'blocked' not 'clean'"` | Pre-merge guard fired; PR state changed between polling and the guard re-read | Investigate the PR; usually a CI flake or a required check that re-triggered |

### What stays Claude's responsibility (NOT delegated to close.py)

- The **completion comment** posted at step 8 of `workflow-jira.md` (when the PR opens and Jira → In Review). That's per-ticket narrative; close.py's closure comment is the uniform shape at step 12.
- The **WARN/BLOCK halt** path. close.py is the PASS path; halt.py is a future Epic.
- The **worktree-based cleanup** (ExitWorktree + SCRUM-388 primary-tree FF). close.py only handles flat-checkout cleanup; worktree runs follow the prose in `workflow-full-auto.md`.
- **Test analysis and implementation** (steps 4-5 of FULL_AUTO). The agentic part of the loop.

## What's coming next (forward links)

- **SCRUM-530 / Phase 1** ✓ done — `scripts/full_auto/close.py` shipped.
- **SCRUM-531 / Phase 2** ✓ done — dry-run validation against 5 historical FULL_AUTOs; zero unresolved drift; see `ops/full-auto-validation/SUMMARY.md`.
- **SCRUM-532 / Phase 3** — this section. CLAUDE.md / workflow docs updated.
- **(Future, separate Epic)** — Cloudflare-tunnel webhook listener that imports `close.py` for the post-gate close-out path. De-risked by Phase 0-3.

---

## Troubleshooting

**`RuntimeError: No GitHub credentials available.`**

The script tried `GITHUB_TOKEN` env var (unset) and `gh auth token` (failed). Run `gh auth status` to diagnose. Re-authenticate with `gh auth login` if needed.

**`RuntimeError: Missing Jira credentials.`**

Either `ATLASSIAN_EMAIL` or `ATLASSIAN_API_TOKEN` is unset or empty. Check `.env.local` is sourced (`echo $ATLASSIAN_EMAIL` should show it). If using direnv, run `direnv reload`.

**Token works locally but not in CI.**

Repo secrets are scoped per environment. Verify the secret name matches (`ATLASSIAN_API_TOKEN`, exact case) and is granted to the workflow's environment.

---

## Phase 0 scope guarantee

This ticket lands:
- `scripts/full_auto/__init__.py`
- `scripts/full_auto/lib/__init__.py`
- `scripts/full_auto/lib/auth.py` (~80 LOC)
- `scripts/test_full_auto_auth.py` (~150 LOC, 12 tests)
- `.env.local.example` (committed template)
- This runbook (`docs/agent/full-auto-scripts.md`)

**Zero behaviour change.** Claude continues to do close-out via MCPs + bash tool calls. The auth module is foundation only; nothing imports it yet.

That changes in Phase 1.
