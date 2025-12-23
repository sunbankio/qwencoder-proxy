// Package iflow provides the iFlow provider implementation
package iflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
)

const (
	// OAuth constants from reference/iflow.rs
	AuthURL      = "https://iflow.cn/oauth"
	TokenURL     = "https://iflow.cn/oauth/token"
	UserInfoURL  = "https://iflow.cn/api/oauth/getUserInfo"
	APIKeyURL    = "https://platform.iflow.cn/api/openapi/apikey"
	ClientID     = "10009311001"
	ClientSecret = "4Z3YjXycVsQvyGF1etiNlIBB4RsqSDtW"
	DefaultPort  = 11451
	APIBaseURL   = "https://apis.iflow.cn/v1"
)

// SupportedModels lists all supported iFlow models
var SupportedModels = []string{
	"glm-4.6",
	"qwen3-coder-plus",
	"qwen3-max",
	"deepseek-v3.2",
	"deepseek-r1",
	"qwen3-vl-plus",
	"kimi-k2",
	"kimi-k2-0905",
}

// Provider implements the provider.Provider interface for iFlow
type Provider struct {
	baseURL       string
	authenticator *Authenticator
	httpClient    *http.Client
	logger        *logging.Logger
}

// NewProvider creates a new iFlow provider
func NewProvider(authenticator *Authenticator) *Provider {
	if authenticator == nil {
		authenticator = NewAuthenticator(nil)
	}
	return &Provider{
		baseURL:       APIBaseURL,
		authenticator: authenticator,
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
		logger:        logging.NewLogger(),
	}
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return "iflow"
}

// Protocol returns the native protocol
func (p *Provider) Protocol() provider.ProtocolType {
	return provider.ProtocolOpenAI
}

// SupportedModels returns list of supported model IDs
func (p *Provider) SupportedModels() []string {
	return SupportedModels
}

// SupportsModel checks if the provider supports the given model
func (p *Provider) SupportsModel(model string) bool {
	for _, m := range SupportedModels {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return false
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

// ListModels returns available models in OpenAI format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	// iFlow doesn't support /models endpoint, return hardcoded models
	modelsResp := OpenAIModelsResponse{
		Object: "list",
		Data:   []OpenAIModel{},
	}

	for _, modelID := range SupportedModels {
		modelsResp.Data = append(modelsResp.Data, OpenAIModel{
			ID:      modelID,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "iflow",
		})
	}

	return &modelsResp, nil
}

// GenerateContent handles non-streaming requests with OpenAI format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	tokenPrefix := token
	if len(token) > 20 {
		tokenPrefix = token[:20]
	}
	p.logger.DebugLog("[iFlow] Using access token (first 20 chars): %s", tokenPrefix)

	// Marshal the request
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "iflow-cli/0.4.8")

	p.logger.DebugLog("[iFlow] Sending chat completions request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	p.logger.DebugLog("[iFlow] Response status: %d", resp.StatusCode)
	p.logger.DebugLog("[iFlow] Response headers: %v", resp.Header)

	if resp.StatusCode == http.StatusUnauthorized {
		// Try to refresh token and retry
		_, refreshErr := p.authenticator.GetToken(ctx)
		if refreshErr != nil {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("unauthorized and token refresh failed: %s", string(body))
		}

		// Retry with refreshed token
		token, _ = p.authenticator.GetToken(ctx)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send retry request: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.logger.DebugLog("[iFlow] API error response body: %s", string(body))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	p.logger.DebugLog("[iFlow] Raw response body: %s", string(respBody))

	var chatResp OpenAIChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// GenerateContentStream handles streaming requests with OpenAI format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Marshal the request
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("User-Agent", "qwencoder-proxy/1.0")

	p.logger.DebugLog("[iFlow] Sending streaming chat completions request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	p.logger.DebugLog("[iFlow] Streaming response status: %d", resp.StatusCode)
	p.logger.DebugLog("[iFlow] Streaming response headers: %v", resp.Header)

	if resp.StatusCode == http.StatusUnauthorized {
		// Try to refresh token and retry
		_, refreshErr := p.authenticator.GetToken(ctx)
		if refreshErr != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("unauthorized and token refresh failed: %s", string(body))
		}

		// Retry with refreshed token
		token, _ = p.authenticator.GetToken(ctx)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send retry request: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		p.logger.DebugLog("[iFlow] Streaming API error response body: %s", string(body))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// OpenAIModelsResponse represents OpenAI models API response
type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// OpenAIModel represents a model in OpenAI format
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIChatResponse represents OpenAI chat completion response
type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

// OpenAIChoice represents a choice in OpenAI response
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// OpenAIMessage represents a message in OpenAI format
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIUsage represents token usage in OpenAI response
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
