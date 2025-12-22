package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/sunbankio/qwencoder-proxy/converter"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
	"github.com/sunbankio/qwencoder-proxy/provider/gemini"
	"github.com/sunbankio/qwencoder-proxy/provider/iflow"
	"github.com/sunbankio/qwencoder-proxy/provider/kiro"
)

// FormatModelsResponse formats the models data from a provider into an OpenAI-compatible list
func FormatModelsResponse(modelsData interface{}, providerType provider.ProviderType, logger *logging.Logger) []map[string]interface{} {
	var allModels []map[string]interface{}

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
	case *iflow.OpenAIModelsResponse:
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
			logger.ErrorLog("[Common] Unexpected models data format for provider %s", providerType)
		}
	}
	return allModels
}

// GenerateAndConvert handles non-streaming content generation and conversion to OpenAI format
func GenerateAndConvert(ctx context.Context, p provider.Provider, conv converter.Converter, nativeReq interface{}, model string) (interface{}, error) {
	nativeResp, err := p.GenerateContent(ctx, model, nativeReq)
	if err != nil {
		return nil, err
	}
	return conv.ToOpenAIResponse(nativeResp, model)
}

// StreamResponse handles streaming content generation and writing to the response writer
func StreamResponse(w http.ResponseWriter, r *http.Request, factory *provider.Factory, p provider.Provider, nativeReq interface{}, model string, logger *logging.Logger) error {
	ctx := r.Context()
	stream, err := p.GenerateContentStream(ctx, model, nativeReq)
	if err != nil {
		return err
	}
	defer stream.Close()

	// Record success for routing
	factory.RecordSuccess(model, p.Name())
	logger.DebugLog("[Common] Recorded success for streaming provider %s with model %s", p.Name(), model)

	// Set streaming headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	// Stream the response - convert each chunk to OpenAI format
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			// For now, just pass through the raw stream
			// In a complete implementation, we would convert each SSE chunk to OpenAI format
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}
