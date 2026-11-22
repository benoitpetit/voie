package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
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
	store, _ := config.LoadCredentials()
	info.Working = store.Token("openrouter") != ""
	return info
}

func (p *OpenRouter) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, fmt.Errorf("%w (run `voie connect openrouter` if the token was rejected)", err)
	}
	return response, nil
}

func (p *OpenRouter) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(string)) error {
	if err := p.openAIKeylessProvider.ChatCompletionStream(ctx, messages, model, callback); err != nil {
		return fmt.Errorf("%w (run `voie connect openrouter` if the token was rejected)", err)
	}
	return nil
}
