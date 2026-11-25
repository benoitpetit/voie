package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
	sqlitestore "github.com/benoitpetit/voie/internal/storage/sqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

func TestModelsOutputHasProviderAndModel(t *testing.T) {
	rt, _ := newCLITestRuntime(t)
	var stdout bytes.Buffer
	if err := runModels(&stdout, rt.Service(), false, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "test provider") && !strings.Contains(stdout.String(), "MODEL") {
		t.Fatalf("grouped output = %q", stdout.String())
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
	err := executeForTest(context.Background(), []string{"chat", "--strategy", "auto", "--task", "general", "hello"}, nil, &out, &stderr, rt)
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

func TestCliStateDetectsAllInstalledAgentCLIs(t *testing.T) {
	previous := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/agent", nil }
	t.Cleanup(func() { lookPath = previous })
	for _, name := range []string{"cline", "kiro", "antigravity", "codebuddy", "qoder", "cursor", "minimax-code", "devin", "copilot-cli"} {
		if got := cliState(name); got != "installed" {
			t.Errorf("cliState(%q) = %q, want installed", name, got)
		}
	}
}

func TestHealthcheckPortOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	command := newHealthcheckCommand(server.Client())
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--port", port})
	if err := command.Execute(); err != nil {
		t.Fatalf("healthcheck --port %s: %v", port, err)
	}
}

type tierTestProvider struct{}

func (*tierTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "test", Label: "Test", Working: true,
		SupportedModels: []string{"free-model", "paid-model"}, FreeModels: []string{"free-model"}}
}
func (*tierTestProvider) SupportsModel(model string) bool {
	return model == "free-model" || model == "paid-model"
}
func (*tierTestProvider) ChatCompletion(context.Context, []app.Message, string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{}, nil
}
func (*tierTestProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}

func TestModelsTierFlags(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("test", &tierTestProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args   []string
		want   []string
		absent []string
	}{
		{args: nil, want: []string{"TIER", "free-model", "paid-model"}},
		{args: []string{"--free"}, want: []string{"free-model"}, absent: []string{"paid-model"}},
		{args: []string{"--paid"}, want: []string{"paid-model", "price unknown"}, absent: []string{"free-model"}},
		{args: []string{"--all"}, want: []string{"free-model", "paid-model"}},
		{args: []string{"--provider", "test", "--paid"}, want: []string{"paid-model"}, absent: []string{"free-model"}},
	} {
		var stdout, stderr bytes.Buffer
		command := newModelsCommand(func() (*app.Service, error) { return service, nil })
		command.SetOut(&stdout)
		command.SetErr(&stderr)
		command.SetArgs(tc.args)
		if err := command.Execute(); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%v: output missing %q: %s", tc.args, want, stdout.String())
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(stdout.String(), absent) {
				t.Errorf("%v: output unexpectedly contains %q: %s", tc.args, absent, stdout.String())
			}
		}
	}
}

func TestModelsRejectsConflictingTierFlags(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("test", &tierTestProvider{})
	service, _ := app.NewService(registry, app.ServiceOptions{})
	var stdout, stderr bytes.Buffer
	command := newModelsCommand(func() (*app.Service, error) { return service, nil })
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--free", "--paid"})
	if err := command.Execute(); err == nil {
		t.Fatal("conflicting tier flags were accepted")
	}
}

func TestModelsAllConflictsAndUnknownProvider(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("test", &tierTestProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--all", "--free"}, {"--provider", "missing"}} {
		var stdout, stderr bytes.Buffer
		command := newModelsCommand(func() (*app.Service, error) { return service, nil })
		command.SetOut(&stdout)
		command.SetErr(&stderr)
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

type subscribeTestProvider struct{}

func (*subscribeTestProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "sub", Label: "Sub", Working: true,
		SupportedModels: []string{"sub-auto"}, NoFreeModels: true, Subscription: true}
}
func (*subscribeTestProvider) SupportsModel(model string) bool { return model == "sub-auto" }
func (*subscribeTestProvider) ChatCompletion(context.Context, []app.Message, string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{}, nil
}
func (*subscribeTestProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}

func TestModelsOutputShowsSubscribePrice(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("sub", &subscribeTestProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runModels(&stdout, service, false, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "subscribe") {
		t.Fatalf("models output = %q, want subscribe label", stdout.String())
	}
}

func TestBuildFallbackOverrideInfersEnablement(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.StringArray("fallback-model", nil, "")
	if err := flags.Set("fallback-model", "other"); err != nil {
		t.Fatal(err)
	}
	override, err := buildFallbackOverride(flags, false, false, 0, 0, []string{"other"})
	if err != nil {
		t.Fatal(err)
	}
	if override == nil || override.Enabled == nil || !*override.Enabled {
		t.Fatalf("override = %+v, want enabled fallback", override)
	}
	flags2 := pflag.NewFlagSet("test2", pflag.ContinueOnError)
	flags2.Bool("no-fallback", false, "")
	if err := flags2.Set("no-fallback", "true"); err != nil {
		t.Fatal(err)
	}
	override2, err := buildFallbackOverride(flags2, false, true, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if override2 == nil || override2.Enabled == nil || *override2.Enabled {
		t.Fatalf("override = %+v, want disabled fallback", override2)
	}
}

func TestChatRejectsStrategyFlagConflictsEarly(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"chat", "--strategy", "auto", "--model", "model", "hello"}, "--model is only valid with --strategy classic"},
		{[]string{"chat", "--strategy", "ensemble", "--model", "model", "hello"}, "--model is only valid with --strategy classic"},
		{[]string{"chat", "--strategy", "ensemble", "--ensemble-model", "model", "hello"}, "ensemble requires at least two models"},
	} {
		var stdout, stderr bytes.Buffer
		err := executeForTest(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, mustCLITestRuntime(t))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args %v error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

type cliEnsembleProvider struct{}

func (*cliEnsembleProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "ensemble-test", Label: "Ensemble", Working: true,
		SupportedModels: []string{"a", "b", "synth"}, SupportsStream: true}
}
func (*cliEnsembleProvider) SupportsModel(model string) bool {
	return model == "a" || model == "b" || model == "synth"
}
func (*cliEnsembleProvider) ChatCompletion(_ context.Context, _ []app.Message, model string) (*app.ChatCompletionResponse, error) {
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "answer from " + model}}}}, nil
}
func (*cliEnsembleProvider) ChatCompletionStream(_ context.Context, _ []app.Message, model string, callback func(string)) error {
	callback("answer from " + model)
	return nil
}

func TestChatExecutesEnsembleAcrossCLI(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("ensemble-test", &cliEnsembleProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{SynthesisModel: "synth", Timeout: time.Second,
		HealthProbe: func(context.Context, string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	rt := &cliTestRuntime{service: service}
	var stdout, stderr bytes.Buffer
	err = executeForTest(context.Background(), []string{
		"chat", "--strategy", "ensemble", "--ensemble-mode", "independent",
		"--ensemble-model", "a", "--ensemble-model", "b", "hello",
	}, strings.NewReader(""), &stdout, &stderr, rt)
	if err != nil {
		t.Fatalf("ensemble chat error: %v", err)
	}
	if !strings.Contains(stdout.String(), "answer from synth") {
		t.Fatalf("stdout = %q, want synthesized answer", stdout.String())
	}
}

func TestChatRejectsInvalidStrategyBeforeModel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := executeForTest(context.Background(), []string{"chat", "--strategy", "bogus", "hello"}, strings.NewReader(""), &stdout, &stderr, mustCLITestRuntime(t))
	if err == nil || !strings.Contains(err.Error(), "strategy must be classic, auto, or ensemble") {
		t.Fatalf("error = %v, want invalid strategy error", err)
	}
}

func TestChatRejectsSingleLegacyEnsembleModelEarly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := executeForTest(context.Background(), []string{"chat", "--strategy", "ensemble", "--models", "model", "hello"}, strings.NewReader(""), &stdout, &stderr, mustCLITestRuntime(t))
	if err == nil || !strings.Contains(err.Error(), "ensemble requires at least two models") {
		t.Fatalf("error = %v, want early minimum-models error", err)
	}
}

func TestChatHelpDocumentsEveryStrategy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := executeForTest(context.Background(), []string{"chat", "--help"}, nil, &stdout, &stderr, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Classic:", "Auto:", "Ensemble:",
		`voie chat --model MODEL_ID "Summarize this text"`,
		`voie chat --strategy auto --task coding "Review this code"`,
		`voie chat --strategy ensemble --ensemble-mode debate --debate-turns 2`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("chat help missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestChatHelpAdaptsToStrategyFlag(t *testing.T) {
	cases := map[string][]string{
		"classic":  {"Classic:", `voie chat --model MODEL_ID "Summarize this text"`},
		"auto":     {"Auto:", `voie chat --strategy auto --task coding "Review this code"`},
		"ensemble": {"Ensemble:", `voie chat --strategy ensemble --ensemble-mode debate --debate-turns 2`},
	}
	for strategy, wants := range cases {
		var stdout, stderr bytes.Buffer
		if err := executeForTest(context.Background(), []string{"chat", "--strategy", strategy, "--help"}, nil, &stdout, &stderr, nil); err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("--strategy %s help missing %q:\n%s", strategy, want, stdout.String())
			}
		}
		for _, other := range []string{"Classic:", "Auto:", "Ensemble:"} {
			if strings.Contains(other, strings.ToUpper(strategy[:1])+strategy[1:]+":") {
				continue
			}
			if strings.Contains(stdout.String(), other) {
				t.Errorf("--strategy %s help unexpectedly shows %s:\n%s", strategy, other, stdout.String())
			}
		}
	}
}

func TestChatStreamWritesModelChunks(t *testing.T) {
	registry := app.NewRegistry()
	registry.Register("ensemble-test", &cliEnsembleProvider{})
	service, err := app.NewService(registry, app.ServiceOptions{SynthesisModel: "synth", Timeout: time.Second,
		HealthProbe: func(context.Context, string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	rt := &cliTestRuntime{service: service}
	var stdout, stderr bytes.Buffer
	err = executeForTest(context.Background(), []string{"chat", "--model", "a", "--stream", "hello"}, strings.NewReader(""), &stdout, &stderr, rt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "answer from a") {
		t.Fatalf("stdout = %q, want streamed model chunk", stdout.String())
	}
}
