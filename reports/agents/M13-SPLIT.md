# M13 拆票日志（tech-lead，2026-08-30）

> 输入：docs/prd/milestone-13.md v1.0（合入 develop）+ ROADMAP M13 立项段 + BOARD M12 节遗留登记（「M12 未纳入项」+ T-356 终验维持登记四项）。
> 产出：BOARD.md「## M13 票据」节（T-358~T-380，本日志为拆票依据与派发建议，不构成 BOARD 变更）。
> 票数对齐：PRD §1.3 估 21~26，实拆 **23 票**（P0×7 / P1×12 / P2×4；波外条件票 3 张计入 P2）。若 Q2/Q3/Q5 条件腿全触发上浮 26，恰在线内上限。

## 1. 批次表（全宽 2，沿 M11/M12 口径；波内 area 互斥，跨波同 area/同角色串行）

| 波 | 票 | 角色 | area | dep | 要点 |
|---|---|---|---|---|---|
| **B0** | T-358 [P0] webhook.md | reverse-engineer | docs/reverse/webhook.md | — | 官方文档基线特例（取证路径）；36 事件 + 覆盖界 + Q4 取证腿 |
| | T-359 [P0] ADR-0041 | architect | DECISIONS.md | —（与 T-358 并行，wire 细目对齐） | outbox/投递/SSRF/权限门/第 19 槽建议 |
| **B1** | T-360 [P1] ADR-0042 | architect | DECISIONS.md | —（同 area 随 T-359 串行） | D-F2 迁移方案（K53） |
| | T-361 [P1] FR-121 de-flake | devops-engineer | .circleci/ + Jenkins + 逃逸点 _test.go | — | race 全树一次绿×2；PRD 明示独立可首波并行 |
| **B2** | T-362 [P0] FR-114 订阅+织入 | dev-go-core | internal/webhook + httpapi（**窗口独占**） | T-358,T-359 | 主轴上；36 注册 + outbox 旁路 + 第 19 槽三缝 |
| | T-363 [P0] FR-116 remote | dev-registry-adapter | adapter/docker /v2 remote 链 + repo | T-342 | D-5 翻转点；Bearer 认证 + 缓存 + Q5 评估腿 |
| **B3** | T-364 [P0] FR-115 投递引擎 | dev-go-core | internal/webhook 投递链（串行随 T-362） | T-362 | 主轴下；kill -9 幸存/退避/死信/签名/SSRF |
| | T-365 [P1] FR-116 virtual | dev-registry-adapter | ForRepoType 面 + repo virtual（串行随 T-363） | T-363 | 成员聚合；混仓 400 维持 |
| **B4** | T-366 [P1] FR-115 FE+消费者 | dev-frontend | web/src | T-364 | 四闸门同构 + dogfood Jenkins 条件腿 |
| | T-367 [P1] FR-117 引擎缝 | dev-registry-adapter | internal/remote + repo + helm 消费点（同域先后脚） | T-363 | chartsBaseUrl/_external 落盘（T-342 评估结论兑现） |
| **B5** | T-368 [P1] FR-118 旋钮两枚 | dev-go-core | internal/config + httpapi 门 + storage cron | — | 两旋钮聚合一票；L14 断言开关化 |
| | T-372 [P1] FR-122.1 trash 树节点 | dev-frontend | web/src（串行随 T-366） | —（M12 T-352 在案） | 先改册后实现（console-m8 推翻条款） |
| **B6** | T-369 [P1] FR-119.1 D8 翻转 | dev-go-core | adapter/conan | T-348 | 整树删；M12 断言反转豁免票 |
| | T-370 [P0] FR-120 文面裁定包 | product-manager | docs/prd + nuget.md + artifact-operations.md | —（**随时可动，宜早插空**） | D-10 材料上 BOARD + flat/L31 落笔 |
| **B7** | T-371 [P1] FR-119.2 D-F2 迁移 | dev-go-core | adapter/conan（串行随 T-369） | T-360,T-369 | 存量迁移幂等零损 + settings 恢复 |
| | T-375 [P1] 文档主增量 | tech-writer | docs/user/ | T-362~T-368 | webhook 指南/HelmOCI remote/旋钮/api-ref/FAQ |
| **B8** | T-373 [P1] QA 中期回归 | qa-engineer | 测试矩阵 | T-362~T-367 | webhook L02~L09 + HelmOCI L10~L13 首跑 + 反转预核实 |
| | T-374 [P2] FR-122 docs 尾巴 | tech-writer | console-m8 侧栏段 + npm.md（串行随 T-372/375） | T-372 | 侧栏清单 15 对齐 + 尾斜杠注记 |
| **B9** | T-376 [P1] release + UAT 首跑 | release-engineer | deploy/ + charts/ + CD 链 | 全部实现票（T-362~T-372） | **UAT 随里程碑 PR 首跑必做**（M12 教训） |
| **B10** | T-377 [P0] QA 终验 | qa-engineer | 全量矩阵 | 全部 + T-376 | L01~L24 + DoD 八条 + m13-done 门 |
| 波外 | T-378 [P2·条件 Q3] | dev-go-core | adapter/nuget | T-370 终裁=翻转 + 余量 | D-10 → 409 逐字 |
| | T-379 [P2·条件 Q2] | dev-go-core | adapter/nuget symbol 子域 | P0/P1 收官 + 余量 | M12 T-357 承接 |
| | T-380 [P2·条件 Q5] | dev-registry-adapter | adapter/docker remote 本体 | T-363 K54 判定≈0 + BOARD 留痕 | docker remote 顺车 |

优先级统计：P0×7（T-358/359/362/363/364/370/377）；P1×12；P2×4（T-374 + 条件 3）。主轴前置双产物置顶 B0（规格票先行——官方文档基线条款 PRD §1.4-1）；裁决/裁定承载票：Q3=T-370（+T-378 条件）、Q4=T-358 取证腿、Q6=T-358 标注 + T-362 暂行实现。

## 2. 依赖图

```
T-358(webhook.md) ──┬─→ T-362(FR-114 订阅+织入·独占) ─→ T-364(FR-115 投递) ─→ T-366(FR-115 FE+消费者) ─┐
T-359(ADR-0041) ────┘                                                                    │
T-360(ADR-0042) ──┬─→ T-371(D-F2 迁移) ←─ T-369(D8 翻转) ←─ T-348(M12 规格双证)         │
T-361(de-flake·零依赖)    T-370(PM 文面·零依赖宜早) → T-378(条件 Q3)                     │
T-342(M12) ─→ T-363(HelmOCI remote) ─┬─→ T-365(virtual)                                 │
                                      └─→ T-367(chartsBaseUrl/_external 缝) → T-380(条件 Q5)
T-368(旋钮·零依赖)    T-372(trash 树节点·零依赖) ─→ T-374(运维尾巴 docs)                  │
                       ↓                                                                 ↓
T-373(QA 中期·dep B2~B4 主体) → T-375(docs 主增量) → T-376(release+UAT 首跑) → T-377(QA 终验·P0) → m13-done
波外条件：T-378(Q3) / T-379(Q2·余量) / T-380(Q5·K54)
```

三条主线：Webhook（B0→B2→B3→B4，主轴）、HelmOCI 三态（B2→B3→B4，D-5 翻转点）、conan 翻转与迁移（B1/B6→B7）。零依赖穿插票：T-361、T-368、T-370、T-372（宽度补位候选，按 conductor 派发节奏；T-370 宜早——D-10 裁定材料上 BOARD）。web/ 串行链 T-366→T-372 为 FE 面唯一互斥约束；tech-writer 串行链 T-375→T-374。

## 3. 风险登记

| # | 风险 | 缓解 |
|---|---|---|
| R1 | **Q4 webhook 槽档位终裁时点**——须在 T-362 AC3 三缝断言（B2）前收口 | T-358 AC2 官方 license 标注取证上 BOARD；conductor B2 前安排裁决窗；暂行 pro+/feature-int 不阻塞实现主体，翻转仅动槽位表 |
| R2 | **Q6 事件覆盖界**（36 类型注册休眠 vs 裁剪） | 按暂行「注册休眠」实现（T-362 AC2 注明不伪造触发）；终裁若裁剪仅动注册表与文档，面小不返工 |
| R3 | **webhook.md 取证路径特例**——反编译集合无该 addon，官方文档为唯一基准；文档与 inv-4 锚点可能冲突 | 效力序=用户裁决>规格票>ADR>PRD 暂行（PRD §4 约定）；T-358 置信度标定 + 分歧上 BOARD；envelope 按「数据契约非代码」红线（ADR-0001） |
| R4 | **dev-go-core 五票串行负载**（T-362/364/368/369/371） | B2/B3/B5/B6/B7 严格错峰一票一波；T-362 窗口独占；router/slots/main.go 接线统一交 conductor（M11 纪律）；必要时 T-369/T-371 可改派 dev-registry-adapter（⑨ 备裁） |
| R5 | **dev-registry-adapter 三票链**（T-363→T-365→T-367）+ FR-117 同域先后脚 | B2/B3/B4 串行已内建；internal/repo 与 adapter/helm 共写面错峰；T-367 引擎缝回归面大（M3 SSRF 五参数）——AC3 硬门 |
| R6 | **T-367 schema 定座**——charts_base_url per-protocol 槽涉共享 schema/no-inert-fields 规则（T-342 §5 D-2①「须 architect 定座」） | 票内定案 + 留痕；必要时 architect 会审（不单列 ADR，避免 B0 加票）；半落（回显不生效字段）为明确反例禁止 |
| R7 | **dogfood Jenkins 条件腿**（T-366 AC2）dep 用户环境（VM 栈在位） | 容器接收器腿（httpbin/脚本 + 故障注入）为下限；不可得则容器腿 + BOARD 留痕非 DoD 缺口（PRD FR-115-AC5 既定） |
| R8 | **UAT 首跑时序**（T-376）——M12 T-355 未执行教训 | T-376 AC2 与 conductor 里程碑 PR 收口协同条款（时序写进票面）；docs 票（T-374/T-375 若后合入）以 PR 分支复跑 docs 烟测腿补偿 |
| R9 | **de-flake 边界**——race 全树转绿不得静默放宽 | T-361 AC3 escape 点逐处注释/留痕 grep 清单；预算臂语义独立标注保留（FR-121.3） |
| R10 | **条件票触发态**——Q2/Q3/Q5 全触发票数上浮 26 | 恰为 PRD §1.3 上限；未触发项 BOARD 留痕非 DoD 缺口（DoD#1 条款） |
| R11 | **断言反转/布局对齐核实链**——三处（D8/folderDownload/D-F2）PRD 回写核实归 QA | T-373 预核实（合入后补腿）+ T-377 终验 AC1 收口；豁免票归属 100%（git diff m12-done..HEAD 审计） |

## 4. 首派建议（B0/B1）

- **B0（即派）**：**T-358**（reverse-engineer，webhook.md 新建——解锁主轴全链；官方文档取证为唯一基准，无预研缺口但取证量大，宜即开工）+ **T-359**（architect，ADR-0041——outbox/投递/SSRF 架构定案；与 T-358 并行、wire 细目落 Accepted 前对齐）。两票 area 互斥（docs/reverse/ vs DECISIONS.md），零依赖零交织。
- **B1（B0 任一票收口即接续）**：**T-360**（architect，ADR-0042——D-F2 迁移方案解锁 T-371；docs-only 同 area 随 T-359 串行）+ **T-361**（devops-engineer，FR-121 de-flake——PRD 明示「独立可首波并行」，race 全树转绿尽早落避免拖到收口期与终验挤压；B0 宽度富余亦可前移）。
- 插空建议：**T-370**（PM 文面裁定包）零 area 冲突随时可动——**宜早派**（D-10 对照材料上 BOARD 越早，Q3 终裁与 T-378 条件票判定越从容；不阻塞任何实现票）。
- 派单纪律（全程）：no-git 条款（共享树禁分支/禁提交，conductor 收口）；T-362 派单注明窗口独占 + 接线交 conductor；T-366/T-367 派单附 Q4/Q6 暂行口径与终裁时点；涉 SSRF/代理面票（T-364/T-367）措辞平实化（T-313 教训）；webhook 域票附「官方文档为唯一行为基准」工作方式条款（PRD §1.4-1）。

## 5. PRD/BOARD 微调与歧义登记（不直改，交 conductor/PM；不阻塞拆票）

1. **helm.md 增量段归属表述不一致**：PRD §1.3 前置④「随 FR-117 票」vs §5.6 K51「随 FR-116 票」。拆票处置：remote 链增量随 T-363（K51 口径）、chartsBaseUrl/_external 增量随 T-367（§1.3 口径）——两段各自 mini 规格随票（T-330 模式）。交 PM 下版统一表述。
2. **ADR-0042「视 Q 裁定」指涉不明**：Q1~Q7 无 D-F2 对应项。按 DoD#4 字面立票（T-360，B1）；若 conductor 判定可并入 T-371 票内 mini-ADR 则 T-360 撤销（票数 23→22，仍在线内）。
3. **conan 小票 owner 口径**：PRD 下游消费者表列 dev-go-core（与 M11 T-308 先例一致），与 M12 T-340（dev-registry-adapter）异派。拆票按 PRD 口径派 dev-go-core（T-369/T-371）；conductor 可改派，票面 area/dep 不变（备裁登记，见 BOARD 风险⑨）。
4. **FR-122.2 console-m8.md 写入者边界**：文件地图 docs/design 归 architect/ux-designer，PRD 下游消费者列 tech-writer（收官清扫）。拆票按 PRD 口径派 tech-writer（T-374）并在票面注明设计册文面协作点；conductor 可改派 ux-designer（area/dep 不变）。锚册 ledger 腿与 FE 协作（复跑 anchor-audit）。
5. **T-370 优先级双值**：PRD「P0 裁定动作 / P2 落笔」。票面取 P0（裁定阻塞 DoD#4 的 Q3 归位），落笔段 P2 性质票内注明——不构成调度歧义。
6. **ROADMAP M13 立项段**与 PRD v1.0 一致无阻塞分歧；「M12 未纳入项」滚程去向与 §2.2 对账无缺——收口时按 DoD#7 建「M13 未纳入项」段（PM 职责，非本拆票动作）。
