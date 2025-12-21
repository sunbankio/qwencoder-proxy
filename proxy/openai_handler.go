// Package proxy provides HTTP handlers for the proxy server
package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sunbankio/qwencoder-proxy/converter"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
)

// OpenAIHandler handles OpenAI-compatible requests and routes them to appropriate providers
type OpenAIHandler struct {
	factory   *provider.Factory
	convFactory *converter.Factory
	logger    *logging.Logger
}

// NewOpenAIHandler creates a new OpenAI-compatible handler
func NewOpenAIHandler(factory *provider.Factory, convFactory *converter.Factory) *OpenAIHandler {
	return &OpenAIHandler{
		factory:     factory,
		convFactory: convFactory,
		logger:      logging.NewLogger(),
	}
}

// ServeHTTP handles HTTP requests for OpenAI-compatible routes
func (h *OpenAIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse the path: /v1/chat/completions, /v1/models, etc.
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	path = strings.TrimPrefix(path, "/")

	h.logger.DebugLog("[OpenAI Handler] Request: %s %s", r.Method, path)

	switch {
	case path == "models" && r.Method == http.MethodGet:
		h.handleListModels(w, r)
	case path == "chat/completions" && r.Method == http.MethodPost:
		h.handleChatCompletions(w, r)
	default:
		// Fall back to original proxy handler for other paths
		ProxyHandler(w, r)
	}
}

// handleListModels handles GET /v1/models
func (h *OpenAIHandler) handleListModels(w http.ResponseWriter, r *http.Request) {
	// For now, return a simple response - in a real implementation we'd aggregate models from all providers
	models := []map[string]interface{}{
		{
			"id":       "gemini-2.5-flash",
			"object":   "model",
			"created":  1677648736,
			"owned_by": "google",
		},
		{
			"id":       "gemini-2.5-pro",
			"object":   "model",
			"created":  1677648736,
			"owned_by": "google",
		},
		{
			"id":       "claude-sonnet-4-5",
			"object":   "model",
			"created":  1677648736,
			"owned_by": "anthropic",
		},
		{
			"id":       "claude-opus-4-5",
			"object":   "model",
			"created":  1677648736,
			"owned_by": "anthropic",
		},
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   models,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to encode response: %v", err)
	}
}

// handleChatCompletions handles POST /v1/chat/completions
func (h *OpenAIHandler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var openaiReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&openaiReq); err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to decode request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Extract model from request
	model, ok := openaiReq["model"].(string)
	if !ok {
		http.Error(w, "Model is required", http.StatusBadRequest)
		return
	}

	// Determine which provider to use based on model
	provider, err := h.factory.GetByModel(model)
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to get provider for model %s: %v", model, err)
		http.Error(w, fmt.Sprintf("Provider not found for model: %s", model), http.StatusBadRequest)
		return
	}

	// Get the appropriate converter based on the provider's protocol
	conv, err := h.convFactory.Get(provider.Protocol())
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to get converter for protocol %s: %v", provider.Protocol(), err)
		http.Error(w, fmt.Sprintf("Converter not found for protocol: %s", provider.Protocol()), http.StatusInternalServerError)
		return
	}

	// Convert OpenAI request to provider's native format
	nativeReq, err := conv.FromOpenAIRequest(openaiReq)
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to convert request: %v", err)
		http.Error(w, "Failed to convert request format", http.StatusInternalServerError)
		return
	}

	// Check if streaming
	isStreaming := false
	if streamVal, ok := openaiReq["stream"].(bool); ok {
		isStreaming = streamVal
	}

	if isStreaming {
		h.handleStreamChatCompletions(w, r, provider, conv, nativeReq, model)
	} else {
		h.handleNonStreamChatCompletions(w, r, provider, conv, nativeReq, model)
	}
}

// handleNonStreamChatCompletions handles non-streaming chat completions
func (h *OpenAIHandler) handleNonStreamChatCompletions(w http.ResponseWriter, r *http.Request, provider provider.Provider, conv converter.Converter, nativeReq interface{}, model string) {
	ctx := r.Context()
	nativeResp, err := provider.GenerateContent(ctx, model, nativeReq)
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] GenerateContent failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert response back to OpenAI format
	openaiResp, err := conv.ToOpenAIResponse(nativeResp, model)
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to convert response: %v", err)
		http.Error(w, "Failed to convert response format", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
		h.logger.ErrorLog("[OpenAI Handler] Failed to encode response: %v", err)
	}
}

// handleStreamChatCompletions handles streaming chat completions
func (h *OpenAIHandler) handleStreamChatCompletions(w http.ResponseWriter, r *http.Request, provider provider.Provider, conv converter.Converter, nativeReq interface{}, model string) {
	ctx := r.Context()
	stream, err := provider.GenerateContentStream(ctx, model, nativeReq)
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] GenerateContentStream failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	// Set streaming headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Stream the response - for now, just pass through the raw stream
	// In a real implementation, we'd convert each chunk to OpenAI format
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				h.logger.ErrorLog("[OpenAI Handler] Write error: %v", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			h.logger.ErrorLog("[OpenAI Handler] Stream read error: %v", err)
			return
		}
	}
}

// RegisterOpenAIRoutes registers OpenAI-compatible routes
func RegisterOpenAIRoutes(mux *http.ServeMux, factory *provider.Factory, convFactory *converter.Factory) {
	handler := NewOpenAIHandler(factory, convFactory)
	
	// Replace the default proxy handler with our OpenAI handler for /v1 routes
	// Keep the original handler for other routes
	mux.Handle("/v1/", handler)
}