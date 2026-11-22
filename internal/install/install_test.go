package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
			_, _ = fmt.Fprintf(w, `{"tag_name":"v1","assets":[{"name":"CLIProxyAPI_v1_%s_%s.tar.gz","browser_download_url":%q}]}`,
				runtime.GOOS, runtime.GOARCH, server.URL+"/asset")
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
