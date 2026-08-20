# 评审报告 T-95（视角: correctness + concurrency）

- ticket: T-95 includes/excludes + quotaBytes enforcement + usage 端点（commit `13bc7f3`）
- reviewer: code-reviewer；日期 2026-08-20
- 结论: **REQUEST_CHANGES**（blocking 1，non-blocking 6）

## 必须修改（blocking）

### B1 — 声明 checksum 的幂等重传在配额边界被误拒 413（预检 delta 与计量 delta 背离）

**位置**
- `internal/repo/service.go:378-382`（PutWithOptions 流式臂）
- `internal/repo/service.go:534-538`（PutFromBlob）
- `internal/repo/service.go:621-625`（PutLandedBlob）
- 根因注释：`internal/repo/governance.go:304-317`（checkQuota 对 `replaced` 语义的描述写反）

**问题**：三处 `replaced` 的计算均为

```go
replaced := int64(0)
if !idempotent && existing != nil {
    replaced = existing.Size
}
```

幂等重传（declared sha256 == 既有节点 sha256，`isIdempotentRedeploy` 判真）时 `replaced` 被置 0，于是预检 `delta = incoming - 0 = 全量`；而同事务计量层（`usageAdjustPutStmt`）真实发生的 delta 是 `incoming - 既有行 size = 0`。checkQuota 自己的注释（"replaced … 0 when … a same-content retransmit, which changes nothing and is exempt"）恰好把方向写反：`replaced=0` 使 delta 最大化，绝不豁免。

**失败场景（已在本地用一次性注入用例实测复现，跑完即删，树未改动）**：quota=800 的仓已持有 `a.bin`(800B)（used=800 恰在顶）。任何「同内容同路径 + 声明校验和」的写入全部 413：

1. 带 `X-Checksum-Sha256` 的同内容 PUT 重传（`expect.Sha256` 非空）→ 413；
2. X-Checksum-Deploy 秒传对同路径同 blob 的重宣告（PutFromBlob，`ref.Sha256` 恒声明）→ 413；
3. docker finalize / 重推同 digest（PutLandedBlob，digest-keyed 路径天然幂等）→ 413；manifest step-1（`internal/adapter/docker/manifest.go:218` 恒带 `Sha256`）重推同 digest 镜像 → 413。

对照：**不带**声明校验和的裸 PUT 重传走 overwrite 路径（`idempotent=false` → `replaced=800` → delta 0）→ 放行。同一操作是否被拒取决于客户端是否声明 checksum，且每次误拒还落一条 `quota.exceeded` 审计 + WARN（观测噪声）。叠加已登记的 manifest 双计数（usage 常高于直觉值），配额仓上的 docker 重推/CI 重试会大面积误拒。

**违反**：repo-semantics §3 幂等重传契约（同 checksum 跳双门）、工作日志自述「幂等重传豁免（同内容同路径 delta=0 不 413）」、以及预检与同事务计量的一致性。

**现有测试为何没抓住**：`internal/repo/governance_test.go:323-328` 的 "idempotent retransmit at the ceiling" 传入空 `storage.BlobRef{}`（未声明 sha）→ `isIdempotentRedeploy` 返回 false → 实际测的是 overwrite 豁免臂，不是声明路径。httpapi 侧同样无覆盖。

**修复建议**：三处把 `!idempotent && existing != nil` 改为 `existing != nil`（幂等 ⇒ `existing.Size == incoming` ⇒ delta 0 ⇒ 复用现有 `delta <= 0` 短路豁免，零额外 store 读）；或显式 `if idempotent { 跳过 checkQuota }`。同步修正 governance.go checkQuota 的注释。补三臂回归：带 `expect.Sha256` 的重传、PutFromBlob 重宣告、PutLandedBlob 同 digest（可直接采用本次评审的复现用例形态）。

## 建议改进（non-blocking）

1. **同事务回滚测试的注入点在方法入口**：`fakes_test.go` 的 `hookUsage.PutNodeWithUsage` 在委托前 check，失败发生在 `BeginTx` 之前——「adjust 已执行、node upsert 失败」的中途窗口未被覆盖。回滚正确性由单事务 + defer Rollback 的结构保证（代码正确），但测试证明力弱于工作日志声称；建议后续加语句级 seam（失败 tx 内第二条语句）。
2. **`repo.AuditActionQuotaExceeded` 字符串字面量复刻**了 T-93 已导出的 `audit.ActionQuotaExceeded`（`internal/audit/api.go:110`，值一致）。T-93 已先行落地，「audit 词表是 T-93 area」的理由过时；建议改为 alias（与其余 action 常量同款），防漂移。
3. **parseGovernance 每请求 Unmarshal**：Get/Put 每次都解析 config JSON（纯 CPU、无 store I/O，「默认仓零额外 I/O」的断言成立）。若压测出现热点，可按 repo 行缓存已解析 governance。
4. **architecture §4.6 键名写作 `quota_bytes`**，实现/PRD/REST 均为 `quotaBytes`；建议随已登记的 §4.6 remote 口径勘误一并收口（转 architect）。
5. **DeleteRepo(deleteContent=true) 的计量兜底**：node 清理走未计量 `DeleteByPrefix`，repo_usage 依赖 repositories 行删除的 FK `ON DELETE CASCADE`（004 DDL 已建，终态正确）；node 清理与行删除之间失败会留下短暂虚高的 counter（重试自愈）。建议留注释或后续票显式化。
6. **`governance.defaulted()` 生产未接线**（仅 internal_test 引用）：真正的默认短路由 allowsPath/checkQuota 内部结构承担，「默认」有两处定义，建议后续接线或删除。

## 已核实无问题的关键面（取证记录）

- **匹配器同构复刻零漂移**：`govMatcher` 与 `auth/pathMatcher` 重命名后机械 diff，可执行语句逐行一致（`**` 回溯、`dir/**` 含目录自身、matchStart 仅 folder 形态、`pi>0` 门、Ant token trim）。
- **UsageStore 并发红线**：两个写方法首条语句即写（delta 标量子查询在 INSERT 内部，事务以写开），无读→写升级，符合 store.go「every tx opens ON a write statement」不变量；`Usage().Get`/`checkQuota` 读均在事务外；`nodeUpsertStmt` 与 NodeStore.Put 逐字节一致；Delete 契约（ErrNodeNotFound + 回滚零 delta adjust）对齐。
- **挂点完备**：Put 四族全部过门（pattern 在 resolveWriteRepo 之后、drain body 之前；quota 在 size 已知后、元数据写之前）；docker finalize→PutLandedBlob（uploads.go:638）、mount→PutFromBlob（uploads.go:542）、manifest step-1→svc.Put（manifest.go:218）；未发现绕过全部挂点的写路径（remote 引擎写不计量为票面 Q2 裁决；heal 支路已用计量删）。PutManifest 不查配额的例外论证成立（双 node 布局 + step-1 已过门，反例不存在：blob 面每字节都有界）。
- **413/409 原子性**：拒绝分支均在任何元数据写之前；流式臂 blob 留 GC 为声明行为；测试断言 node/counter/ledger/索引/tag 零残留。
- **005 回填**：幂等（schema_migrations + ON CONFLICT DO NOTHING）、升级形态（回退 version 行→重开按 SUM 回填→其上继续计量→三开不翻倍）测试真实；文件体无事务语句，与 migrator 包裹约定一致。
- **harness 接真 audit**：与 cmd 装配形态一致；httpapi 全包通过，nil→true 只收紧（此前根本不记事件），无既有断言语义变化。docker 两个测试桩为纯机械补齐。
- **门序合规**：pattern 门在权限对之前（repo-semantics §2 步 2/3）、配额在覆盖检查后（步 5）、virtual 写按目标 local 仓判定、Get 仅 local 分支且在 ACL 后 404 逐字同形。
- **复跑验证**：`go build ./...`、`go vet`（repo/metadata/httpapi/adapter/docker）、`go test -count=1` 四包、`-race` T-95 测试面全部 ok。

## 范围外发现

无新增（已知遗留五项按登记处理，未重复上报）。
