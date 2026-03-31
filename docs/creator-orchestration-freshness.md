# Creator orchestration recommendations — freshness (SCRUM-16)

## Behavior

While a **creator** has a session open in **edit** mode, the **AI Suggested Next Actions** panel refreshes its recommendations automatically when relevant session activity arrives over the **WebSocket** connection shared with participants.

- Updates are implemented by **debounced** calls to the existing **`POST /api/sessions/{id}/orchestration/recommendations/sync`** endpoint (same as the manual **Refresh** button). Typical coalescing window: **~900 ms** after the last qualifying event (see `ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS` in the web app).
- **No** answers, drafts, or publishes are created automatically; only recommendation rows and the panel UI update.
- **Manual Refresh** remains available and still shows an explicit success message. Auto-refresh runs **quietly** (no success toast per event).

## Events that trigger a coalesced sync

Including but not limited to: new question, answer created/updated, session updated (e.g. materials/transcript), processing ready, stance updated, invitation accepted (participant joined).

## What still requires explicit creator actions

- Approving or dismissing recommendations, generating draft answers, posting answers, and other orchestration **actions** are unchanged and remain user-driven.
