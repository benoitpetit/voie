package providers

import (
	"net/http"

	"github.com/benoitpetit/voie/internal/app"
)

// OVH serves anonymous, EU-hosted OpenAI-compatible endpoints; a 429 signals
// a rate limit rather than the end of the free offer.
type OVH struct {
	*openAIKeylessProvider
}

var ovhConfig = openAIKeylessConfig{
	Name:           "ovh",
	Label:          "OVHcloud AI Endpoints",
	URL:            "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1/models",
	CompletionsURL: "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1/chat/completions",
	DefaultModel:   "ovh-qwen3.8-27b",
	Description:    "OVHcloud anonymous lane; OVH states data is not used to train models",
	SupportsStream: true,
	ModelIDs: map[string]string{
		"ovh-qwen3.8-27b":  "Qwen3.8-27B",
		"ovh-qwen3.6-27b":  "Qwen3.6-27B",
		"ovh-gpt-oss-120b": "gpt-oss-120b",
	},
}

var _ app.Provider = (*OVH)(nil)

func NewOVH() *OVH {
	return &OVH{newOpenAIKeylessProvider(ovhConfig, &http.Client{Timeout: openAIKeylessTimeout})}
}
