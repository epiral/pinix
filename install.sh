#!/bin/bash
set -e

# Pinix installer
# Usage: curl -fsSL https://dl.pinixai.com/install.sh | bash

BASE_URL="https://dl.pinixai.com/releases/latest"
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

# Download pinix binary
TMPFILE=$(mktemp)
if ! curl -fsSL "$URL" -o "$TMPFILE"; then
  echo "Failed to download Pinix from $URL"
  rm -f "$TMPFILE"
  exit 1
fi

# Install pinix
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPFILE" "$INSTALL_DIR/pinix"
  chmod +x "$INSTALL_DIR/pinix"
else
  sudo mv "$TMPFILE" "$INSTALL_DIR/pinix"
  sudo chmod +x "$INSTALL_DIR/pinix"
fi

VERSION=$("$INSTALL_DIR/pinix" --version 2>&1 || echo "unknown")
echo "Pinix installed: $VERSION"

# --- Dependencies ---

# Check Node.js (required for bb-browser)
if ! command -v node &>/dev/null; then
  echo ""
  echo "Node.js is required for browser capabilities."
  echo "Install Node.js: https://nodejs.org"
  echo ""
fi

# Check Bun (required for Clips)
if ! command -v bun &>/dev/null; then
  echo ""
  echo "Bun is required for running Clips."
  echo "Install Bun: curl -fsSL https://bun.sh/install | bash"
  echo ""
fi

# Install bb-browser (browser automation for agents)
if command -v npm &>/dev/null; then
  echo ""
  echo "Installing bb-browser (browser capabilities for agents)..."
  if npm install -g bb-browser@latest --prefer-online 2>/dev/null; then
    BB_VERSION=$(bb-browser --version 2>/dev/null || echo "unknown")
    echo "bb-browser installed: $BB_VERSION"
  else
    echo "Warning: bb-browser install failed. You can install it later:"
    echo "  npm install -g bb-browser"
  fi
else
  echo ""
  echo "npm not found — skipping bb-browser install."
  echo "To add browser capabilities later:"
  echo "  npm install -g bb-browser"
fi

echo ""
echo "Get started:"
echo "  pinix start                        start Pinix"
echo "  pinix login                        log in to Pinix"
echo "  pinix hub add @pinix/todo          install your first Clip"
echo "  pinix invoke todo list             use a Clip"
echo "  open http://localhost:9000          open Console"
