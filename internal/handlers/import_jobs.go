package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/sessionimport/template"
)

// SCRUM-357: HTTP endpoints for asynchronous template-driven session imports.
//
// POST /api/import-jobs       — submit a template descriptor; runs schema
//                                validate + preflight synchronously, then
//                                spawns the worker as a goroutine and
//                                returns the job id. A non-empty
//                                preflight error response means zero DB
//                                writes have occurred.
// GET  /api/import-jobs/:id   — return the current job state (per-element
//                                results included). Polled by the UI
//                                (SCRUM-354) until terminal.

const importJobMaxBodyBytes = template.MaxDescriptorBytes

// ImportJobSubmitRequest is the POST body — a v1 template descriptor.
// We accept it as-is so the validator can produce structured errors.

// ImportJobSubmitResponse is returned on a successful submit. job_id is
// always set; session_id is set when the worker has created the
// destination row (which it does up-front, so it's typically populated
// here too).
type ImportJobSubmitResponse struct {
	JobID     string                       `json:"job_id"`
	SessionID *string                      `json:"session_id,omitempty"`
	Errors    []template.ValidationError   `json:"errors,omitempty"`
}

// CreateImportJob handles POST /api/import-jobs.
//
// On schema validation failure or preflight failure the response is HTTP
// 400 with a structured error array; no DB writes have occurred (the
// session row is created only by the worker after preflight passes).
func (h *Handlers) CreateImportJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if !user.GlobalRole.CanCreateSessions() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "your role does not allow creating sessions"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(importJobMaxBodyBytes)+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	if len(body) > importJobMaxBodyBytes {
		writeJSON(w, http.StatusBadRequest, ImportJobSubmitResponse{
			Errors: []template.ValidationError{{ReasonCode: template.ReasonDescriptorTooLarge, Message: fmt.Sprintf("body exceeds %d bytes", importJobMaxBodyBytes)}},
		})
		return
	}

	tmpl, schemaErrs := template.ParseAndValidate(body)
	if len(schemaErrs) > 0 {
		writeJSON(w, http.StatusBadRequest, ImportJobSubmitResponse{Errors: schemaErrs})
		return
	}

	// Preflight runs network I/O. SSRF protection is on by default.
	pre := template.Run(r.Context(), tmpl, template.Config{})
	if len(pre.Errors) > 0 {
		writeJSON(w, http.StatusBadRequest, ImportJobSubmitResponse{Errors: pre.Errors})
		return
	}

	// Persist the job. The worker will set session_id once the dst session
	// row exists.
	descriptor := json.RawMessage(body)
	job := &models.ImportJob{
		ID:                 uuid.New(),
		UserID:             &user.ID,
		State:              models.ImportJobStateQueued,
		TemplateDescriptor: descriptor,
		ElementsState:      map[string]models.ImportElementResult{},
	}
	if err := h.DB.CreateImportJob(r.Context(), job); err != nil {
		log.Printf("CreateImportJob: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to enqueue import job"})
		return
	}

	// Spawn the worker. Use a fresh context (decoupled from the request
	// lifetime) so a slow client disconnect doesn't kill the import.
	go func(jobID uuid.UUID, t *template.Template, p template.PreflightResult, creator string) {
		ctx := context.Background()
		deps := template.WorkerDeps{
			DB:       h.DB,
			Storage:  h.Storage,
			R2Prefix: strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/"),
			Trigger:  h.triggerIndex,
		}
		if err := template.RunImportJob(ctx, deps, jobID, t, p, creator); err != nil {
			log.Printf("RunImportJob %s: %v", jobID, err)
		}
	}(job.ID, tmpl, pre, user.Email)

	resp := ImportJobSubmitResponse{JobID: job.ID.String()}
	if job.SessionID != nil {
		s := job.SessionID.String()
		resp.SessionID = &s
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// GetImportJob handles GET /api/import-jobs/:id.
func (h *Handlers) GetImportJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "import-jobs" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	jobID, err := uuid.Parse(parts[2])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job id"})
		return
	}
	job, err := h.DB.GetImportJob(r.Context(), jobID)
	if err != nil {
		log.Printf("GetImportJob: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read job"})
		return
	}
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	// ACL: only the job's owner or an admin may read it.
	if user.GlobalRole != models.GlobalRoleAdmin {
		if job.UserID == nil || *job.UserID != user.ID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
	}
	writeJSON(w, http.StatusOK, job)
}

// ImportJobsRouter dispatches /api/import-jobs and /api/import-jobs/:id.
func (h *Handlers) ImportJobsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 2 && parts[0] == "api" && parts[1] == "import-jobs":
		// /api/import-jobs
		h.RequireAuth(h.CreateImportJob)(w, r)
	case len(parts) == 3 && parts[0] == "api" && parts[1] == "import-jobs":
		// /api/import-jobs/:id
		h.RequireAuth(h.GetImportJob)(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// Sentinel — keeps the errors import alive for future use.
var _ = errors.New
