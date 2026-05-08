package sessionimport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// ImportSessionRow inserts c.Dst into the sessions table. The orchestrator
// is responsible for resolving title uniqueness and setting CreatedBy/Status
// before calling this primitive. On success the existing CreateSession trigger
// also creates the creator membership row (so c.Dst's creator gets access
// without explicit membership management here).
func ImportSessionRow(ctx context.Context, c *Ctx) error {
	return c.Deps.DB.CreateSession(ctx, c.Dst)
}

// ImportArtifacts inserts a destination artifact for each source artifact and
// records the old→new mapping in c.ArtifactRemap. If src is empty, a single
// default artifact is created so the destination is immediately usable
// (matches CreateSession behavior). Per-row failures are logged and recorded
// in PartialFailures.
func ImportArtifacts(ctx context.Context, c *Ctx, src []*models.Artifact) error {
	if len(src) == 0 {
		if _, err := c.Deps.DB.CreateArtifact(ctx, c.Dst.ID, c.Dst.Title, nil); err != nil {
			log.Printf("sessionimport ImportArtifacts default: %v", err)
			c.recordPartialFailure("artifacts")
		}
		return nil
	}
	for _, a := range src {
		newArtifact, err := c.Deps.DB.CreateArtifact(ctx, c.Dst.ID, a.Title, a.Description)
		if err != nil {
			log.Printf("sessionimport ImportArtifacts CreateArtifact: %v", err)
			c.recordPartialFailure("artifacts")
			continue
		}
		c.ArtifactRemap[a.ID] = newArtifact.ID
	}
	return nil
}

// ImportMaterials creates a destination material for each source material,
// re-keying R2 / local-disk storage objects so the destination owns its own
// files. Slide manifests and PNGs are copied for materials that support
// derived slide decks (PPT/PPTX). Per-row failures are non-fatal.
//
// srcSessionID is the source session's UUID string used to rewrite local
// upload paths; for templates this would come from the template descriptor's
// SourceStorageNamespace.
func ImportMaterials(ctx context.Context, c *Ctx, src []*models.Material, srcStorageNamespace string) error {
	r2Prefix := strings.TrimSuffix(strings.TrimSpace(c.Deps.R2Prefix), "/")
	for _, m := range src {
		newArtifactID, ok := c.ArtifactRemap[m.ArtifactID]
		if !ok {
			continue
		}
		newMaterial := &models.Material{
			ID:              uuid.New(),
			ArtifactID:      newArtifactID,
			SessionID:       c.Dst.ID,
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
		switch {
		case m.StorageProvider == "r2" && m.StorageKey != "" && c.Deps.Storage != nil:
			newKey := storage.BuildArtifactStorageKey(r2Prefix, c.Dst.ID, newArtifactID, m.Filename)
			rc, err := c.Deps.Storage.Get(ctx, m.StorageKey)
			if err != nil {
				log.Printf("sessionimport ImportMaterials R2 Get %s: %v", m.StorageKey, err)
				c.recordPartialFailure("materials")
				continue
			}
			var size int64
			if m.SizeBytes != nil {
				size = *m.SizeBytes
			}
			_, _, err = c.Deps.Storage.Put(ctx, newKey, rc, m.ContentType, size)
			_ = rc.Close()
			if err != nil {
				log.Printf("sessionimport ImportMaterials R2 Put %s: %v", newKey, err)
				c.recordPartialFailure("materials")
				continue
			}
			newMaterial.StorageKey = newKey
		case m.StorageProvider == "local" && m.Filename != "" && srcStorageNamespace != "":
			srcPath := filepath.Join(storage.UploadRoot(), storage.SessionStorageRoot, srcStorageNamespace, "data", "uploads", filepath.Base(m.Filename))
			dstDir := storage.SessionUploadsAbsDir(c.Dst.ID)
			if err := os.MkdirAll(dstDir, 0755); err != nil {
				log.Printf("sessionimport ImportMaterials MkdirAll: %v", err)
				c.recordPartialFailure("materials")
				continue
			}
			dstPath := filepath.Join(dstDir, filepath.Base(m.Filename))
			if err := copyLocalFile(srcPath, dstPath); err != nil {
				log.Printf("sessionimport ImportMaterials local copy %s: %v", srcPath, err)
				c.recordPartialFailure("materials")
				continue
			}
			newMaterial.StorageURL = storage.SessionArtifactPath(c.Dst.ID, m.Filename)
		}
		// Slide assets (manifest + PNGs) for PPT/PPTX-derived decks.
		if models.MaterialSupportsDerivedSlideDeck(m) {
			copySlideAssets(ctx, c, m, newMaterial, srcStorageNamespace)
		}
		if err := c.Deps.DB.CreateMaterial(ctx, newMaterial); err != nil {
			log.Printf("sessionimport ImportMaterials CreateMaterial: %v", err)
			c.recordPartialFailure("materials")
			continue
		}
		c.MaterialRemap[m.ID] = newMaterial.ID
	}
	return nil
}

func copySlideAssets(ctx context.Context, c *Ctx, m, newMaterial *models.Material, srcStorageNamespace string) {
	switch {
	case m.StorageProvider == "r2" && m.StorageKey != "" && newMaterial.StorageKey != "" && c.Deps.Storage != nil:
		rc, err := c.Deps.Storage.Get(ctx, storage.SlidesManifestKeyFromArtifactKey(m.StorageKey))
		if err != nil {
			return
		}
		var manifest utils.SlideManifest
		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			rc.Close()
			return
		}
		rc.Close()
		newSlides := make([]utils.SlideManifestEntry, 0, len(manifest.Slides))
		for _, entry := range manifest.Slides {
			slideRc, err := c.Deps.Storage.Get(ctx, entry.StorageKey)
			if err != nil {
				continue
			}
			newSlideKey := storage.SlideImageKeyFromArtifactKey(newMaterial.StorageKey, entry.Index)
			_, _, err = c.Deps.Storage.Put(ctx, newSlideKey, slideRc, "image/png", 0)
			slideRc.Close()
			if err != nil {
				continue
			}
			newSlides = append(newSlides, utils.SlideManifestEntry{Index: entry.Index, StorageKey: newSlideKey})
		}
		newManifestBytes, _ := json.Marshal(utils.SlideManifest{Slides: newSlides})
		_, _, _ = c.Deps.Storage.Put(ctx, storage.SlidesManifestKeyFromArtifactKey(newMaterial.StorageKey), bytes.NewReader(newManifestBytes), "application/json", int64(len(newManifestBytes)))
	case m.StorageProvider == "local" && newMaterial.StorageURL != "" && m.StorageURL != "":
		srcSlidesDir := filepath.Join(storage.UploadRoot(), m.StorageURL+"_slides")
		dstSlidesDir := filepath.Join(storage.UploadRoot(), newMaterial.StorageURL+"_slides")
		info, err := os.Stat(srcSlidesDir)
		if err != nil || !info.IsDir() {
			return
		}
		_ = os.MkdirAll(dstSlidesDir, 0755)
		entries, _ := os.ReadDir(srcSlidesDir)
		for _, e := range entries {
			_ = copyLocalFile(filepath.Join(srcSlidesDir, e.Name()), filepath.Join(dstSlidesDir, e.Name()))
		}
	}
}

// ImportSessionLinks creates a destination session_links row for each source
// link, copying URL/title/status/extracted_text/error_message verbatim.
func ImportSessionLinks(ctx context.Context, c *Ctx, src []*models.SessionLink) error {
	for _, link := range src {
		full, err := c.Deps.DB.GetSessionLinkByID(ctx, link.ID)
		if err != nil || full == nil {
			continue
		}
		newLink := &models.SessionLink{
			ID:            uuid.New(),
			SessionID:     c.Dst.ID,
			URL:           full.URL,
			Title:         full.Title,
			Status:        full.Status,
			ExtractedText: full.ExtractedText,
			ErrorMessage:  full.ErrorMessage,
		}
		if err := c.Deps.DB.CreateSessionLink(ctx, newLink); err != nil {
			log.Printf("sessionimport ImportSessionLinks CreateSessionLink: %v", err)
			c.recordPartialFailure("session_links")
			continue
		}
		c.LinkRemap[link.ID] = newLink.ID
	}
	return nil
}

// ImportVideoSources creates a destination video_sources row for each source
// row, re-keying the stored video object (R2 or local) and copying transcript
// text/VTT/segments. The first copied source is forced to the primary role so
// the UI shows exactly one primary on the destination.
//
// srcStorageNamespace is the source session's UUID string (R2 keys embed it
// and the legacy code uses string-replace; preserving that behavior).
func ImportVideoSources(ctx context.Context, c *Ctx, src []*models.VideoSource, srcStorageNamespace string) error {
	for i, vs := range src {
		newArtifactID, ok := c.ArtifactRemap[vs.ArtifactID]
		if !ok {
			continue
		}
		// SCRUM-341: remap video_sources.file_artifact_id through the
		// FileArtifactRemap populated by ImportPrimaryFileArtifact /
		// (SCRUM-343) ImportFileArtifacts. Source pointer that has no remap
		// entry (e.g. file_artifact failed to copy, or non-primary
		// file_artifact not yet covered before SCRUM-343) is recorded as nil
		// on the clone; never carry the source's pointer forward.
		var fileArtifactID *uuid.UUID
		if vs.FileArtifactID != nil {
			if newID, ok := c.FileArtifactRemap[*vs.FileArtifactID]; ok {
				idCopy := newID
				fileArtifactID = &idCopy
			} else {
				log.Printf("sessionimport ImportVideoSources: source video_source %s file_artifact_id %s not in remap; clone left nil", vs.ID, *vs.FileArtifactID)
			}
		}
		copyVS := &models.VideoSource{
			ID:                    uuid.New(),
			ArtifactID:            newArtifactID,
			SessionID:             c.Dst.ID,
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
			FileArtifactID:        fileArtifactID,
		}
		if vs.StoredVideoObjectKey != nil && *vs.StoredVideoObjectKey != "" {
			oldKey := *vs.StoredVideoObjectKey
			newKey := strings.Replace(oldKey, srcStorageNamespace, c.Dst.ID.String(), 1)
			if newKey != oldKey {
				if c.Deps.Storage != nil {
					rc, err := c.Deps.Storage.Get(ctx, oldKey)
					if err == nil {
						_, _, err = c.Deps.Storage.Put(ctx, newKey, rc, "video/mp4", 0)
						rc.Close()
						if err == nil {
							copyVS.StoredVideoObjectKey = &newKey
						}
					}
				}
				if copyVS.StoredVideoObjectKey == nil && strings.HasPrefix(oldKey, "sessions/") {
					absSrc := filepath.Join(storage.UploadRoot(), filepath.FromSlash(oldKey))
					absDst := filepath.Join(storage.UploadRoot(), filepath.FromSlash(newKey))
					if _, err := os.Stat(absSrc); err == nil {
						if err := os.MkdirAll(filepath.Dir(absDst), 0755); err == nil {
							if err := copyLocalFile(absSrc, absDst); err == nil {
								copyVS.StoredVideoObjectKey = &newKey
							}
						}
					}
				}
			}
		}
		if err := c.Deps.DB.CreateVideoSource(ctx, copyVS); err != nil {
			log.Printf("sessionimport ImportVideoSources CreateVideoSource: %v", err)
			c.recordPartialFailure("video_sources")
			continue
		}
		c.VideoSourceRemap[vs.ID] = copyVS.ID
		if vs.TranscriptText != nil && *vs.TranscriptText != "" {
			_ = c.Deps.DB.UpdateVideoSourceZoomTranscript(ctx, copyVS.ID, *vs.TranscriptText, vs.RawVTT, vs.TranscriptSegments)
		}
		if i == 0 {
			_ = c.Deps.DB.SetVideoSourceVideoRole(ctx, c.Dst.ID, copyVS.ID, models.VideoRolePrimary)
		}
	}
	return nil
}

// ImportPrimaryFileArtifact copies the source primary video file_artifact
// (R2 or local) and sets sessions.primary_video_artifact_id on the destination.
// fa is the source file_artifact (must be ready and have a non-empty
// StorageKey); pass nil to skip.
//
// SCRUM-343 will extend this to copy ALL session-scoped file_artifacts and
// build c.FileArtifactRemap; today we only handle the primary so existing
// behavior is preserved.
func ImportPrimaryFileArtifact(ctx context.Context, c *Ctx, fa *models.FileArtifact) error {
	if fa == nil || fa.Status != models.FileArtifactStatusReady || fa.StorageKey == "" {
		return nil
	}
	filename := "video"
	if fa.Filename != nil && *fa.Filename != "" {
		filename = *fa.Filename
	}
	newFAID := uuid.New()
	switch fa.StorageProvider {
	case "r2":
		if c.Deps.Storage == nil {
			return nil
		}
		r2Prefix := strings.TrimSuffix(strings.TrimSpace(c.Deps.R2Prefix), "/")
		newKey := storage.BuildArtifactStorageKey(r2Prefix, c.Dst.ID, newFAID, filename)
		rc, err := c.Deps.Storage.Get(ctx, fa.StorageKey)
		if err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact R2 Get %s: %v", fa.StorageKey, err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		var size int64
		if fa.SizeBytes != nil {
			size = *fa.SizeBytes
		}
		_, _, err = c.Deps.Storage.Put(ctx, newKey, rc, fa.ContentType, size)
		_ = rc.Close()
		if err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact R2 Put %s: %v", newKey, err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		newFA := &models.FileArtifact{
			ID:              newFAID,
			SessionID:       &c.Dst.ID,
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
		if err := c.Deps.DB.CreateFileArtifact(ctx, newFA); err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact CreateFileArtifact: %v", err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		if err := c.Deps.DB.SetSessionPrimaryVideoArtifact(ctx, c.Dst.ID, &newFAID); err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact SetSessionPrimaryVideoArtifact: %v", err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		c.FileArtifactRemap[fa.ID] = newFAID
		c.CopiedPrimaryVideo = true
	case "local":
		baseName := filepath.Base(fa.StorageKey)
		if baseName == "" || baseName == "." {
			baseName = "zoom.mp4"
		}
		newRelKey := filepath.Join(storage.SessionStorageRoot, c.Dst.ID.String(), "videos", baseName)
		absSrc := filepath.Join(storage.UploadRoot(), filepath.FromSlash(fa.StorageKey))
		absDst := filepath.Join(storage.UploadRoot(), filepath.FromSlash(newRelKey))
		if err := os.MkdirAll(filepath.Dir(absDst), 0755); err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact MkdirAll: %v", err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		size, err := copyLocalFileWithSize(absSrc, absDst)
		if err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact local copy %s: %v", absSrc, err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		newFA := &models.FileArtifact{
			ID:              newFAID,
			SessionID:       &c.Dst.ID,
			OwnerUserID:     nil,
			Kind:            fa.Kind,
			Filename:        fa.Filename,
			ContentType:     fa.ContentType,
			SizeBytes:       &size,
			Sha256:          fa.Sha256,
			StorageProvider: "local",
			StorageBucket:   "local",
			StorageKey:      filepath.ToSlash(newRelKey),
			Status:          models.FileArtifactStatusReady,
		}
		if err := c.Deps.DB.CreateFileArtifact(ctx, newFA); err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact CreateFileArtifact: %v", err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		if err := c.Deps.DB.SetSessionPrimaryVideoArtifact(ctx, c.Dst.ID, &newFAID); err != nil {
			log.Printf("sessionimport ImportPrimaryFileArtifact SetSessionPrimaryVideoArtifact: %v", err)
			c.recordPartialFailure("file_artifacts")
			return nil
		}
		c.FileArtifactRemap[fa.ID] = newFAID
		c.CopiedPrimaryVideo = true
	}
	return nil
}

// ImportTranscripts is a stub. SCRUM-342 will copy transcripts +
// transcript_segments rows here.
func ImportTranscripts(ctx context.Context, c *Ctx, src []*models.Transcript) error {
	return nil
}

// ImportSessionMetadata copies session-level framing fields from the source
// onto the destination: premise, primary_decision, decision_outcome, and the
// explicit primary_content_kind + primary_*_id pointer (remapped through the
// per-category remap maps populated by earlier primitives).
//
// source_reference_url is intentionally not handled here — it is part of
// CreateSession's INSERT, so the orchestrator sets it on c.Dst before
// ImportSessionRow runs.
//
// Failure modes:
//   - If a primary remap lookup misses (source pointed at a child the copy
//     could not reproduce), the clone's explicit primary is left unset and a
//     warning is logged. The legacy SCRUM-271 resolver fallback (e.g.
//     primary_video_artifact_id without an explicit kind) still applies.
//   - Any DB error from UpdateSessionContext / UpdateSessionPrimary is
//     non-fatal here and surfaces in c.PartialFailures (consumed by SCRUM-344).
func ImportSessionMetadata(ctx context.Context, c *Ctx, src *models.Session) error {
	if src == nil {
		return nil
	}
	if src.Premise != nil || src.PrimaryDecision != nil || src.DecisionOutcome != nil {
		if err := c.Deps.DB.UpdateSessionContext(ctx, c.Dst.ID, src.Premise, src.PrimaryDecision, src.DecisionOutcome); err != nil {
			log.Printf("sessionimport ImportSessionMetadata UpdateSessionContext: %v", err)
			c.recordPartialFailure("session_metadata")
		}
	}
	if src.PrimaryContentKind == nil {
		return nil
	}
	switch *src.PrimaryContentKind {
	case models.SessionPrimaryContentKindVideo:
		if src.PrimaryVideoArtifactID == nil {
			return nil
		}
		newID, ok := c.FileArtifactRemap[*src.PrimaryVideoArtifactID]
		if !ok {
			log.Printf("sessionimport: source primary video file_artifact %s not in remap; clone explicit primary left unset", *src.PrimaryVideoArtifactID)
			return nil
		}
		if err := c.Deps.DB.UpdateSessionPrimary(ctx, c.Dst.ID, models.SessionPrimaryContentKindVideo, &newID); err != nil {
			log.Printf("sessionimport ImportSessionMetadata UpdateSessionPrimary(video): %v", err)
			c.recordPartialFailure("session_metadata")
		}
	case models.SessionPrimaryContentKindDocument:
		if src.PrimaryMaterialID == nil {
			return nil
		}
		newID, ok := c.MaterialRemap[*src.PrimaryMaterialID]
		if !ok {
			log.Printf("sessionimport: source primary material %s not in remap; clone explicit primary left unset", *src.PrimaryMaterialID)
			return nil
		}
		if err := c.Deps.DB.UpdateSessionPrimary(ctx, c.Dst.ID, models.SessionPrimaryContentKindDocument, &newID); err != nil {
			log.Printf("sessionimport ImportSessionMetadata UpdateSessionPrimary(document): %v", err)
			c.recordPartialFailure("session_metadata")
		}
	case models.SessionPrimaryContentKindLink:
		if src.PrimarySessionLinkID == nil {
			return nil
		}
		newID, ok := c.LinkRemap[*src.PrimarySessionLinkID]
		if !ok {
			log.Printf("sessionimport: source primary session_link %s not in remap; clone explicit primary left unset", *src.PrimarySessionLinkID)
			return nil
		}
		if err := c.Deps.DB.UpdateSessionPrimary(ctx, c.Dst.ID, models.SessionPrimaryContentKindLink, &newID); err != nil {
			log.Printf("sessionimport ImportSessionMetadata UpdateSessionPrimary(link): %v", err)
			c.recordPartialFailure("session_metadata")
		}
	}
	return nil
}

// MaybeEnqueueProcessingJob re-enqueues a fresh queued processing job for the
// destination when the source had a cloud-import job (Zoom/Teams/Meet) but no
// MP4 was reproduced — so the worker can fetch the MP4 for the new session.
// The source's session row is used to default the job source.
func MaybeEnqueueProcessingJob(ctx context.Context, c *Ctx, srcJob *models.SessionProcessingJob, srcSession *models.Session) {
	if c.CopiedPrimaryVideo {
		return
	}
	if srcJob == nil || (srcJob.MeetingUUID == nil && srcJob.InstanceUUID == nil) {
		return
	}
	jobSource := srcJob.Source
	if jobSource == "" {
		if srcSession != nil && srcSession.SourceProvider != "" {
			jobSource = string(srcSession.SourceProvider)
		} else {
			jobSource = models.SessionProcessingJobSourceZoom
		}
	}
	creatorIdentity := srcJob.CreatorIdentity
	if creatorIdentity == nil && srcSession != nil && srcSession.CreatedBy != nil {
		creatorIdentity = srcSession.CreatedBy
	}
	newJob := &models.SessionProcessingJob{
		ID:              uuid.New(),
		SessionID:       c.Dst.ID,
		Source:          jobSource,
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     srcJob.MeetingUUID,
		InstanceUUID:    srcJob.InstanceUUID,
		CreatorIdentity: creatorIdentity,
	}
	if err := c.Deps.DB.CreateOrGetSessionProcessingJob(ctx, newJob); err != nil {
		log.Printf("sessionimport MaybeEnqueueProcessingJob (%s): %v", jobSource, err)
		return
	}
	if err := c.Deps.DB.UpdateSessionSourceProvider(ctx, c.Dst.ID, models.SessionSourceProvider(jobSource)); err != nil {
		log.Printf("sessionimport MaybeEnqueueProcessingJob UpdateSessionSourceProvider: %v", err)
	}
	log.Printf("sessionimport: enqueued %s processing for new session %s (MP4 will be available when job completes)", jobSource, c.Dst.ID)
}

// copyLocalFile copies src to dst (binary). Both paths must be absolute.
func copyLocalFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstF.Close()
	_, err = io.Copy(dstF, srcF)
	return err
}

func copyLocalFileWithSize(src, dst string) (int64, error) {
	srcF, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer srcF.Close()
	dstF, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer dstF.Close()
	return io.Copy(dstF, srcF)
}
