---
title: bf CLI 使用指南
sidebar_position: 54
---

# bf CLI 使用指南

> 适用版本：M6（T-166 四子命令；行为逐项核对 `cmd/bf/main.go`/`config.go`）。
> 本文四条子命令示例于本机构建（HEAD）对真实 `binflow-server` 实例完整往返复跑：建仓→上传→下载 sha256 一致→建用户→发 Token。

`bf` 是 BinFlow 的命令行客户端：建仓、传制品、建用户、发 Token——CI 脚本与自动化场景的趁手面。配置走 `~/.bf/config.yaml` 多 profile，**密钥永不落盘**（配置里只存环境变量名）。

## 安装

随发布产物分发（goreleaser 六平台），或源码构建：

```bash
go build -o bf ./cmd/bf
bf --version    # bf <version> (<revision>)
```

## 配置：profile 与凭据

`~/.bf/config.yaml`（路径可用 `BF_CONFIG` 覆盖；文件缺失不是错误——零配置直接用默认地址 `http://localhost:8080`）：

```yaml
profiles:
  default:
    base_url: http://localhost:8080
    username: admin
    password_env: BINFLOW_PASSWORD     # 只存变量名，口令在环境里
  prod:
    base_url: https://binflow.example.com
    token_env: PROD_BF_TOKEN           # Bearer 优先于 Basic
default_profile: prod                  # 可选：--profile 缺省时的选择
```

| profile 字段 | 说明 |
|---|---|
| `base_url` | 服务地址（非密） |
| `username` | Basic 认证用户名（非密） |
| `password_env` | 持有 Basic 口令的环境变量**名** |
| `token_env` | 持有 Bearer Token 的环境变量**名** |
| `skip_tls_verify` | 跳过 TLS 校验（自签实例的开发便利，慎用） |

覆盖优先级（高 → 低）：

- **地址**：`--server` 旗标 > `BF_BASE_URL` / `BINFLOW_SERVER_URL` > profile `base_url` > 内置默认。
- **凭据**：`BF_TOKEN` > profile `token_env` 指到的值（Token 优先）；`BF_USERNAME`+`BF_PASSWORD` > profile `username`+`password_env` 指到的值。
- 配了口令但没配用户名直接报错（不给静默匿名）。

## 四个子命令

全局旗标须放在命令词**之前**（`bf --profile prod repo create ...`）。

### 1. repo create — 建仓

```bash
bf repo create libs-generic --type local --package-type generic
# Repository 'libs-generic' created.
```

| 旗标 | 缺省 | 说明 |
|---|---|---|
| `--type` | `local` | `local` / `remote` / `virtual` |
| `--package-type` | `generic` | `generic` / `docker` / `maven` / `npm` / `pypi` |
| `--description` | 空 | 描述 |

remote/virtual 的成员、上游地址等字段本命令不带——用 REST（`PUT /api/repositories/{key}`，见[API 参考](../api-reference.md)）配全。

### 2. artifact upload — 上传制品

```bash
echo "hello" > f.txt
bf artifact upload f.txt --repo libs-generic --path doc/hello.txt
# sha256: e5ae375795547a0166772d02025ccb4df9ed1520d1638d60802a0d90a2eb4e94
# uri: http://localhost:8080/binflow/libs-generic/doc/hello.txt
```

| 旗标 | 必填 | 说明 |
|---|---|---|
| `--repo` | 是 | 目标仓 |
| `--path` | 是 | 仓内路径 |
| `--content-type` | 否 | 缺省由服务端按扩展名推断 |

第一行 stdout 是本地算的 sha256（脚本可断言），第二行是可下载 URI。

### 3. user create — 建用户

```bash
bf user create ci-user --password-env CI_PW --email ci@example.com
# User 'ci-user' created.
```

| 旗标 | 说明 |
|---|---|
| `--password` | 直接给口令（会进 shell 历史，优先用 `--password-env`） |
| `--password-env` | 持有口令的环境变量 |
| `--email` | 必填（服务端拒空） |
| `--admin` | 授管理员 |

用户名必须小写（服务端拒混合大小写）。两者都缺直接客户端报错。

### 4. token create — 发 Token

```bash
bf token create --username deployer --expires-in 3600
# <token 值——首行，仅此一次显示>
# token_id: 1
# expires_in: 3600
```

| 旗标 | 说明 |
|---|---|
| `--username` | Token 主体，缺省取解析出的 profile 用户名 |
| `--expires-in` | 秒；0 = 服务端默认 TTL |
| `--scope` | 如 `api:*` |
| `--description` | 标签 |

**Token 值只在创建时返回一次**，服务端无法找回——当场入保险箱。

## 验证（完整 roundtrip）

```bash
export BF_BASE_URL=http://localhost:8080 BF_USERNAME=admin BF_PASSWORD=<pw>
bf repo create t-repo --type local --package-type generic
bf artifact upload app.bin --repo t-repo --path v1/app.bin
# 回读对账
curl -s $BF_BASE_URL/binflow/t-repo/v1/app.bin | shasum -a 256
# 与上传打印的 sha256 一致
```

## 约定与退出码

- 成功：结果打 stdout（机器可读优先）；失败：错误打 stderr、退出码 1（服务端 `errors[].message` 会带出来）。
- `--help`/`-h` 在任意层级可用，退出码 0。
- 请求超时 30s/请求，5xx 与网络错误自动重试（默认 5 次指数退避）。

## 已知边界（如实）

- 只有 create 类动作：删仓/删制品/改密请用 REST 或控制台。
- `bf config set` 未实现——profile 手工编辑 YAML。
- `repo create` 不带 remote/virtual 专有字段（见上）。

## 下一步

- REST 全量面：[API 参考](../api-reference.md)
- 各协议客户端接入：[npm](../integrations/npm.md) / [Maven](../integrations/maven.md) / [PyPI](../integrations/pypi.md) / [Docker](../docker-registry.md)
- Artifactory 整体搬迁：[迁移指南](migrate-artifactory.md)
