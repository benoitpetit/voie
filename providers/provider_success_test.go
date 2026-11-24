package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestPerplexityCompletionReturnsResolvedModelAndAggregatedAnswer(t *testing.T) {
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/sse/perplexity_ask" {
			t.Errorf("request = %s %s, want POST /rest/sse/perplexity_ask", r.Method, r.URL.Path)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		params, _ := payload["params"].(map[string]interface{})
		if params["model_preference"] != "turbo" || payload["query_str"] != "Hello" {
			t.Errorf("payload = %#v, want resolved model and user prompt", payload)
		}
		if params["version"] != perplexityAPIVersion || params["mode"] != "copilot" {
			t.Errorf("params = %#v, want the current Perplexity ask envelope", params)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"blocks\":[{\"diff_block\":{\"patches\":[{\"value\":{\"steps\":[{\"items\":[{\"payload\":{\"text_payload\":{\"text\":\"Hel\"}}}]}]}}]}}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"blocks\":[{\"markdown_block\":{\"answer\":\"Hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))

	response, err := (&Perplexity{}).ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hello"}}, "turbo")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "turbo" || response.Choices[0].Message.Content != "Hello" {
		t.Fatalf("response model/content = %q/%q, want turbo/Hello", response.Model, response.Choices[0].Message.Content)
	}
}

func TestPerplexityOptionalCookieHeader(t *testing.T) {
	t.Setenv("VOIE_PERPLEXITY_COOKIES", "  pplx.edge-sid=abc; pplx.edge-vid=def  ")
	if got := perplexityCookies(); got != "pplx.edge-sid=abc; pplx.edge-vid=def" {
		t.Fatalf("perplexityCookies() = %q", got)
	}
	t.Setenv("VOIE_PERPLEXITY_COOKIES", "")
	if got := perplexityCookies(); got != "" {
		t.Fatalf("perplexityCookies() with empty env = %q", got)
	}
}

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

func TestCohereCompletionResolvesAliasAndRemovesNULTokens(t *testing.T) {
	const messageID = "11111111-2222-4333-8444-555555555555"
	useProviderHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/conversation":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode create conversation: %v", err)
			}
			if payload["model"] != "command-r-08-2024" || payload["preprompt"] != "Be concise" {
				t.Errorf("conversation payload = %#v", payload)
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "test"})
			_, _ = io.WriteString(w, `{"conversationId":"conv-1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/conversation/conv-1/__data.json":
			_, _ = fmt.Fprintf(w, `{"nodes":[{}, {"data":[{"messages":1},[2],{"id":"%s"}]}]}`, messageID)
		case r.Method == http.MethodPost && r.URL.Path == "/conversation/conv-1":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read send message: %v", err)
			}
			if !strings.Contains(string(body), messageID) || !strings.Contains(string(body), "user: Hello") {
				t.Errorf("send body = %s", body)
			}
			_, _ = io.WriteString(w, "{\"type\":\"stream\",\"token\":\"Hello\\u0000\"}\n{\"type\":\"finalAnswer\"}\n")
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))

	response, err := (&CohereCommand{}).ChatCompletion(testProviderContext(), []Message{
		{Role: "system", Content: "Be concise"},
		{Role: "user", Content: "Hello"},
	}, "command-r")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "command-r-08-2024" || response.Choices[0].Message.Content != "Hello" {
		t.Fatalf("response model/content = %q/%q, want command-r-08-2024/Hello", response.Model, response.Choices[0].Message.Content)
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
