// Package converter provides format conversion between different LLM API formats
package converter

import (
	"encoding/json"
	"fmt"

	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/provider/gemini"
)

// GeminiConverter handles Gemini to/from OpenAI format conversions
type GeminiConverter struct{}

// NewGeminiConverter creates a new Gemini converter
func NewGeminiConverter() *GeminiConverter {
	return &GeminiConverter{}
}

// ToOpenAIRequest converts Gemini format to OpenAI format
func (c *GeminiConverter) ToOpenAIRequest(native interface{}) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return native, nil
}

// ToOpenAIResponse converts Gemini format to OpenAI format
func (c *GeminiConverter) ToOpenAIResponse(native interface{}, model string) (interface{}, error) {
	// Convert Gemini response to OpenAI format
	geminiResp, ok := native.(*gemini.GeminiResponse)
	if !ok {
		// Try to convert from map if it's not already a GeminiResponse
		if data, ok := native.(map[string]interface{}); ok {
			jsonBytes, _ := json.Marshal(data)
			geminiResp = &gemini.GeminiResponse{}
			json.Unmarshal(jsonBytes, geminiResp)
		} else {
			return nil, fmt.Errorf("unexpected response type: %T", native)
		}
	}

	// Create OpenAI-compatible response
	openAIResp := map[string]interface{}{
		"id":    "chatcmpl-" + generateID(),
		"object": "chat.completion",
		"created": getCurrentTimestamp(),
		"model": model,
		"choices": []interface{}{},
		"usage": map[string]interface{}{
			"prompt_tokens": 0,
			"completion_tokens": 0,
			"total_tokens": 0,
		},
	}

	if geminiResp.UsageMetadata != nil {
		openAIResp["usage"] = map[string]interface{}{
			"prompt_tokens": geminiResp.UsageMetadata.PromptTokenCount,
			"completion_tokens": geminiResp.UsageMetadata.CandidatesTokenCount,
			"total_tokens": geminiResp.UsageMetadata.TotalTokenCount,
		}
	}

	choices := []interface{}{}
	for i, candidate := range geminiResp.Candidates {
		if candidate.Content != nil {
			var content string
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					content += part.Text
				}
			}

			choice := map[string]interface{}{
				"index": i,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": convertFinishReason(candidate.FinishReason),
			}
			choices = append(choices, choice)
		}
	}

	openAIResp["choices"] = choices

	return openAIResp, nil
}

// ToOpenAIStreamChunk converts Gemini format to OpenAI format for streaming
func (c *GeminiConverter) ToOpenAIStreamChunk(native interface{}, model string) (interface{}, error) {
	// For streaming, we need to convert the SSE data
	// This is more complex and would require parsing the SSE stream
	// For now, return as-is but in a proper implementation, this would convert each chunk
	return native, nil
}

// FromOpenAIRequest converts OpenAI format to Gemini format
func (c *GeminiConverter) FromOpenAIRequest(req interface{}) (interface{}, error) {
	// Convert OpenAI request format to Gemini format
	openAIReq, ok := req.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected request type: %T", req)
	}

	// Create Gemini request
	geminiReq := &gemini.GeminiRequest{
		Contents: []gemini.Content{},
	}

	// Convert messages
	if messages, ok := openAIReq["messages"].([]interface{}); ok {
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				role, _ := msgMap["role"].(string)
				content := convertContent(msgMap["content"])
				
				// Map OpenAI roles to Gemini roles
				geminiRole := role
				if role == "system" {
					// For system messages, we'll use the systemInstruction field
					geminiReq.SystemInstruction = &gemini.Content{
						Role:  "user",
						Parts: []gemini.Part{{Text: content}},
					}
					continue
				} else if role == "assistant" {
					geminiRole = "model"
				} else if role == "user" {
					geminiRole = "user"
				}
				
				geminiReq.Contents = append(geminiReq.Contents, gemini.Content{
					Role:  geminiRole,
					Parts: []gemini.Part{{Text: content}},
				})
			}
		}
	}

	// Convert generation config
	if temp, ok := openAIReq["temperature"].(float64); ok {
		if geminiReq.GenerationConfig == nil {
			geminiReq.GenerationConfig = &gemini.GenerationConfig{}
		}
		geminiReq.GenerationConfig.Temperature = &temp
	}

	if topP, ok := openAIReq["top_p"].(float64); ok {
		if geminiReq.GenerationConfig == nil {
			geminiReq.GenerationConfig = &gemini.GenerationConfig{}
		}
		geminiReq.GenerationConfig.TopP = &topP
	}

	if maxTokens, ok := openAIReq["max_tokens"].(float64); ok {
		maxTokensInt := int(maxTokens)
		if geminiReq.GenerationConfig == nil {
			geminiReq.GenerationConfig = &gemini.GenerationConfig{}
		}
		geminiReq.GenerationConfig.MaxOutputTokens = &maxTokensInt
	}

	return geminiReq, nil
}

// FromOpenAIResponse converts OpenAI format to Gemini format
func (c *GeminiConverter) FromOpenAIResponse(resp interface{}) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return resp, nil
}

// Protocol returns the native protocol
func (c *GeminiConverter) Protocol() provider.ProtocolType {
	return provider.ProtocolGemini
}

// Helper functions
func convertContent(content interface{}) string {
	if contentStr, ok := content.(string); ok {
		return contentStr
	}
	
	// Handle content as array of content parts (for vision models)
	if contentArr, ok := content.([]interface{}); ok {
		var result string
		for _, part := range contentArr {
			if partMap, ok := part.(map[string]interface{}); ok {
				if text, exists := partMap["text"]; exists {
					if textStr, ok := text.(string); ok {
						result += textStr
					}
				}
			}
		}
		return result
	}
	
	return fmt.Sprintf("%v", content)
}

func convertFinishReason(geminiFinishReason string) string {
	switch geminiFinishReason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	default:
		return "stop" // default to stop
	}
}

func generateID() string {
	// Simple ID generation for demo purposes
	return "gen_123456"
}

func getCurrentTimestamp() int64 {
	// Simple timestamp for demo purposes
	return 1234567890
}