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
	SessionID   uuid.UUID      `json:"session_id"` // Artifacts belong to sessions
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
	ArtifactID    uuid.UUID          `json:"artifact_id"` // Still keep for reference, but session_id is primary
	SessionID     uuid.UUID          `json:"session_id"`  // Materials belong to sessions
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
	ID                    uuid.UUID             `json:"id"`
	ArtifactID            uuid.UUID             `json:"artifact_id"` // Still keep for reference, but session_id is primary
	SessionID             uuid.UUID             `json:"session_id"`  // Video sources belong to sessions
	Provider              string                `json:"provider"`
	VideoURL              string                `json:"video_url"`    // Deprecated: use embed_url or media_url
	PlaybackMode          string                `json:"playback_mode"` // 'embed' or 'direct'
	EmbedURL              *string               `json:"embed_url,omitempty"`
	MediaURL              *string               `json:"media_url,omitempty"`
	DurationSeconds       *int                  `json:"duration_seconds,omitempty"`
	PosterURL             *string               `json:"poster_url,omitempty"`
	TranscriptStatus      VideoTranscriptStatus `json:"transcript_status"`
	TranscriptText        *string               `json:"transcript_text,omitempty"`
	AutoTranscribeEnabled bool                  `json:"auto_transcribe_enabled,omitempty"`
	TranscriptionSource   *string               `json:"transcription_source,omitempty"` // 'manual', 'loom_api', 'whisper'
	TranscriptionJobID    *uuid.UUID            `json:"transcription_job_id,omitempty"`
	CreatedAt             time.Time             `json:"created_at"`
}

// Phase 2: Q&A Models

type QuestionSource string

const (
	QuestionSourceText QuestionSource = "text"
)

type Question struct {
	ID              uuid.UUID     `json:"id"`
	ArtifactID      uuid.UUID     `json:"artifact_id"` // Still keep for reference, but session_id is primary
	SessionID       uuid.UUID     `json:"session_id"`  // Questions belong to sessions (required)
	AskedBy         *string       `json:"asked_by,omitempty"`
	QuestionText    string        `json:"question_text"`
	QuestionSource  QuestionSource `json:"question_source"`
	VideoTimeSeconds *int         `json:"video_time_seconds,omitempty"` // Timestamp when question was asked
	CreatedAt       time.Time     `json:"created_at"`
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
	Confirmed    bool        `json:"confirmed"` // Creator confirmation for positive answers
	CreatedAt    time.Time   `json:"created_at"`
}

// Phase 3: Session Models

type SessionStatus string

const (
	SessionStatusOpen   SessionStatus = "open"
	SessionStatusClosed SessionStatus = "closed"
)

type Session struct {
	ID         uuid.UUID     `json:"id"`
	Title      string        `json:"title"`
	CreatedBy  *string       `json:"created_by,omitempty"`
	Status     SessionStatus `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type SessionParticipant struct {
	ID             uuid.UUID `json:"id"`
	SessionID      uuid.UUID `json:"session_id"`
	ParticipantRef string    `json:"participant_ref"`
	JoinedAt       time.Time `json:"joined_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	WatchProgress  float32   `json:"watch_progress"` // 0.0-1.0
}

type SessionEventType string

const (
	SessionEventTypeJoin     SessionEventType = "join"
	SessionEventTypeLeave    SessionEventType = "leave"
	SessionEventTypePlay     SessionEventType = "play"
	SessionEventTypePause    SessionEventType = "pause"
	SessionEventTypeSeek     SessionEventType = "seek"
	SessionEventTypeQuestion SessionEventType = "question"
)

type SessionEvent struct {
	ID              uuid.UUID       `json:"id"`
	SessionID       uuid.UUID       `json:"session_id"`
	ParticipantRef  *string         `json:"participant_ref,omitempty"`
	EventType       SessionEventType `json:"event_type"`
	VideoTimeSeconds *int           `json:"video_time_seconds,omitempty"`
	Payload         map[string]interface{} `json:"payload"` // JSONB stored as map
	CreatedAt       time.Time       `json:"created_at"`
}

// Transcript Job Models (Auto-transcription)

type TranscriptJobStatus string

const (
	TranscriptJobStatusQueued      TranscriptJobStatus = "queued"
	TranscriptJobStatusDownloading TranscriptJobStatus = "downloading"
	TranscriptJobStatusTranscribing TranscriptJobStatus = "transcribing"
	TranscriptJobStatusSaving      TranscriptJobStatus = "saving"
	TranscriptJobStatusCompleted   TranscriptJobStatus = "completed"
	TranscriptJobStatusFailed      TranscriptJobStatus = "failed"
)

type TranscriptJob struct {
	ID                uuid.UUID            `json:"id"`
	VideoSourceID     uuid.UUID            `json:"video_source_id"`
	SessionID         uuid.UUID            `json:"session_id"`
	Status            TranscriptJobStatus   `json:"status"`
	ErrorMessage      *string              `json:"error_message,omitempty"`
	SourceURL         string               `json:"source_url"`
	ResolvedMediaURL  *string              `json:"resolved_media_url,omitempty"`
	QueuedAt          time.Time            `json:"queued_at"`
	StartedAt         *time.Time           `json:"started_at,omitempty"`
	CompletedAt       *time.Time           `json:"completed_at,omitempty"`
	WhisperModel      *string              `json:"whisper_model,omitempty"`
	DetectedLanguage  *string              `json:"detected_language,omitempty"`
	DurationSeconds   *int                 `json:"duration_seconds,omitempty"`
	JobKey            string               `json:"job_key"` // For idempotency: hash(video_source_id + source_url)
	LoomPassword      *string              `json:"loom_password,omitempty"` // Password for password-protected Loom videos (not logged, used only during resolution)
}
