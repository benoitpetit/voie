package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

var taskNames = map[string]struct{}{
	"coding": {}, "reasoning": {}, "writing": {}, "translation": {},
	"summarization": {}, "general": {},
}

var capabilityNames = map[string]struct{}{
	"coding": {}, "reasoning": {}, "writing": {}, "translation": {},
	"summarization": {}, "streaming": {},
}

type RoutingPolicy struct {
	Models map[string]ModelDescriptor `json:"models"`
	Tasks  map[string]TaskRule        `json:"tasks"`
	// Fallback overrides the global fallback policy per routing model. Its
	// pointer fields (enabled, max_retries, max_fallback_models) override
	// environment and defaults; Models maps a routed model ID to an ordered
	// list of fallback model IDs.
	Fallback *RoutingFallback `json:"fallback,omitempty"`
}

type RoutingFallback struct {
	Enabled           *bool               `json:"enabled,omitempty"`
	MaxRetries        *int                `json:"max_retries,omitempty"`
	MaxFallbackModels *int                `json:"max_fallback_models,omitempty"`
	Models            map[string][]string `json:"models,omitempty"`
}

type ModelDescriptor struct {
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type TaskRule struct {
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	PreferredModels      []string `json:"preferred_models,omitempty"`
}

func LoadRoutingPolicy(path string) (RoutingPolicy, error) {
	policy := RoutingPolicy{Models: make(map[string]ModelDescriptor), Tasks: make(map[string]TaskRule)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return policy, nil
	}
	if err != nil {
		return RoutingPolicy{}, fmt.Errorf("read routing policy %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		return RoutingPolicy{}, fmt.Errorf("parse routing policy %q: %w", path, err)
	}
	if policy.Models == nil {
		policy.Models = make(map[string]ModelDescriptor)
	}
	if policy.Tasks == nil {
		policy.Tasks = make(map[string]TaskRule)
	}
	return policy, nil
}

func ValidateRoutingPolicy(policy RoutingPolicy, registeredModels map[string]struct{}) error {
	for model, descriptor := range policy.Models {
		if !isRegisteredModel(registeredModels, model) {
			return fmt.Errorf("routing policy references unknown model %q", model)
		}
		if err := validateCapabilities(descriptor.Capabilities); err != nil {
			return fmt.Errorf("routing policy model %q: %w", model, err)
		}
	}
	for task, rule := range policy.Tasks {
		if _, ok := taskNames[task]; !ok {
			return fmt.Errorf("routing policy contains unknown task %q", task)
		}
		if err := validateCapabilities(rule.RequiredCapabilities); err != nil {
			return fmt.Errorf("routing policy task %q: %w", task, err)
		}
		for _, model := range rule.PreferredModels {
			if !isRegisteredModel(registeredModels, model) {
				return fmt.Errorf("routing policy task %q references unknown model %q", task, model)
			}
		}
	}
	if policy.Fallback != nil {
		if err := validateRoutingFallback(policy.Fallback, registeredModels); err != nil {
			return fmt.Errorf("routing policy fallback: %w", err)
		}
	}
	return nil
}

func validateRoutingFallback(fallback *RoutingFallback, registeredModels map[string]struct{}) error {
	if err := validateFallbackBound("max_retries", fallback.MaxRetries); err != nil {
		return err
	}
	if err := validateFallbackBound("max_fallback_models", fallback.MaxFallbackModels); err != nil {
		return err
	}
	original := fallback.Models
	fallback.Models = make(map[string][]string, len(original))
	for key, models := range original {
		key = strings.ToLower(strings.TrimSpace(key))
		if !isRegisteredModel(registeredModels, key) {
			return fmt.Errorf("references unknown model %q", key)
		}
		normalized := make([]string, 0, len(models))
		seen := make(map[string]struct{}, len(models))
		for _, model := range models {
			model = strings.ToLower(strings.TrimSpace(model))
			if model == "" {
				return fmt.Errorf("model %q contains an empty fallback model id", key)
			}
			if !isRegisteredModel(registeredModels, model) {
				return fmt.Errorf("model %q references unknown fallback model %q", key, model)
			}
			if _, ok := seen[model]; ok {
				return fmt.Errorf("model %q contains duplicate fallback model %q", key, model)
			}
			seen[model] = struct{}{}
			normalized = append(normalized, model)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("model %q must not reference itself in its fallback list", key)
		}
		fallback.Models[key] = normalized
	}
	return nil
}

func validateFallbackBound(name string, value *int) error {
	if value == nil || (*value >= 0 && *value <= 3) {
		return nil
	}
	return fmt.Errorf("%s must be between 0 and 3", name)
}

func isRegisteredModel(registeredModels map[string]struct{}, model string) bool {
	_, ok := registeredModels[strings.ToLower(strings.TrimSpace(model))]
	return ok
}

func validateCapabilities(capabilities []string) error {
	for _, capability := range capabilities {
		if _, ok := capabilityNames[capability]; !ok {
			return fmt.Errorf("unknown capability %q; supported capabilities: %s", capability, strings.Join(sortedKeys(capabilityNames), ", "))
		}
	}
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
