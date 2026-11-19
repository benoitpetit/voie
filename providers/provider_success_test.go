package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
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
		if payload["model_preference"] != "gpt5" || payload["query_str"] != "Hello" {
			t.Errorf("payload = %#v, want resolved model and user prompt", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"blocks\":[{\"markdown_block\":{\"answer\":\"Hel\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"blocks\":[{\"markdown_block\":{\"answer\":\"Hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))

	response, err := (&Perplexity{}).ChatCompletion(testProviderContext(), []Message{{Role: "user", Content: "Hello"}}, "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "gpt5" || response.Choices[0].Message.Content != "Hello" {
		t.Fatalf("response model/content = %q/%q, want gpt5/Hello", response.Model, response.Choices[0].Message.Content)
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
