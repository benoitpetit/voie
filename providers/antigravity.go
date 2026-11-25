package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/install"
)

// Antigravity relays free models from a local CLIProxyAPI gateway. The gateway
// owns the Google OAuth credential; voie only holds the loopback key.
type Antigravity struct {
	*openAIKeylessProvider
	gateway *LocalGateway
}

func antigravityGatewaySpec() LocalGatewaySpec {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	configDir := filepath.Join(dir, "voie", "gateways", "antigravity")
	binary := install.BinaryPath("antigravity")
	if binary == "" {
		binary = "CLIProxyAPI"
	}
	body := `config-version: 8
server:
  host: "127.0.0.1"
  port: 8317
management:
  allow-remote: false
  secret-key: "voie-management"
access:
  api-keys:
    - "voie-local"
oauth:
  auth-dir: "` + filepath.Join(dir, ".cli-proxy-api") + `"
  providers:
    antigravity:
      antigravity-credits: true
      tos-pause-window: "6h"
`
	return LocalGatewaySpec{
		Name:       "antigravity",
		Binary:     binary,
		Args:       []string{"-config", filepath.Join(configDir, "config.yaml")},
		Dir:        configDir,
		ConfigPath: filepath.Join(configDir, "config.yaml"),
		ConfigBody: []byte(body),
		BaseURL:    "http://127.0.0.1:8317",
		HealthPath: "/v1/models",
		KeyEnv:     "VOIE_ANTIGRAVITY_KEY",
		Port:       8317,
	}
}

func NewAntigravity() *Antigravity {
	gateway := NewLocalGateway(antigravityGatewaySpec())
	cfg := openAIKeylessConfig{
		Name:           "antigravity",
		Label:          "Antigravity",
		URL:            "http://127.0.0.1:8317/v1/models",
		CompletionsURL: "http://127.0.0.1:8317/v1/chat/completions",
		DefaultModel:   "ag-flash",
		ModelIDs: map[string]string{
			"ag-flash":  "gemini-3.8-flash-tiered",
			"ag-sonnet": "claude-sonnet-4-6",
		},
		Description:    "Free Antigravity models through a local CLIProxyAPI gateway",
		SupportsStream: true,
		TokenSupplier:  gateway.localKey,
		BodyHint:       antigravityBodyHint,
		Ensure:         gateway.Ensure,
	}
	return &Antigravity{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), gateway: gateway}
}

func antigravityBodyHint(status int, body []byte) string {
	if status != http.StatusForbidden || !strings.Contains(string(body), "TOS_VIOLATION") {
		return ""
	}
	appeal := ""
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) == nil {
		if root, ok := parsed["error"].(map[string]any); ok {
			if details, ok := root["details"].([]any); ok && len(details) > 0 {
				if detail, ok := details[0].(map[string]any); ok {
					if metadata, ok := detail["metadata"].(map[string]any); ok {
						appeal, _ = metadata["appeal_url"].(string)
					}
				}
			}
		}
	}
	if appeal != "" {
		return "Antigravity account TOS_VIOLATION is terminal; appeal at " + appeal
	}
	return "Antigravity account TOS_VIOLATION is terminal; do not retry or rotate this account"
}

func (p *Antigravity) SupportsModel(model string) bool {
	return p.gateway.Available() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *Antigravity) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.Working = providerConnected("antigravity") && p.gateway.Available()
	return info
}

func (p *Antigravity) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	return p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
}
