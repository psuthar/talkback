// list_sessions tool: enumerate sessions readable by the acting user (same ACL as GET /api/sessions).
package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

type listSessionsInput struct {
	Limit  *int `json:"limit,omitempty" jsonschema:"Optional max rows (same semantics as GET /api/sessions?limit=)"`
	Offset *int `json:"offset,omitempty" jsonschema:"Optional offset when limit is set (non-negative)"`
}

// listSessionsOutput uses JSON object shapes for sessions (not *models.Session) so MCP structured-output
// schema validation accepts the same payload as GET /api/sessions (uuid fields as strings).
type listSessionsOutput struct {
	Sessions []listSessionsRow `json:"sessions"`
}

type listSessionsRow struct {
	Session map[string]any `json:"session"`
	MyRole  string         `json:"my_role"`
}

func registerListSessions(server *mcp.Server, db *database.DB) {
	const desc = "Lists TalkBack sessions the acting user may access, with my_role per row (admin | creator | participant). Same rules as GET /api/sessions. Optional limit and offset match HTTP query semantics (pagination applies only when limit > 0). Requires DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID (or per-key map in strict mode)."
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolListSessions,
		Description: desc,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listSessionsInput) (*mcp.CallToolResult, listSessionsOutput, error) {
		start := time.Now()
		tool := ToolListSessions
		defer func() {
			logMCPToolComplete(tool, time.Since(start), nil)
		}()

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, listSessionsOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, listSessionsOutput{}, err
		}
		if user == nil {
			return nil, listSessionsOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		rows, err := db.ListSessionsWithRolesForUser(ctx, user)
		if err != nil {
			return nil, listSessionsOutput{}, err
		}

		if in.Limit != nil && *in.Limit > 0 {
			off := 0
			if in.Offset != nil && *in.Offset > 0 {
				off = *in.Offset
			}
			rows = database.ApplySessionListPagination(rows, *in.Limit, off)
		}

		out := listSessionsOutput{Sessions: make([]listSessionsRow, 0, len(rows))}
		for _, r := range rows {
			b, err := json.Marshal(r.Session)
			if err != nil {
				return nil, listSessionsOutput{}, err
			}
			var obj map[string]any
			if err := json.Unmarshal(b, &obj); err != nil {
				return nil, listSessionsOutput{}, err
			}
			out.Sessions = append(out.Sessions, listSessionsRow{Session: obj, MyRole: r.MyRole})
		}
		return nil, out, nil
	})
}
