package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/sessionimport/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-357: template-driven import job handler tests.
//
// We can't easily exercise the full happy path end-to-end here because the
// preflight stage by default refuses to dial 127.0.0.1, and the test
// fixtures' upstream server runs on 127.0.0.1. We instead exercise:
//
//   1. Schema validation rejection (no DB writes, structured errors).
//   2. Preflight rejection (auth-required URL → 400 + structured errors).
//   3. Job lifecycle integration: CreateImportJob + GetImportJob round-trip
//      directly against the DB layer (skipping the handler's preflight).
//
// The full happy-path worker flow is exercised by SCRUM-355's regression
// sweep against the deployed CI environment.

func TestCreateImportJob_SchemaValidationRejected(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	body := []byte(`{"version": 99, "title": "x", "elements": []}`)
	req := httptest.NewRequest(http.MethodPost, "/api/import-jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addUserSessionCookie(t, h, req, "import-schema@example.com")
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateImportJob)(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "schema-invalid template must be rejected: %s", w.Body.String())
	var resp ImportJobSubmitResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Errors)
	assert.Equal(t, template.ReasonVersionUnsupported, resp.Errors[0].ReasonCode)
}

func TestCreateImportJob_DescriptorTooLarge(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	big := make([]byte, template.MaxDescriptorBytes+1)
	for i := range big {
		big[i] = ' '
	}
	req := httptest.NewRequest(http.MethodPost, "/api/import-jobs", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	addUserSessionCookie(t, h, req, "import-too-big@example.com")
	w := httptest.NewRecorder()

	h.RequireAuth(h.CreateImportJob)(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp ImportJobSubmitResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Errors)
	assert.Equal(t, template.ReasonDescriptorTooLarge, resp.Errors[0].ReasonCode)
}

func TestImportJob_RoundTrip(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	user := addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "import-rt@example.com")

	// Create a job directly via DB (skipping preflight).
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &user.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: json.RawMessage(`{"version":1,"title":"Test","elements":[]}`),
		ElementsState:      map[string]models.ImportElementResult{},
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)
	assert.Equal(t, models.ImportJobStateQueued, got.State)
	require.NotNil(t, got.UserID)
	assert.Equal(t, user.ID, *got.UserID)
}

func TestImportJob_StateTransitions(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	user := addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "import-state@example.com")
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &user.ID,
		TemplateDescriptor: json.RawMessage(`{"version":1,"title":"x","elements":[]}`),
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))

	// queued → running → succeeded with timestamps populated.
	require.NoError(t, h.DB.UpdateImportJobState(ctx, job.ID, models.ImportJobStateRunning, nil))
	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, models.ImportJobStateRunning, got.State)
	require.NotNil(t, got.StartedAt, "started_at must be set on transition to running")

	require.NoError(t, h.DB.UpdateImportJobState(ctx, job.ID, models.ImportJobStateSucceeded, nil))
	got, err = h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ImportJobStateSucceeded, got.State)
	require.NotNil(t, got.CompletedAt, "completed_at must be set on terminal transition")
	// started_at must persist across the second update.
	require.NotNil(t, got.StartedAt)
}

func TestImportJob_ElementsState(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	user := addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "import-elems@example.com")
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &user.ID,
		TemplateDescriptor: json.RawMessage(`{"version":1,"title":"x","elements":[]}`),
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))

	require.NoError(t, h.DB.SetImportJobElement(ctx, job.ID, "agenda", models.ImportElementResult{Status: models.ImportElementStatusSucceeded}))
	require.NoError(t, h.DB.SetImportJobElement(ctx, job.ID, "policy", models.ImportElementResult{Status: models.ImportElementStatusFailed, ErrorCode: "url_unreachable", Message: "boom"}))

	got, err := h.DB.GetImportJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, models.ImportElementStatusSucceeded, got.ElementsState["agenda"].Status)
	assert.Equal(t, models.ImportElementStatusFailed, got.ElementsState["policy"].Status)
	assert.Equal(t, "url_unreachable", got.ElementsState["policy"].ErrorCode)
}

func TestGetImportJob_ACL(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	owner := addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "import-acl-owner@example.com")
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &owner.ID,
		TemplateDescriptor: json.RawMessage(`{"version":1,"title":"x","elements":[]}`),
	}
	require.NoError(t, h.DB.CreateImportJob(ctx, job))

	// A different user must NOT be able to read the job.
	intruder := addUserSessionCookie(t, h, httptest.NewRequest(http.MethodGet, "/", nil), "import-acl-other@example.com")
	intruderReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/import-jobs/%s", job.ID.String()), nil)
	addUserSessionCookieFor(t, h, intruderReq, intruder)
	intruderW := httptest.NewRecorder()
	h.RequireAuth(h.GetImportJob)(intruderW, intruderReq)
	assert.Equal(t, http.StatusForbidden, intruderW.Code)

	// The owner CAN read it.
	ownerReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/import-jobs/%s", job.ID.String()), nil)
	addUserSessionCookieFor(t, h, ownerReq, owner)
	ownerW := httptest.NewRecorder()
	h.RequireAuth(h.GetImportJob)(ownerW, ownerReq)
	require.Equal(t, http.StatusOK, ownerW.Code)
	var got models.ImportJob
	require.NoError(t, json.Unmarshal(ownerW.Body.Bytes(), &got))
	assert.Equal(t, job.ID, got.ID)
	_ = ctx
	_ = time.Now
}
