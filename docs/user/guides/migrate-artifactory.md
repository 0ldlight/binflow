---
title: 从 Artifactory 迁移（bf-migrate）
sidebar_position: 55
---

# 从 Artifactory 迁移（bf-migrate）

> 适用版本：M6 建面（T-167）、**M15 时点措辞清账**（2026-09-02，T-428——T-397 遗留 #3 登记；四阶段语义 / 包型跳过口径 / 空目标守卫与新旗标按 as-built 逐项核对 `cmd/bf-migrate/main.go` 与 `internal/migrate/`，制品阶段与守卫随 T-196 落地）。
> bf-migrate 的读取面按 Artifactory 公开 REST 形状实现；对真实 Artifactory 实例的整体验收为条件腿（票据 Q9，已执行——实录见[附录 V28](../admin/real-env-appendix.md#v28真实-artifactory-迁移实腿dep用户环境)）——先 `--dry-run` 摸底是安全网。

`bf-migrate` 把 Artifactory 实例的**仓库定义、用户、token 台账、generic/maven 布局仓的制品本体**（四阶段，见下）搬进 BinFlow。概念一一对应（local/remote/virtual、repo key、permission target——术语不变，总对照表见 [FAQ](../faq.md#从-artifactory-迁移对照表)）。

## 迁移什么、不迁移什么

| 阶段 | 迁移内容 | 不迁/不可迁 |
|---|---|---|
| repos | local/remote/virtual 仓定义（rclass 1:1；packageType 见下行映射集） | federated 仓；**映射集外包型仓**——bf-migrate 的包型映射集 = `generic`/`docker`/`maven`/`npm`/`pypi`（`gradle` 别名为 `maven` 并警告），集外包型记原因跳过（见下「工具映射面 ≠ 产品能力面」）；**remote 仓上游密码**（Artifactory 从不回显凭据，目标侧重填） |
| users | internal realm 用户（名、email、admin 位、groups） | `anonymous`/`_system_`/IdP realm 用户、无 email 用户；**口令不可导出**（见下） |
| tokens | 仅台账：列出、计数 | **token 值不可导出**（Artifactory 只回元数据）——目标侧重发再分发 |
| artifacts | **generic 与 maven（含 gradle 别名）布局仓的制品本体**：源侧清单 → 下载中双哈希（sha1/sha256）校验 → 目标侧 plain-file PUT；`--concurrency` 并行（默认 4）、逐仓统计行进报告 | docker/npm/pypi 仓的**制品**（协议上传面——registry manifest/blob 会话、npm tarball+元数据、twine multipart——无法用 plain-file 面复刻，配置迁、制品留源侧记原因）；映射集外包型仓（repo 阶段已跳过，`artifacts not migrated`）；remote 仓（缓存不迁，重配上游后按需回填）与 virtual 仓（聚合无本体） |

**工具映射面 ≠ 产品能力面**：repos 阶段跳过 nuget/go/cargo/conan/helm/helmoci/rpm/deb 等包型仓，说的是 **bf-migrate 的转换器映射集**（`internal/migrate/converter.go` 只携带五型），不是 BinFlow 的产品能力——这些包型 M10~M14 已全部交付。此类仓在目标侧**手建同 key 仓**、用各协议客户端（`dotnet nuget push` / `conan upload` / `helm push` 等）重灌制品即可；bf-migrate 的制品阶段不接手集外包型。

两处 Artifactory 硬约束决定了两条策略：

1. **口令**：任何响应都不含用户口令。二选一——`--password-env <VAR>`（全员共享一个初始口令，迁后轮换）或 `--passwords-out <file>`（每用户随机 24 位口令，写 0600 文件、resume 追加）。两者皆无且有待迁用户时，首个写入前 fail-fast。
2. **token 值**：`GET /api/security/token` 只有元数据。第三阶段全部标 skipped 并提示目标重建。

另有一点**不在本工具范围**：**组定义**（组需目标侧预建——迁移用户的 groups 引用未知组会 400 记为该项失败）。制品本体已由第四阶段承载（范围与边界见上表 artifacts 行）。

## 前置条件

- 源：Artifactory REST 可达，admin 凭据（Bearer token 或 API key）。
- 目标：BinFlow 实例 + admin API token；**默认只许迁入空目标（零仓实例）**——非空目标会被守卫拒绝，确需合并见 `--allow-non-empty`。
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
# bf-migrate summary (dry-run)
# repos: found=6 migrated=5 skipped=1 already-done=0 failed=0    ← migrated 在 dry-run 里是「将写入」计数
# users: found=4 migrated=2 skipped=2 already-done=0 failed=0
# tokens: found=2 migrated=0 skipped=2

# 全量（生成每用户口令）
bf-migrate migrate --passwords-out ./migrated-passwords.txt

# 中断后续传：跳过进度文件里已完成的项，失败项重试
bf-migrate migrate --passwords-out ./migrated-passwords.txt --resume
```

全量跑完的 summary 还有第四行 `artifacts: found=N migrated=N skipped=N already-done=N failed=N bytes=N`（其后逐仓明细行——跳过原因逐字给出，如 docker 仓「upload through the registry protocol … artifacts left behind」）与 `progress:` / `report: migration_report.json` 两行。

| 旗标 | 缺省 | 说明 |
|---|---|---|
| `--artifactory-url` | `$ARTIFACTORY_URL` | 源地址（含 context path，必填） |
| `--artifactory-token-env` | `ARTIFACTORY_TOKEN` | 源 Bearer token 变量名 |
| `--artifactory-api-key-env` | `ARTIFACTORY_API_KEY` | 源 API key 变量名（token 未设时回退，`X-JFrog-Art-Api` 头） |
| `--server` | `$BINFLOW_SERVER_URL` → `http://localhost:8080` | 目标地址 |
| `--token-env` | `BINFLOW_TOKEN` → `BF_TOKEN` | 目标 admin token 变量名（非 dry-run 必须解析到） |
| `--dry-run` | 关 | 只统计不写（目标占用只观测不拒绝） |
| `--resume` | 关 | 载入进度文件，跳过已完成项 |
| `--allow-non-empty` | 关 | 覆盖空目标守卫，并入非空目标（create-or-replace：同 key 仓重配、同路径制品覆盖、源未提及的目标内容不动） |
| `--concurrency` | `4` | 制品阶段每仓并行拷贝数 |
| `--progress-file` | `.bf-migrate-progress.json` | 检查点路径 |
| `--report-file` | `migration_report.json` | 运行报告路径（逐阶段计数 + 逐仓制品行 + 跳过原因） |
| `--retry-max` | `5` | 目标瞬时失败重试次数（负数关闭） |
| `--password-env` / `--passwords-out` | 无 | 口令策略二选一 |
| `--skip-users` | 关 | users 阶段可选化：源用户列表被拒（400/403）时降级为警告继续，不再中断全场 |

## 四阶段语义（保序、可断点）

- 顺序固定 **repos → users → tokens → artifacts**：virtual 引用成员仓、token 引用用户；制品殿后——配置问题先暴露，再动大数据。repo 按类型再按 key 排序，**virtual 殿后**——成员先于聚合仓就位。
- **空目标守卫**：写入型运行只许迁入零仓目标。目标非空时整场拒绝（`migrate: target instance is not empty: <占用明细> — only an empty instance (zero repositories) may be migrated into; pass --allow-non-empty to merge anyway`），目标不可列举时同样 fail-closed；`--dry-run` 只观测不拒绝，`--resume` 沿本对端进度续跑不被拦（占用即本迁移自己的先前写入）。
- **逐项检查点**：成功/跳过即写进度（原子落盘 temp+rename）；失败项**不标记**，`--resume` 就是重试杠杆。级联失败（成员仓没建好 → virtual 400）同样可恢复。artifacts 阶段进度按 32 条批落盘——批窗口内崩溃只会重拷幂等上传。
- 写入动词是 create-or-replace（PUT 语义），重跑天然幂等；不带 `--resume` 的新跑会重置进度文件。
- 进度文件绑定源/目标 URL：两场迁移混用一个检查点会被拒载。
- 单项失败不中断全场：结束聚合报错、退出码 1。

## 迁移后手工收尾清单

1. **remote 仓上游密码**：逐个重填（Artifactory 不回显，迁移只带 url/username）。
2. **组与权限**：`POST /api/v1/permissions` 挂授权（组预建前置）。
3. **token 重发**：按台账在目标侧 `bf token create`（或 `POST /api/security/token`），再分发给持有方。
4. **用户口令分发**：`migrated-passwords.txt`（0600）安全渠道分发，强制首次登录轮换。
5. **virtual 成员与 local 仓 patterns 复核**：见下节已知缺口。

## 已知缺口与行为注记（如实，写脚本时留意）

- ~~virtual 成员与 local 仓 patterns 会丢~~ **已修复**（M6 起建仓请求体按服务端拼写 `repositories` / `includesPattern` / `excludesPattern` 发送）——迁移完的 virtual 仓成员与 patterns 正常带上。仍建议迁移后按「手工收尾清单」第 5 条复核一遍。
- **maven 仓重迁轮 `maven-metadata.xml` 会被目标重算**：merge/create-or-replace 重跑时，jar/pom 再上传会触发 BinFlow 按 maven 语义**服务端重算** metadata（`<lastUpdated>` 等字段变化、节点 sha 随之不同）——这与源侧「平面复制」不同，是设计行为（V28 真实源迁移 R-7 实证）。比对迁移前后字节时以构件本体（jar/pom）为准，metadata 差异可忽略。
- **源为 Artifactory OSS 时的能力边界**：OSS 7.x 无 docker registry 与 npm 协议面（`/v2/`、`/api/npm/{repo}/**` 全 404）——docker/npm 仓只能 **config-only 迁移**（报告记 "artifacts left behind"），与网络连通性无关；协议制品迁移须 Pro/企业版源（V28 R-2 实证）。
- **制品阶段只覆盖 generic 与 maven（含 gradle 别名）布局**：docker/npm/pypi 仓**配置可迁、制品留源侧**（报告逐仓记原因——协议上传面无法用 plain-file 面复刻）；映射集外包型仓同样不接手（见上「工具映射面 ≠ 产品能力面」）。逐仓明细在 summary 与 `migration_report.json`。
- 真实 Artifactory 整体验收为条件腿（Q9，已执行——实录与差异清单见[附录 V28](../admin/real-env-appendix.md#v28真实-artifactory-迁移实腿dep用户环境)）；先 `--dry-run`、小仓试点、再全量。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| `--artifactory-url is required` | 地址没给/没进 env | 补上（记得带 context path） |
| `migrate: target instance is not empty: ...` | 空目标守卫（默认只许迁入零仓目标） | 确认目标本该为空；确要合并则 `--allow-non-empty`（create-or-replace 语义，见旗标表） |
| `no BinFlow credentials: ... (required for writes)` | 非 dry-run 且 token 未解析到 | `--token-env` 指到已导出的变量 |
| 用户项 400 `Unable to find group by name` | 目标侧缺组 | 预建组后 `--resume` |
| token 阶段 401/403 只警告不失败 | 源凭据非 admin（列表不可见） | 换 admin 凭据；该阶段不标进度，resume 会重试 |
| `skipped repo <key>: package type "..." is not supported` | 包型在 bf-migrate 映射集外（五型 + gradle 别名） | 目标侧手建同 key 仓 + 协议客户端重灌（见「工具映射面 ≠ 产品能力面」） |
| 结束退出码 1 | 有 item 级失败 | 看汇总；修复后 `--resume` 精确重试 |

## 下一步

- 目标侧配置：[bf CLI](bf-cli.md)（建仓/发 Token 的脚本面）
- 差异总览：[FAQ · 从 Artifactory 迁移](../faq.md#从-artifactory-迁移对照表)；控制台操作路径对照：[Artifactory → BinFlow 操作路径对照表](../artifactory-path-map.md)
- 各协议客户端切源：[客户端接入](../README.md)
