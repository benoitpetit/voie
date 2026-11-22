package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runConnect(t *testing.T, args []string, stdin string, check func(string, string) error) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := newConnectCommand(check)
	command.SetIn(strings.NewReader(stdin))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func TestConnectSavesTokenFromFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stdout, _, err := runConnect(t, []string{"openrouter", "--token", "sk-or-v1-test"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "sk-or-v1-test") {
		t.Errorf("stdout leaked the token: %q", stdout)
	}
	if !strings.Contains(stdout, "openrouter") {
		t.Errorf("stdout does not name the provider: %q", stdout)
	}
}

func TestConnectReadsTokenFromStdin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _, err := runConnect(t, []string{"orcarouter"}, "sk-orca-stdin\n", nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConnectListShowsState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, _, err := runConnect(t, []string{"openrouter", "--token", "sk-or-v1-test"}, "", nil); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"openrouter", "connected", "orcarouter", "not connected"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--list output missing %q:\n%s", want, stdout)
		}
	}
}

func TestConnectRejectsUnknownProvider(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _, err := runConnect(t, []string{"nope", "--token", "x"}, "", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
}

func TestDisconnectRemovesToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, _, err := runConnect(t, []string{"openrouter", "--token", "sk-or-v1-test"}, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runConnect(t, []string{"disconnect", "openrouter"}, "", nil); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "not connected") {
		t.Errorf("token was not removed:\n%s", stdout)
	}
}

func TestConnectOpencodeUsesLocalKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var gotProvider, gotToken string
	check := func(provider, token string) error {
		gotProvider, gotToken = provider, token
		return nil
	}
	if _, _, err := runConnect(t, []string{"opencode-zen", "--token", "voie-local"}, "", check); err != nil {
		t.Fatal(err)
	}
	if gotProvider != "opencode-zen" || gotToken != "voie-local" {
		t.Errorf("check called with (%q, %q)", gotProvider, gotToken)
	}
}
