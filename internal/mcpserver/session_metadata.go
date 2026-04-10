// get_session_metadata tool: read-only session row + owner enrichment; requires DATABASE_URL.
// See package doc (doc.go) for SCRUM-39 wiring and ACL.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

type getSessionMetadataInput struct {
	SessionID string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
}

type getSessionMetadataOutput struct {
	SessionID string              `json:"session_id"`
	Title     string              `json:"title"`
	CreatedAt string              `json:"created_at"`
	Owner     *sessionOwnerOutput `json:"owner,omitempty"`
}

type sessionOwnerOutput struct {
	CreatedBy   *string `json:"created_by,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
}

// Stable MCP tool error_code values (SCRUM-71 / SCRUM-54).
const (
	mcpErrCodeActingUserNotConfigured = "acting_user_not_configured"
	mcpErrCodeSessionAccessDenied     = "session_access_denied"
)

func mcpToolErr(httpStatus int, message string) error {
	return mcpToolErrCode("", httpStatus, message)
}

// mcpToolErrCode returns a JSON-shaped error for MCP tool handlers. When code is non-empty, clients receive error_code (SCRUM-54).
func mcpToolErrCode(code string, httpStatus int, message string) error {
	m := map[string]any{
		"error":       message,
		"http_status": httpStatus,
	}
	if code != "" {
		m["error_code"] = code
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return errors.New(string(b))
}

func registerGetSessionMetadata(server *mcp.Server, db *database.DB) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetSessionMetadata,
		Description: "Returns TalkBack session metadata (title, created_at, owner) for a session ID. Requires DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID; enforces the same access rules as the web app (404 if missing, 403 if not allowed).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getSessionMetadataInput) (*mcp.CallToolResult, getSessionMetadataOutput, error) {
		start := time.Now()
		tool := ToolGetSessionMetadata
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
			return nil, getSessionMetadataOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, getSessionMetadataOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, getSessionMetadataOutput{}, err
		}
		if user == nil {
			return nil, getSessionMetadataOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		session, err := db.GetSession(ctx, sessionID)
		if err != nil || session == nil {
			if err != nil && strings.Contains(err.Error(), "not found") {
				return nil, getSessionMetadataOutput{}, mcpToolErr(404, "session not found")
			}
			if err != nil {
				return nil, getSessionMetadataOutput{}, err
			}
			return nil, getSessionMetadataOutput{}, mcpToolErr(404, "session not found")
		}

		if allowed, err := userMayReadSessionMCP(ctx, db, session, user); err != nil {
			return nil, getSessionMetadataOutput{}, err
		} else if !allowed {
			return nil, getSessionMetadataOutput{}, mcpToolErrCode(mcpErrCodeSessionAccessDenied, 403, "you do not have access to this session")
		}

		out := getSessionMetadataOutput{
			SessionID: session.ID.String(),
			Title:     session.Title,
			CreatedAt: session.CreatedAt.UTC().Format(time.RFC3339),
		}
		owner := &sessionOwnerOutput{CreatedBy: session.CreatedBy}
		if session.CreatedBy != nil && strings.TrimSpace(*session.CreatedBy) != "" {
			if creator, err := db.GetUserByEmail(ctx, *session.CreatedBy); err == nil && creator != nil {
				dn := creator.DisplayName
				owner.DisplayName = &dn
			}
		}
		out.Owner = owner
		return nil, out, nil
	})
}

func userMayReadSessionMCP(ctx context.Context, db *database.DB, session *models.Session, u *models.User) (bool, error) {
	if u == nil {
		return false, nil
	}
	if u.GlobalRole == models.GlobalRoleAdmin {
		return true, nil
	}
	return db.UserCanAccessSession(ctx, session.ID, u)
}
