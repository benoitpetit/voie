package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

type a2aTestProvider struct{}

func (*a2aTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test", Working: true, SupportedModels: []string{"m"}, SupportsStream: true}
}
func (*a2aTestProvider) SupportsModel(model string) bool { return model == "m" }
func (*a2aTestProvider) ChatCompletion(_ context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "answer from " + model}}}}, nil
}
func (*a2aTestProvider) ChatCompletionStream(_ context.Context, _ []app.Message, model string, callback func(string)) error {
	callback("stream from " + model)
	return nil
}

func a2aTestService(t *testing.T) *app.Service {
	t.Helper()
	registry := app.NewRegistry()
	registry.Register("test", &a2aTestProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func rpcRequest(t *testing.T, handler http.Handler, body string) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body)))
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("response %q: %v", recorder.Body.String(), err)
	}
	return result
}

func TestAgentCardAdvertisesVoieSkills(t *testing.T) {
	card := AgentCardFor("https://voie.example")
	if len(card.Skills) != 7 {
		t.Fatalf("skills = %+v, want 7", card.Skills)
	}
	ids := map[string]bool{}
	for _, skill := range card.Skills {
		ids[skill.ID] = true
	}
	for _, want := range []string{"voie.chat.classic", "voie.chat.auto", "voie.chat.ensemble.independent", "voie.chat.ensemble.chain", "voie.chat.ensemble.debate", "voie.models.discover", "voie.conversation.manage"} {
		if !ids[want] {
			t.Errorf("missing skill %q", want)
		}
	}
	if len(card.SupportedInterfaces) != 1 || card.SupportedInterfaces[0].ProtocolBinding != "JSONRPC" || card.SupportedInterfaces[0].ProtocolVersion != "1.0" {
		t.Fatalf("interfaces = %+v", card.SupportedInterfaces)
	}
	if !card.Capabilities.Streaming || card.SupportedInterfaces[0].URL != "https://voie.example/a2a" {
		t.Fatalf("card = %+v", card)
	}
}

func TestSendMessageReturnsCompletedTask(t *testing.T) {
	handler := NewHandler(a2aTestService(t))
	result := rpcRequest(t, handler, `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"model":"m"}}}}`)
	if result["error"] != nil {
		t.Fatalf("error = %v", result["error"])
	}
	task, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %+v", result)
	}
	status, _ := task["status"].(map[string]any)
	if status["state"] != "TASK_STATE_COMPLETED" {
		t.Fatalf("status = %+v", status)
	}
	artifacts, _ := task["artifacts"].([]any)
	if len(artifacts) == 0 {
		t.Fatalf("artifacts = %v", artifacts)
	}
	artifact, _ := artifacts[0].(map[string]any)
	parts, _ := artifact["parts"].([]any)
	part, _ := parts[0].(map[string]any)
	if part["text"] != "answer from m" {
		t.Fatalf("part = %+v", part)
	}
}

func TestLegacyAliasAndUnknownMethod(t *testing.T) {
	handler := NewHandler(a2aTestService(t))
	result := rpcRequest(t, handler, `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"text":"hi"}],"metadata":{"model":"m"}}}}`)
	if result["error"] != nil {
		t.Fatalf("legacy alias failed: %v", result["error"])
	}
	result = rpcRequest(t, handler, `{"jsonrpc":"2.0","id":2,"method":"nope","params":{}}`)
	failure, _ := result["error"].(map[string]any)
	if failure["code"] != float64(-32601) {
		t.Fatalf("error = %+v, want -32601", failure)
	}
}

func TestSendStreamingMessageEmitsTaskEvents(t *testing.T) {
	handler := NewHandler(a2aTestService(t))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"SendStreamingMessage","params":{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"model":"m"}}}}`))
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{"statusUpdate", "artifactUpdate", "TASK_STATE_COMPLETED"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream %q missing %q", body, want)
		}
	}
	if bytes.Count(recorder.Body.Bytes(), []byte("data:")) < 2 {
		t.Fatalf("stream = %q, want at least two SSE events", body)
	}
}

func TestCancelTaskMarksWorkingTaskCanceled(t *testing.T) {
	h := NewHandler(a2aTestService(t))
	handler := http.Handler(h)
	h.store.create("task-1", "", context.Background(), func() {})
	h.store.setStatus("task-1", "TASK_STATE_WORKING")
	result := rpcRequest(t, handler, `{"jsonrpc":"2.0","id":1,"method":"CancelTask","params":{"id":"task-1"}}`)
	task, _ := result["result"].(map[string]any)
	status, _ := task["status"].(map[string]any)
	if status["state"] != "TASK_STATE_CANCELED" {
		t.Fatalf("status = %+v, want canceled", status)
	}
}
