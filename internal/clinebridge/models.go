package clinebridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ACPModel is a model advertised by an ACP agent in its session/new result.
type ACPModel struct {
	ID                string `json:"id"`
	Name              string `json:"name,omitempty"`
	Description       string `json:"description,omitempty"`
	SupportsImages    bool   `json:"supports_images,omitempty"`
	SupportsReasoning bool   `json:"supports_reasoning,omitempty"`
}

// ParseModels extracts available models from a session/new result. Agents that
// expose only config options fall back to the model option values.
func ParseModels(result map[string]any) []ACPModel {
	models, _ := result["models"].(map[string]any)
	var out []ACPModel
	if available, ok := models["availableModels"].([]any); ok {
		for _, raw := range available {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["modelId"].(string)
			if id == "" {
				continue
			}
			name, _ := item["name"].(string)
			description, _ := item["description"].(string)
			meta, _ := item["_meta"].(map[string]any)
			images, _ := meta["supportsImages"].(bool)
			reasoning, _ := meta["supportsReasoning"].(bool)
			out = append(out, ACPModel{ID: id, Name: name, Description: description, SupportsImages: images, SupportsReasoning: reasoning})
		}
	}
	if len(out) > 0 {
		return out
	}
	options, _ := result["configOptions"].([]any)
	for _, raw := range options {
		option, ok := raw.(map[string]any)
		if !ok || option["id"] != "model" {
			continue
		}
		values, _ := option["options"].([]any)
		for _, entry := range values {
			value, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id, _ := value["value"].(string)
			if id == "" {
				continue
			}
			name, _ := value["name"].(string)
			out = append(out, ACPModel{ID: id, Name: name})
		}
	}
	return out
}

type modelCache struct {
	At     time.Time  `json:"at"`
	Models []ACPModel `json:"models"`
}

func modelCachePath(provider string) string {
	dir, err := os.UserConfigDir()
	if err != nil || provider == "" {
		return ""
	}
	return filepath.Join(dir, "voie", "gateways", provider, "models.json")
}

// WriteModelCache persists an agent's model catalogue for offline discovery.
func WriteModelCache(provider string, models []ACPModel) error {
	path := modelCachePath(provider)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(modelCache{At: time.Now(), Models: models})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ReadModelCache returns a catalogue no older than 24 hours.
func ReadModelCache(provider string) ([]ACPModel, error) {
	path := modelCachePath(provider)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cached modelCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, err
	}
	if time.Since(cached.At) > 24*time.Hour {
		return nil, nil
	}
	return cached.Models, nil
}
