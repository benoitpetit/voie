package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestOVHMetadataAndModelSupport(t *testing.T) {
	info := NewOVH().GetInfo()
	if info.Name != "ovh" || info.DefaultModel != "ovh-qwen3.8-27b" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if len(info.SupportedModels) != 3 {
		t.Errorf("SupportedModels = %d, want 3: %v", len(info.SupportedModels), info.SupportedModels)
	}
	if !info.Working || info.NeedsAuth || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected OVH capabilities in GetInfo(): %#v", info)
	}
	if !NewOVH().SupportsModel("OVH-QWEN3.8-27B") {
		t.Error("SupportsModel rejected ovh-qwen3.8-27b")
	}
}

func TestOVHMapsFriendlyIDToUpstreamAndKeepsResponseID(t *testing.T) {
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1/chat/completions" {
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
		if payload.Model != "Qwen3.8-27B" {
			t.Errorf("payload model = %q, want Qwen3.8-27B", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"Qwen3.8-27B","choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))

	response, err := NewOVH().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "ovh-qwen3.8-27b")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "ovh-qwen3.8-27b" || response.Choices[0].Message.Content != "OK" {
		t.Fatalf("response model/content = %q/%q, want ovh-qwen3.8-27b/OK", response.Model, response.Choices[0].Message.Content)
	}
}

func TestOVHClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "ovh-internal-body"
			useKeylessProviderResponse(t, tc.status, secret)
			_, err := NewOVH().ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "ovh-qwen3.8-27b")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestOVHNetworkFailureIsTransient(t *testing.T) {
	useKeylessProviderNetworkFailure(t)

	_, err := NewOVH().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "ovh-qwen3.8-27b")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
