package providers

import "testing"

func TestNewKeylessModelsRouteToTheirProviders(t *testing.T) {
	registry := NewRegistry()
	for _, tc := range []struct{ model, provider string }{
		{"kilo-auto", "kilo"},
		{"ovh-qwen3.8-27b", "ovh"},
		{"blockrun-nemotron-3.5-bolt", "blockrun"},
		{"pollinations-openai-fast", "pollinations"},
		{"oc2a-hy4-flash-free", "opencode2api"},
		{"oc2ago-gpt-6-luna", "opencode-go"},
	} {
		provider := registry.GetForModel(tc.model)
		if provider == nil {
			t.Errorf("GetForModel(%q) = nil, want %s", tc.model, tc.provider)
			continue
		}
		if got := provider.GetInfo().Name; got != tc.provider {
			t.Errorf("GetForModel(%q) provider = %q, want %q", tc.model, got, tc.provider)
		}
	}
	for _, tc := range []struct{ model, provider string }{
		{"gpt-5.6-luna", "duckai"},
		{"command-a", "cohere"},
	} {
		provider := registry.GetForModel(tc.model)
		if provider == nil || provider.GetInfo().Name != tc.provider {
			t.Errorf("GetForModel(%q) provider = %v, want %q", tc.model, provider, tc.provider)
		}
	}
}
