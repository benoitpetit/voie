package providers

import "testing"

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
		if NewExternalGateway(spec).GetInfo().Working {
			t.Errorf("%s Working = true without an install directory", spec.Name)
		}
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
