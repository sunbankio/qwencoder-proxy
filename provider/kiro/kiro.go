// Package kiro provides the Kiro/Claude provider implementation
package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
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

// SupportsModel checks if the provider supports the given model
func (p *Provider) SupportsModel(model string) bool {
	modelLower := strings.ToLower(model)
	if strings.HasPrefix(modelLower, "claude-") {
		return true
	}
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
	return p.authenticator.IsAuthenticated()
}

// getBaseURL returns the base URL for the Kiro API
func (p *Provider) getBaseURL() string {
	region := p.authenticator.GetRegion()
	return fmt.Sprintf("https://codewhisperer.%s.amazonaws.com", region)
}

// getHeaders returns the headers for Kiro API requests
func (p *Provider) getHeaders(token string, invocationID string) map[string]string {
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
		"amz-sdk-invocation-id":  invocationID,
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
	invocationID := generateUUID()
	for k, v := range p.getHeaders(token, invocationID) {
		req.Header.Set(k, v)
	}

	p.logger.DebugLog("[Kiro] Sending generateAssistantResponse request to %s", url)
	p.logger.DebugLog("[Kiro] Request body: %s", string(reqBody))
	fmt.Println("[Kiro DEBUG] Request body:", string(reqBody))
	fmt.Println("[Kiro DEBUG] Invocation ID:", invocationID)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Read the entire response body
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse AWS Event Stream
	events := p.parseAwsEventStream(respData)
	if len(events) == 0 {
		return nil, fmt.Errorf("no events found in response")
	}

	return p.convertToClaudeResponse(events, model), nil
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
	invocationID := generateUUID()
	for k, v := range p.getHeaders(token, invocationID) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")

	p.logger.DebugLog("[Kiro] Sending SendMessageStreaming request to %s", url)
	fmt.Println("[Kiro DEBUG] Streaming Invocation ID:", invocationID)

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
	conversationID := generateUUID()

	// Extract system prompt
	systemPrompt := claudeReq.System

	// Process messages
	var history []map[string]interface{}

	if len(claudeReq.Messages) == 0 {
		return map[string]interface{}{
			"conversationState": map[string]interface{}{
				"chatTriggerType": "MANUAL",
				"conversationId":  conversationID,
				"currentMessage": map[string]interface{}{
					"userInputMessage": map[string]interface{}{
						"content": "Hello",
						"modelId": model,
						"origin":  "AI_EDITOR",
					},
				},
			},
		}
	}

	// If there's a system prompt, we need to handle it
	startIndex := 0
	if systemPrompt != "" {
		firstMsg := claudeReq.Messages[0]
		if firstMsg.Role == "user" {
			// Prepend system prompt to the first user message
			content := p.extractTextContent(firstMsg.Content)
			history = append(history, map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": fmt.Sprintf("%s\n\n%s", systemPrompt, content),
					"modelId": model,
					"origin":  "AI_EDITOR",
				},
			})
			startIndex = 1
		} else {
			// Add system prompt as a standalone user message
			history = append(history, map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": systemPrompt,
					"modelId": model,
					"origin":  "AI_EDITOR",
				},
			})
		}
	}

	// Process middle messages for history
	for i := startIndex; i < len(claudeReq.Messages)-1; i++ {
		msg := claudeReq.Messages[i]
		if msg.Role == "user" {
			history = append(history, map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": p.extractTextContent(msg.Content),
					"modelId": model,
					"origin":  "AI_EDITOR",
				},
			})
		} else if msg.Role == "assistant" {
			history = append(history, map[string]interface{}{
				"assistantResponseMessage": map[string]interface{}{
					"content": p.extractTextContent(msg.Content),
				},
			})
		}
	}

	// Last message is the current message
	lastMsg := claudeReq.Messages[len(claudeReq.Messages)-1]
	currentContent := p.extractTextContent(lastMsg.Content)

	// If the last message is an assistant message (unusual for starting a response),
	// move it to history and add a "Continue" user message as current
	if lastMsg.Role == "assistant" {
		history = append(history, map[string]interface{}{
			"assistantResponseMessage": map[string]interface{}{
				"content": currentContent,
			},
		})
		currentContent = "Continue"
	}

	if currentContent == "" {
		currentContent = "Continue"
	}

	userInputMessage := map[string]interface{}{
		"content": currentContent,
		"modelId": model,
		"origin":  "AI_EDITOR",
	}

	conversationState := map[string]interface{}{
		"chatTriggerType": "MANUAL",
		"conversationId":  conversationID,
		"currentMessage": map[string]interface{}{
			"userInputMessage": userInputMessage,
		},
	}

	if len(history) > 0 {
		conversationState["history"] = history
	}

	req := map[string]interface{}{
		"conversationState": conversationState,
	}

	// Add profileArn if auth method is social
	if p.authenticator.GetAuthMethod() == "social" {
		if profileArn := p.authenticator.GetProfileArn(); profileArn != "" {
			req["profileArn"] = profileArn
		}
	}

	return req
}

// extractTextContent extracts plain text from MessageContent
func (p *Provider) extractTextContent(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	if blocks, ok := content.([]interface{}); ok {
		var result string
		for _, b := range blocks {
			if bMap, ok := b.(map[string]interface{}); ok {
				if bMap["type"] == "text" {
					if t, ok := bMap["text"].(string); ok {
						result += t
					}
				}
			}
		}
		return result
	}
	// Fallback for other potential types (should not happen with ClaudeRequest)
	return fmt.Sprintf("%v", content)
}

// generateUUID generates a simple UUID
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// KiroEvent represents a parsed event from the Kiro API
type KiroEvent struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// parseAwsEventStream extracts JSON payloads from AWS Event Stream binary data
func (p *Provider) parseAwsEventStream(data []byte) []KiroEvent {
	var events []KiroEvent

	// Possible JSON keys that indicate the start of an event
	patterns := []string{
		`{"content":`,
		`{"name":`,
		`{"followupPrompt":`,
		`{"input":`,
		`{"stop":`,
	}

	searchStart := 0
	for {
		earliest := -1
		for _, pattern := range patterns {
			pos := bytes.Index(data[searchStart:], []byte(pattern))
			if pos != -1 {
				absPos := searchStart + pos
				if earliest == -1 || absPos < earliest {
					earliest = absPos
				}
			}
		}

		if earliest == -1 {
			break
		}

		// Find the end of the JSON object using brace counting
		braceCount := 0
		jsonEnd := -1
		inString := false
		escapeNext := false

		for i := earliest; i < len(data); i++ {
			char := data[i]

			if escapeNext {
				escapeNext = false
				continue
			}

			if char == '\\' {
				escapeNext = true
				continue
			}

			if char == '"' {
				inString = !inString
				continue
			}

			if !inString {
				if char == '{' {
					braceCount++
				} else if char == '}' {
					braceCount--
					if braceCount == 0 {
						jsonEnd = i
						break
					}
				}
			}
		}

		if jsonEnd == -1 {
			// Incomplete JSON, but we are parsing a full response, so it shouldn't happen
			break
		}

		jsonStr := data[earliest : jsonEnd+1]
		var parsed map[string]interface{}
		if err := json.Unmarshal(jsonStr, &parsed); err == nil {
			// Determine event type
			if _, ok := parsed["content"]; ok && parsed["followupPrompt"] == nil {
				events = append(events, KiroEvent{Type: "content", Data: parsed})
			} else if _, ok := parsed["name"]; ok && parsed["toolUseId"] != nil {
				events = append(events, KiroEvent{Type: "toolUse", Data: parsed})
			} else if _, ok := parsed["input"]; ok && parsed["name"] == nil {
				events = append(events, KiroEvent{Type: "toolUseInput", Data: parsed})
			} else if _, ok := parsed["stop"]; ok {
				events = append(events, KiroEvent{Type: "toolUseStop", Data: parsed})
			}
		}

		searchStart = jsonEnd + 1
		if searchStart >= len(data) {
			break
		}
	}

	return events
}

// convertToClaudeResponse converts multiple Kiro events to Claude format
func (p *Provider) convertToClaudeResponse(events []KiroEvent, model string) *ClaudeResponse {
	fullContent := ""
	var toolUseBlocks []ContentBlock

	for _, event := range events {
		switch event.Type {
		case "content":
			if text, ok := event.Data["content"].(string); ok {
				fullContent += text
			}
		case "toolUse":
			name, _ := event.Data["name"].(string)
			id, _ := event.Data["toolUseId"].(string)
			inputStr, _ := event.Data["input"].(string) // Input is usually a string here?

			var input interface{}
			if inputStr != "" {
				json.Unmarshal([]byte(inputStr), &input)
			}

			toolUseBlocks = append(toolUseBlocks, ContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  name,
				Input: input,
			})
		case "toolUseInput":
			if len(toolUseBlocks) > 0 {
				if _, ok := event.Data["input"].(string); ok {
					// This is tricky because Input is interface{}
					// We might need to accumulate it as string first
				}
			}
		}
	}

	response := &ClaudeResponse{
		ID:    "msg_" + generateUUID(),
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Usage: &Usage{
			InputTokens:  0,
			OutputTokens: 0,
		},
		StopReason: "end_turn",
	}

	if fullContent != "" {
		response.Content = append(response.Content, ContentBlock{
			Type: "text",
			Text: fullContent,
		})
	}

	for _, block := range toolUseBlocks {
		response.Content = append(response.Content, block)
		response.StopReason = "tool_use"
	}

	return response
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
