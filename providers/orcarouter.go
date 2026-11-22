package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

// OrcaRouter relays free OrcaRouter models through its OpenAI-compatible API.
type OrcaRouter struct {
	*openAIKeylessProvider
}

func NewOrcaRouter() *OrcaRouter {
	store, _ := config.LoadCredentials()
	return &OrcaRouter{newOpenAIKeylessProvider(openAIKeylessConfig{
		Name:           "orcarouter",
		Label:          "OrcaRouter",
		URL:            "https://api.orcarouter.ai/v1/models",
		CompletionsURL: "https://api.orcarouter.ai/v1/chat/completions",
		DefaultModel:   "orcafree",
		ModelIDs:       map[string]string{"orcafree": "orcarouter/free"},
		Description:    "Free OrcaRouter models through a configured API token",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + store.Token("orcarouter")},
	}, &http.Client{Timeout: openAIKeylessTimeout})}
}

func (p *OrcaRouter) SupportsModel(model string) bool {
	store, _ := config.LoadCredentials()
	return store.Token("orcarouter") != "" && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *OrcaRouter) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	store, _ := config.LoadCredentials()
	info.Working = store.Token("orcarouter") != ""
	return info
}

func (p *OrcaRouter) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, fmt.Errorf("%w (run `voie connect orcarouter` if the token was rejected)", err)
	}
	return response, nil
}

func (p *OrcaRouter) ChatCompletionStream(ctx context.Context, messages []Message, model string, callback func(string)) error {
	if err := p.openAIKeylessProvider.ChatCompletionStream(ctx, messages, model, callback); err != nil {
		return fmt.Errorf("%w (run `voie connect orcarouter` if the token was rejected)", err)
	}
	return nil
}
