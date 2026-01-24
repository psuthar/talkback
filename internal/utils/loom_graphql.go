package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// LoomGraphQLClient handles GraphQL requests to Loom's API
type LoomGraphQLClient struct {
	client      *http.Client
	graphqlURL  string
	userAgent   string
}

// NewLoomGraphQLClient creates a new Loom GraphQL client
func NewLoomGraphQLClient() *LoomGraphQLClient {
	return &LoomGraphQLClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		graphqlURL: "https://www.loom.com/graphql",
		userAgent:  "Mozilla/5.0 (compatible; TalkBack/1.0)",
	}
}

// GetVideoSourceRequest represents the GraphQL request for GetVideoSource
type GetVideoSourceRequest struct {
	OperationName string                 `json:"operationName"`
	Variables     GetVideoSourceVariables `json:"variables"`
	Query         string                 `json:"query"`
}

// GetVideoSourceVariables represents the variables for GetVideoSource
type GetVideoSourceVariables struct {
	VideoID         string   `json:"videoId"`
	Password        *string  `json:"password,omitempty"`
	AcceptableMimes []string `json:"acceptableMimes,omitempty"`
}

// GetVideoSourceResponse represents the GraphQL response
type GetVideoSourceResponse struct {
	Data GetVideoSourceData `json:"data"`
	Errors []GraphQLError    `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message string `json:"message"`
}

// GetVideoSourceData represents the data in GetVideoSource response
type GetVideoSourceData struct {
	GetVideo *GetVideoResult `json:"getVideo"`
}

// GetVideoResult represents the video data from Loom (using RegularUserVideo fragment)
type GetVideoResult struct {
	ID                string            `json:"id,omitempty"`
	NullableRawCdnURL *NullableRawCdnURL `json:"nullableRawCdnUrl,omitempty"`
	Typename          string            `json:"__typename,omitempty"`
}

// NullableRawCdnURL represents the CDN URL structure
type NullableRawCdnURL struct {
	URL         string            `json:"url"`
	Credentials *VideoCredentials `json:"credentials,omitempty"`
	Typename    string            `json:"__typename,omitempty"`
}

// VideoCredentials represents signed URL credentials (if required)
// Note: Field names match GraphQL response (Policy, Signature, KeyPairId)
type VideoCredentials struct {
	Policy    string `json:"Policy"`
	Signature string `json:"Signature"`
	KeyPairID string `json:"KeyPairId"`
	Typename  string `json:"__typename,omitempty"`
}

// ResolveVideoSource resolves a Loom video to a downloadable URL via GraphQL
// Uses the exact query structure from Loom's browser implementation
// By default, prefers MP4/WEBM over streaming formats
func (c *LoomGraphQLClient) ResolveVideoSource(ctx context.Context, videoID string, password *string) (*VideoSourceResult, error) {
	return c.ResolveVideoSourceWithFormats(ctx, videoID, password, []string{"MP4", "WEBM", "DASH", "M3U8"})
}

// ResolveVideoSourceWithFormats resolves a Loom video with specific acceptable MIME types
// This allows requesting only certain formats (e.g., MP4-only for Whisper compatibility)
func (c *LoomGraphQLClient) ResolveVideoSourceWithFormats(ctx context.Context, videoID string, password *string, acceptableMimes []string) (*VideoSourceResult, error) {
	// GraphQL query matching Loom's exact structure from browser
	query := `
		query GetVideoSource($videoId: ID!, $password: String, $acceptableMimes: [CloudfrontVideoAcceptableMime]) {
			getVideo(id: $videoId, password: $password) {
				... on RegularUserVideo {
					id
					nullableRawCdnUrl(acceptableMimes: $acceptableMimes, password: $password) {
						url
						credentials {
							Policy
							Signature
							KeyPairId
							__typename
						}
						__typename
					}
					__typename
				}
				__typename
			}
		}
	`

	// Default to preferring MP4/WEBM if not specified
	if len(acceptableMimes) == 0 {
		acceptableMimes = []string{"MP4", "WEBM", "DASH", "M3U8"}
	}

	variables := GetVideoSourceVariables{
		VideoID:         videoID,
		Password:        password,
		AcceptableMimes: acceptableMimes,
	}

	reqBody := GetVideoSourceRequest{
		OperationName: "GetVideoSource",
		Variables:     variables,
		Query:         query,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.graphqlURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	// Optionally add cookies/session token here if available
	// For public videos, no auth may be needed

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GraphQL request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Loom GraphQL returned status %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("GraphQL request failed with status %d", resp.StatusCode)
	}

	var gqlResp GetVideoSourceResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	// Check for GraphQL errors
	if len(gqlResp.Errors) > 0 {
		errMsg := gqlResp.Errors[0].Message
		for i := 1; i < len(gqlResp.Errors); i++ {
			errMsg += "; " + gqlResp.Errors[i].Message
		}
		return nil, fmt.Errorf("GraphQL errors: %s", errMsg)
	}

	if gqlResp.Data.GetVideo == nil {
		return nil, fmt.Errorf("video not found or not accessible")
	}

	result := gqlResp.Data.GetVideo

	if result.NullableRawCdnURL == nil || result.NullableRawCdnURL.URL == "" {
		return nil, fmt.Errorf("no CDN URL available - video may be private or require authentication")
	}

	mediaURL := result.NullableRawCdnURL.URL
	credentials := result.NullableRawCdnURL.Credentials

	// Check if credentials are present (indicates signed URL)
	requiresAuth := credentials != nil && (credentials.Policy != "" || credentials.Signature != "")

	return &VideoSourceResult{
		MediaURL:     mediaURL,
		RequiresAuth: requiresAuth,
		Credentials:  credentials,
	}, nil
}

// VideoSourceResult represents the result of resolving a video source
type VideoSourceResult struct {
	MediaURL     string
	RequiresAuth bool
	Credentials  *VideoCredentials
}
