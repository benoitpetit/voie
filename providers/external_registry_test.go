package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExternalGatewaysAreRegistered(t *testing.T) {
	registry := NewRegistry()
	for _, spec := range ExternalGateways {
		if registry.Get(spec.Name) == nil {
			t.Errorf("gateway %q is not registered", spec.Name)
		}
	}
}

func TestExternalGatewayHiddenWithoutInstall(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, spec := range ExternalGateways {
		if len(spec.Install) > 0 {
			continue // global installs are detected by binary presence
		}
		if NewExternalGateway(spec).GetInfo().Working {
			t.Errorf("%s Working = true without an install directory", spec.Name)
		}
	}
}

func TestFetchGatewayModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"opencode/big-pickle"},{"id":"opencode/hy4-flash-free"}]}`)
	}))

	models := fetchGatewayModels("groq", server.URL, "test-key")
	if len(models) != 2 {
		t.Fatalf("models = %v, want 2", models)
	}
	for alias, upstream := range models {
		if !strings.HasPrefix(alias, "gq-") {
			t.Errorf("alias %q is not provider-prefixed", alias)
		}
		if upstream == "" {
			t.Errorf("empty upstream for %q", alias)
		}
	}

	// Simulate a fresh process with the gateway stopped: the persisted list
	// must still be returned.
	gatewayModelsMu.Lock()
	delete(gatewayModelCaches, "groq")
	gatewayModelsMu.Unlock()
	server.Close()
	if cached := fetchGatewayModels("groq", server.URL, "test-key"); len(cached) != 2 {
		t.Errorf("persisted models = %v, want 2", cached)
	}
}

func TestCodeArtsGatewaySeparateHealthAndKey(t *testing.T) {
	spec, ok := ExternalGatewayByName("codearts2api")
	if !ok {
		t.Fatal("missing codearts2api")
	}
	if spec.Port != 8326 || spec.HealthPort != 8080 {
		t.Errorf("port/health = %d/%d, want 8326/8080", spec.Port, spec.HealthPort)
	}
	if spec.KeyFile == "" {
		t.Error("no generated key file configured")
	}
}

func TestExternalGatewayAccountRequirements(t *testing.T) {
	for _, spec := range ExternalGateways {
		account, ok := ExternalGatewayAccount(spec.Name)
		if !ok {
			t.Errorf("no account info for %s", spec.Name)
			continue
		}
		if account.Required == "" || account.Kind == "" || account.URL == "" || account.Key == "" {
			t.Errorf("incomplete account info for %s: %+v", spec.Name, account)
		}
	}
	codearts, _ := ExternalGatewayAccount("codearts2api")
	if codearts.Required != "none" {
		t.Errorf("codearts2api should have a zero-account anonymous path, got %q", codearts.Required)
	}
}

func TestExternalGatewaySpecsAreCoherent(t *testing.T) {
	ports := make(map[int]string)
	for _, spec := range ExternalGateways {
		if len(spec.Start) == 0 {
			t.Errorf("%s has no start command", spec.Name)
		}
		if spec.SetupHint == "" {
			t.Errorf("%s has no account setup hint", spec.Name)
		}
		if !strings.HasPrefix(spec.HealthPath, "/health") && spec.HealthPath != "/v1/models" && spec.HealthPath != "/api/tags" && spec.HealthPath != "/api/ping" {
			t.Errorf("%s health path %q is not a documented probe", spec.Name, spec.HealthPath)
		}
		if previous, ok := ports[spec.Port]; ok {
			t.Errorf("port %d is shared by %s and %s", spec.Port, previous, spec.Name)
		}
		ports[spec.Port] = spec.Name
	}
}

func TestExternalGatewayCorrectedCommands(t *testing.T) {
	omni, _ := ExternalGatewayByName("omniroute")
	if got := strings.Join(omni.Start, " "); got != "omniroute" {
		t.Errorf("omniroute start = %q", got)
	}
	if omni.HealthPath != "/healthz" {
		t.Errorf("omniroute health = %q", omni.HealthPath)
	}
	cli, _ := ExternalGatewayByName("cli2api")
	if got := strings.Join(cli.Deps[0], " "); got != "go build -o gateway ./cmd/server" {
		t.Errorf("cli2api build = %q", got)
	}
	workbuddy, _ := ExternalGatewayByName("workbuddy2api")
	if got := strings.Join(workbuddy.Start, " "); got != "python -m codebuddy_proxy" {
		t.Errorf("workbuddy start = %q", got)
	}
}

func TestExternalGatewayInstallCommand(t *testing.T) {
	spec, ok := ExternalGatewayByName("omniroute")
	if !ok {
		t.Fatal("omniroute spec missing")
	}
	command := NewExternalGateway(spec).InstallCommand()
	if command == "" {
		t.Fatal("install command is empty")
	}
}
