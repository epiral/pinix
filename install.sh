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

# Install Bun (required for running Clips)
if ! command -v bun &>/dev/null; then
  echo ""
  echo "Installing Bun (required for Clips)..."
  if command -v unzip &>/dev/null; then
    curl -fsSL https://bun.sh/install | bash 2>/dev/null
    # Source bun into current PATH for immediate use
    export BUN_INSTALL="$HOME/.bun"
    export PATH="$BUN_INSTALL/bin:$PATH"
    if command -v bun &>/dev/null; then
      echo "Bun installed: $(bun --version)"
    else
      echo "Warning: Bun install completed but not found in PATH."
      echo "  Run: source ~/.bashrc (or ~/.zshrc)"
    fi
  else
    echo "Warning: unzip is required to install Bun."
    echo "  Install unzip first, then run: curl -fsSL https://bun.sh/install | bash"
  fi
fi

# Install bb-browser (browser Clip — provides browser automation + stream)
echo ""
echo "Installing bb-browser..."
if command -v bun &>/dev/null; then
  bun install -g bb-browser 2>/dev/null && echo "bb-browser installed (via bun)" || echo "Warning: bb-browser install failed"
elif command -v npm &>/dev/null; then
  npm install -g bb-browser 2>/dev/null && echo "bb-browser installed (via npm)" || echo "Warning: bb-browser install failed"
else
  echo "Warning: neither bun nor npm found, skipping bb-browser install."
  echo "  Install manually: npm install -g bb-browser"
fi

# Linux: install Xvfb (required for headed Chrome in headless environments)
if [ "$OS" = "linux" ]; then
  if ! command -v Xvfb &>/dev/null; then
    echo ""
    echo "Installing Xvfb (required for browser on Linux)..."
    if command -v apt-get &>/dev/null; then
      sudo apt-get update -qq && sudo apt-get install -y -qq xvfb 2>/dev/null && echo "Xvfb installed" || echo "Warning: Xvfb install failed"
    elif command -v yum &>/dev/null; then
      sudo yum install -y xorg-x11-server-Xvfb 2>/dev/null && echo "Xvfb installed" || echo "Warning: Xvfb install failed"
    else
      echo "Warning: install Xvfb manually (apt: xvfb, yum: xorg-x11-server-Xvfb)"
    fi
  fi
fi

echo ""
echo "Pinix installed successfully!"
echo ""
echo "Get started:"
echo "  pinix start                        start Pinix"
echo "  pinix login                        log in to Pinix"
echo "  pinix hub add @pinix/todo          install your first Clip"
echo "  pinix invoke todo list             use a Clip"
echo "  open http://localhost:9000          open Console"
