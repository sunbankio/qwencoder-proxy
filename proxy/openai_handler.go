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
	"github.com/sunbankio/qwencoder-proxy/provider/gemini"
	"github.com/sunbankio/qwencoder-proxy/provider/kiro"
)

// OpenAIHandler handles OpenAI-compatible requests and routes them to appropriate providers
type OpenAIHandler struct {
	factory     *provider.Factory
	convFactory *converter.Factory
	logger      *logging.Logger
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
		// For other /v1 paths that we don't specifically handle, return an error
		// rather than falling back to the old proxy handler
		http.Error(w, fmt.Sprintf("Unsupported OpenAI-compatible endpoint: %s", path), http.StatusNotFound)
	}
}

// handleListModels handles GET /v1/models
func (h *OpenAIHandler) handleListModels(w http.ResponseWriter, r *http.Request) {
	// Get models from all providers
	providers := []provider.ProviderType{
		provider.ProviderQwen,
		provider.ProviderGeminiCLI,
		provider.ProviderKiro,
		provider.ProviderAntigravity,
		provider.ProviderIFlow,
	}

	var allModels []map[string]interface{}

	for _, providerType := range providers {
		provider, err := h.factory.Get(providerType)
		if err != nil {
			h.logger.ErrorLog("[OpenAI Handler] Failed to get provider %s: %v", providerType, err)
			continue
		}

		modelsData, err := provider.ListModels(r.Context())
		if err != nil {
			h.logger.ErrorLog("[OpenAI Handler] Failed to list models for provider %s: %v", providerType, err)
			continue
		}

		// Refresh the model provider mapping for this provider
		if refreshErr := h.factory.RefreshProviderModels(r.Context(), providerType); refreshErr != nil {
			h.logger.ErrorLog("[OpenAI Handler] Failed to refresh provider models for %s: %v", providerType, refreshErr)
		}

		// Determine the correct owned_by value based on provider type
		ownedBy := string(providerType)
		if string(providerType) == "gemini-cli" {
			ownedBy = "gemini"
		}
		
		// Handle the response based on its type
			switch v := modelsData.(type) {
			case *gemini.GeminiModelsResponse:
				for _, model := range v.Models {
					allModels = append(allModels, map[string]interface{}{
						"id":       model.Name,
						"object":   "model",
						"created":  1677648736,
						"owned_by": ownedBy,
					})
				}
			case *kiro.ClaudeModelsResponse:
				for _, model := range v.Data {
					allModels = append(allModels, map[string]interface{}{
						"id":       model.ID,
						"object":   "model",
						"created":  1677648736,
						"owned_by": ownedBy,
					})
				}
			case map[string]interface{}:
				// Handle generic map responses (like from Qwen)
				if data, ok := v["data"].([]interface{}); ok {
					for _, model := range data {
						if modelMap, ok := model.(map[string]interface{}); ok {
							// Ensure the model has the required fields
							if _, exists := modelMap["id"]; !exists {
								// If no id field, skip this model
								continue
							}
							if _, exists := modelMap["object"]; !exists {
								modelMap["object"] = "model"
							}
							if _, exists := modelMap["created"]; !exists {
								modelMap["created"] = 1677648736
							}
							if _, exists := modelMap["owned_by"]; !exists {
								modelMap["owned_by"] = ownedBy
							}
							allModels = append(allModels, modelMap)
						}
					}
				} else {
					// Handle case where the response is not in the expected format
					h.logger.ErrorLog("[OpenAI Handler] Unexpected models data format for provider %s", providerType)
				}
			}	}

	response := map[string]interface{}{
		"object": "list",
		"data":   allModels,
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

	// Log provider selection
	h.logger.DebugLog("[OpenAI Handler] Selected provider %s for model %s", provider.Name(), model)

	// Check if there's a last successful provider for this model
	if lastProvider, hasLastSuccess := h.factory.GetLastSuccessProvider(model); hasLastSuccess {
		h.logger.DebugLog("[OpenAI Handler] Last successful provider for model %s was %s", model, lastProvider)
	} else {
		h.logger.DebugLog("[OpenAI Handler] No previous successful provider for model %s", model)
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
	nativeResp, err := provider.GenerateContent(r.Context(), model, nativeReq)
	if err != nil {
		h.logger.ErrorLog("[OpenAI Handler] GenerateContent failed with provider %s: %v", provider.Name(), err)
		
		// Try to find an alternative provider
		if altProvider, altErr := h.factory.GetAlternativeProvider(model, provider.Name()); altErr == nil {
			h.logger.DebugLog("[OpenAI Handler] Retrying with alternative provider %s for model %s", altProvider.Name(), model)
			
			// Get converter for the alternative provider
			altConv, convErr := h.convFactory.Get(altProvider.Protocol())
			if convErr != nil {
				h.logger.ErrorLog("[OpenAI Handler] Failed to get converter for alternative provider %s: %v", altProvider.Name(), convErr)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			
			// Retry with alternative provider using the same native request
			// Note: This assumes compatible request formats between providers
			altResp, altErr := altProvider.GenerateContent(r.Context(), model, nativeReq)
			if altErr != nil {
				h.logger.ErrorLog("[OpenAI Handler] Alternative provider %s also failed: %v", altProvider.Name(), altErr)
				http.Error(w, fmt.Sprintf("All providers failed for model %s. Primary error: %v, Alternative error: %v", model, err, altErr), http.StatusInternalServerError)
				return
			}
			
			// Record success for alternative provider
			h.factory.RecordSuccess(model, altProvider.Name())
			h.logger.DebugLog("[OpenAI Handler] Recorded success for alternative provider %s with model %s", altProvider.Name(), model)
			
			// Convert response back to OpenAI format
			openaiResp, convErr := altConv.ToOpenAIResponse(altResp, model)
			if convErr != nil {
				h.logger.ErrorLog("[OpenAI Handler] Failed to convert alternative response: %v", convErr)
				http.Error(w, "Failed to convert response format", http.StatusInternalServerError)
				return
			}
			
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
				h.logger.ErrorLog("[OpenAI Handler] Failed to encode alternative response: %v", err)
			}
			return
		}
		
		h.logger.ErrorLog("[OpenAI Handler] No alternative provider available for model %s", model)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Record success for routing
	h.factory.RecordSuccess(model, provider.Name())
	h.logger.DebugLog("[OpenAI Handler] Recorded success for provider %s with model %s", provider.Name(), model)

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

	// Record success for routing
	h.factory.RecordSuccess(model, provider.Name())
	h.logger.DebugLog("[OpenAI Handler] Recorded success for streaming provider %s with model %s", provider.Name(), model)

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

	// Stream the response - convert each chunk to OpenAI format
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			// For now, just pass through the raw stream
			// In a complete implementation, we would convert each SSE chunk to OpenAI format
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

// ProviderSpecificHandler forces all requests to use a specific provider
type ProviderSpecificHandler struct {
	factory     *provider.Factory
	convFactory *converter.Factory
	providerType provider.ProviderType
	logger      *logging.Logger
}

// NewProviderSpecificHandler creates a new handler that forces requests to use a specific provider
func NewProviderSpecificHandler(factory *provider.Factory, convFactory *converter.Factory, providerType provider.ProviderType) *ProviderSpecificHandler {
	return &ProviderSpecificHandler{
		factory:     factory,
		convFactory: convFactory,
		providerType: providerType,
		logger:      logging.NewLogger(),
	}
}

// ServeHTTP handles HTTP requests for provider-specific OpenAI-compatible routes
func (h *ProviderSpecificHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse the path: remove the provider prefix (e.g., /qwen/v1/) to get the actual API path
	path := r.URL.Path
	if strings.HasPrefix(path, "/qwen/v1/") {
		path = strings.TrimPrefix(path, "/qwen/v1")
	} else if strings.HasPrefix(path, "/gemini/v1/") {
		path = strings.TrimPrefix(path, "/gemini/v1")
	} else if strings.HasPrefix(path, "/kiro/v1/") {
		path = strings.TrimPrefix(path, "/kiro/v1")
	} else if strings.HasPrefix(path, "/antigravity/v1/") {
		path = strings.TrimPrefix(path, "/antigravity/v1")
	}
	path = strings.TrimPrefix(path, "/")

	h.logger.DebugLog("[Provider-Specific Handler] Request: %s %s (Provider: %s)", r.Method, path, h.providerType)

	// Get the specific provider
	provider, err := h.factory.Get(h.providerType)
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Provider not found: %s", h.providerType)
		http.Error(w, fmt.Sprintf("Provider not available: %s", h.providerType), http.StatusInternalServerError)
		return
	}

	switch {
	case path == "models" && r.Method == http.MethodGet:
		h.handleListModels(w, r, provider)
	case path == "chat/completions" && r.Method == http.MethodPost:
		h.handleChatCompletionsWithProvider(w, r, provider)
	default:
		http.Error(w, fmt.Sprintf("Unsupported OpenAI-compatible endpoint: %s", path), http.StatusNotFound)
	}
}

// handleListModels handles GET /v1/models for a specific provider
func (h *ProviderSpecificHandler) handleListModels(w http.ResponseWriter, r *http.Request, provider provider.Provider) {
	// Get models from the specific provider
	modelsData, err := provider.ListModels(r.Context())
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to list models: %v", err)
		http.Error(w, "Failed to list models", http.StatusInternalServerError)
		return
	}

	// Format the models response in OpenAI-compatible format
	var allModels []map[string]interface{}

	// Determine the correct owned_by value based on provider type
	ownedBy := string(h.providerType)
	if string(h.providerType) == "gemini-cli" {
		ownedBy = "gemini"
	}

	// Handle the response based on its type
	switch v := modelsData.(type) {
	case *gemini.GeminiModelsResponse:
		for _, model := range v.Models {
			allModels = append(allModels, map[string]interface{}{
				"id":       model.Name,
				"object":   "model",
				"created":  1677648736,
				"owned_by": ownedBy,
			})
		}
	case *kiro.ClaudeModelsResponse:
		for _, model := range v.Data {
			allModels = append(allModels, map[string]interface{}{
				"id":       model.ID,
				"object":   "model",
				"created":  1677648736,
				"owned_by": ownedBy,
			})
		}
	case map[string]interface{}:
		// Handle generic map responses (like from Qwen)
		if data, ok := v["data"].([]interface{}); ok {
			for _, model := range data {
				if modelMap, ok := model.(map[string]interface{}); ok {
					// Ensure the model has the required fields
					if _, exists := modelMap["id"]; !exists {
						// If no id field, skip this model
						continue
					}
					if _, exists := modelMap["object"]; !exists {
						modelMap["object"] = "model"
					}
					if _, exists := modelMap["created"]; !exists {
						modelMap["created"] = 1677648736
					}
					if _, exists := modelMap["owned_by"]; !exists {
						modelMap["owned_by"] = ownedBy
					}
					allModels = append(allModels, modelMap)
				}
			}
		} else {
			// Handle case where the response is not in the expected format
			h.logger.ErrorLog("[Provider-Specific Handler] Unexpected models data format for provider %s", h.providerType)
		}
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   allModels,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to encode models response: %v", err)
	}
}

// handleChatCompletionsWithProvider handles POST /v1/chat/completions with a specific provider
func (h *ProviderSpecificHandler) handleChatCompletionsWithProvider(w http.ResponseWriter, r *http.Request, provider provider.Provider) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var openaiReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&openaiReq); err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to decode request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Extract model from request
	model, ok := openaiReq["model"].(string)
	if !ok {
		http.Error(w, "Model is required", http.StatusBadRequest)
		return
	}

	// Get the appropriate converter based on the provider's protocol
	conv, err := h.convFactory.Get(provider.Protocol())
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to get converter for protocol %s: %v", provider.Protocol(), err)
		http.Error(w, fmt.Sprintf("Converter not found for protocol: %s", provider.Protocol()), http.StatusInternalServerError)
		return
	}

	// Convert OpenAI request to provider's native format
	nativeReq, err := conv.FromOpenAIRequest(openaiReq)
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to convert request: %v", err)
		http.Error(w, "Failed to convert request format", http.StatusInternalServerError)
		return
	}

	// Check if streaming
	isStreaming := false
	if streamVal, ok := openaiReq["stream"].(bool); ok {
		isStreaming = streamVal
	}

	if isStreaming {
		h.handleStreamChatCompletionsWithProvider(w, r, provider, conv, nativeReq, model)
	} else {
		h.handleNonStreamChatCompletionsWithProvider(w, r, provider, conv, nativeReq, model)
	}
}

// handleNonStreamChatCompletionsWithProvider handles non-streaming chat completions with a specific provider
func (h *ProviderSpecificHandler) handleNonStreamChatCompletionsWithProvider(w http.ResponseWriter, r *http.Request, provider provider.Provider, conv converter.Converter, nativeReq interface{}, model string) {
	nativeResp, err := provider.GenerateContent(r.Context(), model, nativeReq)
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] GenerateContent failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Record success for routing
	h.factory.RecordSuccess(model, provider.Name())
	h.logger.DebugLog("[Provider-Specific Handler] Recorded success for provider %s with model %s", provider.Name(), model)

	// Convert response back to OpenAI format
	openaiResp, err := conv.ToOpenAIResponse(nativeResp, model)
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to convert response: %v", err)
		http.Error(w, "Failed to convert response format", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] Failed to encode response: %v", err)
	}
}

// handleStreamChatCompletionsWithProvider handles streaming chat completions with a specific provider
func (h *ProviderSpecificHandler) handleStreamChatCompletionsWithProvider(w http.ResponseWriter, r *http.Request, provider provider.Provider, conv converter.Converter, nativeReq interface{}, model string) {
	ctx := r.Context()
	stream, err := provider.GenerateContentStream(ctx, model, nativeReq)
	if err != nil {
		h.logger.ErrorLog("[Provider-Specific Handler] GenerateContentStream failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	// Record success for routing
	h.factory.RecordSuccess(model, provider.Name())
	h.logger.DebugLog("[Provider-Specific Handler] Recorded success for streaming provider %s with model %s", provider.Name(), model)

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

	// Stream the response - convert each chunk to OpenAI format
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			// For now, just pass through the raw stream
			// In a complete implementation, we would convert each SSE chunk to OpenAI format
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				h.logger.ErrorLog("[Provider-Specific Handler] Write error: %v", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			h.logger.ErrorLog("[Provider-Specific Handler] Stream read error: %v", err)
			return
		}
	}
}

// RegisterOpenAIRoutes registers OpenAI-compatible routes
func RegisterOpenAIRoutes(mux *http.ServeMux, factory *provider.Factory, convFactory *converter.Factory) {
	handler := NewOpenAIHandler(factory, convFactory)
	
	// Replace the default proxy handler with our OpenAI handler for /v1 routes
	mux.Handle("/v1/", handler)
}

// RegisterProviderSpecificRoutes registers provider-specific OpenAI-compatible routes
func RegisterProviderSpecificRoutes(mux *http.ServeMux, factory *provider.Factory, convFactory *converter.Factory) {
	// Register routes for each provider
	mux.Handle("/qwen/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderQwen))
	mux.Handle("/gemini/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderGeminiCLI))
	mux.Handle("/kiro/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderKiro))
	mux.Handle("/antigravity/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderAntigravity))
	mux.Handle("/iflow/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderIFlow))
}