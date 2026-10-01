package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultAPIBaseURL = "https://api.github.com"
	requestTimeout    = 30
	maxReleaseJSON    = 2 << 20
	maxChecksums      = 1 << 20
	maxArchiveSize    = 128 << 20
	maxExecutableSize = 128 << 20
)

var (
	ErrDevelopmentBuild  = errors.New("cannot update a development build; install a tagged release first")
	ErrUnsupportedTarget = errors.New("unsupported operating system or architecture")
)

type Options struct {
	Owner             string
	Repository        string
	CurrentVersion    string
	GOOS              string
	GOARCH            string
	ExecutablePath    string
	APIBaseURL        string
	HTTPClient        *http.Client
	ReplaceExecutable func(stagedPath, executablePath string) (pending bool, err error)
}

type Result struct {
	CurrentVersion string
	LatestVersion  string
	AlreadyCurrent bool
	Updated        bool
	Pending        bool
}

func Run(ctx context.Context, options Options) (Result, error) {
	result := Result{CurrentVersion: options.CurrentVersion}
	if strings.TrimSpace(options.CurrentVersion) == "" || strings.EqualFold(options.CurrentVersion, "dev") {
		return result, ErrDevelopmentBuild
	}
	current, err := parseVersion(options.CurrentVersion)
	if err != nil {
		return result, fmt.Errorf("parse current version: %w", err)
	}
	if options.Owner == "" || options.Repository == "" {
		return result, errors.New("GitHub repository owner and name are required")
	}
	if _, err := archiveAssetName(options.CurrentVersion, options.GOOS, options.GOARCH); err != nil {
		return result, err
	}

	apiBase, err := parseAPIBaseURL(options.APIBaseURL)
	if err != nil {
		return result, err
	}
	client := boundedClient(options.HTTPClient, apiBase)
	latest, err := fetchLatestRelease(ctx, client, apiBase, options.Owner, options.Repository)
	if err != nil {
		return result, err
	}
	latestVersion, err := parseVersion(latest.TagName)
	if err != nil {
		return result, fmt.Errorf("latest GitHub release has invalid tag %q: %w", latest.TagName, err)
	}
	result.LatestVersion = latestVersion.String()
	if compareVersion(latestVersion, current) <= 0 {
		result.AlreadyCurrent = true
		return result, nil
	}
	archiveName, err := archiveAssetName(result.LatestVersion, options.GOOS, options.GOARCH)
	if err != nil {
		return result, err
	}
	assets := make(map[string]releaseAsset, len(latest.Assets))
	for _, asset := range latest.Assets {
		if _, exists := assets[asset.Name]; exists {
			return result, fmt.Errorf("latest GitHub release contains duplicate asset %q", asset.Name)
		}
		assets[asset.Name] = asset
	}
	archiveAsset, ok := assets[archiveName]
	if !ok {
		return result, fmt.Errorf("latest release is missing supported asset %q", archiveName)
	}
	checksumAsset, ok := assets["checksums.txt"]
	if !ok {
		return result, errors.New("latest release is missing checksums.txt")
	}
	checksumURL, err := validateAssetURL(checksumAsset.BrowserDownloadURL, apiBase)
	if err != nil {
		return result, fmt.Errorf("invalid checksum download URL: %w", err)
	}
	checksumBody, err := getBytes(ctx, client, checksumURL.String(), maxChecksums)
	if err != nil {
		return result, fmt.Errorf("download checksums.txt: %w", err)
	}
	expectedChecksum, err := parseChecksum(string(checksumBody), archiveName)
	if err != nil {
		return result, err
	}
	archiveURL, err := validateAssetURL(archiveAsset.BrowserDownloadURL, apiBase)
	if err != nil {
		return result, fmt.Errorf("invalid archive download URL: %w", err)
	}
	archiveBody, err := getBytes(ctx, client, archiveURL.String(), maxArchiveSize)
	if err != nil {
		return result, fmt.Errorf("download %s: %w", archiveName, err)
	}
	actualChecksum := sha256.Sum256(archiveBody)
	if !strings.EqualFold(expectedChecksum, hex.EncodeToString(actualChecksum[:])) {
		return result, fmt.Errorf("SHA-256 checksum mismatch for %s", archiveName)
	}
	binaryName := "voie"
	if options.GOOS == "windows" {
		binaryName = "voie.exe"
	}
	executable, err := extractExecutable(archiveBody, archiveName, binaryName)
	if err != nil {
		return result, fmt.Errorf("extract %s: %w", archiveName, err)
	}
	target := options.ExecutablePath
	if target == "" {
		target, err = os.Executable()
		if err != nil {
			return result, fmt.Errorf("locate current executable: %w", err)
		}
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return result, fmt.Errorf("resolve executable path: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		target = resolved
	}
	info, err := os.Stat(target)
	if err != nil {
		return result, fmt.Errorf("inspect current executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("current executable path is not a regular file")
	}
	staged, err := stageExecutable(filepath.Dir(target), executable, info.Mode().Perm())
	if err != nil {
		return result, err
	}
	keepStaged := false
	defer func() {
		if !keepStaged {
			_ = os.Remove(staged)
		}
	}()
	replace := options.ReplaceExecutable
	if replace == nil {
		replace = replaceExecutable
	}
	pending, err := replace(staged, target)
	if err != nil {
		return result, fmt.Errorf("replace current executable: %w", err)
	}
	keepStaged = pending
	result.Updated = true
	result.Pending = pending
	return result, nil
}

func stageExecutable(directory string, contents []byte, mode os.FileMode) (string, error) {
	if mode == 0 {
		mode = 0o755
	}
	file, err := os.CreateTemp(directory, ".voie-update-*")
	if err != nil {
		return "", fmt.Errorf("create staged executable: %w", err)
	}
	path := file.Name()
	cleanup := func(err error) (string, error) {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		return cleanup(fmt.Errorf("set staged executable permissions: %w", err))
	}
	if _, err := file.Write(contents); err != nil {
		return cleanup(fmt.Errorf("write staged executable: %w", err))
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync staged executable: %w", err))
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close staged executable: %w", err)
	}
	return path, nil
}

func parseChecksum(contents, name string) (string, error) {
	var found string
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", fmt.Errorf("invalid SHA-256 entry for %s", name)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("invalid SHA-256 entry for %s: %w", name, err)
		}
		if found != "" {
			return "", fmt.Errorf("checksums.txt contains duplicate entries for %s", name)
		}
		found = fields[0]
	}
	if found == "" {
		return "", fmt.Errorf("checksums.txt has no SHA-256 entry for %s", name)
	}
	return found, nil
}

func parseAPIBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		raw = defaultAPIBaseURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("invalid GitHub API base URL %q", raw)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("GitHub API URL must use HTTPS")
	}
	return parsed, nil
}

func validateAssetURL(raw string, apiBase *url.URL) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("asset URL is invalid")
	}
	if parsed.Scheme == "https" {
		return parsed, nil
	}
	if parsed.Scheme == apiBase.Scheme && parsed.Host == apiBase.Host && apiBase.Scheme == "http" && isLoopbackHost(apiBase.Hostname()) {
		return parsed, nil
	}
	return nil, errors.New("asset URL must use HTTPS")
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1"
}

func boundedClient(client *http.Client, apiBase *url.URL) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	previousCheck := copy.CheckRedirect
	copy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != "https" && !(request.URL.Scheme == apiBase.Scheme && request.URL.Host == apiBase.Host && apiBase.Scheme == "http" && isLoopbackHost(apiBase.Hostname())) {
			return errors.New("GitHub download redirect must use HTTPS")
		}
		if len(via) >= 10 {
			return errors.New("too many GitHub download redirects")
		}
		if previousCheck != nil {
			return previousCheck(request, via)
		}
		return nil
	}
	return &copy
}
