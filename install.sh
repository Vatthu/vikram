#!/bin/sh
# Vikram Installer
# Usage: curl -sSL https://raw.githubusercontent.com/Vatthu/vikram/main/install.sh | sh
#
# This script downloads the latest Vikram binary for your OS/architecture
# and installs it to /usr/local/bin (or ~/.local/bin if no write access).

set -e

REPO="Vatthu/vikram"
BINARY="vikram"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l|armhf)  ARCH="armv7" ;;
    *)             echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release tag
echo "Finding latest Vikram release..."
LATEST=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)

if [ -z "$LATEST" ]; then
    echo "Error: Could not determine latest release. Check https://github.com/${REPO}/releases"
    exit 1
fi

echo "Latest version: ${LATEST}"

# Build download URL (matches GoReleaser naming convention)
ARCHIVE="${BINARY}_${LATEST#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${ARCHIVE}"

# Download
TMPDIR=$(mktemp -d)
echo "Downloading ${URL}..."
curl -sSL -o "${TMPDIR}/${ARCHIVE}" "$URL"

# Extract
cd "$TMPDIR"
tar xzf "$ARCHIVE"

# Install
if [ -w "$INSTALL_DIR" ]; then
    mv "$BINARY" "${INSTALL_DIR}/${BINARY}"
else
    # Fall back to user-local install
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "$INSTALL_DIR"
    mv "$BINARY" "${INSTALL_DIR}/${BINARY}"
    echo ""
    echo "Installed to ${INSTALL_DIR}/${BINARY}"
    echo "Make sure ${INSTALL_DIR} is in your PATH:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

# Cleanup
rm -rf "$TMPDIR"

echo ""
echo "✅ Vikram ${LATEST} installed successfully!"
echo ""
echo "Get started:"
echo "  vikram onboard    # First-time setup (2 minutes)"
echo "  vikram doctor     # Verify everything works"
echo "  vikram agent      # Start chatting"
