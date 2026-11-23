package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
	Env        map[string]string
}

// LocalGateway manages one loopback gateway process.
type LocalGateway struct {
	spec        LocalGatewaySpec
	HealthCheck func() bool // test seam; nil uses HTTP against BaseURL+HealthPath
}

type runningProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
}

var (
	gatewayMu      sync.Mutex
	gatewayProcess = make(map[string]*runningProcess)
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
	if strings.HasPrefix(g.spec.Binary, "./") && g.spec.Dir != "" {
		return filepath.Join(g.spec.Dir, strings.TrimPrefix(g.spec.Binary, "./"))
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
	// A responding port means the gateway is up. Several upstreams require
	// their own login on /v1/models and would answer 401 to our local key.
	return true
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
	cmd.Dir = g.spec.Dir
	// Never let a gateway inherit the caller's terminal: several of them
	// watch stdin to detect parent exit, which keeps the user's shell open.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if len(g.spec.Env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range g.spec.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdin = strings.NewReader("")
	// A real *os.File avoids os/exec's copying goroutines: a grandchild that
	// inherits the write end would keep Wait() blocked forever otherwise.
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	running := &runningProcess{cmd: cmd, done: make(chan struct{})}
	gatewayProcess[g.spec.Name] = running
	// Exactly one Wait() per process, owned by the manager. A second Wait()
	// from ShutdownGateways can block forever on some platforms.
	go func() {
		_ = cmd.Wait()
		_ = devNull.Close()
		gatewayMu.Lock()
		if gatewayProcess[g.spec.Name] == running {
			delete(gatewayProcess, g.spec.Name)
		}
		gatewayMu.Unlock()
		close(running.done)
	}()
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
	_ = cmd.Process.Kill()
	<-running.done
	return fmt.Errorf("gateway %s did not become healthy within 15s", g.spec.Name)
}

func runningGateways() map[string]*exec.Cmd {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	copy := make(map[string]*exec.Cmd, len(gatewayProcess))
	for name, running := range gatewayProcess {
		copy[name] = running.cmd
	}
	return copy
}

// ShutdownGateways terminates every gateway started by this process.
func ShutdownGateways() {
	gatewayMu.Lock()
	processes := make([]*runningProcess, 0, len(gatewayProcess))
	for name, running := range gatewayProcess {
		processes = append(processes, running)
		delete(gatewayProcess, name)
	}
	gatewayMu.Unlock()
	for _, running := range processes {
		if running.cmd.Process == nil {
			continue
		}
		_ = running.cmd.Process.Signal(os.Interrupt)
		select {
		case <-running.done:
		case <-time.After(500 * time.Millisecond):
			_ = syscall.Kill(-running.cmd.Process.Pid, syscall.SIGKILL)
			<-running.done
		}
	}
}
