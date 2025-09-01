package proxy

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChunkToJson(t *testing.T) {
	tests := []struct {
		name     string
		chunk    string
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name:    "Valid chunk with content",
			chunk:   `{"choices":[{"finish_reason":null,"logprobs":null,"delta":{"content":"Hello"},"index":0}],"object":"chat.completion.chunk","usage":null,"created":1756647301,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-0eca7aef-dd7d-944c-8795-31b539fe4c3d"}`,
			wantErr: false,
		},
		{
			name:    "Valid chunk with empty content",
			chunk:   `{"choices":[{"finish_reason":"stop","delta":{"content":""},"index":0,"logprobs":null}],"object":"chat.completion.chunk","usage":null,"created":1756647301,"system_fingerprint":null,"model":"qwen3-coder-plus","id":"chatcmpl-0eca7aef-dd7d-944c-8795-31b539fe4c3d"}`,
			wantErr: false,
		},
		{
			name:    "DONE chunk",
			chunk:   `[DONE]`,
			wantErr: true, // Expecting error as [DONE] is not a valid JSON object
		},
		{
			name:    "Malformed JSON",
			chunk:   `{"choices":[{"delta":{"content":"Hello"}`,
			wantErr: true,
		},
		{
			name:    "Not a data chunk (should be handled before chunkToJson)",
			chunk:   `event: some_event`,
			wantErr: true, // chunkToJson expects JSON, so this should fail parsing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunkToJson(tt.chunk)

			if tt.wantErr {
				if result != nil {
					t.Errorf("chunkToJson() = %v, want nil for error case", result)
				}
			} else {
				if result == nil {
					t.Errorf("chunkToJson() = nil, want valid JSON for success case")
					return
				}

				// Optionally, unmarshal the expected string into a map for comparison if tt.expected is provided
				if tt.expected != nil {
					var expectedMap map[string]interface{}
					err := json.Unmarshal([]byte(tt.chunk), &expectedMap) // No "data:" prefix here
					if err != nil {
						t.Fatalf("Failed to unmarshal expected JSON for test setup: %v", err)
					}
					if !reflect.DeepEqual(result, expectedMap) {
						t.Errorf("chunkToJson() = %v, want %v", result, expectedMap)
					}
				}
			}
		})
	}
}