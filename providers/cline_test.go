package providers

import (
	"testing"
)

func TestClineConfig(t *testing.T) {
	t.Setenv("VOIE_CLINE_KEY", "voie-local")
	provider := NewCline()
	if info := provider.GetInfo(); info.Name != "cline" || info.DefaultModel != "cline-flash" {
		t.Fatalf("GetInfo = %+v", info)
	}
	if got := provider.cfg.ModelIDs["cline-flash"]; got != "cline-free/nemotron-3.5-super-vl" {
		t.Errorf("cline-flash maps to %q", got)
	}
	if got := provider.cfg.CompletionsURL; got != "http://127.0.0.1:8092/v1/chat/completions" {
		t.Errorf("CompletionsURL = %q", got)
	}
}

func TestClineWorkingRequiresCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("VOIE_CLINE_KEY", "voie-local")
	if NewCline().GetInfo().Working {
		t.Error("Working = true without the cline executable")
	}
}

func TestClineBridgeSpawnArgs(t *testing.T) {
	spec := clineGatewaySpec()
	if len(spec.Args) == 0 || spec.Args[0] != "cline-bridge" {
		t.Errorf("spawn args = %v, want cline-bridge", spec.Args)
	}
	if spec.Port != 8092 {
		t.Errorf("port = %d, want 8092", spec.Port)
	}
}
