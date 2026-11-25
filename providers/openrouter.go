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

// OpenRouter relays free OpenRouter models through its OpenAI-compatible API.
// The stable `openrouter/free` router avoids tracking rotating :free IDs.
type OpenRouter struct {
	*openAIKeylessProvider
}

func NewOpenRouter() *OpenRouter {
	return &OpenRouter{newOpenAIKeylessProvider(openAIKeylessConfig{
		Name:           "openrouter",
		Label:          "OpenRouter",
		URL:            "https://openrouter.ai/api/v1/models",
		CompletionsURL: "https://openrouter.ai/api/v1/chat/completions",
		DefaultModel:   "orfree",
		ModelIDs:       map[string]string{"orfree": "openrouter/free"},
		ModelsSupplier: func() map[string]string {
			models := catalog.Models("openrouter")
			for id := range pricing.All() {
				models[app.PrefixModelID("openrouter", id)] = id
			}
			return models
		},
		Description:    "Free OpenRouter models through a configured API token",
		SupportsStream: true,
		TokenSupplier: func() string {
			current, _ := config.LoadCredentials()
			return current.Token("openrouter")
		},
	}, &http.Client{Timeout: openAIKeylessTimeout})}
}

func (p *OpenRouter) SupportsModel(model string) bool {
	store, _ := config.LoadCredentials()
	return store.Token("openrouter") != "" && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *OpenRouter) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.FreeModels = make([]string, 0)
	seenFree := map[string]bool{}
	for id := range catalog.Models("openrouter") {
		info.FreeModels = append(info.FreeModels, id)
		seenFree[id] = true
	}
	for _, upstream := range catalog.Models("openrouter") {
		concrete := app.PrefixModelID("openrouter", upstream)
		if !seenFree[concrete] {
			info.FreeModels = append(info.FreeModels, concrete)
			seenFree[concrete] = true
		}
	}
	sort.Strings(info.FreeModels)
	store, _ := config.LoadCredentials()
	info.Working = store.Token("openrouter") != ""
	return info
}

func (p *OpenRouter) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, tokenErrorHint(err, "openrouter")
	}
	return response, nil
}

func (p *OpenRouter) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(string)) error {
	if err := p.openAIKeylessProvider.ChatCompletionStream(ctx, messages, model, callback); err != nil {
		return tokenErrorHint(err, "openrouter")
	}
	return nil
}
