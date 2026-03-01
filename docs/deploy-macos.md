# macOS 部署指南

在 macOS 上部署 Pinix Server 并配置开机自启。

## 1. 构建

```bash
cd ~/Developer/epiral/repos/pinix
go build -o ~/bin/pinix .
```

确保 `~/bin` 在 `$PATH` 中（可在 `~/.zshrc` 添加 `export PATH="$HOME/bin:$PATH"`）。

## 2. 初始化配置

```bash
mkdir -p ~/.config/pinix

# 生成随机 Super Token
SUPER_TOKEN=$(openssl rand -hex 32)
cat > ~/.config/pinix/config.yaml << EOF
super_token: "${SUPER_TOKEN}"
clips: []
tokens: []
EOF
chmod 600 ~/.config/pinix/config.yaml

echo "Super Token: ${SUPER_TOKEN}"
# ⚠️ 记录此 Token，后续管理操作需要
```

## 3. 验证手动启动

```bash
pinix serve --addr :9875
# 看到 "pinix listening on :9875" 即成功
# Ctrl+C 停止
```

## 4. 配置 launchd 自启动

```bash
cat > ~/Library/LaunchAgents/ai.yan5xu.pinix.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>ai.yan5xu.pinix</string>

    <key>ProgramArguments</key>
    <array>
        <string>/Users/cp/bin/pinix</string>
        <string>serve</string>
        <string>--addr</string>
        <string>:9875</string>
    </array>

    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>/Users/cp</string>
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

> **注意**：`ProgramArguments` 中的路径和 `HOME` 需替换为实际用户路径。端口按需修改。

## 5. 启动 / 停止 / 重启

```bash
# 加载并启动
launchctl load ~/Library/LaunchAgents/ai.yan5xu.pinix.plist

# 停止并卸载
launchctl unload ~/Library/LaunchAgents/ai.yan5xu.pinix.plist

# 重启（停 → 启）
launchctl unload ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
launchctl load ~/Library/LaunchAgents/ai.yan5xu.pinix.plist

# 查看状态
launchctl list | grep pinix
```

## 6. 日志

```bash
tail -f /tmp/pinix.log          # stdout
tail -f /tmp/pinix.error.log    # stderr
```

## 7. 升级

```bash
# 构建新版
cd ~/Developer/epiral/repos/pinix
git pull
go build -o ~/bin/pinix .

# 重启服务
launchctl unload ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
launchctl load ~/Library/LaunchAgents/ai.yan5xu.pinix.plist
```

## 8. 注册 Clip

```bash
SUPER_TOKEN="<your-super-token>"
SERVER="http://localhost:9875"

# 注册
pinix clip create --name my-clip --workdir /path/to/clip --server $SERVER --token $SUPER_TOKEN

# 列出
pinix clip list --server $SERVER --token $SUPER_TOKEN

# 生成 Clip Token（给 Client 用）
pinix token generate --clip <clip_id> --label "my-client" --server $SERVER --token $SUPER_TOKEN
```
