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

## What's coming next (forward links)

- **SCRUM-530 / Phase 1** — `scripts/full_auto/close.py` lands. Will replace ~14 individual tool calls Claude makes in the close-out half of every FULL_AUTO. Imports from `lib/auth.py` (this ticket).
- **SCRUM-531 / Phase 2** — dry-run mode validates `close.py` against actual Claude behaviour before cut-over. Captures dry-run output for 3-5 FULL_AUTOs, resolves drift.
- **SCRUM-532 / Phase 3** — CLAUDE.md / `workflow-jira.md` / `workflow-full-auto.md` rewritten to call `close.py` as the canonical post-merge close-out.
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
