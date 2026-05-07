# Technical Deep Dive — Alex Chen with Diego Alvarez

**Round:** Technical Deep Dive (coding + systems debugging)
**Date:** 2026-03-23
**Duration:** 62 minutes
**Interviewer:** Diego Alvarez (Staff Engineer, Notifications)
**Candidate:** Alex Chen
**Format:** Zoom + CoderPad
**Tooling:** Zoom Cloud (auto transcript, manually corrected by Diego)

---

**[00:00:11] Diego Alvarez:** Hey Alex. I'm Diego, I'm on the notifications team. Priya told you what this round is, right?

**[00:00:19] Alex Chen:** Yeah, technical deep dive — coding plus a debugging walk-through.

**[00:00:24] Diego Alvarez:** Right. We're going to do about 25 minutes of coding in CoderPad and then 30 minutes on a real-world debugging problem. The coding part isn't a brain teaser — it's deliberately a real problem we hit. Sound good?

**[00:00:39] Alex Chen:** Sounds good.

**[00:00:42] Diego Alvarez:** Okay, here's the setup. You're given a stream of notification events. Each event has an ID, a user ID, a campaign ID, and a timestamp. Some events are duplicates — same user, same campaign, within a 30-second window. Write a function that takes the stream and returns the deduplicated set. Optimize for memory because the stream can be 10 million events per minute.

**[00:01:08] Alex Chen:** Okay. Quick clarification — is the stream sorted by timestamp?

**[00:01:14] Diego Alvarez:** Roughly, but assume some out-of-order events within a few seconds.

**[00:01:21] Alex Chen:** Got it. And the dedup window is 30 seconds — so two events with the same user and campaign within 30 seconds of each other are duplicates, but if they're 31 seconds apart they're not?

**[00:01:34] Diego Alvarez:** Correct.

**[00:01:36] Alex Chen:** Okay, my first thought is a sliding window with a hash map keyed on (user, campaign), where the value is the timestamp of the last seen event. As I process each event I check the map — if there's a hit and the delta is under 30 seconds, drop. Otherwise emit and update the map.

**[00:02:05] Diego Alvarez:** Memory implications?

**[00:02:08] Alex Chen:** That map grows unbounded if I never clean it. So I need to periodically evict entries older than 30 seconds. I could do that lazily — every N events, walk the map and drop expired entries. Or I can use a min-heap keyed on timestamp alongside the map and pop expired entries off the front.

**[00:02:34] Diego Alvarez:** Let's go with the heap approach. Code it up.

**[00:02:39] Alex Chen:** Sure, let me — Python okay?

**[00:02:42] Diego Alvarez:** Python's fine.

**[00:02:45] Alex Chen:** [typing]

```python
import heapq
from typing import Iterator

DEDUP_WINDOW_S = 30

def dedup_stream(events: Iterator[dict]) -> Iterator[dict]:
    seen = {}  # (user_id, campaign_id) -> timestamp
    expiry = []  # min-heap of (timestamp, key)
    for event in events:
        key = (event['user_id'], event['campaign_id'])
        ts = event['timestamp']

        # evict expired
        while expiry and expiry[0][0] < ts - DEDUP_WINDOW_S:
            expired_ts, expired_key = heapq.heappop(expiry)
            if seen.get(expired_key) == expired_ts:
                del seen[expired_key]

        # check dedup
        if key in seen and ts - seen[key] < DEDUP_WINDOW_S:
            continue

        seen[key] = ts
        heapq.heappush(expiry, (ts, key))
        yield event
```

Okay, I think that works. The only subtle thing is the eviction check — I'm only deleting from `seen` if the heap entry timestamp matches the current value, otherwise I might delete an entry that was updated to a more recent timestamp.

**[00:05:48] Diego Alvarez:** Walk me through what happens if events come in out-of-order. Say user A campaign X at t=10, then user A campaign X at t=8.

**[00:06:02] Alex Chen:** [pause] Hmm. So my map has the t=10 entry. The t=8 event comes in, my dedup check is `ts - seen[key] < 30`, which is `8 - 10 = -2`, which is less than 30, so I'd dedup it. That's actually wrong, isn't it? Because t=8 happened before t=10, so the t=10 was already a duplicate of *that*, but I yielded it.

**[00:06:32] Diego Alvarez:** Mm-hm.

**[00:06:34] Alex Chen:** [thinking] So actually with out-of-order events the whole approach needs a buffer. I'd need to delay yielding by — say, the maximum reorder window, like 5 seconds. Buffer events for 5 seconds, sort within the buffer, then run the dedup logic on the sorted stream.

**[00:07:06] Diego Alvarez:** Yeah. That's the right answer. Code that up.

**[00:07:11] Alex Chen:** [typing]

```python
import heapq
from collections import deque
from typing import Iterator

DEDUP_WINDOW_S = 30
REORDER_BUFFER_S = 5

def dedup_stream(events: Iterator[dict]) -> Iterator[dict]:
    seen = {}
    expiry = []
    buffer = []  # min-heap of (timestamp, event)

    for event in events:
        heapq.heappush(buffer, (event['timestamp'], event))
        # flush events older than the reorder buffer
        # use the *latest* timestamp seen as our "now"
        now = event['timestamp']
        while buffer and buffer[0][0] <= now - REORDER_BUFFER_S:
            ts, ev = heapq.heappop(buffer)
            yield from _process(ev, seen, expiry)

    # flush remaining
    while buffer:
        ts, ev = heapq.heappop(buffer)
        yield from _process(ev, seen, expiry)


def _process(event, seen, expiry):
    key = (event['user_id'], event['campaign_id'])
    ts = event['timestamp']
    while expiry and expiry[0][0] < ts - DEDUP_WINDOW_S:
        expired_ts, expired_key = heapq.heappop(expiry)
        if seen.get(expired_key) == expired_ts:
            del seen[expired_key]
    if key in seen and ts - seen[key] < DEDUP_WINDOW_S:
        return
    seen[key] = ts
    heapq.heappush(expiry, (ts, key))
    yield event
```

**[00:11:32] Diego Alvarez:** Memory complexity?

**[00:11:35] Alex Chen:** O(N) where N is the number of distinct (user, campaign) pairs in the dedup window plus the reorder buffer. So roughly the throughput times the window plus buffer length. For 10M events/min and a 30-second window, that's 5M entries times — call it 100 bytes per entry — so 500MB, give or take. That's tight on a single instance.

**[00:12:08] Diego Alvarez:** So how would you scale it?

**[00:12:11] Alex Chen:** Partition by user_id. Each shard handles a subset of users, and dedup is local to a shard because two events with the same user always go to the same shard. Then you don't have a single 500MB map, you have N smaller maps across N workers.

**[00:12:34] Diego Alvarez:** Cool. Let's leave the coding here. I want to go to debugging.

**[00:12:42] Alex Chen:** Okay.

**[00:12:45] Diego Alvarez:** Real scenario. Production system. Notification send service is consuming from a Kafka topic, sending pushes via APNs and FCM. You start seeing a 5% error rate on the FCM path that wasn't there yesterday. Latency is also up — p50 went from 80ms to 220ms on the FCM call. APNs is fine. What's your first move?

**[00:13:14] Alex Chen:** First move is the obvious one — what changed yesterday? I'd check the deploy log for any change to the FCM client, our network config, our cert rotation, anything in the FCM dependency chain. If nothing changed on our side, I'd check FCM's status page.

**[00:13:38] Diego Alvarez:** Status page is green. Last deploy of the FCM service was three days ago.

**[00:13:44] Alex Chen:** Okay. Then I'd look at the error response codes. 5% errors — what's the breakdown? If it's all 5xx I'm thinking server-side issue at FCM. If it's 4xx I'm thinking something on our end like malformed payload. If it's a mix, maybe a specific cohort of payloads.

**[00:14:09] Diego Alvarez:** It's all 503s.

**[00:14:13] Alex Chen:** All 503s. That's "service unavailable" from FCM. Their status page says green but a 5% 503 rate at our scale doesn't feel like nothing. I'd check whether the 503s are uniform across our regions or concentrated in one. If concentrated, it might be a regional FCM issue or a network path issue. Also — do we share the same FCM client across all our send workers, or do we have per-region clients?

**[00:14:48] Diego Alvarez:** Per-region clients. The 503s are concentrated in EU-WEST.

**[00:14:55] Alex Chen:** EU-WEST. So either FCM has a regional issue affecting EU-WEST that isn't on their status page, or our EU-WEST egress is degraded, or our connection pool to FCM in EU-WEST is exhausted. The latency increase is consistent with connection pool exhaustion — requests are queueing.

**[00:15:25] Diego Alvarez:** What would you do to confirm the connection pool theory?

**[00:15:30] Alex Chen:** Check the metrics on the connection pool — pool size, active connections, queued requests. If queued requests is non-zero, that's confirmation. Also check whether we recently changed traffic — has EU-WEST send volume gone up?

**[00:15:52] Diego Alvarez:** Pool max is 100. Active connections are pegged at 100. Send volume is up 18% in EU-WEST week-over-week.

**[00:16:05] Alex Chen:** Okay, that's pretty conclusive. The fix is either bump the pool size, or — depending on what's bottlenecking — add more send workers. I'd also want to know why send volume is up 18% — is that organic growth or is a customer behaving badly? Because just bumping pool size masks the problem if it's a runaway customer.

**[00:16:33] Diego Alvarez:** Good. Let me push you. You bump pool size — what's the risk?

**[00:16:39] Alex Chen:** A bigger pool means more concurrent requests against FCM. If the underlying issue is FCM rate-limiting us, a bigger pool just gets us more 429s. So I'd want to know FCM's stated quota for our project, and where we are relative to it. 503s sometimes mask 429s in FCM's behavior — they soft-throttle by returning 503.

**[00:17:08] Diego Alvarez:** Yeah, that's a real thing in FCM. Okay, last question. Say you fix this, but a week later you see the same symptom, this time in US-EAST. What do you build to prevent the third occurrence?

**[00:17:24] Alex Chen:** Long-term fix is two things. One, autoscaling on the connection pool plus the worker count, with FCM rate-limit-aware backoff. Two, an alert that fires on connection pool utilization above, say, 75% sustained for five minutes, before we start seeing 503s. The right time to alert is on the precondition, not the symptom.

**[00:17:55] Diego Alvarez:** I like that. Tactical question — would you put that alert on the worker side or the FCM client side?

**[00:18:04] Alex Chen:** Hmm. [pause] I'd want to put it on the FCM client. The pool lives there, and that's where the precondition is observable. The worker side just sees latency.

**[00:18:21] Diego Alvarez:** What if the FCM client is a vendor library you don't control?

**[00:18:26] Alex Chen:** Then you wrap it. Wrap the FCM client in our own HTTP wrapper, instrument the wrapper. We do that for APNs already at Pageform — Apple's library is fine but we wrap it to get our own metrics.

**[00:18:46] Diego Alvarez:** Good. Switching to a different topic. Tell me about a hard production incident you owned end-to-end.

**[00:18:55] Alex Chen:** Last summer. We had a multi-region failover go bad. Primary went down — actual primary, not a drill — and the failover to the secondary region kicked in correctly, but we started seeing duplicate notifications for about a 90-second window. About 4 million duplicate sends went out before we caught it.

**[00:19:22] Diego Alvarez:** Why?

**[00:19:24] Alex Chen:** The dedup state — we keep a Redis-backed dedup token store — was replicated async across regions with a target lag of under 200ms. During the failover, the secondary region started accepting writes before the replication caught up. So the secondary's dedup state was about 800ms behind primary at the moment of the cutover. Sends that primary had already done weren't reflected in secondary's dedup state, so secondary re-sent them.

**[00:20:02] Diego Alvarez:** How did you fix it?

**[00:20:05] Alex Chen:** Two changes. First, the failover logic now waits for replication lag to be under 100ms before accepting writes in the secondary, with a hard timeout at 30 seconds — if we can't get there in 30 seconds we fail open and accept the duplicate risk to keep sending. Second, we added a deferred dedup check — every send is also written to a journal that gets reconciled across regions every five minutes, and if we detect a duplicate during reconciliation we record it for compliance reporting. Doesn't prevent the duplicate, but at least we know.

**[00:20:48] Diego Alvarez:** Hmm. The reconciliation step is interesting. Why didn't you make the dedup state synchronous instead?

**[00:20:57] Alex Chen:** [pause] We considered it. The latency hit was too high — a synchronous cross-region write would have added about 70-80ms p99 to every send. At our throughput that was a non-starter for the SLA.

**[00:21:23] Diego Alvarez:** Okay, but what about a hybrid — synchronous dedup writes only for high-stakes campaigns, async for the rest?

**[00:21:33] Alex Chen:** That would be a smarter design, yeah. We didn't do it because the customer-facing concept of "high-stakes campaign" didn't exist in our product. But if I were designing it from scratch, that's where I'd land.

**[00:21:53] Diego Alvarez:** Got it. Let me ask about the team. Have you led any cross-team initiatives?

**[00:22:01] Alex Chen:** Yes. The smart batching project I mentioned to Priya — that crossed Reach, Customer Engagement, and Applied ML. I was the technical lead across those three teams' contributions.

**[00:22:18] Diego Alvarez:** And how did you handle disagreements across teams?

**[00:22:23] Alex Chen:** Most cross-team disagreements I've seen are scope or priority disagreements, not technical ones. So I tried to make scope explicit early — we had a one-page RFC that listed who owned what, and we revisited it every two weeks.

**[00:22:48] Diego Alvarez:** Good. Question about depth. How comfortable are you with the JVM? A lot of our backend is on the JVM.

**[00:22:57] Alex Chen:** Honest answer — Pageform is mostly Go. I came up on Java in college and Tessera was a mix of Java and Python, but I haven't written serious Java in maybe three years. I can read it fluently. I'd need a few weeks of ramp to be productive in a Java codebase.

**[00:23:24] Diego Alvarez:** That's fair. We're mostly Kotlin/JVM with some Go. You'd ramp.

**[00:23:32] Alex Chen:** Good.

**[00:23:35] Diego Alvarez:** One more. What's something you've changed your mind about technically in the last two years?

**[00:23:43] Alex Chen:** Microservices. I used to be more dogmatic about service boundaries — every domain gets its own service. I've come around to the view that for small teams, a couple of well-organized monoliths with clean module boundaries is much better than ten microservices. The operational tax of microservices at small scale is way higher than the architectural benefit.

**[00:24:14] Diego Alvarez:** Cool. Anything you want to ask me?

**[00:24:20] Alex Chen:** What's the team's ratio of feature work to platform work right now?

**[00:24:25] Diego Alvarez:** Roughly 40% feature, 60% platform. We have explicit platform sprints. I think we're a healthier team than most because of it.

**[00:24:41] Alex Chen:** Good to hear.

**[00:24:43] Diego Alvarez:** Cool, that's our hour. Thanks Alex.

---

## Diego's notes (entered into Greenhouse)

- Coding: Solid. Hit the off-by-one on out-of-order events, but caught and corrected himself when I probed. Final code is shipped-quality. 4/5.
- Debugging: Strong. Walked the right hypothesis path on the 503/connection pool scenario. Identified the correct precondition alert. Knows FCM 503-vs-429 quirk. 5/5.
- Production ownership: Honest about a real failure. Did not dodge the question about why he didn't go synchronous on dedup — admitted the hybrid would have been a smarter design.
- **Concern:** JVM rust. We are 70% Kotlin/JVM. He'd ramp, but for a Senior Staff role I'd usually expect deeper depth in our primary language. Not a deal-breaker.
- **Concern (mild):** "I'd lose a round before misrepresenting" sounds great in interviews but I want to see whether he actually pushes back when stakes are real. Values round can probe.
- Recommendation: **Hire**. Confidence: high on technical, medium on language/stack ramp.
- Leveling: I think L6, but reasonable people could call him L5 given the JVM ramp and the team-scope step up.
