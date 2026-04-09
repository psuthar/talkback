# TalkBack

**TalkBack** is an experimental platform for exploring how modern software delivery (SDLC) can evolve into an **intelligent, explainable, and partially autonomous system**.

At its core, TalkBack enables asynchronous, meeting-driven collaboration (videos, documents, transcripts, and Q&A).
But more importantly, it serves as a **living case study** for applying AI and deterministic agents to real-world engineering workflows.

---

## 🚀 What makes this project different

Most CI/CD pipelines answer:

> “Did the tests pass?”

TalkBack explores a different question:

> **“Is this change actually safe to deploy—and why?”**

To do this, the system introduces a **Release Readiness Agent** that:

- Analyzes code changes
- Maps changes to product risk areas
- Verifies whether the correct validations were executed
- Produces a deterministic outcome: **PASS / WARN / BLOCK**
- Explains *why* that outcome was reached
- Provides **recommended remediation actions**

This moves CI/CD from:
- static pipelines → **decision systems**
- opaque results → **explainable outcomes**
- manual interpretation → **guided action**

---

## 🧠 Key capabilities

### 1. Release Readiness Agent (CI-integrated)

- Runs in GitHub Actions on PRs and merges
- Evaluates:
  - change risk (based on files touched)
  - validation coverage (what tests actually ran)
  - execution signals (smoke, E2E, etc.)
- Produces:
  - PASS / WARN / BLOCK outcome
  - deterministic score
  - explicit override reasoning
  - remediation guidance

**PR Risk Report (v2.8)** — a complementary, **git-diff-only** risk pass (runs in the same CI workflow before the Python evaluator):

- Deterministic scoring from changed paths and churn (no LLM in the decision path)
- **Category breakdown:** code changes, workflow/deployment, and test confidence
- **Reducers:** signals that *lower* risk (e.g. validation notes in commits, unit vs E2E test evidence for sensitive areas)
- **Required actions before merge:** explicit checklist items, separate from optional mitigations
- **Automatic PR comment:** every PR receives a risk summary comment (🟢/🟡/🔴 result, score, top signals); the comment updates on new commits — no artifact inspection needed
- CLI: `go run ./cmd/prrisk --repo-root . --base-ref <ref> --output-dir artifacts/release-readiness`
- Artifacts: `pr_risk.json` / `pr_risk.md`

Full pipeline details: [`ops/release-readiness/README.md`](ops/release-readiness/README.md).

Example:

```
Score: 90 (PASS range)
Outcome: WARN

Why:
- Workflow/config changed
- No validation note provided

Recommended action:
- Add validation note describing what was tested and verified
```

---

### 2. Deterministic + Explainable Decisions

- No LLM in the decision path
- All scoring and outcomes are rule-based
- Every decision is traceable and explainable

> Trust comes from understanding *why*, not just seeing a result.

---

### 3. Product-aware validation model

Instead of generic “tests passed,” the system evaluates **real product flows**, such as:

- authentication/session behavior
- content upload and extraction
- navigation and rendering
- Q&A (RAG) behavior

Changes dynamically trigger **required validations**.

---

### 4. Remediation guidance

For every WARN or BLOCK:

- likely cause
- recommended fix
- type of fix (code, test, config, process)

This reduces guesswork and speeds up resolution.

---

### 5. Validation notes (human + system collaboration)

For risky changes (e.g. CI/workflow updates), developers can include:

```
Validation:
- ran workflow_dispatch
- verified artifacts uploaded
- confirmed expected behavior
```

This allows the system to suppress warnings when appropriate.

---

## 🧭 Why this exists

This project explores a broader idea:

> **What if the SDLC itself became intelligent?**

Instead of:
- CI pipelines
- dashboards
- manual interpretation

We move toward:
- systems that evaluate risk
- systems that explain decisions
- systems that suggest fixes
- eventually → systems that learn from production

---

## ⚠️ Experimental project

This repository is intentionally evolving and may include:
- incomplete features
- experimental patterns
- changing designs

The goal is not perfection—it is exploration.

---

## 🏗️ Architecture overview

- **Backend:** Go (API + processing)
- **Frontend:** React (Vite)
- **Database:** PostgreSQL
- **Storage:** Cloudflare R2 (planned/partial)
- **CI/CD:** GitHub Actions
- **AI:** OpenAI (RAG, transcription, Q&A)

---

## 📦 Core application (TalkBack)

TalkBack enables:

- Uploading videos, documents, and transcripts
- Creating sessions around content
- Asking questions (text or voice)
- Getting answers with citations (RAG)

The SDLC system is built **alongside** the product—not in isolation.

---

# ------------------------------------------------------------
# Local Development
# ------------------------------------------------------------

## Prerequisites

- Go 1.21 or later
- Docker and Docker Compose
- Windows 11 (or compatible environment)

---

## Setup

### 1. Start Postgres database

```bash
docker compose -f deploy/docker-compose.yml up -d
```

---

### 2. Configure environment variables

```bash
copy .env.example .env
```

Example:

```
DATABASE_URL=postgres://talkback:talkback@localhost:5432/talkback?sslmode=disable
OPENAI_API_KEY=your_openai_api_key_here
RUN_MIGRATIONS=true
ALLOW_DEV_RESET=false
DEV_RESET_DELETE_FILES=false
```

---

### 3. Run API server

```bash
go run ./cmd/api
```

---

### MCP server (agents — Cursor / Claude Code)

To run the **`talkback-mcp`** stdio server from this repo (Model Context Protocol for AI agents), see **[`docs/mcp-server.md`](docs/mcp-server.md)** — build steps, env vars, `./scripts/setup-mcp-config.sh`, and example JSON configs (placeholders only).

---

## Web UI

```bash
cd web
npm install
npm run dev
```

---

## Testing

```bash
go test ./...
```

---

## Deploying on Render

(Refer to existing Render deployment instructions in the repository.)

---

## License

This project is licensed under the **Apache License 2.0**.

- Permissive open-source license
- Includes explicit patent grant protections
- Suitable for experimentation, reuse, and commercial exploration
