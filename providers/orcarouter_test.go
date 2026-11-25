package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/benoitpetit/voie/internal/pricing"
)

func TestOrcaRouterExposesPublicPaidModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"anthropic/claude-fable-5","pricing":{"prompt":"0.00001","completion":"0.00005"}},
			{"id":"orcarouter/free","pricing":null}
		]}`))
	}))
	defer server.Close()
	pricing.RegisterSource("orcarouter", server.URL, func() string { return "tok-orca" })
	t.Cleanup(func() { pricing.RegisterSource("orcarouter", "", nil) })

	info := NewOrcaRouter().GetInfo()
	supported := map[string]bool{}
	for _, id := range info.SupportedModels {
		supported[id] = true
	}
	if !supported["orc-anthropic/claude-fable-5"] {
		t.Fatalf("paid catalogue model missing from SupportedModels: %v", info.SupportedModels)
	}
	free := map[string]bool{}
	for _, id := range info.FreeModels {
		free[id] = true
	}
	if free["orc-anthropic/claude-fable-5"] {
		t.Fatal("paid catalogue model must not be listed as free")
	}
	if !free["orcarouter/free"] {
		t.Fatalf("unpriced router alias must stay free: %v", info.FreeModels)
	}
	price, ok := pricing.Lookup("orcarouter", "anthropic/claude-fable-5")
	if !ok || price.Prompt != "0.00001" {
		t.Fatalf("price = %+v, %v", price, ok)
	}
}
