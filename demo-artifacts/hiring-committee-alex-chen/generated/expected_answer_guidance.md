# Expected Answer Guidance

For the highest-leverage demo questions, this doc spells out:

- **What ideal synthesis behavior looks like** — the specific shape an answer should take
- **Expected citations** — which artifacts a strong answer should reference
- **Failure modes** — what a *weak* TalkBack answer would look like, so you can spot when it's failing live

If a TalkBack answer cites only the feedback packet, the synthesis is broken — the value here is fusion across recordings, scorecards, ATS, and Slack.

---

## Q4 — "Did everyone on the committee agree, or was there disagreement?"

**Ideal synthesis behavior**
Name the disagreement axes specifically: leveling (Rohan starting at L5; Sara/Priya at L6) and scope ownership (Nathan starting at lean-no-hire). Note that the disagreement *evolved* during the debrief — Rohan moved L5→L6, Nathan moved lean-no-hire→L6 — and explain the arguments that moved each.

**Expected citations**
- `transcripts/final_hiring_committee_debrief.transcript.md` — the round-robin opening, then Rohan/Sara exchange, then Diego's role-constraint argument
- `documents/Interview_Feedback_Packet.pdf` — Rohan's pre-committee L5 recommendation
- `transcripts/values_behavioral_interview.transcript.md` — origin of Rohan's decisiveness concern

**Weak answer to watch for**
"There was some disagreement but the committee agreed to hire at L6." — this flattens the dissents. A strong answer names the dissents as still-logged.

---

## Q10 — "Alex's first hypothesis on the FCM 503 debugging scenario"

**Ideal synthesis behavior**
Walk the diagnosis tree: status page → error code breakdown → regional concentration → connection pool exhaustion → FCM-soft-throttle-via-503. Note that Alex asked the right questions in sequence rather than guessing.

**Expected citations**
- `transcripts/technical_deep_dive.transcript.md` (turns 13:14 through 17:24)

**Weak answer to watch for**
A summary that just says "he debugged it well." A strong answer names the specific FCM-503-as-soft-throttle quirk because that's the unusual signal.

---

## Q13 ★ — "What did Alex say *he hasn't done* in distributed systems?"

**Ideal synthesis behavior**
Pull from at least two interviews. Multiple admissions:
- No fencing implementation from scratch (system design)
- No formal-methods background (TLA+, Jepsen) (system design + hiring manager)
- Never operated a hybrid sync/async dedup design (technical deep dive)
- Hasn't run at 2B/day scale (hiring manager)

**Expected citations**
- `transcripts/system_design_review.transcript.md` (fencing turn ~22:55, formal methods)
- `transcripts/hiring_manager_interview.transcript.md` (scale, formal methods stretch)
- `transcripts/technical_deep_dive.transcript.md` (hybrid dedup admission)

**Weak answer to watch for**
A single-source answer from only system design. A strong answer should pull at least two transcripts because the gaps were named in different rounds.

---

## Q16 ★ — "Compare the architecture diagram and the candidate whiteboard"

**Ideal synthesis behavior**
TalkBack should describe both images and *contrast* them: the polished architecture diagram represents the documented design (with cross-region replication shown explicitly), while the candidate's whiteboard sketch is rougher, includes hand-written notes about known gaps ("?? fencing — TODO", "split-brain risk on cutover"), and uses the hybrid-dedup terminology Alex mentioned in dialogue.

**Expected citations**
- `images/system_design_architecture.png` — clean version
- `images/candidate_whiteboard.jpg` — rough version with annotations
- `transcripts/system_design_review.transcript.md` — to ground the differences

**Weak answer to watch for**
Citing only the architecture image. The whiteboard is the artifact that contains Alex's *hesitations* — the synthesis is in showing the contrast.

---

## Q19 — "Tell me about a time Alex was wrong technically"

**Ideal synthesis behavior**
The Linkerd story. Pull from values round and feedback packet — it appears in both. A strong answer also notes that the Slack `slack_thread_concerns.png` flags concern about the *nine-month cost* of the wrong call, and that the committee debrief addressed this only briefly.

**Expected citations**
- `transcripts/values_behavioral_interview.transcript.md` (Linkerd turn 7:10)
- `documents/Interview_Feedback_Packet.pdf` (Rohan's strengths)
- `images/slack_thread_concerns.png` (Jenna's flag about the cost)

---

## Q23 ★ — "Where did Alex's values-round responses contradict something else?"

**Ideal synthesis behavior**
Subtle question. The strongest finding: Alex told Rohan in the values round that he's been "actively trying to be more decisive," but in the technical deep dive he scoped a six-week spike rather than calling the Temporal shot directly. These are framed differently but reflect the same pattern Rohan flagged. Also worth noting: Alex's claim of strong cross-team scope on the smart-batching project (hiring manager) is what Nathan questioned (committee), suggesting self-perception > external read.

**Expected citations**
- `transcripts/values_behavioral_interview.transcript.md` (Temporal-spike framing)
- `transcripts/technical_deep_dive.transcript.md` (the same spike approach repeated)
- `transcripts/final_hiring_committee_debrief.transcript.md` (Nathan's challenge)

---

## Q26 ★ — "Should Alex be hired at L6 or L5?"

**Ideal synthesis behavior**
This is the marquee question. A great answer:
1. Names the specific evidence on each side. L6 case: distributed systems depth (Sara), correct dedup hypothesis (Priya), API versioning shot-call (Priya), real production ownership of the 4M-duplicate incident (Diego/transcript). L5 case: decisiveness pattern (Rohan), low-stakes nature of decisive examples (Rohan), JVM ramp (Diego), formal-methods gap (Sara), unverified cross-team scope (Nathan).
2. Names the role constraint that resolved the debate: leading the dedup rebuild requires L6 scope. Down-leveling would block the keystone project.
3. Names the resulting comp posture: median of L6 band, with structured six-month review.

**Expected citations**
- `documents/Interview_Feedback_Packet.pdf`
- `transcripts/final_hiring_committee_debrief.transcript.md`
- `documents/Senior_Staff_Engineer_JD.pdf` (role expectations)
- `internal-notes/compensation_guidance.docx`

**Weak answer to watch for**
A vote-tally answer ("3 said L6, 1 said L5"). The synthesis is in the *arguments*, not the count.

---

## Q29 ★ — "What argument moved Rohan from L5 to L6?"

**Ideal synthesis behavior**
Two arguments stacked together: Priya's API versioning shot-call example (which Rohan said he'd underweighted), and Diego's role-constraint argument that down-leveling blocks the rebuild. The combination matters; neither alone moved him.

**Expected citations**
- `transcripts/final_hiring_committee_debrief.transcript.md` (turns ~7:54, ~15:33, ~18:25)

---

## Q34 ★ — "Where do Sara and Rohan most directly disagree?"

**Ideal synthesis behavior**
On the meaning of process-driven decisioning. Sara reads the Temporal-spike approach as deliberate Senior-Staff process; Rohan reads it as structural avoidance of the call. They are looking at the same evidence and drawing opposite leveling conclusions. Strong answer notes the meta-insight that this is a culture difference, not a fact-finding gap.

**Expected citations**
- `transcripts/final_hiring_committee_debrief.transcript.md` (Sara/Rohan exchange ~5:25–6:38)
- `documents/Interview_Feedback_Packet.pdf` (both reviewers' explicit framings)

---

## Q42 ★ — "Is Alex set up to lead the dedup rebuild?"

**Ideal synthesis behavior**
Pull threads:
- He has the right technical instinct (correct hypothesis on the failure mode in the hiring manager round)
- He's missing formal-methods depth — Devon (Principal) has been pre-committed as a pairing partner
- He's missing JVM/Kotlin fluency — three-month ramp expected (Diego); Marcus J pre-committed for ramp pairing
- His cross-team-scope-ownership is unverified (Nathan); reference call may resolve
- The committee tied a structured six-month review to the rebuild

So: he's set up, but with explicit support and explicit conditions. Not "yes, plug and play."

**Expected citations**
- `transcripts/hiring_manager_interview.transcript.md`
- `transcripts/final_hiring_committee_debrief.transcript.md`
- `internal-notes/final_committee_notes.docx`

---

## Q47 ★ — "Why didn't Marcus and Emily recommend top-of-band L6 comp?"

**Ideal synthesis behavior**
Two reasons stacked, both in the comp memo and the calibration call: (1) logged dissents in committee mean top-of-band would over-signal commitment we don't have, (2) median preserves headroom for promotion and avoids the historical pattern of top-of-band-with-reservations leading to attrition within 18 months. Strong answer also notes the deliberate decision *not* to let competing-offer pressure drive top-of-band.

**Expected citations**
- `internal-notes/compensation_guidance.docx`
- `transcripts/compensation_leveling_discussion.transcript.md`
- `transcripts/final_hiring_committee_debrief.transcript.md` (Marcus's posture)

---

## Q51 ★ — "Scenario in which we'd down-level to L5"

**Ideal synthesis behavior**
The down-level trigger was defined explicitly in the comp/leveling discussion: a *split committee* on level. The committee did emerge with L6 unanimous (despite logged dissents on quality), so the trigger was not pulled. A strong answer also notes the logical fallback: if reference comes back weak, the committee said it would revisit before extending, which could re-open the down-level question.

**Expected citations**
- `transcripts/compensation_leveling_discussion.transcript.md`
- `transcripts/final_hiring_committee_debrief.transcript.md` (conditions section)
- `internal-notes/compensation_guidance.docx`

---

## Q57 ★ — "One-paragraph summary to the rest of the org"

**Ideal synthesis behavior**
A strong answer is a *real* paragraph, not a list, capturing: what we hired (L6 Senior Staff), why (depth + role need), what's still uncertain (cross-team scope, JVM ramp, decisiveness pattern), how we're mitigating (Devon pairing, structured review, Marcus J ramp), and the keystone first-year project (dedup rebuild).

This is a stress test for whether TalkBack can produce executive-style narrative from disparate inputs.

**Expected citations**
- All committee artifacts

---

## Q63 ★ — "If Alex misses cross-team milestones at month 3, what's the root cause?"

**Ideal synthesis behavior**
Hypothesis-shaped answer that ties future risk back to documented concerns: the most likely cause is the cross-team scope ownership Nathan flagged — Alex may revert to consensus-building when partner teams push back, instead of holding the line. Right intervention: structured weekly RACI sync between Alex, the SRE lead, and the Realtime Infra lead (Priya was supposed to set up the first one in month 1). Secondary cause: JVM ramp slowing his ability to get into the code, which compounds.

**Expected citations**
- `transcripts/values_behavioral_interview.transcript.md` (consensus pattern)
- `transcripts/final_hiring_committee_debrief.transcript.md` (Nathan's concern)
- `internal-notes/final_committee_notes.docx` (mitigation plan)

This is the question that shows TalkBack reasoning *forward in time* using documented concerns and mitigations — most powerful demo finish.
