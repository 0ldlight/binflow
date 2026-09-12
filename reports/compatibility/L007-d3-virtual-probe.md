# L007-2 — D3 定向探针（过期 blob 回源）+ virtual 面冷 miss 活体差分（U-PROTO 面）

- Ticket: L007-2（differential-qa-engineer；conductor 派发两件：① D3 `docker/remote-blob-expired-revalidation` 的 LOOP 007 三选一裁决素材 ② L006-2 挂账的 virtual 面冷 miss 活体差分）
- 日期: 2026-09-12（UTC；下文时刻均 UTC）
- 模式: **dual**（BinFlow UAT http://localhost:8083 vs 参照 :8082 Artifactory-pro 7.161.20）
- BinFlow 基线: **uat-l0072-c1193f5f**（= develop HEAD c1193f5f；按 L003-3 §2 指南，离线变体构建——DaoCloud alpine 镜像源当日 EOF，`tools/difftest/l0072/build-image-offline.sh` 以本地 alpine:3.24 RepoDigest 钉基 + 复用缓存 assets 镜像（同 alpine 3.24.1 血统）+ classic builder 复放 build-release.sh phase 1/2/4；纯净性：`go version -m` vcs.revision=c1193f5fa17d…，version/healthz/ui 200 smoke 通过）
- 参照: :8082 运行时 7.161.20。**事件**：04:23Z 容器自行 Exited(0)（本会话仅 API 读写，未触其运行面）；`docker start` 后 ~5 min 收敛（frontend init 327s），仓配置经重启幸存，ping 200 后开跑
- 上游: 本地 registry:3（l0072-upstream，127.0.0.1:5591，种子 `l0072/busybox:t1`（manifest 1cfa4e2b…、config blob **c6348fa8**…/459B、layer b0509380…/2.1MB）与 `l0072/hello:cold`（manifest **d1a8d0a4**…）——busybox 服务任务 1、hello 服务任务 2，指纹互斥；判据 = 上游 access 行（`^[0-9.]+ - - `）按 path×UA 计数）
- 测试仓: 双端各 3——l0072-b-remote（remote/docker，retrievalCachePeriodSecs=**60** 双端回读确认；BinFlow 另 allowPrivateUpstream:true）、l0072-v-remote（同参）、l0072-virt（virtual/docker，members=[l0072-v-remote]）。**用后已删净（双端 DELETE 200，列表 grep l0072 = 0）**
- 客户端: curl 8.7.1（--noproxy，Basic 直连）+ 真实 docker CLI 27.5.1（一次性 dind difftest-dind，--insecure-registry 8082/8083）
- 凭据: 命令行 shell 变量传递，不落盘；报告脱敏

## 0. 结论速览

| 问题 | 答案 |
|---|---|
| D3：参照在某窗后对过期 blob 回源吗 | **否——全臂零回源**（1×TTL ×3、INM、**10×TTL（~14 min）**、真实 docker pull 全部 0；filestore 文件 mtime 全程未动）。**「对齐收（参照在某窗回源）」被证据排除** |
| D3：BinFlow 过期 blob 行为 | **每次检索窗到期即回源重取**（+1，窗随重取滑动，remote_cache 行重写；冷 200 / INM 304 两条客户端可见路径均如此）；TTL 兑现精确（窗长恰 60s） |
| D3 三选一建议 | **INTENTIONAL 候选**（BinFlow 兑现自家 retrievalCachePeriodSecs；客户端可见面全臂双端全同）+ 注记「对齐可经 digest-blob 窗豁免实现（blob 内容寻址不可变，窗口重取语义空转，参照即此语义）」；终裁归 conductor/compatibility-engineer |
| virtual 冷 miss（L006-2 挂账） | **活体闭环**：既有 tag 冷 miss ×3 双端同语义（首问回源→本地→本地）；到期回源臂双端各 +1（SAME）；负缓存冷 miss ×3 双端同语义（tag 键负行、1800s 窗）——httptest 锁定的机制全部活体双端兑现 |
| **新发现（virtual 面）** | **冷负 miss 首答 body 注记分歧**：BinFlow 经 virtual 的冷 404 首答 message 带 `(upstream answered 404 404 Not Found)` 后缀（176B vs 参照恒 137B）；负缓存命中后即与参照逐字节同（138B+尾换行，既有归一内）。同构建直读面无此注记 → **virtual 面特有**。BUG 候选登记 |

---

## 1. 任务 1：D3 定向探针（docker/remote-blob-expired-revalidation，LOOP 007 裁决门）

对象：config blob `sha256:c6348fa8…`（459B，实链内容——busybox:1.38.0-glibc config）。双端 04:30:35Z–04:30:46Z 对齐种缓存（同一 10s 窗内完成）。

### 1.1 种缓存（warm，04:30:35Z 起）

| 腿 | 客户端可见 | 上游往返（UA 判别） |
|---|---|---|
| BF manifest | 200/2.77s | GET t1 **304**（上轮 04:20 首取的行已过窗 → 条件重验，§8-C 形） |
| BF blob | 200/3.85s/459B | GET c6348fa8 **200** ×1 |
| ART manifest | 200/4.06s | HEAD t1 + GET t1 200（SEED 形） |
| ART blob | 200/0.80s/459B | GET c6348fa8 200 **×2**（双取，N2） |

双端 Etag 逐字同（`105e5808…` sha1 形）；BinFlow 行：`l0072/busybox/blobs/c6348fa8… | fetched 04:30:42Z | expires 04:31:42Z`（窗长恰 60s）。
附注：本会话容器重建后的**首笔**远端请求（04:20 轮）BF 侧 29s/48s 异常慢，之后所有触网请求 0.4–4s 正常（Artifactory 同量级）——记 Risks，非本票面。

### 1.2 过期窗 ×3（04:32:56Z–04:33:07Z，无条件 GET，双端交错）

| 臂 | BinFlow :8083 | 参照 :8082 | 上游计数 |
|---|---|---|---|
| GET1 | 200/1.05s/459B，`X-Binflow-Cache: MISS`，**上游 +1**（GET 200 459） | 200/0.071s/459B，本地 | **1 vs 0 DIVERGENT** |
| GET2 | 200/0.042s，HIT，上游 0 | 200/0.036s，本地，上游 0 | SAME |
| GET3 | 200/0.035s，HIT，上游 0 | 200/0.029s，本地，上游 0 | SAME |

L004-1 X4 精确复现：BinFlow 过期即重取（重取即滑窗 → 二问起 HIT）；参照过期 blob 本地应答零回源。

### 1.3 过期窗 + 客户端 quoted INM（04:34:35Z）

| 臂 | BinFlow | 参照 | 上游 |
|---|---|---|---|
| `If-None-Match: "105e5808…"` | **304**/0.04s，MISS，**上游 +1**（重取后再 304） | **304**/0.020s，本地 | **1 vs 0 DIVERGENT**（X3 复现；客户端可见面 SAME） |

### 1.4 10×TTL 跨窗复核（04:44:49Z ≈ 暖取后 14 min = 14×TTL；BF 行距上次滑窗 ~9 min）

| 臂 | BinFlow | 参照 | 上游 |
|---|---|---|---|
| 无条件 GET | 200/0.46s，MISS，**上游 +1** | 200/0.034s，本地，**上游 0** | **1 vs 0 DIVERGENT** |

**参照侧跨 10×TTL 窗后仍零回源——「参照在某阈值后回源」假说被排除。**

### 1.5 真实 docker CLI pull 对照（04:45:23Z / 04:45:37Z，dind docker 27.5.1，exit 0 双端，镜像可拉取可运行）

| 端 | manifest Δ | config blob Δ | layer Δ（冷，本会话首取） | 客户端结果 |
|---|---|---|---|---|
| BinFlow | **+1**（条件重验） | **+0**（04:44:49 重取后窗新鲜 → HIT；若窗过期按 1.2/1.3 必 +1） | +1 | Downloaded newer image，digest 一致 |
| 参照 | **+1**（HEAD 探查，按 X1 既有形） | **+0**（过期 blob 仍本地） | **+2**（N2 双取） | 同 |

**真实客户端全链 pull 下参照对过期 blob 依旧零回源**——D3 分歧在最重观察面上成立。

### 1.6 本地行/存储状态（逐臂快照）

- BinFlow `remote_cache`（docker cp db+WAL）：blob 行每臂重取后重写——`04:30:42→04:31:42`、`04:34:36→04:35:36`（1.3 臂）、（1.4 臂后同形）；重写时刻=重取时刻、窗长恒 60s。
- 参照 filestore：`data/artifactory/filestore/10/105e580851660fb421118318bfcbffa7a8155c20`（459B）mtime 全程 = **2026-09-11 03:06:12**（前轮 L004-1 会话所写，checksum 存储去重）——本会话全部请求（含上游 GET ×3 的 04:30 双取）后三次复查均未变 → 参照侧既不回源也不重写。

### 1.7 D3 三选一裁决素材（建议，终裁不在我权）

| 选项 | 证据判读 |
|---|---|
| **对齐收**（参照确在某窗回源——BinFlow 窗语义对齐） | **排除**：参照在 1×/3×TTL、INM、10×TTL（14 min）、真实 pull 四类臂上对已缓存 digest blob 全部零回源、零重写；manifest 面则恒 +1——参照的区分不是「窗口长短」而是 **manifest（可变 tag）/blob（内容寻址不可变）语义二分** |
| **INTENTIONAL**（BinFlow 兑现 TTL 承诺） | 成立面：BinFlow 对 retrievalCachePeriodSecs 的兑现精确（窗长恰 TTL、到期必回源、滑窗、行重写）；客户端可见面全臂双端全同（200/304/内容/digest/etag）；参照面本行为在 U-PROTO 矩阵无账面承诺被违反 |
| **维持分歧登记** | 事实态：分歧真实、持续、纯上游侧。若裁 INTENTIONAL，建议登记注记：对齐可经「digest 引用 blob 豁免检索窗」实现（blob GET 结果不可能变化，窗口重取语义空转、每次到期白付一次上游往返+带宽）——参照即此语义 |

**我的建议（供裁）**：INTENTIONAL 候选 + 上述对齐路径注记；`known-divergence.yaml` D3 条目 authority 由 pending 升 differential（evidence=本报告 §1），classification 由 UNKNOWN 按裁翻 INTENTIONAL。

---

## 2. 任务 2：virtual 面冷 miss 活体差分（L006-2 挂账清偿）

拓扑：virtual `l0072-virt` → remote 成员 `l0072-v-remote`（TTL 60）→ registry:3。对象 tag：`l0072/hello:cold`（种子推送直入 registry，未经任何仓——**冷**确认）。L006-2 的 httptest walk 已锁机制，本节补活体双端。

### 2.1 既有 tag 冷 miss ×3 经 virtual 路由（04:46:09Z 起，双端交错）

| 臂 | BinFlow | 参照 | 上游 |
|---|---|---|---|
| GET1（冷） | 200/0.054s/1023B，MISS，digest d1a8d0a4…，etag 90f142e3… | 200/0.379s/1023B，同 digest 同 etag | BF **+1**（GET 200 1023）vs ART **+2**（HEAD+GET，SEED 形）——N1/N2 形状族不判 |
| GET2 | 200/0.030s，HIT，上游 0 | 200/0.032s，本地，上游 0 | SAME |
| GET3 | 200/0.021s，HIT，上游 0 | 200/0.039s，本地，上游 0 | SAME |

body sha256 双端六份逐字节同；BinFlow 行：`l0072-v-remote | l0072/hello/manifests/d1a8d0a4… | 04:46:09→04:47:09 | content`（60s 窗）。**virtual 路由的冷 miss→缓存→本地链路活体成立，双端同语义。**

### 2.2 到期回源臂（04:47:48Z，成员 TTL 过期后经 virtual 复问）

| 臂 | BinFlow | 参照 | 上游 |
|---|---|---|---|
| 无条件 GET | 200/0.044s，`X-Binflow-Cache: REVALIDATED`，上游 **GET→304** +1 | 200/0.050s，上游 **HEAD 200** +1 | 各 +1，SAME（形状 N1） |

### 2.3 负缓存冷 miss ×3 经 virtual 路由（04:48:12Z，不存在的 tag `nope`）

| 臂 | BinFlow | 参照 | 上游 | body |
|---|---|---|---|---|
| neg1（冷） | 404/0.036s，上游 **GET 404** +1 | 404/0.361s，上游 **HEAD 404** +1 | 各 +1 SAME | **BF 176B 注记版 vs ART 137B**（见 2.4） |
| neg2 | 404/0.022s，本地 | 404/0.009s，本地 | 0 SAME | BF 138B ≡ ART 137B（尾换行，既有归一内） |
| neg3 | 404/0.021s，本地 | 404/0.009s，本地 | 0 SAME | 同上 |

BinFlow 负行：`l0072-v-remote | l0072/hello/tags/nope | negative | 04:48:13→05:18:13`（1800s = missedRetrievalCachePeriodSecs 默认，tag 键——C14 键法 virtual 面活体直证）。**L006-2 seam（virtual 读探针）经活体：首问一取、二问起本地零回源，与直读面同语义。**

### 2.4 新发现：virtual 面冷负 miss 首答 body 注记（BUG 候选）

```
BF  neg1（冷，经 virtual）: {"errors":[{"code":"MANIFEST_UNKNOWN","message":"The named
    manifest is not known to the registry. (upstream answered 404 404 Not Found)","detail":{"manifest":"l0072/hello"}}]}
BF  neg2/3（负缓存本地）:   …not known to the registry.","detail":…}        ← 与参照逐字同（+尾换行）
ART neg1/2/3（全臂）:      …not known to the registry.","detail":…}        ← 137B 恒定
```

对照实验（同构建 c1193f5f）：**直读面**冷负 miss（/v2/l0072-v-remote/…/manifests/nope2）首答**无注记**（138B 形，与参照同；二问起本地）→ 注记为 **virtual 面特有**（virtual handler 对成员上游 404 回显的 message 拼接直读面不做）。客户端可见面分歧（首答 message 串），影响一次性（负行落库后即对齐）。建议登记 `docker/virtual-remote-cold-negative-body-annotation`（BUG 候选；L006-2 walk 测试只断上游计数/行键，未比 message 体——正好漏过此面）。

### 2.5 结论

- L006-2 挂账（「virtual 面 by-digest/tag 冷 miss 的活体差分未跑」）**清偿**：机制三臂（既有 tag 冷 miss / 到期回源 / 负缓存冷 miss）活体双端全 SAME（N1/N2 形状族既有登记内）。
- 新增一项 BUG 候选（2.4）移交 compatibility-engineer 登记 + 转 dev 域修复票。

---

## 3. 环境清理记录（已执行）

- 仓：双端 l0072-b-remote / l0072-v-remote / l0072-virt DELETE 全 200（BinFlow deleteContent=true）；双端仓库列表 grep l0072 = 0。
- 容器：difftest-dind、l0072-upstream 已 rm -f；临时测试镜像 l0072-basetest 已 rmi；宿主种子 tag（127.0.0.1:5591/l0072/*）已 rmi。
- 落盘：/tmp 全部 l0072* 中间件（头文件、body、db 快照、构建目录）已删；凭据全程仅 shell 变量。
- 保留：UAT :8083 = uat-l0072-c1193f5f healthy（后续差分直接消费）；参照 :8082 运行中（ping 200）；镜像 binflow:uat-l0072-c1193f5f-alpine(-amd64) 与 assets 中间镜像（与历轮 uat-* 同例保留）。

## 4. 复现骨架（凭据脱敏；脚本=tools/difftest/l0072/）

```bash
# UAT 重建（离线变体，镜像源通时可回 L003-3 §2 原链）:
tools/difftest/l0072/build-image-offline.sh   # → binflow:uat-l0072-c1193f5f-alpine
# 上游: docker run -d --name l0072-upstream -p 127.0.0.1:5591:5000 registry:3
#   种子: docker tag busybox 127.0.0.1:5591/l0072/busybox:t1; docker push …；hello:cold 同法
# 仓: 双端 PUT l0072-b-remote{remote/docker, url=http://host.docker.internal:5591,
#     retrievalCachePeriodSecs:60}（BinFlow 另 allowPrivateUpstream:true）；
#     virtual 面: l0072-v-remote 同参 + l0072-virt{virtual/docker, repositories:[l0072-v-remote]}
# D3 矩阵: warm(manifest+blob GET) → sleep 70 → blob GET ×3（交错）→ sleep 70 → INM 臂
#     → sleep 585 → 10×TTL 臂 → dind pull 臂（每臂 docker logs l0072-upstream |
#     grep -cE '<path>.*<UA>' 前后差分）
# 行证: docker cp binflow-ga:/var/lib/binflow/binflow.db{,-wal} → sqlite3 remote_cache
# 参照存储证: ls -l data/artifactory/filestore/<sha1前2>/<sha1余38>（mtime 是否重写）
# 真实客户端: docker run -d --privileged docker:27.5.1-dind --insecure-registry=…8082 --insecure-registry=…8083
#     → docker exec … docker pull host.docker.internal:<port>/l0072-b-remote/l0072/busybox:t1
```

### 回归对照（对前轮）

| 前轮结论 | 本轮 | 处置 |
|---|---|---|
| L004-1 X3/X4（过期 blob：BF +1 vs 参照 0） | 双臂精确复现（1.2/1.3） | 无回归 |
| L004-1 review_gate「参照是否在某窗后回源未验」 | 10×TTL + 真实 pull 零回源（1.4/1.5） | 门内问题答毕 |
| L006-2 walk httptest（virtual 冷 miss 机制） | 活体三臂双端 SAME（§2） | 机制活体化，无回归 |
| L006 by-digest 直读面 404 体逐字同 | 直读面维持同（2.4 对照）；virtual 面**新**注记 | 新候选账 §2.4 |
| N1/N2（首问动词 HEAD vs GET / blob 双取） | 复现（1.1/2.1/2.3） | 维持既有登记 |
