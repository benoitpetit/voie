package providers

import (
	"context"
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

// Available reports whether the gateway can be started. It performs no network.
func (g *LocalGateway) Available() bool {
	if _, err := exec.LookPath(g.spec.Binary); err != nil {
		return false
	}
	store, err := config.LoadCredentials()
	if err != nil {
		return false
	}
	return store.Token(g.spec.Name) != ""
}

func (g *LocalGateway) healthy() bool {
	if g.HealthCheck != nil {
		return g.HealthCheck()
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(g.spec.BaseURL + g.spec.HealthPath)
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
	cmd := exec.CommandContext(ctx, g.spec.Binary, g.spec.Args...)
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
	return nil
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
		}
		delete(gatewayProcess, name)
	}
}
