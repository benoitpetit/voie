// Package catalog maintains the free-model catalogue from the
// awesome-free-ai-coding registry. It refreshes automatically on first use,
// caches the result on disk, and falls back to the embedded snapshot offline.
package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultSourceURL = "https://raw.githubusercontent.com/mvalentsev/awesome-free-ai-coding/main/registry.yaml"

var (
	sourceOverride string
	remoteDisabled bool
)

// SetSource points the catalogue at another registry (used by tests). An
// empty URL disables remote refresh and keeps the embedded snapshot.
func SetSource(url string) {
	mu.Lock()
	defer mu.Unlock()
	sourceOverride = url
	remoteDisabled = url == ""
	current = nil
}

func source() string {
	if remoteDisabled {
		return ""
	}
	if sourceOverride != "" {
		return sourceOverride
	}
	return defaultSourceURL
}

const refreshInterval = 6 * time.Hour

// registry id -> (voie provider name, alias prefix)
var providerAliases = map[string][2]string{
	"openrouter-free":       {"openrouter", "or"},
	"orcarouter":            {"orcarouter", "orc"},
	"groq-free":             {"groq", "gq"},
	"google-ai-studio":      {"google-ai-studio", "gs"},
	"nvidia-nim":            {"nvidia-nim", "nv"},
	"zai-glm":               {"zai", "zai"},
	"mistral-free":          {"mistral", "mi"},
	"cloudflare-workers-ai": {"cloudflare-ai", "cf"},
	"cohere":                {"cohere-trial", "coh"},
	"requesty":              {"requesty", "rq"},
	"aihubmix":              {"aihubmix", "ah"},
	"anyapi":                {"anyapi", "any"},
	"beatapi":               {"beatapi", "beat"},
	"iflytek-spark":         {"iflytek-spark", "spark"},
	"together-ai":           {"together", "tg"},
	"vlm-run-gateway":       {"vlm-run", "vlm"},
}

type registryEntry struct {
	ID  string `yaml:"id"`
	API struct {
		ModelIDs []string `yaml:"model_ids"`
	} `yaml:"api"`
}

type snapshot struct {
	FetchedAt time.Time                    `json:"fetched_at"`
	Models    map[string]map[string]string `json:"models"`
}

var (
	mu          sync.Mutex
	current     *snapshot
	slugPattern = regexp.MustCompile(`[^a-z0-9]+`)
)

func cachePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "registry-cache.json")
}

// Models returns the free-model aliases for a provider, refreshing the
// catalogue from the registry when the cached snapshot is stale.
func Models(provider string) map[string]string {
	snap := load()
	result := make(map[string]string)
	for alias, upstream := range embeddedModels[provider] {
		result[alias] = upstream
	}
	if snap != nil {
		for alias, upstream := range snap.Models[provider] {
			result[alias] = upstream
		}
	}
	return result
}

func load() *snapshot {
	mu.Lock()
	defer mu.Unlock()
	if current != nil && time.Since(current.FetchedAt) < refreshInterval {
		return current
	}
	if cached := readCache(); cached != nil && time.Since(cached.FetchedAt) < refreshInterval {
		current = cached
		return current
	}
	fresh, err := fetch()
	if err != nil {
		if cached := readCache(); cached != nil {
			current = cached
			return current
		}
		return nil
	}
	current = fresh
	writeCache(fresh)
	return fresh
}

func fetch() (*snapshot, error) {
	url := source()
	if url == "" {
		return nil, fmt.Errorf("remote catalogue disabled")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errBadStatus(resp.StatusCode)
	}
	var entries struct {
		Entries []registryEntry `yaml:"entries"`
	}
	if err := yaml.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	models := make(map[string]map[string]string)
	for _, entry := range entries.Entries {
		mapping, ok := providerAliases[entry.ID]
		if !ok {
			continue
		}
		for _, upstream := range entry.API.ModelIDs {
			alias := mapping[1] + "-" + slug(upstream)
			if models[mapping[0]] == nil {
				models[mapping[0]] = make(map[string]string)
			}
			models[mapping[0]][alias] = upstream
		}
	}
	return &snapshot{FetchedAt: time.Now(), Models: models}, nil
}

type errBadStatus int

func (e errBadStatus) Error() string {
	return fmt.Sprintf("registry fetch failed with HTTP %d", int(e))
}

func slug(value string) string {
	return strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(value), "-"), "-")
}

func readCache() *snapshot {
	path := cachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var snap snapshot
	if json.Unmarshal(data, &snap) != nil || snap.Models == nil {
		return nil
	}
	return &snap
}

func writeCache(snap *snapshot) {
	path := cachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}
