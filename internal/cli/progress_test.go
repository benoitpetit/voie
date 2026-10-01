package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/app"
)

func TestLineProgressReporterWritesMonochromeStages(t *testing.T) {
	var stderr bytes.Buffer
	reporter := newProgressReporter(&stderr, false)
	reporter.Handle(app.ProgressEvent{Stage: "routing", Message: "Selected model (provider)", Status: "succeeded"})
	reporter.Finish(nil)
	if !strings.Contains(stderr.String(), "routing") || !strings.Contains(stderr.String(), "Selected model") {
		t.Fatalf("progress output = %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "\033[") {
		t.Fatalf("progress output contains terminal escapes: %q", stderr.String())
	}
}
