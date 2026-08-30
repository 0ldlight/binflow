# PRD — M13 Artifactory 对齐第四程：Webhook 统一事件总线 + HelmOCI 三态齐装 + 配置旋钮与文面债收口

> **PRD 状态：v1.1（2026-08-30，待 conductor 审；v1.1 = v1.0 + T-370 文面修正——T-358 webhook.md 规格活化后的校准：重试语义对齐官方基准〔固定间隔 10s/4xx 不重试〕、事件数 36→13 域 66 型、envelope 字段修正、SSRF 键落定、Q4 取证结论、K47~K50 回填；详见 §0 修订记录）**。主轴选题（PM，§2.2 留痕）：**Webhook 统一事件总线**——主矩阵缺口 6（36 事件〔inv-4 I1 内部总线枚举口径；官方可订阅面实为 66 型/13 域——webhook.md §0 校准〕CloudEvents envelope + HTTP outbound + 订阅管理），inv-4 判定「可整体平移、无需模拟 Access 拆分」；**取证路径特例**：反编译集合无 webhook addon（full-feature-matrix §待验证 L365）→ **JFrog 官方 REST 文档为唯一行为基准**，inv-4 §I/§K 反编译锚点仅补空白。范围基线：ROADMAP「M12 未纳入项」（票级遗留聚类 / P2 登记维持 / 运维尾巴）。**HA 本体维持 Q1 终裁「单列专程」——前置 = PRODUCT.md「明确不做」修订解禁（用户动作，截稿未发生），本 PRD 不排任何 HA 工作项**。19:05 行为逐项对齐条款为常设工作准绳（M11 §1.4 制度延续）。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-13.md` |
| 里程碑 | M13 — Artifactory 对齐第四程（Webhook 订阅与投递 + 事件源织入 + HelmOCI remote/virtual 翻转 + chartsBaseUrl/_external + 旋钮两枚 + conan 翻转与迁移 + 文面/测试/运维债收口） |
| 状态 | v1.0 草案（FR-114~FR-122 九条需求；契约矩阵 11 条〔A 10 / C 0 / D 0 / 待裁 1〕+ 档位矩阵增量 1 行〔webhook 第 19 槽 Q4〕；L01~L24 验收命令骨架；开放问题 Q1~Q7 带暂行） |
| 上游依据 | PRODUCT.md（Non-goals 不越界：HA/Xray 本体不做——Q1 前置未解禁；高频子集承诺维持）、BOARD.md（2026-08-30 m12-done 收官：Sprint 942 笔头批 `346485e`——五处文档漂移清 + M12 PRD 两处文面修正 + ROADMAP「M12 未纳入项」段建立）、ROADMAP「M12 未纳入项」（范围基线）+「M11 未纳入项」M12+ 主轴候选行、docs/reverse/artifactory-full-feature-matrix.md（§十大缺口 2/6/9 + §五企业集成 Webhook 行 + §待验证「Webhook addon 反编译集合缺失」+ L186 官方文档基准声明）、docs/reverse/inv-4-addons.md（§I 统一事件 I2/I3/I4/I5 + §K3 outbox 模式）、docs/reverse/helm.md（chartsBaseUrl 回源链 §5/S8/S10、`_external`/`_transitive` 端点表 §2/§3、D-5）、docs/reverse/repo-operations.md（§2.1 folderDownloadConfig 六字段双证）、docs/reverse/nuget.md（§5.1 publish 臂②——D-10 分歧）、docs/reverse/conan.md（§3.2 D8 整树删 + §4 files 布局）、reports/agents/T-356.md（终验 ⚠️ 4 维持登记〔L03 D-10 / L12 flat / L14 folderDownload / L17 retention〕+ 观察③⑨ + DoD#1 条件票留痕）、reports/agents/T-348.md（D8 双证新取证 + §4 建议）、reports/agents/T-340.md（§4 D-F2 登记）、reports/agents/T-313.md（D-2/D-3/D-5）、reports/agents/T-342.md（D-3 评估结论：建议 M13）、reports/iteration-942.md（m12-done 程序与总账）、docs/prd/milestone-12.md（体例 + §2.2 滚程 + FR-110.1 邻域）、docs/design/console-m8.md（树浏览器「Trash Can 常驻节点不建」推翻点 + 侧栏清单）、ADR-0001/0032/0033（clean-room / license 门控 / addon 注册表） |
| 下游消费者 | tech-lead（拆票——宽度 ≤2 内建，§1.3 分票提示）、architect（**ADR-0041**：webhook 事件总线架构〔outbox/投递语义/SSRF 策略〕；视 Q 裁定 **ADR-0042**：D-F2 存量布局迁移）、reverse-engineer（webhook.md 新建 + helm.md 增量段 + conan.md D8 翻转行联动）、dev-go-core（webhook 引擎 + 订阅 REST + conan 小票 + config 旋钮）、dev-registry-adapter（HelmOCI remote/virtual + chartsBaseUrl/_external 缝）、dev-frontend（webhook 控制台最小面 + trash 树常驻节点）、devops-engineer（de-flake raceEnabled escape）、qa-engineer（L 序列 + 断言反转核实 + 双形态回归）、tech-writer（webhook 指南 + HelmOCI remote + 旋钮文档 + 收官清扫）、release-engineer（部署烟测 + UAT 链——M12 T-355 未执行教训：本程 UAT 随里程碑 PR 首跑必须落地）、conductor（裁决入口 + tag m13-done） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-30 | 初版草案（待 conductor 审）：M13 范围（主轴选题 Webhook 事件总线 + ROADMAP「M12 未纳入项」票级遗留聚类全收编 + P2 登记维持四项 + 运维尾巴三项）、FR-114~FR-122、档位矩阵增量 1 行（webhook 槽 Q4）、契约矩阵 11 条、L01~L24、开放问题 Q1~Q7 带暂行；随稿完成 ROADMAP M13 立项行 + 当前里程碑头切换（PM 职责内两处） |
| v1.1 | 2026-08-30 | T-370 文面修正（规格活化后校准——效力序：webhook.md 规格票 > PRD 暂行值）：① 重试语义——FR-115.2/AC2/§1.2 量化门槛/L07/场景 B 五处「指数退避/退避序列」修正为官方基准（重试仅发送失败或 HTTP≥500，4xx/3xx 不重试；固定间隔 10s 非指数退避；retryCount 5 首试计入；单次超时 30s；死信为 BinFlow additive 扩展）；**登记 ADR-0041 决策 4 投递参数与 webhook.md §5 冲突，待 architect 锚点回填修订（K50/BOARD T-370 裁定行）**；② 事件数——「36 事件」规范表述更新为「13 域 66 型」（36 为 inv-4 I1 内部统一总线枚举层）+ 覆盖界落定（本体 9 型 / 休眠 57 型）；③ envelope——FR-114 AC3 删「时间戳」字段断言（artifact 域载荷无时间戳字段，勿发明——T-358 §1-8）；④ SSRF 键名落定（官方 `event.security.blacklist.enabled` ↔ BinFlow `webhook.allow_private_target`，ADR-0041 决策 6）；⑤ Q4 取证结论入文（官方矩阵 Non-commercial ❌/Pro ✅——建议维持 pro+）+ kind 定 KindFeature（ADR-0041 决策 8）；⑥ K47~K50 回填（T-358 AC3 转办 PM 事项） |

---

## 1. 背景与目标

### 1.1 背景

M12 以 `m12-done`（2026-08-30，PR #42 合并 main）收官：NuGet 对齐 bundle（v2 路由全集 + search 上游代理 + 动态解析）、制品生命周期域（copy/move/归档族/回收站）、dual-write fail-open、RSS 瘦身转绿、HelmOCI local（第 18 槽）、MUI 批三与回写批全清；终验 T-356 唯一 P1 缺口（协议仓 restore 索引联动）与确定性红（槽位断言 17→18）均已在收官窗清偿（iteration-941/942）。M13 面对四股输入的汇合：

1. **主轴选题（PM 裁量，§2.2 留痕）**：**Webhook 统一事件总线**进 M13——理由：① inv-4 §I 判定「可整体平移、无需模拟 Access 拆分」（单体实现，订阅管理原生承载），实现风险在候选中最优；② CI/CD 与下游集成刚需（主矩阵缺口 6 原文），且 T-247 Jenkins dogfood 栈仍在 VM 保留——**存在真实消费者可作验收腿**（产品「客户端真实可用」准绳的直接兑现）；③ 与 M12 制品生命周期域天然衔接：删除/复制/移动/属性等事件源缝在统一删除 seam 与操作族上已备，事件织入是旁路增量（主路径零变化，回归风险低）。**落选者滚程**：AQL + 13 老搜索 = 搜索基建专程（查询语言 + 执行引擎 + 分页，体量专程级——M14 专程候选第一顺位）；Build-info 域 = CI 集成「入向」标注域（缺口 9），与 webhook「出向」同族，先出后入，M14+；HA 本体 = Q1 终裁维持「单列专程」，**前置 PRODUCT.md Non-goal 修订解禁未发生 → 不进**（Xray 集成面同族处置）。
2. **票级遗留聚类（ROADMAP「M12 未纳入项」，PM 收编）**：HelmOCI remote/virtual（D-5 翻转点——docker /v2 面首个 remote pull-through，现态 remote 仓只读无代理链）；chartsBaseUrl 分体基址（T-313 D-2）+ `_external` 落盘缓存（D-3——T-342 architect 评估结论「建议 M13 引擎 absolute-URL 缝票」）；folderDownloadConfig / trashcan.retention_days 两枚 YAML 旋钮（缝已备——T-356 L14/L17 如实登记「恒关/恒 14d」）；conan D8 整树删翻转（T-348 双证新取证）+ D-F2 布局迁移（T-340 §4）；deb bz2（维持 Q/推翻通道——不立项）；npm registry/token 尾斜杠接入注释（docs 微票）。
3. **P2 登记维持四项（T-356 ⚠️ 承接）**：D-10 同字节幂等分歧（nuget.md §5.1 臂②规格 409 vs BinFlow as-built 201——上 BOARD 终裁，PM 不代拍）；flat 措辞（PRD「flat 扁平化」vs repo-operations.md §1.6 裁定「flat 折叠进 copy 主参数族」——文面债）；L31 拒启次序解释空间（AC5 主措辞已 `346485e` 对齐 T-349；剩 fail-open AC2「停机窗内重启」与 as-built boot 探针姿势的加注——T-356 观察⑨）；满载 flake 新成员（TestBigTreeCopyNo5xx 预算臂 raceEnabled escape——与 deb 满载族同族处置）。
4. **运维尾巴**：trash 树常驻节点（console-m8 树浏览器「Trash Can 常驻节点不建（BinFlow 无回收站）」条款被 M12 FR-106 推翻——最小面补齐 + 规格回写）；console-m8 侧栏清单过时（12/13 vs 15）。

**不贪多**：M13 以「一条 P0 主线（Webhook 总线两 FR）+ 一个翻转点（HelmOCI 三态齐装）+ 配置旋钮/小票/文面/测试/运维债系统性收口」为形态；任何 Q 触发的增项（symbol server、docker remote 顺车、bz2 翻转）必须走条件票/等量置换条款。

### 1.2 M13 目标与量化门槛

> 一句话：把统一事件总线立起来（订阅 → 事件织入 → 可靠投递全链，真实消费者验收），HelmOCI 补齐 remote/virtual 三态，偿清 M12 登记的旋钮/翻转/文面/测试债——全程延续行为逐项对齐制度。

量化门槛（未达即里程碑不完成）：

| 指标 | M13 门槛 | 来源 |
|---|---|---|
| Webhook 订阅面 | 订阅 CRUD + test 端点 curl 全链绿（wire 照官方文档——webhook.md 逐端点）；过滤器命中/未命中双臂 | FR-114 |
| 事件触发面 | artifact/artifactProperty/docker 域事件在发布/删除/属性/推送路径真实触发（七字段 envelope 逐字段断言——artifact 域载荷无时间戳字段）；事件类型注册（13 域 66 型全表；36 为内部总线枚举层）与触发源覆盖界照 K48 定案 | FR-114 |
| 投递可靠性 | kill -9 后 outbox 事件零丢补投；接收器 500 → 固定间隔 10s 重试恢复（4xx 不重试负面臂）；超上限死信可查；secret 签名接收侧校验绿 | FR-115 |
| 真实消费者 | dogfood Jenkins 条件腿或容器接收器腿全绿（dep: 用户环境时条件票留痕） | FR-115 |
| HelmOCI remote | 上游 push → 经 BinFlow remote pull 字节/digest 一致；二次命中本地缓存（上游不回源）；上游认证腿绿 | FR-116 |
| HelmOCI virtual | local+remote 成员聚合 pull 双域制品；混仓校验维持 400 | FR-116 |
| chartsBaseUrl/_external | 异构基址回源正确；`_external` 首次 GET 落盘 + 二次命中本地；local 仓 400 维持 | FR-117 |
| 旋钮两枚 | folderDownload 开→目录 zip 200（关态 403 文案逐字维持）；retention_days=1 短周期清理 + 默认 14 维持 | FR-118 |
| conan 翻转与迁移 | v1 DELETE 整树删（多修订全消）；D-F2 存量迁移幂等零损 + settings 元数据恢复（`-q` 过滤腿） | FR-119 |
| 文面裁定包 | D-10 上 BOARD 终裁落痕；flat/L31 加注两处落笔 | FR-120 |
| 测试与 CI 债 | `make test`（race）全树全绿——不依赖「隔离复跑绿」辩护；CI e2e job 三连绿 | FR-121 |
| 运维尾巴 | 树常驻节点 + 侧栏清单回写 + 尾斜杠注记三件落笔 | FR-122 |

### 1.3 上游依赖与并行关系（含分票提示）

- **前置产物（拆票前）**：① **webhook.md**（reverse-engineer；**已交付 T-358〔2026-08-30〕**——官方文档逐端点出处 + 事件清单〔13 域 66 型 + 覆盖界：本体 9 型/休眠 57 型〕+ CloudEvents envelope 形态 + secret/签名 + 过滤器 + 投递语义；inv-4 I2~I5/K3 反编译锚点补白；tech-lead 就绪度确认）；② **ADR-0041**（architect——事件总线架构：outbox 载体〔SQLite/PG 双方言〕/投递队列与重试策略/重试上限与死信/secret 存储与签名/订阅 URL SSRF 策略〔复用 M3 Guard + 私网开关语义〕/与审计事件族的关系；**Accepted 2026-08-30〔T-359〕——决策 4 投递参数与 webhook.md §5 官方值冲突，待锚点回填修订〔见 FR-115.2 注/K50〕**）；③ 视 Q 裁定 **ADR-0042**（D-F2 存量布局迁移方案：启动期 vs 惰性、回滚）；④ helm.md 增量段（chartsBaseUrl/_external as-built 锚点细化——S8/S10 已高置信，增量小，随 FR-117 票）；⑤ trash-can.md / repo-operations.md 旋钮行回写随票（T-330 模式，不单列前置）。
- **依赖序**：FR-114/115 dep webhook.md + ADR-0041（规格/ADR 先行，事件织入大票窗口独占）；FR-116 dep helm.md 增量（remote 代理链设计）且与 FR-117 同域先后脚（remote 引擎 absolute-URL 缝在 FR-117 消费）；FR-119.2 dep ADR-0042；FR-120 为 PM 票随时可动（D-10 裁定材料不阻塞任何实现票）；FR-121 devops 独立可首波并行；FR-122.1 FE 票 dep 锚册纪律（console-m8 回写先行或同票）。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-114 拆 2 票（规格票 → 订阅 REST + 事件织入大票〔internal/webhook + httpapi，窗口独占〕）；FR-115 拆 2 票（投递引擎〔outbox + 重试/死信/签名〕→ 控制台最小面 + 真实消费者 e2e〔web/〕）；FR-116 拆 2 票（remote pull-through → virtual 聚合）；FR-117 一票（internal/remote 引擎缝 + repo config）；FR-118 一票（internal/config）；FR-119 拆 2 票（D8 翻转小票 / D-F2 迁移票——同 area 串行）；FR-120 一票（PM）；FR-121 一票（devops）；FR-122 拆 2 票（FE〔web/〕/ docs 票）；QA 两票（中期回归 + 终验）；tech-writer 一~两票；release 一票（烟测 + **UAT 随里程碑 PR 首跑**——M12 T-355 未执行教训，T-356 §6 留痕）。估 **21~26 票**（含条件票 slot：symbol server Q2 / docker remote 顺车 Q5 / D-10 翻转 Q3）。
- **QA 并行面**：webhook 需可编排接收器夹具（容器 httpbin/本地脚本接收器 + 故障注入 500/超时）+ kill -9 编排；HelmOCI remote 需上游夹具（自指 BinFlow helmoci local〔M12 已有〕+ mock registry 认证腿）；D-F2 需存量双拼布局树夹具（T-340 复现脚本可复用）；retention 需短周期时钟夹具（M12 cron 单测 13/15 天臂已在）；断言反转两处（conan D8 / folderDownload 开关化）+ 一处服务端布局对齐（D-F2）须 PRD 回写核实。

### 1.4 全程工作方式条款（M11 §1.4 / M12 §1.4 制度延续——常设准绳，M13 全程生效）

1. **行为逐项对齐**：本里程碑全部范围（webhook 面、HelmOCI remote/virtual、chartsBaseUrl/_external、旋钮、conan 翻转、迁移、文面回写）的可观测行为 / 命名 / 语义 / 错误码 / 交互 100% 照 Artifactory；**webhook 域取证特例**：反编译集合无该 addon——以 JFrog 官方 REST 文档为唯一行为基准（full-feature-matrix §待验证既定 + inv-4 L186 声明），inv-4 §I/§K 反编译锚点仅补文档空白（outbox 六方言 DDL 形态、outbound dispatcher 存在性）。
2. **自有裁定权收归用户**：任何与 Artifactory 的行为分歧必须上 BOARD 请用户裁决（本程新增：D-10 终裁、Q4 档位、Q6 事件覆盖界）——本 PRD 暂行值仅为开工口径，标注「暂行待裁」（§7 Q 表）。
3. **规格票出处义务**：webhook.md / helm.md 增量段 / conan.md 翻转行逐条给出「官方文档锚点或反编译类/方法 + 行为描述」。
4. **红线保留（ADR-0001 不变）**：不逐行翻译 Java→Go；license 文档格式维持自有 ed25519；webhook envelope 照官方 JSON 契约（数据契约非代码）。
5. **流程条款（延续）**：新端点走 PM FR + ADR 流程，实现票不得私加端点；拆票宽度 ≤2；协议/行为面票必须依赖对应规格票；每票 AC 附真实客户端验收命令。

---

## 2. 范围

### 2.1 In scope

| # | 来源（指令/裁决/登记） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 主轴选题（PM，§2.2 留痕）+ 主矩阵缺口 6 + inv-4 §I | FR-114（Webhook 订阅管理 REST + 事件类型注册〔13 域 66 型〕+ 事件源织入）+ FR-115（投递引擎：outbox/重试/死信/签名/SSRF + 控制台最小面 + 可观测） | P0（FR-115 FE 面 P1） |
| B | ROADMAP「M12 未纳入项」票级遗留（D-5 翻转点） | FR-116（HelmOCI remote pull-through + virtual 聚合——docker 面首个 remote 数据链） | P0（remote）/ P1（virtual） |
| B | 票级遗留（T-313 D-2/D-3 + T-342 评估结论） | FR-117（chartsBaseUrl 分体基址 + `_external` 落盘缓存——引擎 absolute-URL 缝票） | P1 |
| B | 票级遗留（旋钮两枚，缝已备） | FR-118（folderDownloadConfig 六字段 + trashcan.retention_days——YAML 旋钮化） | P1 |
| B | 票级遗留（T-348 D8 双证 + T-340 §4 D-F2） | FR-119（conan v1 DELETE 整树删翻转 + files 通道布局迁移与存量搬迁） | P1 |
| C | P2 登记维持（T-356 ⚠️ 承接） | FR-120（文面裁定包：D-10 上 BOARD 终裁〔P0 裁定动作〕+ flat 措辞回写 + fail-open AC2 加注〔P2 落笔〕） | P0 / P2 |
| D | 运维尾巴 + 满载 flake | FR-121（de-flake：raceEnabled escape + CI runner 权威化）；FR-122（trash 树常驻节点 + console-m8 侧栏清单 + npm 尾斜杠注记） | P1 / P2 |
| E | 候选池裁量 | （不设 FR）NuGet symbol server 余量条件票（Q2——M12 承接）；docker remote 本体顺车评估（Q5）；deb bz2 推翻通道（Q7）；其余主轴候选滚 M14+（§2.2） | — |

前置产物（非 FR）：webhook.md、ADR-0041（、视 Q 裁定 ADR-0042）。

### 2.2 Non-goals — M13 明确不做

**产品级（继承 PRODUCT.md，不越界）**：**HA 集群本体与 Xray 集成面本体不做**——「行为逐项对齐」指令不自动解锁功能本体；**HA 进任何里程碑的前置 = 用户修订 PRODUCT.md「明确不做」段**（Q1——截稿未发生；解禁后专程单列 + ADR 群〔心跳/推举/传播〕，PM 届时另出 PRD）。不做 Artifactory 全量 REST 兼容（高频子集承诺维持）。

**M13 里程碑级 Non-goals（含主轴滚程留痕——去向全部登记）**：

| 不做项 | 隔离边界 / 去向 |
|---|---|
| **HA 本体**（集群心跳/推举/传播）+ Xray 集成面本体 | Q1 维持 M12 终裁「单列专程」——**前置 PRODUCT.md 修订解禁（用户动作）**；未解禁不进 M13/M14 任何主线；解禁后 PM 建议专程里程碑（非混编） |
| AQL + 13 老搜索族 | **M14 专程候选第一顺位**（搜索基建——查询语言/执行引擎/分页，体量专程级；主矩阵缺口 2）；M13 主轴容量让位 webhook（同集成域，先出向后） |
| Build-info 域（PUT/append/查询/promotion/retention） | M14+（主矩阵缺口 9——CI 集成「入向」标注域；与 webhook 同族先出后入；`/api/docker/{repo}/v2/promote` 随域） |
| NuGet symbol server | **余量条件票**（Q2，非 DoD 硬门——M12 Q2 未触发承接）——全部 P0/P1 收官且余量足则触发（mini as-built 规格随票，T-293 终裁口径）；未触发 M14+ |
| docker（非 helmoci）remote pull-through 本体 | **Q5 顺车评估**：暂行不顺车滚 M14+；FR-116 规格票若判定 /v2 共享缝边际成本≈0 则条件票承接（BOARD 留痕） |
| 制品 license 识别（licences.xml 91 模式）/ 冷存储分层 | M14+（冷存储随 Cleanup-Retention 域立项） |
| Cleanup/Retention 策略引擎 | M14+（与回收站分界维持——M13 仅 retention 旋钮，勿混淆） |
| Go 深化（sumdb 代理 + external 重定向）/ Terraform / GitLFS / HuggingFace 等 AI/ML 13 型 | 滚 M14+ 远期分期（沿 M12 §2.2 既定） |
| deb Packages.bz2 压缩档 | **维持不做**（Q7——dsnet 依赖不可得实证 T-314；LC-45 D 层留痕；用户提供实现路径则翻转） |
| conan remote search 上游代理 | 维持登记（T-348 §4-2 P3 候选——cargo 已跟进，conan 404 诚实文案在案；用户可推翻） |
| statisticsEnabled / sourceOrigin 行为化、属性复制协议面扩列、keypair T-319 / SAML T-331 差异族、crates.io 直连双主机 | 维持在案（各票报告在档——M12 §2.2 既定） |
| D-10 同字节幂等行为翻转 | **待裁不动**（Q3——终裁前维持 as-built 201；终裁若翻转走条件小票） |
| 66 型中 BinFlow 无本体域的 57 型触发源实现（build/releaseBundle/distribution/curation/xray/app_trust 等——webhook.md 覆盖界逐条标注） | Q6——暂行「类型注册休眠」（可订阅、无触发源、文档如实）；不伪造触发 |
| 逐行翻译 Java→Go / 复制 JFrog license 密钥格式 | **永久不做**（ADR-0001） |

---

## 3. 用户与场景（M13 视角）

- **场景 A（平台工程师，CI 联动）**：Jenkins 流水线订阅 BinFlow 仓的制品发布事件——`mvn deploy` 落仓即触发下游构建 job（T-247 dogfood 栈真实复现）；secret 签名让接收端可验真。
- **场景 B（运维，可靠投递）**：webhook 接收端短暂宕机——BinFlow 固定间隔重试（10s）、恢复后补投；持续失败入死信可查；BinFlow 自身重启事件不丢（outbox 持久）。
- **场景 C（Helm 平台工程师）**：helmoci 仓三态齐装——remote 代理上游 registry（首拉回源、二次命中缓存），虚仓聚合本地私有 chart 与远端公共 chart；上游 chart 托管在自定义基址时 chartsBaseUrl 独立配置。
- **场景 D（管理员，配置面）**：按需打开目录 zip 下载（默认关——文案逐字不变）；回收站保留期按合规要求从 14 天调至 30 天，YAML 一键。
- **场景 E（conan 用户）**：v1 删除行为对齐（坐标根整树删——多修订全消）；存量 v1 files 树平滑迁移，`-q` 过滤恢复工作。
- **场景 F（维护者/QA）**：race 全树不靠隔离辩护绿起来；PRD/规格/实现三者文面一致（flat/L31/D-10 各归各位）。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；Playwright spec 置 `web/e2e/m13/`；**webhook 订阅 REST 的端点路径/请求响应体以 webhook.md（官方文档出处）定案为准，本节 AC 钉行为与状态码**；helmoci remote 数据面沿 /v2 既有挂载（M2 ADR-0010 / M12 HL-3）；旋钮键名以 K52 定案（暂行 `trashcan.retention_days`）；**行为基准冲突时的效力序：用户裁决（BOARD）> 规格票（含 as-built 段）> ADR > 本 PRD 暂行值**。全部实现票不得私加端点。

### 4.1 Webhook 统一事件总线·上（主轴选题，P0）

#### FR-114 订阅管理 REST + 事件注册（13 域 66 型）+ 事件源织入（internal/webhook + httpapi + 各 adapter 事件缝；前置 webhook.md〔已交付 T-358〕+ ADR-0041）

**用户故事**：
- 作为 CI/CD 工程师，我用 curl（或 Jenkins 配置面）创建 webhook 订阅——指定事件类型、仓/路径过滤器、回调 URL 与 secret；发布/删除制品后接收端立即收到结构化事件。
- 作为运维管理员，我管理订阅全生命周期：查、改、删、test（不等待真实事件即可验证链路）；未授权主体不可碰订阅面。

行为规格：

- **114.1 前置规格票（reverse-engineer；已交付 T-358，2026-08-30）**：**docs/reverse/webhook.md**——官方文档逐端点出处（订阅 CRUD/test 七端点 + 过滤器形态 + secret/签名 + envelope 字段集）；事件清单按域分组（**13 域 66 型**——36 为 inv-4 I1 内部统一总线枚举层，两层关系见 webhook.md §0）并逐条标注 **BinFlow 触发源覆盖界**（本体 9 型 → 织入点；休眠 57 型 → 注册休眠，Q6）；inv-4 §I（I2 管理在 Access 的平移判定 / I3 outbound dispatcher / I4 事件注册 REST / I5 worker events 40+ 类型清单）与 §K3（outbox 六方言 DDL）作为反编译锚点补白；tech-lead 就绪度确认（M13 拆票在案）。
- **114.2 订阅 REST 族**：CRUD + test（wire 逐端点照 webhook.md；基座前缀差异沿 E-26 口径）；权限门（admin 或 ADR-0041 定案的专用权限——`internal:webhook` 权限常量的 BinFlow 映射归 ADR）；订阅校验（URL 合法性/事件类型闭集/过滤器语法）。
- **114.3 事件源织入**：在既有统一删除 seam、copy/move 操作族、属性系统、各协议发布路径上旁路取事件（M12 生命周期域底座复用；outbox 异步——主路径零变化）；P0 触发域 = **本体 9 型**（artifact 5 + artifactProperty 2 + docker pushed/deleted——逐型清单与织入点照 webhook.md 覆盖界标注；docker tag* 无独立织入点归休眠）；其余 57 型照 K48 覆盖界定案（注册休眠）。
- **114.4 门控**：新 addon 槽 `webhook`（第 19 槽，kind = **KindFeature**——ADR-0041 决策 8 已裁不新增第三值，「feature-int」为注记性写法）——community locked / pro+ unlocked（**Q4 待终裁确认**：webhook.md 取证在案——官方功能矩阵 Webhooks 行 Non-commercial ❌/Pro ✅，T-358 §1-6 **建议维持 pro+ 不翻转**）；三缝语义自动生效（FR-85 机制复用，事件织入面降级为不入箱）。
- **114.5 与 Build-info 的边界**：build/releaseBundle 等无本体域事件类型按 Q6 处置（暂行注册休眠）——不伪造触发源。

验收标准（AC）：

- **AC1（规格票交付）**：webhook.md 落 docs/reverse/（官方文档出处逐条 + 13 域 66 型事件清单 + 覆盖界标注 + 置信度标定）；tech-lead 拆票就绪确认。**（已兑现：T-358 done 2026-08-30）**
- **AC2（订阅 CRUD + test）**：curl 全链——create（201/返回 id 形态照规格）→ get → list → update → delete；`test` 端点向接收器真发一条测试事件（接收器侧断言信封）。
- **AC3（事件触发）**：generic 仓 PUT 制品 → 接收器收到 artifact 域 deployed 事件（七字段 envelope 逐字段断言照 webhook.md——**artifact 域载荷无时间戳字段，勿发明**〔T-358 §1-8〕；token 触发时 userContext.isToken=true 臂）；DELETE / copy / move / 属性 PUT 各触发对应事件；docker push → docker 域事件（dind 腿）。
- **AC4（过滤器）**：订阅限定 repo/path 过滤 → 命中发、未命中不发（接收器计数双臂断言）。
- **AC5（权限与门控）**：非授权主体订阅 CRUD 全 403 零副作用；webhook 槽三缝（community 建订阅 403 + `X-Binflow-License-Required: webhook` → pro 200 → 卸载降级形态）。
- **AC6（回归）**：M1~M12 P0 抽样零回归（事件织入为 outbox 旁路——主路径时延与行为零变化）。

### 4.2 Webhook 统一事件总线·下（主轴选题，P0）

#### FR-115 投递引擎（outbox/重试/死信/签名/SSRF）+ 控制台最小面 + 可观测（internal/webhook 投递链 + web/；dep ADR-0041）

**用户故事**：
- 作为接收端维护者，我按官方契约验签（secret → HMAC）；投递失败有界重试、超限入死信可排查；BinFlow 重启不丢事件。
- 作为管理员，我在控制台管理订阅并查看最近投递记录；订阅私网回调地址受策略管控（SSRF 防护与既有 remote/replication 同族语义）。

行为规格：

- **115.1 outbox 持久化**（inv-4 K3 outbox 模式——BinFlow 单体两方言）：事件产生 → 事务入箱 → 异步投递；重启幸存（盘上事实源）。
- **115.2 重试与死信（v1.1 对齐 webhook.md §5 官方基准——原「指数退避」为 PRD 暂行值，按效力序规格票 > PRD 修正）**：重试条件 = **仅发送失败或 HTTP ≥500（4xx/3xx 不重试）**；**固定间隔 10s**（retryWaitMillis，非指数退避）；retryCount **5**（首试计入，共 5 次尝试）；单次超时 **30s**（含建连/重定向/读体）；重定向不跟随（ADR-0041 决策 2）；投递不阻塞主路径。超上限 → status=dead 死信可查可重放（**BinFlow additive 扩展**——官方无持久死信、耗尽后弃投仅排障记录留痕〔webhook.md §5.3 明示 BinFlow 形态归 ADR-0041〕）。**ADR-0041 决策 4 冲突登记**：该 ADR（与 T-358 并行定案）参数为 10 次尝试/2s 起步 ×2 递增/单次 10s 超时，且决策 2 将 3xx·4xx 计入可重试——与 webhook.md §5 官方值冲突；按效力序（webhook.md > ADR）**建议 architect 随锚点回填修订 ADR-0041 决策 4 对齐规格**（机制条款不翻：outbox 双方言两表/死信 additive/Guard 默认拒私网）；T-370 BOARD 裁定行已登记，终裁归 conductor/用户。
- **115.3 签名**：订阅 secret → 请求签名头 `X-JFrog-Event-Auth` + HMAC-SHA256（官方契约已锚——webhook.md §6，K49；`use_secret_for_signing` 双态，签名态头承载中置信待活体）；secret 存储 AES-GCM 链维持（不落明文）。
- **115.4 SSRF 防护**：订阅 URL 校验复用 M3 Guard 家族（DNS rebinding pinning 五参数）+ 私网目标开关——官方 canonical 键 `event.security.blacklist.enabled`（默认 true 禁私网，webhook.md §5.4）；BinFlow 旋钮拼写已由 ADR-0041 决策 6 定案为 `webhook.allow_private_target`（默认 false，镜像 replication 键族——与官方 blacklist 语义等价、键名 BinFlow 化）。
- **115.5 控制台最小面**（P1；web/）：订阅列表/新建/编辑/删除/test + 最近投递记录（状态/耗时/重试计数）——MUI 组件纪律（FR-111 四闸门同构）；readonly_admin 只读。
- **115.6 可观测**：`binflow_webhook_{deliveries_total, retries_total, dead_letter_total, queue_depth}` 指标族 + 审计事件 `webhook.subscription.{create,update,delete,test}` + 死信告警日志一行；投递日志 URL 脱敏。

验收标准（AC）：

- **AC1（outbox 幸存）**：触发事件 → kill -9 → 重启 → 事件补投接收器（零丢断言）。
- **AC2（重试语义；v1.1）**：接收器 500×N → 固定间隔 10s 重试序列 → 恢复 200 → 最终成功；接收器 4xx（如 404）→ **不重试**（单次终态）负面臂；全程主路径（制品 PUT）零 5xx 零阻塞。
- **AC3（死信）**：持续失败超上限 → 死信状态可查（REST/控制台）+ 告警日志；重放动作绿。
- **AC4（签名）**：配置 secret 的订阅 → 接收器按官方契约 HMAC 校验绿（篡改 body → 校验红）。
- **AC5（真实消费者）**：T-247 dogfood Jenkins 条件腿（dep: 用户环境——VM 栈在位则接一条 pipeline 触发腿，事件 → job 触发取证）或容器接收器腿（httpbin/脚本接收器 + 故障注入）全绿；条件不可得则容器腿 + BOARD 留痕（非 DoD 缺口）。
- **AC6（SSRF）**：订阅私网 URL → 拒绝形态照 ADR-0041（默认策略 + 开关双臂）。
- **AC7（可观测与 FE）**：指标/审计断言；控制面 Playwright 腿（CRUD + 投递记录 + readonly 臂）+ 四闸门维持。

### 4.3 HelmOCI 三态齐装（D-5 翻转点）

#### FR-116 HelmOCI remote pull-through + virtual 聚合（internal/adapter/docker remote 链 + repo；P0 remote / P1 virtual）

**用户故事**：
- 作为 Helm 平台工程师，helmoci remote 仓代理上游 OCI registry——`helm pull oci://$BASE/<helmoci-remote>/...` 首拉回源、二次命中本地缓存；虚仓聚合本地私有 chart 与远端 chart，pull 体验与 local 无差别。

行为规格：

- **116.1 remote pull-through（P0）**：helmoci remote 仓——manifest（by tag/digest）+ blob 按需回源、缓存落盘（checksum 寻址复用）、上游认证链（401 → WWW-Authenticate Bearer → token 交换 → 拉取）；**docker /v2 面首个 remote 数据链**（现态 remote 仓在 /v2 面只读无代理——`ErrRepoTypeNotSupported` 只读姿态；本 FR 为 M3 Q4 缓议的翻转点，经 helmoci 先航）。
- **116.2 virtual（P1）**：helmoci 成员聚合（tag 并集 / by-digest 路由 / 首见语义照规格）；「Helm 与 HelmOCI 不混仓」校验维持 400（M12 边界复用）。
- **116.3 门控**：helmoci 槽（pro）既有——remote/virtual 自动受缝。
- **116.4 docker remote 顺车评估（Q5）**：规格票判定 /v2 共享缝边际成本；暂行不顺车（docker remote 本体滚 M14+）。
- **116.5 上游故障降级**：已缓存制品可拉；未缓存零 5xx（降级形态照 remote 既有口径——本地事实兜底 + 降级标记）。

验收标准（AC）：

- **AC1（remote 全链）**：自指上游（BinFlow helmoci local push chart）→ 经 BinFlow helmoci-remote `helm pull` → 字节一致 + manifest digest 断言；二次 pull 命中本地缓存（上游访问计数不增断言）；`helm install` 上真集群腿（M12 kind 夹具复用）。
- **AC2（上游认证）**：上游需 Bearer 认证形态（mock registry 或自指 token 面）→ token 交换链绿。
- **AC3（virtual 聚合）**：helmoci-virtual = local + remote 成员 → pull 双域制品；混仓（helm + helmoci）建仓 400 维持。
- **AC4（降级与回归）**：停上游 → 已缓存可拉 / 未缓存零 5xx；docker dind /v2 面全量回归 + M12 helmoci local 序列零回归。
- **AC5（门控）**：helmoci 槽三缝（remote/virtual 建仓含内——community 400 点名 helmoci/pro → 卸载降级）。

### 4.4 remote 域配置缝票（T-313 D-2/D-3 + T-342 评估结论）

#### FR-117 chartsBaseUrl 分体基址 + `_external` 落盘缓存（internal/remote 引擎缝 + repo config；P1）

**用户故事**：作为 Helm remote 仓管理员，上游 chart 托管在自定义基址（index 内绝对 URL 异于仓基址）时，`charts_base_url` 独立配置即可正确回源；虚仓聚合外部域依赖时，`_external` 路径首次下载回源并落盘，二次命中本地。

行为规格：

- **117.1 chartsBaseUrl（D-2，P1）**：remote 仓 `charts_base_url` 字段——回源地址 = chartsBaseUrl + 仓内相对路径，缺省回退仓 URL（helm.md §5 回源链/S8/S10 高置信锚点）；REST 接受/回显 + 控制台仓表单字段（repo config 域）。
- **117.2 `_external` 落盘缓存（D-3——T-342 评估结论兑现）**：**引擎 absolute-URL 缝票**——虚仓/代理面改写产出的 `_external/<protocol>/<url>` 路径实现按需回源取数与落盘缓存（现态：改写形态在场、取数落盘缺位）；落盘命名空间自身（Artifactory -cache 仓惯例不采纳——conan.md N7 同裁定）；`_transitive` 镜像路径同缝；local 仓 `_external` → 400 维持（helm.md §2 端点表）。
- **117.3 门控**：helm 槽既有——配置字段与引擎缝不新增槽。

验收标准（AC）：

- **AC1（chartsBaseUrl）**：上游 index 内绝对 URL 指向自定义基址 → remote 配 `charts_base_url` → `helm pull` 回源正确 + 字节一致；REST PUT/GET 回显；缺省回退臂。
- **AC2（`_external` 落盘）**：虚仓（remote 成员聚合外部域 chart 依赖形态）→ index 内 URL 改写为 `_external` 路径 → 首次 GET 200 + 存储树落盘可寻址 + 二次命中本地（上游不回源计数断言）；local 仓 `_external` GET → 400 维持。
- **AC3（回归）**：helm 经典仓三态（M11 FR-99 序列）+ M12 helmoci 序列零回归。

### 4.5 配置旋钮两枚（缝已备）

#### FR-118 folderDownloadConfig 六字段 + trashcan.retention_days（internal/config + 对应消费面；P1）

**用户故事**：作为管理员，目录 zip 下载按需开启（合规边界内配置匿名与限额），回收站保留期按组织要求调整——两枚 YAML 旋钮，行为与关闭态逐字不变。

行为规格：

- **118.1 folderDownloadConfig**：六字段旋钮化（repo-operations.md §2.1 双证——`enabled` 默认 false / `enabledForAnonymous` / 大小上限 / 文件数上限 / 并发上限 / `enabledEmptyDirectories`）；BinFlow config.yaml 键名族 K52 定案；关态 403 文案逐字维持（T-356 L14 实测基线 `Download Folder functionality is disabled.`）；开态复用 M12 目录 zip 链路（`GET /api/archive/download`）。
- **118.2 trashcan.retention_days**：默认 14 维持（M12 as-built）；旋钮生效于保留期清理 cron（M12 13/15 天臂单测基座）；trash-can.md「恒 14d」已知边界表 + api-reference「配置旋钮未落」自注随票翻转。
- **118.3 门控**：repo-operations / trashcan 槽既有——旋钮不新增槽。

验收标准（AC）：

- **AC1（folderDownload）**：默认关 → 403 文案逐字；开启 → `GET /api/archive/download` 200 + 解包 sha256 对账（M12 L14 断言升级为开关化——断言反转登记）；匿名关 → 401 文案臂；超限拒绝臂。
- **AC2（retention）**：`retention_days=1` 短周期夹具 → 过期自动清理 + 审计；期内不清理；默认 14 回归（M12 cron 臂）。
- **AC3（文档）**：trash-can.md / api-reference / artifact-operations.md 自注翻转 + `make docs` SUCCESS。

### 4.6 conan 翻转与迁移小票包

#### FR-119 D8 整树删翻转 + D-F2 files 通道布局迁移（internal/adapter/conan；P1；D-F2 段 dep ADR-0042）

**用户故事**：作为 conan 1.x 用户，`conan remove <ref>` 删除整个坐标（全部修订）——与 Artifactory 及参考实现一致；v1 files 通道上传的包树落规格布局，`-q` 过滤恢复工作。

行为规格：

- **119.1 D8 整树删翻转（T-348 N5 双证）**：v1 `DELETE conans/<ref>` 由 as-built「latest 修订链」翻转为「**坐标根整树删（全部修订）**」（反编译 `LocalConanHandler.removeRecipe`〔getRecipePath 无修订段 + canWrite 门〕+ conan 1.66 参考实现 remove_conanfile 双证；v2 无修订 DELETE 同步核对——规格 §3.1 该行本写「全部修订」）；conan.md 规格行随票回写（分歧消除）。
- **119.2 D-F2 布局迁移（T-340 §4）**：`v1.go channelFileName` 复数/单数 trim 错位修正 → 包文件落 `<root>/<pid>/<pRev>/<file>` 规格布局（现态双拼 `…/0/package/<pid>/0/package/<pid>/<file>`——roundtrip 对称故客户端面无感）；**存量树迁移**（启动期或惰性——ADR-0042 定案，含回滚）；迁移后 ① ref-search conaninfo 读取恢复（settings 非 `{{}}`）② v1 包 snapshot 键裸文件名。
- **119.3 conan 1.x / 2.x 双客户端回归**：全链（upload/install/remove/search）双客户端绿。

验收标准（AC）：

- **AC1（D8）**：多修订包（r1/r2）→ v1 DELETE 坐标 → 全修订树删（GET 404 × 2）；conan 1.66 remove 腿 + 2.x 回归绿；M12 as-built「latest 链」断言**反转**（归属本 FR 豁免票，PRD 回写）。
- **AC2（D-F2 迁移）**：存量双拼布局树夹具（T-340 复现脚本）→ 升级迁移 → 规格布局；迁移前后制品 sha256 对账零损 + install roundtrip；迁移幂等（二跑零改）。
- **AC3（settings 恢复）**：新 PUT v1 通道包 → ref-search settings 含 conaninfo 字段（非空）；`-q` 过滤腿命中。
- **AC4（回归）**：conan v1/v2 全链（M11 T-308/T-312 + M12 T-340 序列）零回归。

### 4.7 文面裁定包（P2 登记维持四项承接）

#### FR-120 D-10 终裁上 BOARD + flat 措辞回写 + fail-open AC2 加注（PM；裁定动作 P0 / 落笔 P2）

**用户故事**：作为团队，M12 终验维持登记的四处文面/裁定债各归各位——规格、PRD、实现三者一致，后续拆票不再读到旧口径。

行为规格：

- **120.1 D-10 同字节幂等终裁（上 BOARD，PM 出对照材料不代拍）**：nuget.md §5.1 臂②（包已存在且无 d 权限 → 409）vs BinFlow as-built（同字节 + 仅 w 权限主体重传 → 201，T-356 L03 实测）；终裁方向二选一：对齐 409（行为翻转小票 slot）或裁定 201 为有意差异（D 层留痕 + nuget.md 差异登记行）；终裁前维持 as-built。
- **120.2 flat 措辞回写（T-356 L12）**：M12 PRD「flat 扁平化」措辞 → repo-operations.md §1.6 裁定「flat 折叠进 copy 主参数族」口径（M12 PRD v1.1 增订 + docs/user/integrations/artifact-operations.md 同步）。
- **120.3 L31 残余解释空间加注（T-356 观察⑨）**：AC5 主措辞已 `346485e` 对齐 T-349（收官笔头批在案）；剩余 = M12 PRD FR-107 AC2「停机窗内重启」与 as-built boot 探针 fail-closed 姿势（S3 全停窗内整进程重启 → bucket 探针拒启；队列在盘幸存 + 恢复后水位/排空 T-356 实测）的 **PRD 加注**（加注不改行为、ADR-0040 零修改）。

验收标准（AC）：

- **AC1（D-10）**：BOARD 裁决落痕（对照材料在案）；若翻转 → 小票验证绿（同字节无 d → 409 逐字）；若留痕 → nuget.md 差异行落笔。
- **AC2（flat）**：M12 PRD v1.1 增订 + 用户文档同步落笔（grep 旧措辞零残留）。
- **AC3（L31 加注）**：M12 PRD AC2 加注落笔（ADR-0040 不动、行为零变化断言维持）。

### 4.8 测试与 CI 债（满载 flake 族收口）

#### FR-121 raceEnabled escape + CI runner 权威化（devops-engineer；P1）

**用户故事**：作为维护者，`make test`（race）全树一次绿——不再以「隔离复跑绿」为辩护词；CI e2e 专用 job 连续绿成为可信信号。

行为规格：

- **121.1 raceEnabled escape（T-356 DoD#6 满载 flake 新成员）**：`TestBigTreeCopyNo5xx` 预算臂在 race 姿态下的豁免/重定标机制（race 开销计入预算或 skip 该臂——形态票内定案）；deb 满载族（T-329 以来）同机制收口或归因留痕。
- **121.2 CI 权威化**：`.github/workflows/ci.yml` e2e 专用 job（T-356 L32 承证在盘——真二进制 + 三 seed + workers=2 AUTHORITATIVE 声明）三连绿基线；Jenkins nightly 全量链对账。
- **121.3 边界**：不为绿而削断言——预算臂的语义以独立标注（escape 注释 + issue 留痕）保留，不做静默放宽。

验收标准（AC）：

- **AC1（race 全树）**：`make test`（race）全树零 FAIL，一次跑绿（不依赖隔离复跑）——连续两轮。
- **AC2（CI 三连）**：e2e 专用 job 三连绿 + nightly 对账记录入票。
- **AC3（无静默放宽）**：escape 点逐处注释/留痕可审计（grep 清单入票报告）。

### 4.9 运维尾巴（FE + docs）

#### FR-122 trash 树常驻节点 + console-m8 侧栏清单 + npm 尾斜杠注记（web/ + docs；P1/P2）

**用户故事**：作为 Artifactory 迁移用户，制品树里能直接看到回收站入口（与 Artifactory 树浏览器同形）；控制台规格册与现役页面清单一致；npm 接入文档不再踩尾斜杠坑。

行为规格：

- **122.1 trash 树常驻节点（P1；console-m8 推翻条款兑现）**：console-m8 树浏览器节「Trash Can 常驻节点**不建**（BinFlow 无回收站）」被 M12 FR-106 推翻——**先改册后实现**（规格先行纪律）：console-m8.md 该节回写 + 制品树顶层常驻回收站节点最小面（入口跳转/内嵌沿用 M12 回收站页面；锚册纪律——新锚入册）；四闸门维持。
- **122.2 console-m8 侧栏清单回写（P2）**：清单 12/13 → 对齐现役 15 页（M12 新增页面后过时——ROADMAP 运维尾巴登记）；锚册 ledger PASS。
- **122.3 npm registry/token 尾斜杠接入注记（P2；ROADMAP 票级遗留）**：docs/user/integrations/npm.md 接入段补注（registry URL 尾斜杠形态说明——避免接入踩坑）。

验收标准（AC）：

- **AC1（树节点）**：Playwright——制品树顶层回收站常驻节点可见 + 深链 + readonly 臂；console-m8.md 回写落笔（推翻条款留痕）；四闸门 + axe 双主题 0。
- **AC2（侧栏清单）**：console-m8 清单与现役页面 15 对齐 + 锚册 ledger PASS + 全量 playwright 绿。
- **AC3（npm 注记）**：npm.md 注记落笔 + `make docs` SUCCESS 零断链。

---

## 5. 兼容性矩阵（M13——Webhook 端点对齐 + HelmOCI 三态 + 旋钮/翻转留痕）

### 5.1 层级定义（沿用 M2~M12 分级口径）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 端点路径/方法/语义对齐 Artifactory 或公开规范（高频子集承诺范围）；基座前缀差异（`/binflow` vs `/artifactory`）沿 E-26 口径；加宽回显 additive |
| **C 自有（/api/v1 或内部语义）** | 无 Artifactory 对应或 BinFlow 自有设计；行为模式对齐但载体自定 |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / 依赖不可得实证 / 安全向），矩阵留痕防再议 |
| **待裁** | 存在与 Artifactory（或规格）的可观测差异，须用户终裁——终裁后归 A/D 并回写 |

### 5.2 档位 × addon 解锁矩阵（M13 增量 1 行，其余沿 M12 §5.2〔18 槽〕不变）

| addon 槽位（M13 增量行） | kind | community | pro | enterprise | M13 状态 |
|---|---|---|---|---|---|
| webhook | feature（KindFeature——ADR-0041 决策 8） | locked | unlocked | unlocked | M13 交付（FR-114/115；取证结论在案：官方矩阵 Non-commercial ❌/Pro ✅——建议维持 pro+〔T-358 §1-6〕，Q4 终裁确认；kind 已定 KindFeature） |

- 三态叠加规则、`addons.disabled` 熔断、license 覆盖表语义全部沿 M10 §5.2 不变；webhook 槽自动获得门控三缝行为（FR-85 机制复用）。
- helmoci 槽（M12 转正，pro）承载 remote/virtual——不新增槽；repo-operations / trashcan 槽既有，旋钮（FR-118）不涉门控。

### 5.3 契约矩阵（11 条，LC-46~LC-56 接续 M12 编号）

| # | 端点/契约面 | Artifactory 对应 / 公开规范 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| LC-46 | Webhook 订阅 CRUD + test REST 族（含过滤器/事件类型闭集校验） | JFrog 官方 REST 文档（webhook 订阅族——端点逐条以 webhook.md 定案）；inv-4 I2：Artifactory 侧管理在 Access，BinFlow 单体平移（「无需模拟 Access 拆分」判定） | A | P0 | 复核后（webhook.md） | L01/L02 |
| LC-47 | 事件类型集（13 域 66 型）+ 触发源织入（本体 9 型）+ CloudEvents envelope（七字段） | 官方文档（事件清单/envelope——webhook.md 已锚）+ inv-4 I3（outbound dispatcher）/I5（worker-events 40+ 类型清单互证）；BinFlow 触发源覆盖界 = K48/Q6 | A | P0 | 高（T-358 交付） | L03/L04 |
| LC-48 | 投递语义（outbox 持久/固定间隔重试〔10s×4、4xx 不重试〕/死信 additive/secret 签名/代理与超时〔30s〕） | 官方文档（投递行为——webhook.md §5 已锚）+ inv-4 K3 outbox 模式（BinFlow 载体自定——SQLite/PG 两方言） | A | P0 | 高（T-358 交付） | L05~L08 |
| LC-49 | HelmOCI remote pull-through + virtual 聚合（manifest/blob 回源 + 缓存 + Bearer 认证链 + tag 并集/by-digest 路由） | Artifactory HelmOCI remote/virtual（helm.md）+ OCI Distribution 规范（/v2 面代理链——M3 Q4 缓议翻转点经 helmoci 先航） | A | P0/P1 | 高（复用 M12 HL-3 机制 + 既有 remote 引擎） | L10/L11 |
| LC-50 | remote 仓 `charts_base_url` 分体基址（回源 = chartsBaseUrl + 相对路径，缺省回退仓 URL） | artifactory.xsd 真字段 + helm.md §5 回源链/S8/S10（高置信） | A | P1 | 高 | L12 |
| LC-51 | `_external`/`_transitive` 外部依赖落盘缓存（absolute-URL 引擎缝；local → 400 维持） | helm.md §2/§3 端点表（高置信）+ T-342 评估结论 | A | P1 | 高 | L13 |
| LC-52 | folderDownloadConfig 六字段旋钮（enabled 默认 false/匿名开关/大小/文件数/并发/空目录；关态 403 文案逐字） | repo-operations.md §2.1（config 描述符 + 模板双证，高置信） | A | P1 | 高 | L14 |
| LC-53 | trashcan.retention_days 旋钮（默认 14；cron 消费） | config.xml trashcanConfig（M12 mini 规格锚点；BinFlow 键名 K52） | A | P1 | 高 | L15 |
| LC-54 | conan v1 `DELETE conans/<ref>` 坐标根整树删（全部修订）——**as-built latest 链翻转** | conan.md §3.2 D8（T-348 双证：`LocalConanHandler.removeRecipe` + conan 1.66 参考实现；v2 同型互证 `getRecipePathForRemove`） | A | P1 | 高 | L16 |
| LC-55 | conan v1 files 通道布局 `<root>/<pid>/<pRev>/<file>` + 存量迁移（现态双拼路径——服务端布局对齐，客户端面无感） | conan.md §4 规格 + T-340 §4 D-F2 登记 | A | P1 | 高 | L17 |
| LC-56 | NuGet publish 同字节幂等臂（D-10） | nuget.md §5.1 臂②（包已存在且无 d 权限 → **409**）vs BinFlow as-built（同字节 + 仅 w 主体重传 → **201**，T-356 L03 实测） | **待裁**（Q3——终裁后归 A〔翻转对齐 409〕或 D〔有意差异留痕〕并回写） | P2 | 高（分歧双证在案） | L19（as-built 维持断言 + 裁定留痕） |

> 计数：**11 条 = A 10（LC-46~LC-55）+ C 0 + D 0 + 待裁 1（LC-56）**。deb Packages.bz2 维持 M12 LC-45 D 层留痕不重复立行（Q7 推翻通道）。既有契约面（五基础包型、go/nuget/cargo、conan/deb/rpm/helm、配置域、操作族/回收站、MPU 新 wire）M13 对 M12 as-built 零行为变化（§5.4），断言反转两处 + 服务端布局对齐一处均经 PRD 回写。webhook outbox 表结构为内部载体（非契约面），不入矩阵。

### 5.4 回归基线（M13 断言反转两处 + 布局对齐一处——均经 PRD 回写；其余零回归）

| 既有断言 | M13 期望 |
|---|---|
| M1~M12 全部 P0 序列（双形态：无 license 默认 + pro） | 零回归（webhook 事件织入为 outbox 旁路主路径零变化；HelmOCI remote 只增不改既有 /v2 面） |
| M12 conan v1 DELETE「latest 修订链」as-built（T-348 登记分歧） | **反转**：FR-119.1 落地后坐标根整树删（断言翻转 100% 归属 FR-119 豁免票，PRD 回写） |
| M12 folderDownload「恒关 403」（T-356 L14 如实登记） | **开关化**：旋钮开启后 200（关态文案逐字维持；断言升级归属 FR-118 豁免票） |
| M12 conan v1 files 通道双拼布局（T-340 D-F2） | **布局对齐**：迁移后落规格布局（roundtrip 对称故客户端断言不变；服务端树断言翻转归属 FR-119） |
| trash-can.md「恒 enabled/14d」已知边界表 + api-reference「旋钮未落」自注 | 翻转为旋钮化在档（FR-118 文档面） |
| console-m8「Trash Can 常驻节点不建」条款 | **推翻回写**：先改册后实现（FR-122.1 规格先行纪律） |
| `make test`（race）两包环境性 FAIL（隔离绿辩护姿态，T-356 DoD#6） | **转绿**：FR-121 落地后全树一次绿（escape 点显式标注可审计） |
| binstore 三链 / M6 迁移 H 序列 / M12 fail-open 全链 | 零回归（FR-120.3 仅 PRD 加注，行为与 ADR-0040 零修改） |
| console-ux 锚册 / anchor ledger / MUI 四闸门 | 零改动熔断线延续（FR-115.5/122.1 新 FE 面走新锚入册流程） |

### 5.5 M13 核心验收命令（L 序列骨架，QA 直接引用）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-114 Webhook 订阅与事件 ==========
# L01 规格票走查：webhook.md（官方文档出处逐条 + 13 域 66 型清单 + 覆盖界标注）+ tl 就绪确认
# L02 订阅 CRUD + test：curl 全链（create→get→list→update→delete；test 真发一条到接收器，信封断言）
# L03 事件触发：generic PUT → artifact deployed 事件（envelope 逐字段）；DELETE/copy/move/属性 PUT 各臂；
#    docker push → docker 域事件（dind 腿）；事件类型闭集外 → 400
# L04 过滤器：repo/path 过滤命中发/未命中不发（接收器计数双臂）
# L05 权限与门控：非授权主体 CRUD 403；webhook 槽三缝（community 403+License-Required 头→pro 200→卸载降级不入箱）

# ========== FR-115 投递引擎 ==========
# L06 outbox 幸存：触发事件 → kill -9 → 重启 → 补投零丢；主路径 PUT 全程零 5xx 零阻塞
# L07 重试/死信：接收器 500×N → 固定间隔 10s 重试序列 → 恢复成功；4xx → 不重试负面臂；持续失败 → 死信可查可重放 + 告警日志
# L08 签名与真实消费者：secret → 接收器 HMAC 校验绿（篡改红）；dogfood Jenkins 条件腿或容器接收器腿全绿
# L09 SSRF 负面：私网订阅 URL 拒绝形态照 ADR-0041（默认策略 + 开关双臂）
# （FE 面）控制面 Playwright：订阅 CRUD + 投递记录 + readonly 臂；四闸门维持

# ========== FR-116 HelmOCI remote/virtual ==========
# L10 remote 全链：自指上游 push → 经 helmoci-remote helm pull（字节/digest 一致）→ 二次命中缓存
#    （上游计数不增）→ helm install 真集群腿；上游 Bearer 认证腿
# L11 virtual 与边界：local+remote 成员 pull 双域；混仓 400 维持；停上游已缓存可拉/未缓存零 5xx；
#    docker dind /v2 全量回归 + M12 helmoci local 序列；helmoci 槽三缝

# ========== FR-117 chartsBaseUrl/_external ==========
# L12 chartsBaseUrl：异构基址上游 → charts_base_url 回源正确 + 字节一致；REST 回显；缺省回退臂
# L13 _external：虚仓外部域依赖 → URL 改写 → 首次 GET 200+落盘可寻址 → 二次命中本地（上游不回源）；
#    local 仓 _external → 400 维持；helm 经典仓三态回归

# ========== FR-118 旋钮 ==========
# L14 folderDownload：默认关 403 文案逐字 → 开启 200 + 解包 sha256 对账（断言升级，归属 FR-118）；
#    匿名关 401 臂 + 超限拒绝臂
# L15 retention：retention_days=1 → 过期清理+审计；期内不清理；默认 14 回归（M12 cron 臂）

# ========== FR-119 conan ==========
# L16 D8：多修订包 → v1 DELETE 坐标 → 全修订树删（GET 404×2）；conan 1.66 remove 腿 + 2.x 回归
#    （M12「latest 链」断言反转，归属 FR-119）
# L17 D-F2：存量双拼树夹具 → 迁移 → 规格布局 + sha256 对账零损 + 幂等；settings 恢复（-q 过滤腿命中）；
#    conan v1/v2 全链回归

# ========== FR-120 文面裁定包 ==========
# L18 落笔核查：flat 措辞（M12 PRD v1.1 + artifact-operations.md grep 零残留）+ fail-open AC2 加注
#    （ADR-0040 零修改、行为零变化断言维持）
# L19 D-10：as-built 维持断言（同字节+w 主体 → 201）+ BOARD 裁决留痕；若翻转则小票验证绿（409 逐字）

# ========== FR-121/FR-121 de-flake 与运维 ==========
# L20 race 全树：make test（race）一次绿连续两轮（不依赖隔离复跑）；escape 点注释清单可审计
# L21 CI 三连：e2e 专用 job 三连绿 + nightly 对账；运维尾巴：树常驻节点 Playwright（可见/深链/readonly）+
#    console-m8 回写落笔 + 侧栏清单 15 对齐 + npm 尾斜杠注记 + make docs SUCCESS

# ========== 回归与收口 ==========
# L22 全量回归：M1~M12 全 P0 双形态复跑 + 契约变更面（git diff m12-done..HEAD -- internal/ cmd/）100% 归属
#    M13 豁免票 + 断言反转两处/布局对齐一处 PRD 回写核实
# L23 NFR：100 并发发布→事件入箱零丢+投递 P95；HelmOCI remote 首拉 P95 对齐 remote 口径；资源门维持
#    （footprint ≤100MB/check-size ≤100MiB/冷启动 <2s——webhook 引擎不得破 M12 转绿门）
# L24 文档：tech-writer 增量（webhook 指令/HelmOCI remote 接入/旋钮说明/收官清扫）客户端命令实测可复跑
```

### 5.6 待校准项（webhook.md / ADR-0041 / ADR-0042 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K47 | webhook 订阅 REST wire（端点路径/方法/请求响应体/错误码——官方文档逐端点） | **已回填（T-358）**：`/event/api/v1/subscriptions*` 七端点——list/create(201)/get/update(204，key 不可改)/delete(204)/test（吃完整订阅体，非 key 引用）/troubleshooting；key 正则 `^[A-Za-z][A-Za-z0-9_\-]+$`、handlers 恰 1、enabled 默认 false、custom 型五方法 + Go 模板 `{{.path}}` | webhook.md §1–§2 |
| K48 | 事件清单 + BinFlow 触发源覆盖界（无本体域处置：注册休眠 vs 裁剪） | **已回填（T-358）**：13 域 66 型全表 + 本体 9 型/休眠 57 型覆盖界逐条标注；暂行注册休眠维持——裁剪翻转面小（ADR-0041 决策 7 翻转路径在案） | webhook.md + Q6 终裁 |
| K49 | 签名形态（secret → HMAC 算法/头名）+ envelope 字段集 + 过滤器语法 | **已回填（T-358）**：头名 `X-JFrog-Event-Auth` + HMAC-SHA256（官方 openssl 验签命令逐字）；`use_secret_for_signing` 双态（签名态头承载中置信待活体）；envelope 七字段（含 userContext.isToken） | webhook.md §6 |
| K50 | outbox 载体与投递参数（表结构两方言/重试策略/重试上限/死信阈值/代理与超时/SSRF 键名） | ADR-0041 Accepted（决策 3/4/6：双方言两表/Dispatcher/`webhook.allow_private_target` 默认 false）——**决策 4 投递参数与 webhook.md §5 官方值冲突**（10 次/2s 指数/10s 超时/4xx 计入可重试 vs 5 次/固定 10s/30s 超时/4xx 不重试）：按效力序 webhook.md > ADR，待 architect 锚点回填修订（T-370 BOARD 登记）；死信 additive 维持 | ADR-0041 + webhook.md §5 |
| K51 | helmoci remote 上游协议界（OCI Distribution 规范面/Bearer token 交换/manifest 校验策略） | 复用既有 remote 引擎 + /v2 面 | helm.md 增量段（随 FR-116 票） |
| K52 | 旋钮键名族（folderDownloadConfig 六字段与 trashcan.retention_days 的 BinFlow config.yaml 映射） | 暂行 `trashcan.retention_days` + folderDownload 族照 repo-operations.md §2.1 字段名 | FR-118 票内定案 |
| K53 | D-F2 存量迁移方案（启动期 vs 惰性/回滚/幂等策略） | 启动期迁移暂行（幂等零损为硬约束） | ADR-0042 |
| K54 | docker remote 顺车成本判定（/v2 共享缝边际成本） | 暂行不顺车（滚 M14+） | FR-116 规格票 → Q5 |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| PRODUCT.md「明确不做」 | HA/Xray 本体是否进 M13 | 不进（Q1——前置 = PRODUCT.md 修订解禁，用户动作；解禁后专程另出 PRD） | 用户决策（Q1） |
| ADR-0001（clean-room） | webhook 域反编译集合缺失的取证路径 | 官方文档为唯一行为基准（full-feature-matrix 既定）+ inv-4 锚点补白；envelope 为数据契约非代码复制 | §1.4 条款 1/4 |
| ADR-0040（fail-open）vs M12 PRD AC2「停机窗内重启」措辞 | as-built boot 探针 fail-closed 姿势解释空间（T-356 观察⑨） | **加注不改行为、ADR 零修改**（FR-120.3） | PM 文面（FR-120） |
| M12 PRD flat 措辞 vs repo-operations.md §1.6 | 文面债（T-356 L12） | M12 PRD v1.1 增订 + 用户文档同步（FR-120.2） | PM 文面（FR-120） |
| console-m8「Trash Can 常驻节点不建」 | 被 M12 FR-106 推翻 | **先改册后实现**（规格先行纪律，FR-122.1） | FE 票内含规格回写 |
| nuget.md §5.1 臂② vs as-built 201（D-10） | 规格-实现分歧**对照材料已上 BOARD（T-370，2026-08-30）**——PM 建议对齐 409，终裁待用户 | 终裁前维持 as-built；终裁后联动（翻转小票 T-378 或 nuget.md 差异行） | 用户决策（Q3） |
| ADR-0032/0033（license 门控/addon 注册表） | webhook 第 19 槽 | Q4 终裁（暂行 pro+；取证结论建议维持——T-358 §1-6；kind 已定 KindFeature） | 视需要随 ADR-0041 批次补注 |

### 6.2 性能（M13 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P58 webhook 投递吞吐与时延 | 100 并发制品发布 → 事件入箱零丢 + 投递 P95 有界（本地接收器 ≤2s）；**投递全链不阻塞主路径**（发布路径 P95 对齐 M12 基线，偏差 <10%） | P0 |
| NFR-P59 HelmOCI remote 回源 | 首拉 P95 对齐既有 remote 口径（maven/npm 家族）；二次命中本地（P95 对齐本地仓）；万 blob 缓存树零 5xx | P0 |
| NFR-P60 资源门维持 | 空载 RSS ≤100MB（`make footprint` 维持绿——webhook 引擎不得破 M12 转绿门）；check-size ≤100MiB；冷启动 <2s 三连 | P0 |

### 6.3 安全底线（M13 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S65 webhook SSRF 面 | 订阅 URL 走 M3 Guard 家族（DNS rebinding pinning 五参数）+ 私网目标开关（replication 同款语义）；私网默认策略照 ADR-0041 | L09 |
| NFR-S66 签名与凭据 | secret 存储 AES-GCM 链维持（不落明文、日志脱敏）；投递日志/死信面凭据零泄露（grep 审计） | L06~L08 |
| NFR-S67 迁移与数据面 | D-F2 迁移 sha256 对账零损 + 幂等 + 回滚路径（ADR-0042）；folderDownload 匿名默认关维持；chartsBaseUrl/_external 回源零新 SSRF 面（复用 Guard） | L13/L14/L17 |

### 6.4 可观测性（M13 增量）

- 新审计事件：`webhook.subscription.{create,update,delete,test}`（actor/订阅标识/事件类型集/URL 脱敏）、`webhook.dead_letter`（订阅标识/原因/规模）。
- /metrics：`binflow_webhook_{deliveries_total, retries_total, dead_letter_total, queue_depth}`（gauge+counter 族，沿用 remote family 口径）；`binflow_addon_gate_requests_total{addon,decision}` webhook 槽标签自动生效。
- 启动日志：outbox 水位一行（重启幸存条目数——fail-open 队列同款形态）。
- 既有指标族与口径冻结维持。

---

## 7. 开放问题（Q1~Q7，均带暂行；需用户/conductor 决策，PM 不代拍）

| # | 问题 | 影响面 | 暂行口径（v1.0） |
|---|---|---|---|
| Q1 | **HA 本体排期**（承接 M12 Q1 终裁「M12+ 单列专程」） | 架构级大件；PRODUCT.md Non-goal | **前置 = 用户修订 PRODUCT.md「明确不做」段（解禁 HA）**——截稿未发生，M13 不排任何 HA 工作项；解禁后 PM 建议专程里程碑（心跳/推举/传播 ADR 群）。Xray 集成面本体同族处置 |
| Q2 | **NuGet symbol server 余量条件票**（M12 Q2 未触发承接） | NuGet 域增量面（.pdb/GUID 路径） | 全部 P0/P1 收官且余量足 → 触发（mini as-built 规格随票，T-293 终裁口径）；未触发 M14+，BOARD 留痕非 DoD 缺口 |
| Q3 | **D-10 同字节幂等终裁**：nuget.md §5.1 臂②（同字节无 d 权限 → 409）vs BinFlow as-built 201 | nuget publish 语义；规格-实现一致性 | 终裁前维持 as-built 201（LC-56 待裁行）；PM 已备对照材料上 BOARD——方向：对齐 409（翻转小票 slot）或有意差异 D 层留痕（nuget.md 差异行） |
| Q4 | **webhook 槽档位归属**（community/pro/enterprise + kind） | 门控矩阵第 19 槽 | 暂行 pro+ 解锁；**取证结论在案（T-358 §1-6）**：官方功能矩阵 Webhooks 行 Non-commercial ❌/Pro ✅/Enterprise ✅——**建议维持 pro+ 不翻转**，待终裁确认；kind 已由 ADR-0041 决策 8 定 KindFeature（feature-int 为注记性写法） |
| Q5 | **docker（非 helmoci）remote 本体顺车**：FR-116 建成 /v2 remote 数据链后 docker remote 边际成本 | M13 容量 vs docker 三态齐装时机 | 暂行不顺车（滚 M14+）；FR-116 规格票判定共享缝边际成本≈0 则条件票承接（K54，BOARD 留痕） |
| Q6 | **66 型中无本体域 57 型处置**（build/releaseBundle/distribution/curation/xray/app_trust 族 BinFlow 无触发源） | 事件类型闭集与订阅校验面 | 暂行「类型注册休眠」——webhook.md 已按此交付全表（本体 9 型/休眠 57 型，覆盖界逐条标注）；裁剪方案（仅注册有源域）待用户裁定——翻转面小（ADR-0041 决策 7 翻转路径在案） |
| Q7 | **deb Packages.bz2 维持**（承接 M12 Q7/LC-45） | debian 索引压缩集完备度 | 维持不做（dsnet 依赖不可得实证 T-314；plain+gz+xz/lzma 在场 apt 零损）；用户推翻通道：提供实现路径（如纯 Go bzip2 写入器）则翻转 |

---

## 8. M13 验收剧本（QA 总纲）

1. **回归基线（硬门槛先行）**：M1~M12 全部 P0 序列双形态（无 license + pro）复跑全绿；契约变更面（`git diff m12-done..HEAD -- internal/ cmd/`）100% 归属 M13 豁免票。
2. **Webhook 域**：L01（规格票走查）→ L02（订阅 CRUD + test）→ L03/L04（事件触发 + 过滤器）→ L05（权限/门控三缝）→ L06~L09（outbox 幸存 → 重试/死信 → 签名/真实消费者 → SSRF）→ 控制面 Playwright 腿。
3. **HelmOCI 三态**：L10（remote 全链 + 认证 + 缓存命中 + 真集群 install）→ L11（virtual 聚合 + 降级 + /v2 全量回归 + 门控）。
4. **remote 配置缝**：L12（chartsBaseUrl）→ L13（`_external` 落盘 + local 400 维持 + helm 三态回归）。
5. **旋钮与 conan**：L14/L15（folderDownload 开关化 + retention 短周期）→ L16（D8 整树删 + 双客户端）→ L17（D-F2 迁移零损幂等 + settings 恢复）。
6. **文面与测试债**：L18（flat/L31 落笔核查）→ L19（D-10 as-built 断言 + 裁决留痕）→ L20（race 全树一次绿×2 + escape 可审计）→ L21（CI 三连 + 运维尾巴三件）。
7. **NFR 与收口**：L22（全量回归 + 归属审计 + 断言反转回写核实）→ L23（并发/资源门维持）→ L24（文档实测复跑）。
8. **文档**：tech-writer 增量——Webhook 使用指南（订阅/过滤/签名/重试语义/SSRF 边界 + 接收端示例）、HelmOCI remote/virtual 接入、旋钮说明（folderDownload/retention——trash-can.md 自注翻转）、api-reference 增量（webhook 端点族 + 断言反转处）、FAQ 增补（事件丢失排查/死信重放/remote 缓存命中）。
9. **release**：部署烟测 + **UAT 随里程碑 PR 首跑**（M12 T-355 未执行教训——T-356 §6 留痕，本程必做项）。

---

## 9. M13 DoD

1. §4 全部 P0 AC（FR-114/115/116 remote 段）经 qa 验证全绿；P1（FR-116 virtual 段/117/118/119/121/122 + FR-115 FE 面）全绿；P2（FR-120 落笔段/122.2/122.3）全绿；条件票（NuGet symbol server Q2 / docker remote 顺车 Q5 / D-10 翻转 Q3 / deb bz2 Q7）按余量条款——未触发不构成 DoD 缺口，须 BOARD 留痕；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（订阅面/事件触发/投递可靠性/真实消费者/HelmOCI remote/virtual/chartsBaseUrl/_external/旋钮两枚/conan 翻转与迁移/文面裁定/测试债/运维尾巴——全部有实测数字归档）；
3. 回归硬门槛：M1~M12 全部 P0 序列双形态复跑全绿；M12 as-built 零行为变化（豁免票归属外）；契约变更面 100% 归属 M13 豁免票；**断言反转两处**（conan D8 latest 链→整树删 / folderDownload 恒关→旋钮化〔关态文案逐字维持〕）+ **布局对齐一处**（D-F2 双拼→规格布局）经 PRD 回写核实；
4. 前置产物齐备：webhook.md（官方文档出处逐条 + 13 域 66 型 + 覆盖界——已交付 T-358）+ ADR-0041 Accepted（含 K47~K50 校准；**决策 4 投递参数对 webhook.md §5 的锚点回填修订完成**）；视 Q 裁定 ADR-0042（K53）；Q3/Q4/Q6 终裁归位（D-10 归 A 或 D / webhook 槽档位 / 事件覆盖界）；
5. tech-writer 增量文档交付（§8-8）；新增能力的客户端命令全部实测可复跑；
6. NFR-P58~P60 达标归档；`make test`（race）全树一次绿连续两轮（FR-121——不再接受隔离复跑辩护）/ `make lint` 0 issues / gofmt 空维持 / 默认并发全量 e2e 绿；`make footprint` 维持绿 + check-size ≤100MiB（webhook 引擎不得破 M12 转绿门）；
7. §2.2 Non-goals 与主轴滚程对账完成：滚入 M14+ 项在 ROADMAP 登记（「M13 未纳入项」于收口时建段）；§1.4 出处义务在全部 M13 规格票可审计（webhook.md 官方文档锚点逐条）；
8. 主会话 git tag `m13-done`（对外发布任何制品先经用户确认；PR 化合并沿既定 gitflow 程序；UAT 首跑证据随里程碑 PR 归档）。

