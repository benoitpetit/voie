package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnsembleUsesExplicitModelsAndReturnsRoutingMetadata(t *testing.T) {
	registry := NewRegistry()
	a := &testProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a"}}}
	b := &testProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b"}}}
	synth := &testProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}}
	registry.Register("a", a)
	registry.Register("b", b)
	registry.Register("synth", synth)
	service, err := NewService(registry, ServiceOptions{SynthesisModel: "synth"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a", "b"}, Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "synth" || response.Routing == nil || len(response.Routing.Models) != 2 {
		t.Fatalf("response = %+v", response)
	}
}

func TestEnsembleRejectsMoreThanThreeModels(t *testing.T) {
	service, _ := NewService(NewRegistry(), ServiceOptions{})
	_, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a", "b", "c", "d"}, Messages: []Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsembleRejectsFewerThanTwoSuccesses(t *testing.T) {
	registry := NewRegistry()
	registry.Register("a", &testProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a"}}, response: &ChatCompletionResponse{}})
	registry.Register("b", &testProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b"}}})
	registry.Register("synth", &testProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}})
	service, _ := NewService(registry, ServiceOptions{SynthesisModel: "synth", Timeout: time.Second})
	_, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a", "b"}, Messages: []Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, ErrEnsembleInsufficient) {
		t.Fatalf("error = %v", err)
	}
}
