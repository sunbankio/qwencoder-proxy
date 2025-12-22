// Package qwen provides the Qwen provider implementation
package qwen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sunbankio/qwencoder-proxy/auth"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/qwenclient"
)

const (
	// DefaultBaseURL is the default Qwen API base URL
	DefaultBaseURL = "https://dashscope.aliyuncs.com/api/v1"
)

// SupportedModels lists all supported Qwen models
var SupportedModels = []string{
	"qwen-max",
	"qwen-plus",
	"qwen-turbo",
	"qwen-long",
	"qwen-vl-max",
	"qwen-vl-plus",
	"qwen-audio-turbo",
	"qwen2.5-72b-instruct",
	"qwen2.5-32b-instruct",
	"qwen2.5-14b-instruct",
	"qwen2.5-7b-instruct",
	"qwen2.5-3b-instruct",
	"qwen2.5-1.5b-instruct",
	"qwen2.5-0.5b-instruct",
	"qwen3-72b-instruct",
	"qwen3-32b-instruct",
	"qwen3-14b-instruct",
	"qwen3-7b-instruct",
	"qwen3-4b-instruct",
	"qwen3-1.8b-instruct",
	"qwen3-0.5b-instruct",
	"qwen3-coder-max",
	"qwen3-coder-plus",
	"qwen3-coder-turbo",
	"qwen3-coder",
	"qwen3-coder-32b",
	"qwen3-coder-14b",
	"qwen3-coder-7b",
	"qwen3-coder-3b",
	"qwen3-coder-1.5b",
	"qwen3.5-7b-instruct",
	"qwen3.5-14b-instruct",
	"qwen3.5-32b-instruct",
	"qwen3.5-72b-instruct",
	"qwen3.5-110b-instruct",
	"qwen3.5-0.5b-instruct",
	"qwen3.5-1.5b-instruct",
	"qwen3.5-3b-instruct",
}

// Provider implements the provider.Provider interface for Qwen
type Provider struct {
	baseURL       string
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
return nil // Let the qwenclient handle credential clearing
}

// NewProvider creates a new Qwen provider
func NewProvider() *Provider {
	return &Provider{
		baseURL:       DefaultBaseURL,
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
	return provider.ProtocolOpenAI
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
			"provider":   string(p.Name()),
		}
	}
	return map[string]interface{}{
		"object": "list",
		"data":   models,
	}, nil
}

// GenerateContent handles non-streaming requests with native format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	token, _, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Convert request to proper format for Qwen API
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/services/aigc/text-generation/generation", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	p.logger.DebugLog("[Qwen] Sending request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var qwenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&qwenResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return qwenResp, nil
}

// GenerateContentStream handles streaming requests with native format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	token, _, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Convert request to proper format for Qwen API
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/services/aigc/text-generation/generation", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	p.logger.DebugLog("[Qwen] Sending streaming request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}