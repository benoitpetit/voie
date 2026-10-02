package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadRoutingPolicyValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	data := `{"models":{"provider/model":{"description":"General assistant","capabilities":["writing","reasoning"]}},"tasks":{"coding":{"required_capabilities":["coding"],"preferred_models":["provider/model"]}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadRoutingPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Models["provider/model"].Description != "General assistant" || !reflect.DeepEqual(policy.Models["provider/model"].Capabilities, []string{"writing", "reasoning"}) {
		t.Fatalf("model policy = %+v", policy.Models["provider/model"])
	}
	if !reflect.DeepEqual(policy.Tasks["coding"].RequiredCapabilities, []string{"coding"}) || !reflect.DeepEqual(policy.Tasks["coding"].PreferredModels, []string{"provider/model"}) {
		t.Fatalf("task policy = %+v", policy.Tasks["coding"])
	}
}

func TestLoadRoutingPolicyAllowsMissingFile(t *testing.T) {
	policy, err := LoadRoutingPolicy(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Models) != 0 || len(policy.Tasks) != 0 {
		t.Fatalf("missing-file policy = %+v, want empty policy", policy)
	}
}

func TestLoadRoutingPolicyRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoutingPolicy(path); err == nil {
		t.Fatal("LoadRoutingPolicy accepted malformed JSON")
	}
}

func TestValidateRoutingPolicyRejectsUnknownReferences(t *testing.T) {
	known := map[string]struct{}{"provider/model": {}}
	cases := []struct {
		name   string
		policy RoutingPolicy
	}{
		{name: "model reference", policy: RoutingPolicy{Tasks: map[string]TaskRule{"coding": {PreferredModels: []string{"missing/model"}}}}},
		{name: "task name", policy: RoutingPolicy{Tasks: map[string]TaskRule{"unsupported": {}}}},
		{name: "model capability", policy: RoutingPolicy{Models: map[string]ModelDescriptor{"provider/model": {Capabilities: []string{"unknown"}}}}},
		{name: "task capability", policy: RoutingPolicy{Tasks: map[string]TaskRule{"coding": {RequiredCapabilities: []string{"unknown"}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRoutingPolicy(tc.policy, known); err == nil {
				t.Fatal("ValidateRoutingPolicy accepted an unknown reference")
			}
		})
	}
}

func fallbackIntPtr(value int) *int { return &value }

func TestLoadRoutingPolicyParsesFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	data := `{"fallback":{"enabled":false,"max_retries":2,"max_fallback_models":1,"models":{"provider/model":["test/model"]}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadRoutingPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Fallback == nil {
		t.Fatal("fallback not parsed")
	}
	if policy.Fallback.Enabled == nil || *policy.Fallback.Enabled {
		t.Fatalf("fallback enabled = %v, want false", policy.Fallback.Enabled)
	}
	if policy.Fallback.MaxRetries == nil || *policy.Fallback.MaxRetries != 2 {
		t.Fatalf("fallback max_retries = %v, want 2", policy.Fallback.MaxRetries)
	}
	if policy.Fallback.MaxFallbackModels == nil || *policy.Fallback.MaxFallbackModels != 1 {
		t.Fatalf("fallback max_fallback_models = %v, want 1", policy.Fallback.MaxFallbackModels)
	}
	if len(policy.Fallback.Models["provider/model"]) != 1 || policy.Fallback.Models["provider/model"][0] != "test/model" {
		t.Fatalf("fallback models = %+v", policy.Fallback.Models)
	}
}

func TestLoadRoutingPolicyFallbackOmittedValuesStayNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	if err := os.WriteFile(path, []byte(`{"fallback":{"models":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadRoutingPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Fallback == nil || policy.Fallback.Enabled != nil || policy.Fallback.MaxRetries != nil || policy.Fallback.MaxFallbackModels != nil {
		t.Fatalf("omitted fallback fields must stay nil: %+v", policy.Fallback)
	}
}

func TestValidateRoutingPolicyFallbackAcceptsAndNormalizes(t *testing.T) {
	known := map[string]struct{}{"provider/model": {}, "test/model": {}}
	enabled := true
	policy := RoutingPolicy{Fallback: &RoutingFallback{
		Enabled:           &enabled,
		MaxRetries:        fallbackIntPtr(2),
		MaxFallbackModels: fallbackIntPtr(1),
		Models:            map[string][]string{"Provider/Model": {"TEST/model"}},
	}}
	if err := ValidateRoutingPolicy(policy, known); err != nil {
		t.Fatal(err)
	}
	if _, ok := policy.Fallback.Models["provider/model"]; !ok {
		t.Fatalf("fallback key not normalized: %+v", policy.Fallback.Models)
	}
	if policy.Fallback.Models["provider/model"][0] != "test/model" {
		t.Fatalf("fallback entry not normalized: %+v", policy.Fallback.Models)
	}
}

func TestValidateRoutingPolicyAcceptsEmptyFallback(t *testing.T) {
	policy := RoutingPolicy{}
	if err := ValidateRoutingPolicy(policy, map[string]struct{}{}); err != nil {
		t.Fatalf("empty routing policy rejected: %v", err)
	}
}

func TestValidateRoutingPolicyFallbackRejectsOutOfRangeBounds(t *testing.T) {
	known := map[string]struct{}{"provider/model": {}}
	cases := []struct {
		name   string
		policy RoutingPolicy
	}{
		{name: "max_retries too high", policy: RoutingPolicy{Fallback: &RoutingFallback{MaxRetries: fallbackIntPtr(4)}}},
		{name: "max_retries negative", policy: RoutingPolicy{Fallback: &RoutingFallback{MaxRetries: fallbackIntPtr(-1)}}},
		{name: "max_fallback_models too high", policy: RoutingPolicy{Fallback: &RoutingFallback{MaxFallbackModels: fallbackIntPtr(4)}}},
		{name: "max_fallback_models negative", policy: RoutingPolicy{Fallback: &RoutingFallback{MaxFallbackModels: fallbackIntPtr(-1)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRoutingPolicy(tc.policy, known)
			if err == nil || !strings.Contains(err.Error(), "fallback") {
				t.Fatalf("error = %v, want fallback bounds error", err)
			}
		})
	}
}

func TestValidateRoutingPolicyFallbackRejectsInvalidModels(t *testing.T) {
	known := map[string]struct{}{"provider/model": {}, "test/model": {}}
	cases := []struct {
		name   string
		policy RoutingPolicy
	}{
		{name: "unknown key", policy: RoutingPolicy{Fallback: &RoutingFallback{Models: map[string][]string{"missing/model": {"test/model"}}}}},
		{name: "unknown entry", policy: RoutingPolicy{Fallback: &RoutingFallback{Models: map[string][]string{"provider/model": {"missing/model"}}}}},
		{name: "duplicate entry", policy: RoutingPolicy{Fallback: &RoutingFallback{Models: map[string][]string{"provider/model": {"test/model", "test/model"}}}}},
		{name: "self reference", policy: RoutingPolicy{Fallback: &RoutingFallback{Models: map[string][]string{"provider/model": {"provider/model"}}}}},
		{name: "empty id", policy: RoutingPolicy{Fallback: &RoutingFallback{Models: map[string][]string{"provider/model": {"  "}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRoutingPolicy(tc.policy, known); err == nil {
				t.Fatalf("ValidateRoutingPolicy accepted %s", tc.name)
			}
		})
	}
}
