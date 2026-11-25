package cli

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

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

func TestInstallListHasNoBlankAccountColumn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	command := newInstallCommand()
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"openrouter":  "token",
		"groq":        "token",
		"vlm-run":     "none",
		"codebuddy":   "subscription",
		"cline":       "subscription",
		"freellmpool": "none",
		"omniroute":   "dashboard+upstream",
	}
	found := map[string]bool{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		columns := regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(line), -1)
		if len(columns) < 5 || columns[0] == "NAME" {
			continue
		}
		if columns[3] == "-" {
			t.Errorf("row %s has no ACCOUNT type", columns[0])
		}
		if expected, ok := want[columns[0]]; ok && columns[3] != expected {
			t.Errorf("%s ACCOUNT = %q, want %q", columns[0], columns[3], expected)
		}
		found[columns[0]] = true
	}
	for name := range want {
		if !found[name] {
			t.Errorf("%s missing from install list", name)
		}
	}
}
