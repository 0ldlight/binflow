# L004-2 — C14/ADR-0048 双臂探针 + 负缓存消失机理定谳

-- Ticket: L004-2（differential-qa-engineer）
-- 日期: 2026-09-11
-- 模式: **dual**（BinFlow UAT http://localhost:8083 vs 参照 :8082 Artifactory-pro 7.161.20）
:- BinFlow 基线: **uat-l0042-c81119b7**（本票按 L003-3 指南重建；镜像纯净性 `go version -m` vcs.revision=c81119b7cd6f658e5cab2045c7d3cd3ebf37ec28，vcs.time=2026-09-11T04:11:29Z；version/healthz/readyz OK）
: 重建注记: daocloud mirror 当窗不可达（manifest inspect 三连 EOF），按「本地 index + PATH shim」喂 phase 3 base digest（alpine 内容同源 3.24；distroless 合成 index 未消费——VARIANTS=alpine）。脚本零改动。
-- 上游: 本地 registry:3（difftest-upstream，127.0.0.1:5588，delete enabled；种子 l004img:seed/:keep = hello-world 单构，manifest sha256:d1a8d0a4…、config e2ac70e7…、layer 4f55086f…）——两实例仓 URL 同指 http://host.docker.internal:5588（同拓扑）
-- 测试仓: 双端各建 l004-docker-remote（remote/docker，missedRetrievalCachePeriodSecs=**60** 加速 TTL 复核——任务书明示的等价配置键，BinFlow 落 repo 行 config JSON，Artifactory 落 repo 配置，均验证回读）；参照侧另建 t1800 复刻 L002-2 条件
-- 客户端: 真实 docker CLI 27.5.1（dind difftest-dind，--insecure-registry 5588/8082/8083）+ curl 8.7.1（--noproxy 隐含本地）
-- 判据: **上游 registry access 行按路径+UA 计数为主**（本地上游 RTT≈20-40ms 与本地负缓存应答不可分——这正是 L000-B 误判机理，见 §1.3）；BinFlow 侧加 sqlite remote_cache 行直证（容器卷 db+WAL 快照拷贝）
-- 凭据: 命令行内使用，本报告脱敏；测试资产用后删净（容器/仓/临时文件）

## 0. 结论速览

| 问题 | 答案 |
|---|---|
| BinFlow manifest 冷 miss 负缓存是「消失了」吗 | **从未存在**——T-363（2026-08-30）诞生起 404 臂的写就只在 standing（有本地过期副本）臂；冷 tag/digest miss 从不写行。无任何 commit 删除过它（§1） |
| L000-B「重复 miss 40-70ms 本地」是什么 | 本地上游 RTT 被误读为本地负缓存（本轮实测：BinFlow 每次回源的 miss 也是 30-36ms；参照侧本地负缓存应答 21-27ms——同量级，时延不可分） |
| Artifactory 参照侧负缓存到底有没有 | **有，且双面生效**：manifest tag miss（已知 image）1 次上游探查后本地负缓存；blob 链内 404 同理；均按 missedRetrievalCachePeriodSecs 到期后回源（跨 TTL 复核通过，§2.1/§2.4） |
| L002-2「双端 3/3 回源」复现吗 | **不复现**。贴齐条件（本地 registry:3 上游、TTL=1800、真实 docker CLI）重测参照侧 = **1/3 上游**（§2.1 t1800 行）。差异归因存疑列入 Risks（实例已重启，无法回放当时状态） |
| 三选一 | **C 变体（非回归的 BUG 缺口）**：manifest 冷 miss 面未兑现自家 missedRetrievalCachePeriodSecs 承诺，而参照兑现了 → ADR-0048 的 Errata 触发条件成立（「若复测翻转（Artifactory 实有负缓存），本 ADR 自动作废、C14 按对齐收」），按对齐收 = 开实现票补 manifest 冷 miss 负缓存（§3） |

## 1. 代码定谳（先于探针，决定预期）

### 1.1 CacheRemoteMiss 全历史（git log -S，internal/ 全域）

| commit | 日期 | 内容 |
|---|---|---|
| 05acd91a (T-66) | 早期 | **通用引擎** fetcher（internal/remote/fetcher.go）的负缓存写诞生：404 无条件 PutCache（fetcher.go:986-990）。该面服务于 path 型 remote，**不是 docker /v2 链** |
| d2746341 (T-363) | **2026-08-30** | **docker /v2 remote 链诞生**（internal/adapter/docker/remote.go +1073 行）：CacheRemoteMiss 恰好两处——manifest 臂（引入时 L669/现 L967）与 blob 臂（引入时 L894/现 L1291）。此后 `git log -S CacheRemoteMiss -- internal/adapter/docker/remote.go` **再无任何 commit 触及**（仅 af2f6c02/c27713d9 动测试与 repo 面实现） |
| ea2e5ebe (ADR-0047/C10) | 2026-09-11 | blob 冷路径加链门（blobChainAdmits）：链外 digest 本地 404 零上游**不写行**（确定性）；链内照旧 |
| 1af6af36 / 615d379d / 7c2f8666 | 2026-09-11 | 仅 wire 前缀/错误族/face 头族——逐 diff 复核未动 404 臂守卫（唯一触及为 L003-2 给 serveRemote*Copy 加 face 参数） |

### 1.2 HEAD（c81119b7）两臂的确定行为

- **manifest 臂**（remote.go:963-972）：上游 404 时 `if standing != nil && standing.node != nil` 才 `CacheRemoteMiss(manifestNodePath(image, standing.dgst))` 并 STALE 续命；**standing==nil（冷 miss：tag 无本地行、digest 无本地副本）→ `unfound.write` 直接 404，不写任何负缓存行**。注释与 repo 面 API doc 一致：*"Digest-keyed paths only — a TAG miss has no storage path to key and simply answers 404 (helm.md 8.3)"*。
- **blob 臂**（remote.go:1289-1296）：上游 404 时 `CacheRemoteMiss(blobNodePath(image, hex))` **无条件**（standing 有无都写）；冷路径先过 ADR-0047 链门（链外本地 404 不进门、零上游、不写行）。
- **TTL 源**：`remoteMissedTTL` 读 repo 行 config JSON 的 `missedRetrievalCachePeriodSecs`（缺省 1800，0 视同缺省）→ 本票测试仓显式 60。

### 1.3 「消失」机理定谳

**消失是幻象**：manifest 冷 miss 的负缓存在 docker /v2 面**自 T-363（2026-08-30）起就不存在**，无 commit 增删过该路径的写。L000-docker-remote-diff C14 的「BinFlow 首次 miss ~200ms → 重复 miss 40-70ms 本地负缓存」判读依据是时延，无缓存行/上游日志证据；本轮实测证明该时延区间与本地上游单次 RTT 完全重叠（BinFlow 每次回源 30-36ms；wire-bug 时代首次含 Bearer dance ~200ms、后续 token 复用单 GET 40-70ms——逐次回源完全解释 L000-B 观察）。L002-2 以日志计数观察「BinFlow 3/3 回源」与本代码事实一致。

## 2. 双臂活体矩阵（每臂 ×3 连发，判据=上游 access 行计数[按 UA 区分] + BinFlow sqlite 行）

### 2.1 manifest-404 臂（随机 tag miss，已知 image）

| 子臂 | BinFlow :8083（TTL=60） | Artifactory :8082（TTL=60） |
|---|---|---|
| curl ×3（tag l004miss-*） | **3/3 回源**（每次 1 条 binflow-remote GET；36/30/30ms；404 MANIFEST_UNKNOWN compact） | **1/3 回源**（try1 一条 Artifactory UA HEAD→404；94ms）+ try2/3 **本地**（21/27ms，零上游） |
| 真实 docker CLI（1 pull = HEAD+GET） | **2 次回源**（HEAD 与 GET 各一条 binflow-remote） | （t1800 仓）CLI ×3 pull：**1/3 回源** |
| TTL=1800（t1800 仓，复刻 L002-2 条件） | —（代码同型，无 TTL 分支） | CLI ×3：**1/3 回源**——L002-2 的 3/3 不复现 |
| 60s 过期复核 | **无行可过期**（sqlite 零负缓存行；仅 seed 落地行 l004img/manifests/d1a8d0a4… kind=content） | m1 过期后请求**回源 +1**（04:22:40 写 → 04:24:29 复问）→ 负缓存条目按 missedTTL 到期 |
| sqlite 行（BinFlow） | tag 路径**零行** | —（无对等观测面，以上游计数判） |

**子臂 M-b（image 名不存在，:latest）**：BinFlow **3/3 回源**（30-32ms）；Artifactory **0/3 回源**（48/22/25ms，含 try1 零上游——对未知 image 名确定性本地 404）。→ 相邻新分歧（非 C14 本体，§3.3）。

### 2.2 blob-404 臂·链外随机 digest（ADR-0047 边界验证）

| 项 | BinFlow :8083 | Artifactory :8082 |
|---|---|---|
| 随机合法 digest ×3 | **0/3 回源**（22-26ms 本地 404 BLOB_UNKNOWN compact；sqlite 零行） | **0/3 回源**（24-111ms 本地 404 pretty；marker 语义） |

→ **SAME**：双端对「无链成员 digest」均确定性本地应答、零上游、零行。ADR-0047 门控边界活体验证通过。

### 2.3 blob-404 臂·链内但上游 404（构造：rm 上游 layer blob 文件 + 重启 registry 清描述符缓存；manifest 完好）

digest = sha256:4f55086f…（seed manifest 命名的 layer，已随 manifest 落地入链；两实例均**未**曾拉过该 blob——无 standing 副本）。

| 项 | BinFlow :8083（TTL=60） | Artifactory :8082（TTL=60） |
|---|---|---|
| try1 | 404（38ms）；上游 +1 binflow-remote GET→404；**负缓存行落地**：`l004img/blobs/4f55086f… \| negative \| fetched 07:44:24Z \| expires 07:45:24Z` | 404（41ms，pretty）；上游 +1 Artifactory GET→404 |
| try2/try3 | 404（22/26ms）**零上游**（RemoteProbeNegative 本地应答） | 404（20/16ms）**零上游** |
| 60s 过期复核 | 行到期后请求**回源 +1** 且**行重写**（fetched 07:45:35Z → expires 07:46:35Z） | **回源 +1**（同型） |
| 真实 docker CLI | pull l004img:seed：manifest HIT + config 落地（上游 +1）+ **layer 被活跃负缓存行本地拦截**（“unknown blob”，layer 上游计数冻结）——端到端客户端可见 | —（t1800 CLI 腿已覆盖 miss 面） |

→ **SAME**：blob 面（链内上游 404）双端负缓存**均生效且按 missedTTL 到期回源**——`missedRetrievalCachePeriodSecs` 配置承诺在双端 blob 面均兑现。

### 2.4 双端逐臂总表

| 臂 | BinFlow 上游/3 | Artifactory 上游/3 | 判定 |
|---|---|---|---|
| M-a tag miss（curl） | 3 | 1 | **DIVERGENT**（参照有负缓存，BinFlow 无） |
| M-a tag miss（docker CLI，t1800） | 2（1 pull） | 1 | 同上 |
| M-b 未知 image 名 | 3 | 0 | **DIVERGENT**（新发现，§3.3） |
| B1 链外随机 digest | 0 | 0 | SAME（双端本地确定性） |
| B2 链内上游 404 | 1 | 1 | SAME（双端负缓存+TTL 过期） |
| 过期复核（有行侧） | 回源+重写 | 回源 | SAME（missedTTL 语义双端一致） |

## 3. 三选一建议（conductor 裁）

**建议：C 的精确变体——BUG 缺口成立，但非回归**。理由：

1. **A（parity + ADR-0048 闭）不成立**：M-a 面双端行为明确分歧（3/3 vs 1/3 回源），M-b 亦分歧。「双端一致」仅在 B1/B2 成立。
2. **B（维持 ADR-0048 INTENTIONAL）前提颠倒**：ADR-0048 的两个事实前提本轮均被推翻——①「Artifactory 运行时未生效」：实测双面生效、跨 TTL 到期复核通过（其自带 Errata 触发条件「若复测翻转（Artifactory 实有负缓存），本 ADR 自动作废、C14 按对齐收」**已触发**）；②「BinFlow 侧负缓存生效（现状即目标行为）」：manifest 冷 miss 面从未有过（§1.3），L000-B 的正向观察是时延误读。
3. **C（真消失且系回归）半成立**：行为缺口真实存在（BinFlow manifest 冷 miss 面未兑现自家 `missedRetrievalCachePeriodSecs` 承诺——键名与默认值两侧同名同值），但「消失/回归」不成立（T-363 诞生即如此，无行为倒退）→ 裁 BUG 而非 REGRESSION，开**实现票**：manifest 冷 miss 臂补负缓存行写入（对齐参照：missedTTL 窗口内重复 tag/未知 digest miss 本地 404 零上游）。设计注意：现实现注释自称「TAG miss has no storage path to key」——参照显然有键法（其 404 错误体 Path: `l004img/seed/sha256__4f55086f…` 透露其内部路径布局），键法设计归 dev/compatibility-engineer。
4. **B1/B2 面维持现状**：链外门控与链内负缓存双端 SAME——ADR-0047 边界活体验证通过、blob 面负缓存兑现承诺，均无需动。

### 3.1 翻态建议（known-divergence C14 / 契约 #16）

- `docker/remote-manifest-404-negative-cache`：UNKNOWN → **BUG**（authority 可引本报告 + ADR-0048 Errata 触发事实）。
- 契约条目 `docker/remote-manifest-404-no-negative-cache` 的 expect/timing 断言需随实现票翻回「窗口内本地负缓存」语义（当前断言「与双端现状一致」基于本轮已证伪的 L002-2 观察）。

### 3.2 ADR-0048 处置建议

按其 Errata 条款追加作废声明（不静默删改）：复测翻转已发生（Artifactory 实有负缓存，双面+跨 TTL），决策 B 的前提失效；C14 转对齐路径（实现票）。INTENTIONAL 登记随之撤销。

### 3.3 相邻新分歧（建议单列票裁定，勿并入 C14）

- **M-b 未知 image 名**：Artifactory 本地确定性 404（零上游）vs BinFlow 每次回源。参照语义近于「catalog/marker 未知名不外问」；与 ADR-0047 的 digest 链门同族但作用在 manifest/name 面。裁 INTENTIONAL（对齐）或 BUG（对齐参照）属 conductor。

## 4. 与前轮报告的对账（回归对照门）

| 前轮结论 | 本轮 | 处置 |
|---|---|---|
| L000-diff C14「BinFlow 负缓存生效（40-70ms 本地）」 | 证伪（时延误读；代码+活体双证从未存在） | 账实不符项，随 C14 翻 BUG 一并闭 |
| L000-evidence E6-3「Artifactory 6.6s 每次回源、配置未见生效」 | 本地 registry:3 上不复现（1/3）；6.6s 量级疑当时 docker.io 慢上游 + 无日志计数佐证 | 前轮观察降级为「当时条件未控」 |
| L002-2 #16「双端 3/3 回源趋同」 | BinFlow 3/3 复现✓；Artifactory 1/3 不复现（贴齐条件：同上游型/TTL=1800/真实 CLI） | 上报：L002-2 参照侧观察无法复现（实例当日重启过，历史状态不可回放；本轮为第一手双证据） |

## 5. 复现命令骨架（凭据脱敏）

```bash
# 上游：registry:3 + 种子（hello-world 单构）+ 删 layer 文件后 restart 清缓存
# BinFlow 仓：PUT /binflow/api/repositories/l004-docker-remote {rclass:remote,packageType:docker,url:http://host.docker.internal:5588,missedRetrievalCachePeriodSecs:60,allowPrivateUpstream:true}
# Artifactory 仓：PUT /artifactory/api/repositories/l004-docker-remote 同型
# 计数：docker logs difftest-upstream 2>&1 | grep -c "^172.*<PATH> HTTP.*<UA>"
# 行证据：docker cp binflow-ga:/var/lib/binflow/binflow.db{,-wal,-shm} 快照 → sqlite3 "SELECT path,kind,fetched_at,expires_at FROM remote_cache WHERE repo_key='l004-docker-remote'"
# M-a：curl -u … -H 'Accept: …manifest.v2+json,…' :8083(8082)/v2/l004-docker-remote/l004img/manifests/l004miss-<rand> ×3
# B2：GET …/blobs/sha256:4f55086f… ×3 → 60s 后再 1 次
```
