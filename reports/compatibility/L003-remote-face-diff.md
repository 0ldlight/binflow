# L003-2 remote 面头集+分页实现票对拍报告：docker-remote 契约残余 DIVERGENT 清偿

- Ticket: L003-2（dev-registry-adapter，docker 域）
- 日期: 2026-09-11
- 模式: **dual**（参照 :8082 Artifactory-pro 7.161.20 实测 vs BinFlow 验证实例 http://127.0.0.1:8084）
- BinFlow 基线: **uat-l0032-952e556d**（develop HEAD 952e556d + 未提交 L003-2 工作树，`git diff HEAD --stat` internal/adapter/docker 9 文件 +906/-58 行。**未复用共享 UAT 容器 binflow-ga（uat-l0033，并行轨道 L003-3 在测）**——独立容器 `binflow-l0032-verify`、独立卷 `binflow-l0032-data`、端口 8084，零共享状态）
- 上游: 本地 registry:3（difftest-upstream，127.0.0.1:5588，busybox:t1/t2/t3 + hello-world:latest + l0032-probe:1；busybox 本轮为 OCI 单构 manifest `sha256:1cfa4e2b…8525f8`，与 L001-1 轮同字节）
- 客户端: 真实 docker CLI 29.7.2（一次性 dind `difftest-dind`，--insecure-registry 覆盖 5588/8082/8084）+ curl 8.7.1（--noproxy '*'）
- normalize: fixtures/normalize.yaml#docker-remote（superset=tolerate_and_note；X-Binflow-Cache/X-Request-Id 为 BinFlow 超集头， tolerated-and-noted）
- 凭据: 命令行内使用，本报告一律脱敏
- 并行轨道干扰披露: 共享宿主 L003-3 轨道并发改 internal/httpapi（工作树红、整模块 `make lint` 暂红——全部 11 条告警在其 system_storage_admin*/storage_info 文件，L003-2 包面 lint 0 告警）；共享 Artifactory 参照实例本报告窗口内两次自重启（对拍腿均在 :200 窗口完成；尾部清理受 503/连接拒绝阻断，见「环境清理记录」）。

## 结论速览

**本票面 5 项全部落地并双端活体对拍通过**。受影响契约条目 6 条（#1/#2/#6/#7/#11 + 分页面）双端头集/体逐头逐值一致（BinFlow 既有超集头 tolerated）；建议翻绿 5 条（#1/#2/#6/#7/#11），分页为契约补条目面（known-divergence docker/remote-tags-catalog-pagination 从 UNKNOWN 转可实现态）。

**两项对重放报告的勘误级发现（上报 conductor / compatibility-engineer）**:
1. **L002-2 报告分页表「Link 头无」行与自身原始捕获矛盾**——`/tmp/l0022/a_tagsn.h`（?n=2）明确带 `Link: </v2/audit-probe-docker-remote/busybox/tags/list?last=t2&n=2>; rel="next"`；本轮 Artifactory 活体复现同形 Link。**Artifactory remote 面在截断时发 Link**，重放报告该行系笔误（裸 GET 不截断故无 Link，报告以裸 GET 行推了全表）。L003-2 按 ground truth 实现（截断即 Link），非按报告表格。
2. **repo 域 _catalog 的 Link 指向 registry 级路径**——Artifactory 从 `/v2/<repoKey>/_catalog?n=2` 发 `Link: </v2/_catalog?last=hello-world&n=2>; rel="next"`（**非** repo 域路径；本轮第三镜像 l0032-probe 注入后活体直证）。参照系自身的 quirk，L003-2 照抄（代码注释已注明）。

## 逐面对拍（票面 5 项）

### ① C06+#11 manifest/blob 头集 — 通过（建议 #6/#7/#11 翻绿）

BinFlow（fresh MISS，同参数同刻）vs Artifactory（fresh fetch），逐头：

| 头 | BinFlow (f_mf.h) | Artifactory (a_mf.h, 本轮) | 判定 |
|---|---|---|---|
| Accept-Ranges | bytes | bytes | 一致 |
| Content-Disposition | attachment; filename="manifest.json" | 同 | 一致 |
| Etag | 7b175af6…（=sha1，无引号） | 同值 | 一致（同 body 同 sha1） |
| Last-Modified | 03:10:39（=拉取时刻，与 Date 同秒） | 03:06:11（=拉取时刻） | 语义一致（值实例可变，derived 规则 manifest_last_modified_is_fetch_time 过） |
| X-Artifactory-Filename | manifest.json | 同 | 一致 |
| X-Artifactory-Origin-Remote-Path | http://host.docker.internal:5588/v2/busybox/manifests/t1（**请求引用逐字回显**） | 同形（…/manifests/t2，其请求为 t2） | 一致 |
| X-Checksum-Md5/Sha1/Sha256 | b9ee013f…/7b175af6…/1cfa4e2b… | 同三值 | 一致 |
| Docker-Content-Digest | sha256:1cfa4e2b… | 同 | 一致 |
| X-Binflow-Cache / X-Request-Id | MISS / 有 | 无 | BinFlow 超集，tolerated_and_note |

- body sha256 == 上游推送回执 == Docker-Content-Digest（1cfa4e2b…8525f8）✓；Etag/X-Checksum-Sha1 与 `shasum -a 1` 独立核算一致 ✓。
- **缓存 HIT 头集不缩水**（E2-3 语义维持）：二拉 manifest GET 带 X-Binflow-Cache: HIT + 全套 face（Etag/Last-Modified/Origin-Remote-Path/Checksum 族）✓。
- **manifest HEAD（by digest）**: BinFlow = 参照活体窄集（Docker-Content-Digest/Last-Modified/X-Artifactory-Docker-Registry=audit-l0032-docker-remote）**+ GET 全集**（Etag/校验和族/Filename/Origin-Remote-Path——对参照活体为超集）。参照活体 HEAD（a_mfh.h 本轮复现）无 Etag/校验和族——契约 #7 对参照侧的高估维持上报（L002-2 escalation #3）；BinFlow 超集姿态满足契约现行 present/derived 断言（条目无 absent 断言），**建议按 BinFlow 现状翻绿**，compatibility-engineer 复核条目时可择「收窄 expect 至参照活体窄集（BinFlow 超集 tolerated）」或维持现行全集 expect——两口径下 BinFlow 均过。
- **blob GET/206**: Last-Modified=落缓存时刻（03:10:39，与 manifest 同秒=同轮拉取）+ `Content-Disposition: attachment; filename="sha256__c6348fa8…"` + X-Artifactory-Filename 同值 + Origin-Remote-Path（…/blobs/sha256:c6348fa8…）+ 既有校验和族/Etag——**与 a_blob.h/a_blobr.h 逐头逐值一致**（校验和 e6329bed/105e5808 双端同值，同 blob）。206 窗口头集不缩水 ✓。
- 附注（契约面残留）: 契约 #11 的 body note 称「blob Last-Modified=上游真实 mtime（E2-5）」，与两轮活体（L002-2 落缓存时刻 + 本轮 03:10:39=拉取秒）不符——E2-5 的 docker.io 腿观察与 registry:3 腿冲突，建议 compatibility-engineer 复核该 note（BinFlow 按活体实现=落缓存时刻）。

### ② #1 ping Content-Type charset — 通过（建议翻绿）

- BinFlow 匿名 ping: `Content-Type: application/json;charset=ISO-8859-1` + Bearer 挑战（service=host 回显）+ compact UNAUTHORIZED 逐字——与 a_ping.h 逐头一致（X-Request-Id 超集 tolerated）。
- 实测 charset 形态**仅 ping 两 401 臂**：全捕获面（tags/catalog/token/404/400 族）双端均裸 application/json，实现按此收窄（errors.go 的 CT 参数化 + 仅 ping 臂传 charset），未波及其他已验证面。

### ③ #2 ping 坏凭据臂 — 通过（建议翻绿）

- BinFlow: 401 + `WWW-Authenticate: Basic realm="Artifactory Realm"` + pretty `{"errors":[{"status":401,"message":"Bad Credentials"}]}` + charset Content-Type——与 a_pingbad.h 逐字对齐（api-version 头为 BinFlow 超集，tolerated）。
- 契约条目 expect 的 `WWW-Authenticate: ^Bearer …` pattern 需同步改锚为 `^Basic realm="Artifactory Realm"$`（条目面裁定归 compatibility-engineer，本报告只建议）。
- 实现面注记：httpapi 路由对「出示但被拒」凭据经 v2AuthFailure seam 委托 docker.RenderAuthFailure——ping 臂分支全在 adapter 包内（handler.go），**未触碰 internal/httpapi**（票面边界守住）。

### ④ 分页透传 — 通过（契约补条目面；known-divergence 转可实现态）

双端活体（tags: 本轮 a_tagsn/a_tagsl/a_tagsinv vs f_tags*；catalog: 第三镜像注入后）:

| 请求 | Artifactory | BinFlow | 判定 |
|---|---|---|---|
| tags `?n=2` | [t1,t2] + Link `</v2/audit-l0032-docker-remote/busybox/tags/list?last=t2&n=2>; rel="next"` | 同 + **Link 逐字节一致** | 一致 |
| tags `?n=2&last=t1` | [t2,t3]，无 Link | 同 | 一致 |
| tags `?n=abc` | 404 pretty "Not Found" | 同（Content-Type 裸 application/json 同） | 一致 |
| tags 裸 GET | 全量 [t1,t2,t3]，无 Link | 同 | 一致 |
| catalog `?n=2`（3 仓） | [busybox,hello-world] + Link `</v2/_catalog?last=hello-world&n=2>; rel="next"` | 同 + **Link 逐字节一致**（含 /v2/_catalog 路径 quirk 照抄） | 一致 |
| catalog `?n=2&last=busybox` | （未单拍；tags 面同语义已证） | [hello-world,l0032-probe] | 实现对称 |
| catalog `?n=abc` | （未单拍） | 404 "Not Found" | 实现对称 |
| catalog 裸 GET | 全量 3 仓（活体聚合，l0032-probe 双端同见） | 同 | 一致 |

- invalid-n 404 **先于上游接触**（单测直证 hits=0）。
- 未观察角落（实现自决，注记）: n=0/负数按「非正整数」并入 404 家族；catalog 的 last/invalid-n 臂 Artifactory 侧未单拍（tags 面同构实现）。

### ⑤ Review A 微票三件 — 通过

- **refs-unparsed WARN**: remoteManifestRefs 改返 parse error，landFetchedManifest 落 WARN（cache_result=refs-unparsed，字段对偶 chain-unrecorded 行）——单测覆盖（manifest 面解析失败路径）。
- **TestChainGateColdDigestPosture 重复链外腿**: 二次链外 GET 同形 404 + 上游 blob 往返计数冻结（2 不变）+ 负缓存行仍不落——单测通过。
- **T-L002-1.md 勘误**: 两处「17 包」→「16 包」+ 文末勘误注记（`go list` 实数 16 复核）。

## 回归面（非本票条目，防退化）

- **#8 条件 GET**: GET t1 + `If-None-Match: "<sha1>"` → 200 全量 610B（永无 304，E3-2 语义维持——Etag 的加入未引入条件求值）。
- **#9/#10 pull 序列**: dind 真客户端 `docker pull :8084/…/busybox:t3` exit 0、digest 逐字（1cfa4e2b…==上游回执==dind 本地）；rmi 后二拉 exit 0，上游 t3 manifest 计数冻结（本轮 fresh 实例 1 次往返后 0 新增——上游日志逐条归因，access+otel 双行同请求计 1）；hello-world:latest 拉取 exit 0。
- **#17/#18**: tags 裸 GET 全量 pretty（name=busybox 上游回显）/catalog 活体聚合——维持。
- 四门: build/vet/gofmt(-l internal/adapter 为空)/golangci-lint（docker+helmoci 包 0 告警）；docker+helmoci+repo 三包 -count=1 全绿；race 子集（face/pagination/ping/chain-gate/pull/renderauth 家族）全绿。整模块 make lint 红 = 并行轨道 httpapi WIP 文件（11 条告警逐条位于其 system_storage_admin*/storage_info_*，无一条在本票文件）。

## 契约条目处置建议（判定权归 conductor / compatibility-engineer）

| 条目 | 现状 | 建议 |
|---|---|---|
| #1 docker/remote-v2-ping-anonymous | DIVERGENT | **翻绿 VERIFIED**（charset 断言本轮过；binflow_state 刷新至 uat-l0032-952e556d） |
| #2 docker/remote-v2-ping-bad-credentials | DIVERGENT | **翻绿**，同时 expect.headers.pattern 的 Bearer 形改锚 `^Basic realm="Artifactory Realm"$` + literal Content-Type charset |
| #6 docker/remote-manifest-get-tag | DIVERGENT | **翻绿 VERIFIED**（全 derived/present/literal 断言活体过；HIT 面不缩水附记） |
| #7 docker/remote-manifest-head-digest | DIVERGENT | **翻绿**（BinFlow=参照窄集+GET 全集超集；或先按 L002-2 escalation #3 复核条目 expect 再翻——两口径均过） |
| #11 docker/remote-blob-get | DIVERGENT | **翻绿 VERIFIED**（Last-Modified/Origin-Remote-Path 补齐；body note 的「上游 mtime」语义建议按活体改「落缓存时刻」） |
| 分页（无条目，known-divergence docker/remote-tags-catalog-pagination=UNKNOWN） | UNKNOWN | **补契约条目**（surface: tags/list 与 _catalog 的 n/last/invalid-n/Link 四臂，本轮双端活体值已可执行化）；L002-2 报告「无 Link 头」行按本报告勘误 |

## 残留 / 风险

- **（收编补充测新增证）ping 面拒绝凭据的 Bearer 类臂消息差**: Artifactory 活体（本轮 :8082，/v2/ + `Authorization: Bearer <未知值>`）= **同 Basic realm 挑战 + charset CT + pretty `"Props Authentication Token not found"`**——与 bad Basic 臂的 "Bad Credentials" 是同形不同消息。BinFlow 现实现将所有被拒凭据类渲染同一 "Bad Credentials"（挑战头/信封形态已对齐，消息粒度未分）。**消息级 delta 登记为残留**（实现归后续票；本票收编按 conductor 指令 test-only 更新了 internal/httpapi 四处期望为 BinFlow 实测形态，挑战头+信封与差分一致）。
- 虚拟面（virtual repo 经 remote member 服出）头集无契约无证据：本票让 fetch 臂带 Origin-Remote-Path（member facts 在手）、HIT 臂缺省（省一次 seam 往返）——虚拟面证据出现时补。
- blob Last-Modified 语义（落缓存时刻 vs E2-5 的上游 mtime）待 compatibility-engineer 复核裁断（两轮 registry:3 活体均=落缓存时刻）。
- n=0/负数、catalog last/invalid 的 Artifactory 臂未单拍（对称实现）。
- schema1 HEAD 无 Accept 的参照怪癖（L002-2 #7 附注）未实现——BinFlow 同条件回正确 v2 manifest，维持现状上报。

## 环境清理记录（已执行完毕）

- BinFlow 验证实例: 仓 `audit-l0032-docker-remote` DELETE ?deleteContent=true → 200；容器 `binflow-l0032-verify` 与卷 `binflow-l0032-data` 已 rm。
- Artifactory: 仓 `audit-l0032-docker-remote` DELETE → **200（已删）**——中途被参照实例自身故障阻断两轮（DELETE 503 → 整体连接拒绝，窗口内多次自重启；**非本票操作所致**——对拍腿全部完成于其 :200 窗口），实例恢复后重试即闭。
- 一次性容器 `difftest-dind`/`difftest-upstream` 已 rm -f（上游 seeds 随之清空；l0032-probe 同灭）。
- 未触碰: 共享 UAT binflow-ga（uat-l0033，并行轨道）、l0033-minio/mctrace（并行轨道）、postgres、共享 Artifactory 实例本体。

## 原始证据索引

/tmp/l0032/（本机暂存;`bf_`/`f_`=BinFlow 腿、`a_`=Artifactory 腿）: f_ping*/f_pingbad*、f_mf*/f_mfh/f_blob*/f_blobr*、f_tags*/f_cat*（四臂）、a_mf*/a_mfh/a_blob*、a_tagsn*/a_tagsl*/a_tagsinv*/a_tags*、a_catn2*、bf.tok、alegs.sh/legs.sh/final.sh（腿脚本原文）。
