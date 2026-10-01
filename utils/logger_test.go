package utils

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

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

func TestDefaultLoggerOutputIsStderr(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = oldStderr }()
	defer SetOutput(oldStderr)
	SetOutput(nil)
	Info("stderr default test")
	_ = write.Close()
	data, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	if !strings.Contains(string(data), "stderr default test") {
		t.Fatalf("stderr output = %q", data)
	}
}

func TestRequestLoggerIncludesIdentityAndDefaultsToMonochrome(t *testing.T) {
	var output bytes.Buffer
	SetOutput(&output)
	defer SetOutput(os.Stdout)
	SetNoColor(true)
	request := NewRequestLogger("GET", "/health", "must-not-appear", "must-not-appear")
	request.Start()
	request.End(200, "request_id=%s status=%d", request.ID, 200)
	if strings.Contains(output.String(), "\033[") || !strings.Contains(output.String(), request.ID) {
		t.Fatalf("request logs lack monochrome request identity: %q", output.String())
	}
	if strings.Contains(output.String(), "must-not-appear") {
		t.Fatalf("request logs contain discarded fields: %q", output.String())
	}
	if time.Since(request.StartTime) < 0 {
		t.Fatal("request logger elapsed duration is invalid")
	}
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
