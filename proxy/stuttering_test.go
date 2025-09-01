package proxy

import (
	"testing"
)

func TestStutteringProcess(t *testing.T) {
	tests := []struct {
		name              string
		buf               string
		currentChunkData  string
		expectedStuttering bool
	}{
		{
			name:              "First chunk should be buffered (stuttering)",
			buf:               "",
			currentChunkData:  `{"choices":[{"delta":{"content":"Hello"}}]}`,
			expectedStuttering: true,
		},
		{
			name:              "Continuation chunk (stuttering continues)",
			buf:               `{"choices":[{"delta":{"content":"Hello"}}]}`,
			currentChunkData:  `{"choices":[{"delta":{"content":"Hello world"}}]}`,
			expectedStuttering: true,
		},
		{
			name:              "Non-continuation chunk (stuttering resolved)",
			buf:               `{"choices":[{"delta":{"content":"Hello"}}]}`,
			currentChunkData:  `{"choices":[{"delta":{"content":"World"}}]}`,
			expectedStuttering: false,
		},
		{
			name:              "DONE message (stuttering resolved)",
			buf:               `{"choices":[{"delta":{"content":"Hello"}}]}`,
			currentChunkData:  `[DONE]`,
			expectedStuttering: false,
		},
		{
			name:              "Malformed JSON (stuttering resolved)",
			buf:               `{"choices":[{"delta":{"content":"Hello"}}]}`,
			currentChunkData:  `{"choices":[{"delta":{"content":"Hello"}`,
			expectedStuttering: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stutteringProcess(tt.buf, tt.currentChunkData)
			if result != tt.expectedStuttering {
				t.Errorf("stutteringProcess(%q, %q) = %v, want %v", tt.buf, tt.currentChunkData, result, tt.expectedStuttering)
			}
		})
	}
}