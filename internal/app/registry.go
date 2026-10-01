package app

import (
	"sort"
	"strings"
)

type Registry struct {
	providers map[string]Provider
}

func NewRegistry() *Registry { return &Registry{providers: make(map[string]Provider)} }

func (r *Registry) Register(name string, provider Provider) {
	if r == nil || provider == nil {
		return
	}
	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	r.providers[strings.ToLower(strings.TrimSpace(name))] = provider
}

func (r *Registry) Get(name string) Provider {
	if r == nil {
		return nil
	}
	return r.providers[strings.ToLower(strings.TrimSpace(name))]
}

func (r *Registry) GetAll() map[string]Provider {
	if r == nil {
		return map[string]Provider{}
	}
	copy := make(map[string]Provider, len(r.providers))
	for name, provider := range r.providers {
		copy[name] = provider
	}
	return copy
}

func (r *Registry) GetProviderNames() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) GetForModel(model string) Provider {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return nil
	}
	// Prefer the previous routing precedence for aliases shared by providers.
	priority := []string{"duckai", "perplexity", "deepai", "quillbot", "cohere", "yqcloud"}
	seen := make(map[string]bool, len(priority))
	for _, name := range priority {
		seen[name] = true
		if provider := r.Get(name); provider != nil && provider.SupportsModel(model) {
			return provider
		}
	}
	for _, name := range r.GetProviderNames() {
		if seen[name] {
			continue
		}
		if provider := r.Get(name); provider != nil && provider.SupportsModel(model) {
			return provider
		}
	}
	return nil
}
