// Package converter provides format conversion between different LLM API formats
package converter

import (
	"fmt"
	"time"

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
	// Parse the Claude response
	claudeResp, ok := native.(*kiro.ClaudeResponse)
	if !ok {
		// Try to parse from map if it's not already a ClaudeResponse
		respMap, ok := native.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid response type, expected *kiro.ClaudeResponse or map[string]interface{}")
		}
		
		// Convert map to ClaudeResponse
		claudeResp = &kiro.ClaudeResponse{}
		if id, ok := respMap["id"].(string); ok {
			claudeResp.ID = id
		}
		if role, ok := respMap["role"].(string); ok {
			claudeResp.Role = role
		}
		if modelStr, ok := respMap["model"].(string); ok {
			claudeResp.Model = modelStr
		}
		if stopReason, ok := respMap["stop_reason"].(string); ok {
			claudeResp.StopReason = stopReason
		}
		
		// Parse content array
		if contentRaw, ok := respMap["content"].([]interface{}); ok {
			for _, contentItem := range contentRaw {
				if contentMap, ok := contentItem.(map[string]interface{}); ok {
					block := kiro.ContentBlock{}
					if blockType, ok := contentMap["type"].(string); ok {
						block.Type = blockType
					}
					if text, ok := contentMap["text"].(string); ok {
						block.Text = text
					}
					claudeResp.Content = append(claudeResp.Content, block)
				}
			}
		}
		
		// Parse usage
		if usageRaw, ok := respMap["usage"].(map[string]interface{}); ok {
			usage := &kiro.Usage{}
			if inputTokens, ok := usageRaw["input_tokens"].(float64); ok {
				usage.InputTokens = int(inputTokens)
			}
			if outputTokens, ok := usageRaw["output_tokens"].(float64); ok {
				usage.OutputTokens = int(outputTokens)
			}
			claudeResp.Usage = usage
		}
	}

	// Extract text content from Claude content blocks
	var content string
	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	// Convert stop_reason to OpenAI finish_reason
	finishReason := "stop"
	switch claudeResp.StopReason {
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	case "stop_sequence":
		finishReason = "stop"
	case "tool_use":
		finishReason = "tool_calls"
	}

	// Create OpenAI response structure
	openAIResp := map[string]interface{}{
		"id":      claudeResp.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   claudeResp.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    claudeResp.Role,
					"content": content,
				},
				"finish_reason": finishReason,
			},
		},
	}

	// Add usage if available
	if claudeResp.Usage != nil {
		openAIResp["usage"] = map[string]interface{}{
			"prompt_tokens":     claudeResp.Usage.InputTokens,
			"completion_tokens": claudeResp.Usage.OutputTokens,
			"total_tokens":      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		}
	}

	return openAIResp, nil
}

// ToOpenAIStreamChunk converts Claude format to OpenAI format
func (c *ClaudeConverter) ToOpenAIStreamChunk(native interface{}, model string) (interface{}, error) {
	// Parse the Claude stream event
	streamEvent, ok := native.(*kiro.StreamEvent)
	if !ok {
		// Try to parse from map if it's not already a StreamEvent
		eventMap, ok := native.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid stream event type, expected *kiro.StreamEvent or map[string]interface{}")
		}
		
		// Convert map to StreamEvent
		streamEvent = &kiro.StreamEvent{}
		if eventType, ok := eventMap["type"].(string); ok {
			streamEvent.Type = eventType
		}
		if index, ok := eventMap["index"].(float64); ok {
			streamEvent.Index = int(index)
		}
		
		// Parse delta
		if deltaRaw, ok := eventMap["delta"].(map[string]interface{}); ok {
			delta := &kiro.Delta{}
			if text, ok := deltaRaw["text"].(string); ok {
				delta.Text = text
			}
			if stopReason, ok := deltaRaw["stop_reason"].(string); ok {
				delta.StopReason = stopReason
			}
			streamEvent.Delta = delta
		}
		
		// Parse content_block
		if blockRaw, ok := eventMap["content_block"].(map[string]interface{}); ok {
			block := &kiro.ContentBlock{}
			if blockType, ok := blockRaw["type"].(string); ok {
				block.Type = blockType
			}
			if text, ok := blockRaw["text"].(string); ok {
				block.Text = text
			}
			streamEvent.ContentBlock = block
		}
	}

	// Create base OpenAI stream chunk
	chunk := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", streamEvent.Index),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": streamEvent.Index,
				"delta": map[string]interface{}{},
			},
		},
	}

	choice := chunk["choices"].([]map[string]interface{})[0]

	// Handle different event types
	switch streamEvent.Type {
	case "message_start":
		// First chunk with role
		choice["delta"] = map[string]interface{}{
			"role": "assistant",
		}
	case "content_block_start":
		// Content block started, no delta content yet
		choice["delta"] = map[string]interface{}{}
	case "content_block_delta":
		// Content delta
		if streamEvent.Delta != nil && streamEvent.Delta.Text != "" {
			choice["delta"] = map[string]interface{}{
				"content": streamEvent.Delta.Text,
			}
		}
	case "content_block_stop":
		// Content block ended
		choice["delta"] = map[string]interface{}{}
	case "message_delta":
		// Message delta with stop reason
		if streamEvent.Delta != nil && streamEvent.Delta.StopReason != "" {
			finishReason := "stop"
			switch streamEvent.Delta.StopReason {
			case "end_turn":
				finishReason = "stop"
			case "max_tokens":
				finishReason = "length"
			case "stop_sequence":
				finishReason = "stop"
			case "tool_use":
				finishReason = "tool_calls"
			}
			choice["finish_reason"] = finishReason
		}
		choice["delta"] = map[string]interface{}{}
	case "message_stop":
		// Message ended
		choice["finish_reason"] = "stop"
		choice["delta"] = map[string]interface{}{}
	default:
		// Unknown event type, return empty delta
		choice["delta"] = map[string]interface{}{}
	}

	return chunk, nil
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
