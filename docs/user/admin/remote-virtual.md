---
title: remote / virtual 仓库管理
sidebar_position: 40
---

# remote / virtual 仓库管理

> 适用版本：M3（pull-through 代理缓存 + 聚合解析；PRD milestone-3 v1.2、ADR-0012/0013）+ **M10 增补**（smart remote 生效字段子集：`socketTimeoutMs`〔含 xsd 别名〕/`metadataRetrievalTimeoutSecs`）+ **M11 增补**（`enableTokenAuthentication`/`contentSynchronisation` 接受且生效〔L25 反转，T-317〕；`unusedArtifactsCleanupPeriodHours` 清理引擎生效〔T-324〕；conan/helm/rpm/debian 三类仓型——T-312/313/314/315；**cargo remote/virtual 仓型**——T-316/T-318，见 [Cargo 接入](../integrations/cargo.md)）+ **M14 增补**（**docker remote 仓型**——FR-129/T-392，community 档自动受缝）+ **近期增补**（**远端浏览可选档 `listRemoteFolderItems`**——helm/debian/rpm 三型，见[下文专节](#远端浏览可选档listremotefolderitems)）。
> 本文命令在 M3 QA 基线（commit `0f86229`，T-75/T-76 验收产物）上复验：建仓字段回显、缓存 MISS→HIT 冻结、DELETE 强刷、凭据加密落盘、无钥 fail-fast、virtual 收口与写路由均按预期（复跑记录见 `reports/agents/T-77.md`）。远端浏览可选档的 wire 事实（PUT/GET 回显、批 1 型门与类型门 400、`remoteDegraded` 注记）在 HEAD 构建的本地 scratch 实例（2026-09-06）curl 实测。

三种仓型各司其职，概念与 Artifactory 一一对应（术语不变）：

| 仓型 | rclass | 职责 | 可写 |
|---|---|---|---|
| 本地仓 | `local` | 团队自有制品的落盘地 | 是 |
| **remote 仓** | `remote` | 上游代理缓存（pull-through）：首次回源、此后命中本地缓存，上游故障不传染 | 否（405，只读代理） |
| **virtual 仓** | `virtual` | 把 local 与 remote 成员缝成单一访问入口 | 默认否（405）；显式配置写路由后可写【暂行口径，见下】 |

## 建仓

统一走管理面 API：`PUT /binflow/api/repositories/{key}`（admin 凭据，重复 PUT 为更新、成员变更即时生效）。

### remote 仓字段表

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-remote-central \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"maven","url":"https://repo.maven.apache.org/maven2"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

| 字段 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `url` | 是 | — | 上游 base URL，仅 `http`/`https`（`file://`/`ftp://` 建仓即 400）；请求路径直接拼接 |
| `username` / `password` | 否 | 空 | 上游 Basic 认证；password 静态加密落库（见[凭据小节](#上游凭据与-binflow_remote_credentials_key)），GET 永不回显 |
| `retrievalCachePeriodSecs` | 否 | **7200** | 缓存命中期：期内 GET 不回源；过期后下次请求触发回源 |
| `missedRetrievalCachePeriodSecs` | 否 | **1800** | 404 负缓存期：期内同路径 404 零上游流量（防穿透）。**显式 `0` = 回落默认 1800 而非禁用**（与 Artifactory 语义一致——验证「负缓存已关」时勿用 0，直接观察 `X-Binflow-Cache`/回源流量）。**M10 起**接受 `missRetrievalCachePeriodSecs`（无 ed）为输入别名——回显恒用 canonical 拼写（与 Artifactory 一致），两拼写同时给非零且不相等 → 400 |
| `socketTimeoutMillis` | 否 | **15000** | 上游连接/读/响应头超时（毫秒粒度，可表达亚秒超时）——**canonical 拼写**（artifactory.xsd，**M12 起回显统一为本拼写**，FR-113.1/T-290-2 兑现）。M10 期 PRD 拼写 `socketTimeoutMs` 仍接受为**输入别名**（只进不出，回显恒为新拼写；两拼写非零分歧 400）；显式 `0` = 缺席（回落 `socketTimeoutSecs`/默认） |
| `socketTimeoutSecs` | 否 | **15** | 上游连接/读超时（秒）——**legacy 字段**（M3）：`socketTimeoutMillis` 非零时以 ms 为准；回显时恒附派生 `socketTimeoutSecs`（= ceil(ms/1000)，永不虚报更长超时） |
| `metadataRetrievalTimeoutSecs` | 否 | **60** | **M10**：并发拉取同一 metadata 路径（如 `maven-metadata.xml`）时等待者的等锁上限，超时回发旧缓存副本（零回源）——per-repo 化（原为引擎级常量 60s） |
| `unusedArtifactsCleanupPeriodHours` | 否 | **0**（关） | 未使用缓存制品的清理周期（小时）。**M11 起生效**——cleanup 引擎按窗口删除「窗口内无下载事件且未再落地」的缓存 node（在用判定含 virtual 仓聚合下载；`GET /api/v1/system/cleanup` 查状态，见 [API 参考](../api-reference.md#m11-增补速览t-328)） |
| `enableTokenAuthentication` | 否 | **false** | **M11（L25 反转）**：`true` 时拉取侧对上游发 `Authorization: Bearer <password>`（无密码 = 匿名维持）；Basic 形态的既有仓零变化 |
| `contentSynchronisation` | 否 | `{"enabled":false,…}` | **M11（L25 反转）**：拉取侧内容同步策略对象，四子字段 `enabled` / `propertiesEnabled`（内容类节点落地后从上游属性面附着属性，best-effort）/ `statisticsEnabled` / `sourceOrigin`（后两子字段接受 + 回显，暂无行为）。canonical 回显恒带四子字段 |
| `assumedOfflinePeriodSecs` | 否 | **300** | 上游故障静默期：故障标记后期内零上游流量，期后自动恢复探测 |
| `hardFail` | 否 | **false** | `true` 时上游错误向上抛 **502**（默认 404 + 有缓存服务缓存） |
| `allowPrivateUpstream` | 否 | **false** | SSRF 私网放行开关，仅 admin 可设、写审计日志（见[SSRF 小节](#ssrf-防护与-allowprivateupstream-放行指引)） |
| `priorityResolution` | 否 | `false` | 作为 virtual 成员时的优先解析标记（见下文） |
| `chartsBaseUrl` | 否 | 空（回退仓 URL） | **M13，仅 `packageType=helm` 的 remote**：content 类回源（tgz/`.prov`/`_external` 折叠路径）的分体基址——metadata（index.yaml）恒走仓 URL；绝对 http(s) URL、`""` = 清除；**其它包型携带 → 400 点名字段**；异 host 时无凭据出站。用法见 [Helm Chart 仓库接入](../integrations/helm-charts.md#chartsbaseurl分体回源基址m13) |
| `listRemoteFolderItems` | 否 | **false** | **远端浏览可选档**，仅 `helm` / `debian` / `rpm` 三型 remote（详见[下文专节](#远端浏览可选档listremotefolderitems)）：`true` 时浏览面列出未缓存的远端目录条目；其它包型携带 `true` → 400 点名批 1 集。**指针语义**：显式 `false` 与缺省可区分——flip-off 更新存活（实测 round trip）；回显恒在场（布尔，无 omitempty） |

> **smart remote 字段注意（T-290/T-317/T-346）**：① `enableTokenAuthentication` / `contentSynchronisation`
> 自 **M11 起接受且生效**（上表；M10 期的按名 400 已退役）；**其余**未知字段维持
> M3 的容忍丢弃语义（迁移脚本兼容）。② 别名拼写（`socketTimeoutMs` / `missRetrievalCachePeriodSecs`）
> 只进不出，回显恒为 canonical（**M12 起为 `socketTimeoutMillis`**（FR-113.1 翻转）/ `missedRetrievalCachePeriodSecs`）——
> 既有存量仓回显沿用其落库形态，下次 PUT 重写后即翻为新拼写。
> ③ 消费优先级：新列值 > canonical JSON > legacy `socketTimeoutSecs` > 产品默认——升级既有仓
> 零回填零行为变化。

`packageType` 合法值：五核心 `generic` / `docker` / `maven` / `npm` / `pypi`（community 地板，**三仓型全组合可建**——`rclass × packageType` 组合门已全量退役，docker 的 virtual 聚合同样开闸，实测建仓 200）；进阶八型 `go` / `nuget` / `cargo` / `conan` / `helm` / `helmoci` / `rpm` / `debian`（**license ≥ pro**——低档位建仓 400 `package type not available on this instance: ...`，实测文案）：`conan` / `helm` / `rpm` / `debian` 三类仓型齐备（见各接入指南）、`cargo` 与 `helmoci` 含 remote/virtual 仓型（语义见 [Cargo 接入](../integrations/cargo.md) 与 [Helm Chart 仓库接入](../integrations/helm-charts.md)）、`go` 见 [Go Modules 接入](../integrations/golang.md)、`nuget` 见 [NuGet 接入](../integrations/nuget.md)；`docker` remote 仓型同样在 community 档（语义见[下文专节](#docker-remote-仓m14fr-129)与 [Docker 接入](../docker-registry.md#remote-仓pull-through-代理上游m14)）。

回显形态（`GET .../repositories/{key}`）：上游 `url` 与参数在 `configuration` 对象内，**`password` 字段不出现在响应里**（传过也不回显）。

### virtual 仓字段表

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-virtual \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"maven",
       "repositories":["maven-local","maven-remote-x","maven-remote-central"],
       "defaultDeploymentRepo":"maven-local"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `repositories` | 是 | 成员 repo key 数组（非空；成员不存在 → 400；成员为 virtual → 400，嵌套 virtual 不做） |
| `defaultDeploymentRepo` | 否 | 写路由目标：必须是成员中的 **local** 仓（指向非 local 成员 → 400）；别名 `defaultDeploymentRepoRef` / `deploymentRepository` 亦接受；未配置 → 一切写操作 405 |
| 成员的 `priorityResolution` | 否 | **标记在成员仓自己的配置上**（per-repository 字段，非 virtual 成员数组内）：优先桶整体前置于声明序 |

解析顺序（两桶序）：`priorityResolution: true` 的成员（桶内按声明序）→ 其余成员（桶内按声明序）；下载类解析**首命中即停**。响应头 `X-Binflow-Resolved-From: <成员key>` 标明来源——「拿到旧副本」排障先看它。成员增删/改序/改标记**即时生效**（逐请求现算，无解析缓存）。

## remote 请求流与缓存语义

`GET/HEAD /binflow/<remote>/<path>` 六步：

1. checksum 后缀（`.sha1`/`.md5`/`.sha256` 等）**不回源** → 404 `Checksums are not downloadable.`（checksum 经响应头 `X-Checksum-*` 提供；mvn 3.9 对缺失旁车只告警不失败）。
2. 查**负缓存**：期内已知 miss → 直接 404，零上游流量。
3. 查**本地缓存**：命中且未过 `retrievalCachePeriodSecs` → 直接服务（M1 头集齐全），零上游流量。
4. 过期或缺失 → 回源（Basic 凭据按仓配置）：上游 200 → 流式落盘（与响应逐位一致）；上游 404 → 写负缓存，若本地有过期副本仍回发过期副本（expired-but-serving）。
5. 上游 5xx/超时/连接失败 → 仓标记 **assumed-offline**（静默 `assumedOfflinePeriodSecs`）：有缓存（**含过期**）→ 服务缓存并附 `X-Binflow-Upstream-Error: <摘要>` 头；无缓存 → 404（message 含 offline 状态）；`hardFail: true` 仓 → 502。静默期结束后自动恢复回源，无需人工干预。
6. 上游 401/403（凭据错误）→ 视为资源不存在：404 + message 附上游状态摘要（可据此排障凭据）；不进负缓存、不开 offline 窗，改对凭据立即生效。

可观测头（`curl -D -` 直接看）：

| 响应头 | 含义 |
|---|---|
| `X-Binflow-Cache: HIT \| MISS \| STALE \| REVALIDATED` | 缓存状态（STALE = 上游故障时服务的过期副本） |
| `X-Binflow-Resolved-From: <成员key>` | virtual 解析命中的成员 |
| `X-Binflow-Upstream-Error: <摘要>` | 服务缓存时的上游故障摘要 |

其它行为：PUT/POST → **405** + `Allow: GET`（`Remote repository '<key>' is a read-only proxy cache; deployments to remote repositories are not accepted.`）；缓冲型响应（packument/simple/metadata）上限 64MB，超限 502；重定向逐跳跟随且每跳重过 SSRF 校验，上限 5 跳；大文件代理全程流式（1GB 实测服务进程 RSS 增量 < 256MB）。

## docker remote 仓（M14，FR-129）

docker 从 M2 的「仅 local」扩为 **local + remote 两态**（virtual 维持拒绝，见文末矩阵）。走根级 `/v2` 栈：`docker pull <host>/<remote仓key>/<镜像名>:<tag>` 首拉回源、此后命中本地缓存——与 maven/npm/pypi remote 同一套引擎语义（负缓存/TTL/降级/offline 窗全适用），观测面同为 `X-Binflow-Cache` 头族。**community 档即可用**（docker 槽在地板档，remote 不新增 license 槽——无 license 实例建仓 200，T-392/T-397 双轮实测）。

```bash
# 建仓：url 指向上游 distribution 根（含 /v2）——与 helmoci remote 同口径
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"docker",
       "url":"https://registry-1.docker.io/v2"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

| 面 | 行为（实测） |
|---|---|
| `url` 形态 | 上游 distribution **根含 `/v2`**：Docker Hub 形态 `https://registry-1.docker.io/v2`；上游是另一台 BinFlow 时 `http://<host>:<port>/v2/<上游仓key>`。回源路径 = `<url>/<镜像名（去掉本仓 key）>/manifests/<ref>` 与 `<url>/<镜像名>/blobs/<digest>`（live 证据：`GET http://…/v2/up-local/t397img/manifests/1`） |
| 首拉（MISS） | manifest + config/layer blob 逐路径回源落盘（blob 流式 + digest 强校验，不符不落盘）；`Docker-Content-Digest` 与直连上游推送 digest **全等** |
| 二次拉取（HIT） | **上游内容 GET 计数冻结**（delta 0，零回源）；`X-Binflow-Cache: HIT`（by-tag / by-digest / blobs 逐路径） |
| 上游认证 | 上游 401 + `WWW-Authenticate: Bearer` challenge → 仓凭据（username/password）自动完成 token 交换 → Bearer 重试，**舞步恰一轮**（token 按 scope 缓存，二跳零复舞）；无凭据时匿名交换（公用 registry 形态）。BinFlow 自身上游（闭匿名）首试即收 Basic，凭据直连不触发舞步。`enableTokenAuthentication: true` 时凭据以 Bearer 形态发出 |
| 降级 | TTL 过期 + 上游故障：有缓存 → **200 + `X-Binflow-Cache: STALE` + `X-Binflow-Upstream-Error`**，`Docker-Content-Digest` 不变（docker 客户端照常拉）；未缓存 ref → **404 `MANIFEST_UNKNOWN`**，message 附上游摘要，**零 5xx** |
| 边界 | remote 仓写动词 405（只读代理，通用规则）；**docker virtual 建仓 400**（PRD Q4 未交付，文案见[报错对照](#常见报错码对照跨域汇总v12-定案码)）；`tags/list` 只见已缓存 tag（通用口径，见 [FAQ](../faq.md)） |

> **SSRF**：上游 URL 命中私网/环回（如本机演练 `192.168.x.x`、内网 registry）建仓后拉取会被 [SSRF 防护](#ssrf-防护与-allowprivateupstream-放行指引)拒绝（404 message 带 `ssrf-guard` 摘要）——内网上游由 admin 配 `allowPrivateUpstream: true` 放行（本机双实例演练即用此通道实测）。另注：客户端在 dind 里访问宿主实例用 `host.docker.internal`，但**那是容器视角的名字**——remote 仓的 `url` 是 BinFlow 服务端发起的请求，须填服务端可达的地址。
>
> **dind 调试注记（客户端侧）**：dind 29.x 默认 containerd snapshotter 对 plain-HTTP registry 的 blob 取数会走 https 回退（不遵守 `--insecure-registry`）→ 拉取超时且 BinFlow 侧零到达。启动 dind 加 `--feature containerd-snapshotter=false` 回经典 overlay2（`docker run -d --privileged docker:dind --insecure-registry <host:port> --feature containerd-snapshotter=false`；详见仓库内 `web/e2e/README.md`）。macOS Docker Desktop 另有 dind↔宿主大包 PMTU 黑洞的环境症（~MB 级响应停摆），解法为 dind 内 `iptables -t mangle -A OUTPUT/-A INPUT -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1300` 后再开新连接（T-392 环境注记）。

## 远端浏览可选档（listRemoteFolderItems）

remote 仓默认**只列已缓存制品**（上文第 3 步的缓存语义不变）；`listRemoteFolderItems: true` 后，**浏览面**（控制台制品树与存储 API 的 FolderInfo）额外呈现**未缓存的远端目录与文件**——面向「先看上游有什么、再点拿什么」的浏览工作流。**包管理器协议面（helm repo/deb apt/rpm dnf 的解析与拉取）行为零变化**——可选档只影响浏览。

**适用面**：`helm` / `debian` / `rpm` 三型 remote（枚举引擎按各自索引元数据派生目录树——helm 的 index.yaml、deb 的 dists、rpm 的 repodata）。generic / maven 等类型**不做 HTML 目录抓取**（明确的取舍——上游目录页 HTML 无稳定结构，不建不伪造「将支持」）：携带 `true` → 400，实测文案：

```
invalid repository config: remote generic repository config: listRemoteFolderItems
true is not accepted (remote folder enumeration exists for the batch-1 types: helm, debian, rpm)
```

```bash
# 开档（PUT；显式 false = 关档——flip-off 过 round trip，缺省即 false）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helm-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"helm","url":"https://charts.example.com",
       "listRemoteFolderItems":true}'                          # 200
curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories/helm-remote
# configuration.listRemoteFolderItems: true（布尔恒在场，永为 canonical 拼写）
```

开档后的行为要点：

| 面 | 行为 |
|---|---|
| 树形态 | FolderInfo children 并入**派生行**（与缓存行同形：`uri` + `folder` 两字段）；枚举快照带 TTL（服务端常量），上游索引变更在快照过期后反映；改仓 `url` 即弃旧快照 |
| 派生行的元数据占位 | 派生文件行的 `size` / `lastModified` 为占位值（`0` / epoch-0）、无 sha2——**非事实值**；控制台相应列显示 `—`，点击条目回源后自纠为真实值并落地为缓存行 |
| 点击未缓存条目 | item-info GET 即 pull-through 回源（真实 size/digest），`?stats` 下载计数联动（与内容下载同一单源） |
| **上游降级不塌树** | 已缓存条目始终可列可用；远端层故障（上游不可达 / assumed-offline 静默窗）时 FolderInfo 附**可选 `remoteDegraded` 字段**（错误注记原文），控制台据此给降级横幅——实测两形：`remote enumeration unavailable: upstream 'index.yaml': connection failed`（不可达）/ SSRF 拒绝形态（注记含 ssrf-guard 摘要）。健康、关档与 local 树**恒缺省该字段**（`omitempty`）；文件级 body 恒不带（注记是目录级事实）；`?list` 平铺面镜像同一注记 |
| 关档（默认） | 浏览面仅缓存行——与历史版本行为一致；枚举快照失效即回到纯缓存视图 |
| virtual 扩面 | 含开档 remote 成员的 virtual 仓同样呈现该成员的派生行（聚合语义不变，越权仓零泄漏） |

控制台对应：建仓/编辑表单 Advanced 步复选「列出远端目录条目」+ 仓库详情回显行 + 树内「远端」Chip / 降级横幅——见[控制台指南](../console.md#制品树浏览器artifacts)。

## 缓存管理与强刷手法

- **强刷单个路径**：`DELETE /binflow/<remote>/<path>` → 204（仅删本地缓存，不触达上游），下次 GET 重新回源。这是 M3 唯一的强刷手法——**没有** `?refresh=true` 参数（保持 URL 语义纯净，与 Artifactory 一致）。mvn `-U` 拿不到新 SNAPSHOT 时，对 `maven-metadata.xml` 的缓存路径执行 DELETE 即可。未缓存路径 DELETE → 404（幂等）。**docker remote 注记（M14 实测）**：manifest 以 digest 寻址落盘，按 tag 路径或裸 hex 路径 DELETE 均不命中缓存节点（404、缓存不动）——清 docker remote 缓存请走「整仓清空」或观察头。
- **整仓清空**：删仓时带 `?deleteContent=true`（缓存 node 一并删除后重建仓），或按路径逐个 DELETE。
- **删仓**：`DELETE /binflow/api/repositories/<key>?deleteContent=true`。
- 缓存跨重启保留：重启实例（含换密钥重启）后已缓存内容直接 HIT，不回退重拉（T-77 复跑实证）。
- 每 remote 仓的命中统计 REST（`/api/v1/remote/stats`）为 P2 项，M3 未提供。

## 上游凭据与 `BINFLOW_REMOTE_CREDENTIALS_KEY`

上游 password 以 **AES-256-GCM** 加密落元数据库（密文形如 `enc:v1:<base64>`，明文永不落盘/不入 YAML/不入日志）。主密钥仅经环境变量注入：

```bash
# 生成（base64 的 32 字节随机密钥）：
openssl rand -base64 32
# 部署时注入（compose environment / K8s Secret / systemd EnvironmentFile）：
BINFLOW_REMOTE_CREDENTIALS_KEY=<上一步的值>
```

行为要点（均已实测）：

| 场景 | 行为 |
|---|---|
| 带钥启动 + 建带 password 的仓 | 密文 `enc:v1:` 落库；DB 中 grep 不到明文 |
| **无钥启动且库中已有加密凭据** | **启动 fail-fast**（退出码非 0），日志：`remote repository "<key>" stores encrypted credentials but no master key is configured: set BINFLOW_REMOTE_CREDENTIALS_KEY (base64 of exactly 32 bytes) and restart`——恢复 = 配好 env 重启 |
| 无钥运行时建带 password 的仓 | password 被**丢弃**并记 WARN（`remote repository password dropped — no credentials master key configured`），仓可用但回源走匿名——请勿在此状态下录入凭据 |
| 存量明文（升级场景） | 设钥后首次启动由迁移一次性加密，明文消失、代理行为不变 |
| 密钥轮换 | M3 不做（单密钥；轮换 = 换钥后重录各仓凭据） |

运维建议：密钥进 Secret 管理而非明文脚本；换钥前先确认无存量 `enc:v1:` 行依赖旧钥（有则凭据需重录）。

## SSRF 防护与 `allowPrivateUpstream` 放行指引

**默认拒绝（无需配置）**。BinFlow 的全部出站请求逐目标过 SSRF 校验链，命中以下类别一律 400（客户端报 `Cannot fetch '<repo>/<path>': upstream target refused — private or suppressed upstream (...)`）：

- 回环（`127.0.0.0/8`、`::1`，含 NAT64/6to4/IPv4-compatible 等过渡格式内嵌的回环地址）
- RFC1918 私网（`10/8`、`172.16/12`、`192.168/16`）与 IPv6 ULA（`fc00::/7`）
- 链路本地（`169.254/16`、`fe80::/10`——**含云 metadata 服务 169.254.169.254**）
- 未指定/保留（`0.0.0.0`、`::`）、组播、Teredo
- 非 http(s) scheme（`file://`、`gopher://` 建仓即 400）
- 重定向到上述目标（每一跳完整重过链，跟到第 5 跳为止）

防 DNS rebinding：校验通过的 IP 钉死用于实际拨号，不重新走系统解析。每次拒绝留 WARN 结构化日志（零堆栈）：

```
remote: outbound target rejected (ssrf-guard)  repo=<key> target=<host:port> category=<类别> phase=check ip=<IP>
```

**放行内网上游**（场景：上游是内网 Nexus/Artifactory）。操作步骤：

1. 确认目标上游确实受信且必要——放行后**该 remote 仓可触达部署网络内任意私网目标**（含同网段云 metadata 服务），等同授予该仓「内网 Reachability」权限，属高权限操作；
2. 建仓/改仓时由 **admin** 设置 `"allowPrivateUpstream": true`（仅 admin 可设，非 admin 请求被拒）；
3. 审计留痕：`audit_events` 表记录 `repo.create`/`repo.update` 且 detail 携带 `allowPrivateUpstream` 值（M3 无审计 REST，用 sqlite 查元数据库：`select time, actor, action, repo_key, detail from audit_events where detail like '%allowPrivateUpstream%'`）；
4. 定期复核：豁免仓清单 = `GET .../repositories?type=remote` 后逐仓核对配置，无用即收回（改回 `false` 即时生效）。

风险边界三条底线：豁免是**仓级**而非全局——只放真正需要内网上游的仓；重定向链同样在豁免范围内（内网上游 302 到内网目标会跟随），上游本身的安全水位就是你的水位；云 metadata 服务永不建议出现在任何上游路径上。

## 出站网络要求

M3 起 BinFlow 从「纯内网服务」变为**出网客户端**（架构规范 M3 网络增补，ADR-0012）：

- 需要对各上游域名的 **443/80 出站**放行（按 remote 仓的 `url` 逐仓开通；如 `repo.maven.apache.org:443`、`pypi.org:443`）；
- compose/Helm 部署产物**不内置 egress 代理**——出网策略（防火墙/NetworkPolicy/代理）由部署方自行配置；
- SSRF 防护在应用层（建仓 scheme 校验 + 请求时全 IP 双检），不依赖、也不替代部署层网络策略——纵深防御，两层都建议配。

## virtual 语义速查

| 面 | 行为 |
|---|---|
| 下载解析 | 两桶序、首命中即停；`X-Binflow-Resolved-From` 标来源；全 miss → 404 |
| remote 成员的 stale 语义 | 成员命中 stale 缓存（含过期副本/offline 期）即作为该成员结果返回，**不跳下一成员**；仅真 404（负缓存/无副本/offline 无缓存）才继续桶序 |
| 探索性 miss 无痕 | virtual 解析的 miss 不落缓存副作用（防成员扫描污染缓存） |
| metadata 聚合 | maven `maven-metadata.xml` versions 并集/latest 重算；npm packument 版本先到先得 + dist-tags/time 并集；pypi simple 条目并集——**每次现算不缓存**，任一 remote 成员的 miss 走负缓存静默 |
| 写路由 | 未配 `defaultDeploymentRepo` → PUT/POST/DELETE 一律 **405** + `Allow: GET`，body 逐字：`No local repository was configured as local deployment repository for the (<key>) virtual repository.`；配置后写操作路由到该 local 成员执行（权限/覆盖检查/checksum 链按目标仓语义；npm publish 与 pypi upload 同走路由），virtual GET 立即可见 |
| DELETE | **不透传成员删除**（有意不兼容，405）。未配写路由 → 上文 C5 文案；已配写路由 → `Deletes are not propagated through the virtual repository '<key>'; delete the artifact in its member repository directly.`。删缓存请对 remote 成员操作 |

> **【暂行】写路由口径**：virtual 默认不可写、显式配置 `defaultDeploymentRepo` 后路由写入，是 PRD v1.2 的暂行定案（开放问题 Q2，显式优于隐式——防「以为发到 local 实际进了缓存」类事故）。口径若调整将在本文与 CHANGELOG 更新。

## 上游兼容性速查

| 上游 | remote 代理 | 说明 |
|---|---|---|
| Maven Central（`https://repo.maven.apache.org/maven2`） | **可用**（实测） | mvn 全链经 BinFlow，含传递依赖 |
| pypi.org | **可用**（实测；302 → files.pythonhosted.org 逐跳过链后跟随） | simple + 下载全链 |
| registry.npmjs.org | **M3 不可用**——packument 不在 `<name>/packument.json` 布局路径（404），npmjs 代理归 M4 | 布局兼容上游（内网 Nexus/Artifactory）可用 |
| docker registry 上游（`packageType=docker`，M14） | **自指上游（另一台 BinFlow）可用**（实测：digest 全等、二拉零回源、降级 STALE）；Bearer challenge 上游（mock 全舞步）可用 | Docker Hub（`https://registry-1.docker.io/v2`）等公用 registry 直连**未实测**——Bearer 舞步同构，待验证 |
| 内网私网上游 | 可用，需 `allowPrivateUpstream: true` | 见 SSRF 小节 |

## M3 有意不兼容清单（汇总）

各协议域细节见对应接入指南；下表为里程碑级全表（PRD §2.2）：

| 不做项 | 表现 | 归属 |
|---|---|---|
| ~~docker 类型的 virtual 仓（聚合）~~ | **已交付**——docker 三仓型全开（local 自 M2 / remote 见[专节](#docker-remote-仓m14fr-129) / virtual 聚合读面按成员并集服务，实测建仓 200）；rclass × packageType 组合门已全量退役，建仓面唯一剩余门是 license 档位 | done |
| ~~docker 类型的 remote 仓~~ | **M14 已交付**（本行原为「M3 建仓 400、替代 `skopeo copy`」——T-392 开放矩阵后作废留痕） | M14 done |
| Gradle/Ivy/sbt/conan/go module 等其它生态 | `packageType` 仅 generic/docker/maven/npm/pypi，其余 400（Gradle 走 maven 仓可用，P2 观察） | M4+/M6+ |
| Maven 索引（indexer） | `/binflow/<repo>/.index/**` 404 | M4+ |
| remote 主动预取/复制（cache warming/replication） | 仅被动 pull-through | M6+ |
| 上游认证高级形态（Bearer 流、云 OIDC） | 仅 Basic + 匿名上游 | M4+ |
| npm search / score / audit 端点 | 404 | 不排期 |
| PyPI JSON API、yank（PEP 592）、分离 metadata（PEP 658） | 404（PEP 691 JSON simple **已支持**） | M4+ 评估 |
| `/binflow/api/pypi-ui/**` 托管 UI 前缀 | 404 | 不排期 |
| virtual 高级治理（成员排除模式、per-user 视图） | 仅解析顺序 + 可选写路由 | M4 |
| remote 缓存手动管理面（按路径 evict REST、缓存浏览） | 仅 `DELETE /binflow/<remote>/<path>`；stats REST P2 未提供 | M4 |
| virtual DELETE 透传成员删除 | 405（防误删上游缓存；对成员仓直接操作） | M4 评估 |
| 每协议 Web 控制台视图 | 树对五种 packageType 统一按路径呈现，docker 仓带 tag 徽标/摘要列（M8 跨仓树）；npm 包目录、pypi 归一名视图、maven metadata 只读面板仍为登记后续项（见[控制台指南](../console.md#制品树浏览器artifacts)） | 部分交付 |

## 常见报错码对照（跨域汇总，v1.2 定案码）

| 码 | message（节选） | 场景 | 处置 |
|---|---|---|---|
| 409 | `Repository '<repo>' rejected deployment of ...: handling of snapshots is disabled (handleSnapshots=false).` | maven 仓关了 SNAPSHOT（release 对应 `handleReleases`） | 改仓配置或换仓 |
| 409 | `Checksum error for '<repo>/<path>': received '<x>' but actual is '<y>'` | 客户端摘要与实测不符（client-checksums 仓；generic/maven/pypi 域同文案族） | 检查构件；或配 `server-generated-checksums` |
| 403 | `Cannot modify pre-existing version '<v>', aborting upload for: '<name>'` | npm 同版本重复 publish | 升版本或先 unpublish |
| 405 | `Remote repository '<key>' is a read-only proxy cache; deployments to remote repositories are not accepted.`（+`Allow: GET`） | 对 remote 仓 PUT/POST | 写操作走 local 仓或 virtual 写路由 |
| 405 | `No local repository was configured as local deployment repository for the (<key>) virtual repository.`（+`Allow: GET`） | virtual 未配写路由时的 PUT/POST/DELETE | 配 `defaultDeploymentRepo` 或直连 local 仓 |
| 404 | `Checksums are not downloadable.` | remote 仓的 checksum 旁车请求 | 用响应头 `X-Checksum-*`；mvn 客户端不受影响 |
| 404 | `Failed to find the requested resource '<repo>/<path>': upstream <host> is assumed offline (no cached copy; retry later).` | 上游故障且无缓存（默认 hardFail:false） | 等 `assumedOfflinePeriodSecs` 静默期自动恢复；急用可临时缩短该值 |
| 502 | `Upstream '<host>' failed for '<repo>/<path>' (hardFail enabled): ...` | 同上，但仓配了 `hardFail: true` | 同上 |
| 404 | `... (upstream answered 401 ...; credentials refused or insufficient)` | 上游凭据错误（401/403 视为 unfound） | 核对仓配置的 username/password |
| 400 | `Cannot fetch '<repo>/<path>': upstream target refused — private or suppressed upstream (...)` | SSRF 防护拒绝私网/环回目标 | 内网上游配 `allowPrivateUpstream`（见上）；公网上游检查 url/网络 |
| 400 | `package type not available on this instance: package type '<t>' is not available (license tier 'community' < 'pro')` | community 档建进阶包型仓（`go`/`nuget`/`cargo`/`conan`/`helm`/`helmoci`/`rpm`/`debian`——实测文案，license 档位是建仓面唯一剩余门） | 安装 license（见 [License 与 Add-ons 管理](license.md)）或改用五核心包型 |
| 400 | `file '<f>' already exists in repository '<repo>'; overwriting is not allowed (...)` | PyPI 同 filename 重复上传 | 升版本重构建 |
| 400 | `unknown action '<action>'` | PyPI 上传 `:action` 非 `file_upload` | 用 twine |
| 401 | `authentication required` | 匿名写操作（publish/upload/deploy） | 配置客户端凭据（settings.xml / `_auth` / `.pypirc`） |

所有非 2xx 响应为统一信封 `{"errors":[{"status":...,"message":...}]}`（E-01），无 HTML 栈页。

## 下一步

- 各协议客户端配置：[maven](../integrations/maven.md) · [npm](../integrations/npm.md) · [pypi](../integrations/pypi.md) · [docker](../docker-registry.md)
- 管理面 API（建仓/用户/token）：API 参考篇（随里程碑补齐）
- 从 Artifactory 迁移的概念对照：[faq.md](../faq.md)
