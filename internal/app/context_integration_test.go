package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/benoitpetit/voie/internal/app"
	sqlitestore "github.com/benoitpetit/voie/internal/storage/sqlite"
)

type contextCaptureProvider struct {
	requestID      string
	conversationID string
}

func (*contextCaptureProvider) GetInfo() app.ProviderInfo {
	return app.ProviderInfo{Name: "capture", Working: true, DefaultModel: "m", SupportedModels: []string{"m"}}
}
func (*contextCaptureProvider) SupportsModel(model string) bool { return model == "m" }
func (p *contextCaptureProvider) ChatCompletion(ctx context.Context, _ []app.Message, _ string) (*app.ChatCompletionResponse, error) {
	p.requestID = app.RequestID(ctx)
	p.conversationID = app.ConversationID(ctx)
	return &app.ChatCompletionResponse{Choices: []app.Choice{{Message: app.Message{Role: "assistant", Content: "ok"}}}}, nil
}
func (*contextCaptureProvider) ChatCompletionStream(context.Context, []app.Message, string, func(string)) error {
	return nil
}

func TestCompletePropagatesRequestAndConversationContext(t *testing.T) {
	store, err := sqlitestore.NewSQLiteStore(t.TempDir()+"/conv.db", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conversation, err := store.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	provider := &contextCaptureProvider{}
	registry := app.NewRegistry()
	registry.Register("capture", provider)
	service, err := app.NewService(registry, app.ServiceOptions{Conversations: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx := app.WithRequestID(context.Background(), "req-1")
	if _, err := service.Complete(ctx, app.CompletionRequest{Model: "m", ConversationID: conversation.ID, Messages: []app.Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if provider.requestID != "req-1" || provider.conversationID != conversation.ID {
		t.Fatalf("provider saw request=%q conversation=%q", provider.requestID, provider.conversationID)
	}
}
