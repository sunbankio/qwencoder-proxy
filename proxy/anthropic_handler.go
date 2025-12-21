// Package proxy provides HTTP handlers for the proxy server
package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/provider/kiro"
)

// AnthropicHandler handles requests to /anthropic/* routes
type AnthropicHandler struct {
	provider *kiro.Provider
	logger   *logging.Logger
}

// NewAnthropicHandler creates a new Anthropic route handler
func NewAnthropicHandler(p *kiro.Provider) *AnthropicHandler {
	return &AnthropicHandler{
		provider: p,
		logger:   logging.NewLogger(),
	}
}

// ServeHTTP handles HTTP requests for Anthropic routes
func (h *AnthropicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse the path: /anthropic/models, /anthropic/messages, etc.
	path := strings.TrimPrefix(r.URL.Path, "/anthropic")
	path = strings.TrimPrefix(path, "/")

	h.logger.DebugLog("[Anthropic Handler] Request: %s %s", r.Method, path)

	switch {
	case path == "models" && r.Method == http.MethodGet:
		h.handleListModels(w, r)
	case path == "messages" && r.Method == http.MethodPost:
		h.handleMessages(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleListModels handles GET /anthropic/models
func (h *AnthropicHandler) handleListModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	models, err := h.provider.ListModels(ctx)
	if err != nil {
		h.logger.ErrorLog("[Anthropic Handler] Failed to list models: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(models); err != nil {
		h.logger.ErrorLog("[Anthropic Handler] Failed to encode response: %v", err)
	}
}

// handleMessages handles POST /anthropic/messages
func (h *AnthropicHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var request kiro.ClaudeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.logger.ErrorLog("[Anthropic Handler] Failed to decode request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if streaming
	isStreaming := request.Stream

	if isStreaming {
		h.handleStreamMessages(w, r, &request)
	} else {
		h.handleNonStreamMessages(w, r, &request)
	}
}

// handleNonStreamMessages handles non-streaming messages
func (h *AnthropicHandler) handleNonStreamMessages(w http.ResponseWriter, r *http.Request, request *kiro.ClaudeRequest) {
	ctx := r.Context()
	response, err := h.provider.GenerateContent(ctx, request.Model, request)
	if err != nil {
		h.logger.ErrorLog("[Anthropic Handler] GenerateContent failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.ErrorLog("[Anthropic Handler] Failed to encode response: %v", err)
	}
}

// handleStreamMessages handles streaming messages
func (h *AnthropicHandler) handleStreamMessages(w http.ResponseWriter, r *http.Request, request *kiro.ClaudeRequest) {
	ctx := r.Context()
	stream, err := h.provider.GenerateContentStream(ctx, request.Model, request)
	if err != nil {
		h.logger.ErrorLog("[Anthropic Handler] GenerateContentStream failed: %v", err)
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

	// Stream the response
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				h.logger.ErrorLog("[Anthropic Handler] Write error: %v", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			h.logger.ErrorLog("[Anthropic Handler] Stream read error: %v", err)
			return
		}
	}
}

// RegisterAnthropicRoutes registers Anthropic routes with the given provider factory
func RegisterAnthropicRoutes(mux *http.ServeMux, factory *provider.Factory) error {
	p, err := factory.Get(provider.ProviderKiro)
	if err != nil {
		return err
	}

	kiroProvider, ok := p.(*kiro.Provider)
	if !ok {
		return nil // Provider not available or wrong type
	}

	handler := NewAnthropicHandler(kiroProvider)
	mux.Handle("/anthropic/", handler)
	return nil
}