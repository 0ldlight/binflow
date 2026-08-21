# PRD — M2 云原生旗舰：Docker Registry v2（含 Helm OCI）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-2.md` |
| 里程碑 | M2 — Docker Registry v2（对应 ROADMAP.md「M2 — 云原生旗舰」全部条目） |
| 状态 | **v1.4**（T-121：T-105 QA 勘误 E1 收口——D06/D10 的 upload `Location` 为相对路径，命令物化 `$REG` 前缀 + FR-8/DE-02 注记，行为零变更。v1.3（Q1/ADR-0010、§6.5 定案无遗留；T-43 三项勘误已收口；T-44 四项勘误收口：ping 无条件挑战（C6/D44-1）、offline_token 接受并忽略（C5/D44-2）、token POST 表单凭据口径（C4/D44-3）、删光 manifest 后 tags/list 200 空（C7/D44-6））） |
| 上游依据 | PRODUCT.md、ROADMAP.md M2 节、M1 交付基线（T-18-qa/T-19-qa 全绿 + 观察项 O1~O4）、ADR-0005/0006/0008/0009、docs/design/architecture.md（M1 定稿） |
| 下游消费者 | tech-lead（拆票）、architect（ADR-0010 路由细化 / 设计更新）、dev 各角色、qa-engineer（D 序列验收）、release-engineer（部署烟测） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-18 | 初版：M2 范围、FR-7~FR-13、兼容矩阵（Registry v2 spec 对齐 + 层级归属）、D01~D24 验收命令、五客户端分级矩阵、M1 观察项 O1~O4 处置、Q1 路由方案两案对比（待定） |
| v1.1 | 2026-08-18 | 采纳 tech-lead T-32 风险清单 R1/R2/R4/R5 + Q1 用户定案（ADR-0010），依据 `docs/reverse/docker-registry.md`（已落地）：① **R1（硬时序，赶 T-37 token 票派发前）**——token realm 由 `/binflow/api/security/token` 改为 **`/v2/token`**（adapter 自有端点，ADR-0010 定案；`Docker-Distribution-Api-Version: registry/2.0` 头随附）；DE-13/FR-11/D04/D04b/NFR-S9 同步更新；新增「双 token 入口并存」说明（M1 端点 admin-only 管理面 vs `/v2/token` 任意有效用户 docker login 语义入口——设计而非矛盾）；Q1 定案标注：根级 `/v2`（ADR-0010），§7 两案对比表转为决策记录、D 序列 `REG=localhost:8080` 口径坐实。② **R2**——FR-9-AC7 by-tag 删除由 202 改为 **405 UNSUPPORTED**（docker-registry.md §8 高置信度：官方 spec 禁止 by-tag；§10 建议栏 3）；D13c 的 by-tag 断言删除。③ **R4**——FR-10 空仓 `tags:null` 口径维持，补 QA 断言注记（`jq '.tags == null'`，防 T-43 脚本误写空数组）。④ **R5**——§6.5 四项按规格定案：manifest 上限 4MB、`offline_token=true` → 400 `invalid_request`、`service` 值固定 `"binflow"`、catalog `last` 游标 exclusive；§6.5 由「待校准」改「校准记录」，相应暂定值在 FR-11/DE-11/DE-12 落地 |
| v1.2 | 2026-08-19 | T-43 QA 勘误三项收口（C1/C2/C3，见 reports/agents/T-43-qa.md「PRD 口径勘误」节）：① **C3+D3（PM 裁定）**——`/v2/token` 端点的错误响应**统一 OAuth 形** `{"error","error_description"}`（含 400 参数错与 401 凭据错），资源端点仍用 spec 错误体；v1.1 三处打架表述（FR-11-AC7=spec / §5.2 注=OAuth / NFR-S10=/v2 全 spec）对齐，裁定理由：与 T-37 主实现 400 路径及 M1 管理面 token 端点（E-17 OAuth 形）同族、docker 客户端不解析 401 body 无兼容性损失；实现侧 401=spec 的一行修由 conductor 另派（T-37）。② **C1**——D06/D07/D10/D12/D13/D13d 的 name 由单段（`/v2/<repoKey>/...`）改为全名 `<repoKey>/<image>`（单段 name 实测 404，T-33 评审 AC；mount `from=` 要求全名）；D01/D06 补 name 模型注记与单段 404 断言；两处联动一并收敛：FR-8-AC6 的 mount AC 示例改全名、D04b/D04c 手工协商的 scope 值（`repository:<repoKey>/<image>:...`）改全名。③ **C2**——DE-13/FR-11 补 scope 推导注记：`/v2/` ping 挑战无 scope，scope 仅 repository/catalog 资源端点推导（四形态：GET→pull / 写→pull,push / DELETE→pull,delete / catalog→registry:catalog:*，T-43 全数实测） |
| v1.3 | 2026-08-19 | T-44 QA 勘误四项收口（C4~C7，见 reports/agents/T-44-qa.md 缺陷 D44-1/2/3/6 与勘误节）：① **C6+D44-1（P0，最重要）**——DE-01/FR-11-AC2/D04 的「匿名开 → ping 200 `{}`」口径**推翻**，改为「**未认证 ping 一律 401+Bearer 挑战（无条件，不随 anonymous_access 变化）**；已认证 → 200 `{}`」：docker daemon/containers-image 为 ping-缓存型认证器，ping 200 即判「无需认证」，后续 401 不再协商 token（实证：默认配置 push 零凭据全断、错口令 login 也 Succeeded）；真实世界（Docker Hub/GHCR/Harbor）匿名可 pull 但 ping 仍挑战，匿名访问经匿名 token 通行（docker-registry.md §0/§5 同构）。**匿名拉取不受损**：挑战 → 客户端无凭据请求 `/v2/token` → `anonymous_access=true` 时照发 pull scope 匿名 token → 匿名 pull 照常（podman 挑战模式已实证全通）；FR-11-AC4（错凭据 login 拒绝）随 ping 挑战化在默认配置下恢复有效。② **C5+D44-2（P0）**——§6.5② 「offline_token=true → 400」**推翻**，改为「**接受并忽略**」（官方 distribution token spec 明文定义该参数且 server MAY ignore；docker 29.x daemon login 必带——400 直接打断挑战模式 login；Artifactory 实况亦为接受，docker-registry.md §5.2）；FR-11-AC7/D04c 断言改为 200 正常 token（无 refresh_token）。③ **C4+D44-3（P0，口径补明）**——`/v2/token` 的 **POST 表单凭据**：无 Basic 头时解析 OAuth 表单（`grant_type=password` 的 `username`/`password`，或 `client_id`/`client_secret` 对），与 Basic 同权；两者皆缺才按匿名（官方 token spec OAuth 流凭据在 form body——buildx/helm 客户端用此形态）。④ **C7+D44-6（P2）**——「删光最后一个 manifest」后 tags/list 由 404 `NAME_UNKNOWN` 改为 **200 且 `"tags":null`**（name 持续性语义，OCI conformance 期望；与 R4 空仓形态统一）；404 仅保留给「无任何登记的 name」；FR-10-AC3/DE-12/D09 注记同步 |
| v1.4 | 2026-08-21 | T-121 勘误（T-105 QA 建议 **E1**——M2 侧仅此一条；T-105 的 E 编号独立于 T-43 的 C 编号与 T-103 的 E 编号，后者分别在本文 v1.2 与 M4 PRD v1.3 收口）：**E1** §5.4 D06/D10 的 upload `Location` **为相对路径**（`/v2/<name>/blobs/uploads/<uuid>`——T-43 3.1 实测定案，distribution spec 允许相对引用，docker/oras 等真实客户端自行按 RFC 3986 拼 base，无兼容性损失）——v1.0~v1.3 的命令字面把 `$LOC` 直接当 URL 用，照抄执行会请求失败（T-105 §2.4 修正账 D06/D10「客户端旗标」项：QA 拼接 base 后全绿）。修正：D06/D10 两处捕获改 **`LOC=$REG$(...)` 物化拼接**（D10b 的 offset 查询随之可用）；FR-8 行为规格与 DE-02 矩阵行补「相对路径」注记。**服务端行为零变更**（返回值不变），仅验收命令与文档口径修正 |

---

## 1. 背景与目标

### 1.1 背景

M1 已交付「存储引擎 + 仓库模型 + Generic 闭环 + 认证骨架」，全部 P0/P1 AC 经 qa 全绿（T-18/T-19），`m1-done` 已打 tag。M2 是 BinFlow 的**云原生旗舰能力**：Docker Registry v2（含 OCI image/spec 与 Helm OCI 承载）——这是 PRODUCT.md 成功标准第一条「docker push/pull 真实客户端全链路可用」的主体，也是内网镜像分发场景（目标用户「离线与受限网络」）的核心价值。

M2 在 M1 地基上**追加**而非返工：
- 存储复用：docker blob 与 generic 文件走同一 checksum 寻址 filestore（ADR-0006 布局不变），layer 去重免费获得；
- 元数据复用：`nodes`/`blobs` 表加 docker 语义视图（manifest 以 node 形态存储，tag 是轻量指针表），schema 变更走既有迁移器（ADR-0007）；
- 认证复用：M1 的 Basic/Token 中间件按前缀挂载能力（PRD M1 §5.4 前向兼容需求 3）在 M2 兑现——`/v2/` 挂 Bearer token 流。

### 1.2 M2 目标

> 一句话：交付一个 `docker login/push/pull` 完整可用、通过五真实客户端（docker/podman/crane/skopeo/oras）conformance 的 Docker Registry v2 本地仓库，且 Helm chart 可经 oras 推拉。

量化门槛（未达即里程碑不完成）：

| 指标 | M2 门槛 | 来源 |
|---|---|---|
| 客户端 conformance | §5.3 分级矩阵：docker/podman/crane/oras 全过（P0 项），skopeo 全过（P1） | ROADMAP M2「conformance 全过」 |
| token 认证流 | `docker login` 对 `auth` 端点的 Bearer/Www-Authenticate 协商成功（D05） | ROADMAP M2 token 认证流 |
| 镜像完整性 | push 后 manifest digest 与客户端侧 `docker manifest inspect` 一致；pull 到全新 daemon 后容器可运行（D08） | PRODUCT 成功标准 |
| 去重继承 | 同一 layer 在不同 tag/镜像间只存一份 blob（`/binflow/api/v1/storage/stats` 计数不变，D11） | PRODUCT 核心能力 1 |
| 分块上传 | chunked PATCH ≥ 3 段成功且 GET 回的 blob 与源逐位一致（D10） | ROADMAP M2 chunked |
| 冷启动不回退 | 空库冷启动 < 2s（含 docker 路由注册后，D20） | M1 NFR-P1 不回退 |
| 部署烟测 | Docker 镜像 + compose 两条部署路径烟测绿（release-engineer） | ROADMAP M2 + DoD 第 3 条 |

### 1.3 上游依赖

- **逆向规格**：`docs/reverse/docker-registry.md` **已落地**（v1.1 起作为本文行为依据之一）：Registry v2 / OCI **有公开官方规范**（distribution spec、OCI image-spec），以官方为准（ADR-0001 clean-room 铁律）；规格只补「Artifactory 对 spec 的偏离与补充」。v1.1 已按规格完成 R1/R2/R4/R5 回写（§6.5 四项定案），本文与规格现处一致状态；后续规格若有勘误按同流程回写。
- **ADR-0010（已定案）**：`/v2` 挂载形态 = **根级例外直挂**（用户定案，见 §7 决策记录）；D 序列 `REG=localhost:8080` 口径随之坐实。

---

## 2. 范围

### 2.1 In scope（与 ROADMAP M2 条目一一对应）

| # | ROADMAP 条目 | 本 PRD 功能需求 |
|---|---|---|
| 1 | blob upload 协议（POST/PATCH/PUT，monolithic + chunked） | FR-8 |
| 2 | manifest schema2 / OCI 存取（by-digest / by-tag） | FR-9 |
| 3 | `/v2/_catalog`、tags/list；docker login 的 token 认证流 | FR-10 + FR-11 |
| 4 | Helm OCI 承载（oras 客户端可用） | FR-12 |
| 5 | conformance：五客户端全过 | FR-13 + §5.3 分级矩阵 |
| 6 | 部署烟测：Docker 镜像 + compose（release-engineer） | FR-14 |
| 7 | （M1 遗留收编）观察项处置 + docker 仓库的管理面打通 | §4 FR-7 + §6 |

（M2 的逆向规格、架构 ADR 为流程条目，作上游输入。）

### 2.2 Non-goals — M2 明确不做（防范围蔓延）

| 不做项 | 归属 | M2 的隔离边界 |
|---|---|---|
| remote docker 仓库（Docker Hub 等代理缓存/pull-through） | M3 | `rclass=remote` 仍全局 400（M1 E-07 边界不变）；docker 类型的 remote 在 M3 与 generic remote 一并交付 |
| virtual docker 仓库（聚合解析） | M3 | 同上，`rclass=virtual` 400 |
| GC 的 manifest 级联删除（删 manifest 后其独占 layer 的引用链回收） | M4 | M2 的 GC 仍是 M1 的 blob 级 mark-sweep（ADR-0006）；删 manifest 只删 node + tag 行，孤儿 layer 靠既有 grace 期 GC 物理回收——**语义上够用**（grace 后回收），M4 才做「删 manifest 即时级联」的体验优化 |
| 存储布局变更 | — | 布局继承 M1（ADR-0006：`blobs/<2hex>/<sha256>` + `sessions/<uuid>/`）；docker blob 与 generic 文件同池。chunked 会话复用 `sessions/` 目录但 state.json 需扩 offset/已收字节——state.json 内容**不是**兼容承诺（ADR-0006 勘误①），bump version 即可 |
| Docker Hub v2 API 的非分发端点（`/v2/` 之外的 `/api/`、用户/组织/仓库管理） | 不排期 | 只实现 **distribution spec** 端点集；`/v2/` 下 spec 未定义的路径 404 + E-26 |
| manifest 列表中的 foreign/non-distributable layer 特殊处理 | 不排期 | 按 OCI spec 正常存取（存 node），不做来源校验 |
| token 的 OAuth2 完整语义（refresh_token 流、offline token） | M6+ | M2 只做 distribution token 协议所需子集（§4 FR-11）；`refresh_token` grant 返回 `unsupported_grant_type` |
| Web 控制台 docker 视图 | M4 | M2 无 UI |
| Prometheus `/metrics` | M6+ | 不做（ADR-0008：`/metrics` 端点路径已预留） |
| 镜像签名/attestation 语义（cosign 等） | M3+ 评估 | M2 只保证 manifest/artifact 以 OCI 形态透传存储（oras 可用即间接覆盖 referrers 场景的存储面）；referrers API（`/v2/<name>/referrers/`）**不做**，404 + E-26 |

---

## 3. 用户与场景（M2 视角）

- **场景 A（CI 镜像构建推送）**：流水线 `docker build` 后 `docker push $REG/acme/app:build-123`，凭 CI 账号 + token 完成 Bearer 认证；构建产物含 SHA 标签供部署侧引用。
- **场景 B（内网镜像分发）**：air-gapped 集群的节点 `docker pull $REG/acme/app:v1.2.0`，匿名读走「ping 挑战 → 匿名 token（pull scope）→ 拉取」路径（v1.3/C6：docker 域匿名读机制与 M1 generic 直读不同但语义一致，`anonymous_access` 默认开仍生效）或经反代注入认证。
- **场景 C（运维搬运）**：平台工程师用 `skopeo copy docker://docker.io/library/nginx:alpine docker://$REG/library/nginx:alpine` 把公网镜像搬进内网，再用 `crane` 做体检/清单 diff。
- **场景 D（Helm 交付）**：平台团队 `oras push $REG/charts/myapp:1.0.0 --config chart.json:application/vnd.cncf.helm.config.v1+json ./myapp-1.0.0.tgz:application/vnd.cncf.helm.chart.content.v1.tar+gzip`（v1.3/C4 双类型形态）或 `helm push`（helm 客户端 OCI 模式），部署侧 `oras pull` / `helm pull` 取回。
- **场景 E（多 tag 共存）**：同一镜像打 `v1.2` 与 `latest` 两 tag：`docker tag` + 二次 push，只多一条 tag 行，layer 零重传（去重继承）。

---

## 4. 功能需求

约定延续 M1：`BASE=http://localhost:8080`、`admin`/`$ADMIN_PW`；新增 `REG=localhost:8080`（docker 客户端目标，**不含** `http://` 与路径——docker CLI 只接受 `host[:port]`，这是 §7 Q1 的根源）。D 序列命令见 §5.4。

### FR-7 docker 仓库类型与管理面打通（dev-go-core）

**用户故事**：作为管理员，我希望能用 M1 已有的仓库管理 API 创建 docker 类型本地仓库，且 generic 仓库的既有行为分毫不变——这样 M1 的迁移脚本与 CI 无需任何修改。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-7-AC1 | `packageType` 接受 `docker`：D01 建仓 `{"rclass":"local","packageType":"docker"}` → 200 `Successfully created repository 'docker-local'`；C06 查询返回 `packageType=="docker"` | P0 |
| FR-7-AC2 | generic 行为零回归：M1 C 序列 P0 项（C03/C05/C06/C07/C08/C10/C13/C14/C18）在 M2 构建产物上复跑全绿（qa 以 T-18 场景 3/4 为回归基线） | P0 |
| FR-7-AC3 | repo key 保留字 `v2`（M1 已拒）维持；`api` 维持拒绝 | P0 |
| FR-7-AC4 | item info（`GET /binflow/api/storage/docker-local/...`）能列出 manifest/tag 对应的 node（形态：`mimeType=application/vnd.docker.distribution.manifest.v2+json` 等）；字段全集同 M1 FR-3-AC6（size 字符串/originalChecksums 等） | P1 |
| FR-7-AC5 | 删除 docker 仓库走既有 `DELETE /binflow/api/repositories/{key}?deleteContent=true`，删除后 `/v2/_catalog` 不再含该 repo | P1 |

### FR-8 blob upload 协议（dev-registry-adapter）

**用户故事**：作为 docker/oras 客户端，我希望按 distribution spec 的三种上传方式（POST-then-monolithic-PUT / POST-then-chunked-PATCH-PUT / POST 单请求 monolithic）把 layer/blob 推进仓库，任一方式推的 blob 都能被其它方式拉回——这是 spec 兼容性的地基。

行为规格（对齐 distribution spec，置信度高=官方 spec 明文）：
- `POST /v2/<name>/blobs/uploads/`： initiating；可带 `?digest=<sha256>`（单请求 monolithic，body 即内容，201 Created + `Location: /v2/<name>/blobs/<digest>`）；不带 digest → 202 Accepted + `Location: <upload-session-url>`（**相对路径**——spec 允许相对引用，客户端需拼 scheme://host；T-43 3.1 定案，v1.4/E1 注记）+ `Docker-Upload-UUID` 头 + `Range: 0-0`。
- `PATCH <upload-url>`：chunked 追加，`Content-Range` 可选（给了则必须与服务端已收字节数吻合，否则 416 Requested Range Not Satisfiable + `Range: 0-<received>`）；成功 202 + `Location` + `Range: 0-<received>`。
- `PUT <upload-url>?digest=<sha256>`：终结上传；body 可空（流已在 PATCH 推完）或带最后一段；校验 sha256 一致，不一致 400（spec：`DIGEST_INVALID`）。
- `GET /v2/<name>/blobs/<digest>`：200 流式返回；支持 `Range`（M1 已实现的 206/416 语义直接复用，Q6 定案的 M2 前置条件在此兑现）；404 用 spec 错误体（§5.2）。
- `HEAD /v2/<name>/blobs/<digest>`：200 + `Content-Length` + `Docker-Content-Digest`，无 body。
- 跨镜像去重：blob 按 sha256 全局寻址（与 generic 同池），`POST ...uploads/?mount=<digest>&from=<repo>` 的 cross-repo mount **M2 做**（spec 明文，crane/oras 高频路径）→ 201 直接挂载成功。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-8-AC1 | D06（monolithic PUT）：10MB blob 经 POST→PUT 推送，201 + `Location` 含 digest；随后 `GET` 回的内容 sha256 与源一致 | P0 |
| FR-8-AC2 | D07（单请求）：`POST ?digest=` 带 body 直接 201 | P0 |
| FR-8-AC3 | D10（chunked）：≥3 段 PATCH（如 1MB+1MB+8MB）全部 202，`Range` 头递增正确；终结 PUT 201；GET 回逐位一致（`cmp`） | P0 |
| FR-8-AC4 | D10b（中断恢复）：PATCH 2 段后 `GET <upload-url>` → 204 + `Range: 0-<received>`（spec 的 offset 查询），续传第 3 段成功终结 | P1 |
| FR-8-AC5 | D12（digest 校验失败）：PUT `?digest=` 值与内容不符 → 400，spec 错误体 `DIGEST_INVALID`；该 session 不产生 blob | P0 |
| FR-8-AC6 | D13（mount）：`POST /v2/charts/myapp/blobs/uploads/?mount=<已有digest>&from=docker-local/acme/app` → 201，零 body 传输（**目标 name 与 `from` 均为全名 `<repoKey>/<image>`**，v1.2/C1；单段 name → 404） | P0 |
| FR-8-AC7 | blob 与 generic 同池去重：`curl -T` 经 generic 仓库上传内容 X，再经 docker blobs 接口推相同内容 → `/binflow/api/v1/storage/stats` 的 blob 计数不变（D11 变体，跨协议去重） | P1 |
| FR-8-AC8 | `Range` 拉取：`curl -H 'Range: bytes=0-99' <blob-url>` → 206（复用 M1 实现，docker 客户端偶发断点续拉依赖） | P1 |
| FR-8-AC9 | 会话落盘：chunked 上传中断（kill -9）后重启，未终结 session 的 blob 不可见（GET 404），`sessions/` 残渣由既有 ttl 清理——M1 FR-2-AC4 语义在 docker 路径的延续（D14） | P0 |

### FR-9 manifest schema2 / OCI 存取（dev-registry-adapter）

**用户故事**：作为镜像使用者，我希望 push 进来的 manifest（无论 docker schema2 还是 OCI）能按 digest 精确取回、按 tag 方便引用，且 by-digest 内容永远逐位稳定——镜像供应链的信任基础。

行为规格（spec 对齐）：
- `PUT /v2/<name>/manifests/<reference>`：reference 为 tag（`[a-zA-Z0-9_][a-zA-Z0-9.-_]{0,127}`）或 `sha256:<hex>`；服务端计算 body 的 sha256 为 digest，`reference` 是 digest 时必须与计算值一致否则 400 `DIGEST_INVALID`；成功 201 + `Location` + `Docker-Content-Digest`；`Content-Type` 必须按 manifest 原始类型透传存储（schema2 / OCI index / OCI manifest，含未来新类型——**透传不白名单**，保证前瞻兼容）。
- `GET /v2/<name>/manifests/<reference>`：按 tag（解析为当前 digest）或 digest；`Accept` 头协商——客户端 Accept 不含存储的 Content-Type 时 404（manifest unknown）；响应头带 `Docker-Content-Digest` 与存储的 `Content-Type`。
- `HEAD` 同 GET 头部，无 body。
- manifest 引用的 blob 存在性校验：PUT 时 config/layer digest 未上传 → 400 `MANIFEST_BLOB_UNKNOWN`（spec `MANIFEST_INVALID` 家族）。
- tag 语义：同 tag 再 PUT = 覆盖指向（M1 覆盖语义的 docker 版）；manifest 被删后 tag 行级联删除。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-9-AC1 | D07 后 `docker manifest push`/PUT manifest → 201；`GET /v2/<name>/manifests/sha256:<d>` 的 body 与 push 原文逐位一致（`cmp`），`Docker-Content-Digest` 头 == digest | P0 |
| FR-9-AC2 | by-tag 取回：`GET .../manifests/v1` 200；覆盖 tag（push 新 manifest 同 tag）后 by-tag 内容为新，by-digest 旧 manifest 仍可取 | P0 |
| FR-9-AC3 | D13b：PUT manifest 时引用不存在的 blob → 400 `MANIFEST_BLOB_UNKNOWN`/`MANIFEST_INVALID` | P0 |
| FR-9-AC4 | digest 不匹配：PUT `manifests/sha256:<错误值>` → 400 `DIGEST_INVALID` | P0 |
| FR-9-AC5 | 多架构（OCI index / manifest list）：`docker buildx` 产出的 index（linux/amd64+arm64）push/pull 正常，index 内子 manifest 均 by-digest 可取（D08b，crane 验证） | P1 |
| FR-9-AC6 | 删除：`DELETE .../manifests/sha256:<d>` → 202（spec）；随后 by-digest 均 404；关联 tag 级联清；被其它 manifest 引用的 layer 不受影响（级联物理回收归 M4，见 §2.2） | P0 |
| FR-9-AC7 | tag 删除：`DELETE .../manifests/<tag>` → **405 `UNSUPPORTED`**（v1.1 定案，docker-registry.md §8 高置信度：官方 spec 禁止 by-tag 删除，Artifactory 的 by-tag 支持属对 spec 偏离、§10 建议栏 3 明示「按官方仅 digest」；tag 的「删除」由覆盖 push 或 by-digest 删除级联实现）；tag 本身不受该请求影响（仍可 GET） | P1 |

### FR-10 catalog 与 tags/list（dev-registry-adapter）

**用户故事**：作为 CI 脚本与平台工程师，我希望能枚举仓库里有哪些镜像名与哪些 tag——镜像的「目录页」是自动化的刚需。

行为规格（spec 对齐）：`GET /v2/_catalog` → 200 `{"repositories":[...]}`（名字典序）；`GET /v2/<name>/tags/list` → 200 `{"name":"<name>","tags":[...]}`（字典序）；两者均支持 `?n=<int>` 分页 + `Link: <url>; rel="next"` 响应头（`last=` 游标，**exclusive**——返回 `last` 之后（不含）的条目，docker-registry.md §6 高置信度与官方语义一致）；`n` 缺省取 **100**（官方默认；Artifactory 偏离为全量，BinFlow 按官方，§6.5 定案）；`n` 非法 → 400 `PAGINATION_NUMBER_INVALID`；**空 tag 集两种场景均返回 200 且 `"tags":null`**（spec 允许 null 或空数组，BinFlow 取 null 对齐 distribution 参考实现；注意 Artifactory 偏离为 404 NO_TAGS_FOUND，不跟进）：① 镜像存在但无 tag；② **曾存在但 manifest 已删光的 name（v1.3/C7/D44-6：name 持续性语义——OCI conformance 期望 200 空表，删光 ≠ name 消失）**；404 `NAME_UNKNOWN` 仅保留给「无任何登记的 name」（从未 push 过）。
**QA 断言注记（v1.1，防脚本误写；v1.3 扩展）**：空集断言必须写 `jq '.tags == null'`（**不是** `.tags == []` 也不是 `.tags | length == 0`——null 上取 length 会报错）；非空断言用 `jq '.tags | type == "array"'`；「删光最后一个 manifest 后 tags/list」断言 = **200 + `.tags == null`**（不是 404）。repo 名（`<name>`）含斜杠（如 `library/nginx`、`acme/team/app`）必须支持——这是 docker 命名空间惯例。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-10-AC1 | D09：push ≥2 个镜像（含嵌套名 `acme/team/app`）后 `_catalog` 200 列出全部名字，jq 断言 | P0 |
| FR-10-AC2 | D09：某镜像多 tag 后 `tags/list` 返回全部 tag 字典序；`?n=1` 返回 1 个 + `Link` 头含 `rel="next"`，跟随 next 可枚举完 | P0 |
| FR-10-AC3 | name 两态（v1.3/C7）：无任何登记的 name → 404 `NAME_UNKNOWN`；**曾存在但 manifest 删光的 name → 200 且 `"tags":null`**（name 持续性，OCI conformance 期望；D09 注记） | P1 |
| FR-10-AC4 | 分页参数非法（`n=abc`）→ 400 | P2 |

### FR-11 token 认证流（docker login）（dev-go-core + dev-registry-adapter）

**用户故事**：作为 docker 客户端用户，我执行 `docker login $REG` 输入用户名口令后，后续 push/pull 自动携带 Bearer token——我不想也无法手工管理每个请求的认证头。

行为规格（对齐 distribution spec token 鉴权 + M1 token 基建；realm 端点形态依 ADR-0010 与 docker-registry.md §5 定案）：
- 未认证请求 `/v2/**` **资源端点** → 401 + `Www-Authenticate: Bearer realm="<BASE>/v2/token",service="binflow",scope="<repository>:<name>:pull"`（negotiate 挑战，docker/podman/skopeo 均按此协商）；响应体为 spec 错误体（`UNAUTHORIZED`）。**scope 推导仅 repository/catalog 资源端点发生**（v1.2/C2 注记）：GET→`pull`、写→`pull,push`、DELETE→`pull,delete`、catalog→`registry:catalog:*`（T-43 全数实测）；**`/v2/` ping 的挑战无 scope 参数**（与 docker-registry.md §5.1 挑战样例同形）。
- **`GET/POST /v2/token`（adapter 自有端点，ADR-0010 定案）**：`GET /v2/token?service=binflow&scope=<scope>` + Basic 凭据（docker 新版本用 GET with Basic，旧客户端可能 POST form——两种都接受）→ 200 `{"token":"<jwt/opaque>","access_token":"<同 token>","expires_in":<秒>,"issued_at":"<RFC3339>"}`；两字段并存（部分客户端只认其一）。参数：`account`（缺省 anonymous）、`client_id`（接受不校验）；**`offline_token=true` → 接受并忽略**（v1.3/C5/D44-2 推翻 v1.2 的 400 拒绝：官方 distribution token spec 明文定义该参数且 server MAY ignore，docker 29.x daemon login 必带——400 直接打断挑战模式 login；Artifactory 实况亦为接受，docker-registry.md §5.2；响应为普通 token，**不返回 `refresh_token`**——M2 无刷新流）。**凭据来源（v1.3/C4/D44-3 补明）**：① Basic 头；② **POST 表单**——无 Basic 头时解析 OAuth 表单（`grant_type=password` 的 `username`/`password`，或 `client_id`/`client_secret` 对），与 Basic 同权（官方 token spec OAuth 流凭据在 form body，buildx/helm 客户端用此形态）；两者皆缺才按匿名（匿名 + `anonymous_access=true` → pull scope 匿名 token；匿名关 → 401）。**错误体（v1.2/C3+D3 裁定）：本端点所有非 2xx 一律 OAuth 形 `{"error":"<code>","error_description":"<msg>"}`**（400 参数错与 401 凭据错同形；docker 客户端不解析 401 body，无兼容性损失；与 M1 管理面 token 端点 E-17 的 OAuth 形同族）。所有 `/v2/**` 响应（含 token 端点）带 `Docker-Distribution-Api-Version: registry/2.0` 头（spec SHOULD，Artifactory 全端点强制——docker-registry.md §0）。
- **双 token 入口并存（设计而非矛盾）**：
  - `POST /binflow/api/security/token` = **管理面入口**（M1 E-17）：**admin-only**（T-15/D3 裁决：非 admin 签发 → 403），OAuth 风格错误体，返回 `token_id` 便于按 id 吊销——面向运维/脚本管理 token 生命周期；
  - `GET/POST /v2/token` = **docker login 语义入口**（本 FR）：**任意有效用户**（含非 admin）均可凭 Basic 换取自身 pull/push scope 的 Bearer token——docker 客户端协商行为所必需（挑战头 realm 指向它，客户端无法被要求先取得 admin 权限）；
  - 两入口签发的 token 落同一 token 表、共用 M1 的吊销端点与验签链（FR-11-AC6 联动）；差异在授权模型（admin 管理全量 vs 用户换自身 scope）与错误码语义（管理面非 admin → 403；`/v2/token` 参数错 → 400 / 凭据错 → 401）——**错误体格式两入口统一为 OAuth 形**（v1.2/C3）。
- scope 授权映射：`pull` → M1 read、`push`/`pull,push` → write；scope 对应仓库/路径无授权 → 该 scope 被剔除或整体 403（spec 宽松，BinFlow 取**返回 token 但 scope 收窄**——与 M1「已认证按 ACL 判定」一致，O4 定界同理）；匿名 + `anonymous_access=true` 时 pull scope 免认证直发。权限判定在**资源端点**执行（token 端点不做 scope 逐项预校验，docker-registry.md §5.2 同构）。
- `/v2/` 探针（`GET /v2/`，v1.3/C6 定案）：**未认证请求一律 401 + Bearer 挑战（无条件，不随 `anonymous_access` 变化）**；带有效凭据（Basic 或 Bearer token）→ 200 `{}`。理由：docker daemon/containers-image 是 ping-缓存型认证器——ping 200 即判「无需认证」并缓存该结论，后续 401 不再发起 token 协商（T-44 D44-1 实证：默认配置下 push 零凭据全断、错口令 login 也 Succeeded）；真实世界（Docker Hub/GHCR/Harbor）匿名可 pull 但 ping 仍挑战，匿名访问经匿名 token 通行（docker-registry.md §0/§5 同构）。**匿名拉取不受损**：挑战 → 客户端无凭据请求 `/v2/token` → `anonymous_access=true` 时照发 pull scope 匿名 token → 匿名 pull 照常（podman 挑战模式 T-44 已实证全通）。FR-11-AC4（错凭据 login 拒绝）随 ping 挑战化在默认配置下恢复有效。
- token 复用 M1 的存储/吊销（token 表、revoke 端点）；`docker logout` 无服务端动作。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-11-AC1 | D05：`docker login $REG -u admin`（交互或 `--password-stdin`）退出码 0，`Login Succeeded` | P0 |
| FR-11-AC2 | D04（v1.3/C6）：`curl -s $REG/v2/` **不带凭据 → 401** + `Www-Authenticate: Bearer realm=".../v2/token"` 头（**无条件**：`anonymous_access` 开/关两种配置均如此）；带 Basic 凭据 → 200 `{}` | P0 |
| FR-11-AC3 | D04b：手工协商：带 Basic 凭据 `GET $BASE/v2/token?service=binflow&scope=...` → 200，`token`/`access_token` 非空且 `expires_in` 为正整数；用该 token `curl -H "Authorization: Bearer <t>" $REG/v2/_catalog` → 200。**非 admin 用户**（如 ci-bot）同样 200 换得 pull scope token（双入口语义，D04c） | P0 |
| FR-11-AC4 | 错误凭据 `docker login` → 非 0 退出码，输出含 `unauthorized`/`401`；服务端 401（不泄露用户是否存在） | P0 |
| FR-11-AC5 | 无 push 权限用户（仅 read）`docker push` → 宕户端报 `denied`/403，服务端日志无 5xx | P1 |
| FR-11-AC6 | 吊销联动：revoke 该 token 后 Bearer 请求 → 401（M1 E-18 行为对 Bearer 入口生效；`/v2/token` 与管理面入口签发的 token 同受管辖） | P1 |
| FR-11-AC7 | offline_token 接受并忽略（v1.3/C5，推翻 v1.2 的 400）：`GET /v2/token?...&offline_token=true`（Basic 凭据）→ **200 正常 token**，响应与不带该参数同形且**无 `refresh_token` 字段**（docker daemon login 必带此参数，400 会打断挑战模式）。**POST 表单凭据**（v1.3/C4）：`POST /v2/token -d 'grant_type=password&username=admin&password=...'`（无 Basic 头）→ 200 **admin 主体 token**（用它 Bearer 写操作成功，非匿名 scope） | P0 |

### FR-12 Helm OCI 承载（dev-registry-adapter）

**用户故事**：作为平台工程师，我把 Helm chart 当普通 OCI artifact 推进仓库（chart tgz 作 layer + helm config 作 manifest），部署侧原样取回——镜像仓库顺带成为 chart 的唯一事实源。

行为规格：不实现任何 Helm 专有端点；依赖 FR-8/FR-9 的「manifest Content-Type 透传 + blob 通用存取」即可承载（OCI artifact 模型）。**artifact 形态（v1.3/C4 定案）**：chart tgz 作 **layer**（`application/vnd.cncf.helm.chart.content.v1.tar+gzip`），helm config 作 **`--config`**（`application/vnd.cncf.helm.config.v1+json`）——config 类型**不可**用作 layer 类型（helm 客户端按 layer 媒体类型找 chart tgz，找不到即报 `manifest does not contain a layer with mediatype …helm.chart.content.v1.tar+gzip`，T-44 实证）。验收以 oras 为主客户端（helm 客户端 OCI 模式作 P2 观察；helm pull 已按正确形态 T-44 补验通过）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-12-AC1 | D15（v1.3/C4 双类型形态）：`oras push $REG/charts/myapp:1.0.0 --config chart.json:application/vnd.cncf.helm.config.v1+json ./myapp-1.0.0.tgz:application/vnd.cncf.helm.chart.content.v1.tar+gzip` 退出码 0（config 与 layer 媒体类型各就其位；helm 客户端可识别该形态） | P0 |
| FR-12-AC2 | D15：`oras pull` 到空目录成功，取回的 tgz 与源 `cmp` 一致；`oras repo tags` 列出 `1.0.0` | P0 |
| FR-12-AC3 | crane 可 inspect 该 artifact 的 manifest（`crane manifest $REG/charts/myapp:1.0.0` 输出合法 JSON） | P1 |
| FR-12-AC4 | （观察项）`helm chart push`（实验性 OCI 客户端）成功则记录通过；失败不阻塞（P2，helm 客户端版本差异大） | P2 |

### FR-13 conformance：五真实客户端（dev-registry-adapter + qa）

**用户故事**：作为评估者，我不看自证测试，只信真实客户端——docker/podman/crane/skopeo/oras 五个客户端开箱可用，才证明兼容性不是纸面的。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-13-AC1 | D16 docker：login → build → push → rmi → pull → run 全链路退出码 0 | P0 |
| FR-13-AC2 | D17 podman：login → push（从 docker daemon 导入或 build）→ pull → run 全链路 0（podman 无 daemon 环境为加分项） | P0 |
| FR-13-AC3 | D18 crane：`crane pull`/`crane digest`/`crane manifest`/`crane copy`（本实例两 repo 间 copy）全 0；`crane copy docker.io/library/alpine:latest $REG/library/alpine:latest`（公网可用时）0 | P0 |
| FR-13-AC4 | D19 skopeo：`skopeo copy docker://$REG/acme/app:v1 docker-archive:...` 与 `--dest-...` 反向 0；`skopeo inspect` 输出合法 JSON | P1 |
| FR-13-AC5 | D15 oras：push/pull/tags 全 0（FR-12 联动） | P0 |
| FR-13-AC6 | conformance 工具本身（distribution 官方 conformance suite，若可离线运行）通过 push/pull/content-management 三组；不可离线运行时以五客户端矩阵覆盖等效（qa 报告注明） | P2 |

### FR-14 部署烟测（release-engineer）

**用户故事**：作为部署者，我希望 M2 交付的镜像与 compose 产物起的就是可用的 docker registry——「用 BinFlow 存放构建 BinFlow 的基础镜像」自举场景必须先跑通。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-14-AC1 | `docker compose up -d`（M1 compose 产物 + M2 镜像 tag）后 D04/D05/D16 三组命令在该实例上全过（非本机 bare 进程） | P0 |
| FR-14-AC2 | Dockerfile（或等价构建产物）存在且 `docker build` 退出码 0；镜像内 `binflow-server --help` 可执行 | P0 |
| FR-14-AC3 | compose 实例 restart 后已 push 镜像仍可 pull（持久化，D21） | P0 |
| FR-14-AC4 | 烟测报告归档 `reports/agents/T-<n>-smoke.md`，含「外部 registry 上传的镜像 + digests 清单」（对外发布镜像仍需用户确认，安全底线） | P1 |

---

## 5. 兼容性矩阵（M2 核心：对齐 distribution spec）

### 5.1 层级定义（沿用 M1 §5.1 四层 + 本节 docker 特化）

> M1 四层定义（兼容 / 兼容子集 / 语义等同但路径不同 / 有意不兼容）继续适用。M2 的特殊性：**对齐基准从「Artifactory 行为」换成「distribution/OCI 官方 spec」**（ADR-0001：有公开规范的以官方为准）。「兼容」判定 = 真实客户端（§5.3 矩阵）不改配置通过。

### 5.2 M2 端点矩阵

错误契约（v1.2/C3 收敛）：`/v2/**` **资源端点**的错误体用 **distribution spec 格式** `{"errors":[{"code":"<CODE>","message":"<msg>","detail":<opt>}]}`（如 `BLOB_UNKNOWN`/`DIGEST_INVALID`/`MANIFEST_UNKNOWN`/`NAME_UNKNOWN`/`UNAUTHORIZED`/`DENIED`/`UNSUPPORTED`）；**例外：`/v2/token` 端点的错误（400 参数错、401 凭据错）一律 OAuth 形 `{"error":"<code>","error_description":"<msg>"}`**——与 M1 管理面 token 端点（`/binflow/api/security/token`，E-17）同族，token 端点族全 OAuth、其余按域分层（延续 M1 §5.1 三分层注，docker 域内再分资源层=spec、token 层=OAuth）。

「置信度」：高 = distribution/OCI spec 明文；中 = 参考实现行为/PRD 暂定，待 `docs/reverse/docker-registry.md` 校准（§6.4）。

| # | 端点（方法 路径） | spec 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| DE-01 | `GET /v2/` | **未认证一律 401 + `Www-Authenticate: Bearer realm=...` 挑战（无条件，不随 anonymous_access 变化）**；已认证 → 200 `{}`（v1.3/C6/D44-1 定案：ping-缓存型客户端 docker/skopeo/podman 依赖 ping 挑战触发 token 协商，ping 200 会导致永不携带凭据；匿名读经匿名 token 通行，见 FR-11） | 兼容（spec + 真实世界行为：Docker Hub/GHCR/Harbor 同构，docker-registry.md §0/§5） | P0 | 高 | D04 |
| DE-02 | `POST /v2/<name>/blobs/uploads/` | initiating：带 `?digest=` 单请求 201；否则 202 + `Location`（**相对路径**，客户端拼 base——T-43 3.1 定案，v1.4/E1）+ `Docker-Upload-UUID` + `Range: 0-0` | 兼容（spec） | P0 | 高 | D06/D07 |
| DE-03 | `POST .../uploads/?mount=<digest>&from=<repo>` | cross-repo mount，201 直接就位 | 兼容（spec） | P0 | 高 | D13 |
| DE-04 | `PATCH <upload-url>` | chunked 追加 202 + `Range: 0-<received>`；Content-Range 不符 416 | 兼容（spec） | P0 | 高 | D10 |
| DE-05 | `PUT <upload-url>?digest=` | 终结 201；digest 不符 400 `DIGEST_INVALID`；session 内 offset 查询 `GET <upload-url>` 204 + Range | 兼容（spec） | P0 | 高 | D10b/D12 |
| DE-06 | `GET /v2/<name>/blobs/<digest>` | 200 流式 + `Docker-Content-Digest`；支持 Range 206/416；404 `BLOB_UNKNOWN` | 兼容（spec） | P0 | 高 | D06 |
| DE-07 | `HEAD /v2/<name>/blobs/<digest>` | 200 + `Content-Length` + `Docker-Content-Digest` 无 body | 兼容（spec） | P0 | 高 | D06 |
| DE-08 | `PUT /v2/<name>/manifests/<ref>` | tag 或 digest；digest 不符 400；blob 缺失 400 `MANIFEST_BLOB_UNKNOWN`（Artifactory 回 MANIFEST_INVALID 文案——docker-registry.md §10 建议按官方，BinFlow 从官方）；**body 上限 4MB**（§6.5 定案，超限 400 `MANIFEST_INVALID`）；201 + `Docker-Content-Digest`；Content-Type 透传 | 兼容（spec） | P0 | 高 | D07/D13b |
| DE-09 | `GET /v2/<name>/manifests/<ref>` | by-tag/by-digest；Accept 协商（存储 OCI 而 Accept 全是 docker 类型 → 404，docker-registry.md §4#3；不做 schema2→1 转换——§4 BinFlow 校准）；`Docker-Content-Digest`；404 `MANIFEST_UNKNOWN` | 兼容（spec） | P0 | 高 | D07/D08 |
| DE-10 | `DELETE /v2/<name>/manifests/<digest>` | **仅 by-digest**：202；级联 tag；404 `MANIFEST_UNKNOWN`；by-tag → **405 `UNSUPPORTED`**（v1.1 定案，官方 spec 禁止 by-tag，docker-registry.md §8/§10-3；FR-9-AC7） | 兼容（spec，by-digest）/ 有意不兼容（by-tag 分支） | P0 | 高 | D13c |
| DE-11 | `GET /v2/_catalog` | 200 `{"repositories":[...]}` 字典序；`?n=`+`Link` 分页（`last` 游标 exclusive；`n` 缺省 100，非法 400 `PAGINATION_NUMBER_INVALID`） | 兼容（spec） | P0 | 高 | D09 |
| DE-12 | `GET /v2/<name>/tags/list` | 200 `{"name","tags"}`；分页同上（last exclusive）；空 tag 集（无 tag 或 **manifest 删光，v1.3/C7：name 持续性语义**）→ 200 `"tags":null`（**非空数组**，QA 断言 `jq '.tags == null'`）；404 `NAME_UNKNOWN` 仅对无登记 name；Artifactory 偏离 404 NO_TAGS_FOUND 不跟进 | 兼容（spec + OCI conformance 期望，T-44 D44-6） | P0 | 高 | D09 |
| DE-13 | token 流：`Www-Authenticate` 挑战 + `GET/POST /v2/token`（adapter 自有端点，ADR-0010） | 401 挑战头（realm=`<BASE>/v2/token`、`service="binflow"`；**v1.2/C2 注记：`/v2/` ping 的挑战无 scope 参数，scope 仅 repository/catalog 资源端点推导**——GET→pull、写→pull,push、DELETE→pull,delete、catalog→`registry:catalog:*`，T-43 全数实测）；token 端点 200 `{"token","access_token","expires_in","issued_at"}`；`offline_token=true` → **接受并忽略**（v1.3/C5，无 refresh_token）；凭据来源 Basic 头或 POST OAuth 表单（v1.3/C4，buildx/helm 形态）；scope 权限判定在资源端点（FR-11 双入口说明） | 兼容（spec）+ **新增自有端点**（docker-registry.md §5.1「realm 指向自身 token 端点」同构；与 M1 管理面 `POST /binflow/api/security/token` 并存，授权模型不同——见 FR-11） | P0 | 高（挑战形态，docker-registry.md §5.1；scope 四形态 T-43 实测）/ 中（scope 收窄策略） | D04b/D04c/D05 |
| DE-14 | `DELETE /v2/<name>/blobs/<digest>` | spec 允许 blob 删除；BinFlow **有意不兼容**：405 `UNSUPPORTED`（blob 物理删除唯一入口是 GC，ADR-0006 安全底线；spec 允许 registry 不支持） | 有意不兼容 | P1 | 高 | D13d |
| DE-15 | `/v2/<name>/referrers/`（OCI referrers API） | M2 不做 → 404 + spec 错误体（E-26 联动）。注记（T-51，架构终审）：manifest PUT 的 `OCI-Subject` 响应头**无条件返回**（subject digest 良构即回），与 OCI spec「repo 开 referrers API 才回」的条件不符——有意为之：oras 等客户端凭该头探测 referrers、得 404 后安全回退 tag 模式，无条件返回不伤推送体验；头信号与端点能力的不一致以本注记显式归档，referrers 端点本体仍 M2 不做 | 有意不兼容（OCI-Subject 头为有意偏差，见注记） | — | — | D24 |
| DE-16 | `/v2/` 下 spec 未定义路径 | 404 + spec 错误体（不返回 E-01，保持域内一致性） | 有意不兼容 | P0 | — | D24 |
| DE-17 | docker 域的 `Link`/`Range`/`Docker-Upload-UUID`/`Docker-Content-Digest`/`Content-Type` 头集 | 头语义与名按 spec 逐字对齐（客户端硬依赖） | 兼容（spec） | P0 | 高 | 全 D 序列 |

> 计数：17 条。兼容（spec）13（v1.1：DE-13 改为 `/v2/token` 自有端点后仍计入 spec 兼容——挑战/响应形态按 spec token 协议）、有意不兼容 3（DE-14/15/16）+ DE-10 的 by-tag 分支（405，FR-9-AC7）。管理端点（docker repo 的建/删/查）零新增——复用 M1 E-04~E-08（FR-7）；**新增唯一产品端点：`/v2/token`**（DE-13）。

### 5.3 五客户端分级矩阵（conformance 判定标准）

「全过」定义：所列操作退出码 0 且服务端日志无 5xx。

| 客户端 | 必测操作 | 分级 |
|---|---|---|
| docker（daemon + CLI） | login/logout、build、push（新+重复 tag）、pull（新 daemon）、run、rmi、manifest inspect（buildx 多架构为 P1 加分） | **P0 必须全过** |
| podman | login、push、pull、run | **P0 必须全过** |
| crane | pull、digest、manifest、ls、copy（实例内两 repo） | **P0 必须全过**（copy 跨公网为 P1 视网络） |
| oras | push（helm config 作 `--config`、chart tgz 作 layer，v1.3/C4 形态）、pull、repo tags | **P0 必须全过** |
| skopeo | copy（双向）、inspect | **P1 必须全过**（ROADMAP「五客户端全过」的 P1 成员；若 skopeo 不可用须在 qa 报告注明环境原因与补救） |
| helm（OCI 实验模式） | chart push/pull | P2 观察（不作门槛） |

### 5.4 M2 核心验收命令（D 序列，QA 直接引用）

> 依赖 §7 Q1 定案的 `REG` 取值。下文按**方案 B（根级例外，`REG=localhost:8080`）**书写；若定案为方案 A（`/binflow/v2` + 反代 rewrite），仅 `REG` 的对外形态变化（经反代域名访问），命令本体与期望输出不变。

```bash
# 环境准备
export BASE=http://localhost:8080        # 管理面（M1 口径不变）
export REG=localhost:8080                # docker/oras 客户端目标（host[:port]，无 scheme）
export ADMIN_PW=password
docker pull alpine:3.20                   # 准备一个小基础镜像（公网；离线环境用本地已有镜像替代）

# D01 建 docker 仓库（FR-7-AC1）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-local \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"docker"}' \
  -o /dev/null -w '%{http_code}\n'                   # 200
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/charts \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"docker"}' \
  -o /dev/null -w '%{http_code}\n'                   # 200（helm OCI 用同型仓库）
# name 模型注记（v1.2/C1）：/v2/<name> 中 name = <repoKey>/<image>——首段是仓库 key（如 docker-local），
# 其余段是镜像名（如 acme/app）；仓库名与镜像名是两个概念，URL 里拼成全名 docker-local/acme/app。
# 单段 name（只有 repoKey、无镜像段，如 /v2/docker-local/blobs/...）→ 404（T-33 评审 AC，实测确认）。

# D04 /v2/ 探针（FR-11-AC2；v1.3/C6：未认证一律挑战，不随 anonymous_access 变化）
curl -s -o /dev/null -w '%{http_code}\n' $REG/v2/    # 401（匿名开=默认配置，亦是 401）
curl -sI $REG/v2/ | grep -i www-authenticate         # Bearer realm=".../v2/token",service="binflow"...
curl -sI $REG/v2/ | grep -i docker-distribution-api-version   # registry/2.0
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $REG/v2/   # 200（带凭据）
curl -su admin:$ADMIN_PW $REG/v2/                    # 输出 {}
# 匿名拉取路径不受损（挑战→匿名 token→pull）：anonymous_access=true（默认）时，
#   curl -s "$BASE/v2/token?service=binflow&scope=repository:docker-local/acme/app:pull"（无凭据）
#   → 200 匿名 pull token；用它 Bearer GET blob/manifest → 200

# D04b 手工 token 协商（FR-11-AC3，v1.1：走 /v2/token；v1.2/C1：scope 的 repository 名同用全名 <repoKey>/<image>）
TOKEN=$(curl -su admin:$ADMIN_PW "$BASE/v2/token?service=binflow&scope=repository:docker-local/acme/app:pull,push" | jq -r .token)
echo -n "$TOKEN" | wc -c                             # > 0
curl -s -H "Authorization: Bearer $TOKEN" -o /dev/null -w '%{http_code}\n' $REG/v2/_catalog   # 200

# D04c 双入口语义（v1.1 新增）：非 admin 用户走 /v2/token（FR-11-AC3；v1.2/C1：scope 全名）
TOKEN2=$(curl -su ci-bot:ci-pw "$BASE/v2/token?service=binflow&scope=repository:docker-local/acme/app:pull" | jq -r .token)
echo -n "$TOKEN2" | wc -c                            # > 0（任意有效用户可换 token；token 端点不预校验 scope，权限在资源端点判）
# 对照：同一非 admin 用户走管理面入口 → 403（T-15/D3 裁决）
curl -su ci-bot:ci-pw -X POST $BASE/binflow/api/security/token -d 'grant_type=client_credentials' \
  -o /dev/null -w '%{http_code}\n'                   # 403
# offline_token 接受并忽略（FR-11-AC7，v1.3/C5：docker daemon login 必带此参数）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' "$BASE/v2/token?service=binflow&offline_token=true"  # 200（正常 token，无 refresh_token）
# POST 表单凭据（FR-11-AC7/C4，v1.3：buildx/helm 形态）
FORMTOK=$(curl -s -X POST $BASE/v2/token -d 'grant_type=password&username=admin&password='$ADMIN_PW'&service=binflow&scope=repository:docker-local/acme/app:pull,push' | jq -r .token)
curl -s -H "Authorization: Bearer $FORMTOK" -o /dev/null -w '%{http_code}\n' $REG/v2/_catalog   # 200（admin 主体，非匿名 scope）

# D05 docker login（FR-11-AC1）
echo "$ADMIN_PW" | docker login $REG -u admin --password-stdin   # Login Succeeded，退出码 0

# D06 monolithic blob 上传 + 回取（FR-8-AC1，纯 curl 走 spec）
dd if=/dev/urandom of=blob.bin bs=1m count=10
DGST="sha256:$(sha256sum blob.bin | cut -d' ' -f1)"
# v1.2/C1：name 用全名 docker-local/acme/app（repoKey=docker-local + image=acme/app，与 D08 一致）
# v1.4/E1（T-105）：Location 为相对路径（/v2/...，T-43 3.1 定案）——客户端需拼 scheme://host，此处以 $REG 前缀物化（D10 同）
LOC=$REG$(curl -su admin:$ADMIN_PW -si -X POST $REG/v2/docker-local/acme/app/blobs/uploads/ \
  | awk -F': ' 'tolower($1)=="location"{gsub("\r","");print $2}')
curl -su admin:$ADMIN_PW -X PUT --data-binary @blob.bin -H 'Content-Type: application/octet-stream' \
  "$LOC?digest=$DGST" -o /dev/null -w '%{http_code}\n'           # 201
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' -X POST $REG/v2/docker-local/blobs/uploads/   # 404（单段 name，C1 断言）
curl -su admin:$ADMIN_PW -o dl.bin $REG/v2/docker-local/acme/app/blobs/$DGST
sha256sum blob.bin dl.bin                                          # 两行相同
curl -su admin:$ADMIN_PW -sI $REG/v2/docker-local/acme/app/blobs/$DGST | \
  awk -F': ' 'tolower($1)=="docker-content-digest"{gsub("\r","");print $2}'   # == $DGST

# D07 单请求上传 + manifest（FR-8-AC2 / FR-9-AC1，docker CLI 全流程隐式覆盖，此处 curl 抽查）
curl -su admin:$ADMIN_PW -X POST --data-binary @blob.bin \
  "$REG/v2/docker-local/acme/app/blobs/uploads/?digest=$DGST" -o /dev/null -w '%{http_code}\n'  # 201

# D08 docker 全链路（FR-13-AC1 / 1.2 目标）
docker tag alpine:3.20 $REG/docker-local/acme/app:v1
docker push $REG/docker-local/acme/app:v1                          # 退出码 0，digest 输出
docker rmi $REG/docker-local/acme/app:v1
docker pull $REG/docker-local/acme/app:v1                          # 退出码 0
docker run --rm $REG/docker-local/acme/app:v1 echo ok              # 输出 ok，退出码 0

# D08b 多架构（FR-9-AC5，P1）
docker buildx build --platform linux/amd64,linux/arm64 -t $REG/docker-local/acme/multi:1 --push .  # 有 Dockerfile 时
crane manifest $REG/docker-local/acme/multi:1 | jq -r .mediaType   # application/vnd.oci.image.index.v1+json 或 schema2 list

# D09 catalog / tags（FR-10）
curl -su admin:$ADMIN_PW $REG/v2/_catalog | jq -r '.repositories[]'   # 列出 docker-local 下全部名字
curl -su admin:$ADMIN_PW $REG/v2/docker-local/acme/app/tags/list | jq -r '.tags[]'  # 含 v1
curl -su admin:$ADMIN_PW "$REG/v2/docker-local/acme/app/tags/list?n=1" -i | grep -i '^link:'   # rel="next" 存在
# 空仓断言注记（v1.1，防 T-43 脚本误写；v1.3/C7 扩展）：对「镜像存在但无 tag」或「manifest 已删光」的 name：
#   curl -s .../tags/list | jq '.tags == null'   → true（是 null，不是 [] ；length==0 会因 null 报错）
#   curl -s -o /dev/null -w '%{http_code}\n' .../tags/list   → 200（删光 manifest 不是 404）
# 从未登记的 name（如 /v2/docker-local/no-such/img/tags/list）→ 404 NAME_UNKNOWN
# last 游标 exclusive：?n=1 两次（第二次带 last=<第一次返回的 tag>）不重复、可枚举完
# n 非法：?n=abc → 400 PAGINATION_NUMBER_INVALID

# D10 chunked 上传（FR-8-AC3；v1.2/C1：全名；Location 相对路径 → 捕获时拼 $REG，v1.4/E1）
split -b 1m blob.bin part_                                            # 10 段
LOC=$REG$(curl -su admin:$ADMIN_PW -si -X POST $REG/v2/docker-local/acme/app/blobs/uploads/ \
  | awk -F': ' 'tolower($1)=="location"{gsub("\r","");print $2}')
for f in part_*; do
  LOC=$REG$(curl -su admin:$ADMIN_PW -si -X PATCH --data-binary @$f "$LOC" \
    | awk -F': ' 'tolower($1)=="location"{gsub("\r","");print $2}')
done
curl -su admin:$ADMIN_PW -X PUT "$LOC?digest=$DGST" -o /dev/null -w '%{http_code}\n'   # 201
curl -su admin:$ADMIN_PW -o dl2.bin $REG/v2/docker-local/acme/app/blobs/$DGST && cmp blob.bin dl2.bin && echo IDENTICAL

# D10b 中断恢复（FR-8-AC4，P1）：PATCH 2 段后查询 offset 再续传
curl -su admin:$ADMIN_PW -si $LOC | awk -F': ' 'tolower($1)=="range"{gsub("\r","");print $2}'  # 0-<received>

# D11 去重继承（FR-8-AC7 / 1.2 目标）
B1=$(curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/stats | jq .blobs)
docker tag alpine:3.20 $REG/docker-local/acme/app:v1-copy && docker push $REG/docker-local/acme/app:v1-copy
B2=$(curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/stats | jq .blobs)
echo "$B1 -> $B2"    # 同内容再 push：blob 计数不变（仅 node/tag 变化）

# D12 digest 校验失败（FR-8-AC5；v1.2/C1：全名）
curl -su admin:$ADMIN_PW -X POST --data-binary @blob.bin \
  "$REG/v2/docker-local/acme/app/blobs/uploads/?digest=sha256:$(printf '0%.0s' {1..64})" \
  | jq -r '.errors[0].code'                       # DIGEST_INVALID（HTTP 400）

# D13 cross-repo mount（FR-8-AC6；v1.2/C1：目标与 from 均全名——from=<repoKey>/<image>，与 D15 的 charts/myapp 一致）
curl -su admin:$ADMIN_PW -X POST "$REG/v2/charts/myapp/blobs/uploads/?mount=$DGST&from=docker-local/acme/app" \
  -o /dev/null -w '%{http_code}\n'                # 201
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $REG/v2/charts/myapp/blobs/$DGST   # 200

# D13b manifest 引用缺失 blob（FR-9-AC3）
curl -su admin:$ADMIN_PW -X PUT -H 'Content-Type: application/vnd.oci.image.manifest.v1+json' \
  --data-binary '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"sha256:$(printf '1%.0s' {1..64})","size":2},"layers":[]}' \
  "$REG/v2/docker-local/acme/broken:m1" | jq -r '.errors[0].code'   # MANIFEST_BLOB_UNKNOWN / MANIFEST_INVALID（400）

# D13c 删除 manifest（FR-9-AC6；v1.1：by-tag 删除已改 405，断言只保留 by-digest）
DIG=$(crane digest $REG/docker-local/acme/app:v1)
curl -su admin:$ADMIN_PW -X DELETE "$REG/v2/docker-local/acme/app/manifests/$DIG" -o /dev/null -w '%{http_code}\n'  # 202
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' "$REG/v2/docker-local/acme/app/manifests/$DIG"            # 404
# by-tag 删除拒绝（FR-9-AC7，v1.1 定案）：
curl -su admin:$ADMIN_PW -X DELETE "$REG/v2/docker-local/acme/app/manifests/some-tag" | jq -r '.errors[0].code'    # UNSUPPORTED（HTTP 405）

# D13d blob DELETE 拒绝（DE-14；v1.2/C1：全名）
curl -su admin:$ADMIN_PW -X DELETE "$REG/v2/docker-local/acme/app/blobs/$DGST" | jq -r '.errors[0].code'  # UNSUPPORTED（405）

# D14 kill -9 会话一致性（FR-8-AC9）：chunked 进行中 kill 服务 → 重启 → blob 404 / 历史 200 / health 200（同 M1 C30 手法）

# D15 Helm OCI（FR-12；v1.3/C4：config 用 --config、chart tgz 作 layer——config 类型不可作 layer，helm 客户端不认）
printf '{"name":"myapp","version":"1.0.0","description":"m2 qa"}' > chart.json
printf 'name: myapp\nversion: 1.0.0\n' > Chart.yaml && helm package . 2>/dev/null || cp myapp-1.0.0.tgz .  # 取得 tgz
oras push $REG/charts/myapp:1.0.0 \
  --config chart.json:application/vnd.cncf.helm.config.v1+json \
  ./myapp-1.0.0.tgz:application/vnd.cncf.helm.chart.content.v1.tar+gzip   # 退出码 0
mkdir pull-out && cd pull-out && oras pull $REG/charts/myapp:1.0.0 && cmp ../myapp-1.0.0.tgz myapp-1.0.0.tgz && echo IDENTICAL
oras repo tags $REG/charts/myapp                       # 含 1.0.0

# D16~D19 五客户端矩阵（FR-13，§5.3 为准）
# D16 docker：D05/D08 全链路即 D16
# D17 podman：
podman login $REG -u admin --password-stdin <<< "$ADMIN_PW"
podman pull $REG/docker-local/acme/app:v1 && podman run --rm $REG/docker-local/acme/app:v1 echo ok
# D18 crane：
crane digest $REG/docker-local/acme/app:v1             # sha256:...
crane manifest $REG/docker-local/acme/app:v1 | jq .    # 合法 JSON
crane copy $REG/docker-local/acme/app:v1 $REG/docker-local/copy/app:v1   # 退出码 0
# D19 skopeo（P1）：
skopeo inspect docker://$REG/docker-local/acme/app:v1 | jq .Digest
skopeo copy docker://$REG/docker-local/acme/app:v1 docker-archive:app.tar # 退出码 0

# D20 冷启动不回退（1.2 目标）：空库裸二进制启动 → ping OK < 2s（M1 C 序列同法计时）

# D21 持久化（FR-14-AC3）：compose 实例 push 后 restart → pull 仍 0

# D22 无 push 权限用户（FR-11-AC5）：ci-bot 仅 read 授权后 docker push → denied/403
# D23 token 吊销联动（FR-11-AC6）：revoke 后 Bearer 请求 401
# D24 未定义端点（DE-15/16）：/v2/<name>/referrers/ 与 /v2/foo/bar/baz/qux → 404 + errors[] spec 体
```

---

## 6. 非功能需求与 M1 遗留处置

### 6.1 性能（M2 增量）

| NFR | 指标 | 优先级 |
|---|---|---|
| NFR-P5 | 冷启动 < 2s 不回退（含 docker 路由注册，D20） | P0 |
| NFR-P6 | 单镜像 push/pull 吞吐不劣于 generic 路径 ±20%（10MB~100MB 样本，qa 记录，不设硬门；GA 基准归 M5） | P1 |
| NFR-P7 | 100 并发 pull 同一 blob：`seq 100 \| xargs -P100 crane pull`（或 curl blob GET）退出码全 0、零 5xx | P1 |

### 6.2 安全底线（M2 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S9 | token 挑战头不泄露内部路径细节：`realm` 指向 `/v2/token`（公开端点，ADR-0010），`service` 为固定值 `"binflow"`；401 挑战不带堆栈/内部 ID | D04 抓头 |
| NFR-S10 | docker 域错误体不混用格式（v1.2/C3 收敛口径）：`/v2/**` 资源端点全部 spec 错误体、`/v2/token` 端点全部 OAuth 形（400/401 同形）——同一端点内不出现双格式，DE-16 边界内不出现 E-01 | D12/D24/D04c |
| NFR-S11 | `<name>` 路径安全：`/v2/../../etc/passwd/blobs/...` 类穿越 → 400/404，数据目录外无文件（M1 NFR-S4 的 docker 版） | qa 变体用例 |
| NFR-S12 | blob/manifest 写操作一律需认证（匿名读开也不豁免 push）；`mount` 参数跨 repo 需对源 repo 有 read | 代码评审 + D22 |

### 6.3 可观测性（M2 增量）

- 结构化日志沿用 M1 字段集；docker 域请求的 `path` 记 `/v2/...` 原始路径；`user` 字段对 Bearer token 请求记 token 主体名。
- `/binflow/api/v1/health` 增加 `registry` 子系统状态（路由注册 + blob 目录可写）——形态变化须与 M1 响应向后兼容（只增字段）。

### 6.4 M1 遗留观察项处置（T-18/T-19 报告，逐条定界）

| 观察项 | 内容 | M2 处置 |
|---|---|---|
| O1 | 慢上传被客户端中断时服务端日志以 500 收卷（无可见脏数据，仅日志语义） | **纳入 M2**（NFR-OBS-1，P1）：docker 域新增「客户端断开」日志定界——`client disconnect` 标注或 499 风格状态（实现取一，architect 定），区分真 5xx 故障；generic 域一并受益（同一上传 handler 家族）。qa 验收：构造中断后服务日志中该请求**不出现 5xx 级 ERROR 计数**（以 disconnect 标注收卷） |
| O2 | README 默认端口 8080 在宿主被占时的复跑留档 | **纳入 M2 部署烟测**（FR-14 附带）：release-engineer 烟测在干净环境（CI runner 或干净 VM）按默认端口完整复跑一次并留档；README 既有 env 覆写说明不变 |
| O3 | `gc` 子命令无 `-c` 旗标；`--grace-days` 无法表达 <24h | **纳入 M2**（运维便利，P2）：`gc` 补 `-c` 旗标与 serve 一致；`--grace-hours` 增补。AC：`binflow-server gc -c other.yaml --help` 正常、`gc --grace-hours 1` 被接受 |
| O4 | 匿名读开时，已认证但无 read 权限用户对内容 GET/item info 得 403（匿名反而 200） | **M2 定界后维持**（产品语义定案）：保守正确方向（已认证走自身 ACL），不改。写入 §4 FR-11 的 scope 映射语境：docker 域同理——匿名 pull 放行、已认证零权限用户 pull 拒绝。**不改代码**，PRD 记录为既定语义，M4 权限完整版再统一评审「是否回落匿名通道」 |
| Content-Type 扩展名映射（T-19 DoD P2 清点遗留） | generic 下载默认 `application/octet-stream` | **纳入 M2**（P2）：按扩展名的 mime 映射（json/tgz/zip/txt 等高频集），docker 域 Content-Type 由 spec 语义管理不受影响。AC：`curl -TI .../x.json` 的 Content-Type 为 `application/json` |
| T-18 O2（用户 PUT 重复 409 vs Artifactory replace 201） | 自有语义差异 | **显式归档**：维持 BinFlow 自有语义（409 拒绝重复建），E-19 已属「语义等同但路径不同」层，不改；写入 docs/user 的 API 差异说明（tech-writer M2 文档票附带） |

### 6.5 校准记录（v1.1：`docker-registry.md` 已落地，四项全部定案）

| # | 项 | v1.0 暂定值 | **v1.1 定案** | 依据 |
|---|---|---|---|---|
| ① | `_catalog`/tags 分页 `last` 游标语义 | 按 spec（`n`+`Link`） | **exclusive**（`last` 之后不含本身，docker-registry.md §6 与官方一致）；`n` 缺省 **100**（官方默认；Artifactory 偏离为全量——BinFlow 按官方，不跟进偏离） | docker-registry.md §6（高） |
| ② | `offline_token=true`（v1.1 误判为 Artifactory 私有扩展） | 不接受（400 invalid_request） | **v1.3 推翻（C5/D44-2）：接受并忽略**——官方 distribution token spec 明文定义该参数且 server MAY ignore；docker 29.x daemon login 的 token GET 必带（400 实证打断挑战模式 login，T-44 D44-2）；Artifactory 实况亦为「接受但走同一 provider」（docker-registry.md §5.2）。响应为普通 token，不返回 `refresh_token`（M2 无刷新流，v1.0 Non-goal 维持） | 官方 token spec（MAY ignore）+ docker-registry.md §5.2 + T-44 实证 |
| ③ | manifest 大小上限 | 暂定 4MB（待证） | **4MB 定案**：PUT manifest body > 4MB → 400 `MANIFEST_INVALID`（detail 说明超限）；DE-08 已落地。规格未见 Artifactory 显式上限值，4MB 为业界通行量级（4MB 足够容纳超大 index），从严防滥用 | PRD 定案（规格无值） |
| ④ | `/v2/` 挑战头 `service` 值 | BinFlow service-id（首启生成） | **固定 `"binflow"`**（可配置覆盖 `registry.service_id`）；不采用 Artifactory 的「service=host」推导（docker-registry.md §5.1——其值绑定部署形态，反代后不稳；固定值对客户端无语义约束，spec 仅要求挑战与签发两侧一致） | docker-registry.md §5.1（高）+ PRD 决策 |

---

## 7. 决策与开放问题

### Q1（M2 关键决策，**已定案**）：`/v2` 硬编码前缀与 `/binflow` 统一前缀的冲突解法

**定案（v1.1，用户确认 → ADR-0010）：方案 B，根级例外 `/v2` 直挂。** ADR-0008 预告的冲突按其预留的「根级例外」分支落地；本 PRD v1.0 全文即按方案 B 书写，D 序列与 DE 矩阵无需改写。两案对比表保留为决策记录（当初的决策依据材料）：

| 维度 | 方案 A：仅 `/binflow/v2` + 反代 rewrite | **方案 B：根级例外 `/v2` 直挂（已定案）** |
|---|---|---|
| 客户端指向 | `REG=<反代域名>`（反代把 `/v2/*` rewrite 到 `/binflow/v2/*`） | **`REG=localhost:8080` 直连可用** |
| 裸二进制/无反代场景 | **docker/podman/skopeo 无法直连**（客户端不允许子路径前缀）——必须先架反代，README 快速开始复杂化 | 开箱即用 |
| token realm 指向 | realm 需经反代映射（rewrite 规则覆盖两个域） | **realm = `<BASE>/v2/token`，零映射**（v1.1 随 ADR-0010 定案为 adapter 自有端点） |
| ADR-0008 纯度 | 完全统一（无根级例外） | 打破「所有产品端点统一 `/binflow`」的例外一处（探针端点 `/healthz` 等已是先例） |
| 与 M1 E-26 的交互 | 根路径 `/v2/` 仍 404（现状不变） | M1 的「`/v2/**` → 404」断言在 M2 构建上反转为 200（**qa 回归基线须同步更新该条**，见 §8 第 1 步） |
| 同域共存（其它服务共享域名） | 无冲突（前缀隔离） | `/v2` 被占用时冲突（需换端口或反代）——docker 客户端本就推荐独立 host:port |
| 行业先例 | — | distribution/Harbor/Artifactory 均根级 `/v2`（spec 生态事实标准） |

**落地影响（随定案生效）**：① M1 E-26 对 `/v2/` 的 404 断言在 M2 起失效，回归基线以 DE-01（200 `{}`）为准；② `/v2/**` 全域是 docker adapter 的路由领地，资源端点错误用 spec 格式、`/v2/token` 用 OAuth 形（NFR-S10，v1.2 收敛）；③ `Docker-Distribution-Api-Version: registry/2.0` 头全端点带上（docker-registry.md §0/§10-7）。

### 其余开放问题

| # | 问题 | 影响面 | 暂行假设 |
|---|---|---|---|
| Q2 | docker 域匿名 pull 是否受 `security.anonymous_access` 全局开关直接管辖（M1 键复用），还是 docker 仓库单列开关（Artifactory 有 per-repo 匿名下载开关） | FR-11、部署文档 | 复用全局键（v1.3/C6 后语义不变：开关管的是**匿名 token 是否发 pull scope**，ping 恒挑战）；per-repo 开关 M4 权限完整版一并做 |
| Q3 | `docker logout`/token 生命周期：docker 客户端不刷新 token，过期后行为 | FR-11 | expires_in 默认 30 天（M1 现值），过期重新 login 即可，不做 refresh |
| Q4 | manifest 覆盖 tag 时是否限制「不可变 tag」（production 硬需求，Artifactory 有 tag retention/不可变设置） | FR-9 | M2 允许覆盖（spec 默认）；不可变 tag 归 M4 治理 |
| Q5 | `_catalog` 是否需要 repo 级权限过滤（非 admin 只见有 read 的 repo） | FR-10 | 按 M1「已认证按 ACL」原则过滤、匿名全见（匿名读开时）——与 O4 定界一致 |

---

## 8. M2 验收剧本（QA 总纲）

1. **回归基线**：FR-7-AC2（M1 C 序列 P0 项复跑全绿；E-26 的 `/v2/` 断言按 Q1 定案更新口径）。
2. **仓库与探针**：D01 → D04/D04b/D04c（含双入口与 offline_token）→ D05。
3. **blob 协议**：D06 → D07 → D10 → D10b → D12 → D13 → D14。
4. **manifest**：D08 → D08b → D09 → D13b → D13c → D13d。
5. **去重与 stats**：D11（+ generic↔docker 跨协议变体）。
6. **五客户端矩阵**：D16 → D17 → D18 → D19 → D15（oras），按 §5.3 分级判定。
7. **权限面**：D22 → D23 → NFR-S11/S12 变体。
8. **性能**：D20（P0）→ NFR-P7 → NFR-P6 记录。
9. **部署烟测**：FR-14-AC1~AC4（release-engineer，含 O2 默认端口干净复跑）。
10. **M1 遗留收口**：O1 日志定界用例、O3 gc 旗标、Content-Type 映射抽查。

## 9. M2 DoD

1. §4 全部 P0/P1 AC 经 qa 验证全绿（P2 延后在 BOARD 记录）；
2. §8 剧本全绿，五客户端分级矩阵（§5.3）P0 成员全过、skopeo 过或注明环境补救；
3. Q1 已定案（ADR-0010）且 PRD 已回写（v1.1 完成）；`docker-registry.md` 规格落地且 §6.5 四项校准已定案回写（v1.1 完成，无遗留）；
4. release-engineer 烟测报告（Docker 镜像 + compose）归档；
5. tech-writer 产出 docker 接入文档（login/push/pull、oras/Helm、token 说明、E-19 差异说明）；
6. 主会话完成 `m2-done` tag（对外发布镜像先经用户确认）。
