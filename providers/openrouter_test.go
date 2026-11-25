package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benoitpetit/voie/internal/pricing"
)

func TestOpenRouterExposesPublicPaidModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5","pricing":{"prompt":"0.000002","completion":"0.000006"}},{"id":"openrouter/free","pricing":{"prompt":"0","completion":"0"}}]}`))
	}))
	defer server.Close()
	pricing.SetSource(server.URL)
	t.Cleanup(func() { pricing.SetSource("") })
	info := NewOpenRouter().GetInfo()
	found := false
	for _, id := range info.SupportedModels {
		if id == "or-openai/gpt-5" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SupportedModels missing paid model: %v", info.SupportedModels)
	}
	freeIDs := map[string]bool{}
	for _, id := range info.FreeModels {
		freeIDs[id] = true
	}
	if freeIDs["or-openai/gpt-5"] {
		t.Fatal("paid model listed as free")
	}
	if !freeIDs["or-openrouter/free"] {
		t.Fatal("concrete ID of a free upstream must also be free")
	}
}
