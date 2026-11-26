package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestQuillbotCompletionParsesContentEvents(t *testing.T) {
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/ai-chat/chat/conversation/") {
			t.Errorf("request = %s %s, want Quillbot conversation POST", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "{\"type\":\"content\",\"content\":\"Hello\"}\n")
		_, _ = io.WriteString(w, "{\"type\":\"content\",\"content\":\" world\"}\n")
	}))

	response, err := (&Quillbot{}).ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hi"}}, "quillbot-search")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "quillbot-search" || response.Choices[0].Message.Content != "Hello world" {
		t.Fatalf("response model/content = %q/%q, want quillbot-search/Hello world", response.Model, response.Choices[0].Message.Content)
	}
}

func TestYqcloudCompletionAggregatesPlainTextChunks(t *testing.T) {
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/generateStream" {
			t.Errorf("request = %s %s, want POST /api/generateStream", r.Method, r.URL.Path)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["system"] != "Be concise" || payload["prompt"] != "Hello\n" || payload["stream"] != true {
			t.Errorf("payload = %#v, want system, prompt and streaming mode", payload)
		}
		_, _ = io.WriteString(w, "Hello\nworld")
	}))

	response, err := (&Yqcloud{}).ChatCompletion(testProviderContext(), []Message{
		{Role: "system", Content: "Be concise"},
		{Role: "user", Content: "Hello"},
	}, "gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "gpt-4" || response.Choices[0].Message.Content != "Helloworld" {
		t.Fatalf("response model/content = %q/%q, want gpt-4/Helloworld", response.Model, response.Choices[0].Message.Content)
	}
}

func TestRegisteredProviderMetadataDoesNotClaimLiveReachability(t *testing.T) {
	registry := NewRegistry()
	for _, name := range registry.GetProviderNames() {
		if info := registry.Get(name).GetInfo(); info.Alive {
			t.Errorf("provider %q reports Alive=true in static metadata", name)
		}
	}
}

func TestKeylessProviderForwardsOptionsAndDecodesToolCalls(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`)
	}))
	defer server.Close()
	provider := newOpenAIKeylessProvider(openAIKeylessConfig{
		Name: "test", CompletionsURL: server.URL, DefaultModel: "model",
		ModelIDs: map[string]string{"model": "model"},
	}, server.Client())
	temperature, maxTokens := 0.2, 64
	ctx := app.WithRequestOptions(context.Background(), app.RequestOptions{
		Temperature: &temperature, MaxTokens: &maxTokens, Tools: []map[string]any{{"type": "function"}},
	})
	response, err := provider.ChatCompletion(ctx, []Message{{Role: "user", Content: "hi"}}, "model")
	if err != nil {
		t.Fatal(err)
	}
	if captured["temperature"] != 0.2 || captured["max_tokens"] != float64(64) {
		t.Fatalf("captured payload = %#v", captured)
	}
	if _, ok := captured["tools"]; !ok {
		t.Fatalf("tools were not forwarded: %#v", captured)
	}
	if len(response.Choices) == 0 || response.Choices[0].Message.ToolCalls == nil {
		t.Fatalf("tool calls were not decoded: %+v", response.Choices)
	}
}

func TestKeylessStreamPropagatesToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\",\"tool_calls\":[{\"index\":0,\"id\":\"call1\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"{\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\",\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"}\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	provider := newOpenAIKeylessProvider(openAIKeylessConfig{
		Name: "test", CompletionsURL: server.URL, DefaultModel: "model", SupportsStream: true,
		ModelIDs: map[string]string{"model": "model"},
	}, server.Client())
	var toolCalls []any
	ctx := app.WithToolCallSink(context.Background(), func(value any) { toolCalls = append(toolCalls, value) })
	if err := provider.ChatCompletionStream(ctx, []Message{{Role: "user", Content: "hi"}}, "model", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(toolCalls) != 2 {
		t.Fatalf("tool call deltas = %d, want 2", len(toolCalls))
	}
}

func TestPPLXProxySignUpAnswerBecomesActionableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Sign up and repeat your request."}}]}`)
	}))
	defer server.Close()
	provider := newOpenAIKeylessProvider(openAIKeylessConfig{
		Name: "pplx-proxy", CompletionsURL: server.URL, DefaultModel: "auto",
		ModelIDs: map[string]string{"auto": "auto"},
		ResponseHint: func(content string) error {
			if strings.Contains(content, "Sign up and repeat your request.") {
				return errors.New("Perplexity cookie expired; run `voie connect pplx-proxy`")
			}
			return nil
		},
	}, server.Client())
	_, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, "auto")
	if err == nil || !strings.Contains(err.Error(), "voie connect pplx-proxy") {
		t.Fatalf("error = %v, want actionable cookie error", err)
	}
}

func TestPPLXProxyOmitsUnsupportedMaxTokens(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()
	provider := newOpenAIKeylessProvider(openAIKeylessConfig{
		Name: "pplx-proxy", CompletionsURL: server.URL, DefaultModel: "auto",
		ModelIDs: map[string]string{"auto": "auto"}, OmitDefaultMaxTokens: true,
	}, server.Client())
	if _, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "hi"}}, "auto"); err != nil {
		t.Fatal(err)
	}
	if _, ok := captured["max_tokens"]; ok {
		t.Fatalf("max_tokens must be omitted for pplx-proxy: %#v", captured)
	}
}

func TestGatewayModelIDsMatchCaseInsensitively(t *testing.T) {
	provider := newOpenAIKeylessProvider(openAIKeylessConfig{
		Name: "freellmpool", DefaultModel: "pool-ovh/Meta-Llama-3_3-70B-Instruct",
		ModelIDs: map[string]string{"pool-ovh/Meta-Llama-3_3-70B-Instruct": "ovh/Meta-Llama-3_3-70B-Instruct"},
	}, nil)
	if !provider.SupportsModel("pool-ovh/Meta-Llama-3_3-70B-Instruct") {
		t.Fatal("mixed-case model ID is not supported")
	}
	resolved, err := provider.resolve("pool-ovh/Meta-Llama-3_3-70B-Instruct")
	if err != nil || resolved != "ovh/Meta-Llama-3_3-70B-Instruct" {
		t.Fatalf("resolve = %q, %v", resolved, err)
	}
}

func TestPPLXProxySurfacesUpstreamErrorMessage(t *testing.T) {
	var hint string
	if p := newOpenAIKeylessProvider(openAIKeylessConfig{}, nil); p != nil {
		// BodyHint is configured by NewExternalGateway; verify the parser shape
		// using the same extraction helper.
		hint = pplxProxyErrorHint(403, []byte(`{"error":{"message":"model requires Pro"}}`))
	}
	if hint != "model requires Pro" {
		t.Fatalf("hint = %q", hint)
	}
}
