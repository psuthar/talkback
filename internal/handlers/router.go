package handlers

import (
	"net/http"
	"strings"
)

func (h *Handlers) ArtifactsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 || parts[0] != "artifacts" {
		http.Error(w, "Invalid path", http.StatusNotFound)
		return
	}

	if len(parts) == 2 {
		// /artifacts/{id} - GET
		if r.Method == http.MethodGet {
			h.GetArtifact(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(parts) == 3 {
		action := parts[2]

		switch action {
		case "materials":
			if r.Method == http.MethodPost {
				h.UploadMaterial(w, r)
				return
			}
		case "video":
			if r.Method == http.MethodPost {
				h.AttachVideoURL(w, r)
				return
			}
		case "questions":
			if r.Method == http.MethodPost {
				h.AskQuestion(w, r)
				return
			}
			if r.Method == http.MethodGet {
				h.GetQuestions(w, r)
				return
			}
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(parts) == 5 {
		// /artifacts/{id}/video/{video_id}/transcript
		if parts[2] == "video" && parts[4] == "transcript" {
			if r.Method == http.MethodPost {
				h.UploadTranscript(w, r)
				return
			}
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "Invalid path", http.StatusNotFound)
}
