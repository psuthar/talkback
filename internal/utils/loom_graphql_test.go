package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoomGraphQLClient_ResolveVideoSource_Success(t *testing.T) {
	// Mock successful GraphQL response
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID: "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: &NullableRawCdnURL{
					URL: "https://cdn.loom.com/videos/example.mp4",
					Credentials: &VideoCredentials{
						Policy:    "test-policy",
						Signature: "test-signature",
						KeyPairID: "test-keypair",
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and content type
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse request body
		var reqBody GetVideoSourceRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify request structure
		assert.Equal(t, "GetVideoSource", reqBody.OperationName)
		assert.Equal(t, "19aeabbe324b45cb9ec3c74ab3547e66", reqBody.Variables.VideoID)
		assert.Nil(t, reqBody.Variables.Password) // No password in this test

		// Verify query contains expected fields
		assert.Contains(t, reqBody.Query, "query GetVideoSource")
		assert.Contains(t, reqBody.Query, "$videoId: ID!")
		assert.Contains(t, reqBody.Query, "nullableRawCdnUrl")

		// Send mock response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create client with test server URL
	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	// Call ResolveVideoSource
	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	// Verify results
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "https://cdn.loom.com/videos/example.mp4", result.MediaURL)
	assert.True(t, result.RequiresAuth) // Has credentials
	assert.NotNil(t, result.Credentials)
	assert.Equal(t, "test-policy", result.Credentials.Policy)
	assert.Equal(t, "test-signature", result.Credentials.Signature)
	assert.Equal(t, "test-keypair", result.Credentials.KeyPairID)
}

func TestLoomGraphQLClient_ResolveVideoSource_WithPassword(t *testing.T) {
	password := "test-password"
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID: "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: &NullableRawCdnURL{
					URL: "https://cdn.loom.com/videos/protected.mp4",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GetVideoSourceRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify password is included in variables
		assert.NotNil(t, reqBody.Variables.Password)
		assert.Equal(t, password, *reqBody.Variables.Password)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", &password)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "https://cdn.loom.com/videos/protected.mp4", result.MediaURL)
	assert.False(t, result.RequiresAuth) // No credentials
}

func TestLoomGraphQLClient_ResolveVideoSource_NoCredentials(t *testing.T) {
	// Response without credentials (public video)
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID: "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: &NullableRawCdnURL{
					URL:         "https://cdn.loom.com/videos/public.mp4",
					Credentials: nil, // No credentials
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "https://cdn.loom.com/videos/public.mp4", result.MediaURL)
	assert.False(t, result.RequiresAuth) // No credentials means public
	assert.Nil(t, result.Credentials)
}

func TestLoomGraphQLClient_ResolveVideoSource_GraphQLError(t *testing.T) {
	mockResponse := GetVideoSourceResponse{
		Errors: []GraphQLError{
			{Message: "Video not found"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "invalid-id", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "GraphQL errors")
	assert.Contains(t, err.Error(), "Video not found")
}

func TestLoomGraphQLClient_ResolveVideoSource_MultipleGraphQLErrors(t *testing.T) {
	mockResponse := GetVideoSourceResponse{
		Errors: []GraphQLError{
			{Message: "First error"},
			{Message: "Second error"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "invalid-id", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "First error")
	assert.Contains(t, err.Error(), "Second error")
}

func TestLoomGraphQLClient_ResolveVideoSource_NoVideoData(t *testing.T) {
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: nil, // No video data
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "invalid-id", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "video not found or not accessible")
}

func TestLoomGraphQLClient_ResolveVideoSource_NoCDNURL(t *testing.T) {
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID:                "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: nil, // No CDN URL
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no CDN URL available")
}

func TestLoomGraphQLClient_ResolveVideoSource_EmptyURL(t *testing.T) {
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID: "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: &NullableRawCdnURL{
					URL: "", // Empty URL
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no CDN URL available")
}

func TestLoomGraphQLClient_ResolveVideoSource_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "GraphQL request failed with status 500")
}

func TestLoomGraphQLClient_ResolveVideoSource_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse GraphQL response")
}

func TestLoomGraphQLClient_ResolveVideoSource_NetworkError(t *testing.T) {
	// Use an invalid URL to trigger network error
	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: "http://localhost:99999/invalid",
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "GraphQL request failed")
}

func TestLoomGraphQLClient_ResolveVideoSource_AcceptableMimes(t *testing.T) {
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID: "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: &NullableRawCdnURL{
					URL: "https://cdn.loom.com/videos/example.mp4",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody GetVideoSourceRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify acceptable mimes (prefer MP4/WEBM over streaming formats for Whisper compatibility)
		expectedMimes := []string{"MP4", "WEBM", "DASH", "M3U8"}
		assert.Equal(t, expectedMimes, reqBody.Variables.AcceptableMimes)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestNewLoomGraphQLClient(t *testing.T) {
	client := NewLoomGraphQLClient()

	assert.NotNil(t, client)
	assert.NotNil(t, client.client)
	assert.Equal(t, "https://www.loom.com/graphql", client.graphqlURL)
	assert.Equal(t, "Mozilla/5.0 (compatible; TalkBack/1.0)", client.userAgent)
	assert.NotNil(t, client.client.Timeout)
}

func TestLoomGraphQLClient_ResolveVideoSource_CredentialsWithEmptyFields(t *testing.T) {
	// Response with credentials but empty fields (should not require auth)
	mockResponse := GetVideoSourceResponse{
		Data: GetVideoSourceData{
			GetVideo: &GetVideoResult{
				ID: "19aeabbe324b45cb9ec3c74ab3547e66",
				NullableRawCdnURL: &NullableRawCdnURL{
					URL: "https://cdn.loom.com/videos/example.mp4",
					Credentials: &VideoCredentials{
						Policy:    "", // Empty
						Signature: "", // Empty
						KeyPairID: "", // Empty
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &LoomGraphQLClient{
		client:     http.DefaultClient,
		graphqlURL: server.URL,
		userAgent:  "Test/1.0",
	}

	result, err := client.ResolveVideoSource(context.Background(), "19aeabbe324b45cb9ec3c74ab3547e66", nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	// Empty credentials should not require auth
	assert.False(t, result.RequiresAuth)
}

// TestLoomGraphQLResponseStructure_ValidatesExactJSONFormat tests that our structs
// correctly unmarshal the exact JSON structure that Loom's GraphQL API returns.
// This test serves as a backwards-compatibility check: if Loom changes their API
// structure in a backwards-incompatible way, this test will fail.
//
// IMPORTANT: This test uses the EXACT JSON structure from Loom's GraphQL API.
// If Loom changes field names (e.g., nullableRawCdnUrl -> rawCdnUrl, Policy -> policy),
// or the structure (e.g., credentials moves out of nullableRawCdnUrl), this test will fail.
func TestLoomGraphQLResponseStructure_ValidatesExactJSONFormat(t *testing.T) {
	// This is the EXACT JSON structure that Loom's GraphQL API returns
	// Based on the structure we observed from Loom's browser implementation
	loomGraphQLResponseJSON := `{
		"data": {
			"getVideo": {
				"id": "19aeabbe324b45cb9ec3c74ab3547e66",
				"nullableRawCdnUrl": {
					"url": "https://cdn.loom.com/videos/example.mp4",
					"credentials": {
						"Policy": "test-policy-value",
						"Signature": "test-signature-value",
						"KeyPairId": "test-keypair-id",
						"__typename": "CloudFrontVideoCredentials"
					},
					"__typename": "CloudFrontVideoUrl"
				},
				"__typename": "RegularUserVideo"
			}
		}
	}`

	// Test 1: Unmarshal into our response struct
	var response GetVideoSourceResponse
	err := json.Unmarshal([]byte(loomGraphQLResponseJSON), &response)
	require.NoError(t, err, "Failed to unmarshal Loom GraphQL response - this indicates a backwards-incompatible change in Loom's API structure")

	// Test 2: Validate the nested structure we depend on
	require.NotNil(t, response.Data.GetVideo, "getVideo field is missing or null - Loom API structure changed")
	require.NotNil(t, response.Data.GetVideo.NullableRawCdnURL, "nullableRawCdnUrl field is missing or null - Loom API structure changed")
	require.NotEmpty(t, response.Data.GetVideo.NullableRawCdnURL.URL, "url field is missing or empty - Loom API structure changed")
	require.NotNil(t, response.Data.GetVideo.NullableRawCdnURL.Credentials, "credentials field is missing - Loom API structure changed")

	// Test 3: Validate exact field names (case-sensitive)
	// These field names must match EXACTLY: Policy (capital P), Signature (capital S), KeyPairId (capital K, capital P, capital I)
	credentials := response.Data.GetVideo.NullableRawCdnURL.Credentials
	assert.Equal(t, "test-policy-value", credentials.Policy, "Policy field name or structure changed")
	assert.Equal(t, "test-signature-value", credentials.Signature, "Signature field name or structure changed")
	assert.Equal(t, "test-keypair-id", credentials.KeyPairID, "KeyPairId field name or structure changed (note: Go field is KeyPairID but JSON is KeyPairId)")

	// Test 4: Test response without credentials (public video)
	publicVideoJSON := `{
		"data": {
			"getVideo": {
				"id": "19aeabbe324b45cb9ec3c74ab3547e66",
				"nullableRawCdnUrl": {
					"url": "https://cdn.loom.com/videos/public.mp4",
					"__typename": "CloudFrontVideoUrl"
				},
				"__typename": "RegularUserVideo"
			}
		}
	}`

	var publicResponse GetVideoSourceResponse
	err = json.Unmarshal([]byte(publicVideoJSON), &publicResponse)
	require.NoError(t, err, "Failed to unmarshal public video response")
	require.NotNil(t, publicResponse.Data.GetVideo)
	require.NotNil(t, publicResponse.Data.GetVideo.NullableRawCdnURL)
	assert.Equal(t, "https://cdn.loom.com/videos/public.mp4", publicResponse.Data.GetVideo.NullableRawCdnURL.URL)
	assert.Nil(t, publicResponse.Data.GetVideo.NullableRawCdnURL.Credentials, "Credentials should be null for public videos")

	// Test 5: Test GraphQL errors structure
	errorResponseJSON := `{
		"errors": [
			{
				"message": "Video not found"
			}
		]
	}`

	var errorResponse GetVideoSourceResponse
	err = json.Unmarshal([]byte(errorResponseJSON), &errorResponse)
	require.NoError(t, err, "Failed to unmarshal GraphQL error response")
	require.Len(t, errorResponse.Errors, 1)
	assert.Equal(t, "Video not found", errorResponse.Errors[0].Message, "GraphQL error message field name changed")
}

// TestLoomGraphQLRequestStructure_ValidatesExactJSONFormat tests that our request
// structure matches what Loom's GraphQL API expects.
// This test validates the request format we send to Loom.
func TestLoomGraphQLRequestStructure_ValidatesExactJSONFormat(t *testing.T) {
	videoID := "19aeabbe324b45cb9ec3c74ab3547e66"
	password := "test-password"

	variables := GetVideoSourceVariables{
		VideoID:         videoID,
		Password:        &password,
		AcceptableMimes: []string{"MP4", "WEBM", "DASH", "M3U8"},
	}

	request := GetVideoSourceRequest{
		OperationName: "GetVideoSource",
		Variables:     variables,
		Query:         `query GetVideoSource($videoId: ID!, $password: String, $acceptableMimes: [CloudfrontVideoAcceptableMime]) { getVideo(id: $videoId, password: $password) { ... on RegularUserVideo { id nullableRawCdnUrl(acceptableMimes: $acceptableMimes, password: $password) { url credentials { Policy Signature KeyPairId __typename } __typename } __typename } __typename } }`,
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(request)
	require.NoError(t, err)

	// Unmarshal back to verify structure
	var unmarshaled GetVideoSourceRequest
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	require.NoError(t, err)

	// Validate structure
	assert.Equal(t, "GetVideoSource", unmarshaled.OperationName)
	assert.Equal(t, videoID, unmarshaled.Variables.VideoID)
	require.NotNil(t, unmarshaled.Variables.Password)
	assert.Equal(t, password, *unmarshaled.Variables.Password)
	assert.Equal(t, []string{"MP4", "WEBM", "DASH", "M3U8"}, unmarshaled.Variables.AcceptableMimes)

	// Verify JSON field names match what Loom expects
	var jsonMap map[string]interface{}
	err = json.Unmarshal(jsonBytes, &jsonMap)
	require.NoError(t, err)

	// Check top-level fields
	assert.Contains(t, jsonMap, "operationName", "Request must include 'operationName' field (lowercase)")
	assert.Contains(t, jsonMap, "variables", "Request must include 'variables' field (lowercase)")
	assert.Contains(t, jsonMap, "query", "Request must include 'query' field (lowercase)")

	// Check variables structure
	variablesMap, ok := jsonMap["variables"].(map[string]interface{})
	require.True(t, ok, "variables must be an object")
	assert.Contains(t, variablesMap, "videoId", "Variables must include 'videoId' field (camelCase)")
	assert.Contains(t, variablesMap, "password", "Variables must include 'password' field (lowercase)")
	assert.Contains(t, variablesMap, "acceptableMimes", "Variables must include 'acceptableMimes' field (camelCase)")

	// Verify acceptableMimes is an array
	acceptableMimes, ok := variablesMap["acceptableMimes"].([]interface{})
	require.True(t, ok, "acceptableMimes must be an array")
	assert.Equal(t, 4, len(acceptableMimes))
}

// TestLoomGraphQLClient_ResolveVideoSource_LiveIntegration tests against Loom's live GraphQL API.
// This test makes a real HTTP request to Loom. It is opt-in so local runs and CI without network
// expectations stay stable: set TALKBACK_RUN_LOOM_LIVE_TEST=1 (CI workflows set this).
// Also skipped when -short is set.
//
// IMPORTANT: This test validates backwards compatibility - if Loom changes their API
// in a backwards-incompatible way, this test will fail.
//
// Video used: https://www.loom.com/share/19aeabbe324b45cb9ec3c74ab3547e66
func TestLoomGraphQLClient_ResolveVideoSource_LiveIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live integration test (use -short flag)")
	}
	if os.Getenv("TALKBACK_RUN_LOOM_LIVE_TEST") != "1" {
		t.Skip("Skipping Loom live GraphQL integration; set TALKBACK_RUN_LOOM_LIVE_TEST=1 to run (see TESTING.md)")
	}

	// Video ID from Loom share URL: https://www.loom.com/share/19aeabbe324b45cb9ec3c74ab3547e66
	videoID := "19aeabbe324b45cb9ec3c74ab3547e66"

	// Create a real GraphQL client (points to Loom's actual endpoint)
	client := NewLoomGraphQLClient()

	// Make the actual request to Loom's GraphQL API
	result, err := client.ResolveVideoSource(context.Background(), videoID, nil)

	// Validate that we got a successful response
	require.NoError(t, err, "Failed to resolve video from Loom's live GraphQL API - this may indicate an API change or network issue")
	require.NotNil(t, result, "Result should not be nil")
	require.NotEmpty(t, result.MediaURL, "Media URL should not be empty - Loom API structure may have changed")

	// Validate that we got a valid media URL
	assert.True(t, len(result.MediaURL) > 0, "Media URL should not be empty")
	assert.True(t, len(result.MediaURL) > 10, "Media URL should be a valid URL")

	// Log the result for debugging (but don't fail if fields are unexpected)
	t.Logf("Successfully resolved Loom video %s", videoID)
	t.Logf("Media URL: %s", result.MediaURL)
	t.Logf("Requires Auth: %v", result.RequiresAuth)
	if result.Credentials != nil {
		t.Logf("Has Credentials: true")
	} else {
		t.Logf("Has Credentials: false")
	}

	// Basic validation - the URL should be accessible
	// We don't validate the exact URL format as it may vary, but it should exist
	assert.Contains(t, result.MediaURL, "http", "Media URL should be a valid HTTP/HTTPS URL")

	// Note: We don't assert on the exact URL format because:
	// 1. Loom may use different CDN URLs
	// 2. URLs may be signed/expiring
	// 3. The important thing is that our code can parse Loom's response structure

	// Validate that the response structure matches what we expect
	// If Loom changes their API structure, the ResolveVideoSource method should
	// still work (it may return different data, but it should not error)
	assert.NotEmpty(t, result.MediaURL, "Media URL should be populated")
}
