# Helm 包型（经典 chart 仓 + Helm OCI）行为规格

- 票据：T-292（FR-91.1 覆盖集第 5 份）；M11 Helm/HelmOCI 适配票的前置规格。
- **公开规范锚点（官方优先）**：① 经典仓协议以 Helm 官方 [The Chart Repository Guide](https://helm.sh/docs/topics/chart_repository/) 为准（chart 仓 = 任一能服务 `index.yaml` + tgz 的 HTTP 服务器；index.yaml 的 apiVersion/entries/generated/urls/digest/created 字段义；provenance `.prov` 并置；helm repo add 对无效 index.yaml 拒绝）。② HelmOCI 以 Helm 官方 [Use OCI-based registries](https://helm.sh/docs/topics/registries/) 为准（`oci://` 引用语法、push 不带 basename/tag、**tag 严格 = chart 语义化版本**、manifest 的 config/layer media type、`.prov` 作为附加 layer 自动上传、digest 安装、dependencies 的 `repository: oci://...`）。③ JFrog 行为面（virtual index 计算/namespace/relative urls/外部依赖改写/enforce layout/limitations）以 [Kubernetes Helm Chart Repositories](https://docs.jfrog.com/artifactory/docs/kubernetes-helm-chart-repositories) 官方文档为准；管理 REST 以 Calculate Helm Chart Index（`POST /api/helm/{repoKey}/reindex`）与 Helm Charts Partial Re-Indexing 官方文档为准。反编译（`reverse-src/artifactory/src/batch2-protocol/com/jfrog/ph/helm/**`、`org/artifactory/addon/helmoci/**`、docker addon 的 HelmOCI 接线，inv-3 §2.2 HELM/HELMOCI 行）补规范未写的空白：index.yaml 读写细节、URL 改写算法、虚仓缓存键、事件链、reindex 语义、HelmOCI 与 docker v2 栈的精确复用面，逐条标注「此条补充公开规范」。
- 置信度：`高` = 官方文档 + 反编译双证（或纯官方定义）；`中` = 仅反编译可见；`低` = 推断待验证。低置信条目不作验收依赖。

---

## 1. 基础路径形态

### 1.1 经典 chart 仓（HTTP API v1）

| 系统 | 形态 | 说明 | 置信度 |
|---|---|---|---|
| Artifactory（参考对照） | `$BASE/artifactory/api/helm/<repoKey>/...` | 经典仓挂在**专用 API 段**（`HelmConstants.API_HELM = "api/helm"`），下载端点族（index.yaml/tgz/_external/_transitive）都在此面下；`helm repo add` 用这个 URL。**上传不走此面**——`curl -T` 直接 PUT 存储内容面 `$BASE/artifactory/helm-local/<path>`（JFrog 文档原文）。 | 高（代码 + JFrog 文档双证） |
| BinFlow（实现口径，建议） | 内容面 `$BASE/binflow/<repoKey>/index.yaml` 等；兼容别名 `$BASE/binflow/api/helm/<repoKey>/...` | helm 客户端对 repo base 只做相对 GET（无根路径假设），两种挂载都兼容。建议：**主面 = 内容面**（与其余包型同构、免双路由）；`api/helm` 别名挂进 httpapi 的 `apiProtocolMounts`（router.go 现有 npm/pypi/nuget 同机制）供习惯 Artifactory URL 的用户直迁。归 ADR。 | —（BinFlow 决策建议） |

- 上传面：PUT .tgz 走存储内容面 `$BASE/binflow/<repoKey>/<path>`（与 Artifactory `curl -T` 行为同构，§4）。

### 1.2 HelmOCI（registry v2 复用）

| 系统 | 形态 | 说明 | 置信度 |
|---|---|---|---|
| Artifactory（参考对照） | docker v2 全套 REST 直接服务 HelmOCI 仓（`isDockerGroup() = Docker|OCI|HelmOCI`），仅分析打点按 Helm 归类、UI 按 config layer media type 区分 | 高（代码） | |
| BinFlow（实现口径） | 复用既有 docker adapter（ADR-0010）：`oci://$HOST/<repoKey>` → `/v2/<repoKey>/<chartName>/manifests/<version>`；仓行 `package_type=helmoci` 分发到 docker adapter（registry.go 的 per-package-type 命名空间扩展点） | 高（BinFlow 现状接线点明确；helmoci 类型注册为增量工作） | |

## 2. 经典仓端点表（相对仓根；反编译路由 + helm.sh 客户端行为双证）

| 方法 | 路径 | 语义 | 成功响应 | 错误响应 | 置信度 |
|---|---|---|---|---|---|
| GET | `index.yaml` | chart 索引（local=仓根存储文件；remote=回源；virtual=聚合面 §7） | 200 `text/yaml` | 404 | 高 |
| HEAD | `index.yaml` | 索引存在性（virtual：有成员仓 → 200 空体；无成员 → 404；**不流内容**） | 200 / 404 | 高 |
| GET | `<filePath>.tgz`（路由正则 `tar\.gz|tgz|zip|git\.refs|prov` 结尾） | chart 压缩包 / `.prov` 溯源文件 / zip 下载 | 200 流 + `X-Checksum-*` | 404 | 高 |
| GET | `_external/{protocol}/{url...}` | 外部依赖代理（remote/virtual 专属；local → 400 不支持） | 200 流 | 400 / 404 | 高 |
| GET | `_transitive/{protocol}/{url...}` | 传递依赖代理（上游 `_external` 的镜像路径；同上 local 拒绝） | 200 流 | 400 / 404 | 高 |
| GET | `{namespace}/_external/...`、`{namespace}/_transitive/...`、`{namespace}/<chart>.tgz` | 命名空间变体（virtual useNamespaces 开启时；namespace=成员仓 key） | 200 | 400（缺 namespace）/ 404（namespace 非成员，`Namespace '<ns>' does not exist within virtual repository: '<key>'`） | 高 |
| PUT | `<path>.tgz`（存储内容面） | 上传 chart（§4 触发自动 reindex） | 201 | 400/403（enforce layout §4.3）/409 | 高 |
| PUT | `<chart>.tgz.prov` | 上传 provenance 文件（普通文件存储；不进 index.yaml——官方 index 不含 prov 条目，`--verify` 按 `<tgz url>.prov` 约定取） | 201 | 400/403 | 高（官方 prov 约定 + 路由正则含 prov） |
| DELETE | `<path>.tgz` | 删除 + 索引条目移除事件 | 204 | 404/403 | 高 |
| POST | `api/helm/{repoKey}/reindex`（管理面） | 全仓 reindex（异步；见 §5） | 200 | — | 高 |
| POST | `api/helm/{repoKey}/reindex/{path}`（管理面） | 部分路径 reindex（同步等待；path 可为目录或单个 .tgz） | 200 | — | 高（JFrog REST 文档 + 代码） |

virtual 面 `index.yaml` 只能从**仓根**请求：子路径请求 index.yaml → 404（`The index file is being requested from an unsupported location...`）。高（代码显式断言）。

## 3. 存储布局（layout）

```
<repoKey>/                         ← classic Helm 仓（local）
  index.yaml                       ← 索引（系统生成、仓根、普通节点）
  <chart>-<version>.tgz            ← chart 包（路径任意，惯例仓根或子目录）
  <chart>-<version>.tgz.prov       ← provenance（用户 PUT，普通文件）

<virtualKey> 存储仓（虚仓缓存）：
  .index/<sha256(contextUrl)>/<permissionHashCode>/index.yaml
                                   ← 聚合 index.yaml 缓存；键含客户端上下文 URL 哈希
                                     （X-Orig-Client-Uri 或 servlet context URL）与权限集哈希
                                     ——不同对外 base/不同权限各自一条缓存

remote 缓存仓：
  index.yaml                       ← 上游索引原样缓存（可过期元数据类）
  _external/<protocol>/<url...>    ← 经改写下载的外部依赖落盘路径
  <chart>.tgz / .prov              ← 上游 chart/prov 缓存
```

- 物理 blob 仍走 BinFlow 既有 filestore 寻址；上述为逻辑路径。`.index` 是 Helm 的虚仓元数据根常量（`HELM_CACHE_METADATA_ROOT`）。高（代码常量 + VirtualCachePathHandler 拼接）。
- `.tgz` 判定 = 扩展名 `.tgz`（`tar.gz` 之外的形式按普通文件处理，不触发 chart 索引事件）；`index.yaml` 判定 = 路径以 `index.yaml` 结尾。高。

## 4. 上传链（PUT .tgz + 自动 index 重算）

### 4.1 事件链（高——indexer 走读）

1. PUT .tgz 落存储（统一 checksum 头链：malformed 400 / 不符 409）→ 入库事件进 "Helm Metadata" 队列（worker 默认 5）。
2. 索引器**读仓根既有 index.yaml**（不存在则新建空骨架 `apiVersion: v1` + `generated: <now>`）。
3. 解析 tgz：gzip → tar，取**归档根目录**的 `Chart.yaml`（ name/version/appVersion/description/home/icon/keywords/maintainers/sources/apiVersion/type/kubeVersion/deprecated/annotations 字段）与 `requirements.yaml` 的 dependencies（Chart.yaml 内嵌 dependencies 亦读）；无 Chart.yaml / 非法 tar → 该包跳过（不拒 PUT、不进索引）。
4. 写节点属性 `chart.*`（§4.2），然后更新 index.yaml 条目并整体回写仓根 `index.yaml`（read-modify-write，非增量 append）。
5. 完成后清虚仓缓存：删除包含该 local 仓的所有虚仓的 `/.index` 元数据缓存。
6. 删除/移动事件：按删除前节点的 `chart.name`+`chart.version` 属性从 index.yaml 移除条目（属性缺失则告警跳过）；move = 源移除 + 目标新增。

### 4.2 `chart.*` 节点属性（高——代码属性表）

| 属性 | 来源字段 | 备注 |
|---|---|---|
| `chart.name` / `chart.version` / `chart.appVersion` / `chart.description` / `chart.home` / `chart.created` / `chart.type` / `chart.apiVersion` / `chart.annotations` | Chart.yaml 同名字段 | `created` 为索引时刻（非 Chart.yaml created） |
| `chart.isDeprecated` | deprecated=true 时才写 `"true"` | |
| `chart.sources` / `chart.maintainers` / `chart.dependencies` | 列表多值属性 | dependencies 含 repository/condition/version 等 |

### 4.3 Enforce Layout（两个仓级开关，默认关；403 族；高——代码 + JFrog 文档双证，文档含确切 403 文案）

- `forceMetadataNameVersion`（Enforce Chart Name and Version）：tgz 文件名必须 = `<Chart.yaml.name>-<Chart.yaml.version>.tgz`；version 必须合法 SemVer2；Chart.yaml 缺 name/version → 403 `"This action is prevented due to the Enforce Layout Policy, the metadata of the package ... could not be read or is malformed."`
- `forceNonDuplicateChart`（Prevent Duplicate Chart Paths）：同 name+version 已在仓内**其它路径**存在 → 403（`...a package with the same name and version <n>-<v> already exists in the repository.`）；查重走 `chart.name`+`chart.version` 属性搜索（两开关齐开时按文件名搜索）。
- 校验发生在 PUT/复制/移动的入库钩子上；非 .tgz 路径不校验。JFrog 注意点：仅对 7.104.2+ 新建仓有效（forward-only）——BinFlow 绿地无此约束。

### 4.4 仓生命周期联动（高——config listener 代码）

- 创建 local Helm 仓 → 自动触发一次全仓 reindex（为已导入/复制的存量 chart 建 index）。
- remote 仓改 URL → 删除该仓缓存 `index.yaml` + 虚仓 `/.index` 缓存（强刷上游索引）。
- 实例对外 base URL 全局变更 → **全部 local Helm 仓全量 reindex**（index 内绝对 URL 需重写）。

## 5. index.yaml 语义与 reindex

### 5.1 index.yaml 生成细节（官方字段义 + 代码生成细节双证；标注处为补充）

- 结构：`apiVersion: v1` / `entries.<chartName>: [<版本条目...]` / `generated: <ISO8601>`（每次写出取当前时刻）。同一 chart 的版本条目按 **SemVer 降序**（不可解析版本回退字符串降序比较）——即 newest first，客户端 `helm install repo/chart` 无版本时取首条。高（官方示例同序 + 代码 TreeSet 比较）。
- 版本条目字段 = Chart.yaml 全字段 + 服务端附加三件：`digest`（tgz 的 **sha256** 十六进制，不带 `sha256:` 前缀——官方示例亦为裸 hex）、`created`（索引时刻 ISO8601）、`urls: [<一个下载 URL>]`。同 name+version 重复上传 = 条目覆盖（remove+add）。
- **urls 生成规则**（此条补充公开规范；高——代码）：默认 `urls = [<对外 base URL>/<repoKey>/<仓内路径>]`（绝对 URL）；开启 `helm.relative.urls.enabled`（本代码快照默认 false；JFrog 已宣布新版本默认 relative，要求 helm 3+）后 `urls = [<仓内相对路径>]`。BinFlow 建议**默认 relative**（helm 3 为唯一支持面，index 更小）。
- **YAML 序列化细节**（此条补充公开规范；中）：version/appVersion 值恒加双引号（防数值/布尔型 YAML 标量歧义——`1.0` 会被客户端解析为 float）；最小化引号、无文档起始标记；annotations 经自定义序列化器。条目写入前做**单条目 roundtrip 校验**（序列化→反序列化失败则该 chart 整体不进 index，仅日志）。
- `serverInfo` 字段：官方可选，代码不输出——客户端容忍缺失。高。

### 5.2 reindex 端点（JFrog 官方 REST + 代码语义）

- `POST /api/helm/{repoKey}/reindex`：全仓重算，**异步调度**（复位 Helm 事件队列后全量扫 .tgz：清空 index.yaml → 逐包重加（等效 removeChartsOnThePath("/")）→ 回写 → 清虚仓缓存）。仅 local/federated。
- `POST /api/helm/{repoKey}/reindex/{path}`：**同步等待**的部分重算。path 指向 .tgz = 单包重加；指向目录 = 先移除该路径前缀的全部条目再扫目录内 .tgz 重加（并行，worker 默认 5）。**移除匹配依据 = 条目 urls 前缀**（absolute 模式按 `base/<repoKey>/<path>`，relative 模式按 `<path>`）。

## 6. remote 仓语义（pull-through；中——命令/改写器走读 + JFrog 文档互证）

- `GET index.yaml`：回源 `<upstream>/index.yaml`，原样缓存到 remote 缓存仓（可过期元数据类，走 repo-semantics §7.4 元数据新鲜期 + 单飞）。
- `GET <chart>.tgz`：**回源地址 = chartsBaseUrl + 仓内相对路径**——chartsBaseUrl 取仓配置的 helm charts base URL，缺省回退仓 URL（对上游 index 内绝对 URL 的兼容手段是虚仓改写而非直连；remote 直取只服务路径可对齐的镜像仓）。高（代码）。
- 外部依赖代理：`GET _external/<protocol>/<url>` → 实际回源 `<protocol>://<url>`，落盘 `_external/<protocol>/<url>`；URL 必须命中该仓 externalDependencies 模式（Enable Dependency Rewrite + Patterns Allow List，Ant 风格，默认 `**`），否则 **400** `"Could not download HELM package at <url> - URL is not configured as an external dependency"`。`_transitive/<protocol>/<url>` → 回源上游的 `_external/<protocol>/<url>`（上游自身也是 Artifactory 时衔接）。高（代码 + JFrog 文档双证）。
- 回源 .tgz 后异步解析 Chart.yaml 写 `chart.*` 属性（供 Packages 视图/搜索）。
- PUT/DELETE 拒绝（通用 remote 写拒绝）。

### 6.1 chartsBaseUrl 分体基址与 `_external` 落盘（M13 T-367 增量；FR-117——配置位 / 回源基址规则 / 凭据边界 / 落盘缓存）

> 取证基线：本节 §6/S10（T-292 反编译规格）+ JFrog 官方文档的 remote 仓语义；实现落法为 BinFlow 决策，逐条标注。

- **配置位**：`chartsBaseUrl` 走 remote 仓 canonical config JSON（`repositories.config`；enableTokenAuthentication 先例——不加 remote_configs 列）。**per-protocol 定座（票内定案，T-342 §5 D-2① 的兑现）**：仅 `packageType=helm` 的 remote 仓接受该字段；其它包型 remote 携带之 → 建仓/改仓 400 点名字段（validateLocalKeypairRef 的 refuse-by-name 姿态——静默接受会产出 inert 字段，静默丢弃则让管理员误以为生效；两害取其明拒）。值必须为绝对 http(s) URL；显式 `""` = 清除（回退仓 URL）。
- **回源基址规则**：content 类路径（chart tgz / .prov / `_external` 折叠路径）的回源 = `chartsBaseUrl + 仓内相对路径`；**metadata 类（仓根 index.yaml）恒走仓 URL**——分体基址只描述「chart 存放地」，不描述「索引挂载地」。缺省（未配置）回退仓 URL（镜像对齐默认，S10 回退链）。引擎按 storage path 寻址缓存/负缓存/TTL（单一键空间，基址替换只影响出站一跳）。
- **凭据边界**：chartsBaseUrl 与仓 URL 同 scheme+host → 复用仓凭据客户端（绝对 URL 覆盖）；**异 host → 无凭据客户端**（仓上游凭据绝不发往第三方 host——redirect 链的 Authorization 剥离规则前移到取数源头）。`_external` 面恒为无凭据（第三方目标），SSRF 链（逐跳复查/私网豁免旗标/超时）照走。
- **`_external` 落盘缓存**：`_external/<protocol>/<url...>` 的取数经引擎 FetchAbsolute 缝落盘于同形折叠路径（content 类 TTL），负缓存 / STALE 降级 / 单飞与普通路径一视同仁；**第三方故障不写 assumed-offline 窗口、不受其静默**（外部目标的健康与仓上游独立）；二次拉取本地 HIT 零 egress。`_transitive` 本就走 path-joined 引擎取数（T-313 起已落盘），不受本票影响。虚仓 `_external` walk 的命中腿落进**该 remote 成员**的缓存（ReadVirtualMember 口径），响应带 `X-BinFlow-Resolved-From`。

## 7. virtual 仓语义（聚合 index.yaml + URL 改写；JFrog 文档主线 + 代码改写算法双证）

1. **请求分流**：`index.yaml`（元数据文件）→ MERGER 策略（聚合）；其余 .tgz/.prov → RESOLVER（first-found 成员序，repo-semantics §8.1）。
2. **聚合流程**（JFrog 文档口径 + 代码一致）：查虚仓缓存（§3 键：contextUrl 哈希 + 权限集哈希）→ 未命中/过期则逐成员取 index.yaml 抽 entries → 逐 chart 合并版本集（同名同版本按成员序 first-wins—— LinkedHashSet 语义，中）→ **URL 改写** → 重新序列化写缓存并返回。JFrog 行为细节：合并进行中其它同权限请求**等待既有计算**（锁等待默认 6 分钟，回退参数 `artifactory.virtual.repo.metadata.merge.lock.timeout.sec.HELM=120`）；缓存新鲜期 = 元数据检索缓存期（JFrog 文档称默认 6 分钟、建议 ≥60s）。
3. **URL 改写算法**（此条补充公开规范；高——改写器逐分支）：
   - local 成员条目：`urls[0]` ← `<对外 base>/<localRepoKey>/<仓内路径>`（absolute 模式）或 `<仓内路径>`（relative 模式）；
   - remote 成员条目：以该仓 chartsBaseUrl 为前缀替换为虚仓 URL `<base>/<virtualKey>/<路径>`——**识别基址 = 该仓 chartsBaseUrl，缺省回退仓 URL（T-367 起配置位生效，§6.1）**（**主面 = 内容面**——与 §1.1 BinFlow 定案一致；Artifactory 原始形态为 `<base>/api/helm/<virtKey>/<路径>`，BinFlow 的 `api/helm` 仅为只读别名、不进聚合 index 的下载 URL，否则 index 内 URL 与主面不一致——**TL 验收 HL-1 附带修订，T-293 合入 2026-08-26**）；条目 URL 指向 chartsBaseUrl 之外的**允许清单命中**的外部地址 → 改写为 `<虚仓URL>/_external/https/<host/path>`（协议 `://` 折叠为 `/`）；命中上游 `_external` 前缀 → 改写为 `_transitive`；允许清单未命中 → **保持原 URL**（客户端直连外部）；`oci://` 前缀条目默认改写，开启 `helm.preserve.oci.urls`（默认 false）则保留原样。
4. **namespace 模式**（虚仓开关，默认关）：entries 键与下载路径以 `<成员仓key>/` 前缀区分（`/<virt>/<成员>/<chart>.tgz`）；可选再开 `helm.modify.chart.name.with.namespace`（默认 false）把 chart 名本身改为 `<成员>/<chart>`。
5. HEAD index.yaml：有成员仓即 200（不实际聚合）；无成员 404。
6. PUT 到 virtual：通用 defaultDeploymentRepoRef 路由（405 语义）。

## 8. HelmOCI（registry v2 复用面）

### 8.1 客户端协议（官方 helm.sh 规范，高）

- `helm registry login <host>[:port]`——host 仅域名[:端口]，不带 scheme/path；凭据走 Basic/token。
- `helm push <chart>-<ver>.tgz oci://<host>/<repo>`——引用**不含 basename/tag**；basename=chart 名、tag=Chart.yaml 的 version（**严格绑定，客户端强制**）。`.prov` 与 tgz 并置时自动作为附加 layer 一起推。
- `helm pull/show/template/install/upgrade oci://<host>/<repo>/<chart> --version <v>`——下载类引用**含 basename**；支持 `@sha256:<digest>` 精确安装。
- dependencies：`repository: oci://<host>/<repo>` → `dependency update` 取 `oci://<host>/<repo>/<depName>:<version>`。
- manifest 形态（服务端只需按 OCI manifest 存取，不解析语义）：
  - config：`application/vnd.cncf.helm.config.v1+json`（Chart.yaml 的 JSON 化）
  - layer 1：`application/vnd.cncf.helm.chart.content.v1.tar+gzip`（chart 包本体）
  - layer 2（有 prov 时）：`application/vnd.cncf.helm.chart.provenance.v1.prov`
  - manifest 本体：`application/vnd.oci.image.manifest.v1+json`（OCI 而非 docker schema2）

### 8.2 与 docker v2 栈的复用面（对照 docs/reverse/docker-registry.md）

| docker-registry.md 端点/机制 | HelmOCI 复用 | 置信度 |
|---|---|---|
| §0 `/v2` 挂载（BinFlow ADR-0010：`/v2/**` 根异常 + 镜像名首段=repoKey 的 repo gate） | 完全复用：`oci://$HOST/<repoKey>/<chart>` → `/v2/<repoKey>/<chart>/...`；HelmOCI 仓行 package_type 分发到 docker adapter 是唯一增量接线 | 高（Artifactory `isDockerGroup={Docker,OCI,HelmOCI}` 同构） |
| §2 blob 上传会话（POST→PATCH→PUT，digest 校验，_uploads 24h 清理） | 复用（chart content layer / config blob 同一 push 流） | 高 |
| §3 manifest PUT 校验链 | 复用骨架；**增量校验项**：helm OCI manifest 的 config/layer media type 集合进 Content-Type→类型映射；Artifactory 侧对 HelmOCI 仓不强制 chart 语义校验（任意 OCI 工件可入，JFrog 文档明示此"限制"） | 高 |
| §4 manifest GET Accept 协商 | 复用（helm 发 `Accept: application/vnd.oci.image.manifest.v1+json`；现有 OCI 类型协商已覆盖） | 高 |
| §5 token 端点 / 401 challenge | 复用（`helm registry login` 走同一 Basic/token 面） | 高 |
| §6 tags/list、_catalog 分页 | 复用（`helm pull oci://.../<chart>` 不带 --version 时先 tags/list 取最新——tag 即版本，字典序取最大即客户端语义） | 中（helm 客户端 tags 取舍未逐版核对） |
| §8 删除（by-digest） | 复用 | 高 |
| §9 Referrers API | HelmOCI 可选（JFrog 7.90.1+ 用 referrers 关联 OCI 引用）；BinFlow docker adapter 现状 DE-16 规格体 404——helm pull/push **不依赖** referrers，M11 可不做 | 高（复用面）/ 中（优先级） |

- 虚仓限制（JFrog 文档）：Helm 与 HelmOCI 仓**不能混入同一虚仓**（两套协议族）。BinFlow 建仓校验对齐此约束。
- `.prov`/`--verify` 链在两形态下的差异：经典仓 = `.prov` 与 tgz 并置的普通文件（§2）；HelmOCI = prov 作为 manifest 附加 layer 随 push 上传、`helm pull --verify` 校验。服务端均不需理解内容。

### 8.3 HelmOCI remote 仓语义（M13 T-363 增量；K51——上游协议界 / Bearer 交换 / 校验与降级策略）

> 取证基线：[OCI Distribution Spec]（官方）+ BinFlow 既有 remote 引擎口径（repo-semantics §7）；无反编译对应物（JFrog HelmOCI remote 的内部实现不在反编译集合内），按 clean-room 以公开规范为准。

**上游 URL 约定**：`url` 指向上游 registry 的 **distribution API 根（含 `/v2` 前缀）**——Docker Hub 形态 `https://registry-1.docker.io/v2`；BinFlow 自指上游 `http://<host>:<port>/v2/<上游仓key>`。仓内相对路径直接拼接（`<image>/manifests/<ref>`、`<image>/blobs/sha256:<hex>`）。

**manifest / blob 按需回源**：
- manifest GET/HEAD（by tag / by digest）：先查本地缓存（`docker_tags`/`docker_manifests` 行 + `remote_cache` TTL 新鲜期）——新鲜则 `X-BinFlow-Cache: HIT` 直接服务；未命中回源：**Accept 头透传**（上游内容协商归上游，BinFlow 不做 schema 转换），响应体以**实测 sha256 寻址落盘**（`<image>/manifests/<hex>`），并写 manifest 行 + tag 指针（tag 请求时）→ 下次解析即本地事实。
- blob GET/HEAD：digest 键路径同样回源；**流式落盘**（不缓冲），commit 时**强制校验 digest**——与请求 digest 不符的 body 永不落盘（502，不缓存）。
- 二次拉取零回源（HIT，上游计数不增）；上游删除后缓存照常服务（新鲜期内 HIT）。

**上游认证链（docker remote 模式）**：上游 401 + `WWW-Authenticate: Bearer realm=…,service=…[,scope=…]` → 以仓配置凭据（Basic；`enableTokenAuthentication` 时为静态 Bearer）GET realm 完成 token 交换（`token`/`access_token` 双拼写、`expires_in` 缺省 60s、上限 1h）→ 以 Bearer 重试一次；token 按 scope（`repository:<image>:pull`）缓存。交换失败 → 降级路径（不 5xx）。

**manifest 校验策略**：结构 pass-through（与 local 面一致，无 media type 白名单）；实测 digest 为寻址唯一真相，上游声明 `Docker-Content-Digest` 不一致时**登记不拒绝**（WARN）；by-digest 请求实测不符 → 502 不落盘。ref 边（config/layer 描述符）best-effort 提取，解析失败不影响透传服务。

**降级矩阵（照既有 remote 口径，FR-116.5）**：上游 5xx / 传输故障 / token 交换失败——有过期副本 → `STALE` + `X-Binflow-Upstream-Error` 标记继续服务；无副本 → 404 unfound 族（MANIFEST_UNKNOWN / BLOB_UNKNOWN），**零裸 5xx**。上游 404：digest 键路径写负缓存（missedTTL 窗口内零回源应答 404）+ 过期副本续serve；**tag 404 不写负缓存**（tag 无存储键，直接 404）。上游 401/403（舞步穷尽后）：404 unfound + 摘要（不写负缓存——凭据状态可纠正）。SSRF 拒绝形态照 NFR-S13（400）。

**边界与口径**：
- 写动词全量 405 + `Allow: GET`（RE-05）；缓存失效走 REST 面 RE-06（DELETE /binflow/<repo>/…），/v2 面不做缓存失效动词。
- `tags/list` / `_catalog`：**服务已缓存的 tag/image 行**（tag 随 manifest 落地），不代理上游 tags/list——无缓存镜像的 tag 集不可见（helm 带 `--version` 拉取不受影响；不带 version 的"最新版"仅从已缓存 tag 中选取）。登记为 M13 口径，上游 tags/list 代理滚后续票。
- 会话与落盘分置：上游会话（Accept/Bearer/tag 解析）由 docker 适配器持有（internal/remote 引擎的 path-joined 取数无法表达 OCI 会话语义；出站链路复用引擎的导出 client——SSRF 链/逐跳复查/超时/重试同源）；缓存落盘经 repo 服务的 RemoteV2Plane 缝（与 `Engine.land()` 同一不变量：blob-first / checksum 寻址 / TTL 行 / GC hold）。singleflight 与 assumed-offline 窗口不在此面（登记：T-367 引擎缝可评估收编）。
- docker（非 helmoci）remote 建仓仍拒绝（T-380 条件票；Q5/K54 顺车评估 conductor 留痕）。

### 8.4 HelmOCI virtual 仓语义（M13 T-365 增量；FR-116.2——成员聚合 / 首见路由 / 降级矩阵）

> 取证基线：JFrog 官方文档的 virtual 总纲（repo-semantics §8 的 first-found 成员序）+ 本仓既有 /v2 数据链口径（§8.3）；HelmOCI virtual 的 JFrog 内部实现不在反编译集合内，按 clean-room 以公开规范 + 本仓 ADR-0013（虚仓两桶序/first-hit-stops）为准。

**成员构成与边界**：helmoci virtual = helmoci local + helmoci remote 成员的聚合读面（成员序 = 两桶：priorityResolution 标记成员在前、声明序在后）。「Helm 与 HelmOCI 不混仓」校验（§8.2 收尾条，T-309）在建仓/改仓面维持 400，双向生效（helm virtual 含 helmoci 成员同样拒绝）。**registry-v2 同型成员规则（T-367 rider，照 Artifactory「virtual 成员须同包型」语义）**：helmoci virtual 的成员必须 helmoci 型——配 docker 成员 → 400（`cannot mix the helmoci and docker package types` 形态，文案照混仓家族模式）；docker virtual 配 helmoci 成员同拒（矩阵解封后生效）。docker（非 helmoci）virtual 建仓仍拒绝（T-380 矩阵不动）。

**读面（/v2 路由按 row.Class=virtual 分流）**：
- manifest GET/HEAD（by tag / by digest）：按成员序逐成员解析——local 成员答以既有行+节点（行在而节点缺失 = 成员 miss，walk 继续）；remote 成员先读缓存事实（tag/manifest 行 + remote_cache 探测），HIT 即服务、NEGATIVE 跳过、STALE/MISS 走该成员的上游会话（Accept 透传 + Bearer 舞步照 §8.3），拉到后落进**该成员**的缓存（checksum 寻址、digest 强校验、tag 行随记）。**首见语义**：第一个能产出正文的成员胜出（成员序决定，非内容新旧）；响应带 `X-BinFlow-Resolved-From: <成员key>`（ADR-0013 诊断面）。
- blob GET/HEAD：同一 walk 作用在 digest 键路径上——local 成员查节点、remote 成员探测/回源/落盘（流式、commit 强校验 digest）。
- tags/list 与 _catalog：**并集面**（服务层四读用例的 virtual 臂）——tag 并集按成员序 first-wins 去重、全局排序、官方分页；image 并集以 **virtual key** 为前缀渲染（catalog 命名被寻址面而非成员）。remote 成员只贡献**已缓存**的 tag/image 行（§8.3 D-2 口径：不代理上游 tags/list——未缓存版本在 tags/list 不可见；helm 带 `--version` 拉取不受影响）。
- HEAD 与 GET 同路径同判定；未知引用走完 walk 后答 unfound 族（404 MANIFEST_UNKNOWN / BLOB_UNKNOWN），可附最后一次上游摘要，零裸 5xx。

**降级矩阵（FR-116.5 经聚合面）**：成员 RESULT 即走停止（含 STALE——过期副本 + `X-Binflow-Upstream-Error` 标记续 serve）；成员 UNFOUND（负缓存、无副本、上游 404/401/403、传输故障无副本）继续下一成员；**分类性非 unfound 失败**（SSRF 链 400、body 上限 502）原样透传、不吞成虚仓级 404。上游断连且副本过期：经 virtual 拉取照答 STALE；未缓存引用走完 walk 答 404。

**写面**：/v2 写动词对 virtual 一律 405 + `Allow: GET`。未配 defaultDeploymentRepo → C5 文案原样（「No local repository was configured …」）；已配 → 如实文案（registry v2 面的 push-through 路由**未实现**，点名目标仓、不谎称未配置）——登记为后续票。

**审计与可观测**：virtual 服务记一条以 **virtual key** 为址的 download 审计行（detail 带 resolvedFrom 成员），镜像 getVirtual 口径；remote 成员的缓存落盘照记成员为址的行（引擎 land 口径）。`X-BinFlow-Cache`（MISS/HIT/STALE）仅出现在 remote 成员服务面上，local 成员服务无缓存语义标头。

**缝的形态**：服务层出 `V2VirtualPlane` 能力缝（成员序 / 成员事实 / 成员落盘 / 成员上游事实 / 写拒绝渲染，全部以**成员在序**为守卫、不重跑成员自身的权限门——权限问题已在 virtual key 上由 /v2 路由门回答，镜像 ReadVirtualMember 口径）；docker 适配器驱动 walk（remote 成员的 miss 即上游会话是适配器的业务）。四读用例（ResolveManifest/ResolveTag/ListTags/ListImages）在 virtual 行上直接服务（服务层内 walk），`_catalog` 因此不再因 virtual 行 5xx（T-365 修复的隐患）。

## 9. 真实客户端命令清单（qa 可直接引用；helm 3.x——经典仓 ≥3.0，OCI 面 ≥3.8；本机 `brew install helm` 即可跑 L-h1~L-h4）

```bash
# ── 服务端准备（curl；$ADMIN = user:pass，$BASE 例 http://localhost:8080）──
# L-h1 构造并上传 chart（helm package 产出 <name>-<version>.tgz）
helm create mychart && helm package mychart            # mychart-0.1.0.tgz
curl -su $ADMIN -T mychart-0.1.0.tgz "$BASE/binflow/helm-local/mychart-0.1.0.tgz"
curl -su $ADMIN "$BASE/binflow/helm-local/index.yaml" | head -30    # entries.mychart + digest/urls 对账
# sha256 对账：digest 字段 == shasum -a 256 mychart-0.1.0.tgz

# L-h2 经典仓消费（helm 3）
helm repo add binflow "$BASE/binflow/helm-local" --username user --password pass
helm repo update                                        # GET index.yaml
helm search repo binflow/mychart
helm install rel1 binflow/mychart --version 0.1.0
helm pull binflow/mychart --version 0.1.0               # GET .tgz + 校验 digest
curl -su $ADMIN "$BASE/binflow/helm-local/index.yaml" -o /dev/null -w '%{http_code}\n'  # 200
curl -sI -u $ADMIN "$BASE/binflow/helm-local/index.yaml" | head -1                     # HEAD 200

# L-h3 provenance --verify 链（可选；有 GPG 私钥环境）
gpg --armor --detach-sign --output mychart-0.1.0.tgz.prov mychart-0.1.0.tgz
curl -su $ADMIN -T mychart-0.1.0.tgz.prov "$BASE/binflow/helm-local/mychart-0.1.0.tgz.prov"
helm pull binflow/mychart --version 0.1.0 --verify      # 取 <tgz url>.prov 并验签

# L-h4 reindex 管理面（BinFlow 路径以 ADR 定案为准，下为建议形态）
curl -su $ADMIN -X POST "$BASE/binflow/api/helm/helm-local/reindex"
curl -su $ADMIN -X POST "$BASE/binflow/api/helm/helm-local/reindex/subdir"

# L-h5 enforce layout（仓开启 forceMetadataNameVersion/forceNonDuplicateChart 后）
mv mychart-0.1.0.tgz wrong-name.tgz
curl -su $ADMIN -T wrong-name.tgz "$BASE/binflow/helm-local/wrong-name.tgz" -o /dev/null -w '%{http_code}\n'
# 期待 403 + Enforce Layout Policy 文案

# L-h6 HelmOCI（helm ≥3.8；BinFlow docker /v2 面）
helm registry login $HOST -u user -p pass
helm push mychart-0.1.0.tgz oci://$HOST/helmoci-local            # → /v2/helmoci-local/mychart/manifests/0.1.0
helm pull oci://$HOST/helmoci-local/mychart --version 0.1.0
helm show all oci://$HOST/helmoci-local/mychart --version 0.1.0
helm install rel2 oci://$HOST/helmoci-local/mychart --version 0.1.0
helm pull oci://$HOST/helmoci-local/mychart@sha256:<digest>     # digest 安装

# L-h7 virtual 聚合（members: helm-local + helm-remote（上游 charts.helm.sh 或任一 index 仓））
helm repo add binflow-virt "$BASE/binflow/helm-virt" -u user -p pass
helm repo update && helm search repo binflow-virt                # 聚合 index + 改写后的 urls
# remote 成员外部依赖场景：上游 index 含 https://github.com/... 条目且允许清单命中
#   → urls 改写为 $BASE/binflow/helm-virt/_external/https/github.com/...

# L-h8 HelmOCI remote pull-through（M13 T-363；上游 = 另一 BinFlow helmoci local 或任一 OCI registry）
#   建仓：PUT /binflow/api/repositories/helmoci-remote
#   {"rclass":"remote","packageType":"helmoci","url":"$UPSTREAM_BASE/v2/<上游仓key>"}
helm pull oci://$HOST/helmoci-remote/mychart --version 0.1.0     # 首拉 MISS 回源；再拉 HIT 零回源
helm install rel3 oci://$HOST/helmoci-remote/mychart --version 0.1.0
curl -sI $BASE/v2/helmoci-remote/mychart/manifests/0.1.0 | grep X-BinFlow-Cache   # MISS → HIT 观测面

# L-h9 HelmOCI virtual 聚合（M13 T-365；成员 = helmoci local + helmoci remote，remote 上游同 L-h8）
#   建仓：PUT /binflow/api/repositories/helmoci-virt
#   {"rclass":"virtual","packageType":"helmoci","repositories":["helmoci-local","helmoci-remote"]}
helm pull oci://$HOST/helmoci-virt/mychart --version 0.1.0      # 双域：local 成员直答 / remote 成员回源，digest 与 push 一致
helm pull oci://$HOST/helmoci-virt/mychart@sha256:<digest>      # by-digest 路由
helm install rel4 oci://$HOST/helmoci-virt/mychart --version 0.1.0
curl -sI $BASE/v2/helmoci-virt/mychart/manifests/0.1.0 | grep -E 'X-BinFlow-(Cache|Resolved-From)'
#   MISS/HIT/STALE + Resolved-From:<成员key> 观测面；tags/list = 成员并集（remote 侧仅已缓存 tag）
helm push mychart-0.2.0.tgz oci://$HOST/helmoci-virt            # → 405（C5 文案；v2 面无 push-through）

# L-h12 chartsBaseUrl 分体基址 + 依赖落盘（M13 T-367；上游 index 与 chart 体分置两址）
#   建仓：PUT /binflow/api/repositories/helm-remote
#   {"rclass":"remote","packageType":"helm","url":"$INDEX_UPSTREAM",
#    "chartsBaseUrl":"$CHARTS_BASE"}        # 非 helm 包型携带 chartsBaseUrl → 400 点名字段
curl -su $ADMIN $BASE/binflow/api/repositories/helm-remote | grep chartsBaseUrl   # REST 回显
helm repo add bf-remote $BASE/binflow/helm-remote && helm repo update
helm pull bf-remote/hetchart --version 0.1.0     # index 走 $INDEX_UPSTREAM，tgz 走 $CHARTS_BASE；字节一致
curl -sI $BASE/binflow/helm-remote/hetchart-0.1.0.tgz | grep X-BinFlow-Cache   # MISS → HIT
#   依赖链（虚仓改写 → _external 落盘）：上游 index 的条目 urls 指向外部 host 且允许清单命中
#   → 虚仓 index 改写为 _external/<scheme>/<host>/<path>；helm dependency update / pull 走该折叠路径
#   → 首拉 MISS 落盘（成员仓可见 _external/... 节点），二拉 HIT 零 egress（外部文件服务计数冻结）
```

认证：经典仓 `helm repo add --username/--password`（或 `--pass-credentials`）；HelmOCI `helm registry login`（http 明文仅限 qa）。高（官方文档）。

## 10. 与公开规范的差异/补充清单（逐条「此条补充公开规范」）

| # | 条目 | 来源 | 置信度 |
|---|---|---|---|
| S1 | 上传 .tgz 自动 index.yaml 重算链（read-modify-write、事件队列、虚仓缓存清理、删除按 chart.name/version 属性移除） | 反编译 | 高 |
| S2 | `chart.*` 十三项节点属性 | 反编译 | 高 |
| S3 | urls 生成规则（absolute/relative 双模式 + base URL 全局变更触发全量 reindex） | 反编译 + JFrog 文档 | 高 |
| S4 | YAML 序列化细节（version/appVersion 恒引号、roundtrip 校验失败剔除条目、SemVer 降序、无 serverInfo） | 反编译 | 中 |
| S5 | enforce layout 两开关与 403 文案、SemVer2 校验、查重策略 | 反编译 + JFrog 文档（文案原文） | 高 |
| S6 | reindex 全仓（异步）与按路径（同步等待）语义、移除匹配按 urls 前缀 | 反编译 + JFrog REST 文档 | 高 |
| S7 | 虚仓缓存键 `.index/<sha256(ctxUrl)>/<permHash>/index.yaml` 与合并锁等待 6 分钟 | 反编译 + JFrog 文档 | 高 |
| S8 | URL 改写算法全分支（local/remote/chartsBaseUrl/_external/_transitive/允许清单未命中保留原样/oci:// 保留开关） | 反编译 + JFrog 文档 | 高 |
| S9 | namespace 模式（路径前缀 + 可选改名）与子路径 index.yaml 404 | 反编译 + JFrog 文档 | 高 |
| S10 | remote chartsBaseUrl 回退链与 `_external`/`_transitive` 代理路径形态 | 反编译 | 高 |
| S15 | chartsBaseUrl 配置位（per-protocol refuse-by-name）、content/metadata 基址分置、异 host 凭据剥离、`_external` 落盘缓存（M13 T-367 增量，§6.1） | 本仓实现决策（反编译规格的落法） | 高 |
| S11 | HelmOCI = docker v2 栈直接服务（isDockerGroup 三类型集）、config model 继承 OCI、不校验 chart 语义 | 反编译 + JFrog 文档 | 高 |
| S12 | 仓生命周期联动（建仓自动 reindex / remote URL 变更清缓存） | 反编译 | 高 |
| S13 | 虚仓版本合并 first-wins 语义 | 反编译（LinkedHashSet 推断） | 中 |
| S14 | `.prov` 在经典仓按普通文件并置存储（不进 index） | 官方 prov 约定 + 路由正则 | 高 |

## 11. 显式不做（M11 登记建议）

1. **helm 2 客户端兼容**：JFrog 已弃 <3.0，relative-urls 模式天然要求 3+；BinFlow 只支持 helm 3。
2. **Referrers API（HelmOCI 附加引用面）**：helm push/pull 不依赖；BinFlow docker adapter 现状规格体 404 已够，M12+ 再议。
3. **`git.refs` 类下载路由正则里的边角形态**（`zip`/`git.refs`）：VCS 场景遗留，Helm 面按未知路径 404。
4. **index.yaml 服务端签名**（JFrog 无此能力，验签走 .prov）。
5. **多 base URL 仓（single/multi-base 文档分叉）**：BinFlow 单 base + relative urls 已覆盖需求。

## 12. M11 拆票就绪度自评

- **可直接拆票**：经典仓端点族（§2）、上传/事件链与 index.yaml 生成（§4–§5）、enforce layout（§4.3）、remote 代理语义（§6）、虚仓聚合 + URL 改写（§7）、HelmOCI 复用接线（§8.2）、客户端命令（§9）。
- **拆票依赖/裁决点**：① 挂载形态定案（内容面为主 + `api/helm` 别名，§1.1）；② relative urls 默认值（建议 true）；③ HelmOCI 依赖 docker adapter 的 package-type 分发扩展（registry.go `RepoTypes` 扩展点）与 helm media type 集合入 manifest 类型映射——建议 HelmOCI 单列一张票跟 docker adapter 联调；④ GPG/`--verify` 只需文件存储不依赖 keypair 体系（比 RPM/Debian 轻）。
- **低置信项**：无「低」级条目；S4/S13 及 tags/list 取最新版语义（§8.2）为中，均不阻塞拆票。

### 待验证清单（动态验证即可升高）

1. `helm repo update` 对 relative urls 形态 index 的解析（BinFlow 建议默认，需 helm 3 实测）。
2. 虚仓聚合下上游为 charts.helm.sh（URL 全为外部域）时的改写/透传行为（S8 允许清单默认 `**` 时全改写为 `_external`）。
3. helm 3.13+ 对 `helm pull oci://` 不带 --version 时的最新版选择（tags 字典序 vs SemVer，S8/§8.2）。
4. `.prov` 上传后 `helm pull --verify` 对 `<tgz-url>.prov` 取数路径在 BinFlow 挂载形态下的实际 URL 拼接。
