# PRD — M11 Artifactory 对齐第二程：配置域指令兑现 + 第一梯队包型批量实现 + 行为逐项对齐制度化

> **PRD 状态：v1.0 草案（2026-08-26，待 conductor 审）**。主轴来源：BOARD 用户指令日志 7 条中的最近四条（2026-08-26 11:22 MUI 迁移〔T-299/T-300 已定名〕、11:35 认证配置前端化、11:45 存储配置独立文件化、**19:05 license 门控「行为逐项对齐」**——最后一条改变 M11 起的全部工作方式）+ conductor M11 范围种子（A 指令兑现 / B 包型批量 / C 债务 / D 候选池裁量）。范围消费：M10 FR-91 五份规格（tech-lead 就绪度确认 reports/agents/tl-fr91-ac3.md——23 裁决点 + K-1/R-1/R-2 缺项）、M10 PRD §2.2/§4.7 滚入项、M9 Q5 复制硬化建议、ROADMAP「M10 未纳入项」。**新端点全部走 PM FR + ADR 流程（ADR-0035 起）**。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-11.md` |
| 里程碑 | M11 — Artifactory 对齐第二程（认证/存储配置化 + 存量 MUI 迁移 + 自有裁定回头看 + conan/debian/rpm/helm 四包型 + cargo 补齐 + 复制硬化与工程债） |
| 状态 | v1.0 草案（FR-92~FR-102 十一条需求；契约矩阵 18 条〔A 14 / C 2 / D 1 / 待裁 1〕+ 档位 × addon 矩阵扩展 5 槽；L01~L45 验收命令骨架；开放问题 Q1~Q8 带暂行） |
| 上游依据 | PRODUCT.md（Non-goals 不越界：HA/Xray 本体仍不做——Q1 待用户终裁）、BOARD.md 用户指令日志（2026-08-25 三条主轴指令 + 持续部署 T-298〔已兑现〕+ 2026-08-26 四条新指令）、docs/reverse/artifactory-full-feature-matrix.md（213 条目主矩阵 §十大缺口）、docs/reverse/{conan,cargo,debian,rpm,helm}.md（FR-91 产出 + tl-fr91-ac3 23 裁决点）、docs/reverse/auth-integration.md（FR-92 行为基准——M11 前置复核）、docs/reverse/config-formats.md §1 + s3-storage-layout.md（FR-93 行为基准——M11 前置复核）、docs/reverse/replication.md（FR-101）、docs/prd/milestone-10.md（§2.2 滚入项 + §5.6.1 as-built 校准表 + DoD 体例）、ADR-0032/0033/0034（license/属性/五协议管理面横切——DB-4/TL-1 已由 ADR-0034 承载，tl 报告 K-3 收口） |
| 下游消费者 | tech-lead（拆票——tl-fr91-ac3 23 裁决点随票消化 + 本 PRD §1.3 分票提示，宽度 ≤2 内建）、architect（**ADR-0035**：认证配置面 REST/持久化/变更即生效；**ADR-0036**：存储配置独立文件与链式 schema；视需要 ADR-0037 复制硬化语义）、reverse-engineer（两份复核票 + R-1/R-2 规格修订）、dev-go-core（internal/auth 配置面 + internal/adapter/{conan,deb} + 复制硬化）、dev-registry-adapter（internal/adapter/{rpm,helm,cargo}）、dev-go-storage（存储配置链 + S3 续传债）、dev-frontend（T-299/T-300 + 认证配置页）、qa-engineer（L 序列 + 四包型真实客户端矩阵）、tech-writer（认证/存储配置指南 + 四包型接入 + 回头看文档同步）、release-engineer（部署矩阵演进 + CD 链验证）、conductor（裁决入口 + tag m11-done） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-26 | 初版草案（待 conductor 审）：M11 范围（conductor 种子 A~D 全承载）、FR-92~FR-102（认证配置前端化 / 存储配置文件化 / MUI 两批 / 回头看 / conan / debian / rpm / helm / cargo remote+virtual / 复制硬化 / 工程债）、档位 × addon 矩阵扩展、契约矩阵 18 条、L01~L45、开放问题 Q1~Q8 带暂行；随稿完成 ROADMAP M11 段补实 |

---

## 1. 背景与目标

### 1.1 背景

M10 交付了 license 门控基座、addon 注册表、三个包型 addon（go/nuget/cargo）与属性系统（21/21，tag `m10-done`），并把「全功能对齐 Artifactory」推进到有挂载点的阶段。M11 面对三股输入的汇合：

1. **用户指令密集落地（A 组，P0）**：2026-08-26 一天内四条指令——认证配置前端化（OAuth2/LDAP/SAML 控制台可配置 + 严格对齐 Artifactory 行为）、存储配置独立文件化（binarystore.xml 行为模式）、存量控制台 MUI 化两批（T-299/T-300 已定名）、以及**「行为逐项对齐」工作方式指令**（19:05：license 门控功能的可观测行为/命名/语义/错误码/交互 100% 照 Artifactory，「不要有太多自己的想法」；自有裁定权收归用户；规格票逐条附 Artifactory 行为出处）。前三条是功能，第四条是 M11 全程的**制度性条款**。
2. **第一梯队包型批量实现的条件已齐（B 组）**：FR-91 五份规格（conan/cargo/debian/rpm/helm，1158 行零低置信）+ tech-lead 就绪度确认（5/5 可拆、23 裁决点已裁、阻塞缺项 0）。协议适配票必须依赖规格票（BOARD 既定规矩）——M11 是该红利的兑现窗口。
3. **M10 债务与 M9 遗留（C 组）**：S3 MPU kill -9 续传（T-289 AC2 descope 债）、unused-cleanup 引擎（T-290 勘误③收窄）、复制硬化域（M9 Q5 + smart remote `enableTokenAuthentication`/`contentSynchronisation`——与 M10 属性系统天然同域）、D-8 boot footprint、D-9 测试基建。

**不贪多（M10 教训：基座优先）**：候选池 D 组（Trash can / Cleanup-Retention 策略引擎 / 制品操作族 / Webhook / AQL）全部建议 M12+，仅 Trash can 以余量条件票形式保留（Q7，非 DoD 硬门）。M11 已处满载——任何 Q 触发的增项（如 HA 本体）必须等量置换。

### 1.2 M11 目标与量化门槛

> 一句话：兑现四条用户指令（认证配置前端化、存储配置文件化、MUI 两批、行为逐项对齐制度化），批量落地第一梯队四包型（conan/debian/rpm/helm）并补齐 cargo 家族，偿清 M10/M9 关键债务——全程以「行为逐项对齐」为工作准绳。

量化门槛（未达即里程碑不完成）：

| 指标 | M11 门槛 | 来源 |
|---|---|---|
| 认证配置变更即生效 | REST 返回后 ≤1s 下一次认证走新配置（不重启）；mock 目录 A→B 切换断言 + Keycloak 真容器腿 | FR-92 |
| 认证配置面覆盖 | OAuth2(OIDC)/LDAP/SAML 三协议配置段 REST 往返 + 测试连接 + 控制台页组（MUI）全绿；敏感字段零明文回显 | FR-92 |
| 存储配置文件化 | 独立文件表达 filestore/S3/dual-write 三链全部起服 + roundtrip sha256 对账；存量 binflow.yaml 内嵌段实例零破坏 | FR-93 |
| MUI 迁移 | 两批完成，console-ux 册锚零改动（交互逻辑零变化）+ anchor ledger PASS + 全量 Playwright 绿 | FR-94 |
| 回头看裁决率 | 基线清单（M10 自有裁定 23 项 + tl-fr91-ac3 与 Artifactory 有可观测差异的裁决点）100% 出结论（维持须附出处 / 改回须验证 / 分歧上 BOARD），零无出处维持 | FR-95 |
| 新增包型真实客户端 | conan 2.x / apt-get / dnf / helm 3 真实客户端 local 全链绿（unsigned 模式）；cargo remote/virtual 绿 | FR-96~FR-100 |
| 门控全链 | 新四包型槽位按 §5.2 矩阵：community 建仓 403 → pro 200 → 卸载降级 pull 200/push 403（降级不劫持维持） | FR-96~FR-99 |
| 复制硬化 | contentSynchronisation 属性同步端到端（源打标 → 目标到达）；M10「按名 400」分支退役反转回归绿 | FR-101 |
| 工程债收口 | S3 MPU kill -9 续传复活（status 保留 → 续传 complete）；空载 RSS ≤100MB 恢复（PRODUCT.md 基线）；D-9 修复后默认并发 e2e 三连绿 | FR-102 |
| 规格复核交付 | auth-integration.md / config-formats.md §1 两份复核版（逐条附 Artifactory 行为出处）+ R-1/R-2 修订合入，tech-lead 就绪度确认 | FR-92/93/95 |

### 1.3 上游依赖与并行关系（含分票提示）

- **前置产物（architect，拆票前）**：ADR-0035（认证配置面：REST 路径/信封、DB 配置描述符模型、变更即生效机制、双源优先级、敏感字段脱敏形态）；ADR-0036（存储配置独立文件：文件名/位置、链式 provider schema、冲突与兼容处置、fail-fast 语义）；复制硬化语义若超出 replication.md 既有锚点则 ADR-0037。
- **前置规格票（reverse-engineer，实现票依赖）**：① auth-integration.md 复核票（FR-92 唯一行为基准——该规格写于 M6 前后，SAML 段〔§3〕属低置信区须补齐或明确边界；复核按 19:05 口径逐条附 Artifactory 行为出处）；② config-formats.md §1 复核票（FR-93 行为基准：provider 链/模板体系/文件位置语义）；③ R-1（cargo.md yank 404 行 + config.json base 来源）/ R-2（helm.md §7.3 改写目标改主面）随本触点合入。
- **BOARD 既定票**：T-299（MUI 批次一）/ T-300（批次二）已定名定 area，进 M11 首批。
- **23 裁决点（tl-fr91-ac3）**：由 tech-lead 拆票时消化（改判 2 项〔CN-1/CG-2〕与各定案直接进票面 AC）；其中与 Artifactory 有**可观测默认值差异**的裁决点（RP-2/TL-5/TL-4/HL-2 等）按 19:05 口径须上 BOARD 用户终裁（Q8），拆票以暂行值开工、终裁后翻转。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-92 拆 3 票（复核票 → BE 票〔internal/auth + httpapi + metadata〕→ FE 票〔web 认证配置页组〕，串行链）；FR-93 拆 3 票（复核票 → BE 票〔internal/config + storage 装配〕→ 部署/文档票〔charts/deploy/systemd + CD 链〕）；FR-94 即 T-299/T-300 两票（web/ 域，互斥分批）；FR-95 一票裁决票（docs-only，规划期完成）+ 改回项随域票；FR-96~99 各拆 local P0 一票 + remote/virtual P1 一票（四型 area 不重叠可并行波次；rpm local 票内含 header 解析器子任务；helm 经典仓与 HelmOCI 票 area 不重叠〔HL-3〕）；FR-100 两票（cargo remote / cargo virtual，dep T-294，同 area 串行）；FR-101 一票；FR-102 拆 2~3 票（S3 续传 + cleanup 引擎 / D-8+D-9 打包）。估 **28~32 票**（含 QA 终验 + 中期回归 + 文档票）。
- **QA 并行面**：L 序列按 FR 分组；四包型真实客户端矩阵（conan/apt/dnf/helm）需容器化环境编排（VM Jenkins 可复用）；认证配置需 mock 目录 + Keycloak/OpenLDAP 容器（M6 夹具复用）；行为对齐审计腿（L15~L17）专门剧本。

### 1.4 全程工作方式条款（用户指令 2026-08-26 19:05，硬约束——M11 全程生效）

1. **行为逐项对齐**：license 门控相关功能（M11 全部范围：包型槽位、认证配置、存储配置、复制硬化等）的**可观测行为 / 命名 / 语义 / 错误码 / 交互 100% 照 Artifactory**；反编译代码作为行为参考精读（对照到行为规格粒度）。
2. **自有裁定权收归用户**：M11 起任何与 Artifactory 的行为分歧必须上 BOARD 请用户裁决——conductor / architect / tech-lead / PM **不再自裁**（PM 本 PRD 的暂行值仅为开工口径，标注「暂行待裁」）。
3. **规格票出处义务**：所有 M11 规格票（含复核票）必须逐条给出「Artifactory 行为出处（反编译类/方法 + 行为描述）」而非「等价设计」。
4. **红线保留（ADR-0001 不变，用户知情确认）**：不逐行翻译 Java→Go——reverse-src 是 JFrog 版权反编译产物，逐行翻译 = 版权代码进入发布物；license 文档格式维持自有 ed25519（其可观测行为面已对齐）。**公开规范的协议以官方规范为准（PRODUCT.md / ADR-0001 既有红线）**：规范覆盖处以规范为准，规范未覆盖或 Artifactory 特有扩展（管理面、默认值、错误码超出规范处）100% 照 Artifactory。
5. **先例**：D-6（matrix_params 逃生开关不实现——Artifactory 无此开关）是该指令第一次应用；FR-95 是其制度化执行面。
6. **流程条款（延续）**：新端点走 PM FR + ADR 流程，实现票不得私加端点；拆票宽度 ≤2；协议适配票必须依赖对应规格票；每票 AC 附真实客户端验收命令。

---

## 2. 范围

### 2.1 In scope

| # | 来源（指令/种子/规格） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 用户指令 11:35 + auth-integration.md | FR-92（认证配置前端化：OAuth2/LDAP/SAML 管理面 REST + 测试连接 + 变更即生效 + 控制台页组） | P0 |
| A | 用户指令 11:45 + config-formats.md §1 | FR-93（存储配置独立文件化：链式 provider 表达 + 兼容窗 + 部署矩阵演进） | P0 |
| A | 用户指令 11:22（MUI） | FR-94（存量控制台 MUI 化两批：T-299 批次一 / T-300 批次二——交互逻辑零变化） | P0（批一）/ P1（批二） |
| A | 用户指令 19:05 | FR-95（M10 自有裁定回头看：对齐审计——基线 23 项 + tl 裁决差异点逐条复核） | P0 |
| B | 用户指令③ + FR-91 规格 + tl-fr91-ac3 | FR-96（conan 包型：v2 local 全量 + v1 握手三端点 + remote/virtual） | P0（local）/ P1 |
| B | 同上 | FR-97（debian 包型：automatic local 主票 + virtual/remote + trivial P2 增量） | P0（local）/ P1 |
| B | 同上 | FR-98（rpm 包型：local 管线〔header 解析器〕+ reindex 端点族 + remote/virtual） | P0（local）/ P1 |
| B | 同上 + Q2 | FR-99（helm 经典仓：local + virtual/remote；HelmOCI 条件票） | P0（local）/ P1 |
| B | T-294 遗留 + cargo.md S4 | FR-100（cargo remote pull-through + virtual 聚合——cargo 家族补齐） | P1 |
| C | M9 Q5 + M10 §2.2 + replication.md | FR-101（复制硬化：enableTokenAuthentication/contentSynchronisation 生效 + 属性复制同步 + replica 隔离终裁执行） | P1 |
| C | T-289/T-290 债 + T-297 D-8/D-9 | FR-102（工程债打包：S3 MPU kill -9 续传 / unused-cleanup 引擎 / D-8 footprint / D-9 测试基建 / 文档尾巴三处） | P1 / P2 |
| D | 候选池裁量 | （不设 FR）Trash can 余量条件票（Q7）；Cleanup-Retention 策略引擎 / 制品操作族 / Webhook / AQL → M12+ | — |

前置产物（非 FR）：ADR-0035、ADR-0036（、视需要 ADR-0037）；规格复核票两份 + R-1/R-2 修订。

### 2.2 Non-goals — M11 明确不做

**产品级（继承 PRODUCT.md，不越界）**：HA 集群本体与 Xray 集成面本体不做（槽位维持 enterprise 占位——「行为逐项对齐」指令**不自动解锁功能本体**，须用户修订 PRODUCT.md，Q1 终裁）；不做 Artifactory 全量 REST 兼容（高频子集承诺维持）。PRODUCT.md「明确不做」中「不做 LDAP / SAML / OIDC」一行已被 M6（OIDC/LDAP）与 M11 指令（SAML 配置面）先后解禁——**建议用户随 M11 修订 PRODUCT.md 该行**（PM 不代改）。

**M11 里程碑级 Non-goals**：

| 不做项 | 隔离边界 / 去向 |
|---|---|
| HA 本体（集群心跳/推举/传播）、Xray 集成面本体 | Q1 用户终裁；暂行不进（进则须等量置换现有范围） |
| AQL + 13 老搜索族 | M12+（十大缺口 2；依赖属性系统深化，与 M11 复制硬化部分同域先行） |
| Cleanup/Retention **策略引擎**（治理面） | M12+（缺口 4）；M11 仅 FR-102 的 remote 缓存 unused-cleanup 引擎——两者不同物，勿混淆 |
| 制品操作族（copy/move/zap/目录 zip/`archive!/`/explode） | M12+（缺口 5） |
| Webhook / 统一事件总线（36 事件） | M12+（缺口 6，可整体平移） |
| Trash can 回收站 | **余量条件票**（Q7，非 DoD 硬门）；未触发则 M12+（缺口 3） |
| Build-info 域 / Go 深化（sumdb 代理、external 重定向） | M12+ |
| Terraform / GitLFS 实现（FR-91 余量第 6/7 份规格未执行） | M12+（规格随 M12 规划另派） |
| NuGet symbol server | M12+（T-293 终裁：立项时随票补 as-built 规格） |
| 制品 license 识别（licences.xml 91 模式）/ 冷存储分层 | M12+（ROADMAP DoD-7 补词显式条目） |
| HuggingFace 等 AI/ML 13 型 | M12+ 分期（用户指令③远期主体） |
| conan v1 files 直传通道（及 v1 其余数据面） | **不做**（CN-1 改判收窄：仅握手三端点；conan 1.x EOL，qa 提出兼容诉求再立项）——按未知路由 404 |
| cargo 失败「200+errors」双轨形态 | **不做**（CG-2 改判：统一 4xx/5xx + errors 信封） |
| license 公钥 config 覆盖（运行时换钥） | M12+（T-293 终裁③：需求走新 ADR，不入 M11） |
| 逐行翻译 Java→Go / 复制 JFrog license 密钥格式 | **永久不做**（ADR-0001；19:05 指令用户知情确认保留） |
| M10 未纳入项其余（票级遗留 17 条、E-04/R2/R6 等） | 滚入 M12+ 候选池（ROADMAP 已列；M11 仅收编 C 组点名项） |

---

## 3. 用户与场景（M11 视角）

- **场景 A（平台管理员，认证配置）**：公司接入 AD 域。管理员在控制台 Admin > Security 配 LDAP（host/Base DN/搜索模式），点「测试连接」验证可达，保存即生效——无需 SSH 改 YAML、无需重启；SAML/OIDC 同构。变更全程审计、client secret 不回显。
- **场景 B（运维，存储演进）**：单盘 filestore 起家的实例要迁 S3。运维改**独立存储配置文件**为 dual-write 链（filestore→S3），主配置 binflow.yaml 不动；迁移完成后切纯 S3。CD 链（Jenkins → VM 172.16.58.129）验证切换，数据目录零触碰。
- **场景 C（多生态研发团队）**：C++（conan）、系统组（apt/deb）、运维组（dnf/rpm）、平台组（helm chart）四个团队把 BinFlow 当唯一内网源——四种真实客户端零学习成本接入（对齐指令的直接受益面）。
- **场景 D（license 运营）**：pro license 到期。已建的 conan/deb/rpm/helm 仓 pull 仍 200（数据不劫持），push/建仓 403 提示所需档位——M10 降级语义在新四包型上不变。
- **场景 E（对齐审计，用户新增视角）**：用户逐项核对 BinFlow 与 Artifactory 行为一致性——19:05 指令后，任何「BinFlow 自己的想法」要么有 Artifactory 出处背书，要么经用户裁决留痕。FR-95 把 M10 存量裁定全部拉回该口径。
- **场景 F（复制运维）**：双数据中心互推。管理员启用 contentSynchronisation 属性同步——源实例打的 `build=77;env=prod` 标随复制到达目标，统计同步开启——目标侧 usage 面一致。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；Playwright spec 置 `web/e2e/m11/`；新端点 wire 细节以 ADR-0035/0036 定案为准，本节 AC 钉行为与状态码；**行为基准冲突时的效力序：用户裁决（BOARD）> 复核版规格 > ADR > 本 PRD 暂行值**（19:05 条款）。全部实现票不得私加端点。

### 4.1 认证配置前端化（用户指令 11:35，P0）

#### FR-92 OAuth2/LDAP/SAML 管理面配置：REST + 测试连接 + 变更即生效 + 控制台页组（internal/auth + httpapi + metadata + web）

**用户故事**：
- 作为实例管理员，我在控制台 Admin > Security 域配置外部认证（OAuth2/OIDC、LDAP、SAML），保存后**即刻生效**（不重启），下次登录就走新配置。
- 作为管理员，保存前我能「测试连接」验证目录/IdP 可达性与凭据正确性，失败信息可诊断但不泄露凭据。
- 作为安全审计员，每次配置变更有审计行；client secret / 私钥等敏感字段永不回显明文、不落普通日志。

行为规格：

- **92.1 前置复核票（reverse-engineer，本 FR 唯一行为基准）**：auth-integration.md 按 19:05 口径复核——LdapSetting/LdapGroupSetting/SearchPattern（§1）、OAuth 设置（§2）、SAML 设置（§3，低置信区——补齐或明确边界）、用户自动创建流程（§4）、配置优先级与生效语义；**逐条附 Artifactory 行为出处（反编译类/方法 + 行为描述）**；tech-lead 就绪度确认。
- **92.2 REST 面（wire 归 ADR-0035）**：三协议配置段读/写（Artifactory `GET/POST /api/system/configuration` 的 ldapSettings/oauthSettings/samlSettings 段行为模式——路径形态以复核票锚点为准）+ 测试连接端点 + 启停语义。**变更即生效**：写成功后下一次认证请求走新配置（Artifactory 语义，不重启）。
- **92.3 持久化与双源**：配置落 DB（配置描述符为中心的行为模式）；binflow.yaml `auth.oidc/ldap` 段处置见 Q4（暂行：DB 配置面权威，文件段作首启种子，运行期文件段被覆盖时 WARN）。
- **92.4 控制台**：admin 认证配置页组（MUI 组件层〔FR-94 口径〕+ Artifactory 交互层：Admin > Security 同构、三协议 Tab、测试连接按钮、启停开关）；readonly_admin 只读；普通 user 导航不可达。
- **92.5 收敛进既有认证多臂链**：新配置面只改配置来源，不另起认证栈——Basic / api-key / Bearer+OIDC / 裸 token / cookie 五臂行为零变化（M6/M7 回归硬门槛）。
- **92.6 审计与脱敏**：审计 `auth.config.update`（actor/协议段/变更摘要——敏感字段 redact）；敏感字段回显脱敏形态按复核票（Artifactory 回显形态优先）。
- **92.7 SAML 深度（Q3 暂行）**：配置面字段集照 Artifactory samlSettings 落地；SAML 运行时（SP 断言消费登录流）按复核票结论单列票或 M12——暂行：配置面先行、运行时不含在本 FR DoD。

验收标准（AC）：

- **AC1（复核票交付）**：auth-integration.md 复核版落 docs/reverse/，三协议字段表逐条附 Artifactory 出处、置信度重标；tech-lead 出具拆票就绪确认（缺项清单为空或登记前置票）。
- **AC2（REST 往返）**：GET 认证配置 → 三协议段在位；PUT/POST 修改 LDAP `url` → 200 回显一致；client secret 等敏感字段回显为脱敏形态（明文 grep 零命中）。
- **AC3（变更即生效）**：mock 目录 A→B 切换——PUT 新配置后 ≤1s（无重启）`authenticateCredentials` 走 B（旧配置失败/新配置成功双断言）；Keycloak 真容器 OIDC 腿同构（改 issuer/失效旧 IdP）。
- **AC4（测试连接）**：对可达目录/IdP → 成功形态；不可达/凭据错 → 失败形态（错误文案可诊断、不含凭据、无 SSRF 新面——复用 M3 Guard 与 ADR-0025 决策 4 开关）。
- **AC5（控制台）**：Playwright——admin 三协议 Tab 配置往返 + 测试连接交互 + 启停；readonly_admin 只读态；敏感输入框 write-only 形态。
- **AC6（认证回归）**：M6 OIDC/LDAP H 序列抽样（H24 登录流 / H27 admin 映射 / H30 LDAP 流 / H33 admin_group_dn / H36 三臂优先级）+ M7 RBAC 矩阵零回归。
- **AC7（审计）**：每次变更审计行在位（redact 断言）；`make test` 相关包 race 绿 + lint 0。

### 4.2 存储配置独立文件化（用户指令 11:45，P0）

#### FR-93 独立存储配置文件：链式多方案 provider 表达 + 兼容窗 + 部署矩阵演进（internal/config + storage 装配 + deploy/charts）

**用户故事**：
- 作为运维，存储后端配置与主配置解耦——用**专用存储配置文件**（对齐 Artifactory `$JFROG_HOME/var/etc/artifactory/binarystore.xml` 行为模式）声明存储链，filestore→S3 演进不动 binflow.yaml。
- 作为管理员，一份文件表达多方案链：filestore（默认）/ S3 / dual-write 迁移链（远期 cache-fs 层、Azure/GS 留扩展位）。
- 作为存量实例的所有者，升级不炸——旧 binflow.yaml 内嵌 storage 段在兼容窗内继续生效。

行为规格：

- **93.1 前置复核票**：config-formats.md §1 复核——provider 链 / 模板体系 / 文件位置语义逐条附 Artifactory 出处；**格式自由**（BinFlow 用 YAML，clean-room 只对齐行为：独立文件、链式多方案、provider 模板语义）。
- **93.2 独立文件（暂行名 `binstore.yaml`，与 binflow.yaml 同目录；Q5/ADR-0036 终裁名与位置）**：链式 provider 表达——`filestore`（默认，等价现状 disk）/ `s3`（等价现状 s3 段）/ 链式 `dual-write`（filestore→S3 迁移链，消费 M6 三模式语义 bypass/dual-write/completed）；provider 模板语义按复核结论落最小等价（Artifactory 模板体系按需裁剪——分歧上 BOARD）。
- **93.3 兼容与冲突**：存量内嵌 storage 段与独立文件并存处置见 Q5（暂行：独立文件优先、内嵌段 WARN 提示迁移；冲突同写 → fail-fast 拒启动并指明两处来源）；独立文件坏格式/坏链 → fail-fast 拒启动（错误指明文件与行号）。
- **93.4 凭据纪律维持**：S3 secret 等凭据继续走 env 引用（M6 rejectSecrets + AES-GCM 链零变化）——独立文件**不接受**明文 secret 键（出现即拒启）。
- **93.5 部署矩阵演进**：charts / deploy（compose、k8s、systemd）三形态存储配置示例同步；CD 链（T-298 Jenkins → VM 172.16.58.129）验证配置切换——**数据目录不动**。
- **93.6 API 面不动**：storage REST / 迁移端点 / 备份恢复行为零变化（只换承载文件与链式表达）。

验收标准（AC）：

- **AC1（复核票交付）**：config-formats.md §1 复核版 + tech-lead 就绪确认（同 FR-92-AC1 模式）。
- **AC2（三形态全链）**：binstore.yaml 分别表达 filestore / S3 / dual-write 三链 → 起服 → 上传/下载 roundtrip sha256 对账全绿（S3/dual-write 用 MinIO 栈）。
- **AC3（链式语义）**：dual-write 链上传双写、S3 故障降级与 M6 迁移序列行为一致（H 序列口径复跑）。
- **AC4（兼容与 fail-fast）**：存量 binflow.yaml（内嵌段、无独立文件）实例照常起 + WARN 日志在位；并存形态按 Q5 裁定断言；坏文件 → 拒启动 + 错误指明文件/行；独立文件含明文 secret 键 → 拒启。
- **AC5（CD 链）**：VM 经 Jenkins deploy 切换配置形态 → 探针 + 烟测 + 回归冒烟绿（数据目录零触碰、sha256 对账）。
- **AC6（回归）**：M6 S3/迁移 H 序列 + M1 存储序列 + M10 MPU/smart-remote 序列零回归。

### 4.3 存量控制台 MUI 化（用户指令 11:22，批一 P0 / 批二 P1）

#### FR-94 MUI 迁移两批：T-299 批次一（登录/壳层/仓库列表与表单）+ T-300 批次二（制品浏览树/搜索/安全与治理/admin 余面）——交互逻辑零变化（web/，BOARD 已定名）

**用户故事**：作为 Artifactory 迁移用户，控制台交互与 Artifactory 一致（M8 规范既定）不受迁移影响；作为 BinFlow 维护者，组件层统一到 MUI（@mui/material v7 + emotion，T-291 已引入 MuiProvider），降低双栈成本。

行为规格：

- **94.1 批次一（T-299，P0——M11 关键路径首批）**：登录页 / 应用壳层（导航树、模式切换、顶栏）/ 仓库列表与表单域。
- **94.2 批次二（T-300，P1）**：制品浏览树与详情 / 搜索 / 安全与治理页（Users、Groups、Permissions、License & Add-ons 等）/ admin 余面。
- **94.3 交互零变化条款（验收核心）**：仅组件层换 MUI；console-ux 册锚**零改动**（diff=0 或 100% 归属豁免票）；服务端契约 diff=0（FE 票私改契约即违约，M8 熔断线延续）。若发现 Artifactory 交互与 console-ux 册有出入，按 19:05 条款上 BOARD 裁决后先改册再迁移——不得借迁移顺手改交互。
- **94.4 四闸门（每批验收）**：console-ux 锚零改动 + anchor-audit ledger PASS + 全量 Playwright 绿 + assert-tokens 零硬编码；axe 双主题 serious=0；SPA gzip 总量相对 T-291 基线增量 ≤25%（NFR-P51）。

验收标准（AC）：

- **AC1（批次一）**：T-299 四闸门全绿 + axe 双主题全零 + 体积增量达标。
- **AC2（批次二）**：T-300 同上；两批完成后 web/ 无旧组件栈残留页（共享层收敛清单留痕）。
- **AC3（交互零变化）**：console-ux 册 diff=0（豁免票 100% 归属）；M8 零学习成本剧本抽样 4/8 复跑绿。
- **AC4（回归）**：全量 e2e 绿（默认并发）+ 服务端零改动（git diff 审计）。

### 4.4 自有裁定回头看（用户指令 19:05，P0）

#### FR-95 对齐审计票：M10 自有裁定 + tl 裁决差异点逐条复核（docs 裁决票 + 改回项随域票）

**用户故事**：作为用户，我要逐项核对「BinFlow 自有裁定」与 Artifactory 的可观测差异——凡「等价设计」类若有差异，改回 Artifactory 形态（用户原话口径）；作为团队，19:05 之后的分歧不再自裁。

行为规格：

- **95.1 基线清单（复核对象）**：① M10 各票自有裁定 23 项——T-287 NuGet L1~L7 ×7 / T-289 ×5 / T-290 ×4 / T-294 ×7（T-293 已终裁项按新口径重审）；② tl-fr91-ac3 23 裁决点中与 Artifactory 有可观测差异的默认值/形态项——RP-2（calculateYumMetadata 默认 true vs Artifactory false）、TL-5（索引/repomd 默认 SHA-256 vs Artifactory SHA-1）、TL-4（debianDefaultArchitectures 默认关闭）、HL-2（helm relative urls 默认 true）、CG-2（cargo 失败形态按官方规范非 Artifactory 双轨）、CN-1（conan v1 收窄）等；③ D-6 先例复核（matrix_params 无开关——已按新口径裁定，维持即可）。
- **95.2 复核口径（三择一，零悬空）**：**维持**——附 Artifactory 行为出处（「Artifactory 无对应行为」或「无差异」或「公开规范强制」三者之一）；**改回**——立实现动作（随域票或独立小票），改回后按 Artifactory 形态断言；**上 BOARD**——可观测差异且方向不明，请用户裁决（19:05 条款——不再自裁）。
- **95.3 时点**：M11 规划期（拆票前）完成清单裁决并回写本 PRD §5.6.1；改回项实现随各域票消化。
- **95.4 同步义务**：改回项须回归全绿 + 文档同步（api-reference / 用户文档）+ PRD 回写。

验收标准（AC）：

- **AC1（裁决完备）**：基线清单 100% 出结论（BOARD 留痕 + PRD 回写表），零「无出处维持」、零悬空项。
- **AC2（改回验证）**：每个改回项 = curl/真实客户端断言 Artifactory 形态 + 全量回归绿 + 文档同步（逐项登记）。
- **AC3（出处审计）**：维持项出处 grep 可核（反编译类/方法或官方规范锚点）。
- **AC4（回归）**：M1~M10 全 P0 复跑零回归（改回项的连带断言翻转 100% 归属 FR-95 豁免票）。

### 4.5 第一梯队包型批量实现（用户指令③ + FR-91 规格，B 组）

> 统一约定（五型适用）：① 门控槽位见 §5.2（conan/debian/rpm/helm/helmoci = pro 档）；门控行为照 FR-85 既有三缝语义（community 建仓 403 → pro 200 → 降级 pull 200 / push 403）。② rclass 三态 = local / remote（pull-through，复用 M3 缓存 + SSRF + 凭据链）/ virtual（聚合，复用既有机制）。③ 管理面 reindex 族统一走 dispatchAPI 显式路由族 `$BASE/binflow/api/<proto>/...`（DB-4 统一 + ADR-0034 已承载）；权限门 = 认证 + CanManageRepo。④ 对外绝对 URL 一律 `server.base_url` 优先、空值请求推导（TL-1，ADR-0034）。⑤ 规格票已就绪（FR-91），实现票 dep 对应规格票号即可开工；23 裁决点按 tl-fr91-ac3 落票面，Q8 涉及项以暂行值开工、终裁后翻转。⑥ 默认 unsigned 模式验收（GPG keypair K-1 见 Q6）。

#### FR-96 conan 包型（internal/adapter/conan；local P0 / remote+virtual P1）

**用户故事**：作为 C++ 开发者，`conan remote add` 指向 BinFlow 后 upload/install/list 与 conan-center 无差别；conan 1.x 老客户端握手也能过。

行为规格：

- **96.1 local v2 全量（conan.md §3.1 17 端点）**：recipe/package 全 CRUD + revisions 降序 + index.json 修订索引（同构响应体）+ `.timestamp`（TL-3：首写定终身，latest 排序以 index.json `time` 为准；`conan.timestamp.override` 配置位不暴露文档）+ `<ref>` 语法与 `_` 占位（S12）。
- **96.2 v1 握手三端点（CN-1 收窄）**：`ping` / `users/authenticate` / `users/check_credentials`（conan 2.x 硬依赖）；v1 files 直传通道不做（未知路由 404）。
- **96.3 能力头（TL-2）**：`X-Conan-Server-Version` 恒 `0.20.0`；`X-Conan-Server-Capabilities` 按 rclass 输出（local：complex_search,checksum_deploy,revisions,matrix_params；remote/virtual 追加 only_v2）。
- **96.4 remote pull-through（P1）**：默认 conan.center 代理；复用 remote 缓存/SSRF/上游故障降级语义。
- **96.5 virtual（P1，S11 最小实现）**：time 归并 + files 并集 + first-found；`conan install` 经虚仓兜底验收。
- **96.6 管理面**：conan reindex 端点（dispatchAPI 族）。

验收标准（AC）：

- **AC1（真实客户端 local）**：conan 2.x `remote add` → `conan upload`（多包多版本带 revisions）→ `conan install` / `conan list` 全绿；index.json 与修订链 curl 断言齐。
- **AC2（v1 握手）**：三端点 curl 形态断言（conan 1.x 环境可得则活体腿；不可得则 curl 等价 + BOARD 留痕——不许只测 happy path 的负面臂照跑）。
- **AC3（能力头）**：三 rclass 能力头值断言（TL-2 逐字）。
- **AC4（remote）**：`conan install` 公共包经 BinFlow 拉取 + 二次命中缓存断言。
- **AC5（virtual）**：本地优先命中本地 / 未命中走 remote / time 归并正确（三腿）。
- **AC6（门控全链）**：community 建仓 403（错误体含 conan/pro）→ pro 200 → 卸载降级 pull 200 / push 403。
- **AC7（回归）**：五基础包型 + go/nuget/cargo 零回归（remote 共享面）。

#### FR-97 debian 包型（internal/adapter/deb；automatic local P0 / virtual+remote P1 / trivial P2）

**用户故事**：作为系统组工程师，`curl -T pkg.deb` 推包后 `apt-get update && apt-get install` 直接可用；`/etc/apt/sources.list` 指向 BinFlow 与指向发行版仓库体验一致。

行为规格：

- **97.1 automatic local 主票（P0）**：debPUT 链（坐标属性登记 + `.deb/.dsc`，矩阵参数坐标示例直接为 AC 命令）→ 增量索引重算（Packages/Sources + 压缩集 + By-Hash）+ Release/InRelease 生成（unsigned：DB-1）；**索引直写拒绝**——`dists/**/{Release*,Packages*,Sources*,by-hash/**}` 客户端 PUT 一律 403（DB-3，文案含路径）；**缺坐标 .deb 400 拒绝**（DB-2）+ 豁免边界（remote 回源写入 / replication / move-copy 系统内路径豁免）；`debianDefaultArchitectures` 默认关闭（TL-4——Q8 待终裁）。
- **97.2 virtual stanza 聚合（P1）**+ **remote 归一化 pull-through（P1）**：apt 经虚仓/代理仓全链。
- **97.3 trivial 布局（P2 增量票，TL-7）**：共享索引生成内核，默认路径先行。
- **97.4 GPG（Q6）**：K-1 keypair 条件前置票；若进 M11，Debian 签名小票接上（Release/InRelease 签名 + apt 无 `[trusted=yes]` 验证）。
- **97.5 管理面**：deb reindex 端点（dispatchAPI 族）。

验收标准（AC）：

- **AC1（debPUT + apt 全链）**：curl PUT .deb（含 `;deb.distribution=...;deb.component=...;deb.architecture=...` 矩阵参数坐标）→ Packages/Release/by-hash 断言 → 容器内 `apt-get update && apt-get install` 全绿（`[trusted=yes]` unsigned）。
- **AC2（拒绝面）**：索引路径直写 403（文案含路径与「索引由 debPUT 链生成」）；缺坐标 400；remote 回源豁免断言（上游无坐标包可缓存）。
- **AC3（By-Hash）**：索引更新后旧摘要 by-hash 路径在场（debian.md L-d 序列断言直接引用）。
- **AC4（virtual/remote）**：apt 经虚仓本地优先 + remote 兜底；代理仓公共 deb 二次命中缓存。
- **AC5（门控全链）**：同 FR-96-AC6 模式。
- **AC6（回归）**：属性系统（坐标属性依赖 FR-89 node_props）+ M10 属性序列零回归。
- **AC7（条件：K-1 签名腿）**：若 Q6 进 M11——签名后 `apt-get update` 无 trusted 豁免全绿；未进则 BOARD 留痕非缺口。

#### FR-98 rpm 包型（internal/adapter/rpm；local P0 / remote+virtual P1 / modules P2）

**用户故事**：作为运维，`curl -T pkg.rpm` 推 RPM 后 `dnf install` 直接可用；repodata 由 BinFlow 自动维护，无需 createrepo。

行为规格：

- **98.1 local 管线（P0）**：RPM header 解析器（自研件——规格 §2.4 tag 集：PROVIDE/REQUIRE 六组五元组，票面单列子任务）→ 上传重算链 → repodata（primary/filelists/other；压缩与算法形态按规格；`_tmp_` 原子性；代数保留；.rpmcache）+ comps 组文件；校验算法默认 SHA-256（TL-5——Q8 待终裁）；unsigned 模式（RP-1：无密钥时删旧 `.asc`/`.key` 行为照规格 §4.3）。
- **98.2 reindex 端点族（rpm.md §3.2 七分支）**：400/403/404/409/202/200 状态码矩阵（table-driven 直接转化）+ `calculateYumMetadata` 默认值（RP-2 建议 true——Q8 待终裁；409 auto-async 冲突分支照搬——已验证 Artifactory 行为）。
- **98.3 remote 可过期集合（P1）** + **virtual 聚合（P1，RP-3 收紧）**：成员序拼接 + name+arch 优先成员去重 + 聚合 repomd 不签名；首版无 repomd-sha1 缓存键持久化（进程内 singleflight + 短 TTL 替代）；「≥2 成员有 repomd 才聚合」「同步超时转透传」保留。
- **98.4 modules.yaml 透传（P2，S7 中置信——面窄）**。
- **98.5 管理面**：yum reindex 端点（dispatchAPI 族）。

验收标准（AC）：

- **AC1（真实客户端 local）**：curl PUT .rpm → repodata 断言（repomd `checksum`/`open-checksum` 与实测 sha256 对账）→ 容器内 `dnf install`（`repo_gpgcheck=0`）全链绿 + 依赖解析正确（header 解析器实证）。
- **AC2（reindex 七分支）**：状态码矩阵 table-driven + 活体 curl 七分支逐条。
- **AC3（header 解析器）**：tag 集单测（规格 §2.4 清单）+ dnf `resolvedep` 断言。
- **AC4（virtual/remote）**：dnf 经虚仓聚合优先成员命中 + remote 可过期集合行为断言。
- **AC5（门控全链）**：同 FR-96-AC6 模式。
- **AC6（回归）**：五基础包型 + 新四包型互不干扰（契约变更面审计）。

#### FR-99 helm 包型（internal/adapter/helm；经典仓 local P0 / virtual+remote P1 / HelmOCI Q2 条件票）

**用户故事**：作为平台工程师，`helm repo add` 指向 BinFlow 后 push/install/upgrade 与公共 chart 仓库无差别；`--verify` 签名链可用（.prov 普通文件）。

行为规格：

- **99.1 经典仓 local（P0，helm.md）**：PUT 链（chart + `.prov`〔HL-4：普通文件存储，不走 keypair〕）→ index.yaml 生成（relative urls 默认 true——HL-2，Q8 待终裁；absolute 模式留配置位不暴露）+ enforce layout 两开关（403 断言）+ reindex 两端点。
- **99.2 挂载形态（HL-1）**：主面 = 内容面 `$BASE/binflow/<repoKey>/...`；`api/helm` 别名经 `apiProtocolMounts` 挂载（router.go 封闭清单「协议凭票赢得挂载」设计路径）、**只读**；R-2 修订合入（虚仓 URL 改写目标 = 主面，api/helm 别名不进 index URL）。
- **99.3 virtual 聚合 + URL 改写（P1，R-2 修订后算法）** + **remote 代理（P1：chartsBaseUrl/_external/_transitive）**。
- **99.4 HelmOCI（Q2 条件票，HL-3）**：`package_type=helmoci` 分发——新 handler `Register` 复用 docker adapter 端点实现（ForRepoType 机制现成）+ helm config/layer/prov 三 media type 入 manifest 映射；虚仓「Helm 与 HelmOCI 不混仓」校验放建仓校验。
- **99.5 门控**：helm（pro）；helmoci 同档（若实现）。

验收标准（AC）：

- **AC1（真实客户端 local）**：`helm repo add` → chart 上传（helm CLI 或 PUT 链）→ `helm install` 全绿；index.yaml 断言（entries/urls/relative 形态）；`--verify` + `.prov` 腿。
- **AC2（拒绝与别名）**：enforce layout 两开关 403 断言；reindex 两端点状态码；`api/helm` 别名 GET 200 / 写 405（只读）。
- **AC3（virtual）**：`helm repo update` 经虚仓聚合；下载 URL 为主面形态（R-2 修订后断言）。
- **AC4（remote）**：chartsBaseUrl 改写 + 公共 chart 代理二次命中缓存。
- **AC5（条件：HelmOCI）**：若 Q2 触发——`helm push oci://$BASE/<repo>/...` + `helm pull` 全绿 + docker 面零回归；未触发则 BOARD 留痕非 DoD 缺口。
- **AC6（门控全链）**：同 FR-96-AC6 模式。

#### FR-100 cargo remote + virtual（internal/adapter/cargo，P1，dep T-294）

**用户故事**：作为 Rust 开发者，crates.io 经 BinFlow 代理（remote），内部 crate 与公共 crate 在同一虚仓解析——cargo 家族三态齐装。

行为规格：

- **100.1 remote pull-through（cargo.md S4）**：sparse 索引代理 + `config.original.json` 翻译链（dl/api 重指 BinFlow，TL-1 base）+ `.crate` 缓存；search 代理（`q/per_page`）。
- **100.2 virtual（CG-1）**：按 (name, vers) 首见成员去重（官方 MUST 合规义务）+ 解析顺序本地优先；索引行恒一行。
- **100.3 门控**：槽位 cargo（pro，M10 已占位）——remote/virtual 建仓同受门控。

验收标准（AC）：

- **AC1（remote）**：`cargo build` 经 cargo-remote 拉公共 crate 全绿 + 二次命中缓存断言；`config.json` dl/api 指向正确、`config.original.json` 保留上游形态。
- **AC2（virtual）**：同名 crate 本地优先 + 索引去重断言（同 name+vers 唯一行）+ `cargo install` 经虚仓。
- **AC3（回归）**：T-294 local 序列（L-r1~L-r6）零回归。

### 4.6 复制硬化（M9 Q5 + M10 §2.2，P1）

#### FR-101 enableTokenAuthentication/contentSynchronisation 生效 + 属性复制同步 + replica 隔离终裁执行（internal/replication + config + repo）

**用户故事**：作为双中心运维，我启用 contentSynchronisation 后源实例的属性/统计随复制到达目标——治理数据不缺腿；replica 隔离语义有明确终局。

行为规格：

- **101.1 smart remote 复制字段生效**：`enableTokenAuthentication`、`contentSynchronisation`（属性同步 / 统计同步子字段集——语义锚 replication.md + artifactory.xsd，K37 校准）接受 + 回显 + **行为生效**；M10「按名 400」分支退役（PRD 断言反转回写）。
- **101.2 属性复制同步**：push 复制携带 node properties（消费 M10 属性系统）；目标侧 `?properties` 可读。
- **101.3 replica 隔离（M9 Q5 终裁执行）**：按用户终裁口径实现（暂行按 ADR-0025 决策 1 现状维持——终裁翻转时实现票承接）。
- **101.4 回归红线**：M6 复制序列 + M10 smart-remote 序列（socketTimeout 族）零回归。

验收标准（AC）：

- **AC1（字段生效）**：PUT remote 配置含两字段 → 200 回显一致（M10 L25 断言反转）；行为探针——源打标 `build=77` → 复制 → 目标 `?properties=build` 读回；统计同步形态按 K37 定案断言。
- **AC2（replica 隔离）**：双实例活体断言（终裁口径的每条行为）。
- **AC3（回归）**：M6 H 复制序列 + M10 L25 其余字段维持。

### 4.7 工程债打包（M10 债 + T-297 登记，P1/P2）

#### FR-102 S3 MPU kill -9 续传复活 + unused-cleanup 引擎 + D-8/D-9 + 文档尾巴（storage + remote + web 测试基建 + docs）

**用户故事**：作为大文件上传方，S3 后端浏览器直传中断后重启可续（M10 承诺复活）；作为 remote 仓运维，`unusedArtifactsCleanupPeriodHours` 真实生效清理缓存；作为团队，boot footprint 与测试基建恢复健康基线。

行为规格：

- **102.1 S3 MPU kill -9 续传（T-289 AC2 descope 债，§11.31 付债路径）**：upload ID 落 `upload_sessions` 表 + S3 ResumeSession 经 ListParts 重建——M10 FR-90-AC2 随之复活（status 保留 → 续传 complete）。
- **102.2 unused-cleanup 引擎（T-290 勘误③收窄项）**：remote 缓存清理 cron 最小实现——`unusedArtifactsCleanupPeriodHours` 字段（M10 已落库）驱动；过期未用缓存制品清理、在用/最近使用保活；可观测（日志 + 指标）。
- **102.3 D-8 boot footprint**：空载 RSS 138MB → **≤100MB 恢复**（PRODUCT.md 基线；不达须差异归因 + 改善 ≥20% 并 BOARD 留痕）。
- **102.4 D-9 测试基建**：e2e seed 竞态修复 + verifyM10 硬编码口令退役（改密实例不再必 401）。
- **102.5 文档尾巴**：goproxy.md GOPRIVATE 勘误（reverse-engineer 触点）、cargo.md §9 四档路径示例复核（随 R-1）、D-3 NuGet 文档一行（tech-writer）。

验收标准（AC）：

- **AC1（S3 续传）**：MinIO 栈 MPU create → 2 片 PUT → kill -9 → 重启 → status 保留 → 续传 complete → sha256 对账（M10 L24 断言翻转，探针注释复原）。
- **AC2（cleanup 引擎）**：设短周期 → 过期未用缓存制品被清 + 在用保活 + 审计/日志断言；blob 零孤儿（对账）。
- **AC3（footprint）**：空载 RSS ≤100MB + 冷启动 <2s 维持。
- **AC4（测试基建）**：D-9 修复后默认并发全量 e2e 三连绿；verifyM10 口令外置（env/夹具）。
- **AC5（文档尾巴）**：三处收口（grep 断言）。

### 4.8 候选池 D 组处置（不设 FR）

Trash can：**余量条件票**（Q7）——全部 P0/P1 收官且余量足时单票执行（语义独立、实现面窄：删除入回收站 + 保留期 + 恢复/清空 + REST/控制台最小面，行为基准 docs/reverse 主矩阵缺口 3 + 逆向小规格随票）；未触发不构成 DoD 缺口，BOARD 留痕。Cleanup-Retention 策略引擎 / 制品操作族 / Webhook / AQL：**M12+ 建议**（价值/依赖排序见 §7 Q 备注与 ROADMAP）。

---

## 5. 兼容性矩阵（M11——包型端点对齐分级 + 配置域行为模式 + 制度化 D 项留痕）

### 5.1 层级定义（沿用 M2~M10 分级口径）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 端点路径/方法/语义对齐 Artifactory 或公开规范（高频子集承诺范围）；基座前缀差异（`/binflow` vs `/artifactory`）沿 E-26 口径；加宽回显 additive |
| **C 自有（/api/v1 或内部语义）** | 无 Artifactory 对应或 BinFlow 自有设计；行为模式对齐但载体自定 |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / PM 裁定 / 19:05 行为对齐指令应用），矩阵留痕防再议 |
| **待裁** | 存在与 Artifactory 的可观测差异，按 19:05 口径须用户终裁（Q8）——终裁后归 A/D 并回写 |

### 5.2 档位 × addon 解锁矩阵（M11 扩展——新增 5 槽，其余沿 M10 §5.2）

| addon 槽位（新增行） | kind | community | pro | enterprise | M11 状态 |
|---|---|---|---|---|---|
| conan | package-eco | locked | unlocked | unlocked | M11 交付（FR-96） |
| debian | package-eco | locked | unlocked | unlocked | M11 交付（FR-97） |
| rpm | package-eco | locked | unlocked | unlocked | M11 交付（FR-98） |
| helm | package-eco | locked | unlocked | unlocked | M11 交付（FR-99 经典仓） |
| helmoci | package-eco | locked | unlocked | unlocked | Q2 条件票（HL-3，未触发则槽位占位） |

- 三态叠加规则、`addons.disabled` 熔断、license 覆盖表语义全部沿 M10 §5.2 不变；新槽位自动获得门控三缝行为（FR-85 机制复用，零新织入点）。
- **与 Artifactory 分级的有意差异（留痕）**：五基础包型 + properties 归 community 维持（M10 留痕）；新四包型归 pro 与 Artifactory 同构（无差异）。

### 5.3 契约矩阵（18 条，LC-13~LC-30 接续 M10 编号）

| # | 端点/契约面 | Artifactory 对应 / 公开规范 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| LC-13 | 认证配置管理面 REST（三协议配置段读/写 + 启停） | Artifactory `GET/POST /api/system/configuration`（ldapSettings/oauthSettings/samlSettings 段）+ Access 配置面——路径与信封以复核票锚点为准（ADR-0035） | A | P0 | 复核后 | L02/L03 |
| LC-14 | 认证配置持久化载体（DB 配置描述符为中心；双源优先级 Q4） | Artifactory config descriptor 行为模式（UI/REST 写入权威） | C | P0 | 高（行为） | L02/L10 |
| LC-15 | SAML 配置面字段集（运行时深度 Q3） | Artifactory samlSettings（auth-integration.md §3 低置信区——复核票补齐） | A | P0 | 复核后 | L02/L05 |
| LC-16 | 存储配置独立文件（filestore/S3/dual-write 链式 provider 表达 + fail-fast） | binarystore.xml 行为模式（config-formats.md §1；载体自有 YAML） | C | P0 | 高（行为） | L08~L10 |
| LC-17 | conan v2 内容面（17 端点 + index.json 修订索引 + `.timestamp`） | conan v2 API（GitLab 官方文档锚点优先 + conan.md） | A | P0 | 高 | L18 |
| LC-18 | conan v1 握手三端点（files 通道不做——CN-1 收窄） | conan 1.x API（EOL，收窄子集） | A | P0 | 高 | L19 |
| LC-19 | conan 能力头族（Version/Capabilities 按 rclass） | Artifactory 实测值（TL-2 定案） | A | P0 | 中→定案 | L20 |
| LC-20 | 五协议管理面 reindex 族（`$BASE/binflow/api/<proto>/...`，dispatchAPI） | Artifactory `/api/conan|deb|yum|helm/...` 同构（DB-4 统一；**ADR-0034 已承载**） | A | P0/P1 | 高 | L28/L32 |
| LC-21 | debian apt 内容面（Packages/Sources/Release/InRelease/压缩集/By-Hash） | Debian repository 规范（Debian wiki 锚点优先 + debian.md） | A | P0 | 高 | L23 |
| LC-22 | debian 索引直写 403 + 缺坐标 400（+ 系统内路径豁免） | debian.md（DB-2/DB-3 定案；Artifactory 行为出处随规格在档） | A | P0 | 高 | L24 |
| LC-23 | rpm repomd 内容面（repodata 三索引/comps/代数保留/_tmp_ 原子性） | repomd 社区规范 + dnf.conf(5) + rpm.md | A | P0 | 高 | L27 |
| LC-24 | rpm 默认值族：calculateYumMetadata（true vs Artifactory false）/ 校验算法（SHA-256 vs Artifactory SHA-1） | Artifactory 默认 vs tl-fr91-ac3 RP-2/TL-5——**Q8 用户终裁** | **待裁** | P0 | 高 | L15/L27 |
| LC-25 | helm 经典仓内容面 + index.yaml（relative urls 默认 true——HL-2）+ api/helm 只读别名 | helm.sh 官方两页 + Artifactory 同构挂载（HL-1；R-2 修订） | A | P0/P1 | 高 | L31/L32 |
| LC-26 | HelmOCI 分发（package_type=helmoci 复用 docker 面 + 三 media type） | Artifactory HelmOCI（HL-3 单列）——**Q2** | A | 条件 | 高 | L34 |
| LC-27 | cargo remote（config.original.json 翻译 + search 代理）/ virtual（(name,vers) 去重） | crates.io sparse index 官方规范 + cargo.md（CG-1 去重为官方 MUST） | A | P1 | 高 | L36/L37 |
| LC-28 | 复制硬化字段（enableTokenAuthentication/contentSynchronisation 属性+统计同步）+ replica 隔离 | artifactory.xsd（inv-4 F5）+ replication.md；M10 按名 400 退役反转 | A | P1 | 高 / 隔离待终裁 | L38/L39 |
| LC-29 | unusedArtifactsCleanupPeriodHours 清理引擎 | artifactory.xsd 字段行为（M10 仅落库 → M11 生效） | A | P1 | 高 | L41 |
| LC-30 | `storage.matrix_params` 逃生开关 | **D（有意不兼容）**——Artifactory 无此开关（D-6 先例，19:05 口径首次应用；config 键不存在） | D | — | 高 | L17（负向） |

> 计数：**18 条 = A 14（LC-13/15/17/18/19/20/21/22/23/25/26/27/28/29）+ C 2（LC-14/16）+ 待裁 1（LC-24）+ D 1（LC-30）**。待裁项终裁后归位并回写本表。既有契约面（五基础包型、go/nuget/cargo、license 族）M11 对五基础包型与 M10 as-built **零行为变化**（§5.4），新包型全部经门控三缝。

### 5.4 回归基线（M11 不反转既有断言——无 license 默认实例 + pro 实例双形态）

| 既有断言 | M11 期望 |
|---|---|
| M1~M10 全部 P0 序列（C/D/H/M/W/V/U/N/L） | 零回归（FR-92/93 只改配置承载与来源，不改协议/门/授权行为） |
| M6 认证 H 序列（Keycloak/OpenLDAP 容器） | 零回归（配置面新增不改五臂语义；文件段 → DB 双源过渡按 Q4 断言） |
| M6 S3/迁移序列 + M10 MPU/smart-remote | 零回归（FR-93 只换承载文件；M10 L24 kill -9 断言随 FR-102-AC1 **翻转复活**） |
| remote 配置「contentSynchronisation/enableTokenAuthentication 按名 400」（M10 L25） | **反转**：FR-101 落地后 200 接受 + 回显 + 生效（PRD 回写） |
| `calculateYumMetadata` 等默认值 | 新面（M11 首次引入）——按 Q8 终裁落断言 |
| packageType 枚举闭集 | 加宽 conan/debian/rpm/helm（〔helmoci 条件〕additive；未知型 400 维持；门控 403 先于枚举 400——沿 ADR-0032 顺序） |
| console-ux 册锚 / anchor ledger | FE 迁移零改动（FR-94 熔断线）；豁免票 100% 归属审计 |
| 建仓/内容面错误体 errors[] 信封 | 新包型错误面沿用信封（cargo 404 体等按各规格逐字） |

### 5.5 M11 核心验收命令（L 序列骨架，QA 直接引用）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-92 认证配置 ==========
# L01 复核票走查：auth-integration.md 复核版（三协议字段表逐条附 Artifactory 出处 + SAML 边界）+ tech-lead 就绪确认
# L02 REST 往返：GET 认证配置 → 三段在位；PUT 改 LDAP url/port → 200 回显一致；敏感字段明文 grep=0
# L03 变更即生效：PUT 新目录配置后（无重启）mock 目录 A 失败/B 成功双断言；Keycloak 真容器改 issuer 同构
# L04 测试连接：可达 → 成功形态；不可达/凭据错 → 失败形态（无凭据泄露）
# L05 控制台：Playwright admin 三协议 Tab 往返 + readonly 只读 + 敏感框 write-only
# L06 认证回归：M6 H24/H27/H30/H33/H36 抽样 + M7 矩阵零回归；auth.config.update 审计 redact 断言

# ========== FR-93 存储配置 ==========
# L07 复核票走查：config-formats.md §1 复核版（provider 链/模板/位置语义出处齐）
# L08 三形态：binstore.yaml 表达 filestore/S3/dual-write → 起服 → roundtrip sha256 对账（MinIO 栈）
# L09 链式：dual-write 上传双写 + S3 故障降级（M6 H 序列口径）
# L10 兼容与 fail-fast：存量 binflow.yaml 内嵌段照常起 + WARN；并存处置断言（Q5 裁定形）；坏文件/明文 secret 拒启
# L11 CD 链：VM 172.16.58.129 经 Jenkins deploy 切换配置 → 探针 + 烟测 + 回归冒烟（数据目录零触碰）

# ========== FR-94 MUI 两批 ==========
# L12 批次一（T-299）：登录/壳层/仓库域四闸门（锚零改动/ledger PASS/全量 playwright/assert-tokens）+ axe 双主题 0 + gzip 增量 ≤25%
# L13 批次二（T-300）：树/搜索/安全治理/admin 余面同四闸门；旧组件栈残留页清零
# L14 服务端契约 git diff=0 + M8 剧本抽样 4/8 复跑

# ========== FR-95 回头看 ==========
# L15 清单裁决：基线 23 项 + tl 差异点（RP-2/TL-5/TL-4/HL-2/CN-1/CG-2 等）100% 出结论（维持附出处/改回/上 BOARD）——Q8 剧本
# L16 改回项验证：逐项 curl/真实客户端断言 Artifactory 形态 + 回归绿 + 文档同步登记
# L17 维持项出处 grep 审计零「无出处维持」；D-6 负向：storage.matrix_params config 键不存在（LC-30）

# ========== FR-96 conan ==========
# L18 local v2：conan 2.x remote add → upload（revisions 多版本）→ install → list；index.json/修订链 curl 断言
# L19 v1 握手：curl ping/users/authenticate/users/check_credentials 形态断言（活体或 curl 等价 + 留痕）
# L20 能力头：三 rclass X-Conan-Server-Version/Capabilities 逐字断言（TL-2）
# L21 remote + virtual：conan install 公共包经 remote + 二次缓存；虚仓本地优先/未命中走 remote/time 归并
# L22 门控全链：community 建仓 403 → pro 200 → 卸载 pull 200/push 403

# ========== FR-97 debian ==========
# L23 debPUT + apt：curl -T pkg.deb ";deb.distribution=stable;deb.component=main;deb.architecture=amd64"
#   → Packages/Release/by-hash 断言 → 容器 apt-get update && apt-get install 全绿（[trusted=yes]）
# L24 拒绝面：dists/**/Release* 直写 403（文案含路径）；缺坐标 400；remote 回源豁免
# L25 virtual + remote：apt 经虚仓本地优先 + 代理仓公共 deb 二次缓存
# L26 门控全链（同 L22 模式）；K-1 签名腿条件执行（Q6）

# ========== FR-98 rpm ==========
# L27 local：curl -T pkg.rpm → repodata（primary/filelists/other）+ repomd checksum/open-checksum sha256 对账
#   → 容器 dnf install（repo_gpgcheck=0）+ 依赖解析断言
# L28 reindex 七分支：400/403/404/409/202/200 逐条活体（rpm.md §3.2 矩阵）；calculateYumMetadata 默认值断言（Q8 终裁形）
# L29 virtual 聚合（RP-3：优先成员去重/singleflight）+ remote 可过期集合；comps 断言
# L30 门控全链；modules.yaml 透传 P2 条件腿

# ========== FR-99 helm ==========
# L31 经典仓：helm repo add → chart 上传（含 .prov）→ helm install --verify；index.yaml relative urls 断言
# L32 拒绝与别名：enforce layout 两开关 403；reindex 两端点；api/helm 别名 GET 200/写 405
# L33 virtual + remote：虚仓聚合 + 下载 URL 主面形态（R-2 修订）；chartsBaseUrl 改写 + 公共 chart 二次缓存
# L34 HelmOCI 条件腿（Q2）：helm push oci://$BASE/<repo>/... + helm pull + docker 面零回归；未触发则留痕
# L35 门控全链

# ========== FR-100 cargo ==========
# L36 remote：cargo build 经 cargo-remote + 二次缓存；config.json dl/api 指向 + config.original.json 上游形态
# L37 virtual：同 name+vers 索引唯一行断言（CG-1）+ 本地优先 + cargo install 经虚仓

# ========== FR-101 复制硬化 ==========
# L38 字段生效：remote 配置含 enableTokenAuthentication/contentSynchronisation → 200 回显（M10 L25 反转）
#   + 源打标 build=77 → 复制 → 目标 ?properties=build 读回；统计同步断言（K37 形态）
# L39 replica 隔离：双实例活体（Q5 终裁口径逐条）

# ========== FR-102 工程债 ==========
# L40 S3 续传复活（MinIO）：MPU create → 2 片 → kill -9 → 重启 → status 保留 → 续传 complete → sha256 对账
# L41 cleanup 引擎：短周期 → 过期未用缓存被清 + 在用保活 + 日志/审计断言 + blob 零孤儿
# L42 D-8/D-9：空载 RSS ≤100MB + 冷启动 <2s；默认并发全量 e2e 三连绿 + verifyM10 口令外置

# ========== 回归与 NFR（收口跑）==========
# L43 M1~M10 全 P0 复跑（无 license + pro 双实例）+ 服务端契约变更面 100% 归属 M11 豁免票
# L44 NFR：认证配置变更即生效 ≤1s / 100 并发四包型真实客户端零 5xx / 冷启动 <2s / RSS ≤100MB / SPA gzip 增量 ≤25%
# L45 Trash can 条件腿（Q7 触发则执行其票面 AC）；未触发 BOARD 留痕
```

### 5.6 待校准项（ADR-0035/0036 与复核票落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K30 | 认证配置 REST 路径/信封/字段拼写与三协议字段集全集 | 行为基准复核票（Artifactory 段形） | 复核票 + ADR-0035 |
| K31 | 认证配置双源优先级（DB vs binflow.yaml auth 段）与文件段处置 | DB 权威、文件段首启种子、覆盖 WARN（Q4） | Q4 → ADR-0035 |
| K32 | 测试连接端点形态与错误文案边界（SSRF 面） | 复用 M3 Guard + ADR-0025 决策 4 | 复核票 + ADR-0035 |
| K33 | 存储配置文件名/位置/链式 schema/并存与冲突处置 | `binstore.yaml` 同目录；独立文件优先 + 内嵌 WARN + 冲突 fail-fast（Q5） | Q5 → ADR-0036 |
| K34 | SAML 配置面字段集与运行时归属边界 | 配置面先行、运行时不含 DoD（Q3） | Q3 + 复核票 |
| K35 | 包型默认值族终裁（RP-2/TL-5/TL-4/HL-2/CN-1/CG-2） | tl-fr91-ac3 结论暂行开工 | Q8 → BOARD 用户终裁 |
| K36 | GPG keypair 体系形态（CRUD/存储/口令/repoKey 关联——debian/rpm 共用） | K-1 条件票进 M11（Q6） | Q6 → 新 ADR |
| K37 | contentSynchronisation 子字段集与统计同步语义 | replication.md + artifactory.xsd 锚定 | 复核 + ADR-0037（如需） |

### 5.6.1 回头看回写表（FR-95 裁决后填——v1.0 占位）

| 基线项 | 结论（维持/改回/上 BOARD） | 出处或改回动作 | 回写落点 |
|---|---|---|---|
| T-287 L1~L7（NuGet ×7） | 待裁决（M10 已按 T-293 复核维持——新口径重审） | — | 本表 + 各域票 |
| T-289 ×5 / T-290 ×4 / T-294 ×7 | 待裁决 | — | 同上 |
| tl 裁决差异点（RP-2/TL-5/TL-4/HL-2/CN-1/CG-2 等） | 待裁决（Q8） | — | 本表 + LC-24 归位 |
| D-6 matrix_params 无开关 | **维持**（先例已裁） | Artifactory 无此开关（19:05 口径首次应用） | LC-30 |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| PRODUCT.md「明确不做」 | ①「不做 LDAP/SAML/OIDC」已被 M6（OIDC/LDAP）与 M11 指令（SAML 配置面）先后解禁；② HA/Xray 本体是否随「行为对齐」指令进 M11 | ① 建议用户随 M11 修订该行（PM 不代改）；② 指令不自动解锁本体（Q1 终裁，暂行不进） | 用户决策（Q1） |
| ADR-0001（clean-room） | 19:05 指令「照搬代码只是 Java 转 Go」与版权红线的张力 | 红线保留（用户知情确认）：行为逐项对齐 ≠ 逐行翻译；公开规范覆盖处以规范为准 | 本 PRD §1.4 条款 4 留痕 |
| ADR-0032（license） | 公钥 config 覆盖未实现（T-293 终裁③） | 不入 M11（M12+ 走新 ADR）——§2.2 留痕 | 无 |
| ADR-0034（五协议管理面） | tl 报告 K-3（DB-4/TL-1 ADR 补记） | 已承载收口——M11 直接消费，无新增动作 | 无 |
| M10 PRD §5.4/L25 断言 | contentSynchronisation/enableTokenAuthentication 按名 400 | M11 反转为 200 + 生效（FR-101）——PRD 回写义务 | PM 回写（as-built 惯例随终验） |
| M10 FR-90-AC2（S3 续传 descope） | kill -9 后 status 404 探针断言 | M11 复活翻转（FR-102-AC1），探针注释复原 | QA 序列更新 |
| ADR-0025 决策 1（replica 暂行）+ M9 Q5 | replica 隔离归属 | M11 收编终裁执行（FR-101.3） | Q5 用户终裁 → 实现 |
| ADR-0029 决策 4（FE 契约熔断线） | MUI 迁移不得私改契约/交互 | 四闸门为验收硬门（FR-94.4） | 无 |

### 6.2 性能（M11 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P48 配置变更即生效 | 认证配置 REST 返回后 ≤1s 下一次认证走新配置（L03 实测）；存储配置重启生效（对齐 Artifactory binarystore 行为）+ fail-fast | P0 |
| NFR-P49 新包型协议面 | 100 并发真实客户端（conan install / apt-get / dnf install / helm install）零 5xx；remote 缓存命中 P95 对齐既有 remote 口径 | P0 |
| NFR-P50 资源基线 | 冷启动 <2s 维持；**空载 RSS ≤100MB 恢复**（D-8 债，PRODUCT.md 基线） | P0 |
| NFR-P51 MUI 迁移体积 | 两批完成后 SPA gzip 相对 T-291 基线增量 ≤25%；axe 双主题 serious=0 维持 | P1 |
| NFR-P52 S3 续传零拷贝窗口 | kill -9 后已传分片幸存（对齐 M7/M9 续传口径：部分数据不丢、续传后逐位校验） | P1 |

### 6.3 安全底线（M11 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S57 认证配置敏感面 | client secret/私钥等：回显脱敏（明文 grep=0）、审计 redact、普通日志零明文；binflow.yaml/DB 存储侧沿用既有加密纪律 | L02/L06 |
| NFR-S58 存储配置凭据 | 独立文件不接受明文 secret 键（拒启）；env 引用 + AES-GCM 链零变化；文件权限建议 0600（部署文档明示） | L10 |
| NFR-S59 门控降级不劫持 | 新四包型（含 helmoci）照 FR-85 语义：降级/熔断零删除、读恒 200（写面 403） | L22/L26/L30/L35 |
| NFR-S60 测试连接与包型 remote SSRF 面 | 复用 M3 Guard（DNS rebinding pinning 等五参数）+ ADR-0025 决策 4 私网开关——零新 SSRF 面 | L04 + 各 remote 腿 |

### 6.4 可观测性（M11 增量）

- 新审计事件：`auth.config.update`（actor/协议段/摘要 redact）；`storage.config.load`（失败腿 WARN + 审计）；`addon.gate.deny` 随新槽位自动扩面（零新词）。
- /metrics：新包型流量指标族沿用既有 family 口径；`binflow_addon_gate_requests_total{addon,decision}` 新槽位标签自动生效；cleanup 引擎计数器（清理对象数/字节数）。
- 启动日志：存储链形态一行（provider 链摘要，凭据 redact）；认证配置来源（DB/文件种子）一行。
- 既有指标族与口径冻结维持。

---

## 7. 开放问题（Q1~Q8，均带暂行；需用户/conductor 决策，PM 不代拍）

> **2026-08-26 20:35 用户裁定（BOARD 留痕）**：Q1 = 不进 M11（M12+ 单列）；Q2 = 单列条件票；Q6 = 进（条件票）；Q8 = 全部照 Artifactory（唯一例外 TL-5 rpm 校验算法留 SHA-256，安全向留痕）。Q3/Q4/Q5/Q7 维持暂行。

| # | 问题 | 影响面 | 暂行口径（v1.0） |
|---|---|---|---|
| Q1 | **HA 本体（及 Xray 集成面本体）是否进 M11**：「行为逐项对齐」指令是否隐含功能本体解锁 | 范围体量（HA 是主矩阵缺口 9 前的大项）；PRODUCT.md 修订 | **不进**——指令对齐的是「行为」，本体解锁须用户修订 PRODUCT.md；M11 已满载，进则须等量置换（建议 M12+ 单列里程碑） |
| Q2 | **HelmOCI 单列 or 缓**：HL-3 已裁单列票（复用 docker 面，成本低）但 M11 满载 | FR-99 范围；docker 面回归面积 | 单列条件票（P1，非 DoD 硬门）——四包型 P0/P1 收官且余量足则执行；未触发 BOARD 留痕非缺口 |
| Q3 | **SAML 深度**：仅配置面（字段集照 Artifactory）还是含 SP 运行时（断言消费登录流） | FR-92 范围与面积；auth-integration.md §3 低置信 | 配置面先行（DoD 含）；运行时按复核票结论单列票或 M12——若复核证实 Artifactory SAML 行为可低成本对齐且用户要求，进 M11 须置换 |
| Q4 | **认证配置双源优先级**：DB 配置面 vs binflow.yaml auth 段（M6 既有部署形态） | 存量实例升级体验；运维心智 | DB 配置面权威（Artifactory 配置描述符行为模式）；文件段作首启种子，运行期被覆盖 WARN——终裁归 ADR-0035，若判「文件段永久权威/只读面板」须用户拍板 |
| Q5 | **存储配置兼容窗**：binflow.yaml 内嵌 storage 段与独立文件并存多久、冲突形态 | 存量实例；部署矩阵复杂度 | 独立文件优先 + 内嵌段 WARN（M11 全程）；M12 评估移除；同写冲突 fail-fast——终裁归 ADR-0036 |
| Q6 | **GPG keypair（K-1）是否进 M11**：debian/rpm 签名的前置条件票 | FR-97/98 签名腿；K-1 面积（CRUD/存储/口令） | **进**（P1 条件票 + 两张签名小票）；不进则维持 unsigned 验收（`[trusted=yes]`/`repo_gpgcheck=0`）——DoD 不含签名腿 |
| Q7 | **Trash can 余量票触发条件** | D 组唯一 M11 候选 | 全部 P0/P1 收官且余量足 → 单票（语义独立、实现面窄，规格随票）；否则 M12 与 Cleanup-Retention 同域立项 |
| Q8 | **包型默认值族终裁**（RP-2 calculateYumMetadata=true / TL-5 SHA-256 / TL-4 关闭 / HL-2 relative=true / CN-1 v1 收窄 / CG-2 失败形态）——均与 Artifactory 默认有可观测差异 | 四包型行为面；Artifactory 迁移用户预期 | 以 tl-fr91-ac3 结论暂行开工（拆票即用）；按 19:05 口径逐条上 BOARD 用户终裁（LC-24 归位）——**默认全部改回 Artifactory 形态是候选之一**，涉及安全向默认（SHA-256）建议保留并留痕理由 |

---

## 8. M11 验收剧本（QA 总纲）

1. **回归基线（硬门槛先行）**：M1~M10 全部 P0 序列在无 license 默认实例 + pro 实例双形态复跑全绿；服务端契约变更面（`git diff m10-done..HEAD -- internal/ cmd/`）100% 归属 M11 豁免票。
2. **认证配置域**：L01（复核票走查）→ L02（REST 往返 + 脱敏）→ L03（变更即生效双臂）→ L04（测试连接）→ L05（控制台）→ L06（认证回归 + 审计）。
3. **存储配置域**：L07 → L08（三形态 roundtrip）→ L09（链式语义）→ L10（兼容/fail-fast）→ L11（CD 链 VM 实腿）。
4. **MUI 与回头看**：L12/L13（两批四闸门）→ L14（契约 diff=0）→ L15~L17（清单裁决完备 + 改回验证 + 出处审计）。
5. **conan**：L18~L21（local v2 真实客户端 → v1 握手 → 能力头 → remote/virtual）+ L22 门控。
6. **debian**：L23~L25（debPUT+apt 全链 → 拒绝面 → virtual/remote）+ L26 门控（+K-1 条件腿）。
7. **rpm**：L27~L29（local 管线+dnf → reindex 七分支 → virtual/remote）+ L30 门控（+modules 条件腿）。
8. **helm**：L31~L33（经典仓全链 → 拒绝/别名 → virtual/remote）+ L34 HelmOCI 条件腿 + L35 门控。
9. **cargo 补齐**：L36/L37（remote 缓存/翻译链 + virtual 去重）。
10. **复制硬化与工程债**：L38/L39（字段生效 + 属性到达 + replica 隔离）→ L40（S3 续传复活）→ L41（cleanup 引擎）→ L42（D-8/D-9）。
11. **NFR 与收口**：L43（全量回归 + 归属审计）→ L44（性能/资源/体积）→ L45（Trash can 条件腿留痕）。
12. **文档**：tech-writer 交付——认证配置指南（三协议 + 测试连接 + 变更即生效 + 双源说明）、存储配置文件指南（三链示例 + 兼容窗 + CD 链）、四包型接入指南（conan/apt/dnf/helm + cargo remote/virtual）、api-reference 增量（新端点 + L25 反转 + LC-24 归位）、FAQ 增补（unsigned 模式、门控降级行为）。

---

## 9. M11 DoD

1. §4 全部 P0 AC（FR-92/93/94 批一/95/96~99 local）经 qa 验证全绿；P1（FR-94 批二/96~99 remote+virtual/100/101/102 主体）全绿；条件票（HelmOCI Q2 / Trash can Q7 / K-1 签名腿 Q6 / trivial Q7 域）按余量条款——未触发不构成 DoD 缺口，须 BOARD 留痕；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（变更即生效时延 / 存储三链 / MUI 四闸门 / 回头看裁决率 / 四包型真实客户端链 / 复制硬化 / 债务收口——全部有实测数字归档）；
3. 回归硬门槛：M1~M10 全部 P0 序列双形态复跑全绿；五基础包型与 M10 as-built 零行为变化；服务端契约变更面 100% 归属 M11 豁免票；两处断言反转（L25 按名 400 → 200、L24 kill -9 → 续传）经 PRD 回写；
4. 前置产物齐备：ADR-0035/0036 Accepted（含 K30~K33 校准）；两份规格复核版交付（逐条附 Artifactory 行为出处）+ R-1/R-2 合入；FR-95 裁决表（§5.6.1）100% 填实、Q8 涉及项终裁归位；
5. tech-writer 五类文档交付（§8-12）；四包型 + 配置域接入指南的客户端命令全部实测可复跑；
6. NFR-P48~P52 达标归档；`make test`（race）/`make lint` 0 issues / gofmt 空维持；默认并发全量 e2e 绿（D-9 修复后三连绿）；
7. §2.2 Non-goals 与 §4.8 候选池处置对账完成：滚入 M12+ 项在 ROADMAP「M11 未纳入项」登记无遗漏；19:05 行为对齐条款在全部 M11 票的规格出处义务可审计；
8. 主会话 git tag `m11-done`（对外发布任何制品先经用户确认；license 根密钥事务沿 M10 交付形态）。
