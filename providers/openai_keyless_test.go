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

func keylessStubConfig() openAIKeylessConfig {
	return openAIKeylessConfig{
		Name:           "stub",
		Label:          "Stub",
		URL:            "https://example.test/models",
		CompletionsURL: "https://example.test/v1/chat/completions",
		DefaultModel:   "friendly",
		ModelIDs:       map[string]string{"friendly": "upstream-model"},
		Description:    "stub provider",
		SupportsStream: true,
	}
}

func TestOpenAIKeylessNonStreamingSendsNoAuthorizationAndKeepsVoieModelID(t *testing.T) {
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.String() != "https://example.test/v1/chat/completions" {
			t.Errorf("request = %s %s, want POST https://example.test/v1/chat/completions", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		var payload struct {
			Model     string    `json:"model"`
			Messages  []Message `json:"messages"`
			Stream    bool      `json:"stream"`
			MaxTokens int       `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Model != "upstream-model" || payload.Stream || payload.MaxTokens != 1024 ||
			len(payload.Messages) != 1 || payload.Messages[0].Content != "Hi" {
			t.Errorf("payload = %#v, want upstream model, one message, 1024 tokens, no stream", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Hello"}}]}`)
	}))

	response, err := newOpenAIKeylessProvider(keylessStubConfig(), &http.Client{}).
		ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "friendly" || response.Choices[0].Message.Content != "Hello" {
		t.Fatalf("response model/content = %q/%q, want friendly/Hello", response.Model, response.Choices[0].Message.Content)
	}
}

func TestOpenAIKeylessStreamingAggregatesDeltasWithoutAuthorization(t *testing.T) {
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))

	var chunks []string
	err := newOpenAIKeylessProvider(keylessStubConfig(), &http.Client{}).
		ChatCompletionStream(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly", func(chunk string) {
			chunks = append(chunks, chunk)
		})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Hel", "lo"}; !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v, want %#v", chunks, want)
	}
}

func TestOpenAIKeylessPseudoStreamEmitsOneFinalChunk(t *testing.T) {
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Stream {
			t.Error("payload stream = true, want false for a non-streaming provider")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Hello"}}]}`)
	}))

	cfg := keylessStubConfig()
	cfg.SupportsStream = false
	var chunks []string
	err := newOpenAIKeylessProvider(cfg, &http.Client{}).
		ChatCompletionStream(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly", func(chunk string) {
			chunks = append(chunks, chunk)
		})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Hello"}; !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v, want %#v", chunks, want)
	}
}

func TestOpenAIKeylessSendsConfiguredHeaders(t *testing.T) {
	useKeylessProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer local-key" {
			t.Errorf("Authorization = %q, want Bearer local-key", got)
		}
		if got := r.Header.Get("X-Custom"); got != "yes" {
			t.Errorf("X-Custom = %q, want yes", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))

	cfg := keylessStubConfig()
	cfg.Headers = map[string]string{"Authorization": "Bearer local-key", "X-Custom": "yes"}
	response, err := newOpenAIKeylessProvider(cfg, &http.Client{}).
		ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly")
	if err != nil {
		t.Fatal(err)
	}
	if response.Choices[0].Message.Content != "OK" {
		t.Fatalf("content = %q, want OK", response.Choices[0].Message.Content)
	}
}

func TestOpenAIKeylessEmptyCompletionIsAnError(t *testing.T) {
	useKeylessProviderResponse(t, http.StatusOK, `{"choices":[],"secret":"stub-internal-body"}`)

	_, err := newOpenAIKeylessProvider(keylessStubConfig(), &http.Client{}).
		ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error = %v, want empty response error", err)
	}
	if strings.Contains(err.Error(), "stub-internal-body") {
		t.Fatalf("error %q leaked the upstream body", err.Error())
	}
}

func TestOpenAIKeylessMetadataAndModelSupport(t *testing.T) {
	cfg := openAIKeylessConfig{
		Name:           "stub",
		Label:          "Stub Provider",
		URL:            "https://example.test/models",
		CompletionsURL: "https://example.test/v1/chat/completions",
		DefaultModel:   "alpha",
		ModelIDs:       map[string]string{"zeta": "upstream-zeta", "alpha": "upstream-alpha"},
		Description:    "stub provider",
		SupportsStream: true,
	}
	provider := newOpenAIKeylessProvider(cfg, &http.Client{})
	info := provider.GetInfo()
	if info.Name != "stub" || info.Label != "Stub Provider" || info.URL != "https://example.test/models" ||
		info.DefaultModel != "alpha" || info.Description != "stub provider" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if !info.Working || info.NeedsAuth || info.Alive || info.ReverseEngineering || !info.SupportsStream {
		t.Errorf("unexpected capabilities in GetInfo(): %#v", info)
	}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(info.SupportedModels, want) {
		t.Errorf("SupportedModels = %v, want %v", info.SupportedModels, want)
	}
	if !provider.SupportsModel("ALPHA") || !provider.SupportsModel(" Zeta ") {
		t.Error("SupportsModel rejected an existing model")
	}
	if provider.SupportsModel("unknown") {
		t.Error("SupportsModel accepted an unknown model")
	}
}

func TestOpenAIKeylessClassifiesUpstreamFailures(t *testing.T) {
	for _, tc := range statusTable() {
		t.Run(tc.name, func(t *testing.T) {
			useKeylessProviderResponse(t, tc.status, "stub-internal-body")
			_, err := newOpenAIKeylessProvider(keylessStubConfig(), &http.Client{}).
				ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly")
			classifyFailure(t, err, tc.category, tc.status, "stub-internal-body")
		})
	}
}

func TestOpenAIKeylessNetworkFailureIsTransient(t *testing.T) {
	useKeylessProviderNetworkFailure(t)

	_, err := newOpenAIKeylessProvider(keylessStubConfig(), &http.Client{}).
		ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "friendly")
	classifyFailure(t, err, app.FailureTransient, 0, "")
}
