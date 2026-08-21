---
title: systemd 服务（裸机）
sidebar_position: 15
---

# systemd 服务（裸机）

> 适用版本：M5 GA v1.0.0（contrib/systemd/binflow.service + install.sh；T-138, FR-39, PB-07）。
> 本文命令在 Linux 系统（systemd 251+）上复跑：install 脚本干跑退出码 0、语法检查通过。macOS 无 systemd，Windows 无 systemd——两平台标注为 Q3 降级口径。

BinFlow 为 Linux 裸机部署提供了标准的 systemd service unit 和安装/卸载脚本。服务以非 root `binflow` 用户运行，应用了 `NoNewPrivileges`、`ProtectSystem` 等安全加固。

## 安装脚本（推荐）

`contrib/systemd/install.sh` 提供了幂等的安装路径，自动完成：校验和验证 → 系统用户创建 → 目录布局 → 二进制安装 → 配置安装 → systemd unit 注册 → 服务启动。

### 前置要求

- Linux（amd64 或 arm64）with systemd >= 250
- `bash, curl, sha256sum, systemctl, useradd, mkdir, chown, cp, ln`
- root/sudo 权限

### 快速安装

```bash
sudo bash contrib/systemd/install.sh
```

安装脚本默认行为：
- 从 GitHub Releases 下载最新版
- 校验 checksums 后解压安装
- 创建 `binflow` 系统用户
- 创建 `/etc/binflow`（配置）和 `/var/lib/binflow`（数据）
- 安装二进制到 `/usr/local/bin/binflow-server`
- 安装 systemd unit 并 `enable --now`

### 指定版本

```bash
sudo bash contrib/systemd/install.sh --version v1.0.0
```

### 自定义下载源

```bash
sudo bash contrib/systemd/install.sh --base-url https://releases.example.com/artifacts
```

### 干跑预览

```bash
sudo bash contrib/systemd/install.sh --dry-run --version v1.0.0
```

### 配置管理

安装脚本会在以下位置安装默认配置：

```bash
# 默认配置会从归档内复制
# 如果 /etc/binflow/binflow.yaml 已存在则跳过（幂等）
```

首次启动前检查/编辑配置：

```bash
sudo vim /etc/binflow/binflow.yaml
```

环境变量覆盖（可选）：创建 `/etc/binflow/env`，systemd 使用 `EnvironmentFile=-/etc/binflow/env` 加载（文件不存在不报错）：

```ini
BINFLOW_ADMIN_PASSWORD=my-secret
BINFLOW_LOGGING__LEVEL=debug
```

## 手动安装

### 1. 下载二进制

参考[单二进制安装](binary.md)的下载和校验步骤，将二进制放到目标位置。

### 2. 创建用户和目录

```bash
sudo useradd -r -s /usr/sbin/nologin -M -d /var/lib/binflow binflow
sudo mkdir -p /etc/binflow /var/lib/binflow
sudo chown -R binflow:binflow /etc/binflow /var/lib/binflow
```

### 3. 安装二进制

```bash
sudo cp ./binflow-server /usr/local/bin/binflow-server
sudo chmod 755 /usr/local/bin/binflow-server
```

### 4. 安装配置

将默认配置复制到 `/etc/binflow/binflow.yaml`（可在源码的 `dist/config.yaml` 找到参考）。

### 5. 安装 systemd unit

```bash
sudo cp contrib/systemd/binflow.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now binflow
```

## 服务管理

```bash
# 启停
sudo systemctl start binflow
sudo systemctl stop binflow
sudo systemctl restart binflow
sudo systemctl reload-or-restart binflow

# 状态检查
sudo systemctl status binflow

# 日志
sudo journalctl -u binflow -f
sudo journalctl -u binflow --since "5 minutes ago"

# 开机自启
sudo systemctl enable binflow
sudo systemctl disable binflow
```

## 健康检查

```bash
curl -s http://127.0.0.1:8080/readyz
# ok
```

## 安全加固说明

service unit 应用了以下加固（见 `contrib/systemd/binflow.service`）：

| 加固 | 说明 |
|---|---|
| `User=binflow`、`Group=binflow` | 非 root 运行 |
| `NoNewPrivileges=yes` | 禁止 setuid/capability 提权 |
| `ProtectSystem=strict` | /usr、/boot、/etc 只读（例外见 ReadOnlyPaths） |
| `ProtectHome=yes` | 禁止访问 /home、/root、/run/user |
| `ReadWritePaths=/var/lib/binflow` | 仅数据目录可写 |
| `ReadOnlyPaths=/etc/binflow` | 配置目录只读 |
| `PrivateTmp=yes` | 私有 /tmp |
| `PrivateDevices=yes` | 私有 /dev |
| `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` | 仅网络套接字 |
| `LimitNOFILE=65536` | 文件描述符限制 |
| `CapabilityBoundingSet=` | 零额外能力 |
| `SystemCallFilter=@system-service` | 系统调用过滤 |

## 卸载

### 使用 install 脚本

```bash
# 卸载（保留数据 + 配置）
sudo bash contrib/systemd/install.sh --uninstall

# 卸载并清除所有数据（谨慎）
sudo bash contrib/systemd/install.sh --uninstall --purge
```

### 手动卸载

```bash
# 停止并禁用服务
sudo systemctl stop binflow
sudo systemctl disable binflow

# 删除 unit 文件
sudo rm -f /etc/systemd/system/binflow.service
sudo systemctl daemon-reload

# 删除二进制
sudo rm -f /usr/local/bin/binflow-server

# 删除用户（仅当确定没有依赖）
sudo userdel binflow

# 删除数据（谨慎——所有制品丢失）
sudo rm -rf /var/lib/binflow
sudo rm -rf /etc/binflow
```

## 目录布局

| 路径 | 说明 |
|---|---|
| `/usr/local/bin/binflow-server` | 二进制 |
| `/etc/binflow/binflow.yaml` | 配置文件 |
| `/etc/binflow/env` | 环境变量文件（可选） |
| `/var/lib/binflow` | 数据目录（blobs + SQLite + sessions） |
| `/etc/systemd/system/binflow.service` | systemd unit 文件 |

## 下一步

- [单二进制安装](binary.md) — 无 systemd 的裸机方式
- [docker-compose 部署](compose.md) — 容器化替代
- [备份与恢复手册](../admin/backup-restore.md) — export/import CLI