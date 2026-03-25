package processing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/transcript"
	"github.com/psuthar/talkback/internal/utils"
	"github.com/psuthar/talkback/internal/zoom"
)

func zoomMaxVideoDurationSeconds() int64 {
	v := os.Getenv("ZOOM_MAX_VIDEO_DURATION_SECONDS")
	if v == "" {
		return 600
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n <= 0 {
		return 600
	}
	return n
}

func maxUploadBytesVideo() int64 {
	v := os.Getenv("MAX_UPLOAD_BYTES_VIDEO")
	if v == "" {
		return 1024 * 1024 * 1024
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n <= 0 {
		return 1024 * 1024 * 1024
	}
	return n
}

// ZoomTokenFunc returns a valid Zoom access token for the given creator identity (for background workers).
type ZoomTokenFunc func(ctx context.Context, creatorIdentity string) (string, error)

// TeamsTokenFunc returns a valid Microsoft Graph access token for the given creator identity.
type TeamsTokenFunc func(ctx context.Context, creatorIdentity string) (string, error)

// RunJob runs the ingestion pipeline for one session_processing_job (fetch → download → parse → chunk → embed → ready).
// It dispatches to the appropriate provider pipeline based on job.Source.
// Idempotent: skips stages whose outputs already exist. Updates job state and session mirror.
// onJobReady is optional; when set, it is called when the job reaches ready so the API can broadcast to WebSocket clients.
func RunJob(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getZoomToken ZoomTokenFunc, getTeamsToken TeamsTokenFunc, store storage.Interface, storagePrefix string, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) error {
	switch job.Source {
	case models.SessionProcessingJobSourceZoom:
		return runZoomJob(ctx, db, job, getZoomToken, store, storagePrefix, jobProcessor, onJobReady)
	case models.SessionProcessingJobSourceTeams:
		if getTeamsToken == nil {
			attempt := job.AttemptCount + 1
			setJobFailedPermanent(ctx, db, job.ID, attempt, "teams_not_configured", "Teams token resolver not configured")
			_ = db.UpdateSessionProcessingMirror(ctx, job.SessionID, models.ProcessingStateFailedPermanent)
			return nil
		}
		return runTeamsJob(ctx, db, job, getTeamsToken, store, storagePrefix, jobProcessor, onJobReady)
	default:
		// Any job source not explicitly handled here is a misconfiguration. Fail
		// permanently so the worker never reclaims it and the job does not spin.
		attempt := job.AttemptCount + 1
		msg := fmt.Sprintf("unsupported job source: %s", job.Source)
		setJobFailedPermanent(ctx, db, job.ID, attempt, "unsupported_source", msg)
		_ = db.UpdateSessionProcessingMirror(ctx, job.SessionID, models.ProcessingStateFailedPermanent)
		return nil
	}
}

// runZoomJob runs the Zoom-specific ingestion pipeline for one session_processing_job
// (fetch → download → parse → chunk → embed → ready).
// When store and storagePrefix are set, downloads Zoom MP4 and uploads to storage, then sets session primary_video_artifact_id.
// If Zoom transcript is not available, enqueues a Whisper fallback job when the video is stored locally and jobProcessor is set.
func runZoomJob(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getZoomToken ZoomTokenFunc, store storage.Interface, storagePrefix string, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) (err error) {
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

	// --- Stage: fetch (recordings only; transcript checked after MP4 ingest) ---
	updateJobState(ctx, db, jobID, models.ProcessingStateFetching, models.ProcessingStageFetch, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateFetching)

	rec, fetchErr := utils.GetMeetingRecordingsWithRetry(accessToken, instanceUUID)
	if fetchErr != nil {
		return handleZoomError(ctx, db, job, attempt, fetchErr, updateMirror)
	}

	// Zoom MP4 ingest: R2 when configured, else local disk (for local debugging). Idempotent: skip if session already has primary.
	doMp4Ingest := true
	if sess, _ := db.GetSession(ctx, sessionID); sess != nil && sess.PrimaryVideoArtifactID != nil {
		if fa, _ := db.GetFileArtifactByID(ctx, *sess.PrimaryVideoArtifactID); fa != nil && fa.Status == models.FileArtifactStatusReady {
			doMp4Ingest = false
			log.Printf("IMPORT_START session_id=%s meeting_uuid=%s skip_mp4=already_ingested", sessionID, instanceUUID)
		}
	}

	if doMp4Ingest {
		// Enforce configurable max Zoom recording duration (default 10 minutes) before download.
		maxDurationSec := zoomMaxVideoDurationSeconds()
		durationMinutes := rec.Duration
		durationSeconds := durationMinutes * 60
		if durationMinutes > 0 && int64(durationSeconds) > maxDurationSec {
			msg := fmt.Sprintf("Demo limit: Zoom recordings must be %d minutes or less. This recording is %d minutes.", maxDurationSec/60, durationMinutes)
			log.Printf("ZOOM_DURATION_REJECTED session_id=%s duration_minutes=%d max_seconds=%d", sessionID, durationMinutes, maxDurationSec)
			setJobFailedPermanent(ctx, db, jobID, attempt, "zoom_duration_limit", msg)
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
		if durationMinutes <= 0 {
			log.Printf("ZOOM_DURATION_UNKNOWN session_id=%s duration_unknown=true", sessionID)
		}

		fileTypes := make([]string, 0, len(rec.RecordingFiles))
		recTypes := make([]string, 0, len(rec.RecordingFiles))
		for _, f := range rec.RecordingFiles {
			fileTypes = append(fileTypes, f.FileType)
			if f.RecordingType != "" {
				recTypes = append(recTypes, f.RecordingType)
			}
		}
		log.Printf("IMPORT_START session_id=%s meeting_uuid=%s", sessionID, instanceUUID)
		log.Printf("ZOOM_RECORDING_FILES count=%d file_types=%v recording_types=%v", len(rec.RecordingFiles), fileTypes, recTypes)

		mp4File, selErr := utils.SelectCanonicalMP4(rec.RecordingFiles)
		if selErr != nil {
			log.Printf("CANONICAL_FILE_SELECTED error=%v (no MP4)", selErr)
			setJobFailedPermanent(ctx, db, jobID, attempt, "no_mp4", "No MP4 recording found for this meeting recording.")
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
		log.Printf("CANONICAL_FILE_SELECTED file_type=%s recording_type=%s download_url_present=%v", mp4File.FileType, mp4File.RecordingType, mp4File.DownloadURL != "")

		resp, downErr := zoom.DownloadRecordingFile(ctx, mp4File.DownloadURL, accessToken)
		if downErr != nil {
			log.Printf("ZOOM_DOWNLOAD status=error error=%v", downErr)
			next := time.Now().Add(BackoffTransient(attempt))
			setJobFailedTransient(ctx, db, jobID, attempt, "zoom_mp4_download", downErr.Error(), next)
			updateMirror(models.ProcessingStateFailedTransient)
			return nil
		}
		defer resp.Body.Close()

		// Abort if Zoom response size exceeds video upload cap (avoid streaming large body into R2).
		if resp.ContentLength > 0 {
			maxVideoBytes := maxUploadBytesVideo()
			if resp.ContentLength > maxVideoBytes {
				log.Printf("ZOOM_DOWNLOAD size_exceeds_cap content_length=%d max=%d", resp.ContentLength, maxVideoBytes)
				setJobFailedPermanent(ctx, db, jobID, attempt, "zoom_file_too_large", fmt.Sprintf("Recording file size (%d bytes) exceeds maximum allowed (%d bytes).", resp.ContentLength, maxVideoBytes))
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
		}

		if store != nil {
			// R2 path: create artifact pending, Put, HEAD verify, then mark ready with size/etag. storagePrefix may be "" (keys under sessions/).
			ingestStart := time.Now()
			artifactID := uuid.New()
			storageKey := storage.BuildArtifactStorageKey(storagePrefix, sessionID, artifactID, "zoom.mp4")
			bucket := os.Getenv("R2_BUCKET")
			if bucket == "" {
				bucket = "talkback-r2-bucket"
			}
			zoomMp4Filename := "zoom.mp4"
			fa := &models.FileArtifact{
				ID:              artifactID,
				SessionID:       &sessionID,
				Kind:            models.FileArtifactKindVideo,
				Filename:        &zoomMp4Filename,
				ContentType:     "video/mp4",
				StorageProvider: "r2",
				StorageBucket:   bucket,
				StorageKey:      storageKey,
				Status:          models.FileArtifactStatusPending,
			}
			if meta := zoomArtifactMetadata(instanceUUID, mp4File); len(meta) > 0 {
				fa.MetadataJSON = meta
			}
			if err := db.CreateFileArtifact(ctx, fa); err != nil {
				setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}

			log.Printf("R2_UPLOAD_START bucket=%s key=%s", bucket, storageKey)
			etag, _, putErr := store.Put(ctx, storageKey, resp.Body, "video/mp4", resp.ContentLength)
			if putErr != nil {
				log.Printf("R2_UPLOAD_DONE error=%v", putErr)
				_ = db.UpdateFileArtifactToFailed(ctx, artifactID, "r2_put: "+putErr.Error())
				setJobFailedPermanent(ctx, db, jobID, attempt, "r2_upload", putErr.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			log.Printf("R2_UPLOAD_DONE etag=%s", etag)

			exists, size, contentType, headErr := store.Head(ctx, storageKey)
			if headErr != nil || !exists || size <= 0 {
				stage := "r2_head"
				if headErr != nil {
					stage = "r2_head"
				} else if !exists {
					stage = "r2_head_verify"
				} else if size <= 0 {
					stage = "r2_head_empty"
				}
				_ = db.UpdateFileArtifactToFailed(ctx, artifactID, stage+": object missing or empty after put")
				setJobFailedPermanent(ctx, db, jobID, attempt, "r2_upload", "object missing or empty after upload")
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			ct := "video/mp4"
			if contentType != "" {
				ct = contentType
			}
			// Persist size and etag in metadata; mark ready only after successful HEAD.
			metaWithEtag := mergeEtagIntoMetadata(fa.MetadataJSON, etag)
			if err := db.UpdateFileArtifactToReadyWithMetadata(ctx, artifactID, size, ct, metaWithEtag); err != nil {
				log.Printf("processing job: update artifact ready: %v", err)
			}
			if err := db.SetSessionPrimaryVideoArtifact(ctx, sessionID, &artifactID); err != nil {
				log.Printf("processing job: set primary_video_artifact_id error: session_id=%s error=%v", sessionID, err)
			}
			durationMs := time.Since(ingestStart).Milliseconds()
			log.Printf("zoom_ingest_completed session_id=%s artifact_id=%s size_bytes=%d duration_ms=%d outcome=ready", sessionID, artifactID, size, durationMs)
			log.Printf("IMPORT_DONE artifact_id=%s storage_key=%s storage=r2", artifactID, storageKey)
		} else {
			// Local path (for local debugging without R2)
			artifactID := uuid.New()
			localRelKey := filepath.Join(storage.SessionStorageRoot, sessionID.String(), "videos", "zoom.mp4")
			uploadRoot := storage.UploadRoot()
			dir := filepath.Join(uploadRoot, storage.SessionStorageRoot, sessionID.String(), "videos")
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Printf("LOCAL_MP4 mkdir error: %v", err)
				setJobFailedPermanent(ctx, db, jobID, attempt, "local_mkdir", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			absPath := filepath.Join(uploadRoot, localRelKey)
			f, err := os.Create(absPath)
			if err != nil {
				log.Printf("LOCAL_MP4 create file error: %v", err)
				setJobFailedPermanent(ctx, db, jobID, attempt, "local_create", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			size, err := io.Copy(f, resp.Body)
			_ = f.Close()
			if err != nil {
				os.Remove(absPath)
				log.Printf("LOCAL_MP4 write error: %v", err)
				setJobFailedPermanent(ctx, db, jobID, attempt, "local_write", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			zoomMp4Filename := "zoom.mp4"
			fa := &models.FileArtifact{
				ID:              artifactID,
				SessionID:       &sessionID,
				Kind:            models.FileArtifactKindVideo,
				Filename:        &zoomMp4Filename,
				ContentType:     "video/mp4",
				StorageProvider: "local",
				StorageBucket:   "local",
				StorageKey:      localRelKey,
				Status:          models.FileArtifactStatusPending,
			}
			if meta := zoomArtifactMetadata(instanceUUID, mp4File); len(meta) > 0 {
				fa.MetadataJSON = meta
			}
			if err := db.CreateFileArtifact(ctx, fa); err != nil {
				os.Remove(absPath)
				setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			if err := db.UpdateFileArtifactToReady(ctx, artifactID, size, "video/mp4"); err != nil {
				log.Printf("processing job: update artifact ready: %v", err)
			}
			if err := db.SetSessionPrimaryVideoArtifact(ctx, sessionID, &artifactID); err != nil {
				log.Printf("processing job: set primary_video_artifact_id error: session_id=%s error=%v", sessionID, err)
			}
			log.Printf("IMPORT_DONE artifact_id=%s storage_key=%s storage=local", artifactID, localRelKey)
		}
	}

	// Transcript: use native Zoom transcript if available; otherwise Whisper fallback (when video is local and jobProcessor set)
	transcriptFile, transcriptStatus := utils.FindTranscriptFileWithStatus(rec.RecordingFiles)
	if transcriptFile == nil || transcriptStatus == utils.TranscriptStatusProcessing {
		// Whisper fallback: if we have the video stored locally, enqueue a transcript job and move to awaiting_whisper
		if jobProcessor != nil {
			sess, _ := db.GetSession(ctx, sessionID)
			if sess != nil && sess.PrimaryVideoArtifactID != nil {
				fa, _ := db.GetFileArtifactByID(ctx, *sess.PrimaryVideoArtifactID)
				if fa != nil && fa.Status == models.FileArtifactStatusReady && (fa.StorageProvider == "local" || fa.StorageProvider == "r2") && fa.StorageKey != "" {
					// Resolve source URL based on storage provider.
					var whisperSourceURL string
					if fa.StorageProvider == "r2" {
						if store == nil {
							log.Printf("Zoom Whisper fallback: storage provider is r2 but store is nil; skipping")
						} else {
							presigned, presignErr := store.PresignGet(ctx, fa.StorageKey, 4*time.Hour)
							if presignErr != nil {
								log.Printf("Zoom Whisper fallback: presign r2 key %q: %v; skipping", fa.StorageKey, presignErr)
							} else {
								whisperSourceURL = presigned
							}
						}
					} else {
						whisperSourceURL = filepath.ToSlash(fa.StorageKey)
					}
					if whisperSourceURL != "" {
					// Ensure artifact + video_source exist (idempotent)
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
					var videoID uuid.UUID
					if len(sources) > 0 {
						videoID = sources[0].ID
					} else {
						videoID = uuid.New()
						zoomURL := "https://zoom.us/recording/detail?meeting_id=" + url.QueryEscape(instanceUUID)
						vs := &models.VideoSource{
							ID:               videoID,
							ArtifactID:       artifactID,
							SessionID:        sessionID,
							Provider:         "zoom",
							VideoURL:         zoomURL,
							PlaybackMode:     "embed",
							OriginalURL:      &zoomURL,
							TranscriptStatus: models.VideoTranscriptStatusPending,
							SourceType:       models.VideoSourceTypeEmbedURL,
						}
						if err := db.CreateVideoSource(ctx, vs); err != nil {
							log.Printf("Zoom Whisper fallback: create video source: %v", err)
							// fall through to waiting
						}
					}
					if videoID != uuid.Nil {
						jobKey := "zoom_whisper:" + sessionID.String()
						existing, _ := db.GetTranscriptJobByKey(ctx, jobKey)
						if existing == nil || existing.Status == models.TranscriptJobStatusFailed {
							sourceURL := whisperSourceURL
							tj := &models.TranscriptJob{
								ID:            uuid.New(),
								VideoSourceID: videoID,
								SessionID:     sessionID,
								Status:        models.TranscriptJobStatusQueued,
								SourceURL:     sourceURL,
								JobKey:        jobKey,
								QueuedAt:      time.Now(),
							}
							if err := db.CreateTranscriptJob(ctx, tj); err != nil {
								log.Printf("Zoom Whisper fallback: create transcript job: %v", err)
							} else {
								_ = db.UpdateVideoSourceTranscriptionJob(ctx, videoID, &tj.ID)
								if enqErr := jobProcessor.Enqueue(ctx, tj); enqErr != nil {
									log.Printf("Zoom Whisper fallback: enqueue: %v", enqErr)
								} else {
									log.Printf("Zoom Whisper fallback: enqueued transcript job for session %s", sessionID)
									updateJobState(ctx, db, jobID, models.ProcessingStateAwaitingWhisper, "download", attempt, nil, nil, nil)
									_ = db.UnlockSessionProcessingJob(ctx, jobID)
									updateMirror(models.ProcessingStateAwaitingWhisper)
									return nil
								}
							}
						}
					}
					} // end if whisperSourceURL != ""
				}
			}
		}
		// No Whisper fallback or no presignable URL: wait for Zoom transcript
		code := "transcript_not_ready"
		msg := "no transcript file available"
		if transcriptStatus == utils.TranscriptStatusProcessing {
			msg = "transcript still processing"
		}
		next := time.Now().Add(BackoffWaiting(attempt))
		setJobWaiting(ctx, db, jobID, attempt, code, msg, next)
		updateMirror(models.ProcessingStateWaiting)
		return nil
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
	if indexErr := rag.IndexSession(ctx, db, embedder, sessionID, store); indexErr != nil {
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
	if onJobReady != nil {
		onJobReady(sessionID)
	}
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

// zoomArtifactMetadata returns JSON for artifact.metadata_json (meeting_uuid, recording file id for troubleshooting).
func zoomArtifactMetadata(meetingUUID string, mp4File *utils.ZoomRecordingFile) []byte {
	if meetingUUID == "" && (mp4File == nil || mp4File.ID == "") {
		return nil
	}
	m := map[string]string{}
	if meetingUUID != "" {
		m["zoom_meeting_uuid"] = meetingUUID
	}
	if mp4File != nil && mp4File.ID != "" {
		m["zoom_recording_file_id"] = mp4File.ID
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

// mergeEtagIntoMetadata merges etag into existing metadata_json; returns new JSON bytes.
func mergeEtagIntoMetadata(existing []byte, etag string) []byte {
	m := make(map[string]string)
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &m)
	}
	if etag != "" {
		m["etag"] = etag
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

func updateJobState(ctx context.Context, db *database.DB, jobID uuid.UUID, state, stage string, attempt int, nextRetryAt *time.Time, code, msg *string) {
	_ = db.UpdateSessionProcessingJobState(ctx, jobID, state, stage, attempt, nextRetryAt, code, msg)
}

// maxTransientAttempts is the number of failed_transient retries before escalating to failed_permanent.
const maxTransientAttempts = 5

// maxWaitingAttempts is the number of waiting retries before escalating to failed_permanent.
const maxWaitingAttempts = 5

func setJobFailedTransient(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string, nextRetryAt time.Time) {
	if attempt > maxTransientAttempts {
		escalatedMsg := fmt.Sprintf("permanent failure after %d retries: %s", maxTransientAttempts, msg)
		updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "fetch", attempt, nil, &code, &escalatedMsg)
	} else {
		updateJobState(ctx, db, jobID, models.ProcessingStateFailedTransient, "fetch", attempt, &nextRetryAt, &code, &msg)
	}
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func setJobFailedPermanent(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string) {
	updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "fetch", attempt, nil, &code, &msg)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func setJobWaiting(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string, nextRetryAt time.Time) {
	if attempt > maxWaitingAttempts {
		escalatedMsg := fmt.Sprintf("permanent failure after %d retries: %s", maxWaitingAttempts, msg)
		updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "download", attempt, nil, &code, &escalatedMsg)
	} else {
		updateJobState(ctx, db, jobID, models.ProcessingStateWaiting, "download", attempt, &nextRetryAt, &code, &msg)
	}
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
