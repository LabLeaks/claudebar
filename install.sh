#!/bin/sh
# claudebar installer
# Usage: curl -sSfL https://raw.githubusercontent.com/lableaks/claudebar/master/install.sh | sh

set -e

REPO="lableaks/claudebar"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Get latest release tag
LATEST=$(curl -sSf "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "$LATEST" ]; then
  echo "Error: Could not determine latest release"
  exit 1
fi

VERSION="${LATEST#v}"
ARCHIVE="claudebar_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$LATEST/$ARCHIVE"

echo "Installing claudebar $LATEST ($OS/$ARCH)..."

# Download and extract
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -sSfL "$URL" -o "$TMP/$ARCHIVE"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"

# Install
mkdir -p "$INSTALL_DIR"
rm -f "$INSTALL_DIR/claudebar"
cp "$TMP/claudebar" "$INSTALL_DIR/claudebar"
chmod +x "$INSTALL_DIR/claudebar"

echo "Installed claudebar $LATEST to $INSTALL_DIR/claudebar"

# Check PATH
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo ""; echo "Add $INSTALL_DIR to your PATH if it's not already there:" ; echo "  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
