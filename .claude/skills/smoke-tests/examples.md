# Smoke Test Examples

Concrete, copy-paste-ready patterns for each TalkBack smoke flow. All examples use
the real Postgres test infrastructure — no mocks.

---

## Flow 1 — Admin/Session Setup

```go
// internal/handlers/smoke_session_setup_test.go
package handlers

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "talkback/internal/models"
)

func TestSmoke_SessionSetup_CreatorCanCreateAndRetrieve(t *testing.T) {
    t.Parallel()
    h, cleanup := setupTestHandlersParallel(t)
    defer cleanup()

    ctx := context.Background()

    // Seed a creator user and attach their session cookie
    creatorUser := addUserSessionCookie // called inline on each request below

    // --- Step 1: Create session ---
    body, _ := json.Marshal(map[string]any{
        "title": "Q4 Planning",
    })
    req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    creator := addUserSessionCookie(t, h, req, "creator@smoke.test")
    w := httptest.NewRecorder()
    h.RequireAuth(h.HandleCreateSession)(w, req)

    require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
    var session models.Session
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &session))

    assert.NotEmpty(t, session.ID)
    assert.Equal(t, "Q4 Planning", session.Title)
    assert.Equal(t, "open", string(session.Status))
    assert.Equal(t, "none", session.IndexStatus)
    assert.Equal(t, creator.ID.String(), *session.CreatedBy)

    // --- Step 2: Retrieve session ---
    req2 := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
    addUserSessionCookie(t, h, req2, "creator@smoke.test")
    w2 := httptest.NewRecorder()
    h.RequireAuth(h.HandleGetSession)(w2, req2.WithContext(
        withPathParam(req2.Context(), "id", session.ID.String()),
    ))

    require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
    var fetched models.Session
    require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &fetched))
    assert.Equal(t, session.ID, fetched.ID)

    _ = ctx
    _ = creatorUser // used inline
}
```

---

## Flow 2 — Material Ingestion (enqueue assertion)

The ingestion pipeline is async. The smoke test validates that:
1. The upload endpoint returns 201 with a material record.
2. A processing job row is enqueued in the DB.

It does **not** wait for text extraction to complete.

```go
// internal/handlers/smoke_material_ingestion_test.go
package handlers

import (
    "bytes"
    "context"
    "mime/multipart"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "talkback/internal/models"
)

const smokeFixturePDF = "Quarterly review highlights: revenue up 12%, churn reduced by 8%."

func TestSmoke_MaterialIngestion_UploadEnqueuesJob(t *testing.T) {
    t.Parallel()
    h, cleanup := setupTestHandlersParallel(t)
    defer cleanup()

    // Create a session
    session := createTestSessionForHandlers(t, h.DB, "Ingestion Smoke")

    // Build multipart upload
    var buf bytes.Buffer
    w := multipart.NewWriter(&buf)
    fw, err := w.CreateFormFile("file", "report.txt")
    require.NoError(t, err)
    _, _ = fw.Write([]byte(smokeFixturePDF))
    w.WriteField("kind", "document")
    w.Close()

    req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", &buf)
    req.Header.Set("Content-Type", w.FormDataContentType())
    addUserSessionCookie(t, h, req, "creator@smoke.test")
    rec := httptest.NewRecorder()
    h.RequireAuth(h.HandleUploadMaterial)(rec, req.WithContext(
        withPathParam(req.Context(), "id", session.ID.String()),
    ))

    require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

    var material models.Material
    require.NoError(t, json.NewDecoder(rec.Body).Decode(&material))
    assert.Equal(t, session.ID, material.SessionID)
    assert.Equal(t, "document", material.Kind)
    // text_status starts as "pending" — extraction is async
    assert.Equal(t, models.MaterialTextStatusPending, material.TextStatus)

    // Confirm job was enqueued (DB assertion, no polling)
    jobs, err := h.DB.ListMaterialJobsForSession(context.Background(), session.ID)
    require.NoError(t, err)
    assert.NotEmpty(t, jobs, "expected at least one processing job to be enqueued")
}
```

---

## Flow 3 — Invite Flow

```go
// internal/handlers/smoke_invite_flow_test.go
package handlers

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "talkback/internal/models"
)

func TestSmoke_InviteFlow_AcceptCreatesParticipant(t *testing.T) {
    t.Parallel()
    h, cleanup := setupTestHandlersWithInvitations(t)
    defer cleanup()

    session := createTestSessionForHandlers(t, h.DB, "Invite Smoke")

    // --- Step 1: Creator sends invite ---
    invBody, _ := json.Marshal(map[string]any{"email": "participant@smoke.test"})
    invReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/invitations", bytes.NewReader(invBody))
    invReq.Header.Set("Content-Type", "application/json")
    addUserSessionCookie(t, h, invReq, "creator@smoke.test")
    invW := httptest.NewRecorder()
    h.RequireAuth(h.HandleCreateInvitation)(invW, invReq.WithContext(
        withPathParam(invReq.Context(), "id", session.ID.String()),
    ))
    require.Equal(t, http.StatusCreated, invW.Code, invW.Body.String())

    var inv struct{ Token string `json:"token"` }
    require.NoError(t, json.Unmarshal(invW.Body.Bytes(), &inv))
    require.NotEmpty(t, inv.Token)

    // --- Step 2: Resolve token ---
    resolveBody, _ := json.Marshal(map[string]any{"token": inv.Token})
    resolveReq := httptest.NewRequest(http.MethodPost, "/api/invitations/resolve", bytes.NewReader(resolveBody))
    resolveReq.Header.Set("Content-Type", "application/json")
    resolveW := httptest.NewRecorder()
    h.HandleResolveInvitation(resolveW, resolveReq)

    require.Equal(t, http.StatusOK, resolveW.Code, resolveW.Body.String())
    var resolved struct {
        SessionID string `json:"session_id"`
        Email     string `json:"email"`
    }
    require.NoError(t, json.Unmarshal(resolveW.Body.Bytes(), &resolved))
    assert.Equal(t, session.ID.String(), resolved.SessionID)
    assert.Equal(t, "participant@smoke.test", resolved.Email)

    // --- Step 3: Accept invitation (creates account) ---
    acceptBody, _ := json.Marshal(map[string]any{
        "token":        inv.Token,
        "display_name": "Smoke Participant",
        "password":     "SmokePass123!",
    })
    acceptReq := httptest.NewRequest(http.MethodPost, "/api/invitations/accept", bytes.NewReader(acceptBody))
    acceptReq.Header.Set("Content-Type", "application/json")
    acceptW := httptest.NewRecorder()
    h.HandleAcceptInvitation(acceptW, acceptReq)

    require.Equal(t, http.StatusOK, acceptW.Code, acceptW.Body.String())

    var acceptResp struct{ User models.User `json:"user"` }
    require.NoError(t, json.Unmarshal(acceptW.Body.Bytes(), &acceptResp))
    assert.Equal(t, models.GlobalRoleParticipant, acceptResp.User.GlobalRole)
    assert.Equal(t, "participant@smoke.test", acceptResp.User.Email)
}
```

---

## Flow 4 — Participant Access Control

```go
// internal/handlers/smoke_participant_access_test.go
package handlers

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSmoke_ParticipantAccess_CanReadSession(t *testing.T) {
    t.Parallel()
    h, cleanup := setupTestHandlersParallel(t)
    defer cleanup()

    session := createTestSessionForHandlers(t, h.DB, "Access Smoke")

    req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
    addParticipantSessionCookie(t, h, req)
    w := httptest.NewRecorder()
    h.RequireAuth(h.HandleGetSession)(w, req.WithContext(
        withPathParam(req.Context(), "id", session.ID.String()),
    ))

    require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestSmoke_ParticipantAccess_CannotDeleteSession(t *testing.T) {
    t.Parallel()
    h, cleanup := setupTestHandlersParallel(t)
    defer cleanup()

    session := createTestSessionForHandlers(t, h.DB, "Access Smoke Delete")

    req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
    addParticipantSessionCookie(t, h, req)
    w := httptest.NewRecorder()
    // DeleteSession requires admin role
    h.RequireAuth(h.RequireAdmin(h.HandleDeleteSession))(w, req.WithContext(
        withPathParam(req.Context(), "id", session.ID.String()),
    ))

    assert.Equal(t, http.StatusForbidden, w.Code)
}
```

---

## Flow 5 — Ask/Answer Validation (stub RAG)

For CI, skip real LLM calls. Inject a stub answer directly and verify persistence + retrieval.

```go
// internal/handlers/smoke_ask_answer_test.go
package handlers

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "talkback/internal/models"
)

func TestSmoke_AskAnswer_QuestionPersistedAndAnswerStructureValid(t *testing.T) {
    t.Parallel()
    h, cleanup := setupTestHandlersParallel(t)
    defer cleanup()

    session := createTestSessionForHandlers(t, h.DB, "Q&A Smoke")

    // --- Step 1: Participant asks a question ---
    qBody, _ := json.Marshal(map[string]any{
        "question_text": "What were the revenue highlights?",
    })
    qReq := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions", bytes.NewReader(qBody))
    qReq.Header.Set("Content-Type", "application/json")
    addParticipantSessionCookie(t, h, qReq)
    qW := httptest.NewRecorder()
    h.RequireAuth(h.HandleCreateQuestion)(qW, qReq.WithContext(
        withPathParam(qReq.Context(), "id", session.ID.String()),
    ))

    require.Equal(t, http.StatusCreated, qW.Code, qW.Body.String())
    var question models.Question
    require.NoError(t, json.Unmarshal(qW.Body.Bytes(), &question))
    assert.Equal(t, "What were the revenue highlights?", question.QuestionText)
    assert.Equal(t, session.ID, question.SessionID)

    // --- Step 2: Inject a stub answer (bypasses real LLM) ---
    stubAnswer := &models.Answer{
        QuestionID:   question.ID,
        AnswerText:   "Revenue increased 12% in Q4 per the quarterly review.",
        AnswerStatus: models.AnswerStatusAnswered,
        Confidence:   0.88,
        Citations: []models.Citation{
            {SourceType: "material", ChunkID: "chunk-abc-123"},
        },
    }
    err := h.DB.UpsertAnswer(context.Background(), stubAnswer)
    require.NoError(t, err)

    // --- Step 3: Retrieve questions and validate answer structure ---
    listReq := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/questions", nil)
    addParticipantSessionCookie(t, h, listReq)
    listW := httptest.NewRecorder()
    h.RequireAuth(h.HandleListQuestions)(listW, listReq.WithContext(
        withPathParam(listReq.Context(), "id", session.ID.String()),
    ))

    require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
    var questions []struct {
        ID     string       `json:"id"`
        Answer models.Answer `json:"answer"`
    }
    require.NoError(t, json.Unmarshal(listW.Body.Bytes(), &questions))
    require.NotEmpty(t, questions)

    ans := questions[0].Answer
    // Structural validity — never assert exact text
    assert.Contains(t, []string{"answered", "not_covered", "error"}, string(ans.AnswerStatus))
    assert.GreaterOrEqual(t, ans.Confidence, float32(0.0))
    assert.LessOrEqual(t, ans.Confidence, float32(1.0))
    assert.NotEmpty(t, ans.AnswerText)

    // Keyword check against known fixture content
    assert.True(t,
        strings.Contains(strings.ToLower(ans.AnswerText), "revenue") ||
            strings.Contains(strings.ToLower(ans.AnswerText), "quarterly"),
        "expected answer to reference seeded fixture keywords",
    )

    for _, c := range ans.Citations {
        assert.Contains(t, []string{"material", "transcript", "link"}, c.SourceType)
    }
}
```

---

## Bounded Poll Helper (for future async tests)

```go
// internal/handlers/smoke_helpers_test.go
package handlers

import (
    "context"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/stretchr/testify/require"

    "talkback/internal/database"
    "talkback/internal/models"
)

// waitForMaterialTextReady polls until a material reaches TextStatusReady.
// Only use in tests that control a synchronous worker goroutine.
// Prefer asserting job enqueue instead of polling in unit smoke tests.
func waitForMaterialTextReady(t *testing.T, db *database.DB, materialID uuid.UUID) *models.Material {
    t.Helper()
    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        m, err := db.GetMaterial(context.Background(), materialID)
        require.NoError(t, err)
        if m.TextStatus == models.MaterialTextStatusReady {
            return m
        }
        time.Sleep(200 * time.Millisecond)
    }
    t.Fatal("material did not reach ready state within 5s")
    return nil
}

// waitForSessionIndexReady polls until a session's index_status is "ready".
func waitForSessionIndexReady(t *testing.T, db *database.DB, sessionID uuid.UUID) {
    t.Helper()
    deadline := time.Now().Add(10 * time.Second)
    for time.Now().Before(deadline) {
        s, err := db.GetSession(context.Background(), sessionID)
        require.NoError(t, err)
        if s.IndexStatus == "ready" {
            return
        }
        time.Sleep(300 * time.Millisecond)
    }
    t.Fatal("session index did not reach ready state within 10s")
}
```
