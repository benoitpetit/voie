package providers

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/benoitpetit/voie/internal/catalog"
)

func TestTokenProvidersAreRegistered(t *testing.T) {
	registry := NewRegistry()
	for _, spec := range TokenProviders {
		provider := registry.Get(spec.Name)
		if provider == nil {
			t.Errorf("provider %q is not registered", spec.Name)
			continue
		}
	}
}

func TestTokenProviderBearerAndAlias(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	spec := TokenProviders[0] // groq
	t.Setenv(spec.EnvVar, "test-token")
	var gotAuth, gotModel string
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotModel = extractModel(string(body))
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	provider := NewTokenProvider(spec)
	if _, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, spec.Alias); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != spec.UpstreamModel {
		t.Errorf("model = %q, want %q", gotModel, spec.UpstreamModel)
	}
}

func TestRegistryCatalogueExpandsTokenProviders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		provider string
		wantMin  int
	}{
		{"openrouter", 15},
		{"orcarouter", 3},
		{"groq", 3},
		{"google-ai-studio", 11},
		{"nvidia-nim", 17},
		{"aihubmix", 36},
	} {
		catalog.SetSource("")
		models := catalog.Models(tc.provider)
		if len(models) < tc.wantMin {
			t.Errorf("%s registry models = %d, want at least %d", tc.provider, len(models), tc.wantMin)
		}
	}
}

func TestOpenRouterExposesRegistryModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	catalog.SetSource("")
	t.Setenv("VOIE_OPENROUTER_KEY", "test-token")
	provider := NewOpenRouter()
	info := provider.GetInfo()
	if len(info.SupportedModels) < 10 {
		t.Fatalf("OpenRouter models = %d, want the full free catalogue", len(info.SupportedModels))
	}
	for alias, upstream := range catalog.Models("openrouter") {
		if !provider.SupportsModel(alias) {
			t.Errorf("SupportsModel(%q) = false", alias)
		}
		if got := provider.cfg.ModelsSupplier()[alias]; got != upstream {
			t.Errorf("%s maps to %q, want %q", alias, got, upstream)
		}
	}
}

func TestTokenProviderHiddenWithoutToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, spec := range TokenProviders {
		t.Setenv(spec.EnvVar, "")
		provider := NewTokenProvider(spec)
		if provider.GetInfo().Working {
			t.Errorf("%s Working = true without a token", spec.Name)
		}
		if provider.SupportsModel(spec.Alias) {
			t.Errorf("%s SupportsModel(%q) = true without a token", spec.Name, spec.Alias)
		}
	}
}
