// Package sessionimport contains the primitives that materialize content
// (artifacts, materials, links, video sources, transcripts, file artifacts,
// processing jobs, session metadata) into a destination session.
//
// CopySession (internal/handlers) populates SourceData from a source session
// row and threads it through the primitives. A future Session Templates feature
// (see SCRUM-347 spike) plugs in a different SourceData populator without
// rewriting the primitives or the handler-level orchestration.
//
// Design rules:
//   - Primitives accept input data slices, not source session IDs. Helpers
//     never load from a source session; the orchestrator does that and hands
//     them the data.
//   - Storage rekey requires the source storage namespace (today the source
//     session's UUID string) because the existing R2 keys embed it. That
//     value is carried on SourceData.SourceStorageNamespace so non-session
//     callers can supply their own.
//   - Per-row failures are non-fatal (log + continue) and accumulate in
//     Ctx.PartialFailures (consumed by SCRUM-344).
package sessionimport

import (
	"github.com/google/uuid"

	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
)

// SourceData holds the source-side content needed to materialize a destination session.
type SourceData struct {
	// Session is the source session row used for metadata (premise, decision, etc.).
	// CopySession populates this from a database row; templates will fabricate one.
	Session *models.Session

	Artifacts           []*models.Artifact
	Materials           []*models.Material
	SessionLinks        []*models.SessionLink
	VideoSources        []*models.VideoSource
	PrimaryFileArtifact *models.FileArtifact          // optional primary MP4
	ProcessingJob       *models.SessionProcessingJob  // optional cloud-import re-enqueue source

	// SourceStorageNamespace is the path segment to swap in storage keys
	// (today the source session UUID string). Required for video_sources rekey.
	SourceStorageNamespace string

	// Stubs populated by later epic tickets:
	Transcripts      []*models.Transcript    // SCRUM-342
	AllFileArtifacts []*models.FileArtifact  // SCRUM-343
}

// Deps wires runtime dependencies into the primitives.
type Deps struct {
	DB           *database.DB
	Storage      storage.Interface
	R2Prefix     string
	TriggerIndex func(uuid.UUID)
}

// Ctx is per-import mutable state threaded through every primitive.
type Ctx struct {
	Deps Deps
	Dst  *models.Session

	ArtifactRemap     map[uuid.UUID]uuid.UUID
	MaterialRemap     map[uuid.UUID]uuid.UUID
	LinkRemap         map[uuid.UUID]uuid.UUID
	VideoSourceRemap  map[uuid.UUID]uuid.UUID
	FileArtifactRemap map[uuid.UUID]uuid.UUID

	// PartialFailures accumulates the high-level categories that had at
	// least one non-fatal failure during import. Consumed by SCRUM-344.
	PartialFailures []string

	// CopiedPrimaryVideo records whether a primary file_artifact was
	// successfully copied; the handler uses it to decide whether to
	// re-enqueue a source processing job.
	CopiedPrimaryVideo bool
}

// NewCtx builds a fresh Ctx for a destination session.
func NewCtx(deps Deps, dst *models.Session) *Ctx {
	return &Ctx{
		Deps:              deps,
		Dst:               dst,
		ArtifactRemap:     map[uuid.UUID]uuid.UUID{},
		MaterialRemap:     map[uuid.UUID]uuid.UUID{},
		LinkRemap:         map[uuid.UUID]uuid.UUID{},
		VideoSourceRemap:  map[uuid.UUID]uuid.UUID{},
		FileArtifactRemap: map[uuid.UUID]uuid.UUID{},
	}
}

// recordPartialFailure adds a category to PartialFailures (deduped).
func (c *Ctx) recordPartialFailure(category string) {
	for _, s := range c.PartialFailures {
		if s == category {
			return
		}
	}
	c.PartialFailures = append(c.PartialFailures, category)
}
