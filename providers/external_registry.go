package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

// ExternalGatewaySpec describes a third-party loopback gateway that exposes
// one or more coding-agent services as OpenAI-compatible endpoints. These are
// opt-in and user-installed: voie never bundles or operates them.
type ExternalGatewaySpec struct {
	Name            string
	Label           string
	Repo            string
	Port            int
	Alias           string
	Upstream        string
	HealthPath      string
	HealthPort      int        // separate health port when admin and /v1 listeners differ
	KeyFile         string     // generated gateway key file (absolute or ~-prefixed)
	Install         []string   // explicit install; empty means git clone Repo into the gateway dir
	Deps            [][]string // commands run in the gateway dir after install
	Start           []string   // command that launches the gateway on its loopback port
	SetupHint       string     // account configuration required before inference
	AccountRequired string     // "none", "upstream", "dashboard+upstream"
	AccountKind     string     // which account exactly
	AccountURL      string     // where to create or link it
	KeyRequired     string     // local gateway key policy
}

// ExternalGateways are ranked by observed activity in Nov 2026. Install and
// start commands were verified against each repository in Nov 2026. All are
// BYO-account: a bare start only exposes the health probe, not inference.
var ExternalGateways = []ExternalGatewaySpec{
	{
		Name: "omniroute", Label: "OmniRoute", Repo: "https://github.com/cheaper-inference/OmniRoute",
		Port: 20128, Alias: "omni-free", Upstream: "openrouter/free", HealthPath: "/healthz",
		Install:         []string{"npm", "install", "-g", "omniroute@latest"},
		SetupHint:       "connect a provider in the OmniRoute dashboard and create a gateway API key",
		AccountRequired: "dashboard+upstream",
		AccountKind:     "OmniRoute dashboard password + at least one upstream provider (keyless NOAUTH providers exist)",
		AccountURL:      "http://localhost:20128",
		KeyRequired:     "optional locally (REQUIRE_API_KEY off by default)",
		Start:           []string{"omniroute"},
	},
	{
		Name: "qoder2api", Label: "Qoder2API Hub", Repo: "https://github.com/shuishuipingan/qoder2api-hub",
		Port: 8790, Alias: "qoder2-free", Upstream: "auto", HealthPath: "/health",
		Deps:            [][]string{{"bash", "-c", "chmod +x *.sh"}},
		SetupHint:       "open the dashboard (default password admin) and add your Qoder account",
		AccountRequired: "upstream",
		AccountKind:     "Qoder CN/intl account (free daily check-in credits)",
		AccountURL:      "https://qoder.com/account/integrations",
		KeyRequired:     "optional on localhost when no key is set",
		Start:           []string{"bash", "start-qoder-proxy.sh"},
	},
	{
		Name: "windsurfapi", Label: "WindsurfAPI", Repo: "https://github.com/dwgx/WindsurfAPI",
		Port: 3003, Alias: "devin2-free", Upstream: "swe-2-flash-medium", HealthPath: "/health",
		Deps:            [][]string{{"bash", "setup.sh"}},
		SetupHint:       "log in with a Windsurf/Devin account and set API_KEY in .env",
		AccountRequired: "upstream",
		AccountKind:     "Windsurf/Devin account (free models available)",
		AccountURL:      "https://windsurf.com/show-auth-token",
		KeyRequired:     "required: API_KEY or WINDSURFAPI_ALLOW_UNAUTHENTICATED=1 on loopback",
		Start:           []string{"npm", "start"},
	},
	{
		Name: "copilot-api", Label: "Copilot API", Repo: "https://github.com/caozhiyuan/copilot-api",
		Port: 4141, Alias: "copilot2-free", Upstream: "auto", HealthPath: "/v1/models",
		Install:         []string{"npm", "install", "-g", "@jeffreycao/copilot-api@latest"},
		SetupHint:       "run the gateway once and complete provider setup (Copilot/Codex/xAI login)",
		AccountRequired: "upstream",
		AccountKind:     "GitHub Copilot Free, Codex or xAI account",
		AccountURL:      "https://github.com/settings/copilot",
		KeyRequired:     "optional on loopback",
		Start:           []string{"copilot-api", "start"},
	},
	{
		Name: "cursor-api-proxy", Label: "Cursor ACP Proxy", Repo: "https://github.com/anyrobert/cursor-api-proxy",
		Port: 8765, Alias: "cursor2-free", Upstream: "auto", HealthPath: "/healthz",
		Install:         []string{"npm", "install", "-g", "cursor-api-proxy@latest"},
		SetupHint:       "install the Cursor agent CLI and run `agent login` (or set CURSOR_API_KEY)",
		AccountRequired: "upstream",
		AccountKind:     "Cursor Free/Hobby account or dashboard integration key",
		AccountURL:      "https://cursor.com/dashboard/integrations",
		KeyRequired:     "optional",
		Start:           []string{"cursor-api-proxy"},
	},
	{
		Name: "codearts2api", Label: "CodeArts2API", Repo: "https://github.com/zhuweiyou/codearts2api",
		Port: 8326, Alias: "codearts-free", Upstream: "opencode/mimo-v2.6-flash-free", HealthPath: "/healthz",
		HealthPort: 8080, KeyFile: "~/.codearts2api/openai-gateway/api-key",
		Deps:            [][]string{{"npm", "install", "-g", "pnpm@11"}, {"pnpm", "install"}, {"pnpm", "run", "build"}},
		SetupHint:       "no account needed: the OpenCode anonymous channel serves mimo-v2.6-flash-free; key read from ~/.codearts2api/openai-gateway/api-key",
		AccountRequired: "none",
		AccountKind:     "auto-added OpenCode anonymous channel (api_key=public, mimo-v2.6-flash-free) works with no account",
		AccountURL:      "https://github.com/zhuweiyou/codearts2api",
		KeyRequired:     "required: generated gateway key at <home>/openai-gateway/api-key",
		Start:           []string{"pnpm", "start"},
	},
	{
		Name: "cli2api", Label: "CLI2API", Repo: "https://github.com/caigee-cmd/cli2api",
		Port: 3010, Alias: "cli2-free", Upstream: "qoder", HealthPath: "/health",
		Deps:            [][]string{{"go", "build", "-o", "gateway", "./cmd/server"}},
		SetupHint:       "save the admin key, add an upstream account, then create a client key",
		AccountRequired: "upstream",
		AccountKind:     "Qoder / WorkBuddy / Trae account",
		AccountURL:      "http://127.0.0.1:3010",
		KeyRequired:     "required: generated admin or client key",
		Start:           []string{"./gateway"},
	},
	{
		Name: "workbuddy2api", Label: "WorkBuddy2API", Repo: "https://github.com/hawklithm/workbuddy2api",
		Port: 8787, Alias: "wb2-free", Upstream: "auto", HealthPath: "/health",
		Install:         []string{"pip", "install", "workbuddy2api"},
		SetupHint:       "link a CodeBuddy account at /admin or with --login",
		AccountRequired: "upstream",
		AccountKind:     "CodeBuddy (CN) or WorkBuddy (Global) account",
		AccountURL:      "http://127.0.0.1:8787/admin",
		KeyRequired:     "none: placeholder accepted",
		Start:           []string{"python", "-m", "codebuddy_proxy"},
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

func gatewayTokenSupplier(spec ExternalGatewaySpec) func() string {
	return func() string {
		store, _ := config.LoadCredentials()
		if value := store.Token(spec.Name); value != "" {
			return value
		}
		if spec.KeyFile != "" {
			path := spec.KeyFile
			if strings.HasPrefix(path, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
				}
			}
			if data, err := os.ReadFile(path); err == nil {
				if value := strings.TrimSpace(string(data)); value != "" {
					return value
				}
			}
		}
		return "voie-local"
	}
}

// ExternalGateway is a normal provider pointed at a user-installed gateway.
type ExternalGateway struct {
	*openAIKeylessProvider
	spec    ExternalGatewaySpec
	gateway *LocalGateway
}

func NewExternalGateway(spec ExternalGatewaySpec) *ExternalGateway {
	healthPort := spec.Port
	if spec.HealthPort > 0 {
		healthPort = spec.HealthPort
	}
	tokenSupplier := gatewayTokenSupplier(spec)
	gateway := NewLocalGateway(LocalGatewaySpec{
		Name:       spec.Name,
		Binary:     spec.Start[0],
		Args:       spec.Start[1:],
		Dir:        gatewayDir(spec),
		BaseURL:    "http://127.0.0.1:" + itoa(healthPort),
		HealthPath: spec.HealthPath,
		KeyEnv:     "VOIE_" + spec.Name + "_KEY",
		Port:       spec.Port,
	})
	cfg := openAIKeylessConfig{
		Name:           spec.Name,
		Label:          spec.Label,
		URL:            "http://127.0.0.1:" + itoa(healthPort) + spec.HealthPath,
		CompletionsURL: "http://127.0.0.1:" + itoa(spec.Port) + "/v1/chat/completions",
		DefaultModel:   spec.Alias,
		ModelIDs:       map[string]string{spec.Alias: spec.Upstream},
		Description:    spec.Label + " through a user-installed reverse-engineered gateway (ToS risk)",
		SupportsStream: true,
		TokenSupplier:  tokenSupplier,
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
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	// A cloned gateway is only usable when its start command exists too.
	binary := p.spec.Start[0]
	if strings.HasPrefix(binary, "./") {
		_, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(binary, "./")))
		return err == nil
	}
	_, err := exec.LookPath(binary)
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

// GatewayAccount describes what a user must set up before a gateway serves
// real inference.
type GatewayAccount struct {
	Required string
	Kind     string
	URL      string
	Key      string
}

// ExternalGatewayAccount returns the account requirements for a gateway.
func ExternalGatewayAccount(name string) (GatewayAccount, bool) {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return GatewayAccount{}, false
	}
	return GatewayAccount{Required: spec.AccountRequired, Kind: spec.AccountKind, URL: spec.AccountURL, Key: spec.KeyRequired}, true
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
	gateway := NewExternalGateway(spec).gateway
	if !gateway.Available() {
		return fmt.Errorf("%s start command %q is unavailable; run `voie install %s` and use a Node 20-24 toolchain if needed", name, spec.Start[0], name)
	}
	return gateway.Ensure(context.Background())
}
