// Package prrisk implements deterministic PR risk scoring (v2.x) from git diffs.
package prrisk

import (
	"time"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

// Version is the major report schema version.
const Version = 2

// VersionMinor is the minor report schema version.
// v2.5 == Version=2, VersionMinor=5 (enforcement + merge recommendation)
const VersionMinor = 5

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
	BaseRef               string         `json:"base_ref"`
	HeadRef               string         `json:"head_ref"`
	FileCount             int            `json:"file_count"`
	TotalAdded            int            `json:"total_added"`
	TotalDeleted          int            `json:"total_deleted"`
	TotalLOC              int            `json:"total_loc"`                // added + deleted (diff churn)
	TestLOCRatio          float64        `json:"test_loc_ratio,omitempty"` // test LOC / total LOC
	Files                 []FileChange   `json:"files"`
	DomainHits            map[string]int `json:"domain_hits"` // domain -> file count
	TestDomainHits        map[string]int `json:"test_domain_hits,omitempty"`
	TestUnitDomainHits    map[string]int `json:"test_unit_domain_hits,omitempty"`
	TestE2EDomainHits     map[string]int `json:"test_e2e_domain_hits,omitempty"`
	TestFiles             int            `json:"test_files"` // files classified as tests
	UnitTestFiles         int            `json:"unit_test_files,omitempty"`
	E2ETestFiles          int            `json:"e2e_test_files,omitempty"`
	ConfigFiles           int            `json:"config_files"`
	MigrationFiles        int            `json:"migration_files"`
	GitError              string         `json:"git_error,omitempty"`
	ValidationNoteFound   bool           `json:"validation_note_found,omitempty"`
	ValidationNoteSnippet string         `json:"validation_note_snippet,omitempty"`
	// RepoRoot is the git worktree root used for contextual git queries (v2.4+).
	RepoRoot string `json:"repo_root,omitempty"`
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

// ConfidenceAdjustment is one contribution to the test confidence score.
type ConfidenceAdjustment struct {
	Reason string  `json:"reason"`
	Delta  float64 `json:"delta"` // positive = more confident, negative = less
}

// ConfidenceBreakdown explains how the test_confidence score was computed.
type ConfidenceBreakdown struct {
	BaseScore   float64                `json:"base_score"`
	Adjustments []ConfidenceAdjustment `json:"adjustments,omitempty"`
	FinalScore  float64                `json:"final_score"`
}

// RiskCategory is one breakdown lane for decision-grade reporting.
// Categories cover: code risk, workflow/config risk, and test confidence.
type RiskCategory struct {
	Key        string               `json:"key"`
	Label      string               `json:"label"`
	RiskScore  float64              `json:"risk_score"`
	Confidence float64              `json:"confidence,omitempty"` // primarily used by test confidence category
	Factors    []string             `json:"factors,omitempty"`    // factor IDs contributing to risk_score
	Reducers   []string             `json:"reducers,omitempty"`   // reducer IDs affecting risk_score
	Breakdown  *ConfidenceBreakdown `json:"breakdown,omitempty"`  // only set for test_confidence category
}

// RiskReducer is something that lowers risk deterministically (based on diff signals).
type RiskReducer struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Points   float64 `json:"points"` // positive amount reduces risk
	Evidence string  `json:"evidence,omitempty"`
	// CategoryKey indicates which lane this reducer primarily affects.
	CategoryKey string `json:"category_key,omitempty"`
}

// RequiredAction represents a pre-merge checklist item derived from risk analysis.
type RequiredAction struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	FixType     string `json:"fix_type,omitempty"` // code|test|config|process|infra|db
	AppliesWhen string `json:"applies_when,omitempty"`
	// Checklist provides concrete steps; deterministic templates only.
	Checklist []string `json:"checklist,omitempty"`
}

// Integrations holds optional hooks for CI / issue trackers (placeholders).
type Integrations struct {
	JiraIssueKey      string `json:"jira_issue_key,omitempty"`
	PRCommentMarkdown string `json:"pr_comment_markdown,omitempty"`
}

// Enforcement is deterministic merge/review policy derived from score, factors, and context (v2.5+).
type Enforcement struct {
	MergeRecommendation string   `json:"merge_recommendation"` // pass | warn | block
	Rationale           string   `json:"rationale"`
	ReviewStrategy      string   `json:"review_strategy"`
	RequiredValidations []string `json:"required_validations"`
	ReviewRequirements  []string `json:"review_requirements"`
	RoutingHints        []string `json:"routing_hints,omitempty"`
}

// ScoreMath is the explicit, auditable score calculation (v2.3+; report minor v2.4 adds context).
type ScoreMath struct {
	FactorsSubtotal  float64  `json:"factors_subtotal"`  // sum of risk factor points
	ReducersSubtotal float64  `json:"reducers_subtotal"` // sum of reducer points (positive)
	NetBeforeFloor   float64  `json:"net_before_floor"`  // clamp100(factors − reducers)
	FloorMinScore    float64  `json:"floor_min_score"`   // minimum enforced score when floor rules apply; 0 if none
	FloorApplied     bool     `json:"floor_applied"`     // true if net was raised to meet floor
	FloorReasons     []string `json:"floor_reasons,omitempty"`
	FinalScore       float64  `json:"final_score"`
	FinalBand        string   `json:"final_band"`
}

// Result is the full scoring output.
type Result struct {
	Version         int              `json:"version"`
	VersionMinor    int              `json:"version_minor"`
	GeneratedAt     time.Time        `json:"generated_at"`
	BaseRef         string           `json:"base_ref"`
	Signals         Signals          `json:"signals"`
	RiskScore       float64          `json:"risk_score"` // 0–100, higher = riskier (same as ScoreMath.FinalScore)
	RiskBand        string           `json:"risk_band"`
	ScoreMath       ScoreMath        `json:"score_math"`
	Interpretation  string           `json:"interpretation,omitempty"` // plain-English summary
	Factors         []RiskFactor     `json:"factors"`
	Categories      []RiskCategory   `json:"categories,omitempty"`
	Reducers        []RiskReducer    `json:"reducers,omitempty"`
	RequiredActions []RequiredAction `json:"required_actions,omitempty"`
	Mitigations     []Mitigation     `json:"mitigations"`
	Integrations    Integrations     `json:"integrations"`
	// ContextInsights is deterministic contextual intelligence (v2.4+): proximity, concentration, hotspots, intent.
	ContextInsights *riskcontext.ContextInsights `json:"context_insights,omitempty"`
	// Enforcement is merge gating + validations + review routing (v2.5+).
	Enforcement Enforcement `json:"enforcement"`
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

	// Reducers subtract points from the base score.
	ValidationNoteReducerPoints   float64 // strong validation note (E2E/smoke evidence)
	WorkflowPartialReducerPoints  float64 // moderate validation note (staging/CI evidence)
	UnitTestEvidenceReducerPoints float64
	E2ETestEvidenceReducerPoints  float64
	// If only tests changed (no sensitive code domains), reduce overall risk.
	TestOnlyDiffReducerPoints float64
	// If test LOC ratio >= threshold (but not all-test), reduce test confidence risk.
	TestHeavyLOCRatioThreshold float64
	TestHeavyReducerPoints     float64

	// Contextual signals (v2.4+) — small additive factors; see internal/prrisk/context.
	ContextProximityLowPoints   float64
	ContextScatteredPoints      float64
	ContextHotspotPoints        float64
	ContextIntentMismatchPoints float64
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

		ValidationNoteReducerPoints:   6,
		WorkflowPartialReducerPoints:  4,
		UnitTestEvidenceReducerPoints: 5,
		E2ETestEvidenceReducerPoints:  10,
		TestOnlyDiffReducerPoints:     6,
		TestHeavyLOCRatioThreshold:    0.40,
		TestHeavyReducerPoints:        4,

		ContextProximityLowPoints:   5,
		ContextScatteredPoints:      5,
		ContextHotspotPoints:        4,
		ContextIntentMismatchPoints: 6,
	}
}
