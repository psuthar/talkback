# Hiring Manager Interview — Alex Chen with Priya Raman

**Round:** Hiring Manager
**Date:** 2026-03-16
**Duration:** 47 minutes
**Interviewer:** Priya Raman (Hiring Manager, Platform)
**Candidate:** Alex Chen
**Format:** Zoom video, recorded
**Tooling:** Zoom Cloud + transcript via Zoom AI Companion

---

**[00:00:08] Priya Raman:** Hey Alex, good to finally meet you over video. How's your week going?

**[00:00:13] Alex Chen:** Good, thanks. A little hectic — we're in the middle of a P1 we're tracking down — but I blocked off this hour cleanly.

**[00:00:23] Priya Raman:** Appreciate it. Want to start the way I always start? You give me your eight-minute version of how you got to where you are, and I'll interrupt with questions.

**[00:00:34] Alex Chen:** Sure. So I started in 2017 at Tessera Health right out of grad school — undergrad CS at UMich, master's at CMU. Tessera was patient messaging — push, SMS, secure email, all of it had to be HIPAA-compliant. I was on a four-person team, mostly heads-down on the delivery layer. We hit a wall around 2019 when we were trying to scale past 50 million daily messages and our monolith just couldn't keep up. I led the rewrite — broke out the delivery service, introduced a Redis Streams queue, eventually moved to Kafka. That work got me promoted to Senior Engineer.

**[00:01:31] Priya Raman:** When you say "led the rewrite" — what does that mean concretely? You were Senior, so I'm imagining you weren't the architect of record.

**[00:01:42] Alex Chen:** Fair pushback. I was technical lead — I owned the design, but my Staff Eng at the time, his name was Vinay Patel, was the architect of record. He approved the Kafka direction. I owned execution, scoping, and roughly 60% of the actual code. I'd say I architected the consumer side and Vinay architected the producer side and the contract between them.

**[00:02:18] Priya Raman:** That's a more honest answer than most candidates give me. Keep going.

**[00:02:23] Alex Chen:** [laughs] I joined Pageform in 2021 — 2020, sorry, late 2020 — as Staff Engineer on the Reach platform. Smaller team, but the scope was bigger. Reach handles all customer-facing notifications across email, push, in-app, SMS. About 800 million sends per day at peak, and we have multi-region failover requirements because our customers are global. The first big thing I did was lead the migration from a polling architecture — every service polled the campaign service for what to send — to an event-driven one. Kafka, dedup via Redis, retry workers. That cut our send latency p99 from like four seconds to under 600 milliseconds. I got promoted to Senior Staff at Pageform about eighteen months ago.

**[00:03:42] Priya Raman:** Tell me about a project you're particularly proud of from the Senior Staff time.

**[00:03:48] Alex Chen:** Probably the smart-batching project. We had a problem where users were getting too many notifications — a customer would push three campaigns in a window and a user might get three pushes back-to-back, which is a bad experience and tanks click-through. We built a system that takes the candidate set of notifications going to a user in a short window, runs them through a small ranking model, and either sends one bundled notification or drops the lower-ranked ones. Shipping that involved the inference path, the latency budget, the model retraining pipeline, and a ton of A/B testing. We saw a 23% lift in CTR and a 31% drop in unsubscribe rate.

**[00:04:48] Priya Raman:** Who built the model?

**[00:04:51] Alex Chen:** Our applied ML team — two folks. I worked closely with them on the inference latency budget. The model itself isn't my work; the production system around it is.

**[00:05:08] Priya Raman:** Good. I think a lot of candidates would imply they did the ML work too.

**[00:05:14] Alex Chen:** I'd rather lose the round than misrepresent that.

**[00:05:18] Priya Raman:** Okay, let me change tack. The role here is Senior Staff on Notification Infra. We do about 2 billion notifications a day, multi-region. Our biggest open problem is dedup correctness during regional failover. Twice in the last six months we've had a customer push delivered twice across regions because the dedup state didn't propagate fast enough. If you joined tomorrow, what's the first thirty days?

**[00:05:54] Alex Chen:** Honestly, the first thirty days I'd mostly listen. I've seen too many Senior Staff hires come in and start re-architecting on day three. So my first two weeks would be one-on-ones with every engineer on the team, reading recent design docs, reading the post-mortems on those two incidents you mentioned. Then weeks three and four I'd write up my read of the situation — not a re-architecture proposal, just "here's what I see, here's what I'd want to dig into more." Then I'd present that to you and the team and ask what's wrong with it.

**[00:06:47] Priya Raman:** What about the dedup problem specifically — without me telling you the architecture, what's your first hypothesis?

**[00:06:55] Alex Chen:** With the caveat that I'd want to look at the actual system — most multi-region dedup failures I've seen are either (a) the dedup token store isn't replicated synchronously across regions and the failover happens before replication catches up, or (b) the retry logic doesn't check dedup state before re-sending and you have a window where a retry from region A and an original from region B both fire. Less commonly, (c) clock skew across regions corrupts the dedup window. My first move would be to look at the timing of those two incidents and see whether the dedup miss was at the token-store level or the retry level.

**[00:07:50] Priya Raman:** That's pretty close to where we landed in the post-mortems. Both were retry-level — token store was fine, retry workers in the secondary region weren't checking dedup state during the cutover window.

**[00:08:05] Alex Chen:** That makes sense. The fix is probably making the dedup check transactional with the send, so you can't retry without re-checking. But it gets ugly with throughput.

**[00:08:18] Priya Raman:** Yeah. We have an open design doc on this. I'll have you talk to Sara about it in the system design round. Let me ask a softer question. Tell me about a time you disagreed with a more senior person.

**[00:08:35] Alex Chen:** Ah, sure. Last year my Director — at Pageform — wanted us to build a new notification template engine in-house. He had a strong opinion that off-the-shelf options were too restrictive. I disagreed. I thought we should adopt MJML and just build customizations on top, because owning a template engine end-to-end was going to be a year of work that didn't differentiate us. I wrote a doc — pros, cons, cost analysis — and pushed back in writing. He didn't agree at first. We actually got a third opinion from a Principal Eng in another org. She sided with me, mostly. We adopted MJML.

**[00:09:35] Priya Raman:** And how did your Director feel about that?

**[00:09:38] Alex Chen:** [pause] Honestly, it was a little tense for a few weeks. He didn't love being overruled. But we had a good one-on-one about it, I made it clear it wasn't personal, and now we have a totally normal working relationship.

**[00:10:01] Priya Raman:** Good. The reason I ask — Senior Staff at our company means you're going to disagree with VPs sometimes. We need people who'll push back in writing, not in hallway gossip.

**[00:10:15] Alex Chen:** Understood.

**[00:10:18] Priya Raman:** Question about scope. The team has 11 engineers — three Staff, four Senior, three Mid, one Junior. As Senior Staff, you'd be the most senior IC. There's no Principal on the team right now, though there's one in the broader org. How comfortable are you with the responsibility of being the technical north star for a team where there's no one above you on the IC ladder?

**[00:10:48] Alex Chen:** [pause] Honestly, that's a step up for me. At Pageform I have a Principal in my reporting chain — Vinay actually moved over with me, he's now Principal — and I lean on him. So this would be a stretch. I think I can do it, but I want to be honest that it's an upward step.

**[00:11:13] Priya Raman:** I appreciate that. Could you tell me where you'd lean on others if you joined?

**[00:11:19] Alex Chen:** Probably on capacity planning at our scale. 2 billion sends a day is bigger than what I'm running today — Pageform is around 800 million, like I said. The architectural patterns are similar but the operational reality at 2.5x scale is going to surprise me. I'd want to spend time with our SRE team early. And — I'd lean on Sara for distributed systems formal verification stuff. I'm strong on practical distributed systems but I'm rusty on the more formal side, like TLA+ work or anything Jepsen-adjacent.

**[00:12:08] Priya Raman:** Useful. Sara will love hearing you say that, by the way. What about people management? You don't have direct reports today, right?

**[00:12:17] Alex Chen:** Right. I've been a tech lead for four engineers — code reviews, design reviews, I'm in their growth conversations, but they don't formally report to me. I've thought about whether I want to manage. Right now I don't. I want to keep being an IC for the next few years.

**[00:12:42] Priya Raman:** Good — that's the right answer for this role. Last question and then I'll open it up. What's the thing you're worst at, that's costing you?

**[00:12:55] Alex Chen:** [pause] Probably written communication for non-engineering audiences. I write design docs that engineers love and PMs find dense. I've been working on it. I had a peer feedback round at Pageform where my PM said my docs are technically perfect but she has to read them twice to find the business impact. I've been trying to start every doc with the business framing now.

**[00:13:35] Priya Raman:** Useful. Okay, what do you want to ask me?

**[00:13:40] Alex Chen:** What's the engineering culture like in disagreements? Is it written-doc heavy or meeting-heavy?

**[00:13:48] Priya Raman:** Mostly written. We have a culture of decision docs — every non-trivial call is written down and circulated for 48 hours before it's locked in. Decisions don't survive Slack threads here. You'd fit in fine on that axis.

**[00:14:08] Alex Chen:** Good to hear. What's the team's biggest unmet need from a Senior Staff?

**[00:14:14] Priya Raman:** Architectural clarity. The team has been heads-down on incident response and feature work for so long that nobody's painted a clear two-year picture of where the platform is going. I want a Senior Staff who can do that — and the dedup problem we discussed is the immediate forcing function for it.

**[00:14:36] Alex Chen:** That's the kind of work I want to do.

**[00:14:39] Priya Raman:** Cool. We're going to bring you back for a technical deep dive with Diego, then a system design with Sara, then a values round, then committee. Marcus will set those up. Any concerns or asks before we go?

**[00:14:54] Alex Chen:** No, just thanks for the time. This was a real conversation, I appreciate that.

**[00:15:00] Priya Raman:** Likewise. Talk soon.

---

## Priya's notes (post-interview, dropped into Greenhouse)

- Strong technical foundation. Honest about scope of his contributions vs. team's. Did not over-claim ML work.
- Volunteered the right hypotheses on the dedup problem before I described the architecture.
- **Self-identified L6 stretch areas:** scale (2B vs 800M), formal distributed systems, technical north star with no Principal above him on the team. I think this is a strength, not a weakness — but committee will need to decide whether his stretches are normal-Senior-Staff stretches or whether they push him to L5.
- Communication: candid that his docs are dense for non-engineers. Plausible self-awareness. Watch in values round.
- Recommend: hire. Leaning L6 but I want Sara and Nathan's read on the scope question.
