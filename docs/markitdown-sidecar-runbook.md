# MarkItDown sidecar — operator runbook

Operational guide for the Python sidecar deployed alongside `talkback` (SCRUM-305). Service code lives at `services/markitdown-sidecar/`; Go client lives at `internal/markitdown/`. Deployment is defined in the root `render.yaml`.

## At a glance

| Service               | Path                          | Health                  |
|-----------------------|-------------------------------|-------------------------|
| `markitdown-sidecar`  | `services/markitdown-sidecar` | `GET /healthz` (200)    |
| Backend integration   | `internal/markitdown/`        | `Client.Enabled()`      |

Two feature flags on `talkback` gate user-visible behavior:
- `MARKITDOWN_IMAGE_EXTRACTION_ENABLED` — image captioning for `kind=image` materials.
- `MARKITDOWN_URL_EXTRACTION_ENABLED` — sidecar-first link extraction (with Go-path fallback on failure).

Both default `false`. Either disabled (or sidecar unconfigured) → legacy behavior.

## First-time rollout

1. **Deploy the sidecar** (Render → Apply Blueprint with the updated `render.yaml`). Confirm `markitdown-sidecar` reaches `live` status; `GET <url>/healthz` should return `{"status":"ok","version":"<sha>"}`.
2. **Set required secrets** on `markitdown-sidecar`:
   - `SIDECAR_SECRET`: 32+ random bytes (`openssl rand -hex 32`). The bearer token. Never commit.
   - `OPENAI_API_KEY`: required by `/extract/image` for vision LLM calls.
3. **Wire the backend** on `talkback`:
   - `MARKITDOWN_SIDECAR_URL`: the sidecar's Render service URL (from the dashboard).
   - `MARKITDOWN_SIDECAR_SECRET`: same value as `SIDECAR_SECRET` above.
4. **Verify integration without flipping flags.** With both extraction flags still `false`, deploy `talkback`. The startup log should read: `Markitdown sidecar client enabled (base=..., image_extraction=false)`. No user-visible behavior changes; flags are still off.
5. **Flip one flag at a time.** Set `MARKITDOWN_IMAGE_EXTRACTION_ENABLED=true` on `talkback` (Dashboard → Save → service redeploys). Upload an image-bearing material in staging; confirm `text_status` reaches `ready` with non-empty `extracted_text`.
6. **Then URL extraction.** Set `MARKITDOWN_URL_EXTRACTION_ENABLED=true`. Add a structured URL (with headings/tables) to a session; confirm the link's chunked content preserves the markdown structure.

## Rollback

Three levers, fastest first:

1. **Flag flip** (~30s redeploy on `talkback`): set `MARKITDOWN_IMAGE_EXTRACTION_ENABLED=false` and/or `MARKITDOWN_URL_EXTRACTION_ENABLED=false`. Backend reverts to legacy behavior immediately on next request. No restart on the sidecar.
2. **Disconnect**: clear `MARKITDOWN_SIDECAR_URL` on `talkback`. `Client.Enabled()` returns false, and both extraction paths short-circuit.
3. **Service stop**: pause `markitdown-sidecar` in Dashboard. Backend's URL extraction transparently falls back to the Go DOM walker (logged as `markitdown_sidecar_fallback`); image extraction marks new uploads `text_status=failed` with a clear error_message.

Material rows already populated by the sidecar before rollback are safe — the extracted_text remains valid markdown.

## Secret rotation (`SIDECAR_SECRET`)

Both services must agree. Coordinated update:

1. Generate new value: `openssl rand -hex 32 | pbcopy`.
2. Set `SIDECAR_SECRET` (sidecar) AND `MARKITDOWN_SIDECAR_SECRET` (backend) to the new value in Dashboard. Save both.
3. Both services redeploy. There is a brief window (seconds) where one is on the old secret and the other on the new — the backend's calls during that window return `ErrSidecarUnauthorized` and (for URL extraction) fall back to legacy. Image extraction calls during the window land as `text_status=failed` and can be retried by the user via the existing UI.

If the rotation drift is unacceptable, do this off-hours.

## How to check if the sidecar is healthy

Three signals, in increasing order of "something needs attention":

1. **`/healthz` returns 200** (Render service detail page or `curl <sidecar-url>/healthz`). Liveness only — does not test the LLM path.
2. **Backend per-call log lines** (SCRUM-306). Every `talkback` call to the sidecar emits one line in either of two formats:
   - Image: `markitdown.image: outcome=<tag> duration_ms=<n> [tokens=<n>] [code=<stable>] [err=<short>]`
   - URL: `markitdown.url: outcome=<tag> duration_ms=<n> [code=<stable>] [err=<short>]`
   Outcome tags: `success`, `unavailable`, `unauthorized`, `bad_request`, `upstream_http`, `unclassified_error`. Aggregate by `outcome=` to get a per-tag rate; aggregate by `duration_ms=` to get latency histograms.
3. **Backend fallback warning** — `WARN markitdown_sidecar_fallback url=<...> err=<...>`. URL extraction is silently falling back to the legacy Go walker. A small steady rate is normal (transient sidecar restarts); a sustained spike means the sidecar is degraded.

Quick triage flowchart:
- `/healthz` not 200 → sidecar process is dead; restart in Render.
- `/healthz` 200, but `outcome=unavailable` rate elevated → sidecar is healthy *to itself* but talkback can't reach it (network policy, env var drift, secret mismatch). Re-check `MARKITDOWN_SIDECAR_URL` and the SECRET pair.
- `outcome=unauthorized` non-zero → SECRET drift between sidecar and backend. Re-sync per the secret rotation procedure above.
- `outcome=bad_request` rate elevated → caller bugs (e.g., backend sending wrong content type); check the `code=` field for the stable error identifier.

## Reading sidecar logs

Render → `markitdown-sidecar` → Logs. The sidecar emits one structured JSON line per request:

```json
{"event":"request","method":"POST","path":"/extract/image","status_code":200,"duration_ms":4321,"request_id":"a1b2c3d4...","tokens_used":120,"model":"gpt-4o-mini","operation":"extract.image"}
```

Useful filters:
- `"operation":"extract.image"` — image captioning calls.
- `"operation":"extract.url"` — URL extraction calls.
- `"status_code":4xx` — caller bugs (bad content type, too-large body).
- `"status_code":5xx` — sidecar internal errors. Investigate.
- `"request_id"` matches the `x-request-id` header echoed back to the backend, so a single request can be traced across services.

The backend logs `markitdown_sidecar_fallback` (INFO) every time URL extraction transparently falls back. A spike there is the first sign the sidecar is degraded.

## Incident playbook

| Symptom                                   | Likely cause                              | Action |
|-------------------------------------------|-------------------------------------------|--------|
| All `/extract/image` requests 401         | Backend / sidecar SECRET drift            | Re-sync `SIDECAR_SECRET` and `MARKITDOWN_SIDECAR_SECRET`. |
| `/extract/image` returns 500 with `llm_misconfigured` | `OPENAI_API_KEY` missing or invalid on sidecar | Set/refresh the key; redeploy sidecar. |
| `/extract/image` returns 500 with `dependency_missing` | MarkItDown / openai pip install failed in image | Rebuild the Docker image; check Render build logs. |
| Image uploads stuck on `text_status=pending` | Worker not running / sidecar unreachable | Check `talkback` logs for the link-extraction worker; check `markitdown-sidecar` is `live`. |
| URL extractions returning legacy content unexpectedly | `MARKITDOWN_URL_EXTRACTION_ENABLED=false` | Flip to `true` and redeploy `talkback`. |
| Spike in `markitdown_sidecar_fallback` log lines | Sidecar 5xx / unreachable                 | Check sidecar service status; if persistent, flip flags off and investigate. |
| Cost spike / OpenAI bill alarm            | Large image uploads triggering many vision calls | Use Option 1 rollback (flag flip) to immediately stop `extract.image` traffic; investigate per-session usage in sidecar logs. |

## Related docs

- `services/markitdown-sidecar/README.md` — local development quickstart.
- `internal/markitdown/client.go` — typed Go client API.
- `docs/agent/workflow-full-auto.md` — gate policy for landing changes.
- SCRUM-298 (epic) — high-level rationale and links to all child tickets.
