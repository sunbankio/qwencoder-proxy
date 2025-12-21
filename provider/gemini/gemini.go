// Package gemini provides the Gemini CLI provider implementation
package gemini

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
)

const (
	// DefaultBaseURL is the default Gemini API base URL
	DefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// SupportedModels lists all supported Gemini models
var SupportedModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	"gemini-2.5-pro",
	"gemini-2.5-pro-preview-06-05",
	"gemini-2.5-flash-preview-09-2025",
	"gemini-3-pro-preview",
	"gemini-3-flash-preview",
}

// Provider implements the provider.Provider interface for Gemini CLI
type Provider struct {
	baseURL       string
	authenticator *auth.GeminiAuthenticator
	httpClient    *http.Client
	logger        *logging.Logger
}

// NewProvider creates a new Gemini provider
func NewProvider(authenticator *auth.GeminiAuthenticator) *Provider {
	if authenticator == nil {
		authenticator = auth.NewGeminiAuthenticator(nil)
	}
	return &Provider{
		baseURL:       DefaultBaseURL,
		authenticator: authenticator,
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
		logger:        logging.NewLogger(),
	}
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return provider.ProviderGeminiCLI
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

// ListModels returns available models in native Gemini format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	url := fmt.Sprintf("%s/models", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var modelsResp GeminiModelsResponse
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

	// Marshal request body
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", p.baseURL, model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	p.logger.DebugLog("[Gemini] Sending generateContent request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
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

	// Marshal request body
	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", p.baseURL, model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	p.logger.DebugLog("[Gemini] Sending streamGenerateContent request to %s", url)

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
