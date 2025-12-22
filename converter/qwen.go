// Package converter provides format conversion between different LLM API formats
package converter

import (
	"fmt"
	"time"

	"github.com/sunbankio/qwencoder-proxy/provider"
)

// QwenConverter handles Qwen to/from OpenAI format conversions
type QwenConverter struct{}

// NewQwenConverter creates a new Qwen converter
func NewQwenConverter() *QwenConverter {
	return &QwenConverter{}
}

// ToOpenAIRequest converts Qwen format to OpenAI format
func (c *QwenConverter) ToOpenAIRequest(native interface{}) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return native, nil
}

// ToOpenAIResponse converts Qwen format to OpenAI format
func (c *QwenConverter) ToOpenAIResponse(native interface{}, model string) (interface{}, error) {
	// Convert Qwen response to OpenAI format
	qwenResp, ok := native.(map[string]interface{})
	if !ok {
		return native, nil
	}

	// Check if it's an error response
	if qwenResp["error"] != nil {
		return native, nil
	}

	// Convert Qwen response to OpenAI format
	openAIResp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%s", model),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{},
		"usage": map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}

	// Handle different Qwen response formats
	if choicesData, exists := qwenResp["choices"]; exists {
		// If choices is already in the expected format
		if choices, ok := choicesData.([]interface{}); ok {
			var openAIChoices []interface{}
			for i, choice := range choices {
				if choiceMap, ok := choice.(map[string]interface{}); ok {
					message := map[string]interface{}{
						"role":    "assistant",
						"content": "",
					}
					
					if msg, msgExists := choiceMap["message"]; msgExists {
						if msgMap, msgOk := msg.(map[string]interface{}); msgOk {
							if content, contentOk := msgMap["content"].(string); contentOk {
								message["content"] = content
							} else if content, contentOk := msgMap["content"]; contentOk {
								// Handle content as interface{} if it's not a string
								if contentStr, ok := content.(string); ok {
									message["content"] = contentStr
								}
							}
						}
					}
					
					finishReason := "stop"
					if reason, reasonExists := choiceMap["finish_reason"]; reasonExists {
						if reasonStr, ok := reason.(string); ok {
							finishReason = reasonStr
						}
					}
					
					openAIChoice := map[string]interface{}{
						"index":   i,
						"message": message,
						"finish_reason": finishReason,
					}
					openAIChoices = append(openAIChoices, openAIChoice)
				}
			}
			openAIResp["choices"] = openAIChoices
		}
	} else if output, ok := qwenResp["output"].(map[string]interface{}); ok {
		// Handle the output.choices format
		if choices, ok := output["choices"].([]interface{}); ok && len(choices) > 0 {
			var openAIChoices []interface{}
			for i, choice := range choices {
				if choiceMap, ok := choice.(map[string]interface{}); ok {
					message := map[string]interface{}{
						"role":    "assistant",
						"content": "",
					}
					
					if msg, exists := choiceMap["message"]; exists {
						if msgMap, msgOk := msg.(map[string]interface{}); msgOk {
							if content, contentOk := msgMap["content"].(string); contentOk {
								message["content"] = content
							} else if content, contentOk := msgMap["content"]; contentOk {
								if contentStr, ok := content.(string); ok {
									message["content"] = contentStr
								}
							}
						}
					}
					
					finishReason := "stop"
					if reason, reasonExists := choiceMap["finish_reason"]; reasonExists {
						if reasonStr, ok := reason.(string); ok {
							finishReason = reasonStr
						}
					}
					
					openAIChoice := map[string]interface{}{
						"index":   i,
						"message": message,
						"finish_reason": finishReason,
					}
					openAIChoices = append(openAIChoices, openAIChoice)
				}
			}
			openAIResp["choices"] = openAIChoices
		}
	}

	// Handle usage if present
	if usage, exists := qwenResp["usage"]; exists {
		if usageMap, ok := usage.(map[string]interface{}); ok {
			if promptTokens, ok := usageMap["prompt_tokens"]; ok {
				if pt, ok := promptTokens.(float64); ok {
					openAIResp["usage"].(map[string]interface{})["prompt_tokens"] = int(pt)
				} else if pt, ok := promptTokens.(int); ok {
					openAIResp["usage"].(map[string]interface{})["prompt_tokens"] = pt
				}
			}
			if completionTokens, ok := usageMap["completion_tokens"]; ok {
				if ct, ok := completionTokens.(float64); ok {
					openAIResp["usage"].(map[string]interface{})["completion_tokens"] = int(ct)
				} else if ct, ok := completionTokens.(int); ok {
					openAIResp["usage"].(map[string]interface{})["completion_tokens"] = ct
				}
			}
			if totalTokens, ok := usageMap["total_tokens"]; ok {
				if tt, ok := totalTokens.(float64); ok {
					openAIResp["usage"].(map[string]interface{})["total_tokens"] = int(tt)
				} else if tt, ok := totalTokens.(int); ok {
					openAIResp["usage"].(map[string]interface{})["total_tokens"] = tt
				}
			}
		}
	} else if output, ok := qwenResp["output"].(map[string]interface{}); ok {
		if usage, exists := output["usage"]; exists {
			if usageMap, ok := usage.(map[string]interface{}); ok {
				if promptTokens, ok := usageMap["prompt_tokens"]; ok {
					if pt, ok := promptTokens.(float64); ok {
						openAIResp["usage"].(map[string]interface{})["prompt_tokens"] = int(pt)
					} else if pt, ok := promptTokens.(int); ok {
						openAIResp["usage"].(map[string]interface{})["prompt_tokens"] = pt
					}
				}
				if completionTokens, ok := usageMap["completion_tokens"]; ok {
					if ct, ok := completionTokens.(float64); ok {
						openAIResp["usage"].(map[string]interface{})["completion_tokens"] = int(ct)
					} else if ct, ok := completionTokens.(int); ok {
						openAIResp["usage"].(map[string]interface{})["completion_tokens"] = ct
					}
				}
				if totalTokens, ok := usageMap["total_tokens"]; ok {
					if tt, ok := totalTokens.(float64); ok {
						openAIResp["usage"].(map[string]interface{})["total_tokens"] = int(tt)
					} else if tt, ok := totalTokens.(int); ok {
						openAIResp["usage"].(map[string]interface{})["total_tokens"] = tt
					}
				}
			}
		}
	}

	return openAIResp, nil
}

// ToOpenAIStreamChunk converts Qwen format to OpenAI format
func (c *QwenConverter) ToOpenAIStreamChunk(native interface{}, model string) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return native, nil
}

// FromOpenAIRequest converts OpenAI format to Qwen format
// For Qwen API, we can pass through the OpenAI format as-is since Qwen supports OpenAI-compatible format
func (c *QwenConverter) FromOpenAIRequest(req interface{}) (interface{}, error) {
	// For Qwen API, we can pass the OpenAI request format directly since Qwen supports OpenAI-compatible format
	return req, nil
}

// FromOpenAIResponse converts OpenAI format to Qwen format
func (c *QwenConverter) FromOpenAIResponse(resp interface{}) (interface{}, error) {
	// For now, return as-is - we'll implement proper conversion later
	return resp, nil
}

// Protocol returns the native protocol
func (c *QwenConverter) Protocol() provider.ProtocolType {
	return provider.ProtocolQwen
}