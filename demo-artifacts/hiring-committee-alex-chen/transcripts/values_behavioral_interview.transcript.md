# Values & Behavioral Interview — Alex Chen with Rohan Mehta

**Round:** Values & Behavioral
**Date:** 2026-04-06
**Duration:** 48 minutes
**Interviewer:** Rohan Mehta (Senior Engineer, Realtime Infra)
**Candidate:** Alex Chen
**Format:** Zoom video, recorded
**Tooling:** Zoom Cloud, transcript via Zoom AI Companion (lightly cleaned)

---

**[00:00:13] Rohan Mehta:** Hey Alex. Rohan, Senior on Realtime Infra. This is the values round.

**[00:00:18] Alex Chen:** Hi Rohan.

**[00:00:20] Rohan Mehta:** Just to set expectations — this round is mapped to our company values. We have five: customer obsession, ownership, technical truth, deliberate decisions, and growth orientation. I'll ask you behavioral questions tied to those. I'll be looking for specific stories, not aspirations.

**[00:00:42] Alex Chen:** Got it.

**[00:00:45] Rohan Mehta:** Let's start with customer obsession. Tell me about a time you changed a technical decision based on customer feedback.

**[00:00:55] Alex Chen:** Sure. Last year we built a webhook-based delivery confirmation system at Pageform. Engineering decision was to send one webhook per delivery event. After a few weeks of beta, two of our largest customers told us their webhook ingestion infrastructure was getting hammered — they were getting 2-3x the webhook traffic they expected because of retries and per-recipient confirmations. They asked us for batched webhooks instead. From an engineering standpoint, batching introduced a delay and complicated the contract. But the customers' point was valid — at their scale, individual webhooks were a tax. We rebuilt it as a batched stream with configurable batch size. Took us about a month. I led that change.

**[00:02:01] Rohan Mehta:** Was anyone on your team against the change?

**[00:02:06] Alex Chen:** Yes. One of our Senior engineers — Devi — argued we should put the burden on the customer side, that they should consolidate the webhooks. I pushed back. My view was if two of our top five customers are saying the same thing, that's not a customer skill issue, that's a product issue.

**[00:02:31] Rohan Mehta:** How did you handle the disagreement?

**[00:02:34] Alex Chen:** Wrote a doc, scheduled a 30-minute meeting with the team, walked through the data. Devi still didn't fully agree, but she said she'd unblock the work. We did the work. Six months later she told me unprompted that she'd been wrong — the batched API has been adopted by ten more customers since.

**[00:03:01] Rohan Mehta:** Good. Ownership next. Tell me about a time you owned something you didn't have to.

**[00:03:09] Alex Chen:** A bit over a year ago, we had a customer escalation about delivery delays in APAC. The escalation came in on a Friday afternoon. The on-call engineer was junior, and APAC delivery is in our team's domain but it's typically owned by another senior eng who was on PTO. I wasn't on call. I picked it up because nobody else was going to dig in until Monday, and the customer was a strategic account. I spent Saturday morning tracing it — turned out to be a misconfigured DNS record for our APAC SES endpoint that was causing intermittent failures. Fixed it, wrote up the post-mortem, sent it to the customer Monday.

**[00:04:14] Rohan Mehta:** Why didn't you escalate to someone else?

**[00:04:18] Alex Chen:** I considered it. The next-up senior on the rotation was already overloaded with another P1, and the customer was getting noisier. I made the call that I had the context to fix it faster than I could hand it off.

**[00:04:39] Rohan Mehta:** Hmm. Have you ever owned something you regretted owning?

**[00:04:46] Alex Chen:** [pause] Yeah. About two years ago I picked up a project — porting our internal metrics library to a newer telemetry standard — that was technically a platform-team project. I owned it because I had time and the platform team was understaffed. It dragged on for six months, took focus away from my own roadmap, and the platform team eventually re-did some of it because their architectural opinions had changed. I should have just accepted it wasn't my fight.

**[00:05:24] Rohan Mehta:** Useful answer. Technical truth.

**[00:05:30] Alex Chen:** Mm.

**[00:05:32] Rohan Mehta:** Tell me about a time you delivered hard technical news to a stakeholder who didn't want to hear it.

**[00:05:40] Alex Chen:** A PM at Pageform wanted to ship a feature that would have broken our API contract for older customers. The feature was a change to how we returned delivery status — moving from a string to a structured object. PM's argument was the new format was strictly better and old customers should adapt. I disagreed — backwards compatibility is a contract, and breaking it without a deprecation period would tank trust. We went back and forth for about two weeks. I wrote a doc with the data on how many customers were on the old format, the projected revenue at risk if we lost any, and proposed a versioned API instead. PM didn't love adding versioning — it slowed his roadmap — but he eventually agreed.

**[00:06:41] Rohan Mehta:** What was your relationship with that PM like afterward?

**[00:06:46] Alex Chen:** Took a few weeks to recover. He felt I was blocking him. I think the doc helped because it wasn't an emotional argument, it was data. We're fine now.

**[00:07:04] Rohan Mehta:** When have you been wrong technically and had to admit it?

**[00:07:10] Alex Chen:** [pause] Eighteen months ago I argued strongly that we should adopt a particular open-source service mesh — Linkerd. I'd done research, written a doc, presented it. After we adopted it, the operational overhead turned out to be much higher than I'd projected. We struggled with it for nine months before reverting to a simpler approach. I had to write the post-mortem on my own decision and admit publicly to the team that I'd underweighted the ops cost.

**[00:07:53] Rohan Mehta:** How did the team respond?

**[00:07:57] Alex Chen:** Mixed. Most were forgiving — they said it was a reasonable bet, just didn't pay off. A couple were quietly frustrated that I'd been so confident at the start. I think the lesson I took is that I should temper my confidence on things I haven't actually run in production myself.

**[00:08:24] Rohan Mehta:** Good. Deliberate decisions. Tell me about a decision you delayed on purpose.

**[00:08:33] Alex Chen:** [pause] Hmm. We had a debate about whether to migrate our notification scheduler from in-house cron to Temporal. Strong opinions on both sides. Rather than pick, I scoped a six-week spike with explicit success criteria. We'd commit either way at the end of the spike based on the data. I deliberately did not pre-decide — I told the team I genuinely didn't know which side would be right. We ended up moving to Temporal but it was close.

**[00:09:14] Rohan Mehta:** Why is that "deliberate"?

**[00:09:17] Alex Chen:** Because the alternative is to pick by gut, which on this kind of question is almost always biased toward whoever has the loudest opinion. By forcing a six-week spike with criteria, the decision was made by the data, not the politics.

**[00:09:38] Rohan Mehta:** Have you had a deliberate decision blow up?

**[00:09:43] Alex Chen:** Probably the Linkerd one I just mentioned. That was a deliberate decision — we did a spike, we measured, we wrote a doc, we voted. The decision process was clean. The decision was wrong because the data we got from the spike didn't generalize to the production reality.

**[00:10:09] Rohan Mehta:** What's the lesson?

**[00:10:12] Alex Chen:** Spike conditions don't always match production conditions. For infra changes, you should always have a defined "we revert if X happens" criterion, even after you commit. We didn't have a clean revert criterion for Linkerd, which is partly why it dragged on.

**[00:10:35] Rohan Mehta:** Growth orientation. What's something you've actively worked on improving in the last year?

**[00:10:43] Alex Chen:** Two things. One — I've been deliberately trying to be more decisive in less data-rich situations. My tendency is to wait for more data before calling a shot. I've forced myself to make calls earlier and accept being wrong sometimes. Two — communication for non-engineering audiences. I write dense docs. I've been getting feedback from my PM and applying it.

**[00:11:18] Rohan Mehta:** Concrete example of being more decisive?

**[00:11:23] Alex Chen:** Two months ago we had a debate about whether to adopt OpenTelemetry tracing for our notification path. The data was inconclusive — some teams loved it, some teams said it was a tax. Old me would have asked for another spike. New me said we'd commit to OpenTelemetry for six months and re-evaluate. Made the call in a 30-minute meeting. The team adapted.

**[00:11:54] Rohan Mehta:** And on the communication side — what's the feedback you're applying?

**[00:11:59] Alex Chen:** My PM at Pageform — Anjali — told me my docs were technically perfect but business-illiterate. She said I'd write a 12-page architecture doc and the PM-relevant info was in paragraph six on page eight. So I've been front-loading every doc with a one-paragraph business framing — what's the customer impact, what's the revenue exposure, what's the team cost. Then the technical content. It's helped. Anjali noticed.

**[00:12:39] Rohan Mehta:** Last broader question. Why this company?

**[00:12:46] Alex Chen:** Honestly, two reasons. One — the AI infra angle. I've done a small amount of AI infrastructure work and I want to do a lot more. Pageform is mostly product features now. Two — the engineering culture I've heard about. Marcus and Priya both described a written-doc-heavy culture, which fits how I work. I've been at Pageform five years and the culture there has gotten more meeting-heavy as we've grown. I want to go somewhere that takes written reasoning seriously.

**[00:13:24] Rohan Mehta:** What's the thing about our culture you'd worry about?

**[00:13:30] Alex Chen:** I've heard the team is currently in a high-incident-response mode. From what Priya described. I'd worry about being on-call-heavy and not getting enough heads-down architecture time. I'd want to make sure that as the platform stabilizes, the on-call load drops.

**[00:13:55] Rohan Mehta:** Yeah, fair. We're working on it. Last — I'm going to go meta. What did you think of this interview?

**[00:14:05] Alex Chen:** [laughs] You want me to grade you?

**[00:14:09] Rohan Mehta:** No, just tell me what you think.

**[00:14:14] Alex Chen:** I think you got real answers from me, which is a sign the interview was working. Some values rounds I've been in feel like a checkbox exercise. This felt like a conversation. The Linkerd question was probably the most useful thing — it forced me to re-examine a decision I'd already processed.

**[00:14:42] Rohan Mehta:** Fair. Anything you want to ask me?

**[00:14:47] Alex Chen:** What's a value you think the team currently struggles with?

**[00:14:52] Rohan Mehta:** [pause] Ownership. We have a habit of waiting for the obvious owner to step in. That's part of why the dedup problem we're rebuilding got as bad as it did before someone owned the rebuild.

**[00:15:11] Alex Chen:** That's good context.

**[00:15:14] Rohan Mehta:** Cool. Thanks Alex.

---

## Rohan's notes

- Strong on customer obsession and technical truth. Webhook batching story is a clean example — listened to customers, pushed through internal pushback, delivered.
- Linkerd self-critique was unprompted and specific. He didn't deflect.
- **Concern:** Decisiveness. He's *aware* he's not decisive enough — that's good. But "I've been working on it for a year" with two examples doesn't fully convince me. The OpenTelemetry example was decisive within an already-narrow window. The bigger question is whether he can call the shot on something *bigger* than a tracing library, where being wrong has more consequence.
- **Concern (specific to leveling):** A Senior Staff IC needs to be the one calling shots, not the one converging the team. His self-described tendency toward consensus-building is a Staff (L5) trait. At L6 we expect more "this is the call, here's why, write up your dissent."
- Recommendation: **Hire**, but at L5 Staff, not L6. He's a strong L5. He's *almost* L6 but the consensus-tendency knock is real for our culture.
- Caveat: I don't have full context on his peer feedback at Pageform. If his current manager or peers say he's actually more decisive than he describes, my read might shift.
