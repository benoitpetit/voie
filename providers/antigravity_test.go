package providers

import (
	"strings"
	"testing"
)

func TestAntigravityConfig(t *testing.T) {
	t.Setenv("VOIE_ANTIGRAVITY_KEY", "voie-local")
	provider := NewAntigravity()
	info := provider.GetInfo()
	if info.Name != "antigravity" || info.DefaultModel != "ag-flash" {
		t.Fatalf("GetInfo = %+v", info)
	}
	for _, want := range []string{"ag-flash", "ag-sonnet"} {
		if _, ok := provider.cfg.ModelIDs[want]; !ok {
			t.Errorf("model map missing %q", want)
		}
	}
	if got := provider.cfg.ModelIDs["ag-sonnet"]; got != "claude-sonnet-4-6" {
		t.Errorf("ag-sonnet maps to %q", got)
	}
}

func TestAntigravityGeneratesConfig(t *testing.T) {
	spec := antigravityGatewaySpec()
	body := string(spec.ConfigBody)
	for _, want := range []string{"host: \"127.0.0.1\"", "port: 8317", "api-keys:", "auth-dir:", "tos-pause-window:"} {
		if !strings.Contains(body, want) {
			t.Errorf("config missing %q:\n%s", want, body)
		}
	}
}

func TestAntigravityWorkingRequiresBinary(t *testing.T) {
	t.Setenv("VOIE_ANTIGRAVITY_KEY", "voie-local")
	if NewAntigravity().GetInfo().Working {
		t.Error("Working = true without the CLIProxyAPI binary")
	}
}
