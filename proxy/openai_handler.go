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
	factory       *provider.Factory
	convFactory   *converter.Factory
	logger        *logging.Logger
	fixedProvider provider.ProviderType // If set, always use this provider
}

// NewOpenAIHandler creates a new OpenAI-compatible handler
func NewOpenAIHandler(factory *provider.Factory, convFactory *converter.Factory) *OpenAIHandler {
	return &OpenAIHandler{
		factory:     factory,
		convFactory: convFactory,
		logger:      logging.NewLogger(),
	}
}

// NewProviderSpecificHandler creates a new handler that forces requests to use a specific provider
func NewProviderSpecificHandler(factory *provider.Factory, convFactory *converter.Factory, providerType provider.ProviderType) *OpenAIHandler {
	return &OpenAIHandler{
		factory:       factory,
		convFactory:   convFactory,
		fixedProvider: providerType,
		logger:        logging.NewLogger(),
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

	// Determine the path by stripping known prefixes
	path := r.URL.Path
	prefixes := []string{"/v1", "/qwen/v1", "/gemini/v1", "/kiro/v1", "/antigravity/v1", "/iflow/v1"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			path = strings.TrimPrefix(path, prefix)
			break
		}
	}
	path = strings.TrimPrefix(path, "/")

	h.logger.DebugLog("[Handler] Request: %s %s (Fixed Provider: %s)", r.Method, path, h.fixedProvider)

	switch {
	case path == "models" && r.Method == http.MethodGet:
		h.handleListModels(w, r)
	case path == "chat/completions" && r.Method == http.MethodPost:
		h.handleChatCompletions(w, r)
	default:
		http.Error(w, fmt.Sprintf("Unsupported OpenAI-compatible endpoint: %s", path), http.StatusNotFound)
	}
}

// handleListModels handles GET /v1/models
func (h *OpenAIHandler) handleListModels(w http.ResponseWriter, r *http.Request) {
	var allModels []provider.OpenAIModel

	if h.fixedProvider != "" {
		// Specific provider request
		p, err := h.factory.Get(h.fixedProvider)
		if err != nil {
			http.Error(w, fmt.Sprintf("Provider not available: %s", h.fixedProvider), http.StatusInternalServerError)
			return
		}
		modelsData, err := p.ListModels(r.Context())
		if err != nil {
			h.logger.ErrorLog("[Handler] Failed to list models for provider %s: %v", h.fixedProvider, err)
			http.Error(w, "Failed to list models", http.StatusInternalServerError)
			return
		}
		allModels = h.factory.FormatOpenAIModels(modelsData, h.fixedProvider)
	} else {
		// General /v1/models request - show all from all providers
		for _, providerType := range h.factory.ListTypes() {
			p, err := h.factory.Get(providerType)
			if err != nil {
				continue
			}

			modelsData, err := p.ListModels(r.Context())
			if err != nil {
				h.logger.ErrorLog("[Handler] Failed to list models for provider %s: %v", providerType, err)
				continue
			}

			// Use shared formatting logic from factory
			providerModels := h.factory.FormatOpenAIModels(modelsData, providerType)
			allModels = append(allModels, providerModels...)
		}
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   allModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleChatCompletions handles POST /v1/chat/completions
func (h *OpenAIHandler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var openaiReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&openaiReq); err != nil {
		h.logger.ErrorLog("[Handler] Failed to decode request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	model, _ := openaiReq["model"].(string)
	if model == "" {
		http.Error(w, "Model is required", http.StatusBadRequest)
		return
	}

	var p provider.Provider
	var err error

	if h.fixedProvider != "" {
		p, err = h.factory.Get(h.fixedProvider)
	} else {
		p, err = h.factory.GetByModel(model)
	}

	if err != nil {
		h.logger.ErrorLog("[Handler] Failed to get provider: %v", err)
		http.Error(w, fmt.Sprintf("Provider not found: %v", err), http.StatusBadRequest)
		return
	}

	h.logger.DebugLog("[Handler] Using provider %s for model %s", p.Name(), model)

	conv, err := h.convFactory.Get(p.Protocol())
	if err != nil {
		h.logger.ErrorLog("[Handler] No converter for protocol %s: %v", p.Protocol(), err)
		http.Error(w, "Protocol conversion not supported", http.StatusInternalServerError)
		return
	}

	nativeReq, err := conv.FromOpenAIRequest(openaiReq)
	if err != nil {
		h.logger.ErrorLog("[Handler] Conversion failed: %v", err)
		http.Error(w, "Failed to convert request", http.StatusInternalServerError)
		return
	}

	isStreaming, _ := openaiReq["stream"].(bool)
	if isStreaming {
		if err := StreamResponse(w, r, h.factory, p, nativeReq, model, h.logger); err != nil {
			h.logger.ErrorLog("[Handler] Streaming error: %v", err)
		}
	} else {
		h.handleNonStreamCompletions(w, r, p, conv, nativeReq, model)
	}
}

func (h *OpenAIHandler) handleNonStreamCompletions(w http.ResponseWriter, r *http.Request, p provider.Provider, conv converter.Converter, nativeReq interface{}, model string) {
	resp, err := GenerateAndConvert(r.Context(), p, conv, nativeReq, model)
	if err != nil {
		h.logger.ErrorLog("[Handler] GenerateAndConvert failed with %s: %v", p.Name(), err)

		// Try alternative if not a fixed provider request
		if h.fixedProvider == "" {
			if altProvider, altErr := h.factory.GetAlternativeProvider(model, p.Name()); altErr == nil {
				h.logger.DebugLog("[Handler] Retrying with alternative %s", altProvider.Name())
				altConv, _ := h.convFactory.Get(altProvider.Protocol())
				if altResp, altErr := GenerateAndConvert(r.Context(), altProvider, altConv, nativeReq, model); altErr == nil {
					h.factory.RecordSuccess(model, altProvider.Name())
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(altResp)
					return
				}
			}
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.factory.RecordSuccess(model, p.Name())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RegisterOpenAIRoutes registers all OpenAI-compatible routes
func RegisterOpenAIRoutes(mux *http.ServeMux, factory *provider.Factory, convFactory *converter.Factory) {
	// General route
	mux.Handle("/v1/", NewOpenAIHandler(factory, convFactory))
}

// RegisterProviderSpecificRoutes registers provider-specific OpenAI-compatible routes
func RegisterProviderSpecificRoutes(mux *http.ServeMux, factory *provider.Factory, convFactory *converter.Factory) {
	// Provider-specific routes
	mux.Handle("/qwen/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderQwen))
	mux.Handle("/gemini/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderGeminiCLI))
	mux.Handle("/kiro/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderKiro))
	mux.Handle("/antigravity/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderAntigravity))
	mux.Handle("/iflow/v1/", NewProviderSpecificHandler(factory, convFactory, provider.ProviderIFlow))
}
