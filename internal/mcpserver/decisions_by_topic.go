// get_decisions_by_topic: find sessions whose structured decision fields mention a topic (SCRUM-64).
package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

const decisionsByTopicSchemaVersion = "1"

type getDecisionsByTopicInput struct {
	Topic string `json:"topic" jsonschema:"Topic or phrase to match against premise, primary_decision, and decision_outcome (case-insensitive substring)"`
	Limit int    `json:"limit,omitempty" jsonschema:"Max sessions to return (default 40, max 100)"`
}

type decisionsByTopicItem struct {
	SessionID           string  `json:"session_id"`
	SessionTitle        string  `json:"session_title"`
	SessionUpdatedAt    string  `json:"session_updated_at,omitempty"`
	Premise             *string `json:"premise,omitempty"`
	PrimaryDecision     *string `json:"primary_decision,omitempty"`
	DecisionOutcome     *string `json:"decision_outcome,omitempty"`
	DecisionStanceCount int     `json:"decision_stance_count"`
}

type getDecisionsByTopicOutput struct {
	SchemaVersion string                 `json:"schema_version"`
	Topic         string                 `json:"topic"`
	Results       []decisionsByTopicItem `json:"results"`
	Note          string                 `json:"note,omitempty"`
}

func registerGetDecisionsByTopic(server *mcp.Server, db *database.DB) {
	const desc = "Returns sessions accessible to the acting user whose premise, primary_decision, or decision_outcome contains the topic as a case-insensitive substring. Each result includes session metadata and stance count; does not return full stance rows (use get_session_decisions per session). Scoped to the same session visibility as search_all_sessions (SCRUM-63/70). Requires DATABASE_URL and acting user."
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetDecisionsByTopic,
		Description: desc,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getDecisionsByTopicInput) (*mcp.CallToolResult, getDecisionsByTopicOutput, error) {
		start := time.Now()
		tool := ToolGetDecisionsByTopic
		defer func() {
			logMCPToolComplete(tool, time.Since(start), nil)
		}()

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, getDecisionsByTopicOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		topic := strings.TrimSpace(in.Topic)
		if topic == "" {
			return nil, getDecisionsByTopicOutput{}, mcpToolErr(400, "topic is required")
		}

		limit := in.Limit
		if limit <= 0 {
			limit = 40
		}
		if limit > database.MaxDecisionTopicResults {
			limit = database.MaxDecisionTopicResults
		}

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, getDecisionsByTopicOutput{}, err
		}
		if user == nil {
			return nil, getDecisionsByTopicOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		sessionIDs, err := db.ListAccessibleSessionIDsForUser(ctx, user)
		if err != nil {
			return nil, getDecisionsByTopicOutput{}, err
		}
		if len(sessionIDs) == 0 {
			return nil, getDecisionsByTopicOutput{
				SchemaVersion: decisionsByTopicSchemaVersion,
				Topic:         topic,
				Results:       nil,
				Note:          "No accessible sessions found for this user.",
			}, nil
		}

		rows, err := db.ListSessionsMatchingDecisionTopic(ctx, sessionIDs, topic, limit)
		if err != nil {
			return nil, getDecisionsByTopicOutput{}, err
		}

		out := make([]decisionsByTopicItem, 0, len(rows))
		for _, r := range rows {
			out = append(out, decisionsByTopicItem{
				SessionID:           r.SessionID.String(),
				SessionTitle:        r.Title,
				SessionUpdatedAt:    r.UpdatedAt.UTC().Format(time.RFC3339),
				Premise:             r.Premise,
				PrimaryDecision:     r.PrimaryDecision,
				DecisionOutcome:     r.DecisionOutcome,
				DecisionStanceCount: r.StanceCount,
			})
		}

		note := ""
		if len(out) == 0 {
			note = "No sessions matched this topic in premise, primary_decision, or decision_outcome."
		}

		return nil, getDecisionsByTopicOutput{
			SchemaVersion: decisionsByTopicSchemaVersion,
			Topic:         topic,
			Results:       out,
			Note:          note,
		}, nil
	})
}
