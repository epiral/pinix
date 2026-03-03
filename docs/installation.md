# Pinix Installation Guide

This guide walks you through installing, configuring, and running Pinix from scratch.

## System Requirements

| Platform | Architecture | Status |
|----------|-------------|--------|
| macOS    | Apple Silicon (ARM64) | Supported |
| Linux    | x86_64 / ARM64 | Supported |

**Additional requirements:**

- **macOS:** macOS 12 (Monterey) or later, Apple Silicon
- **Linux:** KVM enabled (`/dev/kvm` accessible). Your user must be in the `kvm` group:
  ```bash
  sudo usermod -aG kvm $USER
  # Log out and back in for the change to take effect
  ```

## 1. Install BoxLite

Pinix uses [BoxLite](https://github.com/boxlite-ai/boxlite) to run each Clip in an isolated micro-VM. You need the `boxlite` CLI binary on your machine.

### Option A: Download a Pre-built Binary

Download the latest release from [GitHub Releases](https://github.com/boxlite-ai/boxlite/releases):

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/boxlite-ai/boxlite/releases/latest/download/boxlite-runtime-aarch64-apple-darwin.tar.gz \
  | tar xz -C /usr/local/bin boxlite

# Linux (x86_64)
curl -fsSL https://github.com/boxlite-ai/boxlite/releases/latest/download/boxlite-runtime-x86_64-unknown-linux-gnu.tar.gz \
  | tar xz -C /usr/local/bin boxlite
```

### Option B: Build from Source

Requires Rust (1.88+) and platform build tools.

```bash
git clone https://github.com/boxlite-ai/boxlite.git
cd boxlite

# Install build dependencies (Homebrew on macOS, apt on Linux)
make setup

# Build the CLI
make runtime
cargo build --release -p boxlite-cli
```

The binary is at `target/release/boxlite`. Copy it to a directory in your `PATH`:

```bash
cp target/release/boxlite /usr/local/bin/
```

### Verify BoxLite

```bash
boxlite info
```

You should see output including the version number and virtualization support status.

## 2. Install Pinix

### Option A: Download a Pre-built Binary

Download the latest release from [GitHub Releases](https://github.com/epiral/pinix/releases):

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/epiral/pinix/releases/latest/download/pinix-darwin-arm64 -o /usr/local/bin/pinix
chmod +x /usr/local/bin/pinix

# Linux (x86_64)
curl -fsSL https://github.com/epiral/pinix/releases/latest/download/pinix-linux-amd64 -o /usr/local/bin/pinix
chmod +x /usr/local/bin/pinix
```

### Option B: Build from Source

Requires Go 1.25+.

```bash
git clone https://github.com/epiral/pinix.git
cd pinix
go build -o /usr/local/bin/pinix .
```

### Verify Pinix

```bash
pinix --help
```

## 3. Initialize Configuration

Pinix stores its configuration at `~/.config/pinix/config.yaml`.

```bash
mkdir -p ~/.config/pinix

# Generate a random Super Token (save it — you'll need it for admin operations)
SUPER_TOKEN=$(openssl rand -hex 32)

cat > ~/.config/pinix/config.yaml << EOF
super_token: "${SUPER_TOKEN}"
clips: []
tokens: []
EOF

chmod 600 ~/.config/pinix/config.yaml

echo "Your Super Token: ${SUPER_TOKEN}"
```

> **Keep your Super Token safe.** It grants full admin access to the Pinix server (creating/deleting Clips, managing tokens). It cannot be regenerated through the API — you must edit the config file directly to change it.

### Configuration Fields

| Field | Description |
|-------|-------------|
| `super_token` | 64-character hex string. Grants admin access to all server operations. |
| `clips` | List of registered Clips. Managed automatically by `pinix clip create/delete`. |
| `tokens` | List of Clip-scoped access tokens. Managed automatically by `pinix token generate/revoke`. |

Each Clip entry supports the following fields:

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Auto-generated | Unique identifier (auto-assigned on creation) |
| `name` | Yes | Human-readable name |
| `workdir` | Yes | Host path to the Clip directory (mounted as `/clip` in the VM) |
| `mounts` | No | Additional host-to-VM bind mounts (`source`, `target`, `read_only`) |
| `image` | No | OCI container image override (default: `debian:12-slim`) |

## 4. Start the Server

### Manual Start

```bash
pinix serve --addr :9875
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `:8080` | Listen address (`host:port`) |
| `--boxlite` | (auto-detect on PATH) | Path to the `boxlite` binary |

If `boxlite` is not in your `PATH`, specify it explicitly:

```bash
pinix serve --addr :9875 --boxlite /path/to/boxlite
```

You should see:

```
[sandbox] backend: boxlite
pinix listening on :9875
```

### macOS: Auto-start with launchd

Create a Launch Agent so Pinix starts automatically on login:

```bash
cat > ~/Library/LaunchAgents/ai.yan5xu.pinix.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>ai.yan5xu.pinix</string>

    <key>ProgramArguments</key>
    <array>
        <string>$(which pinix)</string>
        <string>serve</string>
        <string>--addr</string>
        <string>:9875</string>
    </array>

    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>$HOME</string>
    </dict>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>StandardOutPath</key>
    <string>/tmp/pinix.log</string>

    <key>StandardErrorPath</key>
    <string>/tmp/pinix.error.log</string>
</dict>
</plist>
EOF
```

> **Note:** Replace the paths in `ProgramArguments` and `HOME` with your actual absolute paths. launchd does not expand `~` or `$HOME`.

Load and start:

```bash
launchctl load ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
```

Stop and unload:

```bash
launchctl unload ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
```

### Linux: Auto-start with systemd

Create a user-level systemd service:

```bash
mkdir -p ~/.config/systemd/user

cat > ~/.config/systemd/user/pinix.service << EOF
[Unit]
Description=Pinix Server
After=network.target

[Service]
ExecStart=/usr/local/bin/pinix serve --addr :9875
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
EOF
```

Enable and start:

```bash
systemctl --user daemon-reload
systemctl --user enable --now pinix
```

Check status:

```bash
systemctl --user status pinix
journalctl --user -u pinix -f
```

## 5. Quick Start: Register a Clip and Run a Command

With the server running, let's register your first Clip and verify everything works.

### 5.1 Create a Clip Directory

A Clip is a directory containing executable scripts in a `commands/` subdirectory:

```bash
mkdir -p ~/my-first-clip/commands

cat > ~/my-first-clip/commands/hello << 'SCRIPT'
#!/bin/sh
echo "Hello from Pinix!"
echo "Arguments: $@"
SCRIPT

chmod +x ~/my-first-clip/commands/hello
```

### 5.2 Register the Clip

```bash
export SERVER="http://localhost:9875"
export SUPER_TOKEN="<your-super-token>"

pinix clip create \
  --name my-first-clip \
  --workdir ~/my-first-clip \
  --server $SERVER \
  --token $SUPER_TOKEN
```

Output:

```
clip_id: <generated-clip-id>
```

Save the `clip_id` for the next step.

### 5.3 Generate a Clip Token

Clip Tokens are scoped credentials that allow executing commands on a specific Clip:

```bash
pinix token generate \
  --clip <clip-id> \
  --label "my-client" \
  --server $SERVER \
  --token $SUPER_TOKEN
```

Output:

```
id:    t_xxxxxxxxxxxx
token: <64-char-hex-clip-token>
```

Save the `token` value.

### 5.4 Invoke a Command

```bash
export CLIP_TOKEN="<clip-token-from-above>"

pinix invoke hello world \
  --server $SERVER \
  --token $CLIP_TOKEN
```

Expected output:

```
Hello from Pinix!
Arguments: world
```

### 5.5 Check Clip Info

```bash
pinix info --server $SERVER --token $CLIP_TOKEN
```

Output:

```
name:        my-first-clip
description:
has_web:     false
commands:
  - hello
```

### 5.6 List All Clips

```bash
pinix clip list --server $SERVER --token $SUPER_TOKEN
```

## FAQ

### BoxLite says "binary not found"

Make sure the `boxlite` binary is in your `PATH`, or pass it explicitly with `--boxlite`:

```bash
pinix serve --boxlite /path/to/boxlite
```

### "KVM not available" on Linux

Enable KVM kernel modules and ensure your user has access:

```bash
sudo modprobe kvm kvm_intel   # or kvm_amd for AMD CPUs
sudo usermod -aG kvm $USER
# Log out and back in, then verify:
ls -l /dev/kvm
```

### Server starts but Clip invocations fail

- Verify BoxLite is working: `boxlite info`
- Check that the Clip's `workdir` path exists and the `commands/` scripts are executable
- Check server logs for errors:
  - Manual: visible in terminal
  - launchd: `tail -f /tmp/pinix.error.log`
  - systemd: `journalctl --user -u pinix -f`

### How do I change the Super Token?

Edit `~/.config/pinix/config.yaml` directly and restart the server. Generate a new token with:

```bash
openssl rand -hex 32
```

### What OCI images can I use for Clips?

By default, Clips run in `debian:12-slim`. You can override this per-Clip by adding an `image` field to the Clip entry in `config.yaml`:

```yaml
clips:
  - id: abc123
    name: my-python-clip
    workdir: /path/to/clip
    image: python:3.13-slim
```

BoxLite will pull the image automatically on first use.

### How do I add extra mounts to a Clip?

Add a `mounts` field to the Clip entry in `config.yaml`:

```yaml
clips:
  - id: abc123
    name: my-clip
    workdir: /path/to/clip
    mounts:
      - source: /host/data
        target: /data
        read_only: true
```

The Clip's `workdir` is always mounted at `/clip` automatically.
