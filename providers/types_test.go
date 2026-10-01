package providers

import "testing"

func TestRegistryRoutesDuckAIModels(t *testing.T) {
	registry := NewProviderRegistry()
	provider := registry.Get("duckai")
	if provider == nil {
		t.Fatal("registry.Get(duckai) = nil")
	}
	if provider.GetInfo().Name != "duckai" {
		t.Fatalf("registered provider name = %q, want duckai", provider.GetInfo().Name)
	}
	for _, existing := range []string{"perplexity", "deepai", "quillbot", "yqcloud", "cohere"} {
		if registry.Get(existing) == nil {
			t.Errorf("existing provider %q is missing after Duck.ai registration", existing)
		}
	}
	jimmy := registry.Get("jimmy")
	if jimmy == nil {
		t.Fatal("registry.Get(jimmy) = nil")
	}
	if jimmy.GetInfo().DefaultModel != "llama3.1-8B" {
		t.Errorf("Jimmy default model = %q, want llama3.1-8B", jimmy.GetInfo().DefaultModel)
	}
	if routed := registry.GetForModel("llama3.1-8B"); routed == nil || routed.GetInfo().Name != "jimmy" {
		t.Errorf("GetForModel(llama3.1-8B) = %v, want jimmy", routed)
	}

	foundName := false
	for _, name := range registry.GetProviderNames() {
		if name == "duckai" {
			foundName = true
			break
		}
	}
	if !foundName {
		t.Fatal("duckai is absent from provider names")
	}

	for _, model := range []string{
		"gpt-5.6-luna", "gpt-5.4-mini", "claude-haiku-4-5", "gpt-5.4-nano",
		"gpt-4o-mini", "claude-3-haiku", "o4mini",
	} {
		routed := registry.GetForModel(model)
		if routed == nil || routed.GetInfo().Name != "duckai" {
			t.Errorf("GetForModel(%q) provider = %v, want duckai", model, routed)
		}
	}
	for _, model := range []string{
		"mistral-small-4", "gpt-oss-120b", "gemma-4-31b",
		"gpt-5.6-terra", "claude-sonnet-4-6", "claude-opus-4-8", "gpt-5.6-sol",
		"llama", "mixtral",
	} {
		if routed := registry.GetForModel(model); routed != nil && routed.GetInfo().Name == "duckai" {
			t.Errorf("GetForModel(%q) routed to Duck.ai, want removed model to use another provider", model)
		}
	}

	if routed := registry.GetForModel("openai"); routed != nil {
		t.Fatalf("GetForModel(openai) = %q, want nil (generic defaults are resolved by app.Service)", routed.GetInfo().Name)
	}
}

func TestCohereExcludesUnresponsiveArabicModel(t *testing.T) {
	registry := NewProviderRegistry()
	cohere := registry.Get("cohere")
	if cohere == nil {
		t.Fatal("registry.Get(cohere) = nil")
	}

	const removedModel = "command-r7b-arabic-02-2025"
	if cohere.SupportsModel(removedModel) {
		t.Errorf("Cohere.SupportsModel(%q) = true, want false", removedModel)
	}
	for _, model := range cohere.GetInfo().SupportedModels {
		if model == removedModel {
			t.Errorf("Cohere SupportedModels still contains %q", removedModel)
		}
	}
	if routed := registry.GetForModel(removedModel); routed != nil && routed.GetInfo().Name == "cohere" {
		t.Errorf("GetForModel(%q) routed to Cohere, want it excluded", removedModel)
	}
}
