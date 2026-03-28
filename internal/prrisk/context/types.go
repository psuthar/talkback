package riskcontext

// ContextInsights is the structured v2.4 contextual intelligence snapshot (JSON-serializable).
type ContextInsights struct {
	Proximity          ProximityInsight     `json:"proximity"`
	Concentration      ConcentrationInsight `json:"concentration"`
	Hotspots           []HotspotInsight     `json:"hotspots,omitempty"`
	HotspotsSkipReason string               `json:"hotspots_skip_reason,omitempty"` // non-empty when git log was unavailable
	Intent             IntentInsight        `json:"intent"`
}

// ProximityInsight describes test-to-code co-location in this diff.
type ProximityInsight struct {
	Mode                 string  `json:"mode"` // co_located | partial | distant | n_a
	NonTestFiles         int     `json:"non_test_files"`
	WithNearbyTestInDiff int     `json:"with_nearby_test_in_diff"`
	Ratio                float64 `json:"ratio"` // WithNearbyTestInDiff / NonTestFiles (0 if none)
	Detail               string  `json:"detail"`
}

// ConcentrationInsight describes whether churn is focused vs scattered.
type ConcentrationInsight struct {
	Mode       string  `json:"mode"` // focused | balanced | scattered
	TopPrefix  string  `json:"top_prefix,omitempty"`
	TopShare   float64 `json:"top_share,omitempty"` // 0–1 of LOC in top prefix
	HHI        float64 `json:"hhi,omitempty"`       // Herfindahl on prefix LOC shares
	UniqueDirs int     `json:"unique_prefixes"`     // distinct 2-segment prefixes
	Detail     string  `json:"detail"`
}

// HotspotInsight is a path area that appears frequently in recent git history and overlaps this diff.
type HotspotInsight struct {
	Prefix      string `json:"prefix"`
	RecentCount int    `json:"recent_hits"` // times seen in sampled commits
	Detail      string `json:"detail"`
}

// IntentInsight compares PR title/body keywords to domains touched in the diff.
type IntentInsight struct {
	Title              string   `json:"title,omitempty"`
	KeywordsMatched    []string `json:"keywords_matched,omitempty"`
	DomainsExpected    []string `json:"domains_expected,omitempty"`
	DomainsInDiff      []string `json:"domains_in_diff,omitempty"`
	Aligned            bool     `json:"aligned"`
	Mismatch           bool     `json:"mismatch"`
	Detail             string   `json:"detail"`
}

// FactorContribution is a small deterministic score bump from contextual signals.
type FactorContribution struct {
	ID     string
	Label  string
	Points float64
	Detail string
}
