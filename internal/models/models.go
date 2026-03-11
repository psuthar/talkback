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
	MaterialTextStatusPending    MaterialTextStatus = "pending"
	MaterialTextStatusProcessing MaterialTextStatus = "processing"
	MaterialTextStatusReady      MaterialTextStatus = "ready"
	MaterialTextStatusFailed     MaterialTextStatus = "failed"
)

type Material struct {
	ID              uuid.UUID          `json:"id"`
	ArtifactID      uuid.UUID          `json:"artifact_id"` // Still keep for reference, but session_id is primary
	SessionID       uuid.UUID          `json:"session_id"`  // Materials belong to sessions
	Kind            string             `json:"kind"`
	Filename        string             `json:"filename"`
	ContentType     string             `json:"content_type"`
	StorageURL      string             `json:"storage_url"`
	StorageProvider string             `json:"storage_provider"` // "local" or "r2"
	StorageKey      string             `json:"storage_key"`      // R2 object key when storage_provider is "r2"
	SizeBytes       *int64             `json:"size_bytes,omitempty"` // file size in bytes; nil for pasted content
	TextStatus      MaterialTextStatus `json:"text_status"`
	ExtractedText   *string            `json:"extracted_text,omitempty"`
	Title           *string            `json:"title,omitempty"`          // Display title (defaults to filename)
	ErrorMessage    *string            `json:"error_message,omitempty"`  // Set when text_status is failed
	CreatedAt       time.Time          `json:"created_at"`
	DeletedAt       *time.Time         `json:"deleted_at,omitempty"`     // Set when user deletes file (tombstone); file removed from R2/local
}

type VideoProvider string

const (
	VideoProviderLoom  VideoProvider = "loom"
	VideoProviderZoom  VideoProvider = "zoom"
	VideoProviderOther VideoProvider = "other"
)

type VideoTranscriptStatus string

const (
	VideoTranscriptStatusMissing    VideoTranscriptStatus = "missing"
	VideoTranscriptStatusPending    VideoTranscriptStatus = "pending"
	VideoTranscriptStatusProcessing VideoTranscriptStatus = "processing"
	VideoTranscriptStatusReady      VideoTranscriptStatus = "ready"
	VideoTranscriptStatusFailed     VideoTranscriptStatus = "failed"
)

type VideoSourceType string

const (
	VideoSourceTypeUpload    VideoSourceType = "upload"
	VideoSourceTypeDirectURL VideoSourceType = "direct_url"
	VideoSourceTypeEmbedURL  VideoSourceType = "embed_url"
)

// VideoRole indicates whether a video is the session's primary presentation or additional.
// Only video_sources may have a non-null video_role; at most one primary per session.
type VideoRole string

const (
	VideoRolePrimary   VideoRole = "primary"
	VideoRoleSecondary VideoRole = "secondary"
)

type VideoSource struct {
	ID                    uuid.UUID             `json:"id"`
	ArtifactID            uuid.UUID             `json:"artifact_id"` // Still keep for reference, but session_id is primary
	SessionID             uuid.UUID             `json:"session_id"`  // Video sources belong to sessions
	Provider              string                `json:"provider"`
	VideoURL              string                `json:"video_url"`     // Deprecated: use embed_url or media_url
	PlaybackMode          string                `json:"playback_mode"` // 'embed' or 'direct'
	EmbedURL              *string               `json:"embed_url,omitempty"`
	MediaURL              *string               `json:"media_url,omitempty"`
	DurationSeconds       *int                  `json:"duration_seconds,omitempty"`
	PosterURL             *string               `json:"poster_url,omitempty"`
	SourceType            VideoSourceType       `json:"source_type"`                       // 'upload', 'direct_url', 'embed_url'
	StoredVideoObjectKey  *string               `json:"stored_video_object_key,omitempty"` // Path to uploaded/downloaded MP4
	OriginalURL           *string               `json:"original_url,omitempty"`            // Original user-provided URL
	FailureReason         *string               `json:"failure_reason,omitempty"`          // Error message on failure
	TranscriptStatus      VideoTranscriptStatus `json:"transcript_status"`
	TranscriptText        *string               `json:"transcript_text,omitempty"`
	AutoTranscribeEnabled bool                  `json:"auto_transcribe_enabled,omitempty"`
	TranscriptionSource   *string               `json:"transcription_source,omitempty"` // 'manual', 'loom_api', 'whisper', 'zoom_api'
	TranscriptionJobID    *uuid.UUID            `json:"transcription_job_id,omitempty"`
	RawVTT                *string               `json:"raw_vtt,omitempty"`             // Original VTT from Zoom (optional)
	TranscriptSegments    []TranscriptSegment   `json:"transcript_segments,omitempty"` // Normalized start/end/text (optional)
	VideoRole             *VideoRole           `json:"video_role,omitempty"`         // primary | secondary; only one primary per session
	CreatedAt             time.Time             `json:"created_at"`
}

// TranscriptSegment is a normalized VTT cue for jump-to-moment and citations
type TranscriptSegment struct {
	StartTime float64 `json:"start_time"` // seconds
	EndTime   float64 `json:"end_time"`   // seconds
	Text      string  `json:"text"`
}

// TranscriptStatus for session-level transcript artifact
type TranscriptStatus string

const (
	TranscriptStatusParsing TranscriptStatus = "parsing"
	TranscriptStatusReady   TranscriptStatus = "ready"
	TranscriptStatusFailed  TranscriptStatus = "failed"
)

// FileArtifactKind is the type of binary file stored in R2.
type FileArtifactKind string

const (
	FileArtifactKindVideo          FileArtifactKind = "video"
	FileArtifactKindDocument       FileArtifactKind = "document"
	FileArtifactKindImage          FileArtifactKind = "image"
	FileArtifactKindAttachment     FileArtifactKind = "attachment"
	FileArtifactKindExport         FileArtifactKind = "export"
	FileArtifactKindTranscriptRaw  FileArtifactKind = "transcript_raw"
)

// FileArtifactStatus is the upload/processing status of a file artifact.
type FileArtifactStatus string

const (
	FileArtifactStatusPending FileArtifactStatus = "pending"
	FileArtifactStatusReady   FileArtifactStatus = "ready"
	FileArtifactStatusFailed  FileArtifactStatus = "failed"
)

// FileArtifact represents a binary file stored in R2 (video, document, image, etc.).
type FileArtifact struct {
	ID               uuid.UUID         `json:"id"`
	SessionID        *uuid.UUID        `json:"session_id,omitempty"`
	OwnerUserID      *uuid.UUID        `json:"owner_user_id,omitempty"`
	Kind             FileArtifactKind  `json:"kind"`
	Filename         *string           `json:"filename,omitempty"`
	ContentType      string            `json:"content_type"`
	SizeBytes        *int64            `json:"size_bytes,omitempty"`
	Sha256           *string           `json:"sha256,omitempty"`
	StorageProvider  string            `json:"storage_provider"`
	StorageBucket    string            `json:"storage_bucket"`
	StorageKey       string            `json:"storage_key"`
	Status           FileArtifactStatus `json:"status"`
	FailureReason    *string           `json:"failure_reason,omitempty"`
	MetadataJSON     []byte            `json:"metadata_json,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// Transcript represents a transcript artifact for a session (Mission #2)
type Transcript struct {
	ID           uuid.UUID        `json:"id"`
	SessionID    uuid.UUID        `json:"session_id"`
	Source       string           `json:"source"` // "zoom", "loom", "whisper", "manual"
	Language     *string          `json:"language,omitempty"`
	Status       TranscriptStatus `json:"status"`
	RawText      *string          `json:"raw_text,omitempty"`
	ErrorMessage *string          `json:"error_message,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// TranscriptSegmentRow is one segment row in transcript_segments table
type TranscriptSegmentRow struct {
	ID           uuid.UUID `json:"id"`
	TranscriptID uuid.UUID `json:"transcript_id"`
	SessionID    uuid.UUID `json:"session_id"`
	Idx          int       `json:"idx"`
	StartMs      int       `json:"start_ms"`
	EndMs        int       `json:"end_ms"`
	Text         string    `json:"text"`
	SpeakerLabel *string   `json:"speaker_label,omitempty"`
	SourceRef    *string   `json:"source_ref,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// IngestionJob tracks Zoom (and future) import jobs per session (legacy; prefer SessionProcessingJob)
type IngestionJob struct {
	ID           uuid.UUID `json:"id"`
	SessionID    uuid.UUID `json:"session_id"`
	Source       string    `json:"source"` // "zoom"
	State        string    `json:"state"`  // queued|fetching|ready|failed
	MeetingUUID  *string   `json:"meeting_uuid,omitempty"`
	InstanceUUID *string   `json:"instance_uuid,omitempty"`
	LastError    *string   `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SessionProcessingJobState and Stage (Mission #4)
const (
	ProcessingStateQueued             = "queued"
	ProcessingStateFetching           = "fetching"
	ProcessingStateDownloading       = "downloading"
	ProcessingStateParsing           = "parsing"
	ProcessingStateChunking          = "chunking"
	ProcessingStateEmbedding         = "embedding"
	ProcessingStateWaiting           = "waiting"
	ProcessingStateAwaitingWhisper   = "awaiting_whisper" // Zoom: video stored, Whisper job enqueued for transcript
	ProcessingStateReady            = "ready"
	ProcessingStateFailedTransient   = "failed_transient"
	ProcessingStateFailedPermanent  = "failed_permanent"
	ProcessingStateCanceled         = "canceled"
)

const (
	ProcessingStageFetch   = "fetch"
	ProcessingStageDownload = "download"
	ProcessingStageParse   = "parse"
	ProcessingStageChunk   = "chunk"
	ProcessingStageEmbed   = "embed"
	ProcessingStageReady   = "ready"
)

// SessionProcessingJob is the authoritative pipeline job for session import/index (Mission #4)
type SessionProcessingJob struct {
	ID               uuid.UUID  `json:"id"`
	SessionID        uuid.UUID  `json:"session_id"`
	Source           string     `json:"source"` // "zoom"
	State            string     `json:"state"`
	Stage            string     `json:"stage"`
	AttemptCount     int        `json:"attempt_count"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	LastErrorCode    *string    `json:"last_error_code,omitempty"`
	LastErrorMessage *string    `json:"last_error_message,omitempty"`
	LockedAt         *time.Time `json:"locked_at,omitempty"`
	LockOwner        *string    `json:"lock_owner,omitempty"`
	MeetingUUID      *string    `json:"meeting_uuid,omitempty"`
	InstanceUUID     *string    `json:"instance_uuid,omitempty"`
	CreatorIdentity  *string    `json:"creator_identity,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// ZoomConnection stores OAuth tokens for a creator's Zoom account (keyed by creator_identity_id)
type ZoomConnection struct {
	ID                    uuid.UUID `json:"id"`
	CreatorIdentityID     string    `json:"creator_identity_id"`
	ZoomUserID            string    `json:"zoom_user_id"`
	ZoomAccountID         *string   `json:"zoom_account_id,omitempty"`
	ZoomUserEmail         *string   `json:"zoom_user_email,omitempty"`
	AccessTokenEncrypted  []byte    `json:"-"` // never expose in JSON
	RefreshTokenEncrypted []byte    `json:"-"`
	ExpiresAt             time.Time `json:"expires_at"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// Phase 2: Q&A Models

type QuestionSource string

const (
	QuestionSourceText QuestionSource = "text"
)

type Question struct {
	ID                uuid.UUID      `json:"id"`
	ArtifactID        uuid.UUID      `json:"artifact_id"` // Still keep for reference, but session_id is primary
	SessionID         uuid.UUID      `json:"session_id"`  // Questions belong to sessions (required)
	ParentQuestionID  *uuid.UUID     `json:"parent_question_id,omitempty"` // Threaded reply: parent question (nil = root)
	AskedBy           *string        `json:"asked_by,omitempty"`
	QuestionText      string         `json:"question_text"`
	QuestionSource    QuestionSource `json:"question_source"`
	VideoTimeSeconds  *int           `json:"video_time_seconds,omitempty"` // Timestamp when question was asked
	CreatedAt         time.Time      `json:"created_at"`
}

type AnswerStatus string

const (
	AnswerStatusAnswered   AnswerStatus = "answered"
	AnswerStatusNotCovered AnswerStatus = "not_covered"
	AnswerStatusError      AnswerStatus = "error"
)

// CitationAnchor holds navigation anchor (time range, page, section, block).
// Fields not relevant to the anchor type may be omitted.
type CitationAnchor struct {
	Type    string `json:"type"` // "time_range" | "page" | "section" | "block" | "none"
	StartMs *int64 `json:"start_ms,omitempty"`
	EndMs   *int64 `json:"end_ms,omitempty"`
	Page    *int   `json:"page,omitempty"`
	Section string `json:"section,omitempty"`
	Block   *int   `json:"block,omitempty"`
}

type Citation struct {
	CitationID string          `json:"citation_id,omitempty"` // stable within answer: C1, C2, ...
	ChunkID    string          `json:"chunk_id"`              // unique identifier for the chunk
	SourceType string          `json:"source_type"`           // "material" or "transcript"
	SourceID   string          `json:"source_id"`             // material_id or video_id
	Locator    string          `json:"locator,omitempty"`     // human-readable (legacy)
	Snippet    string          `json:"snippet,omitempty"`     // ~200-300 chars (legacy)
	Anchor     *CitationAnchor `json:"anchor,omitempty"`      // structured anchor for navigation
	Label      string          `json:"label,omitempty"`       // e.g. "Transcript 01:12–04:38", "Document p. 4"
	Excerpt    string          `json:"excerpt,omitempty"`     // first ~200 chars of chunk text
}

type Answer struct {
	ID           uuid.UUID    `json:"id"`
	QuestionID   uuid.UUID    `json:"question_id"`
	AnswerText   string       `json:"answer_text"`
	AnswerStatus AnswerStatus `json:"answer_status"`
	Confidence   float32      `json:"confidence"` // 0.0-1.0
	Citations    []Citation   `json:"citations"`
	Model        *string      `json:"model,omitempty"`   // "manual" for human-provided; e.g. "gpt-4o-mini" for system
	AnsweredBy   *string      `json:"answered_by,omitempty"` // email/name when model = manual
	Confirmed    bool         `json:"confirmed"` // Creator confirmation for system-generated answers
	CreatedAt    time.Time    `json:"created_at"`
}

// Phase 3: Session Models

type SessionStatus string

const (
	SessionStatusOpen   SessionStatus = "open"
	SessionStatusClosed SessionStatus = "closed"
)

// SessionSourceProvider indicates how the session was created (zoom vs upload/other)
type SessionSourceProvider string

const (
	SessionSourceZoom   SessionSourceProvider = "zoom"
	SessionSourceUpload SessionSourceProvider = "upload"
)

type Session struct {
	ID                      uuid.UUID  `json:"id"`
	Title                   string     `json:"title"`
	CreatedBy               *string    `json:"created_by,omitempty"`
	Status                  SessionStatus         `json:"status"`
	SourceProvider          SessionSourceProvider `json:"source_provider,omitempty"`
	SourceReferenceURL      *string    `json:"source_reference_url,omitempty"`
	PrimaryVideoArtifactID  *uuid.UUID `json:"primary_video_artifact_id,omitempty"` // R2 file_artifact for main video
	IndexStatus             string    `json:"index_status,omitempty"`   // "none" | "building" | "ready" | "failed"
	IndexUpdatedAt          *time.Time `json:"index_updated_at,omitempty"`
	ProcessingState         string    `json:"processing_state,omitempty"`       // mirror of session_processing_jobs.state
	ProcessingUpdatedAt     *time.Time `json:"processing_updated_at,omitempty"`
	Premise                 *string   `json:"premise,omitempty"`
	PrimaryDecision         *string   `json:"primary_decision,omitempty"`
	DecisionOutcome         *string   `json:"decision_outcome,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
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
	ID               uuid.UUID              `json:"id"`
	SessionID        uuid.UUID              `json:"session_id"`
	ParticipantRef   *string                `json:"participant_ref,omitempty"`
	EventType        SessionEventType       `json:"event_type"`
	VideoTimeSeconds *int                   `json:"video_time_seconds,omitempty"`
	Payload          map[string]interface{} `json:"payload"` // JSONB stored as map
	CreatedAt        time.Time              `json:"created_at"`
}

// Transcript Job Models (Auto-transcription)

type TranscriptJobStatus string

const (
	TranscriptJobStatusQueued       TranscriptJobStatus = "queued"
	TranscriptJobStatusDownloading  TranscriptJobStatus = "downloading"
	TranscriptJobStatusTranscribing TranscriptJobStatus = "transcribing"
	TranscriptJobStatusSaving       TranscriptJobStatus = "saving"
	TranscriptJobStatusCompleted    TranscriptJobStatus = "completed"
	TranscriptJobStatusFailed       TranscriptJobStatus = "failed"
)

type TranscriptJob struct {
	ID               uuid.UUID           `json:"id"`
	VideoSourceID    uuid.UUID           `json:"video_source_id"`    // nil (uuid.Nil) when job is for a material
	MaterialID       *uuid.UUID          `json:"material_id,omitempty"` // set when transcribing an uploaded material (e.g. MP4)
	SessionID        uuid.UUID           `json:"session_id"`
	Status           TranscriptJobStatus `json:"status"`
	ErrorMessage     *string             `json:"error_message,omitempty"`
	SourceURL        string              `json:"source_url"`
	ResolvedMediaURL *string             `json:"resolved_media_url,omitempty"`
	QueuedAt         time.Time           `json:"queued_at"`
	StartedAt        *time.Time          `json:"started_at,omitempty"`
	CompletedAt      *time.Time          `json:"completed_at,omitempty"`
	WhisperModel     *string             `json:"whisper_model,omitempty"`
	DetectedLanguage *string             `json:"detected_language,omitempty"`
	DurationSeconds  *int                `json:"duration_seconds,omitempty"`
	JobKey           string              `json:"job_key"`                 // For idempotency: hash(video_source_id + source_url)
	LoomPassword     *string             `json:"loom_password,omitempty"` // Password for password-protected Loom videos (not logged, used only during resolution)
}

// Mission #3: Session-scoped RAG chunks and embeddings

// SessionChunk is a retrievable chunk for session-scoped RAG
type SessionChunk struct {
	ID          uuid.UUID              `json:"id"`
	SessionID   uuid.UUID              `json:"session_id"`
	SourceType  string                 `json:"source_type"` // "transcript" | "material"
	SourceID    *uuid.UUID             `json:"source_id,omitempty"`
	ChunkIdx    int                    `json:"chunk_idx"`
	Text        string                 `json:"text"`
	AnchorJSON  map[string]interface{} `json:"anchor_json,omitempty"`
	ContentHash string                 `json:"content_hash"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// SessionChunkEmbedding stores embedding vector for a chunk (JSONB array of floats)
type SessionChunkEmbedding struct {
	ID             uuid.UUID `json:"id"`
	ChunkID        uuid.UUID `json:"chunk_id"`
	SessionID      uuid.UUID `json:"session_id"`
	EmbeddingModel string    `json:"embedding_model"`
	Embedding      []float32 `json:"embedding"`
	CreatedAt      time.Time `json:"created_at"`
}

// --- TalkBack auth (users, login sessions) ---

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type GlobalRole string

const (
	GlobalRoleUser       GlobalRole = "user"        // legacy; treat as creator
	GlobalRoleAdmin      GlobalRole = "admin"
	GlobalRoleCreator    GlobalRole = "creator"
	GlobalRoleParticipant GlobalRole = "participant"
)

// CanCreateSessions returns true for admin and creator roles.
func (r GlobalRole) CanCreateSessions() bool {
	return r == GlobalRoleAdmin || r == GlobalRoleCreator || r == GlobalRoleUser
}

type User struct {
	ID               uuid.UUID  `json:"id"`
	Email            string     `json:"email"`
	DisplayName      string     `json:"display_name"`
	Status           UserStatus `json:"status"`
	GlobalRole       GlobalRole `json:"global_role"`
	EmailVerifiedAt  *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	LastLoginAt      *time.Time `json:"last_login_at,omitempty"`
}

type PasswordCredential struct {
	UserID            uuid.UUID `json:"user_id"`
	PasswordHash      string    `json:"-"`
	PasswordUpdatedAt time.Time `json:"password_updated_at"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type LoginSession struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	IP        *string   `json:"ip,omitempty"`
	UserAgent *string   `json:"user_agent,omitempty"`
}

// SessionInvitation records that a user was invited to a session (by creator or another invited participant).
type SessionInvitation struct {
	ID               uuid.UUID `json:"id"`
	SessionID        uuid.UUID `json:"session_id"`
	UserID           uuid.UUID `json:"user_id"`
	InvitedByUserID  *uuid.UUID `json:"invited_by_user_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// Stance values for decision_stances.
const (
	StanceAgree        = "agree"
	StanceDisagree     = "disagree"
	StanceConditional  = "conditional"
	StanceAbstain      = "abstain"
	StanceNeedMoreInfo = "need_more_info"
)

var ValidStances = []string{StanceAgree, StanceDisagree, StanceConditional, StanceAbstain, StanceNeedMoreInfo}

// DecisionStance represents a participant's recorded position on a session's primary decision.
type DecisionStance struct {
	ID        uuid.UUID `json:"id"`
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	Stance    string    `json:"stance"` // agree | disagree | conditional | abstain | need_more_info
	Rationale *string   `json:"rationale,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DecisionStanceWithUser extends DecisionStance with the submitter's email for the creator view.
type DecisionStanceWithUser struct {
	DecisionStance
	UserEmail string `json:"user_email"`
}

// StanceAggregate holds per-stance counts for a session.
type StanceAggregate struct {
	Agree        int `json:"agree"`
	Disagree     int `json:"disagree"`
	Conditional  int `json:"conditional"`
	Abstain      int `json:"abstain"`
	NeedMoreInfo int `json:"need_more_info"`
	Total        int `json:"total"`
}
