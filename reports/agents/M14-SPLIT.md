# M14 拆票日志（tech-lead，2026-08-31）

> 输入：docs/prd/milestone-14.md v1.0（conductor 已审定——Q1~Q7 暂行维持、票区间 21~26 确认）+ docs/design/console-artifactory-parity.md（§7 矩阵/§8 V1~V8/§9 E1~E7/§10 批次）+ brand/logo 与 brand/package-icons README + reports/agents/M14-PRD.md 下游消费者表 + BOARD M13 节体例（M13-SPLIT.md 先例）。
> 产出：BOARD.md「## M14 票据」节（T-381~T-402，本日志为拆票依据与派发建议，不构成 BOARD 变更）。
> 票数对齐：PRD §1.3 估 21~26，实拆 **22 票**（P0×7 / P1×12 / P2×3；波外条件票 2 张计入 P2）。若 Q6 翻转触发 + D3 立项（Q7）上浮 24，线内；余量 symbol server（T-402）已计入。

## 1. 批次表（全宽 2，沿 M11~M13 口径；波内 area 互斥，跨波同 area/同角色串行；**FE 主轴 web/src 一波一票错峰**）

| 波 | 票 | 角色 | area | dep | 要点 |
|---|---|---|---|---|---|
| **B0** | T-381 [P0] FR-123 活体核验 | qa-engineer（ux 共笔） | parity 规格 + 核验证据 | — | 前置锚；Q1 降级路径内置；三出口 + 低置信三枚图标顺带腿 |
| | T-391 [P1] FR-128 FE 债 | dev-frontend | web/src 仓库表 hover + web/e2e README | — | 零依赖补位（早落使后续 axe/QA 受益） |
| **B1** | T-382 [P0] FR-124.1 D1 抽屉化 | dev-frontend | web/src SetMeUpDialog.tsx | T-381（V1） | smu-* 锚族冻结铁门槛；OIDC 续铸/step-up/命令块不折行 |
| | T-392 [P1] FR-129 docker remote | dev-registry-adapter | internal/adapter/docker + repo | T-363（M13 缝） | 三态齐装收口；Bearer + 缓存 + 降级；与 FE 天然错峰 |
| **B2** | T-383 [P0] FR-124.2 M1 向导单 Dialog | dev-frontend | web/src RepositoryFormPage.tsx | T-381（V2） | 决策项 A；深链兼容；编辑态整页豁免 |
| | T-393 [P1] FR-130 前置规格小票 | reverse-engineer | docs/reverse/nuget.md + npm.md 增量 | — | v3-flat 补锚（K59）+ npm login 端点族实证（K60） |
| **B3** | T-384 [P0] FR-124.3 M3 创建 modal | dev-frontend | web/src UsersPage/GroupsPage | T-381（V6） | 决策项 B；编辑整页 E5 豁免 |
| | T-394 [P1] FR-130 聚合小票包 | dev-go-core | internal/adapter/npm + cmd/ + charts/（会审） | T-393 | npm login + PVC keep + 启动日志 + v3-flat 对照（不动行为） |
| **B4** | T-385 [P0] FR-124.4 L2 行内菜单 | dev-frontend | web/src 四列表行 | T-381（V4）；随 T-384 先后脚 | 菜单无删除断言（E1）；行点击进详情维持 |
| | T-395 [P1] PM Q 终裁联动回写 | product-manager | docs/prd + ROADMAP | T-381 | Q1~Q7 材料上 BOARD + K55~K61 回填 + 未纳入段备稿 |
| **B5** | T-389 [P0] FR-126 logo 六用例 | dev-frontend | web/src/assets/brand + web/public + LoginPage/AppShell + docs-site 槽 | —（Q2 圈定窗 B2 前截止） | 候选 1 工作稿；wordmark path（K56 票内定案） |
| | T-396 [P1] QA 中期回归 | qa-engineer | 测试矩阵 | T-382~T-385,T-392,T-394 | L02~L05 + BE 腿首跑 + M8 spec 迁移复核 + 归属审计 |
| **B6** | T-386 [P1] FR-125.1 Tokens 真身 | dev-frontend | web/src Tokens 路由 | T-381（V6→Q4） | PlaceholderPage 退役；零新端点 |
| | T-397 [P1] 文档 A 接入/运维 | tech-writer | docs/user/ | T-392,T-394 | docker remote 指南 + npm login + helm keep + api-ref |
| **B7** | T-390 [P1] FR-127 图标 30 枚接线 | dev-frontend | web/src/assets/pkg-icons + 四消费点 + lib/repos.ts | T-382,T-383,T-381 | npm/go 转 path；门控三件套；暗底 ≥3:1（单票波） |
| **B8** | T-387 [P1] FR-125.2 L1 列选/刷新 | dev-frontend | web/src 仓库/审计工具栏 | FE 链随 T-385（共写 RepositoriesPage） | per-page localStorage 持久 |
| | T-398 [P1] 文档 B console parity + 品牌 | tech-writer | docs/user/ console.md + FAQ | T-382~T-386,T-389,T-390 | 形态迁移对照表；L1 小节随 T-387 合入后补笔 |
| **B9** | T-388 [P2] FR-125.3/4 F2+N2 | dev-frontend（ux 线稿前置） | web/src EmptyState + AppShell 侧栏 | T-381（V5 降级出口） | 不引第三方插画库（单票波） |
| **B10** | T-399 [P1] release 烟测 + UAT | release-engineer | deploy/ + charts/ + CD 链 | 全部实现票 | Chart 版本收口；PVC keep UAT 腿；SPA/favicon embed 核验 |
| **B11** | T-400 [P0] QA 终验 | qa-engineer | 全量矩阵 | 全部 + T-399 | L01~L18 + **L16 矩阵逐格终评** + DoD 八条 + m14-done |
| 波外 | T-401 [P2·条件 Q6] v3-flat 翻转 | dev-go-core | adapter/nuget | T-393,T-394 对照不一致 + Q6 终裁=对齐 | 不一致才触发；否则 D 层留痕 |
| | T-402 [P2·条件 余量] symbol server 四承 | dev-go-core | adapter/nuget symbol 子域 | P0/P1 收官 + 余量 | T-357/T-379 延续 |

优先级统计：P0×7（T-381/382/383/384/385/389/400）；P1×12（T-386/387/390/391/392/393/394/395/396/397/398/399）；P2×3（T-388 + 条件 2）。B7/B9 为单票波（FE 链尾 + 非票池耗尽所致，非阻塞）。

## 2. 依赖图

```
T-381(核验锚·B0) ──┬─→ T-382(D1·B1) ─→ T-383(M1·B2) ─→ T-384(M3·B3) ─→ T-385(L2·B4)   [批 1 FE 串行链]
                   │        │              │                              │
T-391(FE 债·B0 零依赖) │        └──────────────┴──────────────┬───────────┘
                   │                                       ↓
T-363(M13 缝) ─→ T-392(docker remote·B1) ──────────────→ T-389(logo·B5) ─→ T-386(Tokens·B6) ─→ T-390(图标·B7) ─→ T-387(L1·B8) ─→ T-388(F2+N2·B9)
                   │                                                                                    │
T-393(reverse 规格·B2) ─→ T-394(FR-130 聚合·B3) ──┬─→ T-401(条件 Q6)                                      │
                                                 ↓                                                      ↓
T-395(PM 裁定·B4，dep T-381)          T-396(QA 中期·B5，dep 批1+T-392+T-394)          T-397(docs A·B6)  T-398(docs B·B8)
                                                                                                        ↓
                                              T-399(release+UAT·B10) ─→ T-400(QA 终验·B11) ─→ m14-done
```

两条主线：FE 形态链（B0~B9 十波——本程决定性路径）+ BE 副线（B1 docker remote、B2/B3 规格→小票包，B5 前收官并入 QA 中期）。零依赖穿插票：T-391（已排 B0 补位）、T-392（BE 首航）。qa 三腿（B0/B5/B11）与 tech-writer 两腿（B6/B8）同角色跨波串行。

## 3. 风险登记（与 BOARD 同文）

| # | 风险 | 缓解 |
|---|---|---|
| R1 | **Q1 核验源终裁时点**——须 B1 派发前（建议与 Q2/Q3 同窗）；V1/V2/V4/V6 落定前批 1 细节断言不得转正 | T-381 AC3 降级路径内置（t226 容器〔T-228 保留栈 docker start〕→ 外部活体 → ③凭标注推进）；批 1 手势级断言均高/中高置信不阻塞；降级清单 BOARD 留痕 |
| R2 | **Q2 logo 圈定窗截止 B2 前波**——T-389（B5）以候选 1 工作稿起步，窗后未推翻即转正 | 资产参数化单点引用（web/src/assets/brand/，换稿=换文件零返工）；wordmark 路线 K56 票内定案（Inter OFL path + license 文本 vs 手工勾画） |
| R3 | **Q3 决策项 A/B/C 终裁**——影响 T-383 深链形态/T-384 编辑整页豁免/T-382 Deploy 保持居中 | 暂行照 parity §10 批 1 建议实现（spec 作者 + PM 认同）；终裁随 Q1/Q2 同窗 B1~B2；翻转面小（决策项 C 本就「保持不动」为主） |
| R4 | **锚族冻结 + 新锚入册（web/src 改动铁门槛）**——smu-*/login-*/app-nav 零改名；新面（L2 菜单/Tokens/列选器）新锚入册 | 每张 FE 票 AC 内嵌 anchor-audit 0 断链断言；D1 壳替换不动 smu-* 锚族（保留清单六项逐一 AC）；侧栏品牌区不动 app-nav-brand 结构；console-ux 锚册 + ledger 维持 |
| R5 | **E1~E7 豁免复核点位**——L2 菜单混入删除即违 E1（豁免倒退=缺陷） | T-385 AC1 菜单无删除**缺席断言**（V4 若 Artifactory 含删除亦不跟进——安全设计不倒退 §1.4-3）；T-400 AC2 终验逐条复核七条 + 三出口（V5 降级/V7 关闭/E7 再议）落档 |
| R6 | **四闸门 + axe 双主题 + SPA 预算维持**——FE 十票全量合入条件 | 票票 AC 内嵌（typecheck/assert:tokens/anchor ledger/lint + axe serious=0）；30 枚图标 + logo 资产计入 SPA gzip 增量 ≤10KB（NFR-P61）；FE 票服务端 diff=0 复核沿 M8 T-235 先例 |
| R7 | **活体核验降级路径与零静默升格** | 核验源不可得项维持「中/低置信 + 以核验为准」标注；grep 附注清单与核验结论一一对应（T-381 AC1）；置信度回写=改契约须 §0 修订留痕 |
| R8 | **D1 抽屉化重构的既有兼容**——M8 e2e Set Me Up spec 断言现壳形态 | 既有 spec 走「迁移更新非反转」（行为语义断言——铸币/step-up/OIDC 续铸——全量保留复测）；更新面 100% 归属 M14 豁免票（T-396/T-400 归属审计） |
| R9 | **FE 串行链 10 票工期**——web/src 一波一票（B0~B9）为默认纪律 | 压缩选项两处 conductor 裁量：①D1+M1 并波（重载体文件不同——PRD §1.3 明示可并行，省一波）；②L1+FE 债合票（M13 ⑦ 先例，省一波）；T-399 可前移 B9（F2+N2 P2 尾票不阻塞烟测面） |
| R10 | **条件票触发态**——Q6 翻转（T-401）/symbol 余量四承（T-402）/N2 V5 降级（T-388 票内出口）/D3 Q7 立项（不占票号——用户立项才开后端域票） | 全触发上浮 24（PRD 线内）；未触发项 BOARD 留痕非 DoD 缺口（DoD#1 条款） |

## 4. 首派建议（B0/B1）

- **B0（即派）**：**T-381**（qa-engineer 执行腿 + ux-designer 共笔——核验源 Q1 暂行①t226 容器恢复优先；先试 `docker start`，不可得即走②外部活体/官方文档，再不可得走③降级并当日 BOARD 留痕——**不恋战，批 1 不等**）+ **T-391**（dev-frontend，FE 债——零依赖，hover 对比度早落使后续每张 FE 票 axe 干净）。
- **B1（B0 任一收口即接续）**：**T-382**（D1 抽屉化——批 1 最高优先；派单附 V1 结论或降级附注 + smu-* 锚族保留清单）+ **T-392**（dev-registry-adapter——docker remote 首航；派单附 K54 判定与 T-363 as-built 缝索引）。
- 插空建议：**T-395**（PM Q 终裁材料）dep 仅 T-381——**宜早**（Q1/Q2/Q3 三终裁窗 B1~B2，材料越早上 BOARD 越从容；不阻塞实现票，B4 槽位可提前）。
- 派单纪律（全程）：no-git 条款维持；FE 票逐张附「锚族冻结 + 新锚入册 + 四闸门 + 服务端 diff=0」四件套条款；T-382 附 Q3-决策项 C 暂行（Deploy 保持居中不动）；T-383 附 Q3-A 深链形态；T-389 附 Q2 工作稿口径；T-394 附「v3-flat 对照不动行为——翻转走 T-401」红线。

## 5. 估票对比与取舍留痕（对 PRD §1.3 分票提示逐条）

| PRD 提示 | 拆票处置 | 取舍理由 |
|---|---|---|
| FR-123 拆 1~2 票 | **1 票**（T-381，qa 执行腿 + ux 共笔同票） | PRD 明示「可同票」；拆两票引入共笔时序耦合（核验结论→回写两段交接），一票内自闭环；共笔署名在票面 |
| FR-124 拆 4 票 | **4 票**（T-382~T-385） | 照 PRD；串行序 D1→M1→M3→L2（L2 与 M3 共写 Users/GroupsPage 先后脚；L2 与 T-387 共写 RepositoriesPage 先后脚）；D1+M1 并波为 conductor 裁量选项非默认 |
| FR-125 拆 3 票 | **3 票**（Tokens / L1 / F2+N2 合并） | 照 PRD；F2+N2 均 P2 轻量合票，N2 的 V5 降级出口内置票内 |
| FR-126 拆 1~2 票 | **1 票**（T-389 含 path 化 + docs-site 槽） | 六用例 + path 化强耦合（资产单点），拆开反而资产双源；docs-site navbar 槽为配置位小改随票（票面注明 conductor 可转 tech-writer） |
| FR-127 一票 | **1 票**（T-390） | 照 PRD；四消费点跨 D1/M1 共写面 → 排 B7（两者合入后） |
| FR-128 一票 | **1 票**（T-391） | 照 PRD；**前移 B0 补位**（零依赖，hover 早落优化后续 axe 基线） |
| FR-129 拆 1~2 票 | **1 票**（T-392 含 dind 全链） | 照 PRD「可同票」；M13 T-363 单票同族先例 |
| FR-130 拆 2~3 票 | **2 票**（T-393 规格前置 + T-394 聚合实现）+ T-401 条件 slot | conductor 指令「聚合一票（dev-go-core）」→ 实现面单票 T-394；但 clean-room 规格先行纪律（本角色章程：协议票必须 dep 规格票）要求 v3-flat 补锚 + npm login 端点实证（K59/K60）先落 docs/reverse → reverse 小票 T-393 独立（PRD §1.3 亦列「v3-flat 补锚〔reverse〕」为独立项）；**聚合票省出的波次抵消规格票**，总波数不变 |
| QA 两票 | **3 票**（T-381/T-396/T-400） | PRD「QA 两票」指回归腿（中期+终验）；FR-123 执行腿也是 qa——三腿跨波串行不冲突 |
| tech-writer 一~两票 | **2 票**（T-397 接入运维 B6 / T-398 console+品牌 B8） | 解锁时序天然两段：BE 副线文档 B3 后即备、console 截图须终形态（批 2/品牌合入后）；一票会拖到 B8 尾部且单票过重 |
| release 一票 / PM 一票 | T-399 / T-395 | 照 PRD |
| 条件票 slot | T-401/T-402 占号；N2 内置 T-388；**D3 不占号** | D3 立项（Q7）需先出后端依赖解析域票——非 M14 可派实体，登记风险⑩即可 |

## 6. 歧义与口径登记（不阻塞拆票，交 conductor/PM）

1. **FR-130.1/130.2 优先级 PRD 内部双值**：§4 行文标 P2，DoD#1 列 P1。票面取 **P1**（DoD 口径——收敛口径以 DoD 为准），130.3/130.4 腿 P2 性质票内注明（M13 T-370 双值先例）。PM 下版统一。
2. **helm PVC keep owner 口径**：PRD 下游消费者表列 release-engineer，conductor 拆票指令「FR-130 聚合一票 dev-go-core」。处置：**注解面归 T-394**（dev-go-core，charts/ 腿为文件地图协作点——release-engineer 会审），**Chart 版本 bump 与 UAT 验证腿归 T-399**（release，PRD §8-10「PVC keep 在 UAT 链验证」）。conductor 可改派，票面 area/dep 不变。
3. **FR-124 D1/M1「可并行」vs FE 错峰指令**：PRD §1.3 允许并波（重载体文件不同），conductor 指令「FE 主轴错峰串行」。默认错峰（本拆票），并波登记为裁量选项（R9）。
4. **Q4 Tokens 字段集定案时点**：V6 核验（T-381，B0）→ Q4 终裁 → T-386（B6）消费——时序充裕；若 Q4 终裁晚于 B6 派发，T-386 以暂行字段集起步、终裁后票内收口（票面已注）。
5. **T-389 docs-site 腿**：navbar logo 配置槽为配置位小改，随 FE 票（资产同源）；docs-site 重建产物（build/.docusaurus）为生成物，提交面以配置与源资产为限——沿仓库现状惯例。
6. **「四承」计数口径**：PRD 称 symbol server 余量「M12→M13→M14 四承」（T-357→T-379→T-402）——照 PRD 原文引用，不作二次解释。
