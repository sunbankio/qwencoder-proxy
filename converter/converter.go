// Package converter provides format conversion between different LLM API formats
package converter

import (
	"github.com/sunbankio/qwencoder-proxy/provider"
)

// Converter handles protocol translation between different API formats
type Converter interface {
	// ToOpenAI converts native format to OpenAI format
	ToOpenAIRequest(native interface{}) (interface{}, error)
	ToOpenAIResponse(native interface{}, model string) (interface{}, error)
	ToOpenAIStreamChunk(native interface{}, model string) (interface{}, error)

	// FromOpenAI converts OpenAI format to native format
	FromOpenAIRequest(req interface{}) (interface{}, error)
	FromOpenAIResponse(resp interface{}) (interface{}, error)

	// Protocol returns the native protocol this converter handles
	Protocol() provider.ProtocolType
}

// Factory manages converter instances
type Factory struct {
	converters map[provider.ProtocolType]Converter
}

// NewFactory creates a new converter factory
func NewFactory() *Factory {
	f := &Factory{
		converters: make(map[provider.ProtocolType]Converter),
	}
	// Register default converters
	f.Register(NewOpenAIConverter())
	f.Register(NewGeminiConverter())
	f.Register(NewClaudeConverter())
	return f
}

// Register adds a converter to the factory
func (f *Factory) Register(c Converter) {
	f.converters[c.Protocol()] = c
}

// Get returns a converter for the given protocol
func (f *Factory) Get(protocol provider.ProtocolType) (Converter, error) {
	c, ok := f.converters[protocol]
	if !ok {
		return nil, &ConverterError{Protocol: protocol, Message: "converter not found"}
	}
	return c, nil
}

// ConvertError represents an error during conversion
type ConverterError struct {
	Protocol provider.ProtocolType
	Message  string
}

func (e *ConverterError) Error() string {
	return "converter error for protocol " + string(e.Protocol) + ": " + e.Message
}