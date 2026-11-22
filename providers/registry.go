package providers

import "github.com/benoitpetit/voie/internal/app"

func NewRegistry() *app.Registry {
	registry := app.NewRegistry()
	registry.Register("perplexity", &Perplexity{})
	registry.Register("duckai", NewDuckAI())
	registry.Register("deepai", &DeepAI{})
	registry.Register("quillbot", &Quillbot{})
	registry.Register("yqcloud", &Yqcloud{})
	registry.Register("jimmy", &Jimmy{})
	registry.Register("kilo", NewKilo())
	registry.Register("ovh", NewOVH())
	registry.Register("blockrun", NewBlockRun())
	registry.Register("pollinations", NewPollinations())
	registry.Register("opencode-zen", NewOpenCodeZen())
	registry.Register("opencode-go", NewOpenCodeGo())
	registry.Register("openrouter", NewOpenRouter())
	registry.Register("orcarouter", NewOrcaRouter())
	registry.Register("antigravity", NewAntigravity())
	registry.Register("kiro", NewKiro())
	registry.Register("cline", NewCline())
	for _, spec := range TokenProviders {
		registry.Register(spec.Name, NewTokenProvider(spec))
	}
	return registry
}
