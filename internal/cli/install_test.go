package cli

import "testing"

func TestInstallNextIsNoneForReadyRows(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status, account string
		want                        string
	}{
		{"codearts2api", "gateway", "installed", "none", "NONE"},
		{"freellmpool", "gateway", "installed", "none", "NONE"},
		{"ollama", "gateway", "installed", "none", "NONE"},
		{"lmstudio", "gateway", "not installed", "none", "voie install lmstudio"},
		{"qoder2api", "gateway", "installed", "upstream", "voie connect qoder2api"},
		{"openrouter", "api", "connected", "-", "NONE"},
		{"groq", "api", "not connected", "-", "voie connect groq"},
	} {
		if got := installNext(tc.kind, tc.status, tc.account, tc.name); got != tc.want {
			t.Errorf("installNext(%q,%q,%q,%q) = %q, want %q", tc.kind, tc.status, tc.account, tc.name, got, tc.want)
		}
	}
}

func TestInstallNextReconnectsOnError(t *testing.T) {
	if got := installNext("api", "error", "-", "openrouter"); got != "voie connect openrouter" {
		t.Fatalf("api error NEXT = %q", got)
	}
	if got := installNext("cli", "error", "-", "codebuddy"); got != "voie connect codebuddy" {
		t.Fatalf("cli error NEXT = %q", got)
	}
	if got := installNext("gateway", "error", "upstream", "omniroute"); got != "voie connect omniroute" {
		t.Fatalf("gateway error NEXT = %q", got)
	}
}
