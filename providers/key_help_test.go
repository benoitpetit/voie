package providers

import (
	"os"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestKeyHelpCoversEveryProvider(t *testing.T) {
	for _, name := range KeyHelpProviders() {
		steps := KeyHelp(name)
		if len(steps) == 0 {
			t.Errorf("no key help for %s", name)
		}
		for _, step := range steps {
			if strings.Contains(step, "No credential instructions") {
				t.Errorf("%s has no registered instructions", name)
			}
		}
	}
}

func TestKeyHelpCloudflareRequiresBothValues(t *testing.T) {
	help := strings.Join(KeyHelp("cloudflare-ai"), " ")
	if !strings.Contains(help, "Account ID") || !strings.Contains(help, "Workers AI") {
		t.Errorf("Cloudflare help is incomplete: %s", help)
	}
}

func TestKeyHelpTokenProvidersIncludeURL(t *testing.T) {
	for _, name := range []string{"openrouter", "groq", "cohere-trial", "requesty"} {
		help := strings.Join(KeyHelp(name), " ")
		if !strings.Contains(help, "https://") {
			t.Errorf("%s help has no key URL: %s", name, help)
		}
	}
}

func TestKeyHelpProvidersAreUniqueAndComplete(t *testing.T) {
	names := KeyHelpProviders()
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("duplicate key help provider %q", name)
		}
		seen[name] = true
	}
	for _, spec := range TokenProviders {
		if !seen[spec.Name] {
			t.Errorf("token provider %q has no key help entry", spec.Name)
		}
	}
	for _, spec := range ExternalGateways {
		if !seen[spec.Name] {
			t.Errorf("gateway %q has no key help entry", spec.Name)
		}
	}
}

func TestKeyHelpModelHintsUsePublicIDs(t *testing.T) {
	for _, spec := range ExternalGateways {
		want := "--model " + app.PrefixModelID(spec.Name, spec.Alias)
		if help := strings.Join(KeyHelp(spec.Name), " "); !strings.Contains(help, want) {
			t.Errorf("%s: help does not suggest %q: %s", spec.Name, want, help)
		}
	}
}

func TestNoDeadLLM7Case(t *testing.T) {
	source, err := os.ReadFile("key_help.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), `case "llm7"`) {
		t.Fatal("dead llm7 key help case is still present")
	}
}
