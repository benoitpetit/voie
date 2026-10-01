package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveAssetNameSupportsReleaseTargets(t *testing.T) {
	cases := []struct {
		version, goos, goarch, want string
	}{
		{"0.0.2", "linux", "amd64", "voie_0.0.2_linux_amd64.tar.gz"},
		{"v0.0.2", "darwin", "arm64", "voie_0.0.2_darwin_arm64.tar.gz"},
		{"0.0.2", "windows", "amd64", "voie_0.0.2_windows_amd64.zip"},
	}
	for _, tc := range cases {
		got, err := archiveAssetName(tc.version, tc.goos, tc.goarch)
		if err != nil || got != tc.want {
			t.Errorf("archiveAssetName(%q, %q, %q) = %q, %v; want %q", tc.version, tc.goos, tc.goarch, got, err, tc.want)
		}
	}
	if _, err := archiveAssetName("0.0.2", "windows", "arm64"); !errors.Is(err, ErrUnsupportedTarget) {
		t.Fatalf("unsupported target error = %v, want ErrUnsupportedTarget", err)
	}
}

func TestRunRejectsDevelopmentBeforeNetworkOrFileChanges(t *testing.T) {
	var requests int
	server := newTestServer(func(request *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(request, http.StatusInternalServerError, nil), nil
	})

	dir := t.TempDir()
	executable := filepath.Join(dir, "voie")
	if err := os.WriteFile(executable, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), testOptions(server, executable, "dev"))
	if !errors.Is(err, ErrDevelopmentBuild) {
		t.Fatalf("Run error = %v, want ErrDevelopmentBuild", err)
	}
	if requests != 0 {
		t.Fatalf("made %d network requests for a dev build", requests)
	}
	if got := readFile(t, executable); got != "old executable" {
		t.Fatalf("executable changed to %q", got)
	}
}

func TestRunRejectsUnsupportedTargetBeforeNetwork(t *testing.T) {
	var requests int
	server := newTestServer(func(request *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(request, http.StatusOK, nil), nil
	})
	options := testOptions(server, filepath.Join(t.TempDir(), "voie"), "0.0.1")
	options.GOOS, options.GOARCH = "windows", "arm64"
	if _, err := Run(context.Background(), options); !errors.Is(err, ErrUnsupportedTarget) {
		t.Fatalf("Run error = %v, want ErrUnsupportedTarget", err)
	}
	if requests != 0 {
		t.Fatalf("made %d network requests for an unsupported target", requests)
	}
}

func TestRunReportsAlreadyCurrentWithoutDownloadingAssets(t *testing.T) {
	var assetRequests int
	server := newTestServer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/repos/benoitpetit/voie/releases/latest" {
			return releaseResponse(request, "v0.0.1", nil), nil
		}
		assetRequests++
		return testHTTPResponse(request, http.StatusNotFound, nil), nil
	})

	options := testOptions(server, filepath.Join(t.TempDir(), "voie"), "0.0.1")
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.AlreadyCurrent || result.Updated || result.LatestVersion != "0.0.1" {
		t.Fatalf("result = %+v", result)
	}
	if assetRequests != 0 {
		t.Fatalf("downloaded %d assets when already current", assetRequests)
	}
}

func TestRunDownloadsVerifiesAndReplacesExecutable(t *testing.T) {
	assetName := "voie_0.0.2_linux_amd64.tar.gz"
	archive := tarGz(map[string][]byte{"voie": []byte("new executable"), "README.md": []byte("release notes")})
	assets := map[string][]byte{assetName: archive}
	assets["checksums.txt"] = []byte(checksumLine(assetName, archive))
	server := releaseServer(t, "v0.0.2", assets)

	target := filepath.Join(t.TempDir(), "voie")
	if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := testOptions(server, target, "0.0.1")
	options.ReplaceExecutable = func(stagedPath, executablePath string) (bool, error) {
		return false, os.Rename(stagedPath, executablePath)
	}
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.AlreadyCurrent || result.LatestVersion != "0.0.2" {
		t.Fatalf("result = %+v", result)
	}
	if got := readFile(t, target); got != "new executable" {
		t.Fatalf("installed executable = %q", got)
	}
}

func TestRunRejectsMissingOrIncorrectChecksumWithoutReplacing(t *testing.T) {
	archiveName := "voie_0.0.2_linux_amd64.tar.gz"
	archive := tarGz(map[string][]byte{"voie": []byte("new executable")})
	for _, tc := range []struct {
		name     string
		checksum []byte
	}{
		{name: "missing checksum"},
		{name: "incorrect checksum", checksum: []byte(strings.Repeat("0", 64) + "  " + archiveName + "\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assets := map[string][]byte{archiveName: archive, "checksums.txt": tc.checksum}
			server := releaseServer(t, "v0.0.2", assets)

			target := filepath.Join(t.TempDir(), "voie")
			if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
				t.Fatal(err)
			}
			called := false
			options := testOptions(server, target, "0.0.1")
			options.ReplaceExecutable = func(string, string) (bool, error) {
				called = true
				return false, nil
			}
			if _, err := Run(context.Background(), options); err == nil {
				t.Fatal("Run succeeded without a valid archive checksum")
			}
			if called {
				t.Fatal("replacement was attempted without a valid archive checksum")
			}
			if got := readFile(t, target); got != "old executable" {
				t.Fatalf("executable changed to %q", got)
			}
		})
	}
}

func TestRunReturnsHTTPAndTimeoutErrors(t *testing.T) {
	t.Run("http status", func(t *testing.T) {
		server := newTestServer(func(request *http.Request) (*http.Response, error) {
			return testHTTPResponse(request, http.StatusInternalServerError, nil), nil
		})
		if _, err := Run(context.Background(), testOptions(server, filepath.Join(t.TempDir(), "voie"), "0.0.1")); err == nil {
			t.Fatal("Run accepted HTTP 500")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := newTestServer(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		options := testOptions(server, filepath.Join(t.TempDir(), "voie"), "0.0.1")
		options.HTTPClient = &http.Client{Timeout: time.Millisecond, Transport: server.HTTPClient.Transport}
		if _, err := Run(context.Background(), options); err == nil {
			t.Fatal("Run accepted a timed out request")
		}
	})
}

func TestRunLeavesExecutableUntouchedWhenReplacementFails(t *testing.T) {
	archiveName := "voie_0.0.2_linux_amd64.tar.gz"
	archive := tarGz(map[string][]byte{"voie": []byte("new executable")})
	assets := map[string][]byte{archiveName: archive, "checksums.txt": []byte(checksumLine(archiveName, archive))}
	server := releaseServer(t, "v0.0.2", assets)

	target := filepath.Join(t.TempDir(), "voie")
	if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	options := testOptions(server, target, "0.0.1")
	options.ReplaceExecutable = func(string, string) (bool, error) { return false, errors.New("replace failed") }
	if _, err := Run(context.Background(), options); err == nil {
		t.Fatal("Run succeeded when executable replacement failed")
	}
	if got := readFile(t, target); got != "old executable" {
		t.Fatalf("executable changed to %q", got)
	}
}

func TestRunKeepsStagedExecutableWhenReplacementIsPending(t *testing.T) {
	archiveName := "voie_0.0.2_linux_amd64.tar.gz"
	archive := tarGz(map[string][]byte{"voie": []byte("new executable")})
	assets := map[string][]byte{archiveName: archive, "checksums.txt": []byte(checksumLine(archiveName, archive))}
	server := releaseServer(t, "v0.0.2", assets)
	target := filepath.Join(t.TempDir(), "voie")
	if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stagedPath string
	options := testOptions(server, target, "0.0.1")
	options.ReplaceExecutable = func(staged, _ string) (bool, error) {
		stagedPath = staged
		return true, nil
	}
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || !result.Pending {
		t.Fatalf("result = %+v, want pending update", result)
	}
	if _, err := os.Stat(stagedPath); err != nil {
		t.Fatalf("staged executable was removed before deferred replacement: %v", err)
	}
	if got := readFile(t, target); got != "old executable" {
		t.Fatalf("target changed before deferred replacement to %q", got)
	}
}

func releaseServer(t *testing.T, tag string, files map[string][]byte) *testServer {
	t.Helper()
	return newTestServer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/repos/benoitpetit/voie/releases/latest" {
			assets := make([]releaseAsset, 0, len(files))
			for name := range files {
				assets = append(assets, releaseAsset{Name: name, BrowserDownloadURL: "https://api.github.test/assets/" + name})
			}
			return releaseResponse(request, tag, assets), nil
		}
		name := strings.TrimPrefix(request.URL.Path, "/assets/")
		data, ok := files[name]
		if !ok {
			return testHTTPResponse(request, http.StatusNotFound, nil), nil
		}
		return testHTTPResponse(request, http.StatusOK, data), nil
	})
}

func releaseResponse(request *http.Request, tag string, assets []releaseAsset) *http.Response {
	data, _ := json.Marshal(latestRelease{TagName: tag, Assets: assets})
	return testHTTPResponse(request, http.StatusOK, data)
}

type testServer struct {
	URL        string
	HTTPClient *http.Client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func newTestServer(handler func(*http.Request) (*http.Response, error)) *testServer {
	return &testServer{
		URL:        "https://api.github.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(handler)},
	}
}

func testHTTPResponse(request *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)), Request: request,
	}
}

func testOptions(server *testServer, executable, version string) Options {
	return Options{
		Owner: "benoitpetit", Repository: "voie", CurrentVersion: version,
		GOOS: "linux", GOARCH: "amd64", ExecutablePath: executable,
		APIBaseURL: server.URL, HTTPClient: server.HTTPClient,
	}
}

func tarGz(files map[string][]byte) []byte {
	var output bytes.Buffer
	compressor := gzip.NewWriter(&output)
	writer := tar.NewWriter(compressor)
	for name, data := range files {
		_ = writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = writer.Write(data)
	}
	_ = writer.Close()
	_ = compressor.Close()
	return output.Bytes()
}

func checksumLine(name string, archive []byte) string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestExtractExecutableRejectsTraversalAndAcceptsSafeSidecar(t *testing.T) {
	unsafe := tarGz(map[string][]byte{"../voie": []byte("bad"), "voie": []byte("new")})
	if _, err := extractExecutable(unsafe, "voie_0.0.2_linux_amd64.tar.gz", "voie"); err == nil {
		t.Fatal("extractExecutable accepted a traversal path")
	}

	safe := tarGz(map[string][]byte{"README.md": []byte("notes"), "voie": []byte("new")})
	got, err := extractExecutable(safe, "voie_0.0.2_linux_amd64.tar.gz", "voie")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("executable payload = %q", got)
	}
}

func TestChecksumRequiresExactAssetNameAndSHA256(t *testing.T) {
	archive := []byte("archive")
	line := checksumLine("voie_0.0.2_linux_amd64.tar.gz", archive)
	got, err := parseChecksum(line, "voie_0.0.2_linux_amd64.tar.gz")
	if err != nil || got != fmt.Sprintf("%x", sha256.Sum256(archive)) {
		t.Fatalf("parseChecksum = %q, %v", got, err)
	}
	if _, err := parseChecksum(line, "voie_0.0.3_linux_amd64.tar.gz"); err == nil {
		t.Fatal("parseChecksum accepted a checksum for a different asset")
	}
}

func TestExtractExecutableSupportsWindowsZipAndRejectsLinks(t *testing.T) {
	safe := zipArchive(map[string][]byte{"README.md": []byte("notes"), "voie.exe": []byte("windows binary")})
	got, err := extractExecutable(safe, "voie_0.0.2_windows_amd64.zip", "voie.exe")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "windows binary" {
		t.Fatalf("executable payload = %q", got)
	}

	unsafe := zipArchive(map[string][]byte{"../voie.exe": []byte("outside"), "voie.exe": []byte("inside")})
	if _, err := extractExecutable(unsafe, "voie_0.0.2_windows_amd64.zip", "voie.exe"); err == nil {
		t.Fatal("extractExecutable accepted a ZIP traversal path")
	}

	var output bytes.Buffer
	compressor := gzip.NewWriter(&output)
	writer := tar.NewWriter(compressor)
	if err := writer.WriteHeader(&tar.Header{Name: "voie", Typeflag: tar.TypeSymlink, Linkname: "/tmp/other"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractExecutable(output.Bytes(), "voie_0.0.2_linux_amd64.tar.gz", "voie"); err == nil {
		t.Fatal("extractExecutable accepted a symlink")
	}
}

func zipArchive(files map[string][]byte) []byte {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range files {
		file, _ := writer.Create(name)
		_, _ = file.Write(data)
	}
	_ = writer.Close()
	return output.Bytes()
}
