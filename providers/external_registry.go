package providers

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/benoitpetit/voie/internal/app"
)

// ExternalGatewaySpec describes a third-party loopback gateway that exposes
// one or more coding-agent services as OpenAI-compatible endpoints. These are
// opt-in and user-installed: voie never bundles or operates them.
type ExternalGatewaySpec struct {
	Name       string
	Label      string
	Repo       string
	Port       int
	Alias      string
	Upstream   string
	HealthPath string
	Install    []string   // explicit install; empty means git clone Repo into the gateway dir
	Deps       [][]string // commands run in the gateway dir after install
	Start      []string   // command that launches the gateway on its loopback port
	SetupHint  string     // account/key configuration required before inference
}

// ExternalGateways are ranked by observed activity in Nov 2026. Install and
// start commands were verified against each repository in Nov 2026. All are
// BYO-account: a bare start only exposes the health probe, not inference.
var ExternalGateways = []ExternalGatewaySpec{
	{
		Name: "omniroute", Label: "OmniRoute", Repo: "https://github.com/cheaper-inference/OmniRoute",
		Port: 20128, Alias: "omni-free", Upstream: "openrouter/free", HealthPath: "/healthz",
		Install:   []string{"npm", "install", "-g", "omniroute@latest"},
		SetupHint: "connect a provider in the OmniRoute dashboard and create a gateway API key",
		Start:     []string{"omniroute"},
	},
	{
		Name: "qoder2api", Label: "Qoder2API Hub", Repo: "https://github.com/shuishuipingan/qoder2api-hub",
		Port: 8790, Alias: "qoder2-free", Upstream: "auto", HealthPath: "/health",
		Deps:      [][]string{{"bash", "-c", "chmod +x *.sh"}},
		SetupHint: "open the dashboard (default password admin) and add your Qoder account",
		Start:     []string{"bash", "start-qoder-proxy.sh"},
	},
	{
		Name: "windsurfapi", Label: "WindsurfAPI", Repo: "https://github.com/dwgx/WindsurfAPI",
		Port: 3003, Alias: "devin2-free", Upstream: "swe-2-flash-medium", HealthPath: "/health",
		Deps:      [][]string{{"bash", "setup.sh"}},
		SetupHint: "log in with a Windsurf/Devin account and set API_KEY in .env",
		Start:     []string{"npm", "start"},
	},
	{
		Name: "copilot-api", Label: "Copilot API", Repo: "https://github.com/caozhiyuan/copilot-api",
		Port: 4141, Alias: "copilot2-free", Upstream: "auto", HealthPath: "/v1/models",
		Install:   []string{"npm", "install", "-g", "@jeffreycao/copilot-api@latest"},
		SetupHint: "run the gateway once and complete provider setup (Copilot/Codex/xAI login)",
		Start:     []string{"copilot-api", "start"},
	},
	{
		Name: "cursor-api-proxy", Label: "Cursor ACP Proxy", Repo: "https://github.com/anyrobert/cursor-api-proxy",
		Port: 8765, Alias: "cursor2-free", Upstream: "auto", HealthPath: "/healthz",
		Install:   []string{"npm", "install", "-g", "cursor-api-proxy@latest"},
		SetupHint: "install the Cursor agent CLI and run `agent login` (or set CURSOR_API_KEY)",
		Start:     []string{"cursor-api-proxy"},
	},
	{
		Name: "codearts2api", Label: "CodeArts2API", Repo: "https://github.com/zhuweiyou/codearts2api",
		Port: 8088, Alias: "codearts-free", Upstream: "minimax", HealthPath: "/healthz",
		Deps:      [][]string{{"corepack", "enable"}, {"pnpm", "install"}, {"pnpm", "run", "build"}},
		SetupHint: "add a channel/account in /admin and save the generated gateway key",
		Start:     []string{"pnpm", "start"},
	},
	{
		Name: "cli2api", Label: "CLI2API", Repo: "https://github.com/caigee-cmd/cli2api",
		Port: 3010, Alias: "cli2-free", Upstream: "qoder", HealthPath: "/health",
		Deps:      [][]string{{"go", "build", "-o", "gateway", "./cmd/server"}},
		SetupHint: "save the admin key, add an upstream account, then create a client key",
		Start:     []string{"./gateway"},
	},
	{
		Name: "workbuddy2api", Label: "WorkBuddy2API", Repo: "https://github.com/hawklithm/workbuddy2api",
		Port: 8787, Alias: "wb2-free", Upstream: "auto", HealthPath: "/health",
		Install:   []string{"pip", "install", "workbuddy2api"},
		SetupHint: "link a CodeBuddy account at /admin or with --login",
		Start:     []string{"python", "-m", "codebuddy_proxy"},
	},
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
		TokenSupplier:  gateway.localKey,
		Ensure:         gateway.Ensure,
	}
	return &ExternalGateway{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), spec: spec, gateway: gateway}
}

func (p *ExternalGateway) working() bool {
	if len(p.spec.Install) > 0 {
		// Globally installed tools have no gateway directory.
		_, err := exec.LookPath(p.spec.Start[0])
		return err == nil
	}
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
	if len(p.spec.Install) > 0 {
		return strings.Join(p.spec.Install, " ")
	}
	return "git clone " + p.spec.Repo + " " + gatewayDir(p.spec)
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
	installed = NewExternalGateway(spec).working()
	command := ""
	if len(spec.Start) > 0 {
		command = strings.Join(spec.Start, " ")
	}
	return installed, NewExternalGateway(spec).InstallCommand(), command
}

// ExternalGatewaySetupHint returns the account configuration a gateway needs
// before it can serve inference.
func ExternalGatewaySetupHint(name string) string {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return ""
	}
	return spec.SetupHint
}

// StartExternalGateway starts a gateway on demand and reports whether it became healthy.
func StartExternalGateway(name string) error {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return errUnknownProvider
	}
	return NewExternalGateway(spec).gateway.Ensure(context.Background())
}
