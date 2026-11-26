package providers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/catalog"
)

// TokenProviderSpec describes an OpenAI-compatible API that needs a user token.
type TokenProviderSpec struct {
	Name           string
	Label          string
	CompletionsURL string
	ModelsURL      string
	Alias          string
	UpstreamModel  string
	EnvVar         string
}

// TokenProviders is the declarative catalogue. Adding a provider here is
// enough to expose it through the registry, connect, models and all transports.
var TokenProviders = []TokenProviderSpec{
	{Name: "groq", Label: "Groq", CompletionsURL: "https://api.groq.com/openai/v1/chat/completions", ModelsURL: "https://api.groq.com/openai/v1/models", Alias: "grfree", UpstreamModel: "openai/gpt-oss-120b", EnvVar: "VOIE_GROQ_KEY"},
	{Name: "google-ai-studio", Label: "Google AI Studio", CompletionsURL: "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", ModelsURL: "https://generativelanguage.googleapis.com/v1beta/openai/models", Alias: "gsfree", UpstreamModel: "gemini-3.8-flash", EnvVar: "VOIE_GOOGLE_AI_STUDIO_KEY"},
	{Name: "nvidia-nim", Label: "NVIDIA NIM", CompletionsURL: "https://integrate.api.nvidia.com/v1/chat/completions", ModelsURL: "https://integrate.api.nvidia.com/v1/models", Alias: "nimfree", UpstreamModel: "z-ai/glm-5.3-flash", EnvVar: "VOIE_NVIDIA_NIM_KEY"},
	{Name: "zai", Label: "Z.ai GLM", CompletionsURL: "https://api.z.ai/api/paas/v4/chat/completions", ModelsURL: "https://api.z.ai/api/paas/v4/models", Alias: "zaifree", UpstreamModel: "glm-4.7-flash", EnvVar: "VOIE_ZAI_KEY"},
	{Name: "mistral", Label: "Mistral", CompletionsURL: "https://api.mistral.ai/v1/chat/completions", ModelsURL: "https://api.mistral.ai/v1/models", Alias: "mistralfree", UpstreamModel: "mistral-large-latest", EnvVar: "VOIE_MISTRAL_KEY"},
	{Name: "cloudflare-ai", Label: "Cloudflare Workers AI", CompletionsURL: cloudflareURL("chat/completions"), ModelsURL: cloudflareURL("models"), Alias: "cfaifree", UpstreamModel: "@cf/qwen/qwen3.8-27b", EnvVar: "VOIE_CLOUDFLARE_AI_KEY"},
	{Name: "cohere-trial", Label: "Cohere Trial", CompletionsURL: "https://api.cohere.com/compatibility/v1/chat/completions", ModelsURL: "https://api.cohere.com/compatibility/v1/models", Alias: "cohfree", UpstreamModel: "north-mini-code-1-0", EnvVar: "VOIE_COHERE_TRIAL_KEY"},
	{Name: "requesty", Label: "Requesty", CompletionsURL: "https://router.requesty.ai/v1/chat/completions", ModelsURL: "https://router.requesty.ai/v1/models", Alias: "reqfree", UpstreamModel: "nvidia/nemotron-3-ultra-550b-a55b", EnvVar: "VOIE_REQUESTY_KEY"},
	{Name: "aihubmix", Label: "AIHubMix", CompletionsURL: "https://aihubmix.com/v1/chat/completions", ModelsURL: "https://aihubmix.com/v1/models", Alias: "ahfree", UpstreamModel: "coding-glm-5.3-flash-free", EnvVar: "VOIE_AIHUBMIX_KEY"},
	{Name: "anyapi", Label: "AnyAPI", CompletionsURL: "https://api.anyapi.ai/v1/chat/completions", ModelsURL: "https://api.anyapi.ai/v1/models", Alias: "anyfree", UpstreamModel: "qwen/qwen3.8-27b:free", EnvVar: "VOIE_ANYAPI_KEY"},
	{Name: "beatapi", Label: "BeatAPI", CompletionsURL: "https://api.beatapi.io/v1/chat/completions", ModelsURL: "https://api.beatapi.io/v1/text/models", Alias: "beatfree", UpstreamModel: "glm-5.3-flash-free", EnvVar: "VOIE_BEATAPI_KEY"},
	{Name: "iflytek-spark", Label: "iFlytek Spark Lite", CompletionsURL: "https://spark-api-open.xf-yun.com/v1/chat/completions", ModelsURL: "https://spark-api-open.xf-yun.com/v1/models", Alias: "sparkfree", UpstreamModel: "lite", EnvVar: "VOIE_IFLYTEK_SPARK_KEY"},
	{Name: "vlm-run", Label: "VLM Run Gateway", CompletionsURL: "https://gateway.vlm.run/v1/openai/chat/completions", ModelsURL: "https://gateway.vlm.run/v1/openai/models", Alias: "vlmfree", UpstreamModel: "qwen/qwen3.5-0.8b", EnvVar: "VOIE_VLM_RUN_KEY"},
	{Name: "runcrate", Label: "Runcrate", CompletionsURL: "https://api.runcrate.ai/v1/chat/completions", ModelsURL: "https://api.runcrate.ai/v1/models", Alias: "runfree", UpstreamModel: "deepseek-ai/DeepSeek-V4.1-Flash", EnvVar: "VOIE_RUNCRATE_KEY"},
}

const cloudflareAccountKey = "cloudflare-ai-account"

func cloudflareAccount() string {
	if account := strings.TrimSpace(os.Getenv("VOIE_CLOUDFLARE_ACCOUNT_ID")); account != "" {
		return account
	}
	store, err := config.LoadCredentials()
	if err == nil {
		if account := store.Raw(cloudflareAccountKey); account != "" {
			return account
		}
	}
	return ""
}

func cloudflareURL(path string) string {
	account := cloudflareAccount()
	if account == "" {
		account = "ACCOUNT_ID"
	}
	return "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/v1/" + path
}

// ConnectField is one value `voie connect` must collect for a provider.
type ConnectField struct {
	Key     string // credentials key
	Label   string
	Secret  bool
	Default string
}

// ConnectFields returns the credentials a provider needs. Most need only a
// token; Cloudflare Workers AI also needs an account id embedded in its URL.
func ConnectFields(provider string) []ConnectField {
	if provider == "cloudflare-ai" {
		return []ConnectField{
			{Key: provider, Label: "API token", Secret: true},
			{Key: cloudflareAccountKey, Label: "Cloudflare account ID", Secret: false},
		}
	}
	label := "API token"
	secret := true
	if localKeyDefault(provider) != "" {
		label = "Local gateway key (Enter for voie-local)"
		secret = false
	}
	return []ConnectField{{Key: provider, Label: label, Secret: secret, Default: localKeyDefault(provider)}}
}

func localKeyDefault(provider string) string {
	switch provider {
	case "opencode-zen", "opencode-go", "antigravity", "kiro", "cline":
		return "voie-local"
	}
	if _, ok := ACPProviderSpecByName(provider); ok {
		return "voie-local"
	}
	if _, ok := ExternalGatewayByName(provider); ok {
		return "voie-local"
	}
	return ""
}

// anonymousToken is the documented no-signup credential for a few providers.
func anonymousToken(provider string) string {
	switch provider {
	case "vlm-run":
		return "vlmrun"
	default:
		return ""
	}
}

// tokenErrorHint adds a reconnect hint only for authentication failures.
// Rate limits and transient errors already carry their own status in the
// typed ProviderFailure and must not suggest replacing a valid token.
func tokenErrorHint(err error, provider string) error {
	var failure *app.ProviderFailure
	if errors.As(err, &failure) && (failure.Status == http.StatusUnauthorized || failure.Status == http.StatusForbidden) {
		return fmt.Errorf("%w (run `voie connect %s` if the token was rejected)", err, provider)
	}
	return err
}

// TokenProvider is one instance of the declarative catalogue.
type TokenProvider struct {
	*openAIKeylessProvider
	spec TokenProviderSpec
}

func NewTokenProvider(spec TokenProviderSpec) *TokenProvider {
	cfg := openAIKeylessConfig{
		Name:           spec.Name,
		Label:          spec.Label,
		URL:            spec.ModelsURL,
		CompletionsURL: spec.CompletionsURL,
		DefaultModel:   spec.Alias,
		ModelIDs:       map[string]string{spec.Alias: spec.UpstreamModel},
		Description:    spec.Label + " via a configured API token",
		SupportsStream: true,
		TokenSupplier: func() string {
			store, _ := config.LoadCredentials()
			value := store.Token(spec.Name)
			if value == "" && (spec.Name == "vlm-run") {
				return anonymousToken(spec.Name)
			}
			return value
		},
	}
	cfg.ModelsSupplier = func() map[string]string { return catalog.AllModels(spec.Name) }
	if spec.Name == "cloudflare-ai" {
		cfg.URLSupplier = func() string { return cloudflareURL("models") }
		cfg.CompletionsURLSupplier = func() string { return cloudflareURL("chat/completions") }
	}
	return &TokenProvider{openAIKeylessProvider: newOpenAIKeylessProvider(cfg, &http.Client{Timeout: openAIKeylessTimeout}), spec: spec}
}

func (p *TokenProvider) token() string {
	store, _ := config.LoadCredentials()
	value := strings.TrimSpace(store.Token(p.spec.Name))
	if value == "" {
		return anonymousToken(p.spec.Name)
	}
	return value
}

func (p *TokenProvider) SupportsModel(model string) bool {
	return p.token() != "" && p.openAIKeylessProvider.SupportsModel(model)
}

func (p *TokenProvider) GetInfo() app.ProviderInfo {
	info := p.openAIKeylessProvider.GetInfo()
	free := sortedKeys(catalog.Models(p.spec.Name))
	// The declared alias is a documented free entry even when the remote
	// catalogue is offline or excludes it for other reasons.
	if p.spec.Alias != "" {
		free = append(free, p.spec.Alias)
		sort.Strings(free)
	}
	info.FreeModels = free
	info.Working = p.token() != ""
	return info
}

func sortedKeys(models map[string]string) []string {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TokenProviderNames lists every declarative token provider.
func TokenProviderNames() []string {
	names := make([]string, 0, len(TokenProviders))
	for _, spec := range TokenProviders {
		names = append(names, spec.Name)
	}
	return names
}

func TokenProviderModelsURL(name string) string {
	for _, spec := range TokenProviders {
		if spec.Name == name {
			return spec.ModelsURL
		}
	}
	return ""
}
