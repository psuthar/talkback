package obsworker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// nerdGraphNRQLRequest is the payload for NerdGraph NRQL query.
type nerdGraphNRQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

// nerdGraphResponse matches the NerdGraph response shape for nrql.results.
type nerdGraphResponse struct {
	Data   *nerdGraphData  `json:"data"`
	Errors []nerdGraphErr `json:"errors,omitempty"`
}

type nerdGraphData struct {
	Actor *nerdGraphActor `json:"actor"`
}

type nerdGraphActor struct {
	Account *nerdGraphAccount `json:"account"`
}

type nerdGraphAccount struct {
	NRQL *nerdGraphNRQL `json:"nrql"`
}

type nerdGraphNRQL struct {
	Results []map[string]interface{} `json:"results"`
}

type nerdGraphErr struct {
	Message string `json:"message"`
}

const nrqlQueryTemplate = `query RunNRQL($accountId: Int!, $nrql: String!) {
  actor {
    account(id: $accountId) {
      nrql(query: $nrql) {
        results
      }
    }
  }
}`

// NerdGraphClient calls New Relic NerdGraph for NRQL.
type NerdGraphClient struct {
	Endpoint string
	APIKey   string
	AccountID int
	Client   *http.Client
}

// NewNerdGraphClient builds a client from config (uses default HTTP client and region endpoint).
func NewNerdGraphClient(cfg Config) *NerdGraphClient {
	return NewNerdGraphClientWithHTTP(cfg.NerdGraphEndpoint(), cfg.APIKey, cfg.AccountID, &http.Client{Timeout: defaultTimeout})
}

// NewNerdGraphClientWithHTTP builds a client with explicit endpoint and HTTP client (for tests with httptest).
func NewNerdGraphClientWithHTTP(endpoint string, apiKey string, accountID int, client *http.Client) *NerdGraphClient {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &NerdGraphClient{
		Endpoint:  endpoint,
		APIKey:    apiKey,
		AccountID: accountID,
		Client:    client,
	}
}

// RunNRQL runs a single NRQL query and returns the results array.
// Returns nil, nil when the query returns no rows.
func (n *NerdGraphClient) RunNRQL(nrql string) ([]map[string]interface{}, error) {
	body := nerdGraphNRQLRequest{
		Query: nrqlQueryTemplate,
		Variables: map[string]interface{}{
			"accountId": n.AccountID,
			"nrql":      nrql,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, n.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Key", n.APIKey)

	resp, err := n.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("NerdGraph returned HTTP %d (expected 200): %s", resp.StatusCode, snippet)
	}

	var out nerdGraphResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(out.Errors) > 0 {
		msg := out.Errors[0].Message
		if len(out.Errors) > 1 {
			msg = fmt.Sprintf("%s (and %d more)", msg, len(out.Errors)-1)
		}
		return nil, fmt.Errorf("GraphQL errors: %s", msg)
	}

	if out.Data == nil || out.Data.Actor == nil || out.Data.Actor.Account == nil || out.Data.Actor.Account.NRQL == nil {
		return nil, fmt.Errorf("unexpected response shape: missing data.actor.account.nrql")
	}

	return out.Data.Actor.Account.NRQL.Results, nil
}
