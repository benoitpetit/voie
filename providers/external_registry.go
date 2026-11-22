package providers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

// ExternalGatewaySpec describes a third-party loopback gateway that exposes
// one or more coding-agent services as OpenAI-compatible endpoints. These are
// opt-in and user-installed: voie never bundles or operates them.
type ExternalGatewaySpec struct {
	Name        string
	Label       string
	Repo        string
	Port        int
	Alias       string
	Upstream    string
	HealthPath  string
	InstallKind string // "go" or "npm"
}

// ExternalGateways are ranked by observed activity in Nov 2026. They use
// reverse-engineered upstream endpoints and carry ToS/account risk.
var ExternalGateways = []ExternalGatewaySpec{
	{Name: "omniroute", Label: "OmniRoute", Repo: "https://github.com/cheaper-inference/OmniRoute", Port: 20128, Alias: "omni-free", Upstream: "openrouter/free", HealthPath: "/v1/models", InstallKind: "npm"},
	{Name: "qoder2api", Label: "Qoder2API Hub", Repo: "https://github.com/shuishuipingan/qoder2api-hub", Port: 8790, Alias: "qoder2-free", Upstream: "auto", HealthPath: "/v1/models"},
	{Name: "windsurfapi", Label: "WindsurfAPI", Repo: "https://github.com/dwgx/WindsurfAPI", Port: 3003, Alias: "devin2-free", Upstream: "swe-2-flash-medium", HealthPath: "/v1/models"},
	{Name: "copilot-api", Label: "Copilot API", Repo: "https://github.com/caozhiyuan/copilot-api", Port: 4141, Alias: "copilot2-free", Upstream: "auto", HealthPath: "/v1/models", InstallKind: "npm"},
	{Name: "cursor-api-proxy", Label: "Cursor ACP Proxy", Repo: "https://github.com/anyrobert/cursor-api-proxy", Port: 8765, Alias: "cursor2-free", Upstream: "auto", HealthPath: "/healthz", InstallKind: "npm"},
	{Name: "codearts2api", Label: "CodeArts2API", Repo: "https://github.com/zhuweiyou/codearts2api", Port: 8080, Alias: "codearts-free", Upstream: "minimax", HealthPath: "/healthz", InstallKind: "npm"},
	{Name: "cli2api", Label: "CLI2API", Repo: "https://github.com/caigee-cmd/cli2api", Port: 3010, Alias: "cli2-free", Upstream: "qoder", HealthPath: "/v1/models"},
	{Name: "workbuddy2api", Label: "WorkBuddy2API", Repo: "https://github.com/hawklithm/workbuddy2api", Port: 8787, Alias: "wb2-free", Upstream: "auto", HealthPath: "/health"},
}

func ExternalGatewayByName(name string) (ExternalGatewaySpec, bool) {
	for _, spec := range ExternalGateways {
		if spec.Name == name {
			return spec, true
		}
	}
	return ExternalGatewaySpec{}, false
}

func gatewayDir(spec ExternalGatewaySpec) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "gateways", spec.Name)
}

// ExternalGateway is a normal provider pointed at a user-installed gateway.
type ExternalGateway struct {
	*openAIKeylessProvider
	spec ExternalGatewaySpec
}

func NewExternalGateway(spec ExternalGatewaySpec) *ExternalGateway {
	store, _ := config.LoadCredentials()
	key := store.Token(spec.Name)
	if key == "" {
		key = "voie-local"
	}
	cfg := openAIKeylessConfig{
		Name:           spec.Name,
		Label:          spec.Label,
		URL:            "http://127.0.0.1:" + itoa(spec.Port) + spec.HealthPath,
		CompletionsURL: "http://127.0.0.1:" + itoa(spec.Port) + "/v1/chat/completions",
		DefaultModel:   spec.Alias,
		ModelIDs:       map[string]string{spec.Alias: spec.Upstream},
		Description:    spec.Label + " through a user-installed reverse-engineered gateway (ToS risk)",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + key},
	}
	return &ExternalGateway{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), spec: spec}
}

func (p *ExternalGateway) working() bool {
	dir := gatewayDir(p.spec)
	if dir == "" {
		return false
	}
	_, err := os.Stat(dir)
	return err == nil
}

func (p *ExternalGateway) SupportsModel(model string) bool {
	return p.working() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *ExternalGateway) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.Working = p.working()
	return info
}

// InstallCommand returns the exact command a user must run to install a gateway.
func (p *ExternalGateway) InstallCommand() string {
	dir := gatewayDir(p.spec)
	if p.spec.InstallKind == "npm" {
		return "npm install --prefix " + dir + " " + p.spec.Repo
	}
	return "git clone " + p.spec.Repo + " " + dir
}
