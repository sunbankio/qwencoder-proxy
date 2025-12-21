// Package antigravity provides the Google Antigravity provider implementation
package antigravity

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
	"github.com/sunbankio/qwencoder-proxy/provider/gemini"
)

const (
	// DefaultDailyBaseURL is the default Antigravity daily environment base URL
	DefaultDailyBaseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	// DefaultAutopushBaseURL is the default Antigravity autopush environment base URL
	DefaultAutopushBaseURL = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
	// DefaultUserAgent is the default user agent string
	DefaultUserAgent = "antigravity/1.11.5 windows/amd64"
)

// SupportedModels lists all supported Antigravity models
var SupportedModels = []string{
	"gemini-2.5-computer-use-preview-10-2025",
	"gemini-3-pro-image-preview",
	"gemini-3-pro-preview",
	"gemini-3-flash-preview",
	"gemini-2.5-flash",
	"gemini-claude-sonnet-4-5",
	"gemini-claude-sonnet-4-5-thinking",
	"gemini-claude-opus-4-5-thinking",
}

// ModelAliasMapping maps aliases to actual model names
var ModelAliasMapping = map[string]string{
	"rev19-uic3-1p":                    "gemini-2.5-computer-use-preview-10-2025",
	"gemini-3-pro-image":               "gemini-3-pro-image-preview",
	"gemini-3-pro-high":                "gemini-3-pro-preview",
	"gemini-3-flash":                   "gemini-3-flash-preview",
	"gemini-2.5-flash":                 "gemini-2.5-flash",
	"claude-sonnet-4-5":                "gemini-claude-sonnet-4-5",
	"claude-sonnet-4-5-thinking":       "gemini-claude-sonnet-4-5-thinking",
	"claude-opus-4-5-thinking":         "gemini-claude-opus-4-5-thinking",
}

// Provider implements the provider.Provider interface for Antigravity
type Provider struct {
	dailyBaseURL    string
	autopushBaseURL string
	authenticator   *auth.GeminiAuthenticator // Antigravity uses similar auth to Gemini CLI
	httpClient      *http.Client
	logger          *logging.Logger
}

// NewProvider creates a new Antigravity provider
func NewProvider(authenticator *auth.GeminiAuthenticator) *Provider {
	if authenticator == nil {
		authenticator = auth.NewGeminiAuthenticator(&auth.GeminiOAuthConfig{
			ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
			ClientSecret: "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",
			Scope:        "https://www.googleapis.com/auth/cloud-platform",
			RedirectPort: 8086,
			CredsDir:     ".antigravity",
			CredsFile:    "oauth_creds.json",
		})
	}
	return &Provider{
		dailyBaseURL:    DefaultDailyBaseURL,
		autopushBaseURL: DefaultAutopushBaseURL,
		authenticator:   authenticator,
		httpClient:      &http.Client{Timeout: 5 * time.Minute},
		logger:          logging.NewLogger(),
	}
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return provider.ProviderAntigravity
}

// Protocol returns the native protocol
func (p *Provider) Protocol() provider.ProtocolType {
	return provider.ProtocolGemini
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
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	url := fmt.Sprintf("%s/v1internal:fetchAvailableModels", p.dailyBaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultUserAgent)

	p.logger.DebugLog("[Antigravity] Sending fetchAvailableModels request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var modelsResp gemini.GeminiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &modelsResp, nil
}

// GenerateContent handles non-streaming requests with native format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Map model alias if needed
	actualModel := model
	if alias, exists := ModelAliasMapping[model]; exists {
		actualModel = alias
	}

	// Marshal request body
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// For Antigravity, we need to determine the correct endpoint based on the model
	url := p.getBaseURLForModel(actualModel)
	url = fmt.Sprintf("%s/loadCodeAssist", url) // Using loadCodeAssist as the endpoint

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultUserAgent)

	p.logger.DebugLog("[Antigravity] Sending loadCodeAssist request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp gemini.GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &geminiResp, nil
}

// GenerateContentStream handles streaming requests with native format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Map model alias if needed
	actualModel := model
	if alias, exists := ModelAliasMapping[model]; exists {
		actualModel = alias
	}

	// Marshal request body
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// For Antigravity, we need to determine the correct endpoint based on the model
	url := p.getBaseURLForModel(actualModel)
	url = fmt.Sprintf("%s/loadCodeAssist", url) // Using loadCodeAssist as the endpoint

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", DefaultUserAgent)
	req.Header.Set("Accept", "text/event-stream")

	p.logger.DebugLog("[Antigravity] Sending streaming loadCodeAssist request to %s", url)

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

// getBaseURLForModel returns the appropriate base URL for the given model
func (p *Provider) getBaseURLForModel(model string) string {
	// For now, default to daily environment
	// In a real implementation, we might have logic to determine which environment to use
	return p.dailyBaseURL
}

// GetDailyBaseURL returns the daily environment base URL
func (p *Provider) GetDailyBaseURL() string {
	return p.dailyBaseURL
}

// GetAutopushBaseURL returns the autopush environment base URL
func (p *Provider) GetAutopushBaseURL() string {
	return p.autopushBaseURL
}

// SetDailyBaseURL sets the daily environment base URL
func (p *Provider) SetDailyBaseURL(url string) {
	p.dailyBaseURL = url
}

// SetAutopushBaseURL sets the autopush environment base URL
func (p *Provider) SetAutopushBaseURL(url string) {
	p.autopushBaseURL = url
}