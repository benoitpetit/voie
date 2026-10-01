package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

func TestNewWithRegistryRejectsUnknownDefaultProvider(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("test", &runtimeTestProvider{})
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute, DefaultProvider: "missing"}
	if _, err := NewWithRegistry(cfg, registry); !errors.Is(err, app.ErrUnknownProvider) {
		t.Fatalf("NewWithRegistry() error = %v, want ErrUnknownProvider", err)
	}
}

func TestNewWithRegistryRejectsUnknownPolicyModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	if err := os.WriteFile(path, []byte(`{"models":{"missing/model":{"description":"Missing","capabilities":["coding"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := app.NewRegistry()
	registry.Register("test", &runtimeTestProvider{})
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute, RoutingConfigPath: path}
	if _, err := NewWithRegistry(cfg, registry); err == nil || !strings.Contains(err.Error(), "missing/model") {
		t.Fatalf("NewWithRegistry() error = %v, want unknown model reference", err)
	}
}

func TestNewWithRegistryExposesSharedServiceWithoutListening(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("test", &runtimeTestProvider{})
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute}
	runtime, err := NewWithRegistry(cfg, registry)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Service() == nil {
		t.Fatal("Runtime.Service is nil")
	}
	models := runtime.Service().ListModels()
	if len(models) != 1 || models[0].ID != "model" {
		t.Fatalf("models = %+v", models)
	}
}

type runtimeTestProvider struct{}

func (*runtimeTestProvider) ChatCompletion(context.Context, []app.Message, string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Content: "ok"}}}}, nil
}
func (*runtimeTestProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}
func (*runtimeTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test", Label: "Test", URL: "https://example.invalid", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}}
}
func (*runtimeTestProvider) SupportsModel(model string) bool { return model == "model" }
