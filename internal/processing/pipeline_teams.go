package processing

import (
	"bytes"
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
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/msgraph"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/transcript"
	"github.com/psuthar/talkback/internal/utils"
)

// runTeamsJob runs the Teams / Microsoft Graph ingestion pipeline for one session_processing_job.
// meeting_uuid = online meeting id; instance_uuid = recording id (Graph).
func runTeamsJob(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getTeamsToken TeamsTokenFunc, store storage.Interface, storagePrefix string, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) error {
	sessionID := job.SessionID
	jobID := job.ID
	attempt := job.AttemptCount + 1

	updateMirror := func(state string) {
		_ = db.UpdateSessionProcessingMirror(ctx, sessionID, state)
	}

	meetingID := ""
	recordingID := ""
	if job.MeetingUUID != nil {
		meetingID = strings.TrimSpace(*job.MeetingUUID)
	}
	if job.InstanceUUID != nil {
		recordingID = strings.TrimSpace(*job.InstanceUUID)
	}
	if meetingID == "" || recordingID == "" {
		setJobFailedPermanent(ctx, db, jobID, attempt, "teams_missing_ids", "missing meeting_id or recording_id on job")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	var accessToken string
	if getTeamsToken != nil && job.CreatorIdentity != nil && *job.CreatorIdentity != "" {
		var tokErr error
		accessToken, tokErr = getTeamsToken(ctx, *job.CreatorIdentity)
		if tokErr != nil {
			setJobFailedPermanent(ctx, db, jobID, attempt, "teams_auth", tokErr.Error())
			updateMirror(models.ProcessingStateFailedPermanent)
			return nil
		}
	} else {
		setJobFailedPermanent(ctx, db, jobID, attempt, "teams_auth", "creator_identity required for Teams import")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	httpClient := msgraph.DefaultHTTPClient()

	updateJobState(ctx, db, jobID, models.ProcessingStateFetching, models.ProcessingStageFetch, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateFetching)

	var rec *msgraph.RecordingDetail
	var err error
	if msgraph.IsTeamsOneDriveMeeting(meetingID) {
		rec, err = msgraph.GetDriveItemRecordingDetail(ctx, accessToken, meetingID, recordingID, httpClient)
	} else {
		rec, err = msgraph.GetRecording(ctx, accessToken, meetingID, recordingID, httpClient)
	}
	if err != nil {
		log.Printf("[teams] session_id=%s job_id=%s attempt=%d fetch_recording_error meeting_id=%q recording_id=%q err=%v",
			sessionID, jobID, attempt, meetingID, recordingID, err)
		next := time.Now().Add(BackoffTransient(attempt))
		setJobFailedTransient(ctx, db, jobID, attempt, "teams_fetch_recording", err.Error(), next)
		updateMirror(models.ProcessingStateFailedTransient)
		return nil
	}
	log.Printf("[teams] session_id=%s job_id=%s attempt=%d fetch_recording_ok meeting_id=%q recording_id=%q content_url_len=%d",
		sessionID, jobID, attempt, meetingID, recordingID, len(rec.RecordingContentURL))
	if strings.TrimSpace(rec.RecordingContentURL) == "" {
		log.Printf("[teams] session_id=%s job_id=%s attempt=%d no_download_url meeting_id=%q recording_id=%q",
			sessionID, jobID, attempt, meetingID, recordingID)
		setJobFailedPermanent(ctx, db, jobID, attempt, "teams_no_download_url", "Recording has no download URL yet, or your account cannot access it.")
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	// --- MP4 ingest (idempotent per-recording: SCRUM-467) ---
	// Skip ONLY when this specific (provider, meeting_uuid, instance_uuid)
	// already has a ready video_source on this session. The pre-SCRUM-467
	// check skipped on session.PrimaryVideoArtifactID, which broke every
	// 2nd-and-later import.
	doMp4Ingest := true
	if shouldSkipMP4Ingest(ctx, db, sessionID, "teams", &meetingID, &recordingID) {
		doMp4Ingest = false
		log.Printf("teams_ingest_skip session_id=%s meeting_id=%q recording_id=%q reason=already_ingested_this_recording", sessionID, meetingID, recordingID)
	}

	if doMp4Ingest {
		log.Printf("[teams] session_id=%s job_id=%s attempt=%d mp4_download_start subject=%q", sessionID, jobID, attempt, rec.Subject)
		resp, downErr := msgraph.DownloadHTTP(ctx, rec.RecordingContentURL, accessToken, httpClient)
		if downErr != nil {
			log.Printf("[teams] session_id=%s job_id=%s attempt=%d mp4_download_error err=%v", sessionID, jobID, attempt, downErr)
			next := time.Now().Add(BackoffTransient(attempt))
			setJobFailedTransient(ctx, db, jobID, attempt, "teams_mp4_download", downErr.Error(), next)
			updateMirror(models.ProcessingStateFailedTransient)
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("[teams] session_id=%s job_id=%s attempt=%d mp4_download_http_error status=%d", sessionID, jobID, attempt, resp.StatusCode)
			next := time.Now().Add(BackoffTransient(attempt))
			setJobFailedTransient(ctx, db, jobID, attempt, "teams_mp4_download", fmt.Sprintf("HTTP %d", resp.StatusCode), next)
			updateMirror(models.ProcessingStateFailedTransient)
			return nil
		}
		if resp.ContentLength > 0 {
			maxVideoBytes := maxUploadBytesVideo()
			if resp.ContentLength > maxVideoBytes {
				setJobFailedPermanent(ctx, db, jobID, attempt, "teams_file_too_large", fmt.Sprintf("Recording file size (%d bytes) exceeds maximum allowed (%d bytes).", resp.ContentLength, maxVideoBytes))
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
		}

		teamsMp4 := "teams.mp4"
		if store != nil {
			ingestStart := time.Now()
			artifactID := uuid.New()
			storageKey := storage.BuildArtifactStorageKey(storagePrefix, sessionID, artifactID, teamsMp4)
			bucket := os.Getenv("R2_BUCKET")
			if bucket == "" {
				bucket = "talkback-r2-bucket"
			}
			fa := &models.FileArtifact{
				ID:              artifactID,
				SessionID:       &sessionID,
				Kind:            models.FileArtifactKindVideo,
				Filename:        &teamsMp4,
				ContentType:     "video/mp4",
				StorageProvider: "r2",
				StorageBucket:   bucket,
				StorageKey:      storageKey,
				Status:          models.FileArtifactStatusPending,
			}
			if meta := teamsArtifactMetadata(meetingID, recordingID); len(meta) > 0 {
				fa.MetadataJSON = meta
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
			metaWithEtag := mergeEtagIntoMetadata(fa.MetadataJSON, etag)
			if err := db.UpdateFileArtifactToReadyWithMetadata(ctx, artifactID, size, ct, metaWithEtag); err != nil {
				log.Printf("teams job: update artifact ready: %v", err)
			}
			if err := setPrimaryIfNotSet(ctx, db, sessionID, artifactID); err != nil {
				log.Printf("teams job: set primary video: %v", err)
			}
			log.Printf("teams_ingest_completed session_id=%s artifact_id=%s size_bytes=%d duration_ms=%d", sessionID, artifactID, size, time.Since(ingestStart).Milliseconds())
		} else {
			artifactID := uuid.New()
			localRelKey := filepath.Join(storage.SessionStorageRoot, sessionID.String(), "videos", teamsMp4)
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
				Filename:        &teamsMp4,
				ContentType:     "video/mp4",
				StorageProvider: "local",
				StorageBucket:   "local",
				StorageKey:      localRelKey,
				Status:          models.FileArtifactStatusPending,
			}
			if meta := teamsArtifactMetadata(meetingID, recordingID); len(meta) > 0 {
				fa.MetadataJSON = meta
			}
			if err := db.CreateFileArtifact(ctx, fa); err != nil {
				os.Remove(absPath)
				setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
				updateMirror(models.ProcessingStateFailedPermanent)
				return nil
			}
			if err := db.UpdateFileArtifactToReady(ctx, artifactID, size, "video/mp4"); err != nil {
				log.Printf("teams job: update artifact ready: %v", err)
			}
			if err := setPrimaryIfNotSet(ctx, db, sessionID, artifactID); err != nil {
				log.Printf("teams job: set primary video: %v", err)
			}
		}
	}

	// --- Transcript (VTT) ---
	updateJobState(ctx, db, jobID, models.ProcessingStateDownloading, models.ProcessingStageDownload, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateDownloading)

	var vttStr string
	var vttErr error
	if !msgraph.IsTeamsOneDriveMeeting(meetingID) {
		vttStr, vttErr = msgraph.FetchTranscriptVTTBestEffort(ctx, accessToken, meetingID, recordingID, httpClient)
	}
	if vttErr != nil {
		log.Printf("teams transcript fetch error (non-fatal): %v", vttErr)
	}
	vttStr = strings.TrimSpace(vttStr)

	if vttStr == "" {
		if jobProcessor != nil {
			sess, _ := db.GetSession(ctx, sessionID)
			if sess != nil && sess.PrimaryVideoArtifactID != nil {
				fa, _ := db.GetFileArtifactByID(ctx, *sess.PrimaryVideoArtifactID)
				if fa != nil && fa.Status == models.FileArtifactStatusReady && (fa.StorageProvider == "local" || fa.StorageProvider == "r2") && fa.StorageKey != "" {
					// Resolve source URL based on storage provider.
					var whisperSourceURL string
					if fa.StorageProvider == "r2" {
						if store == nil {
							log.Printf("Teams Whisper fallback: storage provider is r2 but store is nil; skipping")
						} else {
							presigned, presignErr := store.PresignGet(ctx, fa.StorageKey, 4*time.Hour)
							if presignErr != nil {
								log.Printf("Teams Whisper fallback: presign r2 key %q: %v; skipping", fa.StorageKey, presignErr)
							} else {
								whisperSourceURL = presigned
							}
						}
					} else {
						whisperSourceURL = filepath.ToSlash(fa.StorageKey)
					}
					if whisperSourceURL != "" {
					artifacts, _ := db.GetArtifactsBySessionID(ctx, sessionID)
					var artifactID uuid.UUID
					if len(artifacts) > 0 {
						artifactID = artifacts[0].ID
					} else {
						title := "Teams Recording"
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
					// SCRUM-470: always create a new video_source per Teams
					// Whisper-fallback import. Reusing sources[0] was
					// single-recording-era logic that silently dropped the
					// 2nd-and-later recording.
					videoID := uuid.New()
					teamsURL := "https://teams.microsoft.com/"
					vs := &models.VideoSource{
						ID:               videoID,
						ArtifactID:       artifactID,
						SessionID:        sessionID,
						Provider:         "teams",
						VideoURL:         teamsURL,
						PlaybackMode:     "embed",
						OriginalURL:      &teamsURL,
						TranscriptStatus: models.VideoTranscriptStatusPending,
						SourceType:       models.VideoSourceTypeEmbedURL,
					}
					if err := db.CreateVideoSource(ctx, vs); err != nil {
						log.Printf("Teams Whisper fallback: create video source: %v", err)
					}
					if videoID != uuid.Nil {
						jobKey := "teams_whisper:" + sessionID.String()
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
								log.Printf("Teams Whisper fallback: create transcript job: %v", err)
							} else {
								_ = db.UpdateVideoSourceTranscriptionJob(ctx, videoID, &tj.ID)
								if enqErr := jobProcessor.Enqueue(ctx, tj); enqErr != nil {
									log.Printf("Teams Whisper fallback: enqueue: %v", enqErr)
								} else {
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
		next := time.Now().Add(BackoffWaiting(attempt))
		setJobWaiting(ctx, db, jobID, attempt, "teams_transcript_unavailable", "No Teams transcript available yet (check transcription policy / licensing). For cloud storage without Whisper, try again later or use local API storage for auto-transcription.", next)
		updateMirror(models.ProcessingStateWaiting)
		return nil
	}

	updateJobState(ctx, db, jobID, models.ProcessingStateParsing, models.ProcessingStageParse, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateParsing)

	parsed, parseErr := transcript.ParseVTT(bytes.NewReader([]byte(vttStr)))
	if parseErr != nil {
		setJobFailedPermanent(ctx, db, jobID, attempt, "vtt_parse_error", parseErr.Error())
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}

	transcriptRow := &models.Transcript{
		SessionID: sessionID,
		Source:    "teams",
		Status:    models.TranscriptStatusParsing,
	}
	if err := db.CreateTranscript(ctx, transcriptRow); err != nil {
		existing, _ := db.GetTranscriptBySessionID(ctx, sessionID, "teams")
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
		setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	_ = db.UpdateTranscriptStatus(ctx, transcriptRow.ID, models.TranscriptStatusReady, &rawText, nil)

	artifacts, _ := db.GetArtifactsBySessionID(ctx, sessionID)
	var artifactID uuid.UUID
	if len(artifacts) > 0 {
		artifactID = artifacts[0].ID
	} else {
		title := "Teams Recording"
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
	// SCRUM-470: always create a new video_source per Teams import.
	teamsURL := "https://teams.microsoft.com/"
	videoID := uuid.New()
	ts := "teams_api"
	vs := &models.VideoSource{
		ID:                  videoID,
		ArtifactID:          artifactID,
		SessionID:           sessionID,
		Provider:            "teams",
		VideoURL:            teamsURL,
		PlaybackMode:        "embed",
		OriginalURL:         &teamsURL,
		TranscriptStatus:    models.VideoTranscriptStatusReady,
		TranscriptionSource: &ts,
		SourceType:          models.VideoSourceTypeEmbedURL,
	}
	if err := db.CreateVideoSource(ctx, vs); err != nil {
		setJobFailedPermanent(ctx, db, jobID, attempt, "db_error", err.Error())
		updateMirror(models.ProcessingStateFailedPermanent)
		return nil
	}
	_ = db.UpdateVideoSourceZoomTranscript(ctx, videoID, rawText, &vttStr, videoSegments)

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

	updateJobState(ctx, db, jobID, models.ProcessingStateReady, models.ProcessingStageReady, attempt, nil, nil, nil)
	updateMirror(models.ProcessingStateReady)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
	if onJobReady != nil {
		onJobReady(sessionID)
	}
	log.Printf("teams processing job completed: session_id=%s job_id=%s", sessionID, jobID)
	return nil
}

func teamsArtifactMetadata(meetingID, recordingID string) []byte {
	m := map[string]string{}
	if meetingID != "" {
		m["teams_meeting_id"] = meetingID
	}
	if recordingID != "" {
		m["teams_recording_id"] = recordingID
	}
	if msgraph.IsTeamsOneDriveMeeting(meetingID) {
		m["teams_source"] = "onedrive"
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}
