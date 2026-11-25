package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/runtime"
	"github.com/benoitpetit/voie/providers"
)

// readDoc reads a documentation file relative to the repository root.
func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func assertContains(t *testing.T, label, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: missing %q", label, needle)
	}
}

func assertOmits(t *testing.T, label, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%s: must not contain %q", label, needle)
	}
}

func TestLicenceAndReleaseLink(t *testing.T) {
	assertContains(t, "LICENSE", readDoc(t, "LICENSE"), "MIT License")
	assertOmits(t, "README.md", readDoc(t, "README.md"), "releases/tag/v0.0.3")
}

func TestReadmeFramesVoieAsRelay(t *testing.T) {
	readme := readDoc(t, "README.md")
	assertOmits(t, "README.md", readme, "to discover and call supported models")
	assertContains(t, "README.md", readme, "relays requests to public model providers")
	assertContains(t, "README.md", readme, "does not run models")
}

func TestInterfaceDocsDiscloseRelay(t *testing.T) {
	for _, tc := range []struct{ file, needle string }{
		{"api-reference.md", "no local inference"},
		{"docs/architecture.md", "no local inference"},
		{"docs/mcp.md", "no local inference"},
		{"skills/voie/SKILL.md", "no local inference"},
	} {
		assertContains(t, tc.file, readDoc(t, tc.file), tc.needle)
	}
}

func TestDocumentedProvidersMatchCode(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	rt, err := runtime.NewWithRegistry(cfg, providers.NewRegistry())
	if err != nil {
		t.Fatalf("runtime.NewWithRegistry: %v", err)
	}

	names := make(map[string]bool)
	for _, name := range rt.Registry.GetProviderNames() {
		names[name] = true
	}
	for _, want := range []string{"duckai", "deepai", "quillbot", "yqcloud", "jimmy", "kilo", "ovh", "blockrun", "pollinations", "opencode-zen", "opencode-go", "openrouter", "orcarouter", "antigravity", "kiro", "cline", "groq", "google-ai-studio", "nvidia-nim", "zai", "mistral", "cloudflare-ai", "cohere-trial", "requesty", "aihubmix", "anyapi", "beatapi", "iflytek-spark", "vlm-run", "codebuddy", "minimax-code", "devin", "qoder", "copilot-cli", "cursor", "runcrate", "9routerx", "freellmapi", "freellmpool", "free-llm-router", "trae2api", "ollama", "lmstudio", "llamacpp", "omniroute", "qoder2api", "windsurfapi", "copilot-api", "cursor-api-proxy", "codearts2api", "cli2api", "workbuddy2api"} {
		if !names[want] {
			t.Errorf("provider %q is not registered", want)
		}
	}
	if names["cohere"] {
		t.Error("cohere must not be registered: the HuggingFace Space requires sign-in")
	}
}

func TestDocumentedCommandsAndStatusCodes(t *testing.T) {
	readme := readDoc(t, "README.md")
	for _, cmd := range []string{"serve", "chat", "conversations", "models", "providers", "mcp", "completion", "version", "update"} {
		assertContains(t, "README.md", readme, "`"+cmd+"`")
	}

	api := readDoc(t, "api-reference.md")
	for _, code := range []string{"400", "401", "404", "408", "409", "410", "500", "502", "503", "504"} {
		assertContains(t, "api-reference.md", api, "| `"+code+"` |")
	}
}

func TestDockerfileDeclaresAPIToken(t *testing.T) {
	dockerfile := readDoc(t, "Dockerfile")
	assertContains(t, "Dockerfile", dockerfile, "HOST=0.0.0.0")
	assertContains(t, "Dockerfile", dockerfile, "API_TOKEN=")
	assertContains(t, "docs/deploy-coolify.md", readDoc(t, "docs/deploy-coolify.md"), "API_TOKEN")
}

func TestReadmeProviderCountMatchesRegistry(t *testing.T) {
	readme := readDoc(t, "README.md")
	re := regexp.MustCompile(`has (\d+) providers`)
	match := re.FindStringSubmatch(readme)
	if match == nil {
		t.Fatal("README has no provider count")
	}
	got, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	if want := len(providers.NewRegistry().GetProviderNames()); got != want {
		t.Errorf("README provider count = %d, want %d", got, want)
	}
}

func TestReadmeGatewayCountMatchesSpecs(t *testing.T) {
	readme := readDoc(t, "README.md")
	want := fmt.Sprintf("%d third-party gateways", len(providers.ExternalGateways))
	assertContains(t, "README.md", readme, want)
}

func TestChangelogCountsAndTokenListMatchCode(t *testing.T) {
	changelog := readDoc(t, "CHANGELOG.md")
	tokenCount := len(providers.TokenProviders) + 2
	assertContains(t, "CHANGELOG.md", changelog, fmt.Sprintf("- %d token-based", tokenCount))
	assertContains(t, "CHANGELOG.md", changelog, fmt.Sprintf("%d opt-in third-party gateways", len(providers.ExternalGateways)))
	assertContains(t, "CHANGELOG.md", changelog, "Runcrate")
}

func TestEnvExampleListsRequiredVariables(t *testing.T) {
	env := readDoc(t, ".env.example")
	for _, name := range []string{
		"REQUIRE_CONVERSATION_SCOPE",
		"VOIE_CLOUDFLARE_ACCOUNT_ID", "VOIE_OPENCODE_GATEWAY_URL",
		"KIRO_GATEWAY_DIR", "VOIE_CLINE_FREE_FEED_URL",
	} {
		assertContains(t, ".env.example", env, name)
	}
}

func TestReadmeDocumentsAllProviderKeyEnvVars(t *testing.T) {
	readme := readDoc(t, "README.md")
	for _, spec := range providers.ExternalGateways {
		name := "VOIE_" + strings.ToUpper(strings.ReplaceAll(spec.Name, "-", "_")) + "_KEY"
		assertContains(t, "README.md", readme, name)
	}
}

func TestCIRunsNpmTestsRaceAndWindowsBuild(t *testing.T) {
	workflow := readDoc(t, ".github/workflows/ci.yml")
	assertContains(t, "ci.yml", workflow, "npm test")
	assertContains(t, "ci.yml", workflow, "go test -race ./...")
	assertContains(t, "ci.yml", workflow, "windows-latest")
	assertContains(t, "ci.yml", workflow, "GOOS=windows")
	assertContains(t, "ci.yml", workflow, "TestReplaceExecutableWindows")
}

func TestDocsAndSkillMatchSurfaces(t *testing.T) {
	skill := readDoc(t, "skills/voie/SKILL.md")
	assertContains(t, "SKILL.md", skill, "ensemble.models")
	assertContains(t, "SKILL.md", skill, "create_conversation")
	mcp := readDoc(t, "docs/mcp.md")
	assertContains(t, "docs/mcp.md", mcp, "groups")
	assertContains(t, "docs/mcp.md", mcp, "expires_at")
	readme := readDoc(t, "README.md")
	assertContains(t, "README.md", readme, "DEBUG=true")
	cli := readDoc(t, "docs/cli.md")
	assertOmits(t, "docs/cli.md", cli, "owner column")
}

func TestAPIRefDocumentsOpenAISubset(t *testing.T) {
	api := readDoc(t, "api-reference.md")
	assertContains(t, "api-reference.md", api, "temperature")
	assertContains(t, "api-reference.md", api, "tool_calls")
	assertContains(t, "api-reference.md", api, "completion choice is supported")
}

func TestDocsDescribeModelTiers(t *testing.T) {
	cli := readDoc(t, "docs/cli.md")
	for _, flag := range []string{"--free", "--paid", "--provider", "TIER"} {
		assertContains(t, "docs/cli.md", cli, flag)
	}
	mcp := readDoc(t, "docs/mcp.md")
	assertContains(t, "docs/mcp.md", mcp, "tier")
	assertContains(t, "docs/mcp.md", mcp, "price unknown")
	assertContains(t, "docs/mcp.md", mcp, "subscribe")
	api := readDoc(t, "api-reference.md")
	assertContains(t, "api-reference.md", api, "tier")
	assertContains(t, "api-reference.md", api, "price unknown")
	assertContains(t, "api-reference.md", api, "subscribe")
	assertContains(t, "README.md", readDoc(t, "README.md"), "TIER")
}

func TestDocsDescribePPLXProxyGateway(t *testing.T) {
	for _, file := range []string{"README.md", "docs/cli.md"} {
		content := readDoc(t, file)
		assertContains(t, file, content, "pplx-proxy")
		assertContains(t, file, content, "cookie")
	}
}

func TestDocsDescribeCompactInstallList(t *testing.T) {
	content := readDoc(t, "docs/cli.md")
	assertContains(t, "docs/cli.md", content, "--verbose")
	assertContains(t, "docs/cli.md", content, "NEXT")

}

func TestDocsDescribeDisconnectCleanup(t *testing.T) {
	cli := readDoc(t, "docs/cli.md")
	assertContains(t, "docs/cli.md", cli, "--remove-app")
	assertContains(t, "docs/cli.md", cli, "saved key")
	assertContains(t, "docs/cli.md", cli, "disconnect")
	readme := readDoc(t, "README.md")
	assertContains(t, "README.md", readme, "--remove-app")
}

func TestDocsDescribeModelDiscovery(t *testing.T) {
	mcp := readDoc(t, "docs/mcp.md")
	assertContains(t, "docs/mcp.md", mcp, "tier")
	assertContains(t, "docs/mcp.md", mcp, "free_models")
	api := readDoc(t, "api-reference.md")
	assertContains(t, "api-reference.md", api, "`tier`")
	assertContains(t, "api-reference.md", api, "free_models")
}
