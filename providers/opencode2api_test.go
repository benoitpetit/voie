package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestOpenCode2APIMetadataAndModelSupport(t *testing.T) {
	info := NewOpenCode2API().GetInfo()
	if info.Name != "opencode2api" || info.DefaultModel != "oc2a-hy4-flash-free" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if len(info.SupportedModels) != 11 {
		t.Errorf("SupportedModels = %d, want 11: %v", len(info.SupportedModels), info.SupportedModels)
	}
	if !info.Working || info.NeedsAuth || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected OpenCode2API capabilities in GetInfo(): %#v", info)
	}
	provider := NewOpenCode2API()
	if !provider.SupportsModel("OC2A-HY4-FLASH-FREE") {
		t.Error("SupportsModel rejected oc2a-hy4-flash-free")
	}
	if provider.SupportsModel("hy4-flash-free") {
		t.Error("SupportsModel accepted an upstream ID without the voie prefix")
	}
}

func TestOpenCode2APIMapsFriendlyIDToGatewayAndKeepsResponseID(t *testing.T) {
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
		if payload.Model != "hy4-flash-free" {
			t.Errorf("payload model = %q, want hy4-flash-free", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"hy4-flash-free","choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))

	response, err := NewOpenCode2API().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "oc2a-hy4-flash-free")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "oc2a-hy4-flash-free" || response.Choices[0].Message.Content != "OK" {
		t.Fatalf("response model/content = %q/%q, want oc2a-hy4-flash-free/OK", response.Model, response.Choices[0].Message.Content)
	}
}

func TestOpenCode2APIClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "gateway-internal-body"
			useKeylessProviderResponse(t, tc.status, secret)
			_, err := NewOpenCode2API().ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "oc2a-hy4-flash-free")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestOpenCode2APINetworkFailureIsTransient(t *testing.T) {
	useKeylessProviderNetworkFailure(t)

	_, err := NewOpenCode2API().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "oc2a-hy4-flash-free")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
