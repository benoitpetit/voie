package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestOpenCodeGoMetadataAndModelSupport(t *testing.T) {
	provider := NewOpenCodeGo()
	info := provider.GetInfo()
	if info.Name != "opencode-go" || info.Label != "OpenCode Go" || info.DefaultModel != "oc2ago-gpt-6-luna" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if len(info.SupportedModels) != 20 {
		t.Errorf("SupportedModels = %d, want 20: %v", len(info.SupportedModels), info.SupportedModels)
	}
	if !info.Working || info.NeedsAuth || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected OpenCode Go capabilities in GetInfo(): %#v", info)
	}
	if !strings.Contains(info.Description, "Go API key or OAuth") {
		t.Errorf("Description = %q, want explicit Go credential requirement", info.Description)
	}
	if !provider.SupportsModel("OC2AGO-GPT-6-LUNA") || provider.SupportsModel("gpt-6-luna") {
		t.Error("SupportsModel must accept prefixed voie IDs and reject bare upstream IDs")
	}
}

func TestOpenCodeGoMapsFriendlyIDToGatewayAndKeepsResponseID(t *testing.T) {
	t.Setenv("OPENCODE2API_KEY", "test-gateway-key")
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != "http://127.0.0.1:8090/v1/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-gateway-key" {
			t.Errorf("Authorization = %q, want Bearer test-gateway-key", got)
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Model != "gpt-6-luna" {
			t.Errorf("payload model = %q, want gpt-6-luna", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"gpt-6-luna","choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))

	response, err := NewOpenCodeGo().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "oc2ago-gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "oc2ago-gpt-6-luna" || response.Choices[0].Message.Content != "OK" {
		t.Fatalf("response model/content = %q/%q, want oc2ago-gpt-6-luna/OK", response.Model, response.Choices[0].Message.Content)
	}
}

func TestOpenCodeGoClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "gateway-internal-body"
			useKeylessProviderResponse(t, tc.status, secret)
			_, err := NewOpenCodeGo().ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "oc2ago-gpt-6-luna")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestOpenCodeGoNetworkFailureIsTransient(t *testing.T) {
	useKeylessProviderNetworkFailure(t)

	_, err := NewOpenCodeGo().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "oc2ago-gpt-6-luna")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
