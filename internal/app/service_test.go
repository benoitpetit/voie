package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestServiceResolvesExplicitAndDefaultModels(t *testing.T) {
	registry := NewRegistry()
	perplexity := &testProvider{info: ProviderInfo{Name: "perplexity", Working: true, DefaultModel: "turbo", SupportedModels: []string{"turbo"}}}
	duckai := &testProvider{info: ProviderInfo{Name: "duckai", Working: true, DefaultModel: "luna", SupportedModels: []string{"luna", "alias"}}}
	registry.Register("perplexity", perplexity)
	registry.Register("duckai", duckai)
	service, err := NewService(registry, ServiceOptions{DefaultProvider: "duckai", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, provider, model, wantProvider, wantModel string }{
		{"explicit model", "", "alias", "duckai", "alias"},
		{"configured generic", "", "openai", "duckai", "luna"},
		{"explicit provider generic", "perplexity", "openai", "perplexity", "turbo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := service.Complete(context.Background(), CompletionRequest{Provider: tc.provider, Model: tc.model, Messages: []Message{{Role: "user", Content: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			if response.Provider != tc.wantProvider {
				t.Errorf("response provider = %q, want %q", response.Provider, tc.wantProvider)
			}
			p := duckai
			if tc.wantProvider == "perplexity" {
				p = perplexity
			}
			if p.gotModel != tc.wantModel {
				t.Errorf("provider model = %q, want %q", p.gotModel, tc.wantModel)
			}
		})
	}
}

func TestServiceUsesPerplexityTurboWithoutConfiguredDefault(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "perplexity", Working: true, DefaultModel: "turbo", SupportedModels: []string{"turbo"}}}
	registry.Register("perplexity", provider)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), CompletionRequest{Model: "openai", Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if provider.gotModel != "turbo" {
		t.Fatalf("model = %q, want turbo", provider.gotModel)
	}
}

func TestServiceRejectsUnknownProviderModelMismatchAndDisabledProvider(t *testing.T) {
	registry := NewRegistry()
	registry.Register("active", &testProvider{info: ProviderInfo{Name: "active", Working: true, DefaultModel: "a", SupportedModels: []string{"a"}}})
	registry.Register("disabled", &testProvider{info: ProviderInfo{Name: "disabled", Working: false, DefaultModel: "b", SupportedModels: []string{"b"}}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	msg := []Message{{Role: "user", Content: "hi"}}
	cases := []struct {
		name     string
		req      CompletionRequest
		sentinel error
	}{
		{"unknown model", CompletionRequest{Model: "not-real", Messages: msg}, ErrUnknownModel},
		{"unknown provider", CompletionRequest{Provider: "missing", Model: "openai", Messages: msg}, ErrUnknownProvider},
		{"mismatch", CompletionRequest{Provider: "active", Model: "b", Messages: msg}, ErrProviderModelMismatch},
		{"disabled", CompletionRequest{Provider: "disabled", Model: "openai", Messages: msg}, ErrProviderDisabled},
		{"empty messages", CompletionRequest{Model: "a"}, ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Complete(context.Background(), tc.req)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error = %v, want %v", err, tc.sentinel)
			}
		})
	}
}

func TestServiceHonorsExplicitProviderForSharedModelAndDiscoveryRouting(t *testing.T) {
	registry := NewRegistry()
	duckAI := &testProvider{info: ProviderInfo{Name: "duckai", Label: "Duck.ai", Working: true, DefaultModel: "canonical", SupportedModels: []string{"canonical", "alias"}}}
	perplexity := &testProvider{info: ProviderInfo{Name: "perplexity", Label: "Perplexity", Working: true, DefaultModel: "alias", SupportedModels: []string{"alias"}}}
	registry.Register("perplexity", perplexity)
	registry.Register("duckai", duckAI)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := []Message{{Role: "user", Content: "hello"}}
	response, err := service.Complete(context.Background(), CompletionRequest{Provider: "perplexity", Model: "alias", Messages: messages})
	if err != nil {
		t.Fatalf("explicit provider completion: %v", err)
	}
	if response.Provider != "perplexity" {
		t.Fatalf("response provider = %q, want perplexity", response.Provider)
	}

	var alias *ModelInfo
	for _, model := range service.ListModels() {
		if model.ID == "alias" {
			alias = &model
			break
		}
	}
	if alias == nil {
		t.Fatal("alias missing from model catalogue")
	}
	if got := alias.Meta["provider"]; got != "duckai" {
		t.Fatalf("alias catalogue provider = %v, want default route duckai", got)
	}
}

func TestServiceAcceptsToolConversationMessages(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}}}
	registry.Register("test", provider)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := []Message{
		{Role: "user", Content: "calculate"},
		{Role: "assistant", ToolCalls: []any{map[string]any{"id": "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "2"},
	}
	if _, err := service.Complete(context.Background(), CompletionRequest{Model: "model", Messages: messages}); err != nil {
		t.Fatalf("tool conversation rejected: %v", err)
	}
}

func TestServiceRejectsEmptyUpstreamResponseAndTimesOut(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}}, response: &ChatCompletionResponse{}}
	service, err := NewService(registry, ServiceOptions{Timeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	registry.Register("test", provider)
	if _, err := service.Complete(context.Background(), CompletionRequest{Provider: "test", Model: "model", Messages: []Message{{Role: "user", Content: "hi"}}}); !errors.Is(err, ErrUpstream) {
		t.Fatalf("empty response error = %v, want ErrUpstream", err)
	}
	provider.waitForContext = true
	if _, err := service.Complete(context.Background(), CompletionRequest{Provider: "test", Model: "model", Messages: []Message{{Role: "user", Content: "hi"}}}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout error = %v, want ErrTimeout", err)
	}
}

func TestServiceStreamPassesCancellation(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Working: true, DefaultModel: "model", SupportedModels: []string{"model"}}, waitForContext: true}
	registry.Register("test", provider)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = service.CompleteStream(ctx, CompletionRequest{Provider: "test", Model: "model", Messages: []Message{{Role: "user", Content: "hi"}}}, func(string) {})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("stream error = %v, want ErrCanceled", err)
	}
}

func TestServiceListsStableCatalogue(t *testing.T) {
	registry := NewRegistry()
	registry.Register("z", &testProvider{info: ProviderInfo{Name: "z", Label: "Zulu", Working: true, SupportedModels: []string{"z-model"}}})
	registry.Register("a", &testProvider{info: ProviderInfo{Name: "a", Label: "Alpha", Working: true, SupportedModels: []string{"a-model", "z-model"}}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	models := service.ListModels()
	if got, want := []string{models[0].ID, models[1].ID}, []string{"a-model", "z-model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model ids = %v, want %v", got, want)
	}
}

func TestServiceListsProviderMetadataWithoutHealthRequests(t *testing.T) {
	registry := NewRegistry()
	registry.Register("z", &testProvider{info: ProviderInfo{Name: "z", Working: true, DefaultModel: "z", SupportedModels: []string{"z"}}})
	registry.Register("a", &testProvider{info: ProviderInfo{Name: "a", Working: true, DefaultModel: "a", SupportedModels: []string{"a"}}})
	service, err := NewService(registry, ServiceOptions{HealthProbe: func(context.Context, string) bool { t.Fatal("metadata listing must not probe providers"); return false }})
	if err != nil {
		t.Fatal(err)
	}
	got := service.ListProviderMetadata()
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "z" || got[0].Alive || got[1].Alive {
		t.Fatalf("metadata = %+v", got)
	}
}

type testProvider struct {
	info           ProviderInfo
	response       *ChatCompletionResponse
	gotModel       string
	waitForContext bool
}

func (p *testProvider) ChatCompletion(ctx context.Context, _ []Message, model string) (*ChatCompletionResponse, error) {
	p.gotModel = model
	if p.waitForContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.response != nil {
		return p.response, nil
	}
	return &ChatCompletionResponse{Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}}}, nil
}
func (p *testProvider) ChatCompletionStream(ctx context.Context, _ []Message, _ string, _ func(string)) error {
	if p.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (p *testProvider) GetInfo() ProviderInfo { return p.info }
func (p *testProvider) SupportsModel(model string) bool {
	for _, supported := range p.info.SupportedModels {
		if model == supported {
			return true
		}
	}
	return false
}
