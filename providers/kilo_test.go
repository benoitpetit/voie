package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestKiloMetadataAndModelSupport(t *testing.T) {
	info := NewKilo().GetInfo()
	if info.Name != "kilo" || info.DefaultModel != "kilo-auto" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if len(info.SupportedModels) != 13 {
		t.Errorf("SupportedModels = %d, want 13: %v", len(info.SupportedModels), info.SupportedModels)
	}
	if !info.Working || info.NeedsAuth || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected Kilo capabilities in GetInfo(): %#v", info)
	}
	provider := NewKilo()
	if !provider.SupportsModel("KILO-AUTO") || !provider.SupportsModel("kilo-glyph-cluster") || !provider.SupportsModel("kilo-beam-free") {
		t.Error("SupportsModel rejected a configured Kilo model")
	}
	if provider.SupportsModel("nemotron-ultra") {
		t.Error("SupportsModel accepted a voie ID instead of an upstream ID")
	}
}

func TestKiloMapsFriendlyIDToUpstreamAndKeepsResponseID(t *testing.T) {
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != "https://api.kilo.ai/api/gateway/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Model != "nvidia/nemotron-3-ultra-550b-a55b:free" {
			t.Errorf("payload model = %q, want upstream free ID", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"nvidia/nemotron-3-ultra-550b-a55b:free","choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))

	response, err := NewKilo().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "kilo-nemotron-ultra")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "kilo-nemotron-ultra" || response.Choices[0].Message.Content != "OK" {
		t.Fatalf("response model/content = %q/%q, want kilo-nemotron-ultra/OK", response.Model, response.Choices[0].Message.Content)
	}
}

func TestKiloClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "kilo-internal-body"
			useKeylessProviderResponse(t, tc.status, secret)
			_, err := NewKilo().ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "kilo-auto")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestKiloNetworkFailureIsTransient(t *testing.T) {
	useKeylessProviderNetworkFailure(t)

	_, err := NewKilo().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "kilo-auto")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
