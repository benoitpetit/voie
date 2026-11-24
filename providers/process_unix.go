//go:build !windows

package providers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
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
