package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/config"
	"time"
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
	if got := strings.Join(workbuddy.Start, " "); got != "workbuddy2api --port 8787" {
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

func TestRefreshGatewayModelsRefetchesLiveCatalogue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"fresh-model"}]}`))
	}))
	defer server.Close()
	previous := gatewayBaseURL
	gatewayBaseURL = func(spec ExternalGatewaySpec) string { return server.URL }
	defer func() { gatewayBaseURL = previous }()
	gatewayModelsMu.Lock()
	gatewayModelCaches = make(map[string]gatewayModelCacheEntry)
	gatewayModelsMu.Unlock()
	writeGatewayModelsCache(map[string]gatewayModelCacheEntry{
		"omniroute": {At: time.Now(), Models: map[string]string{"om-stale": "stale-model"}},
	})

	RefreshGatewayModels("omniroute")

	gatewayModelsMu.Lock()
	entry, ok := gatewayModelCaches["omniroute"]
	gatewayModelsMu.Unlock()
	if !ok || entry.Models["om-fresh-model"] != "fresh-model" {
		t.Fatalf("refresh did not fetch the live catalogue: %v", entry.Models)
	}
	if _, stale := entry.Models["om-stale"]; stale {
		t.Fatalf("stale model survived refresh: %v", entry.Models)
	}
	if saved := readGatewayModelsCache()["omniroute"]; saved.Models["om-fresh-model"] != "fresh-model" {
		t.Fatalf("disk cache was not refreshed: %v", saved.Models)
	}
}

func TestGatewayDiscoveryKeepsPaidModels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"Auto"},{"id":"paid-model"}]}`))
	}))
	defer server.Close()
	previous := gatewayBaseURL
	gatewayBaseURL = func(spec ExternalGatewaySpec) string { return server.URL }
	defer func() { gatewayBaseURL = previous }()
	gatewayModelsMu.Lock()
	gatewayModelCaches = make(map[string]gatewayModelCacheEntry)
	gatewayModelsMu.Unlock()

	spec, ok := ExternalGatewayByName("qoder2api")
	if !ok {
		t.Fatal("qoder2api spec missing")
	}
	models := fetchGatewayModels(spec.Name, server.URL, "")
	if len(models) != 2 {
		t.Fatalf("discovered models = %v, want both free and paid", models)
	}
	free := gatewayFreeModels(spec.Name, models)
	if len(free) != 1 || !free["q2-Auto"] {
		t.Fatalf("free models = %v, want only q2-Auto", free)
	}
}

func TestGatewayRequiresConnectBeforeListed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	spec, ok := ExternalGatewayByName("qoder2api")
	if !ok {
		t.Fatal("qoder2api spec missing")
	}
	if err := os.MkdirAll(gatewayDir(spec), 0o700); err != nil {
		t.Fatal(err)
	}
	if NewExternalGateway(spec).GetInfo().Working {
		t.Fatal("installed but unconnected gateway reported as callable")
	}
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetState(spec.Name, "connected"); err != nil {
		t.Fatal(err)
	}
	if !NewExternalGateway(spec).GetInfo().Working {
		t.Fatal("connected gateway should be callable")
	}
}

func TestPPLXProxyGatewaySpec(t *testing.T) {
	spec, ok := ExternalGatewayByName("pplx-proxy")
	if !ok {
		t.Fatal("pplx-proxy gateway is not registered")
	}
	if spec.Port != 8892 || spec.HealthPath != "/health" {
		t.Fatalf("spec = %+v", spec)
	}
	if spec.AccountRequired != "upstream" {
		t.Fatalf("AccountRequired = %q, want upstream", spec.AccountRequired)
	}
}

func TestSetGatewayEnvValue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SetGatewayEnvValue("pplx-proxy", "PPLX_COOKIE", "first"); err != nil {
		t.Fatal(err)
	}
	if err := SetGatewayEnvValue("pplx-proxy", "PPLX_COOKIE", "second"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(gatewayDirFor("pplx-proxy"), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "PPLX_COOKIE=") != 1 || !strings.Contains(string(data), "PPLX_COOKIE=second") {
		t.Fatalf(".env = %q", data)
	}
}

func TestGatewayCheckRunsInsideGatewayDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	spec, ok := ExternalGatewayByName("pplx-proxy")
	if !ok {
		t.Fatal("pplx-proxy spec missing")
	}
	dir := gatewayDir(spec)
	if NewExternalGateway(spec).working() {
		t.Fatal("gateway reported installed without server.py")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.py"), []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if !NewExternalGateway(spec).working() {
		t.Fatal("gateway not reported installed despite server.py in its directory")
	}
}

func TestExternalGatewayUninstallTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		dir  bool
		cmd  string
	}{
		{name: "qoder2api", dir: true},
		{name: "omniroute", dir: true, cmd: "npm uninstall -g omniroute"},
		{name: "workbuddy2api", dir: true, cmd: "uv tool uninstall workbuddy2api"},
	} {
		dir, command := ExternalGatewayUninstall(tc.name)
		if (dir != "") != tc.dir {
			t.Errorf("%s: dir = %q", tc.name, dir)
		}
		got := strings.Join(command, " ")
		if got != tc.cmd {
			t.Errorf("%s: command = %q, want %q", tc.name, got, tc.cmd)
		}
	}
}

func TestFreellmpoolCatalogueKeepsOnlyRoutingAliases(t *testing.T) {
	models := map[string]string{
		"pool-auto":                             "auto",
		"pool-fast":                             "fast",
		"pool-kilo/stepfun/step-3.7-flash:free": "kilo/stepfun/step-3.7-flash:free",
		"pool-ovh/Meta-Llama-3_3-70B-Instruct":  "ovh/Meta-Llama-3_3-70B-Instruct",
	}
	filtered := filterGatewayCatalogue("freellmpool", models)
	if len(filtered) != 2 || filtered["pool-auto"] == "" || filtered["pool-fast"] == "" {
		t.Fatalf("filtered = %v, want only routing aliases", filtered)
	}
	if got := filterGatewayCatalogue("omniroute", models); len(got) != len(models) {
		t.Fatalf("non-pool gateway was filtered: %v", got)
	}
}

func TestPPLXProxyOnlyAutoIsFree(t *testing.T) {
	models := map[string]string{"auto": "auto", "gpt": "gpt", "claude55sonnet": "claude55sonnet"}
	free := gatewayFreeModels("pplx-proxy", models)
	if len(free) != 1 || !free["auto"] {
		t.Fatalf("free models = %v, want only auto", free)
	}
}

func TestGatewayCacheKeysGainProviderPrefix(t *testing.T) {
	models := map[string]string{"auto": "auto", "gpt": "gpt"}
	got := normalizeGatewayModelKeys("pplx-proxy", models)
	if _, ok := got["pplx-auto"]; !ok {
		t.Fatalf("keys = %v, want pplx-auto", got)
	}
	if _, ok := got["pplx-gpt"]; !ok {
		t.Fatalf("keys = %v, want pplx-gpt", got)
	}
}

func TestGatewayFreeAllowlistIsCaseInsensitive(t *testing.T) {
	models := map[string]string{"q2-alias": "auto"}
	free := gatewayFreeModels("qoder2api", models)
	if len(free) != 1 || !free["q2-alias"] {
		t.Fatalf("free = %v, want the lowercase auto alias", free)
	}
}

func TestGatewayProbeFailureKeepsDiskCatalogue(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	previous := gatewayBaseURL
	gatewayBaseURL = func(ExternalGatewaySpec) string { return server.URL }
	defer func() { gatewayBaseURL = previous }()
	writeGatewayModelsCache(map[string]gatewayModelCacheEntry{
		"omniroute": {At: time.Now().Add(-25 * time.Hour), Models: map[string]string{"om-good": "good-model"}},
	})
	gatewayModelsMu.Lock()
	gatewayModelCaches = make(map[string]gatewayModelCacheEntry)
	gatewayModelsMu.Unlock()

	models := fetchGatewayModels("omniroute", server.URL, "")
	if len(models) != 1 || models["om-good"] != "good-model" {
		t.Fatalf("models = %v, want the disk snapshot after a failed probe", models)
	}
}
