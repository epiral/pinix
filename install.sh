#!/bin/bash
set -e

# Pinix installer
# Usage: curl -fsSL https://dl.pinixai.com/install.sh | bash

BASE_URL="https://pinix-blobs-1251447449.cos.ap-beijing.myqcloud.com/releases/latest"
INSTALL_DIR="/usr/local/bin"

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux)  OS="linux" ;;
  darwin) OS="darwin" ;;
  *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)             echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY="pinix-${OS}-${ARCH}"
URL="${BASE_URL}/${BINARY}"

echo "Installing Pinix (${OS}/${ARCH})..."

# Download
TMPFILE=$(mktemp)
if ! curl -fsSL "$URL" -o "$TMPFILE"; then
  echo "Failed to download Pinix from $URL"
  rm -f "$TMPFILE"
  exit 1
fi

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPFILE" "$INSTALL_DIR/pinix"
  chmod +x "$INSTALL_DIR/pinix"
else
  sudo mv "$TMPFILE" "$INSTALL_DIR/pinix"
  sudo chmod +x "$INSTALL_DIR/pinix"
fi

# Check Bun
if ! command -v bun &>/dev/null; then
  echo ""
  echo "Bun is required for running Clips."
  echo "Install Bun: curl -fsSL https://bun.sh/install | bash"
  echo ""
fi

VERSION=$("$INSTALL_DIR/pinix" --version 2>&1 || echo "unknown")
echo ""
echo "Pinix installed: $VERSION"
echo ""
echo "Get started:"
echo "  pinix start                        start Pinix"
echo "  pinix login                        log in to Pinix"
echo "  pinix hub add @pinix/todo          install your first Clip"
echo "  pinix invoke todo list             use a Clip"
echo "  open http://localhost:9000          open Console"
