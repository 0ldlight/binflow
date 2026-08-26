# TL 验收报告：FR-91-AC3 — M10 五份协议行为规格「可直接拆票」确认

- 验收人：tech-lead；日期：2026-08-26；票据：FR-91-AC3。
- 输入：`docs/reverse/{conan,cargo,debian,rpm,helm}.md`（T-284 批：conan/cargo/debian；T-292 批：rpm/helm）。
- 验收口径：六要素（端点表 / layout / 上传下载校验链 / rclass 三态 / 真实客户端命令 / 置信度标注）逐项能直接转化为验收命令或 AC 条款；规格自评裁决点逐点裁决；缺项要么为零、要么登记 M11 前置票。
- 代码现状核对（裁决依据，均为只读）：
  - `server.base_url` 配置已存在（`internal/config/api.go:49`："externally visible URL; empty = derive from request"），npm / nuget / docker adapter 已有 `Options.BaseURL` 注入先例（`internal/adapter/{npm/api.go:29, nuget/handler.go:46, docker/api.go:49}`）。
  - `apiProtocolMounts` 为封闭清单（`internal/httpapi/router.go:729`，现 npm/pypi/nuget），注释明确「a protocol earns a mount only through its own ticket」；管理 REST 走 `dispatchAPI` 显式路由族（router.go:306 起，system/v1 各 family）。
  - adapter 分发按 `package_type` 注册（`internal/adapter/registry.go`，`Register(h)` + `ForRepoType`），新协议 = 新子包 + 一个 Register，扩展点明确。

## 0. 总体结论

| 规格 | 六要素 | 结论 | 阻塞缺项 |
|---|---|---|---|
| conan.md | 6/6 | **可拆** | 无 |
| cargo.md | 6/6 | **可拆（T-294 ready，见 §3）** | 无 |
| debian.md | 6/6 | **可拆** | 无（keypair 已登记 K-1 条件前置票，unsigned 模式不阻塞） |
| rpm.md | 6/6 | **可拆** | 无（keypair 同 K-1） |
| helm.md | 6/6 | **可拆** | 无 |

**AC3 判定：通过。** 缺项清单非空但全部已登记（1 张条件前置票 K-1 + 2 条 ADR 补记 + 2 条规格建议修订），无阻塞项。

## 1. 逐份核验

### 1.1 conan.md（216 行，T-284）— 可拆

六要素：§3.1 v2 端点表 17 行（方法/路径/语义/成功/错误码/置信度全齐，PUT 201 / DELETE 200 / 404 文案均有）；§4 布局含 index.json 结构与排序规则；§5 修订语义 + §6 校验链（含 checksum-deploy 404 透传）；§7 rclass 三态；§8 conan 2.x 命令 L-c1~L-c5（v1 握手硬依赖已覆盖）；置信度列贯穿全表，S1–S12 差异清单带来源。

拆票可操作性：
- v2 主票面可直接照 §3.1 逐行写 AC；`<ref>` 语法、`_` 占位约定（S12）、revisions 降序、index.json 同构响应体都够写 table-driven 用例。
- 拆分建议：**v2 local 全量一张 P0**（含 §4 布局 + index.json 修订索引 + `.timestamp`）；v1 握手三端点并入该票（量小且是 conan 2 硬依赖）；remote pull-through P1；virtual 归并 P1（S11 中置信，先做「time 归并 + files 并集 + first-found」最小实现，AC 用 L-c3 install 链兜底）。
- 依赖：能力头族（§2）响应头注入是全端点横切，票面要单列 AC；管理面 reindex 两端点按裁决 C-1 走 dispatchAPI。
- 建议修订：无（v1 files 通道裁撤是裁决 CN-1，规格 §8 已预留该裁决位）。

### 1.2 cargo.md（205 行，T-284）— 可拆，T-294 直接可派（详见 §3）

六要素：§2 端点表（含 git 弃用闸门 404、错误信封形态）；§4 布局（crates/ + .cargo/ 长元数据 + index/）；§5 publish wire 逐字节双证 + §5.3 校验链（cksum 闭环是天然 AC 对账点）；§8 rclass；§9 L-r1~L-r7；置信度贯穿。

拆票可操作性：五份中最好拆的一份——端点面窄（8 个路由）、无签名依赖、无坐标属性歧义、config.json 合成规则明确。缺口仅两处微观项（yank 404 语义、config.json base 来源），均已在本报告裁决（CG-4、TL-1），随规格下次修订合入即可，不阻塞 T-294。

### 1.3 debian.md（239 行，T-284）— 可拆

六要素：§6 内容面端点表 + §4.3 管理面表；§2 布局两型 + §2.3 坐标属性体系；§3 debPUT 链（矩阵参数示例可直接当 AC 命令）+ §7 apt 端到端校验链；§8 rclass；§9 L-d1~L-d7（含 By-Hash 断言脚本与 auth.conf.d 认证）；置信度贯穿，S1–S12。

拆票可操作性：
- 拆分建议：**automatic local 主票 P0**（debPUT + 增量索引 + Release 生成 + 压缩集 + By-Hash + unsigned 模式）；**trivial 布局 P2 增量票**（裁决 TL-7）；virtual stanza 聚合 P1；remote 归一化 pull-through P1。
- 依赖：坐标属性多值登记与属性变更触发重算依赖 FR-89 属性系统（已落）；GPG 签名依赖 K-1（条件票），unsigned 模式 `[trusted=yes]` 验收闭环（L-d3）。
- 建议修订：无。

### 1.4 rpm.md（247 行，T-292）— 可拆

六要素：§3.1 内容面表 + §3.2 reindex 状态码矩阵（全份最完整的错误码面：400/403/404/409/202/200 七分支带文案，可直接 table-driven）；§2 布局（repodata 命名、压缩与算法形态、_tmp_ 原子性、代数保留、.rpmcache、yumRootDepth）；§4 上传重算链 + §5 校验链（repomd checksum/open-checksum 实测义务明确）；§6 rclass + §6.3 virtual 三层；§7 L-r1~L-r7；置信度贯穿，S1–S14。

拆票可操作性：
- 拆分建议：**local 管线 P0**（RPM header 解析器单列子任务、重算链、comps 组文件、sha256 默认、unsigned）；modules.yaml 透传 P2（S7 中置信、面窄）；remote 可过期集合 P1；virtual 聚合 P1（按裁决 RP-3 简化）。
- 依赖：RPM header 解析器（§2.4，redline 等价物）是本包最大自研件，票面要给 tag 集清单（规格已列 PROVIDE/REQUIRE 六组五元组）——够写 AC；keypair 同 K-1。
- 建议修订：无。

### 1.5 helm.md（251 行，T-292）— 可拆

六要素：§2 经典仓端点表（含 namespace 变体与 404 文案）+ §8.2 HelmOCI 复用映射表（对 docker-registry.md 逐端点对照）；§3 布局（含虚仓 `.index` 缓存键结构）；§4 事件链 + §5 index.yaml 生成细节 + §8 OCI 校验面；§6/§7 rclass；§9 L-h1~L-h7（含 `--verify` 与 enforce layout 403 断言）；置信度贯穿，S1–S14。

拆票可操作性：
- 拆分建议：**经典仓 local P0**（PUT 链 + index.yaml 生成 + enforce layout 两开关 + reindex 两端点）；virtual 聚合 + URL 改写 P1；remote 代理（chartsBaseUrl/_external/_transitive）P1；**HelmOCI 单列 P1**（裁决 HL-3：docker adapter 的 package-type 分发 + helm media type 入 manifest 类型映射，与 docker 面联调）。
- 依赖：无 keypair 依赖（`.prov` 只是普通文件存储）——五份中最轻。
- **建议修订（1 条，规格内部不一致）**：§7.3 URL 改写算法中 remote 成员条目改写目标写的是 `<base>/api/helm/<virtualKey>/<路径>`，与 §1.1「主面 = 内容面、api/helm 仅为别名」的定案矛盾。应改为以主面 `<base>/<virtualKey>/<路径>` 为改写目标（api/helm 别名只读、不进 index URL），否则聚合 index 里的下载 URL 与主面不一致。请 reverse-engineer 合入。

## 2. 裁决点逐点裁决

### 表 A：规格自评裁决点（实点 16 个——debian §12 实列 4 项，FR-91 任务清单记 3，此处按实点全裁）

| # | 来源 | 裁决点 | 裁决 | 分类 |
|---|---|---|---|---|
| CN-1 | conan §11① | v1 实现子集边界（规格建议：握手三端点 + files 通道） | **改判（收紧）**：M11 仅 `ping` / `users/authenticate` / `users/check_credentials` 三端点（conan 2.x 硬依赖，AC 必测）；v1 `files` 直传通道**不做**（仅 conan 1.x 上传流使用，已 EOL），其余 v1 数据面按未知路由 404。qa 若提出 conan 1.x 兼容诉求再立项 | 改判 |
| CN-2 | conan §11② | upload_urls 类绝对 URL 的 base 来源 | 复用 `server.base_url`（配置优先，空值回退请求 scheme+host 推导）——与 npm packument / nuget service index 现行先例完全一致，零新配置。见 TL-1 | 定案 |
| CG-1 | cargo §12① | 虚仓索引去重（S5 低置信） | **同意规格建议**：按 (name, vers) 首见成员去重。官方 MUST「同 name+version 唯一行」没有留拼接的空间，去重是合规义务不是选项 | 同意 |
| CG-2 | cargo §12② | publish 失败形态：4xx/5xx 为主 + IO 类保留 200+errors | **改判（收紧）**：统一 4xx/5xx + errors 信封，**不做 200+errors 双轨**（含 IO 类 → 500 + 信封）。官方明文两种形态 cargo 均按失败展示，双轨只增加实现与测试面，无兼容收益 | 改判 |
| CG-3 | cargo §12③ | 重复版本（同 name+vers 忽略 build metadata）拒绝码 | **同意规格建议**：409 + errors 信封 | 同意 |
| DB-1 | debian §12① | GPG 签名依赖实例 keypair | M11 先落 **unsigned 模式**（`[trusted=yes]` 验收，签名留接口与开关位）；keypair 体系登记条件前置票 **K-1**（见 §4）。若 K-1 进 M11 排期则 Debian/RPM 签名各补一张小票接上 | 定案+登记 |
| DB-2 | debian §12② | 缺坐标 .deb：400 拒绝 vs 静默存储 | **同意规格建议（400）**，加豁免边界：校验仅作用于**客户端对 local 仓的 .deb/.dsc PUT**；remote-cache 回源写入、replication、move/copy 等系统内路径豁免（indexCached 补算链依赖「先存后补坐标」语义） | 同意+补边 |
| DB-3 | debian §12③ | 索引文件禁客户端直写（规格给 403 或忽略+重算二选一） | **定案 403**：`dists/**/{Release*,Packages*,Sources*,by-hash/**}` 面向客户端 PUT 一律 403（错误文案含路径与「索引由 debPUT 链生成」提示）。显式拒绝可测、可解释；「忽略+重算」会让客户端误以为直写成功 | 定案（二选一） |
| DB-4 | debian §12④ | 管理面路径形态（/binflow/api/deb/... vs 自有 /api/v1） | **定案 `$BASE/binflow/api/<proto>/...`**（Artifactory 同构，直迁用户肌肉记忆），并**推广为五协议统一**（conan reindex / deb reindex / yum reindex / helm reindex 同形）。实现注意：走 `dispatchAPI` 显式路由族（router.go:306 各 family 同模式，权限门 = 认证 + CanManageRepo），**不是** `apiProtocolMounts` 内容面重写（该清单封闭且语义不同）。需 architect 在 DECISIONS.md 补一条 ADR 条目（见 §4 K-3） | 定案（跨规格） |
| RP-1 | rpm §10① | GPG 签名依赖实例 keypair | 同 DB-1：unsigned 模式（`repo_gpgcheck=0` 验收，L-r2）+ K-1 共用；无密钥时删旧 `.asc`/`.key` 的行为照规格 §4.3 保留 | 并入 DB-1 |
| RP-2 | rpm §10② | calculateYumMetadata 默认值（Artifactory false vs 规格建议 true） | **同意规格建议（true）**：BinFlow 上传即建索引，对齐 Debian/Helm 的行为直觉，减少「传了包 dnf 看不见」类工单。保留 reindex 端点 409（auto-async 冲突）分支——那是已验证的 Artifactory 行为，兼容面照搬 | 同意 |
| RP-3 | rpm §10③ | 虚仓聚合最小实现边界 | **同意规格建议**（成员序拼接 + name+arch 仅对优先成员去重 + 聚合 repomd 不签名）**并再收紧**：首版不做 repomd-sha1 缓存键持久化（§6.3② 的整套缓存判定），以进程内 singleflight + 短 TTL 缓存替代；「≥2 成员有 repomd 才聚合」与「同步超时转透传」保留。缓存键机制若实测成瓶颈再立项 | 同意+收紧 |
| HL-1 | helm §12① | 挂载形态（内容面为主 + api/helm 别名） | **同意**：主面 = 内容面 `$BASE/binflow/<repoKey>/...`；`api/helm` 别名通过把 "helm" 加入 `apiProtocolMounts`（router.go:729）实现——这正是该封闭清单注释里「协议凭自己的票赢得挂载」的设计路径。**附带规格修订**：§7.3 改写目标从 `api/helm` 改为主面（见 §1.5） | 同意+修订 |
| HL-2 | helm §12② | relative urls 默认值 | **同意规格建议（true）**：BinFlow 只支持 helm 3（规格 §11.1），relative 模式 index 更小、免实例改域名触发全量 reindex（§4.4 第三条联动直接消失）。absolute 模式不实现，留配置位不暴露 | 同意 |
| HL-3 | helm §12③ | HelmOCI 拆票方式 | **同意**：HelmOCI 单列一张票，两处增量——① repo 行 `package_type=helmoci` 分发：新 handler `Register`（key "helmoci"）内部复用 docker adapter 端点实现（`ForRepoType` 机制现成）；② helm config/layer/prov 三个 media type 入 manifest Content-Type 类型映射。经典仓票与 HelmOCI 票 area 不重叠，可并行。虚仓「Helm 与 HelmOCI 不得混仓」校验放建仓校验（经典仓票 AC） | 同意 |
| HL-4 | helm §12④ | GPG/--verify 依赖（规格自注「比 RPM/Debian 轻」） | **确认无需裁决**：`.prov` 是普通文件存储，不走 keypair 体系。记录在案，K-1 不含 helm | 无需裁决 |

### 表 B：TL 追加裁决（拆票时直接引用）

| # | 裁决 | 依据 |
|---|---|---|
| TL-1 | 全部新协议的对外绝对 URL（cargo config.json 的 dl/api、conan upload/download_urls、helm index urls 若用 absolute 模式、各协议 Location 类响应头）**统一取 `server.base_url`，空值回退请求 scheme+host 推导**（X-Forwarded-Proto 照 npm 先例）。不新增配置项、不各协议自造 | `internal/config/api.go:49` 语义现成；npm/nuget/docker 三处先例 |
| TL-2 | conan 能力头自报值：`X-Conan-Server-Version` **恒报 `0.20.0`**（对齐 Artifactory 实测值，客户端只做比较不强制）；`X-Conan-Server-Capabilities` 按实际支持输出（local：`complex_search,checksum_deploy,revisions,matrix_params`；remote/virtual 追加 `only_v2`） | 规格 §2 置信度中、风险低，定案消歧 |
| TL-3 | conan `.timestamp`：**首写定终身，不覆盖、无阈值例外**（阈值机制是 Artifactory 历史包袱）；latest 排序一律以 index.json `time` 字段为准。提供 `conan.timestamp.override` 配置位但不暴露文档 | 规格 §4 已建议，定案 |
| TL-4 | debian `debianDefaultArchitectures` **默认关闭**（仅按实际坐标生成索引，不产空 Packages）——同意规格 §7.4 建议；配置项保留供运维显式开启 | 规格 §7.4「登记裁决」 |
| TL-5 | rpm 索引/repomd 校验算法**默认 SHA-256**（不沿用 Artifactory 的 SHA-1 默认）；索引文件名摘要、repomd `checksum type`、primary pkgid 三处一致用 sha256。dnf 4/5 对 sha256 全兼容（真实发行版仓主流形态），qa 用 L-r2 实测兜底 | 规格 §10 待验证 1 的倾向，定案；Artifactory 默认是历史兼容 |
| TL-6 | cargo yank/unyank 对不存在的 crate 或版本 → **404 + errors 信封**（规格 §2 表未列该分支）——随规格修订补行，T-294 AC 已按此写 | 拆票时发现的微观缺口 |
| TL-7 | debian trivial 布局（§2.2）**拆为 P2 增量票**，automatic 主票（P0）不含 trivial 分支——两者共享索引生成内核，先交默认路径 | 范围控制；规格建议「两型都做」不改变，只定先后 |

## 3. cargo.md → T-294 拆票要点（AC 草案，可直接粘贴进派单）

依据：`docs/reverse/cargo.md` + 本报告裁决 CG-1/CG-2/CG-3/TL-1/TL-6。范围：**local 全量**（remote/virtual 各自单票，dep 本票）。

```
T-294 [P0] Cargo adapter：sparse 索引 + crates API（local）
role: dev-go-core
area: internal/adapter/cargo（新包）；repo 配置注册项随包内（不动 httpapi router 与其它 adapter）
dep: 无（属性系统 FR-89 已落）
AC:
  1. 端点面（adapter.Register，package_type=cargo 经 ForRepoType 分发命中）：
     a. GET <repo>/ → 200 空体；GET index/config.json → 200 {"dl":"<base>/binflow/<repo>/v1/crates","api":"<base>/binflow/<repo>"}
        （base 取 server.base_url，空则请求推导——TL-1，对齐 npm/nuget 注入）；仓禁匿名读时 config.json 裸取 401、带凭据 200。
     b. GET index/{pkgPath} 按规格 §3.2 四档路径推导；命中 200 text/plain NDJSON + ETag(=文件 sha256)，
        If-None-Match 命中 304；不存在 404 + errors 信封；git index 请求 404 + 弃用文案（规格 §1）。
     c. GET v1/crates/{name}/{version}/download → 200 .crate 流；404 体 {"errors":[{"detail":"unable to download crate"}]}。
     d. PUT api/v1/crates/new：[u32 LE 长度][JSON][u32 LE 长度][.crate] 解帧；成功 200（warnings 可省）；
        同 name+vers（忽略 build metadata）已存在 → 409 + errors 信封（CG-3）；
        目标已存在且调用者无 DELETE 权限 → 401（匿名）/403；无写权限 → 401/403；
        解帧/IO 失败 → 500 + errors 信封。全部失败路径统一 4xx/5xx + 信封，禁止 200+errors（CG-2）。
     e. DELETE api/v1/crates/{n}/{v}/yank 与 PUT .../unyank → 200 {"ok":true}；401/403 + 信封；
        目标不存在 → 404 + 信封（TL-6）。
     f. GET api/v1/crates?q=&per_page= → 官方契约 {"crates":[{name,max_version,description}],"meta":{"total":N}}，
        不鉴权、排除 yanked、per_page 默认 10 上限 100；owners 四端点与 init → 未知路径 404。
  2. 存储面：blob 逻辑路径 crates/<name>/<name>-<version>.crate（filestore 寻址）；长元数据
     .cargo/crates/<name>/<name>-<version>.json；节点属性 crate.name/version/description/keywords(;连接)/categories(;连接)/
     yanked(yank 加、unyank 删) 按规格 §4。crate 名校验：首字符字母 + [A-Za-z0-9_-] + ≤64；vers 合法 SemVer 2.0。
  3. 索引面：publish/yank/unyank 接受后异步整文件重写 index/<pkgPath>（全版本逐行一行）；索引行 schema 按规格 §3.3
     （features 全放 features、v 省略）；yank/unyank 仅翻转对应行 yanked 布尔；同 name+vers 恒一行。
     AC 对账点：索引行 cksum == 存储实测 sha256 == 下载文件 shasum -a 256。
  4. 测试：table-driven 单测覆盖 解帧器（合法/截断/长度错位）、四档索引路径推导、索引行 JSON schema、
     错误信封、重复版本 409、yank 翻转与 404；契约测试覆盖 config.json 两态 base、ETag/304、download 404 体；
     真实客户端验收跑 cargo.md §9 L-r1~L-r6 全绿（publish→索引行出现+cksum 对账、consumer build、yank/unyank 翻转、search），
     不许只测 happy path（qa 在有 cargo 1.8x 的环境执行）。
```

配套拆分（M11 排期用）：cargo remote pull-through（config.original.json 翻译链 + search 代理，S4）P1、dep T-294；cargo virtual（CG-1 去重）P1、dep T-294。两票 area 仍是 `internal/adapter/cargo`，与 T-294 串行（dep 链），不并行派发。

## 4. 缺项总清单

| # | 类型 | 内容 | 处置 |
|---|---|---|---|
| K-1 | **M11 条件前置票**（P1） | 实例 GPG keypair 体系：keypair CRUD/存储/口令、按 repoKey 关联（debian Release/InRelease、rpm repomd.xml.asc/.key 共用；helm 不需要）。M11 默认 unsigned 验收不依赖它；若排期纳入，则作为 debian/rpm 签名小票的前置 | 已登记，交 conductor 排期裁决 |
| K-3 | ADR 补记（P2，architect） | DECISIONS.md 两条：① 五协议管理面统一 `$BASE/binflow/api/<proto>/...`，实现走 dispatchAPI 显式路由族（非 apiProtocolMounts）（DB-4）；② 新协议对外绝对 URL 一律 server.base_url + 请求回退（TL-1） | 已登记，随 M11 首张适配票前合入 |
| R-1 | 规格建议修订（reverse-engineer，不阻塞） | cargo.md：§2 表补 yank/unyank 404 分支（TL-6）；§8 remote 段补 config.json base 来源 = server.base_url | 随下次规格修订 |
| R-2 | 规格建议修订（reverse-engineer，不阻塞） | helm.md §7.3：虚仓 URL 改写目标由 `<base>/api/helm/<virtKey>/...` 改为主面 `<base>/<virtKey>/...`（与 §1.1 定案一致，HL-1） | 随下次规格修订 |

阻塞拆票的缺项：**0**。

## 5. 统计

- 规格：5/5 可直接拆票（conan / cargo / debian / rpm / helm），六要素 30/30 项齐备。
- 裁决：**23 点**——规格自评实点 16（conan 2 / cargo 3 / debian 4 / rpm 3 / helm 4；FR-91 任务书原计 15，debian 实列 4 项）+ TL 追加 7。
- 分类：同意规格建议 9；定案（含规格二选一定案与跨规格统一定案）6；改判 **2**（CN-1、CG-2，均为收紧方向，无方向性推翻）；并入/确认 3；TL 追加 7。
- 缺项：前置票 1（K-1，条件性、不阻塞 unsigned 路线）+ ADR 补记 2 条 + 规格修订建议 2 条；阻塞项 0。
