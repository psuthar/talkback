# Fault Injection Testing for Observability

This document describes how to use the **test-only fault injection harness** to generate real observability signals for validating obsworker and AI analysis.

## Safety

- **Disabled by default.** Fault routes return 404 unless `OBS_TEST_MODE=true`.
- **Optional token guard.** When `OBS_TEST_TOKEN` is set, callers must send `X-Obs-Test-Token: <value>`.
- **Isolated.** R2 fault route uses a temporary client; it does not affect the app's real R2 client or secrets.

## Enabling Test Mode

1. Set `OBS_TEST_MODE=true` when starting the API server.
2. (Optional) Set `OBS_TEST_TOKEN` to a shared secret and pass it via `--token` to obsfault.

```bash
OBS_TEST_MODE=true OBS_TEST_TOKEN=my-secret go run ./cmd/api
```

## Fault Routes

All routes live under `/debug/fault/` and require `POST`.

| Route | Purpose |
|-------|---------|
| `POST /debug/fault/error` | Returns HTTP 500 with synthetic message |
| `POST /debug/fault/latency?ms=1500` | Sleeps for `ms` ms, returns 200 |
| `POST /debug/fault/r2?mode=auth` | Triggers controlled R2 failure |

### R2 Modes

- `auth` — invalid credentials (default)
- `notfound` — non-existent bucket
- `endpoint` — invalid endpoint URL
- `timeout` — very short context timeout

## obsfault CLI

The `obsfault` CLI repeatedly hits fault routes and prints a summary.

```bash
go run ./cmd/obsfault --scenario error --count 20
go run ./cmd/obsfault --scenario latency --count 20 --latency-ms 1500
go run ./cmd/obsfault --scenario r2 --count 10 --r2-mode auth
```

With token:

```bash
go run ./cmd/obsfault --base-url http://localhost:8080 --token my-secret --scenario error --count 20
```

## Testing Matrix

### Scenario A — Forced 500

**Trigger:**
```bash
obsfault --scenario error --count 20
```

**Expected:**
- RED status
- Top errors populated in bundle
- AI should identify isolated app exception path

### Scenario B — Latency spike

**Trigger:**
```bash
obsfault --scenario latency --count 20 --latency-ms 1500
```

**Expected:**
- YELLOW or RED
- p95 increase
- Deep dive shows `/debug/fault/latency`

### Scenario C — R2 failure

**Trigger:**
```bash
obsfault --scenario r2 --count 10 --r2-mode auth
```

**Expected:**
- RED
- Storage-related errors in bundle
- AI should infer downstream storage/config/auth issue

## Validation Sequence

1. Start API with `OBS_TEST_MODE=true` (and optional `OBS_TEST_TOKEN`).
2. Run `obsfault` for each scenario.
3. Run obsworker: `go run ./cmd/obsworker`
4. Confirm:
   - Bundle status (YELLOW/RED as expected)
   - Deep dive usefulness
   - AI analysis specificity (when `OBS_ENABLE_AI_ANALYSIS=true`)

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_TEST_MODE` | `false` | Must be `true` to enable fault routes |
| `OBS_TEST_TOKEN` | (unset) | When set, require `X-Obs-Test-Token` header |
