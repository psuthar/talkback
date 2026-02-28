# R2 Presigned Storage (TalkBack)

All binary files (Zoom MP4 ingests, user uploads, exports) are stored in **Cloudflare R2** using presigned **PUT** for uploads and presigned **GET** for access.

- **Bucket:** `talkback-r2-bucket`
- **Upload flow:** `POST /api/artifacts/presign-put` → client PUT to R2 → `POST /api/artifacts/complete`
- **Access flow:** `GET /api/artifacts/{id}/access` → returns presigned GET URL for video/docs/images

## Environment variables

See `.env.example` for:

- `STORAGE_DRIVER=r2`
- `R2_BUCKET`, `R2_ACCOUNT_ID`, `R2_ENDPOINT`, `R2_REGION`
- `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`
- `R2_PREFIX`, `R2_PRESIGN_PUT_TTL_SECONDS`, `R2_PRESIGN_GET_TTL_SECONDS`
- `PUBLIC_APP_ORIGIN`
- Upload caps: `MAX_UPLOAD_BYTES_DEFAULT`, `MAX_UPLOAD_BYTES_VIDEO`, `MAX_SESSION_ARTIFACTS`

## Local dev

1. Copy `.env.example` to `.env` and set R2 keys.
2. Start backend; upload and playback use R2 presigned URLs.

## Render checklist

- [ ] In Render Web Service → Environment, set:
  - `STORAGE_DRIVER=r2`
  - `R2_BUCKET`, `R2_ACCOUNT_ID`, `R2_ENDPOINT`, `R2_REGION`
  - `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` (as secrets)
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
