package providers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	InstallKind string   // "npm" or "git" (kept for the printed command)
	Deps        []string // dependency install command, run in the cloned directory
	Start       []string // command that launches the gateway on its loopback port
}

// ExternalGateways are ranked by observed activity in Nov 2026. They use
// reverse-engineered upstream endpoints and carry ToS/account risk.
var ExternalGateways = []ExternalGatewaySpec{
	{Name: "omniroute", Label: "OmniRoute", Repo: "https://github.com/cheaper-inference/OmniRoute", Port: 20128, Alias: "omni-free", Upstream: "openrouter/free", HealthPath: "/v1/models", InstallKind: "npm", Deps: []string{"npm", "install"}, Start: []string{"npm", "start"}},
	{Name: "qoder2api", Label: "Qoder2API Hub", Repo: "https://github.com/shuishuipingan/qoder2api-hub", Port: 8790, Alias: "qoder2-free", Upstream: "auto", HealthPath: "/v1/models", InstallKind: "git", Deps: []string{"pip", "install", "-r", "requirements.txt"}, Start: []string{"python3", "app.py"}},
	{Name: "windsurfapi", Label: "WindsurfAPI", Repo: "https://github.com/dwgx/WindsurfAPI", Port: 3003, Alias: "devin2-free", Upstream: "swe-2-flash-medium", HealthPath: "/v1/models", InstallKind: "git", Deps: []string{"npm", "install"}, Start: []string{"npm", "start"}},
	{Name: "copilot-api", Label: "Copilot API", Repo: "https://github.com/caozhiyuan/copilot-api", Port: 4141, Alias: "copilot2-free", Upstream: "auto", HealthPath: "/v1/models", InstallKind: "npm", Deps: []string{"npm", "install"}, Start: []string{"npm", "start"}},
	{Name: "cursor-api-proxy", Label: "Cursor ACP Proxy", Repo: "https://github.com/anyrobert/cursor-api-proxy", Port: 8765, Alias: "cursor2-free", Upstream: "auto", HealthPath: "/healthz", InstallKind: "npm", Deps: []string{"npm", "install"}, Start: []string{"npm", "start"}},
	{Name: "codearts2api", Label: "CodeArts2API", Repo: "https://github.com/zhuweiyou/codearts2api", Port: 8080, Alias: "codearts-free", Upstream: "minimax", HealthPath: "/healthz", InstallKind: "npm", Deps: []string{"npm", "install"}, Start: []string{"npm", "start"}},
	{Name: "cli2api", Label: "CLI2API", Repo: "https://github.com/caigee-cmd/cli2api", Port: 3010, Alias: "cli2-free", Upstream: "qoder", HealthPath: "/v1/models", InstallKind: "git", Deps: []string{"go", "build", "-o", "gateway", "."}, Start: []string{"./gateway"}},
	{Name: "workbuddy2api", Label: "WorkBuddy2API", Repo: "https://github.com/hawklithm/workbuddy2api", Port: 8787, Alias: "wb2-free", Upstream: "auto", HealthPath: "/health", InstallKind: "git", Deps: []string{"pip", "install", "-r", "requirements.txt"}, Start: []string{"python3", "app.py"}},
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
	spec    ExternalGatewaySpec
	gateway *LocalGateway
}

func NewExternalGateway(spec ExternalGatewaySpec) *ExternalGateway {
	store, _ := config.LoadCredentials()
	key := store.Token(spec.Name)
	if key == "" {
		key = "voie-local"
	}
	gateway := NewLocalGateway(LocalGatewaySpec{
		Name:       spec.Name,
		Binary:     spec.Start[0],
		Args:       spec.Start[1:],
		Dir:        gatewayDir(spec),
		BaseURL:    "http://127.0.0.1:" + itoa(spec.Port),
		HealthPath: spec.HealthPath,
		KeyEnv:     "VOIE_" + spec.Name + "_KEY",
		Port:       spec.Port,
	})
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
		Ensure:         gateway.Ensure,
	}
	return &ExternalGateway{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), spec: spec, gateway: gateway}
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

// ExternalGatewayDir returns the install directory for a gateway.
func ExternalGatewayDir(name string) string {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return ""
	}
	return gatewayDir(spec)
}

// ExternalGatewayStatus reports whether a gateway has been installed.
func ExternalGatewayStatus(name string) (installed bool, installCommand, startCommand string) {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return false, "", ""
	}
	dir := gatewayDir(spec)
	_, err := os.Stat(dir)
	command := ""
	if len(spec.Start) > 0 {
		command = strings.Join(spec.Start, " ") + "  (in " + dir + ")"
	}
	return err == nil, NewExternalGateway(spec).InstallCommand(), command
}

// StartExternalGateway starts a gateway on demand and reports whether it became healthy.
func StartExternalGateway(name string) error {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return errUnknownProvider
	}
	return NewExternalGateway(spec).gateway.Ensure(context.Background())
}
