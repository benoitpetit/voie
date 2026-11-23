package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gatewaySpec(t *testing.T, name, binary string, args []string) LocalGatewaySpec {
	t.Helper()
	dir := t.TempDir()
	return LocalGatewaySpec{
		Name:       name,
		Binary:     binary,
		Args:       args,
		ConfigPath: filepath.Join(dir, name+".yaml"),
		ConfigBody: []byte("host: 127.0.0.1\n"),
		BaseURL:    "http://127.0.0.1:65534",
		HealthPath: "/health",
		KeyEnv:     "VOIE_" + strings.ToUpper(name) + "_KEY",
		Port:       65534,
	}
}

func TestGatewayMissingBinaryStaysHiddenWithoutSpawn(t *testing.T) {
	spec := gatewaySpec(t, "missing", "/nonexistent/binary", nil)
	gateway := NewLocalGateway(spec)
	if gateway.Available() {
		t.Error("Available() = true for a missing binary")
	}
}

func TestGatewayGeneratesConfigAndSpawnsOnce(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "spawned")
	spec := gatewaySpec(t, "spawn", "/bin/sh", []string{"-c", "printf x >> " + marker})
	gateway := NewLocalGateway(spec)
	checks := 0
	gateway.HealthCheck = func() bool { checks++; return checks > 2 }

	if err := gateway.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !fileExists(marker) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("process was not spawned: %v", err)
	}
	if strings.Count(string(data), "x") != 1 {
		t.Errorf("spawn count = %d, want 1", strings.Count(string(data), "x"))
	}
	config, err := os.ReadFile(spec.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(config) != string(spec.ConfigBody) {
		t.Errorf("config = %q, want %q", config, spec.ConfigBody)
	}
	info, err := os.Stat(spec.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("config mode = %o, want 600", got)
	}
	ShutdownGateways()
}

func TestGatewayReusesHealthyProcess(t *testing.T) {
	spec := gatewaySpec(t, "healthy", "/nonexistent/binary", nil)
	gateway := NewLocalGateway(spec)
	gateway.HealthCheck = func() bool { return true }
	if err := gateway.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(spec.ConfigPath); !os.IsNotExist(err) {
		t.Error("a healthy gateway must not be spawned or reconfigured")
	}
}

func TestGatewayHealthTreatsAuthResponseAsAlive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	spec := LocalGatewaySpec{Name: "auth", Binary: "/bin/sh", BaseURL: server.URL, HealthPath: "/v1/models"}
	gateway := NewLocalGateway(spec)
	if !gateway.healthy() {
		t.Error("a gateway answering 401 is up but was considered unhealthy")
	}
}

func TestGatewayRelativeBinaryResolvesAgainstDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := LocalGatewaySpec{Name: "relative", Binary: "./gateway", Dir: dir}
	gateway := NewLocalGateway(spec)
	if !gateway.Available() {
		t.Error("relative binary was not resolved against the gateway directory")
	}
}

func TestGatewayShutdownTerminatesProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "spawned")
	spec := gatewaySpec(t, "shutdown", "/bin/sh", []string{"-c", "echo x >> " + marker + "; sleep 30"})
	gateway := NewLocalGateway(spec)
	gateway.HealthCheck = func() bool { return true }
	if err := gateway.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	ShutdownGateways()
	if len(runningGateways()) != 0 {
		t.Errorf("running gateways after shutdown = %d", len(runningGateways()))
	}
}
