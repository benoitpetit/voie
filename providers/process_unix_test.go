//go:build !windows

package providers

import (
	"os/exec"
	"testing"
	"time"
)

func TestProcessHelpersCompileAndSetGroup(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	setProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("setProcessGroup did not set Setpgid")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	killProcessGroup(cmd.Process.Pid)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		killProcessGroup(cmd.Process.Pid)
		t.Fatal("process group was not terminated")
	}
}
