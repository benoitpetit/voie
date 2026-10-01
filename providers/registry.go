package providers

import "github.com/benoitpetit/voie/internal/app"

func NewRegistry() *app.Registry {
	registry := app.NewRegistry()
	registry.Register("perplexity", &Perplexity{})
	registry.Register("duckai", NewDuckAI())
	registry.Register("deepai", &DeepAI{})
	registry.Register("quillbot", &Quillbot{})
	registry.Register("yqcloud", &Yqcloud{})
	registry.Register("cohere", &CohereCommand{})
	return registry
}
