package models

import (
	"time"

	"github.com/google/uuid"
)

type ArtifactStatus string

const (
	StatusDraft ArtifactStatus = "draft"
	StatusReady ArtifactStatus = "ready"
)

type Artifact struct {
	ID          uuid.UUID      `json:"id"`
	Title       string         `json:"title"`
	Description *string        `json:"description,omitempty"`
	Status      ArtifactStatus `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type MaterialKind string

const (
	MaterialKindDocument MaterialKind = "document"
	MaterialKindSlides   MaterialKind = "slides"
	MaterialKindDiagram  MaterialKind = "diagram"
	MaterialKindOther    MaterialKind = "other"
)

type MaterialTextStatus string

const (
	MaterialTextStatusPending MaterialTextStatus = "pending"
	MaterialTextStatusReady   MaterialTextStatus = "ready"
	MaterialTextStatusFailed  MaterialTextStatus = "failed"
)

type Material struct {
	ID            uuid.UUID          `json:"id"`
	ArtifactID    uuid.UUID          `json:"artifact_id"`
	Kind          string             `json:"kind"`
	Filename      string             `json:"filename"`
	ContentType   string             `json:"content_type"`
	StorageURL    string             `json:"storage_url"`
	TextStatus    MaterialTextStatus `json:"text_status"`
	ExtractedText *string            `json:"extracted_text,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
}

type VideoProvider string

const (
	VideoProviderLoom  VideoProvider = "loom"
	VideoProviderZoom  VideoProvider = "zoom"
	VideoProviderOther VideoProvider = "other"
)

type VideoTranscriptStatus string

const (
	VideoTranscriptStatusMissing VideoTranscriptStatus = "missing"
	VideoTranscriptStatusPending VideoTranscriptStatus = "pending"
	VideoTranscriptStatusReady   VideoTranscriptStatus = "ready"
	VideoTranscriptStatusFailed  VideoTranscriptStatus = "failed"
)

type VideoSource struct {
	ID               uuid.UUID             `json:"id"`
	ArtifactID       uuid.UUID             `json:"artifact_id"`
	Provider         string                `json:"provider"`
	VideoURL         string                `json:"video_url"`
	TranscriptStatus VideoTranscriptStatus `json:"transcript_status"`
	TranscriptText   *string               `json:"transcript_text,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
}

// Phase 2: Q&A Models

type QuestionSource string

const (
	QuestionSourceText QuestionSource = "text"
)

type Question struct {
	ID            uuid.UUID     `json:"id"`
	ArtifactID    uuid.UUID     `json:"artifact_id"`
	AskedBy       *string       `json:"asked_by,omitempty"`
	QuestionText  string        `json:"question_text"`
	QuestionSource QuestionSource `json:"question_source"`
	CreatedAt     time.Time     `json:"created_at"`
}

type AnswerStatus string

const (
	AnswerStatusAnswered   AnswerStatus = "answered"
	AnswerStatusNotCovered AnswerStatus = "not_covered"
	AnswerStatusError      AnswerStatus = "error"
)

type Citation struct {
	ChunkID    string `json:"chunk_id"`    // unique identifier for the chunk
	SourceType string `json:"source_type"`  // "material" or "transcript"
	SourceID   string `json:"source_id"`   // material_id or video_id
	Locator    string `json:"locator"`      // timestamp or other locator
	Snippet    string `json:"snippet"`      // ~200-300 chars
}

type Answer struct {
	ID           uuid.UUID   `json:"id"`
	QuestionID   uuid.UUID   `json:"question_id"`
	AnswerText   string      `json:"answer_text"`
	AnswerStatus AnswerStatus `json:"answer_status"`
	Confidence   float32     `json:"confidence"` // 0.0-1.0
	Citations    []Citation  `json:"citations"`
	Model        *string     `json:"model,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}
