package providers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/install"
)

// ACPProviderSpec describes a coding-agent CLI bridged over ACP.
type ACPProviderSpec struct {
	Name       string
	Label      string
	Binary     string
	AgentArgs  []string // ACP invocation; some CLIs use `acp`, others `--acp`
	AuthCheck  []string // command whose exit status says whether the CLI is logged in
	AuthLogin  []string // interactive login command launched by `voie connect`
	Alias      string
	Model      string
	Port       int
	FeedURL    string
	NpmPackage string
}

// ACPProviders are opt-in bridges over the user's own agent CLI login.
var ACPProviders = []ACPProviderSpec{
	{Name: "codebuddy", Label: "CodeBuddy", Binary: "codebuddy", AgentArgs: []string{"--acp"}, Alias: "cb-auto", Model: "auto", Port: 8093},
	{Name: "minimax-code", Label: "MiniMax Code", Binary: "mcode", AgentArgs: []string{"acp"}, Alias: "mm-auto", Model: "auto", Port: 8094},
	{Name: "devin", Label: "Devin", Binary: "devin", AgentArgs: []string{"acp"}, AuthCheck: []string{"auth", "status"}, AuthLogin: []string{"auth", "login"}, Alias: "dv-auto", Model: "auto", Port: 8095},
	{Name: "qoder", Label: "Qoder", Binary: "qoder", AgentArgs: []string{"--acp"}, Alias: "qd-auto", Model: "auto", Port: 8096},
	{Name: "copilot-cli", Label: "GitHub Copilot CLI", Binary: "copilot", AgentArgs: []string{"--acp"}, Alias: "cp-auto", Model: "auto", Port: 8097},
	{Name: "cursor", Label: "Cursor", Binary: "agent", AgentArgs: []string{"acp"}, Alias: "cu-auto", Model: "auto", Port: 8098},
}

func ACPProviderSpecByName(name string) (ACPProviderSpec, bool) {
	for _, spec := range ACPProviders {
		if spec.Name == name {
			return spec, true
		}
	}
	return ACPProviderSpec{}, false
}

// ACPProvider exposes one ACP bridge as a normal voie provider.
type ACPProvider struct {
	*openAIKeylessProvider
	spec    ACPProviderSpec
	gateway *LocalGateway
}

func NewACPProvider(spec ACPProviderSpec) *ACPProvider {
	executable, err := os.Executable()
	if err != nil {
		executable = "voie"
	}
	gateway := NewLocalGateway(LocalGatewaySpec{
		Name:       spec.Name,
		Binary:     executable,
		Args:       []string{"acp-bridge", "--provider", spec.Name},
		BaseURL:    "http://127.0.0.1:" + itoa(spec.Port),
		HealthPath: "/health",
		KeyEnv:     "VOIE_" + spec.Name + "_KEY",
		Port:       spec.Port,
	})
	store, _ := config.LoadCredentials()
	key := store.Token(spec.Name)
	if key == "" {
		key = "voie-local"
	}
	cfg := openAIKeylessConfig{
		Name:           spec.Name,
		Label:          spec.Label,
		URL:            "http://127.0.0.1:" + itoa(spec.Port) + "/health",
		CompletionsURL: "http://127.0.0.1:" + itoa(spec.Port) + "/v1/chat/completions",
		DefaultModel:   spec.Alias,
		ModelIDs:       map[string]string{spec.Alias: spec.Model},
		Description:    spec.Label + " free models through a local ACP bridge",
		SupportsStream: true,
		Headers:        map[string]string{"Authorization": "Bearer " + key},
		Ensure:         gateway.Ensure,
	}
	return &ACPProvider{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), spec: spec, gateway: gateway}
}

func (p *ACPProvider) working() bool {
	available := install.BinaryPath(p.spec.Binary) != ""
	if !available {
		_, err := exec.LookPath(p.spec.Binary)
		if err != nil {
			return false
		}
	}
	// The bridge is started lazily on the first request; requiring a live
	// loopback listener here would hide the model before that call can start it.
	return true
}

// StartACPBridge starts (or reuses) the bridge and reports whether it became
// healthy. `voie connect` uses this so a provider is visible right after it is
// connected.
func StartACPBridge(name string) error {
	spec, ok := ACPProviderSpecByName(name)
	if !ok {
		return errUnknownProvider
	}
	return NewACPProvider(spec).gateway.Ensure(context.Background())
}

var errUnknownProvider = errors.New("unknown provider")

func (p *ACPProvider) SupportsModel(model string) bool {
	return p.working() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *ACPProvider) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	info.Working = p.working()
	return info
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
