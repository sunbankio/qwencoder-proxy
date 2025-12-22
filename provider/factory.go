// Package provider defines the factory for creating and managing providers
package provider

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Factory manages provider instances
type Factory struct {
	providers   map[ProviderType]Provider
	lastSuccess map[string]ProviderType // model -> providerType
	mu          sync.RWMutex
	rng         *rand.Rand
}

// NewFactory creates a new provider factory
func NewFactory() *Factory {
	return &Factory{
		providers:   make(map[ProviderType]Provider),
		lastSuccess: make(map[string]ProviderType),
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
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

	var candidates []Provider
	for _, p := range f.providers {
		if p.SupportsModel(actualModel) {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no provider found for model: %s", model)
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// Multiple candidates found. Try to use the last successful provider.
	if lastType, ok := f.lastSuccess[actualModel]; ok {
		for _, p := range candidates {
			if p.Name() == lastType {
				return p, nil
			}
		}
	}

	// No last success or last success not in candidates. Pick a random one.
	return candidates[f.rng.Intn(len(candidates))], nil
}

// RecordSuccess records that a provider successfully served a model
func (f *Factory) RecordSuccess(model string, providerType ProviderType) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Extract the actual model name without provider prefix if present
	actualModel := model
	if idx := strings.Index(model, ":"); idx != -1 {
		actualModel = model[idx+1:]
	}

	f.lastSuccess[actualModel] = providerType
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
