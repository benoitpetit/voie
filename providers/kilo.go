package providers

import (
	"net/http"

	"github.com/benoitpetit/voie/internal/app"
)

// Kilo serves its free model lane through a keyless OpenAI-compatible gateway.
type Kilo struct {
	*openAIKeylessProvider
}

// Model IDs follow the maintained catalogue at
// https://github.com/mvalentsev/awesome-free-ai-coding/blob/main/configs/opencode.json
var kiloConfig = openAIKeylessConfig{
	Name:           "kilo",
	Label:          "Kilo Code",
	URL:            "https://api.kilo.ai/api/gateway/models",
	CompletionsURL: "https://api.kilo.ai/api/gateway/chat/completions",
	DefaultModel:   "kilo-auto",
	Description:    "Kilo Code keyless gateway; prompts may be logged by upstream providers",
	SupportsStream: true,
	ModelIDs: map[string]string{
		"kilo-auto":               "kilo-auto/free",
		"kilo-openrouter":         "openrouter/free",
		"kilo-nemotron-ultra":     "nvidia/nemotron-3-ultra-550b-a55b:free",
		"kilo-nemotron-lightning": "nvidia/nemotron-3.5-lightning:free",
		"kilo-nemotron-nano-omni": "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free",
		"kilo-nemotron-super-vl":  "nvidia/nemotron-3.5-super-vl:free",
		"kilo-gemma-4-31b":        "google/gemma-4-31b-it:free",
		"kilo-gemma-4-26b":        "google/gemma-4-26b-a4b-it:free",
		"kilo-north-mini-code":    "cohere/north-mini-code:free",
		"kilo-dots-3-note":        "dots-studio/dots-3-note-preview:free",
		"kilo-particle-1":         "particle-ai/particle-1:free",
		"kilo-glyph-cluster":      "stealth/glyph-cluster",
		"kilo-beam-free":          "reflection/beam-free",
	},
}

var _ app.Provider = (*Kilo)(nil)

func NewKilo() *Kilo {
	return &Kilo{newOpenAIKeylessProvider(kiloConfig, &http.Client{Timeout: openAIKeylessTimeout})}
}
