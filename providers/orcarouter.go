package providers

import (
	"context"
	"net/http"
	"sort"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/catalog"
	"github.com/benoitpetit/voie/internal/pricing"
)

const orcaRouterModelsURL = "https://api.orcarouter.ai/v1/models"

func init() {
	// OrcaRouter publishes a private, token-authenticated price catalogue.
	// Registering it here keeps price lookups and model discovery in sync.
	pricing.RegisterSource("orcarouter", orcaRouterModelsURL, func() string {
		current, _ := config.LoadCredentials()
		return current.Token("orcarouter")
	})
}

// OrcaRouter relays OrcaRouter models through its OpenAI-compatible API.
type OrcaRouter struct {
	*openAIKeylessProvider
}

func NewOrcaRouter() *OrcaRouter {
	return &OrcaRouter{newOpenAIKeylessProvider(openAIKeylessConfig{
		Name:           "orcarouter",
		Label:          "OrcaRouter",
		URL:            orcaRouterModelsURL,
		CompletionsURL: "https://api.orcarouter.ai/v1/chat/completions",
		DefaultModel:   "orcafree",
		ModelIDs:       map[string]string{"orcafree": "orcarouter/free"},
		ModelsSupplier: func() map[string]string {
			models := catalog.Models("orcarouter")
			for id, price := range pricing.AllFor("orcarouter") {
				// Unpriced router aliases stay in the verified free lane.
				if price.Prompt == "" {
					continue
				}
				models[app.PrefixModelID("orcarouter", id)] = id
			}
			return models
		},
		Description:    "OrcaRouter models through a configured API token",
		SupportsStream: true,
		TokenSupplier: func() string {
			current, _ := config.LoadCredentials()
			return current.Token("orcarouter")
		},
	}, &http.Client{Timeout: openAIKeylessTimeout})}
}

func (p *OrcaRouter) SupportsModel(model string) bool {
	store, _ := config.LoadCredentials()
	return store.Token("orcarouter") != "" && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *OrcaRouter) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	seen := map[string]bool{}
	free := make([]string, 0)
	addFree := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			free = append(free, id)
		}
	}
	for id := range catalog.Models("orcarouter") {
		addFree(id)
		addFree(app.PrefixModelID("orcarouter", id))
	}
	for id, price := range pricing.AllFor("orcarouter") {
		if price.Prompt == "" || price.Prompt == "0" {
			addFree(id)
			addFree(app.PrefixModelID("orcarouter", id))
		}
	}
	sort.Strings(free)
	info.FreeModels = free
	store, _ := config.LoadCredentials()
	info.Working = store.Token("orcarouter") != ""
	return info
}

func (p *OrcaRouter) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, tokenErrorHint(err, "orcarouter")
	}
	return response, nil
}

func (p *OrcaRouter) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(string)) error {
	if err := p.openAIKeylessProvider.ChatCompletionStream(ctx, messages, model, callback); err != nil {
		return tokenErrorHint(err, "orcarouter")
	}
	return nil
}
