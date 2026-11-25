package clinebridge

import (
	"testing"
)

func TestParseModelsFromSessionResult(t *testing.T) {
	result := map[string]any{
		"models": map[string]any{
			"availableModels": []any{
				map[string]any{"modelId": "gpt-6-luna", "name": "GPT-6 Luna", "description": "x0.07"},
				map[string]any{"modelId": "hy3", "name": "Hy3"},
			},
		},
	}
	models := ParseModels(result)
	if len(models) != 2 || models[0].ID != "gpt-6-luna" || models[0].Name != "GPT-6 Luna" {
		t.Fatalf("models = %+v", models)
	}
}

func TestModelCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	models := []ACPModel{{ID: "gpt-6-luna", Name: "GPT-6 Luna"}}
	if err := WriteModelCache("codebuddy", models); err != nil {
		t.Fatal(err)
	}
	got, err := ReadModelCache("codebuddy")
	if err != nil || len(got) != 1 || got[0].ID != "gpt-6-luna" {
		t.Fatalf("got = %+v, %v", got, err)
	}
}
