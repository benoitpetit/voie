package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProviderFailureErrorStringsDoNotCarryBodies(t *testing.T) {
	network := NewProviderFailure(FailureTransient, "perplexity", "turbo", 0, errors.New("dial tcp: connection refused"))
	status := NewProviderFailure(FailureUnavailable, "perplexity", "turbo", 404, nil)

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"network transient", network, `provider "perplexity": network failure for model "turbo"`},
		{"status renders code", status, `provider "perplexity" returned HTTP 404 for model "turbo"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if strings.Contains(tc.err.Error(), "secret-body") || strings.Contains(tc.err.Error(), "response text") {
				t.Errorf("Error() leaked a response body: %q", tc.err.Error())
			}
		})
	}
}

func TestProviderFailureClassifiesCategories(t *testing.T) {
	cases := []struct {
		name     string
		category FailureCategory
		wantFail bool
	}{
		{"transient", FailureTransient, false},
		{"unavailable", FailureUnavailable, false},
		{"permanent", FailurePermanent, false},
		{"unknown defaults to string", FailureCategory(99), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewProviderFailure(tc.category, "provider", "model", 0, nil)
			var pf *ProviderFailure
			if !errors.As(err, &pf) {
				t.Fatalf("errors.As did not yield *ProviderFailure")
			}
			if pf.Category != tc.category {
				t.Errorf("Category = %v, want %v", pf.Category, tc.category)
			}
			if !errors.Is(err, ErrUpstream) {
				t.Errorf("errors.Is(err, ErrUpstream) = false")
			}
			if err.Error() == "" {
				t.Fatal("empty Error() string")
			}
		})
	}
}

func TestProviderFailurePreservesCause(t *testing.T) {
	cause := errors.New("boom")
	err := NewProviderFailure(FailureTransient, "provider", "model", 503, cause)
	var pf *ProviderFailure
	if !errors.As(err, &pf) {
		t.Fatal("errors.As did not yield *ProviderFailure")
	}
	if pf.Cause == nil || pf.Cause != cause {
		t.Errorf("Cause not preserved: %v", pf.Cause)
	}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false; unwrap chain lost the cause")
	}
}

func TestWrapNetworkFailureClassifiesContextErrors(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		want  FailureCategory
	}{
		{"connectivity", errors.New("dial tcp: connection refused"), FailureTransient},
		{"/", errors.New("EOF"), FailureTransient},
		{"deadline", context.DeadlineExceeded, FailurePermanent},
		{"canceled", context.Canceled, FailurePermanent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := WrapNetworkFailure(tc.cause, "provider", "model")
			var pf *ProviderFailure
			if !errors.As(err, &pf) {
				t.Fatalf("errors.As did not yield *ProviderFailure: %v", err)
			}
			if pf.Category != tc.want {
				t.Errorf("Category = %v, want %v", pf.Category, tc.want)
			}
			if pf.Status != 0 {
				t.Errorf("Status = %d, want 0 for network failure", pf.Status)
			}
			if !errors.Is(err, ErrUpstream) {
				t.Errorf("errors.Is(err, ErrUpstream) = false")
			}
			if !errors.Is(err, tc.cause) {
				t.Errorf("errors.Is(err, cause) = false")
			}
		})
	}
}

func TestFailureCategoryAndOutcomeStrings(t *testing.T) {
	if FailureTransient.String() == "" || FailureUnavailable.String() == "" || FailurePermanent.String() == "" {
		t.Error("FailureCategory.String() must not be empty")
	}
	categories := map[string]string{
		OutcomeSucceeded:        "succeeded",
		OutcomeRetryableFailure: "retryable_failure",
		OutcomeUnavailable:      "unavailable",
		OutcomeFailed:           "failed",
	}
	for want, constant := range categories {
		if constant != want {
			t.Errorf("outcome constant value = %q, want %q", constant, want)
		}
	}
}
