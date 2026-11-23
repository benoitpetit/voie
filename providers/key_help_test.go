package providers

import (
	"strings"
	"testing"
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
