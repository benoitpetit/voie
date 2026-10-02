package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSupportedTaskCategoriesAreStableAndDefensive(t *testing.T) {
	want := []string{"coding", "reasoning", "writing", "translation", "summarization", "general"}
	got := SupportedTaskCategories()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("task categories=%v, want %v", got, want)
	}
	got[0] = "changed"
	if next := SupportedTaskCategories(); !reflect.DeepEqual(next, want) {
		t.Fatalf("task category list is mutable by callers: %v", next)
	}
}

func TestServiceAutoUsesUniqueTaskRuleWithoutRouter(t *testing.T) {
	answer := &routingStub{info: ProviderInfo{Name: "answer", Working: true, SupportedModels: []string{"model-a"}}}
	registry := NewRegistry()
	registry.Register("answer", answer)
	service, err := NewService(registry, ServiceOptions{RoutingPolicy: RoutingPolicy{
		Models: map[string]ModelDescriptor{"model-a": {Description: "coding model", Capabilities: []string{"coding"}}},
		Tasks:  map[string]TaskRule{"coding": {RequiredCapabilities: []string{"coding"}, PreferredModels: []string{"model-a"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Task: "coding", Messages: []Message{{Role: "user", Content: "write a function"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "answer" || response.Model != "model-a" || response.Routing == nil || response.Routing.Strategy != StrategyAuto {
		t.Fatalf("response = %+v", response)
	}
	if got := answer.callCount(); got != 1 {
		t.Fatalf("provider calls = %d, want one answer call without a router", got)
	}
}

func TestServiceAutoUsesRouterAmongEligibleModels(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{
		"router-model": `{"task":"writing","model":"model-b","reason":"better writing support"}`,
	}}
	a := &routingStub{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}}}
	b := &routingStub{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}}}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("a", a)
	registry.Register("b", b)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{
		Models: map[string]ModelDescriptor{
			"model-a": {Description: "coding model", Capabilities: []string{"coding"}},
			"model-b": {Description: "writing model", Capabilities: []string{"writing"}},
		},
		Tasks: map[string]TaskRule{"writing": {RequiredCapabilities: []string{"writing"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var events []ProgressEvent
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "write a short note"}}, OnProgress: func(event ProgressEvent) { events = append(events, event) }})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "model-b" || response.Provider != "b" || response.Routing == nil || response.Routing.Task != "writing" {
		t.Fatalf("response = %+v", response)
	}
	if router.callCount() != 1 || a.callCount() != 0 || b.callCount() != 1 {
		t.Fatalf("calls router/a/b = %d/%d/%d", router.callCount(), a.callCount(), b.callCount())
	}
	if len(events) < 4 || events[1].Stage != "routing" || events[1].Status != "started" || events[2].Status != "succeeded" || events[2].Model != "model-b" {
		t.Fatalf("automatic routing progress = %#v", events)
	}
}

func TestServiceAutoRechecksCandidatesAfterRouterClassifiesTask(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{"router-model": `{"task":"writing","model":"model-a","reason":"incorrect capability choice"}`}}
	a := &routingStub{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}}}
	b := &routingStub{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}}}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("a", a)
	registry.Register("b", b)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{Models: map[string]ModelDescriptor{"model-a": {Capabilities: []string{"coding"}}, "model-b": {Capabilities: []string{"writing"}}}, Tasks: map[string]TaskRule{"writing": {RequiredCapabilities: []string{"writing"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "draft"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "model-b" || b.callCount() != 1 || a.callCount() != 0 {
		t.Fatalf("response=%+v calls a/b=%d/%d", response, a.callCount(), b.callCount())
	}
}

func TestServiceAutoRejectsUnknownOrDisabledRouterChoice(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected string
		register bool
		working  bool
	}{
		{name: "unknown model", selected: "missing-model"},
		{name: "disabled provider", selected: "disabled-model", register: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{
				"router-model": fmt.Sprintf(`{"task":"coding","model":%q,"reason":"bad selection"}`, tc.selected),
			}}
			answer := &routingStub{info: ProviderInfo{Name: "answer", Working: true, SupportedModels: []string{"model-a"}}}
			registry := NewRegistry()
			registry.Register("router", router)
			registry.Register("answer", answer)
			if tc.register {
				registry.Register("disabled", &routingStub{info: ProviderInfo{Name: "disabled", Working: tc.working, SupportedModels: []string{tc.selected}}})
			}
			service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{
				Models: map[string]ModelDescriptor{"model-a": {Capabilities: []string{"coding"}}, tc.selected: {Capabilities: []string{"coding"}}},
				Tasks:  map[string]TaskRule{"coding": {RequiredCapabilities: []string{"coding"}}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "code"}}})
			if !errors.Is(err, ErrRouting) {
				t.Fatalf("Complete() error = %v, want ErrRouting", err)
			}
			if answer.callCount() != 0 {
				t.Fatalf("answer provider called %d times for invalid router selection", answer.callCount())
			}
		})
	}
}

func TestServiceAutoRejectsMalformedRouterJSON(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{"router-model": "not json"}}
	answer := &routingStub{info: ProviderInfo{Name: "answer", Working: true, SupportedModels: []string{"model-a"}}}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("answer", answer)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, ErrRouting) {
		t.Fatalf("Complete() error = %v, want ErrRouting", err)
	}
	if answer.callCount() != 0 {
		t.Fatalf("answer provider called %d times for malformed router response", answer.callCount())
	}
}

func TestServiceAutoRequiresRouterWhenSelectionIsAmbiguous(t *testing.T) {
	registry := NewRegistry()
	registry.Register("a", &routingStub{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}}})
	registry.Register("b", &routingStub{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}}})
	service, err := NewService(registry, ServiceOptions{RoutingPolicy: RoutingPolicy{Models: map[string]ModelDescriptor{
		"model-a": {Capabilities: []string{"writing"}}, "model-b": {Capabilities: []string{"writing"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, ErrRouting) {
		t.Fatalf("Complete() error = %v, want ErrRouting", err)
	}
}

func assertRouterClassifiedOnce(t *testing.T, router *routingStub) {
	t.Helper()
	router.mu.Lock()
	defer router.mu.Unlock()
	if len(router.calls) != 1 || router.calls[0] != "router-model" {
		t.Fatalf("router calls = %v, want exactly one router-model classification", router.calls)
	}
}

func TestServiceAutoFallbackSucceedsOnEligibleCandidate(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{"router-model": `{"task":"coding","model":"model-a","reason":"primary"}`}}
	pa := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}},
		failFirst: map[string]int{"model-a": 10},
		failures:  map[string]error{"model-a": transientFailure("a", "model-a")},
	}
	pb := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}}}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("a", pa)
	registry.Register("b", pb)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{
		Models: map[string]ModelDescriptor{
			"model-a": {Description: "coding model", Capabilities: []string{"coding"}},
			"model-b": {Description: "coding model", Capabilities: []string{"coding"}},
		},
		Tasks: map[string]TaskRule{"coding": {RequiredCapabilities: []string{"coding"}, PreferredModels: []string{"model-a"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "write a function"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "b" || response.Model != "model-b" {
		t.Fatalf("response = %+v", response)
	}
	attempts := response.Routing.Attempts
	if len(attempts) != 3 || attempts[0].Outcome != OutcomeRetryableFailure || attempts[1].Outcome != OutcomeRetryableFailure || attempts[2].Outcome != OutcomeSucceeded {
		t.Fatalf("attempts = %+v", attempts)
	}
	if response.Routing.Strategy != StrategyAuto || response.Routing.Task != "coding" {
		t.Fatalf("routing = %+v", response.Routing)
	}
	assertRouterClassifiedOnce(t, router)
}

func TestServiceAutoFallbackRequestListPrecedence(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{"router-model": `{"task":"coding","model":"model-a","reason":"primary"}`}}
	pa := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}},
		failFirst: map[string]int{"model-a": 10},
		failures:  map[string]error{"model-a": transientFailure("a", "model-a")},
	}
	pb := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}}}
	pc := &fallbackProvider{info: ProviderInfo{Name: "c", Working: true, SupportedModels: []string{"model-c"}}}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("a", pa)
	registry.Register("b", pb)
	registry.Register("c", pc)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{
		Models: map[string]ModelDescriptor{
			"model-a": {Capabilities: []string{"coding"}}, "model-b": {Capabilities: []string{"coding"}}, "model-c": {Capabilities: []string{"coding"}},
		},
		Tasks: map[string]TaskRule{"coding": {RequiredCapabilities: []string{"coding"}, PreferredModels: []string{"model-a", "model-b"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{
		Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "code"}},
		Fallback: &FallbackOverride{Models: []string{"model-c", "model-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "c" {
		t.Fatalf("response provider = %q, want request list first candidate c", response.Provider)
	}
	if len(pc.calls) != 1 || len(pb.calls) != 0 {
		t.Fatalf("calls c=%v b=%v, want request list precedence", pc.calls, pb.calls)
	}
	assertRouterClassifiedOnce(t, router)
}

func TestServiceAutoFallbackDiscardsIneligibleExplicitCandidate(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{"router-model": `{"task":"coding","model":"model-a","reason":"primary"}`}}
	pa := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}},
		failFirst: map[string]int{"model-a": 10},
		failures:  map[string]error{"model-a": transientFailure("a", "model-a")},
	}
	pw := &fallbackProvider{info: ProviderInfo{Name: "w", Working: true, SupportedModels: []string{"model-wrong"}}}
	pb := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}}}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("a", pa)
	registry.Register("w", pw)
	registry.Register("b", pb)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{
		Models: map[string]ModelDescriptor{
			"model-a": {Capabilities: []string{"coding"}}, "model-wrong": {Capabilities: []string{"writing"}}, "model-b": {Capabilities: []string{"coding"}},
		},
		Tasks: map[string]TaskRule{"coding": {RequiredCapabilities: []string{"coding"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{
		Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "code"}},
		Fallback: &FallbackOverride{Models: []string{"model-wrong", "model-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "b" {
		t.Fatalf("response provider = %q, want b after discarding ineligible candidate", response.Provider)
	}
	if len(pw.calls) != 0 || len(pb.calls) != 1 {
		t.Fatalf("calls w=%v b=%v, want ineligible candidate discarded", pw.calls, pb.calls)
	}
	assertRouterClassifiedOnce(t, router)
}

func TestServiceAutoFallbackReturnsFinalErrorWhenExhausted(t *testing.T) {
	router := &routingStub{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}, responses: map[string]string{"router-model": `{"task":"coding","model":"model-a","reason":"primary"}`}}
	pa := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"model-a"}},
		failFirst: map[string]int{"model-a": 10},
		failures:  map[string]error{"model-a": transientFailure("a", "model-a")},
	}
	pb := &fallbackProvider{
		info:      ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"model-b"}},
		failFirst: map[string]int{"model-b": 10},
		failures:  map[string]error{"model-b": transientFailure("b", "model-b")},
	}
	registry := NewRegistry()
	registry.Register("router", router)
	registry.Register("a", pa)
	registry.Register("b", pb)
	service, err := NewService(registry, ServiceOptions{RouterModel: "router-model", RoutingPolicy: RoutingPolicy{
		Models: map[string]ModelDescriptor{"model-a": {Capabilities: []string{"coding"}}, "model-b": {Capabilities: []string{"coding"}}},
		Tasks:  map[string]TaskRule{"coding": {RequiredCapabilities: []string{"coding"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), CompletionRequest{Strategy: StrategyAuto, Messages: []Message{{Role: "user", Content: "code"}}})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	if len(pb.calls) == 0 {
		t.Fatalf("fallback candidate b was never attempted")
	}
	assertRouterClassifiedOnce(t, router)
}

type routingStub struct {
	info      ProviderInfo
	responses map[string]string
	mu        sync.Mutex
	calls     []string
}

func (p *routingStub) ChatCompletion(_ context.Context, _ []Message, model string) (*ChatCompletionResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, model)
	p.mu.Unlock()
	content := "answer"
	if configured, ok := p.responses[model]; ok {
		content = configured
	}
	return &ChatCompletionResponse{Model: model, Choices: []Choice{{Message: Message{Role: "assistant", Content: content}}}}, nil
}

func (*routingStub) ChatCompletionStream(_ context.Context, _ []Message, _ string, callback func(string)) error {
	callback("answer")
	return nil
}

func (p *routingStub) GetInfo() ProviderInfo { return p.info }

func (p *routingStub) SupportsModel(model string) bool {
	for _, supported := range p.info.SupportedModels {
		if strings.EqualFold(model, supported) {
			return true
		}
	}
	return false
}

func (p *routingStub) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}
