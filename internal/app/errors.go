package app

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidInput          = errors.New("invalid input")
	ErrUnknownModel          = errors.New("unknown model")
	ErrUnknownProvider       = errors.New("unknown provider")
	ErrProviderModelMismatch = errors.New("provider does not support model")
	ErrProviderDisabled      = errors.New("provider is disabled")
	ErrTimeout               = errors.New("completion timed out")
	ErrCanceled              = errors.New("completion canceled")
	ErrUpstream              = errors.New("upstream provider failure")
	ErrRouting               = errors.New("model routing failed")
	ErrEnsembleInsufficient  = errors.New("not enough ensemble results")
	ErrConversationNotFound  = errors.New("conversation not found")
	ErrConversationExpired   = errors.New("conversation expired")
	ErrConversationConflict  = errors.New("conversation version conflict")
	ErrConversationStore     = errors.New("conversation store failure")
)

// Outcome constants reported in routing.attempts metadata.
const (
	OutcomeSucceeded        = "succeeded"
	OutcomeRetryableFailure = "retryable_failure"
	OutcomeUnavailable      = "unavailable"
	OutcomeFailed           = "failed"
)

// FailureCategory classifies a provider boundary failure for retry and
// fallback decisions at the application layer.
type FailureCategory int

const (
	// FailureTransient is retryable on the same model: network failures,
	// request timeouts while the caller context is still active, HTTP 408,
	// HTTP 429, and HTTP 5xx.
	FailureTransient FailureCategory = iota
	// FailureUnavailable means the selected model cannot be used; proceed
	// to fallback without retrying that same model (for example, a
	// model-specific HTTP 404).
	FailureUnavailable
	// FailurePermanent is neither retryable nor fallback-eligible.
	FailurePermanent
)

func (c FailureCategory) String() string {
	switch c {
	case FailureTransient:
		return "transient"
	case FailureUnavailable:
		return "unavailable"
	case FailurePermanent:
		return "permanent"
	default:
		return "unknown"
	}
}

// ProviderFailure carries the failure category and the upstream HTTP status
// only. It never carries the response body or any other provider content.
type ProviderFailure struct {
	Category FailureCategory
	Status   int // upstream HTTP status; 0 for network failures
	Provider string
	Model    string
	Cause    error
}

func (e *ProviderFailure) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("provider %q: network failure for model %q", e.Provider, e.Model)
	}
	return fmt.Sprintf("provider %q returned HTTP %d for model %q", e.Provider, e.Status, e.Model)
}

// Unwrap surfaces ErrUpstream and the original cause so existing
// classification (contextOrUpstreamError) and errors.Is/As keep working.
func (e *ProviderFailure) Unwrap() error {
	if e.Cause != nil {
		return errors.Join(ErrUpstream, e.Cause)
	}
	return ErrUpstream
}

// NewProviderFailure builds a typed failure with a stable category and the
// upstream status. status must be 0 when there is no HTTP response.
func NewProviderFailure(category FailureCategory, provider, model string, status int, cause error) *ProviderFailure {
	return &ProviderFailure{Category: category, Status: status, Provider: provider, Model: model, Cause: cause}
}

// WrapNetworkFailure classifies an http.Client transport error at the call
// site: caller cancellation and deadline exhaustion are permanent (retry or
// fallback must not continue), everything else is transient. status is 0.
func WrapNetworkFailure(err error, provider, model string) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return NewProviderFailure(FailurePermanent, provider, model, 0, err)
	}
	return NewProviderFailure(FailureTransient, provider, model, 0, err)
}

type Error struct {
	Kind    error
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e.Cause != nil {
		return errors.Join(e.Kind, e.Cause)
	}
	return e.Kind
}

func appError(kind error, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}
