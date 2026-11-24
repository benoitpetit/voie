package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/providers"
	"github.com/spf13/cobra"
)

func runConnect(t *testing.T, args []string, stdin string, check func(string, string) error) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if check == nil {
		check = func(string, string) error { return nil }
	}
	command := newConnectCommand(check)
	command.SetIn(strings.NewReader(stdin))
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func TestInternalBridgeIsHiddenFromHelp(t *testing.T) {
	command := newClineBridgeCommand()
	if !command.Hidden {
		t.Fatal("cline-bridge must be hidden from top-level help")
	}
}

func TestConnectCancelFromMenu(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stdout, stderr, err := runConnect(t, nil, "0\n", nil)
	if err != nil {
		t.Fatalf("cancel returned an error: %v (%s)", err, stderr)
	}
	if !strings.Contains(stdout, "Canceled.") {
		t.Errorf("stdout = %q, want a cancel confirmation", stdout)
	}
}

func TestKeysCommandShowsAcquisitionSteps(t *testing.T) {
	var stdout, stderr bytes.Buffer
	command := newKeysCommand()
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"cloudflare-ai"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	output := stdout.String() + stderr.String()
	for _, want := range []string{"Account ID", "Workers AI", "API token"} {
		if !strings.Contains(output, want) {
			t.Errorf("keys output missing %q:\n%s", want, output)
		}
	}
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

func TestConnectListIncludesThirdPartyGateways(t *testing.T) {
	_, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "TYPE", "STATE", "token", "keyless gateway", "omniroute", "not installed", "qoder2api"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--list missing %q:\n%s", want, stdout)
		}
	}
}

func TestConnectListShowsDefaultKeyForOpencode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stdout, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "opencode-zen") || !strings.Contains(stdout, "available (anonymous / local key)") {
		t.Errorf("--list does not show the default local key:\n%s", stdout)
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

func TestConnectCollectsAllProviderFields(t *testing.T) {
	fields := providers.ConnectFields("cloudflare-ai")
	if len(fields) != 2 || fields[0].Key != "cloudflare-ai" || fields[1].Key != providers.CloudflareAccountKey() {
		t.Fatalf("Cloudflare fields = %+v", fields)
	}
	for _, provider := range []string{"groq", "openrouter", "codebuddy"} {
		if got := providers.ConnectFields(provider); len(got) != 1 {
			t.Errorf("%s fields = %+v, want one token field", provider, got)
		}
	}
}

func TestConnectAcceptsExtraField(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _, err := runConnect(t, []string{
		"cloudflare-ai", "--token", "cf-token", "--field", providers.CloudflareAccountKey() + "=account-123",
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Raw(providers.CloudflareAccountKey()); got != "account-123" {
		t.Errorf("saved account = %q", got)
	}
}

func TestConnectInstallsGatewayToolchain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := connectInstaller
	defer func() { connectInstaller = previous }()
	var installed string
	connectInstaller = func(_ context.Context, provider string) (string, error) {
		installed = provider
		return "/tmp/tool", nil
	}
	if _, _, err := runConnect(t, []string{"cline", "--token", "voie-local"}, "", nil); err != nil {
		t.Fatal(err)
	}
	if installed != "cline" {
		t.Errorf("installer called with %q, want cline", installed)
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

func TestConnectDoesNotPersistRejectedToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _, err := runConnect(t, []string{"openrouter", "--token", "bad"}, "", func(string, string) error {
		return errors.New("rejected")
	})
	if err == nil {
		t.Fatal("rejected token was accepted")
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if token := store.Token("openrouter"); token != "" {
		t.Fatalf("rejected token was persisted: %q", token)
	}
}

func TestConnectListRejectsArgs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, _, err := runConnect(t, []string{"--list", "junk"}, "", nil)
	if err == nil {
		t.Fatal("--list with a positional argument was accepted")
	}
}

func TestInstallRejectsExtraArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	command := newInstallCommand()
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"cline", "extra"})
	if err := command.Execute(); err == nil {
		t.Fatal("extra install argument was accepted")
	}
}

func TestConnectRollsBackTokenOnInstallFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := connectInstaller
	connectInstaller = func(context.Context, string) (string, error) {
		return "", errors.New("install failed")
	}
	t.Cleanup(func() { connectInstaller = previous })

	stdout, _, err := runConnect(t, []string{"cline", "--token", "new-token"}, "", nil)
	if err == nil {
		t.Fatal("install failure was accepted")
	}
	if strings.Contains(stdout, "connected") {
		t.Fatalf("stdout = %q, want no connected confirmation", stdout)
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if token := store.Token("cline"); token != "" {
		t.Fatalf("token survived a failed connect: %q", token)
	}
}

func TestConnectRestoresPreviousTokenOnFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("cline", "old-token"); err != nil {
		t.Fatal(err)
	}
	previous := connectInstaller
	connectInstaller = func(context.Context, string) (string, error) {
		return "", errors.New("install failed")
	}
	t.Cleanup(func() { connectInstaller = previous })

	if _, _, err := runConnect(t, []string{"cline", "--token", "new-token"}, "", nil); err == nil {
		t.Fatal("install failure was accepted")
	}
	reloaded, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if token := reloaded.Token("cline"); token != "old-token" {
		t.Fatalf("token = %q, want the previous value restored", token)
	}
}

func TestConnectBridgeFailureKeepsTokenAndMarksState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous, previousInstaller := startClineBridge, connectInstaller
	startClineBridge = func(context.Context) error { return errors.New("bridge failed") }
	connectInstaller = func(context.Context, string) (string, error) { return "/usr/bin/cline", nil }
	t.Cleanup(func() { startClineBridge, connectInstaller = previous, previousInstaller })

	stdout, _, err := runConnect(t, []string{"cline", "--token", "valid-token"}, "", nil)
	if err == nil {
		t.Fatal("bridge failure was accepted")
	}
	if strings.Contains(stdout, "connected") {
		t.Fatalf("stdout = %q, want no connected confirmation", stdout)
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if store.Token("cline") != "valid-token" {
		t.Fatalf("valid token was discarded: %q", store.Token("cline"))
	}
	if store.State("cline") != "bridge error" {
		t.Fatalf("state = %q, want bridge error", store.State("cline"))
	}
	var list bytes.Buffer
	if err := runConnectList(&list, store); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.String(), "bridge error") {
		t.Fatalf("connect --list = %q, want bridge error", list.String())
	}
}

func TestConnectMarksGatewayConnected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previous := connectInstaller
	connectInstaller = func(context.Context, string) (string, error) { return "/usr/bin/agent", nil }
	t.Cleanup(func() { connectInstaller = previous })
	stdout, stderr, err := runConnect(t, []string{"qoder2api", "--token", "x"}, "", nil)
	if err != nil {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if store.State("qoder2api") != "connected" {
		t.Fatalf("gateway state = %q, want connected", store.State("qoder2api"))
	}
}

func TestConnectListShowsGatewayNeedsConnect(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "voie", "gateways", "qoder2api")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("qoder2api", "token"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "qoder2api") {
			if !strings.Contains(line, "installed — account: upstream") {
				t.Fatalf("gateway line = %q, want installed/account state", line)
			}
			return
		}
	}
	t.Fatal("qoder2api missing from connect --list")
}

func TestConnectPPLXProxyCapturesCookie(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	previousInstaller, previousCapture, previousStart := connectInstaller, capturePerplexityCookie, startExternalGateway
	previousGatewayInstaller := installExternalGatewayFn
	installExternalGatewayFn = func(*cobra.Command, providers.ExternalGatewaySpec) error { return nil }
	t.Cleanup(func() { installExternalGatewayFn = previousGatewayInstaller })
	connectInstaller = func(context.Context, string) (string, error) { return "/usr/bin/uvicorn", nil }
	capturePerplexityCookie = func(context.Context) (string, error) { return "session-token", nil }
	startExternalGateway = func(string) error { return nil }
	t.Cleanup(func() {
		connectInstaller, capturePerplexityCookie, startExternalGateway = previousInstaller, previousCapture, previousStart
	})
	if _, _, err := runConnect(t, []string{"pplx-proxy"}, "", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "voie", "gateways", "pplx-proxy", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PPLX_COOKIE=session-token") {
		t.Fatalf(".env = %q, want captured cookie", data)
	}
	if !strings.Contains(string(data), "PPLX_PROXY_API_KEY=voie-local") {
		t.Fatalf(".env = %q, want the loopback key voie sends", data)
	}
}

func TestInstallListIsCompact(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	command := newInstallCommand()
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(nil)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, unwanted := range []string{"git clone", "npm install -g", "SETUP REQUIRED", t.TempDir()} {
		if strings.Contains(output, unwanted) {
			t.Errorf("compact list contains %q:\n%s", unwanted, output)
		}
	}
	if strings.Contains(output, os.Getenv("XDG_CONFIG_HOME")) {
		t.Errorf("compact list contains absolute config path:\n%s", output)
	}
	for _, want := range []string{"NAME", "TYPE", "STATUS", "NEXT", "voie install qoder2api", "voie connect beatapi", "api"} {
		if !strings.Contains(output, want) {
			t.Errorf("compact list missing %q:\n%s", want, output)
		}
	}
}

func TestInstallListVerboseShowsCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	command := newInstallCommand()
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--verbose"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "git clone") {
		t.Fatalf("verbose list missing install commands:\n%s", stdout.String())
	}
}

func TestPreflightAcceptsGeneratedRelativeBinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "venv", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "venv", "bin", "pip"), []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := preflightInstallStep([]string{"venv/bin/pip", "install", "-r", "requirements.txt"}, dir); err != nil {
		t.Fatalf("generated binary rejected: %v", err)
	}
	if err := preflightInstallStep([]string{"definitely-missing-tool"}, dir); err == nil {
		t.Fatal("missing tool accepted")
	}
}

func TestDisconnectAsksBeforeRemovingGateway(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, command := providers.ExternalGatewayUninstall("qoder2api")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("qoder2api", "token"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetState("qoder2api", "connected"); err != nil {
		t.Fatal(err)
	}
	previousConfirm := confirmRemoveApp
	confirmRemoveApp = func(*cobra.Command, string) bool { return true }
	t.Cleanup(func() { confirmRemoveApp = previousConfirm })
	var calls []string
	previousRunner := uninstallRunner
	uninstallRunner = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { uninstallRunner = previousRunner })

	stdout, _, err := runConnect(t, []string{"disconnect", "qoder2api"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "the saved key has been removed") {
		t.Fatalf("output = %q, want key removal notice", stdout)
	}
	reloaded, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Token("qoder2api") != "" || reloaded.State("qoder2api") != "" {
		t.Fatalf("credentials not cleared: %+v", reloaded)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(err) {
		t.Fatalf("gateway directory not removed: %v", err)
	}
	if len(calls) != len(command) || (len(command) > 0 && calls[0] != strings.Join(command, " ")) {
		t.Fatalf("uninstall calls = %v, want %v", calls, command)
	}
}

func TestDisconnectKeepsAppWhenDeclined(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, _ := providers.ExternalGatewayUninstall("trae2api")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	previousConfirm := confirmRemoveApp
	confirmRemoveApp = func(*cobra.Command, string) bool { return false }
	t.Cleanup(func() { confirmRemoveApp = previousConfirm })
	called := false
	previousRunner := uninstallRunner
	uninstallRunner = func(string, ...string) error { called = true; return nil }
	t.Cleanup(func() { uninstallRunner = previousRunner })

	if _, _, err := runConnect(t, []string{"disconnect", "trae2api"}, "", nil); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("uninstall ran after the user declined")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("gateway directory should remain: %v", err)
	}
}

func TestDisconnectRemoveAppFlagSkipsPrompt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, _ := providers.ExternalGatewayUninstall("qoder2api")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	previousConfirm := confirmRemoveApp
	confirmRemoveApp = func(*cobra.Command, string) bool {
		t.Fatal("prompt must not be shown with --remove-app")
		return false
	}
	t.Cleanup(func() { confirmRemoveApp = previousConfirm })
	previousRunner := uninstallRunner
	uninstallRunner = func(string, ...string) error { return nil }
	t.Cleanup(func() { uninstallRunner = previousRunner })
	if _, _, err := runConnect(t, []string{"disconnect", "qoder2api", "--remove-app"}, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("gateway directory not removed: %v", err)
	}
}

func TestConnectListShowsGatewayConnectedWithoutToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetState("pplx-proxy", "connected"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runConnect(t, []string{"--list"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "pplx-proxy") {
			if !strings.Contains(line, "connected") || strings.Contains(line, "not connected") {
				t.Fatalf("gateway line = %q, want connected", line)
			}
			return
		}
	}
	t.Fatal("pplx-proxy missing from connect --list")
}
