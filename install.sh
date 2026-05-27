#!/bin/bash
# Pinix installer
# Usage: curl -fsSL https://dl.pinixai.com/install.sh | bash
#
# Do NOT use set -e — sub-installers (fnm, bun) can return non-zero
# from pipe operations even when they succeed.

BASE_URL="https://dl.pinixai.com/releases/latest"

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

# Choose install dir: /usr/local/bin if writable or sudo available, else ~/.local/bin
INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ]; then
  if command -v sudo &>/dev/null && sudo -n true 2>/dev/null; then
    USE_SUDO=1
  else
    # No sudo — install to user directory
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
    USE_SUDO=0
    # Ensure ~/.local/bin is in PATH
    case ":$PATH:" in
      *":$INSTALL_DIR:"*) ;;
      *)
        export PATH="$INSTALL_DIR:$PATH"
        # Add to shell profile if not already there
        for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
          if [ -f "$rc" ] && ! grep -q "$INSTALL_DIR" "$rc" 2>/dev/null; then
            echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$rc"
          fi
        done
        ;;
    esac
  fi
else
  USE_SUDO=0
fi

BINARY="pinix-${OS}-${ARCH}"
URL="${BASE_URL}/${BINARY}"

echo "Installing Pinix (${OS}/${ARCH}) to ${INSTALL_DIR}..."

# Download pinix binary
TMPFILE=$(mktemp)
if ! curl -fsSL "$URL" -o "$TMPFILE"; then
  echo "Failed to download Pinix from $URL"
  rm -f "$TMPFILE"
  exit 1
fi

# Install pinix + pinixd
install_bin() {
  local src="$1" dst="$2"
  if [ "$USE_SUDO" = "1" ]; then
    sudo mv "$src" "$dst" && sudo chmod +x "$dst"
  else
    mv "$src" "$dst" && chmod +x "$dst"
  fi
}

install_bin "$TMPFILE" "$INSTALL_DIR/pinix"

# Also install pinixd if available
PINIXD_URL="${BASE_URL}/pinixd-${OS}-${ARCH}"
TMPFILE2=$(mktemp)
if curl -fsSL "$PINIXD_URL" -o "$TMPFILE2" 2>/dev/null; then
  install_bin "$TMPFILE2" "$INSTALL_DIR/pinixd"
else
  rm -f "$TMPFILE2"
fi

VERSION=$("$INSTALL_DIR/pinix" --version 2>&1 || echo "unknown")
echo "Pinix installed: $VERSION"

# --- Dependencies ---

# Install Node.js (required for bb-browser daemon)
# Uses fnm (fast node manager) — no brew/sudo needed, works on macOS + Linux.
if ! command -v node &>/dev/null; then
  echo ""
  echo "Installing Node.js..."
  if ! command -v fnm &>/dev/null; then
    curl -fsSL https://fnm.vercel.app/install | bash 2>/dev/null
  fi
  FNM_PATH="$HOME/.local/share/fnm"
  export PATH="$FNM_PATH:$PATH"
  eval "$(fnm env --shell bash 2>/dev/null)" 2>/dev/null || true
  if command -v fnm &>/dev/null; then
    fnm install --lts 2>/dev/null
    eval "$(fnm env --shell bash 2>/dev/null)" 2>/dev/null || true
  fi
  if command -v node &>/dev/null; then
    echo "Node.js $(node --version) installed"
  else
    echo "Warning: Node.js installation may require a new shell."
    echo "  Run: source ~/.bashrc (or ~/.zshrc)"
  fi
fi

# Install Bun (required for running Clips)
if ! command -v bun &>/dev/null; then
  echo ""
  echo "Installing Bun (required for Clips)..."
  if command -v unzip &>/dev/null; then
    curl -fsSL https://bun.sh/install | bash 2>/dev/null
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

# Ensure bun is discoverable by pinixd at runtime.
# pinixd searches PATH and ~/.bun/bin/bun. If bun was installed by the
# official installer it's already at ~/.bun/bin/bun. If installed via
# brew/fnm, create a symlink so pinixd can always find it.
if command -v bun &>/dev/null; then
  BUN_BIN=$(command -v bun)
  BUN_HOME="$HOME/.bun/bin"
  if [ ! -x "$BUN_HOME/bun" ] && [ -x "$BUN_BIN" ]; then
    mkdir -p "$BUN_HOME"
    ln -sf "$BUN_BIN" "$BUN_HOME/bun"
  fi
fi

# Install bb-browser (browser Clip — provides browser automation + stream)
echo ""
echo "Installing bb-browser..."
if command -v npm &>/dev/null; then
  npm install -g bb-browser 2>/dev/null && echo "bb-browser installed" || echo "Warning: bb-browser install failed"
else
  echo "Warning: npm not found, skipping bb-browser install."
  echo "  Install manually: npm install -g bb-browser"
fi

# Clean stale bb-viewer binary so daemon downloads the latest static build
BB_VIEWER_PATH="$HOME/.bb-browser/bin/bb-viewer"
if [ -f "$BB_VIEWER_PATH" ]; then
  rm -f "$BB_VIEWER_PATH"
  echo "Cleared cached bb-viewer (will re-download latest on first stream)"
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
