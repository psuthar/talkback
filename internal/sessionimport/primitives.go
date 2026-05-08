package sessionimport

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
			// SCRUM-346: prefer server-side CopyObject; fall back to Get+Put on error.
			if err := c.Deps.Storage.CopyObject(ctx, m.StorageKey, newKey); err != nil {
				log.Printf("sessionimport ImportMaterials CopyObject %s -> %s: %v; falling back to Get+Put", m.StorageKey, newKey, err)
				rc, gerr := c.Deps.Storage.Get(ctx, m.StorageKey)
				if gerr != nil {
					log.Printf("sessionimport ImportMaterials R2 Get %s: %v", m.StorageKey, gerr)
					c.recordPartialFailure("materials")
					continue
				}
				var size int64
				if m.SizeBytes != nil {
					size = *m.SizeBytes
				}
				_, _, perr := c.Deps.Storage.Put(ctx, newKey, rc, m.ContentType, size)
				_ = rc.Close()
				if perr != nil {
					log.Printf("sessionimport ImportMaterials R2 Put %s: %v", newKey, perr)
					c.recordPartialFailure("materials")
					continue
				}
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
			newSlideKey := storage.SlideImageKeyFromArtifactKey(newMaterial.StorageKey, entry.Index)
			// SCRUM-346: prefer server-side CopyObject; fall back to Get+Put.
			if err := c.Deps.Storage.CopyObject(ctx, entry.StorageKey, newSlideKey); err != nil {
				slideRc, gerr := c.Deps.Storage.Get(ctx, entry.StorageKey)
				if gerr != nil {
					continue
				}
				_, _, perr := c.Deps.Storage.Put(ctx, newSlideKey, slideRc, "image/png", 0)
				slideRc.Close()
				if perr != nil {
					continue
				}
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
// SCRUM-358: the destination stored_video_object_key is rebuilt from the
// source key's basename via storage.SessionVideoBasenameKey rather than a
// string-replace of the source session UUID. This makes the primitive
// session-source-agnostic — the SCRUM-350 template path can call
// ImportVideoSources without fabricating a fake source-session namespace.
func ImportVideoSources(ctx context.Context, c *Ctx, src []*models.VideoSource) error {
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
			// SCRUM-358: rebuild from basename + dst session id instead of
			// string-replace on a source-session UUID. Produces the same
			// shape as before (sessions/<dst>/videos/<basename>) without
			// requiring a SourceStorageNamespace.
			newKey := filepath.ToSlash(filepath.Join(storage.SessionStorageRoot, c.Dst.ID.String(), "videos", filepath.Base(filepath.FromSlash(oldKey))))
			if newKey != oldKey {
				if c.Deps.Storage != nil {
					// SCRUM-346: prefer server-side CopyObject; fall back to Get+Put.
					if err := c.Deps.Storage.CopyObject(ctx, oldKey, newKey); err == nil {
						copyVS.StoredVideoObjectKey = &newKey
					} else {
						rc, gerr := c.Deps.Storage.Get(ctx, oldKey)
						if gerr == nil {
							_, _, perr := c.Deps.Storage.Put(ctx, newKey, rc, "video/mp4", 0)
							rc.Close()
							if perr == nil {
								copyVS.StoredVideoObjectKey = &newKey
							}
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

// ImportFileArtifacts copies all session-scoped file_artifacts from src onto
// the destination, copying each storage object (R2 or local) under a new key
// and creating a new file_artifacts row with new ID and session_id == dst.
// Builds c.FileArtifactRemap so SCRUM-341's video_sources.file_artifact_id
// remap and SCRUM-340's primary_video_artifact_id remap can resolve through
// the same map.
//
// If a copied FA's source ID matches sourcePrimaryVideoArtifactID, also call
// SetSessionPrimaryVideoArtifact to preserve the legacy primary path
// (sessions.primary_video_artifact_id). The SCRUM-271 explicit primary
// (primary_content_kind=video) is set later by ImportSessionMetadata.
//
// Per-row failures are non-fatal: log + recordPartialFailure; the FA is left
// out of the remap and any remap-dependent step (video_sources.file_artifact_id)
// will fall back to nil for that pointer.
func ImportFileArtifacts(ctx context.Context, c *Ctx, src []*models.FileArtifact, sourcePrimaryVideoArtifactID *uuid.UUID) error {
	for _, fa := range src {
		newID, ok := importOneFileArtifact(ctx, c, fa)
		if !ok {
			continue
		}
		c.FileArtifactRemap[fa.ID] = newID
		if sourcePrimaryVideoArtifactID != nil && fa.ID == *sourcePrimaryVideoArtifactID {
			if err := c.Deps.DB.SetSessionPrimaryVideoArtifact(ctx, c.Dst.ID, &newID); err != nil {
				log.Printf("sessionimport ImportFileArtifacts SetSessionPrimaryVideoArtifact: %v", err)
				c.recordPartialFailure("file_artifacts")
				continue
			}
			c.CopiedPrimaryVideo = true
		}
	}
	return nil
}

// importOneFileArtifact copies one source FA's storage object and creates a
// new file_artifacts row. Returns the new FA ID and ok=true on success.
func importOneFileArtifact(ctx context.Context, c *Ctx, fa *models.FileArtifact) (uuid.UUID, bool) {
	if fa == nil || fa.Status != models.FileArtifactStatusReady || fa.StorageKey == "" {
		return uuid.Nil, false
	}
	filename := "video"
	if fa.Filename != nil && *fa.Filename != "" {
		filename = *fa.Filename
	}
	newFAID := uuid.New()
	switch fa.StorageProvider {
	case "r2":
		if c.Deps.Storage == nil {
			return uuid.Nil, false
		}
		r2Prefix := strings.TrimSuffix(strings.TrimSpace(c.Deps.R2Prefix), "/")
		newKey := storage.BuildArtifactStorageKey(r2Prefix, c.Dst.ID, newFAID, filename)
		// SCRUM-346: prefer server-side CopyObject; fall back to Get+Put on error.
		if err := c.Deps.Storage.CopyObject(ctx, fa.StorageKey, newKey); err != nil {
			log.Printf("sessionimport ImportFileArtifacts CopyObject %s -> %s: %v; falling back to Get+Put", fa.StorageKey, newKey, err)
			rc, gerr := c.Deps.Storage.Get(ctx, fa.StorageKey)
			if gerr != nil {
				log.Printf("sessionimport ImportFileArtifacts R2 Get %s: %v", fa.StorageKey, gerr)
				c.recordPartialFailure("file_artifacts")
				return uuid.Nil, false
			}
			var size int64
			if fa.SizeBytes != nil {
				size = *fa.SizeBytes
			}
			_, _, perr := c.Deps.Storage.Put(ctx, newKey, rc, fa.ContentType, size)
			_ = rc.Close()
			if perr != nil {
				log.Printf("sessionimport ImportFileArtifacts R2 Put %s: %v", newKey, perr)
				c.recordPartialFailure("file_artifacts")
				return uuid.Nil, false
			}
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
			log.Printf("sessionimport ImportFileArtifacts CreateFileArtifact: %v", err)
			c.recordPartialFailure("file_artifacts")
			return uuid.Nil, false
		}
		return newFAID, true
	case "local":
		baseName := filepath.Base(fa.StorageKey)
		if baseName == "" || baseName == "." {
			baseName = "zoom.mp4"
		}
		newRelKey := filepath.Join(storage.SessionStorageRoot, c.Dst.ID.String(), "videos", baseName)
		absSrc := filepath.Join(storage.UploadRoot(), filepath.FromSlash(fa.StorageKey))
		absDst := filepath.Join(storage.UploadRoot(), filepath.FromSlash(newRelKey))
		if err := os.MkdirAll(filepath.Dir(absDst), 0755); err != nil {
			log.Printf("sessionimport ImportFileArtifacts MkdirAll: %v", err)
			c.recordPartialFailure("file_artifacts")
			return uuid.Nil, false
		}
		size, err := copyLocalFileWithSize(absSrc, absDst)
		if err != nil {
			log.Printf("sessionimport ImportFileArtifacts local copy %s: %v", absSrc, err)
			c.recordPartialFailure("file_artifacts")
			return uuid.Nil, false
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
			log.Printf("sessionimport ImportFileArtifacts CreateFileArtifact: %v", err)
			c.recordPartialFailure("file_artifacts")
			return uuid.Nil, false
		}
		return newFAID, true
	}
	return uuid.Nil, false
}

// ImportTranscripts copies transcripts + transcript_segments rows from the
// source onto the destination. This is the canonical session-level transcript
// table populated by Teams/Meet pipelines (Zoom uses the on-row video_sources
// transcript_text). rag.IndexSession reads transcripts first before falling
// back to video_sources, so without copying these the clone loses richer
// segment-level anchoring.
//
// Whisper output is non-deterministic and ~$0.006/minute, so copying is
// strictly better than recomputing on the clone (see plan §2). The clone
// gets new transcript IDs and new transcript_segments rows linked to those
// new IDs and the destination session.
//
// SCRUM-344: any DB error here is critical. The orchestrator deletes the
// new session row (children cascade) and returns 500 — a clone with a
// missing or partial transcript is misleading enough that it should not
// be exposed.
func ImportTranscripts(ctx context.Context, c *Ctx, src []*models.Transcript) error {
	for _, t := range src {
		newTranscript := &models.Transcript{
			ID:           uuid.New(),
			SessionID:    c.Dst.ID,
			Source:       t.Source,
			Language:     t.Language,
			Status:       t.Status,
			RawText:      t.RawText,
			ErrorMessage: t.ErrorMessage,
		}
		if err := c.Deps.DB.CreateTranscript(ctx, newTranscript); err != nil {
			c.recordPartialFailure("transcripts")
			return fmt.Errorf("ImportTranscripts CreateTranscript: %w", err)
		}
		segments, err := c.Deps.DB.ListSegmentsByTranscriptID(ctx, t.ID)
		if err != nil {
			c.recordPartialFailure("transcripts")
			return fmt.Errorf("ImportTranscripts ListSegmentsByTranscriptID: %w", err)
		}
		if len(segments) == 0 {
			continue
		}
		// Reset IDs so InsertTranscriptSegments generates fresh ones; rewrite
		// transcript_id and session_id for the destination.
		newSegments := make([]models.TranscriptSegmentRow, 0, len(segments))
		for _, s := range segments {
			newSegments = append(newSegments, models.TranscriptSegmentRow{
				TranscriptID: newTranscript.ID,
				SessionID:    c.Dst.ID,
				Idx:          s.Idx,
				StartMs:      s.StartMs,
				EndMs:        s.EndMs,
				Text:         s.Text,
				SpeakerLabel: s.SpeakerLabel,
				SourceRef:    s.SourceRef,
			})
		}
		if err := c.Deps.DB.InsertTranscriptSegments(ctx, newTranscript.ID, c.Dst.ID, newSegments); err != nil {
			c.recordPartialFailure("transcripts")
			return fmt.Errorf("ImportTranscripts InsertTranscriptSegments: %w", err)
		}
	}
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
			// SCRUM-344: critical — clone with the wrong framing is worse than no clone.
			c.recordPartialFailure("session_metadata")
			return fmt.Errorf("ImportSessionMetadata UpdateSessionContext: %w", err)
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
			c.recordPartialFailure("session_metadata")
			return fmt.Errorf("ImportSessionMetadata UpdateSessionPrimary(video): %w", err)
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
			c.recordPartialFailure("session_metadata")
			return fmt.Errorf("ImportSessionMetadata UpdateSessionPrimary(document): %w", err)
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
			c.recordPartialFailure("session_metadata")
			return fmt.Errorf("ImportSessionMetadata UpdateSessionPrimary(link): %w", err)
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
