# Iteration Report — LOOP 001（DIVERGENT 清偿 + 契约化 0→1 + Phase 1 双设计）

- **Iteration**: 001
- **Date**: 2026-09-11
- **Gap Before**: docker remote 差分 DIVERGENT 12（含 8 项可修族）；contracts/ 空（matrix 契约覆盖 0/193）；C10/C14 语义无设计裁定；UAT 上游凭据缺配；url 尾缀行为 UNKNOWN
- **Goal**: 清偿可修 DIVERGENT 族 + 建立契约体系 + 产出 remote-cache/storage 架构裁定

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L001-1 DIVERGENT 族实现 | dev-registry-adapter | ✅ 7/8 SAME（D21 域边界转票）；Review A APPROVE / Review B 1 blocking 返工后条件清偿（virtual 面传导补测试+声明） |
| L001-2 契约化 0→1 | compatibility-engineer | ✅ 21 条目契约 + normalize 规则 + 2 金样；终态 VERIFIED 15 / DIVERGENT 6（待裁 3 + 缓存语义族 3） |
| L001-3 UAT 凭据 + 尾缀取证 | devops-engineer | ✅ D20 闭环（认证回源 E4+A/B 反证）；尾缀=**parity** → 台账首条 resolved(parity) |
| L001-4 remote-cache-v2 + storage-v2 | architect | ✅ 双设计稿 + ADR-0047/0048 + ADR-0012 勘误三入册 |
| L001-5 D21 url 回显 | dev-go-core | ✅ httpapi 双面修复 + 凭据零泄漏断言 |

## Implementation
- docker adapter：tags/_catalog 上游聚合（≤3 页防环）、token 401 errors 数组、manifest/blob 404 detail 键对齐、禁推 400 三文案、bearerChallenge 合一（service host 回显）、unfound 形态 virtual 面传导钉测试
- httpapi：remoteUpstreamURL 助手（list/detail 顶层 url=configuration.url）
- ADR：DigestChainGate（refs 账本+node 短路）、负缓存 INTENTIONAL（附 Errata 触发义务）、TTL 21600s

## Differential
- L000-docker-remote 批次三轮复合：**SAME 13 / DIVERGENT 5 / 待裁 3 / skipped 1**（SAME 6→13）；pre-fix 基线先行取证后重放；坏密码 docker CLI 渲染 `unknown: Bad Credentials` 逐字；未缓存 tag 拉取成功 digest 两侧一致
- 契约消费：21 条目中 15 条 VERIFIED；残余 6 = 待裁 3（token TTL/匿名/负缓存）+ 缓存语义族 3（C06 头集×2 + C10b 门控）

## Security
- D21 修复附凭据字段零泄漏断言（顶层无 username/password）；SSRF 复筛（分页 URL 过 guard.CheckURL）经 Review A 确认；上游凭据密钥 base64-32 gitignored，认证回源 A/B 反证

## UAT
- :8083 = uat-l0011-f80c46aa；BINFLOW_REMOTE_CREDENTIALS_KEY 在位（master-key 告警恒空）；registry:3 认证上游全链通

## Regression
- 0 出环。Review B 拦下 1 起**无声传导面**（unfound 形态波及 virtual 面零测试）——在环补声明+测试清偿；双审制两连胜（LOOP 000/001 各 1 起）

## Compatibility Score
- Before（LOOP 000 终态）: matrix 行级 ✅57/◐28/❌80/⛔17/超集11；差分面 SAME 6
- After: matrix 行级不变（D12-R01 三列刷新，行态翻正待契约全量重放）；**差分面 SAME 13**；契约覆盖 0→1 份（21 条目，15 VERIFIED）；divergence 台账 10 条目（resolved 3 / BUG 残余 2 族 / UNKNOWN 待裁 4 / 新增 pagination UNKNOWN 1）
- ADR 净增 2 + 勘误 1；unknown 118 条不变（本轮以 divergence 台账分流承载）

## New Divergences / Unknowns
- resolved(parity)：url 尾缀（台账新增闭环态，conductor 裁定）
- 新 UNKNOWN：remote tags/_catalog 分页参数（Review A 发现，LOOP 003 限期）
- 待用户/产品裁定累积 7 项：token TTL/形态、匿名默认、SSRF 立场、HA 链、GCS/Azure、云重定向、C05 布局（另有 xray-curation/插件范围两范围票）

## Fixed Gaps
- docker remote 错误形态/聚合/挑战族 8 项（7 SAME + D21）；D20 UAT 凭据；C02/尾缀两案 resolved；契约体系从零到可执行；C10/C14 架构裁定定谳

## Next Priority（LOOP 002）
1. C10 门控实现票（ADR-0047 DigestChainGate，dev-registry-adapter）
2. 契约 21 条目全量重放 + D21 repo GET 取证闭账（differential-qa）
3. prune/gc 管理面规格票（storage-v2 REFACTOR 前置，reverse-engineer）
4. PRD v1.2/C3 勘误票（FR-11 错误体二分退役，product-manager）
5. 用户裁定清单驱动面（HA/GCS/token TTL 等——待 authority 输入后转票）
