# Pinix 安装指南

本指南将引导您从零开始安装、配置并运行 Pinix。

## 系统要求

| 平台 | 架构 | 状态 |
|----------|-------------|--------|
| macOS    | Apple Silicon (ARM64) | Supported |
| Linux    | x86_64 / ARM64 | Supported |

**其他要求：**

- **macOS:** macOS 12 (Monterey) 或更高版本，Apple Silicon 芯片
- **Linux:** 已启用 KVM (可访问 `/dev/kvm`)。您的用户必须在 `kvm` 用户组中：
  ```bash
  sudo usermod -aG kvm $USER
  # 登出并重新登录以使更改生效
  ```

## 1. 安装 BoxLite

Pinix 使用 [BoxLite](https://github.com/boxlite-ai/boxlite) 在隔离的 micro-VM 中运行每个 Clip。您需要在机器上安装 `boxlite` CLI 二进制文件。

### 选项 A：下载预编译二进制文件

从 [GitHub Releases](https://github.com/boxlite-ai/boxlite/releases) 下载最新版本：

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/boxlite-ai/boxlite/releases/latest/download/boxlite-runtime-aarch64-apple-darwin.tar.gz \
  | tar xz -C /usr/local/bin boxlite

# Linux (x86_64)
curl -fsSL https://github.com/boxlite-ai/boxlite/releases/latest/download/boxlite-runtime-x86_64-unknown-linux-gnu.tar.gz \
  | tar xz -C /usr/local/bin boxlite
```

### 选项 B：从源码构建

需要 Rust (1.88+) 和平台编译工具。

```bash
git clone https://github.com/boxlite-ai/boxlite.git
cd boxlite

# 安装构建依赖 (macOS 使用 Homebrew, Linux 使用 apt)
make setup

# 构建 CLI
make runtime
cargo build --release -p boxlite-cli
```

二进制文件位于 `target/release/boxlite`。将其复制到 `PATH` 路径下的目录：

```bash
cp target/release/boxlite /usr/local/bin/
```

### 验证 BoxLite

```bash
boxlite info
```

您应该能看到包含版本号和虚拟化支持状态的输出。

## 2. 安装 Pinix

### 选项 A：下载预编译二进制文件

从 [GitHub Releases](https://github.com/epiral/pinix/releases) 下载最新版本：

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/epiral/pinix/releases/latest/download/pinix-darwin-arm64 -o /usr/local/bin/pinix
chmod +x /usr/local/bin/pinix

# Linux (x86_64)
curl -fsSL https://github.com/epiral/pinix/releases/latest/download/pinix-linux-amd64 -o /usr/local/bin/pinix
chmod +x /usr/local/bin/pinix
```

### 选项 B：从源码构建

需要 Go 1.25+。

```bash
git clone https://github.com/epiral/pinix.git
cd pinix
go build -o /usr/local/bin/pinix .
```

### 验证 Pinix

```bash
pinix --help
```

## 3. 初始化配置

Pinix 的配置文件存储在 `~/.config/pinix/config.yaml`。

```bash
mkdir -p ~/.config/pinix

# 生成随机的 Super Token (请保存好它 —— 您在进行管理操作时会需要它)
SUPER_TOKEN=$(openssl rand -hex 32)

cat > ~/.config/pinix/config.yaml << EOF
super_token: "${SUPER_TOKEN}"
clips: []
tokens: []
EOF

chmod 600 ~/.config/pinix/config.yaml

echo "Your Super Token: ${SUPER_TOKEN}"
```

> **请妥善保管您的 Super Token。** 它拥有 Pinix 服务器的完整管理权限（创建/删除 Clip，管理 Token）。它无法通过 API 重新生成 —— 如果需要更改，您必须直接编辑配置文件。

### 配置字段

| 字段 | 描述 |
|-------|-------------|
| `super_token` | 64 位十六进制字符串。授予对所有服务器操作的管理权限。 |
| `clips` | 已注册 Clip 的列表。由 `pinix clip create/delete` 自动管理。 |
| `tokens` | Clip 作用域的访问 Token 列表。由 `pinix token generate/revoke` 自动管理。 |

每个 Clip 条目支持以下字段：

| 字段 | 是否必填 | 描述 |
|-------|----------|-------------|
| `id` | 自动生成 | 唯一标识符（创建时自动分配） |
| `name` | 是 | 易读的名称 |
| `workdir` | 是 | 宿主机上的 Clip 目录路径（在 VM 中挂载为 `/clip`） |
| `mounts` | 否 | 额外的宿主机到 VM 的绑定挂载 (`source`, `target`, `read_only`) |
| `image` | 否 | OCI 容器镜像覆盖（默认：`debian:12-slim`） |

## 4. 启动服务器

### 手动启动

```bash
pinix serve --addr :9875
```

| 参数 | 默认值 | 描述 |
|------|---------|-------------|
| `--addr` | `:8080` | 监听地址 (`host:port`) |
| `--boxlite` | (在 PATH 中自动检测) | `boxlite` 二进制文件的路径 |

如果 `boxlite` 不在您的 `PATH` 中，请显式指定：

```bash
pinix serve --addr :9875 --boxlite /path/to/boxlite
```

您应该看到：

```
[sandbox] backend: boxlite
pinix listening on :9875
```

### macOS: 使用 launchd 自动启动

创建一个 Launch Agent 以便 Pinix 在登录时自动启动：

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

> **注意：** 请将 `ProgramArguments` 和 `HOME` 中的路径替换为您的实际绝对路径。launchd 不会自动展开 `~` 或 `$HOME`。

加载并启动：

```bash
launchctl load ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
```

停止并卸载：

```bash
launchctl unload ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
```

### Linux: 使用 systemd 自动启动

创建一个用户级 systemd 服务：

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

启用并启动：

```bash
systemctl --user daemon-reload
systemctl --user enable --now pinix
```

检查状态：

```bash
systemctl --user status pinix
journalctl --user -u pinix -f
```

## 5. 快速上手：注册 Clip 并运行命令

在服务器运行的情况下，让我们注册您的第一个 Clip 并验证一切是否正常。

### 5.1 创建 Clip 目录

Clip 是一个包含 `commands/` 子目录的文件夹，其中存放着可执行脚本：

```bash
mkdir -p ~/my-first-clip/commands

cat > ~/my-first-clip/commands/hello << 'SCRIPT'
#!/bin/sh
echo "Hello from Pinix!"
echo "Arguments: $@"
SCRIPT

chmod +x ~/my-first-clip/commands/hello
```

### 5.2 注册 Clip

```bash
export SERVER="http://localhost:9875"
export SUPER_TOKEN="<your-super-token>"

pinix clip create \
  --name my-first-clip \
  --workdir ~/my-first-clip \
  --server $SERVER \
  --token $SUPER_TOKEN
```

输出：

```
clip_id: <generated-clip-id>
```

保存 `clip_id` 以备下一步使用。

### 5.3 生成 Clip Token

Clip Token 是具有特定作用域的凭据，允许在特定的 Clip 上执行命令：

```bash
pinix token generate \
  --clip <clip-id> \
  --label "my-client" \
  --server $SERVER \
  --token $SUPER_TOKEN
```

输出：

```
id:    t_xxxxxxxxxxxx
token: <64-char-hex-clip-token>
```

保存 `token` 的值。

### 5.4 调用命令

```bash
export CLIP_TOKEN="<clip-token-from-above>"

pinix invoke hello world \
  --server $SERVER \
  --token $CLIP_TOKEN
```

预期输出：

```
Hello from Pinix!
Arguments: world
```

### 5.5 查看 Clip 信息

```bash
pinix info --server $SERVER --token $CLIP_TOKEN
```

输出：

```
name:        my-first-clip
description:
has_web:     false
commands:
  - hello
```

### 5.6 列出所有 Clip

```bash
pinix clip list --server $SERVER --token $SUPER_TOKEN
```

## 常见问题 (FAQ)

### BoxLite 提示 "binary not found"

请确保 `boxlite` 二进制文件在您的 `PATH` 中，或者使用 `--boxlite` 显式传递：

```bash
pinix serve --boxlite /path/to/boxlite
```

### Linux 上提示 "KVM not available"

启用 KVM 内核模块并确保您的用户具有访问权限：

```bash
sudo modprobe kvm kvm_intel   # Intel CPU 使用此项，AMD CPU 使用 kvm_amd
sudo usermod -aG kvm $USER
# 登出并重新登录，然后验证：
ls -l /dev/kvm
```

### 服务器已启动，但 Clip 调用失败

- 验证 BoxLite 是否工作：`boxlite info`
- 检查 Clip 的 `workdir` 路径是否存在，以及 `commands/` 脚本是否可执行
- 检查服务器日志中的错误：
  - 手动启动：直接在终端可见
  - launchd: `tail -f /tmp/pinix.error.log`
  - systemd: `journalctl --user -u pinix -f`

### 如何更改 Super Token？

直接编辑 `~/.config/pinix/config.yaml` 并重启服务器。可以使用以下命令生成新 Token：

```bash
openssl rand -hex 32
```

### Clip 可以使用哪些 OCI 镜像？

默认情况下，Clip 在 `debian:12-slim` 中运行。您可以通过在 `config.yaml` 的 Clip 条目中添加 `image` 字段来为每个 Clip 进行覆盖：

```yaml
clips:
  - id: abc123
    name: my-python-clip
    workdir: /path/to/clip
    image: python:3.13-slim
```

BoxLite 将在第一次使用时自动拉取镜像。

### 如何为 Clip 添加额外的挂载点？

在 `config.yaml` 的 Clip 条目中添加 `mounts` 字段：

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

Clip 的 `workdir` 始终会自动挂载在 `/clip`。
