package app

import "context"

// ToolCallSink receives tool-call deltas discovered during streaming without
// changing the Provider callback signature.
type ToolCallSink func(any)

type toolCallSinkKey struct{}

func WithToolCallSink(ctx context.Context, sink ToolCallSink) context.Context {
	return context.WithValue(ctx, toolCallSinkKey{}, sink)
}

func ToolCallSinkFrom(ctx context.Context) ToolCallSink {
	if ctx == nil {
		return nil
	}
	if sink, ok := ctx.Value(toolCallSinkKey{}).(ToolCallSink); ok {
		return sink
	}
	return nil
}
