package catalog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRegistry = `entries:
  - id: groq-free
    api:
      model_ids:
        - openai/gpt-oss-120b
        - qwen/qwen3.8-27b
`

func TestModelsRefreshAutomaticallyAndCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testRegistry))
	}))
	defer server.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	SetSource(server.URL)
	defer SetSource("")

	models := Models("groq")
	if len(models) < 2 {
		t.Fatalf("Models(groq) = %v, want at least 2 models", models)
	}
	if models["gq-openai-gpt-oss-120b"] != "openai/gpt-oss-120b" {
		t.Errorf("alias mapping = %v", models)
	}

	info, err := os.Stat(cachePath())
	if err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache mode = %o, want 600", info.Mode().Perm())
	}

	// With the remote source disabled, the on-disk cache still serves it.
	SetSource("")
	if got := Models("groq"); len(got) < 2 {
		t.Errorf("cached Models(groq) = %v", got)
	}
}

func TestBrokenModelsAreExcluded(t *testing.T) {
	SetSource("")
	defer SetSource("")
	for alias := range Models("vlm-run") {
		if excludedModels[alias] {
			t.Errorf("broken model %q is still exposed", alias)
		}
	}
}

func TestFallsBackToEmbeddedSnapshotOffline(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "empty"))
	SetSource("")
	defer SetSource("")
	if got := Models("openrouter"); len(got) < 10 {
		t.Errorf("offline Models(openrouter) = %d aliases, want the embedded catalogue", len(got))
	}
}

func TestOrcaRouterEmbeddedIDsUseOrcPrefix(t *testing.T) {
	for id := range embeddedModels["orcarouter"] {
		if strings.HasPrefix(id, "ocr-") {
			t.Errorf("orcarouter embedded model %q uses the stale ocr- prefix", id)
		}
	}
}

const testRegistryWithExcluded = `entries:
  - id: vlm-run-gateway
    api:
      model_ids:
        - vlm-qwen-qwen3-8-27b
        - qwen/qwen3.5-0.8b
`

func TestAllModelsIncludesExcludedModelsAsPaid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testRegistryWithExcluded))
	}))
	defer server.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	SetSource(server.URL)
	t.Cleanup(func() { SetSource("") })
	all := AllModels("vlm-run")
	free := Models("vlm-run")
	if len(all) != 3 {
		t.Fatalf("AllModels = %v, want all three models", all)
	}
	if len(free) != 2 {
		t.Fatalf("free Models = %v, want only the two verified free ones", free)
	}
	if _, ok := all["vlm-vlm-qwen-qwen3-8-27b"]; !ok {
		t.Fatalf("excluded model missing from AllModels: %v", all)
	}
}
