package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func releaseArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestInstallGitHubRelease(t *testing.T) {
	archive := releaseArchive(t, "CLIProxyAPI", "#!/bin/sh\necho ok\n")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/router-for-me/CLIProxyAPI/releases/latest":
			assetName := fmt.Sprintf("CLIProxyAPI_v1_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
			_, _ = fmt.Fprintf(w, `{"tag_name":"v1","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
				assetName, server.URL+"/asset", server.URL+"/checksums")
		case "/checksums":
			sum := sha256.Sum256(archive)
			_, _ = fmt.Fprintf(w, "%s  CLIProxyAPI_v1_%s_%s.tar.gz\n", hex.EncodeToString(sum[:]), runtime.GOOS, runtime.GOARCH)
		case "/asset":
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	oldBase := githubAPIBase
	githubAPIBase = server.URL
	defer func() { githubAPIBase = oldBase }()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, err := Install(context.Background(), "antigravity", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(BinaryDir(), "CLIProxyAPI"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\necho ok\n" {
		t.Errorf("binary content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("binary mode = %o, want 755", info.Mode().Perm())
	}
}

func TestInstallUsesPackageManagerForCLIs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var calls []string
	runner := func(name string, args ...string) error {
		calls = append(calls, name+" "+args[0])
		return nil
	}
	for _, provider := range []string{"cline", "kiro"} {
		if _, err := Install(context.Background(), provider, runner); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"npm install", "npm install", "git clone", "uv sync"}
	if len(calls) != len(want) {
		t.Errorf("runner calls = %v, want %v", calls, want)
	}
}

func TestInstallRejectsUnknownProvider(t *testing.T) {
	if _, err := Install(context.Background(), "nope", nil); err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
}

func TestInstallGitHubReleaseRejectsChecksumMismatch(t *testing.T) {
	archive := releaseArchive(t, "CLIProxyAPI", "#!/bin/sh\necho bad\n")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/router-for-me/CLIProxyAPI/releases/latest":
			_, _ = fmt.Fprintf(w, `{"tag_name":"v1","assets":[{"name":"CLIProxyAPI_v1_%s_%s.tar.gz","browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`,
				runtime.GOOS, runtime.GOARCH, server.URL+"/asset", server.URL+"/checksums")
		case "/checksums":
			_, _ = fmt.Fprintf(w, "%064d  CLIProxyAPI_v1_%s_%s.tar.gz\n", 0, runtime.GOOS, runtime.GOARCH)
		case "/asset":
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	defer func() { githubAPIBase = oldBase }()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := Install(context.Background(), "antigravity", nil); err == nil {
		t.Fatal("mismatched checksum was accepted")
	}
	if _, err := os.Stat(filepath.Join(BinaryDir(), "CLIProxyAPI")); err == nil {
		t.Fatal("unverified binary was written")
	}
}

func TestInstallGitHubReleaseRejectsMissingChecksums(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":"v1","assets":[{"name":"CLIProxyAPI_v1_%s_%s.tar.gz","browser_download_url":%q}]}`,
			runtime.GOOS, runtime.GOARCH, server.URL+"/asset")
	}))
	defer server.Close()
	oldBase := githubAPIBase
	githubAPIBase = server.URL
	defer func() { githubAPIBase = oldBase }()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := Install(context.Background(), "antigravity", nil); err == nil {
		t.Fatal("release without checksums.txt was accepted")
	}
}

func TestDownloadBinaryRejectsOversizedArchive(t *testing.T) {
	oldMax := maxArchiveBytes
	maxArchiveBytes = 16
	defer func() { maxArchiveBytes = oldMax }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 32))
	}))
	defer server.Close()
	if _, err := downloadBinary(context.Background(), t.TempDir(), "CLIProxyAPI", server.URL, ""); err == nil {
		t.Fatal("oversized archive was accepted")
	}
}

func TestUninstallRemovesCliPackageAndBinary(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := BinaryDir()
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", ".bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "node_modules", ".bin", "codebuddy")
	if err := os.WriteFile(binary, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	runner := func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	if err := Uninstall(context.Background(), "codebuddy", runner); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "npm uninstall --prefix "+dir+" @tencent-ai/codebuddy-code" {
		t.Fatalf("calls = %v", calls)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("binary still present: %v", err)
	}
}
