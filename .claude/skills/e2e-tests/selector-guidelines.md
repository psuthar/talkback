# Selector Guidelines

Tracks the selector status of every E2E-testable element in TalkBack. Use this before writing any test to know what selectors are available today and what needs to be added first.

---

## Selector Priority Order

1. `data-testid` — preferred; stable, intent-expressing, immune to copy/style changes
2. CSS class from `index.css` — acceptable fallback if the class is actually applied to a JSX element
3. `getByLabel` / `getByRole` — for standard form controls with visible labels or ARIA roles
4. Text content — last resort; fragile if copy changes

---

## Status Legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Selector exists and is applied — usable today |
| ⚠️ | CSS class defined in `index.css` but **not applied** to any JSX element — unusable until component is updated |
| ❌ | No selector; `data-testid` must be added before writing a test |
| 🏷️ | Stable via `getByLabel` or `getByRole` — usable today without changes |

---

## Login Page (`LoginPage.jsx`)

| Element | Current state | Recommended `data-testid` | Notes |
|---------|--------------|--------------------------|-------|
| Email input | 🏷️ `getByLabel('Email')` | `login-email` | label text exists |
| Password input | 🏷️ `getByLabel('Password')` | `login-password` | label text exists |
| Log in tab button | 🏷️ `getByRole('button', { name: 'Log in' })` | `tab-login` | text stable |
| Sign up tab button | 🏷️ `getByRole('button', { name: 'Sign up' })` | `tab-signup` | text stable |
| Display name input (signup) | 🏷️ `getByLabel('Display name')` | `signup-display-name` | label text exists |
| Submit button (login) | 🏷️ `getByRole('button', { name: /log in/i })` | `submit-login` | text "Log in" / "Signing in…" |
| Submit button (signup) | 🏷️ `getByRole('button', { name: /sign up/i })` | `submit-signup` | text "Sign up" / "Creating account…" |
| Error message | ✅ `.error` (CSS class applied inline) | `auth-error` | className="error" on div |

---

## Accept Invite Page (`AcceptInvitePage.jsx`)

| Element | Current state | Recommended `data-testid` | Notes |
|---------|--------------|--------------------------|-------|
| Email (read-only) | ❌ no selector | `invite-email-readonly` | `readOnly` input, pre-filled by API |
| Display name input | 🏷️ `getByLabel('Display name')` | `invite-display-name` | label text present |
| Password input | 🏷️ `getByLabel('Password')` | `invite-password` | label text present |
| Create account button | 🏷️ `getByRole('button', { name: /create account/i })` | `invite-submit` | text "Create account & join" |
| Error message | ❌ uses inline style, no class | `invite-error` | styled div, no class |
| Loading state | ❌ text only | `invite-loading` | "Loading invitation…" text |

---

## Participant Mode / Q&A Panel (`QAPanel.jsx`)

| Element | Current state | Recommended `data-testid` | Notes |
|---------|--------------|--------------------------|-------|
| Question textarea | ❌ no selector | `question-input` | `<textarea>` with placeholder "Ask a question…" |
| Ask button | ❌ no selector | `ask-button` | `<button>` with text "Ask", no class or testid |
| Voice record button | ❌ no selector | `voice-record-button` | has `aria-label` but not stable as selector |
| Thinking spinner | ✅ `.spinner` class applied | — | `<span className="spinner">` during thinking state |
| Footer container | ❌ | `qa-footer` | `<footer className="participant-qa-footer">` — class is on footer |
| Q&A scroll area | ❌ | `qa-scroll` | `<div className="participant-qa-scroll">` — class on div |

---

## Q&A History / QACard (`QAHistory.jsx`)

These are the most critical selectors for E2E assertions. **All of them are missing** — the components use inline styles exclusively despite the classes being defined in `index.css`.

| Element | Current state | Recommended `data-testid` | CSS class to apply |
|---------|--------------|--------------------------|-------------------|
| Individual Q&A card | ⚠️ `.question-item` defined, not applied | `question-item` | Add `className="question-item"` to `QACard` outer div |
| Question text | ⚠️ `.question-text` defined, not applied | `question-text` | Add `className="question-text"` to Q: div in QACard |
| Answer text | ⚠️ `.answer-text` defined, not applied | `answer-text` | Add `className="answer-text"` to A: div in QACard |
| Answer status badge | ⚠️ `.answer-status` defined, not applied | `answer-status` | Add `className={\`answer-status \${q.answer.answer_status}\`}` to status span |
| Citations container | ⚠️ `.citations` defined, not applied | `citations` | Add `className="citations"` (already on div in QAHistory) |
| Individual citation | ⚠️ `.citation` defined, not applied | `citation` | Add `className="citation"` to each `CitationBadge` button |
| Expand/collapse toggle | ❌ has `aria-label` "Expand"/"Collapse" | `qa-card-toggle` | `getByRole('button', { name: 'Expand' })` works today |
| Reply button | ❌ text only | `qa-reply-button` | text "Reply" — fragile |
| Collapse state (question only) | ❌ | — | QACard starts collapsed; expand before asserting answer |

> **Important:** Today, `.citations` class IS applied in `QAHistory.jsx` line 324 (`<div className="citations">`). All other Q&A classes are defined but not applied.

---

## Materials Panel (`SessionMaterialsTab.jsx`, `MaterialsTreePanel.jsx`)

| Element | Current state | Recommended `data-testid` | Notes |
|---------|--------------|--------------------------|-------|
| Materials tab button | ❌ no selector | `materials-tab` | tab nav is app-specific |
| Material list item | ❌ inline styles | `material-item` | each material in list |
| Material filename | ❌ inline styles | `material-filename` | text in bold div |
| Material status badge | ❌ inline styles | `material-status` | "ready"/"pending" span |
| Open/view button | ❌ | `material-open` | click to open document viewer |

---

## Creator Mode (`CreatorMode.jsx`)

| Element | Current state | Recommended `data-testid` | Notes |
|---------|--------------|--------------------------|-------|
| Session title | ❌ | `session-title` | `<h1>` or heading in creator view |
| Edit session button | ❌ | `edit-session` | |
| Delete session button | ❌ | `delete-session` | |
| Invite participant button | ❌ | `invite-participant-button` | opens share modal |

---

## App-Level / Session List

| Element | Current state | Recommended `data-testid` | Notes |
|---------|--------------|--------------------------|-------|
| Session list container | ❌ | `session-list` | top-level list of sessions |
| Individual session item | ❌ | `session-item` | each session in list |
| Session item title | ❌ | `session-item-title` | clickable title in list |
| New session button | ❌ | `create-session-button` | "New Session" or similar |

---

## `index.css` Classes: Applied vs. Defined

These classes are defined in `web/src/index.css` but the QAHistory components use inline styles instead:

| CSS class | Defined in CSS | Applied in JSX | Action needed |
|-----------|---------------|----------------|---------------|
| `.question-item` | ✅ | ❌ | Add to `QACard` outer div |
| `.question-text` | ✅ | ❌ | Add to question `div` in `QACard` |
| `.answer-text` | ✅ | ❌ | Add to answer text `div` in `QACard` |
| `.answer-status` | ✅ | ❌ | Add to status `span` in `QACard` |
| `.answer-status.answered` | ✅ | ❌ | Apply via template literal: `` `answer-status ${status}` `` |
| `.answer-status.not_covered` | ✅ | ❌ | Same pattern |
| `.answer-status.error` | ✅ | ❌ | Same pattern |
| `.citations` | ✅ | ✅ (line 324) | Usable today |
| `.citation` | ✅ | ❌ | Add to `CitationBadge` button |
| `.citation-source` | ✅ | ❌ | Add to source type span |
| `.citation-snippet` | ✅ | ❌ | Add to excerpt span |
| `.spinner` | ✅ | ✅ (QAPanel, QAHistory) | Usable today |

---

## Workflow for Adding a Selector

When a test needs an element that has no stable selector:

1. **Check this file** — confirm the element is listed as ❌ or ⚠️.
2. **Open the component** — find the exact JSX element.
3. **Add `data-testid`** — use the recommended name from this table.
   ```jsx
   // Before:
   <button type="button" onClick={askSessionQuestion} disabled={!questionText?.trim() || loading}>
     Ask
   </button>

   // After:
   <button data-testid="ask-button" type="button" onClick={askSessionQuestion} disabled={!questionText?.trim() || loading}>
     Ask
   </button>
   ```
4. **Or apply the CSS class** from `index.css` if one exists but isn't wired up yet:
   ```jsx
   // Before (QACard):
   <span style={{ color: statusColor }}>{q.answer.answer_status}</span>

   // After:
   <span className={`answer-status ${q.answer.answer_status}`}>{q.answer.answer_status}</span>
   ```
5. **Update this table** to mark the element ✅.
6. **Write the test** using the new selector.

---

## Selectors Usable Today (No Changes Needed)

For writing tests immediately without modifying components:

```typescript
// Login page
page.getByLabel('Email')
page.getByLabel('Password')
page.getByRole('button', { name: /log in/i })
page.getByRole('button', { name: /sign up/i })
page.getByLabel('Display name')

// Invite acceptance
page.getByLabel('Display name')
page.getByLabel('Password')
page.getByRole('button', { name: /create account/i })

// Q&A — loading indicator
page.locator('.spinner')

// Citations container (applied in JSX)
page.locator('.citations')

// Expand/collapse (aria-label present)
page.getByRole('button', { name: 'Expand' })
page.getByRole('button', { name: 'Collapse' })
```
