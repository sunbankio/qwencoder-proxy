// Package converter provides format conversion between different LLM API formats
package converter

import (
	"github.com/sunbankio/qwencoder-proxy/provider"
)

// OpenAIConverter handles OpenAI format conversions
type OpenAIConverter struct{}

// NewOpenAIConverter creates a new OpenAI converter
func NewOpenAIConverter() *OpenAIConverter {
	return &OpenAIConverter{}
}

// ToOpenAIRequest converts native format to OpenAI format
func (c *OpenAIConverter) ToOpenAIRequest(native interface{}) (interface{}, error) {
	// For OpenAI format, no conversion needed
	return native, nil
}

// ToOpenAIResponse converts native format to OpenAI format
func (c *OpenAIConverter) ToOpenAIResponse(native interface{}, model string) (interface{}, error) {
	// For OpenAI format, no conversion needed
	return native, nil
}

// ToOpenAIStreamChunk converts native format to OpenAI format
func (c *OpenAIConverter) ToOpenAIStreamChunk(native interface{}, model string) (interface{}, error) {
	// For OpenAI format, no conversion needed
	return native, nil
}

// FromOpenAIRequest converts OpenAI format to native format
func (c *OpenAIConverter) FromOpenAIRequest(req interface{}) (interface{}, error) {
	// For OpenAI format, no conversion needed
	return req, nil
}

// FromOpenAIResponse converts OpenAI format to native format
func (c *OpenAIConverter) FromOpenAIResponse(resp interface{}) (interface{}, error) {
	// For OpenAI format, no conversion needed
	return resp, nil
}

// Protocol returns the native protocol
func (c *OpenAIConverter) Protocol() provider.ProtocolType {
	return provider.ProtocolOpenAI
}