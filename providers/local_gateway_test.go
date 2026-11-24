package providers

import (
	"context"
	"errors"
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

func TestHealthyRejectsForeignServerOnBridgePort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()
	gateway := NewLocalGateway(LocalGatewaySpec{
		Name: "bridge", BaseURL: server.URL, HealthPath: "/health",
		HealthBody: `"service":"voie-bridge"`,
	})
	if gateway.healthy() {
		t.Fatal("foreign server on the bridge port was reported healthy")
	}
}

func TestHealthyAcceptsOwnBridgeHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","service":"voie-bridge"}`))
	}))
	defer server.Close()
	gateway := NewLocalGateway(LocalGatewaySpec{
		Name: "bridge", BaseURL: server.URL, HealthPath: "/health",
		HealthBody: `"service":"voie-bridge"`,
	})
	if !gateway.healthy() {
		t.Fatal("own bridge health response was rejected")
	}
}

func TestStartACPBridgePropagatesCancellation(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "codebuddy")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := StartACPBridge(ctx, "codebuddy")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartACPBridge error = %v, want context.Canceled", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation took %s, want prompt", time.Since(start))
	}
}

func TestEnsureDoesNotHoldGlobalLockWhilePolling(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	callsA := 0
	a := NewLocalGateway(LocalGatewaySpec{Name: "lock-test-a", Binary: "sleep", Args: []string{"30"}, BaseURL: "http://127.0.0.1:1", HealthPath: "/health"})
	a.HealthCheck = func() bool {
		callsA++
		if callsA == 1 {
			return false
		}
		<-block
		return false
	}
	b := NewLocalGateway(LocalGatewaySpec{Name: "lock-test-b", Binary: "sleep", Args: []string{"30"}, BaseURL: "http://127.0.0.1:2", HealthPath: "/health"})
	callsB := 0
	b.HealthCheck = func() bool {
		callsB++
		return callsB >= 2
	}
	doneA := make(chan struct{})
	go func() {
		_ = a.Ensure(context.Background())
		close(doneA)
	}()
	time.Sleep(100 * time.Millisecond)
	doneB := make(chan error, 1)
	go func() { doneB <- b.Ensure(context.Background()) }()
	select {
	case err := <-doneB:
		if err != nil {
			t.Fatalf("gateway B Ensure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("gateway B blocked on the global gateway lock while A was polling")
	}
	select {
	case <-doneA:
	default:
	}
}

func TestEnsureIncludesChildDiagnostics(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "diagnostic-gateway")
	script := "#!/bin/sh\necho GATEWAY_DIAGNOSTIC >&2\nexec sleep 30\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	gateway := NewLocalGateway(LocalGatewaySpec{Name: "diagnostic", Binary: binary, BaseURL: "http://127.0.0.1:1", HealthPath: "/health"})
	gateway.HealthCheck = func() bool { return false }
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := gateway.Ensure(ctx)
	if err == nil || !strings.Contains(err.Error(), "GATEWAY_DIAGNOSTIC") {
		t.Fatalf("Ensure error = %v, want the child's diagnostic output", err)
	}
}
