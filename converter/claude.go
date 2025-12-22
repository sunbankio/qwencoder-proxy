// Package converter provides format conversion between different LLM API formats
package converter

import (
	"fmt"

	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/provider/kiro"
)

// ClaudeConverter handles Claude to/from OpenAI format conversions
type ClaudeConverter struct{}

// NewClaudeConverter creates a new Claude converter
func NewClaudeConverter() *ClaudeConverter {
	return &ClaudeConverter{}
}

// ToOpenAIRequest converts Claude format to OpenAI format
func (c *ClaudeConverter) ToOpenAIRequest(native interface{}) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return native, nil
}

// ToOpenAIResponse converts Claude format to OpenAI format
func (c *ClaudeConverter) ToOpenAIResponse(native interface{}, model string) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return native, nil
}

// ToOpenAIStreamChunk converts Claude format to OpenAI format
func (c *ClaudeConverter) ToOpenAIStreamChunk(native interface{}, model string) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return native, nil
}

// FromOpenAIRequest converts OpenAI format to Claude format
func (c *ClaudeConverter) FromOpenAIRequest(req interface{}) (interface{}, error) {
	// Convert the request map to Claude format
	reqMap, ok := req.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid request type, expected map[string]interface{}")
	}

	// Extract model
	model, _ := reqMap["model"].(string)

	// Extract messages
	messagesRaw, ok := reqMap["messages"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("messages field is required and must be an array")
	}

	// Convert messages to Claude format
	var messages []kiro.Message
	var systemPrompt string

	for _, msgRaw := range messagesRaw {
		msgMap, ok := msgRaw.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		content := msgMap["content"]

		// Handle system messages separately in Claude
		if role == "system" {
			if contentStr, ok := content.(string); ok {
				systemPrompt = contentStr
			}
			continue
		}

		// Create message with content
		msg := kiro.Message{
			Role:    role,
			Content: content,
		}
		messages = append(messages, msg)
	}

	// Extract max_tokens
	maxTokens := 4096 // default
	if mt, ok := reqMap["max_tokens"].(float64); ok {
		maxTokens = int(mt)
	} else if mt, ok := reqMap["max_tokens"].(int); ok {
		maxTokens = mt
	}

	// Extract optional parameters
	var temperature *float64
	if temp, ok := reqMap["temperature"].(float64); ok {
		temperature = &temp
	}

	var topP *float64
	if tp, ok := reqMap["top_p"].(float64); ok {
		topP = &tp
	}

	// Create Claude request
	claudeReq := &kiro.ClaudeRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		System:      systemPrompt,
		Temperature: temperature,
		TopP:        topP,
	}

	// Handle stream parameter
	if stream, ok := reqMap["stream"].(bool); ok {
		claudeReq.Stream = stream
	}

	return claudeReq, nil
}

// FromOpenAIResponse converts OpenAI format to Claude format
func (c *ClaudeConverter) FromOpenAIResponse(resp interface{}) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return resp, nil
}

// Protocol returns the native protocol
func (c *ClaudeConverter) Protocol() provider.ProtocolType {
	return provider.ProtocolClaude
}
