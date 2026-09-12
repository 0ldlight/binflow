# Iteration Report — LOOP 002（C10 门控落地 + 契约全量重放 + 管理面规格 + 产品勘误）

- **Iteration**: 002
- **Date**: 2026-09-11
- **Gap Before**: C10 语义分歧（盲代理 vs marker 门控）BUG 态；契约 21 条目未全量重放（6 VERIFIED 停留于点验）；prune/gc 管理面零规格；PRD C3/FR-11 与 as-built 漂移；9 项产品裁定无结构化清单
- **Goal**: C10 按 ADR-0047 落地 + 契约重放全量出证 + storage 管理面规格 + 产品面追认与待裁成表

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L002-1 C10 门控实现 | dev-registry-adapter | ✅ 双审 APPROVE（A correctness 0 blocking / B architecture 0 blocking） |
| L002-2 契约全量重放 | differential-qa-engineer | ✅ 21/21（SAME 12 / DIVERGENT 8 / in-flight 1）+ D21 闭账 + 金样 live-captured |
| L002-3 prune/gc 规格票 | reverse-engineer | ✅ 10 端点 + 26 字段 status schema + BinFlow 差异 10 条 + U-STG-20~25 入队 |
| L002-4 PRD 勘误 + 待裁表 | product-manager | ✅ 勘误 C8 + pending-rulings.md（9 项带建议立场） |

## Implementation（L002-1）
- `repo.DigestChainGate` optional facet（direct 臂 + virtual 成员臂；compile-time pin，先例同构）；metadata `BlobInImageChain` 三键精确 EXISTS（PK covering 零迁移——EXPLAIN 双重复现）
- **伴生根因修复**：`remoteManifestRefs` 空 Content-Type 恒 parse 失败——refs 账本自始未写入（潜伏缺口被门控暴露）；mediaType 线程化后 tag/digest/HEAD/virtual/helmoci 全落地路径覆盖
- 拒绝路径零上游接触、不写负缓存行（ADR-0048 交界测试钉死）；fail-open 放行臂过既有权限门（无越权放大，双审确认）

## Differential
- C10 复验（差分直证）：链外 digest 本地 404 4.5~15.9ms **上游日志零请求**；链内未缓存 200 代取；manifest 拉取后门开；真实 docker CLI pull 成功、rmi 重拉 0.157s 零上游
- 契约全量重放：SAME 12 / DIVERGENT 8（待裁 3：token TTL/匿名/负缓存；缓存语义族 C06×2；#1 ping charset / #2 ping 坏凭据臂 / #11 blob GET 头集 新显性）/ in-flight 1（C10 由 L002-1 自验 VERIFIED）
- 分页取证：Artifactory remote 面尊重 n/last、无效 n=404 pretty——BinFlow 无视，实锤分歧（LOOP 003 实现票）

## Security
- 盲代取面（任意 digest 打上游）收敛为链内闭集；SSRF 零新面；参数化 SQL；凭据零泄漏（金样/报告 grep 过）

## UAT
- :8083 = uat-l0022-5f9c48d0（重放基线）；C10 验证在 uat-l0021-5f9c48d0 完成；LOOP 003 开环时重建至已提交树

## Regression
- 0 出环。四包+波及面 16 包 -count=1 全绿（双审独立复跑）；race 子集绿
- 双审轮次战绩 3/3（LOOP 000/001/002 各拦 1 起在环问题）

## Compatibility Score
- Before: 差分 SAME 13 / 契约 VERIFIED 15 / divergence resolved 3
- After: **差分 SAME 13 + C10 VERIFIED（契约 16/21）**；divergence resolved 5（+C10、+D21）；新增 UNKNOWN 6（U-STG-20~25 + pagination 实锤待实现）；storage 管理面从零规格到 10 端点全规格（BinFlow 差异 10 条入账，LOOP 003 实现票）
- 产品面：PRD C8 勘误 + 9 项待裁成表（docs/prd/pending-rulings.md）

## New Divergences / Unknowns
- DIVERGENT 新显性 3（#1/#2/#11——并入 C06 头集票族）；分页实锤；U-STG-20~25（compress 活体/backup 信封/Prune 参数键/UI 鉴权门/gc 并发等）
- 环境事件：参照实例宿主 DHCP 换网段宕机（写死节点 IP 失效）——恢复性修复在案（备份 system.yaml.bak-l0023）

## Fixed Gaps
- C10 marker 门控语义（P0 级架构 gap，ADR-0047 落地）；D21 双面闭账；refs 账本潜伏缺口；PRD/代码口径对齐（C8）

## Next Priority（LOOP 003）
1. prune/gc 管理面实现票（10 端点族，202 异步+status 报告——差异 10 条清偿，dev-go-storage）
2. docker remote 面头集+分页票族（C06/#11/#1/#2 + n/last 透传 + Review A 微票三件——dev-registry-adapter）
3. remote-cache 引擎 §8 B/C/D（21600s TTL 接线/上游 304 透传/Invalidate 连带删 refs——dev-go-storage 串行接 1）
4. MinIO 云链取证（解 R-6 依赖 U-STG-15/09）+ UAT 重建至已提交树（devops 合并票）
5. 用户裁定批处理（9 项待裁表在手——批准即驱动 ADR/台账/实现三线）
