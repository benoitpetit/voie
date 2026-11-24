//go:build windows

package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceExecutableWindows(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	target := filepath.Join(dir, "voie.exe")
	if err := os.WriteFile(staged, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	pending, err := replaceExecutable(staged, target)
	if err != nil {
		t.Fatal(err)
	}
	if !pending {
		t.Fatal("Windows replacement must report a pending deferred replacement")
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged binary must survive until the helper runs: %v", err)
	}
}
