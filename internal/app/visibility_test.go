package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/app"
)

type visibilityProvider struct {
	name    string
	working bool
	model   string
}

func (p *visibilityProvider) ChatCompletion(context.Context, []app.Message, string) (*app.ChatCompletionResponse, error) {
	return nil, nil
}
func (p *visibilityProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}
func (p *visibilityProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: p.name, Label: p.name, Working: p.working, SupportedModels: []string{p.model}}
}
func (p *visibilityProvider) SupportsModel(model string) bool { return p.working && model == p.model }

func newVisibilityService(t *testing.T, providers ...*visibilityProvider) *app.Service {
	t.Helper()
	registry := app.NewRegistry()
	for _, provider := range providers {
		registry.Register(provider.name, provider)
	}
	service, err := app.NewService(registry, app.ServiceOptions{
		Timeout:     time.Second,
		HealthProbe: func(context.Context, string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestUnconfiguredProviderHiddenFromListModels(t *testing.T) {
	service := newVisibilityService(t,
		&visibilityProvider{name: "hidden", working: false, model: "hidden-model"},
		&visibilityProvider{name: "visible", working: true, model: "visible-model"},
	)
	models := service.ListModels()
	if len(models) != 1 || models[0].ID != "visible-model" {
		t.Fatalf("ListModels = %+v, want only visible-model", models)
	}
}

func TestListModelsCarriesGroupMetadata(t *testing.T) {
	service := newVisibilityService(t, &visibilityProvider{name: "visible", working: true, model: "visible-model"})
	models := service.ListModels()
	if len(models) != 1 || models[0].Group == "" {
		t.Fatalf("ListModels = %+v, want group metadata", models)
	}
}

func TestUnconfiguredProviderHiddenFromDiscovery(t *testing.T) {
	service := newVisibilityService(t,
		&visibilityProvider{name: "hidden", working: false, model: "hidden-model"},
		&visibilityProvider{name: "visible", working: true, model: "visible-model"},
	)
	providers, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "visible" {
		t.Errorf("ListProviders = %+v", providers)
	}
	metadata := service.ListProviderMetadata()
	if len(metadata) != 1 || metadata[0].Name != "visible" {
		t.Errorf("ListProviderMetadata = %+v", metadata)
	}
}

func TestUnconfiguredProviderSkippedByRoute(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("hidden", &visibilityProvider{name: "hidden", working: false, model: "hidden-model"})
	if routed := registry.GetForModel("hidden-model"); routed != nil {
		t.Errorf("GetForModel(hidden-model) = %v, want nil", routed.GetInfo().Name)
	}
}
