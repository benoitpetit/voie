package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
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
	var events []ProgressEvent
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a", "b"}, Messages: []Message{{Role: "user", Content: "hello"}}, OnProgress: func(event ProgressEvent) { events = append(events, event) }})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "synth" || response.Routing == nil || len(response.Routing.Models) != 2 {
		t.Fatalf("response = %+v", response)
	}
	stages := map[string]bool{}
	for _, event := range events {
		stages[event.Stage+":"+event.Status] = true
	}
	for _, stage := range []string{"routing:started", "routing:succeeded", "model:started", "model:succeeded", "synthesis:started", "synthesis:succeeded"} {
		if !stages[stage] {
			t.Errorf("missing progress stage %q in %#v", stage, events)
		}
	}
}

func TestEnsembleRejectsMoreThanFiveModels(t *testing.T) {
	registry := NewRegistry()
	registry.Register("synth", &testProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}})
	service, _ := NewService(registry, ServiceOptions{SynthesisModel: "synth"})
	_, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a", "b", "c", "d", "e", "f"}, Messages: []Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsembleRejectsSynthesisModelAsCandidate(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []string{"a", "b", "synth"} {
		registry.Register(id, &testProvider{info: ProviderInfo{Name: id, Working: true, SupportedModels: []string{id}}})
	}
	service, _ := NewService(registry, ServiceOptions{SynthesisModel: "synth"})
	_, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a", "synth"}, Messages: []Message{{Role: "user", Content: "hi"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error=%v, want ErrInvalidInput", err)
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

func TestEnsembleRetriesFailedCandidate(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 1},
		failures:  map[string]error{"a-model": transientFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	synth := &fallbackProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}}
	service, _ := fallbackService(t, ServiceOptions{SynthesisModel: "synth"}, a, b, synth)
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a-model", "b-model"}, Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.calls) != 2 || a.calls[0] != "a-model" || a.calls[1] != "a-model" {
		t.Fatalf("calls a=%v, want a same-model retry", a.calls)
	}
	if len(b.calls) != 1 {
		t.Fatalf("calls b=%v, want a single successful call", b.calls)
	}
	if response.Routing == nil || len(response.Routing.Attempts) != 4 {
		t.Fatalf("routing = %+v", response.Routing)
	}
	attempts := response.Routing.Attempts
	if attempts[0].Model != "a-model" || attempts[0].Outcome != OutcomeRetryableFailure {
		t.Fatalf("attempts[0] = %+v, want retryable failure on a-model", attempts[0])
	}
	if attempts[1].Model != "a-model" || attempts[1].Outcome != OutcomeSucceeded {
		t.Fatalf("attempts[1] = %+v, want succeeded retry on a-model", attempts[1])
	}
	for i := 2; i < 4; i++ {
		if attempts[i].Attempt != i+1 {
			t.Fatalf("attempt ordinal mismatch at %d: %+v", i, attempts[i])
		}
	}
}

func TestEnsembleFallbacksDeterministicAndExcludesUsedAndSynth(t *testing.T) {
	a := &fallbackProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	c := &fallbackProvider{info: ProviderInfo{Name: "c", Working: true, SupportedModels: []string{"c-model"}}}
	d := &fallbackProvider{info: ProviderInfo{Name: "d", Working: true, SupportedModels: []string{"d-model"}}}
	synth := &fallbackProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}}
	service, _ := fallbackService(t, ServiceOptions{SynthesisModel: "synth",
		Fallback: &FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 3,
			Models: map[string][]string{"a-model": {"d-model"}}},
	}, a, b, c, d, synth)
	policy := service.FallbackPolicy()
	got := service.ensembleFallbacks(CompletionRequest{Fallback: &FallbackOverride{Models: []string{"c-model", "b-model"}}}, policy, []string{"a-model"})
	want := []string{"c-model", "b-model", "d-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ensembleFallbacks = %v, want %v", got, want)
	}
	if s := strings.Join(got, ","); strings.Contains(s, "synth") || strings.Contains(s, "a-model") {
		t.Fatalf("ensembleFallbacks %v includes a used candidate or the synthesis model", got)
	}
}

func TestEnsembleSharedBudgetLimitsReplacements(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": permanentFailure("a", "a-model")},
	}
	b := &fallbackProvider{
		info:      ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}},
		failFirst: map[string]int{"b-model": 10},
		failures:  map[string]error{"b-model": permanentFailure("b", "b-model")},
	}
	c := &fallbackProvider{info: ProviderInfo{Name: "c", Working: true, SupportedModels: []string{"c-model"}}}
	d := &fallbackProvider{info: ProviderInfo{Name: "d", Working: true, SupportedModels: []string{"d-model"}}}
	synth := &fallbackProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}}
	service, _ := fallbackService(t, ServiceOptions{SynthesisModel: "synth",
		Fallback: &FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 1},
	}, a, b, c, d, synth)
	_, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a-model", "b-model"}, Messages: []Message{{Role: "user", Content: "hi"}}})
	if !errors.Is(err, ErrEnsembleInsufficient) {
		t.Fatalf("error = %v, want ErrEnsembleInsufficient with a shared budget of one", err)
	}
	if len(c.calls) != 1 || len(d.calls) != 0 {
		t.Fatalf("replacement calls c=%v d=%v, want exactly one replacement within the shared budget", c.calls, d.calls)
	}
}

func TestEnsembleReplacesFailedSlotThenSynthesizes(t *testing.T) {
	a := &fallbackProvider{
		info:      ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}},
		failFirst: map[string]int{"a-model": 10},
		failures:  map[string]error{"a-model": permanentFailure("a", "a-model")},
	}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	c := &fallbackProvider{info: ProviderInfo{Name: "c", Working: true, SupportedModels: []string{"c-model"}}}
	d := &fallbackProvider{info: ProviderInfo{Name: "d", Working: true, SupportedModels: []string{"d-model"}}}
	synth := &fallbackProvider{info: ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}}}
	service, _ := fallbackService(t, ServiceOptions{SynthesisModel: "synth",
		Fallback: &FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 3},
	}, a, b, c, d, synth)
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a-model", "b-model"}, Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.calls) != 1 || len(d.calls) != 0 {
		t.Fatalf("replacement calls c=%v d=%v, want stop once successes match the original count", c.calls, d.calls)
	}
	if response.Model != "synth" || response.Routing == nil || len(response.Routing.Models) != 3 {
		t.Fatalf("response = %+v routing = %+v", response, response.Routing)
	}
}

func TestEnsembleSynthesisRetriesAndFallsBackOnce(t *testing.T) {
	a := &fallbackProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}}
	b := &fallbackProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}}
	synth := &fallbackProvider{
		info:      ProviderInfo{Name: "synth", Working: true, SupportedModels: []string{"synth"}},
		failFirst: map[string]int{"synth": 10},
		failures:  map[string]error{"synth": transientFailure("synth", "synth")},
	}
	synth2 := &fallbackProvider{info: ProviderInfo{Name: "synth2", Working: true, SupportedModels: []string{"synth2"}}}
	service, _ := fallbackService(t, ServiceOptions{SynthesisModel: "synth",
		Fallback: &FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 1,
			Models: map[string][]string{"synth": {"synth2"}}},
	}, a, b, synth, synth2)
	response, err := service.Complete(context.Background(), CompletionRequest{Strategy: StrategyEnsemble, Models: []string{"a-model", "b-model"}, Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "synth2" || response.Provider != "synth2" {
		t.Fatalf("response = %+v, want the synthesis fallback to win", response)
	}
	if len(a.calls) != 1 || len(b.calls) != 1 || len(synth.calls) != 2 {
		t.Fatalf("calls a=%v b=%v synth=%v, want candidates once and synth stransient retry", a.calls, b.calls, synth.calls)
	}
	if len(synth2.calls) != 1 {
		t.Fatalf("calls synth2=%v, want one synthesis fallback call", synth2.calls)
	}
}

func TestSynthesisFallsBackToRouterModelWhenUnavailable(t *testing.T) {
	registry := NewRegistry()
	registry.Register("synth", &testProvider{info: ProviderInfo{Name: "synth", Working: false, SupportedModels: []string{"synth-model"}}})
	registry.Register("router", &testProvider{info: ProviderInfo{Name: "router", Working: true, DefaultModel: "router-model", SupportedModels: []string{"router-model"}}})
	registry.Register("a", &testProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}})
	registry.Register("b", &testProvider{info: ProviderInfo{Name: "b", Working: true, SupportedModels: []string{"b-model"}}})
	service, err := NewService(registry, ServiceOptions{SynthesisModel: "synth-model", RouterModel: "router-model"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.runEnsemble(context.Background(), CompletionRequest{
		Strategy: StrategyEnsemble,
		Ensemble: &EnsembleRequestOptions{Mode: EnsembleIndependent, Models: []string{"a-model", "b-model"}},
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.model != "router-model" {
		t.Fatalf("synthesis model = %q, want the working router model", run.model)
	}
}

func TestSynthesisFallbackRespectsProviderPin(t *testing.T) {
	registry := NewRegistry()
	registry.Register("router", &testProvider{info: ProviderInfo{Name: "router", Working: true, SupportedModels: []string{"router-model"}}})
	registry.Register("a", &testProvider{info: ProviderInfo{Name: "a", Working: true, SupportedModels: []string{"a-model"}}})
	service, err := NewService(registry, ServiceOptions{
		SynthesisModel: "router-model",
		Fallback:       &FallbackPolicy{Enabled: true, MaxFallbackModels: 1, Models: map[string][]string{"router-model": {"a-model"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pinned := service.ensembleSynthesisFallbacks(CompletionRequest{
		Provider: "router",
		Fallback: &FallbackOverride{Models: []string{"a-model"}},
	}, service.FallbackPolicy(), "router-model", nil)
	if len(pinned) != 0 {
		t.Fatalf("fallbacks = %v, want none when the provider is pinned", pinned)
	}
	unpinned := service.ensembleSynthesisFallbacks(CompletionRequest{}, service.FallbackPolicy(), "router-model", nil)
	if len(unpinned) == 0 {
		t.Fatal("explicit fallback list was ignored without a provider pin")
	}
}
