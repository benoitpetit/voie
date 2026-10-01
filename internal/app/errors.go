package app

import "errors"

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
