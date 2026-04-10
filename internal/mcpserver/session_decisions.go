// get_session_decisions and get_decisions tools (SCRUM-55, SCRUM-60): same handler — persisted premise, primary_decision, decision_outcome, and decision_stances only — no LLM extraction.
// JSON contract (field names, types, versioning): docs/mcp-session-decisions-schema.md and docs/schemas/mcp-session-decisions-v1.schema.json (SCRUM-57).
// Empty/partial success vs errors: docs/mcp-structured-intelligence-fallbacks.md (SCRUM-62).
package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// Session decisions MCP output schema version (SCRUM-55 / SCRUM-57); bump when fields change.
const sessionDecisionsSchemaVersion = "1"

type getSessionDecisionsInput struct {
	SessionID string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
}

type getSessionDecisionsOutput struct {
	SchemaVersion   string                      `json:"schema_version"`
	SessionID       string                      `json:"session_id"`
	Premise         *string                     `json:"premise,omitempty"`
	PrimaryDecision *string                     `json:"primary_decision,omitempty"`
	DecisionOutcome *string                     `json:"decision_outcome,omitempty"`
	DecisionStances []sessionDecisionStanceItem `json:"decision_stances"`
}

// sessionDecisionStanceItem is one persisted stance row (+ submitter email), aligned with HTTP GET /api/sessions/{id}/stances responses.
type sessionDecisionStanceItem struct {
	ID        string  `json:"id"`
	SessionID string  `json:"session_id"`
	UserID    string  `json:"user_id"`
	Stance    string  `json:"stance"`
	Rationale *string `json:"rationale,omitempty"`
	UserEmail string  `json:"user_email"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func registerGetSessionDecisionsTools(server *mcp.Server, db *database.DB) {
	const desc = "Returns persisted structured decision fields for a session: premise, primary_decision, decision_outcome, and decision_stances (participant stances with user_email). v1: database fields only — no LLM inference. Requires DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID; same access rules as get_session_metadata (404 if session missing, 403 if not allowed). Output includes schema_version for contract stability."
	registerGetSessionDecisionsTool(server, db, ToolGetSessionDecisions, desc)
	registerGetSessionDecisionsTool(server, db, ToolGetDecisions, "Same behavior as "+ToolGetSessionDecisions+" (SCRUM-60). "+desc)
}

func registerGetSessionDecisionsTool(server *mcp.Server, db *database.DB, toolName string, description string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        toolName,
		Description: description,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getSessionDecisionsInput) (*mcp.CallToolResult, getSessionDecisionsOutput, error) {
		start := time.Now()
		tool := toolName
		var logSessionID string
		defer func() {
			extras := map[string]string{}
			if logSessionID != "" {
				extras["session_id"] = logSessionID
			}
			logMCPToolComplete(tool, time.Since(start), extras)
		}()

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, getSessionDecisionsOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, getSessionDecisionsOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, getSessionDecisionsOutput{}, err
		}
		if user == nil {
			return nil, getSessionDecisionsOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		session, err := db.GetSession(ctx, sessionID)
		if err != nil || session == nil {
			if err != nil && strings.Contains(err.Error(), "not found") {
				return nil, getSessionDecisionsOutput{}, mcpToolErr(404, "session not found")
			}
			if err != nil {
				return nil, getSessionDecisionsOutput{}, err
			}
			return nil, getSessionDecisionsOutput{}, mcpToolErr(404, "session not found")
		}

		if allowed, err := userMayReadSessionMCP(ctx, db, session, user); err != nil {
			return nil, getSessionDecisionsOutput{}, err
		} else if !allowed {
			return nil, getSessionDecisionsOutput{}, mcpToolErrCode(mcpErrCodeSessionAccessDenied, 403, "you do not have access to this session")
		}

		stances, err := db.GetStancesBySessionID(ctx, session.ID)
		if err != nil {
			return nil, getSessionDecisionsOutput{}, err
		}
		out := buildGetSessionDecisionsOutput(session, stances)
		return nil, out, nil
	})
}

func buildGetSessionDecisionsOutput(session *models.Session, stances []*models.DecisionStanceWithUser) getSessionDecisionsOutput {
	items := make([]sessionDecisionStanceItem, 0, len(stances))
	for _, sw := range stances {
		if sw == nil {
			continue
		}
		items = append(items, sessionDecisionStanceItem{
			ID:        sw.ID.String(),
			SessionID: sw.SessionID.String(),
			UserID:    sw.UserID.String(),
			Stance:    sw.Stance,
			Rationale: sw.Rationale,
			UserEmail: sw.UserEmail,
			CreatedAt: sw.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: sw.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return getSessionDecisionsOutput{
		SchemaVersion:   sessionDecisionsSchemaVersion,
		SessionID:       session.ID.String(),
		Premise:         session.Premise,
		PrimaryDecision: session.PrimaryDecision,
		DecisionOutcome: session.DecisionOutcome,
		DecisionStances: items,
	}
}
