package providers

import (
	"context"
	"io"
	"net/http"
	"testing"
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
