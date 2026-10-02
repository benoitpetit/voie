package providers

import (
	"net/http"

	"github.com/benoitpetit/voie/internal/app"
)

// classifyStatus maps an upstream HTTP status to a failure category without
// inspecting the response body: 408/429/5xx are transient (retryable on the
// same model), a model-specific 404 is unavailable (fall back without
// retrying that model), and any other 4xx is permanent (no retry, no
// fallback).
func classifyStatus(status int) app.FailureCategory {
	switch {
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return app.FailureTransient
	case status == http.StatusNotFound:
		return app.FailureUnavailable
	default:
		return app.FailurePermanent
	}
}
