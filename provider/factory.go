// Package provider defines the factory for creating and managing providers
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ModelProviderMap maps models to the providers that support them
type ModelProviderMap map[string][]Provider

// OpenAIModel represents an OpenAI-compatible model object
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// Factory manages provider instances
type Factory struct {
	providers      map[ProviderType]Provider
	lastSuccess    map[string]ProviderType // model -> providerType
	modelProviders ModelProviderMap        // model -> list of providers
	mu             sync.RWMutex
	rng            *rand.Rand
}

// NewFactory creates a new provider factory
func NewFactory() *Factory {
	return &Factory{
		providers:      make(map[ProviderType]Provider),
		lastSuccess:    make(map[string]ProviderType),
		modelProviders: make(ModelProviderMap),
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Register adds a provider to the factory
func (f *Factory) Register(p Provider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers[p.Name()] = p
}

// Get returns a provider by type
func (f *Factory) Get(providerType ProviderType) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	p, ok := f.providers[providerType]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerType)
	}
	return p, nil
}

// GetByModel returns a provider that supports the given model
func (f *Factory) GetByModel(model string) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Look up providers that support this model from our pre-populated mapping
	candidates, exists := f.modelProviders[model]
	if !exists || len(candidates) == 0 {
		return nil, fmt.Errorf("no provider found for model: %s", model)
	}

	// If only one provider supports this model, use it
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// Multiple providers support this model. Try to use the last successful provider.
	if lastType, ok := f.lastSuccess[model]; ok {
		for _, p := range candidates {
			if p.Name() == lastType {
				return p, nil
			}
		}
	}

	// No last success or last success not in candidates. Use the first available provider.
	return candidates[0], nil
}

// GetAlternativeProvider returns an alternative provider for the given model, excluding the specified provider
func (f *Factory) GetAlternativeProvider(model string, excludeProvider ProviderType) (Provider, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Look up providers that support this model from our pre-populated mapping
	candidates, exists := f.modelProviders[model]
	if !exists || len(candidates) == 0 {
		return nil, fmt.Errorf("no provider found for model: %s", model)
	}

	// Find the first provider that is not the excluded one
	for _, p := range candidates {
		if p.Name() != excludeProvider {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no alternative provider found for model: %s (excluding %s)", model, excludeProvider)
}

// RecordSuccess records that a provider successfully served a model
func (f *Factory) RecordSuccess(model string, providerType ProviderType) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastSuccess[model] = providerType
}

// List returns all registered providers
func (f *Factory) List() []Provider {
	f.mu.RLock()
	defer f.mu.RUnlock()

	providers := make([]Provider, 0, len(f.providers))
	for _, p := range f.providers {
		providers = append(providers, p)
	}
	return providers
}

// ListTypes returns all registered provider types
func (f *Factory) ListTypes() []ProviderType {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]ProviderType, 0, len(f.providers))
	for t := range f.providers {
		types = append(types, t)
	}
	return types
}

// PopulateModelProviders fetches all available models from all providers and builds the model-to-provider mapping
func (f *Factory) PopulateModelProviders(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Clear existing mappings
	f.modelProviders = make(ModelProviderMap)

	// Fetch models from all providers
	for providerType, provider := range f.providers {
		// Check if provider needs initialization (like Antigravity)
		if initProvider, ok := provider.(interface{ Initialize(context.Context) error }); ok {
			if err := initProvider.Initialize(ctx); err != nil {
				fmt.Printf("Warning: Failed to initialize provider %s: %v\n", providerType, err)
				continue
			}
		}

		modelsData, err := provider.ListModels(ctx)
		if err != nil {
			// Log the error but continue with other providers
			fmt.Printf("Warning: Failed to fetch models from provider %s: %v\n", providerType, err)
			continue
		}

		// Extract models from the response generically
		modelNames := f.extractModelNames(modelsData)

		// Add this provider to each model it supports
		for _, modelName := range modelNames {
			if _, exists := f.modelProviders[modelName]; !exists {
				f.modelProviders[modelName] = []Provider{}
			}

			// Check if this provider is already in the list for this model
			alreadyAdded := false
			for _, existingProvider := range f.modelProviders[modelName] {
				if existingProvider.Name() == provider.Name() {
					alreadyAdded = true
					break
				}
			}

			if !alreadyAdded {
				f.modelProviders[modelName] = append(f.modelProviders[modelName], provider)
			}
		}
	}

	return nil
}

// RefreshProviderModels fetches models from a specific provider and updates the model-to-provider mapping
func (f *Factory) RefreshProviderModels(ctx context.Context, providerType ProviderType) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	provider, exists := f.providers[providerType]
	if !exists {
		return fmt.Errorf("provider not found: %s", providerType)
	}

	modelsData, err := provider.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch models from provider %s: %w", providerType, err)
	}

	// Get the existing models for this provider to remove old associations
	existingModels := make(map[string]bool)
	for modelName, providers := range f.modelProviders {
		for _, p := range providers {
			if p.Name() == providerType {
				existingModels[modelName] = true
				break
			}
		}
	}

	// Remove this provider from all its previously associated models
	for modelName := range existingModels {
		newProviders := []Provider{}
		for _, p := range f.modelProviders[modelName] {
			if p.Name() != providerType {
				newProviders = append(newProviders, p)
			}
		}
		if len(newProviders) == 0 {
			delete(f.modelProviders, modelName)
		} else {
			f.modelProviders[modelName] = newProviders
		}
	}

	// Extract models from the response and add this provider to each model
	modelNames := f.extractModelNames(modelsData)
	for _, modelName := range modelNames {
		if _, exists := f.modelProviders[modelName]; !exists {
			f.modelProviders[modelName] = []Provider{}
		}

		// Check if this provider is already in the list for this model
		alreadyAdded := false
		for _, existingProvider := range f.modelProviders[modelName] {
			if existingProvider.Name() == provider.Name() {
				alreadyAdded = true
				break
			}
		}

		if !alreadyAdded {
			f.modelProviders[modelName] = append(f.modelProviders[modelName], provider)
		}
	}

	return nil
}

// GetAllModels returns all available models and their providers
func (f *Factory) GetAllModels() map[string][]ProviderType {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make(map[string][]ProviderType)
	for model, providers := range f.modelProviders {
		providerTypes := make([]ProviderType, len(providers))
		for i, provider := range providers {
			providerTypes[i] = provider.Name()
		}
		result[model] = providerTypes
	}
	return result
}

// GetModelProviders returns the list of providers that support a specific model
func (f *Factory) GetModelProviders(model string) []Provider {
	f.mu.RLock()
	defer f.mu.RUnlock()

	candidates, exists := f.modelProviders[model]
	if !exists {
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]Provider, len(candidates))
	copy(result, candidates)
	return result
}

// GetLastSuccessProvider returns the last successful provider for a model
func (f *Factory) GetLastSuccessProvider(model string) (ProviderType, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	providerType, exists := f.lastSuccess[model]
	return providerType, exists
}

// extractModelNames extracts model names from various response formats
func (f *Factory) extractModelNames(data interface{}) []string {
	var modelNames []string

	// Try to convert to map to handle structured responses
	if dataMap, ok := f.convertToMap(data); ok {
		// Handle models field (common in Gemini/Antigravity responses)
		if modelsField, exists := dataMap["models"]; exists {
			if modelsArray, ok := modelsField.([]interface{}); ok {
				for _, item := range modelsArray {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if id, exists := itemMap["id"].(string); exists {
							modelNames = append(modelNames, id)
						} else if name, exists := itemMap["name"].(string); exists {
							modelNames = append(modelNames, name)
						}
					}
				}
			}
		}

		// Handle data field (common in OpenAI-style responses or iFlow)
		if dataField, exists := dataMap["data"]; exists {
			if dataArray, ok := dataField.([]interface{}); ok {
				for _, item := range dataArray {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if id, exists := itemMap["id"].(string); exists {
							modelNames = append(modelNames, id)
						}
					}
				}
			}
		}
	} else if dataArray, ok := data.([]interface{}); ok {
		// Handle direct array responses
		for _, item := range dataArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if id, exists := itemMap["id"].(string); exists {
					modelNames = append(modelNames, id)
				}
			}
		}
	}

	return modelNames
}

// FormatOpenAIModels converts native model data into OpenAI-compatible format
func (f *Factory) FormatOpenAIModels(data interface{}, providerType ProviderType) []OpenAIModel {
	names := f.extractModelNames(data)
	ownedBy := string(providerType)
	if providerType == ProviderGeminiCLI {
		ownedBy = "gemini"
	}

	models := make([]OpenAIModel, 0, len(names))
	for _, name := range names {
		models = append(models, OpenAIModel{
			ID:      name,
			Object:  "model",
			Created: 1677648736,
			OwnedBy: ownedBy,
		})
	}
	return models
}

// convertToMap attempts to convert various response types to a map for processing
func (f *Factory) convertToMap(data interface{}) (map[string]interface{}, bool) {
	switch v := data.(type) {
	case map[string]interface{}:
		return v, true
	default:
		// Try to marshal and unmarshal to convert structs to maps
		if jsonBytes, err := json.Marshal(data); err == nil {
			var result map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &result); err == nil {
				return result, true
			}
		}
		return nil, false
	}
}
