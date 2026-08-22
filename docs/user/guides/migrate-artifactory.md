---
title: 从 Artifactory 迁移（bf-migrate）
sidebar_position: 55
---

# 从 Artifactory 迁移（bf-migrate）

> 适用版本：M6（T-167；行为逐项核对 `cmd/bf-migrate/main.go` 与 `internal/migrate/`，细节参照 `reports/agents/T-167.md`）。
> bf-migrate 的读取面按 Artifactory 公开 REST 形状实现；对真实 Artifactory 实例的整体验收为条件腿（票据 Q9，待验收环境定案）——先 `--dry-run` 摸底是安全网。

`bf-migrate` 把 Artifactory 实例的**仓库定义、用户、token 台账**搬进 BinFlow。概念一一对应（local/remote/virtual、repo key、permission target——术语不变，总对照表见 [FAQ](../faq.md#从-artifactory-迁移对照表)）。

## 迁移什么、不迁移什么

| 阶段 | 迁移内容 | 不迁/不可迁 |
|---|---|---|
| repos | local/remote/virtual 仓定义（rclass/packageType 1:1；`gradle` 映射为 `maven` 并警告） | federated 仓、不支持的包类型（nuget 等）——记原因跳过；**remote 仓上游密码**（Artifactory 从不回显凭据，目标侧重填） |
| users | internal realm 用户（名、email、admin 位、groups） | `anonymous`/`_system_`/IdP realm 用户、无 email 用户；**口令不可导出**（见下） |
| tokens | 仅台账：列出、计数 | **token 值不可导出**（Artifactory 只回元数据）——目标侧重发再分发 |

两处 Artifactory 硬约束决定了两条策略：

1. **口令**：任何响应都不含用户口令。二选一——`--password-env <VAR>`（全员共享一个初始口令，迁后轮换）或 `--passwords-out <file>`（每用户随机 24 位口令，写 0600 文件、resume 追加）。两者皆无且有待迁用户时，首个写入前 fail-fast。
2. **token 值**：`GET /api/security/token` 只有元数据。第三阶段全部标 skipped 并提示目标重建。

另有两点**不在本工具范围**：**制品本体**（见下「制品搬运」）与**组定义**（组需目标侧预建——迁移用户的 groups 引用未知组会 400 记为该项失败）。

## 前置条件

- 源：Artifactory REST 可达，admin 凭据（Bearer token 或 API key）。
- 目标：BinFlow 实例 + admin API token。
- 目标侧已建好用户引用到的**组**。

## 使用

```bash
go build -o bf-migrate ./cmd/bf-migrate   # 或用发布产物
export ARTIFACTORY_URL=http://src-host:8081/artifactory   # 必须含 context path
export ARTIFACTORY_TOKEN=<源 admin token>
export BINFLOW_SERVER_URL=http://binflow:8080
export BINFLOW_TOKEN=<目标 admin token>
```

```bash
# 第一步永远先摸底：只读不写、不落进度文件、目标无需凭据
bf-migrate migrate --dry-run
# phase repos: found 6, migratable 5, skipped 1 ...
# phase users: found 4, migratable 2 ...
# phase tokens: listed 2 — values not exportable, all skipped

# 全量（生成每用户口令）
bf-migrate migrate --passwords-out ./migrated-passwords.txt

# 中断后续传：跳过进度文件里已完成的项，失败项重试
bf-migrate migrate --passwords-out ./migrated-passwords.txt --resume
```

| 旗标 | 缺省 | 说明 |
|---|---|---|
| `--artifactory-url` | `$ARTIFACTORY_URL` | 源地址（含 context path，必填） |
| `--artifactory-token-env` | `ARTIFACTORY_TOKEN` | 源 Bearer token 变量名 |
| `--artifactory-api-key-env` | `ARTIFACTORY_API_KEY` | 源 API key 变量名（token 未设时回退，`X-JFrog-Art-Api` 头） |
| `--server` | `$BINFLOW_SERVER_URL` → `http://localhost:8080` | 目标地址 |
| `--token-env` | `BINFLOW_TOKEN` → `BF_TOKEN` | 目标 admin token 变量名（非 dry-run 必须解析到） |
| `--dry-run` | 关 | 只统计不写 |
| `--resume` | 关 | 载入进度文件，跳过已完成项 |
| `--progress-file` | `.bf-migrate-progress.json` | 检查点路径 |
| `--retry-max` | `5` | 目标瞬时失败重试次数（负数关闭） |
| `--password-env` / `--passwords-out` | 无 | 口令策略二选一 |

## 三阶段语义（保序、可断点）

- 顺序固定 **repos → users → tokens**：virtual 引用成员仓、token 引用用户。repo 按类型再按 key 排序，**virtual 殿后**——成员先于聚合仓就位。
- **逐项检查点**：成功/跳过即写进度（原子落盘 temp+rename）；失败项**不标记**，`--resume` 就是重试杠杆。级联失败（成员仓没建好 → virtual 400）同样可恢复。
- 写入动词是 create-or-replace（PUT 语义），重跑天然幂等；不带 `--resume` 的新跑会重置进度文件。
- 进度文件绑定源/目标 URL：两场迁移混用一个检查点会被拒载。
- 单项失败不中断全场：结束聚合报错、退出码 1。

## 迁移后手工收尾清单

1. **remote 仓上游密码**：逐个重填（Artifactory 不回显，迁移只带 url/username）。
2. **组与权限**：`POST /api/v1/permissions` 挂授权（组预建前置）。
3. **token 重发**：按台账在目标侧 `bf token create`（或 `POST /api/security/token`），再分发给持有方。
4. **用户口令分发**：`migrated-passwords.txt`（0600）安全渠道分发，强制首次登录轮换。
5. **virtual 成员与 local 仓 patterns 复核**：见下节已知缺口。

## 已知缺口（如实，写脚本时留意）

- **virtual 成员与 local 仓 includes/excludes patterns 当前会丢**：CLI 依赖的 `internal/client` 建仓请求体用 `members`/`includes`/`excludes` 拼写，而服务端读 `repositories`/`includesPattern`/`excludesPattern`（T-191 对齐票在路上）。对齐前，迁移完的 virtual 仓是空的、patterns 未带上——用 REST 直接 PUT 补齐：

```bash
curl -su admin:$PW -X PUT $BINFLOW_SERVER_URL/binflow/api/repositories/libs-virtual \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"generic","repositories":["libs-generic","libs-release"]}'
```

- **制品本体不在三阶段内**：仓库定义就位后另行搬运（通用做法：源侧按 `GET /api/storage/<repo>/<path>` 清单遍历 + 目标侧 PUT；或暂用双仓并行、客户端切源）。此面为后续票据范围。
- 真实 Artifactory 整体验收为条件腿（Q9）——先 `--dry-run`、小仓试点、再全量。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| `--artifactory-url is required` | 地址没给/没进 env | 补上（记得带 context path） |
| `no BinFlow credentials: ... (required for writes)` | 非 dry-run 且 token 未解析到 | `--token-env` 指到已导出的变量 |
| 用户项 400 `Unable to find group by name` | 目标侧缺组 | 预建组后 `--resume` |
| token 阶段 401/403 只警告不失败 | 源凭据非 admin（列表不可见） | 换 admin 凭据；该阶段不标进度，resume 会重试 |
| 结束退出码 1 | 有 item 级失败 | 看汇总；修复后 `--resume` 精确重试 |

## 下一步

- 目标侧配置：[bf CLI](bf-cli.md)（建仓/发 Token 的脚本面）
- 差异总览：[FAQ · 从 Artifactory 迁移](../faq.md#从-artifactory-迁移对照表)
- 各协议客户端切源：[客户端接入](../README.md)
