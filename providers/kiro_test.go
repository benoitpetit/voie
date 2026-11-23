package providers

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestKiroConfig(t *testing.T) {
	t.Setenv("VOIE_KIRO_KEY", "voie-local")
	provider := NewKiro()
	if info := provider.GetInfo(); info.Name != "kiro" || info.DefaultModel != "kiro-auto" {
		t.Fatalf("GetInfo = %+v", info)
	}
	if got := provider.cfg.ModelIDs["kiro-sonnet"]; got != "claude-sonnet-4.5" {
		t.Errorf("kiro-sonnet maps to %q", got)
	}
}

func TestKiroGeneratesEnv(t *testing.T) {
	envDir := t.TempDir()
	t.Setenv("KIRO_GATEWAY_DIR", envDir)
	spec := kiroGatewaySpec()
	body := string(spec.ConfigBody)
	for _, want := range []string{"SERVER_HOST=127.0.0.1", "SERVER_PORT=8099", "KIRO_ACP_ENGINE=v2", "MCP_DISCOVERY=user", "ACP_TRUST_TOOLS=false", "KIRO_GATEWAY_API_KEY=voie-local"} {
		if !strings.Contains(body, want) {
			t.Errorf(".env missing %q:\n%s", want, body)
		}
	}
	if spec.ConfigPath != filepath.Join(envDir, ".env") {
		t.Errorf("ConfigPath = %q", spec.ConfigPath)
	}
}

func TestKiroWorkingRequiresKiroCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("VOIE_KIRO_KEY", "voie-local")
	if NewKiro().GetInfo().Working {
		t.Error("Working = true without kiro-cli")
	}
}
