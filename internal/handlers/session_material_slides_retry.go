package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// RetryMaterialSlides handles POST /api/sessions/:id/materials/:mid/retry-slides
// (creator-only). Re-runs the SCRUM-287 slides derivation pipeline for a
// .ppt/.pptx material whose previous attempt left a failed marker — without
// requiring the user to re-upload the file.
//
// Behaviour:
//   - 403 if the caller isn't the session creator (or admin) — same ACL guard
//     as PATCH primary (SCRUM-272/SCRUM-281).
//   - 400 if the material isn't a .ppt/.pptx; only those kinds derive slides.
//   - 202 once the derivation goroutine has been kicked off. The websocket
//     session_updated event from the goroutine signals the eventual outcome,
//     mirroring the original upload path.
//   - 500 only on unexpected DB / storage errors.
//
// The download-to-temp dance for R2 storage mirrors what the upload path
// already does inline; we keep the retry surface symmetric so a retry can
// repair a transient pdftoppm/soffice failure (e.g. binary missing during
// upload, restored before retry).
func (h *Handlers) RetryMaterialSlides(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	// Expected shape: api/sessions/{sid}/materials/{mid}/retry-slides
	if len(parts) < 6 || parts[1] != "sessions" || parts[3] != "materials" || parts[5] != "retry-slides" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(parts[2])
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	materialID, err := uuid.Parse(parts[4])
	if err != nil {
		http.Error(w, "invalid material id", http.StatusBadRequest)
		return
	}

	canEdit, err := h.userIsSessionEditor(r.Context(), sessionID, user)
	if err != nil {
		log.Printf("RetryMaterialSlides userIsSessionEditor: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "acl check failed"})
		return
	}
	if !canEdit {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the session creator can retry slide derivation"})
		return
	}

	mat, err := h.DB.GetMaterialByID(r.Context(), materialID)
	if err != nil || mat == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "material not found"})
		return
	}
	if mat.SessionID != sessionID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "material does not belong to this session"})
		return
	}
	if mat.DeletedAt != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "material has been deleted"})
		return
	}
	if !models.MaterialSupportsDerivedSlideDeck(mat) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "material does not support slide derivation (not a .ppt/.pptx)"})
		return
	}

	// Kick off the same goroutine the upload path uses, but on a re-fetched
	// copy of the source file so the local-mode pdftoppm pipeline can run
	// against the bytes we already stored.
	go h.runSlidesRetry(mat)

	writeJSON(w, http.StatusAccepted, map[string]string{"state": "queued"})
}

// runSlidesRetry materialises the source bytes from storage onto a temp file
// (R2) or reads them from the existing local path, then dispatches to the
// same tryGenerateAndStoreSlides* helper used by the upload path. Failures
// are logged at WARN+ via the existing slides_converter pipeline so an
// operator can correlate this with the SCRUM-287 binary-missing log lines.
func (h *Handlers) runSlidesRetry(mat *models.Material) {
	ctx := context.Background()

	if mat.StorageProvider == "r2" && h.Storage != nil && strings.TrimSpace(mat.StorageKey) != "" {
		// Download to a temp file, then run the same pipeline as the upload
		// path. Ensure the temp file is removed regardless of outcome.
		rc, err := h.Storage.Get(ctx, mat.StorageKey)
		if err != nil {
			log.Printf("[WARN] RetryMaterialSlides: storage.Get %s: %v", mat.StorageKey, err)
			return
		}
		defer rc.Close()

		tmp, err := os.CreateTemp("", "retry-slides-*"+filepath.Ext(mat.Filename))
		if err != nil {
			log.Printf("[WARN] RetryMaterialSlides: tempfile: %v", err)
			return
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if _, err := io.Copy(tmp, rc); err != nil {
			log.Printf("[WARN] RetryMaterialSlides: copy r2→tempfile: %v", err)
			tmp.Close()
			return
		}
		tmp.Close()

		h.tryGenerateAndStoreSlides(ctx, tmp.Name(), mat.StorageKey)
		if h.Hub != nil {
			h.Hub.BroadcastSessionUpdated(mat.SessionID)
		}
		return
	}

	// Local mode: derive directly from the existing on-disk file.
	if mat.StorageProvider == "local" && strings.TrimSpace(mat.StorageURL) != "" {
		// StorageURL is the on-disk path for local-mode uploads; re-running the
		// pipeline overwrites the previous _slides directory contents.
		h.tryGenerateAndStoreSlidesLocal(ctx, mat.StorageURL)
		if h.Hub != nil {
			h.Hub.BroadcastSessionUpdated(mat.SessionID)
		}
		return
	}

	log.Printf("[WARN] RetryMaterialSlides: no storage path for material %s (provider=%q key=%q url=%q)",
		mat.ID, mat.StorageProvider, mat.StorageKey, mat.StorageURL)
}
