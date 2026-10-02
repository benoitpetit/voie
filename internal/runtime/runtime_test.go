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

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }

type runtimeSecondProvider struct{ runtimeTestProvider }

func (*runtimeSecondProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test2", Label: "Test2", URL: "https://example.invalid", Working: true, DefaultModel: "other", SupportedModels: []string{"other"}}
}

func (*runtimeSecondProvider) SupportsModel(model string) bool { return model == "other" }

func TestNewWithRegistryResolvesFallbackPolicy(t *testing.T) {
	newRegistry := func() *app.Registry {
		registry := app.NewRegistry()
		registry.Register("test", &runtimeTestProvider{})
		registry.Register("test2", &runtimeSecondProvider{})
		return registry
	}
	baseConfig := func(path string) *config.Config {
		return &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute, RoutingConfigPath: path}
	}

	t.Run("defaults", func(t *testing.T) {
		runtime, err := NewWithRegistry(baseConfig(filepath.Join(t.TempDir(), "missing.json")), newRegistry())
		if err != nil {
			t.Fatal(err)
		}
		policy := runtime.Service().FallbackPolicy()
		if !policy.Enabled || policy.MaxRetries != 1 || policy.MaxFallbackModels != 1 || len(policy.Models) != 0 {
			t.Fatalf("fallback defaults = %+v", policy)
		}
	})

	t.Run("env overrides defaults", func(t *testing.T) {
		cfg := baseConfig(filepath.Join(t.TempDir(), "missing.json"))
		cfg.FallbackEnabled = boolPtr(false)
		cfg.FallbackMaxRetries = intPtr(2)
		cfg.FallbackMaxModels = intPtr(2)
		runtime, err := NewWithRegistry(cfg, newRegistry())
		if err != nil {
			t.Fatal(err)
		}
		policy := runtime.Service().FallbackPolicy()
		if policy.Enabled || policy.MaxRetries != 2 || policy.MaxFallbackModels != 2 {
			t.Fatalf("fallback after env = %+v", policy)
		}
	})

	t.Run("file overrides env", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "routing.json")
		data := `{"fallback":{"enabled":true,"max_retries":2,"max_fallback_models":1,"models":{"other":["model"]}}}`
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := baseConfig(path)
		cfg.FallbackEnabled = boolPtr(false)
		cfg.FallbackMaxRetries = intPtr(3)
		runtime, err := NewWithRegistry(cfg, newRegistry())
		if err != nil {
			t.Fatal(err)
		}
		policy := runtime.Service().FallbackPolicy()
		if !policy.Enabled {
			t.Fatalf("file enabled=true must override env false, got %+v", policy)
		}
		if policy.MaxRetries != 2 {
			t.Fatalf("file max_retries=2 must override env 3, got %+v", policy)
		}
		if policy.MaxFallbackModels != 1 {
			t.Fatalf("file max_fallback_models=1 must apply, got %+v", policy)
		}
		if len(policy.Models) != 1 || policy.Models["other"][0] != "model" {
			t.Fatalf("file fallback models = %+v", policy.Models)
		}
	})

	t.Run("unspecified file fields keep env values", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "routing.json")
		if err := os.WriteFile(path, []byte(`{"fallback":{"max_retries":2}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := baseConfig(path)
		cfg.FallbackMaxRetries = intPtr(3)
		cfg.FallbackMaxModels = intPtr(0)
		runtime, err := NewWithRegistry(cfg, newRegistry())
		if err != nil {
			t.Fatal(err)
		}
		policy := runtime.Service().FallbackPolicy()
		if policy.MaxRetries != 2 || policy.MaxFallbackModels != 0 {
			t.Fatalf("fallback resolution = %+v, want max_retries=2 (file), max_fallback_models=0 (env)", policy)
		}
	})
}

func TestNewWithRegistryRejectsInvalidFallbackConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	if err := os.WriteFile(path, []byte(`{"fallback":{"models":{"missing/model":["other"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := app.NewRegistry()
	registry.Register("test", &runtimeTestProvider{})
	registry.Register("test2", &runtimeSecondProvider{})
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute, RoutingConfigPath: path}
	if _, err := NewWithRegistry(cfg, registry); err == nil || !strings.Contains(err.Error(), "missing/model") {
		t.Fatalf("NewWithRegistry() error = %v, want unknown fallback model reference", err)
	}
}

func TestNewWithRegistryRejectsOutOfRangeFallbackEnv(t *testing.T) {
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Minute, RoutingConfigPath: filepath.Join(t.TempDir(), "missing.json"), FallbackMaxRetries: intPtr(9)}
	registry := app.NewRegistry()
	registry.Register("test", &runtimeTestProvider{})
	if _, err := NewWithRegistry(cfg, registry); err == nil || !strings.Contains(err.Error(), "FALLBACK_MAX_RETRIES") {
		t.Fatalf("NewWithRegistry() error = %v, want FALLBACK_MAX_RETRIES bounds error", err)
	}
}
