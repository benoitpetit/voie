package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/cli"
	"github.com/benoitpetit/voie/internal/runtime"
	"github.com/benoitpetit/voie/internal/transport/httpapi"
	mcpserver "github.com/benoitpetit/voie/internal/transport/mcp"
	"github.com/benoitpetit/voie/utils"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCrossTransportCompletionAndDiscovery(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("fake", integrationProvider{})
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Second}
	rt, err := runtime.NewWithRegistry(cfg, registry)
	if err != nil {
		t.Fatal(err)
	}

	var cliModels bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"models", "--json"}, nil, &cliModels, &bytes.Buffer{}, func() (cli.Runtime, error) { return rt, nil }); err != nil {
		t.Fatal(err)
	}
	var cliCatalogue struct {
		Data []app.ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(cliModels.Bytes(), &cliCatalogue); err != nil {
		t.Fatal(err)
	}
	if len(cliCatalogue.Data) != 1 || cliCatalogue.Data[0].ID != "fake-model" {
		t.Fatalf("CLI catalogue = %+v", cliCatalogue.Data)
	}

	handler := httpapi.NewHandler(rt.Service(), cfg)
	httpModels := httptest.NewRecorder()
	handler.ServeHTTP(httpModels, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if httpModels.Code != http.StatusOK {
		t.Fatalf("HTTP models status = %d: %s", httpModels.Code, httpModels.Body.String())
	}
	var httpCatalogue struct {
		Data []app.ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(httpModels.Body.Bytes(), &httpCatalogue); err != nil {
		t.Fatal(err)
	}
	if len(httpCatalogue.Data) != 1 || httpCatalogue.Data[0].ID != cliCatalogue.Data[0].ID {
		t.Fatalf("HTTP catalogue = %+v, CLI catalogue = %+v", httpCatalogue.Data, cliCatalogue.Data)
	}

	session, closeSessions := integrationMCPClient(t, rt.Service())
	defer closeSessions()
	mcpModels, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_models", Arguments: map[string]any{}})
	if err != nil || mcpModels.IsError || mcpModels.StructuredContent == nil {
		t.Fatalf("MCP models result = %+v, err = %v", mcpModels, err)
	}
	mcpCatalogueJSON, err := json.Marshal(mcpModels.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var mcpCatalogue struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(mcpCatalogueJSON, &mcpCatalogue); err != nil {
		t.Fatal(err)
	}
	if len(mcpCatalogue.Models) != 1 || mcpCatalogue.Models[0].ID != cliCatalogue.Data[0].ID {
		t.Fatalf("MCP catalogue = %s, CLI catalogue = %+v", mcpCatalogueJSON, cliCatalogue.Data)
	}

	var cliAnswer bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"chat", "--model", "fake-model", "hello"}, strings.NewReader(""), &cliAnswer, &bytes.Buffer{}, func() (cli.Runtime, error) { return rt, nil }); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cliAnswer.String()) != "shared answer" {
		t.Fatalf("CLI completion = %q", cliAnswer.String())
	}

	httpCompletion := httptest.NewRecorder()
	handler.ServeHTTP(httpCompletion, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fake-model","messages":[{"role":"user","content":"hello"}]}`)))
	if httpCompletion.Code != http.StatusOK {
		t.Fatalf("HTTP completion status = %d: %s", httpCompletion.Code, httpCompletion.Body.String())
	}
	var response app.ChatCompletionResponse
	if err := json.Unmarshal(httpCompletion.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if got := response.Choices[0].Message.Content; got != "shared answer" {
		t.Fatalf("HTTP completion = %q", got)
	}

	mcpCompletion, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{
		"model": "fake-model", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}})
	if err != nil || mcpCompletion.IsError || mcpCompletion.StructuredContent == nil {
		t.Fatalf("MCP completion result = %+v, err = %v", mcpCompletion, err)
	}
	completionJSON, err := json.Marshal(mcpCompletion.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var mcpAnswer struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(completionJSON, &mcpAnswer); err != nil {
		t.Fatal(err)
	}
	if mcpAnswer.Text != "shared answer" {
		t.Fatalf("MCP completion = %s", completionJSON)
	}
}

func TestCrossTransportFallbackOverrideContract(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("flaky", integrationFlakyProvider{})
	registry.Register("fake", integrationProvider{})
	cfg := &config.Config{Host: "127.0.0.1", Port: "8080", Timeout: time.Second}
	rt, err := runtime.NewWithRegistry(cfg, registry)
	if err != nil {
		t.Fatal(err)
	}

	var cliLogs bytes.Buffer
	var cliAnswer bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"chat", "--model", "flaky-model", "--fallback-model", "fake-model", "hello"}, strings.NewReader(""), &cliAnswer, &cliLogs, func() (cli.Runtime, error) { return rt, nil }); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cliAnswer.String()) != "shared answer" {
		t.Fatalf("CLI completion = %q", cliAnswer.String())
	}
	if strings.Contains(cliLogs.String(), "hello") || strings.Contains(cliLogs.String(), "shared answer") {
		t.Fatalf("CLI stderr leaked prompt or answer: %q", cliLogs.String())
	}

	var apiLogs bytes.Buffer
	utils.SetOutput(&apiLogs)
	defer utils.SetOutput(nil)

	handler := httpapi.NewHandler(rt.Service(), cfg)
	httpCompletion := httptest.NewRecorder()
	handler.ServeHTTP(httpCompletion, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"flaky-model","fallback":{"models":["fake-model"]},"messages":[{"role":"user","content":"hello"}]}`)))
	if httpCompletion.Code != http.StatusOK {
		t.Fatalf("HTTP completion status = %d: %s", httpCompletion.Code, httpCompletion.Body.String())
	}
	var response app.ChatCompletionResponse
	if err := json.Unmarshal(httpCompletion.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Model != "fake-model" || response.Choices[0].Message.Content != "shared answer" {
		t.Fatalf("HTTP completion = %+v, want fallback to fake-model", response)
	}
	if response.Routing == nil || len(response.Routing.Attempts) != 2 || response.Routing.Attempts[0].Outcome != app.OutcomeUnavailable || response.Routing.Attempts[1].Outcome != app.OutcomeSucceeded {
		t.Fatalf("HTTP routing = %+v, want unavailable then succeeded", response.Routing)
	}
	if strings.Contains(apiLogs.String(), "hello") || strings.Contains(apiLogs.String(), "shared answer") {
		t.Fatalf("HTTP logs leaked prompt or answer: %q", apiLogs.String())
	}

	session, closeSessions := integrationMCPClient(t, rt.Service())
	defer closeSessions()
	mcpCompletion, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chat_completion", Arguments: map[string]any{
		"model": "flaky-model", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"fallback": map[string]any{"models": []any{"fake-model"}},
	}})
	if err != nil || mcpCompletion.IsError || mcpCompletion.StructuredContent == nil {
		t.Fatalf("MCP completion result = %+v, err = %v", mcpCompletion, err)
	}
	completionJSON, err := json.Marshal(mcpCompletion.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var mcpAnswer struct {
		Text    string `json:"text"`
		Model   string `json:"model"`
		Routing struct {
			Attempts []json.RawMessage `json:"attempts"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(completionJSON, &mcpAnswer); err != nil {
		t.Fatal(err)
	}
	if mcpAnswer.Text != "shared answer" || mcpAnswer.Model != "fake-model" || len(mcpAnswer.Routing.Attempts) != 2 {
		t.Fatalf("MCP completion = %s", completionJSON)
	}
}

func integrationMCPClient(t *testing.T, service *app.Service) (*mcp.ClientSession, func()) {
	t.Helper()
	server := mcpserver.NewServer(service)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

type integrationProvider struct{}

func (integrationProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "fake", Label: "Fake", URL: "https://example.invalid", Working: true, DefaultModel: "fake-model", SupportedModels: []string{"fake-model"}}
}
func (integrationProvider) SupportsModel(model string) bool { return model == "fake-model" }
func (integrationProvider) ChatCompletion(_ context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{Object: "chat.completion", Model: model, Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "shared answer"}}}}, nil
}
func (integrationProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}

type integrationFlakyProvider struct{}

func (integrationFlakyProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "flaky", Label: "Flaky", URL: "https://example.invalid", Working: true, DefaultModel: "flaky-model", SupportedModels: []string{"flaky-model"}}
}
func (integrationFlakyProvider) SupportsModel(model string) bool { return model == "flaky-model" }
func (integrationFlakyProvider) ChatCompletion(_ context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	return nil, app.NewProviderFailure(app.FailureUnavailable, "flaky", model, http.StatusNotFound, errors.New("model not available"))
}
func (integrationFlakyProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return app.NewProviderFailure(app.FailureUnavailable, "flaky", "flaky-model", http.StatusNotFound, errors.New("model not available"))
}
