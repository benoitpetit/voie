package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benoitpetit/voie/config"
)

func TestLocalGatewayAvailableWithoutToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	spec := LocalGatewaySpec{Name: "antigravity", Binary: "/bin/sh"}
	if !NewLocalGateway(spec).Available() {
		t.Error("a local gateway with a binary must be available with the default key")
	}
}

func TestLocalGatewayHealthSendsBearerKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VOIE_CLINE_KEY", "")
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	spec := LocalGatewaySpec{Name: "cline", Binary: "/bin/sh", BaseURL: server.URL, HealthPath: "/health"}
	gateway := NewLocalGateway(spec)
	if err := gateway.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := "Bearer voie-local"; gotAuth != want {
		t.Errorf("health Authorization = %q, want %q", gotAuth, want)
	}
}

func TestHostedTokenResolvedPerRequest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VOIE_OPENROUTER_KEY", "")
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openrouter", "first-token"); err != nil {
		t.Fatal(err)
	}
	provider := NewOpenRouter()
	var first string
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	if _, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, "orfree"); err != nil {
		t.Fatal(err)
	}
	if first != "Bearer first-token" {
		t.Fatalf("first Authorization = %q", first)
	}
	if err := store.Set("openrouter", "second-token"); err != nil {
		t.Fatal(err)
	}
	var second string
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	if _, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, "orfree"); err != nil {
		t.Fatal(err)
	}
	if second != "Bearer second-token" {
		t.Errorf("second Authorization = %q, want the refreshed token", second)
	}
}
