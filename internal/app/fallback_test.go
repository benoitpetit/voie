package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fallbackProvider struct {
	info  ProviderInfo
	mu    sync.Mutex
	calls []string

	failFirst       map[string]int
	failures        map[string]error
	empty           map[string]bool
	streamFailFirst map[string]int
	streamFailures  map[string]error
	chunkThenFail   map[string]error
}

func (p *fallbackProvider) ChatCompletion(_ context.Context, _ []Message, model string) (*ChatCompletionResponse, error) {
	p.mu.Lock()
	attempt := len(p.calls)
	p.calls = append(p.calls, model)
	p.mu.Unlock()
	if n, ok := p.failFirst[model]; ok && attempt < n {
		return nil, p.failures[model]
	}
	if p.empty[model] {
		return &ChatCompletionResponse{Model: model, Choices: nil}, nil
	}
	return &ChatCompletionResponse{Model: model, Choices: []Choice{{Message: Message{Role: "assistant", Content: "answer from " + model}}}}, nil
}

func (p *fallbackProvider) ChatCompletionStream(_ context.Context, _ []Message, model string, callback func(string)) error {
	p.mu.Lock()
	attempt := len(p.calls)
	p.calls = append(p.calls, "stream:"+model)
	p.mu.Unlock()
	if n, ok := p.streamFailFirst[model]; ok && attempt < n {
		return p.streamFailures[model]
	}
	if err, ok := p.chunkThenFail[model]; ok {
		callback("partial from " + model)
		return err
	}
	callback("answer from " + model)
	return nil
}

func (p *fallbackProvider) GetInfo() ProviderInfo { return p.info }
func (p *fallbackProvider) SupportsModel(model string) bool {
	for _, supported := range p.info.SupportedModels {
		if strings.EqualFold(model, supported) {
			return true
		}
	}
	return false
}

func transientFailure(provider, model string) error {
	return NewProviderFailure(FailureTransient, provider, model, 500, errors.New("upstream 500"))
}

func unavailableFailure(provider, model string) error {
	return NewProviderFailure(FailureUnavailable, provider, model, 404, errors.New("model unavailable"))
}

func permanentFailure(provider, model string) error {
	return NewProviderFailure(FailurePermanent, provider, model, 401, errors.New("authentication failed"))
}

func fallbackService(t *testing.T, options ServiceOptions, providers ...*fallbackProvider) (*Service, *Registry) {
	t.Helper()
	registry := NewRegistry()
	for _, p := range providers {
		registry.Register(p.info.Name, p)
	}
	service, err := NewService(registry, options)
	if err != nil {
		t.Fatal(err)
	}
	return service, registry
}

func TestFallbackResolvePolicyAppliesOverrides(t *testing.T) {
	a := &fallbackProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1,
		Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a)

	policy, err := service.resolvePolicy(CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Enabled || policy.MaxRetries != 1 || policy.MaxFallbackModels != 1 {
		t.Fatalf("inherited policy = %+v", policy)
	}
	if got := policy.Models["a-model"]; len(got) != 1 || got[0] != "b-model" {
		t.Fatalf("inherited model list = %v", got)
	}

	disabled, zero := false, 0
	policy, err = service.resolvePolicy(CompletionRequest{Fallback: &FallbackOverride{
		Enabled: &disabled, MaxRetries: &zero, MaxFallbackModels: &zero,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.MaxRetries != 0 || policy.MaxFallbackModels != 0 {
		t.Fatalf("overridden policy = %+v", policy)
	}
}

func TestFallbackResolvePolicyRejectsInvalidOverrides(t *testing.T) {
	service, _ := fallbackService(t, ServiceOptions{}, &fallbackProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}}, &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}})
	five := 5
	cases := []struct {
		name     string
		override FallbackOverride
		want     string
	}{
		{"max_retries out of range", FallbackOverride{MaxRetries: &five}, "max_retries"},
		{"max_fallback_models out of range", FallbackOverride{MaxFallbackModels: &five}, "max_fallback_models"},
		{"unknown model", FallbackOverride{Models: []string{"missing"}}, `"missing"`},
		{"duplicate model", FallbackOverride{Models: []string{"b-model", "B-MODEL"}}, "more than once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.resolvePolicy(CompletionRequest{Fallback: &tc.override})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("resolvePolicy error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestFallbackClassicRetriesSameModelBeforeSucceeding(t *testing.T) {
	a := &fallbackProvider{
		info:    ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 1},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	service, _ := fallbackService(t, ServiceOptions{}, a)
	response, err := service.Complete(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "a" || response.Model != "a-model" {
		t.Fatalf("response = %+v", response)
	}
	if len(a.calls) != 2 || a.calls[0] != "a-model" || a.calls[1] != "a-model" {
		t.Fatalf("model calls = %v, want same model twice", a.calls)
	}
	if response.Routing == nil || response.Routing.Strategy != StrategyClassic || len(response.Routing.Attempts) != 2 {
		t.Fatalf("routing = %+v", response.Routing)
	}
	first, second := response.Routing.Attempts[0], response.Routing.Attempts[1]
	if first.Attempt != 1 || first.Outcome != OutcomeRetryableFailure || second.Attempt != 2 || second.Outcome != OutcomeSucceeded {
		t.Fatalf("attempts = %+v", response.Routing.Attempts)
	}
}

func TestFallbackClassicRetriesThenFallsBackInOrder(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 2},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	response, err := service.Complete(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "b" || response.Model != "b-model" {
		t.Fatalf("response = %+v", response)
	}
	if got := strings.Join(a.calls, ","); got != "a-model,a-model" || len(b.calls) != 1 || b.calls[0] != "b-model" {
		t.Fatalf("calls a=%v b=%v", a.calls, b.calls)
	}
	attempts := response.Routing.Attempts
	if len(attempts) != 3 || attempts[0].Outcome != OutcomeRetryableFailure || attempts[1].Outcome != OutcomeRetryableFailure || attempts[2].Outcome != OutcomeSucceeded {
		t.Fatalf("attempts = %+v", attempts)
	}
	if attempts[1].Attempt != 2 || attempts[2].Attempt != 3 {
		t.Fatalf("attempt ordinals = %+v", attempts)
	}
}

func TestFallbackClassicFallsBackDirectlyOnUnavailable(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": unavailableFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	response, err := service.Complete(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "b" {
		t.Fatalf("response provider = %q, want b", response.Provider)
	}
	if len(a.calls) != 1 || len(b.calls) != 1 {
		t.Fatalf("calls a=%v b=%v, want one each", a.calls, b.calls)
	}
	attempts := response.Routing.Attempts
	if len(attempts) != 2 || attempts[0].Outcome != OutcomeUnavailable || attempts[1].Outcome != OutcomeSucceeded {
		t.Fatalf("attempts = %+v", attempts)
	}
}

func TestFallbackClassicStopsOnPermanentFailure(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": permanentFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	_, err := service.Complete(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Category != FailurePermanent {
		t.Fatalf("error = %v, want permanent ProviderFailure", err)
	}
	if len(a.calls) != 1 || len(b.calls) != 0 {
		t.Fatalf("calls a=%v b=%v, want no retry and no fallback", a.calls, b.calls)
	}
}

func TestFallbackClassicFirstSuccessStops(t *testing.T) {
	a := &fallbackProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}}
	b := &fallbackProvider{
		info:      ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}},
		failFirst: map[string]int{"b-model": 10},
		failures:  map[string]error{"b-model": transientFailure("b", "b-model")},
	}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	response, err := service.Complete(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "a" || len(b.calls) != 0 {
		t.Fatalf("response=%+v b calls=%v, want fallback untouched", response, b.calls)
	}
}

func TestFallbackClassicExplicitEmptyModelsClearsFallback(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	_, err := service.Complete(context.Background(), CompletionRequest{
		Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}},
		Fallback: &FallbackOverride{Models: []string{}},
	})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if len(a.calls) != 2 || len(b.calls) != 0 {
		t.Fatalf("calls a=%v b=%v, want retry only on primary and no fallback", a.calls, b.calls)
	}
}

func TestFallbackClassicRequestModelsReplaceConfigured(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 2},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	c := &fallbackProvider{info: ProviderInfo{Name: "c", Working: true, SupportedModels: []string{"c-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b, c)
	response, err := service.Complete(context.Background(), CompletionRequest{
		Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}},
		Fallback: &FallbackOverride{Models: []string{"c-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "c" {
		t.Fatalf("response provider = %q, want c", response.Provider)
	}
	if len(b.calls) != 0 || len(c.calls) != 1 {
		t.Fatalf("calls b=%v c=%v, want request list to replace configured", b.calls, c.calls)
	}
}

func TestFallbackClassicNeverBypassesPinnedProvider(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{}, a, b)
	_, err := service.Complete(context.Background(), CompletionRequest{
		Provider: "a", Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}},
		Fallback: &FallbackOverride{Models: []string{"b-model"}},
	})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if len(b.calls) != 0 {
		t.Fatalf("pinned provider bypassed: b calls = %v", b.calls)
	}
	var failure *ProviderFailure
	if !errors.As(err, &failure) || failure.Provider != "a" {
		t.Fatalf("error = %v, want original provider a failure", err)
	}
}

func TestFallbackClassicUsesSameProviderCandidateWhenPinned(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model", "a2-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 0, MaxFallbackModels: 1,
	}}, a, b)
	response, err := service.Complete(context.Background(), CompletionRequest{
		Provider: "a", Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}},
		Fallback: &FallbackOverride{Models: []string{"a2-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "a" || response.Model != "a2-model" {
		t.Fatalf("response = %+v", response)
	}
	if len(a.calls) != 2 || a.calls[1] != "a2-model" || len(b.calls) != 0 {
		t.Fatalf("calls a=%v b=%v, want a then a2 on pinned provider", a.calls, b.calls)
	}
}

func TestFallbackClassicStreamsFallbackBeforeContent(t *testing.T) {
	a := &fallbackProvider{
		info:            ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		streamFailFirst: map[string]int{"a-model": 10},
		streamFailures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	var got strings.Builder
	var routing *RoutingInfo
	err := service.CompleteStreamWithInfo(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}}, func(chunk string, r *RoutingInfo) {
		got.WriteString(chunk)
		routing = r
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "answer from b-model" {
		t.Fatalf("streamed content = %q, want fallback answer", got.String())
	}
	if len(a.calls) != 2 || len(b.calls) != 1 {
		t.Fatalf("stream calls a=%v b=%v", a.calls, b.calls)
	}
	if routing == nil || routing.Strategy != StrategyClassic || len(routing.Attempts) != 3 || routing.Attempts[2].Outcome != OutcomeSucceeded {
		t.Fatalf("routing = %+v", routing)
	}
}

func TestFallbackClassicStreamStopsAfterFirstChunk(t *testing.T) {
	a := &fallbackProvider{
		info:          ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		chunkThenFail: map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	service, _ := fallbackService(t, ServiceOptions{Fallback: &FallbackPolicy{
		Enabled: true, MaxRetries: 1, MaxFallbackModels: 1, Models: map[string][]string{"a-model": []string{"b-model"}},
	}}, a, b)
	var got strings.Builder
	err := service.CompleteStreamWithInfo(context.Background(), CompletionRequest{Model: "a-model", Messages: []Message{{Role: "user", Content: "hi"}}}, func(chunk string, _ *RoutingInfo) {
		got.WriteString(chunk)
	})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("stream error = %v, want ErrUpstream", err)
	}
	if got.String() != "partial from a-model" {
		t.Fatalf("streamed content = %q, want only the partial first chunk", got.String())
	}
	if len(a.calls) != 1 || len(b.calls) != 0 {
		t.Fatalf("stream calls a=%v b=%v, want no retry or fallback after a chunk", a.calls, b.calls)
	}
}