# Test suite diagnostic report (read-only)

**Generated:** 2026-04-11  
**Scope:** Full Go suite (smoke-fixer style) + Playwright E2E (e2e-fixer style). **No code fixes** were applied during this run.

---

## Executive summary

| Suite | Result | Notes |
|-------|--------|--------|
| **Go** (`go test ./...`) | **PASS** | With `DATABASE_URL` pointing at local Postgres (CI-style). |
| **Playwright E2E** (`web/npm run test:e2e`) | **FAIL** | Environment: wrong Chromium arch for Apple Silicon + no API/app running; 19 failed, 9 did not run. |

---

## 1. Go tests (smoke / integration / full tree)

### Command

```bash
docker compose -f deploy/docker-compose.yml up -d postgres   # postgres already running
DATABASE_URL=postgres://talkback:talkback@127.0.0.1:5432/talkback?sslmode=disable \
  go test ./... -count=1 -timeout=15m
```

### Result

- **Exit code:** 0  
- **Failures:** None observed in this run.

### Skipped tests

| Package | Test | Reason |
|---------|------|--------|
| `internal/utils` | `TestWhisperTranscriber_TranscribeFile` | Skips when `OPENAI_API_KEY` unset or Whisper not configured (`t.Skip`). |

### Gaps / observations (Go)

- **Optional OpenAI:** Whisper integration test is intentionally skipped without keys — not a failure.
- **Packages with no `*_test.go`:** Many packages (e.g. parts of `cmd/`, `internal/citation`, `internal/email`, `internal/storage`, `internal/zoom`, …) have no tests — coverage gap, not a failing test.
- **Slower packages (approximate):** `internal/handlers` (~17s), `internal/debugfault` (~15s), `internal/database` (~7s), others in the ~3–4s range.
- **Flakiness:** Not evaluated (single run; all green).

---

## 2. Playwright E2E (`web`)

### Commands

```bash
cd web && npm install && npx playwright install chromium && npm run test:e2e
```

### Result

- **Exit code:** 1  
- **Scheduled:** 28 tests — **19 failed**, **9 did not run**.

### Primary failure modes

1. **Browser binary / architecture**  
   - Error pattern: `Executable doesn't exist at .../chrome-headless-shell-mac-arm64/...`  
   - `npx playwright install chromium` resolved **mac-x64** browser bundles while the runner on **Apple Silicon** expects **arm64** headless shell — install arch mismatch.

2. **API not running**  
   - `connect ECONNREFUSED 127.0.0.1:8081` on `POST .../api/auth/login` (and similar).  
   - Default API base in `web/tests/e2e/fixtures.ts` is **`http://localhost:8081`** (`TALKBACK_API_BASE`). Nothing was listening during the diagnostic run.

3. **Global teardown**  
   - Teardown also calls the API for cleanup; failed with **ECONNREFUSED** when API was absent.

### Failing / blocked tests (titles from Playwright output)

Includes (non-exhaustive): `creator-access`, `invite-invalid-token`, `material-processing-state` (3), `material-viewers`, `orchestration-creator`, `participant-acceptance`, `participant-happy-path`, `pptx-polling-stop`, `qa-history`, `session-availability`, `session-routing` (multiple cases), etc.

### Gaps / observations (E2E)

| Topic | Finding |
|-------|---------|
| **Stack requirement** | `playwright.config.ts` has **no `webServer`** — API and Vite must be started **manually** before E2E (see `web/tests/e2e/README.md`). |
| **Port documentation drift** | **8080** vs **8081** appears in different places: `playwright.config.ts` comment says API **8080**; `fixtures.ts` / E2E README default **8081**; `web/README.md` references **8080** for API/Vite proxy. This is a **consistency gap** for developers and CI. |
| **Apple Silicon** | Ensure Node/playwright install path selects **arm64** browsers, or document using Rosetta/x64 Node consistently. |
| **Assertion quality** | UI selectors and flakes **were not meaningfully exercised** — failures were at **launch** and **network** layers first. |

### What a “green” local E2E run likely needs

- Correct Playwright **Chromium** for host arch (**arm64** on M-series Macs).  
- API listening on the URL used by **`TALKBACK_API_BASE`** (resolve **8080 vs 8081** explicitly).  
- Web app on **`http://localhost:3000`** (per typical Vite setup).  
- Bootstrap/admin steps per `web/tests/e2e/README.md` where required.

---

## 3. Recommended follow-ups (not done in this pass)

1. **E2E:** Align and document a **single** default API port (8080 vs 8081) across `playwright.config.ts`, `fixtures.ts`, `web/README.md`, and E2E README.  
2. **E2E:** Re-run `npm run test:e2e` with API + web up and correct Playwright browsers on **arm64**.  
3. **Go:** Optionally run with `OPENAI_API_KEY` set to execute `TestWhisperTranscriber_TranscribeFile` in CI or a dedicated job.  
4. **Coverage:** Track packages with **no tests** as backlog if product-critical.

---

## 4. CI reference

- Go: `.github/workflows/go-test.yml` runs `go test ./...` with `DATABASE_URL` to `localhost:5432` Postgres service.
