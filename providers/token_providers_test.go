package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

func TestOpenRouterUsesBearerToken(t *testing.T) {
	t.Setenv("VOIE_OPENROUTER_KEY", "sk-or-v1-test")
	var gotAuth, gotURL string
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotURL = r.URL.String()
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))

	if _, err := NewOpenRouter().ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hello"}}, "orfree"); err != nil {
		t.Fatal(err)
	}
	if want := "Bearer sk-or-v1-test"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if want := "https://openrouter.ai/api/v1/chat/completions"; gotURL != want {
		t.Errorf("URL = %q, want %q", gotURL, want)
	}
}

func TestOrcaRouterUsesBearerToken(t *testing.T) {
	t.Setenv("VOIE_ORCAROUTER_KEY", "sk-orca-test")
	var gotAuth, gotURL string
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotURL = r.URL.String()
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))

	if _, err := NewOrcaRouter().ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hello"}}, "orcafree"); err != nil {
		t.Fatal(err)
	}
	if want := "Bearer sk-orca-test"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if want := "https://api.orcarouter.ai/v1/chat/completions"; gotURL != want {
		t.Errorf("URL = %q, want %q", gotURL, want)
	}
}

func TestFreeAliasesResolve(t *testing.T) {
	t.Setenv("VOIE_OPENROUTER_KEY", "sk-or-v1-test")
	t.Setenv("VOIE_ORCAROUTER_KEY", "sk-orca-test")
	for _, tc := range []struct {
		provider, alias, upstream string
	}{
		{"openrouter", "orfree", "openrouter/free"},
		{"orcarouter", "orcafree", "orcarouter/free"},
	} {
		var gotModel string
		useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			gotModel = extractModel(string(body))
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hi"}}]}`)
		}))
		var provider *openAIKeylessProvider
		if tc.provider == "openrouter" {
			provider = NewOpenRouter().openAIKeylessProvider
		} else {
			provider = NewOrcaRouter().openAIKeylessProvider
		}
		if _, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hello"}}, tc.alias); err != nil {
			t.Fatal(err)
		}
		if gotModel != tc.upstream {
			t.Errorf("%s: model = %q, want %q", tc.provider, gotModel, tc.upstream)
		}
	}
}

func extractModel(body string) string {
	const key = `"model":"`
	start := strings.Index(body, key)
	if start < 0 {
		return ""
	}
	rest := body[start+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func TestTokenErrorHintOnlyForAuthFailures(t *testing.T) {
	auth := tokenErrorHint(app.NewProviderFailure(app.FailurePermanent, "openrouter", "m", 401, nil), "openrouter")
	if !strings.Contains(auth.Error(), "voie connect openrouter") {
		t.Errorf("auth error has no reconnect hint: %v", auth)
	}
	rate := tokenErrorHint(app.NewProviderFailure(app.FailureTransient, "orcarouter", "m", 429, nil), "orcarouter")
	if strings.Contains(rate.Error(), "voie connect") {
		t.Errorf("rate-limit error suggests reconnecting: %v", rate)
	}
}

func TestOpencodeUsesLocalKeyFromCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENCODE_ZEN_KEY", "")
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("opencode-zen", "from-file-key"); err != nil {
		t.Fatal(err)
	}
	if got, want := opencodeGatewayKey(), "from-file-key"; got != want {
		t.Errorf("opencodeGatewayKey() = %q, want %q", got, want)
	}
	t.Setenv("OPENCODE_ZEN_KEY", "from-env-key")
	if got, want := opencodeGatewayKey(), "from-env-key"; got != want {
		t.Errorf("opencodeGatewayKey() with env = %q, want %q", got, want)
	}
}

func TestOpenRouterInvalidTokenErrorIsActionable(t *testing.T) {
	t.Setenv("VOIE_OPENROUTER_KEY", "sk-or-v1-secret-token")
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid key"}`)
	}))

	_, err := NewOpenRouter().ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hello"}}, "orfree")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "voie connect openrouter") {
		t.Errorf("error %q does not point to voie connect", err)
	}
	if strings.Contains(err.Error(), "sk-or-v1-secret-token") {
		t.Errorf("error leaked the token: %q", err)
	}
}
