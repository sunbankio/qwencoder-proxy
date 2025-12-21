// Package converter provides format conversion between different LLM API formats
package converter

import (
	"github.com/sunbankio/qwencoder-proxy/provider"
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
	// For now, return as-is - we'll implement proper conversion later
	return req, nil
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