// Package prrisk implements deterministic PR risk scoring (v2) from git diffs.
package prrisk

import "time"

// Version is the report schema version.
const Version = 2

// Domain labels for changed-file classification (extensible).
const (
	DomainAuth       = "auth"
	DomainAPI        = "api"
	DomainDatabase   = "database"
	DomainMigrations = "migrations"
	DomainRAG        = "rag"
	DomainProcessing = "processing"
	DomainStorage    = "storage"
	DomainWeb        = "web"
	DomainWorkflows  = "workflows"
	DomainDeploy     = "deploy"
	DomainTests      = "tests"
	DomainScripts    = "scripts"
	DomainOther      = "other"
)

// FileChange is one path's diff stats.
type FileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// Signals is extracted, deterministic input to scoring.
type Signals struct {
	BaseRef        string         `json:"base_ref"`
	HeadRef        string         `json:"head_ref"`
	FileCount      int            `json:"file_count"`
	TotalAdded     int            `json:"total_added"`
	TotalDeleted   int            `json:"total_deleted"`
	TotalLOC       int            `json:"total_loc"` // added + deleted (diff churn)
	Files          []FileChange   `json:"files"`
	DomainHits     map[string]int `json:"domain_hits"` // domain -> file count
	TestFiles      int            `json:"test_files"`  // files classified as tests
	ConfigFiles    int            `json:"config_files"`
	MigrationFiles int            `json:"migration_files"`
	GitError       string         `json:"git_error,omitempty"`
}

// RiskFactor is one explainable contributor to the score.
type RiskFactor struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Points float64 `json:"points"`
	Detail string  `json:"detail,omitempty"`
}

// Mitigation maps a factor to recommended actions.
type Mitigation struct {
	FactorID string   `json:"factor_id"`
	Actions  []string `json:"actions"`
}

// Integrations holds optional hooks for CI / issue trackers (placeholders).
type Integrations struct {
	JiraIssueKey      string `json:"jira_issue_key,omitempty"`
	PRCommentMarkdown string `json:"pr_comment_markdown,omitempty"`
}

// Result is the full scoring output.
type Result struct {
	Version      int          `json:"version"`
	GeneratedAt  time.Time    `json:"generated_at"`
	BaseRef      string       `json:"base_ref"`
	Signals      Signals      `json:"signals"`
	RiskScore    float64      `json:"risk_score"` // 0–100, higher = riskier
	RiskBand     string       `json:"risk_band"`
	Factors      []RiskFactor `json:"factors"`
	Mitigations  []Mitigation `json:"mitigations"`
	Integrations Integrations `json:"integrations"`
}

// ScoreWeights tune deterministic contributions (sum capped at 100).
type ScoreWeights struct {
	LargeDiffLOC        int // threshold
	LargeDiffPoints     float64
	VeryLargeDiffLOC    int
	VeryLargeDiffPoints float64
	ManyFilesThreshold  int
	ManyFilesPoints     float64
	AuthPoints          float64
	MigrationsPoints    float64
	RAGPoints           float64
	ProcessingPoints    float64
	WebLargeLOC         int
	WebLargePoints      float64
	WorkflowsPoints     float64
	DeployPoints        float64
	ConfigPoints        float64
	TestsMissingPoints  float64
}

// DefaultWeights returns built-in v2 weights.
func DefaultWeights() ScoreWeights {
	return ScoreWeights{
		LargeDiffLOC:        400,
		LargeDiffPoints:     12,
		VeryLargeDiffLOC:    2000,
		VeryLargeDiffPoints: 22,
		ManyFilesThreshold:  35,
		ManyFilesPoints:     14,
		AuthPoints:          14,
		MigrationsPoints:    22,
		RAGPoints:           10,
		ProcessingPoints:    10,
		WebLargeLOC:         400,
		WebLargePoints:      12,
		WorkflowsPoints:     12,
		DeployPoints:        12,
		ConfigPoints:        8,
		TestsMissingPoints:  18,
	}
}
