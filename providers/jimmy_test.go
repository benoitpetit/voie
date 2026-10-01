package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type jimmyRoundTripper func(*http.Request) (*http.Response, error)

func (f jimmyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestJimmyChatCompletion(t *testing.T) {
	provider := &Jimmy{
		endpoint: "https://jimmy.example/api/chat",
		client: &http.Client{Transport: jimmyRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost {
				t.Errorf("request method = %q, want POST", request.Method)
			}
			if request.URL.String() != "https://jimmy.example/api/chat" {
				t.Errorf("request URL = %q", request.URL)
			}
			if request.Header.Get("Origin") != "https://jimmy.example" {
				t.Errorf("Origin = %q", request.Header.Get("Origin"))
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			payload := string(body)
			for _, expected := range []string{`"role":"system"`, `"content":"Be concise"`, `"role":"user"`, `"content":"Hello"`, `"selectedModel":"llama3.1-8B"`, `"systemPrompt":"Be concise"`, `"topK":8`, `"attachment":null`} {
				if !strings.Contains(payload, expected) {
					t.Errorf("request body %s does not contain %s", payload, expected)
				}
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("Paris.<!--stats-->{\"tokens\":1}<!--/stats-->")),
				Header:     make(http.Header),
			}, nil
		})},
	}

	response, err := provider.ChatCompletion(context.Background(), []Message{
		{Role: "system", Content: "Be concise"},
		{Role: "user", Content: "Hello"},
	}, "llama3.1-8B")
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if response.Model != "llama3.1-8B" || response.Choices[0].Message.Content != "Paris." {
		t.Fatalf("ChatCompletion() = %#v", response)
	}
}

func TestJimmyProviderMetadataAndModelSupport(t *testing.T) {
	provider := &Jimmy{}
	info := provider.GetInfo()
	if info.Name != "jimmy" || info.Label != "ChatJimmy" || info.DefaultModel != "llama3.1-8B" {
		t.Fatalf("GetInfo() = %#v", info)
	}
	if info.NeedsAuth || info.SupportsStream || !info.ReverseEngineering {
		t.Errorf("unexpected Jimmy capabilities in GetInfo(): %#v", info)
	}
	if !provider.SupportsModel("LLAMA3.1-8B") {
		t.Error("SupportsModel() rejected Jimmy's model")
	}
	if provider.SupportsModel("llama3.2") {
		t.Error("SupportsModel() accepted an unknown model")
	}
}

func TestJimmyChatCompletionReportsUpstreamErrors(t *testing.T) {
	provider := &Jimmy{
		endpoint: "https://jimmy.example/api/chat",
		client: &http.Client{Transport: jimmyRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader("slow down")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	_, err := provider.ChatCompletion(context.Background(), []Message{{Role: "user", Content: "Hello"}}, "llama3.1-8B")
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("ChatCompletion() error = %v, want upstream status and body", err)
	}
}
