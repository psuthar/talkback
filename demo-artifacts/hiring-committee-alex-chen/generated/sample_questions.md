# Sample TalkBack Questions — Hiring Committee for Alex Chen

These questions are organized by intent, with hard ones near the end of each section. The point of each question is to surface a specific kind of TalkBack value — single-source retrieval, multi-artifact synthesis, disagreement detection, or temporal reasoning.

The "★" markers indicate the questions most worth featuring in a live demo, because they force *cross-artifact* synthesis or expose disagreement.

---

## 1. Quick catch-up (a reviewer who hasn't read anything yet)

1. Who is Alex Chen and what role is he interviewing for?
2. Who interviewed Alex, and in what order?
3. What was the committee's decision?
4. ★ Did everyone on the committee agree, or was there disagreement?
5. What is the proposed comp package?
6. When does Alex need an offer to be effective?
7. Who chaired the committee?
8. What conditions, if any, are tied to the offer?

---

## 2. Technical depth

9. How did Alex describe the smart-batching system at Pageform?
10. ★ What was Alex's first hypothesis on the FCM 503 debugging scenario, and what evidence did he ask for to confirm it?
11. What did Alex propose as the architecture for a 5B/day notification platform?
12. What's Alex's view on synchronous vs async dedup state replication, and what trade-off did he name?
13. ★ What did Alex say *he hasn't done* in distributed systems, across the technical and system design rounds?
14. What language stack is Alex strongest in, and where does he have language gaps?
15. What was the off-by-one bug in Alex's dedup coding solution, and how did he correct it?
16. ★ Comparing the architecture diagram and the candidate whiteboard, what did Alex draw versus what he described?
17. What does Alex's approach to capacity planning look like for a 10x traffic increase?

---

## 3. Behavioral & values alignment

18. How does Alex approach disagreement with senior people?
19. ★ Tell me about a time Alex was wrong technically. What did he do about it?
20. How does Alex weigh customer signal versus internal engineering preference?
21. What value did Alex demonstrate most clearly in the values round, and what value was weakest?
22. What's Alex's stated weakness and what's he doing about it?
23. ★ Where did Alex's values-round responses *contradict* something else in the loop?
24. How does Alex describe his communication style for non-engineering audiences?
25. What did Alex say about his ideal manager?

---

## 4. Leveling

26. ★ Should Alex be hired at L6 Senior Staff or L5 Staff? What's the evidence on both sides?
27. Which interviewers recommended L6? Which recommended L5?
28. What was Rohan's specific case for down-leveling Alex?
29. ★ What argument moved Rohan from L5 to L6 during the committee debrief?
30. ★ What argument moved Nathan from lean-no-hire to L6 during the committee?
31. What does the JD say about L6 expectations, and where does Alex meet or fall short of those?
32. Has the company hired stretch-L6 candidates before? Did it work out?
33. ★ If Alex were hired at L5 instead of L6, what role constraint does that create for the dedup rebuild?

---

## 5. Disagreement analysis

34. ★ Where do Sara and Rohan most directly disagree about Alex?
35. ★ Did Diego and Sara agree on Alex's depth? Where did they differ?
36. What did Jenna Liu (PM observer) flag that no engineering interviewer flagged?
37. ★ Where in the loop did Alex's self-assessment match interviewer assessment, and where did it diverge?
38. Did any interviewer have a view that nobody else shared?
39. ★ Of the concerns raised, which were addressed in the committee debrief and which were left open?
40. Are there topics raised in Slack threads that did not come up in any interview transcript?

---

## 6. Role fit

41. Does Alex's experience scale align with the role's requirements (2B/day vs 800M/day)?
42. ★ Is Alex set up to lead the dedup rebuild, given what he knows and doesn't know? What support does he need?
43. What's the team's current biggest unmet need from a Senior Staff, and is Alex equipped to fill it?
44. How will Alex's JVM ramp affect his first-year impact?
45. Who's been pre-committed to support Alex's onboarding, and on what?

---

## 7. Compensation & competing offers

46. What's the recommended base, sign-on, and equity for Alex?
47. ★ Why did Marcus and Emily not recommend top-of-band L6 comp?
48. Are there competing offers, and how do they affect our offer strategy?
49. What's the comp committee's policy on top-of-band hires?
50. How much base flex does Marcus have without going back to comp committee?
51. ★ What's the scenario in which we should down-level Alex to L5 instead of overpaying L6?

---

## 8. Final recommendation & decision

52. ★ Given everything, what should the committee do?
53. What's the strongest reason to hire Alex? The strongest reason not to?
54. ★ Is there enough information to extend an offer today, or what's missing?
55. If the cross-team reference comes back weak, what should change about our offer?
56. What does success look like at the six-month review for Alex?
57. ★ If you had to write a one-paragraph summary of this hire to the rest of the org, what would it say?

---

## Bonus: synthesis stress tests (pick one or two for the demo)

58. ★ Across all seven interviews, what's the single most consistent strength reviewers cited about Alex?
59. ★ Across all seven interviews, what's the single most consistent concern reviewers cited?
60. What did the comp/leveling internal call reveal that didn't come up in committee?
61. ★ The Slack thread `slack_thread_concerns.png` raises a concern about Linkerd's nine-month cost. Did the committee debrief address that concern?
62. Reading the Pageform incident description in the technical deep dive, what's the architectural pattern the team is trying to *avoid* repeating here?
63. ★ Three months from now, Alex has shipped phase one of the dedup rebuild but is missing his cross-team commitment milestones. Based on what we know about him, what's the most likely root cause and what's the right intervention?
