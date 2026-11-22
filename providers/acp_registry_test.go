package providers

import (
	"testing"
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
