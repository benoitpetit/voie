package config

import (
	"os"
	"path/filepath"
	"reflect"
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
