package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestPollinationsMetadataAndModelSupport(t *testing.T) {
	info := NewPollinations().GetInfo()
	if info.Name != "pollinations" || info.DefaultModel != "pollinations-openai-fast" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if want := []string{"pollinations-openai-fast"}; !reflect.DeepEqual(info.SupportedModels, want) {
		t.Errorf("SupportedModels = %v, want %v", info.SupportedModels, want)
	}
	if !info.Working || info.NeedsAuth || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected Pollinations capabilities in GetInfo(): %#v", info)
	}
}

func TestPollinationsMapsFriendlyIDToUpstreamAndKeepsResponseID(t *testing.T) {
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != "https://text.pollinations.ai/openai" {
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
		if payload.Model != "openai-fast" {
			t.Errorf("payload model = %q, want openai-fast", payload.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"gpt-oss-20b","choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))

	response, err := NewPollinations().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "pollinations-openai-fast")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "pollinations-openai-fast" || response.Choices[0].Message.Content != "OK" {
		t.Fatalf("response model/content = %q/%q, want pollinations-openai-fast/OK", response.Model, response.Choices[0].Message.Content)
	}
}

func TestPollinationsClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "pollinations-internal-body"
			useProviderResponse(t, tc.status, secret)
			_, err := NewPollinations().ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "pollinations-openai-fast")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
}

func TestPollinationsNetworkFailureIsTransient(t *testing.T) {
	useProviderNetworkFailure(t)

	_, err := NewPollinations().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "pollinations-openai-fast")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
