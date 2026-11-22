// Package install provisions the external binaries that voie's local
// gateways need. Nothing is downloaded unless the user asks for a provider.
package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// githubAPIBase is overridable in tests.
var githubAPIBase = "https://api.github.com"

// Runner executes an external command. Injected in tests.
type Runner func(name string, args ...string) error

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// BinaryDir is where downloaded and npm-installed executables are stored.
func BinaryDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "voie", "bin")
}

// KiroBridgeDir is where the official-CLI ACP bridge is cloned.
func KiroBridgeDir() string {
	return kiroBridgeDir()
}

func kiroBridgeDir() string {
	if dir := os.Getenv("KIRO_GATEWAY_DIR"); dir != "" {
		return dir
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "voie", "gateways", "kiro-gateway")
}

// BinaryPath returns an already-installed binary for the provider, if any.
func BinaryPath(provider string) string {
	name := binaryName(provider)
	if name == "" {
		return ""
	}
	path := filepath.Join(BinaryDir(), name)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// Ensure returns an existing binary path, or installs the provider's toolchain.
func Ensure(ctx context.Context, provider string) (string, error) {
	if path, err := exec.LookPath(binaryName(provider)); err == nil {
		return path, nil
	}
	return Install(ctx, provider, nil)
}

func binaryName(provider string) string {
	switch provider {
	case "antigravity":
		return "CLIProxyAPI"
	case "cline":
		return "cline"
	case "kiro":
		return "kiro-cli"
	default:
		return ""
	}
}

// Install provisions the tool and returns the absolute path to use when
// spawning it. runner is optional; nil uses os/exec.
func Install(ctx context.Context, provider string, runner Runner) (string, error) {
	dir := BinaryDir()
	if dir == "" {
		return "", fmt.Errorf("cannot resolve user config directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if runner == nil {
		runner = func(name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			return cmd.Run()
		}
	}
	switch provider {
	case "antigravity":
		return installGitHubRelease(ctx, dir, "router-for-me/CLIProxyAPI", "CLIProxyAPI")
	case "cline":
		if err := runner("npm", "install", "--prefix", dir, "cline"); err != nil {
			return "", fmt.Errorf("install cline via npm: %w", err)
		}
		return filepath.Join(dir, "node_modules", ".bin", "cline"), nil
	case "kiro":
		if err := runner("npm", "install", "--prefix", dir, "kiro-cli"); err != nil {
			return "", fmt.Errorf("install kiro-cli via npm: %w", err)
		}
		bridgeDir := kiroBridgeDir()
		if _, err := os.Stat(filepath.Join(bridgeDir, "main.py")); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(bridgeDir), 0o700); err != nil {
				return "", err
			}
			if err := runner("git", "clone", "https://github.com/ankitcharolia/kiro-gateway.git", bridgeDir); err != nil {
				return "", fmt.Errorf("clone kiro ACP bridge: %w", err)
			}
			if err := runner("uv", "sync", "--directory", bridgeDir); err != nil {
				return "", fmt.Errorf("prepare kiro ACP bridge: %w", err)
			}
		}
		return filepath.Join(dir, "node_modules", ".bin", "kiro-cli"), nil
	default:
		return "", fmt.Errorf("provider %q has no automatic installer", provider)
	}
}

func installGitHubRelease(ctx context.Context, dir, repo, binary string) (string, error) {
	url := githubAPIBase + "/repos/" + repo + "/releases/latest"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub release lookup: HTTP %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	targetOS, targetArch := runtime.GOOS, runtime.GOARCH
	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)
		if !strings.Contains(name, targetOS) || !strings.Contains(name, targetArch) || !strings.HasSuffix(name, ".tar.gz") {
			continue
		}
		return downloadBinary(ctx, dir, binary, asset.BrowserDownloadURL)
	}
	return "", fmt.Errorf("no release asset for %s/%s", runtime.GOOS, runtime.GOARCH)
}

func downloadBinary(ctx context.Context, dir, binary, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg || !strings.Contains(filepath.Base(header.Name), binary) {
			continue
		}
		path := filepath.Join(dir, binary)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(file, tr); err != nil {
			file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("binary %q not found in release archive", binary)
}
