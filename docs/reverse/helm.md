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

## 7. virtual 仓语义（聚合 index.yaml + URL 改写；JFrog 文档主线 + 代码改写算法双证）

1. **请求分流**：`index.yaml`（元数据文件）→ MERGER 策略（聚合）；其余 .tgz/.prov → RESOLVER（first-found 成员序，repo-semantics §8.1）。
2. **聚合流程**（JFrog 文档口径 + 代码一致）：查虚仓缓存（§3 键：contextUrl 哈希 + 权限集哈希）→ 未命中/过期则逐成员取 index.yaml 抽 entries → 逐 chart 合并版本集（同名同版本按成员序 first-wins—— LinkedHashSet 语义，中）→ **URL 改写** → 重新序列化写缓存并返回。JFrog 行为细节：合并进行中其它同权限请求**等待既有计算**（锁等待默认 6 分钟，回退参数 `artifactory.virtual.repo.metadata.merge.lock.timeout.sec.HELM=120`）；缓存新鲜期 = 元数据检索缓存期（JFrog 文档称默认 6 分钟、建议 ≥60s）。
3. **URL 改写算法**（此条补充公开规范；高——改写器逐分支）：
   - local 成员条目：`urls[0]` ← `<对外 base>/<localRepoKey>/<仓内路径>`（absolute 模式）或 `<仓内路径>`（relative 模式）；
   - remote 成员条目：以该仓 chartsBaseUrl 为前缀替换为虚仓 URL `<base>/api/helm/<virtualKey>/<路径>`；条目 URL 指向 chartsBaseUrl 之外的**允许清单命中**的外部地址 → 改写为 `<虚仓URL>/_external/https/<host/path>`（协议 `://` 折叠为 `/`）；命中上游 `_external` 前缀 → 改写为 `_transitive`；允许清单未命中 → **保持原 URL**（客户端直连外部）；`oci://` 前缀条目默认改写，开启 `helm.preserve.oci.urls`（默认 false）则保留原样。
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
