// Package kiro provides the Kiro/Claude provider implementation
package kiro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

const (
	// KiroVersion is the version string for Kiro
	KiroVersion = "0.7.5"
	// UserAgent is the user agent string
	UserAgent = "KiroIDE"
)

// SupportedModels lists all supported Claude models via Kiro
var SupportedModels = []string{
	"claude-opus-4-5",
	"claude-opus-4-5-20251101",
	"claude-haiku-4-5",
	"claude-sonnet-4-5",
	"claude-sonnet-4-5-20250929",
	"claude-sonnet-4-20250514",
	"claude-3-7-sonnet-20250219",
}

// ModelMapping maps external model names to internal Kiro model names
var ModelMapping = map[string]string{
	"claude-opus-4-5":            "claude-opus-4.5",
	"claude-opus-4-5-20251101":   "claude-opus-4.5",
	"claude-haiku-4-5":           "claude-haiku-4.5",
	"claude-sonnet-4-5":          "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_20250514_V1_0",
	"claude-3-7-sonnet-20250219": "CLAUDE_3_7_SONNET_20250219_V1_0",
}

// Provider implements the provider.Provider interface for Kiro
type Provider struct {
	authenticator *auth.KiroAuthenticator
	httpClient    *http.Client
	logger        *logging.Logger
	machineID     string
}

// NewProvider creates a new Kiro provider
func NewProvider(authenticator *auth.KiroAuthenticator) *Provider {
	if authenticator == nil {
		authenticator = auth.NewKiroAuthenticator(nil)
	}
	return &Provider{
		authenticator: authenticator,
		httpClient:    &http.Client{Timeout: 5 * time.Minute},
		logger:        logging.NewLogger(),
		machineID:     generateMachineID(),
	}
}

// generateMachineID generates a unique machine ID
func generateMachineID() string {
	hostname, _ := os.Hostname()
	hash := sha256.Sum256([]byte(hostname + "KIRO_DEFAULT_MACHINE"))
	return hex.EncodeToString(hash[:])
}

// Name returns the provider identifier
func (p *Provider) Name() provider.ProviderType {
	return provider.ProviderKiro
}

// Protocol returns the native protocol
func (p *Provider) Protocol() provider.ProtocolType {
	return provider.ProtocolClaude
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
	return p.authenticator.IsAuthenticated()
}

// getBaseURL returns the base URL for the Kiro API
func (p *Provider) getBaseURL() string {
	region := p.authenticator.GetRegion()
	return fmt.Sprintf("https://codewhisperer.%s.amazonaws.com", region)
}

// getHeaders returns the headers for Kiro API requests
func (p *Provider) getHeaders(token string) map[string]string {
	osName := runtime.GOOS
	if osName == "windows" {
		osName = "windows"
	} else if osName == "darwin" {
		osName = "macos"
	}

	return map[string]string{
		"Content-Type":           "application/json",
		"Accept":                 "application/json",
		"Authorization":          "Bearer " + token,
		"amz-sdk-request":        "attempt=1; max=1",
		"x-amzn-kiro-agent-mode": "vibe",
		"x-amz-user-agent":       fmt.Sprintf("aws-sdk-js/1.0.0 KiroIDE-%s-%s", KiroVersion, p.machineID),
		"user-agent":             fmt.Sprintf("aws-sdk-js/1.0.0 ua/2.1 os/%s lang/go api/codewhispererruntime#1.0.0 m/E KiroIDE-%s-%s", osName, KiroVersion, p.machineID),
	}
}

// ListModels returns available models in Claude format
func (p *Provider) ListModels(ctx context.Context) (interface{}, error) {
	// Kiro doesn't have a model list endpoint, return static list
	models := make([]ClaudeModel, len(SupportedModels))
	for i, model := range SupportedModels {
		models[i] = ClaudeModel{
			ID:          model,
			DisplayName: model,
		}
	}
	return &ClaudeModelsResponse{Data: models}, nil
}

// GenerateContent handles non-streaming requests with native Claude format
func (p *Provider) GenerateContent(ctx context.Context, model string, request interface{}) (interface{}, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Convert request to Kiro format
	claudeReq, ok := request.(*ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type")
	}

	// Map model name
	internalModel := model
	if mapped, exists := ModelMapping[model]; exists {
		internalModel = mapped
	}

	// Build Kiro request
	kiroReq := p.buildKiroRequest(claudeReq, internalModel)

	reqBody, err := json.Marshal(kiroReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getBaseURL() + "/generateAssistantResponse"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for k, v := range p.getHeaders(token) {
		req.Header.Set(k, v)
	}

	p.logger.DebugLog("[Kiro] Sending generateAssistantResponse request to %s", url)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response and convert to Claude format
	var kiroResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&kiroResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return p.convertToClaudeResponse(kiroResp, model), nil
}

// GenerateContentStream handles streaming requests with native Claude format
func (p *Provider) GenerateContentStream(ctx context.Context, model string, request interface{}) (io.ReadCloser, error) {
	token, err := p.authenticator.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Convert request to Kiro format
	claudeReq, ok := request.(*ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type")
	}

	// Map model name
	internalModel := model
	if mapped, exists := ModelMapping[model]; exists {
		internalModel = mapped
	}
	// Build Kiro request
	kiroReq := p.buildKiroRequest(claudeReq, internalModel)

	reqBody, err := json.Marshal(kiroReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := p.getBaseURL() + "/SendMessageStreaming"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for k, v := range p.getHeaders(token) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")

	p.logger.DebugLog("[Kiro] Sending SendMessageStreaming request to %s", url)

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

// buildKiroRequest builds a Kiro API request from a Claude request
func (p *Provider) buildKiroRequest(claudeReq *ClaudeRequest, model string) map[string]interface{} {
	// Convert messages to Kiro format
	messages := make([]map[string]interface{}, 0)
	for _, msg := range claudeReq.Messages {
		kiroMsg := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		messages = append(messages, kiroMsg)
	}

	// Get the last message content
	var lastContent interface{}
	if len(messages) > 0 {
		lastContent = messages[len(messages)-1]["content"]
	}

	req := map[string]interface{}{
		"conversationState": map[string]interface{}{
			"currentMessage": map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": lastContent,
				},
			},
			"chatTriggerType": "MANUAL",
		},
		"profileArn": "",
	}

	// Add system prompt if present
	if claudeReq.System != "" {
		req["systemPrompt"] = claudeReq.System
	}

	return req
}

// convertToClaudeResponse converts a Kiro response to Claude format
func (p *Provider) convertToClaudeResponse(kiroResp map[string]interface{}, model string) *ClaudeResponse {
	// Extract text from Kiro response
	text := ""
	if content, ok := kiroResp["generateAssistantResponseResponse"].(map[string]interface{}); ok {
		if assistantResponse, ok := content["assistantResponseEvent"].(map[string]interface{}); ok {
			if textContent, ok := assistantResponse["content"].(string); ok {
				text = textContent
			}
		}
	}

	return &ClaudeResponse{
		ID:    fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Content: []ContentBlock{
			{
				Type: "text",
				Text: text,
			},
		},
		StopReason: "end_turn",
		Usage: &Usage{
			InputTokens:  0,
			OutputTokens: 0,
		},
	}
}

// MapModelName maps an external model name to internal Kiro model name
func MapModelName(model string) string {
	if mapped, exists := ModelMapping[model]; exists {
		return mapped
	}
	return model
}

// IsValidModel checks if a model is supported
func IsValidModel(model string) bool {
	model = strings.ToLower(model)
	for _, m := range SupportedModels {
		if strings.ToLower(m) == model {
			return true
		}
	}
	return false
}
