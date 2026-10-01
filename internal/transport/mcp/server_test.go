package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/app"
	sqlitestore "github.com/benoitpetit/voie/internal/storage/sqlite"
	"github.com/benoitpetit/voie/utils"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolLifecycleLogsOmitArgumentsResultsAndErrors(t *testing.T) {
	var logs bytes.Buffer
	utils.SetOutput(&logs)
	defer utils.SetOutput(nil)
	utils.SetNoColor(true)
	handler := mcp.ToolHandlerFor[map[string]string, string](func(_ context.Context, _ *mcp.CallToolRequest, input map[string]string) (*mcp.CallToolResult, string, error) {
		if input["value"] == "fail-secret" {
			return nil, "", errors.New("error-secret")
		}
		return textToolResult("result-secret"), "result-secret", nil
	})
	wrapped := logToolCall("test_tool", handler)
	_, _, err := wrapped(context.Background(), nil, map[string]string{"value": "argument-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = wrapped(context.Background(), nil, map[string]string{"value": "fail-secret"})
	if err == nil {
		t.Fatal("expected test handler error")
	}
	for _, marker := range []string{"test_tool", "started", "succeeded", "failed", "request_id="} {
		if !strings.Contains(logs.String(), marker) {
			t.Fatalf("logs %q missing %q", logs.String(), marker)
		}
	}
	for _, secret := range []string{"argument-secret", "result-secret", "fail-secret", "error-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("MCP logs leaked %q: %s", secret, logs.String())
		}
	}
}

func TestMCPToolsExposeStableSchemasAndStructuredResults(t *testing.T) {
	service, _ := mcpTestService(t, nil, time.Second)
	server := NewServer(service)
	clientSession, closeSessions := connectTestClient(t, server)
	defer closeSessions()

	result, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	want := []string{"chat_completion", "create_conversation", "delete_conversation", "get_conversation", "list_conversations", "list_models", "list_providers"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for _, tool := range result.Tools {
		if tool.InputSchema == nil {
			t.Errorf("tool %s has no input schema", tool.Name)
		}
	}

	models, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_models", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if models.IsError || len(models.Content) == 0 || models.StructuredContent == nil {
		t.Fatalf("models result = %+v", models)
	}
	text, ok := models.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "model") {
		t.Fatalf("models text result = %#v", models.Content)
	}

	providers, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_providers", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if providers.IsError || len(providers.Content) == 0 || providers.StructuredContent == nil {
		t.Fatalf("providers result = %+v", providers)
	}

	completion, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{"model": "model", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if completion.IsError || completion.StructuredContent == nil || len(completion.Content) == 0 {
		t.Fatalf("completion result = %+v", completion)
	}
}

func TestMCPChatCompletionAcceptsAutomaticStrategies(t *testing.T) {
	service, _ := mcpTestService(t, nil, time.Second)
	session, closeSessions := connectTestClient(t, NewServer(service))
	defer closeSessions()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{"strategy": "auto", "task": "coding", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMCPConversationToolsCreateResumeAndDelete(t *testing.T) {
	service, _ := mcpTestService(t, nil, time.Second)
	session, closeSessions := connectTestClient(t, NewServer(service))
	defer closeSessions()
	created, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_conversation", Arguments: map[string]any{}})
	if err != nil || created.IsError || created.StructuredContent == nil {
		t.Fatalf("create=%+v err=%v", created, err)
	}
	data, ok := created.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("create structured=%T %v", created.StructuredContent, created.StructuredContent)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("create=%v", data)
	}
	listed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_conversations", Arguments: map[string]any{}})
	if err != nil || listed.IsError {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	shown, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_conversation", Arguments: map[string]any{"id": id}})
	if err != nil || shown.IsError || shown.StructuredContent == nil {
		t.Fatalf("get=%+v err=%v", shown, err)
	}
	deleted, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_conversation", Arguments: map[string]any{"id": id}})
	if err != nil || deleted.IsError {
		t.Fatalf("delete=%+v err=%v", deleted, err)
	}
}

func TestMCPReturnsApplicationFailuresAsToolErrors(t *testing.T) {
	service, _ := mcpTestService(t, errors.New("provider unavailable"), time.Second)
	session, closeSessions := connectTestClient(t, NewServer(service))
	defer closeSessions()
	unknown, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{"model": "unknown", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !unknown.IsError {
		t.Fatalf("unknown model result = %+v, want isError", unknown)
	}
	upstream, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{"provider": "test", "model": "model", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !upstream.IsError {
		t.Fatalf("upstream result = %+v, want isError", upstream)
	}
}

func TestMCPCompletionHonorsTimeout(t *testing.T) {
	service, _ := mcpTestService(t, nil, 10*time.Millisecond)
	session, closeSessions := connectTestClient(t, NewServer(service))
	defer closeSessions()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{"model": "model", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("timeout result = %+v, want isError", result)
	}
}

func connectTestClient(t *testing.T, server *mcp.Server) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	a, b := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	clientSession, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	return clientSession, func() { _ = clientSession.Close(); _ = serverSession.Close() }
}

func mcpTestService(t *testing.T, err error, timeout time.Duration) (*app.Service, *mcpTestProvider) {
	t.Helper()
	registry := app.NewRegistry()
	provider := &mcpTestProvider{err: err, block: timeout <= 10*time.Millisecond}
	registry.Register("test", provider)
	store, storeErr := sqlitestore.NewSQLiteStore(t.TempDir()+"/mcp.db", time.Hour)
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, serviceErr := app.NewService(registry, app.ServiceOptions{Timeout: timeout, Conversations: store, HealthProbe: func(context.Context, string) bool { return true }})
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	return service, provider
}

type mcpTestProvider struct {
	err   error
	block bool
}

func (*mcpTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test", Label: "Test", URL: "https://example.invalid", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}}
}
func (*mcpTestProvider) SupportsModel(model string) bool { return model == "model" }
func (p *mcpTestProvider) ChatCompletion(ctx context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &app.ChatCompletionResponse{Model: model, Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "answer"}}}}, nil
}
func (p *mcpTestProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return p.err
}
