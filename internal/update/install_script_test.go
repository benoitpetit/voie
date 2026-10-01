package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUnixInstallerHelpDoesNotDownload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer runs on Linux and macOS")
	}
	command := exec.Command("sh", filepath.Join("..", "..", "install.sh"), "--help")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installer help failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "--install-dir") || !strings.Contains(string(output), "--version") {
		t.Fatalf("installer help = %q", output)
	}
}

func TestUnixInstallerRejectsUnsupportedOperatingSystemBeforeDownload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer runs on Linux and macOS")
	}
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	uname := filepath.Join(binDir, "uname")
	if err := os.WriteFile(uname, []byte("#!/bin/sh\nprintf 'FreeBSD\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", filepath.Join("..", "..", "install.sh"))
	command.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "unsupported operating system: FreeBSD") {
		t.Fatalf("unsupported target should be rejected before download, output=%q err=%v", output, err)
	}
}

func TestUnixInstallerInstallsLatestChecksumVerifiedArchive(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix installer supports Linux and macOS")
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		t.Skipf("unsupported test architecture %s", arch)
	}
	version := "0.0.2"
	archiveName := fmt.Sprintf("voie_%s_%s_%s.tar.gz", version, runtime.GOOS, arch)
	archive := tarGz(map[string][]byte{"voie": []byte("verified voie executable"), "README.md": []byte("release notes")})
	sum := sha256.Sum256(archive)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + archiveName + "\n")
	output, installDir, err := runUnixInstaller(t, archive, checksums)
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(installDir, "voie")); err != nil {
		t.Fatalf("verified binary was not installed: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(filepath.Join(installDir, "voie"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "verified voie executable" {
		t.Fatalf("installed binary = %q", contents)
	}
}

func TestUnixInstallerLeavesExistingBinaryWhenChecksumIsWrong(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix installer supports Linux and macOS")
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		t.Skipf("unsupported test architecture %s", arch)
	}
	archiveName := fmt.Sprintf("voie_0.0.2_%s_%s.tar.gz", runtime.GOOS, arch)
	archive := tarGz(map[string][]byte{"voie": []byte("new binary")})
	checksums := []byte(strings.Repeat("0", 64) + "  " + archiveName + "\n")
	output, installDir, err := runUnixInstaller(t, archive, checksums, "--version", "0.0.2")
	if err == nil || string(output) == "" {
		t.Fatalf("checksum mismatch should fail with an error, output=%q err=%v", output, err)
	}
	contents, err := os.ReadFile(filepath.Join(installDir, "voie"))
	if err != nil || string(contents) != "old executable" {
		t.Fatalf("existing binary changed despite a bad checksum: contents=%q err=%v", contents, err)
	}
}

func runUnixInstaller(t *testing.T, archive, checksums []byte, extraArgs ...string) ([]byte, string, error) {
	t.Helper()
	root := t.TempDir()
	fixtureArchive := filepath.Join(root, "archive.tar.gz")
	fixtureChecksums := filepath.Join(root, "checksums.txt")
	if err := os.WriteFile(fixtureArchive, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixtureChecksums, checksums, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	curl := `#!/bin/sh
set -eu
destination=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o|--output) destination="$2"; shift 2 ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  */checksums.txt) cp "$FIXTURE_CHECKSUMS" "$destination" ;;
  *) cp "$FIXTURE_ARCHIVE" "$destination" ;;
esac
`
	curlPath := filepath.Join(binDir, "curl")
	if err := os.WriteFile(curlPath, []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(root, "install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "voie"), []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{}, extraArgs...)
	args = append(args, "--install-dir", installDir)
	command := exec.Command("sh", append([]string{filepath.Join("..", "..", "install.sh")}, args...)...)
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FIXTURE_ARCHIVE="+fixtureArchive,
		"FIXTURE_CHECKSUMS="+fixtureChecksums,
	)
	output, err := command.CombinedOutput()
	return output, installDir, err
}
