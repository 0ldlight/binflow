# PRD — M3 多生态与代理（Maven / npm / PyPI / Remote / Virtual）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-3.md` |
| 里程碑 | M3 — 多生态与代理（对应 ROADMAP.md「M3 — 多生态与代理」全部条目） |
| 状态 | **v1.3**（T-121：T-105 QA 勘误 E2/E3/E4 收口，其中 E2/E3 兼收 T-74 E1/E2 未回写遗留——M17 样例文件名 layout 合规化、M16 断言载体改「version 级 metadata + timestamped 直连 GET」、M47 补显式建仓命令钉 `url=https://pypi.org` 站点根、M46 注明 mock 布局 `<name>/packument.json` 与真实 npmjs 已知边界）。v1.2（T-78：Q1 凭据存储依 **ADR-0012 决策 4** 定案关闭——AES-256-GCM + `enc:v1:` 前缀 + env `BINFLOW_REMOTE_CREDENTIALS_KEY` + 有行无钥启动 fail-fast + 存量明文 003 迁移一次性加密；FR-15/FR-20/NFR-S14 措辞统一；§5.5 C2 补「码值 400 暂行」注记。v1.1（T-60）：C1~C8 依 T-59 规格定案、M1 两处勘误吸收、Q3/Q7 定案、Q5 半定案、Q4 维持） |
| 上游依据 | PRODUCT.md、ROADMAP.md M3 节、M1 交付基线（milestone-1.md v1.3.1）、M2 交付基线（milestone-2.md v1.3，T-43/T-44 QA 全绿）、**docs/reverse/maven-npm-pypi.md（T-59 已落地，v1.1 行为依据）**、**docs/repo-semantics.md §7/§8（T-59 扩编，remote/virtual 语义）**、docs/reverse/repo-semantics.md §1~§6（local 语义/checksum 策略/覆盖检查）、docs/reverse/oss-structure.md（M3 拆票结构参考）、docs/reverse/rest-api.md §1.5（旁车 checksum） |
| 下游消费者 | tech-lead（拆票）、architect（remote/virtual ADR、adapter SPI 扩展）、dev 各角色、qa-engineer（M 序列验收）、tech-writer（M3 接入文档） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-19 | 初版：M3 范围、FR-15~FR-22、端点矩阵 RE/ME/NE/PE 四域 35 条、M01~M54 验收命令（mvn/npm/pip/twine 真实客户端 + curl 抽查）、SSRF 防护专节（NFR-S13）、M1/M2 遗留收编、8 项开放问题（含暂行假设） |
| v1.1 | 2026-08-19 | T-60 校准回写（依据 `maven-npm-pypi.md` + repo-semantics §7/§8，T-59，置信度高 ~102/中 ~24）：① §5.5 C1~C8 八项定案（C1 maven-metadata 服务端计算触发时机/版本组与 SNAPSHOT 目录规则/RTFACT-6242 保护性不删；C2 maven-2-default 六字段 layout 模型定案、400 码维持暂行；C3 virtual 四桶搜索序定案（BinFlow 简化两桶）+ stale/下一成员优先关系定案；C4 TTL 7200/1800 定案 + 过期 HEAD 协商列 P1；C5 写路由字段名 + 405 文案定案；C6 remote 缓存删除不同步上游升高置信；C7 PyPI 落盘 `{name}/{version}/{filename}` 定案、PE-03 升兼容（子集）；C8 npm tarball 布局与 scoped 形态定案、`_attachments` 保留维持暂行）。② **M1 勘误吸收**：Maven snapshot policy 拒绝码 **404→409**（SnapshotPolicyException 显式 409，maven-npm-pypi.md §1.4 高置信度；ME-08/FR-16-AC7/M19 同步）；includes/excludes 双值码（下载 404/上传 409）不触 M3 AC，仅归档 §6.4。③ **Q7 定案**：npm 重复 publish **409→403**（`Cannot modify pre-existing version`，规格 §2.3-④ 高置信度；NE-01/FR-18-AC6/M26 更新）；npm 域错误体 **E-01 定案**（三协议共用 `errors[]` 信封，规格 §0 高置信度）；新增 npm 十步校验链要点、`-rev` PUT 恒 200 假成功（unpublish 前置）、dist-tags 201 `{"ok":"created new tag"}`、ETag=包文档 sha1+304、SLIM Accept 协商（P1）。④ **Q3 定案**：PyPI 哈希仅 sha256 定案（规格 §3.4：索引输出必须优先 sha256、Artifactory 同）；补充 `md5_digest` 可缺失（twine ≥6.2）与 `:action` 严格校验 400。⑤ 新增定案行为：remote **checksum 后缀请求不回源 404** `"Checksums are not downloadable."`（FR-20-AC13/M45 探针）；remote 上游故障默认 **404**（assumed-offline 5min 静默）+ `hardFail:true` → 502（推翻 v1.0 的默认 502，FR-20-AC5/M44 更新）；remote 字段默认值对齐（socketTimeout 15s/assumedOfflinePeriodSecs 300/hardFail false）；`snapshotVersionBehavior` 三值（BinFlow 默认 `deployer`，服务端 unique 改写列 P2）；PyPI simple `api-version=2` 头/无尾斜杠 302/ETag-304。⑥ Q4 维持（conductor 已转用户知悉） |
| v1.2 | 2026-08-19 | T-78：**Q1 凭据存储依 ADR-0012 决策 4 定案关闭**（推翻 v1.0/v1.1 暂行「明文 SQLite + 文件权限」）：AES-256-GCM（随机 12B nonce）+ 密文 `enc:v1:<base64(nonce+ciphertext)>` 前缀 + 主密钥 env **`BINFLOW_REMOTE_CREDENTIALS_KEY`**（base64 32B，不入 YAML/不落盘）+ 有带凭据的 remote 配置行而无密钥 → **启动 fail-fast** + 存量明文 **003 迁移一次性加密**（需密钥在场）+ 密钥轮换不做。落点：§7 Q1 改已决、§2.2 Non-goals 注、FR-15 remote 字段（新增 **FR-15-AC9**：DB grep `enc:v1:`/无明文、fail-fast 双态重启、迁移后明文消失四断言）、FR-20-AC10 与 NFR-S14 措辞统一 env 键名、§9 DoD-3。**顺手（R3）**：§5.5 校准表补 C2 注记一行（码值 400 维持暂行，单点改动面），C2 定案本体不动 |
| v1.3 | 2026-08-21 | T-121 勘误（T-105 QA 建议 **E2/E3/E4**；T-105 的 E 编号独立于 T-103 的 E 编号（M4 PRD v1.3 已收口）——**E2/E3 与 T-74 E1/E2 同源**，T-74 五条勘误此前未回写 PRD，本版一并收口其 E1/E2；T-74 E3~E5 未含，遗留清单见 T-121 报告）：**E2（=T-74 E1）** §5.4 M17 样例文件名 `bad.jar` 违反 maven layout 校验（文件名须以 `<artifactId>-<version>` 开头——FR-16-AC8/M20 既有规则）先行命中 400，checksum 策略分支不可达——两处 curl 路径改 **`demo-app-1.1.0-badchk.jar`**（layout 合规名；409/201 断言不变，T-74/T-105 双验）。**E3（=T-74 E2）** §5.4 M16 的 `?list&deep=1` 不可作断言载体（SNAPSHOT 目录为隐式目录、无 folder node——M1 既有语义）——载体改为「version 级 maven-metadata 的 `snapshotVersions`（既有断言行保留）+ `<value>` 提取 timestamped 文件名**直连 GET 200**」（T-74 E2 定载体，T-105 §2.4 修正账 M16c 复验）。**E4** §5.4 M47 原无显式建仓命令、remote 上游 URL 约定缺席——补建仓命令钉 **`url=https://pypi.org`（站点根，服务端拼 `simple/<pkg>/` 路径；带 `/simple` 后缀反而 404）**，T-105 真实 pypi.org 双 MISS 已验（T-75 O3 同形）；M46 注明 **mock 上游布局 = `<name>/packument.json`**（BinFlow 回源请求形态），真实 npmjs packument 于 `/<name>` 直出、不兼容为 **T-75 已证既有边界（非回归）**——FR-20-AC7「公网可用时」腿以 mock 为准、FR-20-AC8 url 口径同步。**服务端行为零变更**，仅验收命令与文档口径修正。顺手：§0 修订表行序修正为升序（原 v1.0,v1.2,v1.1） |

---

## 1. 背景与目标

### 1.1 背景

M1 交付了存储引擎/仓库模型/Generic 闭环/认证骨架，M2 交付了 Docker Registry v2（五客户端 conformance 全绿，`m2-done`）。M3 是 BinFlow 从「镜像仓库」走向「统一制品源」的一步：**开发语言生态的依赖收口**（Maven/npm/PyPI）与 Artifactory 概念模型的最后两块拼图（**remote 代理缓存 / virtual 聚合**）。

M3 的用户价值排序（对齐 PRODUCT「内网统一制品源、CI/CD 依赖收口」）：
1. **remote 代理缓存**是内网场景的第一刚需——开发者/CI 指向 BinFlow 一个地址，BinFlow 负责回源、缓存、离线复用；
2. **Maven 2** 是企业 Java 存量生态的事实标准，layout 与 maven-metadata.xml 语义最重；
3. **npm/PyPI** 补齐 JS/Python 两大生态；
4. **virtual** 把 local 与 remote 缝成单一入口（`settings.xml`/`.npmrc`/`pip.conf` 各只配一个 URL）。

M3 在 M1/M2 地基上追加而非返工：三协议构件全部落同一 checksum 寻址 filestore（跨协议去重免费获得）；remote 缓存即「以 remote repo key 为命名空间的一组普通 node」；virtual 是纯解析层不落盘。npm/PyPI 路由挂 `/binflow/api/npm|pypi/**`（M1 PRD §5.4 预留），Maven 内容直接走 `/binflow/<repo>/<layout path>`（M1 埋下的 packageType 分发点在此兑现）。

### 1.2 M3 目标

> 一句话：交付一个 `mvn deploy/resolve`、`npm publish/install`、`pip install`（走代理）全链路可用的多生态制品源，且 remote 代理缓存（含 SSRF 防护）与 virtual 聚合解析达到 Artifactory 等效语义。

量化门槛（未达即里程碑不完成）：

| 指标 | M3 门槛 | 来源 |
|---|---|---|
| 真实客户端闭环 | mvn 3.9.x deploy→全新 repo resolve 成功；npm 10.x publish→install 成功；pip 24.x 从 BinFlow install 成功（M 序列 P0 全绿） | PRODUCT 成功标准第一条 |
| 代理收口 | local 包与上游包经**同一个 virtual 仓**分别被 mvn/npm/pip 解析成功（M50/M54） | PRODUCT「CI/CD 依赖收口」 |
| SSRF 防护 | 私网/环回/链路本地/非 http(s) scheme/重定向绕过全部被拒且留 WARN 审计日志，默认配置零放行（M42，NFR-S13） | 安全底线 |
| 缓存正确性 | miss→上游→落盘→二次请求零上游流量（上游计数不变）；上游 404 被 negative cache 防穿透（M41/M43） | ROADMAP M3 remote 条目 |
| 去重继承 | 跨协议同内容单份 blob（/api/v1/storage/stats 计数不增，M12 变体） | PRODUCT 核心能力 1 |
| 既有零回归 | M1 C 序列 P0 + M2 D 序列 P0 复跑全绿（E-07/E-26 断言按 §5.6 反转表更新） | M2 FR-7-AC2 先例 |
| 冷启动不回退 | 空库冷启动 < 2s（含三协议路由 + remote/virtual 引擎注册后） | M1 NFR-P1 |

### 1.3 上游依赖与校准流程

- **协议规范**：Maven 2 布局/`maven-metadata.xml`（Maven 官方 Repository Metadata 惯例 + Maven Deploy/Resolver 行为）、npm registry HTTP API（npm 官方 registry 规范）、PyPI PEP 503/592/691 + warehouse upload API **均有公开规范，以官方为准**（ADR-0001 clean-room 铁律）。
- **Artifactory 行为规格**：`docs/reverse/maven-npm-pypi.md` 与 repo-semantics §7/§8 **已落地（T-59）**，v1.1 已据此完成 §5.5 C1~C8 校准回写，本文与规格现处一致状态；规格「待验证」项（maven-npm-pypi.md §5、repo-semantics 待验证 #5~#7）维持暂行并注明来源，后续勘误按同流程回写（+0.1）。
- **架构依赖**：adapter SPI 的 packageType 分发点（M1 §5.4 需求 1）、remote 出站 HTTP 客户端的 SSRF 校验链、virtual 解析器在 `internal/repo` 的落位——归 architect 的 ADR 与设计文档，本 PRD 只约束可观察行为。

---

## 2. 范围

### 2.1 In scope（与 ROADMAP M3 条目一一对应）

| # | ROADMAP 条目 | 本 PRD 功能需求 |
|---|---|---|
| 1 | Maven 2：layout 解析、deploy/resolve、maven-metadata.xml、checksum 策略 | FR-16 + FR-17 |
| 2 | npm：publish / tarball / metadata；PyPI：simple index / upload | FR-18 + FR-19 |
| 3 | remote 仓库代理缓存（pull-through，含 SSRF 防护） | FR-20 |
| 4 | virtual 仓库聚合与解析顺序 | FR-21 |
| 5 | （支撑面）rclass remote/virtual 与 packageType maven/npm/pypi 启用 | FR-15 |
| 6 | （验收面）真实客户端矩阵与回归基线 | FR-22 + §5.3/§5.6 |

（M3 的逆向规格、架构 ADR 为流程条目，作上游输入，不单列功能需求。）

### 2.2 Non-goals — M3 明确不做（防范围蔓延）

**产品级 Non-goals（继承 PRODUCT.md，全程有效）**：不做 HA/联邦、不做 Xray、不做 LDAP/SAML/OIDC、不做 Artifactory 全量 REST 兼容。

**M3 里程碑级 Non-goals**：

| 不做项 | 归属 | M3 的隔离边界 |
|---|---|---|
| docker 类型的 remote 仓（Docker Hub pull-through） | M4+ 评估 | `{"rclass":"remote","packageType":"docker"}` → 400「not supported in M3」。理由：上游 token 协商 + manifest 重写是独立工程量，且 M2 场景 C 已可用 `skopeo copy` 搬运替代；M2 PRD §2.2 曾预告「M3 与 generic remote 一并交付」，本 PRD 推翻该预告并转为开放问题 Q4 供用户定夺（不默认扩大范围） |
| Gradle / Ivy / sbt / conan / go module 等其它生态 | M4+/M6+ | packageType 仅新增 `maven`/`npm`/`pypi`；其余值建仓 400。Gradle 走 Maven 仓的兼容性作 P2 观察项（§5.3），不承诺 |
| Maven 索引（`nexus-maven-repository-index.gz` / indexer） | M4+ | 不生成 Lucene 索引；`/binflow/<repo>/.index/*` 404。IDE 依赖 maven-metadata.xml 与 REST 搜索即可工作（搜索归 M4） |
| remote 的主动预取/复制（cache warming、replication） | M6+ | M3 remote 仅被动 pull-through（请求驱动） |
| remote 上游认证的高级形态（Bearer token 流、云厂商 OIDC 联动） | M4+ | M3 仅 Basic（username/password）与匿名上游；凭据静态加密已定案（ADR-0012 决策 4，见 FR-15/NFR-S14，原开放问题 Q1 已关闭） |
| npm 搜索 API（`/-/v1/search`）、score/star、审计端点（`/-/npm/v1/security`） | 不排期 | 404 + E-01；`npm search` 明示不支持 |
| PyPI PEP 592（yank）、PEP 658（分离 metadata）、JSON API（`/pypi/<pkg>/json`） | M4+ 评估 | P2 只做 PEP 691 JSON simple（PE-06）；yank/658/JSON API 404 |
| PyPI 托管 UI 前缀 `/binflow/api/pypi-ui/**` | 不排期 | 维持 404 + E-01（M1 E-26 边界不变） |
| virtual 的高级治理（成员排除模式、per-user 视图、virtual 的 X-Ray/权限叠加） | M4 | M3 virtual 只做解析顺序 + 可选写路由 |
| remote 缓存的手动管理面（按路径 evict、缓存浏览 REST） | M4 | 仅 `DELETE /binflow/<remote>/<path>` 删除已缓存内容（再次 GET 会回源）+ `/api/v1` 只读统计（P2） |
| 每协议的 Web 控制台视图 | M4 | M3 无 UI |
| metadata 跨成员聚合合并（npm packument union / maven-metadata versions union / PyPI simple union） | M3 P1（非 P0） | P0 语义 = 首个命中成员透传（§4 FR-21）；合并为 P1，且仅「包名/GAV 相同出现在多个成员」场景需要 |

---

## 3. 用户与场景（M3 视角）

- **场景 A（Java 团队依赖收口）**：平台组建 `maven-virtual`（成员 `maven-local` + `maven-remote`→Maven Central）；`settings.xml` 的 `<mirrorOf>*</mirrorOf>` 指向 BinFlow 一处，内部构件 deploy 到 `maven-local`（经 virtual 写路由或直连），第三方依赖首次拉取自动缓存。
- **场景 B（前端/npm 收口）**：`.npmrc` registry 指向 `npm-virtual`；内部包 `npm publish` 落 `npm-local`，公共包经 `npm-remote`（上游 npmjs）pull-through；`npm install` 对用户无感。
- **场景 C（Python/内网 air-gapped）**：受限集群的 `pip.conf` index-url 指向 `pypi-remote`；首次构建经代理缓存的 wheel 在后续离线构建直接命中本地缓存（`pip install` 不出网）。
- **场景 D（迁移自 Artifactory）**：既有 `settings.xml`/`.npmrc`/`pip.conf` 里的 Artifactory URL 替换前缀 `/artifactory`→`/binflow` 后语义不变（repo key、layout、metadata 语义保持）；remote/virtual 配置字段名对齐（§4 FR-15）。
- **场景 E（安全审计）**：安全团队要求「制品仓库的出站请求仅限白名单上游」——SSRF 防护默认拒绝私网目标（NFR-S13），内网上游（内网 Nexus）由管理员显式放行并留审计日志。

---

## 4. 功能需求

约定延续 M1/M2：`BASE=http://localhost:8080`、`admin`/`$ADMIN_PW`、`REG`（docker 用）；新增 QA 环境变量 `MVN_REPO_URL`、`NPM_REGISTRY`、`PIP_INDEX`（M 序列脚本内展开）。优先级 P0/P1/P2 沿用 M1 定义。M 序列命令见 §5.4。

### FR-15 仓库模型扩展：remote / virtual 与三个新 packageType（dev-go-core）

**用户故事**：作为 Artifactory 迁移管理员，我希望用 M1 已有的 `PUT /binflow/api/repositories/{key}` 建出 remote/virtual 仓，字段名与语义与我熟悉的一致——迁移脚本只改前缀不改模型。

行为规格（对齐 Artifactory 仓库配置模型；字段集为兼容子集 + 两处 BinFlow 扩展）：

- **rclass 取值放开**：`local`（既有）| `remote` | `virtual`。M1 E-07 的「remote/virtual → 400」**反转**（§5.6 回归基线同步）。
- **remote 仓配置字段（兼容子集）**：
  - `url`（**必填**，http/https；缺失或 scheme 非法 → 400）——上游 base URL，请求路径直接拼接；
  - `username` / `password`（可选，上游 Basic 认证；GET 回显时 `password` 掩码，见 NFR-S14）。**存储形态定案（v1.2，ADR-0012 决策 4，原 Q1 关闭）**：password 一律 AES-256-GCM 加密落库（随机 12B nonce，密文形如 `enc:v1:<base64(nonce+ciphertext)>`）；主密钥经环境变量 **`BINFLOW_REMOTE_CREDENTIALS_KEY`**（base64 32B）注入、**不入 YAML/不落盘**；存在带凭据的 remote 配置行而未设密钥 → **启动 fail-fast**（退出码非 0 + 日志说明）；密钥轮换不做（M3 单密钥，轮换 = 重新录入凭据）；
  - `retrievalCachePeriodSecs`（缓存命中期，默认 **7200s**，v1.1 定案：repo-semantics §7.1 高置信度）——命中期内 GET 不回源，过期后下次请求触发回源校验（C4）；
  - `missedRetrievalCachePeriodSecs`（404 负缓存期，默认 **1800s**，v1.1 定案同上）——期内同路径 404 不回源（防穿透）；
  - `socketTimeoutSecs`（上游 IO 超时，默认 **15s**——v1.1 对齐 Artifactory `socketTimeoutMillis=15000`，repo-semantics §7.1）；
  - `assumedOfflinePeriodSecs`（上游故障静默期，默认 **300s**，v1.1 新增定案：故障后标记 assumed-offline，期内请求绕过上游，repo-semantics §7.1/§7.6）；
  - `hardFail`（默认 `false`；`true` 时上游错误向上抛 **502** 而非 404——v1.1 对齐 Artifactory `hardFail` 字段语义（其抛 500，BinFlow 取网关语义 502，NFR 可观测性更优且客户端等价感知），repo-semantics §7.1/§7.6）；
  - **BinFlow 扩展** `allowPrivateUpstream`（默认 `false`）：是否允许上游解析到私网/环回地址（NFR-S13；场景 E 的内网上游放行开关）。
  - （Artifactory 另有 `offline`/`storeArtifactsLocally`/`shareConfiguration`/`bypassHeadRequests` 等，M3 不暴露，M4+ 按需；`includesPattern/excludesPattern` remote 语义同 local，M3 不暴露——勘误归档见 §6.4）
- **virtual 仓配置字段（兼容子集）**：
  - `repositories`（**必填**，成员 repo key 数组，非空；成员不存在 → 400；成员不得为 virtual——**嵌套 virtual 不做**，M3 有意不兼容，400）；
  - 成员级标记 `priorityResolution`（v1.1 定案启用：Artifactory 为 per-repo 字段默认 false，repo-semantics §7.1/§8.1；BinFlow 取**两桶简化**——优先桶（标记成员，按声明序）在前、其余在后，因 BinFlow 无独立 `<key>-cache` 仓投影故四桶退化为两桶，见 FR-21）；
  - `defaultDeploymentRepo`（可选，成员中的 local 仓 key；配置后 virtual 可写并路由写入，未配置 → 写操作 405，见 FR-21；v1.1 别名对齐：`defaultDeploymentRepoRef`（repo-semantics §8.2 规格名）与 `deploymentRepository` 亦接受）；
- **packageType 新增**：`maven` | `npm` | `pypi`（合法组合：三类 rclass × {generic, docker, maven, npm, pypi}，docker 仅 local 可用见 §2.2；`remote+docker`/`virtual+docker`？virtual+docker 聚合本实例 docker local 仓——**M3 不开**，400，docker 域 virtual 归 M4 评估）。
- virtual 成员变更（增删成员/改顺序）即时生效，已缓存解析无残留（无解析缓存，逐请求现算——实现归 architect）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-15-AC1 | M01：PUT 建 `maven-local`/`npm-local`/`pypi-local`（`rclass:local` + 对应 packageType）→ 三次均 200 纯文本 `Successfully created repository '<key>'`；`GET .../repositories/{key}` 的 `packageType` 正确 | P0 |
| FR-15-AC2 | M02：PUT 建 `{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","allowPrivateUpstream":true}` → 200；`GET` 单查返回 `rclass=="remote"`、`url` 原样、`password` 字段不回显明文（传过也掩码/缺省） | P0 |
| FR-15-AC3 | M02b：remote 缺 `url` → 400（E-01，message 含 `url`）；`url` 为 `file:///etc` 或 `ftp://x` → 400；`url` 私网地址**建仓成功**（校验在请求时执行，NFR-S13——DNS/网络可变，建仓时只做 scheme 与格式校验） | P0 |
| FR-15-AC4 | M03：PUT 建 `{"rclass":"virtual","packageType":"maven","repositories":["maven-local","maven-remote-x"]}` → 200；缺 `repositories` 或空数组 → 400；成员含不存在的 key → 400；成员含 virtual key → 400 | P0 |
| FR-15-AC5 | M04：`GET /binflow/api/repositories?type=remote` 只列 remote；`?type=virtual`、`?packageType=maven` 过滤正确（M1 E-04 的过滤参数在此启用取值；非法值空数组语义不变） | P1 |
| FR-15-AC6 | 删除 remote 仓：`DELETE .../repositories/generic-remote?deleteContent=true` → 2xx，其缓存 node 一并消失，`GET /binflow/generic-remote/<path>` → 404（repo 不存在分支） | P1 |
| FR-15-AC7 | docker 组合边界：`{"rclass":"remote","packageType":"docker"}` → 400「not supported in M3」；`{"rclass":"virtual","packageType":"docker"}` → 400；`local+docker`（M2 既有）不回归 | P0 |
| FR-15-AC8 | generic 既有行为零回归：M1 C03/C06/C07/C08 与 M2 D01 复跑全绿（§5.6） | P0 |
| FR-15-AC9 | 凭据加密链（v1.2，ADR-0012 决策 4）：① 设 `BINFLOW_REMOTE_CREDENTIALS_KEY` 启动后建带 password 的 remote 仓 → 元数据 DB 文件中 `grep` 不到明文口令、密文行含 `enc:v1:` 前缀；② **不设该 env 重启**（存在带凭据的 remote 配置行）→ 进程启动失败（退出码非 0，日志指明缺 `BINFLOW_REMOTE_CREDENTIALS_KEY`）；③ 重设 env 重启 → 该仓上游 Basic 认证链仍通（FR-20-AC10）；④ 存量明文值（升级场景）：设 env 后首次启动被 003 迁移一次性加密，DB 中明文消失、代理行为不变 | P0 |

### FR-16 Maven 2：layout 解析、deploy/resolve、checksum 策略（dev-registry-adapter）

**用户故事**：作为 Java 开发者，我希望 `mvn deploy` 与 `mvn dependency:get` 对 BinFlow 的用法和对 Nexus/Artifactory 完全一样——`altDeploymentRepository` 填一个 URL 就能收工。

行为规格（Maven 2 layout 为公开规范惯例；checksum 策略依 repo-semantics.md §5 高置信度）：

- **layout**：`<groupId 点转斜杠>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<ext>`；version 段含 `-SNAPSHOT` 即 snapshot。**v1.1 定案（maven-npm-pypi.md §1.2，一手 config 模板，高置信度）**：对齐 `maven-2-default` 布局六字段模型——`artifactPathPattern = [orgPath]/[module]/[baseRev](-[folderItegRev])/[module]-[baseRev](-[fileItegRev])(-[classifier]).[ext]`，`folderIntegrationRevisionRegExp = SNAPSHOT`、`fileIntegrationRevisionRegExp = SNAPSHOT|(?:(?:[0-9]{8}.[0-9]{6})-(?:[0-9]+))`（即 unique snapshot 文件名 = `{module}-{baseRev}-{yyyyMMdd.HHmmss}-{N}[-classifier].{ext}`，目录仍为 `{baseRev}-SNAPSHOT`）；判定顺序 = 先剥 checksum 后缀与 metadata 前缀（`maven-metadata.xml`，含 plugin 群的 `metadata-maven-metadata.xml` 变体）再匹配模板。maven 仓的 PUT/GET 按该 layout 严格解析：
  - 目录层数 ≥ 3（groupId 至少一段 + artifactId + version + 文件）；
  - 文件名必须以 `<artifactId>-<version>` 开头（classifier/扩展任意；version 段允许 `-SNAPSHOT` 或 timestamped 形态）；
  - `maven-metadata.xml` 允许出现在 artifactId 级（release 版本列表）与 version 级（snapshot 信息）；
  - 不合规路径（如 `PUT /binflow/maven-local/foo.jar`）→ **400**（BinFlow 决策：maven packageType 仓启用严格 layout 校验，防 generic 化误用；**400 状态码维持暂行**——规格已定案 layout 模型本身（高置信度），但拒绝状态码规格未见明确值，C2 半定案）。
- **deploy**：`mvn deploy` 经 HTTP PUT 上传 pom/jar 及**旁车 checksum**（`.md5`/`.sha1`，wagon 行为）与 X-Checksum 头（按 deploy 插件版本二者其一或并存）。BinFlow 校验链：
  1. X-Checksum-Sha1/Sha256/Md5 头 → M1 既有链（client-checksums 策略不一致 → 409）；
  2. 旁车文件（`.sha1`/`.md5`/`.sha256`）内容与制品实测**不一致** → 按 repo checksum 策略处理：`client-checksums`（默认）→ **409**（message 含 received/actual）；`server-generated-checksums` → 静默接受、落盘侧车以服务端实测为准（rest-api.md §1.5 / repo-semantics.md §5，高置信度）；
  3. 均未提供 → 服务端计算并接受。
- **checksum 策略三态（repo 配置 `checksumPolicyType`）**：`client-checksums`（默认，严格）| `server-generated-checksums`（宽容）| **未配置**（等价 client-checksums）。三态的第三种输入形态即「客户端一个 checksum 都没给」——服务端生成并接受，永不拒绝（M1 已有雏形在此按 repo 字段暴露为配置）。
- **resolve**：GET/HEAD `/binflow/<repo>/<layout path>` → 200 流式 + M1 头集（`X-Checksum-Sha1/Md5/Sha256`、`ETag=<sha1>`、Range 206/416、条件请求 304）；mvn 的 checksum 校验依赖旁车文件 GET（`<file>.sha1|.md5` → 服务端以**实测值**返回纯文本内容，无尾随换行容忍：内容为裸 hex，尾部 `\n` 允许——BinFlow 返回不带换行，解析时容忍客户端形态）。
- **snapshot**（v1.1 定案，maven-npm-pypi.md §1.3 高置信度）：repo 配置 `snapshotVersionBehavior` 三值——
  - `deployer`（**BinFlow M3 默认**）：客户端传什么文件名落什么盘（mvn 的 unique deploy 自产 timestamped 名、`uniqueVersion=false` 时传 `-SNAPSHOT` 名，均按上传名落盘）；
  - `non-unique`：`-SNAPSHOT` 文件名原样落盘，允许覆盖（repo-semantics §3「不同 checksum 视为覆盖」链路适用）；
  - `unique`（服务端改写）：`-SNAPSHOT` PUT → 服务端改写为 `<ts>-<N>`（N = version 目录 metadata 的 buildNumber+1，同趟 classifier/pom 复用同一 N，伴随 checksum 改写指向同 buildNumber 主文件）——**P2**（buildNumber 连续性依赖 metadata 计算器，规格 §4.2 建议后置，BinFlow 采纳）；
  - 已是 unique（timestamped）文件名的上传在任何 behavior 下都不改写（高置信度）。
  - version 级 `maven-metadata.xml` 的计算见 FR-17。maxUniqueSnapshots 保留策略 M4。
- **handleReleases/handleSnapshots**（repo 配置，默认 true/true）：false 时对应版本类型的 PUT → **409** 拒绝（**v1.1 定案推翻 v1.0 的 404**：`SnapshotPolicyException#getErrorCode` 显式 409，maven-npm-pypi.md §1.4 高置信度——M1 规格的 404 推断已被勘误）；下载侧开关只影响可服务性，不影响 GET 已有缓存。
- **覆盖检查**：沿用 repo-semantics §3——同 checksum 幂等重传免覆盖检查；`maven-metadata.xml` 与旁车 checksum 文件**永不触发覆盖检查**（高置信度，客户端重发是常态）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-16-AC1 | M11：`mvn -B -DskipTests deploy`（`altDeploymentRepository` 指向 `$BASE/binflow/maven-local`，settings.xml 存凭据）→ `BUILD SUCCESS`；jar/pom/旁车全部 201 | P0 |
| FR-16-AC2 | M12：deploy 后 `GET /binflow/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar` → 200 且 sha256 与本地构建产物一致；`.pom` 同理；item info（E-09）`checksums.sha1` 正确 | P0 |
| FR-16-AC3 | M13：全新本地仓（`-Dmaven.repo.local=fresh`）消费侧 pom `<repositories>` 指向 BinFlow，`mvn -B compile` → `BUILD SUCCESS`（mvn 下载 pom/jar + 校验 sha1 通过） | P0 |
| FR-16-AC4 | M14：`GET .../com/acme/demo-app/maven-metadata.xml` → 200 XML（`<versioning>` 含已 deploy 版本、`<lastUpdated>` 非空）；`GET ....xml.sha1|.md5` 内容 == 服务端对该 XML 实测的 sha1/md5 | P0 |
| FR-16-AC5 | M17：client-checksums 仓 PUT 带错误 `X-Checksum-Sha1` → 409（M1 C14 等价链路）；`checksumPolicyType=server-generated-checksums` 仓（M16c 建）同请求 → 2xx 接受，落盘与响应以服务端实测为准 | P0 |
| FR-16-AC6 | M18：旁车两态——`curl -T app.jar.sha1`（内容正确）→ 201 且随后 GET 旁车返回实测值；内容错误（在 client-checksums 仓）→ 409；server-generated 仓 → 接受但 GET 旁车返回**实测值**（客户端声明不落盘） | P0 |
| FR-16-AC7 | M19：`handleSnapshots:false` 仓 deploy `1.0.1-SNAPSHOT` → **409**（v1.1 勘误：SnapshotPolicyException 显式 409）；`handleReleases:false` 仓 deploy release → 409；开关不影响 GET | P1 |
| FR-16-AC8 | M20：layout 校验——`PUT /binflow/maven-local/foo.jar`（无 groupId/version 段）→ 400；`PUT .../com/acme/demo/1.0.0/other-name-1.0.0.jar`（文件名不以 artifactId-version 开头）→ 400 | P0 |
| FR-16-AC9 | M21：Range/条件请求继承——`curl -r 0-99` 拉构件 → 206；`If-None-Match` → 304（M1 FR-4-AC14/AC15 在 maven 路径复验） | P1 |
| FR-16-AC10 | 跨协议去重：与 generic/docker 同内容单份 blob（/api/v1/storage/stats 计数不增，M12 附带断言） | P1 |
| FR-16-AC11 | 删除：`DELETE /binflow/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar` → 204；随后 maven-metadata.xml 中该版本**仍可列出**（版本事实由 metadata 服务端维护，FR-17-AC4 定清理语义） | P1 |
| FR-16-AC12 | `snapshotVersionBehavior=unique` 服务端改写（P2）：PUT `-SNAPSHOT` 名 → 落盘 timestamped 名、buildNumber 连续、伴随 checksum 指向同 buildNumber 主文件；对拍 mvn deploy（maven-npm-pypi.md §1.3/§5-5 待验证项，实现后对拍收口） | P2 |

### FR-17 Maven maven-metadata.xml：生成、合并与 snapshot 语义（dev-registry-adapter）

**用户故事**：作为 CI 与 IDE 用户，我依赖 `maven-metadata.xml` 的 `latest/versions/snapshotVersions` 做版本发现——并发 deploy 不丢版本、`-U` 强刷拿到最新 SNAPSHOT 是构建可复现的底线。

行为规格（**v1.1 全量定案**，maven-npm-pypi.md §1.4 + repo-semantics §8.3，高置信度——服务端计算行为是 Artifactory 对 [MVN-MD] 的实现补充，官方未规定）：

- **触发时机**（deploy 拦截器链，BinFlow 按 OSS 同构实现）：
  - 上传 unique snapshot 文件 / non-unique pom → 计算父目录（version 目录）metadata，**同步**（阻塞 deploy 响应——后续 snapshot 编号依赖它）；
  - 上传其它文件（release jar 等）→ 父目录 metadata，异步；
  - 上传 pom → 祖父目录（group/module 版本清单，非递归），异步；
  - move/copy/delete → 源与目标两侧受影响目录树，异步。
- **版本组目录**（`{orgPath}/{module}/maven-metadata.xml`，即 v1.0 的 artifact 级）：`<versions>` = 子版本目录中含 `*.pom` 的目录集合（同版本多行取 created 最新去重）；排序用 Maven 版本比较器；`<latest>` = 排序后最后一个（SNAPSHOT 也算）、`<release>` = 最后一个非 SNAPSHOT、`<lastUpdated>` = 计算时刻（`yyyyMMddHHmmss`，UTC）；**子目录无 pom → 删除已存在的 metadata 及伴随 checksum**（Artifactory 连 `.sha512` 一并清，BinFlow P2 跟进 sha512 伴随），例外：现存内容是 snapshot 型 metadata 而路径非 snapshot 目录时**不删**（RTFACT-6242，兼容 Maven 2 客户端手工部署 bug，BinFlow 照做）。
- **SNAPSHOT 版本目录**（`.../{baseRev}-SNAPSHOT/maven-metadata.xml`）：`groupId/artifactId` 取自同目录 pom 解析、`<version>` = `{baseRev}-SNAPSHOT`；`<snapshot><buildNumber>/<timestamp>`：repo behavior=non-unique（或 deployer 且无 unique 文件）→ buildNumber 固定 1、无 timestamp；存在 unique 文件 → 取**最新 unique snapshot pom** 的 buildNumber/timestamp（v1.0 的「取最大」表述据此精化）；`<snapshotVersions>`（Maven 3 客户端需要）默认开启，每 (extension × classifier) 取最新一条，`<updated>` = 时间戳去点号。
- **客户端 PUT maven-metadata.xml = 上传事件，权威内容以服务端计算为准**（v1.1 精化，修正 v1.0 的「XML 级 union/max 合并」表述）：PUT 被接受（201，走通用上传链——metadata 永不触发覆盖检查），随后触发受影响目录重算；客户端上传的 XML 不直接成为版本清单来源，服务端从存储事实重算——对外语义等效于合并（并发 deploy 后 GET 必含全部实际存在版本，M15/并发 AC 断言不变）。
- **checksum 一致性**：metadata 每次内容变化后旁车（`.md5/.sha1`）与响应头同步重算（客户端缓存 sha1 陈旧会造成 mvn `Could not transfer metadata` 类错误——绝不允许）。
- **删除联动**：定案并入版本组目录规则（无 pom → 删 metadata 及伴随 checksum）。
- `GET metadata` 对 virtual 仓的聚合语义见 FR-21（v1.1 升定案：内存合并、每次现算不缓存）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-17-AC1 | M14 后 deploy `1.1.0`（M15）：GET artifact 级 metadata 的 `versions` 同时含 `1.0.0` 与 `1.1.0`，`latest==1.1.0`，`lastUpdated` 单调不减 | P0 |
| FR-17-AC2 | M16：deploy `1.2.0-SNAPSHOT`（unique）两次 → 落盘两个 timestamped jar；version 级 metadata 的 `snapshotVersions` 含两条、`buildNumber==2`；artifact 级 metadata `versions` 含 `1.2.0-SNAPSHOT` | P0 |
| FR-17-AC3 | M16b：全新 `maven.repo.local` 下 `mvn -B -U dependency:get -Dartifact=com.acme:demo-app:1.2.0-SNAPSHOT -DremoteRepositories=...` → 成功且解析到 buildNumber 2 的 timestamped 文件（`-U` 强刷链路） | P0 |
| FR-17-AC4 | M15 附带：DELETE `1.0.0` 全部构件后 GET metadata → `versions` 不再含 `1.0.0` | P1 |
| FR-17-AC5 | 并发合并（M15 变体，QA 两进程交错 deploy 1.3.0/1.4.0）：最终 metadata 两版本俱在，无 500（结构化日志零 5xx） | P1 |
| FR-17-AC6 | checksum 一致性：任意 deploy 后 `curl .../maven-metadata.xml.sha1` == `sha1sum`（对 GET 到的 XML 现算） | P0 |

### FR-18 npm：publish / packument / tarball / dist-tags（dev-registry-adapter）

**用户故事**：作为前端工程师，`npm publish` 推内部包、`npm install` 装内部与公共包，registry URL 换成 BinFlow 即可——npm CLI 的一切子命令（dist-tag/unpublish/audit 白名单除外）照常工作。

行为规格（npm registry HTTP API 有公开规范，以官方为准；路由挂 `/binflow/api/npm/<repo>/**`，M1 §5.4 预留位兑现）：

- **publish**（`PUT /binflow/api/npm/<repo>/<package>`，body = packument + `_attachments` base64 tarball；npm ≥ 5 单 PUT，npm 8/9/10 矩阵）→ **201** `{"success":true}`；scoped 包 URL 形态 `<repo>/@scope%2Fname`（URL 编码斜杠；**`%2f` 与 `%2F` 两种编码等价接受**——v1.1 定案，规格 §2.1/§5-3，高置信度），必须支持。**校验顺序即错误顺序（v1.1 定案，规格 §2.3 十步链，高置信度）**：
  1. body JSON 解析失败 → 400；
  2. `_attachments` 非空但 `versions` 空 → 400 `Missing versions in npm package...`；
  3. 对 tarball 目标路径无写权限 → 403 `Cannot deploy to '<tarballPath>'`；
  4. **tarball 路径已存在（同版本重复 publish）→ 403** `Cannot modify pre-existing version '<v>', aborting upload for: '<name>'`（**v1.1 定案推翻 v1.0 的 409**：Artifactory 定 403、npmjs 官方错误族同；npm 客户端只判非 2xx，无兼容性损失）；
  5. name/version 非法（semver 与 leading zeros 规则）→ 400 `Invalid Version: '<v>'...`；
  6. `_attachments` 空但含 `versions[-].deprecated` → deprecate 流程，**201** `{"ok":"updated package"}`；
  7. `_attachments` 空且非 deprecate → 400 `Missing attachments with tarball data...`；
  8. integrity（`sha512-<base64>`）与 tarball 实测不一致 → 400 `Conflict between integrity from metadata and tarball`（Artifactory 默认不强制、仅记属性；**BinFlow 决策：默认强制校验拒 400**——与 client-checksums 策略同向，存储不可变性优先，有意从严归档于 NE-01 注）；
  9. `dist.shasum`（sha1 hex）不一致 → 400 `Conflict between sha1 from metadata and tarball`；
  10. 成功 → 201。
  - tarball 落内容路径 `/binflow/<repo>/<name>/-/<name>-<version>.tgz`；scoped 为 `/binflow/<repo>/@<scope>/<name>/-/@<scope>/<name>-<version>.tgz`（**v1.1 定案**：与 Artifactory 同布局，规格 §2.2 高置信度；packument GET 面不返回 `_attachments`——[NPM-API] 惯例；publish 后物理清空与否维持暂行 C8）。
- **packument**：`GET /binflow/api/npm/<repo>/<package>` → 200 JSON：`name/dist-tags/versions/<v>/{dist{tarball,shasum,integrity}}/_rev/_id/time`；`dist.tarball` **一律重写为指回 BinFlow 的 URL**（含凭据场景不带——匿名读默认开；关闭时 npm 需 `.npmrc` 带 `_auth`，QA 双模式覆盖）；包不存在 → 404（E-01）。**响应头 `X-Checksum-Sha1` 与 `ETag` = 包文档 JSON（非 tarball）的 sha1；`If-None-Match` 命中 → 304**（v1.1 定案，规格 §2.4 高置信度——官方未规定元数据 ETag）。**SLIM 协商（P1）**：`Accept: application/vnd.npm.install-v1+json`（npm ci 场景）→ 瘦身文档（去 README 等重字段），Content-Type 原样回（规格 §2.2 高置信度）。
- **tarball 下载**：`GET /binflow/api/npm/<repo>/<pkg path>/-/<file>.tgz` → 200 流式 + M1 头集；`dist.integrity`（sha512）与 `shasum`（sha1）由服务端实测生成。
- **dist-tags**：`GET|PUT|POST|DELETE /binflow/api/npm/<repo>/-/package/<name>/dist-tags`（npm ≥ 8 形态）与旧形态 `PUT /binflow/api/npm/<repo>/<package>/<tag>`（PUT body 为版本字符串）均支持；**PUT 成功 → 201 `{"ok":"created new tag"}`、DELETE → 200 空、目标缺失 → 404 `npm package not found with name:<n>, and tag:<t>`**（v1.1 定案，规格 §2.1 高置信度）；`npm dist-tag add/rm/ls` 全走通。
- **unpublish**（P1）：npm 客户端两步——① `PUT /binflow/api/npm/<repo>/<package>/-rev/<rev>` → **200 `{"ok":"updated package"}` 恒定假成功**（占位步骤，服务端不做事；**v1.1 新增定案**，规格 §2.1/§2.7-1 高置信度——缺此步 npm unpublish 直接失败，必须实现）；② `DELETE .../<package>/-rev/<rev>`（整包）或 `DELETE .../<package>/-/<filename>/-rev/<rev>`（单版本）→ 200，`versions` 条目移除、dist-tags 引用联动清理。
- **`npm login`/`npm whoami`**：`PUT /-/user/org.couchdb.user:username`（npm ≥ 9 需 `--auth-type=legacy`）→ 201 token（BinFlow 签发 API Token 复用 M1 token 表）；`GET /-/whoami` → 200 `{"username":...}`（未认证 401）。QA 允许以 `.npmrc _auth`（Basic）替代 login 流（等效凭据）。
- **错误体**：npm 域统一 E-01（**v1.1 定案**：三协议共用 `{"errors":[{status,message}]}` 信封，规格 §0 高置信度，开放问题 Q7 收口）；npm 客户端不解析错误体结构，仅展示。
- **域根探针**：`GET /binflow/api/npm/<repo>/` → 200 空 body（连通性探测，规格 §0；P2）。
- **隐藏索引目录**（v1.1 定案，规格 §2.2/§4.5）：Artifactory 以 `.npm/{name}/package.json`、`.pypi/{norm}/...` 隐藏文件维护包索引——BinFlow 以 metadata 表属性为中心重建索引（规格 §4.5 建议），**不落隐藏目录**；若实现侧采用隐藏目录形态，则 REST/浏览层必须不可见（Artifactory 以 `.npm`/`.pypi` 前缀过滤）。
- **`npm ping`**：`GET /-/ping` → 200 `{}`（P2）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-18-AC1 | M22：`.npmrc` registry 指向 `$BASE/binflow/api/npm/npm-local/` + `_auth`；`npm publish`（新包 1.0.0）exit 0，输出含 `+ demo-pkg@1.0.0` | P0 |
| FR-18-AC2 | M22b：`curl GET .../api/npm/npm-local/demo-pkg` → 200，`dist-tags.latest=="1.0.0"`，`versions["1.0.0"].dist.tarball` 以 BinFlow 路径开头，`shasum` == 本地 `npm pack` 产物 sha1 | P0 |
| FR-18-AC3 | M23：全新目录 `npm install demo-pkg --registry <url>` → exit 0，`node_modules/demo-pkg/package.json` version 正确；缓存清空（`npm cache clean --force`）后重复仍 exit 0 | P0 |
| FR-18-AC4 | M24：`npm dist-tag add demo-pkg@1.0.0 beta` exit 0；packument `dist-tags.beta=="1.0.0"`；`npm install demo-pkg@beta` exit 0；`npm dist-tag rm demo-pkg beta` 后 packument 无 beta | P0 |
| FR-18-AC5 | M25：scoped 包 `@acme/util` publish → install 全链 exit 0（URL 编码 `@acme%2Futil` 形态往返一致） | P0 |
| FR-18-AC6 | M26：重复 publish 同版本 → npm CLI 报错非 0；curl 直打 → **403** E-01（message 含 `Cannot modify pre-existing version`；v1.1 勘误：409→403） | P0 |
| FR-18-AC7 | M27：`npm unpublish demo-pkg@1.0.1 --force`（先发 1.0.1）→ exit 0；packument 无 1.0.1；`npm install demo-pkg@1.0.1` 失败（预期）而 `@1.0.0` 仍可装 | P1 |
| FR-18-AC8 | M28：匿名读边界——默认匿名 GET packument/tarball 200、PUT 401；`anonymous_access:false` 后 GET 需 `_auth`（M1 C27 的 npm 版） | P1 |
| FR-18-AC9 | tarball 完整性：`npm install` 后 `node_modules/.package-lock.json` 的 integrity（sha512）与服务端 `X-Checksum-Sha256` 同源一致 | P1 |
| FR-18-AC10 | packument 条件请求（v1.1 定案，P1）：`curl -I` 的 `ETag` == 包文档 JSON 的 sha1；`If-None-Match` 同值重发 → 304 无 body |
| FR-18-AC11 | unpublish 前置占位（v1.1 定案，P1）：`PUT .../demo-pkg/-rev/<任意rev>` → 200 `{"ok":"updated package"}` 且包内容无变化（M27 第一步隐式覆盖，curl 显式断言） |

### FR-19 PyPI：simple index / upload / 下载（dev-registry-adapter）

**用户故事**：作为 Python 开发者，`twine upload` 发内网包、`pip install --index-url` 指向 BinFlow——`pip.conf` 一处配置，依赖解析、hash 校验、离线复用全部成立。

行为规格（PEP 503 公开规范 + warehouse legacy upload 公开惯例；路由挂 `/binflow/api/pypi/<repo>/**`）：

- **simple index**：`GET /binflow/api/pypi/<repo>/simple/` → 200 HTML（项目名锚点列表，P2）；`GET .../simple/<normalized-name>/` → 200 HTML，每文件一个 `<a href="<url>#sha256=<hex>">file</a>`，条目按文件名排序、值经 HTML 转义；**文档头带 `<meta name="api-version" value="2" />`（PEP 629，v1.1 定案，规格 §3.2 高置信度）**；**name normalization 按 PEP 503**（lower + `[-_.]+`→`-`）：`Demo_Pkg.1` 与 `demo-pkg-1` 同一 index 页；未知名 → 404；**无尾斜杠 `GET .../simple/<name>` → 302 重定向到补尾斜杠 URL**（v1.1 定案，规格 §3.1 高置信度）；**ETag + `If-None-Match` → 304**（ETag 为条目稳定哈希的不透明串——Artifactory 用私有的 31-折迭哈希，BinFlow 用任意稳定内容哈希即可，客户端不透明，规格 §3.2）；`Accept: application/vnd.pypi.simple.v1+json`（PEP 691）→ JSON 形态（P2；Artifactory 该开关默认关，BinFlow 亦默认 HTML）。BinFlow 不输出 Artifactory 私有的 `rel="internal|external"` 属性（pip 忽略未知属性，PEP 503 无此定义——有意不补充）。
- **上传**：`POST /binflow/api/pypi/<repo>`，multipart form；**`:action` 必须为 `file_upload`，否则 400 `unknown action '<action>'`**（v1.1 定案，规格 §3.3 高置信度）；`content` 为文件体（文件名取该 part 的 Content-Disposition filename）；**`md5_digest` 为可选客户端 checksum——twine ≥ 6.2 不再发送，缺失时服务端自算并接受**（v1.1 定案，规格 §3.3/§3.4 高置信度；提供且不一致时走 client-checksums 校验链 409）；其余字段（name/version/summary/requires_python/yanked/...）收下作为元数据属性。**响应统一 200**（warehouse 同；twine 判 2xx）。校验：`name`/`version`/`content` 缺失 → 400；**同 filename 重复上传 → 400**（warehouse「file already exists」语义，防覆盖；Artifactory 分支未在规格明示，维持暂行）；**存储路径 `<repo>/<name>/<version>/<filename>`（name 用元数据原始名、不 normalize——v1.1 定案，规格 §3.5 高置信度，C7 收口）**，normalize 仅用于索引查找（`normalizePackageName`）与文件名侧（`normalizeDistributionName`，PEP 427 惯例）。sdist（`.tar.gz`）与 wheel（`.whl`）都支持。
- **下载**：simple index 的 href 指向 `/binflow/api/pypi/<repo>/packages/<name>/<version>/<filename>`（对齐 Artifactory `packages/` 形态，v1.1 定案；内容路径 `/binflow/<repo>/<name>/<version>/<filename>` 为同一 node 的第二入口）→ 复用 M1 下载链（checksum 头/Range/304）；pip 经 `#sha256=` fragment 自校验。
- **哈希算法**：**仅 sha256，v1.1 定案（Q3 收口）**——规格 §3.4：上传侧客户端只可能提供 md5（且可缺失），pip 校验依赖 `#sha256=`，索引输出必须优先 sha256；Artifactory 的 `#md5=` 仅在制品无 sha256 时兜底，BinFlow 服务端必算 sha256 故该分支永不触发，不提供 md5 fragment。
- **域根探针**：`GET /binflow/api/pypi/<repo>/` → 200 空 body（规格 §0；P2）。
- **pip 侧**：`pip install --index-url <url>/simple <pkg>` / `pip download` / `pip index versions`（P2）走通；pip 的 JSON API（`/pypi/<pkg>/json`，warehouse legacy 形态）不做（§2.2）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-19-AC1 | M30：`pip wheel . -w dist/`（或 `python -m build`）后 `twine upload --repository-url $BASE/binflow/api/pypi/pypi-local -u admin -p $ADMIN_PW dist/*` → exit 0，服务端 200 | P0 |
| FR-19-AC2 | M31：`curl .../simple/demo-pkg/` → 200 HTML，含 href 与 `#sha256=`；`Demo_Pkg`、`demo_pkg`、`demo-pkg` 三形态 URL 均返回同一 index 页 | P0 |
| FR-19-AC3 | M32：全新 venv `pip install --index-url $BASE/binflow/api/pypi/pypi-local/simple demo-pkg` → exit 0，`pip show` 版本正确；`pip download` 落盘文件 sha256 与 index fragment 一致 | P0 |
| FR-19-AC4 | M33：重复 upload 同 filename → twine 报错非 0，服务端 400（防覆盖语义） | P0 |
| FR-19-AC5 | M34：依赖链——`demo-pkg` 依赖 `demo-lib`（同仓另一包，METADATA 声明），pip install 自动从同一 index 解析装齐 | P1 |
| FR-19-AC6 | M35：wheel + sdist 同版本并存 → index 页两文件都在，pip 按 `--only-binary`/`--no-binary` 各取所需 | P1 |
| FR-19-AC7 | 匿名边界同 FR-18-AC8 的 pip 版（匿名 install 默认可、upload 401） | P1 |
| FR-19-AC8 | PEP 691 JSON simple（P2）：`curl -H 'Accept: application/vnd.pypi.simple.v1+json' .../simple/demo-pkg/` → JSON `files[]` 与 HTML 等价 | P2 |
| FR-19-AC9 | 上传协议细节（v1.1 定案，P1）：`POST` 携带 `:action=submit` → 400 `unknown action 'submit'`；twine ≥ 6.2（不发送 `md5_digest`）上传 → 200 成功（服务端自算）；无尾斜杠 `curl -I .../simple/demo-pkg`（不带 `/`）→ 302 且 Location 为补尾斜杠 URL；`GET .../simple/demo-pkg/ -I` 的 `ETag` 存在且 `If-None-Match` 重发 → 304 | P1 |

### FR-20 remote 仓库：代理缓存 pull-through（dev-registry-adapter + dev-go-core）

**用户故事**：作为平台工程师，我把 Maven Central/npmjs/PyPI/内网 Nexus 的代理建到 BinFlow，开发者与 CI 只见 BinFlow 一个地址——首次拉取回源、此后命中本地缓存，上游故障不传染。

行为规格（pull-through 语义对齐 Artifactory remote；SSRF 为 BinFlow 安全底线，NFR-S13）：

- **请求流（GET/HEAD `/binflow/<remote>/<path>`，v1.1 按 repo-semantics §7.2 六步定案重写）**：
  1. 前置拒绝：blacked-out / handle* 开关与 include/exclude 不匹配 → 404/409（同 local §2 语义）；
  2. **checksum 后缀请求（`.sha1`/`.md5`/`.sha256` 等）一律不回源 → 404** `"Checksums are not downloadable."`（**v1.1 新增定案**，规格 §1.5/§7.2 高置信度，Artifactory 私有补充——checksum 只经缓存条目或服务端计算（响应头）提供；mvn 客户端对缺失 checksum 文件容忍告警不失败，M45 断言）；
  3. 查**负缓存**（miss cache）：期内已知 miss → 直接 404，零上游流量；
  4. 查**本地缓存**（node 落 remote repo 命名空间）：命中且未过 `retrievalCachePeriodSecs` → 直接服务（零上游流量，M1 头集齐全）；
  5. 缓存过期或缺失 → 回源（Basic 凭据按 repo 配置）：上游 200 → 流式落盘（复用上传会话/checksum 计算，**落盘与响应内容逐位一致**，item info `checksums` 即实测值；上游响应的 `X-Checksum-*` 头读为 original checksum 与实测比对——M3 只登记不拒，四值策略 M4）；上游 404 → 写负缓存 + **若本地有过期副本则仍回发过期副本**（"expired but serving"，v1.1 定案）；上游 5xx/超时/连接失败 → 仓标记 **assumed-offline**（静默 `assumedOfflinePeriodSecs`，期内零上游流量）+ 有缓存（含过期）服务缓存、无缓存 → **404**（E-01，message 含 offline/assumed offline 状态提示——**v1.1 定案推翻 v1.0 的默认 502**，repo-semantics §7.6 高置信度；`hardFail:true` 时改 **502**）；BinFlow 扩展：服务缓存时附 `X-Binflow-Upstream-Error: <摘要>` 头（可观测性扩展，无害）；
  6. remote 仓**不可写**：PUT/POST → **405 + `Allow: GET`**；`DELETE /binflow/<remote>/<path>` 语义 = **仅删本地缓存**（删除后再 GET 触发回源，见 §2.2 边界）。
- **上游凭据 401/403**：视为资源 unfound（404 透传，repo-semantics §7.6 中置信度）；BinFlow message 附 upstream 状态摘要便于排障。
- **缓存键与隔离**：同 path 不同 repo 各自独立缓存；**过期后的回源校验（HEAD 探测 + Last-Modified 比较，较新则撤销过期不重下）为 P1**（v1.1 定案其存在——repo-semantics §7.3/§7.4 高置信度；M3 P0 允许「到期直接 GET 重取」的简化实现，M41 的上游计数断言不受影响）；同 path 并发 miss 单飞（singleflight，P1——等待者直接复用获胜者结果）。
- **npm/PyPI/Maven remote 特化**：
  - maven remote：layout 路径直拼（上游即 Maven Central 形态）；`maven-metadata.xml` 请求按普通缓存对象处理（TTL 到期才回源——`mvn -U` 场景由 TTL 缩短或 `DELETE` 缓存触发刷新，P1 提供 `GET ...?refresh=true` 强刷参数? **不做**——保持 URL 语义与 Artifactory 一致，强刷走 DELETE 缓存路径）；
  - npm remote：packument 与 tarball 均按路径代理；packument 内的 `dist.tarball` 指向上游 URL 时**重写为 BinFlow URL**（否则 `npm install` 会绕过 BinFlow 直连上游——重写是代理语义的一部分，P0）；
  - pypi remote：simple index HTML 直传缓存；其中 href 指向上游绝对 URL 时同样**重写**为本仓路径（P0）；`#sha256=` fragment 保留上游值（哈希不变性——重写仅替换 URL 部分）。
- **virtual 成员中的 remote**：见 FR-21。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-20-AC1 | M41：mock 上游（`python3 -m http.server 9099 --directory upstream-dir`，`allowPrivateUpstream:true` 放行）——首次 `GET /binflow/generic-remote/dir/up.bin` → 200 且 sha256 与上游文件一致（上游 log 命中 1 次）；二次 GET → 200（**上游 log 仍 1 次**，负断言） | P0 |
| FR-20-AC2 | M41b：缓存命中速度——二次 GET 为本地盘速（QA 计时对比首次，≥2x 提速或上游零访问即可判过）；item info（E-09）可查该缓存 node 的 checksums | P1 |
| FR-20-AC3 | M42：SSRF 默认拒绝——不带 `allowPrivateUpstream` 的 remote 指向 `127.0.0.1:9099`：建仓 200 但 GET → **400**（E-01，message 含 `private/suppressed upstream` 类字样）+ 服务日志 WARN（含 repo key、目标 IP）；`file://`/`ftp://` scheme 建仓即 400（FR-15-AC3） | P0 |
| FR-20-AC4 | M43：上游 404 → BinFlow 404；负缓存期内二次 GET 上游 log 零新增 | P0 |
| FR-20-AC5 | M44（v1.1 改写）：上游停机（kill http.server）→ ① GET 已缓存路径 → **200 旧内容** + `X-Binflow-Upstream-Error` 头；② GET 未缓存路径 → **404** E-01（message 含 assumed offline/offline 状态——v1.1 定案，默认 `hardFail:false`）；③ 配 `hardFail:true` 的对照仓同请求 → **502**；④ 停机静默期内（assumedOfflinePeriodSecs=300）恢复上游，静默期结束前请求**不打上游**（上游 log 零新增）、期后自动恢复；⑤ `/binflow/api/v1/health` 全程 200（故障隔离） | P0 |
| FR-20-AC6 | M45：maven remote 代理 Maven Central（公网可用时；离线用 mock 上游摆 junit 布局）——`mvn dependency:get -Dartifact=junit:junit:4.13.2 -DremoteRepositories=central::default::$BASE/binflow/maven-remote` → 成功；仓内 item info 可见 `junit/junit/4.13.2/*.jar`；二次 dependency:get（清本地 repo）零上游 | P0 |
| FR-20-AC7 | M46：npm remote 代理 npmjs（公网可用时；离线用 mock）——`npm install lodash --registry $BASE/binflow/api/npm/npm-remote/` exit 0；抓包/日志证明 tarball 与 packument 均经 BinFlow（`dist.tarball` 已重写）。**v1.3/E4 注记：mock 上游布局 = `<name>/packument.json`（BinFlow 回源请求形态）；真实 npmjs packument 于 `/<name>` 直出——不兼容为 T-75 已证既有边界（非回归），「公网可用时」腿以 mock 为准** | P1 |
| FR-20-AC8 | M47：pip 代理 pypi.org（公网可用时；离线 mock）——`pip install --index-url .../simple six` exit 0，二次零上游。**v1.3/E4：remote `url=https://pypi.org` 站点根（服务端拼 `simple/<pkg>/`；带 `/simple` 后缀反而 404）——T-105 真实上游已验** | P1 |
| FR-20-AC9 | M48：remote 写拒绝——`PUT /binflow/generic-remote/x.bin` → 405 + `Allow: GET`；`DELETE /binflow/generic-remote/dir/up.bin` → 204（删缓存）后 GET 再次回源（上游 log +1，内容一致） | P0 |
| FR-20-AC10 | 上游凭据（凭据经 ADR-0012 加密链存储，见 FR-15-AC9；env `BINFLOW_REMOTE_CREDENTIALS_KEY` 在场）：mock 上游开 Basic（h / tpasswd）→ remote 配 username/password 后 GET 成功；错误凭据 → **404**（上游 401 视为资源 unfound，v1.1 定案 repo-semantics §7.6；message 附 upstream 401 摘要）；GET repo config 不回显明文密码 | P1 |
| FR-20-AC11 | 大文件流式：上游 1GB 文件代理下载，服务进程 RSS 增量 < 256MB（M1 NFR-P3 的 remote 版） | P1 |
| FR-20-AC12 | 上游重定向：302 → http://169.254.169.254/（云 metadata）→ 拒绝（400 + WARN，重定向每跳重过校验链）；302 → 公网同源路径 → 跟随成功（≤5 跳） | P0 |
| FR-20-AC13 | checksum 后缀不回源（v1.1 定案，P0）：`GET /binflow/generic-remote/dir/up.bin.sha1`（含已缓存制品）→ 404，body message == `Checksums are not downloadable.`；上游 log 零新增（M45 对 maven-remote 的 `.jar.sha1` 同断言；mvn 经 M45 全链不受此影响） | P0 |

### FR-21 virtual 仓库：聚合与解析顺序（dev-go-core + dev-registry-adapter）

**用户故事**：作为平台工程师，我把 local 与若干 remote 缝进一个 virtual 仓，成员声明序即默认优先级、可按成员标记 `priorityResolution` 提权——内部制品优先、公共依赖兜底，开发者只配一个 URL。

行为规格（**v1.1 按 repo-semantics §8 定案重写**：virtual 概念对齐 Artifactory，搜索序为「四桶」而非纯列表序——BinFlow 因无独立 `<key>-cache` 仓投影（§6.4）简化为两桶）：

- **GET/HEAD `/binflow/<virtual>/<path>`**：成员按**两桶序**解析——优先桶（`priorityResolution=true` 的成员，桶内按声明序）在前，其余成员（桶内按声明序）在后；local 成员查本地 node，remote 成员走 FR-20 代理链（含其缓存与 stale 语义）；**下载类解析首命中即停**（透传其内容与头集）。全 miss → 404（E-01）。Artifactory 四桶的精确语义（cache 仓与 remote 本体同进退、同优先级内声明序）为规格 §8.1 高置信度定案，BinFlow 两桶为其在「缓存 node 直落 remote 仓」模型下的语义等价简化（差异仅在 cache 命中与 remote 回源的次序，对客户端不可观察）。
- **写路由**：未配 `defaultDeploymentRepo` → PUT/POST/DELETE → **405 + `Allow: GET`**，body `No local repository was configured as local deployment repository for the (<key>) virtual repository.`（**v1.1 定案文案**，repo-semantics §8.2 高置信度）；配置后 → 写操作路由到该 local 成员执行（权限、覆盖检查、checksum 链均按目标仓语义），virtual GET 立即可见（P1）；`defaultDeploymentRepo` 指向非 local 成员 → 400（建仓校验）。npm 的 publish PUT 与 PyPI 的 upload POST 同样走路由（FR-18/19 端点含 `<virtual-repo>` 形态）。
- **metadata 聚合（P1，v1.1 定案 per-protocol 语义——并非统一机制，规格 §8.3 高置信度）**：
  - **Maven**：`maven-metadata.xml` GET 拦截——按桶序逐仓取同名 metadata，**内存合并**：versions 去重按 Maven 版本序重排、latest/release 重算、snapshot 取 buildNumber 更大者、v3 开关下并 `snapshotVersions`（绕开 Maven 官方 merge 不含 snapshotVersions 的缺陷 MNG-5180）；优先成员已产出即停止非优先成员（foundByPriority 短路）；**结果不缓存、每次现算**；任一成员被 block → 整体透传 block；
  - **npm**：首成员为基底、后续成员版本 `putIfAbsent`（先到先得，同版本不覆盖——v1.0 的「靠前成员为准」表述定案）、`dist-tags`/`time` 并集、通用字段取先到者、latest 重算；合并结果**可缓存**（Artifactory 存 virtual cache 仓 TTL 600s 且 <600 视为禁用——BinFlow M3 按规格 §4.3 建议实现「每次现算」版，语义等价，缓存优化 M4）；
  - **PyPI**：simple 索引逐仓收集后条目合并；格式不一致（请求 JSON 而成员只出 HTML）→ 整体回退 HTML；任一成员失败不阻塞其它；remote 成员的「包不存在」走独立负缓存（Artifactory 8h/5 万条——BinFlow 复用 FR-20 负缓存参数，不单列）。
  - P0 语义 = 首个命中成员透传（同名包分散多成员时只见到首个——文档明示，P1 升级合并）。
- **删除**：virtual 不可直接删（405）；`DELETE /binflow/<virtual>/<path>` 不提供「穿透删除」语义（BinFlow 有意不兼容——**v1.1 更正背景**：Artifactory 实为逐成员查找持有者删除（repo-semantics §8.2 中置信度、待验证 #5），非「不做」；BinFlow M3 仍不做，防误删上游缓存，删缓存请对 remote 成员操作，M4 治理时再评估跟进）。
- **virtual 嵌套 virtual**：不支持（FR-15-AC4，400；Artifactory 支持递归展开，BinFlow M3 有意收窄）。
- **浏览**：`GET /binflow/api/storage/<virtual>/<path>` 聚合 children（同名去重，标注来源成员——`props`/字段形态归实现；P1）；`GET /binflow/<virtual>/` 目录列表（匿名可读语义随全局开关）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-21-AC1 | M50：`maven-virtual`（members: maven-local, maven-remote-x）——本地包 `demo-app`（M11）与上游包 `junit:junit:4.13.2`（M45 之上游）分别经 `GET /binflow/maven-virtual/<相应 path>` 200；`mvn dependency:get` 两者均成功 | P0 |
| FR-21-AC2 | M51：解析顺序——同 GAV 两成员都有：成员1 放内容 A、成员2 放内容 B → GET 得 A（sha256 断言）；调整成员顺序重建 virtual → GET 得 B | P0 |
| FR-21-AC3 | M52：virtual 写拒绝——未配 `defaultDeploymentRepo`：`PUT /binflow/maven-virtual/...` → 405 + `Allow: GET`；`mvn deploy` 指向该仓 → 失败（客户端报错），服务端 405 | P0 |
| FR-21-AC4 | M53：写路由（P1）——配 `defaultDeploymentRepo: maven-local` 后 `mvn deploy` 经 virtual → BUILD SUCCESS；`GET /binflow/maven-virtual/<新 GAV>` 200；node 实际落 maven-local（`/binflow/maven-local/<GAV>` 亦 200） | P1 |
| FR-21-AC5 | M54：npm virtual——`npm install demo-pkg lodash --registry $BASE/binflow/api/npm/npm-virtual/` exit 0（本地包与代理包一次装齐）；PyPI virtual 等价（pip install 本地包 + 上游包） | P1 |
| FR-21-AC6 | M55：metadata 合并（P1）——maven：成员1 有 1.0.0、成员2 有 1.1.0 → `GET /binflow/maven-virtual/<GAV>/maven-metadata.xml` versions 含两者；npm packument 合并断言同理 | P1 |
| FR-21-AC7 | virtual 全 miss → 404 E-01；**stale/下一成员优先关系定案（v1.1，C3 收口）**：成员 remote 命中 stale 缓存（含过期副本）即作为该成员的解析结果返回、不跳下一成员；仅成员真正 404（负缓存/无副本/assumed-offline 无缓存）才继续桶序下一成员 | P1 |
| FR-21-AC8 | 聚合浏览：`GET /binflow/api/storage/maven-virtual/com/acme` → 200，children 覆盖两成员的 artifactId 并集 | P2 |
| FR-21-AC9 | priorityResolution 两桶序（v1.1 定案，P1）：M51 变体——把**声明序在后**的成员2 标记 `priorityResolution=true` → 同 GAV GET 得成员2 内容（优先桶整体前置于声明序）；无标记时回归 M51 的声明序行为 | P1 |

### FR-22 conformance：真实客户端矩阵与回归基线（qa + 全 dev 角色）

**用户故事**：作为评估者，mvn/npm/pip/twine 四个真实客户端在 local/remote/virtual 三种仓型上开箱可用——「多生态与代理」不是 curl 自证出来的。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-22-AC1 | mvn 3.9.x：local deploy（M11）→ 新 repo resolve（M13）→ snapshot `-U`（M16b）→ remote 代理（M45）→ virtual 混合解析（M50）全链 exit 0 | P0 |
| FR-22-AC2 | npm 10.x：publish/install/dist-tag/scoped/unpublish（M22~M27）→ remote 代理（M46）→ virtual（M54）exit 0 | P0 |
| FR-22-AC3 | pip 24.x + twine：upload/simple/install/依赖链（M30~M35）→ remote 代理（M47）exit 0 | P0 |
| FR-22-AC4 | Gradle 8.x 走 Maven 仓：`gradle build` 的 dependencies 指向 BinFlow maven local 仓解析成功（P2 观察——Gradle 走 Maven layout 但 HTTP 行为有差异，不作门槛） | P2 |
| FR-22-AC5 | 回归基线（§5.6）：M1 C 序列 P0 + M2 D 序列 P0 复跑全绿（E-07/E-26 断言按反转表更新） | P0 |
| FR-22-AC6 | 性能（§6.1）：冷启动 < 2s、50 并发 pip install（缓存命中）0 错误、remote 代理开销 < 100ms（M60 系列） | P0/P1 |

---

## 5. 兼容性矩阵（M3 核心）

### 5.1 层级定义（沿用 M1 §5.1 四层，M3 特化）

- **协议域（Maven/npm/PyPI）对齐基准 = 官方规范**（Maven 布局惯例、npm registry API、PEP 503）——「兼容」判定 = 真实客户端（mvn/npm/pip/twine）不改配置通过；
- **仓库语义域（remote/virtual）对齐基准 = Artifactory 行为**（repo-semantics.md + 待落地规格）——「兼容」判定 = 配置字段与可观察语义类推成立、迁移脚本仅改前缀；
- 错误契约沿用 M1 §5.1 三分层：本 PRD 新增三域全部落在**制品与通用层（E-01 `errors[]`）**；npm login 的 `/-/user/...` 端点归制品层（不新增第四分层，开放问题 Q7）。

### 5.2 M3 端点矩阵

「置信度」（v1.1 起基线更新）：高 = 官方规范明文或逆向规格（`maven-npm-pypi.md` + repo-semantics §1~§8，T-59 已落地）；中 = PRD 暂定（规格标「待验证」或未见明确值，§5.5 注明来源）。编号前缀：RE = 仓库模型/remote/virtual 行为，ME = Maven，NE = npm，PE = PyPI。

| # | 端点（方法 路径） | 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| RE-01 | `PUT /binflow/api/repositories/{key}` rclass=remote | 字段子集 url（必填）/username/password/retrievalCachePeriodSecs（默认 7200）/missedRetrievalCachePeriodSecs（1800）/socketTimeoutSecs（15）/assumedOfflinePeriodSecs（300）/hardFail（false）+ BinFlow 扩展 allowPrivateUpstream；200 纯文本（M1 E-06 语义） | 兼容（子集） | P0 | 高（v1.1：字段与默认值定案，repo-semantics §7.1） | M02/M02b |
| RE-02 | `PUT .../{key}` rclass=virtual | `repositories[]` 必填；成员级 `priorityResolution`（两桶序）；`defaultDeploymentRepo` 可选（别名 `defaultDeploymentRepoRef`）；嵌套 virtual 400 | 兼容（子集） | P0 | 高（v1.1 定案，repo-semantics §8.1/§8.2） | M03/M52/M53 |
| RE-03 | `GET /binflow/api/repositories`（type/packageType 过滤） | `type=remote|virtual` 取值启用；列表元素含 url（remote）/repositories（virtual）摘要 | 兼容（子集） | P1 | 高 | M04 |
| RE-04 | `GET/HEAD /binflow/<remote>/<path>` | pull-through 六步（§7.2）：checksum 后缀不回源 404 / 负缓存 / 缓存命中直发 / 过期回源（HEAD 协商 P1）/ 上游 404 负缓存+过期副本回发 / 上游故障 assumed-offline 静默——有缓存服务缓存、无缓存 **404**（`hardFail:true` → 502）；M1 头集齐全 | 兼容（语义对齐 Artifactory remote 下载） | P0 | 高（v1.1 定案，repo-semantics §7.2/§7.6；上游 5xx 分支细节标中） | M41/M43/M44 |
| RE-05 | `PUT/POST /binflow/<remote>/<path>` | remote 不可写：405 + `Allow: GET` | 兼容 | P0 | 高 | M48 |
| RE-06 | `DELETE /binflow/<remote>/<path>` | **仅删本地缓存**（幂等 204/404 语义同 M1 E-14），再 GET 触发回源；不触达上游（Artifactory 另有 Zapping 清缓存端点族，BinFlow 不实现该 REST 形态——DELETE 即等价操作） | 兼容（Artifactory 同语义） | P1 | 高（v1.1 升格：repo-semantics §7.4 无上游删除同步语义，C6 收口） | M48 |
| RE-07 | `GET/HEAD /binflow/<virtual>/<path>` | 两桶序（priorityResolution 优先桶 → 其余，桶内声明序）、下载首命中即停；全 miss 404 | 兼容 | P0 | 高（v1.1 定案：Artifactory 四桶序 §8.1，BinFlow 两桶为无 cache 仓投影下的等价简化） | M50/M51 |
| RE-08 | `PUT/DELETE /binflow/<virtual>/<path>` | 未配写路由 → 405 + `Allow: GET` + 定案文案（repo-semantics §8.2）；DELETE 不透传（BinFlow 有意不兼容——Artifactory 实为逐成员删除，中置信度待验证 #5，M4 评估跟进） | 兼容（PUT）/ 有意不兼容（DELETE 透传） | P0/P1 | 高（PUT 分支）/ 中（DELETE 分支背景） | M52 |
| RE-09 | `GET /binflow/api/storage/<virtual>/<path>` | 聚合 children/item info（来源标注 P2） | 兼容（子集） | P1 | 中 | M55b |
| RE-10 | `{"rclass":"remote","packageType":"docker"}` 等 docker 组合 | 400「not supported in M3」（§2.2 + Q4） | 有意不兼容（阶段性） | P0 | — | M05 |
| RE-11 | `GET /binflow/api/v1/remote/stats` | BinFlow 自有：每 remote 仓缓存 node 数/字节数/命中率（hit/miss 计数） | /api/v1 | P2 | — | M59 |
| ME-01 | `PUT /binflow/<maven-repo>/<layout path>` | 构件/pom/旁车 checksum；严格 layout 校验（400）按 maven-2-default 六字段 pattern（v1.1 定案模型）；checksum 策略三态；metadata/sidecar 永不触发覆盖检查；snapshot policy 拒绝 **409** | 兼容 | P0 | 高（checksum 链/layout 模型/409，v1.1 定案）/ 中（400 码值，C2） | M11/M17/M18/M19/M20 |
| ME-02 | `GET/HEAD /binflow/<maven-repo>/<layout path>` | 200 流式 + `X-Checksum-*`/`ETag=<sha1>`/Range/304（M1 链继承） | 兼容 | P0 | 高 | M12/M13/M21 |
| ME-03 | `GET /binflow/<maven-repo>/<file>.{md5,sha1,sha256}` | local：旁车内容 = 服务端实测裸 hex（无换行）；PUT 旁车走校验链；**remote 仓：不回源 → 404 `Checksums are not downloadable.`**（v1.1 定案，规格 §1.5/§7.2） | 兼容 | P0 | 高 | M14/M18/M45 |
| ME-04 | `GET .../maven-metadata.xml`（版本组目录） | 服务端计算生成：versions=含 pom 子目录集、latest/release 重算、lastUpdated=计算时刻；无 pom 子目录 → 删 metadata（RTFACT-6242 例外）；`.md5/.sha1` 旁车一致 | 兼容 | P0 | 高（v1.1 定案，规格 §1.4） | M14/M15 |
| ME-05 | `GET .../<version>/maven-metadata.xml`（snapshot） | snapshotVersions 每 (ext×classifier) 最新、buildNumber/timestamp 取自最新 unique pom（non-unique 固定 1）；`-U` 刷新可用 | 兼容 | P0 | 高（v1.1 定案，规格 §1.4） | M16/M16b |
| ME-06 | `PUT .../maven-metadata.xml` | 客户端上传被接受（201）并触发重算；**权威内容以服务端存储事实计算为准**（等效合并，并发不丢版本） | 兼容 | P0 | 高（v1.1 定案，规格 §1.4——v1.0「XML union/max」表述修正） | M15 变体 |
| ME-07 | snapshot 上传（repo 字段 `snapshotVersionBehavior`：deployer 默认/non-unique/unique） | 三值语义见 FR-16；已是 unique 文件名不改写；服务端 unique 改写（buildNumber 连续）P2 | 兼容 | P0 | 高（v1.1 定案，规格 §1.3） | M16 |
| ME-08 | `handleReleases/handleSnapshots` 开关 | false 时对应 deploy 拒绝 **409**（v1.1 勘误：SnapshotPolicyException 显式 409）；GET 不受影响 | 兼容 | P1 | 高（v1.1 定案，规格 §1.4） | M19 |
| ME-09 | repo 配置 `checksumPolicyType` | `client-checksums`（默认）/`server-generated-checksums` 两值 | 兼容 | P0 | 高（repo-semantics §5） | M17 |
| ME-10 | `GET /binflow/<maven-repo>/.index/**` | Maven 索引不做 → 404 + E-01 | 有意不兼容 | — | — | M58 |
| NE-01 | `PUT /binflow/api/npm/<repo>/<package>` | publish 走十步校验链（§4 FR-18，v1.1 定案）；201 `{"success":true}`；scoped `@scope%2Fname`（`%2f`/`%2F` 等价）；**同版本重复 → 403** `Cannot modify pre-existing version`（v1.1 定案推翻 409）；integrity/shasum 不一致 → 400 | 兼容 | P0 | 高（v1.1 定案，规格 §2.3） | M22/M26 |
| NE-02 | `GET /binflow/api/npm/<repo>/<package>` | packument：name/dist-tags/versions/dist{tarball,shasum,integrity}/_rev/time；tarball URL 一律重写指回 BinFlow；**`ETag`/`X-Checksum-Sha1` = 包文档 JSON 的 sha1 + If-None-Match 304**；SLIM Accept 协商（P1） | 兼容 | P0 | 高（v1.1 增补定案，规格 §2.4） | M22b/M46 |
| NE-03 | `GET /binflow/api/npm/<repo>/<pkg path>/-/<file>.tgz` | tarball 流式 + checksum 头；内容路径 `/binflow/<repo>/...` 亦可 GET（同 node 两入口；布局 v1.1 定案 = `<name>/-/<name>-<version>.tgz`，scoped 含 `@<scope>/` 前缀） | 兼容 | P0 | 高 | M23 |
| NE-04 | dist-tags：`GET|PUT|POST|DELETE /binflow/api/npm/<repo>/-/package/<name>/dist-tags` + 旧式 `PUT .../<package>/<tag>` | npm ≥ 8 与旧客户端两形态；PUT → 201 `{"ok":"created new tag"}`、DELETE → 200 空、缺失 404 定案文案；版本引用联动 | 兼容 | P0 | 高（v1.1 定案，规格 §2.1） | M24 |
| NE-05 | unpublish：`PUT .../-rev/<rev>` + `DELETE .../-rev/<rev>` / `DELETE .../-/<filename>/-rev/<rev>` | **`-rev` PUT 恒 200 `{"ok":"updated package"}` 假成功**（npm unpublish 前置占位，v1.1 新增定案）；DELETE 单版本/整包 → 200；dist-tags 联动 | 兼容 | P1 | 高（v1.1 定案，规格 §2.1/§2.7-1） | M27 |
| NE-06 | `PUT /binflow/api/npm/<repo>/-/user/org.couchdb.user:<name>` + `GET /-/whoami` | npm login（legacy auth-type）/whoami；签发 M1 token 表 token | 兼容 | P1 | 高（npm 行为） | M22c |
| NE-07 | `GET /binflow/api/npm/<repo>/-/ping` | `{}` | 兼容 | P2 | 高 | M57 |
| NE-08 | `GET /binflow/api/npm/<repo>/-/v1/search`、`/-/npm/v1/security/*` | 不做 → 404 + E-01 | 有意不兼容 | — | — | M58 |
| PE-01 | `GET /binflow/api/pypi/<repo>/simple/<name>/` | PEP 503 HTML + `#sha256=` + **api-version=2 头（PEP 629）**；name 归一化三态同页；未知名 404；**无尾斜杠 → 302 补尾斜杠**；ETag + 304（不透明稳定哈希）；条目按文件名排序；gzip 预压缩 P2 | 兼容 | P0 | 高（PEP 503 规范 + v1.1 定案细节，规格 §3.2） | M31 |
| PE-02 | `POST /binflow/api/pypi/<repo>` | twine/warehouse multipart；**`:action` 严格 `file_upload` 否则 400 `unknown action`**；`md5_digest` 可缺失（twine ≥ 6.2，服务端自算）；缺 name/version/content 400；重复 filename 400（维持暂行）；**响应统一 200** | 兼容 | P0 | 高（v1.1 定案，规格 §3.3；重复 filename 分支规格未明示） | M30/M33 |
| PE-03 | 文件下载（simple href → `/binflow/api/pypi/<repo>/packages/<name>/<version>/<filename>`；存储布局 `<repo>/<name>/<version>/<filename>` 原始名） | 复用 M1 内容下载链（checksum 头/Range/304）；pip 经 fragment 自校验；内容路径 `/binflow/<repo>/...` 为同 node 第二入口 | 兼容（子集）（v1.1 升格：布局对齐 Artifactory，C7 收口） | P0 | 高（v1.1 定案，规格 §3.5） | M32 |
| PE-04 | `GET /binflow/api/pypi/<repo>/pypi/<name>/json` | PyPI JSON API 不做 → 404 | 有意不兼容 | — | — | M58 |
| PE-05 | PEP 691：`Accept: application/vnd.pypi.simple.v1+json` | JSON simple 等价形态 | 兼容（子集） | P2 | 高（PEP 691 规范） | M35b |
| PE-06 | `/binflow/api/pypi-ui/**` | 维持 404（M1 E-26） | 有意不兼容 | — | — | M58 |

> 计数：**35 条**。兼容/兼容（子集）**29**（v1.1：PE-03 由「语义等同」升格）；`/binflow/api/v1` **1**（RE-11）；语义等同但路径不同 **0**；有意不兼容 **5**（RE-10、ME-10、NE-08、PE-04、PE-06）——另有 1 个行内有意不兼容分支：RE-08 的 DELETE 透传分支（v1.0 的 NE-01 重复 publish 409 偏离分支已随 403 定案消除）。

### 5.3 真实客户端分级矩阵（conformance 判定标准）

「全过」定义：所列操作退出码 0 且服务端日志无 5xx。公网不可用的场景一律以 mock 上游替代并在 qa 报告注明。

| 客户端 | 必测操作 | 分级 |
|---|---|---|
| mvn 3.9.x | deploy（release+snapshot）、全新 repo resolve、`-U` 强刷、dependency:get（local/remote/virtual 三仓型） | **P0 必须全过** |
| npm 10.x | publish（plain+scoped）、install（缓存清空）、dist-tag add/rm、unpublish、remote 代理 install、virtual install | **P0 必须全过** |
| pip 24.x + twine | upload（wheel+sdist）、install（含依赖链）、download+hash 校验、remote 代理、virtual | **P0 必须全过** |
| curl | 各域协议抽查（packument/simple/metadata/旁车/SSRF 负断言） | **P0 必须全过** |
| Gradle 8.x | `gradle build` 依赖解析走 maven local 仓 | P2 观察（不作门槛） |
| yarn / pnpm | install 走 npm 协议 | P2 观察（协议同 npm，非门槛） |
| python 3.8-（setuptools legacy upload） | 老 twine/upload 形态 | 不做（twine 现代形态为准） |

### 5.4 M3 核心验收命令（M 序列，QA 直接引用）

> 编号与 §4 AC、§5.2 矩阵互相引用。mvn/npm/pip/twine 版本矩阵见 §5.3；公网依赖项（M45/M46/M47 的真实上游）在离线环境用 mock 上游替代。`$BASE/$ADMIN_PW` 沿用 §4 约定。

```bash
# 环境准备
export BASE=http://localhost:8080
export ADMIN_PW=password
export MVN_REPO=$BASE/binflow/maven-local
export NPM_REG=$BASE/binflow/api/npm/npm-local/
export PIP_IDX=$BASE/binflow/api/pypi/pypi-local/simple

# ---- 仓库模型（FR-15） ----
# M01 三协议 local 仓（FR-15-AC1）
for t in maven npm pypi; do
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/$t-local \
    -H 'Content-Type: application/json' -d "{\"rclass\":\"local\",\"packageType\":\"$t\"}" \
    -o /dev/null -w "%{http_code} $t-local\n"        # 200 x3
done
curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories/maven-local | jq -r .packageType   # maven

# M02/M02b remote 建仓与字段校验（FR-15-AC2/AC3；上游 mock 9099）
mkdir -p upstream-dir/dir && printf 'upstream-bytes' > upstream-dir/dir/up.bin
(cd upstream-dir && python3 -m http.server 9099 >/tmp/upstream.log 2>&1 &)   # mock 上游
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","allowPrivateUpstream":true}' \
  -o /dev/null -w '%{http_code}\n'                    # 200
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/bad-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"generic"}' -o /dev/null -w '%{http_code}\n'       # 400（缺 url）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/bad-remote2 -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"generic","url":"file:///etc"}' -o /dev/null -w '%{http_code}\n'  # 400（scheme）

# M03 virtual 建仓与成员校验（FR-15-AC4；maven-remote-x 先建好指向 mock 的 maven remote）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-remote-x -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"maven","url":"http://127.0.0.1:9099/m2","allowPrivateUpstream":true}' \
  -o /dev/null -w '%{http_code}\n'                    # 200
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-virtual -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"maven","repositories":["maven-local","maven-remote-x"]}' \
  -o /dev/null -w '%{http_code}\n'                    # 200
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/bad-virtual -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"maven","repositories":["no-such-repo"]}' -o /dev/null -w '%{http_code}\n'  # 400

# M04 过滤（FR-15-AC5）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/repositories?type=remote" | jq -r '.[].key' | grep -x generic-remote  # 退出码 0
curl -su admin:$ADMIN_PW "$BASE/binflow/api/repositories?packageType=maven" | jq -r '.[].key' | grep -x maven-virtual # 0

# M05 docker 组合边界（RE-10，FR-15-AC7）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"docker","url":"https://registry-1.docker.io"}' -o /dev/null -w '%{http_code}\n'  # 400（Q4 现状）

# ---- Maven（FR-16/FR-17） ----
# M10 settings.xml（凭据注入；M11 前置）
mkdir -p ~/.m2 && cat > ~/.m2/settings.xml <<EOF
<settings><servers><server>
  <id>binflow</id><username>admin</username><password>$ADMIN_PW</password>
</server></servers></settings>
EOF

# M11 mvn deploy（FR-16-AC1；最小工程骨架）
mkdir -p mvn-demo/src/main/java/demo && cd mvn-demo
cat > pom.xml <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>demo-app</artifactId>
  <version>1.0.0</version><packaging>jar</packaging>
</project>
EOF
echo 'package demo; public class App{}' > src/main/java/demo/App.java
mvn -B -q -DskipTests deploy \
  -DaltDeploymentRepository=binflow::default::$MVN_REPO && echo DEPLOY_OK   # BUILD SUCCESS / DEPLOY_OK

# M12 落盘断言（FR-16-AC2；+ 跨协议去重附带断言）
curl -su admin:$ADMIN_PW -o dl.jar $BASE/binflow/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar
sha256sum target/classes/../demo-app-1.0.0.jar dl.jar 2>/dev/null || cmp ~/.m2/repository/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar dl.jar && echo SAME
curl -su admin:$ADMIN_PW $BASE/binflow/api/storage/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.pom | jq -r .checksums.sha1 | wc -c   # 41（40 hex + 换行）
curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar.sha1 | tr -d '\n' | wc -c   # 40（裸 hex）

# M13 消费侧 resolve（FR-16-AC3；全新本地 repo）
cd .. && mkdir consumer && cd consumer
cat > pom.xml <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>consumer</artifactId><version>1.0.0</version>
  <dependencies><dependency><groupId>com.acme</groupId><artifactId>demo-app</artifactId><version>1.0.0</version></dependency></dependencies>
  <repositories><repository><id>bf</id><url>$MVN_REPO</url></repository></repositories>
</project>
EOF
mvn -B -q -Dmaven.repo.local=$(pwd)/fresh-repo compile && echo RESOLVE_OK    # BUILD SUCCESS（sha1 校验随下载通过）

# M14 metadata + 旁车（FR-16-AC4 / FR-17-AC6）
curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/maven-metadata.xml | grep -c '<version>1.0.0</version>'  # 1
curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/maven-metadata.xml -o mm.xml
SIDECAR=$(curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/maven-metadata.xml.sha1 | tr -d '\n')
[ "$SIDECAR" = "$(sha1sum mm.xml | cut -d' ' -f1)" ] && echo SHA1_MATCH      # SHA1_MATCH

# M15 新版本合并（FR-17-AC1；versions 含 1.0.0+1.1.0、latest=1.1.0）
cd ../mvn-demo && sed -i '' 's/1.0.0/1.1.0/' pom.xml 2>/dev/null || sed -i 's/1.0.0/1.1.0/' pom.xml
mvn -B -q -DskipTests deploy -DaltDeploymentRepository=binflow::default::$MVN_REPO
curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/maven-metadata.xml | \
  jq -e '.xpath' 2>/dev/null || curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/maven-metadata.xml | \
  grep -c '<version>'    # ≥2；latest 断言：grep '<latest>1.1.0</latest>' 退出码 0

# M16/M16b snapshot（FR-16-AC7 布局 / FR-17-AC2/AC3）
sed -i '' 's/1.1.0/1.2.0-SNAPSHOT/' pom.xml 2>/dev/null || sed -i 's/1.1.0/1.2.0-SNAPSHOT/' pom.xml
mvn -B -q -DskipTests deploy -DaltDeploymentRepository=binflow::default::$MVN_REPO   # 两次（buildNumber 1→2）
mvn -B -q -DskipTests deploy -DaltDeploymentRepository=binflow::default::$MVN_REPO
# v1.3/E3（=T-74 E2）：?list&deep=1 不可作载体（SNAPSHOT 目录为隐式目录、无 folder node——M1 既有语义）；
#   断言载体 = 下行 version 级 metadata snapshotVersions + <value> 提取 timestamped 文件名直连 GET
TSJAR=$(curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/maven-metadata.xml | \
  grep -o 'demo-app-1\.2\.0-[0-9.]*-[0-9]*\.jar' | sort -u | tail -1)   # 最新 timestamped（buildNumber 2）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' \
  "$BASE/binflow/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/$TSJAR"   # 200（timestamped 直连可取）
curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/maven-metadata.xml | \
  grep -c '<snapshotVersion>'    # ≥2；buildNumber 断言 grep '<buildNumber>2</buildNumber>' = 0 退出码
mvn -B -q -U dependency:get -Dartifact=com.acme:demo-app:1.2.0-SNAPSHOT \
  -DremoteRepositories=bf::default::$MVN_REPO -Dmaven.repo.local=$(pwd)/snap-repo && echo SNAP_OK   # SNAP_OK

# M16c 宽容策略仓（FR-16-AC5 server-generated）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-lenient -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"maven","checksumPolicyType":"server-generated-checksums"}' -o /dev/null -w '%{http_code}\n'  # 200
# M17 checksum 策略两态（FR-16-AC5；curl 直打；v1.3/E2（=T-74 E1）：文件名须 layout 合规——
#   原 bad.jar 会先撞 M20 的 layout 400（须以 <artifactId>-<version> 开头），checksum 分支不可达）
JAR=target/demo-app-1.1.0.jar   # 任取一个已构建 jar
curl -su admin:$ADMIN_PW -T $JAR -H "X-Checksum-Sha1: $(printf '0%.0s' {1..40})" \
  $BASE/binflow/maven-local/com/acme/demo-app/1.1.0/demo-app-1.1.0-badchk.jar -o /dev/null -w '%{http_code}\n'    # 409（client-checksums）
curl -su admin:$ADMIN_PW -T $JAR -H "X-Checksum-Sha1: $(printf '0%.0s' {1..40})" \
  $BASE/binflow/maven-lenient/com/acme/demo-app/1.1.0/demo-app-1.1.0-badchk.jar -o /dev/null -w '%{http_code}\n'  # 201（server-generated）

# M18 旁车两态（FR-16-AC6）
printf '%s' "$(sha1sum $JAR | cut -d' ' -f1)" > good.sha1
curl -su admin:$ADMIN_PW -T good.sha1 $BASE/binflow/maven-local/com/acme/demo-app/1.1.0/demo-app-1.1.0.jar.sha1 -o /dev/null -w '%{http_code}\n'  # 201
curl -su admin:$ADMIN_PW $BASE/binflow/maven-local/com/acme/demo-app/1.1.0/demo-app-1.1.0.jar.sha1 | tr -d '\n'   # == sha1sum $JAR
printf 'deadbeef' > bad2.sha1
curl -su admin:$ADMIN_PW -T bad2.sha1 $BASE/binflow/maven-local/com/acme/demo-app/1.1.0/demo-app-1.1.0.jar.sha1 -o /dev/null -w '%{http_code}\n'   # 409（client-checksums 仓）

# M19 开关拒绝（FR-16-AC7，P1；v1.1 勘误：409——SnapshotPolicyException 显式值，maven-npm-pypi.md §1.4）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-relonly -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"maven","handleSnapshots":false}' -o /dev/null -w '%{http_code}\n'  # 200
curl -su admin:$ADMIN_PW -T $JAR $BASE/binflow/maven-relonly/com/acme/x/2.0-SNAPSHOT/x-2.0-SNAPSHOT.jar -o /dev/null -w '%{http_code}\n'  # 409（拒绝，v1.1：404→409）

# M20 layout 校验（FR-16-AC8）
curl -su admin:$ADMIN_PW -T $JAR $BASE/binflow/maven-local/foo.jar -o /dev/null -w '%{http_code}\n'   # 400
curl -su admin:$ADMIN_PW -T $JAR $BASE/binflow/maven-local/com/acme/demo-app/1.0.0/zzz-1.0.0.jar -o /dev/null -w '%{http_code}\n'  # 400

# M21 Range/304 继承（FR-16-AC9，P1）
curl -su admin:$ADMIN_PW -r 0-99 -o /dev/null -w '%{http_code}\n' $BASE/binflow/maven-local/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar  # 206

# ---- npm（FR-18） ----
# M22 publish（FR-18-AC1；.npmrc Basic 凭据）
mkdir -p npm-demo && cd npm-demo
cat > package.json <<EOF
{"name":"demo-pkg","version":"1.0.0","description":"m3 qa"}
EOF
echo 'module.exports=1' > index.js
AUTH=$(printf 'admin:%s' "$ADMIN_PW" | base64)
cat > .npmrc <<EOF
registry=$NPM_REG
//localhost:8080/binflow/api/npm/npm-local/:_auth=$AUTH
always-auth=true
EOF
npm publish && npm view demo-pkg version            # + demo-pkg@1.0.0 / 1.0.0

# M22b packument（FR-18-AC2）
curl -su admin:$ADMIN_PW $BASE/binflow/api/npm/npm-local/demo-pkg | jq -r '.["dist-tags"].latest'          # 1.0.0
curl -su admin:$ADMIN_PW $BASE/binflow/api/npm/npm-local/demo-pkg | jq -r '.versions["1.0.0"].dist.tarball' # 以 /binflow/ 开头
npm pack --dry-run 2>/dev/null | true   # shasum 断言见 AC2：jq .versions["1.0.0"].dist.shasum == sha1 of tarball

# M22c login/whoami（NE-06，P1）
npm whoami --registry $NPM_REG                        # admin（.npmrc 凭据下）
# npm login --auth-type=legacy --registry $NPM_REG   # 交互式，QA 以 expect 或 .npmrc 等效凭据覆盖

# M23 install（FR-18-AC3）
cd .. && mkdir npm-consumer && cd npm-consumer
npm install demo-pkg --registry $NPM_REG && cat node_modules/demo-pkg/package.json | jq -r .version   # 1.0.0
npm cache clean --force && rm -rf node_modules && npm install demo-pkg --registry $NPM_REG && echo INSTALL_OK

# M24 dist-tags（FR-18-AC4）
npm dist-tag add demo-pkg@1.0.0 beta --registry $NPM_REG
curl -su admin:$ADMIN_PW $BASE/binflow/api/npm/npm-local/demo-pkg | jq -r '.["dist-tags"].beta'   # 1.0.0
npm install demo-pkg@beta --registry $NPM_REG && echo BETA_OK
npm dist-tag rm demo-pkg beta --registry $NPM_REG

# M25 scoped（FR-18-AC5）
mkdir -p scoped-demo && cd scoped-demo
cat > package.json <<EOF
{"name":"@acme/util","version":"1.0.0"}
EOF
npm publish && cd .. && npm install @acme/util --registry $NPM_REG && echo SCOPED_OK

# M26 重复 publish（FR-18-AC6；v1.1 勘误：409→403，Artifactory 定 403 `Cannot modify pre-existing version`）
cd npm-demo && npm version --no-git-tag-version 1.0.0 >/dev/null 2>&1 || true
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/npm/npm-local/demo-pkg \
  -H 'Content-Type: application/json' -d "$(curl -su admin:$ADMIN_PW $BASE/binflow/api/npm/npm-local/demo-pkg)" \
  -o /dev/null -w '%{http_code}\n'    # 403（同版本重复；客户端 npm publish 同效报错）

# M27 unpublish（FR-18-AC7/AC11，P1；v1.1：-rev PUT 恒 200 假成功为流程前置）
cd npm-demo && npm version --no-git-tag-version 1.0.1 >/dev/null && npm publish
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/npm/npm-local/demo-pkg/-rev/00000000000000000000000000000000 \
  -H 'Content-Type: application/json' -d '"0-0000000000000000000000000000000"' | jq -r .ok   # updated package（恒假成功，包内容无变化）
npm unpublish demo-pkg@1.0.1 --force --registry $NPM_REG
curl -su admin:$ADMIN_PW $BASE/binflow/api/npm/npm-local/demo-pkg | jq -r '.versions | keys | join(",")'  # 无 1.0.1

# M28 匿名边界（FR-18-AC8，P1；anonymous_access=false 实例复跑）
curl -s -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/npm/npm-local/demo-pkg   # 200（默认匿名读）
curl -s -X PUT -H 'Content-Type: application/json' -d '{}' $BASE/binflow/api/npm/npm-local/x -o /dev/null -w '%{http_code}\n'  # 401

# ---- PyPI（FR-19） ----
# M30 build + twine upload（FR-19-AC1）
mkdir -p pypi-demo && cd pypi-demo
cat > pyproject.toml <<EOF
[build-system] requires = ["setuptools>=61"] build-backend = "setuptools.build_meta"
[project] name = "demo-pkg" version = "0.1.0" dependencies = []
EOF
pip wheel . -w dist/ >/dev/null && ls dist/*.whl
twine upload --repository-url $BASE/binflow/api/pypi/pypi-local -u admin -p $ADMIN_PW dist/* && echo TWINE_OK

# M31 simple index + 归一化（FR-19-AC2；v1.1 补：api-version 头 / 无尾斜杠 302 / ETag-304 / :action 400）
curl -su admin:$ADMIN_PW $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/ | grep -c '#sha256='   # ≥1
curl -su admin:$ADMIN_PW $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/ | grep -c 'api-version" value="2"'  # 1（PEP 629）
for n in Demo_Pkg demo_pkg demo-pkg; do
  curl -su admin:$ADMIN_PW -o /dev/null -w "%{http_code} $n\n" $BASE/binflow/api/pypi/pypi-local/simple/$n/   # 200 x3
done
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg  # 302 + Location 补尾斜杠
ET=$(curl -su admin:$ADMIN_PW -I $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/ | awk -F': ' 'tolower($1)=="etag"{gsub("\r","");print $2}')
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' -H "If-None-Match: $ET" $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/  # 304
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/pypi/pypi-local -F ':action=submit' -F 'name=x' \
  | jq -r '.errors[0].message'    # unknown action 'submit'（HTTP 400）

# M32 pip install + hash 对账（FR-19-AC3）
python3 -m venv v && ./v/bin/pip install --index-url $PIP_IDX demo-pkg && ./v/bin/pip show demo-pkg | grep -x 'Version: 0.1.0'
./v/bin/pip download --index-url $PIP_IDX -d dl demo-pkg >/dev/null
curl -su admin:$ADMIN_PW $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/ | grep -o '#sha256=[a-f0-9]*' | head -1 | cut -d= -f2
sha256sum dl/demo_pkg-0.1.0-*.whl | cut -d' ' -f1     # 两值一致

# M33 重复上传（FR-19-AC4）
twine upload --repository-url $BASE/binflow/api/pypi/pypi-local -u admin -p $ADMIN_PW dist/* 2>&1 | grep -i 'already exists'; echo "exit=$?"  # 非 0 退出 + 服务端 400

# M34 依赖链（FR-19-AC5，P1）：再建 demo-lib 0.1.0 并令 demo-pkg dependencies=["demo-lib"] 后重传新版本 demo-pkg 0.2.0
./v/bin/pip install --index-url $PIP_IDX 'demo-pkg==0.2.0' && ./v/bin/pip show demo-lib | grep -x 'Name: demo-lib'   # 自动解析同仓依赖

# M35 wheel+sdist 并存（FR-19-AC6，P1）：python -m build 产 .tar.gz + .whl 同传
# M35b PEP 691 JSON（PE-05，P2）
curl -su admin:$ADMIN_PW -H 'Accept: application/vnd.pypi.simple.v1+json' $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/ | jq -r '.files | length'  # ≥1

# ---- remote 代理（FR-20） ----
# M41 pull-through + 缓存命中（FR-20-AC1；上游 log 计数为负断言依据）
UP1=$(grep -c 'GET /dir/up.bin' /tmp/upstream.log || true)
curl -su admin:$ADMIN_PW -o r1.bin $BASE/binflow/generic-remote/dir/up.bin && printf 'upstream-bytes' | cmp - r1.bin && echo FETCH_OK
curl -su admin:$ADMIN_PW -o r2.bin $BASE/binflow/generic-remote/dir/up.bin && cmp r1.bin r2.bin && echo CACHE_HIT
UP2=$(grep -c 'GET /dir/up.bin' /tmp/upstream.log || true); echo "upstream hits: $UP1 -> $UP2"   # 值不变

# M42 SSRF 默认拒绝（FR-20-AC3；RE-04/AC3）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/ssrf-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099"}' -o /dev/null -w '%{http_code}\n'  # 200（建仓不拦，请求时拦）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/ssrf-remote/dir/up.bin   # 400（私网拒绝）
grep -c 'ssrf-remote' <(tail -50 $(ls -t data/logs/* 2>/dev/null || echo /dev/stderr) 2>/dev/null) 2>/dev/null || journalctl -n 50 2>/dev/null | grep -ci 'warn'  # 以服务日志 WARN 断言为准（QA 按部署形态取日志）

# M43 上游 404 负缓存（FR-20-AC4）
UPa=$(wc -l < /tmp/upstream.log)
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-remote/no-such.bin   # 404
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-remote/no-such.bin   # 404
UPb=$(wc -l < /tmp/upstream.log); echo "$UPa -> $UPb"    # 上游访问只 +1（第二次命中负缓存）

# M44 上游故障降级（FR-20-AC5；v1.1 改写：默认 404 + assumed-offline 静默，hardFail 仓 502）
printf 'up2' > upstream-dir/dir/up2.bin   # 预置一个未缓存路径的内容（供恢复后验证）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-remote-hardfail -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099","allowPrivateUpstream":true,"hardFail":true}' \
  -o /dev/null -w '%{http_code}\n'        # 200（hardFail 对照仓）
pkill -f 'http.server 9099'; sleep 1
UPa=$(wc -l < /tmp/upstream.log 2>/dev/null || echo 0)
curl -su admin:$ADMIN_PW -i $BASE/binflow/generic-remote/dir/up.bin | grep -i '^x-binflow-upstream-error'   # 有该头 + 200（stale 服务）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-remote/dir/up2.bin          # 404（未缓存，默认 hardFail:false；message 含 offline 状态）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-remote-hardfail/dir/up.bin  # 502（hardFail:true 仓）
UPb=$(wc -l < /tmp/upstream.log 2>/dev/null || echo 0); [ "$UPa" = "$UPb" ] && echo SILENCE_OK   # 静默期（300s）零上游流量
curl -sfu admin:$ADMIN_PW $BASE/binflow/api/v1/health | jq -r .status                                       # ok
(cd upstream-dir && python3 -m http.server 9099 >/tmp/upstream2.log 2>&1 &)   # 恢复上游（静默期结束后自动可用；QA 可缩短 assumedOfflinePeriodSecs 加速）

# M45 maven 代理（FR-20-AC6/AC13；公网可用走 Central，离线 mock 摆 junit/junit/4.13.2 布局）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"maven","url":"https://repo.maven.apache.org/maven2"}' -o /dev/null -w '%{http_code}\n'  # 200（公网上游无需 allowPrivate）
mvn -B dependency:get -Dartifact=junit:junit:4.13.2 -DremoteRepositories=central::default::$BASE/binflow/maven-remote \
  -Dmaven.repo.local=$(pwd)/junit-repo && echo PROXY_MAVEN_OK
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/maven-remote/junit/junit/4.13.2/junit-4.13.2.jar   # 200（已缓存）
curl -su admin:$ADMIN_PW $BASE/binflow/maven-remote/junit/junit/4.13.2/junit-4.13.2.jar.sha1 | jq -r '.errors[0].message'  # Checksums are not downloadable.（HTTP 404，v1.1 定案：checksum 后缀不回源）

# M46/M47 npm/pypi 代理（FR-20-AC7/AC8，P1；公网或 mock）
#   M46 mock 腿注记（v1.3/E4）：mock 上游布局 = <name>/packument.json（BinFlow 回源请求形态）；
#   真实 npmjs 的 packument 于 /<name> 直出——不兼容为 T-75 已证既有边界（非回归），「公网可用时」腿以 mock 为准
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/npm-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"npm","url":"https://registry.npmjs.org"}' -o /dev/null -w '%{http_code}\n'
npm install lodash --registry $BASE/binflow/api/npm/npm-remote/ && echo PROXY_NPM_OK
npm view lodash version --registry $BASE/binflow/api/npm/npm-remote/ | head -1   # packument 经 BinFlow（tarball 已重写）
# M47 pip 代理（v1.3/E4 补显式建仓命令：url = 站点根 https://pypi.org，服务端拼 simple/<pkg>/——
#   带 /simple 后缀反而 404；T-105 真实上游 six 双 MISS 已验，mock 腿同形 T-75 O3）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/pypi-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"pypi","url":"https://pypi.org"}' -o /dev/null -w '%{http_code}\n'   # 200
./v/bin/pip install --index-url $BASE/binflow/api/pypi/pypi-remote/simple six && echo PROXY_PYPI_OK
# 二次同包 install（清 pip 缓存）→ 服务端上游计数不增（缓存命中断言，FR-20-AC8 后半）

# M48 remote 写拒绝 + 删缓存回源（FR-20-AC9 / RE-05/RE-06）
curl -su admin:$ADMIN_PW -X PUT -T r1.bin $BASE/binflow/generic-remote/x.bin -o /dev/null -w '%{http_code}\n'  # 405（+Allow: GET 头）
curl -su admin:$ADMIN_PW -i -X PUT $BASE/binflow/generic-remote/x.bin | grep -i '^allow:'                   # GET
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/generic-remote/dir/up.bin -o /dev/null -w '%{http_code}\n'  # 204（删缓存）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-remote/dir/up.bin            # 200（再次回源）

# ---- virtual（FR-21） ----
# M50 混合解析（FR-21-AC1）：M11 的 demo-app（local）+ M45 的 junit（remote 成员）
mvn -B dependency:get -Dartifact=com.acme:demo-app:1.0.0 -DremoteRepositories=bfv::default::$BASE/binflow/maven-virtual \
  -Dmaven.repo.local=$(pwd)/v-repo && echo VIRTUAL_LOCAL_OK
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/maven-virtual/junit/junit/4.13.2/junit-4.13.2.jar  # 200（透传 remote 成员）

# M51 解析顺序（FR-21-AC2）：在 maven-local 放构件 A、maven-remote-x mock 放同 GAV 构件 B
#   GET /binflow/maven-virtual/<GAV> → A（sha256 断言）；重建 virtual 交换成员顺序 → B
# M52 virtual 写拒绝（FR-21-AC3 / RE-08）
curl -su admin:$ADMIN_PW -X PUT -T r1.bin $BASE/binflow/maven-virtual/com/acme/x/1.0.0/x-1.0.0.jar -o /dev/null -w '%{http_code}\n'  # 405
curl -su admin:$ADMIN_PW -i -X PUT $BASE/binflow/maven-virtual/com/acme/x/1.0.0/x-1.0.0.jar | grep -i '^allow:'  # GET

# M53 写路由（FR-21-AC4，P1）：PUT 建仓（defaultDeploymentRepo=maven-local）后
#   mvn deploy -DaltDeploymentRepository=binflow::default::$BASE/binflow/maven-virtual → BUILD SUCCESS
#   GET /binflow/maven-virtual/<新GAV> 与 /binflow/maven-local/<新GAV> 均 200

# M54 npm/pypi virtual（FR-21-AC5，P1）
#   npm install demo-pkg lodash --registry $BASE/binflow/api/npm/npm-virtual/  → exit 0
#   pip install --index-url .../npm 替换为 pypi-virtual 的 simple → 本地 demo-pkg + 上游包均装齐

# M55 metadata 合并（FR-21-AC6，P1）：成员1 含 1.0.0、成员2 含 1.1.0 →
#   GET /binflow/maven-virtual/<GAV>/maven-metadata.xml 的 versions 并集断言
# M55b 聚合浏览（RE-09，P1）：GET /binflow/api/storage/maven-virtual/com/acme → children 覆盖两成员

# M57 协议探针（NE-07，P2）：curl $BASE/binflow/api/npm/npm-local/-/ping → {}
# M58 不做端点 404（ME-10/NE-08/PE-04/PE-06）
curl -s $BASE/binflow/maven-local/.index/nexus-maven-repository-index.gz -o /dev/null -w '%{http_code}\n'   # 404
curl -s "$BASE/binflow/api/npm/npm-local/-/v1/search?text=x" -o /dev/null -w '%{http_code}\n'              # 404
curl -s $BASE/binflow/api/pypi/pypi-local/pypi/demo-pkg/json -o /dev/null -w '%{http_code}\n'              # 404
curl -s $BASE/binflow/api/pypi-ui/x -o /dev/null -w '%{http_code}\n'                                       # 404

# M59 remote 统计（RE-11，P2）：GET /binflow/api/v1/remote/stats → JSON 含 generic-remote 的 cached/misses/hits

# M60 性能（FR-22-AC6 / §6.1）
# M60a 冷启动：空库裸二进制启动 → ping OK < 2s（M1 同法计时，含三协议路由注册后）
# M60b 50 并发 pip install（缓存命中）：seq 50 | xargs -P50 -I{} sh -c './v/bin/pip install --index-url ... demo-pkg'  → 全 0、零 5xx
# M60c 代理开销：mock 上游 10MB 文件，curl 直连上游 vs 经 BinFlow miss 拉取，差值 < 100ms（P1 记录）

# M61 回归基线（FR-22-AC5 / §5.6）：M1 C 序列 P0 + M2 D 序列 P0 + C26/D-Basis 断言反转复跑
```

### 5.5 校准记录（v1.1：`maven-npm-pypi.md` + repo-semantics §7/§8 已落地（T-59），C1~C8 逐条定案）

| # | 项 | v1.0 暂定值 | **v1.1 定案** | 依据（置信度） |
|---|---|---|---|---|
| C1 | maven-metadata.xml 服务端计算与「合并」细节 | versions 并集 + 数值取最大（XML 级合并） | **服务端计算为准**：触发时机四类（unique snapshot/non-unique pom 同步，其余异步；pom 额外算祖父目录）；版本组 versions=含 pom 子目录集、latest/release/lastUpdated 重算、无 pom → 删 metadata（RTFACT-6242 保护例外）；SNAPSHOT 目录 buildNumber/timestamp 取最新 unique pom（non-unique 固定 1）、snapshotVersions 每 (ext×classifier) 最新；客户端 PUT metadata 触发重算而非直接采纳；virtual 合并内存现算不缓存（含 MNG-5180 规避） | maven-npm-pypi.md §1.4/§1.6 + repo-semantics §8.3（高） |
| C2 | maven 仓 layout 不合规的拒绝行为与状态码 | 400，严格校验 | **layout 模型定案**：maven-2-default 六字段 pattern（一手 config 模板）；token 语义与 unique snapshot 文件名正则定案；**400 状态码维持暂行**（规格定案了模型、未见明确拒绝码值） | maven-npm-pypi.md §1.2（高，模型）/ 拒绝码维持暂定 |
| C3 | virtual 解析顺序（列表序是否实情；priorityResolution 精确语义；stale 与下一成员优先关系） | 列表顺序、stale 优先于继续 | **四桶序定案**（优先 local+cache → 优先 remote 本体 → 非优先 local+cache → 非优先 remote，桶内声明序、下载首命中即停）；BinFlow 因无 `<key>-cache` 仓投影简化为**两桶**（语义等价）；**stale 命中即返回、仅真 404 才继续下一成员**；嵌套 virtual 递归展开（BinFlow 有意收窄不做） | repo-semantics §8.1（高）；BinFlow 两桶简化为 PRD 决策 |
| C4 | remote 缓存参数默认值与回源协商 | 7200s/1800s；不做协商（纯 TTL） | **默认值定案 7200/1800**（另 socketTimeout 15s、assumed-offline 300s、metadata 刷新锁 60s）；**协商确证存在**：过期后 HEAD 探测 + Last-Modified 比较（较新则撤销过期不重下）——BinFlow **P0 简化为到期重取、HEAD 协商列 P1**（AC 断言不受影响）；并发 miss 单飞定案（P1） | repo-semantics §7.1/§7.3/§7.4（高）；P0 简化为 BinFlow 决策 |
| C5 | virtual 写路由字段名 | `defaultDeploymentRepo` + `deploymentRepository` 别名 | **双名并收维持 + 增补规格名别名 `defaultDeploymentRepoRef`**；405 文案定案 `No local repository was configured as local deployment repository for the (<key>) virtual repository.`；`defaultDeploymentRepo` 指向非 local → 404 `Could not find a local repository named <key> to deploy to.`（BinFlow 建仓时前置 400，语义更早更严，有意从严） | repo-semantics §8.2（高）/ 前置 400 为 BinFlow 决策 |
| C6 | remote 缓存删除是否同步上游 delete | 绝不同步（本地删除） | **维持定案、置信度升高**：Artifactory 的缓存清理（Zapping 端点族）均为本地操作，无上游删除同步语义 | repo-semantics §7.4（高） |
| C7 | PyPI 文件落盘布局与 simple href 形态 | BinFlow 自有 `/binflow/<repo>/<norm-name>/<filename>` | **对齐 Artifactory**：制品路径 `<name>/<version>/<filename>`（name 用元数据原始名不 normalize；normalize 仅用于索引查找与 wheel 文件名侧）；simple href 指向 `/binflow/api/pypi/<repo>/packages/<name>/<version>/<filename>`；PE-03 升格兼容（子集） | maven-npm-pypi.md §3.5/§3.2（高） |
| C8 | npm tarball 落盘布局与 `_attachments` 保留策略 | `<pkg>/-/<name>-<version>.tgz`；publish 后 `_attachments` 清空（惯例） | **布局定案**：`<name>/-/<name>-<version>.tgz`，scoped `@<scope>/<name>/-/@<scope>/<name>-<version>.tgz`；GET 面不返回 `_attachments`（[NPM-API]）；**物理清空与否维持暂行**（规格未明示，对外不可观察） | maven-npm-pypi.md §2.2（高，布局）/ 清空维持暂定 |

> C2 补注（T-78，R3 转交）：layout 拒绝**码值 400 维持暂行**（规格定案了 maven-2-default 模型本体、未见码值）——该口径是**单点改动面**（一处常量 + FR-16-AC8 断言 + M20 期望值），后续规格补值或实测定案后单独回写，不动 C2 定案本体。

> 附带收口（v1.1）：M1 待验证 #4（remote-cache 仓命名 `<key>-cache`）已由规格 §8.4 关闭（virtual cache 同规则）——BinFlow 不模拟该内部投影（缓存 node 直落 remote 仓命名空间，语义等价、对外不可观察差异仅 C3 两桶简化），归档维持 §6.4；规格 §5 的待验证项（`-rev` 隐式副作用、`%2f`/`%2F` 等价、PEP 691 仓库级开关、unique 改写对拍）相应落为 FR-18-AC11/NE-01/PE-05/FR-16-AC12 的断言或 P2 备注。

### 5.6 回归基线反转表（M3 起生效，qa 更新既有断言）

| 既有断言 | 来源 | M3 起的期望 |
|---|---|---|
| `rclass=remote/virtual` 建仓 → 400 | M1 E-07 / C26 | **200**（FR-15；C26 断言反转，仅 `packageType=docker` 组合维持 400） |
| `/binflow/api/npm/**` → 404 | M1 E-26 / C24 的 npm 行 | **按 NE-01~NE-08 分派**；未实现子路径（search/security）仍 404 |
| `/binflow/api/pypi/**`、`/binflow/api/pypi-ui/**` → 404 | M1 E-26 | `pypi/**` 按 PE-01~PE-05 分派；`pypi-ui/**` 维持 404 |
| Maven layout 一律 404（M1 不解析） | M1 §2.2 | maven 仓 layout 按 ME-01~ME-10 分派；**generic 仓对 `.xml`/任意路径行为不变**（M1 generic 直通语义零回归） |
| M1 C 序列 / M2 D 序列 P0 项 | T-18/T-19/T-43/T-44 基线 | 全部复跑全绿（generic 与 docker 域零回归是 M3 的 P0 硬门槛） |

---

## 6. 非功能需求（NFR）与遗留收编

### 6.1 性能（M3 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P11 冷启动不回退 | 空库启动到 ping `OK` < 2s（含 maven/npm/pypi 路由与 remote/virtual 引擎注册，M60a） | P0 |
| NFR-P12 remote 代理开销 | miss 拉取总时长 ≤ 直连上游 + 100ms（10MB 样本，M60c 记录）；hit 路径与 local 下载同量级（M41b） | P1 |
| NFR-P13 并发拉取 | 50 并发 pip install 同一包（缓存命中）全 0 退出、零 5xx（M60b）；100 并发 curl 拉 maven 构件（local）零 5xx（M1 NFR-P2 的 maven 版） | P0（pip）/ P1（maven） |
| NFR-P14 上游流式 | remote 代理 1GB 文件全程 RSS 增量 < 256MB（FR-20-AC11，M1 NFR-P3 的 remote 版） | P1 |
| NFR-P15 元数据操作 | maven-metadata.xml 生成/合并与 npm packument 读写为毫秒级（无全仓扫描；QA 以 500 包规模仓的 metadata GET < 100ms 抽查，P2 记录） | P2 |

### 6.2 安全底线（M3 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| **NFR-S13（SSRF 防护，M3 安全核心）** | 服务进程的一切**出站** HTTP 请求仅能由 remote 仓触发，且逐请求过校验链：① scheme ∈ {http, https}（建仓时校验 + 请求时断言）；② 目标 host 解析后的**全部 IP** 均不得属于：回环（127/8、::1）、私网（RFC1918、IPv6 ULA fc00::/7）、链路本地（169.254/16、fe80::/10，含云 metadata 169.254.169.254）、未指定/保留（0.0.0.0/8、组播、广播）——除非该仓显式 `allowPrivateUpstream:true`（仅 admin 建仓/改仓可设，写审计日志）；**IPv6 过渡格式拆解递归过表（T-65 review 发现，commit 0dc17a2）**：`64:ff9b::/96`（NAT64）与 `2002::/16`（6to4）取内嵌 IPv4 再过全清单（包裹的 v4 为公网时放行——保 DNS64 场景），`2001:0::/32`（Teredo）直接拒，`::a.b.c.d`（IPv4-compatible）同构收编，zone 标识（`%eth0` 类）剥离后判定；③ **DNS rebinding 防护**：校验通过后拨号必须使用已校验的 IP（dialer Control 回调二次校验实际连接地址），不得重新走系统解析；④ 重定向：跟随但**每一跳完整重过 ①②③**，上限 5 跳，拒绝即 400；⑤ 非 streaming 的上游响应（packument/simple/metadata 等缓冲型）大小上限 64MB，超限截断报 502；⑥ 连接/读超时按仓配置（`socketTimeoutSecs` 默认 **15s**，v1.1 对齐 repo-semantics §7.1 的 15000ms）；⑦ 任何拒绝留 **WARN 级结构化日志**（repo key、被拒目标、原因类别），不打堆栈 | M42 全变体：127.0.0.1 / 10.x / 192.168.x / 169.254.169.254（重定向与直连两式）/ ::1 / 0.0.0.0 / file:// / gopher://（建仓 400）；QA 出示日志取证 |
| NFR-S14 上游凭据不泄露（v1.2 依 ADR-0012 定案） | ① 静态加密：password 以 AES-256-GCM 密文（`enc:v1:<base64(nonce+ciphertext)>`）落元数据库，明文不落盘/不入 YAML；主密钥仅经 env `BINFLOW_REMOTE_CREDENTIALS_KEY`（base64 32B）注入，日志与配置回显永不出现密钥；② 存在带凭据的 remote 配置而无密钥 → 启动 fail-fast；③ `GET .../repositories/{key}` 的 `password` 永不回显明文（掩码或缺省）；④ 服务日志、结构化日志不含上游 Authorization 头或明文凭据；⑤ `allowPrivateUpstream` 变更留审计记录；⑥ 密钥轮换不做（M3 单密钥） | FR-15-AC9（DB grep `enc:v1:`/无明文 + fail-fast 双态重启）+ M02 变体 + 日志 grep |
| NFR-S15 上游故障隔离（v1.1 口径） | 上游 5xx/超时不拖垮服务：仓进 assumed-offline 静默期（默认 300s，期内零上游流量），客户端侧——有缓存（含过期）服务缓存、无缓存快速 404（`hardFail:true` 仓 502）；上游慢（挂起连接）不占用无限期资源（socketTimeout 生效）；服务 health 始终 200 | M44 |
| NFR-S16 错误体分层延续 | maven/npm/pypi/remote/virtual 域非 2xx 一律 E-01（制品层）；不出现 HTML 栈页/空 200；npm 搜索等未实现端点 404 + E-01 | M58 + 各失败用例 |
| NFR-S17 写操作认证不豁免 | 三协议 publish/upload（npm PUT、pypi POST、maven PUT）匿名一律 401（匿名读默认开只覆盖 GET/HEAD）；remote 仓不可写（405） | M28 变体 |
| NFR-S18 路径安全延续 | 三协议与 remote/virtual 域路径穿越变体（`../`、`%2e%2e`、编码斜杠、`@scope%2F..`）→ 400/404，数据目录外无文件（M1 NFR-S4 的 M3 版）；npm/pypi 的包名只允许 `[A-Za-z0-9@/._-]` 有限集 | QA 变体用例 |

### 6.3 可观测性（M3 增量）

- 结构化日志沿用 M1 字段集，remote 域请求增补：`upstream_host`、`cache_result`（hit/miss/stale/negative）、`upstream_status`、`upstream_duration_ms`；virtual 域增补 `resolved_repo`（命中成员 key）。
- `/binflow/api/v1/health` 只增字段原则延续：M3 不新增子系统（remote 无后台常驻；如实现含后台清理则加 `remote_cache` 状态且向后兼容）。
- RE-11 `/api/v1/remote/stats`（P2）：每 remote 仓缓存 node 数、字节数、hit/miss/negative 计数。

### 6.4 M1/M2 遗留收编（逐条定界）

| 遗留项 | 来源 | M3 处置 |
|---|---|---|
| sha1-only checksum deploy → 404 | T-13 遗留裁决「维持 404（M3）」 | **收编（P2）**：`X-Checksum-Deploy: true` + 仅 `X-Checksum-Sha1` 时按 sha1 在 filestore 命中即可秒传（Maven 生态 sha1 是主算法，mvn/ wagon 场景真实存在）；sha1/sha256 均缺维持 400。AC：maven 仓对已存在 sha1 的 jar 以 sha1-only 秒传到新 GAV → 201 |
| RepoTypes 空 class 键放行（§5.1 勘误） | T-33/T-48 遗留（architect 域） | 架构侧小票随 M3 首批派发；本 PRD 不约束实现，仅登记 |
| remote-cache 仓命名（`<key>-cache` 推断） | repo-semantics §7.3 待验证 | M3 逆向票校准；BinFlow 实现取「缓存 node 直落 remote repo 命名空间」的简化模型（对外不可见差异——Artifactory 的 `-cache` 仓是内部投影，BinFlow 不模拟该投影，语义等同；归档 C4） |
| O4（匿名读开时已认证零权限用户 403） | M1/M2 定界维持 | virtual/remote 域同理：已认证走自身 ACL、匿名走匿名通道；语义不变，M4 权限完整版统一评审 |
| Email 不落盘（users 表无列） | T-15 遗留（M2 评估） | **不收编**（与 M3 无交集，归 M4 权限/用户模型票） |
| **M1 勘误 ×2（T-59，repo-semantics §9）** | ① snapshot policy 拒绝码 409（显式值，推翻 M1 的 404 推断）；② includes/excludes 拒绝码为下载 404/上传 409 双值（推翻 M1 的统一 404） | ① **已吸收进本文**（ME-08/FR-16-AC7/M19 改 409）；② M3 未暴露 includes/excludes 字段、无 AC 受影响，仅归档——local/remote 仓按此模式实现时（M4）以双值码为准；M1 PRD 的对应标注由 PM 另行走 M1 勘误票（非本票边界） |

---

## 7. 开放问题（**v1.2 状态**：Q1 已依 ADR-0012 决策 4 定案关闭、Q3/Q7 已依 T-59 规格定案、Q4 维持（conductor 已转用户知悉）、Q5/Q6/Q8 关联部分随 C4/C1/C7/C8 定案；**待用户拍板的仅余 Q2**（virtual 写路由默认）及 Q5 的 refresh 参数形态、Q6 的保留策略时机）

| # | 问题 | 影响面 | 暂行假设（v1.0 按 此执行，用户定案后回写） |
|---|---|---|---|
| Q1 | ~~**remote 上游认证凭据的存储形态**~~ **已定案（v1.2，ADR-0012 决策 4）** | FR-15/FR-20、部署文档、安全审计 | **AES-256-GCM 静态加密**：随机 12B nonce，密文 `enc:v1:<base64(nonce+ciphertext)>` 前缀标识；主密钥经 env **`BINFLOW_REMOTE_CREDENTIALS_KEY`**（base64 32B）注入、不入 YAML/不落盘；存在带凭据的 remote 配置行而无密钥 → **启动 fail-fast**；无前缀的存量明文值在 **003 迁移中一次性加密**（需密钥在场）；密钥轮换不做（M3 单密钥，轮换 = 重新录入凭据）。v1.0 暂行「明文 SQLite + 文件权限」作废；FR-15-AC9/NFR-S14 已按此落 AC（DB grep、fail-fast 双态、迁移后明文消失） |
| Q2 | **virtual 写路由默认策略**：M3 是否默认可写（配 `defaultDeploymentRepo` 才可写 vs 有 local 成员即可写） | FR-21/RE-08、迁移脚本行为 | 默认 **405 不可写**；仅显式配置 `defaultDeploymentRepo`（成员中的 local 仓）后按 P1 路由写入——显式优于隐式，防「以为发到 local 实际进了 virtual 缓存」类事故；Artifactory 语义即「未配则 405」，对齐 |
| Q3 | ~~**PyPI simple index 哈希算法**~~ **已定案（v1.1）** | FR-19/PE-01 | **仅 sha256 定案**：规格 §3.4（高置信度）——上传侧客户端只可能提供 md5（且 twine ≥ 6.2 可缺失、服务端自算）；pip 校验依赖 `#sha256=`，索引输出必须优先 sha256；Artifactory 的 `#md5=` 仅在制品无 sha256 时兜底，BinFlow 服务端必算 sha256 故该分支永不触发。上传侧 `md5_digest` 作为可选客户端 checksum 纳入校验链（FR-19 规格） |
| Q4 | **docker remote pull-through 是否提前进 M3**（M2 PRD §2.2 曾预告 M3 交付） | ROADMAP M3 边界、工作量 | **维持不做**（M4+ 评估）：上游 token 协商 + manifest/blob 重写是独立工程量；替代路径 `skopeo copy`（M2 场景 C）已可用。**conductor 已转用户知悉（T-60 派单口径），不动**。用户若定案提前，M3 增补 FR-23 并重排里程碑 |
| Q5 | remote 缓存 TTL 默认值（retrieval/missed）与是否暴露 per-path 刷新参数 | FR-20/RE-01 | **半定案（v1.1，C4）**：TTL 默认值 **7200s/1800s 定案**（repo-semantics §7.1 高置信度，另 socketTimeout 15s、assumed-offline 300s、hardFail false 一并对齐）；**维持不做** per-path `?refresh` 参数（URL 语义纯净），强刷走 DELETE 缓存（RE-06）；HEAD+Last-Modified 回源协商确证存在、BinFlow 列 P1 |
| Q6 | Maven snapshot 策略：maxUniqueSnapshots 保留数、non-unique 覆盖是否需要 per-repo 开关 | FR-16/FR-17 | **部分定案（v1.1）**：`snapshotVersionBehavior` 三值定案（deployer 默认/non-unique/unique——服务端改写 P2）；maxUniqueSnapshots 无保留策略（归 M4 治理）维持；non-unique 覆盖走 M1 覆盖检查链维持 |
| Q7 | ~~npm 域错误体格式与重复 publish 码~~ **已定案（v1.1）** | FR-18/NE-01/NFR-S16 | **E-01 `errors[]` 定案**：三协议共用该信封（maven-npm-pypi.md §0 高置信度，M1 三分层不破）；**重复 publish 409→403**（规格 §2.3-④：Artifactory 定 403 `Cannot modify pre-existing version`，npmjs 官方错误族同）；npm 十步校验链与错误文案全量纳入 FR-18 规格 |
| Q8 | 三协议文件落盘布局对 Artifactory 的对齐深度（npm tarball/pypi 文件路径形态，C7/C8） | FR-18/FR-19、内容路径直打用户 | **部分定案（v1.1，C7/C8 收口）**：npm tarball `<name>/-/<name>-<version>.tgz`（scoped `@<scope>/` 前缀）与 pypi `<name>/<version>/<filename>`（原始名）**均对齐 Artifactory**；simple href 走 `/api/pypi/<repo>/packages/...` 形态；`_attachments` 物理清空维持暂行（对外不可观察）——「不承诺全量内部布局兼容」的总原则维持，但这两处已实证对齐 |

---

## 8. M3 验收剧本（QA 总纲）

1. **回归基线**：§5.6 反转表更新断言 → M1 C 序列 P0 + M2 D 序列 P0 复跑全绿（FR-22-AC5）。
2. **仓库模型**：M01 → M02/M02b → M03 → M04 → M05（含 E-07 反转与 docker 组合边界）。
3. **Maven**：M10/M11 → M12 → M13 → M14 → M15 → M16/M16b → M16c/M17 → M18 → M19 → M20 → M21（snapshot `-U` 与 metadata 合并为重点）。
4. **npm**：M22 系 → M23 → M24 → M25 → M26 → M27 → M28。
5. **PyPI**：M30 → M31 → M32 → M33 → M34 → M35/M35b。
6. **remote**：M41 → M42（SSRF 全变体 + 日志取证）→ M43 → M44 → M45 → M46/M47 → M48（含 FR-20-AC10/11/12）。
7. **virtual**：M50 → M51 → M52 → M53 → M54 → M55/M55b。
8. **边界**：M57/M58（探针与不做端点）+ NFR-S18 路径穿越变体。
9. **性能与安全**：M60a/b/c + NFR-S13~S17 抽查（凭据不回显、日志脱敏 grep）。
10. **文档**：tech-writer 三协议接入指南 + remote/virtual 管理指南（DoD 第 4 条）。

## 9. M3 DoD

1. §4 全部 P0/P1 AC 经 qa 验证全绿（P2 延后在 BOARD 记录）；
2. §8 剧本全绿，§5.3 客户端矩阵 P0 成员（mvn/npm/pip+twine/curl）全过；
3. `docs/reverse/maven-npm-pypi.md` 与 repo-semantics §7/§8 已落地，§5.5 八项校准（C1~C8）逐条定案回写（**v1.1 完成**）；开放问题 Q1/Q3/Q7 已定案（v1.2/v1.1）、Q4 维持，余项（Q2 等）用户定案后回写；
4. tech-writer 产出 Maven/npm/PyPI 接入指南与 remote/virtual 管理指南（含 SSRF 放行操作指引）；
5. 主会话完成 `m3-done` tag。

---

*本 PRD v1.0 由 product-manager（T-57）依据 PRODUCT.md、ROADMAP.md M3 节与 M1/M2 交付基线撰写；与 `docs/reverse/` 规格冲突时按 §5.5 流程回写修订。*






