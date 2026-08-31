# PRD — M14 UI-parity 里程碑：前端交互体验与 Artifactory 完全对齐 + 协议 logo + 自设计品牌 logo + docker remote 首航

> **PRD 状态：v1.1 执行期回写版（2026-08-31，T-395 PM Q 终裁联动回写；v1.0 已 conductor 审定——Q1~Q7 暂行维持、票区间 21~26 确认）**。主轴定音：**用户指令 2026-08-30 三指令**（① 前端交互体验与 JFrog Artifactory **完全一致**——弹窗〔New Repository 向导等〕、抽屉〔Set Me Up/详情 slide-out〕等形态逐一对齐；② 各协议 logo 加上；③ BinFlow 产品 logo 自设计——国际化、偏技术、好看）。**M14 = BinFlow 首个纯前端形态专程里程碑**（副线收编 docker remote 首航与服务端小票包，FE/BE area 天然错峰）。范围基线：`docs/design/console-artifactory-parity.md` §7 页面×模式差距矩阵（17 行 × 8 列，PM 直接引用为骨架）+ §8 活体核验清单 V1~V8 + §9 豁免登记 E1~E7（**常设条款，对齐评审不判差距**）+ §10 落地批次；UX-1 品牌资产两交付（logo 三候选 / 图标 30 枚）+ ROADMAP「M13 未纳入项」候选池（13 项票级候选 PM 收编判定，§2.2 留痕）。**排期让位留痕**：M13 PRD 曾列「AQL + 13 老搜索 = M14 专程候选第一顺位」——被 2026-08-30 用户指令的主轴定音让位，**AQL 滚 M15 专程候选第一顺位**（§2.2）。

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-14.md` |
| 里程碑 | M14 — UI-parity（Artifactory 交互形态逐一对齐——**v1.1 改判后口径**：D1 Set Me Up 抽屉化〔落地〕/ M1·M3 形态断言收口〔决策项 A/B 撤销〕/ L2 有意偏离登记〔票面撤销〕/ L1 工具栏 / Tokens 页真身 + 品牌 logo 转正与六用例 + 包型图标 30 枚接线 + 活体核验 V1~V8 置信度补锚 + docker remote 首航与服务端小票包 + replication 交互对齐增补〔FR-131〕） |
| 状态 | v1.1 执行期回写版（FR-123~FR-131 九条需求〔FR-131 replication 增补——用户指令 2026-08-31〕；契约矩阵 11 条〔A 7 / C 2 / D 1 / 待裁 1——LC-57~LC-67，v1.1：LC-60 归 D·LC-66 材料齐备·LC-67 增补〕+ 档位矩阵增量 0 行〔19 槽维持〕；L01~L19；**Q1~Q7 逐项归位**〔终裁/执行归位 4：Q1/Q2/Q5/Q7 · 撤销归位 2：Q3-A/B · 维持暂行 3 臂：Q3-C/Q4/Q6（Q6 材料齐备待终裁）——§7〕；三改判回写〔FR-124.2/3/4〕+ K55~K61 回填〔§5.6〕；E1~E7 豁免常设条款） |
| 上游依据 | PRODUCT.md（Non-goals 不越界：洞察报表/Xray/Builds·Federation·Lifecactories 管理面永不建——parity 规格 §9 已对齐过滤；Web 控制台为核心能力第 5 条）、用户指令 2026-08-30（M14 主轴三指令——BOARD「用户指令 intake」节在档）、ROADMAP「M13 未纳入项」（范围基线：主轴第一顺位 + 实现类候选 13 项 + 编排注记 2 项）、docs/design/console-artifactory-parity.md（UX-1 交付 3——§1 置信度标尺与差距三档 / §2~§6 模式 catalog N·M·D·L·F / §7 差距矩阵 / §8 V1~V8 / §9 不做清单+豁免 E1~E7 / §10 落地批次）、docs/design/brand/logo/README.md（UX-1 交付 1——三候选对照 + 推荐候选 1「容器·双箭流」+ §2 几何/色彩规格 + §3 wordmark 字体注记 + §4 落地清单六用例 + §5 许可）、docs/design/brand/package-icons/README.md（UX-1 交付 2——15 型 × mono/brand 双版 + §3 helm×helmoci 三重区分 + §4 逐枚持有方/置信度 + §5 许可姿态 + §6 FE 接线注意）、reports/agents/UX-1.md（遗留三项：推荐稿待圈定 / wordmark·npm·go 转 path / V1~V8 未执行）、docs/design/console-ux.md v1.15（IA/四态/token 母册——锚册纪律）+ docs/design/mui-native-visual.md（MUI 原生视觉基线）、docs/prd/milestone-8.md（体例先例：UI 对齐矩阵 24 条〔对齐 8 / 形态不同 3 / 子集 8 / 有意差异 5〕+ ADR-0029 clean-room 对 UI 生效口径 + 四闸门）、docs/prd/milestone-13.md（体例 + LC-56 止编号续接 + K54/Q5 承接）、BOARD M13 收口节（T-377 PASS 总账 + M14 候选池汇总 + T-380 K54 判定「/v2 共享缝边际成本≈0」成立）、reports/agents/T-363.md §Q5（K54 判定）、T-364.md §5（Replay/disable/3xx 登记）、T-366.md §4-2（outbox 行级面）、T-367.md（成员同型）、T-374.md（L1 npm login / L2 hover 对比度 / L3 playwright 前提）、T-376.md（PVC keep / 启动日志）、T-377.md（D1 busy / D2 dind 注记）、T-378.md（v3-flat 403-vs-409）、ADR-0001（clean-room——UI 零复制，M8 口径延续）/ ADR-0029（M8 UI 对齐边界）/ ADR-0032/0033（license 门控——图标门控态纪律消费） |
| 下游消费者 | tech-lead（拆票——票号 T-381 起；宽度 ≤2 内建，§1.3 分票提示）、ux-designer（活体核验协作 + F2 插画线稿派生〔候选 1 隐喻线稿〕+ 核验回写共笔）、qa-engineer（活体核验执行腿〔有浏览器〕+ L01~L18 + 差距矩阵逐格复评 + axe 双主题维持 + 回归双形态）、dev-frontend（主轴全部 FE 票：批 1 形态对齐 / 批 2 补缺 / 品牌 logo 接线 / 图标接线 / FE 债）、dev-registry-adapter（docker remote 首航——helmoci 缝复用）、dev-go-core（npm legacy login 小票）、reverse-engineer（v3-flat 规格补锚 + npm login 端点形态实证整理）、tech-writer（console 用户文档 parity 化 + 品牌注记 + docker remote 接入 + npm login 注记）、release-engineer（烟测 + UAT 随里程碑 PR + helm PVC keep）、conductor（Q1~Q7 裁决窗 + 决策项 A/B/C 终裁 + tag m14-done） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-31 | 初版草案（待 conductor 审）：M14 范围（主轴 UI-parity 批 1/批 2 + 品牌资产接线 + 活体核验前置 + 副线 docker remote 首航与服务端小票包 + FE 债收编）、FR-123~FR-130、契约矩阵 LC-57~LC-66（A 7 / C 2 / 待裁 1）、档位矩阵增量 0 行、L01~L18、开放问题 Q1~Q7 带暂行、E1~E7 豁免常设条款（§1.4/§2.2）；随稿完成 ROADMAP M14 立项行 + 当前里程碑头切换（PM 职责内两处，沿 M13 v1.0 先例） |
| v1.1 | 2026-08-31 | **T-395 执行期 Q 终裁联动回写**（依据：T-381 活体核验 V1~V8 + 三改判 / T-383·T-384 改判收口 / T-385 票面撤销〔conductor 改判〕/ T-389+K56 / T-390 / T-392 / T-393·T-394）：① §7 Q1~Q7 逐项归位（终裁/执行归位 4 + 撤销归位 2 + 维持暂行 3 臂——各带依据锚，PM 出材料不代拍）；② FR-124.2/124.3 转断言收口口径、FR-124.4 改判 E 类有意偏离登记（票面撤销——与 BOARD 改判登记零矛盾核对）；③ §5.6 K55~K61 全量回填；④ 契约矩阵 LC-57~LC-67 修订（LC-60 归 D / LC-66 材料齐备 / 新增 LC-67 replication 增补——用户指令 2026-08-31，§4.9 FR-131）；⑤ 文面修正：FR-130.1/130.2 优先级统一 P1（M14-SPLIT §6-1）+ owner 口径回填（§6-2）+ §6-3/4/5/6 口径注；⑥ ROADMAP「M14 未纳入项」备稿段落笔（T-395 票面 AC2——启用归 conductor 收口窗） |

---

## 1. 背景与目标

### 1.1 背景

M13 以 `m13-done`（2026-08-31，T-377 终验 PASS——DoD 八条达标、L01~L24 全绿、实测数字归档）收官：Webhook 统一事件总线（13 域 66 型 + outbox 投递）、HelmOCI 三态齐装、chartsBaseUrl/_external、旋钮两枚、conan 翻转与迁移、D-10 终裁兑现（T-378）。控制台侧 M8~M13 已完成「信息架构 + 交互逻辑对齐 + MUI 原生视觉」两层；用户 2026-08-30 三指令把**第三层——交互形态（弹窗/抽屉的品种与手势）**定为 M14 主轴。M14 面对四股输入的汇合：

1. **主轴输入（用户指令 2026-08-30，BOARD intake 在档）**：① 交互体验与 Artifactory **完全一致**——「完全一致」的操作化定义 = parity 规格 §1.1「**交互形态对齐**：弹窗/抽屉的品种、出现位置、尺寸档、关闭方式、按钮位、步骤结构」——迁移用户（Artifactory 老手）在 BinFlow 里不用重新学操作手势；**不是**像素复刻、**不是**功能对齐（BinFlow 没有 Builds/Federation/Lifecycles，永不建 Xray——parity §9 Non-goal 防线）。② 各协议 logo（13 包型 + 2 addon 槽，UX-1 已交付 30 枚待接线）。③ 自设计产品 logo（UX-1 已交付三候选，推荐候选 1「容器·双箭流」）。UX-1 三交付已把规格侧备齐：**§7 差距矩阵 = 本 PRD 的骨架**（最大差距 = D1 抽屉族〔Set Me Up 居中 Dialog → 右抽屉〕与 M1 建仓单 modal；次级 = M3 用户/组创建 modal + Tokens 页真身、L2 行内 ⋮；低优先 = L1 列选/刷新、N2 侧栏图标、F2 空态插画位）。**v1.1 注**：V1~V8 活体核验（T-381，parity 规格 v1.0→v1.1）证伪其中 M1 单 modal / M3 创建 modal / L2 行内 ⋮ 三项记忆——Artifactory 7.84 实为「两段式建仓 / 整页创建表单 / 行尾无 ⋮」，§4.2 三改判收口；**批 1 形态改造实做 = D1 抽屉化一票**，其余转断言收口（T-383/T-384 零形态改动）与有意偏离登记（T-385 票面撤销）。
2. **活体核验欠账（UX-1 置信度纪律的自然延伸）**：parity 规格的 Artifactory 行为描述基于作者产品知识（clean-room：无反编译 UI 代码消费），**中/低置信项一律列入 §8 V1~V8、核验前不得作为验收断言**——M14 必须先补「置信度锚」（活体核验源三选，Q1），把 V1~V8 回写规格，批 1 断言才有坚实基线。这不是可选项：**低置信不进断言是本里程碑的宪章级纪律**（§1.4 条款 2）。
3. **候选池收编（ROADMAP「M13 未纳入项」，PM 排期判定 §2.2 留痕）**：FE 类候选（L2 hover 对比度 4.41:1 / playwright 纯净实例前提注记）自然入主轴批次；非 FE 类按「条件已满足度 × 票量 × 与主轴 area 冲突度」三判据裁定——**docker remote 首航（T-380）条件已满足**（T-363 §Q5 判定「/v2 共享缝边际成本≈0」成立，M13 收口波满宽未派发）且 dev-registry-adapter 与 FE 主轴零冲突 → **入 M14 副线 P1**；npm legacy login / helm PVC keep / 启动日志措辞 / v3-flat 补锚四个小票 → 收编为服务端小票包 FR-130；Replay+outbox 行级 REST / D1 busy 重试预算 / 成员同型全包型推广 → **滚 M15+（理由 §2.2 逐条留痕）**。
4. **AQL 让位（排期留痕）**：M13 PRD §2.2 曾列「AQL + 13 老搜索 = M14 专程候选第一顺位」——2026-08-30 用户指令把 M14 主轴定音为 UI-parity，**AQL 顺延为 M15 专程候选第一顺位**（搜索基建——查询语言/执行引擎/分页，体量专程级不变）。

**不贪多**：M14 以「一条 FE 主线（批 1 形态对齐四项 P0 → 批 2 补缺 P1/P2 + 品牌资产两票）+ 一个前置锚（活体核验）+ 一条 BE 副线（docker remote 首航）+ 一个小票包」为形态；任何 Q 触发的增项（v3-flat 翻转 / N2 图标槽 / symbol server 余量四承）必须走条件票/余量条款。

### 1.2 M14 目标与量化门槛

> 一句话：让 Artifactory 老手在 BinFlow 控制台里**零学习成本**（Set Me Up 右滑抽屉、建仓两段式与实体创建整页表单同构——形态基线经 V1~V8 活体核验校准），让新用户**一眼识别**包型（30 枚协议 logo）与品牌（自设计 logo 六用例）；置信度欠账已补锚（V1~V8 回写 parity v1.1），docker remote 首航兑现三态齐装。

量化门槛（未达即里程碑不完成）：

| 指标 | M14 门槛 | 来源 |
|---|---|---|
| 形态对齐批 1（四项处置——v1.1 改判后口径） | D1 Set Me Up 右侧抽屉 **50vw 档**（smu-* 锚族零改名、Esc/backdrop/回焦链维持、Configure/Deploy/Resolve 三 Tab）/ M1 建仓两段式**断言收口**（决策项 A 撤销——V2 实测 BinFlow 现形态已对齐，零形态改动）/ M3 用户·组创建整页表单**断言收口**（决策项 B 撤销——V6 实测非 modal）/ L2 **有意偏离登记**（V4 撤旗——Artifactory 行尾无 ⋮，票面撤销）——落地/收口/登记三类 spec 与复核全绿 | FR-124 |
| 差距矩阵终评 | parity §7 矩阵（17 行 × 8 列）全部 △/✗ 格（豁免格除外）翻 **✅ 或（豁）**，终评覆盖率 100%（复核基线 = 活体核验回写后的矩阵）；逐格复评结论归档 | FR-123/124/125 + QA |
| 活体核验 | V1~V8 逐项回写 parity 规格（置信度列修订 = 改契约，须留痕）；核验源不可得的项如实维持「中/低置信 + 以核验为准」标注，**不得静默升格** | FR-123 |
| 品牌 logo | 候选 1 转正六用例接线：favicon.ico（16/32/48 三档，16px 两箭可辨）/ PWA 180+512 PNG / 登录页 lockup / 侧栏顶（app-nav-brand 结构不动）/ 文档站 navbar——五用例落地（GitHub 远期 P2）；wordmark 转 path（零字体依赖）；双主题各用例核对 | FR-126 |
| 包型图标 | 30 枚四消费点接线全量（pkg-grid / smu-grid / 列表·树·搜索类型列 / addon 矩阵）；npm/go 转 path 前置完成；门控包型 = mono + opacity 0.4 + 档位徽章（brand 版不置灰）；暗底抽查 ≥3:1 | FR-127 |
| a11y 维持 | axe 双主题 serious=0 维持；仓库表 hover 对比度 4.41:1 → **≥4.5:1**；键盘可达（Tab 循环/焦点陷阱/关闭回焦）在抽屉与 modal 形态下全量复测 | FR-124/128 |
| docker remote 首航 | 上游 push → 经 BinFlow docker-remote `docker pull` digest 一致；二次命中本地缓存（上游访问计数不增）；上游 Bearer 认证腿；停上游已缓存可拉 / 未缓存零 5xx | FR-129 |
| 小票包 | npm legacy login 真实客户端绿；helm uninstall PVC keep 验证（数据卷幸存）；v3-flat 403-vs-409 补锚归位（**v1.1**：补锚已落〔K59=409〕+ as-built 403 对照不一致——LC-66 离开「待裁」待 Q6 终裁，T-401 翻转 / D 层留痕两出口） | FR-130 |
| 资源与预算门 | SPA gzip 增量 ≤10KB（30 枚图标 + logo 资产计入）且预算门维持绿；footprint ≤100MB / check-size ≤100MiB / 冷启动 <2s 三连维持（docker remote 引擎不得破 M12 转绿门） | §6.2 |

> **v1.1 执行态注**（2026-08-31 23:3x 基线，M14 12/22 + T-402 增补）：活体核验 / 品牌 logo / 包型图标 / docker remote 四行**已达标归档**（T-381 / T-389+K56 / T-390 / T-392）；a11y 行 hover 对比度**已修**（T-391：亮 4.42→5.08 / 暗 5.55→6.12，不引入新 token——color-mix 配方）；小票包 npm login / PVC keep 注解面 / 启动日志**已落地**（T-393/T-394，UAT 腿归 T-399）；差距矩阵终评与 L1/L2 维持行归 T-400 终验。

### 1.3 上游依赖与并行关系（含分票提示）

- **前置产物（拆票前/B1 首波）**：① **活体核验票**（qa-engineer 执行腿〔有浏览器〕+ ux-designer 共笔回写——V1~V8 逐项核验并回写 parity 规格置信度列 + 产出「差距矩阵复核基线」；核验源三选归 Q1，t226 容器〔OSS 7.84.10，T-228 保留、docker start 可恢复——T-358 §4 在案〕为暂行首选；**降级路径**：核验源不可得 → 文档/视频佐证 + 置信度维持标注，不阻塞批 1 主体〔批 1 四项的手势主形态均为高/中高置信，可直接断言；被核验约束的是细节断言——宽度档/Tab 命名/动作集/字段集〕）；② **wordmark 转 path 定案**（K56——Inter Bold OFL path〔随附 license 文本〕vs 手工勾画，随 FR-126 票内定案）；③ npm legacy login 端点实证整理（T-77 O-4 在案，随 FR-130.1 票规格化，K60）。
- **依赖序**：FR-123（核验）→ FR-124 细节断言收口（主体可并行启动，**V1/V2/V4/V6 结论落定前对应细节断言挂「以核验为准」附注**——**v1.1：V1~V8 已全部落定〔T-381〕，附注清单已逐一对应收口**）；FR-126 logo 圈定窗（Q2）截止 = B2 前波——接线票以候选 1 为工作 logo 起步，用户未推翻即转正（**v1.1：Q2 已闭——候选 1 转正生效〔T-389〕**）；FR-127 dep npm/go 转 path（票内前置步骤——**已随 K56 插空票落**）；FR-129 dep K54 判定（已成立）复用 M13 helmoci remote 缝（T-363 as-built——**已兑现 T-392**）；FR-130.4 dep 补锚（reverse 票先行，翻转走条件票 Q6——**补锚已落 T-393，对照不一致→T-401 待 Q6 终裁**）；FR-128 零依赖任意波次（**已落 T-391**）。
- **实现分区与分票提示（宽度 ≤2 内建）**：FR-123 拆 1~2 票（核验执行 + 回写共笔可同票）；FR-124 拆 4 票（D1 抽屉化 / M1 向导单 Dialog / M3 用户·组创建 modal / L2 行内菜单——全部 `web/src`，**串行或两两错峰**，D1 与 M1 涉及 SetMeUpDialog.tsx 与 RepositoryFormPage.tsx 两个重载体文件不同文件可并行）；FR-125 拆 3 票（Tokens 页真身 / L1 列选刷新 / F2+N2 合并票〔均轻量〕）；FR-126 拆 1~2 票（六用例接线 + path 化同票，或 logo 与文档站分票）；FR-127 一票（30 枚搬运 + 转 path + 四消费点）；FR-128 一票（FE/docs 混合小票）；FR-129 拆 1~2 票（remote pull-through 本体 + dind 全链验收可同票）；FR-130 拆 2~3 票（npm login / chart+日志运维小票 / v3-flat 补锚〔reverse〕+条件翻转）；QA 两票（中期回归 + 终验——含差距矩阵逐格复评与 axe）；tech-writer 一~两票；release 一票（烟测 + UAT 随里程碑 PR）；PM 裁定票一票（Q 终裁联动回写）。估 **21~26 票**（含条件票 slot：NuGet symbol server 余量四承 / v3-flat 翻转 Q6 / N2 图标槽 V5 / D3 立项 Q7）。**v1.1 注**：实拆 22 票（M14-SPLIT）；执行期改判——M1/M3 票转断言收口形（T-383/T-384 零形态改动）、L2 票面撤销（T-385）；N2 已转实票（V5 不降级）、D3 已关闭（Q7 归位）；范围增补 T-402 replication 两段票（用户指令 2026-08-31，FR-131）。
- **area 错峰**：FE 主轴全部 `web/src`（票间串行/错峰由 tech-lead 排）；BE 副线 FR-129 `internal/adapter/docker`（M13 T-363 缝延续）、FR-130 `internal/adapter/npm` + `charts/` + `cmd/`——与 FE 零重叠，可全程并行。**站内先例**：M13 T-366 交付的 `web/src/pages/webhooks/SubscriptionDrawer.tsx` 已是右抽屉形态——FR-124.1 D1 抽屉化可直接复用其组件模式。**注意**：`web/src/lib/repos.ts`（PackageType 联合——helmoci 未入前端联合，UX-1 核对在案）可能需 FE 侧小改（图标 key 对齐），归 FR-127 票内。
- **QA 并行面**：活体核验需浏览器 + t226 容器编排；parity Playwright spec 置 `web/e2e/m14/`（沿 M8 `e2e/m8/` 先例）；docker remote 需 dind 夹具（M2/M13 既有）+ 自指上游（BinFlow docker local push）；PVC keep 需 kind/helm uninstall 编排；hover 对比度走 axe 自定义规则或色值断言（T-374 L2 形态复用）。

### 1.4 全程工作方式条款（M11~M13 §1.4 制度延续 + M14 UI-parity 专项——常设准绳，全程生效）

1. **形态对齐的操作化定义（本主轴的「行为逐项对齐」等价物）**：弹窗/抽屉的品种、出现位置、尺寸档、关闭方式、按钮位、步骤结构照 Artifactory（parity §1.1）——**不是**像素复刻（不逐像素量 JFrog 截图）、**不是**功能对齐（不因此建 Builds/Xray 面）；视觉皮肤维持 MUI 原生基线（memory：MUI native visual upgrade 常设）。
2. **低置信不进断言（UX-1 宪章延伸）**：Artifactory 行为中/低置信细节（宽度档/文案/分组名/动作集）核验前只作「以核验为准」附注；**核验回写 = 改契约**（parity 规格置信度列修订须留痕，不新增 Artifactory 功能面的「顺带发现」——走 PM Non-goal 过滤）。PRD 断言与规格置信度的效力序：用户裁决（BOARD）> parity 规格〔核验回写后〕> 本 PRD 暂行值。
3. **E1~E7 豁免常设（对齐评审不判差距）**：E1 删除类动作不进行内菜单/无一键删（**v1.1 实证加码**：V8 实测 Artifactory 删除 = 520px 确认框 + Delete 绿色主按钮、无 type-the-key——BinFlow 输入 key 确认为**更严有意偏离**；V4 实测 Artifactory 行尾为 icon-trash 直删、无 ⋮——BinFlow 行交互偏离登记见 FR-124.4）｜ E2 keyset「加载更多」分页 ｜ E3 仓库类型 badge 中性色 ｜ E4 权限编辑器整页 + 测试器/diff ｜ E5 用户/组编辑路由页 ｜ E6 中文文案 + 英文术语保留 ｜ E7 toast 右下锚位（**v1.1：V3 实证 Artifactory 为顶部居中单条、~2-3s 自动消失、无堆叠——转「再议」维持不改**〔BinFlow 右下堆叠为既有有意设计；用户推翻才开一行级微调票——ROADMAP 备稿候选登记〕）。**终验复核节点：逐条复核豁免仍成立，豁免倒退 = 缺陷**。
4. **锚族冻结纪律（壳替换不动锚）**：D1 抽屉化**不动 `smu-*` 锚族**；M1 向导化不动建仓表单既有锚；侧栏顶品牌区**不动 `app-nav` 锚所在 DOM 结构**；登录页不动 `login-*` 锚族。新锚入册（console-ux 锚册 + anchor-audit 对账器维持 0 断链）。
5. **红线保留（ADR-0001 对 UI 生效——M8/ADR-0029 口径延续）**：reverse-src 内 JFrog 前端资产零消费零复制；parity 规格的行为描述为本里程碑唯一 Artifactory 侧依据（其自身已声明 clean-room 来源）；图标为 24px 网格几何重绘仅作包型识别（package-icons §5 许可姿态——不得用于品牌位、不得暗示官方背书）；logo 三候选为 UX-1 原创（无第三方商标/字体字形嵌入）。
6. **流程条款（延续）**：新端点仍走 PM FR + ADR 流程（M14 唯二服务端面 = docker remote〔既有域增量，M3 Q4 缓议 + M13 K54 已定案〕与 npm legacy login〔既有 npm 域增量〕，均不新开 ADR，规格随票）；拆票宽度 ≤2；FE 票四闸门（typecheck / assert:tokens / anchor ledger / lint）+ axe 双主题全绿为合入条件；每票 AC 附可执行验收命令（Playwright spec 名或真实客户端命令）。

---

## 2. 范围

### 2.1 In scope

| # | 来源（指令/裁决/登记） | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| A | 主轴指令①（交互完全一致）+ parity §8/§10 批 0 | FR-123（活体核验 V1~V8 + parity 置信度回写 + 差距矩阵复核基线） | P0（前置锚） |
| B | 主轴指令① + parity §10 批 1（§7 矩阵最大差距项） | FR-124（批 1 四项处置——**v1.1 改判后口径**：D1 Set Me Up 抽屉化〔落地；决策项 C 维持暂行〕/ M1 建仓形态断言收口〔决策项 A 撤销〕/ M3 创建形态断言收口〔决策项 B 撤销〕/ L2 有意偏离登记〔V4 撤旗，票面撤销〕） | P0 |
| C | 主轴指令① + parity §10 批 2 | FR-125（批 2 补缺：Tokens 页真身 / L1 列选器+刷新 / F2 空态插画槽 / N2 侧栏图标槽〔**v1.1：V5 实测不降级——实票照排**〕） | P1（F2/N2 P2） |
| D | 主轴指令③ + UX-1 logo README §4 | FR-126（品牌 logo 候选 1 转正六用例接线 + wordmark 转 path） | P0（GitHub 用例 P2） |
| E | 主轴指令② + UX-1 icons README §6 | FR-127（包型图标 30 枚全站接线——npm/go 转 path 前置 + 四消费点 + 门控/暗底纪律） | P1 |
| F | 候选池 FE 类（T-374 L2/L3 + T-377 D2 注记） | FR-128（FE 债与测试前提注记：hover 对比度 + playwright 纯净实例/dind snapshotter README） | P1/P2 |
| G | 候选池非 FE（T-380——K54 条件已满足） | FR-129（docker remote 首航——docker 三态齐装收口，/v2 remote 数据链复用 helmoci 缝） | P1 |
| H | 候选池非 FE（T-374 L1 / T-376 / T-378） | FR-130（服务端小票包：npm legacy login / helm uninstall PVC keep / 启动日志措辞 / v3-flat 403-vs-409 补锚与条件翻转） | P1/P2 |
| I | 候选池裁量 | （不设 FR）NuGet symbol server 余量条件票（M12→M13→M14 四承）；D3 依赖树立项（**v1.1：Q7 已归位关闭——不立项，§2.2**） | — |
| J | **用户指令 intake ②（2026-08-31 23:2x——replication 的交互要与 Artifactory 一致；BOARD intake 在档）** | FR-131（replication 交互对齐——T-402 两段：a 锚定段 t226 活体探测〔差集法只读〕+ parity 册 R 系条目 + 差距清单 / b 实现段照锚定规格；§4.9 v1.1 增补） | P0 插空 |

前置产物（非 FR 行）：活体核验票（= FR-123 承载）、wordmark path 路线定案（K56，随 FR-126）、npm login 实证整理（K60，随 FR-130.1）。**v1.1 执行态**：三者均已交付——T-381（V1~V8 回写）/ K56 插空票 + T-389（wordmark 定案手工勾画 monoline + 六用例五落地）/ T-393 + T-394（K60 定案 + 实装闭环）。

### 2.2 Non-goals — M14 明确不做

**产品级（继承 PRODUCT.md + parity §9 永不建清单，不越界）**：洞察报表/趋势图表、漏洞扫描/合规 UI（Xray 对位）、Builds/Federation/Lifecycles/Release Lifecycle/Repository Path Map 管理面——「交互完全一致」指令**不自动解锁任何功能本体**（与 M13「行为逐项对齐不解锁 HA」同构）。不做 Artifactory 全量 REST 兼容。

**M14 里程碑级 Non-goals（含候选池收编判定留痕——去向全部登记）**：

| 不做项 | 隔离边界 / 去向（PM 判定理由留痕） |
|---|---|
| **AQL + 13 老搜索专程** | **滚 M15 专程候选第一顺位**——M13 PRD 原列 M14 第一顺位，被 2026-08-30 主轴指令让位（用户动作，非 PM 裁量）；体量专程级判断不变（查询语言/执行引擎/分页） |
| Replay + outbox 行级 REST 面（T-364 §5-③ + T-366 §4-2） | **滚 M15+ webhook 域二程**（与 Build-info 入向域同批评估）。理由：M13 刚交付最小面（订阅 CRUD + 最近投递记录），行级 Replay 属**运营增强非协议兼容面**；M14 容量让位 UI-parity 主轴与 docker remote；机制已备（死信重放在案），翻转面小不返工 |
| remote 缓存树高并发 busy 重试预算（T-377 D1） | **滚 M15+ 后端硬化批次**。理由：**门内口径（8 路）零 5xx 已达承诺**；24 路 0.27% SQLITE_BUSY 且**可重试**属超门并发边角；busy_timeout/重试预算姿势牵 SQLite 写路径，需专项回归，不宜插 FE 里程碑 |
| virtual 成员同型全包型推广（T-367） | **滚 M15+ 后端对齐程**。理由：全包型行为翻转需全型回归矩阵（13 包型 × 三 rclass），dev-go-core 负载宜专程；现态缺陷面窄（个别包型未强校验） |
| disable 快照契约翻转（T-364 收口登记④） | **不立项**——T-364 收口已钉死契约，无用户推翻信号；登记维持 |
| HA 本体 + Xray 集成面本体 | 沿 M12/M13 Q 终裁维持「单列专程」——前置 PRODUCT.md「明确不做」修订解禁（用户动作，截稿未发生） |
| 依赖树 D3（parity §4） | **Q7 已归位关闭**（v1.1——V7 实测〔T-381〕：制品详情页签 = General/Effective Permissions/Properties/Followers **无依赖视图**，依赖数据挂 Builds 面〔parity §9 永不建〕——D3 模式行关闭；若用户立项需先出后端依赖解析域票，UI 形态届时补 spec） |
| 像素级复刻 JFrog UI / 复制 JFrog 前端资产 | **永久不做**（ADR-0001/0029——形态对齐非像素对齐；§1.4 条款 5） |
| 官方协议 logo 矢量源复用 / 用于品牌位 | **永久不做**（package-icons §5 许可姿态——几何重绘仅作包型识别；低置信三枚〔nuget/conan/docker 鲸腹〕商业化分发前商标复查） |
| N2 侧栏图标（若 V5 核验为主流版本无图标） | 条件降级——parity N2 既定：「核验发现无则降级为不做」（条件票 slot，V5 结论驱动）。**v1.1：V5 实测一级条目带图标（应用域/管理域双侧栏、子项无图标）——不降级，T-388 实票照排，档位修正为一级条目** |
| F1 toast 锚位改版 | E7 豁免维持——V3 核验结论若「显著不同且用户在意」再开微调票，默认不改。**v1.1：V3 实证显著不同（顶部居中单条 vs BinFlow 右下堆叠）——维持不改（既有有意设计）；用户推翻才开一行级微调票（ROADMAP 备稿候选登记）** |

---

## 3. 用户与场景（M14 视角）

- **场景 A（Artifactory 迁移管理员）**：建仓两段式（入口选型弹窗〔网格/下拉〕→ 整页分节表单，Cancel 在左 Save 在右）——与 Artifactory 肌肉记忆一致（**v1.1 改判后口径**：V2 实测 Artifactory 即两段式，BinFlow 现形态同构——T-383 断言收口）。
- **场景 B（CI 工程师，接入向导）**：仓库行点「Set Me Up」→ 右侧滑入抽屉（50vw 档），按 Configure/Deploy/**Resolve** 三 Tab 平铺拷贝命令片段（V1 实测形态）；铸 token 后片段带真实凭据——右手拷命令、左手看树，抽屉不遮全屏。
- **场景 C（新用户，第一眼识别）**：建仓网格与仓库列表里 docker 鲸腹/maven 羽毛/npm 红方一眼可辨（brand 版）；门控未解锁的包型 mono 置灰 + 档位徽章——知道「有什么、还差什么档位」。
- **场景 D（docker 平台工程师）**：docker remote 仓首航——代理上游 registry 首拉回源、二次命中本地缓存；docker 三态（local/remote/virtual）齐装。
- **场景 E（npm 老客户端用户 / 运维）**：legacy login 通道的老 npm CLI 可登录（T-77 O-4 实证形态）；helm uninstall 后 PVC 数据卷幸存（误卸载不丢制品）；审计页列选器按需收敛列宽。
- **场景 F（QA/维护者）**：V1~V8 核验回写后，parity 规格的置信度列 = 可审计的契约基线；差距矩阵逐格终评给出「哪些已对齐、哪些豁免、为什么」的完整答案。

---

## 4. 功能需求

约定：`BASE=http://127.0.0.1:8080`；`ADMIN="admin:password"`；parity Playwright spec 置 `web/e2e/m14/`；**Artifactory 行为基准 = `docs/design/console-artifactory-parity.md`（UX-1，clean-room 来源声明在册）**，其中/低置信细节以 V1~V8 核验回写为准（效力序见 §1.4 条款 2）；服务端面（FR-129/130）行为基准 = 既有规格票（helm.md remote 段 / npm 生态公开规范 + T-77 O-4 实证）；**M14 全部 FE 票不动服务端契约**（四闸门 + 锚族冻结 + 服务端 diff=0 复核沿 M8 先例）；全部实现票不得私加端点。

### 4.1 前置锚：活体核验与置信度回写（主轴地基，P0）

#### FR-123 活体核验 V1~V8 + parity 规格置信度回写 + 差距矩阵复核基线（qa-engineer 执行腿 + ux-designer 共笔；核验源 Q1）

**用户故事**：
- 作为本团队，批 1 形态对齐的验收断言必须建立在核验过的 Artifactory 行为上——凭记忆的细节不进断言（UX-1 置信度纪律的兑现票）。
- 作为后续里程碑的消费者，parity 规格的置信度列经活体核验升格后，成为可长期引用的契约基线（改置信度=改契约）。

行为规格：

- **123.1 核验源（Q1 三选，暂行①）**：① t226 容器恢复（本地 OSS 7.84.10——T-228 保留栈，docker start 可恢复；注意 OSS 面与商业版差异：addon 门控项隐藏，核心交互面〔Set Me Up/建仓向导/列表〕在）② 外部活体对照（JFrog Cloud 免费实例或官方文档站截图/视频）③ 仅凭 V1~V8 标注推进（核验票降级——置信度维持、细节断言挂「以核验为准」）。暂行 = ①优先 + ②辅助；均不可得 → ③降级（不阻塞批 1 主体，BOARD 留痕）。
- **123.2 核验执行**：V1（D1 锚侧/宽度/Tab 命名/折叠形态）/ V2（M1 rclass 控件形态/表单节名/折叠默认态）/ V3（F1 snackbar 锚位与堆叠）/ V4（L2 行菜单动作集——是否含删除）/ V5（N2 侧栏条目图标有无）/ V6（M3 用户/组/Token 创建字段集与分节 + Token 结果面板）/ V7（D3 依赖树存在面——若确认 BinFlow 不建即关闭）/ V8（M2 删仓 type-the-key 提示语形）。逐项截图/录屏归档。
- **123.3 回写纪律**：核验结论回写 parity 规格对应模式行的置信度列（修订留痕——§0 修订记录补行）；本 PRD 相应「以核验为准」附注摘除或确认；**不新增 Artifactory 功能面的顺带发现**（走 PM Non-goal 过滤）。
- **123.4 差距矩阵复核基线**：核验后重印 §7 矩阵作为终验对照基线（含 V5 降级判定、V7 关闭判定、E7 再议判定三出口）。

验收标准（AC）：

- **AC1（核验交付）**：V1~V8 逐项结论 + 证据（截图/录屏/文档锚点）归档；parity 规格置信度列回写完成（修订记录留痕）；核验源不可得项如实标注维持（零静默升格——grep「以核验为准」清单与核验结论一一对应）。
- **AC2（矩阵基线）**：差距矩阵复核基线落盘（§7 重印版 + 三出口判定）；tech-lead 就批 1/批 2 断言收口确认。
- **AC3（降级路径）**：若走 ③降级——降级清单上 BOARD 留痕，批 1 四项主形态断言不受阻（手势级断言均为高/中高置信）。

**执行态（v1.1 注，T-381 done 2026-08-31）**：核验源 = **① t226 容器一次恢复成功**（`docker start t226-pg t226-artifactory`——OSS 7.84.10 rev 78410900，与规格核验期望版本一致；admin 口令 VM 0600 留存、零落盘）——主降级路径未启用。V1~V8 全 DOM 实测回写：parity 规格 v1.0→**v1.1**（§0 留痕）+ package-icons README v1.0→**v1.1**（§4 三枚修正 + 六枚旁证），**零静默升格**（grep「以核验为准」清单与结论一一对应）。**三出口实际**：V5 **不降级**（一级条目带图标 → N2 维持 △，档位=一级条目）/ V7 **关闭**（D3 关闭——制品页签无依赖视图）/ E7 **再议**（toast 顶部居中实证 → 维持不改）。**子项降级 2 处如实标注**：V6c token 生成表单/一次性明文结果面板（profile 密码门 + OSS 导航无 admin 集中 token 页）；V7 build 详情依赖列表形态（实例无 build 数据不可观测）。**三个改契约级发现**（决策项 A/B 撤销 + L2 撤旗）转 §4.2 改判回写。证据：`reports/agents/t381-evidence/`（27 截图 + measurements.json）；执行期事故 INC-1（误删语料仓）已按字节集无损标准完整恢复——教训成文（破坏性动作默认禁点 + fixture 快照前置），后续票（含 T-402a replication 探测）强制吸收。

### 4.2 批 1 形态对齐四项（主轴 P0——§7 矩阵最大差距）

#### FR-124 D1 Set Me Up 抽屉化（落地）+ M1/M3 断言收口（决策项 A/B 撤销）+ L2 有意偏离登记（票面撤销）（web/src；Q3 终裁态见 §7）

**用户故事**：
- 作为 Artifactory 老手，Set Me Up 是右侧滑入的抽屉——这个手势我不用重新学；建仓两段式、用户/组创建整页表单，BinFlow 现形态已与 Artifactory 同构（V2/V6 实测对齐——v1.1 改判后「零学习成本」同样成立）（场景 A/B）。
- 作为键盘用户，抽屉与弹窗的焦点行为（入框、Tab 循环、关闭回焦）在形态迁移后不劣化。

行为规格：

- **124.1 D1 Set Me Up 抽屉化（parity §4 D1 v1.1——批 1 唯一形态改造票；已落地 T-382）**：`SetMeUpDialog.tsx` 壳 `Dialog` 720px → **`Drawer(anchor="right", variant="temporary")` 宽 `min(clamp(480px, 50vw, 800px), 100vw)`**（**V1 实测修正**：50vw〔480~800〕，1600 视口 800px——T-382 几何实证 `{x:800,w:800}` 逐位一致）。**保留清单（缺一即缺陷，T-382 全量兑现）**：`smu-*` 锚族全量零改名（壳替换不动锚）；Esc/backdrop 关闭链；关闭回焦启动元素；OIDC 续铸重开链路（`AppShell` 的 `resumeOpen` 分支——armed 真 IdP 全链绿）；step-up 内联面板纪律（抽屉内不弹二级框）；命令块 `pre` 换行策略 `overflow-x: auto`（mono 不折断）。**Tab = Configure / Deploy / Resolve 三枚**（V1 实测补 Resolve；片段每 Tab 平铺无手风琴——V1 修正）；底栏「← Select a different package type（左）+ Done（右 primary）」；步 0 药丸化（smu-grid 键盘链路零改动）。**决策项 C（Q3，维持暂行）**：Deploy 对话框〔`DeployDialog.tsx`〕**保持居中 Dialog**——T-382 零改动遵守（V1 未覆盖 Deploy 面形态、无证伪信号；终裁窗维持开放，§7-Q3）。
- **124.2 M1 建仓形态——断言收口（v1.1 改判：决策项 A 撤销）**：V2 活体实测（T-381）证伪「全程单 modal」记忆——Artifactory 7.84 建仓 = **列表入口「Add Repositories」下拉预选 rclass → 「Select Package Type」880px 磁贴 modal → 选型后 modal 关闭、落整页路由表单**的两段式。**BinFlow 现形态（pkg-grid 选型 Dialog → 路由页分节表单）已对齐——零形态改动收口**（T-383 六行对照核验：rclass 手势等价对位 / 两段式实证〔选型后 modal 关、URL 不变、表单在路由页〕/ 六节结构 `form-section-*` 新锚 / 深链三腿 / goto 直达 / remote×docker 组合门控）。**唯一有意差异 = 磁贴尺寸档**：Artifactory 880px 系 33 包型 90×90 大磁贴 vs BinFlow 440px〔1280/1600 实测〕配 190×44 高密度卡磁贴 + 门控徽章——**票内定案不追平**（追平即大面积留白；**440 档写进断言**，改档须有意识动 spec——T-383）。深链 `/admin/repositories/new` 与编辑态 `/admin/:key/edit` 整页维持。包型网格卡片 brand 版图标接线归 FR-127（T-390 已落）。
- **124.3 M3 用户/组创建形态——断言收口（v1.1 改判：决策项 B 撤销）**：V6 活体实测（T-381）证伪「创建走 modal」记忆——Artifactory 7.84 用户/组创建 = **整页路由表单**（用户：User Name/Email/Password/Retype + 5 复选项；组：Group Name/Description/External ID + 复选项 + 成员选择列表；页脚 Cancel 最左 / Reset / Save 右）。**BinFlow 现形态已对齐——零形态改动收口**（T-384 对照核验四行：非 modal 反断言 / URL 不离列表 / 用户四节〔settings/options/password/groups〕+ 组两节字段归属 / 页脚三联几何序 Cancel<Reset<Save；**创建态无 Retype 为票内定案**；External ID/Auto Join 不建沿 console-m8 §6.10 既有裁定）。编辑保留路由页（E5 豁免注记维持）。
- **124.4 L2 列表行交互——有意偏离登记（v1.1 改判：V4 撤旗，票面撤销）**：V4 活体实测（T-381）——**Artifactory 7.84 仓库行/用户行均无 ⋮ 菜单**，行尾为单个 `icon-trash` 直删图标按钮；「行内 ⋮ 收纳行操作」的 parity 前提不成立。**原「加 ⋮ 菜单」票面撤销不派发**（T-385，conductor 改判 2026-08-31）；BinFlow 现行交互（行点击进详情 + 既有动作位）按 **E 类有意偏离登记（E1 家族）**：可发现性优于 icon-only 直删、删除走危险区/输入确认不倒退（V8 实测 Artifactory 删除确认 = 520px message-box + Delete 绿色主按钮、无 type-the-key——BinFlow 输入 key 确认为**更严有意偏离**，E1 实证加码）。**残值收编**（与 BOARD 改判登记零矛盾核对在案）：「菜单无删除」缺席断言精神并入终验 E1~E7 复核面（T-400）；「行点击进详情」语义由各列表既有 spec 覆盖；「复制 key / Set Me Up 行内快捷」降为**可选自有增强候选票**（非 parity 面——T-381 §V4 建议，M15+ 候选池/ROADMAP 备稿登记）。
- **124.5 四闸门 + a11y**：全部新/改 FE 面过 typecheck / assert:tokens / anchor ledger / lint 四闸门；axe 双主题 serious=0；键盘可达（焦点陷阱/Tab 循环/关闭回焦）在 Drawer 与 Dialog 两形态复测。

验收标准（AC）：

- **AC1（D1 抽屉化——已落地 T-382）**：Playwright——Set Me Up 从仓库行/详情打开为右滑抽屉（宽 `min(clamp(480,50vw,800),100vw)` 档断言——1600 视口 {x:800,w:800}）；`smu-*` 锚族对账零改名（anchor-audit 0 断链）；Esc/backdrop 关闭 + 关闭回焦；OIDC 续铸重开链路回归绿（armed 实例腿）；step-up 内联面板在抽屉内完成；命令块横向滚动不折行断言；Configure/Deploy/Resolve 三 Tab 键盘循环 + 焦点陷阱。
- **AC2（M1 断言收口——已收口 T-383，v1.1 改判后口径）**：Playwright——新建仓两段式对照断言（入口选型 modal → 整页分节表单 → Save 落仓）；深链 `/admin/repositories/new?package=<t>` 进入即开选型；浏览器回退回列表；编辑态整页维持；**非「全程单 Dialog」反断言**（决策项 A 撤销留痕）；磁贴 440px 档写进断言；M2/M3 建仓回归序列零回归。
- **AC3（M3 断言收口——已收口 T-384，v1.1 改判后口径）**：Playwright——用户/组创建**非 modal 反断言**（整页表单、URL 不离列表域）；页脚三联 Cancel<Reset<Save；编辑走路由页（E5 豁免注记）；M7 RBAC 语义（角色闭集/readonly 禁用臂）零回归。
- **AC4（L2 有意偏离登记——票面撤销 T-385，v1.1 改判后口径）**：现行行交互断言 = 行点击进详情维持（各列表既有 spec 覆盖）+ 行尾**无 ⋮ 菜单**（不新增——V4 撤旗）；「删除不进行内」缺席断言精神并入 T-400 E1~E7 复核面；readonly 臂禁用态维持。
- **AC5（a11y 与回归）**：axe 双主题 serious=0；键盘全流程（Tab 序/焦点陷阱/回焦——Drawer 与 Dialog 双形态）；M8 e2e 既有 Set Me Up spec 迁移更新（T-382 11P）+ 建仓/用户组 spec 增补对照断言（T-383/T-384）后全量绿；服务端 diff=0（FE 票复核——T-382/T-383/T-384 实测零服务端改动，沿 M8 T-235 先例）。

### 4.3 批 2 补缺（P1/P2）

#### FR-125 Tokens 页真身 + L1 列选器/刷新 + F2 空态插画槽 + N2 侧栏图标槽（web/src；N2 条件 V5）

**用户故事**：
- 作为管理员，Access Tokens 页不再是占位——创建走 modal、明文仅展示一次、吊销有确认（与 Set Me Up 铸币同族手势）。
- 作为审计员，审计列表列多——列选器让我只看关心的列，刷新按钮让我确认最新状态。

行为规格：

- **125.1 Tokens 页真身（parity §3 M3——`PlaceholderPage` 退役）**：创建 modal（`Dialog sm`；BinFlow 语义 = 既有 mint/step-up 链复用〔SetMeUp 同源引擎〕，scope 选择按既有 token 语义）；**一次性明文面板**（复用 `smu-token-panel` 形态——仅展示一次、刷新即失）；吊销确认（`ConfirmDialog` danger 形态）；列表（既有 token REST 消费，readonly_admin 只读臂）。**不私加端点**——消费面 100% 既有 REST。**v1.1 注（Q4 维持降级——V6c）**：Artifactory token 生成表单字段集与一次性明文结果面板**未核验**（profile 密码门 + OSS 导航无 admin 集中 token 页）——字段集维持暂行（BinFlow 既有 token 语义 +「以核验为准」附注），T-386 以暂行字段集起步、终裁后票内收口（M14-SPLIT §6-4 口径）；**V6 增量参照形态**：Artifactory profile 面「Authentication Settings → Generate an Identity Token」+ Identity Tokens 表（4 列 + 列选器）——Tokens 列表形态可参照（列选旁证 L1）。
- **125.2 L1 列选器 + 刷新（仓库/审计先行）**：`RepositoriesPage`/`AuditPage` 工具栏补列选 `Menu`（checkbox 列表，列集含既有全部列；记忆 localStorage per-page）+ 刷新 `IconButton`（静态列表手动刷新；轮询页维持自刷新）。列宽/空列处理沿「无端点列不伪造」纪律。
- **125.3 F2 空态插画槽（P2）**：`EmptyState` 加 40px 可选插画槽位；图形语言 = logo mark 容器+箭隐喻线稿（候选 1 派生，ux-designer 出 2~3 张线稿）；**不引第三方插画库**。
- **125.4 N2 侧栏图标槽（P2，V5 条件票）**：侧栏条目加 16px 图标槽（mono 通用符号 + 包型图标）；**V5 核验为主流版本无条目图标 → 降级不做**（parity N2 既定出口）。**v1.1：V5 实测不降级**——两域侧栏一级条目均带图标（子项无图标，父级分组可折叠）→ T-388 实票照排，档位修正为一级条目。

验收标准（AC）：

- **AC1（Tokens）**：Playwright——创建 modal 全链（创建 → 明文面板一次展示 → 刷新后不可再取）；吊销确认 + 列表状态翻转；readonly_admin 只读臂；`PlaceholderPage` 该路由退役（grep 零残留）；消费端点清单 == 既有 token REST（零新端点断言）。
- **AC2（L1）**：Playwright——仓库/审计页列选开合、列显隐持久（reload 保持）、全选/复位；刷新按钮取数；其余列表页不受影响。
- **AC3（F2/N2）**：空态插画槽渲染/缺省双态；N2 按 V5 结论执行或降级留痕（BOARD）。
- **AC4（回归）**：M8/M12 既有 spec 全量绿 + 四闸门 + axe。

### 4.4 品牌 logo 转正与六用例接线（主轴指令③，P0）

#### FR-126 候选 1「容器·双箭流」转正 + 六用例接线 + wordmark 转 path（web/src + web/public + docs-site；圈定窗 Q2）

**用户故事**：作为产品负责人，BinFlow 有自己的品牌标识——浏览器标签、登录页、侧栏顶、文档站一致呈现；作为用户，16px 的 favicon 里也能认出它。

行为规格：

- **126.1 转正程序（Q2 圈定窗）**：接线票以**候选 1 为工作 logo** 起步（参数化资产形态——`web/src/assets/brand/` 单点引用，替换成本=换文件）；圈定窗截止 B2 前波——用户未推翻即转正（BOARD 留痕）；若用户改选候选 2/3，仅资产替换，接线面零返工。**v1.1 注（Q2 已闭）**：圈定窗已过、用户未推翻——**候选 1「容器·双箭流」转正生效**（T-389 六用例五落地 + K56 生产件 6 件；单点引用层 `BrandLogo.tsx` 在案——换稿=换文件机制保留）。
- **126.2 六用例接线（UX-1 logo README §4 逐行）**：① favicon.ico（mark 单形 16/32/48 三档合成，16px 两箭可辨）；② PWA/apple-touch-icon PNG 180/512；③ 登录页品牌区横版 lockup 暗色版（`LoginPage` 纯文字品牌 + `◆` 菱形符号退役；`login-*` 锚族不动）；④ 侧栏顶 mark 24px + BinFlow 文字（`AppShell.tsx` `BinFlow ◆` 的 `◆` 由 mark 替换，`app-nav-brand` 结构不动，暗底版）；⑤ 文档站 navbar 横版 lockup 浅底版（docs-site 配置槽）；⑥ GitHub Org 头像（**远期 P2，非 DoD**）。
- **126.3 wordmark 转 path（K56——v1.1 已定案）**：生产件零字体依赖。**定案 = 手工勾画 monoline**（cap 100 / 笔宽 16 / 圆帽 / 字距 26，逐字坐标表公开——绕开 Inter 转曲许可面，与 mark 圆帽箭头同语言；K56 插空票 2026-08-31）；v1.0 暂行「Inter Bold OFL path」被票内定案推翻——**INTER-LICENSE 不再适用**（未走 Inter 路线）。
- **126.4 色板锚定纪律**：全部用色锚定 `--bf-accent`/`--bf-text` 双主题值（logo README §2 表逐行）；**改 token 必须同步 logo 资产**（断言：双主题下各用例渲染核对）。

验收标准（AC）：

- **AC1（六用例）**：五用例（①~⑤）落地核对——favicon.ico 三档文件在场且 16px 目测两箭可辨（样张归档）；PNG 尺寸/浅底对比；登录页/侧栏顶/文档站双主题截图核对；`◆` 与纯文字品牌残留 grep=0。
- **AC2（path 化）**：全部生产 SVG 零 `<text>` 零 `font-family`（grep 断言）；Inter 路线时 `INTER-LICENSE` 在场。
- **AC3（锚与门）**：`login-*`/`app-nav` 锚族对账零改名；四闸门 + SPA gzip 增量 ≤10KB 预算内。

### 4.5 包型图标 30 枚全站接线（主轴指令②，P1）

#### FR-127 图标集搬运 + npm/go 转 path + 四消费点接线（web/src；UX-1 icons README §6 消费规则）

**用户故事**：作为用户，建仓网格里的 docker/maven/npm 是它们本来的样子（品牌色）；列表与树里的类型列用单色小标不喧宾夺主；未解锁档位的包型一眼可辨「锁着」。

行为规格：

- **127.1 搬运与封装**：`docs/design/brand/package-icons/{mono,brand}/` 30 枚 → `web/src/assets/pkg-icons/`（或 React 组件封装）；文件名映射两例外注记（`deb.svg` ↔ wire `debian`、`go.svg` ↔ `go`）；`web/src/lib/repos.ts` PackageType 联合与图标 key 对齐（helmoci 入联合的 FE 侧小改，票内承载）。
- **127.2 转 path 前置**：`npm.svg`/`go.svg` 含 `<text>`——生产接线前必须转 path（其余 13 枚纯 path/rect/circle 零依赖；grep 断言）。
- **127.3 四消费点**：① 建仓 `pkg-grid`（brand 版 + 档位门控态用 mono + disabled）——与 FR-124.2 向导联动；② `smu-grid`（brand 版——`CLIENT_PKG_META` 字符图标 `▫ ⬢ ⌬ ⬒ ⬓` 退役）——与 FR-124.1 抽屉联动；③ 仓库列表/制品树/搜索的类型列（mono 版 currentColor 随文字色）；④ `LicenseAddonsPage` addon 矩阵（trashcan/webhook brand 版）。
- **127.4 门控与暗底纪律（K61——v1.1 已定案，T-390）**：门控包型（license 未解锁）= **mono + `opacity: 0.4` + `pkg-tier-*` 档位徽章**（brand 版不置灰——脏色）——三件套已全量落地。暗底纪律 **v1.1 定案**：对比口径修正为**对宿主面而非对背景**（T-390 揪出 package-icons README §6.4 口径错误并已纠）；全枚实算 11 色 <3:1 → **`--bf-pkgicon-*` 提亮档 token**（最差宿主 ≥3.4）取代 v1.0「+10% 亮度豁免簿记」；helm/nuget 超 +10% 量级留 ux 复核（纯蓝通道物理下限）；门控/aria 纪律（装饰 `aria-hidden`、语义 `aria-label`）全量落地。
- **127.5 a11y**：纯装饰图标 `aria-hidden`；承载语义处（类型列）配 `aria-label`（包型名）。

验收标准（AC）：

- **AC1（接线完整）**：Playwright——四消费点逐点核对（网格 brand/列表树搜索 mono/addon 矩阵 brand/门控置灰态）；`CLIENT_PKG_META` 字符图标 grep=0；30 枚全部被消费或注记豁免（如某包型暂无 UI 面）。
- **AC2（path 与纪律）**：`web/src/assets/pkg-icons/` grep `<text>` = 0；门控态断言（mono+opacity+徽章三件套）；暗底抽查记录归档。
- **AC3（预算与回归）**：SPA gzip 增量 ≤10KB；M8 建仓/Set Me Up 既有 spec 联动更新后全量绿；四闸门 + axe。

### 4.6 FE 债与测试前提注记（候选池 FE 类收编，P1/P2）

#### FR-128 仓库表 hover 对比度 + playwright/dind 前提 README 注记（web/src + web/e2e README）

**用户故事**：作为低视力用户，仓库表行 hover 态文字仍可读（≥4.5:1）；作为贡献者，跑 e2e 前知道「必须纯净 community 实例」与「dind snapshotter 的坑」，不再白跑一轮 132 红。

行为规格：

- **128.1 hover 对比度（T-374 L2——4.41:1）**：仓库表 hover 态文字/背景对比度提升至 **≥4.5:1**（双主题；不引入新 token 优先——现 token 微调或 hover 底色换档，票内定案留痕）；axe 双主题 serious=0 维持。
- **128.2 测试前提注记（T-374 L3 + T-377 D2）**：`web/e2e/README` 补两行——① 全量 Playwright 须纯净 **community 实例**前提（pro 宿主 132 红——T-374/T-377 两轮实证）；② dind 调试建议 `--feature containerd-snapshotter=false`（T-377 D2 环境注记）。

验收标准（AC）：

- **AC1（对比度）**：双主题 hover 态色值断言 ≥4.5:1（axe 自定义规则或色值计算断言，T-374 L2 形态复用）；全量 axe serious=0。
- **AC2（注记）**：README 两行落笔 + `make docs`（如有联动）零断链；e2e 全量在纯净 community 形态复跑绿。

### 4.7 docker remote 首航（副线 P1——T-380 条件已满足）

#### FR-129 docker remote pull-through（internal/adapter/docker；复用 M13 helmoci remote 缝——K54 判定成立）

**用户故事**：作为 docker 用户，`docker pull $BASE/docker-remote/<img>` 首拉回源上游、二次命中本地缓存——docker 三态（local/remote/virtual）齐装，与 helmoci 同体验。

行为规格：

- **129.1 remote pull-through**：docker remote 仓——manifest（by tag/digest）+ blob 按需回源、缓存落盘（checksum 寻址复用）；上游认证链（401 → WWW-Authenticate Bearer → token 交换）；**/v2 remote 数据链复用 T-363 helmoci 缝**（M3 Q4 缓议点的 docker 本体落地，K54「共享缝边际成本≈0」判定兑现）。
- **129.2 降级**：已缓存制品可拉；未缓存零 5xx（remote 既有降级口径——本地事实兜底 + 降级标记）。
- **129.3 门控与边界**：docker 槽（核心五包型，community）既有——remote 自动受缝，不新增槽；docker virtual 既有聚合语义维持（回归面）。

**执行态（v1.1 注，T-392 done 2026-08-31）**：全链落地——自指上游 push digest 全等 / fresh 首拉上游 +3 / rmi+prune 复拉 **delta 0 冻结** / mock Bearer **全舞步恰一轮**（401→token 交换 Basic 验证→授权拉取）/ 停上游降级 **200+STALE+Upstream-Error** / 未缓存 404 零 5xx；**K54「/v2 共享缝边际成本≈0」兑现——产品码净变更 ~20 行**（validate.go remote 类型表 + 文案）；断言翻转 7 处（矩阵面）+ MISS/HIT/STALE 三态；helmoci remote 零回归（Digest 同值 tgz 逐字节）；NFR-P63 不破（check-size 94.13MB / footprint 11.9MB / 冷启 767ms）。遗留三项归 ROADMAP 备稿（Prometheus remote 族双计数源 RE-11 / dind PMTU 环境注记 / httpapi WriteTimeout 姿态归 architect）。

验收标准（AC）：

- **AC1（全链）**：自指上游（BinFlow docker local push）→ 经 docker-remote `docker pull` digest 一致 → 二次 pull 命中本地缓存（上游访问计数不增）；dind 腿（M2/M13 夹具复用）。
- **AC2（认证与降级）**：上游 Bearer 认证腿绿；停上游 → 已缓存可拉 / 未缓存零 5xx。
- **AC3（回归与门控）**：docker dind /v2 全量回归（local/virtual 序列）+ M13 helmoci remote 序列零回归；docker 槽 community 可用断言。

### 4.8 服务端小票包（候选池非 FE 收编，P1/P2）

#### FR-130 npm legacy login + helm PVC keep + 启动日志措辞 + v3-flat 补锚（internal/adapter/npm + charts/ + cmd/ + docs/reverse/nuget.md）

**用户故事**：老 npm CLI 的 legacy login 能登上 BinFlow；helm 误卸载不丢数据卷；启动日志不再误导；nuget v3/flat 直推面的状态码语义有规格锚（与 D-10 终裁同族收敛）。

行为规格：

- **130.1 npm legacy login（P1——v1.1 勘误统一：v1.0 §4 行文误标 P2，DoD#1/票面均 P1，M14-SPLIT §6-1 回填；T-374 L1——T-77 O-4 实证）**：npm 生态 legacy auth 端点族服务端支持（端点集 = T-393 K60 六条定案，`docs/reverse/npm.md`）；真实 npm 客户端 legacy 通道全链（login → token → publish/install 消费）；既有 npm 现代认证链零回归。
- **130.2 helm uninstall PVC keep（P1——v1.1 勘误统一，同上；T-376）**：chart PVC 生命周期策略——uninstall 默认**保留**数据卷（resource-policy keep 注解或等效；升级/回滚不丢数据）；docs 部署指南注记「彻底删除需手动清卷」。
- **130.3 启动日志措辞一行（P2；T-376）**：启动日志误导性措辞修正（一行；票内对照前后文留痕）。
- **130.4 v3-flat 403-vs-409 补锚（P2；T-378 遗留——D-10 同族）**：reverse 票补 nuget.md 增量锚（v3/flat 直推面：包已存在时的 403 vs 409 语义——D-10 终裁〔对齐 409〕的邻域面）；**补锚后若 as-built 与锚不一致 → 条件翻转票（Q6）**；一致 → 差异行关闭登记。**v1.1 执行态**：补锚已落（T-393——nuget.md §5.4 六断言 A1~A6，**K59 锚定值 = 409**：官方「409 = already exists」逐字 + flatcontainer 族 GET-only/catch-all PUT 转 v2 publish）；as-built 对照**不一致**（BinFlow 403 ≠ 锚 409，T-394 复核——A6 代码事实）→ **Q6 终裁两出口**：对齐 → T-401 翻转票（LC-66 归 A）；有意差异 → D 层差异行留痕（LC-66 归 D）——PM 不代拍（§7-Q6）。live 四臂矩阵归 T-401（community 档 license 门拒建 nuget 仓——live 腿受阻注记）。

**owner 口径（v1.1 注——M14-SPLIT §6-2 回填）**：PVC keep 注解面归 T-394（dev-go-core——charts/ 腿为文件地图协作点，release-engineer 会审；已按此执行）；Chart 版本 bump 与 UAT 验证腿归 T-399（release——§8-10）。

**执行态（v1.1 注，T-393/T-394 done）**：130.1 已落地——httpapi 写认证门对 `PUT /-/user/org.couchdb.user:<name>` 族**窄域豁免**（谓词 = PUT + 该前缀 + rev 变体 + **repo 行类型==npm 钉死**〔承重墙——否则 generic 仓把该族当节点匿名写入〕；豁免连带清 action/required 与 addon 门——K60 认证面语义）；真实 npm 10.9.8 `npm login --auth-type=legacy` 裸交互全链绿（Logged in → `.npmrc` 改写 `_authToken` → whoami/publish/install 退出码全 0）+ web 型 401→ENYI 回落同链绿 + **login 幂等再铸永不 409 双证**（K60-6 不变量）。130.2 注解面已落（merge 四态渲染 + NOTES 手动清说明；UAT 实证归 T-399）。130.3 已落（built-in defaults 新句 + 钉死测试 + 活体）。遗留归 ROADMAP 备稿（whoami 403 读面 ACL 语义 conductor 裁 / npm≥11 漂移随升级窗 / 缺字段 400-vs-401 逐字另裁）。

验收标准（AC）：

- **AC1（npm login）**：真实 npm 客户端 legacy 通道 login 全链绿（登录 → 凭据落位 → 既有 publish/install 复用）；现代认证链回归绿；端点清单入票（K60 定案）。
- **AC2（PVC keep）**：kind/helm 编排——`helm uninstall` 后 PVC 幸存断言；重装同 release 数据可挂载回归；docs 注记落笔。
- **AC3（日志）**：启动日志前后对照留痕（grep 断言新措辞在场）。
- **AC4（v3-flat）**：nuget.md 增量锚落笔（出处标注）；as-built 对照结论留痕——一致关闭 / 不一致触发条件翻转票（Q6 终裁后）。**v1.1 执行态**：锚已落（K59=409）/ 对照**不一致**（as-built 403）→ T-401 条件票就位，**待 Q6 终裁触发判定**（§7-Q6）。

### 4.9 范围增补：replication 交互对齐（用户指令 2026-08-31，P0 插空——v1.1 增补）

#### FR-131 replication 交互面与 Artifactory 一致（T-402 两段：a 规格锚定〔qa-engineer 插空，在途〕/ b 实现段〔dev-frontend，候 FE lane 空位〕）

**用户故事**：作为 Artifactory 迁移管理员，仓库 Replication 面的表单字段、cron 形态、启停开关、Replicate Now 动作与状态呈现，与我在 Artifactory 里的操作手势一致——配置复制策略不用重新学。

行为规格：

- **131.1 锚定段（T-402a）**：t226 活体探测 Artifactory replication 交互面（仓库 Replication 面的表单字段 / cron 形态 / 启停开关 / Replicate Now 动作 / 状态呈现）+ `console-artifactory-parity.md` 增 **R 系条目**（置信度标尺沿用 §1）+ BinFlow 现状差距清单。**探测纪律（铁门槛）**：差集法只读、**永不点确认**（T-381 INC-1 教训——破坏性动作默认禁点、共享 fixture 写操作先快照、精确行级选择器）。
- **131.2 实现段（T-402b）**：照锚定规格对齐 BinFlow 控制台 replication 面——FE 形态票纪律全套（锚族冻结评估 + 新锚入册 + 四闸门 + axe 双主题 + **服务端 diff=0**；消费既有 replication REST，不私加端点）。
- **131.3 里程碑边界**：本条为收口窗增补票（非 PM 裁量——用户指令 BOARD intake ② 在档）——若实现段未随收口窗完成，滚 M15 首票（conductor 裁，ROADMAP 备稿已列留痕出口）。

验收标准（AC）：

- **AC1（锚定）**：parity 册 R 系条目落笔（逐条出处 + 置信度）+ BinFlow 差距清单归档；t226 探测零破坏性点击（fixture 前后健康对账留痕）。
- **AC2（实现）**：Playwright——replication 面形态对照锚定规格逐项断言；既有 replication REST 零变化（服务端 diff=0）；四闸门 + axe 双主题 serious=0。
- **AC3（收口）**：随收口窗完成归档；未完成 → 滚程留痕 BOARD（增补票条款——非 DoD 缺口）。

---

## 5. 兼容性矩阵（M14——UI 形态契约 + docker remote/npm login wire 面）

### 5.1 层级定义（UI-parity 里程碑适配版——沿 M8 先例「UI 对齐矩阵」+ M2~M13 wire 分级合流）

| 层级 | 定义 |
|---|---|
| **A 兼容** | 交互形态（品种/位置/尺寸档/关闭方式/按钮位/步骤结构）或端点/语义对齐 Artifactory；服务端 wire 面沿既有规格票 |
| **C 自有** | BinFlow 自有设计（无 Artifactory 对应或有意自有）——品牌资产/自有增强面；行为模式对齐但载体自定 |
| **D 有意不兼容 / 不做** | 显式裁决不做（clean-room / 安全向 / E1~E7 豁免族），矩阵留痕防再议 |
| **待裁** | 存在与 Artifactory（或规格锚）的可观测差异，须用户终裁——终裁后归 A/D 并回写 |

**E1~E7 豁免常设条款（parity §9 全文引入本 PRD，对齐评审不判差距；终验逐条复核）**：E1 删除不进列表行内菜单/无一键删 ｜ E2 keyset「加载更多」 ｜ E3 仓库类型 badge 中性色 ｜ E4 权限编辑器整页+测试器/diff ｜ E5 用户/组编辑路由页 ｜ E6 中文文案+英文术语 ｜ E7 toast 右下锚位（V3 后再议）。**豁免倒退 = 缺陷**（例：L2 菜单若混入删除项即违 E1）。

### 5.2 档位 × addon 解锁矩阵（M14 增量 0 行——19 槽维持）

M14 不新增 addon 槽。docker remote 走 docker 槽（核心五包型，community 解锁——remote rclass 自动受既有缝）；npm legacy login 走 npm 槽（核心，community）；Tokens 页消费既有 token REST（无槽）。三态叠加规则、`addons.disabled` 熔断语义全部沿 M10 §5.2 不变。**图标门控态纪律**（FR-127.4）消费既有 license 档位数据，不新增门控语义。

### 5.3 契约矩阵（11 条，LC-57~LC-67 续接 M13 编号——v1.1 修订：LC-60 归 D / LC-66 材料齐备 / 新增 LC-67 replication 增补）

| # | 契约面 | Artifactory 对应 / 公开规范 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| LC-57 | Set Me Up 右侧抽屉形态（**50vw 档〔480~800〕**/ **Configure·Deploy·Resolve 三 Tab** / 片段平铺无手风琴 / 底栏返回链接+Done；smu-* 锚族冻结） | parity §4 D1 **v1.1（V1 实测升格高）** + T-382 as-built（几何实证逐位一致） | A | P0 | **高（V1 实测）——已落地 T-382** | L02 |
| LC-58 | 建仓**两段式**（列表入口选型 modal → 整页路由分节表单）+ 深链/编辑态整页——V2 实测 BinFlow as-built 已对齐（T-383 断言收口，零形态改动）；**磁贴档 440px 有意不追平**（Artifactory 880px 大磁贴——追平即大面积留白，票内定案） | parity §3 M1 **v1.1（V2 实测升格高——决策项 A 撤销）** | A | P0 | **高（V2 实测）——已收口 T-383** | L03 |
| LC-59 | 用户/组创建**整页路由表单**（V6 实测对齐——非 modal 反断言，T-384 收口；页脚 Cancel/Reset/Save 三联）+ Tokens 页真身（BinFlow 补缺——V6 参照形态〔profile Identity Token 面 + 4 列表〕；字段集 V6c 降级，Q4 暂行，T-386 在途）；编辑路由页（E5 豁免） | parity §3 M3 **v1.1（V6 形态面实测升格高——决策项 B 撤销）** | A（用户/组腿）/ C 注（Tokens 腿——OSS 核验源无 admin token 面，自有设计 + 参照形态） | P0/P1 | 高（用户/组形态）/ 以核验为准（Tokens 字段集——V6c 降级如实标注） | L04 / L06 |
| LC-60 | 列表行交互——Artifactory 7.84 行尾**无 ⋮**（icon-trash 直删，V4 实测）；BinFlow **不新增 ⋮ 菜单**（V4 撤旗，T-385 票面撤销），现行「行点击进详情 + 既有动作位」按 **E1 家族有意偏离登记**（可发现性优于 icon-only 直删；删除走危险区确认不倒退）；「复制 key/SetMeUp 行内快捷」= 可选自有增强候选（非 parity 面） | parity §5 L2 **v1.1（V4 实测升格高——原 ⋮ 前提证伪）** | **D（有意不做——v1.1 改判；A→D）** | —（票面撤销） | 高（V4 实测） | L05（改判复核段——T-400） |
| LC-61 | 列表工具栏列选器 + 刷新（仓库/审计先行；列显隐 per-page 持久） | parity §5 L1（置信度中；形态通用件；T-381 L04 顺带观察「Customize Columns」旁证） | A | P1 | 中（形态）/高（控件语义）——**已落地 T-387**（per-page localStorage 定案） | L07 |
| LC-62 | 品牌 logo 六用例（favicon.ico 三档 / PWA PNG / 登录页 / 侧栏顶 / 文档站）+ wordmark path 化（**K56 定案：手工勾画 monoline**）+ 双主题色板锚定 | BinFlow 自有品牌（非 Artifactory 对齐面；色板锚定 tokens.css——改 token 必须同步） | C | P0 | —（自有设计）——**已落地 T-389（五用例）** | L09 |
| LC-63 | 包型图标 30 枚四消费点接线（mono/brand 双版；npm/go 转 path；门控三件套；暗底 ≥3:1——**K61 定案：对宿主面口径 + `--bf-pkgicon-*` 提亮档**） | Artifactory 包型网格用法同款（图形为 24px 网格几何重绘——package-icons §5 许可姿态，非矢量源复用；README §4 v1.1 三枚活体修正〔docker 单色 #0091E2 / conan #669ACC / nuget 现代曲线双形〕） | C | P1 | —（自有重绘；三枚低置信已 T-381 活体修正）——**已落地 T-390** | L10 |
| LC-64 | docker remote pull-through（manifest/blob 回源 + 缓存 + Bearer 认证 + 降级 MISS/HIT/STALE） | Artifactory docker remote + OCI Distribution 规范代理链；**复用 T-363 helmoci /v2 remote 缝**（K54 判定成立——T-392 兑现：产品码净变更 ~20 行） | A | P1 | 高——**已落地 T-392**（真客户端全链 + 降级 + 回归全绿） | L12 |
| LC-65 | npm legacy login 端点族（**K60 六条定案**：`/-/user/org.couchdb.user:<name>` PUT 唯一修复面 + whoami/ping 维持 + `/-/v1/login` 不做即正确 + profile/tokens 404 姿态 + rev-dance 不做 + **login 永不 409 不变量**） | npm 生态 legacy auth 公开端点（`docs/reverse/npm.md` 新建——npm 10.9.8 实物源码 + live 抓包对拍 L1~L7） | A | P1（v1.1 勘误统一） | 高——**已落地 T-394**（真客户端 legacy 全链绿） | L13 |
| LC-66 | NuGet v3/flat 直推面 403-vs-409——**K59 锚定值 = 409**（官方逐字 + flatcontainer 族 GET-only/catch-all PUT 转 v2 publish）；**as-built 403——对照不一致**（T-394 复核） | nuget.md §5.4 六断言 A1~A6（**已补锚** T-393——T-378 遗留/D-10 终裁邻域面收口） | **待裁（材料齐备——v1.1）**：Q6 终裁=对齐 → T-401 翻转后归 A；=有意差异 → D 层差异行留痕。**LC-66 离开「待裁」以终裁落章为准** | P2 | 高（A1~A6 逐条；live 四臂归 T-401——community 档 license 门拒建 nuget 仓注记） | L14 / T-401 |
| LC-67 | replication 交互面对齐（仓库 Replication 面表单字段 / cron 形态 / 启停开关 / Replicate Now 动作 / 状态呈现）——锚定段 t226 差集法只读探测 → parity 册 R 系条目 + 差距清单 → 实现段照锚定规格（**v1.1 新增**） | 用户指令 2026-08-31（BOARD intake ②）；parity 册 R 系（T-402a 在途补锚） | A | P0 插空 | 待锚定（T-402a 在途） | L19 |

> 计数（v1.1）：**11 条 = A 7（LC-57/58/59〔用户·组腿〕/61/64/65/67）+ C 2（LC-62/63——品牌资产自有；LC-59 Tokens 腿为 C 注不计独立条）+ D 1（LC-60——v1.1 改判：V4 撤旗 A→D）+ 待裁 1（LC-66——材料齐备，待 Q6 终裁）**。E1~E7 豁免族单列 §5.1 常设条款不入 LC 计数。既有契约面（五基础包型、九 addon 包型、配置域、操作族/回收站、webhook、helmoci remote）M14 对 M13 as-built 零行为变化（§5.4）——FE 主轴票「服务端 diff=0」复核沿 M8 先例（T-382/T-383/T-384 实测零服务端改动）；docker remote 为既有 docker 槽增量面（LC-64），npm login 为既有 npm 槽增量面（LC-65），replication 为 FE 形态增补面（LC-67——服务端 diff=0 前置）。

### 5.4 回归基线（M14 零断言反转设计——形态迁移不动语义；逐格复评代替反转）

| 既有断言 | M14 期望 |
|---|---|
| M1~M13 全部 P0 序列（双形态：无 license 默认 + pro） | 零回归（FE 形态迁移零服务端变化；docker remote 只增不改既有 /v2 local/virtual 面） |
| M8 e2e Set Me Up / 建仓 / 用户组 spec（现壳形态断言） | **迁移更新非反转（v1.1 执行态）**：Set Me Up spec 更新为抽屉形态（T-382 迁移 11P 已落——行为语义断言铸币/step-up/OIDC 续铸全量保留）；建仓/用户组 spec **维持现形态 + 增补对照断言**（M1/M3 改判——T-383 新 spec 4 腿 / T-384 spec 4 腿）；更新面 100% 归属 M14 豁免票 |
| `CLIENT_PKG_META` 字符图标 / `PlaceholderPage` Tokens 路由 / 登录页 `◆` | **退役**（grep=0 断言——归属 FR-127/125/126 豁免票） |
| axe 双主题 serious=0 / anchor ledger 0 断链 / assert:tokens / lint 四闸门 | 维持全绿（新 FE 面走新锚入册流程——沿 M13 熔断线口径） |
| 差距矩阵 §7（17×8）v1.0 现状档 | **逐格复评**：批 1/批 2 覆盖格翻 ✅/（豁）；核验三出口（V5 降级 / V7 关闭 / E7 再议）落档——终评覆盖率 100% |
| `make test`（race）/ footprint ≤100MB / check-size ≤100MiB / 冷启动 <2s | 维持（SPA gzip 增量 ≤10KB 新增预算门；docker remote 引擎不得破 M12 转绿门） |
| M12/M13 helmoci remote / deb / conan / webhook 序列 | 零回归（FR-130 小票不含行为翻转本体——v3-flat 翻转走条件票 Q6） |

### 5.5 M14 核心验收命令（L 序列骨架，QA 直接引用）

```bash
BASE=http://127.0.0.1:8080; ADMIN=admin:password
# ========== FR-123 活体核验（前置锚） ==========
# L01 V1~V8 逐项核验结论 + 证据归档；parity 规格置信度列回写（修订留痕）；差距矩阵复核基线落盘；
#    核验源不可得项 → 降级清单 BOARD 留痕（零静默升格——「以核验为准」附注与核验结论一一对应）
#    （v1.1 执行态：已执行 T-381——t226 一次恢复；parity v1.1 + package-icons v1.1 落盘；三出口 V5 不降级/V7 关闭/E7 再议；
#     子项降级 2 处〔V6c token 表单 / V7 build 依赖列表〕如实标注）

# ========== FR-124 批 1 形态对齐（v1.1 改判后口径） ==========
# L02 D1 抽屉化（已落 T-382）：Playwright——右滑抽屉宽档断言（min(clamp(480,50vw,800),100vw)——1600 视口 {x:800,w:800}）+
#    smu-* 锚族对账（anchor-audit 0 断链）+ Esc/backdrop/回焦 + OIDC 续铸重开链（armed 腿）+ step-up 内联面板 +
#    命令块 overflow-x 不折行 + Configure/Deploy/Resolve 三 Tab 键盘循环
# L03 M1 断言收口（已落 T-383 spec 4 腿——决策项 A 撤销）：两段式对照（入口选型 modal → 整页分节表单 → Save 落仓）+
#    深链 ?package=<t> + 回退回列表 + 编辑态整页维持 + 磁贴 440px 档断言 + M2/M3 建仓回归序列
# L04 M3 断言收口（已落 T-384 spec 4 腿——决策项 B 撤销）：非 modal 反断言 + URL 不离列表 +
#    页脚三联 Cancel<Reset<Save + 编辑路由页维持（E5）+ RBAC 语义零回归
# L05 L2 有意偏离登记（改判——票面撤销 T-385）：行点击进详情既有 spec 维持 + 行尾无 ⋮ 断言（不新增——V4 撤旗）+
#    E1 家族偏离登记复核（「菜单无删除」缺席断言精神并入 T-400 E1~E7 复核面）

# ========== FR-125 批 2 补缺 ==========
# L06 Tokens 真身：Playwright——创建 modal → 一次性明文（刷新不可再取）→ 吊销确认 → readonly 臂；
#    PlaceholderPage 退役 grep=0；消费端点清单 == 既有 token REST（零新端点）
# L07 L1 工具栏：Playwright——列选开合/显隐持久（reload 保持）/全选复位 + 刷新取数（仓库/审计两页）
# L08 F2/N2：空态插画槽双态渲染；N2 图标槽照排（v1.1：V5 实测不降级——一级条目带图标，T-388 执行）

# ========== FR-126/127 品牌资产 ==========
# L09 logo 六用例：favicon.ico 16/32/48 在场 + 16px 两箭可辨样张 + PNG 180/512 + 登录页/侧栏顶/文档站双主题截图；
#    grep 断言：SVG 零 <text> 零 font-family + ◆ 残留=0 + INTER-LICENSE（Inter 路线时）
# L10 图标接线：Playwright——四消费点逐点（网格 brand / 列表树搜索 mono / addon 矩阵 brand）+ 门控态三件套
#    （mono+opacity 0.4+档位徽章）+ CLIENT_PKG_META 字符图标 grep=0 + 暗底抽查 ≥3:1 + SPA gzip 增量 ≤10KB

# ========== FR-128 FE 债 ==========
# L11 a11y：双主题 hover 对比度色值断言 ≥4.5:1 + 全量 axe serious=0 + 键盘全流程（Drawer/Dialog 双形态焦点）；
#    e2e README 两行注记（纯净 community 前提 + dind snapshotter）落笔核查

# ========== FR-129 docker remote 首航 ==========
# L12 全链：自指上游 push → 经 docker-remote docker pull digest 一致 → 二次命中缓存（上游计数不增）→
#    上游 Bearer 认证腿 → 停上游已缓存可拉/未缓存零 5xx → docker dind /v2 全量回归 + M13 helmoci remote 零回归

# ========== FR-130 服务端小票包 ==========
# L13 npm legacy login：真实 npm 客户端 legacy 通道 login → token → publish/install 全链 + 现代链回归；
#    端点清单入票（K60）
# L14 小票（v1.1 执行态：npm login/PVC keep 注解面/启动日志已落 T-394，UAT 腿归 T-399；v3-flat 补锚已落 T-393、
#    对照不一致 → 翻转归 T-401 待 Q6 终裁）：helm uninstall 后 PVC 幸存 + 重装数据回归 + docs 注记；启动日志前后对照；
#    v3-flat as-built 对照结论留痕（一致关闭 / 不一致条件翻转 Q6）；3xx 终态 V4 活体顺带腿（T-364 遗留——机制已备，核验归档）

# ========== 回归与收口 ==========
# L15 全量回归：M1~M13 全 P0 双形态复跑 + FE 变更面（git diff m13-done..HEAD -- web/src）100% 归属 M14 票 +
#    服务端 diff 复核（FE 票 diff=0 沿 M8 先例）
# L16 差距矩阵终评：§7 复核基线逐格复评——批 1/批 2 覆盖格 ✅/（豁），终评覆盖率 100% + E1~E7 豁免逐条复核
#    （豁免倒退=缺陷）+ V5/V7/E7 三出口落档（v1.1：三出口已判——V5 不降级/V7 关闭/E7 再议维持不改，终评落档归 T-400）
# L17 NFR：SPA gzip 增量 ≤10KB + 预算门维持 + footprint/check-size ≤100MB/≤100MiB + 冷启动 <2s 三连 +
#    docker remote 首拉 P95 对齐 remote 口径
# L18 文档：tech-writer 增量（console parity 化 / 品牌注记 / docker remote 接入 / npm login / helm keep）
#    客户端命令实测可复跑（v1.1 执行态：文档 A 已落 T-397；文档 B T-398 在途）
# L19 replication 增补（v1.1——T-402 两段）：锚定段 parity 册 R 系条目 + BinFlow 差距清单归档（差集法只读零破坏性点击）+
#    实现段形态对照锚定规格逐项断言 + 服务端 diff=0 + 四闸门 + axe 双主题（随 T-402a/b 落定补全——QA 终验覆盖；
#    未随收口窗完成则滚 M15 首票留痕）
```

### 5.6 待校准项（v1.1 全量回填——K55~K61 归位态 + 依据锚；「v1.0 暂行值」列为历史对照保留）

| # | 项 | v1.0 暂行值 | **v1.1 归位值（依据锚）** |
|---|---|---|---|
| K55 | 活体核验源与 V1~V8 逐项结论（含三出口） | 暂行 t226 容器恢复优先 + 外部活体辅助；不可得降级标注 | **已归位（执行实证）**：核验源 = ① t226 容器**一次恢复成功**（OSS 7.84.10 rev 78410900——T-381 §0）；V1~V8 全 DOM 实测回写 parity v1.1 + package-icons v1.1（§0 留痕、零静默升格）；**三出口实际 = V5 不降级**（一级条目带图标→N2 维持 △ 档位=一级条目）**/ V7 关闭**（制品页签无依赖视图→D3 关闭）**/ E7 再议**（Artifactory toast 顶部居中单条 vs BinFlow 右下→维持不改）；置信度升级：D1/M1/M3/L2/F1/N2 形态面中→高、V8 维持高 + 文案实证（520px 确认框 + Delete 绿色主按钮 + 无 type-the-key）；子项降级 2 处如实标注（V6c token 生成表单/结果面板——profile 密码门 + OSS 无 admin token 页；V7 build 详情依赖列表形态——无 build 数据）。证据 `reports/agents/t381-evidence/` |
| K56 | wordmark 字体路线（Inter Bold OFL path vs 手工勾画 path） | 暂行 Inter path（OFL 可嵌入，成本最低） | **已定案（票内定案权行使——暂行被推翻）**：**手工勾画 monoline**（cap 100 / 笔宽 16 / 圆帽 / 字距 26，逐字坐标表公开——绕开 Inter 转曲许可面，与 mark 圆帽箭头同语言；INTER-LICENSE 不适用）。K56 插空票 2026-08-31（生产件 6 件 + npm/go 四枚转 path + conan 换色 #669ACC）；T-389 接线落地（六用例五落地 + 单点引用层 BrandLogo.tsx） |
| K57 | 决策项 A/B/C 终态 | 暂行照 parity §10 批 1 建议 | **A 撤销**（V2 实测证伪「全程单 modal」——Artifactory 7.84 建仓 = 下拉预选 rclass → 880px 磁贴 modal → 整页路由表单两段式；BinFlow 现形态已对齐，T-383 零形态改动收口，唯一有意差异 = 磁贴 440px 档不追平）；**B 撤销**（V6 实测证伪「创建 modal」——用户/组创建整页路由表单；T-384 零形态改动收口 + 非 modal 反断言；页脚 Cancel/Reset/Save 三联核验）；**C 维持暂行**（Deploy 保持居中 Dialog——V1 未覆盖 Deploy 面形态〔无证伪信号〕+ T-382 零改动遵守；终裁窗维持开放，§7-Q3）。撤销 = conductor 改判 2026-08-31（BOARD T-381/T-383/T-384 条目在案） |
| K58 | Tokens 页字段集与分节（Artifactory Access Token 创建对照） | 暂行 BinFlow 既有 token 语义（scope/有效期）+「以核验为准」附注 | **维持暂行（V6c 降级）**：Artifactory token 生成表单字段集/一次性明文结果面板**未核验**（profile 密码门——避免制造凭据变更 + OSS 导航无 admin 集中 token 页）；T-386 以暂行字段集起步、Q4 终裁后票内收口（M14-SPLIT §6-4 口径）；**增量参照形态已获**（V6：profile「Authentication Settings → Generate an Identity Token」+ Identity Tokens 表 4 列 + 列选器——Tokens 列表形态可参照）。商业版/云实例活体源可得时补核验（ROADMAP 备稿候选登记） |
| K59 | v3/flat 直推面 403-vs-409 锚定值与 as-built 对照结论 | 待补锚（D-10 邻域面） | **已锚定（T-393）**：**锚定值 = 409**（包已存在且不可覆盖）——nuget.md §5.4 六断言：官方「409 = already exists」逐字（Microsoft Learn 全文复核）+ service index `PackagePublish/2.0.0` @id = v2 base（真实客户端 push 永落 v2 面）+ flatcontainer 族 GET-only / catch-all PUT 转 v2 publish；**as-built 对照 = 不一致**（BinFlow「已存在+无 d」→ overwrite 门 ErrForbidden → **403**——`flat.go servePush` 代码事实，T-394 复核）→ Q6 终裁两出口（对齐→T-401 翻转 / 有意差异→D 层留痕）；live 四臂矩阵归 T-401（community 档 license 门拒建 nuget 仓） |
| K60 | npm legacy login 端点族清单（T-77 O-4 实证形态规格化） | 待实证整理随票 | **已定案（T-393）+ 已实装（T-394）**：六条——**K60-1** `PUT /-/user/org.couchdb.user:<name>` login（唯一修复面：httpapi 写认证门窄域豁免——PUT + 前缀 + rev 变体 + repo 行类型==npm 钉死）；**K60-2** whoami 维持；**K60-3** ping 维持；**K60-4** `POST /-/v1/login` 不做即正确（任意 4xx/500 → npm ENYI → 自动回落 couch，live 双向验证）；**K60-5** profile/tokens 族不做（404 姿态）；**K60-6** `-rev` rev-dance 族不做 + **不变量：login 永不答 409**。规格载体 `docs/reverse/npm.md`（新建——npm 10.9.8 实物源码 + live 抓包对拍 L1~L7）；真客户端 legacy 全链绿（T-394） |
| K61 | 图标暗底微调登记规则（+10% 亮度豁免簿记） | 预检三枚（docker/npm/pypi）≥3:1 已过；其余枚抽查 | **已定案（T-390——暂行规则被取代）**：① 口径修正——对比对象 = **宿主面而非背景**（package-icons README §6.4 口径错误已纠）；② 全枚实算 11 色 <3:1 → **`--bf-pkgicon-*` 提亮档 token**（最差宿主 ≥3.4）取代「+10% 亮度豁免簿记」；③ helm/nuget 超 +10% 量级留 ux 复核（纯蓝通道物理下限）；④ 低置信三枚活体修正（T-381 顺带腿：docker 单色 #0091E2 / conan #669ACC 已换 / nuget 现代曲线双形保留注记——README §4 v1.1） |

---

## 6. 非功能需求（NFR）

### 6.1 与已有 ADR / 规范的冲突/补充标注

| ADR/规范 | 冲突/补充点 | 本 PRD 立场 | 所需动作 |
|---|---|---|---|
| PRODUCT.md「明确不做」 | 「交互完全一致」指令是否解锁 UI 高级分析/Xray 面 | 不解锁——parity §9 永不建清单对齐（§2.2）；形态对齐≠功能对齐 | 无（防线维持） |
| ADR-0001/0029（clean-room 对 UI） | parity 规格 Artifactory 行为描述的取证合法性 | UX-1 已声明 clean-room 来源（产品知识非反编译）；核验（V1~V8）走活体/官方文档，reverse-src 前端资产零消费 | §1.4 条款 5 |
| console-ux v1.15（锚册纪律） | D1 壳替换是否动锚 | **锚族冻结**（smu-*/login-*/app-nav 零改名）；新锚入册 | FR-124/126 AC |
| M8 UI 对齐矩阵 24 条 | 与 parity §7 矩阵的关系 | M8 = IA/交互逻辑层（已收敛）；M14 = 形态层（弹窗/抽屉品种）——两层正交，M8 矩阵结论不因 M14 翻转 | 无 |
| package-icons §5 许可姿态 | 协议 logo 商标风险 | 几何重绘仅作包型识别；不用于品牌位；低置信三枚商业化分发前商标复查 | §1.4 条款 5（登记维持） |
| ADR-0032/0033（license 门控） | 图标门控态 | 消费既有档位数据（mono+opacity+徽章），不新增门控语义 | FR-127.4 |

### 6.2 性能与资源（M14 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P61 前端预算 | SPA gzip 增量 ≤10KB（30 枚图标 tree-shaken + logo 资产计入）；既有预算门维持绿；抽屉/向导首开无感知卡顿（Playwright 时序断言 <300ms 档——沿 M8 树展开口径） | P0 |
| NFR-P62 docker remote 回源 | 首拉 P95 对齐既有 remote 口径（maven/npm/helmoci 家族基线）；二次命中本地（P95 对齐本地仓）；万 blob 缓存树零 5xx | P1 |
| NFR-P63 资源门维持 | footprint ≤100MB / check-size ≤100MiB / 冷启动 <2s 三连维持（FE 资产 go:embed 增量 + docker remote 引擎不得破 M12 转绿门） | P0 |

### 6.3 可访问性（M14 增量——UI-parity 里程碑专项）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-A1 对比度 | 仓库表 hover ≥4.5:1（双主题）；图标暗底抽查 ≥3:1（图形件标准） | L11/L10 |
| NFR-A2 键盘与焦点 | Drawer/Dialog 双形态焦点陷阱、Tab 循环、关闭回焦全量复测；Esc 语义（取消关闭）逐面断言 | L02~L06/L11 |
| NFR-A3 读屏 | 装饰图标 aria-hidden；类型列 aria-label；抽屉标题 role/命名（MUI Drawer 语义默认承载） | L02/L10 |

### 6.4 安全底线（M14 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S68 危险动作姿态 | 列表行零一键删/零行内删除动作（E1——v1.1 改判后口径：V4 实测 Artifactory 为 icon-trash 直删，BinFlow **更严有意偏离**——删除仅走详情危险区/输入确认〔M2 族既有〕）；豁免倒退=缺陷 | L05/L16 |
| NFR-S69 凭据面 | Tokens/Set Me Up 明文一次性展示维持（刷新即失）；抽屉化不把凭据写 URL/日志（Playwright URL 断言 + 日志 grep） | L02/L06 |
| NFR-S70 数据安全 | helm uninstall PVC keep（误卸载不丢数据卷）；docker remote 上游凭据存储沿 remote 既有 AES-GCM 链 | L12/L14 |

### 6.5 可观测性（M14 增量）

- FE 主轴无新指标；docker remote 进既有 remote family 指标口径（回源/命中计数自动生效）。
- npm legacy login 复用既有 auth 登录审计事件族（新增通道不打新词——票内核对词表，缺词归 audit owner 登记）。
- 启动日志措辞修正（FR-130.3）不新增日志面。

---

## 7. 开放问题（Q1~Q7——v1.1 逐项归位：终裁/执行归位 4〔Q1/Q2/Q5/Q7〕· 撤销归位 2〔Q3-A/B〕· 维持暂行 3 臂〔Q3-C/Q4/Q6〕；状态 + 依据锚；PM 出材料不代拍——终裁落章归 conductor）

| # | 问题 | 影响面 | **归位态（v1.1）——状态 + 依据锚** |
|---|---|---|---|
| Q1 | **Artifactory 活体核验源**（V1~V8 执行前提）：① t226 容器恢复 ② 外部活体对照 ③ 仅凭置信度标注推进（降级） | FR-123 全票；批 1 细节断言的置信度 | **【终裁归位——执行实证】**核验源 = **①**：t226 容器**一次恢复成功**（T-381——`docker start t226-pg t226-artifactory`，OSS 7.84.10 rev 78410900 与期望版本一致；主降级路径未启用）。V1~V8 全 DOM 实测回写（parity v1.1 + package-icons v1.1，零静默升格）；三出口实际 = V5 不降级 / V7 关闭 / E7 再议；子项降级 2 处（V6c / V7 build 依赖列表形态）如实标注。BOARD 留痕 = T-381 done 条目（2026-08-31 12:2x）。**conductor 落章动作：随收口窗归档确认（材料已齐）** |
| Q2 | **logo 候选圈定窗与终选**（候选 1~3；wordmark 路线 K56） | FR-126；品牌长期资产 | **【终裁归位——转正生效】**圈定窗（B2 前波截止）已过、用户未推翻 → **候选 1「容器·双箭流」转正**（T-389 六用例五落地——favicon BMP-in-ICO 三档 / PWA 180+512 / 登录页随主题翻转 / 侧栏顶恒暗板 / docs-site navbar；⑥ GitHub 头像远期 P2）；**K56 定案 = wordmark 手工勾画 monoline**（v1.0 暂行 Inter path 被票内定案推翻——绕开转曲许可面；INTER-LICENSE 不适用）；单点引用层 BrandLogo.tsx 保留换稿=换文件机制。BOARD 留痕 = T-389 done + K56 插空票条目 |
| Q3 | **决策项 A/B/C 形态终裁**（parity §10 批 1 三决策项） | FR-124 三子项的形态边界 | **【撤销归位——A/B 终裁=撤销（conductor 改判 2026-08-31）】**：**A 撤销**——V2 实测证伪「全程单 modal」（Artifactory 7.84 = 下拉预选 rclass → 880px 磁贴 modal → 整页路由表单两段式；BinFlow 现形态已对齐）→ T-383 转断言收口（done，**零形态改动**；唯一有意差异 = 磁贴 440px 档不追平）；**B 撤销**——V6 实测证伪「创建 modal」（用户/组创建整页路由表单）→ T-384 同转收口（done，零形态改动 + 非 modal 反断言）。**【维持暂行——C】**：Deploy 保持居中 Dialog——V1 未覆盖 Deploy 面形态（无证伪信号）+ T-382 零改动遵守 + 抽屉内上传进度表过窄理由仍立（抽屉现 50vw 档）；**终裁窗维持开放**（建议随收口窗落章或明示无限期暂行——conductor 裁）。BOARD 留痕 = T-381 三改判 + T-383/T-384 done 条目 |
| Q4 | **Tokens 页字段集**（Artifactory Access Token 创建对照；BinFlow scope 语义已有） | FR-125.1 字段面（T-386 在途） | **【维持暂行——V6c 降级】**：Artifactory token 生成表单字段集/一次性明文结果面板**未核验**（profile 密码门〔避免制造凭据变更〕+ OSS 导航无 admin 集中 token 页——核验源不可得子项，非静默降级）；T-386 以暂行字段集起步（BinFlow 既有 token 语义 + `smu-token-panel` 形态）、终裁后票内收口（M14-SPLIT §6-4 口径）；增量参照形态已获（V6 profile 面 + Identity Tokens 表 4 列）。**候选登记**：商业版/云实例活体源可得时补核验（ROADMAP 备稿）。**conductor 落章动作：接受降级登记即归位（无须新裁）** |
| Q5 | **docker remote 首航排期确认**（K54 判定成立后的入波确认） | FR-129；M14 容量分配 | **【终裁归位——已入 M14 并落地】**：conductor 审定 PRD v1.0 时 Q5 暂行（入 M14 P1）生效 → **T-392 done**（2026-08-31 12:5x）——dind 真客户端全链（push digest 全等 / 二拉 delta 0 冻结 / mock Bearer 全舞步一轮 / 停上游 200+STALE / 未缓存 404 零 5xx）；**K54「/v2 共享缝边际成本≈0」兑现——产品码净变更 ~20 行**；docker 三态齐装收口。BOARD 留痕 = T-392 done 条目 |
| Q6 | **v3/flat 直推面 403-vs-409 终裁**（LC-66 离开「待裁」） | FR-130.4；nuget v3 直推面语义 | **【维持暂行——材料齐备待终裁】**：**K59 锚定值 = 409**（T-393 nuget.md §5.4 六断言——官方逐字 + flatcontainer GET-only/catch-all PUT 转 v2 publish）；**as-built 对照 = 不一致**（BinFlow 403，T-394 复核）→ **两出口待终裁**：① 终裁=**对齐** → 触发 **T-401 翻转票**（断言反转归属豁免票 + nuget 双客户端回归；LC-66 归 A）；② 终裁=**有意差异** → D 层差异行留痕（LC-66 归 D）。**默认方向参考 = D-10 终裁先例（对齐 409）——PM 建议①，不代拍**。**LC-66 离开「待裁」以终裁落章为准**（收口窗必裁项——否则 DoD#5 不闭）。live 四臂矩阵归 T-401（community 档 license 门拒建 nuget 仓） |
| Q7 | **依赖树 D3 定案**（parity §4——「待 PM 定案的非承诺项」） | parity 矩阵 D3 行去留；M15+ 后端域候选池 | **【终裁归位——关闭】**：V7 实测（T-381）——Artifactory **制品详情页签 = General/Effective Permissions/Properties/Followers，无依赖视图**；依赖数据挂 Builds 面（parity §9 永不建清单）。**D3 模式行关闭**（parity v1.1 已回写）；BinFlow 维持不建。若用户立项需先出后端依赖解析域票（PRODUCT.md 未列该能力——不为对齐而建功能，§2.2 同构逻辑维持）。BOARD 留痕 = T-381 done 条目（§5 三出口执行摘要） |

> **归位总账（v1.1）**：终裁/执行归位 **4**（Q1 核验源 / Q2 logo 转正 / Q5 docker remote / Q7 D3 关闭）；撤销归位 **2**（Q3-A / Q3-B——实测证伪 + conductor 改判 + 零改动收口）；维持暂行 **3 臂**（Q3-C Deploy 居中——终裁窗开放 / Q4 Tokens 字段集——V6c 降级登记 / Q6 v3-flat——**材料齐备待终裁，收口窗必裁**）。**收口核查位**：T-400 AC2「Q1~Q7 终裁归位核查（LC-66 离开『待裁』）」；LC-56 教训（M13 曾滞留「待裁」）防范——Q6 为唯一硬待裁项，材料已备齐。

---

## 8. M14 验收剧本（QA 总纲）

1. **前置锚先行**：L01（活体核验 V1~V8 + 置信度回写 + 矩阵复核基线 + 降级路径留痕）——批 1 细节断言收口的依赖。
2. **回归基线（硬门槛先行）**：M1~M13 全部 P0 序列双形态复跑全绿；FE 变更面（`git diff m13-done..HEAD -- web/src`）100% 归属 M14 票；FE 票服务端 diff=0 复核。
3. **批 1 形态对齐（v1.1 改判后口径）**：L02（D1 抽屉化——**已落 T-382**：锚族/关闭链/回焦/OIDC 续铸/三 Tab/命令块不折行）→ L03（M1 断言收口——**已落 T-383**：两段式对照 + 深链 + 磁贴档）→ L04（M3 断言收口——**已落 T-384**：非 modal 反断言 + 页脚三联）→ L05（L2 有意偏离登记——票面撤销 T-385，复核归 T-400）。
4. **批 2 补缺**：L06（Tokens 真身 + 零新端点——T-386 在途）→ L07（L1 列选/刷新——**已落 T-387**）→ L08（F2 插画槽 / N2 图标槽——V5 不降级照排 T-388）。
5. **品牌资产**：L09（logo 六用例五落地 + path 化 + 双主题——**已落 T-389 + K56**）→ L10（图标 30 枚四消费点 + 门控/暗底纪律 + 预算——**已落 T-390**）。
6. **FE 债与 a11y**：L11（hover 对比度 + axe + 键盘双形态 + README 两注记——**已落 T-391**）。
7. **BE 副线**：L12（docker remote 全链 + 认证/降级 + 回归——**已落 T-392**）→ L13（npm legacy login——**已落 T-394**）→ L14（PVC keep + 启动日志——**已落 T-394**（UAT 腿 T-399）；v3-flat 补锚**已落 T-393** + 对照不一致 → 翻转归 T-401 待 Q6 + 3xx V4 顺带腿）。
7b. **范围增补（v1.1——用户指令 2026-08-31）**：L19（replication 交互对齐——T-402 两段〔a 锚定在途 / b 实现候 FE lane〕；未随收口窗完成则滚 M15 首票留痕）。
8. **parity 收口专项**：L15（全量回归 + 归属审计）→ **L16（差距矩阵逐格终评——覆盖率 100% + E1~E7 豁免逐条复核 + 三出口落档）** → L17（NFR 与资源门）→ L18（文档实测复跑）。
9. **文档**：tech-writer 增量——console.md 用户文档 parity 化（新形态操作说明/截图更新）、品牌注记（logo/图标来源与许可姿态）、docker remote 接入指南、npm legacy login 注记、helm keep 说明、FAQ 增补（形态迁移对照表——Artifactory 手势 → BinFlow 对应）。
10. **release**：部署烟测 + **UAT 随里程碑 PR**（M13 起常态——UAT 首跑证据随里程碑 PR 归档）；helm PVC keep 在 UAT 链验证。

---

## 9. M14 DoD

1. §4 全部 P0 AC（FR-123/124〔落地·收口·登记三类处置〕/126 五用例段/**131 replication 增补段——v1.1**）经 qa 验证全绿；P1（FR-125 主段/127/128.1/129/130.1~130.2）全绿；P2（FR-125.3/125.4/126 GitHub 用例/128.2/130.3/130.4）全绿；条件票（NuGet symbol server 余量四承 / v3-flat 翻转 Q6——T-401）按余量/条件条款——未触发不构成 DoD 缺口，须 BOARD 留痕。**v1.1 注**：N2 已转实票（V5 实测不降级——T-388）；D3 已关闭（Q7 归位——不占条件位）；**T-402 replication 若实现段未随收口窗完成，按 §4.9-131.3 增补票条款滚 M15 留痕（非 DoD 缺口）**；
2. §8 剧本全绿；§1.2 量化门槛表逐行达标（形态对齐四项/矩阵终评/活体核验/品牌六用例/图标接线/a11y/docker remote/小票包/资源门——全部有实测数字或截图证据归档）；
3. **parity 收口专项**：差距矩阵 §7 逐格复评完成（终评覆盖率 100%，三出口〔V5 降级/V7 关闭/E7 再议〕落档）；**E1~E7 豁免逐条复核通过（豁免倒退=缺陷）**；四闸门（typecheck/assert:tokens/anchor ledger/lint）+ axe 双主题 serious=0 全程维持；锚族冻结对账（smu-*/login-*/app-nav 零改名——anchor-audit 0 断链）；
4. 回归硬门槛：M1~M13 全部 P0 序列双形态复跑全绿；M13 as-built 零行为变化（豁免票归属外）；FE 变更面 100% 归属 M14 票 + FE 票服务端 diff=0；既有 spec 迁移更新面（Set Me Up/建仓/用户组）归属审计清晰；
5. 前置产物齐备：活体核验票交付（V1~V8 回写 + 修订留痕 + 矩阵复核基线——**已落 T-381**）+ Q1~Q7 终裁归位（核验源/logo 终选与 wordmark 路线/决策项 A·B·C/Tokens 字段集/docker remote 排期确认/v3-flat 终裁/D3 定案——LC-66 离开「待裁」）。**v1.1 归位基线（§7 总账）**：Q1/Q2/Q5/Q7 终裁或执行归位、Q3-A/B 撤销归位、Q3-C/Q4 维持暂行（登记型）——**唯一硬待裁 = Q6（收口窗必裁：对齐 → T-401 / 有意差异 → D 层留痕，材料齐备）**；
6. NFR-P61~P63 / NFR-A1~A3 / NFR-S68~S70 达标归档；`make test`（race）全树绿维持 / `make lint` 0 issues / gofmt 空维持 / 默认并发全量 e2e 绿；SPA gzip 增量 ≤10KB + footprint ≤100MB + check-size ≤100MiB + 冷启动 <2s 三连维持；
7. §2.2 Non-goals 与候选池收编对账完成：滚入 M15+ 项在 ROADMAP 登记（「M14 未纳入项」**备稿已就绪——T-395 v1.1 落笔，收口窗 conductor 启用并按届时 as-built 修订**；**M15+ 主轴候选第一顺位 = AQL + 13 老搜索专程**——本程让位留痕 + Replay REST/D1 busy/成员同型三项滚程理由随段登记）；§1.4 出处义务在全部 M14 票可审计（parity 规格锚点逐条）；
8. 主会话 git tag `m14-done`（对外发布任何制品先经用户确认；PR 化合并沿既定 gitflow 程序；UAT 证据随里程碑 PR 归档）。
