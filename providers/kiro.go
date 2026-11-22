package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/install"
)

// Kiro relays free models through the official kiro-cli ACP bridge. The bridge
// never reads Kiro credentials: the user signs in with `kiro-cli login`.
type Kiro struct {
	*openAIKeylessProvider
	gateway *LocalGateway
}

func kiroGatewaySpec() LocalGatewaySpec {
	dir := os.Getenv("KIRO_GATEWAY_DIR")
	if dir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			configDir = os.TempDir()
		}
		dir = filepath.Join(configDir, "voie", "gateways", "kiro-gateway")
	}
	body := `SERVER_HOST=127.0.0.1
SERVER_PORT=8091
KIRO_GATEWAY_API_KEY=voie-local
KIRO_CLI_PATH=` + filepath.Join(os.Getenv("HOME"), ".local", "bin", "kiro-cli") + `
KIRO_ACP_ENGINE=v2
MODEL_VALIDATION=strict
MCP_DISCOVERY=user
ACP_TRUST_TOOLS=false
`
	return LocalGatewaySpec{
		Name:       "kiro",
		Binary:     "uv",
		Args:       []string{"run", "main.py"},
		Dir:        dir,
		ConfigPath: filepath.Join(dir, ".env"),
		ConfigBody: []byte(body),
		BaseURL:    "http://127.0.0.1:8091",
		HealthPath: "/health",
		KeyEnv:     "VOIE_KIRO_KEY",
		Port:       8091,
	}
}

func NewKiro() *Kiro {
	store, _ := config.LoadCredentials()
	key := store.Token("kiro")
	if key == "" {
		key = "voie-local"
	}
	gateway := NewLocalGateway(kiroGatewaySpec())
	cfg := openAIKeylessConfig{
		Name:           "kiro",
		Label:          "Kiro",
		URL:            "http://127.0.0.1:8091/health",
		CompletionsURL: "http://127.0.0.1:8091/v1/chat/completions",
		DefaultModel:   "kiro-auto",
		ModelIDs: map[string]string{
			"kiro-auto":   "auto",
			"kiro-sonnet": "claude-sonnet-4.5",
		},
		Description:    "Free Kiro models through an official-CLI ACP bridge",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + key},
		Ensure:         gateway.Ensure,
	}
	return &Kiro{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), gateway: gateway}
}

func (p *Kiro) working() bool {
	if _, err := exec.LookPath("kiro-cli"); err != nil {
		return false
	}
	bridge := install.KiroBridgeDir()
	if bridge == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(bridge, "main.py")); err != nil {
		return false
	}
	return p.gateway.Available()
}

func (p *Kiro) SupportsModel(model string) bool {
	return p.working() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *Kiro) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.Working = p.working()
	return info
}

func (p *Kiro) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, fmt.Errorf("%w (check `kiro-cli whoami` and the ACP bridge)", err)
	}
	return response, nil
}
