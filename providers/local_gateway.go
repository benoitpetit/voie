package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	HealthBody string // required substring in a healthy response; guards loopback ports
	KeyEnv     string
	Port       int
	Env        map[string]string
	EnvFile    string
	Stop       []string
}

// LocalGateway manages one loopback gateway process.
type LocalGateway struct {
	spec        LocalGatewaySpec
	HealthCheck func() bool // test seam; nil uses HTTP against BaseURL+HealthPath
}

// lockedBuffer collects a child process's stderr without racing the Wait
// goroutine that reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() < 64<<10 {
		return b.buf.Write(data)
	}
	return len(data), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.buf.String()
	if len(text) > 4<<10 {
		text = text[len(text)-4<<10:]
	}
	return strings.TrimSpace(text)
}

type runningProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	spec LocalGatewaySpec
}

var (
	gatewayMu      sync.Mutex
	gatewayProcess = make(map[string]*runningProcess)
	gatewayStarted = make(map[string]bool)
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

func (g *LocalGateway) resolveBinary() string {
	// Installers drop binaries inside the gateway directory (for example
	// venv/bin/uvicorn). Any relative path with a separator resolves there,
	// not against voie's own working directory.
	if g.spec.Dir != "" && !filepath.IsAbs(g.spec.Binary) && strings.ContainsRune(g.spec.Binary, os.PathSeparator) {
		return filepath.Join(g.spec.Dir, g.spec.Binary)
	}
	return g.spec.Binary
}

// Available reports whether the gateway can be started. It performs no network.
func (g *LocalGateway) Available() bool {
	_, err := exec.LookPath(g.resolveBinary())
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
	if g.spec.HealthBody != "" {
		if resp.StatusCode != http.StatusOK {
			return false
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if err != nil || !strings.Contains(string(body), g.spec.HealthBody) {
			return false
		}
	}
	return true
}

// Ensure starts the gateway if it is not already healthy.
func (g *LocalGateway) Ensure(ctx context.Context) error {
	if g.healthy() {
		return nil
	}
	gatewayMu.Lock()
	locked := true
	defer func() {
		if locked {
			gatewayMu.Unlock()
		}
	}()
	if _, alreadyRunning := gatewayProcess[g.spec.Name]; alreadyRunning {
		return nil
	}
	if _, err := exec.LookPath(g.resolveBinary()); err != nil {
		return err
	}
	if g.spec.ConfigPath != "" && len(g.spec.ConfigBody) > 0 {
		if _, err := os.Stat(g.spec.ConfigPath); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(g.spec.ConfigPath), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(g.spec.ConfigPath, g.spec.ConfigBody, 0o600); err != nil {
				return err
			}
		}
	}
	// Gateway lifetime is owned by the manager, not by the caller's request
	// context: a request ending must not kill a shared local gateway.
	cmd := exec.Command(g.resolveBinary(), g.spec.Args...)
	if info, err := os.Stat(g.spec.Dir); err == nil && info.IsDir() {
		cmd.Dir = g.spec.Dir
	}
	// Never let a gateway inherit the caller's terminal: several of them
	// watch stdin to detect parent exit, which keeps the user's shell open.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if g.spec.EnvFile != "" {
		if data, err := os.ReadFile(g.spec.EnvFile); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					cmd.Env = append(cmd.Env, line)
				}
			}
		}
	}
	if len(g.spec.Env) > 0 {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		for key, value := range g.spec.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdin = strings.NewReader("")
	// A real *os.File avoids os/exec's copying goroutines: a grandchild that
	// inherits the write end would keep Wait() blocked forever otherwise.
	cmd.Stdout = devNull
	diagnostics := &lockedBuffer{}
	cmd.Stderr = diagnostics
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	running := &runningProcess{cmd: cmd, done: make(chan struct{}), spec: g.spec}
	gatewayProcess[g.spec.Name] = running
	gatewayStarted[g.spec.Name] = true
	// Health polling must not hold the global lock: a gateway that never
	// becomes healthy would block every other gateway start for up to 15s.
	locked = false
	gatewayMu.Unlock()
	// Exactly one Wait() per process, owned by the manager. A second Wait()
	// from ShutdownGateways can block forever on some platforms.
	go func() {
		_ = cmd.Wait()
		_ = devNull.Close()
		// Signal exit before taking the lock so a waiter on done can proceed
		// even while this goroutine is waiting for gatewayMu.
		close(running.done)
		gatewayMu.Lock()
		if gatewayProcess[g.spec.Name] == running {
			delete(gatewayProcess, g.spec.Name)
		}
		gatewayMu.Unlock()
	}()
	fail := func(cause error) error {
		_ = cmd.Process.Kill()
		select {
		case <-running.done:
		case <-time.After(5 * time.Second):
		}
		return fmt.Errorf("gateway %s did not become healthy: %w: %s", g.spec.Name, cause, diagnostics.String())
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.healthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fail(fmt.Errorf("timed out after 15s"))
}

// ShutdownGateways terminates every gateway started by this process.
func ShutdownGateways() {
	gatewayMu.Lock()
	processes := make([]*runningProcess, 0, len(gatewayProcess))
	for name, running := range gatewayProcess {
		processes = append(processes, running)
		delete(gatewayProcess, name)
	}
	for name := range gatewayStarted {
		for _, spec := range ExternalGateways {
			if spec.Name != name || len(spec.Stop) == 0 {
				continue
			}
			stop := exec.Command(spec.Stop[0], spec.Stop[1:]...)
			stop.Dir = gatewayDir(spec)
			stop.Env = os.Environ()
			_ = stop.Run()
			if spec.StopKillBinary != "" {
				killProcessesByBinary(spec.StopKillBinary)
			}
		}
	}
	for name := range gatewayStarted {
		delete(gatewayStarted, name)
	}
	gatewayMu.Unlock()
	for _, running := range processes {
		if len(running.spec.Stop) > 0 {
			stop := exec.Command(running.spec.Stop[0], running.spec.Stop[1:]...)
			stop.Dir = running.spec.Dir
			stop.Env = os.Environ()
			_ = stop.Run()
		}
		if running.cmd.Process == nil {
			continue
		}
		_ = running.cmd.Process.Signal(os.Interrupt)
		select {
		case <-running.done:
		case <-time.After(500 * time.Millisecond):
			killProcessGroup(running.cmd.Process.Pid)
			<-running.done
		}
	}
}
