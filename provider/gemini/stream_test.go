package gemini

import (
	"context"
	"io"
	"testing"

	"github.com/sunbankio/qwencoder-proxy/auth"
)

// TestRawStreamResponse tests the raw streaming response from Gemini
func TestRawStreamResponse(t *testing.T) {
	// Create a Gemini provider
	geminiAuth := auth.NewGeminiAuthenticator(nil)
	provider := NewProvider(geminiAuth)

	// Create a simple request
	request := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": "Hello"},
				},
			},
		},
	}

	// Get stream
	ctx := context.Background()
	stream, err := provider.GenerateContentStream(ctx, "gemini-2.5-flash", request)
	if err != nil {
		t.Fatalf("Failed to get stream: %v", err)
	}
	defer stream.Close()

	// Read and print raw stream
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("Failed to read stream: %v", err)
	}

	t.Logf("Raw stream response:\n%s", string(data))
}