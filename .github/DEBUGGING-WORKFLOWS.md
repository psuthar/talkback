# Debugging GitHub Actions workflows

## 1. Find which step failed (GitHub UI)

1. Repo → **Actions** → click the failed run (e.g. "Observability Agent").
2. Click the failed **job** (e.g. "observability") in the left sidebar or the graph.
3. The **failed step is expanded** and shows its log; scroll to the bottom for the error.

## 2. Get full logs locally (GitHub CLI)

If you use [GitHub CLI](https://cli.github.com/):

```bash
# List recent runs for the Observability Agent workflow
gh run list --workflow=observability-agent.yml -L 5

# View one run (replace RUN_ID with the number from the list)
gh run view RUN_ID

# Stream or download logs for the run
gh run view RUN_ID --log
```

Example: `gh run view 123456789 --log` then search for "exit code" or "Error".

## 3. Run the same steps locally (Observability Agent)

From repo root:

```bash
# 1. Tests (same as workflow)
go test ./...

# 2. Obsworker (set secrets first)
$env:NEW_RELIC_API_KEY = "your_key"   # PowerShell
$env:NEW_RELIC_ACCOUNT_ID = "your_id"
go run ./cmd/obsworker
```

If `go test` fails locally, fix tests first. If `obsworker` fails, the error message (e.g. missing env, NerdGraph error) will point to the cause.

## 4. Common Observability Agent failures

| Step / symptom | Likely cause | Fix |
|----------------|--------------|-----|
| **Run tests** fails | Test failure or flake | Run `go test ./...` locally, fix failing test. |
| **Run obsworker** fails | Missing env, bad API key, or NerdGraph error | Set `NEW_RELIC_API_KEY` and `NEW_RELIC_ACCOUNT_ID` in repo secrets; ensure key has NerdGraph access. |
| **Upload bundle artifacts** fails | No `*-bundle.md` / `*-bundle.json` (obsworker didn’t run or didn’t write) | Obsworker is skipped when secrets are missing; add secrets. If secrets are set, check obsworker step log for errors. |
| **Post bundle to daily Issue** fails | Script error (e.g. labels, API) | Check the step log; create `observability` and `agent` labels in the repo, or the workflow will create the issue without labels. |

## 5. Enable workflow debug logging

To get more verbose logs for a run:

- Repo → **Settings** → **Actions** → **General** → **Debug logging** → enable **Enable debug logging**.
- Re-run the workflow (push or workflow_dispatch). Logs will include more detail; turn debug off when done.
