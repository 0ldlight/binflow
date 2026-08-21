---
title: 单二进制安装
sidebar_position: 10
---

# 单二进制安装

> 适用版本：M5 GA v1.0.0（六个平台：linux/darwin/windows x amd64/arm64）。
> 本文命令在本地 macOS arm64 上复跑：下载→解压→`serve`→`/readyz` 200，退出码 0。

BinFlow 是一个零 CGo 的静态链接 Go 二进制文件——不依赖任何运行时、不依赖 cgo 库、不依赖系统包管理器。下载、解压、运行即可。

## 平台支持

| 操作系统 | 架构 | 产物格式 |
|---|---|---|
| Linux | amd64, arm64 | `.tar.gz` |
| macOS (darwin) | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

## 前置要求

- **无运行时依赖**（零 CGo，静态链接）。
- 存储：BinFlow 将 blob 和 SQLite 数据库写入 `./data/`（或 `$BINFLOW_HOME/data/`），请确保有足够的磁盘空间。
- 网络：默认监听 `:8080`。
- Windows 腿：当前标为「文档验证」级别——路径分隔符与服务模式在 Windows 上可与 Linux/macOS 行为不同，以下命令以 macOS/Linux 为基准，Windows 标注为 Q3 降级口径。

## 安装步骤

### 1. 下载

从 [GitHub Releases](https://github.com/example/binflow/releases) 下载对应平台的 tar.gz（或 zip）：

```bash
export VER=v1.0.0
export OS=$(uname -s | tr '[:upper:]' '[:lower:]')
export ARCH=$(uname -m)
# 架构名归一化
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

curl -fsSLO "https://github.com/example/binflow/releases/download/${VER}/binflow_${VER}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/example/binflow/releases/download/${VER}/binflow_${VER}_checksums.txt"
```

### 2. 校验 sha256

goreleaser 在构建时生成 `binflow_<VER>_checksums.txt`，格式为 `<sha256>  <archive>`：

```bash
grep "binflow_${VER}_${OS}_${ARCH}.tar.gz" binflow_${VER}_checksums.txt | shasum -a 256 -c
# binflow_v1.0.0_darwin_arm64.tar.gz: OK
```

校验失败则立即停止，不要解压。

### 3. 解压

```bash
tar xzf "binflow_${VER}_${OS}_${ARCH}.tar.gz"
# 生成目录：binflow_<VER>_<OS>_<ARCH>/binflow-server
```

产物目录结构：

```
binflow_${VER}_${OS}_${ARCH}/
├── binflow-server          # 静态链接二进制（~40MB）
├── README.md
└── LICENSE                 # 如果存在
```

### 4. 启动服务

```bash
cd "binflow_${VER}_${OS}_${ARCH}"
./binflow-server serve
```

默认行为：
- 监听 `:8080`（所有接口）
- 数据目录 `./data/`（自动创建）
- 管理员密码默认 `password`（服务启动时打印 `WARN` 提醒修改）
- 日志输出到 stderr（控制台格式）

自定义配置：

```bash
# 指定配置文件
./binflow-server serve -c /path/to/binflow.yaml

# 通过环境变量覆盖（全大写 + 点改下划线 + 双下划线分隔层级）
BINFLOW_ADMIN_PASSWORD=my-secret \
BINFLOW_STORAGE__DATA_DIR=/data/binflow \
./binflow-server serve
```

### 5. 验证 `/readyz`

```bash
curl -s http://127.0.0.1:8080/readyz
# 返回：ok（HTTP 200）
```

`/readyz` 端点无需认证，会 ping 元数据存储并检查存储目录可写。返回非 200 说明服务未就绪。

### 6. 版本确认

```bash
./binflow-server --version
# binflow-server v1.0.0 (abc1234)
```

## 后台运行

**macOS（launchd）**：创建 `~/Library/LaunchAgents/com.binflow.server.plist` 或直接用 `nohup`：

```bash
nohup ./binflow-server serve > binflow.log 2>&1 &
```

**Linux**：推荐 systemd（见 [systemd 服务](systemd.md)），临时用：

```bash
nohup ./binflow-server serve > binflow.log 2>&1 &
```

**Windows（Q3 降级）**：将 `binflow-server.exe` 放在任意目录，双击运行或从 PowerShell 启动：

```powershell
.\binflow-server.exe serve
```

## 卸载

```bash
# 停止服务（Ctrl+C 或 kill）
pkill binflow-server

# 删除二进制与数据
rm -rf binflow_${VER}_${OS}_${ARCH}
rm -rf ./data
```

## 目录约定

| 路径 | 说明 |
|---|---|
| `./binflow.yaml` | 默认配置文件（`-c` 未指定时的查找顺序：当前目录 → `$BINFLOW_HOME/`） |
| `./data/` | 默认数据目录（blobs + SQLite + sessions） |
| `$BINFLOW_HOME/` | 配置与数据的备用根目录（环境变量可选） |

## 下一步

- [Docker 运行](docker.md) — 容器化部署
- [systemd 服务](systemd.md) — 裸机生产级守护
- [Generic 接入](../integrations/generic.md) — curl PUT/GET 上传下载