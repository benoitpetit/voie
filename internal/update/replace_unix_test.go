//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceExecutableRenamesAtomically(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	target := filepath.Join(dir, "voie")
	if err := os.WriteFile(staged, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	pending, err := replaceExecutable(staged, target)
	if err != nil || pending {
		t.Fatalf("replaceExecutable = %v, %v", pending, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "binary" {
		t.Fatalf("target = %q, %v", data, err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged file still exists: %v", err)
	}
}
