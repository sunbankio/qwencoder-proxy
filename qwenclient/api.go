package qwenclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"qwenproxy/auth"
)

type NonStreamResponse struct {
	Usage *UsageDetails `json:"usage,omitempty"`
}

type UsageDetails struct {
	PromptTokens        int                  `json:"prompt_tokens,omitempty"`
	CompletionTokens    int                  `json:"completion_tokens,omitempty"`
	TotalTokens         int                  `json:"total_tokens,omitempty"`
	PromptTokensDetails *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type PromptTokensDetails struct {
	CacheType    string `json:"cache_type,omitempty"`
	CachedTokens int    `json:"cached_tokens,omitempty"`
}

// GetValidTokenAndEndpoint gets a valid token and determines the correct endpoint
func GetValidTokenAndEndpoint() (string, string, error) {
	credentials, err := auth.LoadQwenCredentials()
	if err != nil {
		// If credentials file doesn't exist, return a special error that can be handled by the caller
		return "", "", fmt.Errorf("credentials not found: %v. Please authenticate with Qwen by restarting the proxy", err)
	}

	// If token is expired or about to expire, try to refresh it
	if !auth.IsTokenValid(credentials) {
		log.Println("Token is expired or about to expire, attempting to refresh...")
		credentials, err = auth.RefreshAccessToken(credentials)
		if err != nil {
			// If token refresh fails, return a special error that can be handled by the caller
			return "", "", fmt.Errorf("failed to refresh token: %v. Please re-authenticate with Qwen by restarting the proxy", err)
		}
		log.Println("Token successfully refreshed")
	}

	if credentials.AccessToken == "" {
		return "", "", fmt.Errorf("no access token found in credentials")
	}

	// Use resource_url from credentials if available, otherwise fallback to default from auth package
	baseEndpoint := credentials.ResourceURL
	if baseEndpoint == "" {
		baseEndpoint = auth.DefaultQwenBaseURL
	}

	// Normalize the URL: add protocol if missing, ensure /v1 suffix
	if !strings.HasPrefix(baseEndpoint, "http") {
		baseEndpoint = "https://" + baseEndpoint
	}

	const suffix = "/v1"
	if !strings.HasSuffix(baseEndpoint, suffix) {
		baseEndpoint = baseEndpoint + suffix
	}
	return credentials.AccessToken, baseEndpoint, nil
}

// PrepareRequest constructs the target URL and processes the request body
func PrepareRequest(r *http.Request, targetEndpoint string) (string, map[string]interface{}, error) {
	// Construct the full target URL
	requestPath := r.URL.Path
	if strings.HasPrefix(requestPath, "/v1") && strings.HasSuffix(targetEndpoint, "/v1") {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	targetURL := targetEndpoint + requestPath

	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read request body: %w", err)
	}
	// Restore the body for further reads downstream (e.g., in proxy handlers)
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	originalBody := make(map[string]interface{})
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &originalBody); err != nil {
			return "", nil, fmt.Errorf("failed to unmarshal request body: %w", err)
		}
	}

	return targetURL, originalBody, nil
}
