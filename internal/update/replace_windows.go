//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const windowsReplacementScript = `param([int]$ParentPid, [string]$StagedPath, [string]$TargetPath, [string]$LogPath, [string]$ScriptPath)
$ErrorActionPreference = 'Stop'
$backupPath = "$TargetPath.voie-backup"
try {
  try {
    $parent = Get-Process -Id $ParentPid -ErrorAction Stop
    $parent.WaitForExit()
  } catch {}
  Move-Item -LiteralPath $TargetPath -Destination $backupPath -Force
  try {
    Move-Item -LiteralPath $StagedPath -Destination $TargetPath -Force
    Remove-Item -LiteralPath $backupPath -Force
  } catch {
    if (Test-Path -LiteralPath $TargetPath) { Remove-Item -LiteralPath $TargetPath -Force }
    if (Test-Path -LiteralPath $backupPath) { Move-Item -LiteralPath $backupPath -Destination $TargetPath -Force }
    throw
  }
} catch {
  $_ | Out-File -LiteralPath $LogPath -Encoding utf8
  exit 1
} finally {
  Remove-Item -LiteralPath $StagedPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $ScriptPath -Force -ErrorAction SilentlyContinue
}`

func replaceExecutable(stagedPath, executablePath string) (bool, error) {
	script, err := os.CreateTemp("", "voie-update-*.ps1")
	if err != nil {
		return false, fmt.Errorf("create Windows replacement helper: %w", err)
	}
	scriptPath := script.Name()
	if _, err := script.WriteString(windowsReplacementScript); err != nil {
		_ = script.Close()
		_ = os.Remove(scriptPath)
		return false, fmt.Errorf("write Windows replacement helper: %w", err)
	}
	if err := script.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return false, fmt.Errorf("close Windows replacement helper: %w", err)
	}
	logPath := executablePath + ".update.log"
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", scriptPath, strconv.Itoa(os.Getpid()), stagedPath, executablePath, logPath, scriptPath)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008}
	if err := command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return false, fmt.Errorf("start Windows replacement helper: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return false, fmt.Errorf("release Windows replacement helper: %w", err)
	}
	return true, nil
}
