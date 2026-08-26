# PRD — M10 Artifactory 对齐第一程：license 门控基座 + addon 注册表 + 试点包型 + 制品属性系统

> **PRD 状态：v1.2 正式版（2026-08-26 随 T-297 终验 PASS 由 conductor 终审转正）**。v1.0 主轴来源：用户三指令（BOARD 2026-08-25——①「继续对比 artifactory 的反编译代码，要它的所有功能」②「binflow 也需要拥有和 Artifactory 一样的 license 控制，例如控制高可用等」③「补齐剩余的协议，例如 golang，huggingface 等，这也是 license 控制的功能，和 Artifactory 一样使用 addon 的方式加入进来」）+ conductor 裁定 M10 定位「**门控基座 + addon 注册表 + 首批高价值缺口——不贪多**」。范围裁定唯一依据：`docs/reverse/artifactory-full-feature-matrix.md`（213 条目主矩阵）。**clean-room 两处关键应用**：license 文档格式与签名算法为 BinFlow 自有设计（不复制 JFrog license 格式/算法，只对齐「功能分级门控」行为模式）；addon 注册形态为自有 Go 编译期机制（对标 META-INF/addon.{xml,properties} 的行为模式，不复制格式）。**新端点全部走 PM FR + ADR 流程（ADR-0032 起）**。v1.1 为 as-built 回写（§0 修订记录 + §5.6.1 校准表；历史文本不动，分歧以 ADR 侧为准收口——依据 conductor「PRD↔ADR 分歧以 ADR 为准」裁定）。v1.2 = v1.1 + 终验三处措辞勘误（§0 第⑫项：84.6 宽限窗 D-1 / 6.4 审计指标名面 D-2 / 84.4 GET 门 D-7）。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-10.md` |
| 里程碑 | M10 — Artifactory 对齐第一程（license/entitlement 基座 + addon 注册表 + Go/NuGet 试点 + 属性系统 + 快赢包 + 规格预研） |
| 状态 | **v1.2 正式版（2026-08-26，T-297 终验 PASS 随笔转正）**（FR-84~FR-91 八条需求；契约矩阵 12 条〔A 6 / C 5 / D 1〕+ **档位 × addon 解锁矩阵〔核心，11 槽 × 3 档〕**；L01~L30 验收命令骨架；开放问题 Q1~Q7 带暂行——v1.1 起 §5.6.1 校准回写表取代「待校准」状态，实施分歧全部按 ADR 侧收口） |
| 上游依据 | PRODUCT.md（Non-goals 不越界：HA/Xray 本体仍不做，M10 只交付门控基座）、BOARD.md「用户方向指令（2026-08-25）」节（三指令 + conductor 口径 + 盘点成果 213 条目〔已有 20/部分 50/缺失 133/不适用 10〕+ 十大缺口）、主矩阵（一/1.2 GO·NUGET·CARGO 行、一/3.1 矩阵参数行、一/3.2 MPU 行、六 Smart Remote 行、七 REST 属性行、五/J1 产品许可 REST、十 Addon 装配框架行、§十大缺口 1/7/10）、inv-2 §3（AddonType 80 项三档 + `artifactory.addons.disabled`）、inv-3 §2.3/§3.1/§3.2（GO/NUGET/CARGO 协议、矩阵参数、MPU）、inv-4 §J1/J2（licenses REST 与 SubscriptionType 模型）、docs/prd/milestone-9.md（§9 体例 + Q5 复制硬化延后建议 + M10 候选池）、ADR-0025/0026~0031（体例与豁免票模式） |
| 下游消费者 | tech-lead（拆票——分票提示 §1.3，**宽度 ≤2 内建**）、architect（**ADR-0032**：license 文档/档位/门控/addon 注册表；**ADR-0033**：属性系统与矩阵参数路径归一）、dev-go-core（internal/license 新包 + 属性 REST + metadata migration 012+）、dev-registry-adapter（internal/adapter/{go,nuget} + MPU REST）、reverse-engineer（FR-91 规格预研）、web 前端 dev（License & Add-ons 管理页 + Properties Tab）、qa-engineer（L 序列 + 真实客户端矩阵）、tech-writer（license 指南/属性用法/新包型接入）、conductor（tag m10-done；license 根密钥交付形态确认） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-25 | 初版草案（待 conductor 审）：M10 范围（conductor 种子 A~F 全承载）、FR-84~FR-91（license 框架 / 门控织入 / addon 注册表 / Go 试点 / NuGet 试点〔Cargo 余量〕 / 属性系统 / 快赢包 / 规格预研）、档位 × addon 解锁矩阵（核心）、契约矩阵 12 条、L01~L30 验收命令骨架、开放问题 Q1~Q7 带暂行；随稿完成 ROADMAP M10 段补实（原候选池条目滚入「M10 未纳入项」） |
| v1.1 | 2026-08-26 | **as-built 回写（T-293，docs-only 收口票）**：11 项「暂行值 vs 落地」分歧终裁并回写——①85.3 disabled 读 403 作废、D1 优先（ADR-0032 as-built 定案段）②K23~K29 校准全部兑现回写（§5.6.1——K26/K27/K29 校准事实落 architecture §15.3/§15.4 非 ADR-0033，见该表说明）③LC-01 license 路径拼写勘误（as-built `/api/system/license`，非 `/api/v1/`）④90.1「(presigned)」按行为模式对齐为 BinFlow capability URL ⑤90.2 未知字段 400 收窄为按名 400（scenario-D 容忍优先）⑥AC2（S3 kill -9 续传）conductor 裁定 descope M11（§11.31 债路径）⑦AC4 未知字段语义同⑤、L07 install 403 同①勘误 ⑧K28 NuGet v2 边界按 T-287 as-built 定案（T-280 规格缺失记「官方文档路径合规」，architect 建议免补票，conductor 终裁）⑨E-09 反转已同步 api-reference.md（含 POST 陈旧行勘误）。历史正文不改写，分歧注记就地追加；ADR-0032/0033 as-built 附注 + 新 ADR-0034（五协议管理面/URL base 横切定案）随本版落 DECISIONS.md |
| v1.2 | 2026-08-26 | **终验转正 + 三处措辞勘误（T-297 注册项 D-1/D-2/D-7）**：⑫ 84.6/Q6/AC5/§6.4 的「30 天宽限 + binflow_license_expiry_days 指标」按 as-built 收口——ADR-0032 D6「到期即降无 grace」为准（1h clockLeeway 非宽限；过期文档装机即拒 = fail-safe；AC5 结果断言经该路径达成）；审计动作名 as-built=`license.addon.denied`、指标名 as-built=`binflow_addon_gate_requests_total`；84.4 GET /api/system/license 门 = CapSystemRead（readonly_admin 200，与 addons 面一致），POST/DELETE = system:write 403。DoD 八条 1~7 全 PASS（T-297 报告），第 8 条 tag 由 conductor 执行 |

---

## 1. 背景与目标

### 1.1 背景

M1~M9 已交付五包型（generic/docker/maven/npm/pypi）全链、RBAC、部署矩阵、控制台对齐与服务端收口。用户三条指令把产品带入「全功能对齐 Artifactory」的新阶段，全量盘点已完成（主矩阵 213 条目：已有 20 / 部分 50 / 缺失 133 / 不适用 10，对标 7.161.11）。缺口体量（133 条）决定了多里程碑分期——**M10 的唯一使命是把「挂载点」建稳**：

1. **license/entitlement 体系不存在**（inv-4 J1/J2：Artifactory 以 AddonType 80 项 × SubscriptionType 十档注解门控全部企业功能；`artifactory.addons.disabled` 可整体禁用某 addon）。用户明确要求 BinFlow 拥有等价的分级门控能力（控制高可用等）。没有这套基座，后续 52 个缺失包型与企业功能没有统一的解锁语义。
2. **包型 = addon 门控单元**（用户指令③口径）：每包型一个槽位，按 license 档位解锁。52 缺失包型全部纳入后续规划，第一梯队 9 种 + AI/ML 生态 13 型等多里程碑分期承载——M10 用 2~3 个试点验证「注册 → 门控 → license 缺失时行为」全链。
3. **制品属性系统是十大缺口之首**（主矩阵一/3.1 + 七 REST 属性行：矩阵参数 `;k=v` 部署与 `?properties` 读写全部缺失，BinFlow 当前显式 404）——所有 Artifactory 客户端的横切基座，也是后续 AQL/清理策略/复制属性同步的基石。license 门控之外，这是 M10 最重的实功能。
4. **快赢包**：MPU REST 化（S3 multipart 机制已在，只缺 `/api/v1/uploads` REST 面）与 smart remote 字段面（低成本 wire 对齐）按余量取舍。
5. **规格预研**：第一梯队其余包型的行为规格（不实现），为 M11 批量实现铺路——协议适配票必须依赖逆向规格票（BOARD 既定规矩）。

**不贪多**：HA 本体、AQL、trash can、cleanup/retention、制品操作族（copy/move/zip）、webhook 事件总线、build-info、HuggingFace 等 13 型全部归 M11+（§2.2）。基座不稳则后续 52 包型与 133 缺口全无挂载点。

### 1.2 M10 目标与量化门槛

> 一句话：交付 BinFlow 自有的 license 分级门控体系与 addon 注册表（行为模式对齐 Artifactory、格式与算法 clean-room 自有），用 Go + NuGet 两个试点包型验证「包型 = addon」全链，补上制品属性系统这一横切基座，并以 5 份规格预研为 M11 批量对齐备料。

量化门槛（未达即里程碑不完成）：

| 指标 | M10 门槛 | 来源 |
|---|---|---|
| addon 槽位注册数 | ≥10（五基础包型 + go + nuget + cargo 占位 + properties + ≥2 功能占位槽），全部在 `GET /api/v1/addons` 可见且三态（unlocked/locked/disabled）正确 | FR-86 |
| 门控点覆盖率 | 试点包型三入口（建仓 / 内容写 / 内容读回显）100% 经门控；未解锁时建仓 403（错误体指名 addon 与所需档位）+ push 403 + **既有内容 pull 200**（降级不劫持数据） | FR-85/FR-87 |
| 门控切换时延 | license 安装/卸载 REST 返回后 ≤1s 全局生效；并发请求无中间态撕裂 | FR-85 |
| 无 license 默认行为 | 无 license 实例（community 档，Q2 暂行）上 M1~M9 全部 P0 序列复跑零变化 | §5.4 |
| Go 试点真实客户端 | `GOPROXY` 指向 BinFlow 的 `go mod download` + `go build` 全链绿（local 三件套 + remote pull-through + virtual 聚合） | FR-87 |
| NuGet 试点真实客户端 | `dotnet nuget push` + `dotnet add package --source` 全链绿（local + remote pull-through；v2 `FindPackagesById()` curl 断言） | FR-88 |
| 属性系统 | 矩阵参数部署 → `?properties` 读 → PUT/DELETE 往返全绿；存量含 `;` 路径（种子构造）回归零破坏 | FR-89 |
| 规格预研交付 | 5 份行为规格（试点 2 份另计），含端点表/布局/校验链/真实客户端命令/置信度，tech-lead 拆票就绪度确认 | FR-91 |

### 1.3 上游依赖与并行关系（含分票提示）

- **ADR-0032（architect，前置产物）**：license 文档格式与字段集、ed25519 密钥体系、档位闭集与命名、门控点织入位置（认证链之后/内容处理之前）、addon 注册表形态（Go 编译期注册 vs 装配清单 codegen）、`addons.disabled` 等价 config 键、门控失败错误体形态。本 PRD 只约束行为与验收，wire 细节冲突时以 ADR 为准。
- **ADR-0033（前置产物）**：属性系统——node 属性存储 schema（migration 012+）、矩阵参数剥离规则（成对 `;k=v` 判定与存量 `;` 路径归一）、属性键校验规则与长度上限、`?properties` 参数族（recursive/atomic）语义、`storage.matrix_params` 逃生开关默认值。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-84/85/86 建议拆 3~4 票（license 核心〔internal/license + cmd 工具〕→ addon 注册表〔internal/license + repo 校验缝〕→ 门控织入〔httpapi + adapter 缝〕→ 控制台页〔web〕），前三者串行、控制台页可并行；FR-87（internal/adapter/go）与 FR-88（internal/adapter/nuget）area 不重叠可并行，各自前置对应规格票；FR-89 拆 BE（metadata + httpapi + adapter 缝）与 FE（Properties Tab）两票；FR-90 拆 MPU（httpapi + storage S3 面）与 smart-remote 字段（config + repo 域）两票；FR-91 纯 docs/reverse/，全程可并行派发。
- **QA 并行面**：L 序列按 FR 分组；license 三态矩阵需专门的多档位实例编排（community 无 license / pro / enterprise / 过期宽限 / disabled 熔断）。

---

## 2. 范围

### 2.1 In scope

| # | 来源（种子/主矩阵） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 种子 A + 主矩阵五/J1/J2、十/Addon 装配、inv-2 §3 | FR-84（自有 license 文档与档位模型 + 离线签发工具 + 安装/查询/卸载 REST） | P0 |
| A | 种子 A + inv-2 §3（`artifactory.addons.disabled`） | FR-85（entitlement 门控织入 + 全局 addons 禁用开关 + 审计/指标） | P0 |
| B | 种子 B + 主矩阵十/Addon 装配框架 | FR-86（Go 编译期 addon 注册表 + 档位 × addon 解锁矩阵 + REST/控制台状态可见性） | P0 |
| C | 种子 C + 主矩阵一/1.2 GO 行、§十大缺口 7 | FR-87（Go 包型 addon 试点：GOPROXY local/remote/virtual，go 真实客户端） | P0 |
| C | 种子 C + 主矩阵一/1.2 NUGET 行 | FR-88（NuGet 包型 addon 试点：v3 主面 + v2 最小集，dotnet 真实客户端；Cargo 余量第三试点） | P1 |
| D | 种子 D + 主矩阵一/3.1 矩阵参数行、七/REST 属性行、§十大缺口 1 | FR-89（制品属性系统：矩阵参数部署 + `?properties` 读写 + Properties Tab） | P0 |
| E | 种子 E + 主矩阵一/3.2 MPU 行、六/Smart Remote 行、§十大缺口 10 | FR-90（快赢包：MPU REST 化 + smart remote 生效字段子集） | P1（smart-remote 子集 P2） |
| F | 种子 F + 主矩阵一/1.2 第一梯队 | FR-91（第一梯队包型行为规格预研 5 份，reverse-engineer，不实现） | P1 |

前置产物（非 FR）：ADR-0032、ADR-0033。

### 2.2 Non-goals — M10 明确不做

**产品级（继承 PRODUCT.md，不越界）**：不做 HA 集群本体、不做 Xray 式扫描、不做 Artifactory 全量 REST 兼容。**license 框架 ≠ 承诺做 HA**：M10 只交付门控基座与槽位声明面，槽位（ha/xray-integration）对应的功能本体仍属 Non-goals，直至 PRODUCT.md 由用户修订。

**M10 里程碑级 Non-goals**：

| 不做项 | 隔离边界 / 去向 |
|---|---|
| HA 本体（集群心跳/推举/传播） | M11+（主矩阵九/HA 行；M10 仅 ha 功能槽位占位 + 门控行为可见） |
| AQL + 13 老搜索族 | M11+（§十大缺口 2；属性系统是其基石，M10 先落基石） |
| Trash can 回收站 | M11+（§十大缺口 3，语义独立、实现面窄，独立立项） |
| Cleanup/Retention 策略引擎、冷存储 | M11+（§十大缺口 4） |
| 制品操作族（copy/move/zap/目录 zip/归档内浏览 `archive!/`/explode 解包） | M11+（§十大缺口 5） |
| Webhook/统一事件总线（36 事件） | M11+（§十大缺口 6，可整体平移无外部依赖） |
| Build-info 域 | M11+（§十大缺口 9） |
| Go 深化面：sumdb 代理（`sumdb/sum.golang.org/*`）与 external dependencies 重定向 | M11+（FR-87 只落 GOPROXY 核心面；深化项随 Go 规格预研补齐登记） |
| NuGet symbol server（.pdb/GUID 路径）、Conan/Cargo/Debian/RPM/Helm/Terraform/GitLFS 等**实现** | M11+（M10 仅 Cargo 余量试点 + FR-91 规格预研） |
| HuggingFace（含 xet CAS）等 AI/ML 13 型 | M11+ 分期（用户指令③的远期主体；主矩阵分组一/2.6） |
| replica 隔离 / 复制硬化（M9 Q5 建议 + smart remote `contentSynchronisation`/`enableTokenAuthentication`） | **建议并入 M11「复制硬化」**（属性/统计同步与属性系统天然同域，M10 先交付属性基石；M9 Q5 维持暂行升级为 M11 立项建议） |
| 制品 license 识别（licences.xml 91 模式） | M11+（inv-4 J3；与产品 license 门控是两回事，不混淆） |
| M9 候选池（E-04/R2/R6、Q5/E7、票级遗留 17 条） | 滚入 M11+ 候选池登记（ROADMAP 已列，M10 不收编） |
| JFrog license 格式/密钥算法的任何兼容或识别 | **永久不做**（clean-room 铁律；`/api/system/licenses` Artifactory 路径有意不兼容，见 LC-02） |

---

## 3. 用户与场景（M10 视角）

- **场景 A（内网 Go 团队）**：公司要求依赖收口。管理员装 pro license 后建 `go-remote` 仓（pull-through proxy.golang.org），开发者 `go env -w GOPROXY=...` 后 `go mod download` 全走 BinFlow；私有模块三件套 PUT 到 `go-local`，CI 统一解析。
- **场景 B（.NET 团队）**：`dotnet nuget push` 推内部包，`dotnet add package --source` 消费；nuget.org 经 `nuget-remote` 缓存，内网构建不再直连公网。
- **场景 C（平台管理员，license 视角）**：企业决定采购 BinFlow pro 档。管理员拿到离线签发的 license 文档，控制台或 `POST /api/v1/system/license` 安装，`GET /api/v1/addons` 确认 go/nuget 槽位转 unlocked；到期前 metrics/启动日志可见剩余天数。
- **场景 D（值班 SRE，熔断视角）**：某协议面因安全通告需紧急停用。SRE 在 config 设 `addons.disabled: npm` 重启——npm 建仓与内容面全部 403（数据不删），去掉开关恢复后全量回来。
- **场景 E（CI 属性打标）**：流水线 `PUT app.bin;build=77;env=prod` 部署时打标；发布脚本用 `?properties=build,env` 读取核对，用 `PUT ?properties=qa=passed` 回写 QA 结论——为 M11 的按属性搜索/清理策略备好数据面。
- **场景 F（降级安全）**：license 过期宽限期结束。已建 go 仓的制品 **pull 仍 200**（数据不劫持），push 403 提示所需档位——用户从容续期，无数据事故。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；Playwright spec 置 `web/e2e/m10/`；新端点 wire 细节以 ADR-0032/0033 定案为准，本节 AC 钉行为与状态码。**全部新端点须过 architect 评审（ADR-0032/0033），实现票不得私加端点（ADR-0029 决策 4 纪律延续）。**

### 4.1 license/entitlement 框架（种子 A，P0，拆两条 FR）

#### FR-84 自有 license 文档、档位模型与离线签发工具（internal/license 新包 + cmd 工具 + REST）

**用户故事**：
- 作为 BinFlow 的授权运营方，我能用离线工具签发自有格式的 license 文档（指定档位/被授权方/有效期），无需联网、私钥不出手。
- 作为实例管理员，我能安装、查询、卸载 license，安装后即刻生效，卸载后降回基础档且既有制品不被劫持。
- 作为管理员，伪造或损坏的 license 文档必须被拒装——门控体系本身不能是越权入口。

行为规格：

- **84.1 文档格式（自有设计，细节归 ADR-0032）**：JSON payload（`product`/`tier`/`licensee`/`issued_at`/`expires_at`/可选 `addons` 覆盖表）+ ed25519 签名。**不复制 JFrog license 格式与算法**（clean-room；主矩阵五/J1 行为对齐点只在「安装/查询/删除 + 变更即生效 + 触发 addon 重载」这一模式）。
- **84.2 档位闭集（暂行命名，Q3）**：`community` / `pro` / `enterprise`（对标 oss/pro/ent 行为模式，BinFlow 自有命名与分档）。无 license = community 档全解锁（Q2 暂行，对标 OSS 行为模式——不锁死，M1~M9 既有能力零回归）。
- **84.3 离线签发工具**：`bf license generate`（输入 tier/licensee/有效期/可选 addon 覆盖，输出 .lic 文档；需私钥文件）+ `bf license inspect`（本地验签与字段回显）。私钥管理流程见开放问题 Q1（暂行：根密钥用户离线保管；CI 用公开测试密钥对，测试密钥签发文档在默认公钥下必然验签失败——天然防测试文档误用于生产）。
- **84.4 REST 面**：`GET /api/v1/system/license`（当前生效档位/licensee/期限/宽限状态/addons 覆盖/来源〔installed|none〕）；`POST /api/v1/system/license`（安装：验签失败 400、格式坏 400、成功 200 并即时生效）；`DELETE /api/v1/system/license`（卸载：降回 community，即时生效）。门 = admin（readonly_admin 403，与系统管理面一致）。审计 `license.install` / `license.delete`。
- **84.5 持久化与启动**：license 文档落 DB 新表；启动加载并验签——**失败不拒启动**：告警日志 + 降级 community（fail-safe，不维持不可验证的高档位）。
- **84.6 过期与降级（Q6 暂行）**：到期后 30 天宽限期内高档能力维持 + 启动日志与 metrics 提示剩余天数；宽限期后降级 community——**读不降级**（既有制品内容面继续可读）、门控能力的新建仓/写入拒绝（错误体指名所需档位）。

验收标准（AC）：

- **AC1（签发与自检）**：`bf license generate --tier pro --licensee acme --expires 2027-01-01 -o pro.lic` 成功；`bf license inspect pro.lic` 回显字段且验签通过；篡改一字节后 inspect 验签失败。
- **AC2（安装/查询/卸载闭环）**：`POST /api/v1/system/license` 装 pro.lic → 200；`GET /api/v1/system/license` 回显 `tier=pro`、licensee、期限；安装后 ≤1s 内建 go 仓从 403 转 200（L06/L08）；`DELETE` → 200，档位回 community，go 内容面 pull 200 / push 403。
- **AC3（防伪）**：篡改文档安装 → 400（错误体不泄露校验内部细节）；测试密钥签发的文档在默认公钥实例安装 → 400。
- **AC4（无 license 默认）**：全新实例（零 license）`GET /api/v1/system/license` → `tier=community, source=none`；五包型全 P0 冒烟绿（§5.4 回归基线）。
- **AC5（过期降级）**：签发已过宽限期的 pro 文档安装 → 档位按 community 生效（高档门控拒绝）且既有 go 仓制品 pull 200；宽限期内文档 → 高档能力维持 + metrics `binflow_license_expiry_days` 可见。
- **AC6（启动 fail-safe）**：DB 中 license 行被外部篡改后重启 → 实例正常启动（readyz 200）+ 告警日志 + community 生效。
- **AC7（权限）**：readonly_admin / 普通 user 对三端点 403 零副作用；审计流含 install/delete 行（actor/target/tier）。

#### FR-85 entitlement 门控织入与全局 addons 禁用开关（internal/license + httpapi + adapter 缝）

**用户故事**：作为管理员，license 缺失或档位不足时，被门控能力的每个入口都不可达且错误信息指明「差什么」；作为值班 SRE，我有一个能把任意 addon 槽位整体熔断的开关，且它绝不删数据。

行为规格：

- **85.1 门控点（覆盖率指标的对象）**：① 建仓面——`packageType` 属未解锁/禁用槽位 → 403（errors[] 信封，指名 addon id 与所需档位）；② 内容面——未解锁槽位的仓 push/publish 403，**既有内容 pull 200**（降级不劫持数据，场景 F）；③ 回显面——`GET /api/v1/addons` 与仓库列表的槽位状态一致；④ virtual 成员校验——virtual 含未解锁 addon 包型成员 → 建仓 403。门控检查位于认证之后、内容处理之前（织入位置归 ADR-0032，须不破坏 M9 收口的端点族与 RBAC——既有端点的门语义零变化）。
- **85.2 即时生效与无撕裂**：license 安装/卸载为进程内原子切换（per-request 读快照）；并发请求不得观察到中间态（一半门开一半门关）。
- **85.3 全局禁用开关（对标 `artifactory.addons.disabled` 行为模式）**：config 键（暂行名 `addons.disabled`，CSV；ADR-0032 终裁）可禁用**任意**槽位（含五基础包型——运维熔断语义）：该包型建仓/读/写全部 403（错误体指明 disabled 与恢复方法），**数据与仓配置零删除**，去开关重启后全量恢复。重启生效（config 静态加载，README 明示）。
  【T-293 as-built 勘误（2026-08-26）】：本条「读 403」**作废**——与 ADR-0032 D1（读路径恒放行）冲突，按 conductor「以 ADR 为准」裁定 **D1 优先**（ADR-0032 as-built 定案段）：breaker 拒绝面 = 写动词 403 + 建仓/改仓/删仓/virtual 成员 400，读 GET/HEAD 恒 200（含 pull-through 内部写豁免——npm install 等读客户端熔断期照常工作；breaker 403 不带 `X-Binflow-License-Required` 头，文案点名旋钮与恢复路径）。证据 T-283：`TestT283DisabledBreaker`/`TestT283ServiceWritesBypassGate`。】
- **85.4 审计与指标**：审计 `addon.gate.deny`（变更面——建仓/写拒绝逐条；读面拒绝仅计数）；metrics：`binflow_license_tier`、`binflow_license_expiry_days`、`binflow_addon_gates_total{addon,decision}`。

验收标准（AC）：

- **AC1（三入口门控）**：community 实例建 `go-local` → 403（错误体含 `go` 与 `pro`）；curl 直接 PUT go 内容面 → 403；装 pro 后同命令 200。
- **AC2（降级不劫持）**：pro 期建 go 仓并上传制品 → 卸载 license → pull 该制品 200、push 新版本 403、删仓 403（变更面拒绝）。
- **AC3（无撕裂）**：并发 50 goroutine 交替调「建 go 仓 + 建 generic 仓」的同时执行 license 装/卸循环 ×10 → 每个请求结果与最终档位一致（无既非 403 又非 200 的中间态错乱）、generic 仓操作全程零失败（门控不误伤未门控槽位）。
- **AC4（熔断与恢复）**：`addons.disabled: npm` 重启 → npm 建仓/publish/install 全 403、`GET /api/v1/addons` 显示 npm=disabled；generic 仓不受影响；去开关重启 → npm 全量恢复且既有制品逐字节可下载（sha256 对账）。
  【T-293 as-built 勘误：install（读面）= **200 非 403**（D1 优先，同 85.3 勘误）；熔断期既有缓存照常服务。L07 验收命令同理——写面（建仓/publish）403、读面 200。】
- **AC5（virtual 校验）**：community 实例建含 go 成员的 virtual → 403；装 pro 后同请求 200。
- **AC6（RBAC 零回归）**：M7 角色矩阵（V01~V10 口径）+ M9 N 序列 SE 域复跑绿——门控织入不改变任何既有端点的授权语义。
- **AC7（观测）**：`/metrics` 含三个新指标；审计流含 `license.install`/`license.delete`/`addon.gate.deny` 行。

### 4.2 addon 注册表（种子 B，P0）

#### FR-86 Go 编译期 addon 注册表 + 档位 × addon 解锁矩阵 + 状态可见性（internal/license + repo 校验缝 + web）

**用户故事**：作为架构侧后续贡献者，新增一个包型 = 注册一个槽位（元数据 + 装配单元），门控/回显/校验自动获得，不需要在散落各处的 if 里手工接线；作为管理员，我在控制台一眼看到「哪些功能解锁、哪些差哪个档位」。

行为规格：

- **86.1 注册表（对标 META-INF/addon.{xml,properties} 行为模式，自有形态）**：每 addon = 元数据（id / kind〔package-base | package-eco | feature〕/ 所需档位 / 显示名）+ 装配单元（该能力的服务注册点）。Go 编译期注册（init 注册或 codegen 清单，形态归 ADR-0032）。**每 addon 一槽位**——包型 addon 与功能 addon 同一机制。
- **86.2 M10 槽位集（暂行，Q3 终裁）**：五基础包型（generic/docker/maven/npm/pypi，community 恒解锁——**有意不兼容 Artifactory 分级**：这些包型在 Artifactory 是 pro 档，BinFlow 因 M1~M9 既有承诺归基础档，license 体系引入不降级既有能力）+ go + nuget（pro 档试点）+ cargo（pro 档占位，实现 M11，余量试点除外）+ properties（feature，community——同样有意不兼容：Artifactory 归 pro 档）+ **功能占位槽 ≥2**（ha / xray-integration，enterprise 档：解锁状态可见、回显明示「槽位预留，功能本体 M11+」——PRODUCT.md Non-goal 未解除前不承诺本体）。
- **86.3 解锁矩阵（§5.2 核心表）**：档位 × 槽位 → unlocked/locked/disabled 三态；license `addons` 覆盖表可对单槽位提级（如 enterprise 文档把 go 提为 unlocked——本身就是 unlocked，覆盖表用于例外收紧/放宽，语义归 ADR-0032）。
- **86.4 状态面**：`GET /api/v1/addons`（admin/readonly_admin 可读）→ 全槽位清单含 `{id, kind, tier_required, state, reason}`；控制台新增管理页「License & Add-ons」（安装/卸载 license、矩阵总览、disabled 态呈现——FE 腿 P1）。
- **86.5 建仓校验接线**：`packageType` ∈ 注册表且槽位 unlocked，否则 403（FR-85-85.1①）；既有 packageType 枚举校验（未知型 400）维持。`GET /api/repositories` 回显对既有仓零变化。

验收标准（AC）：

- **AC1（注册完备）**：`GET /api/v1/addons` 返回 ≥10 槽位且与 §5.2 矩阵逐行一致（community 实例：五基础 + properties = unlocked，go/nuget/cargo = locked〔pro〕，ha/xray-integration = locked〔enterprise〕）。
- **AC2（编译期注册的单点性）**：新增槽位的代码改动不触碰门控/回显/校验分支（架构断言：注册表是唯一事实源——由 ADR-0032 定义的守卫测试承载，如「槽位清单 = 注册表输出」一致性测试）。
- **AC3（三档矩阵）**：community/pro/enterprise 三实例（或同实例装三档 license 依次）→ `GET /api/v1/addons` 状态翻转与 §5.2 逐格一致；enterprise 档 ha 槽位 unlocked 且回显含「功能本体 M11+」注记。
- **AC4（五包型零回归）**：无 license 实例上五包型建仓/内容面/搜索冒烟与 M9 行为逐字节一致（`git diff` 服务端契约面零漂移或 100% 归属 M10 豁免票）。
- **AC5（控制台）**：Playwright——admin 可见 License & Add-ons 页（当前档位/矩阵/安装卸载流转）；readonly_admin 只读可见；普通 user 导航不可达（门 = 管理面）。

### 4.3 试点包型 addon（种子 C，Go P0 / NuGet P1 / Cargo 余量）

#### FR-87 Go 包型 addon（internal/adapter/go；前置：Go 行为规格票——FR-91 附带或先行）

**用户故事**：作为 Go 开发者，我把 `GOPROXY` 指向 BinFlow 后 `go mod download`/`go build` 与公网无差别；私有模块推上来，CI 与同事统一从我这里解析。

行为规格：

- **87.1 协议面（以 go.dev/ref/mod 公开规范为准，反编译只补空白）**：`@v/list`、`@v/<ver>.info`、`@v/<ver>.mod`、`@v/<ver>.zip` 的 GET/PUT、`@latest`；内容面挂 `$BASE/binflow/{repoKey}/...`（go-default layout 的 `@v` 段对齐，主矩阵一/1.2 GO 行）。local：curl 三件套 PUT（`.zip`/`.mod`/`.info`）；remote：pull-through（默认 `proxy.golang.org`，复用 M3 remote 缓存机制与 SSRF 防护）；virtual：聚合（复用既有 virtual 机制，成员含 go 包型仓）。
- **87.2 门控**：槽位 `go`（pro）——建仓/推送 403、license 缺失行为同 FR-85。
- **87.3 校验**：`.info`/`.mod` JSON 语法与版本号合法性校验（bad request 400）；`.zip` 走 checksum 校验链（与既有 adapter SPI 一致）。
- **不做**：sumdb 代理、external dependencies 重定向（M11+，§2.2）。

验收标准（AC）：

- **AC1（local 三件套）**：curl PUT 三件套 → `@v/list` 含版本、`.info/.mod/.zip` GET 逐字节回读（sha256 对账）。
- **AC2（真实客户端 local）**：`GOPROXY=$BASE/binflow/go-local GOPRIVATE='*' go mod download <mod>@v1.0.2` + `go build` 全绿（scratch module）。
- **AC3（remote pull-through）**：`GOPROXY=$BASE/binflow/go-remote go mod download golang.org/x/mod@v0.17.0` 绿；二次执行命中缓存（回源计数=1 或下载明显提速的量化断言）；上游故障降级行为对齐既有 remote 语义。
- **AC4（virtual）**：`go-virt`（go-local + go-remote）解析顺序正确——本地优先命中本地、未命中走 remote。
- **AC5（门控全链）**：community 实例建 go 仓 403 → 装 pro 200 → 上传/下载绿 → 卸载 license → pull 200 / push 403（FR-85 AC2 的 go 实证腿）。
- **AC6（回归）**：五包型既有 remote/virtual 回归绿（remote 机制共享面零回归）。

#### FR-88 NuGet 包型 addon（internal/adapter/nuget；前置：NuGet 行为规格票）；Cargo 余量第三试点（P2）

**用户故事**：作为 .NET 开发者，`dotnet nuget push` 推包、`dotnet add package --source` 消费，全程不感知 BinFlow 与 nuget.org 的差别。

行为规格：

- **88.1 v3 主面（公开规范为准）**：服务索引 `GET $BASE/binflow/api/nuget/v3/{repoKey}/index.json`（resources：SearchQueryService / flatcontainer / RegistrationsBaseUrl 最小集，自描述发现）；flatcontainer `PUT`（dotnet push 落点）；registrations 版本清单；search 最小（q/前缀）。**v2 最小集**：`FindPackagesById()`（OData，nuget.exe 老客户端与 curl 断言承载；`Packages()Id=` 与 `$count` 归 ADR-0032 定裁）。symbol server 不做（M11+）。
- **88.2 rclass**：local + remote（pull-through nuget.org）+ virtual（聚合）。
- **88.3 门控**：槽位 `nuget`（pro），同 FR-85。
- **88.4 Cargo 余量试点（P2，明确非 DoD 硬门）**：若 NuGet 提前收官且余量足，按其规格预研实现最小面（`api/v1/crates` publish/download + sparse `config.json` 索引，cargo 真实客户端）——否则槽位占位（FR-86-86.2）+ 规格交付。
  【T-293 登记（2026-08-26）】：前置规格票 **T-280 实际从未执行**（nuget.md 不存在——配额乱窗静默丢失，与 T-284 同款根因）；T-287 实现依据 = 官方 NuGet API 规范（learn.microsoft.com/nuget/api）+ 本 PRD + 活体探针——即 clean-room 铁律「有公开规范的协议以官方文档为准」的合规路径（7 项自有裁定经 T-293 逐项复核全部维持，见 §5.6.1 K28 行）。**architect 建议：免补规格票**（补一份 retro 规格对已验证实现无增量信息；M11 NuGet remote/virtual 硬化或 symbol server 立项时随票补 as-built 规格），登记 architecture §12-18——conductor 终裁。】

验收标准（AC）：

- **AC1（v3 push/consume）**：`dotnet nuget push pkg.1.0.0.nupkg --source $BASE/binflow/api/nuget/v3/nuget-local/index.json` 绿；新项目 `dotnet add package Pkg --source ...` + `dotnet restore` 绿；index.json/flatcontainer/registrations curl 断言齐全。
- **AC2（v2 最小集）**：`FindPackagesById()` 对已推包返回 OData 条目（版本/依赖元数据正确）。
- **AC3（remote/virtual）**：`nuget-remote` pull-through（restore 公共包二次命中缓存）+ `nuget-virt` 聚合本地优先。
- **AC4（门控）**：community 建 nuget 仓 403 → 装 pro 200（同 FR-87-AC5 模式）。
- **AC5（回归）**：五包型 + go 试点回归绿。
- **AC6（Cargo 余量腿，条件性）**：若执行——`cargo publish`/`cargo add`/download 真实客户端绿；若不执行——ROADMAP/BOARD 记录余量裁决，不视为 DoD 缺口。

### 4.4 制品属性系统（种子 D，P0，十大缺口之首）

#### FR-89 矩阵参数部署 + `?properties` 读写 + Properties Tab（metadata + httpapi + adapter 缝 + web）

**用户故事**：作为 CI 工程师，我在部署 URL 上打标（`;build=77;env=prod`），属性随制品持久化；作为发布管理员，我用 `?properties` 读、写、删属性，为将来的按属性搜索与清理策略备好数据。

行为规格：

- **89.1 矩阵参数部署（对齐 Artifactory 官方文档双证行为，主矩阵一/3.1 高置信）**：PUT 路径（含 repoKey 段）中的 `;k=v;k2=v2` 成对序列整体剥离并转 properties 随部署存储；键非法 400；值支持多值（逗号分离规则归 ADR-0033）。适用面：file-PUT 型内容面（generic/maven + 新增 go/nuget 上传路径）；docker/npm/pypi 协议型不经文件路径 PUT，不适用（对齐 Artifactory）。**存量路径归一（Q5 暂行）**：仅剥离「成对 `;k=v` 且键合法」的尾随序列；非成对 `;` 维持路径字面语义；config 逃生开关 `storage.matrix_params`（默认 on，ADR-0033 终裁）。
- **89.2 属性 REST（对齐官方文档）**：`GET /api/storage/{repo}/{path}?properties=K1,K2*`（逗号多键 + 尾 `*` 通配；`atomic=true` 时任一键缺失 → 404）；`PUT ?properties=k=v[,v2]&recursive=1`（folder 递归）；`DELETE ?properties=k,k2&recursive=1`。权限：read 门读、write 门写（沿用 storage 端点权限模型）。
- **89.3 存储与回显**：`node_properties` 新表（migration 012+，多值）；`GET /api/storage/{repo}/{path}` 详情体附 `properties` 字段（additive）。长度/数量上限（键 ≤64、单值 ≤1KB、per-node ≤500——暂行，ADR-0033 终裁）防元数据炸弹。
- **89.4 UI**：NodeDetail 补 Properties Tab（读写，M8 预留位）——FE 腿 P1。
- **不做**：属性集（property sets 闭集校验，主矩阵二）、按属性搜索（AQL 族 M11+）、属性复制同步（M11 复制硬化）。

验收标准（AC）：

- **AC1（矩阵参数部署）**：`curl -T app.bin "$BASE/generic-local/a/b/app.bin;build=77;env=prod"` → 200，制品路径为 `a/b/app.bin`；`?properties=build,env` 读回 `{build:[77],env:[prod]}`；`.info`/详情体回显一致。
- **AC2（REST 读写删）**：PUT `?properties=qa=passed,owner=team-a` → 读回；DELETE `?properties=qa` → 仅删该键；通配 `build*`；folder + `recursive=1` 递归生效；`atomic=true` 缺键 404。
- **AC3（校验）**：非法键（`;bad key=1`）→ 400（矩阵参数路径与 REST 写面双臂）；超限（>500 属性）→ 400。
- **AC4（存量回归，Q5）**：种子构造「文件名含非成对 `;` 的既有制品」（M9 语义）→ GET 原路径 200 逐字节回读；`storage.matrix_params=off` 时 `;build=77` 全量按文件名处理（逃生开关臂）。
- **AC5（UI）**：Playwright——详情 Properties Tab 呈现/编辑/删除属性；readonly 用户只读态。
- **AC6（回归）**：M1 storage 序列 + M4 制品浏览 + M9 N 序列零回归（详情体 additive 加宽零破坏）。

### 4.5 快赢包（种子 E，P1；smart-remote 子集 P2）

#### FR-90 MPU REST 化 + smart remote 生效字段子集（httpapi + storage S3 面；config + repo 域）

**用户故事**：作为大文件上传的前端开发者，浏览器可直传 S3 分片（create → presigned part URL ×N → complete），断点可续、可查状态、可中止；作为 remote 仓调优的管理员，超时与缓存期字段终于可配且真实生效。

行为规格：

- **90.1 MPU REST（对齐主矩阵一/3.2 六端点行为模式，E-26 前缀口径）**：`/api/v1/uploads` 的 create/config/urlPart/complete/status/abort——create 携带 repoKey/path/partSizeMB 返回分片 URL 集（presigned）；complete 携带 checksum 校验落盘建 node；status 按会话查；abort 清理。**仅 S3 后端生效**：本地 filestore 返回 501 + 明确错误体（诚实不做 inert 面）。scope 等价物（Artifactory `internal:mpu:x`）归 ADR-0032 定裁（BinFlow token 无 scope 模型，暂行按 write 门）。
  【T-293 as-built（T-289）：「(presigned)」按**行为模式对齐**为 BinFlow capability URL——urlPart 返回的 PUT 目标是 BinFlow URL，字节经服务端中继进 S3 multipart（客户端零 S3 凭据、bucket endpoint 不暴露、checksum 链服务端计算），非字面 S3 presigned 直传（裁定 A，评审维持；字面直传需新 ADR——SSRF/凭据面，architecture §15.4.1/§11.43）。K29 scope 等价物 as-built = 认证 + 目标仓写门（required + path `w`）、会话 id 为不可猜测 capability、complete 校验链 sha256 必填 + sha1/md5 可选错配 409。create 限 generic local 仓；partSizeMB<5MiB clamp 至 5MiB 并如实回显。】
- **90.2 smart remote 生效子集（主矩阵六/Smart Remote 行字段集裁剪）**：M10 只落可真实生效的字段——`socketTimeoutMs`、`metadataRetrievalTimeoutSecs`、`missRetrievalCachePeriodSecs`（接受+回显+行为生效）；`unusedArtifactsCleanupPeriodHours`（P2：缓存清理 cron 最小实现）。**不做 inert 字段**：`enableTokenAuthentication`/`contentSynchronisation` 归 M11 复制硬化（config 严格 schema 维持——未知字段 400 不变）。
  【T-293 as-built（T-290）勘误①】：「未知字段 400 不变」收窄为**仅对两个 M11 字段按名 400**（错误体点名字段 + M11 指引）——M3 实况是未知字段**丢弃容忍**（scenario-D 迁移脚本契约，`TestM02bRemoteConfigValidation` 钉死），逐字全局 400 会破 AC5 回归红线；这正是 90.1「诚实不做 inert 面」的同款精神（architecture §11.42）。勘误②：拼写终裁——canonical `socketTimeoutMs`（PRD 拼写）+ 输入别名 `socketTimeoutMillis`（artifactory.xsd 拼写，只进不出，非零分歧 400）；`missedRetrievalCachePeriodSecs` canonical **保留 Artifactory 拼写**（含 ed，repo-semantics §7.1/repo-semantics 高置信公开拼写），本 PRD 的 `missRetrievalCachePeriodSecs` 拼写降为输入别名。勘误③：`unusedArtifactsCleanupPeriodHours` P2 收窄为**仅字段落库 + 配置面**（清理引擎 M11——conductor 派单收窄留痕）；`socketTimeoutSecs`（M3 legacy 字段）保留接受、消费优先级低于 ms 拼写。字段表 as-built 见 docs/user/admin/remote-virtual.md。】
- **90.3 既有 seam 复用**：S3 multipart 机制（M6）与 upload_sessions 台账（T-209）不动内核，只加 REST 面。

验收标准（AC）：

- **AC1（MPU 全链，MinIO 栈）**：create → 按 urlPart PUT 3×5MiB → status 进度正确 → complete（sha256 对账）→ 制品 GET 逐字节一致；abort 后 blob 零残留、status 404；分片重传幂等。
- **AC2（断点）**：PUT 2 片后 kill -9 重启 → status 保留 → 续传 complete 成功（对齐 M9 续传验收口径）。
  【T-293 as-built：**descope M11**——conductor 已裁定（BOARD sprint 654 留痕）：S3 后端跨重启续传受 §11.31 既有债约束（upload ID 落 `upload_sessions` 表 + S3 ResumeSession 经 ListParts 重建），「零存储层改动」契约下不可达；as-built = kill -9 后 status 404（探针钉死断言 + 翻转条件注释），已 complete 制品仍 200，S3 侧孤儿由启动 sweep + TTL 回收。付债路径归 M11，届时本 AC 随之复活。】
- **AC3（filestore 诚实）**：本地 filestore 实例调 create → 501 + 错误体明示仅 S3。
- **AC4（smart-remote 字段）**：PUT remote 配置含三字段 → 回显一致 + 行为生效探针（socketTimeout 短值触发超时臂 / missRetrievalCachePeriod 控制未命中缓存窗）；未知字段（含 contentSynchronisation）→ 400 维持。
  【T-293 as-built 勘误：「未知字段 400」语义 = **contentSynchronisation/enableTokenAuthentication 按名 400**（AC4 字面达成）；其余未知字段维持 M3 scenario-D 容忍（见 90.2 勘误①）。】
- **AC5（回归）**：M6 S3 序列（H 族）+ M3 remote 回归绿。

### 4.6 规格预研（种子 F，P1，不实现）

#### FR-91 第一梯队包型行为规格 5 份（docs/reverse/，reverse-engineer）

**用户故事**：作为 M11 的 tech-lead，我拆包型实现票时每一型都有行为规格可依赖（BOARD 规矩：协议适配票必须依赖对应逆向规格票），端点/布局/校验/客户端命令拿来即用。

行为规格：

- **91.1 覆盖集（5 份，试点 2 份另计）**：Conan（v1+v2 修订链）、Cargo（sparse 索引+yank）、Debian（apt+By-Hash）、RPM/YUM（repomd）、Helm（经典 chart 仓+HelmOCI）；Terraform（Registry+Backend state 锁）与 GitLFS（含锁协议）作余量第 6/7 份。
- **91.2 每份结构**：端点表（方法/路径/语义/错误码）、layout、上传/下载/校验链、**公开规范锚点优先**（Debian/Helm/Terraform/NuGet/Cargo 有官方规范的以规范为准，反编译只补空白——clean-room）、rclass 三态行为、真实客户端命令清单（qa 可直接引用）、置信度标注（高/中/低，低置信不作验收依赖）、M11 拆票就绪度自评。
- **91.3 试点规格先行**：Go/NuGet 规格作为 FR-87/FR-88 前置票先行交付（同一结构），本 FR 计数不含。

验收标准（AC）：

- **AC1（交付与结构）**：5 份规格落 `docs/reverse/`（`conan.md`/`cargo.md`/`debian.md`/`rpm.md`/`helm.md`），结构六要素齐全、置信度逐行标注。
- **AC2（clean-room 抽查）**：无代码翻译痕迹（conductor/抽检口径同 T-3 先例）；公开规范项引用官方文档而非反编译细节。
- **AC3（拆票就绪度）**：tech-lead 对 5 份出具「可直接拆票」确认（缺项清单为空或已登记 M11 前置票）。

### 4.7 M9 候选池处置（不设 FR）

M9 归档的延后 3 项（E-04 repos 扩列 / R2 搜索契约 / R6 Tokens 页）、Q5/E7 与票级遗留 17 条：**全部滚入 M11+ 候选池**（ROADMAP 已改列「M10 未纳入项」）。理由：M10 服务三指令定调的新基座（license/addon/属性），候选池属旧债域；其中 Q5 replica 隔离与 smart remote `contentSynchronisation` 属性同步天然同域，**建议 M11「复制硬化」统一立项**（消费 M10 属性系统成果）。

---

## 5. 兼容性矩阵（M10 核心——license 档位 × 功能解锁 + 端点对齐分级）

### 5.1 层级定义（沿用 M2~M9 分级口径）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 端点路径/方法/语义对齐 Artifactory 或公开规范（高频子集承诺范围）；基座前缀差异（`/binflow` vs `/artifactory`）沿 E-26 口径；加宽回显 additive |
| **C 自有（/api/v1 或内部语义）** | 无 Artifactory 对应或 BinFlow 自有设计（license 文档格式、addon 注册形态）；行为模式对齐但载体自定 |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / PM 裁定），矩阵留痕防再议 |

### 5.2 档位 × addon 解锁矩阵（本里程碑核心矩阵；暂行版，Q3 终裁）

| addon 槽位 | kind | community | pro | enterprise | M10 状态 |
|---|---|---|---|---|---|
| generic / docker / maven / npm / pypi | package-base | unlocked | unlocked | unlocked | 既有能力入槽（恒解锁） |
| properties（属性系统） | feature-base | unlocked | unlocked | unlocked | M10 交付 |
| go | package-eco | locked | unlocked | unlocked | M10 试点（FR-87） |
| nuget | package-eco | locked | unlocked | unlocked | M10 试点（FR-88） |
| cargo | package-eco | locked | unlocked | unlocked | 槽位占位 + 规格预研（余量试点） |
| ha | feature-ent | locked | locked | unlocked | 槽位占位（本体 M11+，Non-goal 未解除） |
| xray-integration | feature-ent | locked | locked | unlocked | 槽位占位（集成面 M11+） |

- 三态叠加规则：`addons.disabled` 可把任意槽位置 disabled（优先级最高，熔断语义）；license `addons` 覆盖表可对单槽位例外调整（ADR-0032）。
- **与 Artifactory 分级的有意差异（留痕）**：①五基础包型 Artifactory 归 pro 档（inv-2 §3 pro 包类型类），BinFlow 归 community——M1~M9 既有承诺不因 license 体系引入而降级；②Properties Artifactory 归 pro 档，BinFlow 归 community（横切基座放基础档，对标 Artifactory oss 档含 AQL 的先例精神）；③档位命名自有（community/pro/enterprise vs oss/pro/ent），只对齐行为模式。清理与回收站等治理类功能 M11+ 立项时再入矩阵。

### 5.3 契约矩阵（12 条）

| # | 端点/契约面 | Artifactory 对应 / 公开规范 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| LC-01 | `GET/POST/DELETE /api/v1/system/license`（安装/查询/卸载 + 即时生效 + 过期宽限/降级） | `POST/GET /api/system/licenses` + activate/licenseChanged/DELETE（inv-4 J1 高置信）——**行为模式对齐，路径与文档格式自有** | C | P0 | 高（行为）/—（格式） | L01~L03 |
| LC-02 | Artifactory 路径 `/api/system/licenses` 的 JFrog 格式安装 | 同上 | **D（有意不兼容）** | — | 高 | L03（负向：该路径请求可达且明确 400/404 引导，JFrog license 零误装可能） |
| LC-03 | `addons.disabled` 全局禁用开关（config 键） | `artifactory.addons.disabled=<csv>`（inv-2 §3） | C | P0 | 高 | L07 |
| LC-04 | `GET /api/v1/addons`（槽位清单 + 三态 + 所需档位） | 无对应 REST（Artifactory 经 UI/AddonType 内部表） | C | P0 | — | L05 |
| LC-05 | 档位闭集 + 解锁矩阵（§5.2） | SubscriptionType 十档 + AddonType 80 项注解门控（inv-4 J2）——三档简化自有 | C | P0 | 高（行为） | L05/L06 |
| LC-06 | 矩阵参数部署 `PUT {path};k=v;k2=v2` → properties | 有（官方文档双证 + inv-3 §3.1 高置信；含 repoKey 段剥离） | A | P0 | 高 | L18 |
| LC-07 | `GET/PUT/DELETE /api/storage/{repo}/{path}?properties=`（recursive/atomic/通配） | 有（官方文档双证；rest-api.md §3） | A | P0 | 高 | L18/L19 |
| LC-08 | Go 包型内容面（`@v/list`、`.info/.mod/.zip`、`@latest`；local/remote/virtual） | GOPROXY 公开规范（go.dev/ref/mod）为准；Artifactory ph/go 为补空白参考 | A | P0 | 高 | L10~L13 |
| LC-09 | NuGet v3（index.json/flatcontainer/registrations/search）+ v2 `FindPackagesById()` | NuGet v3/v2 公开规范为准；路径形态 `$BASE/binflow/api/nuget/v3/{repo}/index.json`（E-26 前缀口径）；symbol server 不做 | A | P1 | 高（v3）/中（v2 子集边界归 ADR-0032） | L14~L17 |
| LC-10 | Cargo（余量）：`api/v1/crates` + sparse 索引 | crates.io 公开规范 sparse index 为准 | A（条件性） | P2 | 高 | L-cargo（余量腿） |
| LC-11 | MPU REST `/api/v1/uploads` 六端点（create/config/urlPart/complete/status/abort） | 有（inv-4 L3 高置信；E-26 前缀口径；scope 等价物 ADR-0032） | A | P1 | 高 | L23/L24 |
| LC-12 | smart remote 生效字段（socketTimeoutMs/metadataRetrievalTimeoutSecs/missRetrievalCachePeriodSecs〔+P2 unusedArtifactsCleanupPeriodHours〕） | 有（inv-4 F5 artifactory.xsd 字段及默认值）；contentSynchronisation 族归 M11 | A（子集） | P1/P2 | 高 | L25 |

> 计数：**12 条 = A 6（LC-06/07/08/09/11/12）+ C 5（LC-01/03/04/05 + LC-10 条件性计 A 则 A 7/C 4）+ D 1（LC-02）**。Cargo 未执行时按占位不计。既有契约面（/v2、五包型内容面、session/token 族）本里程碑对五基础包型**零行为变化**（§5.4）。

### 5.4 回归基线（M10 不反转既有断言——无 license 默认实例）

| 既有断言 | M10 期望 |
|---|---|
| M1~M9 全部 P0 序列（C/D/H/M/W/V/U/N） | 零回归（FR-86-AC4：五包型入槽不改任何门与 wire） |
| `?properties` 显式 404（gap-endpoints/storage.go E-09 归档） | **反转**：FR-89 落地后 200/PUT/DELETE（matrix 参数路径同理） |
| `GET /api/storage/{path}` 详情体 | additive 加宽附 `properties`（既有断言零改动全绿） |
| packageType 枚举闭集（generic/docker/maven/npm/pypi） | 加宽 go/nuget（additive；未知型 400 严格校验维持；门控 403 先于枚举 400——顺序归 ADR-0032） |
| 建仓/内容面错误体 errors[] 信封 | 新增门控 403 与熔断 403 沿用信封（M1 E 系三分层口径） |
| X-Explode-Archive 400 / 归档族 | 不动（M11+） |
| remote 配置严格 schema（未知字段 400） | 维持（smart-remote 只加白名单字段） |

### 5.5 M10 核心验收命令（L 序列骨架，QA 直接引用）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-84 license ==========
# L01 签发自检：bf license generate --tier pro --licensee acme --expires 2027-01-01 -o pro.lic && bf license inspect pro.lic
#   篡改探针：sed -i 's/acme/acmeX/' pro.lic → inspect 验签失败
# L02 安装/查询/卸载：POST /api/v1/system/license -d @pro.lic → 200；GET → tier=pro；DELETE → 200 → tier=community
# L03 防伪：篡改文档 POST → 400；测试密钥文档在默认公钥实例 → 400；/api/system/licenses（Artifactory 路径）→ 400/404 引导（LC-02 负向）
# L04 无 license 默认：GET /api/v1/system/license → {"tier":"community","source":"none"}；五包型冒烟绿
# L05 过期：已过宽限期 pro 文档 → 按社区档生效；宽限内 → 高档维持 + binflow_license_expiry_days 可见

# ========== FR-85/86 门控与注册表 ==========
# L06 门控矩阵：GET /api/v1/addons | jq '.[]|{id,state}'（与 §5.2 逐格一致）
#   community：建 go-local → 403（errors[] 含 go/pro）；装 pro → 200；卸载 → push 403 / pull 200 / 删仓 403
# L07 熔断：config addons.disabled: npm 重启 → npm 建仓/publish/install 403、GET addons npm=disabled；
#   generic 全程正常；去开关重启 → 恢复 + sha256 对账
# L08 切换时延：安装/卸载 REST 返回后 sleep 1 → 门控行为已翻转（脚本断言）；无撕裂并发腿（FR-85-AC3）
# L09 观测：/metrics 三指标 + 审计 license.install/license.delete/addon.gate.deny 行存在

# ========== FR-87 Go ==========
# L10 local 三件套：curl -su $ADMIN -T mymod.zip  "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.zip"（.mod/.info 同）
#   GET @v/list / @v/v1.0.2.info|.mod|.zip → sha256 逐字节对账
# L11 真实客户端 local：GOPROXY=$BASE/binflow/go-local GOPRIVATE='*' go mod download example.com/mymod@v1.0.2 && go build ./...
# L12 remote：GOPROXY=$BASE/binflow/go-remote go mod download golang.org/x/mod@v0.17.0（二次命中缓存断言）
# L13 virtual：go-virt（local+remote）解析顺序断言（本地优先/未命中走 remote）
# ========== FR-88 NuGet ==========
# L14 v3 push：dotnet nuget push pkg.1.0.0.nupkg --source $BASE/binflow/api/nuget/v3/nuget-local/index.json
#   curl index.json（三 resources）+ flatcontainer + registrations 断言
# L15 消费：dotnet new console && dotnet add package Pkg --source ... && dotnet restore（本地 + nuget-remote pull-through 二次缓存）
# L16 v2：curl "$BASE/binflow/api/nuget/nuget-local/FindPackagesById()?id=Pkg" → OData 条目（版本/依赖正确）
# L17 virtual：nuget-virt 聚合本地优先；门控腿同 L06 模式（community 403 → pro 200）
#   Cargo 余量腿（条件性）：cargo publish / cargo add / download 真实客户端 + api/v1/crates 断言

# ========== FR-89 属性 ==========
# L18 矩阵参数：curl -su $ADMIN -T app.bin "$BASE/generic-local/a/b/app.bin;build=77;env=prod"
#   curl -su $ADMIN "$BASE/api/storage/generic-local/a/b/app.bin?properties=build,env"   # {"build":["77"],"env":["prod"]}
# L19 REST 读写删：PUT "?properties=qa=passed,owner=team-a"；DELETE "?properties=qa"；通配 build*；folder recursive=1；atomic 缺键 404
# L20 校验：";bad key=1" → 400（矩阵参数 + REST 双臂）；>500 属性 → 400
# L21 UI：Playwright Properties Tab 呈现/编辑/删除 + readonly 只读态
# L22 存量回归：非成对 ';' 文件名制品 GET 原路径 200 逐字节回读；storage.matrix_params=off 逃生开关臂

# ========== FR-90 快赢 ==========
# L23 MPU（MinIO）：POST /api/v1/uploads/create → urlPart PUT ×3（5MiB）→ status → complete(sha256) → GET 逐字节；
#   abort → 零残留 + status 404
# L24 filestore 诚实：本地 filestore create → 501 + 明示错误体
# L25 smart-remote：PUT remote 配置含 socketTimeoutMs/metadataRetrievalTimeoutSecs/missRetrievalCachePeriodSecs → 回显 + 生效探针；
#   contentSynchronisation 等未知字段 → 400 维持

# ========== FR-91 规格与控制台 ==========
# L26 规格五份结构完备性走查（端点表/布局/校验链/客户端命令/置信度/就绪度）；clean-room 抽查零翻译痕迹
# L27 控制台：License & Add-ons 页三态矩阵 + 安装卸载流转（Playwright，admin/readonly 两角色）

# ========== 回归与 NFR（收口跑）==========
# L28 M1~M9 全 P0 复跑（无 license 实例）+ 服务端契约变更面 100% 归属 M10 豁免票
# L29 NFR：门控热路径 P95、license 切换 ≤1s、100 并发 go mod download 零 5xx、矩阵参数 PUT 吞吐偏差 <10%
# L30 冷启动 <2s / RSS <100MB 维持（license 加载与注册表不破基线）
```

> **T-293 as-built 勘误（2026-08-26，QA 引用时以本注为准）**：① L02/L03 的 license 端点路径
> as-built 为 `/binflow/api/system/license`（**单数、api 兼容族**）——LC-01 与本节 `/api/v1/system/license`
> 拼写系笔误（ADR-0032 决策 4 / architecture §7.1）；`/api/system/licenses` 复数路径 = 404 引导
> （非 400，K25 终裁）。② L07 的 install 403 → **200**（D1 优先，85.3 勘误）；写面 403 不变。
> ③ L25 的「contentSynchronisation 等未知字段 → 400 维持」→ **仅该两 M11 字段按名 400**
> （AC4 勘误）；「`missRetrievalCachePeriodSecs`」按 90.2 勘误②以别名接受、canonical 为
> `missedRetrievalCachePeriodSecs`。④ L23 的「urlPart PUT ×3」目标为 BinFlow URL（90.1 勘误）。

### 5.6 待校准项（ADR-0032/0033 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K23 | license 文档字段集与编码（JSON+ed25519 签名封装形态）、公钥内嵌/可覆盖策略 | §4.1-84.1 暂行字段 | ADR-0032 |
| K24 | `addons.disabled` config 键名与 CSV 语义（含空格/未知槽位容错） | `addons.disabled`（重启生效） | ADR-0032 |
| K25 | 门控失败错误体形态与门控检查在请求链的精确位置；`/api/system/licenses` 旧路径 400 vs 404 | errors[] 信封含 addon id + 所需档位 | ADR-0032 |
| K26 | 矩阵参数剥离规则（成对判定/多值逗号/repoKey 段）与 `storage.matrix_params` 默认值 | 成对 `;k=v` 尾随剥离、默认 on | ADR-0033 |
| K27 | 属性键字符集/长度上限/per-node 上限 | `[A-Za-z0-9_-]+`、键 ≤64、值 ≤1KB、≤500 | ADR-0033 |
| K28 | NuGet v2 最小集边界（FindPackagesById 之外是否含 `Packages()Id=`/`$count`） | 仅 FindPackagesById() | ADR-0032 |
| K29 | MPU scope 等价物（Artifactory `internal:mpu:x`）与 complete 校验链 | 按 write 门 + sha256 | ADR-0032 |

### 5.6.1 校准回写（T-293 as-built 终裁，2026-08-26——上表七项全部收口）

> 说明：本 PRD §9-4 原期望「K26/K27 归 ADR-0033（属性系统与矩阵参数）」——实际 ADR-0033 槽位
> 被「addon 注册表与包型 addon 化」占用，属性系统的契约承载 = **architecture §15.3（全节）+ 实现**；
> K28/K29 同理未入 ADR-0032 正文。校准事实以 T-293 回写落点为准（下表第三列），效力等同 ADR 校准。

| # | 终裁（as-built） | 回写落点 |
|---|---|---|
| K23 | license 文档 v1 = `<b64url(payloadJSON)>.<b64url(ed25519Sig)>`，payload 字段 typ/alg/kid/ver/licenseId/licensee/tier/issuedAt/notBefore/expiresAt/addons/limits；验签公钥**编译期内嵌**（kid 单元素表 + 多 kid 轮换缝），测试经 Manager 构造注入——**「config 可覆盖公钥」未实现**（Q1 暂行的 air-gapped 自签场景不成立；如需运行时换钥，M11+ 出新 ADR）；leeway 1h、State atomic 快照、每日 ticker 降级 | ADR-0032 决策 1/2 + architecture §15.1.1/15.1.2（契约正文）；本行补记公钥不可覆盖事实 |
| K24 | 键名定案 `addons.disabled`（CSV 标量，env `BINFLOW_ADDONS__DISABLED`）；严格 schema（未知子键/列表形拒绝）；含五核心 id 时 WARN 但生效；**重启生效**（静态装配消费） | ADR-0032/0033 后果 + architecture §15.5 |
| K25 | D2 拒绝形 = 403 errors[] 信封 + 头 `X-Binflow-License-Required: <addonID>`（allowlist 收窄形专用措辞；disabled 熔断形**不带头**）；织入位置 = 三既有决策缝（dispatchContent 写臂 / repo 校验链 / handler 首行）+ /v2 根级写臂，恒在 RBAC 后；`/api/system/licenses` 旧路径 = **E-26 404**（非 400） | ADR-0032 决策 3/4 + **as-built 定案段**（D1 优先一并定案）；architecture §15.1.3/15.1.5 + §7.1 [M10] 清点 |
| K26 | 剥离规则 = **首个 `;` 起的后缀整体匹配 `;k=v(;k2=v2)*` 才剥离**（成对判定），键 `[A-Za-z][A-Za-z0-9_.-]{0,63}`、值 ≤1024B 无控制字节、对数 ≤64；非 k=v 形 `;` 维持字面路径（§11.39 兼容债）；repoKey 段同样剥离；`storage.matrix_params` 默认 **on**（逃生开关）；docker 豁免（/v2 无矩阵参数语义） | architecture §15.3.1（契约正文）+ §11.39；T-286 实现 |
| K27 | 键 `[A-Za-z][A-Za-z0-9_.-]{0,63}`（**首字符须字母**——PRD 暂行 `[A-Za-z0-9_-]+` 作废）；值非空 ≤1024B 无控制字符；单键 ≤32 值；每节点 ≤64 键（PRD 暂行「≤500」作废——与矩阵参数对数上限同值收窄攻击面）；PUT = 同名键值集替换/异名保留的自有 merge（§11.40 待 Artifactory 双证校准） | architecture §15.3.2 + **§15.3.3 as-built 注记**；`internal/metadata/props.go` 常量 |
| K28 | NuGet v2 最小集 as-built（T-287）：`FindPackagesById()?id=`（OData Atom，未知 id = 空 feed）+ `$metadata`（EDMX，老客户端可发现性）+ v2 push `{id}/{version}` + LegacyGallery 别名；**`Search()`/`Packages()Id=`/`$count` 不做**（404）；T-287 七项自有裁定（L1~L7：DELETE 硬删无 listed 位/search=存储事实/上游前缀常量/512MiB 上限/版本键官方归一化/virtual search 贡献=local+已落地缓存）经 T-293 复核**全部维持**。T-280 规格缺失处置见 88.4 登记（建议免补票，conductor 终裁） | 本表 + FR-88.4 登记；architecture §12-18 |
| K29 | scope 等价物 as-built = **认证 + 目标仓写门**（routeAuth required + handler 内 path `w`）；会话 id = 不可猜测 capability（§5.3.1 契约 4 同源，无 BinFlow token scope 模型故无更细粒度）；complete 校验链 = `sha256` 必填 + `sha1`/`md5` 可选、错 sha256 → **409** 且会话消费；Commit 后建 node 失败 → 5xx safe-to-retry | ADR-0032 **as-built 定案段**②；architecture §15.4.1（八臂全契约） |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| PRODUCT.md「明确不做」 | M10 引入 license 门控是否越界 | 不越界：门控基座是「未来企业功能的解锁面」，ha/xray 槽位仅占位声明，本体仍属 Non-goals 直至用户修订 PRODUCT.md | 用户知悉（BOARD 已录口径） |
| ADR-0001（clean-room） | license 格式/算法与 addon 装配格式是逆向高危区 | 自有设计：ed25519 + 自有文档结构；注册表为 Go 编译期机制；行为规格以「行为模式」粒度引用 | ADR-0032 记录 clean-room 边界 |
| ADR-0026~0028（RBAC/step-up/Close） | 门控织入不得破坏已收口端点族 | 门控位于认证后、内容处理前；既有端点门语义零变化（FR-85-AC6 回归） | ADR-0032 定织入点 |
| ADR-0025 决策 1（replica 暂行）+ M9 Q5 | 复制硬化归属 | 建议 M11 立项（与 smart-remote contentSynchronisation 同域，消费 M10 属性成果） | M11 规划输入 |
| 主矩阵五/J1（licenses REST） | Artifactory licenses 面在开源对标下标「不适用」 | BinFlow 依用户指令引入等价行为模式（分级门控），载体全自有 | 本 PRD §5.2/5.3 留痕 |
| gap-endpoints E-09（?properties 404） | 显式 404 归档将被反转 | FR-89 落地后反转，回归表 §5.4 登记 | tech-writer 同步 api-reference |

> **T-293 as-built（2026-08-26）**：E-09 反转已落地并回写——api-reference.md 的 `?properties`
> 行族修正为 **GET/PUT/DELETE 三动词**（M10）：删除 M5 期即存在的两条陈旧行（内容面路径的
> DELETE ?properties——as-built 属性动词仅挂 `/api/storage` 族；POST 增量行——router 三动词
> 冻结，POST 落 E-26 404）；`PUT {path};k=v` 矩阵参数标注 M10 生效。M10 全量文档面（license/
> addons/uploads/Go/NuGet 接入指南）归 T-296 文档五项票。

### 6.2 性能（M10 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P43 门控热路径 | 门控判定为进程内原子读（零 DB 查询）：匿名 GET P95 基线（M7 6.8ms / M8 0.606ms / M9 归档值）偏差 <10% 维持 | P0 |
| NFR-P44 license 切换时延 | 安装/卸载 REST 返回后 ≤1s 全局生效（L08 实测）；并发无撕裂（FR-85-AC3） | P0 |
| NFR-P45 试点协议面 | 100 并发 `go mod download` / `dotnet restore` 零 5xx；remote 缓存命中 P95 对齐既有 remote 指标口径 | P1 |
| NFR-P46 属性写放大 | 矩阵参数 PUT（2 键）与无属性 PUT 吞吐偏差 <10%（本机 scratch 对比归档） | P1 |
| NFR-P47 资源基线维持 | 冷启动 <2s、空载 RSS <100MB（license 加载 + 注册表 + 属性表不破基线） | P0 |

### 6.3 安全底线（M10 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S52 license 防伪 | ed25519 验签失败 400 拒装（错误体不泄露校验内部细节防探针）；license 文档全文与签名不落普通日志/审计（redact，仅 tier/licensee/期限元数据） | L03 |
| NFR-S53 门控 fail-safe | 损坏/不可验证 license → 降级 community + 告警，不拒启动、**不维持不可验证高档位**；门控检查先于内容处理，未解锁能力任何入口不可达 | L05/FR-84-AC6 |
| NFR-S54 熔断不删数据 | addons.disabled 与降级均零删除（仓配置/blob 完整），恢复后 sha256 对账全量一致 | L07/L06 |
| NFR-S55 属性注入防护 | 键字符集白名单 + 键/值/per-node 上限（K27）→ 400；属性值存储与回显转义（XSS 面，UI Tab 验证） | L20/L21 |
| NFR-S56 密钥隔离 | 测试根密钥与生产根密钥隔离（测试签发文档在生产公钥下必拒）；签发工具私钥仅本地文件、零遥测 | L03 + 工具审计 |

### 6.4 可观测性（M10 增量）

- 新审计事件：`license.install` / `license.delete`（actor/tier/licensee/期限）；`addon.gate.deny`（变更面逐条：addon/所需档位/principal 摘要；读面仅 metrics）。
- /metrics 新增：`binflow_license_tier`（gauge 或 info 型标签）、`binflow_license_expiry_days`、`binflow_addon_gates_total{addon,decision}`。
- 启动日志一行生效档位与 license 来源（installed/none/宽限剩余天数）；降级事件 WARN 级。
- 五包型既有指标族与口径冻结维持。

---

## 7. 开放问题（Q1~Q7，均带暂行；需用户/conductor 决策，PM 不代拍）

| # | 问题 | 影响面 | 暂行口径（v1.0） |
|---|---|---|---|
| Q1 | **license 密钥管理流程**：根密钥谁保管、签发工具形态（`bf license generate` vs 独立工具）、CI 测试密钥策略、公钥内嵌 vs config 可覆盖 | FR-84；运营流程 | 离线签发工具 `bf license generate/inspect`（私钥文件本地持有，用户保管根密钥）；CI 用公开测试密钥对（生产公钥下必拒，天然隔离）；二进制内嵌默认公钥 + config 可覆盖（air-gapped 自签场景）——终裁归 ADR-0032 |
| Q2 | **无 license 默认行为**：全解锁 community 档还是锁死 | 全部既有用户体验；迁移叙事 | **community 档全解锁**（对标 OSS 行为模式；M1~M9 承诺不因 license 体系引入而回归断裂）——锁死会造成「升级即停摆」事故面 |
| Q3 | **档位命名与矩阵终版**：community/pro/enterprise 三档是否定名；cargo/properties/ha 等槽位分档是否调整 | §5.2 核心矩阵；营销口径 | 三档暂行定名如上；五基础包型与 properties 归 community（有意不兼容 Artifactory 分级，理由 §5.2）；enterprise 档槽位清单随 M11+ 功能本体立项扩 |
| Q4 | **试点包型定案**：Go + NuGet 是否确认；Cargo 余量第三试点触发条件 | FR-87/88 拆票与 QA 面积 | Go + NuGet（Go 为第一梯队高需求 + GOPROXY 有公开规范；NuGet v3 面对 .NET 生态刚需）；Cargo 仅在 NuGet 提前收官且余量足时触发（明确非 DoD 硬门） |
| Q5 | **矩阵参数与存量路径归一的兼容风险**：`;` 在 M1~M9 是合法文件名字符，剥离语义是数据面变更 | FR-89；存量实例升级 | 成对 `;k=v` 尾随序列才剥离（非成对 `;` 维持字面）；存量种子回归腿（L22）+ `storage.matrix_params` 逃生开关（默认 on）——终裁归 ADR-0033 |
| Q6 | **license 过期/降级行为**：宽限期天数；降级后高级仓写入是否拒绝（读恒放行已定） | FR-84-84.6；续期体验 | 宽限 30 天；降级 = 建仓/写入/删仓拒绝 + 读恒 200（数据不劫持）——天数与写入拒绝范围可调 |
| Q7 | **smart-remote 字段生效子集**：M10 只落 timeout/缓存期三字段是否足够；unusedArtifactsCleanupPeriodHours 进 P2 还是 M11 | FR-90 范围；remote 调优面 | 三字段 P1 + unusedArtifactsCleanup P2（缓存清理 cron 最小实现）；enableTokenAuthentication/contentSynchronisation 确定归 M11 复制硬化（严格 schema 维持，不做 inert 字段） |

---

## 8. M10 验收剧本（QA 总纲）

1. **回归基线（硬门槛先行）**：M1~M9 全部 P0 序列在无 license 默认实例复跑全绿；服务端契约变更面（`git diff m9-done..HEAD -- internal/ cmd/`）100% 归属 M10 豁免票（ADR-0032/0033 域内）。
2. **license 域**：L01（签发自检 + 篡改探针）→ L02（安装/查询/卸载闭环 + 审计）→ L03（防伪 + LC-02 负向）→ L04（无 license 默认）→ L05（过期宽限/降级）。
3. **门控与注册表**：L06（三入口门控 + 降级不劫持）→ L07（熔断与恢复 + sha256 对账）→ L08（切换时延 + 无撕裂并发）→ L09（metrics/审计）→ L27（控制台 License & Add-ons 页）。
4. **Go 试点**：L10~L13（三件套 → 真实客户端 local → remote pull-through 缓存 → virtual 聚合）+ 门控腿（FR-87-AC5）。
5. **NuGet 试点**：L14~L17（v3 push/consume → v2 FindPackagesById → remote/virtual）+ 门控腿；Cargo 余量腿条件执行（AC6 口径）。
6. **属性系统**：L18~L22（矩阵参数 → REST 读写删 → 校验 → UI Tab → 存量 `;` 回归与逃生开关）。
7. **快赢包**：L23/L24（MPU 全链 + 断点 + filestore 诚实）→ L25（smart-remote 生效探针 + 未知字段 400 维持）。
8. **规格与文档**：L26（五份结构走查 + clean-room 抽查 + tech-lead 就绪度确认）。
9. **NFR 收口**：L29/L30（门控热路径 P95、切换时延、试点并发、属性写放大、冷启动/内存）。
10. **文档**：tech-writer 交付——license 安装与档位说明（含 Q2 无 license 行为明示）、addon 矩阵页、属性用法（矩阵参数 + `?properties`）、Go/NuGet 接入指南、api-reference 更新（新端点 + E-09 反转）。

---

## 9. M10 DoD

1. §4 全部 P0 AC（FR-84/85/86/87/89）经 qa 验证全绿；P1（FR-88/90/91）全绿（FR-88-AC6 Cargo 余量腿与 FR-90 smart-remote P2 子集按余量条款——未触发不构成 DoD 缺口，须在 BOARD 留痕）；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（槽位数/门控覆盖率/切换时延/两试点真实客户端链/属性往返/规格交付——全部有实测数字归档）；
3. 回归硬门槛：M1~M9 全部 P0 序列在无 license 默认实例复跑全绿；五包型行为零变化；服务端契约变更面 100% 归属 M10 豁免票；
4. 前置产物齐备：ADR-0032 Accepted（license 文档/档位/门控/注册表，含 K23~K25/K28/K29 校准回写）；ADR-0033 Accepted（属性系统与矩阵参数，含 K26/K27）；
5. tech-writer 五项文档交付（§8-10）；规格预研 5 份 + tech-lead 拆票就绪度确认（M11 输入就位）；
6. NFR-P43~P47 达标归档；`make test`（race）/`make lint` 0 issues / gofmt 空维持；
7. §2.2 Non-goals 与 §4.7 M9 候选池处置对账完成：滚入 M11+ 项在 ROADMAP「M10 未纳入项」登记无遗漏；
8. 主会话 git tag `m10-done`（对外发布任何制品、license 根密钥交付形态均先经用户确认）。
