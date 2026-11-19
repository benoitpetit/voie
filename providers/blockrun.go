package providers

import (
	"net/http"

	"github.com/benoitpetit/voie/internal/app"
)

// BlockRun serves a keyless OpenAI-compatible free lane with a 30 req/min and
// 300 req/hour per-IP budget; upstreams may substitute another free model.
type BlockRun struct {
	*openAIKeylessProvider
}

var blockrunConfig = openAIKeylessConfig{
	Name:           "blockrun",
	Label:          "BlockRun",
	URL:            "https://blockrun.ai/api/v1/models",
	CompletionsURL: "https://blockrun.ai/api/v1/chat/completions",
	DefaultModel:   "blockrun-nemotron-3.5-bolt",
	Description:    "BlockRun keyless free lane; upstreams may substitute another free model",
	SupportsStream: true,
	ModelIDs: map[string]string{
		"blockrun-nemotron-3.5-bolt":      "nvidia/nemotron-3.5-bolt",
		"blockrun-nemotron-3.5-lightning": "nvidia/nemotron-3.5-lightning",
		"blockrun-muse-glimmer-30b":       "nvidia/muse-glimmer-30b",
		"blockrun-laguna-xs-2.1":          "poolside/laguna-xs-2.1",
	},
}

var _ app.Provider = (*BlockRun)(nil)

func NewBlockRun() *BlockRun {
	return &BlockRun{newOpenAIKeylessProvider(blockrunConfig, &http.Client{Timeout: openAIKeylessTimeout})}
}
