package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Context key for the optional acting user (future session ACL). Set when auth succeeds
// and TALKBACK_MCP_ACTING_USER_ID is a valid UUID.
type actingUserCtxKey struct{}

// ActingUserID returns the configured TalkBack user ID for this MCP deployment, if any.
func ActingUserID(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(actingUserCtxKey{}).(uuid.UUID)
	if !ok || v == uuid.Nil {
		return uuid.UUID{}, false
	}
	return v, true
}

// Auth holds accepted API keys and optional acting user for future ACL alignment.
type Auth struct {
	acceptedKeys []string
	actingUserID uuid.UUID
	// keyToUser maps a client API key string (exact match to an entry in acceptedKeys) to a TalkBack
	// users.id. Loaded from TALKBACK_MCP_KEY_USER_MAP_JSON (SCRUM-70). When nil or empty, only
	// actingUserID is used for tools/call identity (Phase 1 behavior).
	keyToUser map[string]uuid.UUID
	// RequireClientKey controls whether each tools/call must include a key matching acceptedKeys.
	// When false (set via TALKBACK_MCP_REQUIRE_CLIENT_KEY=false), only the server env is configured;
	// use for Cursor/Claude Code hosts that cannot attach _meta on tool calls. Default is true.
	RequireClientKey bool
}

// LoadAuthFromEnv loads TALKBACK_MCP_API_KEY (required, non-empty) and optional
// TALKBACK_MCP_ACTING_USER_ID (UUID). Multiple keys may be comma-separated for rotation.
//
// Optional TALKBACK_MCP_KEY_USER_MAP_JSON: JSON object mapping each API key string to a TalkBack user
// UUID (e.g. {"sk-alice":"550e8400-...","sk-bob":"6ba7b810-..."}). Every object key must exactly
// match one entry from TALKBACK_MCP_API_KEY (after comma-splitting and trim). Keys not listed in the
// map still authenticate but fall back to TALKBACK_MCP_ACTING_USER_ID when that is set (backward
// compatible). Per-key identity is applied only when RequireClientKey is true (strict mode); IDE
// mode cannot distinguish callers, so the map is ignored and actingUserID alone applies.
//
// TALKBACK_MCP_REQUIRE_CLIENT_KEY: when unset or true, clients must send a matching key per tools/call.
// Set to false so only the server process env is required (weaker; typical for local IDE MCP).
func LoadAuthFromEnv() (Auth, error) {
	raw := strings.TrimSpace(os.Getenv("TALKBACK_MCP_API_KEY"))
	if raw == "" {
		return Auth{}, fmt.Errorf("TALKBACK_MCP_API_KEY is required (set a shared secret for MCP tool calls)")
	}
	keys := splitCommaKeys(raw)
	if len(keys) == 0 {
		return Auth{}, fmt.Errorf("TALKBACK_MCP_API_KEY is empty after parsing")
	}
	var uid uuid.UUID
	if s := strings.TrimSpace(os.Getenv("TALKBACK_MCP_ACTING_USER_ID")); s != "" {
		u, err := uuid.Parse(s)
		if err != nil {
			return Auth{}, fmt.Errorf("TALKBACK_MCP_ACTING_USER_ID: %w", err)
		}
		uid = u
	}
	keyToUser, err := loadKeyUserMapFromEnv(keys)
	if err != nil {
		return Auth{}, err
	}
	requireClientKey := envBoolDefaultTrue(os.Getenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY"))
	return Auth{
		acceptedKeys:     keys,
		actingUserID:     uid,
		keyToUser:        keyToUser,
		RequireClientKey: requireClientKey,
	}, nil
}

func loadKeyUserMapFromEnv(acceptedKeys []string) (map[string]uuid.UUID, error) {
	raw := strings.TrimSpace(os.Getenv("TALKBACK_MCP_KEY_USER_MAP_JSON"))
	if raw == "" {
		return nil, nil
	}
	var rawMap map[string]string
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		return nil, fmt.Errorf("TALKBACK_MCP_KEY_USER_MAP_JSON: %w", err)
	}
	if len(rawMap) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(acceptedKeys))
	for _, k := range acceptedKeys {
		allowed[k] = struct{}{}
	}
	out := make(map[string]uuid.UUID, len(rawMap))
	for k, v := range rawMap {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("TALKBACK_MCP_KEY_USER_MAP_JSON: empty key")
		}
		if _, ok := allowed[k]; !ok {
			return nil, fmt.Errorf("TALKBACK_MCP_KEY_USER_MAP_JSON: key is not listed in TALKBACK_MCP_API_KEY")
		}
		u, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("TALKBACK_MCP_KEY_USER_MAP_JSON: user id for key: %w", err)
		}
		if u == uuid.Nil {
			return nil, fmt.Errorf("TALKBACK_MCP_KEY_USER_MAP_JSON: nil UUID not allowed")
		}
		out[k] = u
	}
	return out, nil
}

// HasPerKeyActingUsers reports whether TALKBACK_MCP_KEY_USER_MAP_JSON was loaded with at least one mapping.
func (a Auth) HasPerKeyActingUsers() bool {
	return len(a.keyToUser) > 0
}

// ActingUserForClientKey resolves the TalkBack user for a validated client API key (strict mode).
// Precondition: clientKey matched ValidKey. Returns (uuid, true) when an acting user is configured
// for this key or via global TALKBACK_MCP_ACTING_USER_ID fallback.
func (a Auth) ActingUserForClientKey(clientKey string) (uuid.UUID, bool) {
	if len(a.keyToUser) > 0 {
		if u, ok := a.keyToUser[clientKey]; ok && u != uuid.Nil {
			return u, true
		}
	}
	if a.actingUserID != uuid.Nil {
		return a.actingUserID, true
	}
	return uuid.UUID{}, false
}

func envBoolDefaultTrue(s string) bool {
	if strings.TrimSpace(s) == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}

func splitCommaKeys(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ExtractClientAPIKey returns a client-supplied API key from tool call metadata or HTTP extras.
func ExtractClientAPIKey(meta mcp.Meta, extra *mcp.RequestExtra) string {
	if k := extractFromMeta(meta); k != "" {
		return k
	}
	if extra != nil && extra.Header != nil {
		if h := strings.TrimSpace(extra.Header.Get("Authorization")); h != "" {
			return parseBearer(h)
		}
		if h := strings.TrimSpace(extra.Header.Get("X-API-Key")); h != "" {
			return h
		}
	}
	return ""
}

func extractFromMeta(meta mcp.Meta) string {
	if len(meta) == 0 {
		return ""
	}
	if v, ok := meta["talkback"].(map[string]any); ok {
		if s, ok := stringField(v, "apiKey"); ok {
			return s
		}
		if s, ok := stringField(v, "api_key"); ok {
			return s
		}
	}
	if s, ok := stringField(meta, "talkbackApiKey"); ok {
		return s
	}
	if s, ok := stringField(meta, "authorization"); ok {
		return parseBearer(s)
	}
	if s, ok := stringField(meta, "Authorization"); ok {
		return parseBearer(s)
	}
	return ""
}

func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func parseBearer(h string) string {
	h = strings.TrimSpace(h)
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return h
}

// ValidKey reports whether clientKey matches one of the configured accepted keys (constant-time per candidate of equal length).
func (a Auth) ValidKey(clientKey string) bool {
	if clientKey == "" {
		return false
	}
	for _, want := range a.acceptedKeys {
		if constantTimeEqual(clientKey, want) {
			return true
		}
	}
	return false
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
