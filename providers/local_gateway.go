package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	EnvFile    string
	Stop       []string
}

// LocalGateway manages one loopback gateway process.
type LocalGateway struct {
	spec        LocalGatewaySpec
	HealthCheck func() bool // test seam; nil uses HTTP against BaseURL+HealthPath
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
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	running := &runningProcess{cmd: cmd, done: make(chan struct{}), spec: g.spec}
	gatewayProcess[g.spec.Name] = running
	gatewayStarted[g.spec.Name] = true
	// Exactly one Wait() per process, owned by the manager. A second Wait()
	// from ShutdownGateways can block forever on some platforms.
	go func() {
		_ = cmd.Wait()
		_ = devNull.Close()
		// Signal exit before taking the lock: Ensure() holds gatewayMu while
		// it waits on done, so closing afterwards would deadlock whenever a
		// gateway never becomes healthy.
		close(running.done)
		gatewayMu.Lock()
		if gatewayProcess[g.spec.Name] == running {
			delete(gatewayProcess, g.spec.Name)
		}
		gatewayMu.Unlock()
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
	select {
	case <-running.done:
	case <-time.After(5 * time.Second):
	}
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

func killProcessesByBinary(binary string) {
	want := filepath.Base(binary)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		target, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil {
			continue
		}
		if !strings.Contains(filepath.Base(strings.TrimSuffix(target, " (deleted)")), want) {
			continue
		}
		pid, _ := strconv.Atoi(entry.Name())
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
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
			_ = syscall.Kill(-running.cmd.Process.Pid, syscall.SIGKILL)
			<-running.done
		}
	}
}
