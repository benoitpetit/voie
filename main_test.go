package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestFormatErrorIncludesTypeAndCode(t *testing.T) {
	err := app.NewProviderFailure(app.FailureTransient, "a", "m", 503, errors.New("down"))
	formatted := formatError(err)
	if !strings.Contains(formatted, ":") || !strings.Contains(formatted, "transient") {
		t.Fatalf("formatted = %q", formatted)
	}
}
