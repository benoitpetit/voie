package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestBlockRunMetadataAndModelSupport(t *testing.T) {
	info := NewBlockRun().GetInfo()
	if info.Name != "blockrun" || info.DefaultModel != "blockrun-nemotron-3.5-bolt" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if len(info.SupportedModels) != 4 {
		t.Errorf("SupportedModels = %d, want 4: %v", len(info.SupportedModels), info.SupportedModels)
	}
	if !info.Working || info.NeedsAuth || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected BlockRun capabilities in GetInfo(): %#v", info)
	}
}

func TestBlockRunMapsLightningAndKeepsResponseID(t *testing.T) {
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != "https://blockrun.ai/api/v1/chat/completions" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		var payload struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Stream    bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Model != "nvidia/nemotron-3.5-lightning" || payload.MaxTokens != 1024 || payload.Stream {
			t.Errorf("payload = %#v, want lightning, 1024 tokens, no stream", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"nvidia/nemotron-3.5-lightning","choices":[{"message":{"role":"assistant","content":"Hello! How can I help you today?","reasoning_content":"hidden reasoning"}}]}`)
	}))

	response, err := NewBlockRun().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "blockrun-nemotron-3.5-lightning")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "blockrun-nemotron-3.5-lightning" {
		t.Errorf("response.Model = %q, want voie ID", response.Model)
	}
	content := response.Choices[0].Message.Content
	if content != "Hello! How can I help you today?" || strings.Contains(content, "hidden reasoning") {
		t.Fatalf("response content = %q, want the answer without reasoning", content)
	}
}

func TestBlockRunStreamsLightningDeltas(t *testing.T) {
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		var payload struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if !payload.Stream {
			t.Error("payload stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hidden\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))

	var chunks []string
	err := NewBlockRun().ChatCompletionStream(testProviderContext(),
		[]Message{{Role: "user", Content: "Hello"}}, "blockrun-nemotron-3.5-lightning", func(chunk string) {
			chunks = append(chunks, chunk)
		})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"OK"}; !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v, want %#v", chunks, want)
	}
}

func TestBlockRunClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "blockrun-internal-body"
			useProviderResponse(t, tc.status, secret)
			_, err := NewBlockRun().ChatCompletion(testProviderContext(),
				[]Message{{Role: "user", Content: "hello"}}, "blockrun-nemotron-3.5-lightning")
			classifyFailure(t, err, tc.category, tc.status, secret)
		})
	}
	t.Run("capacity exhausted", func(t *testing.T) {
		const secret = "Free model capacity exhausted"
		useProviderResponse(t, http.StatusTooManyRequests, secret)
		_, err := NewBlockRun().ChatCompletion(testProviderContext(),
			[]Message{{Role: "user", Content: "hello"}}, "blockrun-nemotron-3.5-lightning")
		classifyFailure(t, err, app.FailureTransient, http.StatusTooManyRequests, secret)
	})
}

func TestBlockRunNetworkFailureIsTransient(t *testing.T) {
	useProviderNetworkFailure(t)

	_, err := NewBlockRun().ChatCompletion(testProviderContext(),
		[]Message{{Role: "user", Content: "hello"}}, "blockrun-nemotron-3.5-lightning")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
