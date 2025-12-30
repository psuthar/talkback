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
