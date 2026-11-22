package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/benoitpetit/voie/config"
)

// LocalGatewaySpec describes an external gateway process that voie starts on
// demand. The gateway owns upstream auth; voie only knows the local key.
type LocalGatewaySpec struct {
	Name       string
	Binary     string
	Args       []string
	Dir        string
	ConfigPath string
	ConfigBody []byte
	BaseURL    string
	HealthPath string
	KeyEnv     string
	Port       int
}

// LocalGateway manages one loopback gateway process.
type LocalGateway struct {
	spec        LocalGatewaySpec
	HealthCheck func() bool // test seam; nil uses HTTP against BaseURL+HealthPath
}

var (
	gatewayMu      sync.Mutex
	gatewayProcess = make(map[string]*exec.Cmd)
)

func NewLocalGateway(spec LocalGatewaySpec) *LocalGateway { return &LocalGateway{spec: spec} }

// localKey resolves the loopback key. Local gateways default to voie-local, so
// the key is never a precondition for visibility — only the binary is.
func (g *LocalGateway) localKey() string {
	store, err := config.LoadCredentials()
	if err != nil {
		return "voie-local"
	}
	if key := store.Token(g.spec.Name); key != "" {
		return key
	}
	return "voie-local"
}

// Available reports whether the gateway can be started. It performs no network.
func (g *LocalGateway) Available() bool {
	_, err := exec.LookPath(g.spec.Binary)
	return err == nil
}

func (g *LocalGateway) healthy() bool {
	if g.HealthCheck != nil {
		return g.HealthCheck()
	}
	req, err := http.NewRequest(http.MethodGet, g.spec.BaseURL+g.spec.HealthPath, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+g.localKey())
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// Ensure starts the gateway if it is not already healthy.
func (g *LocalGateway) Ensure(ctx context.Context) error {
	if g.healthy() {
		return nil
	}
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	if g.healthy() {
		return nil
	}
	if _, err := exec.LookPath(g.spec.Binary); err != nil {
		return err
	}
	if g.spec.ConfigPath != "" && len(g.spec.ConfigBody) > 0 {
		if err := os.MkdirAll(filepath.Dir(g.spec.ConfigPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(g.spec.ConfigPath, g.spec.ConfigBody, 0o600); err != nil {
			return err
		}
	}
	// Gateway lifetime is owned by the manager, not by the caller's request
	// context: a request ending must not kill a shared local gateway.
	cmd := exec.Command(g.spec.Binary, g.spec.Args...)
	cmd.Dir = g.spec.Dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	gatewayProcess[g.spec.Name] = cmd
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.healthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("gateway %s did not become healthy within 15s", g.spec.Name)
}

func runningGateways() map[string]*exec.Cmd {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	copy := make(map[string]*exec.Cmd, len(gatewayProcess))
	for name, cmd := range gatewayProcess {
		copy[name] = cmd
	}
	return copy
}

// ShutdownGateways terminates every gateway started by this process.
func ShutdownGateways() {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	for name, cmd := range gatewayProcess {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		delete(gatewayProcess, name)
	}
}
