# Install

Each installer downloads the latest release, verifies `checksums.txt`, and
places a single executable. None of them change `PATH` or shell configuration.

## Linux and macOS

amd64 and arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/benoitpetit/voie/main/install.sh | sh
```

The binary is installed to `~/.local/bin/voie`. To pin a version or choose
another directory:

```sh
curl -fsSLO https://raw.githubusercontent.com/benoitpetit/voie/main/install.sh
sh install.sh --version 0.1.5 --install-dir /your/bin
```

## Windows

amd64, PowerShell:

```powershell
irm https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 | iex
```

This installs `%LOCALAPPDATA%\Programs\voie\voie.exe`. For an explicit version
or directory:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/benoitpetit/voie/main/install.ps1 -OutFile install.ps1
./install.ps1 -Version 0.1.5 -InstallDir C:\Tools\voie
```

## npm

Requires Node.js 18 or newer. The `voie-ai` package fetches the matching release
binary for the current platform.

```sh
npm install -g voie-ai
voie models --free
```

Without a global install:

```sh
npx voie-ai models --free
npx voie-ai serve
```

## Build from source

Go 1.25.5 or newer:

```sh
git clone https://github.com/benoitpetit/voie.git
cd voie
go build -o voie .
./voie version
```

Run the checks before sending a change:

```sh
go test ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
```

## Releases

Releases are published when a version tag is pushed. From the commit to
release:

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

The GitHub Actions workflow builds the archives, writes `checksums.txt`, and
publishes the GitHub Release. Archives are named
`voie_VERSION_OS_ARCH.tar.gz` for Linux and macOS and
`voie_VERSION_windows_amd64.zip` for Windows.

The binary embeds its version. `voie update` checks the latest stable release,
verifies its checksum, and replaces the current executable; it refuses
development builds. On Windows, replacement completes after the process exits.

## Manual download

Prebuilt archives are on the
[releases page](https://github.com/benoitpetit/voie/releases).
