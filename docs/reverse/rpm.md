# RPM / YUM 包型（repomd 仓库协议）行为规格

- 票据：T-292（FR-91.1 覆盖集第 4 份）；M11 RPM/YUM 适配票的前置规格。
- **公开规范锚点（官方优先）**：① repomd 仓库格式以社区事实规范为准——repodata 目录结构与 `repomd.xml` 骨架（`<repomd xmlns="http://linux.duke.edu/metadata/repo">` + `<revision>` + 逐 `<data type>` 的 `location href` / `checksum` / `open-checksum` / `timestamp` / `size` / `open-size` 子元素链），参照真实发行版仓（EPEL/Microsoft yum 仓）的 repomd.xml 实例与 [packagecloud: yum repository internals](https://blog.packagecloud.io/yum-repository-internals/) 的格式综述；primary/filelists/other 三索引的 XML 命名空间（`http://linux.duke.edu/metadata/{common,filelists,other,rpm}`）为社区既定标准。② yum/dnf 客户端行为以 [dnf.conf(5) 手册页](https://man7.org/linux/man-pages/man5/dnf.conf.5.html)为准（baseurl/metalink/mirrorlist、`repo_gpgcheck`、`gpgkey`、`$releasever`/`$basearch` 变量替换）。③ 管理 REST 以 JFrog [Calculate YUM Repository Metadata](https://docs.jfrog.com/artifactory/reference/calculateyumrepositorymetadata)（`POST /api/yum/{repoKey}`）为准。反编译（`reverse-src/artifactory/src/batch3-addons/org/artifactory/addon/yum/**`、`batch2-protocol/org/jfrog/metadata/**`（RPM 头解析 + 索引序列化引擎）、`batch2-protocol/com/jfrog/ph/rpm/**`、batch1-core 配置（artifactory.xsd / PackageTypeConfigFields / ConstantValues），inv-3 §2.4 YUM 行）补规范未写的空白：自动 repodata 重算链、校验算法选择、GPG 签名产物、历史代数保留、rclass 细节，逐条标注「此条补充公开规范」。
- 置信度：`高` = 官方/社区规范 + 反编译双证（或纯官方定义）；`中` = 仅反编译可见；`低` = 推断待验证。低置信条目不作验收依赖。

---

## 1. 基础路径形态

| 面 | 形态 | 说明 | 置信度 |
|---|---|---|---|
| 内容面（yum/dnf 客户端访问） | `$BASE/binflow/<repoKey>/<任意仓内路径>` | RPM 与 Debian 同为**纯存储路径协议**：dnf 直接 GET 仓内文件路径（`.rpm`、`repodata/repomd.xml`、索引文件），无独立 API 挂载点（Artifactory 同型：`$BASE/artifactory/<repoKey>/...`）。repoKey 第二段，与其余包型内容面同构（ADR-0008）。 | 高（Artifactory 对照 + dnf baseurl 语义） |
| 管理面（reindex） | Artifactory `$BASE/artifactory/api/yum/{repoKey}`；BinFlow 建议 `$BASE/binflow/api/yum/{repoKey}`（对照批次一 conan/debian 管理面口径，归 ADR） | 见 §3.2 | 高（JFrog 官方 REST 文档 + 代码双证） |

- 客户端 repo 配置（官方 dnf.conf 语义）：`baseurl=$BASE/binflow/<repoKey>[/子目录]`；**`$releasever`/`$basearch` 等变量由客户端在发请求前展开**，服务端只按字面路径服务——BinFlow 无需实现任何变量替换（`metalink`/`mirrorlist` 是客户端侧多镜像协商，BinFlow 单上游 baseurl 场景不涉及）。高（dnf.conf(5)）。
- 多发行版子树：一个仓内可并存多套 repodata（如 `fedora/linux/40/x86_64/repodata/`），由 `yumRootDepth`（§2.4）声明根深度——Artifactory xsd 文档原例即 `fedora/linux/$releasever/$basearch` → depth 4。高（xsd 文档原文 + 代码 AQL depth 过滤双证）。

## 2. 仓库布局（layout）

### 2.1 repodata 目录结构（高——格式骨架社区规范 + 生成代码双证）

```
<repoKey>/[<depth 前缀子目录>/]
  <name>-<version>-<release>.<arch>.rpm        ← .rpm 制品（路径任意，客户端 PUT 决定）
  repodata/
    repomd.xml                                  ← 索引的索引（系统生成）
    repomd.xml.asc                              ← detached ASCII-armored 签名（有密钥时）
    repomd.xml.key                              ← 签名公钥（有密钥时）
    <archivedDigest>-primary.xml.gz             ← 主索引（包名/NEVRA/依赖/摘要…）
    [<archivedDigest>-filelists.xml.gz]         ← 逐包文件清单（enableFileListsIndexing=true 时）
    <archivedDigest>-other.xml.gz               ← changelog 索引
    [<archivedDigest>-modules.yaml.gz]          ← modularity 元数据（透传，见 §4.5）
    [<digest>-<groupFileName>.xml 与 .xml.gz]   ← comps 组文件（用户上传 + 系统改名，§4.4）
```

- **索引文件名 = `<压缩形态摘要>-<indexName>.xml.gz`**（摘要为 sha1，或启用 sha256 后为 sha256）。这是 createrepo 系的标准 checksum-prefixed 命名（真实发行版仓同型），多代索引靠不同摘要天然并存。高（代码文件名拼接 + 真实仓实例双证）。
- repomd.xml 每个 `<data type="primary|filelists|other|group|group_gz|modules">` 子元素链：`location href="repodata/<文件名>"` → `checksum type=...`（压缩形态）→ `timestamp`（秒）→ `size`（压缩字节数）→ `open-size` → `open-checksum`（未压缩 XML 的摘要）。字段集官方实例与代码生成序一致；高。**注意代码固定输出 checksum 在 location 之后**（MarkupBuilder 顺序：location→checksum→timestamp→size→open-size→open-checksum），与 EPEL 实例（checksum 在前）顺序不同——XML 子元素顺序对该协议无语义（客户端按标签解析），BinFlow 任意顺序均可。高。
- `<revision>` 元素输出为空内容（无时间戳文本）。中（代码字面；真实仓常放时间戳字符串，客户端容忍空值——Artifactory 生产在用反证）。
- 根标记 `packages="N"`：primary/filelists/other 的根元素带 `packages="<包数>"` 属性；增量模式重算时会原地改写该数字（`packages="\d+"` 正则替换）。高（代码 + 真实仓实例）。

### 2.2 压缩与校验算法形态（此节整体为反编译补充公开规范）

| 项 | 行为 | 置信度 |
|---|---|---|
| 生成的压缩形态 | **只有 gzip（.xml.gz）**。不生成未压缩 .xml、不生成 .xz/.zst/.sqlite.bz2/.zck | 高（写入器只有 GZIPOutputStream 一条链） |
| 遗留 sqlite 清理 | 每次计算开始先删除 repodata 下全部 `*.sqlite.bz2`（对上游镜像带入的旧 sqlite 元数据主动清除） | 高（代码显式） |
| 校验算法 | 默认 **SHA-1**（repomd 的 checksum type 值为 `sha`）；开启 sha256（实例配置 `rpm` 节 `enableSha256` 或系统参数 `yum.local.repomd.calculate.sha2.enabled`，默认 false）后用 SHA-256 | 高（ConstantValues 默认值 + 代码双链） |
| 虚仓合并的读取面 | 合并成员索引时可解压 `.xml`/`.gz`/`.xz`/`.bz2`/`.zst` 五种形态（成员为外部镜像时上游可能是 xz/zst——BinFlow 成员都是自管仓，实际只遇 .gz） | 高（merger 解压分支） |
| zchunk（.zck） | **不生成不消费**（Artifactory 无 zchunk 代码路径） | 高（全量检索无 zck 处理） |

### 2.3 原子性与历史代数（此条补充公开规范；高——代码）

- 新索引先写仓内临时目录 `_tmp_<nanoTime><hash>/repodata/...`（`_tmp_` 前缀 + ≥13 位数字，有正则常量），全部写完后**整体 move 到正式 `repodata/`**，最后删临时目录。客户端不会看到半成品 repodata。
- **历史代数保留**：每个索引类型（primary/other/modules/modules.gz/group/filelists）按 `lastModified` 保留最近 N 代（`rpm.metadata.history.cycles.to.keep` 默认 **3**），更旧的删除。`enableFileListsIndexing=false` 时把**全部** filelists 旧文件删除。效果：yum 在 repomd.xml 更新瞬间仍可用旧摘要取到旧索引（等价 Debian by-hash 的防撕裂收益，靠代数保留实现）。

### 2.4 制品坐标属性与元数据缓存（此条补充公开规范）

| 项 | 行为 | 置信度 |
|---|---|---|
| `rpm.metadata.*` 属性 | 每个 .rpm 入库时解析 RPM header 写 9 个属性：`name/arch/version/release/epoch/license/group/vendor/summary`（前缀 `rpm.metadata.`）；license 额外拆 and/or/`/` 分隔喂给许可治理；这些属性同时是「本地生成属性」（镜像/复制时排除）与删除事件里增量索引的 NEVRA 来源 | 高（代码属性表 + specification 双处一致） |
| RPM header 解析 | 读 lead + signature + header 三段（redline 库），NEVRA 取自 header tag（NAME/VERSION/RELEASE/EPOCH/ARCH），依赖取 PROVIDE/REQUIRE/CONFLICT/OBSOLETE/RECOMMEND/SUGGEST 六组（name/flags/epoch/version/release 五元组），文件清单取 BASENAMES+DIRINDEXES+DIRNAMES，changelog 取 NAME/TIME/TEXT 三数组；RPM v6 的 LONGSIZE(5009)/PAYLOAD_SIZE(5112/5113) int64 tag 可读 | 高（interpreter 逐 tag 可见） |
| `.rpmcache` | 实例数据目录下 `[$HOME/data]/.rpmcache/<repoKey>/<path>` 缓存解析结果（JSON），以 .rpm 的 sha1（或 sha256）+ mtime 校验失效；删除制品时同步清缓存 | 高（代码） |
| `yumRootDepth` | 仓配置，默认 0（repodata 在仓根）。全量重算的候选 = AQL 查询**深度恰等于 yumRootDepth 的文件夹**（排除 `_tmp_` 前缀）；上传触发的重算取 .rpm 父路径的前 depth 段作重算根 | 高（代码 + xsd 文档双证） |

## 3. 端点/操作表

### 3.1 内容面（全部普通文件 GET/PUT/DELETE；高）

| 方法 | 路径（相对仓根） | 语义 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `repodata/repomd.xml` / `repodata/repomd.xml.asc` / `repodata/repomd.xml.key` | 元索引三件套 | 200 文件 | 404 | 高 |
| GET | `repodata/<digest>-primary.xml.gz` 等 | 索引下载（dnf 按 repomd 的 location href 取） | 200 流 | 404 | 高 |
| GET | `<任意 .rpm 路径>` | 制品下载（primary.xml 的 `location href` 指向仓内相对路径） | 200 流 + `X-Checksum-*` 头 | 404 | 高 |
| GET | `<任意 .rpm 路径>.sha1/.md5/.sha256` 等校验和后缀 | **不回源**，404 `"Checksums are not downloadable."`（remote 语义，repo-semantics §7.2） | — | 404 | 高 |
| PUT | `<任意路径>.rpm` | 上传 .rpm（校验链走 BinFlow 统一头：malformed 400 / 不符 409）+ 触发自动重算（§4） | 201 | 400/403/409 | 高 |
| PUT | `repodata/<groupFileName>.xml` | 上传 comps 组文件（触发组文件处理链 §4.4） | 201 | 400/403 | 高 |
| DELETE | `<任意路径>.rpm` | 删除 + 触发重算 | 204 | 404/403 | 高 |

### 3.2 管理面：reindex（高——JFrog 官方 REST 文档 + 代码双证）

`POST $BASE/binflow/api/yum/{repoKey}?path=<path>&async=<0|1>`，请求头 `X-GPG-PASSPHRASE`（可选，签名口令）；权限 **MANAGE**；`Content-Type: text/plain` 响应。

| 场景 | 行为 | 置信度 |
|---|---|---|
| repoKey 空 | 400 `"Target repository key cannot be blank"` | 高 |
| local/federated 仓，async=1 | 202 `"YUM metadata calculation for repository '<key>' accepted."`（异步调度，逐 yumRootDepth 候选文件夹全量重算） | 高 |
| local/federated 仓，async=0 且仓配置 `calculateYumMetadata=true`（自动计算开） | **409** `"Unable to perform immediate YUM metadata calculation on a repository with auto-async calculation enabled."` | 高 |
| local/federated 仓，async=0 且自动计算关 | 200 同 202 文案（同步完成） | 高 |
| virtual 仓 | `path` 自动补 `/repodata` 后缀；async=1 → 202 `"yum metadata calculation on path <path> in virtual repo <key> scheduled to run."`；async=0 → 200 `"... completed."`（直接触发虚仓合并，见 §6.3） | 高 |
| 仓不存在/非 yum | 404 `"Unable to find repository '<key>'."` | 高 |
| 无 MANAGE 权限 | 403 | 高（官方文档状态码 + 权限断言代码） |

## 4. 上传与自动 repodata 重算链（createrepo_c 等价语义；此节为反编译补充公开规范——官方工具链无服务端自动重算概念）

### 4.1 触发条件（高——interceptor 逐分支）

1. 事件面：local/federated yum 仓的 **afterCreate**（PUT/复制入）/ **afterDelete** / **afterMove**（源删 + 目标加）/ **afterCopy**（目标加）。
2. 路径过滤：路径以 `.rpm` 结尾，**或**命中 `yumGroupFileNames` 配置（`repodata/` 下且文件名以组文件名或其 `.gz` 结尾）。
3. 深度过滤：`.rpm` 父目录段数 ≥ `yumRootDepth`；重算根 = 父路径前 depth 段。
4. 仓开关：`calculateYumMetadata=true`（默认 **false**——不开则上传只存储不重算，需手动 reindex）。**BinFlow 建议默认 true**（对齐 Debian 自动索引的行为直觉），登记裁决。
5. 默认路径 = **全量重算该根目录**（异步，work queue "Rpm Metadata"，worker 数 `rpm.metadata.calculation.workers` 默认 8）。增量模式（`yum.local.metadata.incremental.indexing.enabled`，默认 **false**）为可选优化：解包既有 gz 索引原地增删包条目再重打包——BinFlow 不做（§9），全量重算对齐默认行为。

### 4.2 全量重算流程（高——主干逐行可读）

1. 清理遗留 `*.sqlite.bz2`（§2.2）。
2. 取仓签名密钥对（实例 keypair 体系按 repoKey 关联的 primary keypair）+ 口令（`X-GPG-PASSPHRASE` 传入或仓配置存储的解密值）；口令校验通过才生成签名。
3. 组文件预处理（§4.4）。
4. 遍历根目录全部 `.rpm`：查 `.rpmcache` 命中则复用解析结果，否则解 RPM header（§2.4）并回写缓存 + 属性；分块（100/包）序列化为三索引的包条目段。
5. 写索引：primary（恒生成）→ other（恒生成）→ filelists（仅 `enableFileListsIndexing=true`，默认 false）→ modules（§4.5）→ 组文件条目。全部写入 `_tmp_` 目录。
6. 生成 `repomd.xml`（§2.1 字段链）；随后签名（§4.3）。
7. **零包短路**：重算结果 0 个包且 `rpm.cleanup.repodata.when.no.rpm=true`（默认 false）→ 跳过索引生成并删除既有 repodata 目录；默认 false 时照常生成空索引。
8. `_tmp_` 整体 move 到 `repodata/`，按历史代数清旧（§2.3），删临时目录。
9. 计算完成（含异常）后联动触发包含本仓的全部 yum 虚仓重合并（§6.3）。

### 4.3 GPG 签名链（高——代码 + 官方 repo_gpgcheck 语义双证）

- 产物：`repomd.xml.asc`（repomd.xml 全文的 **detached ASCII-armored** 签名）+ `repomd.xml.key`（公钥，armored）。两者就是 yum/dnf `repo_gpgcheck=1` + `gpgkey=<url>/repodata/repomd.xml.key` 的验签对象（dnf 对 repomd 签名的标准校验路径：取 `repomd.xml.asc` 验 `repomd.xml`）。
- 无密钥 / 口令无效：**跳过签名**，且若仓内已有旧 `repomd.xml.asc`/`repomd.xml.key` 则**删除**（防止客户端拿旧签名验新 repomd）。
- 签名与索引一起走 `_tmp_` → move 的原子路径。

### 4.4 comps 组文件链（yumGroupFileNames；高——代码）

- 仓配置 `yumGroupFileNames`：逗号分隔组定义文件名列表（惯例 `comps.xml`）。
- 用户把组文件 PUT 到 `repodata/<name>.xml` → 重算时：改名为 **`<digest>-<name>.xml`**（digest = 该文件 sha1/sha256）→ 同内容生成 **`<digest2>-<name>.xml.gz`** → 同 basename 的旧组文件（无摘要前缀的或旧摘要的）删除 → 两个新文件各登记一条 `<data type="group">` / `<data type="group_gz">` 条目。
- 已存在**内容相同**（同 digest 名已存在）的组文件：删掉新上传的无前缀副本，索引沿用既有条目。

### 4.5 modules.yaml 透传（中——代码可见、语义面窄）

重算时取 repodata 下**最近修改**的 `*modules.yaml` 文件，gzip 后以 `<digest>-modules.yaml.gz` 登记进 repomd（type=modules）。服务端不解析不生成 modularity 内容——用户 PUT 什么就透传什么。

## 5. 校验链（端到端）

1. **dnf 侧（官方语义，高）**：`dnf makecache` 取 `repomd.xml` →（`repo_gpgcheck=1` 时）`gpgkey` 验 `repomd.xml.asc` → 按 `<checksum>`（或 `open-checksum`）校验 primary.xml.gz → 解析包条目 → 下载 `.rpm`（location href + `checksum`（pkgid）+ `size` 字段校验）→ （`gpgcheck=1` 时）验包签名。
2. **服务端义务**：repomd 的 checksum/open-checksum/size/open-size = 实测值（写入器边写边算摘要与计数，天然一致）；primary 条目的 pkgid checksum = .rpm 实测 sha1/sha256、size = .rpm 字节数、location = 仓内相对路径。**两段 checksum 算法一致**（同 §2.2 的 sha1/sha256 开关）。
3. **上传校验**：PUT .rpm 走 BinFlow 统一 checksum 头链（malformed 400 / 不符 409，repo-semantics §5）；RPM header 非法（非 RPM 格式）**不拒绝 PUT**（201 照常），但索引时该包被跳过（解析失败仅告警日志）。中（走读推断：解析异常路径只 log 不抛；非 .rpm 扩展名内容本来就不触发索引）。
4. **XML 合法性**：包字段含非法 XML 字符（XMLChar 校验）时该包从对应索引剔除（primary/filelists/other 各自独立校验），不影响其它包。中（模型校验方法可见；调用点未逐行核对）。

## 6. rclass 三态行为（对照 repo-semantics.md 框架）

| rclass | 行为 | 置信度 |
|---|---|---|
| local | 全量管线（§4）；PUT/DELETE + 自动重算；组文件/modules 透传；签名链；管理面 reindex 全分支 | 高 |
| remote | 单上游镜像 pull-through（repo-semantics §7.2 通用流程）。yum 特化三点：① **可过期（expirable）路径集合** = `repomd.xml` + repodata 下**非摘要前缀**文件（组文件等）+ 密钥类扩展名（gpg/rsa/asc/pem/crt/cer/key/dsa）+ 无扩展名含 "gpg" 的文件——即 repodata 元数据按 remote 元数据新鲜期回源刷新，摘要前缀的历史索引按制品语义缓存；② `.sqlite` 文件不触发任何索引联动；③ 回源下载 `.rpm` 后异步解析 header 写属性（供搜索/后续本地化），回源下载 repodata 文件后联动虚仓重合并。PUT/DELETE 拒绝。`.rpm` 缓存仓内的 repodata **不重算**（上游的 repodata 原样缓存——Artifactory 不为 remote 缓存自算 repomd，`mirrorlist` 场景由虚仓合并兜底） | 高（expirable 正则 + interceptor 双证；③为代码字面） |
| virtual | **repodata 聚合 + 缓存仓存储**，见 §6.3 | 中—高（分条标注） |

### 6.3 virtual 聚合语义（中——collector/merger/writer 三层走读，归并细节未逐行复现）

1. **拦截面**：GET/HEAD repodata 元数据文件（路径含 `primary.xml`/`filelists.xml`/`other.xml`/`modules.yaml`/`group.xml`/`updateinfo.xml`/`repomd.xml` 之一）且 yum 虚仓**非 cache 成员数 ≥ 2** 时进入虚仓索引逻辑；≤1 个成员则透传（首命中即停）。
2. **缓存判定**：虚仓存储仓（`<key>-cache` 同构）内该路径的聚合 repomd.xml 存在且各成员 repomd sha1 与缓存在 repomd.xml 节点属性上的记录一致 → 直接回缓存；否则重算。**同步计算超时 5000ms**（`yum.virtual.sync.metadata.calculation.timeout.millis`，线程池 10）——超时转异步继续算、本次请求回落缓存/透传。
3. **收集**：逐成员（跳过 cache 仓）取 `<path>/repodata/repomd.xml`，解析 location href 定位 primary/filelists/other/modules/group/updateinfo 实际文件；成员 repomd 缺失或解析失败则整个成员跳过。**少于 2 个成员有 repomd 时不生成聚合**（透传首命中）。
4. **合并**：流式逐行拼接包条目段（剥离各成员的 XML 声明/DOCTYPE/根标签）；**去重键 = `name+arch` 字符串拼接**；priorityResolution 成员的包恒保留，非优先成员的同 name+arch 包丢弃——**非优先成员之间互不去重**（两个非优先成员同 name+arch 会双条目共存，Artifactory 原样行为，BinFlow 沿用即最小实现）。group/modules/updateinfo 索引同法合并（modules/updateinfo 仅非优先列表收集）。
5. **产出**：合并后的 primary/filelists/other(/modules/group/updateinfo) 重新 gzip（新摘要名）+ 新 repomd.xml 写入虚仓存储仓；各成员 repomd sha1 记为该 repomd.xml 的节点属性（下次缓存判定输入）；虚仓可配自己的 keypair 签名（§4.3 同链）。
6. 下载 `.rpm`：虚仓通用 first-found（成员序，repo-semantics §8.1）。
7. PUT 到 virtual：走通用 defaultDeploymentRepoRef 路由（405 语义，repo-semantics §8.2）。

## 7. 真实客户端命令清单（qa 可直接引用；本机 macOS 无 yum/dnf，在 CentOS/Rocky/Fedora 容器执行）

```bash
# ── 服务端准备（curl；$ADMIN = user:pass，$BASE 例 http://localhost:8080）──
# L-r1 上传 .rpm（可用任意发行版 .rpm，或 rpmQueryBuilder/fpm 现造）
curl -su $ADMIN -T mypkg-1.0.0-1.el9.x86_64.rpm \
  "$BASE/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm"
# 自动重算断言（calculateYumMetadata=true 时异步，稍候或同步 reindex）
curl -su $ADMIN -X POST "$BASE/binflow/api/yum/rpm-local?async=0"   # 200（自动计算开启时 409 → 改 async=1）
curl -su $ADMIN "$BASE/binflow/rpm-local/repodata/repomd.xml" | head -20
curl -su $ADMIN "$BASE/binflow/rpm-local/repodata/" # 目录清单：<sha>-primary.xml.gz 等三件套

# ── 客户端（CentOS Stream 9 / dnf 4.x；Fedora 40+ / dnf 5.x 同命令面）──
# L-r2 无签名仓接入（yumRootDepth=0 场景）
cat > /etc/yum.repos.d/binflow.repo <<'EOF'
[binflow]
name=BinFlow RPM
baseurl=http://$HOST:8080/binflow/rpm-local
enabled=1
gpgcheck=0
repo_gpgcheck=0
EOF
dnf clean all && dnf makecache            # repomd→primary→.rpm 全链
dnf repoquery --repo binflow mypkg        # NEVRA 出现
dnf install -y mypkg                      # 安装 + 包校验

# L-r3 子树 + 变量替换（yumRootDepth=4，repodata 在 fedora/linux/40/x86_64/）
cat > /etc/yum.repos.d/binflow-sub.repo <<'EOF'
[binflow-sub]
baseurl=http://$HOST:8080/binflow/rpm-local/fedora/linux/$releasever/$basearch
gpgcheck=0
EOF
# （客户端 $releasever 需与上传路径的发行版号一致——qa 直接写字面 40/x86_64 最稳）

# L-r4 元数据签名链（实例配置 RPM keypair 后）
curl -s "$BASE/binflow/rpm-local/repodata/repomd.xml.key" -o /etc/pki/rpm-gpg/binflow.asc
# repo 文件追加： repo_gpgcheck=1  gpgkey=file:///etc/pki/rpm-gpg/binflow.asc
dnf clean all && dnf makecache            # 验 repomd.xml.asc

# L-r5 手动 reindex 状态码矩阵
curl -su $ADMIN -o /dev/null -w '%{http_code}\n' -X POST "$BASE/binflow/api/yum/rpm-local?async=1"  # 202
curl -su $ADMIN -o /dev/null -w '%{http_code}\n' -X POST "$BASE/binflow/api/yum/rpm-nonexist"       # 404

# L-r6 remote pull-through（仓 rpm-remote 上游 mirrors.kernel.org/fedora-epel 或任一 yum 仓）
# baseurl 指虚仓或直接 remote：断言二次 makecache 不回源（-v 观察）
# L-r7 virtual 聚合（members: rpm-local + rpm-remote，≥2 成员有 repodata 才聚合）
curl -su $ADMIN "$BASE/binflow/rpm-virt/repodata/repomd.xml" | grep -c "<data type"   # 合并索引可见
```

认证：dnf 支持 repo 文件行内 `username=`/`password=`（http 明文仅限 qa）；或 `/etc/yum/vars/` 自定义变量。中（dnf 侧约定，qa 验证）。

## 8. 与公开规范的差异/补充清单（逐条「此条补充公开规范」）

| # | 条目 | 来源 | 置信度 |
|---|---|---|---|
| S1 | 上传 .rpm 自动 repodata 重算全链（触发条件/深度推导/异步队列/零包短路）——官方 repomd 格式只定义文件不定义服务端行为 | 反编译 | 高 |
| S2 | 索引命名 `<digest>-primary.xml.gz` + sha1 默认/sha256 开关 + repomd checksum type 值 `sha` | 反编译 + 真实仓实例 | 高 |
| S3 | 只生成 .xml.gz；sqlite.bz2 主动清除；zck/xz/zst 不生成 | 反编译 | 高 |
| S4 | `_tmp_` 原子替换 + 历史代数保留（默认 3 代，per 索引类型） | 反编译 | 高 |
| S5 | GPG 签名产物 repomd.xml.asc/.key、无密钥删旧签名、X-GPG-PASSPHRASE | 反编译 + dnf repo_gpgcheck 语义 | 高 |
| S6 | comps 组文件 checksum 改名 + .gz 生成 + 同内容去重 | 反编译 | 高 |
| S7 | modules.yaml 最近修改透传 | 反编译 | 中 |
| S8 | `rpm.metadata.*` 九属性 + `.rpmcache` 解析缓存（sha/mtime 失效） | 反编译 | 高 |
| S9 | reindex 端点全状态码矩阵（含 409 auto-async 冲突、虚仓 202/200 文案） | 反编译 + JFrog REST 文档 | 高 |
| S10 | remote 可过期路径集合（repomd + 非摘要前缀文件 + 密钥扩展名）与 .rpm 回源后属性回填 | 反编译 | 高 |
| S11 | virtual 聚合：≥2 成员才算、repomd-sha1 属性做缓存键、同步 5000ms 超时转异步、name+arch 去重且非优先成员间不去重 | 反编译 | 中 |
| S12 | XML 非法字符包剔除、非 RPM 内容包跳过（不拒 PUT） | 反编译 | 中 |
| S13 | 增量索引模式存在但默认关闭 | 反编译 | 高 |
| S14 | `<revision>` 空内容、`packages="N"` 增量改写 | 反编译 | 中 |

## 9. 显式不做（M11 登记建议）

1. **sqlite 元数据**（primary.sqlite.bz2 等）：上游生态已淘汰（createrepo_c 默认也不出），Artifactory 反而主动清除——BinFlow 不生成。
2. **zchunk（.zck）/ xz / zst / 未压缩 .xml 生成**：只出 .gz；合并读取面的多格式解压也无需实现（BinFlow 成员只有自管仓）。
3. **增量索引模式**：默认关闭的特性，全量重算即对齐 Artifactory 默认行为。
4. **updateinfo / prestodelta / comps 自动生成**：updateinfo/prestodelta 从不生成（虚仓只透传合并成员已有的）；comps 只透传用户上传文件（§4.4），不合成。
5. **mirrorlist / metalink**：客户端侧机制，服务端无对应面。
6. **`createrepo_c` 工具服务端化**：无此能力面，索引只由上传链/管理面生成。

## 10. M11 拆票就绪度自评

- **可直接拆票**：内容面端点（§3.1）、repodata 布局与命名/压缩/算法（§2.1–2.2）、原子性 + 历史代数（§2.3）、属性/缓存/yumRootDepth（§2.4）、上传 + 自动重算链（§4.1–4.2）、签名链（§4.3）、组文件链（§4.4）、reindex 端点矩阵（§3.2）、local 行为、remote 可过期集合（§6）、客户端命令（§7）。
- **拆票依赖/裁决点**：① GPG 签名依赖实例 keypair 体系（与 Debian 规格同依赖）——M11 无 keypair 则先落 unsigned 模式；② `calculateYumMetadata` 默认值（Artifactory false vs BinFlow 建议 true）；③ 虚仓聚合的最小实现边界（建议：成员序拼接 + name+arch 对优先成员去重 + 新 repomd 不签名）。
- **低置信项**：无「低」级条目；S7/S11/S12/S14 为中，均不阻塞拆票。

### 待验证清单（动态验证即可升高）

1. dnf 4/5 对 `<revision>` 空值与 checksum type=`sha`（SHA-1）的实际容忍（真实仓多为 sha256——建议 BinFlow 默认直接开 sha256，升 ADR 时一并定）。
2. virtual 聚合下真实 `dnf makecache` 全链（S11 双条目共存时的客户端表现）。
3. remote 镜像外部 yum 仓（上游索引为 xz 压缩）时 BinFlow 是否需要透传即可（上游 repodata 原样缓存，无需解压）。
4. 组文件上传后 dnf `group list` 的可见性（comps DOCTYPE 剥离重组后的兼容性）。
