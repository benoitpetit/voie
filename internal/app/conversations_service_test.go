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
func (m *memoryConversationStore) List(context.Context) ([]ConversationSummary, error) {
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
	resp, err := s.Complete(context.Background(), CompletionRequest{Model: "m", ConversationID: "c1", Messages: []Message{{Role: "user", Content: "now"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.messages) != 3 || p.messages[0].Content != "earlier" || p.messages[2].Content != "now" {
		t.Fatalf("inference messages=%+v", p.messages)
	}
	if store.calls != 1 || len(store.value.Turns) != 2 || resp.ConversationID != "c1" {
		t.Fatalf("store=%+v response=%+v", store, resp)
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
