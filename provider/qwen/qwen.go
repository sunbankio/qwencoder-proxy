// Package qwen provides the Qwen provider implementation
package qwen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/auth"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/qwenclient"
)

const (
	// DefaultBaseURL is the default Qwen API base URL
	DefaultBaseURL = "https://portal.qwen.ai/v1"
)

// SupportedModels lists all supported Qwen models
var SupportedModels = []string{
	"qwen3-coder-plus",
	"qwen3-coder-flash",
}

// Provider implements the provider.Provider interface for Qwen
type Provider struct {
	authenticator *QwenAuthenticator
	httpClient    *http.Client
	logger        *logging.Logger
}

// QwenAuthenticator wraps the existing qwenclient authentication
type QwenAuthenticator struct{}

// NewQwenAuthenticator creates a new Qwen authenticator
func NewQwenAuthenticator() *QwenAuthenticator {
	return &QwenAuthenticator{}
}

// Authenticate performs the authentication flow
func (a *QwenAuthenticator) Authenticate(ctx context.Context) error {
	return auth.AuthenticateWithOAuth()
}

// GetToken returns a valid access token
func (a *QwenAuthenticator) GetToken(ctx context.Context) (string, error) {
	token, _, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		// If we get an auth error, try to trigger the authentication flow
		if strings.Contains(err.Error(), "credentials not found") || strings.Contains(err.Error(), "failed to refresh token") {
			// Trigger authentication flow to get new credentials
			authErr := auth.AuthenticateWithOAuth()
			if authErr != nil {
				return "", fmt.Errorf("authentication required but failed: %v. Error getting token: %w", authErr, err)
			}
			// Try again after authentication
			token, _, err := qwenclient.GetValidTokenAndEndpoint()
			if err != nil {
				return "", err
			}
			return token, nil
		}
		return "", err
	}
	return token, nil
}

// IsAuthenticated checks if valid credentials exist
func (a *QwenAuthenticator) IsAuthenticated() bool {
	_, _, err := qwenclient.GetValidTokenAndEndpoint()
	return err == nil
}

// GetCredentialsPath returns the path to stored credentials
func (a *QwenAuthenticator) GetCredentialsPath() string {
	return auth.GetQwenCredentialsPath()
}

// ClearCredentials removes stored credentials
func (a *QwenAuthenticator) ClearCredentials() error {
	// Use the credentials path to remove the file
	credsPath := a.GetCredentialsPath()
	return os.Remove(credsPath)
}

// NewProvider creates a new Qwen provider
func NewProvider() *Provider {
	return &Provider{
		authenticator: NewQwenAuthenticator(),
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
		logger:        logging.NewLogger(),
	}
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return provider.ProviderQwen
}

// Protocol returns the native protocol
func (p *Provider) Protocol() provider.ProtocolType {
	return provider.ProtocolQwen
}

// SupportedModels returns list of supported model IDs
func (p *Provider) SupportedModels() []string {
	return SupportedModels
}

// GetAuthenticator returns the auth handler for this provider
func (p *Provider) GetAuthenticator() provider.Authenticator {
	return p.authenticator
}

// IsHealthy checks if the provider is available
func (p *Provider) IsHealthy(ctx context.Context) bool {
	// Try to list models as a health check
	_, err := p.ListModels(ctx)
	return err == nil
}

// ListModels returns available models in native format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	// For now, return static list since Qwen doesn't have a public models endpoint
	models := make([]interface{}, len(SupportedModels))
	for i, model := range SupportedModels {
		models[i] = map[string]interface{}{
			"id":         model,
			"object":     "model",
			"created":    1677648736,
			"owned_by":   "qwen",
		}
	}
	return map[string]interface{}{
		"object": "list",
		"data":   models,
	}, nil
}

// GenerateContent handles non-streaming requests with native format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	token, endpoint, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Log the original request before conversion
	p.logger.DebugLog("[Qwen] Original request before conversion: %+v", request)
	
	// Convert request to proper format for Qwen API
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	p.logger.DebugLog("[Qwen] Request body being sent: %s", string(reqBody))

	// For OpenAI-compatible requests, we need to forward the original request path
	// The endpoint from credentials may or may not include /v1, so we ensure it's properly formatted
	// If endpoint is "https://portal.qwen.ai" and we want to call chat completions,
	// the final URL should be "https://portal.qwen.ai/v1/chat/completions"
	
	// Ensure the endpoint ends with /v1 for the Qwen API
	normalizedEndpoint := endpoint
	if !strings.HasSuffix(normalizedEndpoint, "/v1") {
		normalizedEndpoint = normalizedEndpoint + "/v1"
	}
	
	// Since this is called from the OpenAI handler for /v1/chat/completions,
	// we construct the appropriate path
	url := fmt.Sprintf("%s/chat/completions", normalizedEndpoint)
	p.logger.DebugLog("[Qwen] Constructed target URL: %s", url)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set the required headers for Qwen API (following reference implementation)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.10 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")

	p.logger.DebugLog("[Qwen] Sending request to %s with headers: %v", url, req.Header)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.ErrorLog("[Qwen] Failed to send request: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	p.logger.DebugLog("[Qwen] Response status: %d", resp.StatusCode)
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.logger.ErrorLog("[Qwen] API error (status %d): %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var qwenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&qwenResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	p.logger.DebugLog("[Qwen] Received response: %+v", qwenResp)
	return qwenResp, nil
}

// GenerateContentStream handles streaming requests with native format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	token, endpoint, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Convert request to proper format for Qwen API
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// For OpenAI-compatible requests, we need to forward the original request path
	// The endpoint from credentials may or may not include /v1, so we ensure it's properly formatted
	// If endpoint is "https://portal.qwen.ai" and we want to call chat completions,
	// the final URL should be "https://portal.qwen.ai/v1/chat/completions"
	
	// Ensure the endpoint ends with /v1 for the Qwen API
	normalizedEndpoint := endpoint
	if !strings.HasSuffix(normalizedEndpoint, "/v1") {
		normalizedEndpoint = normalizedEndpoint + "/v1"
	}
	
	url := fmt.Sprintf("%s/chat/completions", normalizedEndpoint)
	p.logger.DebugLog("[Qwen] Constructed streaming target URL: %s", url)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set the required headers for Qwen API
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.10 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")

	p.logger.DebugLog("[Qwen] Sending streaming request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.ErrorLog("[Qwen] Failed to send streaming request: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	p.logger.DebugLog("[Qwen] Streaming response status: %d", resp.StatusCode)
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		p.logger.ErrorLog("[Qwen] Streaming API error (status %d): %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}