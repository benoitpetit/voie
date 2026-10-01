package app_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/providers"
)

func TestRegistryRoutesOnlySupportedModels(t *testing.T) {
	registry := providers.NewRegistry()
	for _, model := range []string{"gpt-5.6-luna", "claude-3-haiku", "turbo", "command-a"} {
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
	want := []string{"cohere", "deepai", "duckai", "jimmy", "perplexity", "quillbot", "yqcloud"}
	if got := registry.GetProviderNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GetProviderNames() = %v, want %v", got, want)
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
