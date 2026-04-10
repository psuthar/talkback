// Maps OpenAI / embedding failures to stable http_status + error_code for MCP clients (SCRUM-54).
package mcpserver

import (
	"log"
	"strings"
)

// mcpClassifyEmbeddingError maps an Embed error to a client-safe message and machine-readable error_code.
// Raw upstream errors are not echoed (avoid leaking API details).
func mcpClassifyEmbeddingError(err error) (code string, httpStatus int, clientMsg string) {
	if err == nil {
		return "", 200, ""
	}
	s := strings.ToLower(err.Error())
	if (strings.Contains(s, "openai_api_key") && strings.Contains(s, "not set")) ||
		(strings.Contains(s, "environment variable is not set") && strings.Contains(s, "openai")) {
		return "openai_not_configured", 503, "OpenAI is not configured (set OPENAI_API_KEY); embeddings cannot be generated."
	}
	if strings.Contains(s, "401") || strings.Contains(s, "invalid api key") || strings.Contains(s, "incorrect api key") {
		return "openai_auth_failed", 503, "Embedding provider rejected the API key."
	}
	if strings.Contains(s, "429") || strings.Contains(s, "rate limit") {
		return "openai_rate_limited", 429, "Embedding provider rate limit reached; retry after a short backoff."
	}
	if strings.Contains(s, "insufficient_quota") || strings.Contains(s, "exceeded your current quota") || strings.Contains(s, "billing") {
		return "openai_quota_exceeded", 429, "Embedding provider quota exceeded; check account billing or limits."
	}
	log.Printf("mcp event=embedding_error error_code=embedding_failed detail=%q", err.Error())
	return "embedding_failed", 503, "Embedding request failed."
}

func mcpClassifyIndexError(err error) (code string, clientMsg string) {
	if err == nil {
		return "", ""
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "deadline") || strings.Contains(s, "timeout") || strings.Contains(s, "context canceled") {
		return "index_timeout", "Session index is not ready (timeout). Retry shortly or check ingestion status."
	}
	return "index_unavailable", "Session index is unavailable: " + err.Error()
}
