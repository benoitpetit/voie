package app_test

import (
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestModelIDsArePrefixedByProvider(t *testing.T) {
	for _, tc := range []struct{ provider, id, want string }{
		{"perplexity", "claude35haiku", "pplx-claude35haiku"},
		{"duckai", "gpt-6-luna", "dk-gpt-6-luna"},
		{"openrouter", "or-google-gemma-4-31b-it-free", "or-google-gemma-4-31b-it-free"},
		{"blockrun", "blockrun-muse-glimmer-30b", "blockrun-muse-glimmer-30b"},
	} {
		if got := app.PrefixModelID(tc.provider, tc.id); got != tc.want {
			t.Errorf("PrefixModelID(%q, %q) = %q, want %q", tc.provider, tc.id, got, tc.want)
		}
	}
}

func TestNormalizeModelIDKeepsLegacyIDsWorking(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"pplx-claude35haiku", "claude35haiku"},
		{"orfree", "orfree"},
		{"or-google-gemma-4-31b-it-free", "google-gemma-4-31b-it-free"},
		{"claude35haiku", "claude35haiku"},
	} {
		if got := app.NormalizeModelID(tc.in); got != tc.want {
			t.Errorf("NormalizeModelID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestListModelsUsesPrefixedIDs(t *testing.T) {
	service := newVisibilityService(t, &visibilityProvider{name: "perplexity", working: true, model: "claude35haiku"})
	models := service.ListModels()
	if len(models) != 1 || models[0].ID != "pplx-claude35haiku" {
		t.Fatalf("ListModels = %+v, want a prefixed public ID", models)
	}
	registry := app.NewRegistry()
	registry.Register("perplexity", &visibilityProvider{name: "perplexity", working: true, model: "claude35haiku"})
	if routed := registry.GetForModel("pplx-claude35haiku"); routed == nil {
		t.Error("prefixed model ID cannot be routed")
	}
}

func TestNormalizeModelIDUsesLongestPrefix(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"oc2ago-x", "x"},
		{"oc2a-x", "x"},
		{"cl2-cli2-free", "cli2-free"},
		{"orc-x", "x"},
		{"or-x", "x"},
	} {
		if got := app.NormalizeModelID(tc.input); got != tc.want {
			t.Errorf("NormalizeModelID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLegacyAliasesRemainUnprefixed(t *testing.T) {
	for _, tc := range []struct{ provider, id string }{
		{"openrouter", "orfree"},
		{"orcarouter", "orcafree"},
		{"vlm-run", "vlmfree"},
	} {
		if got := app.PrefixModelID(tc.provider, tc.id); got != tc.id {
			t.Errorf("PrefixModelID(%q, %q) = %q, want unchanged", tc.provider, tc.id, got)
		}
	}
}

func TestPPLXProxyModelIDsAreNamespaced(t *testing.T) {
	if got := app.PrefixModelID("pplx-proxy", "auto"); got != "pplx-auto" {
		t.Fatalf("PrefixModelID = %q, want pplx-auto", got)
	}
	if got := app.PrefixModelID("pplx-proxy", "pplx-gpt"); got != "pplx-gpt" {
		t.Fatalf("already-prefixed ID changed: %q", got)
	}
}

func TestDeclaredAliasesKeepTheirSpelling(t *testing.T) {
	for _, tc := range []struct{ provider, id string }{
		{"omniroute", "omni-free"},
		{"qoder2api", "qoder2-free"},
		{"windsurfapi", "devin2-free"},
		{"copilot-api", "copilot2-free"},
		{"cursor-api-proxy", "cursor2-free"},
		{"workbuddy2api", "wb2-free"},
		{"cli2api", "cli2-free"},
		{"groq", "grfree"},
		{"google-ai-studio", "gsfree"},
		{"zai", "zaifree"},
		{"mistral", "mistralfree"},
		{"aihubmix", "ahfree"},
		{"anyapi", "anyfree"},
		{"beatapi", "beatfree"},
		{"iflytek-spark", "sparkfree"},
		{"runcrate", "runfree"},
	} {
		if got := app.PrefixModelID(tc.provider, tc.id); got != tc.id {
			t.Errorf("%s/%s -> %q", tc.provider, tc.id, got)
		}
	}
}
