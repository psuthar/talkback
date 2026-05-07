# Live Demo Script — TalkBack Hiring Committee for Alex Chen

**Target length:** 5–7 minutes
**Audience:** product/sales prospect ("would this make hiring committees easier?")
**Goal:** demonstrate cross-artifact synthesis, citation, and disagreement reasoning — *not* AI chat over docs.

The demo is built around one principle: **no single artifact answers the hiring question.** That's why TalkBack is the surface, not a chat sidebar in Slack.

---

## Pre-flight checklist (do once before the demo)

- All seven interview transcripts uploaded
- Feedback packet PDF, JD PDF, values PDF, resume PDF uploaded
- Three Slack screenshots uploaded
- Greenhouse export uploaded
- Architecture diagram + candidate whiteboard uploaded
- Comp guidance + final committee notes (DOCX) uploaded
- Premise/decision card visible on the session
- Suggested questions pre-populated (pull top 5 from `sample_questions.md`)

---

## The script

### [0:00–0:30] Set the scene

> "This is a hiring committee that just wrapped up the loop for Alex Chen, a Senior Staff Engineer candidate. Five interviewers, two observers, fifteen artifacts spread across PDFs, screenshots, transcripts, ATS exports — basically how every real hiring committee actually operates. The committee has *not* yet extended an offer. We're going to use TalkBack to make the call."

**On screen:** session premise + decision card visible.

### [0:30–1:30] Question 1 — open with disagreement

**Type:** *"What is the committee's overall recommendation, and where do reviewers disagree?"*

**What to show the audience:** TalkBack hedges — it doesn't return a single tidy "Hire!" answer. It names the disagreement: Rohan came in at L5; Nathan came in at lean-no-hire; everyone else at L6. Citations span the feedback packet PDF, the values transcript, and the final committee transcript.

> "Notice how the answer doesn't flatten the disagreement. That's the value. A summary that said 'Hire' would be wrong — the disagreement is the substance."

### [1:30–2:30] Question 2 — drill into the leveling debate

**Type:** *"What argument moved Rohan from L5 to L6 during committee?"*

**What to show:** TalkBack should pull from the committee transcript and identify two stacked arguments — Priya's API-versioning shot-call counter-example, and Diego's role-constraint observation that L5 blocks Alex from leading the rebuild. Hover the citations.

> "TalkBack didn't just give us a quote. It gave us the *evolution* of the disagreement — what changed someone's mind, with the lines of dialogue that did it."

### [2:30–3:30] Question 3 — cross-artifact synthesis

**Type:** *"Compare the candidate's whiteboard sketch with the cleaned-up architecture diagram. What's missing or different?"*

**What to show:** TalkBack should retrieve both images and contrast them. The whiteboard has hand-written annotations like "?? fencing — TODO" and "split-brain risk on cutover" that don't appear in the cleaned-up diagram.

> "We're now reasoning across two image artifacts. The whiteboard is messy on purpose — it has the candidate's *hesitations*. The clean diagram doesn't. The synthesis is in the contrast."

### [3:30–4:15] Question 4 — pull in Slack context

**Type:** *"Is there context outside the formal interviews that affects how we should level Alex?"*

**What to show:** TalkBack pulls the Slack `slack_thread_leveling.png` and the `slack_thread_offer_risk.png`. The first shows Rohan and Sara debating before committee. The second reveals competing offer pressure that was *deliberately kept out of committee discussion*.

> "Look — TalkBack just surfaced something that wasn't in any interview transcript. The competing offer pressure was discussed only in Slack and the comp memo. A reviewer who only watched the recordings would miss this."

### [4:15–5:00] Question 5 — comp synthesis

**Type:** *"Why did Marcus and Emily recommend median-of-band L6 comp instead of top of band?"*

**What to show:** TalkBack pulls from the compensation guidance memo (DOCX) and the comp/leveling discussion transcript. Two reasons emerge: logged committee dissents, and the historical pattern of top-of-band-with-reservations producing 18-month attrition.

> "We're reasoning across an internal-only memo, an audio-only internal calibration call, and the committee outcome. That's three different formats."

### [5:00–6:00] Question 6 — forward-looking risk

**Type:** *"Three months from now, Alex has shipped phase one of the dedup rebuild but missed his cross-team commitment milestones. Based on what we know, what's the most likely root cause and the right intervention?"*

**What to show:** TalkBack reasons *forward* using documented concerns. Most likely cause: the consensus-building pattern Rohan flagged colliding with cross-team pressure. Right intervention: the weekly RACI sync Priya was supposed to set up in month 1.

> "This is the moment most teams have *after* the hire — three months in, when something slips, you reread the loop trying to remember what you were worried about. With TalkBack, the loop is queryable. The concerns and mitigations are connected."

### [6:00–6:45] Question 7 — submit a stance

**Type:** *"Given everything I just asked, write the committee's one-paragraph hire summary for the rest of the org."*

**What to show:** TalkBack drafts a paragraph that captures the level, the keystone project, the conditions, the dissents, the comp posture. Stance submission flow: reviewer agrees → submits stance → decision card updates.

> "And that's where most committees would have ended four hours ago in a Slack thread, with no record. TalkBack just turned the loop into a defensible decision."

### [6:45–7:00] Close

> "Same question every committee asks — 'do we have enough information to extend an offer?' — answered with citations across PDFs, transcripts, screenshots, and ATS data, in under seven minutes. The artifacts didn't change. The decision is the same. The *time* and the *evidence trail* did."

---

## Backup questions (if you have time / get pushed)

- **Q19:** "Tell me about a time Alex was wrong technically." — Linkerd story, pull from values + feedback packet + Slack concern thread.
- **Q63:** "If reference comes back weak, what changes?" — shows TalkBack reasoning about *conditional* committee outcomes.
- **Q40:** "Are there topics in Slack threads that didn't come up in any interview?" — explicit cross-artifact diff.

## Anti-patterns to avoid in the demo

- Don't ask "summarize this candidate" — that's the question every chatbot answers. Ask the question only TalkBack answers.
- Don't pre-narrate the answer. Let TalkBack surprise the audience by pulling something unexpected (the Slack thread, the whiteboard annotation).
- Don't apologize for the messiness — that's the point. Real hiring loops are messy.

## What success looks like

The audience reaction you want at minute 5:

> *"...wait, it pulled the Slack thread? I didn't even know that was uploaded."*

If you don't get that reaction, the demo isn't working. Restructure to feature the Slack/screenshot/whiteboard cross-references earlier.
