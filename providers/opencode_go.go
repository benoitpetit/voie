package providers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/benoitpetit/voie/internal/app"
)

// OpenCodeGo relays OpenCode Go catalogue models through the same local
// gateway. These IDs need a Go credential configured in the gateway (API key
// or OAuth); voie callers still need no key.
type OpenCodeGo struct {
	*openAIKeylessProvider
}

func opencodeGoConfig() openAIKeylessConfig {
	return openAIKeylessConfig{
		Name:           "opencode-go",
		Label:          "OpenCode Go",
		URL:            opencodeGatewayURL() + "/healthz",
		CompletionsURL: opencodeGatewayURL() + "/v1/chat/completions",
		DefaultModel:   "oc2ago-gpt-6-luna",
		Description:    "OpenCode Go models via a local gateway; requires a Go API key or OAuth connection in the gateway",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + opencodeGatewayKey()},
		ModelIDs: map[string]string{
			"oc2ago-gpt-6.2-astra":     "gpt-6.2-astra",
			"oc2ago-gpt-6.2-sol":       "gpt-6.2-sol",
			"oc2ago-gpt-6.1-sol":       "gpt-6.1-sol",
			"oc2ago-gpt-6-luna":        "gpt-6-luna",
			"oc2ago-gpt-5.6-sol":       "gpt-5.6-sol",
			"oc2ago-gpt-5.6-terra":     "gpt-5.6-terra",
			"oc2ago-gpt-5.6-luna":      "gpt-5.6-luna",
			"oc2ago-claude-opus-5-5":   "claude-opus-5-5",
			"oc2ago-claude-opus-5":     "claude-opus-5",
			"oc2ago-claude-sonnet-5-5": "claude-sonnet-5-5",
			"oc2ago-claude-sonnet-5":   "claude-sonnet-5",
			"oc2ago-claude-haiku-5-5":  "claude-haiku-5-5",
			"oc2ago-gemini-4-carbon":   "gemini-4-carbon",
			"oc2ago-gemini-4-argon":    "gemini-4-argon",
			"oc2ago-gemini-4-hydrogen": "gemini-4-hydrogen",
			"oc2ago-gemini-3.8-flash":  "gemini-3-8-flash",
			"oc2ago-grok-4.7":          "grok-4.7",
			"oc2ago-qwen3.8-max":       "qwen3.8-max",
			"oc2ago-deepseek-v4-pro":   "deepseek-v4-pro",
			"oc2ago-kimi-k3":           "kimi-k3",
		},
	}
}

var _ app.Provider = (*OpenCodeGo)(nil)

func NewOpenCodeGo() *OpenCodeGo {
	return &OpenCodeGo{newOpenAIKeylessProvider(opencodeGoConfig(), &http.Client{Timeout: openAIKeylessTimeout})}
}

// GetInfo hides OpenCode Go when the local gateway has no Go credentials or
// models. The gateway reports `models.go` and `keys.go` on /healthz; without
// them none of the declared IDs can produce a completion.
func (p *OpenCodeGo) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(opencodeGatewayURL() + "/healthz")
	if err != nil {
		info.Working = false
		return info
	}
	defer resp.Body.Close()
	var health struct {
		Models struct {
			Go int `json:"go"`
		} `json:"models"`
		Keys struct {
			Go int `json:"go"`
		} `json:"keys"`
	}
	if json.NewDecoder(resp.Body).Decode(&health) != nil {
		info.Working = false
		return info
	}
	info.Working = health.Models.Go > 0 && health.Keys.Go > 0
	return info
}

func (p *OpenCodeGo) SupportsModel(model string) bool {
	return p.GetInfo().Working && p.openAIKeylessProvider.SupportsModel(model)
}
