package app_test

import (
	"context"
	"sort"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/providers"
)

func TestRegistryRoutesOnlySupportedModels(t *testing.T) {
	registry := providers.NewRegistry()
	for _, model := range []string{"gpt-5.6-luna", "claude-3-haiku", "or-nvidia-nemotron-3-ultra-550b-a55b-free"} {
		if got := registry.GetForModel(model); got == nil {
			t.Errorf("GetForModel(%q) = nil, want registered provider", model)
		}
	}
	if got := registry.GetForModel("unknown-model-typo"); got != nil {
		t.Errorf("GetForModel(unknown-model-typo) = %q, want nil", got.GetInfo().Name)
	}
}

func TestRegistryProviderNamesAreStable(t *testing.T) {
	registry := providers.NewRegistry()
	got := registry.GetProviderNames()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("GetProviderNames() is not sorted: %v", got)
	}
	for _, want := range []string{"duckai", "opencode-zen", "orcarouter", "groq", "codebuddy", "omniroute"} {
		found := false
		for _, name := range got {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("provider %q is missing", want)
		}
	}
	if len(got) < 30 {
		t.Errorf("provider count = %d, want at least 30", len(got))
	}
}

func TestRegisteredProvidersDeclareDefaultModels(t *testing.T) {
	registry := providers.NewRegistry()
	for _, name := range registry.GetProviderNames() {
		info := registry.Get(name).GetInfo()
		if info.DefaultModel == "" {
			t.Errorf("provider %q has no default model", name)
		}
	}
}

func TestRegistryGetAllReturnsProvidersByName(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("sample", &registryTestProvider{})
	if got := registry.Get("sample"); got == nil {
		t.Fatal("Get(sample) = nil")
	}
	if got := registry.GetAll(); len(got) != 1 || got["sample"] == nil {
		t.Fatalf("GetAll() = %v, want one sample provider", got)
	}
}

type registryTestProvider struct{}

func (*registryTestProvider) ChatCompletion(_ context.Context, _ []app.Message, _ string) (*app.ChatCompletionResponse, error) {
	return nil, nil
}

func (*registryTestProvider) ChatCompletionStream(_ context.Context, _ []app.Message, _ string, _ func(string)) error {
	return nil
}

func (*registryTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "sample", DefaultModel: "test"}
}
func (*registryTestProvider) SupportsModel(string) bool { return false }
