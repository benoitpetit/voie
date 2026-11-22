package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

// Cline relays Cline's rotating free models through a local ACP bridge that
// voie starts on demand. The bridge denies all agent tools.
type Cline struct {
	*openAIKeylessProvider
	gateway *LocalGateway
}

func clineGatewaySpec() LocalGatewaySpec {
	executable, err := os.Executable()
	if err != nil {
		executable = "voie"
	}
	return LocalGatewaySpec{
		Name:       "cline",
		Binary:     executable,
		Args:       []string{"cline-bridge"},
		ConfigPath: "",
		BaseURL:    "http://127.0.0.1:8092",
		HealthPath: "/health",
		KeyEnv:     "VOIE_CLINE_KEY",
		Port:       8092,
	}
}

func NewCline() *Cline {
	store, _ := config.LoadCredentials()
	key := store.Token("cline")
	if key == "" {
		key = "voie-local"
	}
	gateway := NewLocalGateway(clineGatewaySpec())
	cfg := openAIKeylessConfig{
		Name:           "cline",
		Label:          "Cline",
		URL:            "http://127.0.0.1:8092/health",
		CompletionsURL: "http://127.0.0.1:8092/v1/chat/completions",
		DefaultModel:   "cline-flash",
		ModelIDs:       map[string]string{"cline-flash": "cline-free/nemotron-3.5-super-vl"},
		Description:    "Free Cline models through a local ACP bridge",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + key},
		Ensure:         gateway.Ensure,
	}
	return &Cline{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), gateway: gateway}
}

func (p *Cline) working() bool {
	if _, err := exec.LookPath("cline"); err != nil {
		return false
	}
	return p.gateway.Available()
}

func (p *Cline) SupportsModel(model string) bool {
	return p.working() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *Cline) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.Working = p.working()
	return info
}

func (p *Cline) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, fmt.Errorf("%w (check `cline auth` and the local ACP bridge)", err)
	}
	return response, nil
}
