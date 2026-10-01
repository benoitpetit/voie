package app

import (
	"context"
	"time"
)

// ConversationStore persists opt-in local conversation state. Implementations
// must use optimistic versions so inference can happen outside a write lock.
type ConversationStore interface {
	Create(context.Context) (Conversation, error)
	Get(context.Context, string) (Conversation, error)
	List(context.Context) ([]ConversationSummary, error)
	Delete(context.Context, string) error
	AppendTurn(context.Context, string, int64, []Message, Message, RoutingInfo) (Conversation, error)
	CleanupExpired(context.Context, time.Time) (int, error)
}

type Conversation struct {
	ID        string             `json:"id"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	ExpiresAt time.Time          `json:"expires_at"`
	Version   int64              `json:"version"`
	Turns     []ConversationTurn `json:"turns"`
}

type ConversationTurn struct {
	UserMessages []Message   `json:"user_messages"`
	Assistant    Message     `json:"assistant"`
	Routing      RoutingInfo `json:"routing"`
	CreatedAt    time.Time   `json:"created_at"`
}

type ConversationSummary struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Version      int64     `json:"version"`
	MessageCount int       `json:"message_count"`
}

func (c Conversation) ContextMessages() []Message {
	var messages []Message
	for _, turn := range c.Turns {
		messages = append(messages, turn.UserMessages...)
		messages = append(messages, turn.Assistant)
	}
	return messages
}
