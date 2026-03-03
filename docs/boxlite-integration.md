# BoxLite Integration Guide

> Pinix 沙箱隔离方案：通过 `os/exec` 调用 BoxLite CLI 实现 Clip 级进程隔离。

## 架构决策

**方案：CLI 直接调用（os/exec）**

放弃了 REST API 双进程方案，改为 Pinix (Go) 通过 `os/exec` 调起 `boxlite` CLI。

理由：
- REST API 与 Go 客户端存在 9 处不匹配（认证、路径格式、流式 I/O 等）
- CLI 天然支持 Bind Mount（`-v` 参数），REST 不支持
- Pinix 主程序保持纯 Go，可轻松交叉编译所有目标平台
- BoxLite CLI 作为可选依赖独立分发

## 调用流程

```
boxlite create --name pinix-clip-<clipID> -v <workdir>:/clip -w /clip debian:12-slim
boxlite start pinix-clip-<clipID>
boxlite exec pinix-clip-<clipID> -- /clip/commands/<cmdName> [args...]
boxlite rm -f pinix-clip-<clipID>
```

## 降级策略

| 条件 | 行为 |
|------|------|
| `--boxlite <path>` 指定且二进制可用 | 沙箱模式：通过 VM 隔离执行 |
| 无 `--boxlite` 或二进制不存在 | 降级模式：直接 `os/exec` 在宿主执行 |
| `--no-sandbox` 强制指定 | 降级模式：跳过沙箱，开发用 |

降级模式下 `Manager.Degraded()` 返回 `true`，调用方可据此决定是否信任第三方 Clip。

## BoxLite 构建（关键）

### 构建顺序：guest → shim → cli

**必须严格按此顺序。** CLI 通过 `include_bytes!` 嵌入 shim 和 guest 二进制到自身，运行时提取到 `~/Library/Application Support/boxlite/runtimes/v<version>/`。

```bash
cd ~/Developer/epiral/repos/boxlite

# 1. Guest（Linux musl 交叉编译，跑在 VM 内）
bash scripts/build/build-guest.sh --dest-dir target/release/

# 2. Shim（macOS native，Hypervisor.framework，自动签名）
bash scripts/build/build-shim.sh

# 3. CLI（嵌入上述二进制）
cargo build --release --package boxlite-cli
```

### 根因教训

2026-03-02 首次编译只跑了 step 3，跳过了 guest 和 shim。构建日志警告 `boxlite-shim not found, skipping embed`，但不报错。结果 runtime 提取缺失 shim → `boxlite start` 时 VM 无法启动 → `dlopen("libkrunfw.5.dylib")` 失败（实际是 shim 本身不存在）。

**判断标准：** 构建日志必须出现 `Embedded runtime: 5 files`，包含：
- `boxlite-shim`（~19MB）
- `boxlite-guest`（~12MB）
- `libkrunfw.5.dylib`（~22MB）
- `debugfs`
- `mke2fs`

### 系统依赖

```bash
brew install llvm lld musl-cross    # shim 需要 LLVM clang，guest 需要 musl
```

## 性能指标（M4 Mac, 2026-03-02 实测）

| 阶段 | 耗时 |
|------|------|
| `boxlite create` | < 100ms |
| `boxlite start`（VM 启动） | ~1.6s |
| `boxlite exec echo hello` | < 300ms（含 VM 重启） |
| 完整构建（guest+shim+cli） | ~100s |

## 物理路径

| 资源 | 路径 |
|------|------|
| BoxLite 源码 | `~/Developer/epiral/repos/boxlite/` |
| CLI 二进制 | `~/Developer/epiral/repos/boxlite/target/release/boxlite` |
| Pinix 集成代码 | `~/Developer/epiral/repos/ws/boxlite-integration/` |
| 沙箱核心 | `internal/sandbox/box.go` |
| Runtime 提取目录 | `~/Library/Application Support/boxlite/runtimes/v0.6.0/` |
| Box 数据 | `~/.boxlite/boxes/` |

## 安全模型

BoxLite 在 macOS 上使用 **seatbelt (sandbox-exec)** 隔离 shim 进程：
- Deny-by-default allowlist
- 动态路径注入（box 的 `bin/` 目录自动加入读取白名单）
- shim 和 dylib 被复制（非硬链接）到 box 目录，确保内存隔离

未来第三方 Clip 生态中，BoxLite 将作为默认执行模式（`--no-sandbox` 仅限开发）。
