package app

import "context"

type requestIDKey struct{}
type conversationIDKey struct{}

// WithRequestID attaches a correlation id to a request context.
func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil || id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the correlation id attached to the context, if any.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// WithConversationID attaches the active conversation id so outbound
// providers can propagate it without changing the Provider interface.
func WithConversationID(ctx context.Context, id string) context.Context {
	if ctx == nil || id == "" {
		return ctx
	}
	return context.WithValue(ctx, conversationIDKey{}, id)
}

// ConversationID returns the active conversation id, if any.
func ConversationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(conversationIDKey{}).(string)
	return id
}
