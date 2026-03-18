package utils

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/urlextract"
)

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}

// OnTranscriptCompletedFunc is called when a transcript job completes successfully (optional, set by caller to e.g. trigger session reindex + broadcast).
type OnTranscriptCompletedFunc func(sessionID uuid.UUID)

// OnSessionUpdatedFunc is called when session state changes and clients should refresh (e.g. material extraction failed); optional, typically broadcast only.
type OnSessionUpdatedFunc func(sessionID uuid.UUID)

// JobProcessor handles background processing of transcription jobs and material extraction jobs
type JobProcessor struct {
	db                   *database.DB
	service              *TranscriptionService
	queue                chan *models.TranscriptJob
	workers              int
	wg                   sync.WaitGroup
	running              bool
	mu                   sync.RWMutex
	OnTranscriptCompleted OnTranscriptCompletedFunc // Optional: called when a transcript/material extraction job completes (e.g. trigger session reindex + broadcast)
	OnSessionUpdated      OnSessionUpdatedFunc      // Optional: called when session should refresh without reindex (e.g. material extraction failed)
}

// NewJobProcessor creates a new job processor
func NewJobProcessor(db *database.DB, workers int) *JobProcessor {
	if workers <= 0 {
		workers = 2 // Default to 2 workers
	}

	return &JobProcessor{
		db:      db,
		service: NewTranscriptionService(),
		queue:   make(chan *models.TranscriptJob, 100), // Buffer up to 100 jobs
		workers: workers,
		running: false,
	}
}

// Start starts the job processor workers
func (jp *JobProcessor) Start(ctx context.Context) {
	jp.mu.Lock()
	if jp.running {
		jp.mu.Unlock()
		return
	}
	jp.running = true
	jp.mu.Unlock()

	for i := 0; i < jp.workers; i++ {
		jp.wg.Add(1)
		go jp.worker(ctx, i)
	}

	log.Printf("Job processor started with %d workers", jp.workers)

	// Startup recovery: re-enqueue queued or stale processing jobs so they are not stranded after a restart
	go func() {
		recoverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		jobs, err := jp.db.GetQueuedOrStaleTranscriptJobs(recoverCtx, 1*time.Hour)
		if err != nil {
			log.Printf("Job processor startup recovery: failed to load queued/stale jobs: %v", err)
			return
		}
		for _, job := range jobs {
			if enqErr := jp.Enqueue(recoverCtx, job); enqErr != nil {
				log.Printf("Job processor startup recovery: failed to enqueue job %s: %v", job.ID, enqErr)
			}
		}
		if len(jobs) > 0 {
			log.Printf("Job processor startup recovery: re-enqueued %d job(s)", len(jobs))
		}
	}()
}

// Stop stops the job processor
func (jp *JobProcessor) Stop() {
	jp.mu.Lock()
	if !jp.running {
		jp.mu.Unlock()
		return
	}
	jp.running = false
	jp.mu.Unlock()

	close(jp.queue)
	jp.wg.Wait()
	log.Println("Job processor stopped")
}

// Enqueue adds a job to the processing queue
func (jp *JobProcessor) Enqueue(ctx context.Context, job *models.TranscriptJob) error {
	jp.mu.RLock()
	running := jp.running
	jp.mu.RUnlock()

	if !running {
		return fmt.Errorf("job processor is not running")
	}

	select {
	case jp.queue <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("job queue is full")
	}
}

// worker processes jobs from the queue
func (jp *JobProcessor) worker(ctx context.Context, workerID int) {
	defer jp.wg.Done()

	log.Printf("Worker %d started", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %d stopping due to context cancellation", workerID)
			return
		case job, ok := <-jp.queue:
			if !ok {
				log.Printf("Worker %d stopping (queue closed)", workerID)
				return
			}
			jp.processJob(ctx, job, workerID)
		}
	}
}

// processMaterialExtractionJob runs PDF/Office text extraction for a material; job has MaterialID set and no VideoSourceID.
func (jp *JobProcessor) processMaterialExtractionJob(ctx context.Context, job *models.TranscriptJob, workerID int) {
	materialID := *job.MaterialID
	log.Printf("Worker %d processing material extraction job %s for material %s", workerID, job.ID, materialID)

	// Idempotent: if material is already ready, mark job completed and return
	mat, err := jp.db.GetMaterialByID(ctx, materialID)
	if err != nil || mat == nil {
		errMsg := "material not found"
		if err != nil {
			errMsg = err.Error()
		}
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, stringPtr(errMsg))
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	if mat.TextStatus == models.MaterialTextStatusReady {
		_ = jp.db.CompleteTranscriptJob(ctx, job.ID, nil, nil, nil)
		log.Printf("Worker %d skipped material %s (already ready)", workerID, materialID)
		return
	}

	if err := jp.db.UpdateTranscriptJobStarted(ctx, job.ID); err != nil {
		log.Printf("Failed to update material extraction job status: %v", err)
		jp.db.FailTranscriptJob(ctx, job.ID, fmt.Sprintf("Failed to start job: %v", err))
		jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, stringPtr(fmt.Sprintf("Failed to start job: %v", err)))
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	if err := jp.db.UpdateMaterialTextStatus(ctx, materialID, models.MaterialTextStatusProcessing, nil); err != nil {
		log.Printf("Warning: Failed to set material processing: %v", err)
	}

	var filePath string
	var cleanup func()
	if strings.HasPrefix(job.SourceURL, "sessions/") || strings.HasPrefix(job.SourceURL, "./sessions/") ||
		strings.HasPrefix(job.SourceURL, "data/") || strings.HasPrefix(job.SourceURL, "./data/") {
		rel := strings.TrimPrefix(job.SourceURL, "./")
		filePath = filepath.Join(storage.UploadRoot(), rel)
		if _, err := os.Stat(filePath); err != nil {
			errMsg := fmt.Sprintf("Local file not found: %v", err)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, &errMsg)
			if jp.OnSessionUpdated != nil {
				jp.OnSessionUpdated(job.SessionID)
			}
			return
		}
		cleanup = func() {}
	} else if strings.HasPrefix(job.SourceURL, "http://") || strings.HasPrefix(job.SourceURL, "https://") {
		tempFile, c, downloadErr := jp.service.DownloadMedia(ctx, job.SourceURL, job.ID.String())
		if downloadErr != nil {
			errMsg := fmt.Sprintf("Failed to download file: %v", downloadErr)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, &errMsg)
			if jp.OnSessionUpdated != nil {
				jp.OnSessionUpdated(job.SessionID)
			}
			return
		}
		filePath = tempFile
		cleanup = c
		defer cleanup()
	} else {
		errMsg := "Unsupported source URL for material extraction"
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, &errMsg)
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}

	text, err := ExtractTextFromFile(filePath)
	if err != nil {
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:497] + "..."
		}
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, &errMsg)
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	if strings.TrimSpace(text) == "" {
		errMsg := "Extraction produced empty text"
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, &errMsg)
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}

	if err := jp.db.UpdateMaterialTextStatus(ctx, materialID, models.MaterialTextStatusReady, &text); err != nil {
		log.Printf("Failed to save extracted text: %v", err)
		errMsg := fmt.Sprintf("Failed to save: %v", err)
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		jp.db.UpdateMaterialTextStatusWithError(ctx, materialID, models.MaterialTextStatusFailed, nil, &errMsg)
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	if err := jp.db.CompleteTranscriptJob(ctx, job.ID, nil, nil, nil); err != nil {
		log.Printf("Failed to complete material extraction job: %v", err)
		return
	}
	if jp.OnTranscriptCompleted != nil {
		jp.OnTranscriptCompleted(job.SessionID)
	}
	log.Printf("Worker %d completed material extraction job %s", workerID, job.ID)
}

// processLinkExtractionJob runs URL fetch+extract for a session link; job has SessionLinkID set.
func (jp *JobProcessor) processLinkExtractionJob(ctx context.Context, job *models.TranscriptJob, workerID int) {
	linkID := *job.SessionLinkID
	log.Printf("Worker %d processing link extraction job %s for link %s", workerID, job.ID, linkID)

	link, err := jp.db.GetSessionLinkByID(ctx, linkID)
	if err != nil || link == nil {
		errMsg := "session link not found"
		if err != nil {
			errMsg = err.Error()
		}
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	if link.Status == models.SessionLinkStatusVerified {
		_ = jp.db.CompleteTranscriptJob(ctx, job.ID, nil, nil, nil)
		log.Printf("Worker %d skipped link %s (already verified)", workerID, linkID)
		return
	}

	if err := jp.db.UpdateTranscriptJobStarted(ctx, job.ID); err != nil {
		log.Printf("Failed to update link extraction job status: %v", err)
		jp.db.FailTranscriptJob(ctx, job.ID, fmt.Sprintf("Failed to start job: %v", err))
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	link.Status = models.SessionLinkStatusProcessing
	link.ErrorMessage = nil
	if err := jp.db.UpdateSessionLink(ctx, link); err != nil {
		log.Printf("Warning: Failed to set link processing: %v", err)
	}

	result, err := urlextract.FetchAndExtract(ctx, job.SourceURL, urlextract.DefaultMaxBytes)
	if err != nil {
		errMsg := err.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:497] + "..."
		}
		link.Status = models.SessionLinkStatusFailed
		link.ErrorMessage = &errMsg
		link.Title = nil
		link.ExtractedText = nil
		_ = jp.db.UpdateSessionLink(ctx, link)
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		log.Printf("Worker %d link extraction failed for %s: %v", workerID, linkID, err)
		return
	}

	link.Status = models.SessionLinkStatusVerified
	if result.Title != "" {
		link.Title = &result.Title
	} else {
		link.Title = nil
	}
	bodyTrim := strings.TrimSpace(result.Body)
	if bodyTrim != "" {
		link.ExtractedText = &bodyTrim
	} else {
		link.ExtractedText = nil
	}
	link.ErrorMessage = nil
	if err := jp.db.UpdateSessionLink(ctx, link); err != nil {
		log.Printf("Failed to save link result: %v", err)
		jp.db.FailTranscriptJob(ctx, job.ID, fmt.Sprintf("Failed to save: %v", err))
		if jp.OnSessionUpdated != nil {
			jp.OnSessionUpdated(job.SessionID)
		}
		return
	}
	if err := jp.db.CompleteTranscriptJob(ctx, job.ID, nil, nil, nil); err != nil {
		log.Printf("Failed to complete link extraction job: %v", err)
		return
	}
	if jp.OnTranscriptCompleted != nil {
		jp.OnTranscriptCompleted(job.SessionID)
	}
	log.Printf("Worker %d completed link extraction job %s", workerID, job.ID)
}

// processJob processes a single transcription job (or material extraction job when MaterialID is set, or link extraction when SessionLinkID is set).
func (jp *JobProcessor) processJob(ctx context.Context, job *models.TranscriptJob, workerID int) {
	if job.SessionLinkID != nil {
		jp.processLinkExtractionJob(ctx, job, workerID)
		return
	}
	isMaterialExtraction := job.MaterialID != nil && job.VideoSourceID == uuid.Nil
	if isMaterialExtraction {
		jp.processMaterialExtractionJob(ctx, job, workerID)
		return
	}

	isMaterialJob := job.MaterialID != nil && job.VideoSourceID == uuid.Nil
	if isMaterialJob {
		log.Printf("Worker %d processing job %s for material %s", workerID, job.ID, *job.MaterialID)
	} else {
		log.Printf("Worker %d processing job %s for video %s", workerID, job.ID, job.VideoSourceID)
	}

	// Update status to downloading/started
	if err := jp.db.UpdateTranscriptJobStarted(ctx, job.ID); err != nil {
		log.Printf("Failed to update job status: %v", err)
		jp.db.FailTranscriptJob(ctx, job.ID, fmt.Sprintf("Failed to start job: %v", err))
		if isMaterialJob {
			jp.db.UpdateMaterialTextStatusWithError(ctx, *job.MaterialID, models.MaterialTextStatusFailed, nil, stringPtr(fmt.Sprintf("Failed to start job: %v", err)))
		}
		return
	}

	// Update video source status to processing (skip for material jobs)
	if !isMaterialJob {
		if err := jp.db.UpdateVideoSourceTranscriptStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusProcessing); err != nil {
			log.Printf("Warning: Failed to update video source status to processing: %v", err)
		}
	}

	var tempFile string
	var cleanup func()

	// Local file path: sessions/{session_id}/... or legacy data/...
	if strings.HasPrefix(job.SourceURL, "sessions/") || strings.HasPrefix(job.SourceURL, "./sessions/") ||
		strings.HasPrefix(job.SourceURL, "data/") || strings.HasPrefix(job.SourceURL, "./data/") {
		// Local file - resolve relative path against upload root, then use directly
		rel := strings.TrimPrefix(job.SourceURL, "./")
		filePath := filepath.Join(storage.UploadRoot(), rel)
		if _, err := os.Stat(filePath); err != nil {
			log.Printf("Local file not found: %v", err)
			errMsg := fmt.Sprintf("Local file not found: %v", err)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			if isMaterialJob {
				jp.db.UpdateMaterialTextStatusWithError(ctx, *job.MaterialID, models.MaterialTextStatusFailed, nil, &errMsg)
			} else {
				jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			}
			return
		}
		tempFile = filePath
		cleanup = func() {}
		defer cleanup()
		log.Printf("Using local file: %s", tempFile)
	} else if strings.HasPrefix(job.SourceURL, "http://") || strings.HasPrefix(job.SourceURL, "https://") {
		// Direct HTTP(S) URL (e.g. R2 presigned GET) - download without Loom resolver
		jobIDStr := job.ID.String()
		var downloadErr error
		tempFile, cleanup, downloadErr = jp.service.DownloadMedia(ctx, job.SourceURL, jobIDStr)
		if downloadErr != nil {
			log.Printf("Failed to download media from URL: %v", downloadErr)
			errMsg := fmt.Sprintf("Failed to download media: %v", downloadErr)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			return
		}
		defer cleanup()
		log.Printf("Downloaded media from URL for transcription")
	} else {
		// Remote URL - resolve and download (Loom logic)
		resolver := NewLoomResolver()
		info, resolveErr := resolver.ResolveMedia(ctx, job.SourceURL, job.LoomPassword)
		if resolveErr != nil {
			log.Printf("Failed to resolve Loom URL: %v", resolveErr)
			errMsg := fmt.Sprintf("Failed to resolve Loom URL: %v", resolveErr)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			return
		}
		if info.MediaURL == "" {
			errMsg := "Unable to resolve downloadable media URL - video may be private"
			log.Printf("%s", errMsg)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			return
		}
		if strings.Contains(info.MediaURL, ".m3u8") || strings.Contains(info.MediaURL, "playlist") {
			errMsg := "Loom returned an HLS playlist (.m3u8) URL which Whisper API cannot process. This video may only be available in streaming format. Please upload the transcript manually, or contact Loom to request direct MP4 access for this video."
			log.Printf("%s (URL: %s)", errMsg, info.MediaURL)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			return
		}
		if err := jp.db.UpdateTranscriptJobProgress(ctx, job.ID, models.TranscriptJobStatusDownloading, &info.MediaURL); err != nil {
			log.Printf("Failed to update job progress: %v", err)
		}
		jobIDStr := job.ID.String()
		var downloadErr error
		tempFile, cleanup, downloadErr = jp.service.DownloadMedia(ctx, info.MediaURL, jobIDStr)
		if downloadErr != nil {
			log.Printf("Failed to download media: %v", downloadErr)
			errMsg := fmt.Sprintf("Failed to download media: %v", downloadErr)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			return
		}
		defer cleanup()
	}

	// Update status to transcribing
	if err := jp.db.UpdateTranscriptJobProgress(ctx, job.ID, models.TranscriptJobStatusTranscribing, nil); err != nil {
		log.Printf("Failed to update job status: %v", err)
	}

	// Transcribe using Whisper
	transcriber := NewWhisperTranscriber()
	if !transcriber.CanTranscribe() {
		errMsg := "OPENAI_API_KEY not set - cannot transcribe"
		log.Printf("%s", errMsg)
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		return
	}

	result, err := transcriber.TranscribeFile(ctx, tempFile, "", "verbose_json", []string{"segment"})
	if err != nil {
		log.Printf("Failed to transcribe: %v", err)
		errMsg := fmt.Sprintf("Failed to transcribe: %v", err)
		jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
		if isMaterialJob {
			jp.db.UpdateMaterialTextStatusWithError(ctx, *job.MaterialID, models.MaterialTextStatusFailed, nil, &errMsg)
		} else {
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
		}
		return
	}

	if err := jp.db.UpdateTranscriptJobProgress(ctx, job.ID, models.TranscriptJobStatusSaving, nil); err != nil {
		log.Printf("Failed to update job status: %v", err)
	}

	durationSeconds := int(result.Duration)
	modelStr := "whisper-1"
	if modelEnv := os.Getenv("WHISPER_MODEL"); modelEnv != "" {
		modelStr = modelEnv
	}

	if isMaterialJob {
		if err := jp.db.UpdateMaterialTextStatus(ctx, *job.MaterialID, models.MaterialTextStatusReady, &result.Text); err != nil {
			log.Printf("Failed to save material transcript: %v", err)
			errMsg := fmt.Sprintf("Failed to save transcript: %v", err)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateMaterialTextStatusWithError(ctx, *job.MaterialID, models.MaterialTextStatusFailed, nil, &errMsg)
			return
		}
	} else {
		if err := jp.db.UpdateVideoSourceTranscript(ctx, job.VideoSourceID, result.Text); err != nil {
			log.Printf("Failed to save transcript: %v", err)
			errMsg := fmt.Sprintf("Failed to save transcript: %v", err)
			jp.db.FailTranscriptJob(ctx, job.ID, errMsg)
			jp.db.UpdateVideoSourceIngestionStatus(ctx, job.VideoSourceID, models.VideoTranscriptStatusFailed, &errMsg)
			return
		}
		if err := jp.db.UpdateVideoSourceTranscriptionSource(ctx, job.VideoSourceID, "whisper"); err != nil {
			log.Printf("Warning: Failed to update transcription source: %v", err)
		}
		// Link transcript to the matching material so materials list shows it like PDF extracted text
		vs, errVS := jp.db.GetVideoSourceByID(ctx, job.VideoSourceID)
		if errVS == nil && vs != nil && vs.StoredVideoObjectKey != nil && *vs.StoredVideoObjectKey != "" {
			mat, errMat := jp.db.GetMaterialBySessionAndStorage(ctx, job.SessionID, *vs.StoredVideoObjectKey)
			if errMat == nil && mat != nil {
				if errUp := jp.db.UpdateMaterialTextStatus(ctx, mat.ID, models.MaterialTextStatusReady, &result.Text); errUp != nil {
					log.Printf("Warning: Failed to update material transcript for video: %v", errUp)
				}
			}
		}
	}

	if err := jp.db.CompleteTranscriptJob(ctx, job.ID, &modelStr, &result.Language, &durationSeconds); err != nil {
		log.Printf("Failed to complete job: %v", err)
		return
	}
	if jp.OnTranscriptCompleted != nil {
		jp.OnTranscriptCompleted(job.SessionID)
	}
	log.Printf("Worker %d completed job %s", workerID, job.ID)
}

// ProcessTranscriptJob is a convenience function to process a transcript job
// This should be called from handlers to enqueue a job
// password is optional and used for password-protected Loom videos
func ProcessTranscriptJob(ctx context.Context, db *database.DB, videoSourceID uuid.UUID, sessionID uuid.UUID, loomURL string, password *string, processor *JobProcessor) error {
	// Check for existing job with same key (idempotency)
	jobKey := GenerateJobKey(videoSourceID.String(), loomURL)
	existingJob, err := db.GetTranscriptJobByKey(ctx, jobKey)
	if err != nil && err.Error() != "failed to get transcript job by key: no rows in result set" {
		return fmt.Errorf("failed to check for existing job: %w", err)
	}

	// If job exists and is not failed, don't create duplicate
	if existingJob != nil {
		if existingJob.Status == models.TranscriptJobStatusCompleted {
			log.Printf("Job already completed for video %s, URL %s", videoSourceID, loomURL)
			return nil
		}
		if existingJob.Status != models.TranscriptJobStatusFailed {
			log.Printf("Job already in progress for video %s, URL %s (status: %s)", videoSourceID, loomURL, existingJob.Status)
			return nil
		}
	}

	// Create new job
	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSourceID,
		SessionID:     sessionID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     loomURL,
		JobKey:        jobKey,
		QueuedAt:      time.Now(),
		LoomPassword:  password,
	}

	if err := db.CreateTranscriptJob(ctx, job); err != nil {
		return fmt.Errorf("failed to create transcript job: %w", err)
	}

	// Link job to video source
	if err := db.UpdateVideoSourceTranscriptionJob(ctx, videoSourceID, &job.ID); err != nil {
		log.Printf("Warning: Failed to link job to video source: %v", err)
	}

	// Enable auto-transcribe flag on video source
	// Note: This requires a database method to update auto_transcribe_enabled
	// For now, we'll skip this and handle it later

	// Enqueue job for processing
	if err := processor.Enqueue(ctx, job); err != nil {
		log.Printf("Failed to enqueue job: %v", err)
		// Still return success - job is created and can be processed later
	}

	return nil
}
