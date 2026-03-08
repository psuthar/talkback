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
)

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}

// OnTranscriptCompletedFunc is called when a transcript job completes successfully (optional, set by caller to e.g. trigger session reindex).
type OnTranscriptCompletedFunc func(sessionID uuid.UUID)

// JobProcessor handles background processing of transcription jobs
type JobProcessor struct {
	db                   *database.DB
	service              *TranscriptionService
	queue                chan *models.TranscriptJob
	workers              int
	wg                   sync.WaitGroup
	running              bool
	mu                   sync.RWMutex
	OnTranscriptCompleted OnTranscriptCompletedFunc // Optional: called when a transcript job completes (e.g. trigger session reindex)
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

// processJob processes a single transcription job
func (jp *JobProcessor) processJob(ctx context.Context, job *models.TranscriptJob, workerID int) {
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
	} else {
		// Remote URL - resolve and download (Loom logic; material jobs use local path above)
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
