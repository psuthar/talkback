# System Design Review — Alex Chen with Sara Okafor

**Round:** System Design
**Date:** 2026-03-30
**Duration:** 64 minutes
**Interviewer:** Sara Okafor (Principal Engineer, Distributed Systems)
**Candidate:** Alex Chen
**Format:** Zoom + Excalidraw whiteboard
**Tooling:** Zoom Cloud + manual transcript clean-up by Sara
**Note:** Alex's whiteboard sketch was exported as JPG and is included as `images/candidate_whiteboard.jpg`

---

**[00:00:09] Sara Okafor:** Hi Alex. Sara, distributed systems lead. This is the system design round.

**[00:00:15] Alex Chen:** Hi Sara. Good to meet you.

**[00:00:18] Sara Okafor:** I'm going to give you a problem and we'll spend roughly 50 minutes on it. I want to see how you think more than I want to see a finished design. Pushing back is encouraged.

**[00:00:30] Alex Chen:** Got it.

**[00:00:33] Sara Okafor:** Design a globally-distributed notification platform. Multi-region, supports email, push, SMS, in-app. Throughput target: 5 billion notifications per day at peak. Strict ordering is not required, but exactly-once delivery within a 24-hour window is required for compliance reasons. Acceptable end-to-end latency is under 2 seconds p99 from accept to attempted delivery. Multi-region failover under 60 seconds RTO. Go.

**[00:01:04] Alex Chen:** Cool. Let me ask clarifying questions first. When you say exactly-once within 24 hours — is that a hard requirement, or a target? Because true exactly-once is famously hard, and most systems aim for at-least-once with deduplication.

**[00:01:25] Sara Okafor:** Treat it as an at-least-once with strong dedup that gives you exactly-once semantics from the customer's perspective. That's the actual product requirement.

**[00:01:37] Alex Chen:** Got it. And the 5 billion per day at peak — what's the burst pattern? Steady state or spiky?

**[00:01:46] Sara Okafor:** Spiky. Customer can push a campaign that sends 200 million in five minutes.

**[00:01:54] Alex Chen:** Okay, so peak QPS is north of 600k/s during a campaign blast. And what's a "campaign" — is that a customer-defined batch, or are these all transactional?

**[00:02:11] Sara Okafor:** Both. Roughly 70/30 transactional vs. campaign by volume.

**[00:02:18] Alex Chen:** Last clarifying — multi-region failover at 60s RTO is for the platform itself. Are downstream providers — APNs, FCM, SES — single-region or multi-region from our perspective?

**[00:02:33] Sara Okafor:** Treat them as multi-region with their own SLAs. Assume FCM gives you 99.9% in each region.

**[00:02:42] Alex Chen:** Okay. Let me sketch.

**[00:02:46] Alex Chen:** [sketching on Excalidraw]

```
+-----------+      +------------+    +-------------+
| Customer  | ---> | API Gateway| -> | Ingest      |
| API Calls |      | (regional) |    | Service     |
+-----------+      +------------+    +-------------+
                                            |
                                            v
                                    +-------------------+
                                    | Notification Topic|
                                    | (Kafka, regional) |
                                    +-------------------+
                                            |
                            +---------------+---------------+
                            v                               v
                      +-----------+                  +-----------+
                      | Dedup     |                  | Dedup     |
                      | Worker    |                  | Worker    |
                      +-----------+                  +-----------+
                            |                               |
                            v                               v
                      +-----------+                  +-----------+
                      | Send      |                  | Send      |
                      | Worker    |                  | Worker    |
                      +-----------+                  +-----------+
                            |
                            v
                      +-----------+
                      | Provider  |  (APNs / FCM / SES)
                      | Adapters  |
                      +-----------+
```

**[00:05:12] Alex Chen:** Let me walk through. Customer hits a regional API gateway. Routing is geo-DNS — customer hits the nearest region. Ingest service does auth, validates the payload, and writes to a regional Kafka topic. Dedup worker reads from Kafka, checks a regional Redis-backed dedup store, drops duplicates, and re-publishes to a "ready-to-send" topic. Send workers read from ready-to-send, hit the provider adapter, get a result back. Result is journaled.

**[00:05:58] Sara Okafor:** Where's exactly-once semantics enforced?

**[00:06:03] Alex Chen:** At the dedup worker. Dedup key is — for transactional, the customer's idempotency key plus the recipient. For campaign, it's the campaign ID plus the recipient.

**[00:06:21] Sara Okafor:** And the dedup store is regional. So a dedup token written in US-EAST isn't visible in EU-WEST. How do you handle a customer that sends the same notification to both regions?

**[00:06:38] Alex Chen:** Hmm. Multiple options. One — push the dedup token to a global store. Either a globally-replicated Redis cluster, or use a managed service like DynamoDB global tables. Two — make the regional dedup store eventually consistent across regions, with a cross-region replication stream, accepting that there's a window where you can have duplicates across regions. Three — partition customers to a primary region, so a given customer's traffic always goes to one region except during failover.

**[00:07:24] Sara Okafor:** Pick one and defend it.

**[00:07:28] Alex Chen:** I'd pick three with elements of two. Customer-pinned to a primary region for normal operation, async replication of dedup tokens cross-region for failover. The reasoning is that exactly-once requires consistent state, and the cheapest way to get consistent state is to never have to read from two regions in normal operation. You only need cross-region state during failover, and during failover you can tolerate slightly worse latency.

**[00:08:10] Sara Okafor:** What's your replication lag target?

**[00:08:14] Alex Chen:** Sub-second. Probably 200-300ms p99 cross-region.

**[00:08:22] Sara Okafor:** And what happens if you fail over before replication catches up?

**[00:08:28] Alex Chen:** [pause] You can have a window of duplicate sends. To handle that, the failover logic should wait for replication lag to clear below a threshold before accepting writes in the secondary. With a hard timeout, because you can't wait forever in a real outage.

**[00:08:54] Sara Okafor:** What's the timeout?

**[00:08:57] Alex Chen:** I'd start at 30 seconds. After 30 seconds we accept writes anyway and accept the dup risk.

**[00:09:08] Sara Okafor:** That's a 30-second send-side outage on the worst-case failover. Your RTO is 60 seconds. Do you have headroom?

**[00:09:18] Alex Chen:** [pause] Tight, yeah. If replication is genuinely lagged because of the very thing causing the outage, 30 seconds may not be enough. So I'd want a degraded mode — start accepting writes earlier with a flag that marks every send as "potentially-dup," then run a reconciliation pass after stability is restored to identify and report duplicates. We accept the duplicate risk during the degraded window, but we have a clean audit trail.

**[00:09:58] Sara Okafor:** That's reasonable. Let me push on the dedup worker. What's the failure mode if it crashes mid-batch?

**[00:10:08] Alex Chen:** Kafka offset semantics. The dedup worker uses transactional Kafka consumer-producer — it reads from the input topic, writes to the output topic, commits the dedup token, all in a Kafka transaction. If it crashes, the transaction aborts, the dedup token isn't committed, and a replacement worker re-reads the message.

**[00:10:38] Sara Okafor:** And the dedup token write to Redis isn't part of the Kafka transaction. So you can have a state where the Kafka commit succeeds but the Redis write didn't.

**[00:10:52] Alex Chen:** [pause] Right. So the Redis write needs to happen before the Kafka commit. That gives you a different problem — you can write the token to Redis but fail to publish to the output topic, leaving the token written but no send happening.

**[00:11:17] Sara Okafor:** Yep.

**[00:11:19] Alex Chen:** The standard way to handle this is to write the token with a TTL that's longer than the expected end-to-end latency, plus a "completed" flag. Worker writes the token in pending state, publishes to output, then a downstream confirms. If a downstream never confirms within the TTL, the token expires and a retry is allowed. Effectively a two-phase process.

**[00:11:53] Sara Okafor:** Good. That's basically what we do here. Let me change topic. Walk me through provider failure handling. APNs goes down. What's the system's behavior?

**[00:12:08] Alex Chen:** APNs has a regional failure or a global one?

**[00:12:14] Sara Okafor:** Start with global.

**[00:12:17] Alex Chen:** Global APNs failure. The send worker can't deliver pushes to iOS devices. Three things happen. One — circuit breaker on the APNs adapter trips after some error threshold, like 50% errors over 30 seconds, and the adapter starts shedding load. Two — failed sends go to a retry queue with exponential backoff. Three — alerts fire to ops.

**[00:12:48] Sara Okafor:** What's the retry queue look like? In-memory? Persisted?

**[00:12:54] Alex Chen:** Persisted. Same Kafka topic with a retry-count header, or a dedicated retry topic. In-memory retry queues lose data on restart, which is unacceptable for compliance.

**[00:13:14] Sara Okafor:** What's the max retry duration?

**[00:13:17] Alex Chen:** Depends on the notification class. For transactional, like a security code, you might retry for only a minute and then drop with a delivery-failed signal back to the customer. For campaign, you might retry for an hour. The notification class is a property of the message.

**[00:13:42] Sara Okafor:** Now make APNs regional. APNs in US-EAST is down, APNs in US-WEST is fine.

**[00:13:50] Alex Chen:** [pause] Hmm. The provider adapter would normally talk to APNs in its own region. If US-EAST APNs is down, we could route US-EAST sends through the US-WEST APNs endpoint. APNs is itself globally addressable for our use case — Apple's the one routing to the device. So we can fail over the adapter without failing over the entire region.

**[00:14:29] Sara Okafor:** Right. And what's the cost of that?

**[00:14:33] Alex Chen:** Cross-region egress and a small latency hit. Maybe 30-40ms extra round-trip.

**[00:14:43] Sara Okafor:** Good. Let me push on observability. I always ask. You're on call at 3am. The platform's overall send success rate just dropped from 99.6% to 98.2%. What dashboard do you go to first?

**[00:14:59] Alex Chen:** Send success rate by provider. Because 99.6 to 98.2 is — that's 1.4 percentage points, which on 5 billion a day is meaningful. I want to know if it's one provider or all providers. If it's one provider, that's a provider issue. If it's all, it's something on our side.

**[00:15:25] Sara Okafor:** It's email. SES specifically.

**[00:15:30] Alex Chen:** Okay. SES dashboard — bounce rate, throttle rate, error code distribution. SES has its own throttling that depends on your sending reputation — if a customer's been sending bad lists, your reputation drops and SES starts throttling. So I'd check whether one customer's sends spiked the bounce rate. 5xx on SES could also be SES issues, but they're rare. More likely is reputation throttling.

**[00:16:10] Sara Okafor:** What metric tells you reputation specifically?

**[00:16:15] Alex Chen:** SES exposes a reputation score via their API. We'd want a dashboard for that, alerting if it drops below a threshold. Also bounce rate, complaint rate, both leading indicators.

**[00:16:34] Sara Okafor:** Have you actually built that?

**[00:16:37] Alex Chen:** Yes, at Pageform. We pull the SES reputation API every 15 minutes per customer and surface it to the customer in their dashboard.

**[00:16:51] Sara Okafor:** Cool. Let me change tack. Capacity planning. Tell me how you'd plan for a 10x traffic increase over 18 months.

**[00:17:02] Alex Chen:** Identify the bottlenecks at current scale. Probably Kafka topic partition count, dedup store throughput, and provider rate limits. Each scales differently. Kafka — repartition. Dedup store — shard. Providers — negotiate higher quotas, and prepare to batch at the provider level if possible.

**[00:17:30] Sara Okafor:** Kafka repartitioning is tricky.

**[00:17:34] Alex Chen:** It's painful. You can add partitions, but only forward — existing keyed messages don't redistribute. So planning for partition count needs to happen up-front. A common approach is to start with way more partitions than you need at current scale, accept the small overhead, and have headroom.

**[00:18:01] Sara Okafor:** What's "way more"?

**[00:18:04] Alex Chen:** I'd start at, say, 256 partitions for a topic that needs 32 today, on the assumption you'll grow into them.

**[00:18:18] Sara Okafor:** Reasonable. Now harder question. We have a customer that sends 70% of their volume in a 4-hour window every Tuesday. How do you handle that?

**[00:18:32] Alex Chen:** Two angles. First — that customer's spike shouldn't degrade other customers. So you need per-customer quota and rate limiting at the ingest layer. The customer's spike should fill their own quota, not consume shared capacity. Second — that customer's spike still needs to be handled. So either the customer pre-schedules with us so we can allocate capacity, or we have enough headroom to absorb the spike. Probably both.

**[00:19:08] Sara Okafor:** What's the implementation of per-customer quota at 600k QPS aggregate?

**[00:19:15] Alex Chen:** Token bucket per customer, distributed. Each ingest replica holds a local bucket that's periodically refilled from a global store. The global store is the source of truth for the customer's allotment. Local buckets give you low-latency rate-limit decisions without hitting the global store on every request.

**[00:19:43] Sara Okafor:** What's the staleness window on the local bucket?

**[00:19:48] Alex Chen:** Refresh every — 200ms, 500ms? The trade-off is that a stale bucket lets the customer briefly exceed their quota during a spike. For most rate-limiting use cases, a small overage is acceptable.

**[00:20:11] Sara Okafor:** What if the customer's product team threatens to leave because of a 5% over-throttle they care a lot about?

**[00:20:19] Alex Chen:** Then you tighten the bucket refresh, accept the latency hit, or move that customer to a dedicated lane.

**[00:20:31] Sara Okafor:** [pause] Okay. Let me ask a last meaty one. End-to-end exactly-once. Walk me through the failure modes you have *not* yet addressed.

**[00:20:46] Alex Chen:** [long pause] Let me think. I've covered ingest-to-dedup, dedup-to-send, and provider failures. What I haven't talked about is — provider-side dups. If we hand a notification to APNs and APNs's response is ambiguous — say, a timeout — we don't know if it succeeded or failed. If we retry, we might double-deliver. APNs gives us a unique notification ID we can use to check, but check-and-retry is expensive.

**[00:21:30] Sara Okafor:** Mm-hm.

**[00:21:32] Alex Chen:** I haven't talked about clock skew. Dedup windows depend on time. If our clocks across regions skew by more than a few seconds, dedup gets weird. We'd want NTP and ideally something stronger like AWS Time Sync or Google's TrueTime if we cared a lot.

**[00:21:55] Sara Okafor:** And?

**[00:21:58] Alex Chen:** Customer side — if a customer issues two API calls with the same idempotency key within the dedup window, we dedup them. If they issue them outside the dedup window, we don't, and that's expected. But if they expect 24-hour dedup and our window is shorter, we have a contract mismatch. Need to be explicit about the window in the API.

**[00:22:25] Sara Okafor:** Good. Last one — and you can stay on the regional failover topic. Tell me the corner case that scares you most about regional failover.

**[00:22:38] Alex Chen:** [pause] [longer pause]

**[00:22:55] Alex Chen:** Honestly — split brain. If our control plane decides the primary region is down and starts accepting writes in the secondary, but the primary is actually still reachable from some customers, you can have both regions accepting writes for the same customer simultaneously. You'd think this is straightforward, but in practice the primary doesn't always know it's been declared down.

**[00:23:25] Sara Okafor:** What's the mitigation?

**[00:23:28] Alex Chen:** Some kind of fencing. Either each region has a generation counter and refuses writes if it's not the highest generation, or you use a lease-based system where only one region holds the active lease at a time. Both are tricky to make bulletproof.

**[00:23:53] Sara Okafor:** Have you implemented fencing in production?

**[00:23:58] Alex Chen:** [pause] Not from scratch, no. Pageform's failover is more rudimentary — we rely on DNS for routing, and during failover we update DNS. We don't have explicit fencing. We're aware it's a gap.

**[00:24:22] Sara Okafor:** Got it. That's an honest answer. Most candidates pretend they've done it. Anything you want to ask me?

**[00:24:32] Alex Chen:** Yeah. What's the team's current solution for the dedup-during-failover problem Priya mentioned?

**[00:24:41] Sara Okafor:** We're rebuilding the dedup state to be synchronously replicated for high-stakes campaigns and async for the rest. The hybrid you described to Diego, basically.

**[00:24:57] Alex Chen:** That sounds right.

**[00:25:00] Sara Okafor:** Cool. Thanks Alex.

---

## Sara's notes

- **Strong** on the design fundamentals. Walked the standard distributed systems landscape capably — dedup, replication, retry, circuit breakers, capacity planning.
- Pushed back well when I challenged. Self-corrected when I pointed out the Kafka transaction / Redis write gap.
- **Honest** about not having implemented fencing from scratch. I respect that. We need someone who'll say what they don't know.
- **Concern (mild):** When I asked about regional failover for the third time, he hand-waved a little before he got to the split-brain answer. I had to push him there. A Senior Staff candidate I'd want to see split-brain front-of-mind, not pulled out under interrogation.
- **Concern (medium):** No formal verification background. He was clear about this in the hiring manager round and again here. For a Senior Staff role on this team, that's a real gap. We have one Principal in the org with TLA+ background; we'd need Alex to lean on her.
- Recommendation: **Strong hire** at L6 Senior Staff. The fencing/formal-methods gap is real but learnable. The depth on real-world distributed systems is exactly what the team needs.
- I'd want to pair him with our Principal on the dedup rebuild for the first six months as a stretch project that closes the formal-methods gap.
