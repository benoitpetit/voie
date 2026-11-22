package providers

import (
	"net/http"

	"github.com/benoitpetit/voie/internal/app"
)

// Pollinations uses its legacy keyless text host; the replacement API requires
// a personal key and Pollen credits.
type Pollinations struct {
	*openAIKeylessProvider
}

var pollinationsConfig = openAIKeylessConfig{
	Name:           "pollinations",
	Label:          "Pollinations.AI",
	URL:            "https://text.pollinations.ai/models",
	CompletionsURL: "https://text.pollinations.ai/openai",
	DefaultModel:   "pll-openai-fast",
	Description:    "Pollinations.AI legacy keyless text API",
	SupportsStream: true,
	ModelIDs: map[string]string{
		"pll-openai-fast": "openai-fast",
	},
}

var _ app.Provider = (*Pollinations)(nil)

func NewPollinations() *Pollinations {
	return &Pollinations{newOpenAIKeylessProvider(pollinationsConfig, &http.Client{Timeout: openAIKeylessTimeout})}
}
