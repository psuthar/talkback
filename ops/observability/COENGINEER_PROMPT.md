# Co-Engineer Prompt: New Relic Diagnostic Bundle

Use this prompt with the diagnostic bundle (markdown or JSON) to get ranked hypotheses and next actions.

---

**Instructions for the AI co-engineer**

Below is a diagnostic bundle from our TalkBack backend (Go API) observed over a time window. It contains:

- Metadata: timestamp, observation window, app name, and (if available) git context.
- NRQL query results: throughput, p95 latency, top transactions, error count, and top errors.

Please:

1. **Summarize** the health of the service in 2–3 sentences.
2. **Rank 3–5 hypotheses** (most likely first) for any problems you see (e.g. slow endpoints, error spikes, capacity).
3. **Suggest 3–5 concrete next actions** (e.g. add an index, scale a component, add a circuit breaker, inspect a specific transaction).

Keep answers concise and actionable. If the bundle shows no issues, say so and suggest one or two proactive checks.

---

**Paste the diagnostic bundle (markdown) below:**

```
<!-- paste ops/bundles/<timestamp>-bundle.md here -->
```
