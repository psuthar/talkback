# Debugging TalkBack on Render

## Participant sees "No sessions" after being invited

### 1. Did you delete and recreate the participant user?

**Invitations are tied to user ID, not email.** When you delete a user and create a new one with the same email:

- The old user had a different UUID. Any invitation was stored as `(session_id, old_user_id)`.
- Deleting the user triggers `ON DELETE CASCADE` on `session_invitations`, so that invitation row is removed.
- The new user has a new UUID and has **no** invitation rows.

**Fix:** After recreating a user, **re-invite them** to the session (Creator: open session → Invite → enter their email again).

### 2. Check auth (and CORS only if split-origin) (participant gets 401)

If the participant was invited correctly but still sees no sessions, the session list may be failing with 401 Unauthorized.

**Unified SPA+API (same Render service, same origin):** First-party requests do not depend on CORS configuration. Focus on cookie/session: wrong `APP_BASE_URL`, expired session, or private browsing blocking cookies.

**Split-origin (legacy: frontend host ≠ API host):** The session cookie may not be sent cross-site without `CORS_ALLOWED_ORIGINS` + `TB_ALLOWED_ORIGINS` (and `SameSite=None` cookie mode).

**InPrivate / Private browsing:** Many browsers restrict third-party or cross-site cookies. If you see 401 only in InPrivate, try a **normal** window first.

**In the browser (as the participant):**

1. Open DevTools → **Network**.
2. Log in as the participant on the app origin.
3. Reload or trigger "Load sessions". Look for:
   - `GET …/api/me` → should be **200** (if 401, cookie not sent or invalid).
   - `GET …/api/sessions` → should be **200** with a JSON array (if 401, same as above).

**On the API service (Render), split-origin only:**

- Set **`CORS_ALLOWED_ORIGINS`** to the exact frontend origin (no trailing slash).
- Set **`TB_ALLOWED_ORIGINS`** to the same value so the session cookie can use `SameSite=None; Secure` when the browser talks to a different API host.
- Redeploy after changing env vars.

**Quick test:** Open the invite link in a normal tab on the same origin as login. If using `?api=` to point at another host, you need the split-origin CORS/cookie settings above.

---

## Video shows "Format error" or won’t play

- The in-app player streams via your API using the **creator’s** Zoom token. No one needs to log into Zoom. The session creator connects Zoom once in TalkBack; the backend stores and refreshes the token so video works for creator and participants. If the creator's connection expires, they reconnect once in Settings; then video works again for everyone.
- If you see "MEDIA_ELEMENT_ERROR: Format error", often the **server returned an error** (e.g. 403) and the browser treats the response as non-video.
- **Checks:**
  1. **Creator** has connected Zoom (Settings → Connect Zoom) so the API has a token for the session creator.
  2. In Network tab, open the request to `…/sessions/{id}/video-sources/{id}/stream`. Check status (200/206 = OK; 403/404/500 = backend error). Check response headers for `Content-Type: video/mp4` on success.
  3. If the stream URL is on a different host than the frontend (e.g. talkback-895n vs talkback-ux), the API must send `Access-Control-Allow-Origin` for the frontend origin (see CORS above).

---

## Summary

| Symptom | Likely cause | Action |
|--------|----------------|--------|
| No sessions after deleting/recreating user | Invitation was for old user ID; CASCADE removed it | Re-invite the user to the session |
| No sessions, 401 on /api/me or /api/sessions | Cookie not sent or invalid session | Same-origin: check login and `APP_BASE_URL`. Split-origin: set `CORS_ALLOWED_ORIGINS` + `TB_ALLOWED_ORIGINS` |
| Video "Format error" | Stream endpoint returned 4xx/5xx or (rare) CORS on split-origin | Creator reconnects Zoom; check stream URL in Network |
