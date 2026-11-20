package providers

import (
	"net/http"
	"os"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
)

// OpenCodeZen relays to a local opencode-zen gateway, which drives the free
// models of OpenCode Zen. The gateway exposes an OpenAI-compatible endpoint on
// loopback and requires only a local API key.
type OpenCodeZen struct {
	*openAIKeylessProvider
}

const (
	opencodeZenBaseURL  = "http://127.0.0.1:8090"
	opencodeZenLocalKey = "voie-local"
)

// opencodeGatewayKey is the local gateway's own key, never an OpenCode credential.
func opencodeGatewayKey() string {
	key := strings.TrimSpace(os.Getenv("OPENCODE_ZEN_KEY"))
	if key == "" {
		return opencodeZenLocalKey
	}
	return key
}

func opencodeZenConfig() openAIKeylessConfig {
	return openAIKeylessConfig{
		Name:           "opencode-zen",
		Label:          "OpenCode Zen",
		URL:            opencodeZenBaseURL + "/healthz",
		CompletionsURL: opencodeZenBaseURL + "/v1/chat/completions",
		DefaultModel:   "oc2a-hy4-flash-free",
		Description:    "Free OpenCode Zen models through a local opencode-zen gateway",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + opencodeGatewayKey()},
		ModelIDs: map[string]string{
			"oc2a-beam-free":                   "beam-free",
			"oc2a-big-pickle":                  "big-pickle",
			"oc2a-deepseek-v4.1-flash-free":    "deepseek-v4.1-flash-free",
			"oc2a-flint-40b-free":              "flint-40b-free",
			"oc2a-hy4-flash-free":              "hy4-flash-free",
			"oc2a-iceman-free":                 "iceman-free",
			"oc2a-longcat-2.5-free":            "longcat-2.5-free",
			"oc2a-mimo-v2.6-flash-free":        "mimo-v2.6-flash-free",
			"oc2a-nemotron-3.5-lightning-free": "nemotron-3.5-lightning-free",
			"oc2a-nemotron-3.5-super-vl-free":  "nemotron-3.5-super-vl-free",
			"oc2a-tidepool-alpha-free":         "tidepool-alpha-free",
		},
	}
}

var _ app.Provider = (*OpenCodeZen)(nil)

func NewOpenCodeZen() *OpenCodeZen {
	return &OpenCodeZen{newOpenAIKeylessProvider(opencodeZenConfig(), &http.Client{Timeout: openAIKeylessTimeout})}
}
