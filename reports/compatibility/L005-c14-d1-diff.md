# L005 — C14+D1 修复后全矩阵重跑 + C14 两形态新证（U-PROTO 面）

- Ticket: L005-1（dev-registry-adapter 实现票；修后重跑为台账 D1 review_gate 硬要求）
- 日期: 2026-09-11/12
- 模式: **dual**（BinFlow UAT http://localhost:8083 vs 参照 :8082 Artifactory-pro 7.161.20）
- BinFlow 基线: **uat-l0051-792c347c**（792c347c + 工作树〔C14+D1 未提交改动〕；按 L003-3 §2 指南重建。纯净性：release 二进制 `go version -m` vcs.revision=792c347c…、vcs.modified=true——工作树代码确在镜像内；version/healthz/ui 200 smoke 通过。构建注记：daocloud 镜像源间歇 EOF（L005-2 同症），ALPINE_SRC/DISTROLESS_SRC 以 canonical 源覆盖绕过，脚本零改动）
- 参照: :8082 运行时 7.161.20（未动）
- 上游: 全新 registry:3（l0041-upstream，127.0.0.1:5591），种子按 digest 精确复刻 L004-1 原始内容：busybox@sha256:1cfa4e2b…（docker.io 按 digest 拉取后重推，manifest 610B / config c6348fa8… 459B 与原轮逐字节同 digest）——304-matrix.sh 硬编码的 MFD/CFGDIGEST 均有效
- 测试仓: 双端各 3——l0041-a-remote（默认检索窗，304 矩阵）、l0041-b-remote（retrievalCachePeriodSecs=60 双端回读验证，过期窗矩阵）、l0051-c14-remote（missedRetrievalCachePeriodSecs=60 双端回读验证，C14 新证）。**用后已删净**（双端回读零残留；BinFlow 侧非空仓按 deleteContent=true 清）
- 客户端: 真实 docker CLI 27.5.1（dind difftest-dind，host:port 形 insecure 表）+ curl 8.7.1（--noproxy；Basic 直连）
- 判据: status / body 指纹 / 归一头集（norm 规则同 L004-1）/ 上游 access 行计数（本次 grep 锚定 `^[0-9.]+ - - ` 访问行，排除 registry:3 debug/trace 噪声行——L004-2 的计数法教训吸收）

## 0. 结论速览

| 问题 | 答案 |
|---|---|
| D1（unquoted INM）修复后 304 矩阵 | **20/20 臂全 SAME**（status+body+上游 delta 全 0）——原 DIVERGENT 三臂 M2/M2c/M2d 全部收敛（§1） |
| M6/M2b 偶合是否翻面（review_gate 问题） | **未翻面**：M6 304/304、M2b 200/200 维持 SAME——M6 的 304 由「日期臂偶合」变为「按值匹配直判」，可观察面不变（§1） |
| 过期窗矩阵回归 | 6/6 客户端可见 SAME；X3/X4 上游计数 0 vs 1 = 既有 D3（待裁，非本票面）维持；§8-C 透传直证行（BinFlow 条件 GET→上游 304）保留（§2） |
| C14 M-a（tag miss） | **双端 1/3 回源同形**（BinFlow try1 GET、参照 try1 HEAD；try2/3 本地）——L004-2 的 BinFlow 3/3 分歧关闭（§3.1） |
| C14 missedTTL 到期 | 双端同语义：60s 到期 → 回源 +1 → 行重写 → 再冻结（sqlite 行直证，§3.2） |
| C14 M-b（未知 image 名） | **双端 1/3 回源同形**——**L004-2 的参照侧 0/3 观察本轮未复现**（干净专属仓 + catalog 聚合后两态均为 1/3）：参照对未知名同样首问一次（HEAD）后本地，与 M-a 同形（§3.3） |
| 真实 CLI（miss 面） | docker CLI 拉缺失 tag：上游恰 1 往返（HEAD 落行、GET 本地应答），客户端渲染 manifest-unknown 错误族（§3.4） |

## 1. 新窗口 304 矩阵（l0041-a-remote，20 臂，双端对照，round 2）

判据维度：status / body sha256 / 归一头集 / 上游 access 计数。round 1（暖机前）与 round 2（暖机后）双轮连跑，round 2 全臂稳定。

| 臂 | 条件 | 参照 :8082 | BinFlow :8083 | 判定 |
|---|---|---|---|---|
| M8 | GET 无条件（对照） | 200/610B | 200/610B 同 digest | SAME |
| M1 | GET + INM 带引号匹配 | 304 裸头 | 304 裸头 | SAME |
| **M2** | GET + INM 不带引号匹配 | 304 | **304（原 200 → 翻面收敛）** | **SAME** |
| M3 | GET + IMS(=LM) | 304 | 304 | SAME |
| M4 | GET + 带引号非匹配 | 200 | 200 | SAME |
| M4b | 带引号非匹配 + IMS 匹配 | 200 | 200 | SAME |
| M5 | 带引号匹配 + IMS 1970 | 304 | 304 | SAME |
| **M6** | 不带引号匹配 + IMS 匹配 | 304 | 304（原日期臂偶合 → 今按值匹配直判；结果不翻） | SAME |
| M2b | 不带引号非匹配 | 200 | 200 | SAME |
| **M2c** | 不带引号非匹配 + IMS 匹配 | 200 | **200（原 304 → 翻面收敛：unquoted 在场封死日期臂）** | **SAME** |
| M7 | HEAD + 带引号匹配 | 200 | 200 | SAME |
| M1d | by digest + 带引号匹配 | 304 | 304 | SAME |
| **M2d** | by digest + 不带引号匹配 | 304 | **304（原 200 → 翻面收敛）** | **SAME** |
| B7 | blob GET 对照 | 200/459B | 200/459B 同 digest | SAME |
| B1 | blob + INM 带引号匹配 | 304 完整 artifact 面 | 304 同头集 | SAME |
| B2 | blob + INM 不带引号匹配 | 304 | 304 | SAME |
| B3 | blob + IMS | 304 | 304 | SAME |
| B4/B5 | blob 非匹配（带/不带引号） | 200/200 | 200/200 | SAME |
| B6 | blob HEAD + 匹配 | 200 | 200 | SAME |

- **上游计数：20 臂双端全部 0 往返**（round 2）。
- M2/M2c/M2d 三臂的归一头集双端 diff 为空（逐头一致）。
- round 1 的 B7 臂 BinFlow 曾 +1：归因＝本轮播种捷径（host docker 仓内已有 busybox 层，daemon 跳过 blob 下载，BinFlow 侧 blob 缓存从未暖过），暖机后归零——非代码行为，登记为操作注记。
- **D1 定谳复验**：`manifestClientNotModified` 弃用「仅带引号解析」，两读面统一为按值不区分引号匹配 + 任意 INM 在场封死日期臂——与参照 M2/M2c/M2d 三臂活体一致。

## 2. 过期窗矩阵（l0041-b-remote，retrievalCachePeriodSecs=60 双端回读确认）

| 臂 | 参照 :8082 | BinFlow :8083 | 客户端可见 | 上游计数 |
|---|---|---|---|---|
| SEED | 200/610B；上游 +1 | 200/610B；+1 | SAME | 1v1 |
| SEEDB | 200/459B；+2（冷取双 GET） | 200/459B；+1 | SAME | 2v1（N2 既有 note） |
| X1 | 200 全量；上游 **HEAD 探查 ×1** | 200 全量；上游**条件 GET ×1 → 304（0 字节）** | SAME | 1v1（N1 形状 note） |
| X2 | 200 全量（重验证不消费客户端条件） | 200 全量（同姿） | SAME | 1v1 |
| X3 | 304 本地，上游 0 | 304，上游 GET ×1 | SAME（304） | 0v1（D3 既有分歧） |
| X4 | 200 本地，上游 0 | 200，上游 GET ×1 | SAME（200） | 0v1（D3 既有分歧） |

- §8-C 透传直证保留：BinFlow X1/X2 上游行 `GET …/manifests/t1 304 0 UA=binflow-remote/1.0`；参照同臂为 HEAD 200 探查（N1）——两形状与 L004-1 完全一致，本票改动未触碰过期窗流。
- X3/X4（D3：过期 blob 回源 vs 本地）维持原样，裁定权在 conductor，非本票面。

## 3. C14 两形态新证（l0051-c14-remote，missedRetrievalCachePeriodSecs=60 双端回读确认）

### 3.1 M-a：tag miss（已知 image，随机 tag）

| 端 | try1 | try2 | try3 | 上游/3 |
|---|---|---|---|---|
| 参照 :8082 | 404 / 21ms / **上游 HEAD ×1** | 404 / 7ms 本地 | 404 / 7ms 本地 | **1** |
| BinFlow :8083 | 404 / 508ms（含会话建连）/ **上游 GET ×1** | 404 / 22ms 本地 | 404 / 28ms 本地 | **1** |

→ **SAME**。L004-2 的 BinFlow 3/3 回源分歧**关闭**（实现：冷 miss 负缓存行写入 + 二问起本地应答）。404 体两端同为 MANIFEST_UNKNOWN（detail=IMAGE 路径，E6 形态）。

### 3.2 missedTTL 到期复核（60s 窗）

60s 窗到期后复问：双端各 **回源 +1 并重写行**（BinFlow sqlite 行直证：`l0041/busybox/tags/l0051miss-b | negative | 19:08:53Z → 19:09:53Z`——重写时刻恰为复问时刻、窗长恰 60s）；复问后再问，计数冻结。**到期回源 + 重写 + 再冻结双端同语义**。

### 3.3 M-b：未知 image 名（:latest）

| 态 | 参照 :8082 | BinFlow :8083 | 上游计数归属 |
|---|---|---|---|
| 干净仓首测（l0051nope ×3） | try1 404 / **上游 HEAD ×1**；try2/3 本地（7ms） | try1 404 / **上游 GET ×1**；try2/3 本地（22ms） | 双端各 1（SAME） |
| catalog 聚合后再测（l0051nope2 ×2） | try1 仍上游 HEAD ×1；try2 本地 | —（同形态已证） | 参照 1 |

→ **双端 SAME（1/3 同形）**。**L004-2 的参照侧「0/3 含 try1 零上游、确定性本地」本轮未复现**：干净专属仓与 _catalog 聚合后两态下，参照对未知 image 名均为首问一次（HEAD）后窗口内本地——与 M-a 同形。本轮按第一手双证据修正 M-b 口径：参照行为 = **首问一次 → missedTTL 窗口内本地**（BinFlow 实现恰好同形）；「未知名不外问」的强读法（零回源、永久确定性）未获复现支持，登记供 conductor 复核 L004-2 §2.1 M-b 子臂的观察条件（疑其计数过滤或前置状态所致，机理同其 §1.3 自记的误读类）。

### 3.4 真实 CLI 腿（BinFlow 侧）

docker CLI 27.5.1 拉缺失 tag（HEAD+GET 一拉）：上游恰 **1 往返**（HEAD 404 落行），GET 由负缓存行本地应答；客户端渲染 `manifest unknown: The named manifest is not known to the registry.`（E6 错误族）。sqlite 行：`l0041/busybox/tags/cli-miss | negative | 19:10:02Z → 19:11:02Z`。

### 3.5 sqlite 行直证汇总（BinFlow 快照，docker cp db+WAL）

```
l0051-c14-remote|l0041/busybox/manifests/1cfa4e2b…|content   |19:07:00Z|2026-09-12T01:07:00Z
l0051-c14-remote|l0041/busybox/tags/l0051miss-b  |negative  |19:08:53Z|19:09:53Z   ← 重写后行
l0051-c14-remote|l0051nope/tags/latest           |negative  |19:09:24Z|19:10:24Z   ← M-b 行
l0051-c14-remote|l0041/busybox/tags/cli-miss     |negative  |19:10:02Z|19:11:02Z   ← CLI HEAD 落行
```

键法（Go-native，本票设计）：digest 引用 → 既有 `image/manifests/<hex>` 节点路径（与 standing 臂同键）；tag 引用 → `image/tags/<tag>` 命名空间（无 node 布局冲突，tags/list 为 wire 路径不受影响）；TTL 沿 `missedRetrievalCachePeriodSecs`（CacheRemoteMiss 既有口径）。virtual 面同臂写成员作用域行（V2CacheMemberMiss）；成员 facts seam 的冷 miss 读探针为 repo 面后续项（见实现票日志 Risks）。

## 4. 契约/台账翻绿建议（归 compatibility-engineer / conductor，本报告只出证）

1. **`docker/remote-manifest-conditional-get`：DIVERGENT → VERIFIED**——M2/M2c/M2d 三臂收敛（本报告 §1，20/20 SAME）；D1 修复实现 = 两读面统一按值匹配 + INM 在场封死日期臂。
2. **known-divergence `docker/remote-manifest-404-negative-cache`（C14）：BUG → resolved/FIXED**——M-a/M-b 双形态 + TTL 到期回源全过（本报告 §3）；authority 链 = 本报告 + L004-negative-cache-probe.md + ADR-0048 Errata。
3. 契约条目 `docker/remote-manifest-404-no-negative-cache` 的 expect/timing 按 L004-2 §3.1 建议翻「窗口内本地负缓存」语义，**M-b 断言按本轮修正口径写**：首问一次（GET/HEAD 皆可）→ 窗口内本地 → 到期回源——勿沿用「未知名零回源」强读法（未复现）。
4. `docker/remote-blob-conditional-get`（L004-1 建议条目）维持 VERIFIED 候选——B1-B6 复证 SAME。
5. N1/N2/D3 维持既有登记，本轮无新增分歧、无回归。

## 5. 复现骨架（凭据脱敏；脚本=tools/difftest/l0041/ 既有两件，零改动）

```bash
# 上游：docker run -d --name l0041-upstream -p 5591:5000 registry:3
#   种子：docker pull busybox@sha256:1cfa4e2b… && tag+push 127.0.0.1:5591/l0041/busybox:{t1,latest}
#   （digest 与 304-matrix.sh 内嵌 MFD 精确一致；config=c6348fa8…/459B 供 CFGDIGEST）
# 仓：双端 PUT {a:默认, b:+retrievalCachePeriodSecs:60, c14:+missedRetrievalCachePeriodSecs:60}
#     url=http://host.docker.internal:5591（BinFlow 另需 allowPrivateUpstream:true）
# 播种：dind docker pull <host>:<port>/<repo>/l0041/busybox:t1（四仓；host 侧播种注意 daemon 本地层跳过 blob 下载——blob 缓存需额外暖机一次）
# 304 矩阵：HOST=… BASIC=… REPO=l0041-a-remote IMAGE=l0041/busybox TAG=t1 CFGDIGEST=… ./304-matrix.sh（双端）
# 过期矩阵：REPO=l0041-b-remote …（内嵌 3×70s sleep）
# C14：curl -u … -H 'Accept: …manifest.v2+json,…' /v2/l0051-c14-remote/{l0041/busybox/manifests/<miss-tag>,<unknown-img>/manifests/latest} ×3
#   计数：docker logs l0041-upstream | grep -E '^[0-9.]+ - - ' | grep <path> | grep -c <UA>（锚访问行，防 debug 行误计）
#   行证：docker cp binflow-ga:/var/lib/binflow/binflow.db{,-wal} → sqlite3 remote_cache
```

### 环境清理记录（已执行）

- 仓：双端 l0041-a/b-remote、l0051-c14-remote DELETE 200（BinFlow 非空仓按 deleteContent=true）；双端列表零残留。
- 容器：l0041-upstream、difftest-dind 已 rm -f；docker ps -a 无 l0041/difftest 残留。
- 落盘清理：BinFlow db/WAL/shm 快照与 purity 抽取目录已删；矩阵产物 /tmp/l0051/m{A,B}-{art,bf} 留存（无凭据）。
- UAT :8083：保留 uat-l0051-792c347c 运行（healthy）；.env.uat 版本键指向该镜像（gitignored）。参照 :8082 未动（ping 200 复核）。
