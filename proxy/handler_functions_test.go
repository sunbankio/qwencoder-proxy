package proxy

import (
	"net/http/httptest"
	"testing"
)

func TestCheckIfStreaming(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    []byte
		expectedResult bool
	}{
		{
			name:           "Streaming request with stream=true",
			requestBody:    []byte(`{"stream": true, "model": "qwen3-coder-plus"}`),
			expectedResult: true,
		},
		{
			name:           "Non-streaming request with stream=false",
			requestBody:    []byte(`{"stream": false, "model": "qwen3-coder-plus"}`),
			expectedResult: false,
		},
		{
			name:           "Request without stream field",
			requestBody:    []byte(`{"model": "qwen3-coder-plus", "temperature": 0.7}`),
			expectedResult: false,
		},
		{
			name:           "Empty request body",
			requestBody:    []byte(``),
			expectedResult: false,
		},
		{
			name:           "Malformed JSON",
			requestBody:    []byte(`{"stream": true, "model": "qwen3-coder-plus"`),
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkIfStreaming(tt.requestBody)
			if result != tt.expectedResult {
				t.Errorf("checkIfStreaming() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestConstructTargetURL(t *testing.T) {
	tests := []struct {
		name           string
		requestPath    string
		targetEndpoint string
		expectedURL    string
	}{
		{
			name:           "Normal path construction",
			requestPath:    "/chat/completions",
			targetEndpoint: "https://portal.qwen.ai/v1",
			expectedURL:    "https://portal.qwen.ai/v1/chat/completions",
		},
		{
			name:           "Path with duplicate /v1",
			requestPath:    "/v1/chat/completions",
			targetEndpoint: "https://portal.qwen.ai/v1",
			expectedURL:    "https://portal.qwen.ai/v1/chat/completions",
		},
		{
			name:           "Root path",
			requestPath:    "/",
			targetEndpoint: "https://portal.qwen.ai/v1",
			expectedURL:    "https://portal.qwen.ai/v1/",
		},
		{
			name:           "Models path",
			requestPath:    "/models",
			targetEndpoint: "https://portal.qwen.ai/v1",
			expectedURL:    "https://portal.qwen.ai/v1/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructTargetURL(tt.requestPath, tt.targetEndpoint)
			if result != tt.expectedURL {
				t.Errorf("constructTargetURL() = %v, want %v", result, tt.expectedURL)
			}
		})
	}
}

func TestSetProxyHeaders(t *testing.T) {
	// Create a test request
	req := httptest.NewRequest("POST", "/chat/completions", nil)
	
	// Set proxy headers
	accessToken := "test-token-123"
	SetProxyHeaders(req, accessToken)
	
	// Test Authorization header
	authHeader := req.Header.Get("Authorization")
	expectedAuth := "Bearer " + accessToken
	if authHeader != expectedAuth {
		t.Errorf("Expected Authorization header to be %s, got %s", expectedAuth, authHeader)
	}
	
	// Test Content-Type header
	contentType := req.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type to be application/json, got %s", contentType)
	}
	
	// Test that other headers are set
	userAgent := req.Header.Get("User-Agent")
	if userAgent == "" {
		t.Error("Expected User-Agent header to be set")
	}
	
	cacheControl := req.Header.Get("X-DashScope-CacheControl")
	if cacheControl != "enable" {
		t.Errorf("Expected X-DashScope-CacheControl to be 'enable', got %s", cacheControl)
	}
}