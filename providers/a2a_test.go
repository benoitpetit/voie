package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/transport/a2a"
)

func a2aRPCServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "agent-card.json") || r.URL.Path == "/" {
			_, _ = w.Write([]byte(`{"name":"remote","description":"remote","supportedInterfaces":[{"url":"` + r.Host + `/a2a","protocolBinding":"JSONRPC","protocolVersion":"1.0"}],"capabilities":{"streaming":true},"skills":[{"id":"voie.chat.classic","name":"Classic","description":"chat","inputModes":["text/plain"],"outputModes":["text/plain"]}],"defaultInputModes":["text/plain"],"defaultOutputModes":["text/plain"]}`))
			return
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Message struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"message"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode: %v", err)
		}
		text := ""
		if len(request.Params.Message.Parts) > 0 {
			text = request.Params.Message.Parts[0].Text
		}
		if request.Method == "SendStreamingMessage" {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, chunk := range []string{"remote says ", text} {
				event := map[string]any{"artifactUpdate": map[string]any{"taskId": "t", "contextId": "c", "artifact": map[string]any{"artifactId": "a", "parts": []any{map[string]any{"text": chunk}}}, "index": 0}}
				encoded, _ := json.Marshal(event)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			return
		}
		result := map[string]any{"id": "t", "contextId": "c", "status": map[string]any{"state": "TASK_STATE_COMPLETED"}, "artifacts": []any{map[string]any{"artifactId": "a", "parts": []any{map[string]any{"text": "remote says " + text}}}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
}

func a2aProvider(t *testing.T) (*A2AProvider, *httptest.Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := a2aRPCServer(t)
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := ConnectA2A(store, "one", server.URL, "tok"); err != nil {
		t.Fatal(err)
	}
	return NewA2AProvider("one"), server
}

func TestA2AProviderChatCompletion(t *testing.T) {
	provider, server := a2aProvider(t)
	defer server.Close()
	model := "a2a-one-voie.chat.classic"
	if !provider.SupportsModel(model) {
		t.Fatalf("model %q unsupported", model)
	}
	response, err := provider.ChatCompletion(context.Background(), []app.Message{{Role: "user", Content: "hello"}}, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content != "remote says hello" {
		t.Fatalf("response = %+v", response)
	}
}

func TestA2AProviderStreaming(t *testing.T) {
	provider, server := a2aProvider(t)
	defer server.Close()
	var chunks string
	err := provider.ChatCompletionStream(context.Background(), []app.Message{{Role: "user", Content: "hello"}}, "a2a-one-voie.chat.classic", func(chunk string) { chunks += chunk })
	if err != nil {
		t.Fatal(err)
	}
	if chunks != "remote says hello" {
		t.Fatalf("chunks = %q", chunks)
	}
}

func TestA2AProviderInfoAndRegistry(t *testing.T) {
	provider, server := a2aProvider(t)
	defer server.Close()
	info := provider.GetInfo()
	if info.Name != "a2a:one" || len(info.SupportedModels) != 1 || info.SupportedModels[0] != "a2a-one-voie.chat.classic" {
		t.Fatalf("info = %+v", info)
	}
	registry := NewRegistry()
	if registry.Get("a2a:one") == nil {
		t.Fatal("configured A2A agent not registered")
	}
}

type voieEchoProvider struct{}

func (*voieEchoProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "local", Working: true, DefaultModel: "m", SupportedModels: []string{"m"}, SupportsStream: true}
}
func (*voieEchoProvider) SupportsModel(model string) bool { return model == "m" }
func (*voieEchoProvider) ChatCompletion(_ context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "echo " + model}}}}, nil
}
func (*voieEchoProvider) ChatCompletionStream(_ context.Context, _ []app.Message, model string, callback func(string)) error {
	callback("echo " + model)
	return nil
}

func TestA2AProviderCallsVoieInbound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	registry := app.NewRegistry()
	registry.Register("local", &voieEchoProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := a2a.NewHandler(service)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(h.Card())
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	store, err := config.LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := ConnectA2A(store, "local", server.URL, ""); err != nil {
		t.Fatal(err)
	}
	provider := NewA2AProvider("local")
	response, err := provider.ChatCompletion(context.Background(), []app.Message{{Role: "user", Content: "hi"}}, "a2a-local-voie.chat.classic")
	if err != nil {
		t.Fatalf("voie-to-voie A2A call failed: %v", err)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Content == "" {
		t.Fatalf("response = %+v", response)
	}
}
