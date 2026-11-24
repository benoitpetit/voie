package app

import "context"

// StreamOptions mirrors OpenAI's stream_options request object.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// RequestOptions carries the optional OpenAI-compatible fields that providers
// can forward to their upstream. It is request-scoped and travels in the
// context so every provider call sees the same values without changing the
// Provider interface.
type RequestOptions struct {
	Temperature   *float64         `json:"temperature,omitempty"`
	MaxTokens     *int             `json:"max_tokens,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	Stop          []string         `json:"stop,omitempty"`
	Tools         []map[string]any `json:"tools,omitempty"`
	ToolChoice    any              `json:"tool_choice,omitempty"`
	StreamOptions *StreamOptions   `json:"stream_options,omitempty"`
}

type requestOptionsKey struct{}

func WithRequestOptions(ctx context.Context, options RequestOptions) context.Context {
	return context.WithValue(ctx, requestOptionsKey{}, options)
}

func RequestOptionsFrom(ctx context.Context) RequestOptions {
	if ctx == nil {
		return RequestOptions{}
	}
	if options, ok := ctx.Value(requestOptionsKey{}).(RequestOptions); ok {
		return options
	}
	return RequestOptions{}
}
