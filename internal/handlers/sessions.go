package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/citation"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// Phase 3: Session Handlers

type CreateSessionRequest struct {
	Title     string  `json:"title"`
	CreatedBy *string `json:"created_by,omitempty"`
}

type CreateSessionResponse struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	CreatedBy *string `json:"created_by,omitempty"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

type GetSessionsResponse struct {
	Sessions []*models.Session `json:"sessions"`
}

type GetSessionResponse struct {
	Session              *models.Session       `json:"session"`
	Artifacts            []*models.Artifact    `json:"artifacts"`
	Materials            []*models.Material    `json:"materials"`
	VideoSources         []*models.VideoSource `json:"video_sources"`
	PrimaryVideo         *models.VideoSource   `json:"primary_video,omitempty"`     // effective primary (explicit primary, else first ready, else first)
	AdditionalVideos     []*models.VideoSource `json:"additional_videos,omitempty"` // all other session videos
	RecentQuestions      []*models.Question    `json:"recent_questions"`
	RecentAnswers        []*models.Answer      `json:"recent_answers"`
	Mode                 string                `json:"mode"`                              // "creator" or "participant"
	CreatedByDisplayName *string               `json:"created_by_display_name,omitempty"` // session creator display name for UI
	UnreadMaterialIDs    []string              `json:"unread_material_ids,omitempty"`     // only when participant_ref provided
	VideoAccessURL       string                `json:"video_access_url,omitempty"`        // presigned R2 URL only when primary artifact is ready, r2, video
	PlaybackReasonCode   string                `json:"playback_reason_code,omitempty"`    // VIDEO_NOT_INGESTED, VIDEO_INGEST_PENDING, VIDEO_INGEST_FAILED
	PlaybackMessage      string                `json:"playback_message,omitempty"`        // safe message when video not playable
	Links                []*models.SessionLink `json:"links,omitempty"`                   // session links for citation URL resolution
	MaterialSlidesReady  map[string]bool       `json:"material_slides_ready,omitempty"`   // material ID -> true when slides manifest exists (PPT/PPTX only)
	MaterialSlidesStatus map[string]string     `json:"material_slides_status,omitempty"`  // material ID -> processing|ready|failed (PPT/PPTX only)
	Primary              *SessionPrimaryDescriptor `json:"primary,omitempty"`             // SCRUM-271: resolved center-pane primary (kind + id; legacy primary_video_artifact_id falls back to kind="video")
}

// SessionWithRole is one session plus the current user's role for it (for GET /api/sessions).
type SessionWithRole = database.SessionListRow

// ListSessions returns all sessions the current user may access, with my_role per session (RequireAuth).
// Admins see all sessions with my_role "admin"; others see sessions they created (my_role "creator") or are invited to (my_role "participant").
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	out, err := h.DB.ListSessionsWithRolesForUser(ctx, user)
	if err != nil {
		log.Printf("ListSessions: %v", err)
		http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
		return
	}

	// Optional limit/offset pagination. Both default to "no limit / no offset" when absent or zero.
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			offset := 0
			if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
				if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
					offset = o
				}
			}
			out = database.ApplySessionListPagination(out, limit, offset)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(out)
}

// CreateSession creates a new session (RequireAuth; requires admin or creator role).
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	// Parse request body
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), user.Email, title, nil)
	if err != nil {
		log.Printf("CreateSession SessionWithTitleExistsForCreator: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
		return
	}
	if exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "A session with this name already exists. Please use a unique name."})
		return
	}

	createdBy := user.Email
	// Create session (creator is the authenticated user)
	session := &models.Session{
		ID:        uuid.New(),
		Title:     title,
		CreatedBy: &createdBy,
		Status:    models.SessionStatusOpen,
	}

	if err := h.DB.CreateSession(r.Context(), session); err != nil {
		log.Printf("Error creating session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a first artifact so the session is immediately usable (edit view, Q&A, and "session has no artifacts" is avoided)
	artifactTitle := session.Title
	if artifactTitle == "" {
		artifactTitle = "Session content"
	}
	if _, err := h.DB.CreateArtifact(r.Context(), session.ID, artifactTitle, nil); err != nil {
		log.Printf("Error creating default artifact for session %s: %v", session.ID, err)
		// Non-fatal: session is created; user can create an artifact from the UI
	}

	response := CreateSessionResponse{
		ID:        session.ID.String(),
		Title:     session.Title,
		CreatedBy: session.CreatedBy,
		Status:    string(session.Status),
		CreatedAt: session.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// CopySessionRequest is the optional body for POST /api/sessions/:id/copy
type CopySessionRequest struct {
	Title *string `json:"title,omitempty"`
}

// CopySessionResponse is the response for POST /api/sessions/:id/copy
type CopySessionResponse struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	CreatedBy *string `json:"created_by,omitempty"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

// CopySession creates a new session with the same artifacts and materials as the source (Creator or Admin only). Everything in the new session is a standalone copy: new IDs, new storage keys, and new file paths; nothing references the source session's storage or records. Questions, answers, and unread state are not copied. RAG is reprocessed for the new session.
func (h *Handlers) CopySession(w http.ResponseWriter, r *http.Request) {
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
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "sessions" || parts[3] != "copy" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	sourceSessionID, err := uuid.Parse(parts[2])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	ctx := r.Context()
	sourceSession, err := h.DB.GetSession(ctx, sourceSessionID)
	if err != nil || sourceSession == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	// Only source creator or admin can copy
	if user.GlobalRole != models.GlobalRoleAdmin {
		if sourceSession.CreatedBy == nil || *sourceSession.CreatedBy != user.Email {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the session creator or an admin can copy this session"})
			return
		}
	}

	var copyReq CopySessionRequest
	_ = json.NewDecoder(r.Body).Decode(&copyReq) // optional body; ignore decode error for empty body

	createdBy := user.Email
	title := "Copy of " + sourceSession.Title
	if copyReq.Title != nil && strings.TrimSpace(*copyReq.Title) != "" {
		title = strings.TrimSpace(*copyReq.Title)
		exists, err := h.DB.SessionWithTitleExistsForCreator(ctx, createdBy, title, nil)
		if err != nil {
			log.Printf("CopySession SessionWithTitleExistsForCreator: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "A session with this name already exists. Please use a unique name."})
			return
		}
	} else {
		// Default title: ensure uniqueness by appending (2), (3), ... as needed
		if len(title) > 512 {
			title = title[:512]
		}
		for n := 1; ; n++ {
			if n > 1 {
				suffix := fmt.Sprintf(" (%d)", n)
				title = "Copy of " + sourceSession.Title + suffix
				if len(title) > 512 {
					title = title[:512]
				}
			}
			exists, err := h.DB.SessionWithTitleExistsForCreator(ctx, createdBy, title, nil)
			if err != nil {
				log.Printf("CopySession SessionWithTitleExistsForCreator: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
				return
			}
			if !exists {
				break
			}
		}
	}

	newSession := &models.Session{
		ID:        uuid.New(),
		Title:     title,
		CreatedBy: &createdBy,
		Status:    models.SessionStatusOpen,
	}
	if err := h.DB.CreateSession(ctx, newSession); err != nil {
		log.Printf("CopySession CreateSession: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	// Everything below is a standalone copy: new IDs, new storage keys, new paths. No references to source session storage or records.
	artifacts, err := h.DB.GetArtifactsBySessionID(ctx, sourceSessionID)
	if err != nil {
		log.Printf("CopySession GetArtifactsBySessionID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list source artifacts"})
		return
	}
	oldToNewArtifact := make(map[uuid.UUID]uuid.UUID)
	if len(artifacts) == 0 {
		// Ensure at least one artifact so the session is usable (match CreateSession behavior)
		if _, err := h.DB.CreateArtifact(ctx, newSession.ID, newSession.Title, nil); err != nil {
			log.Printf("CopySession CreateArtifact default: %v", err)
		}
	}
	for _, a := range artifacts {
		newArtifact, err := h.DB.CreateArtifact(ctx, newSession.ID, a.Title, a.Description)
		if err != nil {
			log.Printf("CopySession CreateArtifact: %v", err)
			continue
		}
		oldToNewArtifact[a.ID] = newArtifact.ID
	}
	materials, err := h.DB.GetActiveMaterialsBySessionID(ctx, sourceSessionID)
	if err != nil {
		log.Printf("CopySession GetActiveMaterialsBySessionID: %v", err)
	} else {
		r2Prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		for _, m := range materials {
			newArtifactID, ok := oldToNewArtifact[m.ArtifactID]
			if !ok {
				continue
			}
			newMaterial := &models.Material{
				ID:              uuid.New(),
				ArtifactID:      newArtifactID,
				SessionID:       newSession.ID,
				Kind:            m.Kind,
				Filename:        m.Filename,
				ContentType:     m.ContentType,
				StorageURL:      "",
				StorageProvider: m.StorageProvider,
				StorageKey:      "",
				SizeBytes:       m.SizeBytes,
				TextStatus:      m.TextStatus,
				ExtractedText:   m.ExtractedText,
				Title:           m.Title,
				ErrorMessage:    m.ErrorMessage,
			}
			if m.StorageProvider == "r2" && m.StorageKey != "" && h.Storage != nil {
				newKey := storage.BuildArtifactStorageKey(r2Prefix, newSession.ID, newArtifactID, m.Filename)
				rc, err := h.Storage.Get(ctx, m.StorageKey)
				if err != nil {
					log.Printf("CopySession R2 Get %s: %v", m.StorageKey, err)
					continue
				}
				var size int64
				if m.SizeBytes != nil {
					size = *m.SizeBytes
				}
				_, _, err = h.Storage.Put(ctx, newKey, rc, m.ContentType, size)
				_ = rc.Close()
				if err != nil {
					log.Printf("CopySession R2 Put %s: %v", newKey, err)
					continue
				}
				newMaterial.StorageKey = newKey
			} else if m.StorageProvider == "local" && m.Filename != "" {
				srcPath := filepath.Join(storage.UploadRoot(), storage.SessionStorageRoot, sourceSessionID.String(), "data", "uploads", filepath.Base(m.Filename))
				dstDir := storage.SessionUploadsAbsDir(newSession.ID)
				if err := os.MkdirAll(dstDir, 0755); err != nil {
					log.Printf("CopySession MkdirAll: %v", err)
					continue
				}
				dstPath := filepath.Join(dstDir, filepath.Base(m.Filename))
				srcF, err := os.Open(srcPath)
				if err != nil {
					log.Printf("CopySession local Open %s: %v", srcPath, err)
					continue
				}
				dstF, err := os.Create(dstPath)
				if err != nil {
					srcF.Close()
					log.Printf("CopySession local Create %s: %v", dstPath, err)
					continue
				}
				_, _ = io.Copy(dstF, srcF)
				srcF.Close()
				_ = dstF.Close()
				newMaterial.StorageURL = storage.SessionArtifactPath(newSession.ID, m.Filename)
			}
			// Copy slide assets (manifest + PNGs) so the cloned session has slide previews.
			if models.MaterialSupportsDerivedSlideDeck(m) {
				if m.StorageProvider == "r2" && m.StorageKey != "" && newMaterial.StorageKey != "" && h.Storage != nil {
					rc, err := h.Storage.Get(ctx, storage.SlidesManifestKeyFromArtifactKey(m.StorageKey))
					if err == nil {
						var manifest utils.SlideManifest
						if json.NewDecoder(rc).Decode(&manifest) == nil {
							rc.Close()
							newSlides := make([]utils.SlideManifestEntry, 0, len(manifest.Slides))
							for _, entry := range manifest.Slides {
								slideRc, err := h.Storage.Get(ctx, entry.StorageKey)
								if err != nil {
									continue
								}
								newSlideKey := storage.SlideImageKeyFromArtifactKey(newMaterial.StorageKey, entry.Index)
								_, _, err = h.Storage.Put(ctx, newSlideKey, slideRc, "image/png", 0)
								slideRc.Close()
								if err != nil {
									continue
								}
								newSlides = append(newSlides, utils.SlideManifestEntry{Index: entry.Index, StorageKey: newSlideKey})
							}
							newManifestBytes, _ := json.Marshal(utils.SlideManifest{Slides: newSlides})
							_, _, _ = h.Storage.Put(ctx, storage.SlidesManifestKeyFromArtifactKey(newMaterial.StorageKey), bytes.NewReader(newManifestBytes), "application/json", int64(len(newManifestBytes)))
						} else {
							rc.Close()
						}
					}
				} else if m.StorageProvider == "local" && newMaterial.StorageURL != "" && m.StorageURL != "" {
					srcSlidesDir := filepath.Join(storage.UploadRoot(), m.StorageURL+"_slides")
					dstSlidesDir := filepath.Join(storage.UploadRoot(), newMaterial.StorageURL+"_slides")
					if info, err := os.Stat(srcSlidesDir); err == nil && info.IsDir() {
						_ = os.MkdirAll(dstSlidesDir, 0755)
						entries, _ := os.ReadDir(srcSlidesDir)
						for _, e := range entries {
							srcF, err := os.Open(filepath.Join(srcSlidesDir, e.Name()))
							if err != nil {
								continue
							}
							dstF, err := os.Create(filepath.Join(dstSlidesDir, e.Name()))
							if err != nil {
								srcF.Close()
								continue
							}
							_, _ = io.Copy(dstF, srcF)
							srcF.Close()
							_ = dstF.Close()
						}
					}
				}
			}
			if err := h.DB.CreateMaterial(ctx, newMaterial); err != nil {
				log.Printf("CopySession CreateMaterial: %v", err)
			}
		}
	}
	// Copy session links (URL, title, status, extracted_text) so the new session has the same links and RAG can index them.
	sourceLinks, _ := h.DB.GetSessionLinksBySessionID(ctx, sourceSessionID)
	for _, listLink := range sourceLinks {
		full, err := h.DB.GetSessionLinkByID(ctx, listLink.ID)
		if err != nil || full == nil {
			continue
		}
		newLink := &models.SessionLink{
			ID:            uuid.New(),
			SessionID:     newSession.ID,
			URL:           full.URL,
			Title:         full.Title,
			Status:        full.Status,
			ExtractedText: full.ExtractedText,
			ErrorMessage:  full.ErrorMessage,
		}
		if err := h.DB.CreateSessionLink(ctx, newLink); err != nil {
			log.Printf("CopySession CreateSessionLink: %v", err)
		}
	}
	// Copy video_sources (Zoom/embed/upload: transcript, URLs, and stored video file so copy has playable video).
	sourceVideoSources, _ := h.DB.GetVideoSourcesBySessionID(ctx, sourceSessionID)
	for i, vs := range sourceVideoSources {
		newArtifactID, ok := oldToNewArtifact[vs.ArtifactID]
		if !ok {
			continue
		}
		copyVS := &models.VideoSource{
			ID:                    uuid.New(),
			ArtifactID:            newArtifactID,
			SessionID:             newSession.ID,
			Provider:              vs.Provider,
			VideoURL:              vs.VideoURL,
			PlaybackMode:          vs.PlaybackMode,
			EmbedURL:              vs.EmbedURL,
			MediaURL:              vs.MediaURL,
			DurationSeconds:       vs.DurationSeconds,
			PosterURL:             vs.PosterURL,
			SourceType:            vs.SourceType,
			StoredVideoObjectKey:  nil,
			OriginalURL:           vs.OriginalURL,
			FailureReason:         vs.FailureReason,
			TranscriptStatus:      vs.TranscriptStatus,
			AutoTranscribeEnabled: vs.AutoTranscribeEnabled,
			TranscriptionSource:   vs.TranscriptionSource,
			TranscriptionJobID:    nil,
			VideoRole:             vs.VideoRole,
		}
		// Copy stored video object (R2 or local) so the clone has the actual file for playback.
		if vs.StoredVideoObjectKey != nil && *vs.StoredVideoObjectKey != "" {
			oldKey := *vs.StoredVideoObjectKey
			newKey := strings.Replace(oldKey, sourceSessionID.String(), newSession.ID.String(), 1)
			if newKey != oldKey {
				if h.Storage != nil {
					rc, err := h.Storage.Get(ctx, oldKey)
					if err == nil {
						_, _, err = h.Storage.Put(ctx, newKey, rc, "video/mp4", 0)
						rc.Close()
						if err == nil {
							copyVS.StoredVideoObjectKey = &newKey
						}
					}
				}
				if copyVS.StoredVideoObjectKey == nil && strings.HasPrefix(oldKey, "sessions/") {
					absSrc := filepath.Join(storage.UploadRoot(), filepath.FromSlash(oldKey))
					absDst := filepath.Join(storage.UploadRoot(), filepath.FromSlash(newKey))
					if srcF, err := os.Open(absSrc); err == nil {
						if err := os.MkdirAll(filepath.Dir(absDst), 0755); err == nil {
							if dstF, err := os.Create(absDst); err == nil {
								_, _ = io.Copy(dstF, srcF)
								dstF.Close()
								copyVS.StoredVideoObjectKey = &newKey
							}
						}
						srcF.Close()
					}
				}
			}
		}
		if err := h.DB.CreateVideoSource(ctx, copyVS); err != nil {
			log.Printf("CopySession CreateVideoSource: %v", err)
			continue
		}
		if vs.TranscriptText != nil && *vs.TranscriptText != "" {
			_ = h.DB.UpdateVideoSourceZoomTranscript(ctx, copyVS.ID, *vs.TranscriptText, vs.RawVTT, vs.TranscriptSegments)
		}
		// Ensure first copied source is primary so UI shows one primary
		if i == 0 {
			_ = h.DB.SetVideoSourceVideoRole(ctx, newSession.ID, copyVS.ID, models.VideoRolePrimary)
		}
	}
	// Copy primary video (file_artifact) if present: copy R2 object or local file and create new file_artifact, set session primary.
	copiedPrimaryVideo := false
	if sourceSession.PrimaryVideoArtifactID != nil {
		fa, err := h.DB.GetFileArtifactByID(ctx, *sourceSession.PrimaryVideoArtifactID)
		if err == nil && fa != nil && fa.Status == models.FileArtifactStatusReady && fa.StorageKey != "" {
			filename := "video"
			if fa.Filename != nil && *fa.Filename != "" {
				filename = *fa.Filename
			}
			newFAID := uuid.New()
			if fa.StorageProvider == "r2" && h.Storage != nil {
				newKey := storage.BuildArtifactStorageKey(strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/"), newSession.ID, newFAID, filename)
				rc, err := h.Storage.Get(ctx, fa.StorageKey)
				if err != nil {
					log.Printf("CopySession primary video R2 Get %s: %v", fa.StorageKey, err)
				} else {
					var size int64
					if fa.SizeBytes != nil {
						size = *fa.SizeBytes
					}
					_, _, err = h.Storage.Put(ctx, newKey, rc, fa.ContentType, size)
					_ = rc.Close()
					if err != nil {
						log.Printf("CopySession primary video R2 Put %s: %v", newKey, err)
					} else {
						newFA := &models.FileArtifact{
							ID:              newFAID,
							SessionID:       &newSession.ID,
							OwnerUserID:     nil,
							Kind:            fa.Kind,
							Filename:        fa.Filename,
							ContentType:     fa.ContentType,
							SizeBytes:       fa.SizeBytes,
							Sha256:          fa.Sha256,
							StorageProvider: "r2",
							StorageBucket:   fa.StorageBucket,
							StorageKey:      newKey,
							Status:          models.FileArtifactStatusReady,
						}
						if err := h.DB.CreateFileArtifact(ctx, newFA); err != nil {
							log.Printf("CopySession CreateFileArtifact: %v", err)
						} else if err := h.DB.SetSessionPrimaryVideoArtifact(ctx, newSession.ID, &newFAID); err != nil {
							log.Printf("CopySession SetSessionPrimaryVideoArtifact: %v", err)
						} else {
							copiedPrimaryVideo = true
						}
					}
				}
			} else if fa.StorageProvider == "local" {
				baseName := filepath.Base(fa.StorageKey)
				if baseName == "" || baseName == "." {
					baseName = "zoom.mp4"
				}
				newRelKey := filepath.Join(storage.SessionStorageRoot, newSession.ID.String(), "videos", baseName)
				absSrc := filepath.Join(storage.UploadRoot(), filepath.FromSlash(fa.StorageKey))
				absDst := filepath.Join(storage.UploadRoot(), filepath.FromSlash(newRelKey))
				if srcF, err := os.Open(absSrc); err != nil {
					log.Printf("CopySession primary video local Open %s: %v", absSrc, err)
				} else {
					if err := os.MkdirAll(filepath.Dir(absDst), 0755); err != nil {
						srcF.Close()
						log.Printf("CopySession primary video MkdirAll: %v", err)
					} else if dstF, err := os.Create(absDst); err != nil {
						srcF.Close()
						log.Printf("CopySession primary video Create %s: %v", absDst, err)
					} else {
						n, _ := io.Copy(dstF, srcF)
						srcF.Close()
						_ = dstF.Close()
						sizeBytes := n
						newFA := &models.FileArtifact{
							ID:              newFAID,
							SessionID:       &newSession.ID,
							OwnerUserID:     nil,
							Kind:            fa.Kind,
							Filename:        fa.Filename,
							ContentType:     fa.ContentType,
							SizeBytes:       &sizeBytes,
							Sha256:          fa.Sha256,
							StorageProvider: "local",
							StorageBucket:   "local",
							StorageKey:      filepath.ToSlash(newRelKey),
							Status:          models.FileArtifactStatusReady,
						}
						if err := h.DB.CreateFileArtifact(ctx, newFA); err != nil {
							log.Printf("CopySession CreateFileArtifact: %v", err)
						} else if err := h.DB.SetSessionPrimaryVideoArtifact(ctx, newSession.ID, &newFAID); err != nil {
							log.Printf("CopySession SetSessionPrimaryVideoArtifact: %v", err)
						} else {
							copiedPrimaryVideo = true
						}
					}
				}
			}
		}
	}
	// If copy has no MP4 yet but source had a cloud import job (Zoom/Teams), enqueue processing so the worker can fetch MP4 for the new session (in-app player only).
	if !copiedPrimaryVideo {
		jobSource := string(sourceSession.SourceProvider)
		if jobSource == "" {
			jobSource = models.SessionProcessingJobSourceZoom
		}
		if sourceJob, err := h.DB.GetSessionProcessingJobBySessionID(ctx, sourceSessionID, jobSource); err == nil && sourceJob != nil && (sourceJob.MeetingUUID != nil || sourceJob.InstanceUUID != nil) {
			creatorIdentity := sourceJob.CreatorIdentity
			if creatorIdentity == nil && sourceSession.CreatedBy != nil {
				creatorIdentity = sourceSession.CreatedBy
			}
			newJob := &models.SessionProcessingJob{
				ID:              uuid.New(),
				SessionID:       newSession.ID,
				Source:          jobSource,
				State:           models.ProcessingStateQueued,
				Stage:           models.ProcessingStageFetch,
				MeetingUUID:     sourceJob.MeetingUUID,
				InstanceUUID:    sourceJob.InstanceUUID,
				CreatorIdentity: creatorIdentity,
			}
			if err := h.DB.CreateOrGetSessionProcessingJob(ctx, newJob); err != nil {
				log.Printf("CopySession CreateOrGetSessionProcessingJob (%s) for new session: %v", jobSource, err)
			} else {
				if err := h.DB.UpdateSessionSourceProvider(ctx, newSession.ID, models.SessionSourceProvider(jobSource)); err != nil {
					log.Printf("CopySession UpdateSessionSourceProvider: %v", err)
				}
				log.Printf("CopySession enqueued %s processing for new session %s (MP4 will be available when job completes)", jobSource, newSession.ID)
			}
		}
	}
	h.triggerIndex(newSession.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CopySessionResponse{
		ID:        newSession.ID.String(),
		Title:     newSession.Title,
		CreatedBy: newSession.CreatedBy,
		Status:    string(newSession.Status),
		CreatedAt: newSession.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// DeleteSession removes a session and all its DB/file data. Admin-only.
func (h *Handlers) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if user.GlobalRole != models.GlobalRoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only an admin can delete sessions"})
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "api" || pathParts[1] != "sessions" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	sessionID, err := uuid.Parse(pathParts[2])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	ctx := r.Context()
	session, err := h.DB.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	// 1. R2: delete all objects under sessions/{sessionID}/
	if h.Storage != nil {
		r2Prefix := strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		prefix := "sessions/" + sessionID.String() + "/"
		if r2Prefix != "" {
			prefix = r2Prefix + "/" + prefix
		}
		if _, delErr := h.Storage.DeletePrefix(ctx, prefix); delErr != nil {
			log.Printf("DeleteSession R2 DeletePrefix %s: %v", prefix, delErr)
		}
	}
	// 2. Local: remove session directory (uploads, videos, transcripts)
	localDir := filepath.Join(storage.UploadRoot(), storage.SessionStorageRoot, sessionID.String())
	if err := os.RemoveAll(localDir); err != nil {
		log.Printf("DeleteSession RemoveAll %s: %v", localDir, err)
	}
	// 3. DB: delete questions first (avoids NOT NULL on questions.session_id if FK is ON DELETE SET NULL), then file_artifacts, then session
	if err := h.DB.DeleteQuestionsBySessionID(ctx, sessionID); err != nil {
		log.Printf("DeleteSession DeleteQuestionsBySessionID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session data"})
		return
	}
	if err := h.DB.DeleteFileArtifactsBySessionID(ctx, sessionID); err != nil {
		log.Printf("DeleteSession DeleteFileArtifactsBySessionID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session data"})
		return
	}
	if err := h.DB.DeleteSession(ctx, sessionID); err != nil {
		log.Printf("DeleteSession DeleteSession: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete session"})
		return
	}
	if h.Hub != nil {
		h.Hub.BroadcastSessionDeleted(sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetArtifactsBySession retrieves all artifacts for a session
func (h *Handlers) GetArtifactsBySession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id}/artifacts)
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "sessions" || pathParts[2] != "artifacts" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Verify session exists
	_, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get artifacts
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error getting artifacts: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get artifacts: %v", err), http.StatusInternalServerError)
		return
	}

	type GetArtifactsResponse struct {
		Artifacts []*models.Artifact `json:"artifacts"`
	}

	response := GetArtifactsResponse{
		Artifacts: artifacts,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetSession retrieves a session with its artifact context
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL path (e.g., /sessions/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 2 || pathParts[0] != "sessions" {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(pathParts[1])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Get session
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Session not found: %v", err), http.StatusNotFound)
		return
	}

	// Get artifacts for this session
	artifacts, err := h.DB.GetArtifactsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get artifacts for session %s: %v", sessionID, err)
		artifacts = []*models.Artifact{}
	}

	// Get materials and video sources from session (active only; excludes soft-deleted)
	allMaterials, err := h.DB.GetActiveMaterialsBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get materials: %v", err)
		allMaterials = []*models.Material{}
	}

	allVideoSources, err := h.DB.GetVideoSourcesBySessionID(r.Context(), sessionID)
	if err != nil {
		log.Printf("Warning: Failed to get video sources: %v", err)
		allVideoSources = []*models.VideoSource{}
	}

	links, _ := h.DB.GetSessionLinksBySessionID(r.Context(), sessionID)
	if links == nil {
		links = []*models.SessionLink{}
	}

	// Get recent questions (limit 20)
	questions, answers, err := h.DB.GetQuestionsBySessionID(r.Context(), sessionID, 20)
	if err != nil {
		log.Printf("Warning: Failed to get questions: %v", err)
		questions = []*models.Question{}
		answers = []*models.Answer{}
	}
	// Enrich answer citations with source_id from chunks so doc citations open correctly in participant view
	chunks, _ := h.DB.ListSessionChunksBySessionID(r.Context(), sessionID)
	chunkSourceByID := make(map[string]struct{ SourceID, SourceType string })
	for _, c := range chunks {
		if c.SourceID != nil {
			chunkSourceByID[c.ID.String()] = struct{ SourceID, SourceType string }{
				SourceID:   c.SourceID.String(),
				SourceType: c.SourceType,
			}
		}
	}
	for _, a := range answers {
		if a == nil {
			continue
		}
		for i := range a.Citations {
			c := &a.Citations[i]
			if c.SourceID != "" {
				continue
			}
			if info, ok := chunkSourceByID[c.ChunkID]; ok {
				c.SourceID = info.SourceID
				if c.SourceType == "" {
					c.SourceType = info.SourceType
				}
			}
		}
	}
	// Enrich citations with navigation (url for link citations, video seek, doc page) so frontend can open links/sections
	chunkURLByChunkID := make(citation.ChunkURLByChunkID)
	for _, ch := range chunks {
		if ch.SourceType == "link" && ch.AnchorJSON != nil {
			if u, ok := ch.AnchorJSON["url"].(string); ok && u != "" {
				chunkURLByChunkID[ch.ID.String()] = u
			}
		}
	}
	for _, a := range answers {
		if a == nil {
			continue
		}
		for i := range a.Citations {
			c := &a.Citations[i]
			t := citation.ResolveCitationTarget(*c, allVideoSources, allMaterials, links, chunkURLByChunkID)
			c.Navigation = &models.CitationNavigation{
				Type:     t.Type,
				URL:      t.URL,
				Fragment: t.Fragment,
				SeekMs:   t.SeekMs,
				Page:     t.Page,
				Block:    t.Block,
			}
		}
	}
	h.enrichAnswersWithDisplayNames(r.Context(), answers)

	// Determine mode: creator if current_user matches session.created_by, otherwise participant
	mode := "participant"
	currentUser := r.Header.Get("X-Current-User")
	if currentUser == "" {
		// Fallback to query parameter
		currentUser = r.URL.Query().Get("user")
	}
	if currentUser != "" && session.CreatedBy != nil && *session.CreatedBy == currentUser {
		mode = "creator"
	}

	// When participant_ref is provided, return unread material IDs for the "new document" marker
	var unreadMaterialIDs []string
	participantRef := r.Header.Get("X-Participant-Ref")
	if participantRef == "" {
		participantRef = r.URL.Query().Get("participant_ref")
	}
	if participantRef != "" {
		unread, err := h.DB.GetUnreadMaterialIDsForParticipant(r.Context(), sessionID, participantRef)
		if err != nil {
			log.Printf("Warning: Failed to get unread materials for participant: %v", err)
		} else {
			for _, id := range unread {
				unreadMaterialIDs = append(unreadMaterialIDs, id.String())
			}
		}
	}

	// R2-only playback: set video_access_url only when primary artifact exists, ready, r2, and content_type is video.
	var videoAccessURL, playbackReasonCode, playbackMessage string
	var primaryVideoArtifact *models.FileArtifact // captured for SessionPrimaryDescriptor enrichment below
	if session.PrimaryVideoArtifactID != nil {
		fa, err := h.DB.GetFileArtifactByID(r.Context(), *session.PrimaryVideoArtifactID)
		primaryVideoArtifact = fa
		if err != nil || fa == nil {
			playbackReasonCode = "VIDEO_NOT_INGESTED"
			playbackMessage = "Video not available for this session."
		} else if fa.Status == models.FileArtifactStatusPending {
			playbackReasonCode = "VIDEO_INGEST_PENDING"
			playbackMessage = "Video is still being prepared. Refresh in a moment."
		} else if fa.Status == models.FileArtifactStatusFailed {
			playbackReasonCode = "VIDEO_INGEST_FAILED"
			playbackMessage = "Video ingest failed. Creator can retry import."
		} else if fa.Status == models.FileArtifactStatusReady {
			// Always use same-origin primary-video URL so the browser never hits R2 directly (avoids CORS). API streams from R2 or local.
			ct := strings.TrimSpace(strings.ToLower(fa.ContentType))
			isVideo := ct == "video/mp4" || strings.HasPrefix(ct, "video/")
			if !isVideo {
				playbackReasonCode = "VIDEO_NOT_INGESTED"
				playbackMessage = "Video not available for this session."
			} else if fa.StorageProvider == "r2" && h.Storage != nil || fa.StorageProvider == "local" {
				base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("API_PUBLIC_ORIGIN")), "/")
				if base == "" {
					scheme := "https"
					if r.TLS == nil {
						scheme = "http"
					}
					if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
						scheme = s
					}
					base = scheme + "://" + r.Host
				}
				videoAccessURL = fmt.Sprintf("%s/api/sessions/%s/primary-video", base, sessionID.String())
			} else {
				playbackReasonCode = "VIDEO_NOT_INGESTED"
				playbackMessage = "Video not available for this session."
			}
		}
	}

	primaryVideo, additionalVideos := resolveEffectivePrimaryAndAdditional(allVideoSources)
	var createdByDisplayName *string
	if session.CreatedBy != nil && *session.CreatedBy != "" {
		if creator, err := h.DB.GetUserByEmail(r.Context(), *session.CreatedBy); err == nil && creator != nil {
			createdByDisplayName = &creator.DisplayName
		}
	}
	// For PPT/PPTX materials, indicate whether derived slides manifest exists and explicit preview status.
	materialSlidesReady := make(map[string]bool)
	materialSlidesStatus := make(map[string]string)
	for _, m := range allMaterials {
		if m != nil && models.MaterialSupportsDerivedSlideDeck(m) {
			status := h.GetSlidesStatus(r.Context(), m)
			materialSlidesStatus[m.ID.String()] = status
			materialSlidesReady[m.ID.String()] = status == "ready"
		}
	}
	// SCRUM-271: resolve primary descriptor for the center pane. The pure
	// resolver returns kind+id; the enrichment helpers fill title/status from
	// data already loaded above (materials, links, the file_artifact fetched
	// for video playback). Both legacy video-first sessions
	// (primary_content_kind NULL + primary_video_artifact_id set) and
	// explicit kind=document/link rows produce a stable Primary block.
	primary := resolveSessionPrimary(session)
	primary = enrichSessionPrimaryFromMaterials(primary, allMaterials)
	primary = enrichSessionPrimaryFromLinks(primary, links)
	primary = enrichSessionPrimaryFromFileArtifact(primary, primaryVideoArtifact)

	response := GetSessionResponse{
		Session:              session,
		Artifacts:            artifacts,
		Materials:            allMaterials,
		VideoSources:         allVideoSources,
		PrimaryVideo:         primaryVideo,
		AdditionalVideos:     additionalVideos,
		RecentQuestions:      questions,
		RecentAnswers:        answers,
		Mode:                 mode,
		CreatedByDisplayName: createdByDisplayName,
		UnreadMaterialIDs:    unreadMaterialIDs,
		VideoAccessURL:       videoAccessURL,
		PlaybackReasonCode:   playbackReasonCode,
		PlaybackMessage:      playbackMessage,
		Links:                links,
		MaterialSlidesReady:  materialSlidesReady,
		MaterialSlidesStatus: materialSlidesStatus,
		Primary:              primary,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // video_access_url is presigned and time-limited; do not cache session response
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UpdateSessionStatus updates the status, title, premise, etc. of a session (creator or admin only).
func (h *Handlers) UpdateSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Extract session ID from URL path (/api/sessions/{id} or /sessions/{id})
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var sessionIDStr string
	if len(pathParts) >= 3 && pathParts[0] == "api" && pathParts[1] == "sessions" {
		sessionIDStr = pathParts[2]
	} else if len(pathParts) >= 2 && pathParts[0] == "sessions" {
		sessionIDStr = pathParts[1]
	} else {
		http.Error(w, "Invalid URL path", http.StatusBadRequest)
		return
	}

	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Load session (need it for auth and for title uniqueness check)
	session, err := h.DB.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// SCRUM-227: editor authority is granted by global admin OR per-session
	// session_memberships.role='creator' — including promoted creators, not
	// only the original by-email creator.
	canEdit, err := h.userIsSessionEditor(r.Context(), sessionID, user)
	if err != nil {
		log.Printf("UpdateSessionStatus userIsSessionEditor: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check authorization"})
		return
	}
	if !canEdit {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the session creator or an admin can update this session"})
		return
	}

	// Parse request body
	type UpdateSessionRequest struct {
		Status          *string `json:"status"`
		Title           *string `json:"title"`
		Premise         *string `json:"premise"`
		PrimaryDecision *string `json:"primary_decision"`
		DecisionOutcome *string `json:"decision_outcome"`
	}

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Update title if provided
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title cannot be empty"})
			return
		}
		createdBy := ""
		if session.CreatedBy != nil {
			createdBy = *session.CreatedBy
		}
		exists, err := h.DB.SessionWithTitleExistsForCreator(r.Context(), createdBy, title, &sessionID)
		if err != nil {
			log.Printf("UpdateSessionStatus SessionWithTitleExistsForCreator: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check session title"})
			return
		}
		if exists {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "A session with this name already exists. Please use a unique name."})
			return
		}
		if err := h.DB.UpdateSessionTitle(r.Context(), sessionID, title); err != nil {
			log.Printf("UpdateSessionStatus UpdateSessionTitle: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session title"})
			return
		}
	}

	// Update status if provided
	if req.Status != nil {
		status := models.SessionStatus(*req.Status)
		if status != models.SessionStatusOpen && status != models.SessionStatusClosed {
			http.Error(w, fmt.Sprintf("Invalid status: %s. Must be 'open' or 'closed'", *req.Status), http.StatusBadRequest)
			return
		}
		if err = h.DB.UpdateSessionStatus(r.Context(), sessionID, status); err != nil {
			log.Printf("Error updating session status: %v", err)
			http.Error(w, fmt.Sprintf("Failed to update session status: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Update premise / primary_decision / decision_outcome if any is provided
	if req.Premise != nil || req.PrimaryDecision != nil || req.DecisionOutcome != nil {
		if err = h.DB.UpdateSessionContext(r.Context(), sessionID, req.Premise, req.PrimaryDecision, req.DecisionOutcome); err != nil {
			log.Printf("Error updating session context: %v", err)
			http.Error(w, fmt.Sprintf("Failed to update session context: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Get updated session
	session, err = h.DB.GetSession(r.Context(), sessionID)
	if err != nil {
		log.Printf("Error getting updated session: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get updated session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(session)
}
