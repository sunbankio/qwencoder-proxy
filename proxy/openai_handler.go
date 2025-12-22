// Package proxy provides HTTP handlers for the proxy server
package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sunbankio/qwencoder-proxy/converter"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
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

		// Use shared formatting logic
		providerModels := FormatModelsResponse(modelsData, providerType, h.logger)
		allModels = append(allModels, providerModels...)
	}

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
			if err := StreamResponse(w, r, h.factory, provider, nativeReq, model, h.logger); err != nil {
				h.logger.ErrorLog("[OpenAI Handler] Streaming failed: %v", err)
				// If headers haven't been written (which StreamResponse does early), we could send error.
				// But StreamResponse handles writing. If it returns error after writing headers, we can't do much.
				// If it returns error before writing headers, we could send 500.
				// For simplicity, we assume StreamResponse logged the error.
				// We can check if w is written to? Hard.
				// StreamResponse writes error to log.
			}
		} else {
			h.handleNonStreamChatCompletions(w, r, provider, conv, nativeReq, model)
		}
	}
	
	// handleNonStreamChatCompletions handles non-streaming chat completions
	func (h *OpenAIHandler) handleNonStreamChatCompletions(w http.ResponseWriter, r *http.Request, provider provider.Provider, conv converter.Converter, nativeReq interface{}, model string) {
		openaiResp, err := GenerateAndConvert(r.Context(), provider, conv, nativeReq, model)
		if err != nil {
			h.logger.ErrorLog("[OpenAI Handler] GenerateAndConvert failed with provider %s: %v", provider.Name(), err)
	
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
				altResp, altErr := GenerateAndConvert(r.Context(), altProvider, altConv, nativeReq, model)
				if altErr != nil {
					h.logger.ErrorLog("[OpenAI Handler] Alternative provider %s also failed: %v", altProvider.Name(), altErr)
					http.Error(w, fmt.Sprintf("All providers failed for model %s. Primary error: %v, Alternative error: %v", model, err, altErr), http.StatusInternalServerError)
					return
				}
	
				// Record success for alternative provider
				h.factory.RecordSuccess(model, altProvider.Name())
				h.logger.DebugLog("[OpenAI Handler] Recorded success for alternative provider %s with model %s", altProvider.Name(), model)
	
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(altResp); err != nil {
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
	
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
			h.logger.ErrorLog("[OpenAI Handler] Failed to encode response: %v", err)
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
	} else if strings.HasPrefix(path, "/iflow/v1/") {
		path = strings.TrimPrefix(path, "/iflow/v1")
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

	// Format the models response using shared logic
	allModels := FormatModelsResponse(modelsData, h.providerType, h.logger)

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
		if err := StreamResponse(w, r, h.factory, provider, nativeReq, model, h.logger); err != nil {
			h.logger.ErrorLog("[Provider-Specific Handler] Streaming failed: %v", err)
		}
	} else {
		// Use GenerateAndConvert
		openaiResp, err := GenerateAndConvert(r.Context(), provider, conv, nativeReq, model)
		if err != nil {
			h.logger.ErrorLog("[Provider-Specific Handler] GenerateAndConvert failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Record success for routing
		h.factory.RecordSuccess(model, provider.Name())
		h.logger.DebugLog("[Provider-Specific Handler] Recorded success for provider %s with model %s", provider.Name(), model)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
			h.logger.ErrorLog("[Provider-Specific Handler] Failed to encode response: %v", err)
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