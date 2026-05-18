package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/googlemeet"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// runGoogleMeetJob runs the Google Meet ingestion pipeline for one session_processing_job.
// meeting_uuid = full conferenceRecord resource name; instance_uuid = full recording resource name.
//
// Stages: fetch (recording state + driveDestination) → download (Drive MP4) → upload
// (R2 or local) → transcript fetch (ListTranscripts) → parse (entries → segments) →
// index → ready. Transcript fallback rules (SCRUM-399 follow-up to SCRUM-380):
// when ListTranscripts returns an empty list the pipeline immediately enqueues
// a Whisper fallback — empirically Meet creates a transcript entity as soon as
// transcription is enabled, so an empty list after the MP4 is FILE_GENERATED
// means transcription was never enabled or the account tier does not surface
// transcripts (e.g. Workspace Individual on a personal Gmail). When every
// listed transcript is in a terminal failure state the same fallback fires.
// Otherwise — there's a transcript entity that's still STARTED/ENDED — keep
// polling: Meet is actively preparing one and we prefer the native transcript
// over a Whisper re-transcription.
func runGoogleMeetJob(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getToken GoogleMeetTokenFunc, store storage.Interface, storagePrefix string, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) error {
	sessionID := job.SessionID
	jobID := job.ID
	attempt := job.AttemptCount + 1

	updateMirror := func(state string) {
		_ = db.UpdateSessionProcessingMirror(ctx, sessionID, state)
	}

	conferenceRecord := ""
	recordingName := ""
	if job.MeetingUUID != nil {
		conferenceRecord = strings.TrimSpace(*job.MeetingUUID)
	}
	if job.InstanceUUID != nil {
		recordingName = strings.TrimSpace(*job.InstanceUUID)
	}
	if conferenceRecord == "" || recordingName == "" {
		setJobFailedPermanent(ctx, db, jobID, attempt, "google_meet_missing_ids", "missing conference_record or recording on job")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	if getToken == nil || job.CreatorIdentity == nil || *job.CreatorIdentity == "" {
		setJobFailedPermanent(ctx, db, jobID, attempt, "google_meet_auth", "creator_identity required for Google Meet import")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	accessToken, tokErr := getToken(ctx, *job.CreatorIdentity)
	if tokErr != nil {
		setJobFailedPermanent(ctx, db, jobID, attempt, "google_meet_auth", tokErr.Error())
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	httpClient := googlemeet.DefaultHTTPClient()

	// --- Stage: fetch ---
	updateJobState(ctx, db, jobID, models.ProcessingStateFetching, models.ProcessingStageFetch, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateFetching)

	rec, err := googlemeet.GetRecording(ctx, accessToken, recordingName, httpClient)
	if err != nil {
		log.Printf("[meet] session_id=%s job_id=%s fetch_recording_error: %v", sessionID, jobID, err)
		if googlemeet.IsPermanent(err) {
			setJobFailedPermanent(ctx, db, jobID, attempt, "google_meet_fetch", err.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
		next := time.Now().Add(BackoffTransient(attempt))
		setJobFailedTransient(ctx, db, jobID, attempt, "google_meet_fetch", err.Error(), next)
		updateMirror(models.ProcessingStateFailedTransient)
		return nil
	}
	if !googlemeet.IsRecordingReady(rec.State) {
		next := time.Now().Add(googlemeet.BackoffWaitingFromState(rec.State))
		setJobWaiting(ctx, db, jobID, attempt, "google_meet_recording_not_ready", fmt.Sprintf("Recording state is %s; waiting for FILE_GENERATED.", rec.State), next)
		updateMirror(models.ProcessingStateWaiting)
		return nil
	}
	if rec.DriveDestination == nil || strings.TrimSpace(rec.DriveDestination.File) == "" {
		setJobFailedPermanent(ctx, db, jobID, attempt, "google_meet_no_drive_file", "Recording is FILE_GENERATED but has no driveDestination.file")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	driveFileID := rec.DriveDestination.File
	exportURI := rec.DriveDestination.ExportURI

	// --- Stage: download + upload — SCRUM-467 per-recording dedupe ---
	// Skip ONLY when this specific (provider, conferenceRecord,
	// recordingName) already has a ready video_source on this session.
	doMp4Ingest := true
	if shouldSkipMP4Ingest(ctx, db, sessionID, "google_meet", &conferenceRecord, &recordingName) {
		doMp4Ingest = false
		log.Printf("google_meet_ingest_skip session_id=%s conference_record=%q recording=%q reason=already_ingested_this_recording", sessionID, conferenceRecord, recordingName)
	}

	if doMp4Ingest {
		updateJobState(ctx, db, jobID, models.ProcessingStateDownloading, models.ProcessingStageDownload, attempt, nil, nil, nil)
		updateMirror(models.ProcessingStateDownloading)

		resp, downErr := googlemeet.DownloadDriveFile(ctx, accessToken, driveFileID, httpClient)
		if downErr != nil {
			next := time.Now().Add(BackoffTransient(attempt))
			setJobFailedTransient(ctx, db, jobID, attempt, "google_meet_drive_download", downErr.Error(), next)
			updateMirror(models.ProcessingStateFailedTransient)
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			next := time.Now().Add(BackoffTransient(attempt))
			setJobFailedTransient(ctx, db, jobID, attempt, "google_meet_drive_download", fmt.Sprintf("HTTP %d", resp.StatusCode), next)
			updateMirror(models.ProcessingStateFailedTransient)
			return nil
		}
		if resp.ContentLength > 0 && resp.ContentLength > maxUploadBytesVideo() {
			setJobFailedPermanent(ctx, db, jobID, attempt, "google_meet_file_too_large", fmt.Sprintf("Recording size (%d bytes) exceeds limit (%d bytes).", resp.ContentLength, maxUploadBytesVideo()))
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}

		mp4Name := "google-meet.mp4"
		artifactID := uuid.New()
		meta := googleMeetArtifactMetadata(conferenceRecord, recordingName, driveFileID, exportURI)

		if store != nil {
			storageKey := storage.BuildArtifactStorageKey(storagePrefix, sessionID, artifactID, mp4Name)
			bucket := os.Getenv("R2_BUCKET")
			if bucket == "" {
				bucket = "talkback-r2-bucket"
			}
			fa := &models.FileArtifact{
				ID:              artifactID,
				SessionID:       &sessionID,
				Kind:            models.FileArtifactKindVideo,
				Filename:        &mp4Name,
				ContentType:     "video/mp4",
				StorageProvider: "r2",
				StorageBucket:   bucket,
				StorageKey:      storageKey,
				Status:          models.FileArtifactStatusPending,
				MetadataJSON:    meta,
			}
			if err := db.CreateFileArtifact(ctx, fa); err != nil {
				setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			etag, _, putErr := store.Put(ctx, storageKey, resp.Body, "video/mp4", resp.ContentLength)
			if putErr != nil {
				_ = db.UpdateFileArtifactToFailed(ctx, artifactID, "r2_put: "+putErr.Error())
				setJobFailedPermanent(ctx, db, jobID, attempt, "r2_upload", putErr.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			exists, size, contentType, headErr := store.Head(ctx, storageKey)
			if headErr != nil || !exists || size <= 0 {
				_ = db.UpdateFileArtifactToFailed(ctx, artifactID, "r2_head: object missing or empty after put")
				setJobFailedPermanent(ctx, db, jobID, attempt, "r2_upload", "object missing or empty after upload")
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			ct := "video/mp4"
			if contentType != "" {
				ct = contentType
			}
			if err := db.UpdateFileArtifactToReadyWithMetadata(ctx, artifactID, size, ct, mergeEtagIntoMetadata(meta, etag)); err != nil {
				log.Printf("[meet] update artifact ready: %v", err)
			}
			if err := setPrimaryIfNotSet(ctx, db, sessionID, artifactID); err != nil {
				log.Printf("[meet] set primary video: %v", err)
			}
		} else {
			localRelKey := filepath.Join(storage.SessionStorageRoot, sessionID.String(), "videos", mp4Name)
			uploadRoot := storage.UploadRoot()
			dir := filepath.Join(uploadRoot, storage.SessionStorageRoot, sessionID.String(), "videos")
			if err := os.MkdirAll(dir, 0755); err != nil {
				setJobFailedPermanent(ctx, db, jobID, attempt, "local_mkdir", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			absPath := filepath.Join(uploadRoot, localRelKey)
			f, err := os.Create(absPath)
			if err != nil {
				setJobFailedPermanent(ctx, db, jobID, attempt, "local_create", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			size, err := io.Copy(f, resp.Body)
			_ = f.Close()
			if err != nil {
				os.Remove(absPath)
				setJobFailedPermanent(ctx, db, jobID, attempt, "local_write", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			fa := &models.FileArtifact{
				ID:              artifactID,
				SessionID:       &sessionID,
				Kind:            models.FileArtifactKindVideo,
				Filename:        &mp4Name,
				ContentType:     "video/mp4",
				StorageProvider: "local",
				StorageBucket:   "local",
				StorageKey:      localRelKey,
				Status:          models.FileArtifactStatusPending,
				MetadataJSON:    meta,
			}
			if err := db.CreateFileArtifact(ctx, fa); err != nil {
				os.Remove(absPath)
				setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			if err := db.UpdateFileArtifactToReady(ctx, artifactID, size, "video/mp4"); err != nil {
				log.Printf("[meet] update artifact ready: %v", err)
			}
			if err := setPrimaryIfNotSet(ctx, db, sessionID, artifactID); err != nil {
				log.Printf("[meet] set primary video: %v", err)
			}
		}
	}

	// --- Stage: transcript fetch ---
	updateJobState(ctx, db, jobID, models.ProcessingStateParsing, models.ProcessingStageParse, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateParsing)

	transcripts, err := googlemeet.ListTranscripts(ctx, accessToken, conferenceRecord, httpClient)
	if err != nil {
		next := time.Now().Add(BackoffTransient(attempt))
		setJobFailedTransient(ctx, db, jobID, attempt, "google_meet_transcripts_list", err.Error(), next)
		updateMirror(models.ProcessingStateFailedTransient)
		return nil
	}
	var readyTranscript *googlemeet.Transcript
	for i := range transcripts {
		if googlemeet.IsRecordingReady(transcripts[i].State) {
			readyTranscript = &transcripts[i]
			break
		}
	}
	if readyTranscript == nil {
		// SCRUM-415: distinguish "transcript will never come" from "transcript
		// is being generated". shouldEnqueueMeetWhisperFallback returns true
		// only when the list is empty OR every transcript is in a terminal-
		// failure state — those cases get an immediate Whisper fallback. The
		// non-terminal branch (Gemini still preparing the transcript) enters
		// waiting_native_transcript with the SCRUM-415 poll cadence; once the
		// recording exceeds MEET_TRANSCRIPT_POLL_MAX_AGE we give up and fall
		// back to Whisper instead.
		if shouldEnqueueMeetWhisperFallback(transcripts) {
			if enqueueGoogleMeetWhisperFallback(ctx, db, sessionID, jobID, attempt, store, jobProcessor, exportURI, updateMirror) {
				return nil
			}
			// Fallback wanted but couldn't be enqueued (no processor, no primary
			// artifact, R2 store missing for r2 artifact, etc.). Fall through to
			// waiting so the next poll retries — this matches Teams' degraded path.
		} else {
			// Transcripts present, all non-terminal → Gemini still generating.
			recEnd, _ := time.Parse(time.RFC3339, rec.EndTime)
			if nativeTranscriptPollExpired(recEnd, time.Now()) {
				// Max-age exhausted — fall back to Whisper to unblock the user.
				if enqueueGoogleMeetWhisperFallback(ctx, db, sessionID, jobID, attempt, store, jobProcessor, exportURI, updateMirror) {
					return nil
				}
				// Fallback unavailable; fall through to waiting (will retry).
			} else {
				next := time.Now().Add(nativeTranscriptPollInterval())
				updateJobState(ctx, db, jobID, models.ProcessingStateWaitingNativeTranscript, models.ProcessingStageParse, attempt, &next, nil, nil)
				_ = db.UnlockSessionProcessingJob(ctx, jobID)
				updateMirror(models.ProcessingStateWaitingNativeTranscript)
				return nil
			}
		}
		next := time.Now().Add(BackoffWaiting(attempt))
		setJobWaiting(ctx, db, jobID, attempt, "google_meet_transcript_not_ready", "No FILE_GENERATED transcript yet; will check again later.", next)
		updateMirror(models.ProcessingStateWaiting)
		return nil
	}

	entries, err := googlemeet.ListTranscriptEntries(ctx, accessToken, readyTranscript.Name, httpClient)
	if err != nil {
		next := time.Now().Add(BackoffTransient(attempt))
		setJobFailedTransient(ctx, db, jobID, attempt, "google_meet_entries_list", err.Error(), next)
		updateMirror(models.ProcessingStateFailedTransient)
		return nil
	}

	// Convert RFC3339 entry timestamps to ms relative to recording.startTime.
	recStart, _ := time.Parse(time.RFC3339, rec.StartTime)
	transcriptRow := &models.Transcript{SessionID: sessionID, Source: "google_meet", Status: models.TranscriptStatusParsing}
	if err := db.CreateTranscript(ctx, transcriptRow); err != nil {
		if existing, _ := db.GetTranscriptBySessionID(ctx, sessionID, "google_meet"); existing != nil {
			transcriptRow = existing
		} else {
			setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
	}
	segmentRows := make([]models.TranscriptSegmentRow, 0, len(entries))
	videoSegments := make([]models.TranscriptSegment, 0, len(entries))
	var rawParts []string
	for i, e := range entries {
		if strings.TrimSpace(e.Text) == "" {
			continue
		}
		startMs := offsetMs(recStart, e.StartTime)
		endMs := offsetMs(recStart, e.EndTime)
		segmentRows = append(segmentRows, models.TranscriptSegmentRow{
			TranscriptID: transcriptRow.ID,
			SessionID:    sessionID,
			Idx:          i,
			StartMs:      startMs,
			EndMs:        endMs,
			Text:         e.Text,
		})
		videoSegments = append(videoSegments, models.TranscriptSegment{
			StartTime: float64(startMs) / 1000,
			EndTime:   float64(endMs) / 1000,
			Text:      e.Text,
		})
		rawParts = append(rawParts, e.Text)
	}
	rawText := strings.Join(rawParts, "\n")
	_ = db.DeleteSegmentsByTranscriptID(ctx, transcriptRow.ID)
	if err := db.InsertTranscriptSegments(ctx, transcriptRow.ID, sessionID, segmentRows); err != nil {
		setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	_ = db.UpdateTranscriptStatus(ctx, transcriptRow.ID, models.TranscriptStatusReady, &rawText, nil)

	// Ensure artifact + video_source rows exist (idempotent).
	artifacts, _ := db.GetArtifactsBySessionID(ctx, sessionID)
	var artifactID uuid.UUID
	if len(artifacts) > 0 {
		artifactID = artifacts[0].ID
	} else {
		title := "Google Meet Recording"
		if s, _ := db.GetSession(ctx, sessionID); s != nil {
			title = s.Title
		}
		artifact, createErr := db.CreateArtifact(ctx, sessionID, title, nil)
		if createErr != nil {
			setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", createErr.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
		artifactID = artifact.ID
	}
	// SCRUM-470: always create a new video_source per Meet import.
	videoID := uuid.New()
	ts := "google_meet_api"
	playbackURL := exportURI
	if playbackURL == "" {
		playbackURL = "https://meet.google.com/"
	}
	vs := &models.VideoSource{
		ID:                  videoID,
		ArtifactID:          artifactID,
		SessionID:           sessionID,
		Provider:            "google_meet",
		VideoURL:            playbackURL,
		PlaybackMode:        "embed",
		OriginalURL:         &playbackURL,
		TranscriptStatus:    models.VideoTranscriptStatusReady,
		TranscriptionSource: &ts,
		SourceType:          models.VideoSourceTypeEmbedURL,
	}
	if err := db.CreateVideoSource(ctx, vs); err != nil {
		setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	_ = db.UpdateVideoSourceZoomTranscript(ctx, videoID, rawText, nil, videoSegments)

	// --- Stage: index ---
	updateJobState(ctx, db, jobID, models.ProcessingStateChunking, models.ProcessingStageChunk, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateChunking)
	updateJobState(ctx, db, jobID, models.ProcessingStateEmbedding, models.ProcessingStageEmbed, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateEmbedding)

	embedder := &rag.OpenAIEmbedder{}
	if indexErr := rag.IndexSession(ctx, db, embedder, sessionID, store); indexErr != nil {
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

	// --- Stage: ready ---
	updateJobState(ctx, db, jobID, models.ProcessingStateReady, models.ProcessingStageReady, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateReady)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
	if onJobReady != nil {
		onJobReady(sessionID)
	}
	log.Printf("[meet] processing job completed: session_id=%s job_id=%s entries=%d", sessionID, jobID, len(entries))
	return nil
}

// offsetMs returns the millisecond offset from base to the RFC3339 timestamp ts.
// If ts is empty or unparseable, returns 0 so the entry doesn't break the segment list.
func offsetMs(base time.Time, ts string) int {
	if ts == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0
	}
	d := t.Sub(base)
	if d < 0 {
		return 0
	}
	return int(d.Milliseconds())
}

// shouldEnqueueMeetWhisperFallback decides whether to abandon waiting on a
// Meet-native transcript and Whisper-transcribe the already-downloaded MP4.
// Pure for unit testing. Triggers on either:
//  1. transcripts list is empty — Meet has not created a transcript entity
//     for this conference record. Since the MP4 reached FILE_GENERATED before
//     this stage runs, an empty list means transcription was never enabled at
//     meeting time or the account tier does not expose transcripts via the v2
//     API. No wait is going to surface one. SCRUM-399 reduced this from a
//     ~40 min poll wait (SCRUM-380's original threshold of 3 attempts) to an
//     immediate fallback.
//  2. every listed transcript is in a terminal failure state — Google has
//     given up; no amount of waiting will help.
//
// When at least one transcript is in a non-terminal state (STARTED/ENDED),
// Meet is actively preparing it; keep polling and prefer the native transcript.
func shouldEnqueueMeetWhisperFallback(transcripts []googlemeet.Transcript) bool {
	if len(transcripts) == 0 {
		return true
	}
	for _, t := range transcripts {
		if !googlemeet.IsTerminalTranscriptState(t.State) {
			return false
		}
	}
	return true
}

// enqueueGoogleMeetWhisperFallback mirrors the Teams fallback (pipeline_teams.go
// lines 253–344): resolves the primary video's whisper source URL, creates a
// transcript job keyed google_meet_whisper:<session_id>, enqueues it on the
// shared JobProcessor, and transitions the session_processing_job to
// awaiting_whisper. Returns true if state was transitioned to awaiting_whisper
// (either by enqueueing a new job or by detecting that a prior fallback job is
// already in flight). Returns false if any precondition fails (no processor,
// no ready primary artifact, R2 store missing for r2 artifact, presign error),
// in which case the caller falls through to setJobWaiting.
func enqueueGoogleMeetWhisperFallback(
	ctx context.Context,
	db *database.DB,
	sessionID, jobID uuid.UUID,
	attempt int,
	store storage.Interface,
	jobProcessor *utils.JobProcessor,
	exportURI string,
	updateMirror func(string),
) bool {
	if jobProcessor == nil {
		return false
	}
	sess, _ := db.GetSession(ctx, sessionID)
	if sess == nil || sess.PrimaryVideoArtifactID == nil {
		return false
	}
	fa, _ := db.GetFileArtifactByID(ctx, *sess.PrimaryVideoArtifactID)
	if fa == nil || fa.Status != models.FileArtifactStatusReady || fa.StorageKey == "" {
		return false
	}
	if fa.StorageProvider != "local" && fa.StorageProvider != "r2" {
		return false
	}

	var whisperSourceURL string
	if fa.StorageProvider == "r2" {
		if store == nil {
			log.Printf("Meet Whisper fallback: storage provider is r2 but store is nil; skipping")
			return false
		}
		presigned, presignErr := store.PresignGet(ctx, fa.StorageKey, 4*time.Hour)
		if presignErr != nil {
			log.Printf("Meet Whisper fallback: presign r2 key %q: %v; skipping", fa.StorageKey, presignErr)
			return false
		}
		whisperSourceURL = presigned
	} else {
		whisperSourceURL = filepath.ToSlash(fa.StorageKey)
	}
	if whisperSourceURL == "" {
		return false
	}

	// Ensure artifact + video_source rows exist (same idempotent pattern as the
	// happy path).
	artifacts, _ := db.GetArtifactsBySessionID(ctx, sessionID)
	var artifactID uuid.UUID
	if len(artifacts) > 0 {
		artifactID = artifacts[0].ID
	} else {
		title := "Google Meet Recording"
		if sess.Title != "" {
			title = sess.Title
		}
		artifact, createErr := db.CreateArtifact(ctx, sessionID, title, nil)
		if createErr != nil {
			log.Printf("Meet Whisper fallback: create artifact: %v", createErr)
			return false
		}
		artifactID = artifact.ID
	}
	// SCRUM-470: always create a NEW video_source per import. Reusing
	// sources[0] was single-recording-era logic — for multi-recording it
	// silently overwrote the Whisper transcript on an unrelated row and
	// never produced a new VIDEOS entry for the imported recording.
	videoID := uuid.New()
	playbackURL := exportURI
	if playbackURL == "" {
		playbackURL = "https://meet.google.com/"
	}
	ts := "whisper"
	vs := &models.VideoSource{
		ID:                  videoID,
		ArtifactID:          artifactID,
		SessionID:           sessionID,
		Provider:            "google_meet",
		VideoURL:            playbackURL,
		PlaybackMode:        "embed",
		OriginalURL:         &playbackURL,
		TranscriptStatus:    models.VideoTranscriptStatusPending,
		TranscriptionSource: &ts,
		SourceType:          models.VideoSourceTypeEmbedURL,
	}
	if err := db.CreateVideoSource(ctx, vs); err != nil {
		log.Printf("Meet Whisper fallback: create video source: %v", err)
		return false
	}

	jobKey := "google_meet_whisper:" + sessionID.String()
	existing, _ := db.GetTranscriptJobByKey(ctx, jobKey)
	if existing != nil && existing.Status != models.TranscriptJobStatusFailed {
		// Prior fallback's Whisper job is still in flight (queued/running) or
		// already completed. Skip duplicate create+enqueue but still reflect
		// awaiting_whisper on the session_processing_job so subsequent polls
		// don't escalate to failed_permanent via maxWaitingAttempts.
		updateJobState(ctx, db, jobID, models.ProcessingStateAwaitingWhisper, models.ProcessingStageDownload, attempt, nil, nil, nil)
		_ = db.UnlockSessionProcessingJob(ctx, jobID)
		updateMirror(models.ProcessingStateAwaitingWhisper)
		return true
	}
	tj := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoID,
		SessionID:     sessionID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     whisperSourceURL,
		JobKey:        jobKey,
		QueuedAt:      time.Now(),
	}
	if err := db.CreateTranscriptJob(ctx, tj); err != nil {
		log.Printf("Meet Whisper fallback: create transcript job: %v", err)
		return false
	}
	_ = db.UpdateVideoSourceTranscriptionJob(ctx, videoID, &tj.ID)
	if enqErr := jobProcessor.Enqueue(ctx, tj); enqErr != nil {
		log.Printf("Meet Whisper fallback: enqueue: %v", enqErr)
		return false
	}
	updateJobState(ctx, db, jobID, models.ProcessingStateAwaitingWhisper, models.ProcessingStageDownload, attempt, nil, nil, nil)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
	updateMirror(models.ProcessingStateAwaitingWhisper)
	log.Printf("Meet Whisper fallback: enqueued transcript job for session %s", sessionID)
	return true
}

func googleMeetArtifactMetadata(conferenceRecord, recordingName, driveFileID, exportURI string) []byte {
	m := map[string]string{}
	if conferenceRecord != "" {
		m["google_conference_record"] = conferenceRecord
	}
	if recordingName != "" {
		m["google_recording_name"] = recordingName
	}
	if driveFileID != "" {
		m["google_drive_file_id"] = driveFileID
	}
	if exportURI != "" {
		m["google_export_uri"] = exportURI
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}
