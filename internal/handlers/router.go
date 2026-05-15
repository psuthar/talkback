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

	if len(parts) == 1 {
		// /artifacts - POST (create artifact, requires session_id in body)
		if r.Method == http.MethodPost {
			h.CreateArtifact(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
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
		// /artifacts/{id}/materials/{material_id}/file - GET (serve material file for viewing)
		if parts[2] == "materials" && parts[4] == "file" {
			if r.Method == http.MethodGet {
				h.ServeMaterialFile(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /artifacts/{id}/video/{video_id}/transcript
		if parts[2] == "video" && parts[4] == "transcript" {
			if r.Method == http.MethodPost {
				h.UploadTranscript(w, r)
				return
			}
		}
		// /artifacts/{id}/video/{video_id}/transcript-job
		if parts[2] == "video" && parts[4] == "transcript-job" {
			if r.Method == http.MethodGet {
				h.GetTranscriptJob(w, r)
				return
			}
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "Invalid path", http.StatusNotFound)
}

// APISessionsRouter handles /api/sessions/:id/import/zoom, /import/teams, and /api/sessions/:id/ingestion
func (h *Handlers) APISessionsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "sessions" {
		http.Error(w, "Invalid path", http.StatusNotFound)
		return
	}
	if parts[3] == "import" && len(parts) >= 5 && parts[4] == "zoom" {
		if r.Method == http.MethodPost {
			h.SessionImportZoom(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "import" && len(parts) >= 5 && parts[4] == "teams" {
		if r.Method == http.MethodPost {
			h.SessionImportTeams(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "import" && len(parts) >= 5 && parts[4] == "google-meet" {
		if r.Method == http.MethodPost {
			h.SessionImportGoogleMeet(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "ingestion" {
		if r.Method == http.MethodGet {
			h.SessionIngestionStatus(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "processing" {
		if len(parts) == 4 {
			if r.Method == http.MethodGet {
				h.SessionProcessingStatus(w, r)
				return
			}
		}
		if len(parts) == 5 {
			if parts[4] == "retry" && r.Method == http.MethodPost {
				h.SessionProcessingRetry(w, r)
				return
			}
			if parts[4] == "cancel" && r.Method == http.MethodPost {
				h.SessionProcessingCancel(w, r)
				return
			}
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "transcript" {
		if r.Method == http.MethodGet {
			h.SessionTranscript(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "ask" {
		if r.Method == http.MethodPost {
			h.RequireAuth(h.SessionAsk)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "orchestration" && len(parts) >= 5 && parts[4] == "draft-answers" {
		if len(parts) == 5 && r.Method == http.MethodPost {
			h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, r)
			return
		}
		if len(parts) == 6 && r.Method == http.MethodDelete {
			h.RequireAuth(h.DeleteOrchestrationDraftAnswer)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "orchestration" && len(parts) >= 5 && parts[4] == "recommendations" {
		if len(parts) == 5 && r.Method == http.MethodGet {
			h.RequireAuth(h.ListOrchestrationRecommendations)(w, r)
			return
		}
		if len(parts) == 6 && parts[5] == "audit" && r.Method == http.MethodGet {
			h.RequireAuth(h.ListOrchestrationRecommendationStatusAudit)(w, r)
			return
		}
		if len(parts) == 6 && parts[5] == "sync" && r.Method == http.MethodPost {
			h.RequireAuth(h.SyncOrchestrationRecommendations)(w, r)
			return
		}
		if len(parts) == 6 && (r.Method == http.MethodPatch || r.Method == http.MethodPut) {
			h.RequireAuth(h.UpdateOrchestrationRecommendationStatus)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "stance" {
		if r.Method == http.MethodPost {
			h.RequireAuth(h.SessionSubmitStance)(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			h.RequireAuth(h.SessionDeleteStance)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "stances" {
		if r.Method == http.MethodGet {
			h.RequireAuth(h.SessionGetStances)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "questions" && len(parts) >= 5 && parts[4] == "polish" {
		if r.Method == http.MethodPost {
			h.SessionPolishQuestion(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "reindex" {
		if r.Method == http.MethodPost {
			h.SessionReindex(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "set-primary-video" {
		if r.Method == http.MethodPost {
			h.SetSessionPrimaryVideoSource(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// SCRUM-412: PATCH /api/sessions/:id/primary-recording — multi-recording
	// era canonical endpoint. Same outcome as set-primary-video but with
	// userIsSessionEditor (role-based) authz and the standardized response
	// shape.
	if parts[3] == "primary-recording" {
		if r.Method == http.MethodPatch {
			h.RequireAuth(h.SessionPatchPrimaryRecording)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "primary-video-artifact" {
		if r.Method == http.MethodPost {
			h.SetSessionPrimaryVideoArtifact(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "primary-video" {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			h.SessionPrimaryVideoStream(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "playback" {
		if r.Method == http.MethodGet {
			h.SessionPlayback(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "materials" {
		if len(parts) == 4 {
			if r.Method == http.MethodGet {
				h.ListSessionMaterials(w, r)
				return
			}
		}
		if len(parts) == 5 {
			if parts[4] == "upload" && r.Method == http.MethodPost {
				h.SessionUploadMaterial(w, r)
				return
			}
			if parts[4] == "paste" && r.Method == http.MethodPost {
				h.SessionPasteMaterial(w, r)
				return
			}
			if r.Method == http.MethodDelete {
				h.DeleteSessionMaterial(w, r)
				return
			}
		}
		// SCRUM-287: POST /api/sessions/:id/materials/:mid/retry-slides — creator-only
		// re-run of the .ppt/.pptx slide derivation pipeline (auth handled inside
		// the handler; we don't wrap with RequireAuth because the handler reads the
		// user from context and emits the project's standard 401/403 JSON shape).
		if len(parts) == 6 && parts[5] == "retry-slides" && r.Method == http.MethodPost {
			h.RequireAuth(h.RetryMaterialSlides)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if parts[3] == "links" {
		if len(parts) == 4 {
			if r.Method == http.MethodGet {
				h.ListSessionLinks(w, r)
				return
			}
			if r.Method == http.MethodPost {
				h.RequireAuth(h.AddSessionLink)(w, r)
				return
			}
		}
		if len(parts) == 5 && r.Method == http.MethodDelete {
			h.RequireAuth(h.DeleteSessionLink)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.Error(w, "Invalid path", http.StatusNotFound)
}

// ApiSessionsRouterWithInvite wraps APISessionsRouter: .../copy (POST), .../invitations (POST/GET) use new invite flow; DELETE .../sessions/:id; .../invite POST legacy; else APISessionsRouter.
func (h *Handlers) ApiSessionsRouterWithInvite(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	pathTrim := strings.Trim(path, "/")
	parts := strings.Split(pathTrim, "/")
	if r.Method == http.MethodDelete && len(parts) == 3 && parts[0] == "api" && parts[1] == "sessions" {
		h.RequireAuth(h.DeleteSession)(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/copy") {
		h.RequireAuth(h.CopySession)(w, r)
		return
	}
	if (r.Method == http.MethodPatch || r.Method == http.MethodPut) && len(parts) == 3 && parts[0] == "api" && parts[1] == "sessions" {
		h.RequireAuth(h.UpdateSessionStatus)(w, r)
		return
	}
	// /api/sessions/:id/memberships/:userId (PATCH) — change role on accepted member
	if (r.Method == http.MethodPatch || r.Method == http.MethodPut) &&
		len(parts) == 5 && parts[0] == "api" && parts[1] == "sessions" && parts[3] == "memberships" {
		h.RequireAuth(h.UpdateSessionMembership)(w, r)
		return
	}
	if strings.HasSuffix(path, "/invitations") {
		if r.Method == http.MethodPost {
			h.RequireAuth(h.CreateInvitation)(w, r)
			return
		}
		if r.Method == http.MethodGet {
			h.RequireAuth(h.ListInvitations)(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/invite") {
		writeJSON(w, http.StatusGone, map[string]string{"error": "use POST /api/sessions/:id/invitations with email and role"})
		return
	}
	h.APISessionsRouter(w, r)
}

// ApiInvitationsRouter handles /api/invitations/resolve, /api/invitations/accept, /api/invitations/register-and-accept, /api/invitations/:id/resend, /api/invitations/:id/revoke.
func (h *Handlers) ApiInvitationsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "invitations" {
		http.Error(w, "Invalid path", http.StatusNotFound)
		return
	}
	if parts[2] == "resolve" {
		h.ResolveInvitation(w, r)
		return
	}
	if parts[2] == "accept" {
		h.OptionalAuthForAccept(h.AcceptInvitation)(w, r)
		return
	}
	if parts[2] == "register-and-accept" {
		h.RegisterAndAcceptInvitation(w, r)
		return
	}
	if parts[2] == "pending" {
		h.RequireAuth(h.ListPendingInvitations)(w, r)
		return
	}
	// parts[2] is UUID, parts[3] is resend, revoke, link, or accept
	if len(parts) >= 4 {
		if parts[3] == "resend" {
			h.RequireAuth(h.ResendInvitation)(w, r)
			return
		}
		if parts[3] == "revoke" {
			h.RequireAuth(h.RevokeInvitation)(w, r)
			return
		}
		if parts[3] == "link" {
			h.RequireAuth(h.GetInvitationLink)(w, r)
			return
		}
		if parts[3] == "accept" {
			h.RequireAuth(h.AcceptInvitationByID)(w, r)
			return
		}
	}
	http.Error(w, "Invalid path", http.StatusNotFound)
}

// SessionsRouter handles session-related routes
func (h *Handlers) SessionsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	// Check if path starts with "sessions"
	if len(parts) == 0 || parts[0] != "sessions" {
		http.Error(w, "Invalid path", http.StatusNotFound)
		return
	}

	if len(parts) == 1 {
		// /sessions - POST (create session)
		if r.Method == http.MethodPost {
			h.CreateSession(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(parts) == 2 {
		// /sessions/{id} - GET
		if r.Method == http.MethodGet {
			h.GetSession(w, r)
			return
		}
		// /sessions/{id} - PATCH/PUT for updating status (close session)
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			h.UpdateSessionStatus(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(parts) == 3 {
		action := parts[2]

		switch action {
		case "artifacts":
			// /sessions/{id}/artifacts - GET (list artifacts in session)
			if r.Method == http.MethodGet {
				h.GetArtifactsBySession(w, r)
				return
			}
		case "participants":
			if r.Method == http.MethodPost {
				h.JoinSessionParticipant(w, r)
				return
			}
		case "events":
			if r.Method == http.MethodPost {
				h.CreateSessionEvent(w, r)
				return
			}
		case "questions":
			if r.Method == http.MethodPost {
				h.AskSessionQuestion(w, r)
				return
			}
			if r.Method == http.MethodGet {
				h.GetSessionQuestions(w, r)
				return
			}
		case "timeline":
			// /sessions/{id}/timeline - GET (session timeline)
			if r.Method == http.MethodGet {
				h.GetSessionTimeline(w, r)
				return
			}
		case "playback":
			// /sessions/{id}/playback - GET (R2 presigned URL only)
			if r.Method == http.MethodGet {
				h.SessionPlayback(w, r)
				return
			}
		case "materials":
			// /sessions/{id}/materials - GET (list materials)
			if r.Method == http.MethodGet {
				h.ListSessionMaterials(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if len(parts) == 4 {
		// /sessions/{id}/questions/voice - POST (transcribe voice to text)
		if parts[2] == "questions" && parts[3] == "voice" {
			if r.Method == http.MethodPost {
				h.TranscribeSessionQuestionVoice(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/questions/mock - POST (create mock question for testing)
		if parts[2] == "questions" && parts[3] == "mock" {
			if r.Method == http.MethodPost {
				h.CreateMockQuestion(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/questions/{questionID} - DELETE (creator/admin only; cascades replies + answers)
		// SCRUM-366: server enforces creator-or-admin via userIsSessionEditor.
		if parts[2] == "questions" && r.Method == http.MethodDelete {
			h.RequireAuth(h.DeleteSessionQuestion)(w, r)
			return
		}
		// /sessions/{id}/video/upload - POST (upload MP4 file)
		if parts[2] == "video" && parts[3] == "upload" {
			if r.Method == http.MethodPost {
				h.UploadVideoFile(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/video/from-url - POST (smart URL ingestion)
		if parts[2] == "video" && parts[3] == "from-url" {
			if r.Method == http.MethodPost {
				h.IngestVideoFromURL(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/materials/seen - POST (mark materials as read by participant)
		if parts[2] == "materials" && parts[3] == "seen" {
			if r.Method == http.MethodPost {
				h.MarkSessionMaterialsSeen(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/materials/upload - POST
		if parts[2] == "materials" && parts[3] == "upload" {
			if r.Method == http.MethodPost {
				h.SessionUploadMaterial(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/materials/paste - POST
		if parts[2] == "materials" && parts[3] == "paste" {
			if r.Method == http.MethodPost {
				h.SessionPasteMaterial(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/orchestration/draft-answers - POST (AI draft; SCRUM-12)
		if parts[2] == "orchestration" && parts[3] == "draft-answers" {
			if r.Method == http.MethodPost {
				h.RequireAuth(h.CreateOrchestrationDraftAnswer)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/orchestration/recommendations - GET
		if parts[2] == "orchestration" && parts[3] == "recommendations" {
			if r.Method == http.MethodGet {
				h.RequireAuth(h.ListOrchestrationRecommendations)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/materials/{material_id} - DELETE (4 path segments)
		if parts[2] == "materials" && r.Method == http.MethodDelete {
			h.DeleteSessionMaterial(w, r)
			return
		}
	}
	if len(parts) == 5 && parts[2] == "materials" {
		// /sessions/{id}/materials/{material_id}/slides - GET
		if parts[4] == "slides" && r.Method == http.MethodGet {
			h.GetMaterialSlides(w, r)
			return
		}
		// /sessions/{id}/materials/{material_id}/slide-image - GET
		if parts[4] == "slide-image" && r.Method == http.MethodGet {
			h.GetMaterialSlideImage(w, r)
			return
		}
		// /sessions/{id}/materials/{material_id}/slide-pdf - GET (SCRUM-444/445 local PDF pipeline)
		if parts[4] == "slide-pdf" && r.Method == http.MethodGet {
			h.GetMaterialSlidePDF(w, r)
			return
		}
		// /sessions/{id}/materials/{material_id} - DELETE
		if r.Method == http.MethodDelete {
			h.DeleteSessionMaterial(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "sessions" && parts[3] == "materials" && r.Method == http.MethodGet {
		if parts[5] == "slides" {
			h.GetMaterialSlides(w, r)
			return
		}
		if parts[5] == "slide-image" {
			h.GetMaterialSlideImage(w, r)
			return
		}
		if parts[5] == "slide-pdf" {
			h.GetMaterialSlidePDF(w, r)
			return
		}
	}

	if len(parts) == 5 {
		// /sessions/{id}/questions/{questionId}/view - POST (mark question as viewed by participant)
		if parts[2] == "questions" && parts[4] == "view" {
			if r.Method == http.MethodPost {
				h.MarkQuestionViewed(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/video-sources/{video_source_id}/stream - GET (proxy Zoom MP4 for in-app playback)
		if parts[2] == "video-sources" && parts[4] == "stream" {
			if r.Method == http.MethodGet {
				h.ZoomVideoStream(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/video-sources/{video_source_id}/display-title - PATCH (set display title)
		if parts[2] == "video-sources" && parts[4] == "display-title" {
			if r.Method == http.MethodPatch {
				h.UpdateVideoSourceDisplayTitle(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/video/transcript/upload - POST (transcript file upload)
		if parts[2] == "video" && parts[3] == "transcript" && parts[4] == "upload" {
			if r.Method == http.MethodPost {
				h.UploadTranscriptFile(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/transcript-jobs/{job_id} - GET (transcript job status)
		if parts[2] == "transcript-jobs" {
			if r.Method == http.MethodGet {
				h.GetTranscriptJobStatus(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/orchestration/draft-answers/{answer_id} - DELETE (dismiss draft)
		if parts[2] == "orchestration" && parts[3] == "draft-answers" {
			if r.Method == http.MethodDelete {
				h.RequireAuth(h.DeleteOrchestrationDraftAnswer)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/orchestration/recommendations/sync - POST
		if parts[2] == "orchestration" && parts[3] == "recommendations" && parts[4] == "sync" {
			if r.Method == http.MethodPost {
				h.RequireAuth(h.SyncOrchestrationRecommendations)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/orchestration/recommendations/audit - GET
		if parts[2] == "orchestration" && parts[3] == "recommendations" && parts[4] == "audit" {
			if r.Method == http.MethodGet {
				h.RequireAuth(h.ListOrchestrationRecommendationStatusAudit)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/orchestration/recommendations/{recommendation_id} - PATCH/PUT
		if parts[2] == "orchestration" && parts[3] == "recommendations" {
			if r.Method == http.MethodPatch || r.Method == http.MethodPut {
				h.RequireAuth(h.UpdateOrchestrationRecommendationStatus)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

	if len(parts) == 5 {
		// /sessions/{id}/questions/{question_id}/answers - POST (create answer; admin or creator)
		if parts[2] == "questions" && parts[4] == "answers" {
			if r.Method == http.MethodPost {
				h.RequireAuth(h.CreateSessionAnswer)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /sessions/{id}/answers/{answer_id}/confirm - PATCH/PUT (update answer confirmation; admin or creator)
		if parts[2] == "answers" && parts[4] == "confirm" {
			if r.Method == http.MethodPatch || r.Method == http.MethodPut {
				h.RequireAuth(h.UpdateAnswerConfirmed)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

	if len(parts) == 6 {
		// /sessions/{id}/questions/{question_id}/answers/voice - POST (transcribe answer voice; admin or creator)
		if parts[2] == "questions" && parts[4] == "answers" && parts[5] == "voice" {
			if r.Method == http.MethodPost {
				h.RequireAuth(h.TranscribeSessionAnswerVoice)(w, r)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

	http.Error(w, "Invalid path", http.StatusNotFound)
}
