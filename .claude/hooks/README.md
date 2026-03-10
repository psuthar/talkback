# Claude Hooks (Scaffold)

This directory is reserved for **future automation hooks**. No active hooks are wired up yet; the intent is to add them later without changing product behavior.

---

## Intended future uses

- **Run focused Go tests after backend edits** — e.g. trigger `go test ./internal/handlers/...` when files under `internal/handlers` or `internal/database` change.
- **Format code after changes** — e.g. run `gofmt` or project formatter on touched Go files.
- **Verify migrations exist when schema changes are made** — e.g. remind or check that a migration was added when new columns/tables are introduced.
- **Warn on broad file modifications** — e.g. flag when a large number of files are changed in one go, to encourage smaller, phased edits.

---

## Current state

- No hooks are implemented or invoked.
- Do not wire up active automation until the team agrees on behavior and tooling (e.g. Cursor tasks, pre-commit, or CI).
- This README documents intent only.
