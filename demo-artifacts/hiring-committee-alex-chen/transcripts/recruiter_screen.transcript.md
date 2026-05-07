# Recruiter Screen — Alex Chen

**Round:** Recruiter Screen
**Date:** 2026-03-09
**Duration:** 32 minutes
**Recruiter:** Marcus Webb (Senior Recruiter)
**Candidate:** Alex Chen
**Format:** Phone (Zoom audio only — no video by candidate request, "joining from car between meetings")
**Tooling:** Zoom Cloud Recording, auto-transcribed via Otter, lightly cleaned by Marcus

---

**[00:00:12] Marcus Webb:** Hey Alex, thanks for making time. Can you hear me okay?

**[00:00:15] Alex Chen:** Yeah, you sound great. Sorry, audio-only — I'm parked in a garage between two things.

**[00:00:21] Marcus Webb:** No worries at all. So just to set context — I'm Marcus, I lead recruiting for the Platform org. The role we'd love to talk to you about is Senior Staff Engineer on the Notification Infrastructure team. Priya Raman is the hiring manager. Have you and Priya already chatted?

**[00:00:38] Alex Chen:** We exchanged a couple of LinkedIn messages a few weeks ago, but nothing real. She mentioned you'd reach out.

**[00:00:46] Marcus Webb:** Perfect. So this screen is mostly to make sure we're aligned on the role, your motivations, comp expectations — pretty informal. Want to start with a quick walkthrough of what you've been up to?

**[00:01:02] Alex Chen:** Sure. So I'm currently at Pageform — five years now. I joined as a Staff Engineer and got promoted to Senior Staff about eighteen months ago. Most of my time there has been on what we call the Reach platform, which is basically our notification and engagement infrastructure. Email, push, in-app, the whole stack. I led the migration from a polling-based architecture to an event-driven one — Kafka backbone, multi-region, with retries, dedup, the usual.

**[00:01:48] Marcus Webb:** Nice. And before Pageform?

**[00:01:51] Alex Chen:** Three years at Tessera Health on their patient messaging system. Smaller scale, but high-stakes — we had to be HIPAA-compliant and the messages mattered. That's where I got my distributed systems chops, honestly.

**[00:02:10] Marcus Webb:** Got it. And what's prompting the look right now? You said you got promoted eighteen months ago, so you're not job-hopping out of frustration, I assume.

**[00:02:21] Alex Chen:** Yeah, no. Pageform's been good to me. The honest answer is two things. One, I've kind of solved the problem I came to solve — the platform is mature now, we're in optimization mode, and I'm spending more time in roadmap meetings than I am building. Two, I'm interested in the AI infrastructure side of what your company is doing. I've done a little bit of AI feature work at Pageform — we shipped a smart-batching system that uses a small ranking model to bundle notifications — but it's not the core of the role.

**[00:03:08] Marcus Webb:** That tracks. So just so I'm direct — the Senior Staff role here, the scope is pretty broad. You'd be the technical lead for notification infra, but you'd also be expected to influence how the rest of Platform thinks about event-driven architecture. There's a lot of cross-org work. Does that energize you or drain you?

**[00:03:35] Alex Chen:** Energizes me, mostly. I'd say my honest weakness is that I sometimes over-index on getting consensus instead of just calling the shot. But I've been trying to be more decisive over the last year.

**[00:03:51] Marcus Webb:** Useful self-awareness. Let me ask about comp. What are you looking for, ballpark?

**[00:04:01] Alex Chen:** I'm at — let me think — base is about 285, target bonus 15%, and my equity grant has about 1.2 million unvested. I'd want to be made whole on the equity, and ideally a bump on base into the low 300s. I'm not chasing a huge number. I care more about the role and the team.

**[00:04:38] Marcus Webb:** Okay, that's helpful. I think we can work in that range. Senior Staff at our company — base band is roughly 295 to 360, and the equity grant for this level is — let me not quote a number until I confirm with Emily, but it's competitive. We can definitely make you whole.

**[00:05:02] Alex Chen:** Cool.

**[00:05:04] Marcus Webb:** One question I always ask — are you in any other active processes?

**[00:05:09] Alex Chen:** I'm chatting with two other companies. One is a later-stage startup, the other is a public company. I'd rather not name them, but both are at similar levels. I'm honestly most excited about your team — the AI angle is what's pulling me — but I want to be transparent that there's a clock.

**[00:05:32] Marcus Webb:** I appreciate that. What's the rough timeline?

**[00:05:36] Alex Chen:** I'd want to make a decision by — call it end of April, early May.

**[00:05:42] Marcus Webb:** Okay. We can definitely move at that pace. Our loop is hiring manager, then a technical deep dive, system design, values, and then committee. We can compress it into about three weeks if you're flexible on timing.

**[00:06:01] Alex Chen:** That works.

**[00:06:03] Marcus Webb:** Couple more questions and then I'll let you go. Can you tell me about a time you had to make a hard call without consensus?

**[00:06:14] Alex Chen:** Yeah. About a year ago we were debating whether to migrate our notification scheduler off of our existing in-house cron service onto Temporal. Half the team wanted to keep the in-house thing — they'd built it, they knew it. The other half wanted Temporal. The debate was going in circles. I made the call to do a six-week spike on Temporal with a rollback plan, set a hard deadline, and we'd commit either way at the end of the spike. That defused the politics, gave us real data, and we ended up moving to Temporal. The point is I didn't pick a side — I picked a process.

**[00:07:11] Marcus Webb:** Hmm, I like that. Last one — what would make this *not* the right role for you?

**[00:07:21] Alex Chen:** If the role is mostly maintenance and not greenfield, I'd lose interest. I want to build, not just keep something running. And — I don't know how to phrase this without sounding arrogant — if my manager is more junior than me, that's usually fine, but if they don't have technical depth, that grates on me.

**[00:07:48] Marcus Webb:** Priya's a strong technical leader, so I think you're good there. Okay, anything you want to ask me?

**[00:07:56] Alex Chen:** What's the team's biggest unsolved problem right now?

**[00:08:02] Marcus Webb:** Honestly, I'd let Priya answer that better, but from what I hear in standups — multi-region consistency on the notification dedup layer. They've had two incidents in the last six months where the same push got delivered twice across regions during a failover. So that's probably going to come up in your loop.

**[00:08:25] Alex Chen:** Good to know. I've actually dealt with that exact thing.

**[00:08:31] Marcus Webb:** Save it for Diego or Sara. Okay — I'll set up the hiring manager round with Priya for next week. Anything else?

**[00:08:42] Alex Chen:** Nope. Thanks, Marcus.

**[00:08:45] Marcus Webb:** Talk soon.

---

## Recruiter notes (Marcus, post-call)

- Strong on motivation. Genuinely excited about AI infra angle, not just chasing comp.
- **Comp:** wants ~310 base + make-whole on ~1.2M equity. Within band. No surprise.
- **Competing offers:** two other companies, both "similar level." Did not name. Decision deadline early May.
- Self-aware about over-consensus tendency. Could be a leveling concern — Senior Staff should call shots, not socialize them.
- Mentioned multi-region dedup unprompted as something he's done. Diego/Sara should probe.
- Recommend: move to hiring manager round.
