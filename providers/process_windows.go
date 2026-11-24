//go:build windows

package providers

import (
	"os/exec"
	"path/filepath"
)

// Windows has no Unix process groups; a detached console process and
// taskkill's tree option serve the same purpose here.
func setProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(pid int) {
	kill := exec.Command("taskkill", "/PID", itoa(pid), "/T", "/F")
	_ = kill.Run()
}

func killProcessesByBinary(binary string) {
	kill := exec.Command("taskkill", "/IM", filepath.Base(binary), "/F")
	_ = kill.Run()
}
