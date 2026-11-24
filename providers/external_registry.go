package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

// ExternalGatewaySpec describes a third-party loopback gateway that exposes
// one or more coding-agent services as OpenAI-compatible endpoints. These are
// opt-in and user-installed: voie never bundles or operates them.
type ExternalGatewaySpec struct {
	Name            string
	Label           string
	Repo            string
	Port            int
	Alias           string
	Upstream        string
	HealthPath      string
	HealthPort      int               // separate health port when admin and /v1 listeners differ
	KeyFile         string            // generated gateway key file (absolute or ~-prefixed)
	Install         []string          // explicit install; empty means git clone Repo into the gateway dir
	Deps            [][]string        // commands run in the gateway dir after install
	Start           []string          // command that launches the gateway on its loopback port
	Check           []string          // optional command that proves the tool is really installed
	ManualInstall   string            // shown instead of running an automated installer
	Env             map[string]string // safe defaults forwarded to install and start commands
	GeneratedEnv    []string          // secrets generated once and reused via EnvFile
	Stop            []string          // command that stops the gateway's own daemon
	StopKillBinary  string            // optional binary to force-kill after Stop (daemonizing tools only)
	FreeModels      []string          // upstream IDs verified free; nil keeps the whole lane, empty keeps none
	SetupHint       string            // account configuration required before inference
	AccountRequired string            // "none", "upstream", "dashboard+upstream"
	AccountKind     string            // which account exactly
	AccountURL      string            // where to create or link it
	DashboardURL    string            // local dashboard to open during `voie connect`
	SetupCommand    []string          // interactive account login to run before starting
	SetupLogCommand []string          // command whose output contains a first-run setup code
	SetupLogPattern string            // regex; first capture group is the code shown to the user
	KeyRequired     string            // local gateway key policy
}

// ExternalGateways are ranked by observed activity in Nov 2026. Install and
// start commands were verified against each repository in Nov 2026. All are
// BYO-account: a bare start only exposes the health probe, not inference.
var ExternalGateways = []ExternalGatewaySpec{
	{
		Name: "omniroute", Label: "OmniRoute", Repo: "https://github.com/cheaper-inference/OmniRoute",
		Port: 20128, Alias: "omni-free", Upstream: "openrouter/free", HealthPath: "/healthz",
		Install:         []string{"npm", "install", "-g", "omniroute@latest"},
		SetupHint:       "connect a provider in the OmniRoute dashboard and create a gateway API key",
		AccountRequired: "dashboard+upstream",
		AccountKind:     "OmniRoute dashboard password + at least one upstream provider (keyless NOAUTH providers exist)",
		AccountURL:      "http://localhost:20128",
		DashboardURL:    "http://localhost:20128",
		KeyRequired:     "optional locally (REQUIRE_API_KEY off by default)",
		Start:           []string{"omniroute"},
	},
	{
		Name: "qoder2api", Label: "Qoder2API Hub", Repo: "https://github.com/shuishuipingan/qoder2api-hub",
		Port: 8790, Alias: "qoder2-free", Upstream: "auto", HealthPath: "/health",
		FreeModels:      []string{"Auto"},
		Deps:            [][]string{{"bash", "-c", "chmod +x *.sh"}},
		SetupHint:       "open the dashboard (default password admin) and add your Qoder account",
		AccountRequired: "upstream",
		AccountKind:     "Qoder CN/intl account (free daily check-in credits)",
		AccountURL:      "https://qoder.com/account/integrations",
		DashboardURL:    "http://127.0.0.1:8790",
		KeyRequired:     "optional on localhost when no key is set",
		Start:           []string{"bash", "start-qoder-proxy.sh"},
	},
	{
		Name: "windsurfapi", Label: "WindsurfAPI", Repo: "https://github.com/dwgx/WindsurfAPI",
		Port: 3003, Alias: "devin2-free", Upstream: "swe-2-flash-medium", HealthPath: "/health",
		Deps:            [][]string{{"bash", "-c", "sed -i 's#/opt/windsurf#$HOME/.windsurf#g' setup.sh && bash setup.sh"}},
		SetupHint:       "log in with a Windsurf/Devin account and set API_KEY in .env",
		AccountRequired: "upstream",
		AccountKind:     "Windsurf/Devin account (free models available)",
		AccountURL:      "https://windsurf.com/show-auth-token",
		DashboardURL:    "http://localhost:3003/dashboard",
		KeyRequired:     "required: API_KEY or WINDSURFAPI_ALLOW_UNAUTHENTICATED=1 on loopback",
		Start:           []string{"npm", "start"},
	},
	{
		Name: "copilot-api", Label: "Copilot API", Repo: "https://github.com/caozhiyuan/copilot-api",
		Port: 4141, Alias: "copilot2-free", Upstream: "auto", HealthPath: "/v1/models",
		Install:         []string{"npm", "install", "-g", "@jeffreycao/copilot-api@latest"},
		SetupHint:       "run the gateway once and complete provider setup (Copilot/Codex/xAI login)",
		AccountRequired: "upstream",
		AccountKind:     "GitHub Copilot Free, Codex or xAI account",
		AccountURL:      "https://github.com/settings/copilot",
		SetupCommand:    []string{"copilot-api", "auth"},
		KeyRequired:     "optional on loopback",
		Start:           []string{"copilot-api", "start"},
	},
	{
		Name: "cursor-api-proxy", Label: "Cursor ACP Proxy", Repo: "https://github.com/anyrobert/cursor-api-proxy",
		Port: 8765, Alias: "cursor2-free", Upstream: "auto", HealthPath: "/healthz",
		Install:         []string{"npm", "install", "-g", "cursor-api-proxy@latest"},
		SetupHint:       "install the Cursor agent CLI and run `agent login` (or set CURSOR_API_KEY)",
		AccountRequired: "upstream",
		AccountKind:     "Cursor Free/Hobby account or dashboard integration key",
		AccountURL:      "https://cursor.com/dashboard/integrations",
		SetupCommand:    []string{"cursor-api-proxy", "login"},
		KeyRequired:     "optional",
		Start:           []string{"cursor-api-proxy"},
	},
	{
		Name: "codearts2api", Label: "CodeArts2API", Repo: "https://github.com/zhuweiyou/codearts2api",
		Port: 8326, Alias: "codearts-free", Upstream: "opencode/mimo-v2.6-flash-free", HealthPath: "/healthz",
		HealthPort: 8080, KeyFile: "~/.codearts2api/openai-gateway/api-key",
		Deps:            [][]string{{"npm", "install", "-g", "pnpm@11"}, {"pnpm", "install"}, {"pnpm", "run", "build"}},
		SetupHint:       "no account needed: the OpenCode anonymous channel serves mimo-v2.6-flash-free; key read from ~/.codearts2api/openai-gateway/api-key",
		AccountRequired: "none",
		AccountKind:     "auto-added OpenCode anonymous channel (api_key=public, mimo-v2.6-flash-free) works with no account",
		AccountURL:      "https://github.com/zhuweiyou/codearts2api",
		KeyRequired:     "required: generated gateway key at <home>/openai-gateway/api-key",
		Start:           []string{"pnpm", "start"},
	},
	{
		Name: "cli2api", Label: "CLI2API", Repo: "https://github.com/caigee-cmd/cli2api",
		Port: 3010, Alias: "cli2-free", Upstream: "qoder", HealthPath: "/health",
		Deps:            [][]string{{"go", "build", "-o", "gateway", "./cmd/server"}},
		SetupHint:       "save the admin key, add an upstream account, then create a client key",
		AccountRequired: "upstream",
		AccountKind:     "Qoder / WorkBuddy / Trae account",
		AccountURL:      "http://127.0.0.1:3010",
		DashboardURL:    "http://127.0.0.1:3010",
		KeyRequired:     "required: generated admin or client key",
		Start:           []string{"./gateway"},
	},
	{
		Name: "workbuddy2api", Label: "WorkBuddy2API", Repo: "https://github.com/hawklithm/workbuddy2api",
		Port: 8787, Alias: "wb2-free", Upstream: "auto", HealthPath: "/health",
		Install:         []string{"uv", "tool", "install", "workbuddy2api"},
		SetupHint:       "link a CodeBuddy account at /admin or with --login",
		AccountRequired: "upstream",
		AccountKind:     "CodeBuddy (CN) or WorkBuddy (Global) account",
		AccountURL:      "http://127.0.0.1:8787/admin",
		DashboardURL:    "http://127.0.0.1:8787/admin",
		KeyRequired:     "none: placeholder accepted",
		Start:           []string{"workbuddy2api", "--port", "8787"},
	},
	{
		Name: "freellmpool", Label: "FreeLLM Pool", Repo: "https://github.com/0xzr/freellmpool",
		Port: 8083, Alias: "pool-free", Upstream: "auto", HealthPath: "/healthz",
		Install:         []string{"uv", "tool", "install", "freellmpool"},
		SetupHint:       "works keyless out of the box; keyless routes send prompts to third parties",
		AccountRequired: "none", AccountKind: "keyless routes (Pollinations, OVHcloud, Kilo, LLM7)",
		AccountURL: "https://github.com/0xzr/freellmpool", KeyRequired: "optional (--api-key)",
		Start: []string{"freellmpool", "proxy", "--port", "8083"},
	},
	{
		Name: "free-llm-router", Label: "Free LLM Proxy Router", Repo: "https://github.com/kaiser-data/free-llm-proxy-router",
		Port: 8082, Alias: "flr-free", Upstream: "auto", HealthPath: "/health",
		Install:         []string{"sh", "-c", "curl -fsSL https://raw.githubusercontent.com/kaiser-data/free-llm-proxy-router/main/scripts/install.sh | sh"},
		SetupHint:       "official free-tier API keys only; no OAuth or session reuse",
		AccountRequired: "upstream", AccountKind: "official free API key (Groq, Gemini, OpenRouter, ...)",
		AccountURL: "https://github.com/kaiser-data/free-llm-proxy-router", KeyRequired: "optional loopback auth token",
		Start: []string{"./free-llm-proxy", "--port", "8082"},
	},
	{
		Name: "trae2api", Label: "Trae2API Hub", Repo: "https://github.com/shuishuipingan/trae2api-hub",
		Port: 8791, Alias: "te-free", Upstream: "auto", HealthPath: "/health",
		FreeModels:      []string{"Auto"},
		Deps:            [][]string{{"bash", "-c", "chmod +x *.sh"}},
		SetupHint:       "consumer-subscription reverse proxy: link your own Trae account in the dashboard; ToS risk",
		AccountRequired: "upstream", AccountKind: "Trae personal account (CN or INTL)",
		AccountURL: "http://127.0.0.1:8791", DashboardURL: "http://127.0.0.1:8791", KeyRequired: "optional locally (dashboard password admin)",
		Start: []string{"bash", "start-trae2api-hub.sh"},
	},
	{
		Name: "ollama", Label: "Ollama", Repo: "https://ollama.com",
		Port: 11434, Alias: "ollama-local", Upstream: "qwen3.5:4b", HealthPath: "/api/tags",
		Install:         []string{"sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh"},
		SetupHint:       "download a model first: ollama pull qwen3.5:4b (or any ID from `ollama list`; inference stays on your machine)",
		AccountRequired: "none", AccountKind: "local models, no account",
		AccountURL: "https://ollama.com/library", KeyRequired: "none",
		Start: []string{"ollama", "serve"},
	},
	{
		Name: "lmstudio", Label: "LM Studio", Repo: "https://lmstudio.ai",
		Port: 1234, Alias: "lms-local", Upstream: "local-model", HealthPath: "/v1/models",
		ManualInstall:   "install LM Studio from https://lmstudio.ai and download a model in the GUI",
		SetupHint:       "start the server in Developer tab or with `lms server start` after downloading a model",
		AccountRequired: "none", AccountKind: "local models, no account",
		AccountURL: "https://lmstudio.ai/docs/developer/core/server", KeyRequired: "none",
		Start: []string{"lms", "server", "start"},
	},
	{
		Name: "llamacpp", Label: "llama.cpp", Repo: "https://github.com/ggml-org/llama.cpp",
		Port: 9931, Alias: "llamacpp-local", Upstream: "local-gguf", HealthPath: "/health",
		ManualInstall:   "build llama-server from https://github.com/ggml-org/llama.cpp and provide a GGUF model",
		SetupHint:       "start with: llama-server --host 127.0.0.1 --port 9931 -m /path/to/model.gguf",
		AccountRequired: "none", AccountKind: "local GGUF model, no account",
		AccountURL: "https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md", KeyRequired: "none",
		Start: []string{"llama-server", "--host", "127.0.0.1", "--port", "9931"},
	},
	{
		Name: "9routerx", Label: "9routerx (safe lanes)", Repo: "https://github.com/yanstd888/9routerx",
		Port: 20129, Alias: "9r-free", Upstream: "oc/big-pickle", HealthPath: "/v1/models",
		Install: []string{"npm", "install", "-g", "9routerx"},
		Env: map[string]string{
			"PORT":     "20129",
			"DATA_DIR": gatewayDirFor("9routerx"),
		},
		GeneratedEnv:    []string{"JWT_SECRET", "INITIAL_PASSWORD", "ROUTER_MASTER_KEY"},
		SetupHint:       "secrets are generated automatically; web-cookie lanes are off by default — never use /browser-login or /import-chrome",
		AccountRequired: "none", AccountKind: "anonymous OpenCode lane (oc/big-pickle) or official API keys",
		AccountURL: "https://github.com/yanstd888/9routerx", KeyRequired: "local sk- key created in dashboard",
		Start:          []string{"9routerx"},
		Stop:           []string{"9routerx", "stop"},
		StopKillBinary: "9routerx",
		FreeModels:     []string{},
	},
	{
		Name: "freellmapi", Label: "FreeLLMAPI", Repo: "https://github.com/tashfeenahmed/freellmapi",
		Port: 3001, Alias: "flm-free", Upstream: "auto", HealthPath: "/api/ping",
		Deps:         [][]string{{"docker", "compose", "up", "-d"}},
		GeneratedEnv: []string{"ENCRYPTION_KEY"},
		Env: map[string]string{
			"CATALOG_SYNC_DISABLED":   "1",
			"FREELLMAPI_UPDATE_CHECK": "off",
			"HOST_BIND":               "127.0.0.1",
			"PORT":                    "3001",
		},
		SetupHint:       "BYOK only: set ENCRYPTION_KEY before connect, add your own provider keys in the dashboard; remote catalog is disabled",
		AccountRequired: "upstream", AccountKind: "official free API keys added in the dashboard",
		AccountURL: "https://github.com/tashfeenahmed/freellmapi", DashboardURL: "http://127.0.0.1:3001", KeyRequired: "unified key from the dashboard",
		SetupLogCommand: []string{"docker", "compose", "logs", "--tail", "200"},
		SetupLogPattern: `First-run setup code:\s*(\S+)`,
		Start:           []string{"docker", "compose", "up", "-d"},
		Stop:            []string{"docker", "compose", "down"},
	},
}

func ExternalGatewayByName(name string) (ExternalGatewaySpec, bool) {
	for _, spec := range ExternalGateways {
		if spec.Name == name {
			return spec, true
		}
	}
	return ExternalGatewaySpec{}, false
}

func gatewayDirFor(name string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "gateways", name)
}

func gatewayDir(spec ExternalGatewaySpec) string {
	return gatewayDirFor(spec.Name)
}

func gatewayTokenSupplier(spec ExternalGatewaySpec) func() string {
	return func() string {
		store, _ := config.LoadCredentials()
		if value := store.Token(spec.Name); value != "" {
			return value
		}
		if spec.KeyFile != "" {
			path := spec.KeyFile
			if strings.HasPrefix(path, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
				}
			}
			if data, err := os.ReadFile(path); err == nil {
				if value := strings.TrimSpace(string(data)); value != "" {
					return value
				}
			}
		}
		return "voie-local"
	}
}

type gatewayModelCacheEntry struct {
	At     time.Time         `json:"at"`
	Models map[string]string `json:"models"`
}

var (
	gatewayModelsMu    sync.Mutex
	gatewayModelCaches = make(map[string]gatewayModelCacheEntry)
)

func gatewayModelsCachePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "gateway-models.json")
}

func readGatewayModelsCache() map[string]gatewayModelCacheEntry {
	result := make(map[string]gatewayModelCacheEntry)
	path := gatewayModelsCachePath()
	if path == "" {
		return result
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &result)
	}
	return result
}

func writeGatewayModelsCache(entries map[string]gatewayModelCacheEntry) {
	path := gatewayModelsCachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

// gatewayFreeModels returns the discovered aliases that are verified free for
// this gateway. nil FreeModels means the whole lane is free; an empty list
// means none of the discovered models are free.
func gatewayFreeModels(name string, models map[string]string) map[string]bool {
	spec, ok := ExternalGatewayByName(name)
	free := make(map[string]bool, len(models))
	if !ok || spec.FreeModels == nil {
		for alias := range models {
			free[alias] = true
		}
		return free
	}
	allowed := make(map[string]bool, len(spec.FreeModels))
	for _, id := range spec.FreeModels {
		allowed[id] = true
	}
	for alias, upstream := range models {
		if allowed[upstream] {
			free[alias] = true
		}
	}
	return free
}

func fetchGatewayModels(name, baseURL, key string) map[string]string {
	gatewayModelsMu.Lock()
	entry, ok := gatewayModelCaches[name]
	gatewayModelsMu.Unlock()
	if ok && time.Since(entry.At) < 10*time.Minute {
		return entry.Models
	}

	gatewayModelsMu.Lock()
	onDisk := readGatewayModelsCache()
	gatewayModelsMu.Unlock()
	if saved, ok := onDisk[name]; ok && len(saved.Models) > 0 && time.Since(saved.At) < 24*time.Hour {
		gatewayModelsMu.Lock()
		gatewayModelCaches[name] = saved
		gatewayModelsMu.Unlock()
		return saved.Models
	}

	models := make(map[string]string)
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/models", nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+key)
		// Dedicated transport: other tests replace http.DefaultTransport to
		// stub provider calls, and the gateway probe must reach its own URL.
		resp, err := (&http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{}}).Do(req)
		if err == nil {
			var payload struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.NewDecoder(resp.Body).Decode(&payload) == nil {
				for _, item := range payload.Data {
					if item.ID != "" {
						models[app.PrefixModelID(name, item.ID)] = item.ID
					}
				}
			}
			resp.Body.Close()
		}
	}
	fresh := gatewayModelCacheEntry{At: time.Now(), Models: models}
	gatewayModelsMu.Lock()
	gatewayModelCaches[name] = fresh
	if len(models) > 0 {
		onDisk[name] = fresh
		writeGatewayModelsCache(onDisk)
	}
	gatewayModelsMu.Unlock()
	return models
}

// gatewayBaseURL is a seam for tests that point a gateway at an httptest
// server instead of its fixed loopback port.
var gatewayBaseURL = func(spec ExternalGatewaySpec) string {
	return "http://127.0.0.1:" + itoa(spec.Port)
}

// discardGatewayModelsCache drops both the in-memory and the persisted copy
// so a refresh actually fetches the gateway again.
func discardGatewayModelsCache(name string) {
	gatewayModelsMu.Lock()
	defer gatewayModelsMu.Unlock()
	delete(gatewayModelCaches, name)
	entries := readGatewayModelsCache()
	delete(entries, name)
	writeGatewayModelsCache(entries)
}

// RefreshGatewayModels discovers the live catalogue while a gateway is up and
// persists it, so `voie models` keeps showing the real models after shutdown.
func RefreshGatewayModels(name string) {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return
	}
	discardGatewayModelsCache(name)
	fetchGatewayModels(spec.Name, gatewayBaseURL(spec), gatewayTokenSupplier(spec)())
}

// ProvisionGatewayEnv creates the generated secrets once and reuses them on
// every start, so the gateway works without manual environment setup.
func ProvisionGatewayEnv(spec ExternalGatewaySpec) error {
	dir := gatewayDir(spec)
	// The gateway process uses this directory as its working directory; create
	// it even when no secrets are needed so exec does not fail with ENOENT.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if len(spec.GeneratedEnv) == 0 {
		return nil
	}
	path := filepath.Join(dir, ".env")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return nil
	}
	var lines []string
	for _, key := range spec.GeneratedEnv {
		lines = append(lines, key+"="+generatedSecret(key))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func generatedSecret(key string) string {
	size := 24
	switch key {
	case "ROUTER_MASTER_KEY":
		buffer := make([]byte, 32)
		if _, err := rand.Read(buffer); err == nil {
			return base64.StdEncoding.EncodeToString(buffer)
		}
	case "JWT_SECRET":
		size = 32
	case "ENCRYPTION_KEY":
		buffer := make([]byte, 32)
		if _, err := rand.Read(buffer); err == nil {
			return hex.EncodeToString(buffer)
		}
	}
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "voie-" + key
	}
	return hex.EncodeToString(buffer)
}

// ProvisionGatewayKey creates the local dashboard key programmatically when
// the gateway supports it, so no manual browser step is needed.
func ProvisionGatewayKey(spec ExternalGatewaySpec) error {
	if spec.Name != "9routerx" {
		return nil
	}
	store, err := config.LoadCredentials()
	if err != nil {
		return err
	}
	if store.Token(spec.Name) != "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(gatewayDir(spec), ".env"))
	if err != nil {
		return err
	}
	password := ""
	for _, line := range strings.Split(string(data), "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && name == "INITIAL_PASSWORD" {
			password = value
		}
	}
	if password == "" {
		return fmt.Errorf("9routerx INITIAL_PASSWORD is missing")
	}
	base := "http://127.0.0.1:" + itoa(spec.Port)
	client := &http.Client{Timeout: 5 * time.Second}
	loginBody, _ := json.Marshal(map[string]string{"password": password})
	login, err := client.Post(base+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		return err
	}
	defer login.Body.Close()
	if login.StatusCode < 200 || login.StatusCode >= 300 {
		return fmt.Errorf("9routerx login failed: HTTP %d", login.StatusCode)
	}
	cookie := ""
	if setCookie := login.Header.Get("Set-Cookie"); setCookie != "" {
		cookie = strings.SplitN(setCookie, ";", 2)[0]
	}
	key := "sk-" + generatedSecret("gateway-key")
	keyBody, _ := json.Marshal(map[string]string{
		"id":   generatedSecret("key-id"),
		"key":  key,
		"name": "voie",
	})
	request, err := http.NewRequest(http.MethodPost, base+"/api/keys", bytes.NewReader(keyBody))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("9routerx key creation failed: HTTP %d", response.StatusCode)
	}
	return store.Set(spec.Name, key)
}

// SetupCodeFromLogs runs the gateway's log command and extracts the first-run
// setup code. It returns an empty string when no code is present (for example
// once an account already exists).
func SetupCodeFromLogs(spec ExternalGatewaySpec) string {
	if len(spec.SetupLogCommand) == 0 || spec.SetupLogPattern == "" {
		return ""
	}
	pattern, err := regexp.Compile(spec.SetupLogPattern)
	if err != nil {
		return ""
	}
	// A freshly started container may not have flushed its boot log yet.
	for attempt := 0; attempt < 4; attempt++ {
		cmd := exec.Command(spec.SetupLogCommand[0], spec.SetupLogCommand[1:]...)
		cmd.Dir = gatewayDir(spec)
		output, err := cmd.CombinedOutput()
		if err == nil {
			if match := pattern.FindSubmatch(output); len(match) >= 2 {
				return strings.TrimSpace(string(match[1]))
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

// ExternalGateway is a normal provider pointed at a user-installed gateway.
type ExternalGateway struct {
	*openAIKeylessProvider
	spec    ExternalGatewaySpec
	gateway *LocalGateway
}

func NewExternalGateway(spec ExternalGatewaySpec) *ExternalGateway {
	healthPort := spec.Port
	if spec.HealthPort > 0 {
		healthPort = spec.HealthPort
	}
	tokenSupplier := gatewayTokenSupplier(spec)
	gateway := NewLocalGateway(LocalGatewaySpec{
		Name:       spec.Name,
		Binary:     spec.Start[0],
		Args:       spec.Start[1:],
		Dir:        gatewayDir(spec),
		BaseURL:    "http://127.0.0.1:" + itoa(healthPort),
		HealthPath: spec.HealthPath,
		KeyEnv:     "VOIE_" + spec.Name + "_KEY",
		Port:       spec.Port,
		Env:        spec.Env,
		EnvFile:    filepath.Join(gatewayDir(spec), ".env"),
		Stop:       spec.Stop,
	})
	cfg := openAIKeylessConfig{
		Name:           spec.Name,
		Label:          spec.Label,
		URL:            "http://127.0.0.1:" + itoa(healthPort) + spec.HealthPath,
		CompletionsURL: "http://127.0.0.1:" + itoa(spec.Port) + "/v1/chat/completions",
		DefaultModel:   spec.Alias,
		ModelIDs:       map[string]string{spec.Alias: spec.Upstream},
		Description:    spec.Label + " through a user-installed reverse-engineered gateway (ToS risk)",
		SupportsStream: true,
		TokenSupplier:  tokenSupplier,
		ModelsSupplier: func() map[string]string {
			return fetchGatewayModels(spec.Name, "http://127.0.0.1:"+itoa(spec.Port), tokenSupplier())
		},
		Ensure: gateway.Ensure,
	}
	return &ExternalGateway{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), spec: spec, gateway: gateway}
}

func (p *ExternalGateway) working() bool {
	if len(p.spec.Check) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, p.spec.Check[0], p.spec.Check[1:]...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		return cmd.Run() == nil
	}
	if len(p.spec.Install) > 0 {
		binary := p.spec.Start[0]
		// Installers that drop binaries into the gateway directory (instead
		// of installing globally) use a ./ path — resolve it there.
		if strings.HasPrefix(binary, "./") {
			candidate := filepath.Join(gatewayDir(p.spec), strings.TrimPrefix(binary, "./"))
			info, err := os.Stat(candidate)
			return err == nil && !info.IsDir()
		}
		_, err := exec.LookPath(binary)
		return err == nil
	}
	dir := gatewayDir(p.spec)
	if dir == "" {
		return false
	}
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	// A cloned gateway is only usable when its start command exists too.
	binary := p.spec.Start[0]
	if strings.HasPrefix(binary, "./") {
		_, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(binary, "./")))
		return err == nil
	}
	_, err := exec.LookPath(binary)
	return err == nil
}

// callable adds the account/connect requirement on top of the install state.
// An installed gateway whose upstream account has not been connected cannot
// serve inference and must stay out of the model catalogue.
func (p *ExternalGateway) callable() bool {
	if p.spec.AccountRequired != "none" {
		store, err := config.LoadCredentials()
		if err != nil || !strings.EqualFold(store.State(p.spec.Name), "connected") {
			return false
		}
	}
	return p.working()
}

func (p *ExternalGateway) SupportsModel(model string) bool {
	return p.callable() && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *ExternalGateway) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	free := gatewayFreeModels(p.spec.Name, p.modelIDs())
	info.FreeModels = make([]string, 0, len(free))
	for alias := range free {
		info.FreeModels = append(info.FreeModels, alias)
	}
	sort.Strings(info.FreeModels)
	info.Working = p.callable()
	// A gateway that is installed and passes its check is ready even when its
	// loopback listener is stopped: gateways start on demand on the first call.
	info.Ready = p.working()
	return info
}

// ExternalGatewayType returns the short provider kind shown by connect --list.
func ExternalGatewayType(spec ExternalGatewaySpec) string {
	switch {
	case spec.ManualInstall != "":
		return "local runtime"
	case spec.AccountRequired == "none":
		return "keyless gateway"
	case spec.DashboardURL != "":
		return "dashboard gateway"
	default:
		return "byok gateway"
	}
}

// InstallCommand returns the exact command a user must run to install a gateway.
func (p *ExternalGateway) InstallCommand() string {
	if p.spec.ManualInstall != "" {
		return "manual: " + p.spec.ManualInstall
	}
	if len(p.spec.Install) > 0 {
		return strings.Join(p.spec.Install, " ")
	}
	return "git clone " + p.spec.Repo + " " + gatewayDir(p.spec)
}

// ExternalGatewayDir returns the install directory for a gateway.
func ExternalGatewayDir(name string) string {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return ""
	}
	return gatewayDir(spec)
}

// ExternalGatewayStatus reports whether a gateway has been installed.
func ExternalGatewayStatus(name string) (installed bool, installCommand, startCommand string) {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return false, "", ""
	}
	installed = NewExternalGateway(spec).working()
	command := ""
	if len(spec.Start) > 0 {
		command = strings.Join(spec.Start, " ")
	}
	return installed, NewExternalGateway(spec).InstallCommand(), command
}

// GatewayAccount describes what a user must set up before a gateway serves
// real inference.
type GatewayAccount struct {
	Required string
	Kind     string
	URL      string
	Key      string
}

// ExternalGatewayAccount returns the account requirements for a gateway.
func ExternalGatewayAccount(name string) (GatewayAccount, bool) {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return GatewayAccount{}, false
	}
	return GatewayAccount{Required: spec.AccountRequired, Kind: spec.AccountKind, URL: spec.AccountURL, Key: spec.KeyRequired}, true
}

// ExternalGatewaySetupHint returns the account configuration a gateway needs
// before it can serve inference.
func ExternalGatewaySetupHint(name string) string {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return ""
	}
	return spec.SetupHint
}

// StartExternalGateway starts a gateway on demand and reports whether it became healthy.
func StartExternalGateway(name string) error {
	spec, ok := ExternalGatewayByName(name)
	if !ok {
		return errUnknownProvider
	}
	gateway := NewExternalGateway(spec).gateway
	if !gateway.Available() {
		return fmt.Errorf("%s start command %q is unavailable; run `voie install %s` and use a Node 20-24 toolchain if needed", name, spec.Start[0], name)
	}
	return gateway.Ensure(context.Background())
}
