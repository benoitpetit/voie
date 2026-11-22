package app

import "strings"

// modelPrefixes gives every provider a short, stable prefix for its model IDs.
// Prefixes already present in upstream IDs (or-, oc2a-, blockrun-, ...) are
// reused so the public catalogue does not change for those providers.
var modelPrefixes = map[string]string{
	"perplexity":       "pplx",
	"duckai":           "dk",
	"deepai":           "dp",
	"quillbot":         "qb",
	"yqcloud":          "yq",
	"jimmy":            "jm",
	"opencode-zen":     "oc2a",
	"opencode-go":      "oc2ago",
	"openrouter":       "or",
	"orcarouter":       "orc",
	"groq":             "gq",
	"google-ai-studio": "gs",
	"nvidia-nim":       "nv",
	"zai":              "zai",
	"mistral":          "mi",
	"cloudflare-ai":    "cf",
	"cohere-trial":     "coh",
	"requesty":         "rq",
	"aihubmix":         "ah",
	"kilo":             "kilo",
	"ovh":              "ovh",
	"blockrun":         "blockrun",
	"pollinations":     "pll",
	"antigravity":      "ag",
	"kiro":             "ki",
	"cline":            "cl",
	"codebuddy":        "cb",
	"minimax-code":     "mm",
	"devin":            "dv",
	"qoder":            "qd",
	"copilot-cli":      "cp",
	"cursor":           "cu",
	"omniroute":        "om",
	"qoder2api":        "q2",
	"windsurfapi":      "ws",
	"copilot-api":      "ca",
	"cursor-api-proxy": "cr",
	"codearts2api":     "cs",
	"cli2api":          "cl2",
	"workbuddy2api":    "wb",
}

// PrefixModelID returns the public model ID for a provider. IDs that already
// carry the provider prefix are left intact.
func PrefixModelID(provider, id string) string {
	prefix := modelPrefixes[provider]
	if prefix == "" || strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + "-" + id
}

// NormalizeModelID strips a known provider prefix so legacy and prefixed IDs
// both keep working at call sites.
func NormalizeModelID(id string) string {
	index := strings.Index(id, "-")
	if index <= 0 {
		return id
	}
	prefix := id[:index]
	for _, known := range modelPrefixes {
		if known == prefix {
			return id[index+1:]
		}
	}
	return id
}
