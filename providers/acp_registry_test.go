package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/clinebridge"
)

func TestACPProvidersAreRegistered(t *testing.T) {
	registry := NewRegistry()
	for _, spec := range ACPProviders {
		if registry.Get(spec.Name) == nil {
			t.Errorf("ACP provider %q is not registered", spec.Name)
		}
		if _, ok := ACPProviderSpecByName(spec.Name); !ok {
			t.Errorf("spec lookup failed for %q", spec.Name)
		}
	}
}

func TestACPProviderHiddenWithoutBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, spec := range ACPProviders {
		provider := NewACPProvider(spec)
		if provider.GetInfo().Working {
			t.Errorf("%s Working = true without its CLI", spec.Name)
		}
	}
}

func TestACPProviderConfig(t *testing.T) {
	spec, ok := ACPProviderSpecByName("codebuddy")
	if !ok {
		t.Fatal("codebuddy spec missing")
	}
	provider := NewACPProvider(spec)
	if got := provider.cfg.CompletionsURL; got != "http://127.0.0.1:8093/v1/chat/completions" {
		t.Errorf("CompletionsURL = %q", got)
	}
	if got := provider.cfg.ModelIDs["cb-auto"]; got != "auto" {
		t.Errorf("alias model = %q", got)
	}
}

func TestCLIProviderRequiresConnectBeforeCallable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codebuddy"), []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var spec ACPProviderSpec
	for _, candidate := range ACPProviders {
		if candidate.Name == "codebuddy" {
			spec = candidate
		}
	}
	if spec.Name == "" {
		t.Fatal("codebuddy spec missing")
	}
	if NewACPProvider(spec).GetInfo().Working {
		t.Fatal("installed CLI reported callable before connect")
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetState("codebuddy", "connected"); err != nil {
		t.Fatal(err)
	}
	if !NewACPProvider(spec).GetInfo().Working {
		t.Fatal("connected CLI should be callable")
	}
}

func TestACPProviderExposesDiscoveredModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := clinebridge.WriteModelCache("codebuddy", []clinebridge.ACPModel{{ID: "gpt-6-luna", Name: "GPT-6 Luna"}}); err != nil {
		t.Fatal(err)
	}
	var spec ACPProviderSpec
	for _, candidate := range ACPProviders {
		if candidate.Name == "codebuddy" {
			spec = candidate
		}
	}
	info := NewACPProvider(spec).GetInfo()
	found := false
	for _, id := range info.SupportedModels {
		if id == "cb-gpt-6-luna" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SupportedModels = %v, want cb-gpt-6-luna", info.SupportedModels)
	}
}
