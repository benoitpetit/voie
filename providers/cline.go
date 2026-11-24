package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/install"
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
		HealthBody: `"service":"voie-bridge"`,
		KeyEnv:     "VOIE_CLINE_KEY",
		Port:       8092,
	}
}

func NewCline() *Cline {
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
		TokenSupplier:  gateway.localKey,
		Ensure:         gateway.Ensure,
	}
	return &Cline{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), gateway: gateway}
}

func (p *Cline) working() bool {
	// Visibility follows the installed CLI; the bridge itself is started
	// lazily on the first request (Ensure). Requiring a live bridge here would
	// hide the model before the first call can start it.
	if install.BinaryPath("cline") != "" {
		return true
	}
	_, err := exec.LookPath("cline")
	return err == nil
}

func (p *Cline) SupportsModel(model string) bool {
	return p.working() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *Cline) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.Working = p.working()
	return info
}

// StartClineBridge starts the in-process ACP bridge used by `voie connect cline`.
func StartClineBridge(ctx context.Context) error {
	return NewCline().gateway.Ensure(ctx)
}

func (p *Cline) ChatCompletion(ctx context.Context, messages []Message, model string) (*ChatCompletionResponse, error) {
	response, err := p.openAIKeylessProvider.ChatCompletion(ctx, messages, model)
	if err != nil {
		return nil, fmt.Errorf("%w (check `cline auth` and the local ACP bridge)", err)
	}
	return response, nil
}
