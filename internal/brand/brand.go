// Package brand contains the user-facing identity shared by the CLI and transports.
package brand

const (
	Name = "voie"
)

// Version is replaced with the release version in tagged builds.
var Version = "dev"

// Banner returns the VOIE wordmark for terminal help and logs.
func Banner() string {
	return `▄ ▄  ▄  ▄ ▄▄
█ █ █ █ ▄ █■
▀■▀  ▀  ▀ ▀▀`
}
