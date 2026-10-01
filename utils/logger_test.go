package utils

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/benoitpetit/voie/internal/brand"
)

func TestSetOutputRoutesLogsToConfiguredWriter(t *testing.T) {
	var output bytes.Buffer
	SetOutput(&output)
	Info("logger output test")
	if !strings.Contains(output.String(), "logger output test") {
		t.Fatalf("log output = %q", output.String())
	}
	SetOutput(os.Stdout)
}

func TestLogServerStartUsesBuildBrandAndVersion(t *testing.T) {
	var output bytes.Buffer
	SetOutput(&output)
	LogServerStart("8080")
	SetOutput(os.Stdout)

	want := brand.Name + " v" + brand.Version + " starting on :8080"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("server start log = %q, want it to contain %q", output.String(), want)
	}
}
