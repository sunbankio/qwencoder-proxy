package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleStreamingResponseV2(t *testing.T) {
	tests := []struct {
		name           string
		responseBody   string
		expectedOutput []string
		expectError    bool
	}{
		{
			name: "Normal streaming flow with stuttering",
			responseBody: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				`data: {"choices":[{"delta":{"content":"Hello world"}}]}`,
				`data: {"choices":[{"delta":{"content":"Different content"}}]}`,
				`data: [DONE]`,
				"",
			}, "\n"),
			expectedOutput: []string{
				"Hello world",
				"Different content",
				"[DONE]",
			},
			expectError: false,
		},
		{
			name: "Stream with malformed JSON",
			responseBody: strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				`data: {"malformed": json`,
				`data: {"choices":[{"delta":{"content":"World"}}]}`,
				`data: [DONE]`,
				"",
			}, "\n"),
			expectedOutput: []string{
				"Hello",
				"World",
				"[DONE]",
			},
			expectError: false,
		},
		{
			name: "Stream with non-content chunks",
			responseBody: strings.Join([]string{
				`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				`data: [DONE]`,
				"",
			}, "\n"),
			expectedOutput: []string{
				"role",
				"Hello",
				"[DONE]",
			},
			expectError: false,
		},
		{
			name: "Stream with empty lines",
			responseBody: strings.Join([]string{
				"",
				`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n"),
			expectedOutput: []string{
				"Hello",
				"[DONE]",
			},
			expectError: false,
		},
		{
			name: "Immediate DONE",
			responseBody: strings.Join([]string{
				`data: [DONE]`,
				"",
			}, "\n"),
			expectedOutput: []string{
				"[DONE]",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock HTTP response
			recorder := httptest.NewRecorder()
			wrapper := &responseWriterWrapper{
				ResponseWriter: recorder,
				statusCode:     http.StatusOK,
			}

			// Create a mock response with the test body
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.responseBody)),
			}
			resp.Header.Set("Content-Type", "text/event-stream")

			ctx := context.Background()

			// Call the streaming handler
			handleStreamingResponse(wrapper, resp, ctx)

			// Check the output
			output := recorder.Body.String()
			
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't. Output: %s", expected, output)
				}
			}
		})
	}
}

func TestHandleStreamingResponse(t *testing.T) {
	responseBody := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}
	resp.Header.Set("Content-Type", "text/event-stream")

	ctx := context.Background()

	// This should not panic or error
	HandleStreamingResponse(wrapper, resp, ctx)

	// Basic check that something was written
	output := recorder.Body.String()
	if len(output) == 0 {
		t.Error("Expected some output, got empty string")
	}
}

func TestStreamingResponseV2_ClientDisconnection(t *testing.T) {
	responseBody := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":"World"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}

	// Create a cancelled context to simulate client disconnection
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should handle the cancellation gracefully
	handleStreamingResponse(wrapper, resp, ctx)

	// The function should return without panicking
	// Output might be empty or partial due to immediate cancellation
}

// Benchmark the streaming handler
func BenchmarkHandleStreamingResponse(b *testing.B) {
	responseBody := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":"Hello world"}}]}`,
		`data: {"choices":[{"delta":{"content":"Different content"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		wrapper := &responseWriterWrapper{
			ResponseWriter: recorder,
			statusCode:     http.StatusOK,
		}

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		}

		ctx := context.Background()
		handleStreamingResponse(wrapper, resp, ctx)
	}
}

// Benchmark the streaming handler
func BenchmarkStreamingHandler(b *testing.B) {
	responseBody := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":"Hello world"}}]}`,
		`data: {"choices":[{"delta":{"content":"Different content"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		wrapper := &responseWriterWrapper{
			ResponseWriter: recorder,
			statusCode:     http.StatusOK,
		}

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		}

		ctx := context.Background()
		handleStreamingResponse(wrapper, resp, ctx)
	}
}