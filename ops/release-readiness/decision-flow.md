# Release Readiness — Decision Flow

```mermaid
flowchart TD
    A([CI triggers:<br>pull_request / push / manual]) --> B

    subgraph INPUTS["Inputs"]
        B[Changed files<br>git diff base...HEAD]
        C[smoke_results.json<br>Go test outcome]
        D[e2e_results.json<br>Playwright → converter]
        E[coverage.json<br>go tool cover]
    end

    B --> RISK
    C --> MERGE
    D --> MERGE
    E --> COV

    subgraph SCORE["Score  (starts at 100)"]
        RISK[Risk detection<br>changed paths → categories<br>auth_session / upload_extraction<br>nav_assets / viewer_materials<br>qa_rag / migrations]
        MERGE[Merge validations<br>explicit evidence + inference<br>smoke pass → auth/upload/qa<br>e2e pass → auth/nav/viewer/qa]
        SOFT[Apply soft penalties<br>−25 missing smoke<br>−15 missing e2e<br>−10 risky config, no note<br>−12 coverage regression<br>−15 non-critical E2E fail<br>−10 E2E retries]
        COV[Coverage check<br>regression vs baseline?]
        COV -->|regression| SOFT
        RISK --> MERGE
        MERGE --> SOFT
    end

    SOFT --> CLAMP[Clamp score 0–100]

    subgraph BLOCKERS["Hard blocker checks  (order matters)"]
        BLK1{Smoke failed<br>or parse error?}
        BLK2{Critical E2E<br>failures?}
        BLK3{Risk triggered<br>without required<br>validation evidence?}
    end

    CLAMP --> BLK1
    BLK1 -->|yes| FLOOR
    BLK1 -->|no| BLK2
    BLK2 -->|yes| FLOOR
    BLK2 -->|no| BLK3
    BLK3 -->|yes| FLOOR
    BLK3 -->|no| DECIDE

    FLOOR[Floor score to 0<br>block_score from config] --> DECIDE

    subgraph DECIDE["Outcome decision"]
        D1{Any hard<br>blockers?}
        D2{Score < 60<br>warn_threshold?}
        D3{Score >= 85<br>pass_threshold<br>AND 0 warnings?}
    end

    DECIDE --> D1
    D1 -->|yes| BLOCK
    D1 -->|no| D2
    D2 -->|yes| BLOCK
    D2 -->|no| D3
    D3 -->|yes| PASS
    D3 -->|no| WARN

    BLOCK([BLOCK<br>score = 0<br>fix before deploy])
    WARN([WARN<br>60 ≤ score < 85<br>or score ≥ 85 with warnings<br>review before deploy])
    PASS([PASS<br>score ≥ 85<br>0 warnings<br>0 blockers])

    BLOCK --> OUT
    WARN --> OUT
    PASS --> OUT

    subgraph OUT["Output"]
        RPT[report.json<br>outcome / score / blockers<br>warnings / validations<br>outcome_overrides]
        MD[report.md<br>Outcome determination table<br>Why line<br>Warnings / Blockers / Validations]
        CI[CI exit code<br>0 = PASS or WARN<br>1 = BLOCK]
    end
```

## Key rules

| Rule | Type | Effect on score | Effect on outcome |
|------|------|-----------------|-------------------|
| Smoke failed | Hard blocker | floor to 0 | BLOCK |
| Critical E2E failure | Hard blocker | floor to 0 | BLOCK |
| Risk path changed, validation missing | Hard blocker | floor to 0 | BLOCK |
| Score < warn_threshold (60) | Score gate | — | BLOCK |
| Score >= pass_threshold (85) AND warnings present | Soft override | — | WARN (not PASS) |
| Non-critical E2E failure | Soft penalty | −15 | contributes to WARN |
| E2E retries | Soft penalty | −10 | contributes to WARN |
| Coverage regression | Soft penalty | −12 | contributes to WARN |
| Risky config change, no validation note | Soft penalty | −10 | contributes to WARN |

## PASS requires both conditions

```
score >= 85  AND  warnings == 0
```

Score alone is not sufficient. A score of 85/100 with any warning produces **WARN**, not PASS.
The `outcome_overrides` field in the JSON names this explicitly when it occurs.
