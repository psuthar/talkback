package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

// ResetResponse represents the response from the reset endpoint
type ResetResponse struct {
	OK      bool              `json:"ok"`
	Deleted map[string]int    `json:"deleted,omitempty"`
	Message string            `json:"message,omitempty"`
}

// ResetErrorResponse represents an error response
type ResetErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (h *Handlers) ResetAllData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Dev-only guardrail
	allowReset := os.Getenv("ALLOW_DEV_RESET")
	if allowReset != "true" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ResetErrorResponse{
			Error:   "forbidden",
			Message: "Reset endpoint is disabled. Set ALLOW_DEV_RESET=true to enable (dev only).",
		})
		return
	}

	// Get deletion flag
	deleteFiles := os.Getenv("DEV_RESET_DELETE_FILES") == "true"

	// Delete data in correct order (respecting foreign keys)
	deleted := make(map[string]int)
	ctx := r.Context()

	// Delete answers first (references questions)
	count, err := h.DB.DeleteAllAnswers(ctx)
	if err != nil {
		log.Printf("Error deleting answers: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ResetErrorResponse{
			Error:   "internal_error",
			Message: fmt.Sprintf("Failed to delete answers: %v", err),
		})
		return
	}
	deleted["answers"] = count

	// Delete questions (references artifacts)
	count, err = h.DB.DeleteAllQuestions(ctx)
	if err != nil {
		log.Printf("Error deleting questions: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ResetErrorResponse{
			Error:   "internal_error",
			Message: fmt.Sprintf("Failed to delete questions: %v", err),
		})
		return
	}
	deleted["questions"] = count

	// Delete video sources (references artifacts)
	count, err = h.DB.DeleteAllVideoSources(ctx)
	if err != nil {
		log.Printf("Error deleting video sources: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ResetErrorResponse{
			Error:   "internal_error",
			Message: fmt.Sprintf("Failed to delete video sources: %v", err),
		})
		return
	}
	deleted["video_sources"] = count

	// Delete materials (references artifacts)
	count, err = h.DB.DeleteAllMaterials(ctx)
	if err != nil {
		log.Printf("Error deleting materials: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ResetErrorResponse{
			Error:   "internal_error",
			Message: fmt.Sprintf("Failed to delete materials: %v", err),
		})
		return
	}
	deleted["materials"] = count

	// Delete artifacts (no dependencies)
	count, err = h.DB.DeleteAllArtifacts(ctx)
	if err != nil {
		log.Printf("Error deleting artifacts: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ResetErrorResponse{
			Error:   "internal_error",
			Message: fmt.Sprintf("Failed to delete artifacts: %v", err),
		})
		return
	}
	deleted["artifacts"] = count

	// Optionally delete uploaded files
	if deleteFiles {
		uploadsDir := "./data/uploads"
		if err := deleteUploadsDirectory(uploadsDir); err != nil {
			log.Printf("Warning: Failed to delete uploads directory: %v", err)
			// Don't fail the request, just log the warning
		} else {
			deleted["uploaded_files"] = 1 // Indicate files were deleted
		}
	}

	response := ResetResponse{
		OK:      true,
		Deleted: deleted,
		Message: "All data reset successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// deleteUploadsDirectory removes all contents of the uploads directory
func deleteUploadsDirectory(dir string) error {
	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil // Directory doesn't exist, nothing to delete
	}

	// Remove all contents
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read uploads directory: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("failed to delete %s: %w", entryPath, err)
		}
	}

	return nil
}
