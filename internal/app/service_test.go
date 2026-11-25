package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/pricing"
)

func TestNormalizeRequestStrategyDefaultsToClassic(t *testing.T) {
	if got := normalizeStrategy(""); got != StrategyClassic {
		t.Fatalf("normalized empty strategy = %q, want %q", got, StrategyClassic)
	}
}

func TestClassicResponseOmitsRoutingAndConversationFields(t *testing.T) {
	data, err := json.Marshal(ChatCompletionResponse{ID: "chatcmpl-test", Object: "chat.completion", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "routing") || strings.Contains(string(data), "conversation_id") {
		t.Fatalf("classic response contains opt-in metadata: %s", data)
	}
}

func TestClassicCompletionPreservesUpstreamModelField(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Working: true, SupportedModels: []string{"requested"}}, response: &ChatCompletionResponse{Model: "upstream-canonical", Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}}}}
	registry.Register("test", provider)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{Model: "requested", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "upstream-canonical" {
		t.Fatalf("classic model=%q, want provider response model", response.Model)
	}
}

func TestCompletionProgressEventsAndCallbackPanicIsolation(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Working: true, SupportedModels: []string{"model"}}}
	registry.Register("test", provider)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var events []ProgressEvent
	request := CompletionRequest{
		Model: "model", Messages: []Message{{Role: "user", Content: "secret prompt"}},
		OnProgress: func(event ProgressEvent) { events = append(events, event) },
	}
	if _, err := service.Complete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 || events[0].Stage != "accepted" || events[0].Status != "started" {
		t.Fatalf("progress events = %#v", events)
	}
	for _, event := range events {
		if strings.Contains(event.Message, "secret prompt") {
			t.Fatalf("progress leaked prompt: %#v", event)
		}
	}
	request.OnProgress = func(ProgressEvent) { panic("observer failure") }
	if _, err := service.Complete(context.Background(), request); err != nil {
		t.Fatalf("observer panic changed completion result: %v", err)
	}
}

func TestStreamCompletionEmitsProgressEvents(t *testing.T) {
	registry := NewRegistry()
	provider := &testProvider{info: ProviderInfo{Name: "test", Working: true, SupportedModels: []string{"model"}}}
	registry.Register("test", provider)
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var events []ProgressEvent
	err = service.CompleteStream(context.Background(), CompletionRequest{
		Model: "model", Messages: []Message{{Role: "user", Content: "secret prompt"}},
		OnProgress: func(event ProgressEvent) { events = append(events, event) },
	}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("stream progress events = %#v", events)
	}
}

func TestServiceRejectsUnknownConfiguredSynthesisModel(t *testing.T) {
	registry := NewRegistry()
	_, err := NewService(registry, ServiceOptions{SynthesisModel: "missing"})
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("error=%v, want ErrUnknownModel", err)
	}
}

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
		if model.ID == "dk-alias" {
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
	var events []ProgressEvent
	if _, err := service.Complete(context.Background(), CompletionRequest{Provider: "test", Model: "model", Messages: []Message{{Role: "user", Content: "hi"}}, OnProgress: func(event ProgressEvent) { events = append(events, event) }}); !errors.Is(err, ErrUpstream) {
		t.Fatalf("empty response error = %v, want ErrUpstream", err)
	}
	failed := false
	for _, event := range events {
		failed = failed || event.Stage == "model" && event.Status == "failed"
	}
	if !failed {
		t.Fatalf("empty upstream response did not emit failed model progress: %#v", events)
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

func TestListModelsKeepsDeclaringProviderForSharedIDs(t *testing.T) {
	registry := NewRegistry()
	registry.Register("deepai", &testProvider{info: ProviderInfo{Name: "deepai", Label: "DeepAI", Working: true, SupportedModels: []string{"gpt-6-luna"}}})
	registry.Register("duckai", &testProvider{info: ProviderInfo{Name: "duckai", Label: "Duck.ai", Working: true, SupportedModels: []string{"gpt-6-luna"}}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"dp-gpt-6-luna": "deepai", "dk-gpt-6-luna": "duckai"}
	for _, model := range service.ListModels() {
		if provider, ok := want[model.ID]; ok {
			if model.Meta["provider"] != provider {
				t.Errorf("%s: meta.provider = %v, want %s", model.ID, model.Meta["provider"], provider)
			}
			delete(want, model.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing catalogue entries: %v", want)
	}
}

func TestListModelsTiersAndFilters(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test", &testProvider{info: ProviderInfo{
		Name: "test", Label: "Test", Working: true,
		SupportedModels: []string{"free-model", "paid-model"},
		FreeModels:      []string{"free-model"},
	}})
	registry.Register("other", &testProvider{info: ProviderInfo{
		Name: "other", Label: "Other", Working: true, SupportedModels: []string{"other-model"},
	}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	all := service.ListModelsFiltered("", "")
	if len(all) != 3 {
		t.Fatalf("all = %d models, want 3", len(all))
	}
	tiers := map[string]ModelTier{}
	for _, model := range all {
		tiers[model.ID] = model.Tier
	}
	if tiers["free-model"] != TierFree || tiers["paid-model"] != TierPaid || tiers["other-model"] != TierFree {
		t.Fatalf("tiers = %v", tiers)
	}
	if got := service.ListModelsFiltered("", TierFree); len(got) != 2 {
		t.Fatalf("free = %d, want 2", len(got))
	}
	if got := service.ListModelsFiltered("", TierPaid); len(got) != 1 || got[0].ID != "paid-model" {
		t.Fatalf("paid = %+v, want only paid-model", got)
	}
	if got := service.ListModelsFiltered("test", TierPaid); len(got) != 1 {
		t.Fatalf("test/paid = %d, want 1", len(got))
	}
	if got := service.ListModelsFiltered("missing", ""); len(got) != 0 {
		t.Fatalf("unknown provider = %d, want 0", len(got))
	}
}

func TestListModelsAddsPublicPriceWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"foo/bar","pricing":{"prompt":"0.000001","completion":"0.000002"}}]}`))
	}))
	defer server.Close()
	pricing.SetSource(server.URL)
	t.Cleanup(func() { pricing.SetSource("") })
	registry := NewRegistry()
	registry.Register("openrouter", &testProvider{info: ProviderInfo{
		Name: "openrouter", Working: true, SupportedModels: []string{"foo/bar"},
		FreeModels: []string{"free-model"},
	}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	models := service.ListModelsFiltered("", "")
	if len(models) != 1 || models[0].Price == nil {
		t.Fatalf("models = %+v, want a price", models)
	}
	if models[0].Price.PromptPerToken != "0.000001" || models[0].Tier != TierPaid {
		t.Fatalf("model = %+v", models[0])
	}
}

func TestGenericModelFallsBackToFirstWorkingProvider(t *testing.T) {
	registry := NewRegistry()
	registry.Register("duckai", &testProvider{info: ProviderInfo{Name: "duckai", Working: true, DefaultModel: "luna", SupportedModels: []string{"luna"}}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{Model: "openai", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "duckai" {
		t.Fatalf("provider = %q, want duckai", response.Provider)
	}
}

type upstreamAwareProvider struct {
	info ProviderInfo
}

func (*upstreamAwareProvider) ChatCompletion(context.Context, []Message, string) (*ChatCompletionResponse, error) {
	return &ChatCompletionResponse{}, nil
}
func (*upstreamAwareProvider) ChatCompletionStream(context.Context, []Message, string, func(string)) error {
	return nil
}
func (p *upstreamAwareProvider) GetInfo() ProviderInfo   { return p.info }
func (*upstreamAwareProvider) SupportsModel(string) bool { return true }
func (*upstreamAwareProvider) UpstreamModel(model string) string {
	if model == "or-free-alias" {
		return "vendor/model:free"
	}
	return ""
}

func TestListModelsUsesUpstreamIDForPricingAndRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model:free","pricing":{"prompt":"0.000002","completion":"0.000006"}}]}`))
	}))
	defer server.Close()
	pricing.SetSource(server.URL)
	t.Cleanup(func() { pricing.SetSource("") })
	registry := NewRegistry()
	registry.Register("openrouter", &upstreamAwareProvider{info: ProviderInfo{
		Name: "openrouter", Working: true, SupportedModels: []string{"or-free-alias"},
	}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	models := service.ListModelsFiltered("", "")
	if len(models) != 1 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].Root != "vendor/model:free" || models[0].Price == nil || models[0].Price.PromptPerToken != "0.000002" {
		t.Fatalf("model = %+v, want upstream root and price", models[0])
	}
}

func TestPublicPrefixSelectsOwningProvider(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"duckai", "deepai"} {
		registry.Register(name, &testProvider{info: ProviderInfo{
			Name: name, Working: true, SupportedModels: []string{"gpt-6-luna"},
		}})
	}
	if got := registry.GetForModel("dp-gpt-6-luna"); got == nil || got.GetInfo().Name != "deepai" {
		t.Fatalf("dp- model routed to %v, want deepai", got)
	}
	if got := registry.GetForModel("dk-gpt-6-luna"); got == nil || got.GetInfo().Name != "duckai" {
		t.Fatalf("dk- model routed to %v, want duckai", got)
	}
}

func TestExplicitEmptyFreeListMeansNoFreeModels(t *testing.T) {
	registry := NewRegistry()
	registry.Register("gateway", &testProvider{info: ProviderInfo{
		Name: "gateway", Working: true, SupportedModels: []string{"paid-model"},
		FreeModels: []string{}, NoFreeModels: true,
	}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	models := service.ListModelsFiltered("", "")
	if len(models) != 1 || models[0].Tier != TierPaid {
		t.Fatalf("models = %+v, want paid", models)
	}
}

func TestProviderListingsUsePublicModelIDs(t *testing.T) {
	registry := NewRegistry()
	registry.Register("duckai", &testProvider{info: ProviderInfo{
		Name: "duckai", Working: true, DefaultModel: "gpt-6-luna",
		SupportedModels: []string{"gpt-6-luna"}, FreeModels: []string{"gpt-6-luna"},
	}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].SupportedModels[0] != "dk-gpt-6-luna" || providers[0].DefaultModel != "dk-gpt-6-luna" {
		t.Fatalf("provider info = %+v, want prefixed IDs", providers)
	}
	metadata := service.ListProviderMetadata()
	if len(metadata) != 1 || metadata[0].SupportedModels[0] != "dk-gpt-6-luna" {
		t.Fatalf("metadata = %+v, want prefixed IDs", metadata)
	}
}

func TestProviderInfoAlwaysExposesFreeModelsField(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test", &testProvider{info: ProviderInfo{Name: "test", Working: true}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	providers, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(providers[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"free_models":[]`) {
		t.Fatalf("provider JSON = %s, want explicit empty free_models", data)
	}
}

func TestListModelsIgnoresZeroPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[` +
			`{"id":"free/upstream","pricing":{"prompt":"0","completion":"0"}},` +
			`{"id":"paid/upstream","pricing":{"prompt":"0.000002","completion":"0.000006"}}]}`))
	}))
	defer server.Close()
	pricing.SetSource(server.URL)
	t.Cleanup(func() { pricing.SetSource("") })
	registry := NewRegistry()
	registry.Register("openrouter", &testProvider{info: ProviderInfo{
		Name: "openrouter", Working: true, SupportedModels: []string{"free/upstream", "paid/upstream"},
		FreeModels: []string{"free/upstream"},
	}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	models := service.ListModelsFiltered("", "")
	byID := map[string]ModelInfo{}
	for _, model := range models {
		byID[model.ID] = model
	}
	free := byID["or-free/upstream"]
	paid := byID["or-paid/upstream"]
	if free.Price != nil {
		t.Fatalf("free model has a zero price: %+v", free.Price)
	}
	if paid.Price == nil || paid.Price.PromptPerToken != "0.000002" {
		t.Fatalf("paid model price missing: %+v", paid)
	}
}
