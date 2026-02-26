# TalkBack Makefile
# Run from repo root.

.PHONY: auth-check

# Mission 1 local auth checkpoint: 7-step verification (DB, server, signup, logout, login, disabled user, bootstrap admin).
# Requires: server running (e.g. go run ./cmd/api), Postgres with migrations applied.
# Optional: set TB_BASE_URL (default http://localhost:8081), TB_COOKIE_NAME (default tb_login), DATABASE_URL or TB_PSQL_DSN.
auth-check:
	bash scripts/auth_check.sh
