package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryConversationStore struct {
	value    Conversation
	calls    int
	conflict bool
	fail     bool
}

func (m *memoryConversationStore) Create(context.Context) (Conversation, error) {
	now := time.Now()
	m.value = Conversation{ID: "c1", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}
	return m.value, nil
}
func (m *memoryConversationStore) Get(_ context.Context, id string) (Conversation, error) {
	if id != m.value.ID {
		return Conversation{}, ErrConversationNotFound
	}
	return m.value, nil
}
func (m *memoryConversationStore) List(context.Context, int, int) ([]ConversationSummary, error) {
	return nil, nil
}
func (m *memoryConversationStore) Delete(context.Context, string) error { return nil }
func (m *memoryConversationStore) CleanupExpired(context.Context, time.Time) (int, error) {
	return 0, nil
}
func (m *memoryConversationStore) AppendTurn(_ context.Context, id string, v int64, user []Message, a Message, r RoutingInfo) (Conversation, error) {
	m.calls++
	if m.fail {
		return Conversation{}, ErrConversationStore
	}
	if m.conflict {
		return Conversation{}, ErrConversationConflict
	}
	if v != m.value.Version {
		return Conversation{}, ErrConversationConflict
	}
	m.value.Turns = append(m.value.Turns, ConversationTurn{UserMessages: user, Assistant: a, Routing: r})
	m.value.Version++
	return m.value, nil
}

type captureConversationProvider struct {
	info     ProviderInfo
	messages []Message
	fail     bool
}

func (p *captureConversationProvider) ChatCompletion(_ context.Context, m []Message, model string) (*ChatCompletionResponse, error) {
	p.messages = append([]Message(nil), m...)
	if p.fail {
		return nil, errors.New("failed")
	}
	return &ChatCompletionResponse{Model: model, Choices: []Choice{{Message: Message{Role: "assistant", Content: "answer"}}}}, nil
}
func (p *captureConversationProvider) ChatCompletionStream(_ context.Context, _ []Message, _ string, _ func(string)) error {
	return nil
}
func (p *captureConversationProvider) GetInfo() ProviderInfo       { return p.info }
func (p *captureConversationProvider) SupportsModel(m string) bool { return m == "m" }

func TestServiceConversationResumesAndPersistsTurn(t *testing.T) {
	store := &memoryConversationStore{}
	_, _ = store.Create(context.Background())
	store.value.Turns = []ConversationTurn{{UserMessages: []Message{{Role: "user", Content: "earlier"}}, Assistant: Message{Role: "assistant", Content: "prior answer"}}}
	p := &captureConversationProvider{info: ProviderInfo{Name: "p", Working: true, SupportedModels: []string{"m"}}}
	r := NewRegistry()
	r.Register("p", p)
	s, _ := NewService(r, ServiceOptions{Conversations: store})
	var events []ProgressEvent
	resp, err := s.Complete(context.Background(), CompletionRequest{Model: "m", ConversationID: "c1", Messages: []Message{{Role: "user", Content: "now"}}, OnProgress: func(event ProgressEvent) { events = append(events, event) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.messages) != 3 || p.messages[0].Content != "earlier" || p.messages[2].Content != "now" {
		t.Fatalf("inference messages=%+v", p.messages)
	}
	if store.calls != 1 || len(store.value.Turns) != 2 || resp.ConversationID != "c1" {
		t.Fatalf("store=%+v response=%+v", store, resp)
	}
	if len(events) == 0 || events[len(events)-2].Stage != "conversation" || events[len(events)-2].Status != "started" || events[len(events)-1].Status != "succeeded" {
		t.Fatalf("conversation persistence progress = %#v", events)
	}
}

func TestServiceConversationFailureDoesNotAppendAndRejectsSystemReplacement(t *testing.T) {
	store := &memoryConversationStore{}
	_, _ = store.Create(context.Background())
	store.value.Version = 1
	p := &captureConversationProvider{info: ProviderInfo{Name: "p", Working: true, SupportedModels: []string{"m"}}, fail: true}
	r := NewRegistry()
	r.Register("p", p)
	s, _ := NewService(r, ServiceOptions{Conversations: store})
	_, err := s.Complete(context.Background(), CompletionRequest{Model: "m", ConversationID: "c1", Messages: []Message{{Role: "user", Content: "now"}}})
	if !errors.Is(err, ErrUpstream) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
	p.fail = false
	_, err = s.Complete(context.Background(), CompletionRequest{Model: "m", ConversationID: "c1", Messages: []Message{{Role: "system", Content: "replace"}, {Role: "user", Content: "now"}}})
	if !errors.Is(err, ErrInvalidInput) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
}

func TestConversationCRUDThroughService(t *testing.T) {
	store := &memoryConversationStore{}
	service, err := NewService(NewRegistry(), ServiceOptions{Conversations: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := service.CreateConversation(ctx)
	if err != nil || created.ID != "c1" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	got, err := service.GetConversation(ctx, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := service.ListConversations(ctx, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteConversation(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestToolMessageRequiresToolCallID(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test", &testProvider{info: ProviderInfo{Name: "test", Working: true, SupportedModels: []string{"model"}}})
	service, err := NewService(registry, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), CompletionRequest{
		Model:    "model",
		Messages: []Message{{Role: "tool", Content: "result"}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestSystemMessageRejectedAfterFirstTurn(t *testing.T) {
	store := &memoryConversationStore{}
	store.value = Conversation{ID: "c1", Version: 1, Turns: []ConversationTurn{{UserMessages: []Message{{Role: "user", Content: "hi"}}, Assistant: Message{Role: "assistant", Content: "hello"}}}}
	registry := NewRegistry()
	registry.Register("test", &testProvider{info: ProviderInfo{Name: "test", Working: true, SupportedModels: []string{"model"}}})
	service, err := NewService(registry, ServiceOptions{Conversations: store})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), CompletionRequest{
		Model: "model", ConversationID: "c1",
		Messages: []Message{{Role: "system", Content: "new instructions"}, {Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
