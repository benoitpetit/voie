package providers

import (
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

func TestExternalGatewaySpecsAreCoherent(t *testing.T) {
	ports := make(map[int]string)
	for _, spec := range ExternalGateways {
		if len(spec.Start) == 0 {
			t.Errorf("%s has no start command", spec.Name)
		}
		if spec.SetupHint == "" {
			t.Errorf("%s has no account setup hint", spec.Name)
		}
		if !strings.HasPrefix(spec.HealthPath, "/health") && spec.HealthPath != "/v1/models" {
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
