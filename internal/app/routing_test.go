package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

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
