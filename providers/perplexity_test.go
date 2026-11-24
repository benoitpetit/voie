package providers

import "testing"

func TestPerplexityExposesPaidModelsAsPaid(t *testing.T) {
	info := (&Perplexity{}).GetInfo()
	found := false
	for _, model := range info.SupportedModels {
		if model == "sonar-pro" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SupportedModels = %v, want sonar-pro", info.SupportedModels)
	}
	if len(info.FreeModels) != 1 || info.FreeModels[0] != "turbo" {
		t.Fatalf("FreeModels = %v, want only turbo", info.FreeModels)
	}
}
