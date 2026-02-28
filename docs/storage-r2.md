# R2 Presigned Storage (TalkBack)

All binary files (Zoom MP4 ingests, user uploads, exports) are stored in **Cloudflare R2** using presigned **PUT** for uploads and presigned **GET** for access.

- **Bucket:** `talkback-r2-bucket`
- **Upload flow:** `POST /api/artifacts/presign-put` → client PUT to R2 → `POST /api/artifacts/complete`
- **Access flow:** `GET /api/artifacts/{id}/access` → returns presigned GET URL for video/docs/images

## Environment variables (required for R2)

Set these for local and for Render (or any deploy):

- **STORAGE_DRIVER** = `r2` (required; without it R2 is off and presign/artifacts return 503)
- **R2_BUCKET** = `talkback-r2-bucket` (or your bucket name)
- **R2_ENDPOINT** – e.g. `https://<account_id>.r2.cloudflarestorage.com`
- **R2_REGION** = `auto` (recommended for R2)
- **R2_ACCESS_KEY_ID**, **R2_SECRET_ACCESS_KEY** – R2 API token (Secret on Render)
- **R2_PREFIX** = `talkback/` (object key prefix; no leading/trailing slash in code)

Optional:

- `R2_ACCOUNT_ID`, `R2_PRESIGN_PUT_TTL_SECONDS`, `R2_PRESIGN_GET_TTL_SECONDS`
- `PUBLIC_APP_ORIGIN` (for CORS / redirects)
- Upload caps: `MAX_UPLOAD_BYTES_DEFAULT`, `MAX_UPLOAD_BYTES_VIDEO`, `MAX_SESSION_ARTIFACTS`

See `.env.example` for a full list.

## R2 bucket CORS (for presigned GET video playback)

The browser fetches video via presigned GET URLs. For the `<video>` element to load R2 objects from your frontend origin, configure CORS on the R2 bucket (Cloudflare dashboard → R2 → your bucket → Settings → CORS policy).

- **Allowed origins:** your frontend origins, e.g. `http://localhost:5173`, `http://localhost:3000`, and your Render frontend (e.g. `https://your-app.onrender.com`).
- **Allowed methods:** `GET`, `HEAD`.
- **Allowed headers:** (leave default or add any the client sends).
- **Expose headers:** optional; `Content-Length`, `Content-Range`, `Accept-Ranges` help with seeking.

Without CORS, the video may fail to load with a network or “blocked” error in the console.

## Local dev

1. Copy `.env.example` to `.env` and set R2 keys.
2. Start backend; upload and playback use R2 presigned URLs.

## Render checklist

If you see in Render logs: **"R2 storage not configured (STORAGE_DRIVER is not 'r2'); video presign and file artifacts will return 503"**, add the env vars below (Dashboard or via `render.yaml`).

- [ ] In Render Web Service → **Environment**, set:
  - `STORAGE_DRIVER=r2` ← required; without it R2 is off and presign/artifacts return 503
  - `R2_BUCKET`, `R2_ACCOUNT_ID`, `R2_ENDPOINT`, `R2_REGION`
  - `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` (as **Secret**)
  - `R2_PREFIX`, `R2_PRESIGN_PUT_TTL_SECONDS`, `R2_PRESIGN_GET_TTL_SECONDS`
  - `PUBLIC_APP_ORIGIN=https://<your-frontend-domain>`
  - Optional caps: `MAX_UPLOAD_BYTES_DEFAULT`, `MAX_UPLOAD_BYTES_VIDEO`, `MAX_SESSION_ARTIFACTS`
- [ ] Redeploy backend after changing env.

## Key naming

- **Session-scoped:** `prefix/sessions/{session_id}/artifacts/{artifact_id}/{safe_filename}`
- **User-scoped (no session):** `prefix/users/{owner_user_id}/artifacts/{artifact_id}/{safe_filename}`

## Demo caps

Upload and per-session limits can be tuned for demo vs production:

- **MAX_UPLOAD_BYTES_DEFAULT** – max size for non-video uploads (default 250MB).
- **MAX_UPLOAD_BYTES_VIDEO** – max size for video uploads (default 1GB).
- **MAX_SESSION_ARTIFACTS** – max file artifacts per session (default 50).

Set lower for demos; raise for production as needed.

## Playback and Zoom stream (legacy)

- When a session has **primary_video_artifact_id** (Zoom import to R2 or R2 video upload), playback uses **video_access_url** from `GET /sessions/:id` (presigned R2 GET). The frontend uses this URL for the video element; no Zoom token or stream proxy is involved.
- **GET /sessions/:id/video-sources/:videoSourceId/stream** remains for **legacy** Zoom-only sessions (no R2, or imported before R2). It proxies the Zoom MP4 using the creator’s Zoom token (refresh token is used when the access token has expired). New Zoom imports with R2 configured use R2 for playback and do not rely on this stream endpoint.
