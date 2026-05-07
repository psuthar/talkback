# Hiring Committee — Alex Chen, Senior Staff Engineer

A heterogeneous, deliberately-messy demo dataset for **TalkBack** that simulates a real hiring loop assembled over several weeks by recruiters, engineers, HR, and the hiring manager.

The goal is to demonstrate TalkBack as the surface that hiring committees actually need: async review across **multiple recordings**, grounded Q&A with citations, cross-artifact synthesis (transcript + ATS + Slack + scorecard), and *honest* disagreement analysis where reviewers do not all agree.

---

## Scenario

| | |
|---|---|
| **Candidate** | Alex Chen |
| **Role** | Senior Staff Engineer — Platform / Notification Infrastructure |
| **Decision** | Extend offer to Alex Chen for Senior Staff Engineer |
| **Premise** | We completed the interview process for Alex Chen. Review the recordings, transcripts, notes, and supporting artifacts to determine whether we should move forward with an offer. |
| **Stage** | Final Hiring Committee Debrief, awaiting committee decision |

---

## Folder layout

```
hiring-committee-alex-chen/
├── README.md                       (this file)
├── session_setup.md                premise, decision, stance distribution
├── manifest.json                   machine-readable artifact registry
├── videos/                         interview metadata (.json) + raw video stubs
├── transcripts/                    seven interview transcripts (.md)
├── documents/                      resume, JD, values, feedback packet (PDF + DOCX)
├── images/                         slack screenshots, architecture, whiteboard, thumbnails
├── internal-notes/                 compensation guidance, committee notes (DOCX)
├── exports/                        Greenhouse ATS CSV export
└── generated/                      sample questions, demo script, expected answers
```

---

## Suggested TalkBack upload flow

The point of TalkBack here is that **no single artifact answers the hiring question**. The committee needs to fuse signal from seven recordings, a feedback packet, an ATS export, and a few Slack threads to make a defensible call.

Recommended order — uploading in this sequence keeps the citation graph clean:

1. **Premise + decision** — paste from `session_setup.md`.
2. **Job description** — `documents/Senior_Staff_Engineer_JD.pdf`. Sets role expectations.
3. **Company values** — `documents/Company_Values.pdf`. Used by the values interview and committee tie-breakers.
4. **Candidate resume** — `documents/Alex_Chen_Resume.pdf` (the DOCX is a duplicate for tool-mix realism).
5. **Seven interviews, in chronological order** — for each: video + transcript + thumbnail.
   1. recruiter_screen
   2. hiring_manager_interview
   3. technical_deep_dive
   4. system_design_review
   5. values_behavioral_interview
   6. compensation_leveling_discussion
   7. final_hiring_committee_debrief  ← **this is the primary artifact for synthesis**
6. **Feedback packet** — `documents/Interview_Feedback_Packet.pdf`. Contains the scorecards and the disagreement.
7. **Slack screenshots** — three PNGs in `images/`. These reveal context that isn't in any transcript.
8. **ATS export** — `exports/greenhouse_export.csv`. Recruiter notes + status timeline.
9. **System design architecture** — `images/system_design_architecture.png`.
10. **Candidate whiteboard** — `images/candidate_whiteboard.jpg`. Rough sketch from the system design round.
11. **Internal notes** — `internal-notes/compensation_guidance.docx`, `final_committee_notes.docx`.

The **primary recording** is `final_hiring_committee_debrief` because it is where disagreement is named, not where it is resolved — that's the synthesis question.

---

## Recommended demo sequence

A 5–7 minute live demo flow is in `generated/demo_script.md`. The high-level arc:

1. Open with the premise and the decision card.
2. Ask "What is the committee's recommendation?" → TalkBack should hedge, because reviewers disagree.
3. Drill into the disagreement: "Who supports the hire and who has reservations, and why?"
4. Cross-artifact: "Does the system design round support a Senior Staff level, or does it suggest Staff?"
5. Pull in Slack: "Is there any context outside the interviews that affects leveling?"
6. Compensation: "What's the comp risk if we extend at L6?"
7. Close: "Given everything, what should the committee do?" → submit a stance.

---

## Which artifacts are intentionally messy

This dataset would not look real if everything were polished. The following artifacts are **deliberately rough**:

| Artifact | Why messy |
|---|---|
| `transcripts/technical_deep_dive.transcript.md` | Has interruptions, a wrong-answer-then-self-correction, and an unresolved follow-up. |
| `transcripts/system_design_review.transcript.md` | Candidate hand-waves regional failover; interviewer notes silence. |
| `images/candidate_whiteboard.jpg` | Hand-sketch style, not a clean diagram. Some boxes unlabeled. |
| `images/slack_thread_leveling.png` | Off-the-cuff debate; one engineer push-backs on the recruiter's level recommendation. |
| `images/slack_thread_concerns.png` | PM raises a communication concern that *no transcript explicitly contains*. |
| `exports/greenhouse_export.csv` | Real-world ATS export quirks: inconsistent capitalization, free-text recruiter notes, one recommendation typo. |
| `documents/Interview_Feedback_Packet.pdf` | One reviewer leaves their "concerns" section empty; another writes a paragraph. |
| `internal-notes/compensation_guidance.docx` | Slightly informal — author flips between "we" and "I", references a competing offer rumor. |
| `documents/Alex_Chen_Resume.docx` | One bullet is dated "2022 - present" but the candidate's tenure summary says "joined Jan 2023" — a small inconsistency a sharp committee should catch. |

---

## What good looks like in a TalkBack answer

For every question in `generated/sample_questions.md`, a strong response should:

- Cite the **specific** artifact (transcript timestamp, PDF section, image) it draws from.
- Name disagreement when reviewers diverge — do not flatten to a fake consensus.
- Pull from at least two artifact types when synthesis is required.
- Distinguish between *what the candidate said* and *what reviewers concluded*.

If a TalkBack answer cites only the feedback packet, the demo is failing — the value is in synthesis, not single-source lookup.
