package update

import (
	"fmt"
	"strconv"
	"strings"
)

type version struct {
	major int
	minor int
	patch int
}

func parseVersion(raw string) (version, error) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("expected a stable X.Y.Z version")
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return version{}, fmt.Errorf("invalid semantic version %q", raw)
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return version{}, fmt.Errorf("invalid semantic version %q", raw)
		}
		values[i] = parsed
	}
	return version{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func compareVersion(left, right version) int {
	if left.major != right.major {
		if left.major < right.major {
			return -1
		}
		return 1
	}
	if left.minor != right.minor {
		if left.minor < right.minor {
			return -1
		}
		return 1
	}
	if left.patch < right.patch {
		return -1
	}
	if left.patch > right.patch {
		return 1
	}
	return 0
}

func archiveAssetName(versionText, goos, goarch string) (string, error) {
	version, err := parseVersion(versionText)
	if err != nil {
		return "", err
	}
	supported := false
	switch goos {
	case "linux", "darwin":
		supported = goarch == "amd64" || goarch == "arm64"
	case "windows":
		supported = goarch == "amd64"
	}
	if !supported {
		return "", fmt.Errorf("%w: %s/%s", ErrUnsupportedTarget, goos, goarch)
	}
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("voie_%s_%s_%s%s", version.String(), goos, goarch, extension), nil
}
