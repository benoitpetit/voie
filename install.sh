#!/bin/sh
set -eu

REPOSITORY="benoitpetit/voie"
VERSION="latest"
INSTALL_DIR="${HOME:-}/.local/bin"
BASE_URL="https://github.com/${REPOSITORY}/releases"

usage() {
  cat <<'EOF'
Install voie from a GitHub release.

Usage: install.sh [--version VERSION] [--install-dir DIR] [--help]

Options:
  --version VERSION  Install a specific release (for example 0.0.3); default: latest
  --install-dir DIR  Installation directory; default: ~/.local/bin
  --help             Show this help

The installer verifies the release archive with checksums.txt. It does not edit
your shell configuration or PATH.
EOF
}

fail() {
  printf 'install.sh: %s\n' "$1" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a value"
      VERSION=$2
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || fail "--install-dir requires a directory"
      INSTALL_DIR=$2
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *) fail "unknown option: $1 (use --help)" ;;
  esac
done

[ -n "$INSTALL_DIR" ] || fail "home directory is unavailable; pass --install-dir"
case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) fail "unsupported operating system: $(uname -s) (supported: Linux, macOS)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) fail "unsupported architecture: $(uname -m) (supported: amd64, arm64)" ;;
esac
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
if command -v sha256sum >/dev/null 2>&1; then
  HASH_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  HASH_TOOL=shasum
else
  fail "sha256sum or shasum is required"
fi

case "$VERSION" in
  latest)
    TAG=latest
    CHECKSUM_URL="$BASE_URL/latest/download/checksums.txt"
    ;;
  *)
    VERSION=${VERSION#v}
    case "$VERSION" in
      *[!0-9.]*|''|.*|*.|*..*) fail "invalid version: $VERSION (expected x.y.z)" ;;
    esac
    old_ifs=$IFS
    IFS=.
    set -- $VERSION
    IFS=$old_ifs
    [ "$#" -eq 3 ] || fail "invalid version: $VERSION (expected x.y.z)"
    for component do
      case "$component" in ''|*[!0-9]*) fail "invalid version: $VERSION (expected x.y.z)" ;; esac
    done
    TAG="v$VERSION"
    CHECKSUM_URL="$BASE_URL/download/$TAG/checksums.txt"
    ;;
esac

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/voie-install.XXXXXX") || fail "could not create temporary directory"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT HUP INT TERM

curl -fsSL "$CHECKSUM_URL" -o "$TMP_DIR/checksums.txt" || fail "could not download release checksums"
if [ "$VERSION" = latest ]; then
  ARCHIVE_NAME=$(awk -v os="$OS" -v arch="$ARCH" '$2 ~ ("^voie_[0-9]+\\.[0-9]+\\.[0-9]+_" os "_" arch "\\.tar\\.gz$") { print $2 }' "$TMP_DIR/checksums.txt")
  [ -n "$ARCHIVE_NAME" ] || fail "no release archive for $OS/$ARCH was found"
  [ "$(printf '%s\n' "$ARCHIVE_NAME" | wc -l | tr -d ' ')" -eq 1 ] || fail "multiple release archives matched $OS/$ARCH"
  VERSION=$(printf '%s\n' "$ARCHIVE_NAME" | sed -E 's/^voie_([0-9]+\.[0-9]+\.[0-9]+)_.*/\1/')
fi
ARCHIVE_NAME="voie_${VERSION}_${OS}_${ARCH}.tar.gz"
EXPECTED=$(awk -v name="$ARCHIVE_NAME" '$2 == name { print $1 }' "$TMP_DIR/checksums.txt")
[ -n "$EXPECTED" ] || fail "checksum entry for $ARCHIVE_NAME was not found"
[ "$(printf '%s\n' "$EXPECTED" | wc -l | tr -d ' ')" -eq 1 ] || fail "multiple checksum entries found for $ARCHIVE_NAME"
curl -fsSL "$BASE_URL/download/v$VERSION/$ARCHIVE_NAME" -o "$TMP_DIR/$ARCHIVE_NAME" || fail "could not download $ARCHIVE_NAME"

if [ "$HASH_TOOL" = sha256sum ]; then
  ACTUAL=$(sha256sum "$TMP_DIR/$ARCHIVE_NAME" | awk '{print $1}')
else
  ACTUAL=$(shasum -a 256 "$TMP_DIR/$ARCHIVE_NAME" | awk '{print $1}')
fi
[ "$ACTUAL" = "$EXPECTED" ] || fail "SHA-256 checksum mismatch for $ARCHIVE_NAME"

mkdir -p "$TMP_DIR/extracted" || fail "could not create extraction directory"
tar -xzf "$TMP_DIR/$ARCHIVE_NAME" -C "$TMP_DIR/extracted" voie || fail "could not extract voie from the release archive"
[ -f "$TMP_DIR/extracted/voie" ] && [ ! -L "$TMP_DIR/extracted/voie" ] || fail "archive does not contain a regular voie executable"
chmod 755 "$TMP_DIR/extracted/voie" || fail "could not set executable permissions"
mkdir -p "$INSTALL_DIR" || fail "could not create installation directory: $INSTALL_DIR"
STAGED="$INSTALL_DIR/.voie.$$"
trap 'rm -f "$STAGED"; cleanup' EXIT HUP INT TERM
cp "$TMP_DIR/extracted/voie" "$STAGED" || fail "could not stage voie in $INSTALL_DIR"
chmod 755 "$STAGED" || fail "could not set installed executable permissions"
mv -f "$STAGED" "$INSTALL_DIR/voie" || fail "could not install voie to $INSTALL_DIR/voie"
printf 'Installed voie %s at %s/voie\n' "$VERSION" "$INSTALL_DIR"
