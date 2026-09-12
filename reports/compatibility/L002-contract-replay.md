# L002-2 契约全量重放报告：docker-remote 21 条目 × 双系统活体对拍

- Ticket: L002-2（differential-qa-engineer）
- 日期: 2026-09-11
- 模式: **dual**（参照 :8082 Artifactory-pro 7.161.20 实测 vs BinFlow UAT http://localhost:8083）
- BinFlow 基线: **uat-l0022-5f9c48d0**（本次重建；worktree rev 5f9c48d0 + 未提交 L002-1 docker adapter WIP，`git diff HEAD --stat` internal/adapter/docker/{remote,virtual}.go +90 行，含 ADR-0047/0048 marker-gate/negative-cache-row 实现）。任务书基线 uat-l0011-f80c46aa 容器构建于 04:40+08:00，**早于 D21 修复 commit b1e1bc12（05:03）**——为闭 D21 按既有实践（L000-F/L001-1 先例）以同卷（binflow-uat_binflow-data）重建；`.env.uat` 的 BINFLOW_VER/BINFLOW_REVISION 已随之指向 uat-l0022-5f9c48d0。
- 上游: 本地 registry:3（`difftest-upstream` host.docker.internal:5588，busybox:t1/t2/t3 + hello-world:latest，本轮回注种子——与 L001-1 轮种子**字节不同**：busybox 为 docker media-type 单构 manifest（digest 92b1d1ca…），L001-1 轮为 OCI 形态（1cfa4e2b…），故 BinFlow 卷内残留缓存对旧 tag 呈 HIT-stale，属缓存语义非分歧）
- 客户端: 真实 docker CLI 27.5.1（一次性 dind `difftest-dind`，经典栈，--insecure-registry 覆盖 5588/8082/8083）+ curl（--noproxy '*'）
- normalize: fixtures/normalize.yaml#docker-remote（superset=tolerate_and_note；时间类数量级；上游 RTT 以对拍实例日志为准——本地上游腿 timing 不可分，一律以 `docker logs difftest-upstream` 判）
- 凭据: 命令行内使用，本报告一律脱敏
- 并行轨道干扰披露: 共享宿主上有 L002-1 轨道并发探测（上游日志见 curl/8.7.1 UA 的 tags/catalog 探测、上游被推入 `l0021-probe` 镜像）。本报告所有判定均以**唯一路径/唯一 digest** 归因（miss-x1/x2/y1、679e701c、92b1d1ca、t3），不受污染；_catalog 对拍以双端背靠背同刻取样对冲。

## 结论速览

**SAME 12 / DIVERGENT 8 / in-flight 1**（21/21 条目，零漏跑）。契约上轮账面（SAME/VERIFIED 15 / DIVERGENT 6）→ 本轮：**新增显性 DIVERGENT 3**（#1 ping Content-Type charset、#2 ping 坏凭据臂、#11 blob GET 头集缺口——三者在契约 expect 面上早已在册，前轮 SAME 判定分别锚定在 body/service 回显/token 臂/校验和族，头集全维度对照后显性化）、**行为翻转趋同 1**（#16 负缓存：双端运行时均已"每次 miss 上游往返"，BinFlow 负缓存在 WIP 构建中消失，归因 L002-1）、**转在途 1**（#14 门控断言本轮实测已过——上游 0 往返/58ms——但判定权归 L002-1 自验）。**D21 双面闭账**（List+Detail url=真实上游，与 :8082 逐字一致）。**金样 2 件活体重采**（derived-from-report → live-captured，confidence medium→high）。**分页第一手证据**（Artifactory remote 面尊重 n/last，BinFlow 无视——UNKNOWN 条目从观察空白变实锤差异）。

## 逐条目重放（21/21）

判定词汇：SAME=契约启用断言归一后全过；DIVERGENT=任一断言失败；in-flight=条目判定权在并行票（任务书指定）。"变化"列对照契约 binflow_state 上轮账面。

### L4 Auth（5 条）

**#1 docker/remote-v2-ping-anonymous — DIVERGENT（新增显性，低危）·变化: SAME→DIVERGENT**
- BinFlow: `401`；`WWW-Authenticate: Bearer realm="http://localhost:8083/v2/token",service="localhost:8083"`（service=host 回显 ✓）；`Docker-Distribution-Api-Version: registry/2.0` ✓；body compact `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}` 逐字 ✓
- Artifactory: 同 401/同 service 回显/同 body；**`Content-Type: application/json;charset=ISO-8859-1`**
- 失败断言: expect.headers.literal `Content-Type: application/json;charset=ISO-8859-1`——BinFlow 回裸 `application/json`（无 charset 参数）。该维度前轮未对照（锚定在 body+service）。
- 处置建议: normalize 新增"Content-Type charset 参数不敏感"规则**或**BinFlow 对齐补 charset——提案权在 compatibility-engineer；低危（客户端不解析该参数）。

**#2 docker/remote-v2-ping-bad-credentials — DIVERGENT（新增显性）·变化: SAME→DIVERGENT（ping 臂首次双端活体）**
- BinFlow ping 臂（GET /v2/ + Basic 坏凭据）: `401` + **Bearer 重挑战** + compact UNAUTHORIZED body（与匿名不可区分）
- Artifactory ping 臂: `401` + **`WWW-Authenticate: Basic realm="Artifactory Realm"`** + pretty `{"errors" : [ { "status" : 401, "message" : "Bad Credentials" } ]}`（`Content-Type: application/json;charset=ISO-8859-1`）
- 失败断言: 本条目 surface 即 ping 端点——expect.body pretty "Bad Credentials" + keys_absent code/detail 均不过；且挑战头形态异（Basic vs Bearer 重挑战）。
- 溯源: 前轮 SAME 锚在 token 端点 verbatim（C02 案），known-divergence C02 resolved note 已注明"ping 重挑战属次要可接受面"——但契约条目面从未收窄。处置建议: compatibility-engineer 或将条目 expect 改锚 token 臂（记录 known 可接受面），或立票对齐 ping 臂（真实 docker CLI 不走此路径——login 流程匿名 ping 后带 Basic 打 token 端点；客户端影响为零）。

**#3 docker/remote-token-issue — DIVERGENT（维持，待裁）·变化: 无**
- BinFlow: `200 {token: <64-hex 不透明>, access_token, expires_in: 2592000, issued_at, scope}`
- Artifactory: `200 {token: eyJ2ZXIiOiIy…（JWT）, expires_in: 9000}`（活体复证）
- 维持 known-divergence `docker/remote-token-response-shape`（LOOP 002 限期升级中，authority 缺位）。

**#4 docker/remote-token-bad-credentials — SAME（维持）·变化: 无**
- 双端逐字: `401` pretty `{"errors":[{"status":401,"message":"Bad Credentials"}]}`（Jackson `" : "` vs Go `": "` 缩进风格差异按 normalize wire_format=pretty 等价，json_key_order=ignore）。

**#5 docker/remote-token-anonymous — DIVERGENT（维持，待裁）·变化: 无**
- BinFlow: `200` 签发匿名 token（实例匿名默认开，compose `BINFLOW_SECURITY_ANONYMOUS_ACCESS:-true`）
- Artifactory: `401` pretty `{"errors" : [ { "status" : 401, "message" : "Authentication is required" } ]}`
- 维持 known-divergence `docker/remote-anonymous-token-policy`（产品裁定 BinFlow remote 面默认匿名姿态）。

### L5/L6 manifest（4 条）

**#6 docker/remote-manifest-get-tag — DIVERGENT（维持，C06 族）·变化: 无**
- BinFlow（fresh fetch，digest 寻址 MISS）: `200`，头集 = Content-Type/Docker-Content-Digest/Docker-Distribution-Api-Version/X-Binflow-Cache/X-Request-Id/Date/Content-Length——**缺 Etag/Last-Modified/Accept-Ranges/X-Checksum-*/X-Artifactory-Filename/X-Artifactory-Origin-Remote-Path/Content-Disposition**（全部 expect 在册断言）
- Artifactory（同参数 fresh fetch）: 全集在档——`Etag: 1980a6b2…（=X-Checksum-Sha1 ✓）`、`Last-Modified: <拉取时刻>`、`Accept-Ranges: bytes`、`Content-Disposition: attachment; filename="manifest.json"`、`X-Artifactory-Filename: manifest.json`、`X-Artifactory-Origin-Remote-Path: http://host.docker.internal:5588/v2/busybox/manifests/t1`、`X-Checksum-Md5/Sha1/Sha256`
- 正面记录: **digest 保真**——BinFlow 按摘要取 `sha256:92b1d1ca…` 命中上游，返回字节与上游 `shasum -a 256` 逐字一致（92b1d1cae5f2…46e3），Content-Type 随上游协商（docker v2+json）✓
- 附注（缓存语义非分歧）: tag 路径 `t1` 命中 L001-1 轮残留缓存（HIT，旧 OCI 字节 1cfa4e2b…）——检索窗内 tag 不回源两侧同语义（Artifactory docker remote 检索窗 21600s 默认）。

**#7 docker/remote-manifest-head-digest — DIVERGENT（维持，C06 族）·变化: 无 + 参照侧新观察**
- BinFlow（HEAD 摘要）: `200` 头集同 #6 窄集——缺 Last-Modified、X-Artifactory-Docker-Registry、Etag、校验和族
- Artifactory（HEAD 摘要 + Accept）: `200`，`X-Artifactory-Docker-Registry: audit-probe-docker-remote`、Last-Modified、Docker-Content-Digest ✓——**但活体 HEAD 亦无 Etag/校验和族/Origin-Remote-Path**（契约 expect.present/pattern 对参照侧 HEAD 有高估，源自 E2-4 docker.io 腿；建议 compatibility-engineer 复核条目）
- 参照侧意外行为（新观察，未入契约）: HEAD **不带 Accept** 时 Artifactory 回 schema1 转换体（`Content-Type: application/vnd.docker.distribution.manifest.v1+prettyjws`，Content-Length 1003，`Docker-Content-Digest: sha256:dbaa8e66…` ≠ 请求摘要）；BinFlow 同条件回正确 v2 manifest。低危（docker CLI 恒带 Accept）。

**#8 docker/remote-manifest-conditional-get — SAME（维持）·变化: 无**
- BinFlow: GET t1 + `If-None-Match: "deadbeef"` → `200` 全量 610B（X-Binflow-Cache: HIT）；Artifactory 同判 `200` 1019B——永无 304，双端一致（E3-2 复证）。

**#9 docker/remote-pull-sequence — SAME（维持）·变化: 无**
- dind `docker pull host.docker.internal:8083/audit-probe-docker-remote/busybox:t3`（未缓存 tag）→ exit 0，`Status: Downloaded newer image`
- digest 逐字: `sha256:92b1d1cae5f235812184415e63d9b24464116c58d3ba3c460b1eb0247f0f46e3` == 上游推送回执 == dind 直拉上游腿（`docker images --digests` 三源一致）
- 上游日志实收 `GET /v2/busybox/manifests/t3`（/v2/ 前缀在，wire path 无回归）

**#10 docker/remote-second-pull-cache-hit — SAME（维持）·变化: 无**
- rmi 后重拉 exit 0；上游 `GET …/manifests/t3` 计数冻结于 1（二拉 0 新增）；后续 manifest GET `X-Binflow-Cache: HIT`（超集头，superset_note）。

### L5/L6 blob（4 条）

**#11 docker/remote-blob-get — DIVERGENT（新增显性）·变化: SAME→DIVERGENT（矩阵 ✅ 面出红，上报项）**
- 通过断言: `200`；`X-Checksum-Md5/Sha1/Sha256` + `Etag(=X-Checksum-Sha1 ✓)` + `Accept-Ranges` + `Docker-Content-Digest` 全在；`sha256(body)==digest`（c6348fa8…，459B config）✓
- 失败断言: expect.present **`Last-Modified` 缺席**、expect.pattern **`X-Artifactory-Origin-Remote-Path` 缺席**（另缺参照侧 Content-Disposition/X-Artifactory-Filename——超集面，不单列断言）
- Artifactory 活体: 全集在档（`Last-Modified: <落缓存时刻>`、`X-Artifactory-Origin-Remote-Path: http://host.docker.internal:5588/v2/busybox/blobs/sha256:c6348fa8…`、`Content-Disposition: attachment; filename="sha256__c6348fa8…"`、`X-Artifactory-Filename: sha256__…`、校验和族、Etag）
- 溯源: L000-F 将 blob 面判 SAME 时对照锚为校验和族（"与参照 E2-3/E3-5 面一致"），Last-Modified/Origin-Remote-Path 两断言未逐项执行——本轮全维度对照后显性。**无法区分"前轮漏判"与"本轮回归"**（f80c46aa 轮未留存该两头的在档观察）；建议按回归处理并入 docker/remote-manifest-headers 实现票（同族头集）。

**#12 docker/remote-blob-get-range — SAME（维持）·变化: 无**
- BinFlow: `206` + `Content-Range: bytes 0-63/459` + 64B + Docker-Content-Digest + 校验和族 + Etag + Accept-Ranges（条目 present 全过）；Artifactory 同形态（206/Content-Range/同校验和值——双端 checksum 逐字一致 e6329bed…/105e5808…）。

**#13 docker/remote-blob-empty-digest-synthetic — SAME（维持）+ 金样活体重采 ·变化: 判定无、金样 derived→live**
- 双端 GET/HEAD 均 `200`/32B/`Docker-Content-Digest: sha256:a3ed95ca…`；body 逐字节一致（`cmp` 通过，hex `1f8b080000096e8800ff621805a360148c5800080000ffff2eafb5ef00040000`）
- 上游日志该 digest **0 命中**（双端）——合成本地应答直证（Artifactory 客户端耗时 9.3ms）
- pending_capture 闭合: **Artifactory 合成路径不补校验和族**（GET/HEAD 均无 Etag/X-Checksum-*/Accept-Ranges/Content-Disposition/Filename/Origin-Remote-Path）；BinFlow 带 Etag+校验和族+Accept-Ranges——超集，superset_policy 容忍并注记。

**#14 docker/remote-blob-unknown-digest-404 — in-flight（任务书指定归 L002-1 自验）**
- body 断言 SAME 维持: `404` compact `{"errors":[{"code":"BLOB_UNKNOWN","message":"blob unknown to registry","detail":{"blobSum":"sha256:679e701c…"}}]}`（E6-2 逐字；两次运行 body 稳定）
- 门控断言（timing.upstream_rtt=false/max_ms=100）本轮 WIP 构建上**实测通过**: run1 58ms / run2 40ms，上游日志 679e701c **0 命中**（无往返）；Artifactory 参照同判（11/12ms，0 命中）——与 L002-1 WIP（ADR-0047 chainGate/marker）相符。**不据此翻态**，判定权归该票。（并行交叉确认：L002-1 已于同日在 known-divergence 台账自行落 resolved 块——"链外 digest 本地 404 ~5ms 且上游日志零请求直证"，与本报告独立取证互证。）

### 404 形态族（2 条）

**#15 docker/remote-manifest-unknown-404 — SAME（维持）·变化: 无**
- BinFlow: `404` compact `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"The named manifest is not known to the registry.","detail":{"manifest":"busybox"}}]}`；Artifactory 活体逐字同（registry:3 上游，无 library/ 前缀——归一化条件语义同 E2-6）。

**#16 docker/remote-manifest-404-no-negative-cache — DIVERGENT（台账维持待裁）·变化: 运行时行为翻转趋同（上报项）**
- 本轮双端活体（同 missedRetrievalCachePeriodSecs=1800，本地上游）:
  - BinFlow: miss-x2 ×3 → 上游 **3/3 往返**（每次 miss 都回源，负缓存消失）；客户端 9.7/12.4/11.5ms
  - Artifactory: miss-y1 ×3 → 上游 **3/3 往返**（UA Artifactory/7.161.20）；客户端 77/10/10ms
- 即双端运行时均为"无负缓存、每 miss 回源"——原 DIVERGENT 依据（BinFlow missedTTL 生效本地 404）在本构建上不成立。
- 归因: BinFlow 负缓存消失疑为 L002-1 WIP（remote.go diff 引 ADR-0048 "negative-cache row…deterministic"）——**非本票裁定**。处置: 待 L002-1 落定 + authority 复核后翻态（若双端均为"无负缓存"，该条目可按 parity 非 BUG 闭环，参照 conductor resolved(parity) 先例）。

### tags/catalog/上传面（6 条）

**#17 docker/remote-tags-list — SAME（维持）·变化: 无**
- BinFlow: `200` pretty `{"name":"busybox","tags":["t1","t2","t3"]}`（t3 为本轮新注上游 tag，实时聚合直证）；上游日志实收 `GET /v2/busybox/tags/list`（UA binflow-remote/1.0）。Artifactory 裸 GET 同判（[t1,t2,t3]）。

**#18 docker/remote-catalog — SAME（维持）·变化: 无**
- 双端背靠背同刻取样逐字同集合: `{"repositories": ["busybox","hello-world","l0021-probe"]}`（l0021-probe 为并行轨道推入上游的镜像——双端同见恰证两侧均为活体聚合）。

**#19/#20/#21 push 三端点（uploads POST / manifest PUT / manifest DELETE）— SAME ×3（维持）·变化: 无**
- 双端均 `400` pretty status 形态三文案逐字（`Unable to upload blobs to a remote repository.` / `Unable to upload a manifest to a remote repository.` / `Unable to delete a manifest from a remote repository.`，无 code/detail）。

## D21 闭账（repo GET 顶层 url 回显）

前置: UAT 重建至含 b1e1bc12 的构建（旧容器 f80c46aa 早于该 commit，重建前实测仍自指——不构成反证）。同构仓双端建立（remote/docker，url=http://host.docker.internal:5588，missedRetrievalCachePeriodSecs=1800）。

| 面 | Artifactory :8082 | BinFlow :8083 | 判定 |
|---|---|---|---|
| List `GET /api/repositories`（remote 行 url） | `http://host.docker.internal:5588` | `http://host.docker.internal:5588` | 一致 |
| Detail `GET /api/repositories/{key}`（顶层 url） | `http://host.docker.internal:5588` | `http://host.docker.internal:5588`（configuration.url 同值） | 一致 |

**D21 = 闭账（L001-5 修复双面复验通过）**；known-divergence `docker/remote-error-shapes` resolved note 已回填本报告为证据。

## 金样活体重采（golden/docker-remote/ 两件）

| 金样 | 旧态 | 新态 | 关键增量 |
|---|---|---|---|
| empty-blob-synthetic | derived-from-report, confidence medium | **live-captured, confidence high** | 参照侧头集全集（合成路径**不**补校验和族）+ 32B raw hex 存档 + BinFlow 超集注记 + 上游 0 往返直证；采集时间/实例/命令入 metadata |
| pull-sequence | derived-from-report, confidence medium | **live-captured, confidence high** | digest 全值 `sha256:c766679d161d4ffe3dc4503b4c9f90b978f0d363fcedb02d1ae0cd271e645c0a`（Artifactory 腿）+ registry:3 腿 manifest digest 92b1d1ca…46e3（BinFlow 对拍腿）+ seq3 头集活体全集 + 二拉缓存冻结证据 |

BinFlow 侧同拓扑对拍留档：本报告 #9/#10/#13 段（exit 0、digest 逐字、HIT/冻结、32B 逐字节一致）。

## 分页 UNKNOWN 第一手取证（known-divergence docker/remote-tags-catalog-pagination）

:8082 remote 面 tags/list（上游 3 tag，2026-09-11 活体）:

| 请求 | Artifactory | BinFlow |
|---|---|---|
| `?n=2` | `{"name":"busybox","tags":["t1","t2"]}`（**尊重 n**） | 全量 [t1,t2,t3]（无视） |
| `?n=2&last=t1` | `{"name":"busybox","tags":["t2","t3"]}`（**尊重 last**） | 全量（无视） |
| `?n=abc` | `404` pretty status 形态 `{"errors":[{"status":404,"message":"Not Found"}]}` | `200` 全量 |
| Link 头 | 无（吸收上游分页不外露） | 无 |

结论: 该 UNKNOWN 从"观察空白"变**实锤双侧行为差异**；known-divergence rationale 已回填。分类仍 UNKNOWN（契约无条目、无 authority；n/last 透传语义 + 无效 n 错误形态归契约补条目 + 实现裁定）。

## 上报项（escalation）

1. **矩阵/契约 ✅ 面出红 ×3**（#1 charset、#2 ping 臂、#11 blob 头集）——账实不符或前轮漏判，需 compatibility-engineer 裁定（改锚/normalize 提案/实现票收编）。
2. **#16 负缓存运行时翻转**——归因疑 L002-1 WIP，翻态待其落定 + authority。
3. **#7 参照侧 HEAD 活体头集低于契约 expect**（Etag/校验和/Origin-Remote-Path 未现）——契约对参照侧有高估，提请复核 E2-4 与本活体的差异（docker.io 腿 vs registry:3 腿）。

## 复跑门

快速腿二次运行（ping-anon/token-anon/empty-blob/m404）: 状态码与 body 指纹（sha1 b5ede2db…）逐次一致，判定稳定。客户端腿（pull ×2、上游计数）单次完成无 flake。

## 环境清理记录（已执行）

- Artifactory: `DELETE /artifactory/api/repositories/audit-probe-docker-remote` → 200（deletedArtifactsCount 11）。
- BinFlow: 首删 400（"repository is not empty: 12 nodes; retry with deleteContent=true"——BinFlow remote 删仓默认拒删非空仓，为 API 行为面观察）；`?deleteContent=true` 重删 → 200；仓列表仅余 `docker-local`（devops 轨道资产，未动）。
- 一次性容器 `difftest-dind`/`difftest-upstream` 已 rm -f；`docker ps -a` 无 difftest 残留；dind 内登录凭据随容器销毁。
- 未触碰: 并行轨道资产（上游 `l0021-probe` 镜像随上游容器销毁；其可能在实例上的同名仓非 audit-probe- 前缀，未动）。上游容器销毁即 seeds 清空。
- UAT 变更留档: `.env.uat` BINFLOW_VER=uat-l0022-5f9c48d0 / BINFLOW_REVISION=5f9c48d0（重建即基线，卷未换）。

## 原始证据索引

/tmp/l0022/（本机暂存，报告内已嵌关键摘录;命名: `a_*`=Artifactory 腿、其余=BinFlow 腿、`up_*`=上游直连）: ping/token ×5、mf/mfd/mfh/mfif、blob/blobr、empty ×2、uk1/uk2、m404、miss ×3、tags/tagsn/tagsl/tagsinv、cat、p1-p3、a_ping/a_pingbad/a_tok*/a_mf*/a_blob*/a_empty*/a_miss*/a_unk*/a_tags*/a_cat/a_p1-3。
