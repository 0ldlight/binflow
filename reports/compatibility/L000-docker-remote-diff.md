# L000-B 差分期报告：docker remote 代理 v2 双系统对拍（U-PROTO-01）

- Ticket: L000-B / U-PROTO-01（差分期，E5）
- 日期: 2026-09-10/11
- 模式: **dual**（参照 :8082 Artifactory 7.161.20 实测 vs BinFlow UAT http://localhost:8083，容器 binflow-ga，rev 59f33ab5）
- 上游拓扑: 主证据腿 = docker.io（Artifactory）；同源对拍腿 = 本地 registry:3（`difftest-upstream` 127.0.0.1:5588，两侧同上游验证）。本机 DNS 被 TUN 劫持（外网域名全 fake-IP），BinFlow 无法直连 docker.io——其 SSRF 防护正确拒绝 fake-IP/私网上游（这本身是行为观察，见 D19）。
- 客户端: 真实 docker CLI 27.5.1（一次性 dind 容器 difftest-dind，经典存储栈；宿主 daemon 为 containerd snapshotter 且对 8082 的 TLS alert 不回退 HTTP，故用 dind，见证据报告 §0）。
- normalize: 剔除 Date/X-Request-Id/X-Artifactory-Id/Node-Id/X-Jfrog-Version/实例 id；token 值剔除；digest 与 status 保留；时间类只保留数量级。
- 凭据: 全文脱敏。

## 结论速览

**BinFlow docker remote 代理对任何标准 registry 均不可用：上游请求 wire path 缺 `/v2/` 前缀（`internal/adapter/docker/remote.go#v2WireManifestPath` / `#v2WireBlobPath` 返回 `image + "/manifests/" + ref`，无 `v2/` 前缀；对照 Artifactory `DockerUtil.downloadManifestUrl` 产 `v2/<image>/manifests/<ref>`，blob handler `executeGet` join 上游 URL 与该 path）。** 上游 registry 收到 `/busybox/manifests/t1`（无 /v2/）一律 404，BinFlow 折叠为 MANIFEST_UNKNOWN/BLOB_UNKNOWN。此为 LOOP 001 实现票的第一输入（一行级根因、manifest+blob 两处）。

case 统计：**SAME 2 / DIVERGENT 11 / UNKNOWN（blocked/unverifiable）5**（另加 skipped 1）。

## Case 明细（L 层级 = L0 HTTP primitive / L1 REST / L4 Auth / L5 Package protocol / L6 Remote / L8 Storage / L9 Cache）

### L4 Auth / token 流

**C01 /v2/ ping 挑战（匿名）** — Artifactory: `401`，`WWW-Authenticate: Bearer realm="http://<host>/v2/token",service="<host:port>"`，body compact UNAUTHORIZED。BinFlow: 同 401、同 body、同 realm 形态，但 **service="binflow"**（常量，非 host 回显）。**DIVERGENT**。归类建议：BUG（低危；docker token spec 的 service 语义 = registry host，客户端一般不校验，但 Artifactory 语义为 host 回显）。另：BinFlow token 响应带 `Docker-Distribution-Api-Version`，Artifactory 不带（微小）。

**C01b token 签发（Basic）** — Artifactory: `{token, expires_in:9000}`（无 issued_at）。BinFlow: `{token, access_token, expires_in:2592000, issued_at, scope}`（expires_in=30 天，token 为不透明 hex 非 JWT）。**DIVERGENT**。归类建议：UNKNOWN（expires_in 值 = 实例策略；额外字段是增量不破坏 docker 客户端——是否对齐 9000/JWT 形态待 authority 裁定；docker CLI 实测登录/拉取均兼容）。

**C02 坏凭据** — Artifactory: ping 与 token 均 `401` pretty `{"errors":[{"status":401,"message":"Bad Credentials"}]}`；CLI 渲染 `unknown: Bad Credentials`。BinFlow: ping 回匿名同款挑战（不区分坏凭据），token 回 `401 {"error":"invalid_client","error_description":"authentication required"}`（OAuth2 形态、非 docker errors 数组）；CLI 渲染 `unauthorized: authentication required`。**DIVERGENT**。归类建议：BUG（token 端点错误体应为 docker errors 数组形态；ping 不区分坏凭据与匿名属可接受——Artifactory 区分）。

**C03 匿名 token** — Artifactory: `401 "Authentication is required"`（匿名访问关）。BinFlow: `200` 发匿名 token（匿名开）。**DIVERGENT**。归类建议：UNKNOWN（实例匿名策略默认值差异——需产品裁定 BinFlow 默认匿名面；匿名 pull 端到端在 wire bug 修复前无法对照）。

### L5/L6 拉取与代理语义

**C04 真实 docker pull（hello-world）** — Artifactory: 成功（17.0s 首拉，digest sha256:5e230903...）。BinFlow: **失败**，`manifest unknown to registry: latest`；上游 registry 日志实收 `GET /busybox/manifests/t1`、`/probe-img-x1/manifests/t1`（**无 /v2/ 前缀**）。**DIVERGENT / BUG（根因如上，`v2WireManifestPath`+`v2WireBlobPath` 两处）**。注：同上游下 Artifactory 腿 200（0.7s）证明上游与凭据形态均可用，隔离出 BinFlow 侧根因。

**C09 blob GET（Range 0-63）** — Artifactory: `206` + Content-Range + 全套校验和。BinFlow: `404 BLOB_UNKNOWN`（同 wire bug；fresh-image blob 路径 95ms 证明有上游往返后仍 404）。**DIVERGENT / BUG**（同根因）。

**C12 manifest 404 形态** — Artifactory: `{"code":"MANIFEST_UNKNOWN","message":"The named manifest is not known to the registry.","detail":{"manifest":"library/<image>"}}`。BinFlow: `{"code":"MANIFEST_UNKNOWN","message":"manifest unknown to registry: <ref>","detail":{"reference":"<ref>"}}`。**DIVERGENT**。归类建议：BUG（message 串与 detail 键/内容均异；部分工具解析 detail.manifest）。

**C13 blob 404 形态** — Artifactory: detail 键 `blobSum`，message 静态。BinFlow: detail 键 `digest`，message 尾带 digest。**DIVERGENT / BUG（detail 键名）**。

**C15 tags/list** — Artifactory: `200` 上游聚合（pretty JSON，name 含 library/ 前缀，字典序全量 tag）。BinFlow: `404 NAME_UNKNOWN`（37ms 本地、无上游往返；detail.name 含 repoKey 全路径 `"audit-probe-docker-remote/library/hello-world"`）。**DIVERGENT / BUG**（远端 tags 聚合缺失 + 错误码词汇表偏离——Artifactory remote 面无 NAME_UNKNOWN）。

**C16 _catalog** — Artifactory: `200 {"repositories":[]}`（上游无 catalog API，仍回 200 空列表）。BinFlow: `404 UNSUPPORTED "path ... carries no registry route"`。**DIVERGENT / BUG**。

**C17 空 blob digest（sha256:a3ed95ca...）** — 两侧均 `200` 32B 合成响应 + `Docker-Content-Digest`。**SAME**。

### 上传面

**C11 remote 禁推** — Artifactory: `400` pretty `{"errors":[{"status":400,"message":"Unable to upload blobs/a manifest to a remote repository."}]}`（blobs uploads/PUT manifest/DELETE manifest 三文案），CLI 前缀 `unknown:`。BinFlow: `405` compact `{"code":"UNSUPPORTED","message":"Remote repository '<key>' is a read-only proxy cache; deployments to remote repositories are not accepted."}`（三端点同一文案）。**DIVERGENT**。归类建议：BUG（status 400→405、code/无 code、文案全异；对齐参照应回 400 + 参照文案。authority 可裁 INTENTIONAL——语义上 405 也成立，但兼容目标是 Artifactory 形态）。

### L9 缓存语义（部分被 wire bug 遮蔽）

**C14 404 负缓存** — Artifactory: 重复拉不存在 tag 仍每次上游往返（6.6s，missedRetrievalCachePeriodSecs=1800 配置在 docker manifest GET 面未见生效，E4）。BinFlow: 首次 miss ~200ms（上游往返失败后）→ 重复 miss 40-70ms 本地负缓存（missedRetrievalCachePeriodSecs=1800 生效）。**DIVERGENT**。归类建议：UNKNOWN（BinFlow 行为符合其配置语义；Artifactory 运行时未负缓存可能是 docker 面特例——需在 Artifactory 侧跨 missedTTL 窗复核后定论；若复核仍无负缓存则 BinFlow 更优、可裁 INTENTIONAL）。

**C05/C06/C07/C08/C10（缓存布局/manifest 头集/二次 pull/If-None-Match/marker 驱动语义）** — 依赖 C04 成功路径，BinFlow 侧被 wire bug 阻断。**UNKNOWN（blocked）**——修复后按证据报告 §10 协议重放即可闭合。C10 部分可判：BinFlow 对随机 digest blob 同样本地快 404（63ms，与 Artifactory marker 语义表面一致），但已知 digest 也 404 使语义无法区分。

### 环境与实例面观察（非协议 case，供 conductor/devops）

- **D19 SSRF 防护**：BinFlow 拒绝私网/ULA 上游（`allowPrivateUpstream` 默认 false，repo 级开关，实测 RFC1918+ULA 均拒，错误体 400 UNSUPPORTED 带详细 hop 信息）。Artifactory 无此防护（remote URL 任意可达即用）。**DIVERGENT / INTENTIONAL 候选**（BinFlow 安全姿态更强；本机 TUN fake-IP 环境下它是 BinFlow 无法直连 docker.io 的直接原因）。
- **D20 UAT 凭据 master key 缺失**：`docker logs binflow-ga`：`remote repository password dropped — no credentials master key configured (BINFLOW_REMOTE_CREDENTIALS_KEY)`——上游凭据静默降级匿名。部署配置缺口（devops 轨道），非产品 bug，但会导致需要凭据的上游全部失败而无显式错误。
- **D21 repo GET 回显**：BinFlow 回显顶层 `url` 为自指派生值（`http://localhost:8083/binflow/<key>`）而真实上游在 `configuration.url`；Artifactory 顶层 url 即真实上游。**DIVERGENT / BUG（API 兼容面，低危）**。
- **C18 上游 429 透传**：两侧均未触发，skipped（Artifactory 侧仅 E1 文案证据）。

## 差异汇总表

| # | case | Artifactory | BinFlow | 判定 | 建议 |
|---|---|---|---|---|---|
| C01 | ping 挑战 service | host 回显 | 常量 "binflow" | DIVERGENT | BUG（低） |
| C01b | token 字段集/TTL | {token,9000} | {token,access_token,2592000,issued_at,scope} | DIVERGENT | UNKNOWN |
| C02 | 坏凭据形态 | status-pretty "Bad Credentials" | ping 重挑战 + token OAuth 形态 | DIVERGENT | BUG |
| C03 | 匿名 token | 401 | 200 | DIVERGENT | UNKNOWN（策略） |
| C04 | 真实 pull | 成功 | 失败（上游 path 缺 /v2/） | DIVERGENT | **BUG（根因已定位）** |
| C05 | 缓存布局 | library/<img>/<tag>/list.manifest.json… | 不可验证 | UNKNOWN | blocked |
| C06 | manifest 头集 | Origin-Remote-Path 等全套 | 不可验证 | UNKNOWN | blocked |
| C07 | 二次 pull | 1.5s 命中 | 不可验证 | UNKNOWN | blocked |
| C08 | If-None-Match | 恒 200 不消费 | 不可验证 | UNKNOWN | blocked |
| C09 | blob Range | 206 | 404（同根因） | DIVERGENT | BUG（同 C04） |
| C10 | blob marker 语义 | marker 驱动 | 表面同（快 404）但语义不可分 | UNKNOWN | blocked |
| C11 | 禁推形态 | 400 + Unable to upload… | 405 + UNSUPPORTED read-only proxy cache | DIVERGENT | BUG（可裁 INTENTIONAL） |
| C12 | manifest 404 体 | detail.manifest + 规范 message | detail.reference + 短 message | DIVERGENT | BUG |
| C13 | blob 404 体 | detail.blobSum | detail.digest | DIVERGENT | BUG |
| C14 | 404 负缓存 | 无（重复 miss 仍上游） | 有（missedTTL 生效） | DIVERGENT | UNKNOWN |
| C15 | tags/list | 200 上游聚合 | 404 NAME_UNKNOWN | DIVERGENT | BUG |
| C16 | _catalog | 200 空列表 | 404 UNSUPPORTED | DIVERGENT | BUG |
| C17 | 空 blob 合成 | 200 32B | 200 32B | **SAME** | — |
| C18 | 上游 429 | E1 文案 | 未触发 | skipped | — |
| — | （auth ping 挑战 body/realm、401 码表、token 端点可作 Bearer 后续请求）| 一致 | 一致 | **SAME** | — |

统计：SAME 2 / DIVERGENT 11 / UNKNOWN 5 / skipped 1。BUG 建议 8、INTENTIONAL 候选 0（待裁）、UNSUPPORTED 0、UNKNOWN 待裁 3（C01b/C03/C14 + C11 可 flip）。

## LOOP 001 输入（按优先级）

1. **P0 wire path**：`internal/adapter/docker/remote.go` `v2WireManifestPath`/`v2WireBlobPath` 补 `v2/` 前缀（对照 Artifactory `DockerUtil.downloadManifestUrl/downloadBlobUrl`；BlobUnit blob handler join 语义）。修复后 C04/C09 通，C05-C08/C10 解锁重放。
2. P1 tags/list 远端聚合（C15）与 _catalog 空列表兼容（C16）。
3. P2 错误体兼容：token 端点 OAuth 形态→docker errors 数组（C02）；manifest/blob 404 的 detail 键与 message（C12/C13）；禁推 400+文案（C11）。
4. P3 ping service=host 回显（C01）。
5. 待裁：C01b token TTL/字段、C03 匿名默认、C14 负缓存对齐方向（需 Artifactory 跨 missedTTL 复核）。
6. 部署面：UAT 补 `BINFLOW_REMOTE_CREDENTIALS_KEY`（D20）；repo GET 回显修正（D21）。

## 复现命令（BinFlow 腿核心三行）

```bash
# 仓：PUT/POST /binflow/api/repositories/audit-probe-docker-remote {rclass:remote,packageType:docker,url:http://host.docker.internal:5588,allowPrivateUpstream:true}
docker exec difftest-dind docker pull host.docker.internal:8083/audit-probe-docker-remote/hello-world
docker logs difftest-upstream --since 2m   # 观察 GET /hello-world/manifests/latest（缺 /v2/）
```

## 环境清理记录（已执行，2026-09-11）

- Artifactory：`DELETE /artifactory/api/repositories/audit-probe-docker-remote` → 200（`deletedArtifactsCount: 22`，3.6s）；`audit-probe-docker-remote2` → 200（`deletedArtifactsCount: 5`，1.3s）。首次尝试 10s 超时为瞬态，60s 重试成功；删后 GET 该 repo → 400（Artifactory 缺仓形态）。
- BinFlow UAT：`DELETE /binflow/api/repositories/audit-probe-docker-remote` → 200（2.6s）；删后仓列表仅剩 devops 轨道自建的 `docker-local`（非本票资产，未动）。
- 一次性容器：`difftest-dind`、`difftest-upstream` 已 rm -f，`docker ps -a` 无 difftest 残留；dind 内登录凭据随容器销毁。
- 宿主侧残留：无（hello-world/busybox 镜像为用户 daemon 常规内容，保留）。

## 复验（L000-F，2026-09-11）——P0 wire path 修复后重放，原结论不改

- 修复: `internal/adapter/docker/remote.go` `v2WireManifestPath`/`v2WireBlobPath` 补 `/v2/` 前缀（上游 URL 语义 = registry 根）。BinFlow UAT 重建为 `uat-l000f-59f33ab5`（同端口 :8083、同卷；`deploy/compose/.env.uat` 的 `BINFLOW_VER` 指向新镜像）。
- 拓扑: 按证据报告 §10 重放——本地 registry:3（`difftest-upstream` 127.0.0.1:5588，hello-world:latest + busybox:t1/t2）作上游；repo `audit-probe-docker-remote`（remote/docker，url=`http://host.docker.internal:5588`，allowPrivateUpstream:true）；真实客户端 = 一次性 dind（docker 27.5.1，`--insecure-registry=host.docker.internal:8083`）。重放后一次性容器已 rm -f；UAT 侧 repo 与缓存保留（复验证据，后续差分可直接消费）。

### 灭因直证（C04/C09 根因）

上游 registry 日志实收（修复后，全部 200）：

```
"GET /v2/hello-world/manifests/latest HTTP/1.1" 200
"GET /v2/hello-world/blobs/sha256:e2ac70e7…685b HTTP/1.1" 200
"GET /v2/hello-world/blobs/sha256:4f55086f…98b4 HTTP/1.1" 200
```

### 逐案重放结论（原表判定 → 复验后）

| case | 原判定 | 复验观察（BinFlow 腿实测） | 复验判定 |
|---|---|---|---|
| C04 真实 pull | DIVERGENT/BUG | dind `docker pull host.docker.internal:8083/audit-probe-docker-remote/hello-world` 成功（1.94s）；digest `sha256:d1a8d0a4eeb6…9e59e` 与上游参照逐字一致 | **SAME（灭）** |
| C05 缓存布局 | UNKNOWN blocked | `hello-world/manifests/<hex>`(1023B) + `hello-world/blobs/<hex>`(config 2415B/layer 577B)，digest 寻址、行带 sha1/sha2；无 tag 路径副本、无 marker 文件、无 library/ 归一 | DIVERGENT（布局异构：BinFlow digest 键 vs Artifactory tag 目录+sha256__+marker） |
| C06 manifest 头集 | UNKNOWN blocked | GET/HEAD: Content-Type/Docker-Content-Digest/Docker-Distribution-Api-Version/X-Binflow-Cache(HIT)；无 Etag/Last-Modified/Accept-Ranges/Origin-Remote-Path。blob 面（全量与 206）带全套 X-Checksum-Md5/Sha1/Sha256 + Etag(sha1) + Accept-Ranges——与 Artifactory E2-3/E3-5 一致 | DIVERGENT（manifest 头集窄于参照；blob 面基本对齐） |
| C07 二次 pull | UNKNOWN blocked | rmi 后重拉：上游计数冻结 26→26，X-Binflow-Cache MISS→HIT 直接可观测；1.94s→1.42s（本地上游 RTT 小，标记+冻结计数为硬证据） | SAME（缓存快路径成立；BinFlow 另有显式 HIT/MISS 标记） |
| C08 If-None-Match | UNKNOWN blocked | 带引号/不带引号、GET/HEAD 一律 200 全量，永无 304 | SAME（与 Artifactory E3-2 客户端可见面一致） |
| C09 blob Range | DIVERGENT/BUG | `Range: bytes=0-63` → 206 + `Content-Range: bytes 0-63/577` + 64B + Docker-Content-Digest + 全套 X-Checksum-* + Etag | **SAME（灭；206 头套与 Artifactory E3-5 对齐）** |
| C10 marker 驱动 | UNKNOWN blocked | (a) 从未拉过 manifest 的链内 digest（busybox config）→ BinFlow 上游代取 200 落缓存（上游日志有往返）；(b) 随机 digest → 上游 404 一次（日志可见往返）→ 此后本地 15-42ms 负缓存 404 | DIVERGENT（语义已可分：BinFlow=digest 盲代理+负缓存；Artifactory=marker 门控无往返快 404） |

### 残留 DIVERGENT 未扩张核查（同场复测）

- C11 禁推：admin token 下 POST uploads/PUT manifest 均 405 + UNSUPPORTED read-only 文案——原形态不变。
- C12 manifest 404：`detail.reference` + 短 message——原形态不变。
- C13 blob 404：`detail.digest`（C10b body 实证）——原形态不变。
- C15 tags/list：上游加 t2 后 BinFlow 仍只回已缓存 `["t1"]`（上游实有 t1,t2）——远端聚合缺失的直接证据，DIVERGENT 维持且证据更锐（原 404 是 pull 被阻的次生现象；修复后 cached-tags 面可用，聚合能力缺口仍在）。
- C16 _catalog：repo 域 `/v2/<repoKey>/_catalog` 仍 404 UNSUPPORTED "carries no registry route"——原形态不变（根级 `/v2/_catalog` 可用并含远端缓存镜像条目）。
- C01 service 常量、C01b/C02/C03/C14、D19/D20/D21：未触碰（P3 service 回显经核查非一行改动——挑战构造两处 + 多测试字面断言，按票面条件留 LOOP 001）。

### 统计更新（复验口径）

SAME 2→6（+C04/C07/C08/C09）/ DIVERGENT 11→12（−C04/C09，+C05/C06/C10）/ UNKNOWN（blocked）5→0 / skipped 1 不变。LOOP 001 输入清单不变（P1 tags 聚合、P2 错误形态、P3 service 回显、待裁三项、部署面 D20/D21）。
