// Package provider defines the factory for creating and managing providers
package provider

import (
	"fmt"
	"strings"
	"sync"
)

// Factory manages provider instances
type Factory struct {
	providers map[ProviderType]Provider
	mu        sync.RWMutex
}

// NewFactory creates a new provider factory
func NewFactory() *Factory {
	return &Factory{
		providers: make(map[ProviderType]Provider),
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

	// Extract the actual model name without provider prefix if present
	actualModel := model
	if idx := strings.Index(model, ":"); idx != -1 {
		actualModel = model[idx+1:]
	}
	modelLower := strings.ToLower(actualModel)

	// Gemini models
	if strings.HasPrefix(modelLower, "gemini-") {
		if p, ok := f.providers[ProviderGeminiCLI]; ok {
			return p, nil
		}
		if p, ok := f.providers[ProviderAntigravity]; ok {
			return p, nil
		}
	}

	// Claude models
	if strings.HasPrefix(modelLower, "claude-") {
		if p, ok := f.providers[ProviderKiro]; ok {
			return p, nil
		}
	}

	// Qwen models
	if strings.HasPrefix(modelLower, "qwen") {
		if p, ok := f.providers[ProviderQwen]; ok {
			return p, nil
		}
	}

	// Fallback: check all providers for model support
	for _, p := range f.providers {
		for _, m := range p.SupportedModels() {
			if strings.EqualFold(m, actualModel) || strings.HasPrefix(modelLower, strings.ToLower(m)) {
				return p, nil
			}
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
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
