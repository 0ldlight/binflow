---
title: FAQ 与故障排查
sidebar_position: 90
---

# FAQ 与故障排查

> 适用版本：M1~M4（各条目标注引入里程碑）。码值与文案以 M4（PRD milestone-4 v1.2）为准，全部经 QA 真机验证（T-103/T-105 验收基线）。

## 状态码信封解读

所有非 2xx 的制品域响应为统一信封（E-01）：

```json
{"errors":[{"status":413,"message":"Repository 'tiny' quota exceeded: used 800 of 1024 bytes; ..."}]}
```

用户管理/组/token 域为**纯文本**错误体，docker `/v2` 面为 spec 信封（`{"errors":[{"code":"DENIED",...}]}`）——三种格式并存是 Artifactory 兼容面，客户端按域名解析即可。

### 401（未认证）

| 场景 | 表现 | 处置 |
|---|---|---|
| 控制台会话过期/未登录 | `GET /api/v1/session` 401；界面 toast「登录已过期」+ 重登 | 重新登录；会话有绝对寿命（见[控制台指南](console.md#登录与会话)），**活跃也不能续命** |
| 登录口令错误 | `{"errors":[{"status":401,"message":"invalid credentials"}]}`——用户不存在与口令错**文案相同** | 核对凭据；连续失败落 `login.failed` 审计 |
| 匿名写操作（publish/push/deploy/upload） | `authentication required` | 配置客户端凭据（settings.xml / `_auth` / `.pypirc` / `docker login`） |
| docker 面未认证 | `401 + Www-Authenticate: Bearer realm=".../v2/token"`——这是**协议正常流程**，客户端自动协商 | 无需干预 |
| 已吊销/过期 token 或会话 cookie 重放 | 401（呈交但失效的凭据**绝不降级匿名**） | 换有效凭据 |

### 403（已认证但无权限 / CSRF）

| 场景 | 表现 | 处置 |
|---|---|---|
| 无 write 权限的路径上传 | 403 `permission denied`（各协议 verbatim 渲染） | 找 admin 加 permission target；组授权即时生效无需重启 |
| 覆盖已有制品但无 delete 权限 | 403 | 覆盖 = 对旧文件的删除，需 delete 权限 |
| **控制台会话 + 跨站 Origin 的写请求** | 403（E-01，CSRF Origin 防线） | 同源页面操作即可；curl/CI **不受影响**（Basic/token 免疫） |
| 非 admin 访问管理面（用户/组/权限/审计/GC/token/仓库列表） | 403 | 管理面恒为 admin-only；**组授予不能提权到 admin**（M4 有意设计） |
| 全局关匿名后的匿名读 | 401 + 挑战头（内容路径）/ 403（个别面） | 提供凭据 |
| npm 同版本重复 publish | 403 `Cannot modify pre-existing version '<v>' ...` | 升版本或先 unpublish |

### 404（不存在 / 有意不做的面）

| 场景 | 表现 | 处置 |
|---|---|---|
| 制品路径不存在 | 404（标准 miss 文案） | 核对路径；**命中仓库 `excludesPattern` 的下载与普通 miss 文案逐字相同**——先查仓配置再怀疑网络 |
| 未实现的端点族 | 404 + E-01：`/api/search/props|users|artifactory|pattern|badge`、`/api/v2/security/permissions/**`、`/api/export/**`、`/api/import/**`、`/api/system/storage/prune/**` | 有意不做（见下文不兼容清单），不是路由故障 |
| `?permissions` 于 virtual/remote 仓 | 400 `only supported on local repositories` | 该视图仅 local 仓 |

### 409（冲突 / 治理拒绝）

| 场景 | 表现 | 处置 |
|---|---|---|
| 路径不匹配仓库模式 | 409，message 含双 pattern：`rejected deployment of '<path>': the path does not match includesPattern '**/*.jar' (excludesPattern 'secret/**').` / `the path matches excludesPattern ...` | 改路径或改 `includesPattern`/`excludesPattern`（excludes 优先） |
| 删除被引用的组 | 409 `Cannot delete group '<g>': it is referenced by permission target(s): <t1>, <t2>. ...` | 先删/改列出的 target |
| GC 与 export/import 撞车 | 409 `gc rejected: export in progress ...`（message 含持锁进程） | 等待当前维护操作完成重试——**不是数据问题** |
| 客户端 checksum 与实测不符 | 409 `Checksum error for '<repo>/<path>': received '<x>' but actual is '<y>'` | 重传一致的构件；或仓配 `server-generated-checksums` |
| maven 仓关 SNAPSHOT/Release | 409 `handling of snapshots is disabled (handleSnapshots=false)` | 改仓配置或换仓 |

### 413（配额超限）

| 场景 | 表现 | 处置 |
|---|---|---|
| 仓库 `quotaBytes` 超限 | 413，message 含 used/quota 双值：`Repository '<repo>' quota exceeded: used 800 of 1024 bytes; the write to '<path>' needs 800 more bytes.` | 删旧腾空间（删后 used 即回落）或调高上限；被拒写**原子**——路径不留半截文件，**同内容重传不受误伤** |
| 五协议客户端侧 | docker `exit 1` + `denied:`；npm `E413`；mvn `status code: 413`；twine `HTTPError: 413` | 同上；每次拒绝落 `quota.exceeded` 审计（含 used/quota） |

## docker login 为什么不走控制台的会话？

控制台会话 cookie 的作用域是 `Path=/binflow`，而 docker 客户端按 Registry spec 固定向**根级 `/v2/...`** 发请求——cookie 结构性送不到。docker 面因此自持认证：`docker login <host>` 走 `/v2/token` 换取 Bearer token（对用户无感）。curl 手工协商与反代直通注意事项见 [Docker 接入](docker-registry.md#token-说明)。

## 高频场景：高 QPS 请用 Access Token

BinFlow 的口令哈希是 **argon2id（memory-hard）**——每请求 Basic 认证都要付出约 **64MB 瞬态内存**的工作集。高并发 Basic 认证（如 CI 集群大量短请求）实测会让服务进程 RSS 显著抬升（100 并发 Basic GET 可到数 GB，事后回落，非泄漏；匿名/Token 路径同负载零增长）。

**建议**：CI、脚本、监控探针等高频客户端一律使用 **API Token（Bearer）**——token 校验不触发 argon2，且天然免疫 CSRF。签发是 admin-only 操作（admin 可指名替目标用户签发，即「给 CI 账号发 token」）：

```bash
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials&username=ci-bot'
# 200 {"access_token":"<64hex>","token_id":...,"expires_in":...,"scope":...}
# access_token 仅此一次明文返回，妥善保存；使用：
curl -s -H "Authorization: Bearer <access_token>" $BASE/binflow/api/v1/storage/usage/<repo>
```

## M4 有意不兼容清单（里程碑级汇总）

从 Artifactory 迁移时的差异点（各域细节见对应指南；M1~M3 清单见 [remote/virtual 管理](admin/remote-virtual.md#m3-有意不兼容清单汇总)）：

| 不做项 | 表现 | 归属 |
|---|---|---|
| 组的 admin 位 | 组只能授 read/write/delete；admin 组成员的非 admin 用户对管理面仍 403 | M4 定案（防组内自提权） |
| `/api/v2/security/permissions/**`（Artifactory v2 权限 API） | 404——BinFlow 权限面是 `/api/v1/permissions` | M4 |
| Artifactory 搜索族（props/users/artifactory/pattern/badge、AQL） | 404——M4 仅 name 子串 + checksum 精确 | M4；gavc/props 后续评估 |
| `/api/system/storage/prune/**` | 404——空间回收走 GC | M4 |
| REST export/import | 404——备份恢复仅 CLI | M4 定案（高危操作带外） |
| 异步 GC 作业 / GC 状态端点 | 同步执行、无 `GET /api/v1/system/gc`（上次运行查审计 `gc.run`） | M4；异步框架 M6+ |
| 审计 CSV 导出 / token 列表 UI / `--tar` 备份单文件 | 控制台不渲染；CLI 显式报未实现 | M4 P2 债务 |
| LDAP/SAML/OIDC 登录、洞察报表、漏洞扫描 | 不做（产品 Non-goal） | 永不 |

## 从 Artifactory 迁移对照表

概念一一对应，术语不变：

| Artifactory | BinFlow | 说明 |
|---|---|---|
| local / remote / virtual 仓 | 同名 rclass 三型 | 语义一致；建仓走 `PUT /api/repositories/{key}`（重复 PUT 为更新） |
| repo key / node / checksum | 同名 | node = 制品节点；checksum 族 sha256/sha1/md5 |
| deployment / resolution | 上传 / 解析 | UI 与文档保留 deployment 原词 |
| permission target、include/exclude patterns | 同名同构 | M4 起 principals 支持 groups；`?permissions` 视图同形（key=主体名、value=r/w/d 字母集） |
| groups / users / access tokens | 同名 | 组删除的 409 保护、`Unable to find group by name '<g>'.` 文案同款 |
| `binflow_session` 控制台会话 | （本产品新增） | server-side session + CSRF Origin 校验；Artifactory 无对应面 |
| System YAML / storage GC / backup | `binflow.yaml` / `POST /api/v1/system/gc` / `export`/`import` CLI | GC 语义（mark-sweep + grace=mtime）同构 |

迁移注意事项（高频三问）：

1. **「我的 404 为什么在 Artifactory 是 200？」**——先查 BinFlow 仓库的 `excludesPattern`（拦截下载与 miss 同文案）与 remote 仓负缓存/assumed-offline（`X-Binflow-Cache` / `X-Binflow-Upstream-Error` 响应头）。
2. **「docker push 为什么报错别的协议都好？」**——docker 固定根级 `/v2`，前置反代必须原样直通（不能 rewrite 进 `/binflow`）；明文 HTTP 需配 daemon 的 insecure-registries。
3. **「脚本 401 但浏览器正常？」**——浏览器是会话 cookie，脚本用 Basic/token；确认没有把控制台 cookie 混进 CI（cookie 过期不受你控制）。

## 排障信息收集

复现问题时附上这三样，基本可以定位大多数故障：

```bash
curl -s $BASE/binflow/api/system/version                     # 版本/修订
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=20"  # 最近审计（谁在何时动了什么）
curl -s -D - -o /dev/null $BASE/binflow/<repo>/<path>        # 完整响应头（缓存/来源/校验和头都在）
```

服务端日志为结构化 JSON（level/msg/字段），无堆栈噪音；升级与已知边界见各指南「有意不兼容」小节。
