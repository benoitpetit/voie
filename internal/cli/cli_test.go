package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
	sqlitestore "github.com/benoitpetit/voie/internal/storage/sqlite"
	"github.com/spf13/cobra"
)

func TestChatUsesSharedServiceAndWritesOnlyAnswer(t *testing.T) {
	rt, provider := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	err := executeForTest(context.Background(), []string{"chat", "--model", "model", "--provider", "test", "hello", "world"}, strings.NewReader("unused"), &stdout, &stderr, rt)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "answer\n" || !strings.Contains(stderr.String(), "accepted") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "hello world") || strings.Contains(stderr.String(), "answer") || strings.Contains(stderr.String(), "\033[") {
		t.Fatalf("stderr contains completion content or color codes: %q", stderr.String())
	}
	if provider.model != "model" || provider.messages[0].Content != "hello world" {
		t.Fatalf("provider request model=%q messages=%+v", provider.model, provider.messages)
	}
}

func TestChatHelpListsEverySupportedTask(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := executeForTest(context.Background(), []string{"chat", "--help"}, nil, &stdout, &stderr, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"coding", "reasoning", "writing", "translation", "summarization", "general", "auto", "ensemble"} {
		if !strings.Contains(stdout.String(), value) {
			t.Errorf("chat help does not mention %q:\n%s", value, stdout.String())
		}
	}
}

func TestChatTaskFlagCompletesSupportedTasks(t *testing.T) {
	root := NewRootCommand(context.Background(), nil, io.Discard, io.Discard, nil)
	chat, _, err := root.Find([]string{"chat"})
	if err != nil {
		t.Fatal(err)
	}
	completion, ok := chat.GetFlagCompletionFunc("task")
	if !ok {
		t.Fatal("--task has no shell completion function")
	}
	got, directive := completion(chat, nil, "")
	want := []string{"coding", "reasoning", "writing", "translation", "summarization", "general"}
	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("task completions=%v, want %v", got, want)
	}
	if directive&cobra.ShellCompDirectiveNoFileComp == 0 {
		t.Fatalf("completion directive=%v, want no file completions", directive)
	}
}

func TestChatReadsPromptFromStdin(t *testing.T) {
	rt, provider := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	err := executeForTest(context.Background(), []string{"chat", "--model", "model"}, strings.NewReader("from stdin\n"), &stdout, &stderr, rt)
	if err != nil {
		t.Fatal(err)
	}
	if provider.messages[0].Content != "from stdin" || stdout.String() != "answer\n" {
		t.Fatalf("message=%+v output=%q", provider.messages, stdout.String())
	}
}

func TestListCommandsSupportReadableAndJSONOutput(t *testing.T) {
	rt, _ := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"models", "--json"}, nil, &stdout, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	var models []app.ModelInfo
	var wrapped struct {
		Data []app.ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &wrapped); err != nil {
		t.Fatal(err)
	}
	models = wrapped.Data
	if len(models) != 1 || models[0].ID != "model" {
		t.Fatalf("models=%+v", models)
	}
	stdout.Reset()
	if err := executeForTest(context.Background(), []string{"providers", "--json"}, nil, &stdout, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	var providers []app.ProviderInfo
	var providerList struct {
		Data []app.ProviderInfo `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &providerList); err != nil {
		t.Fatal(err)
	}
	providers = providerList.Data
	if len(providers) != 1 || providers[0].Name != "test" || !providers[0].Alive {
		t.Fatalf("providers=%+v", providers)
	}
	stdout.Reset()
	if err := executeForTest(context.Background(), []string{"models"}, nil, &stdout, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "model") {
		t.Fatalf("readable models output = %q", stdout.String())
	}
}

func TestServeAndMCPDispatch(t *testing.T) {
	rt, _ := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"serve", "--host", "localhost", "--port", "9090"}, nil, &stdout, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	if rt.host != "localhost" || rt.port != "9090" {
		t.Fatalf("serve address = %s:%s", rt.host, rt.port)
	}
	if err := executeForTest(context.Background(), []string{"mcp"}, nil, &stdout, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	if !rt.mcpCalled {
		t.Fatal("MCP dispatch was not called")
	}
}

func TestCLIRejectsInvalidArgumentsAndReturnsProviderErrors(t *testing.T) {
	rt, provider := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"chat"}, strings.NewReader("prompt"), &stdout, &stderr, rt); err == nil {
		t.Fatal("chat accepted missing --model")
	}
	if err := executeForTest(context.Background(), []string{"unknown"}, nil, &stdout, &stderr, rt); err == nil {
		t.Fatal("unknown command accepted")
	}
	provider.err = context.DeadlineExceeded
	if err := executeForTest(context.Background(), []string{"chat", "--model", "model", "prompt"}, nil, &stdout, &stderr, rt); !errors.Is(err, app.ErrTimeout) {
		t.Fatalf("chat error=%v, want timeout", err)
	}
}

func TestChatAllowsAutomaticStrategiesWithoutModel(t *testing.T) {
	rt, provider := newCLITestRuntime(t)
	var out, stderr bytes.Buffer
	err := executeForTest(context.Background(), []string{"chat", "--strategy", "auto", "--task", "coding", "hello"}, nil, &out, &stderr, rt)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "answer\n" || provider.model != "model" {
		t.Fatalf("out=%q model=%q", out.String(), provider.model)
	}
}

func TestChatRejectsInvalidStrategyFlagCombinations(t *testing.T) {
	rt, _ := newCLITestRuntime(t)
	var out, stderr bytes.Buffer
	for _, args := range [][]string{{"chat", "--strategy", "bogus", "hello"}, {"chat", "--strategy", "auto", "--models", "model", "hello"}} {
		if err := executeForTest(context.Background(), args, nil, &out, &stderr, rt); err == nil {
			t.Errorf("accepted args %v", args)
		}
	}
}

func TestConversationCommandsCreateListShowDelete(t *testing.T) {
	rt, p := newCLITestRuntime(t)
	store, err := sqlitestore.NewSQLiteStore(t.TempDir()+"/cli.db", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := app.NewRegistry()
	r.Register("test", p)
	rt.service, err = app.NewService(r, app.ServiceOptions{Conversations: store})
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err = executeForTest(context.Background(), []string{"conversations", "create"}, nil, &out, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(out.String())
	if id == "" || strings.Contains(id, " ") {
		t.Fatalf("id=%q", id)
	}
	out.Reset()
	if err = executeForTest(context.Background(), []string{"chat", "--model", "model", "--conversation", id, "hello"}, nil, &out, &stderr, rt); err != nil || out.String() != "answer\n" {
		t.Fatalf("classic conversation chat output=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err = executeForTest(context.Background(), []string{"conversations", "list"}, nil, &out, &stderr, rt); err != nil || !strings.Contains(out.String(), id) {
		t.Fatalf("list=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err = executeForTest(context.Background(), []string{"conversations", "show", id}, nil, &out, &stderr, rt); err != nil || !strings.Contains(out.String(), `"turns"`) {
		t.Fatalf("show=%q err=%v", out.String(), err)
	}
	out.Reset()
	if err = executeForTest(context.Background(), []string{"conversations", "delete", id}, nil, &out, &stderr, rt); err != nil || out.Len() != 0 {
		t.Fatalf("delete output=%q err=%v", out.String(), err)
	}
}

func TestVersionCommandsDoNotInitializeRuntime(t *testing.T) {
	originalVersion := brand.Version
	brand.Version = "dev"
	t.Cleanup(func() { brand.Version = originalVersion })

	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			factoryCalls := 0
			var stdout, stderr bytes.Buffer
			err := Execute(context.Background(), args, nil, &stdout, &stderr, func() (Runtime, error) {
				factoryCalls++
				return nil, errors.New("runtime must not initialize")
			})
			if err != nil {
				t.Fatal(err)
			}
			if stdout.String() != "voie dev\n" || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if factoryCalls != 0 {
				t.Fatalf("runtime factory called %d times", factoryCalls)
			}
		})
	}
}

func TestUpdateRejectsDevelopmentBuildWithoutInitializingRuntime(t *testing.T) {
	originalVersion := brand.Version
	brand.Version = "dev"
	t.Cleanup(func() { brand.Version = originalVersion })

	factoryCalls := 0
	var stdout, stderr bytes.Buffer
	err := Execute(context.Background(), []string{"update"}, nil, &stdout, &stderr, func() (Runtime, error) {
		factoryCalls++
		return nil, errors.New("runtime must not initialize")
	})
	if err == nil || !strings.Contains(err.Error(), "development") {
		t.Fatalf("update error = %v, want development-build error", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("runtime factory called %d times", factoryCalls)
	}
}

func executeForTest(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, runtime Runtime) error {
	return Execute(ctx, args, stdin, stdout, stderr, func() (Runtime, error) { return runtime, nil })
}

func newCLITestRuntime(t *testing.T) (*cliTestRuntime, *cliTestProvider) {
	t.Helper()
	registry := app.NewRegistry()
	provider := &cliTestProvider{}
	registry.Register("test", provider)
	service, err := app.NewService(registry, app.ServiceOptions{Timeout: time.Second, HealthProbe: func(context.Context, string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return &cliTestRuntime{service: service}, provider
}

type cliTestRuntime struct {
	service    *app.Service
	host, port string
	mcpCalled  bool
}

func (rt *cliTestRuntime) Service() *app.Service { return rt.service }
func (rt *cliTestRuntime) Serve(_ context.Context, host, port string) error {
	rt.host, rt.port = host, port
	return nil
}
func (rt *cliTestRuntime) MCP(context.Context) error { rt.mcpCalled = true; return nil }

type cliTestProvider struct {
	model    string
	messages []app.Message
	err      error
}

func (*cliTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test", Label: "Test", URL: "https://example.invalid", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}}
}
func (*cliTestProvider) SupportsModel(model string) bool { return model == "model" }
func (p *cliTestProvider) ChatCompletion(_ context.Context, messages []app.Message, model string) (*app.ChatCompletionResponse, error) {
	p.model = model
	p.messages = messages
	if p.err != nil {
		return nil, p.err
	}
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "answer"}}}}, nil
}
func (*cliTestProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}

func TestChatFallbackHelpListsOverrideFlags(t *testing.T) {
	rt, _ := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"chat", "--help"}, strings.NewReader(""), &stdout, &stderr, rt); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--fallback", "--no-fallback", "--fallback-retries", "--fallback-limit", "--fallback-model"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Fatalf("chat help missing %q: %s", flag, stdout.String())
		}
	}
}

func TestChatFallbackDisabledSkipsRetry(t *testing.T) {
	registry := app.NewRegistry()
	provider := &cliFlakyProvider{failures: 1, err: app.NewProviderFailure(app.FailureTransient, "flaky", "flaky-model", 503, errors.New("down"))}
	registry.Register("flaky", provider)
	service, err := app.NewService(registry, app.ServiceOptions{Timeout: time.Second, HealthProbe: func(context.Context, string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	rt := &cliTestRuntime{service: service}
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"chat", "--model", "flaky-model", "hello"}, strings.NewReader(""), &stdout, &stderr, rt); err != nil {
		t.Fatalf("default retry error = %v", err)
	}
	if stdout.String() != "answer\n" {
		t.Fatalf("default stdout = %q, want only the answer", stdout.String())
	}
	provider.failures = 1
	provider.calls = 0
	stdout.Reset()
	if err := executeForTest(context.Background(), []string{"chat", "--model", "flaky-model", "--no-fallback", "hello"}, strings.NewReader(""), &stdout, &stderr, rt); err == nil {
		t.Fatalf("--no-fallback unexpectedly succeeded: %q", stdout.String())
	}
	if provider.calls != 1 {
		t.Fatalf("--no-fallback made %d calls, want exactly one attempt with retries disabled", provider.calls)
	}
}

func TestChatFallbackModelListAccepted(t *testing.T) {
	rt, _ := newCLITestRuntime(t)
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"chat", "--model", "model", "--fallback-model", "model", "--fallback-retries", "0", "hello"}, strings.NewReader(""), &stdout, &stderr, rt); err != nil {
		t.Fatalf("valid fallback flags rejected: %v", err)
	}
	if stdout.String() != "answer\n" {
		t.Fatalf("stdout = %q, want only the answer", stdout.String())
	}
}

func TestChatFallbackValidatesFlagsBeforeCompletion(t *testing.T) {
	invalidArgs := [][]string{
		{"chat", "--model", "model", "--fallback-retries", "5", "hello"},
		{"chat", "--model", "model", "--fallback-limit", "9", "hello"},
		{"chat", "--model", "model", "--fallback", "--no-fallback", "hello"},
	}
	for _, args := range invalidArgs {
		var stdout, stderr bytes.Buffer
		if err := executeForTest(context.Background(), args, strings.NewReader(""), &stdout, &stderr, mustCLITestRuntime(t)); err == nil {
			t.Fatalf("args %v unexpectedly succeeded: %q", args, stdout.String())
		}
	}
	registry := app.NewRegistry()
	provider := &cliFlakyProvider{}
	registry.Register("flaky", provider)
	service, err := app.NewService(registry, app.ServiceOptions{Timeout: time.Second, HealthProbe: func(context.Context, string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	rt := &cliTestRuntime{service: service}
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"chat", "--model", "flaky-model", "--fallback-model", "bogus", "hello"}, strings.NewReader(""), &stdout, &stderr, rt); err == nil {
		t.Fatalf("unknown fallback model unexpectedly succeeded: %q", stdout.String())
	}
	if provider.calls != 0 {
		t.Fatalf("provider called %d times before fallback validation", provider.calls)
	}
}

func mustCLITestRuntime(t *testing.T) *cliTestRuntime {
	t.Helper()
	rt, _ := newCLITestRuntime(t)
	return rt
}

type cliFlakyProvider struct {
	failures int
	err      error
	calls    int
}

func (*cliFlakyProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "flaky", Label: "Flaky", URL: "https://example.invalid", Working: true, DefaultModel: "flaky-model", SupportedModels: []string{"flaky-model"}}
}
func (*cliFlakyProvider) SupportsModel(model string) bool { return model == "flaky-model" }
func (p *cliFlakyProvider) ChatCompletion(_ context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	p.calls++
	if p.failures > 0 {
		p.failures--
		return nil, p.err
	}
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "answer"}}}}, nil
}
func (*cliFlakyProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}
