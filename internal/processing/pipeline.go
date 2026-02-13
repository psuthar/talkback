package processing

import (
	"bytes"
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/transcript"
	"github.com/psuthar/talkback/internal/utils"
)

// ZoomTokenFunc returns a valid Zoom access token for the given creator identity (for background workers).
type ZoomTokenFunc func(ctx context.Context, creatorIdentity string) (string, error)

// RunJob runs the ingestion pipeline for one session_processing_job (fetch → download → parse → chunk → embed → ready).
// Idempotent: skips stages whose outputs already exist. Updates job state and session mirror.
func RunJob(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getZoomToken ZoomTokenFunc) (err error) {
	sessionID := job.SessionID
	jobID := job.ID
	attempt := job.AttemptCount + 1

	// Mirror state for UI
	updateMirror := func(state string) {
		_ = db.UpdateSessionProcessingMirror(ctx, sessionID, state)
	}
	defer func() {
		if err != nil {
			updateMirror(job.State)
		}
	}()

	// Only Zoom is implemented
	if job.Source != "zoom" {
		return nil
	}

	instanceUUID := meetingUUIDForJob(job)
	if instanceUUID == "" {
		setJobFailedPermanent(ctx, db, jobID, attempt, "zoom_missing_meeting", "missing meeting_uuid")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	var accessToken string
	if getZoomToken != nil && job.CreatorIdentity != nil && *job.CreatorIdentity != "" {
		accessToken, err = getZoomToken(ctx, *job.CreatorIdentity)
		if err != nil {
			setJobFailedPermanent(ctx, db, jobID, attempt, "zoom_auth", err.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
	} else {
		setJobFailedPermanent(ctx, db, jobID, attempt, "zoom_auth", "creator_identity required for Zoom import")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	// --- Stage: fetch ---
	updateJobState(ctx, db, jobID, models.ProcessingStateFetching, models.ProcessingStageFetch, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateFetching)

	rec, transcriptFile, fetchErr := utils.GetMeetingRecordingsAndTranscript(accessToken, instanceUUID)
	if fetchErr != nil {
		return handleZoomError(ctx, db, job, attempt, fetchErr, updateMirror)
	}

	// --- Stage: download ---
	updateJobState(ctx, db, jobID, models.ProcessingStateDownloading, models.ProcessingStageDownload, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateDownloading)

	content, downErr := utils.DownloadTranscript(transcriptFile.DownloadURL, accessToken)
	if downErr != nil {
		return handleZoomError(ctx, db, job, attempt, downErr, updateMirror)
	}

	// --- Stage: parse ---
	updateJobState(ctx, db, jobID, models.ProcessingStateParsing, models.ProcessingStageParse, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateParsing)

	vttContent := string(content)
	parsed, parseErr := transcript.ParseVTT(bytes.NewReader(content))
	if parseErr != nil {
		msg := parseErr.Error()
		setJobFailedPermanent(ctx, db, jobID, attempt, "vtt_parse_error", msg)
		updateMirror(models.ProcessingStateFailedPermanent)
		log.Printf("processing job error: session_id=%s job_id=%s stage=parse error_code=vtt_parse_error error=%v", sessionID, jobID, parseErr)
		return nil
	}

	// Idempotent: create or get transcript
	transcriptRow := &models.Transcript{
		SessionID: sessionID,
		Source:    "zoom",
		Status:    models.TranscriptStatusParsing,
	}
	if err := db.CreateTranscript(ctx, transcriptRow); err != nil {
		// May already exist
		existing, _ := db.GetTranscriptBySessionID(ctx, sessionID, "zoom")
		if existing != nil {
			transcriptRow = existing
		} else {
			setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
	}

	segmentRows := make([]models.TranscriptSegmentRow, 0, len(parsed))
	videoSegments := make([]models.TranscriptSegment, 0, len(parsed))
	var rawParts []string
	for i, s := range parsed {
		segmentRows = append(segmentRows, models.TranscriptSegmentRow{
			TranscriptID: transcriptRow.ID,
			SessionID:    sessionID,
			Idx:          i,
			StartMs:      s.StartMs,
			EndMs:        s.EndMs,
			Text:         s.Text,
			SpeakerLabel: s.SpeakerLabel,
			SourceRef:    s.SourceRef,
		})
		videoSegments = append(videoSegments, models.TranscriptSegment{
			StartTime: float64(s.StartMs) / 1000,
			EndTime:   float64(s.EndMs) / 1000,
			Text:      s.Text,
		})
		rawParts = append(rawParts, s.Text)
	}
	rawText := strings.Join(rawParts, "\n")

	_ = db.DeleteSegmentsByTranscriptID(ctx, transcriptRow.ID)
	if err := db.InsertTranscriptSegments(ctx, transcriptRow.ID, sessionID, segmentRows); err != nil {
		msg := err.Error()
		setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", msg)
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	_ = db.UpdateTranscriptStatus(ctx, transcriptRow.ID, models.TranscriptStatusReady, &rawText, nil)

	// Idempotent: ensure artifact + video source exist
	artifacts, _ := db.GetArtifactsBySessionID(ctx, sessionID)
	var artifactID uuid.UUID
	if len(artifacts) > 0 {
		artifactID = artifacts[0].ID
	} else {
		title := rec.Topic
		if title == "" {
			title = "Zoom Recording"
		}
		artifact, createErr := db.CreateArtifact(ctx, sessionID, title, nil)
		if createErr != nil {
			setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", createErr.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
		artifactID = artifact.ID
	}

	sources, _ := db.GetVideoSourcesBySessionID(ctx, sessionID)
	if len(sources) == 0 {
		zoomURL := "https://zoom.us/recording/detail?meeting_id=" + url.QueryEscape(instanceUUID)
		videoID := uuid.New()
		zoomAPI := "zoom_api"
		vs := &models.VideoSource{
			ID:                  videoID,
			ArtifactID:          artifactID,
			SessionID:           sessionID,
			Provider:            "zoom",
			VideoURL:            zoomURL,
			PlaybackMode:        "embed",
			OriginalURL:         &zoomURL,
			TranscriptStatus:    models.VideoTranscriptStatusReady,
			TranscriptionSource: &zoomAPI,
			SourceType:          models.VideoSourceTypeEmbedURL,
		}
		if err := db.CreateVideoSource(ctx, vs); err != nil {
			setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
		_ = db.UpdateVideoSourceZoomTranscript(ctx, videoID, rawText, &vttContent, videoSegments)
	}

	// --- Stage: chunk + embed ---
	updateJobState(ctx, db, jobID, models.ProcessingStateChunking, models.ProcessingStageChunk, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateChunking)
	updateJobState(ctx, db, jobID, models.ProcessingStateEmbedding, models.ProcessingStageEmbed, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateEmbedding)

	embedder := &rag.OpenAIEmbedder{}
	if indexErr := rag.IndexSession(ctx, db, embedder, sessionID); indexErr != nil {
		log.Printf("processing job error: session_id=%s job_id=%s stage=embed error=%v", sessionID, jobID, indexErr)
		// Transient (e.g. OpenAI rate limit) vs permanent
		msg := indexErr.Error()
		if strings.Contains(strings.ToLower(msg), "rate") || strings.Contains(msg, "429") {
			next := time.Now().Add(BackoffTransient(attempt))
			setJobFailedTransient(ctx, db, jobID, attempt, "index_error", msg, next)
			updateMirror(models.ProcessingStateFailedTransient)
		} else {
			setJobFailedPermanent(ctx, db, jobID, attempt, "index_error", msg)
			updateMirror(models.ProcessingStateFailedPermanent)
		}
		return nil
	}

	// --- Ready ---
	updateJobState(ctx, db, jobID, models.ProcessingStateReady, models.ProcessingStageReady, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateReady)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
	log.Printf("processing job completed: session_id=%s job_id=%s state=ready stage=ready duration_ok", sessionID, jobID)
	return nil
}

func meetingUUIDForJob(job *models.SessionProcessingJob) string {
	if job.InstanceUUID != nil && *job.InstanceUUID != "" {
		return *job.InstanceUUID
	}
	if job.MeetingUUID != nil {
		return *job.MeetingUUID
	}
	return ""
}

func updateJobState(ctx context.Context, db *database.DB, jobID uuid.UUID, state, stage string, attempt int, nextRetryAt *time.Time, code, msg *string) {
	_ = db.UpdateSessionProcessingJobState(ctx, jobID, state, stage, attempt, nextRetryAt, code, msg)
}

func setJobFailedTransient(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string, nextRetryAt time.Time) {
	updateJobState(ctx, db, jobID, models.ProcessingStateFailedTransient, "fetch", attempt, &nextRetryAt, &code, &msg)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func setJobFailedPermanent(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string) {
	updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "fetch", attempt, nil, &code, &msg)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func setJobWaiting(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string, nextRetryAt time.Time) {
	updateJobState(ctx, db, jobID, models.ProcessingStateWaiting, "download", attempt, &nextRetryAt, &code, &msg)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func handleZoomError(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, attempt int, err error, updateMirror func(string)) error {
	jobID := job.ID
	ze, ok := utils.IsZoomAPIError(err)
	if !ok {
		// Network or other: treat as transient
		next := time.Now().Add(BackoffTransient(attempt))
		setJobFailedTransient(ctx, db, jobID, attempt, "network_error", err.Error(), next)
		updateMirror(models.ProcessingStateFailedTransient)
		return nil
	}
	if ze.NotReady {
		next := time.Now().Add(BackoffWaiting(attempt))
		code := ze.Code
		if code == "" {
			code = "transcript_not_ready"
		}
		setJobWaiting(ctx, db, jobID, attempt, code, ze.Message, next)
		updateMirror(models.ProcessingStateWaiting)
		return nil
	}
	if ze.Retryable() {
		next := time.Now().Add(BackoffTransient(attempt))
		setJobFailedTransient(ctx, db, jobID, attempt, ze.Code, ze.Message, next)
		updateMirror(models.ProcessingStateFailedTransient)
		return nil
	}
	if ze.Permanent() {
		setJobFailedPermanent(ctx, db, jobID, attempt, ze.Code, ze.Message)
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	// Default: transient
	next := time.Now().Add(BackoffTransient(attempt))
	setJobFailedTransient(ctx, db, jobID, attempt, ze.Code, ze.Message, next)
	updateMirror(models.ProcessingStateFailedTransient)
	return nil
}
