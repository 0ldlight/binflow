# M12 拆票日志（tech-lead，2026-08-28）

> 输入：docs/prd/milestone-12.md v1.0（PR #15 合入版）+ ROADMAP M12 立项段 + BOARD M11 节遗留登记。
> 产出：BOARD.md「## M12 票据」节（T-333~T-357，本日志为拆票依据与派发建议，不构成 BOARD 变更）。
> 票数对齐：PRD §1.3 估 22~28，实拆 **25 票**（P0×8 / P1×14 / P2×3；波外条件票 T-357 计入 P2）。若 Q2/Q5/Q6 条件腿全触发上浮 ≤27，仍在线内。

## 1. 批次表（全宽 2，沿 M11 口径；波内 area 互斥）

| 波 | 票 | 角色 | area | dep | 要点 |
|---|---|---|---|---|---|
| **B0** | T-333 [P0] ADR-0040 | architect | DECISIONS.md | — | fail-open 队列语义定案（K43；视 Q3 附 ADR-0041） |
| | T-334 [P0] nuget.md | reverse-engineer | docs/reverse/nuget.md | — | as-built + 增量出处双段；T-304 §1.1 直取 |
| **B1** | T-335 [P0] repo-operations.md | reverse-engineer | docs/reverse/repo-operations.md | — | copy/move + 归档族 mini 规格 + Q4 取证腿 |
| | T-336 [P0] FR-108 瘦身 | dev-go-core | internal/ embed 面 | — | 裁定⑤；footprint 红→绿；零依赖快赢 |
| **B2** | T-337 [P0] FR-103 NuGet v2 | dev-go-core | internal/adapter/nuget（窗口独占） | T-334 | 裁决①主线上半；v2 全集 + 409 重复臂 |
| | T-338 [P1] FR-107 fail-open | dev-go-storage | internal/storage | T-333 | 裁决③；停机窗 + 队列 + 排空对账 |
| **B3** | T-339 [P0] FR-105 copy/move | dev-go-storage | internal/storage + httpapi | T-335 | 新主轴核心；索引联动 |
| | T-340 [P1] FR-110 conan+cargo 收尾 | dev-registry-adapter | adapter/conan + adapter/cargo | — | D-F（裁决承载④）+ forceConanAuth + cargo 409 |
| **B4** | T-341 [P0] FR-104 NuGet v3 | dev-go-core | internal/adapter/nuget（串行随 T-337） | T-334,T-337 | 裁决①主线下半；search 代理 + 动态解析 |
| | T-342 [P1] FR-109 HelmOCI | dev-registry-adapter | adapter/helm（ForRepoType 面） | — | T-320 承载；P2 段并入 + D-3 评估腿 |
| **B5** | T-343 [P1] FR-105 归档族 | dev-go-storage | storage 归档流 + httpapi | T-335,T-339 | archive!/ + zip + exploded（400 断言反转） |
| | T-344 [P1] FR-111 MUI 批三 | dev-frontend | web/src | — | 交互零变化四闸门；web/ 本波独占 |
| **B6** | T-345 [P1] FR-106 Trash BE | dev-go-storage | 删除 seam + trash cron + httpapi + 槽 | T-339 | T-330 转正；mini 规格随票 + Q3 取证 |
| | T-346 [P1] FR-113 repo config+auth | dev-go-core | internal/repo + internal/auth | — | socketTimeoutMillis + byHash 枚举 + auth 尾巴 |
| **B7** | T-347 [P1] FR-112.1 arch 回写 | architect | docs/design/architecture.md | **T-346** | §15.4.1 终态回写时序耦合（PRD §1.3） |
| | T-348 [P1] FR-112.2/3 规格回写 | reverse-engineer | docs/reverse/cargo.md + conan.md | T-340 | 升置信 + D-G/D-H 校验 |
| **B8** | T-349 [P2] FR-113 storage 尾巴 | dev-go-storage | internal/storage（MPU token + env 启动链） | —（同 area 随 T-338 后） | token 窄域化 + 拒启序 |
| | T-350 [P1] FR-113.6 CI runner | devops-engineer | .circleci/ + Jenkins + e2e 编排 | — | K46 de-flake 定案 |
| **B9** | T-351 [P1] QA 中期回归 | qa-engineer | 测试矩阵 | B2~B5 主体 | L02~L14 首跑 + 三处反转预核实 |
| | T-352 [P1] FR-106 Trash FE | dev-frontend | web/src | T-345（web/ 串行随 T-344 后） | 回收站最小面 |
| **B10** | T-353 [P2] FR-113.2 web 表单 | dev-frontend | web/src 仓编辑器 | T-346,T-352 | deb/rpm 策略键表单 |
| | T-354 [P1] 文档五类 | tech-writer | docs/user/ | 对应域票合入 | NuGet v2/操作族/回收站/HelmOCI/FAQ |
| **B11** | T-355 [P1] release 烟测+UAT | release-engineer | deploy/ + charts/ + CD 链 | 全部实现票 | 新配置键四部署面接线核验 |
| **B12** | T-356 [P0] QA 终验 | qa-engineer | 全量矩阵 | 全部 + T-355 | L01~L35 + DoD 八条 + m12-done 门 |
| 波外 | T-357 [P2·条件 Q2] symbol server | dev-go-core | adapter/nuget 子域 | T-337,T-341 + 余量 | mini as-built 规格随票 |

优先级统计：P0×8（T-333/334/335/336/337/339/341/356）；P1×14；P2×3。四裁决/裁定承载票全部位于 B0~B3 置顶区（裁决①=T-334→337→341；裁决③=T-333→338；裁定⑤=T-336；D-F=T-340）。

## 2. 依赖图

```
T-333(ADR-0040) ──→ T-338(fail-open) ─────────────────────────┐
T-334(nuget.md) ──→ T-337(v2 大票·独占) ──→ T-341(v3 search) ──┤
T-335(repo-ops.md) → T-339(copy/move) ──┬→ T-343(归档族)      ┤
                                        └→ T-345(Trash BE) → T-352(Trash FE) ─→ T-353(web 表单)
T-336(瘦身·零依赖)   T-340(conan/cargo·零依赖) ──→ T-348(规格回写)
T-342(HelmOCI)       T-344(MUI 批三) ──(web/ 串行)──→ T-352 → T-353
T-346(repo config+auth) ──→ T-347(arch 回写·终态时序)
T-349(storage 尾巴)  T-350(CI runner)          │
                     ↓                        ↓
        T-351(QA 中期·dep B2~B5) → … → T-354(docs) → T-355(release) → T-356(QA 终验·P0) → m12-done
T-357(symbol server·条件 Q2——T-337/341 收官且余量足)
```

三条主线：NuGet（B0→B2→B4，裁决①）、生命周期域（B1→B3→B5/B6→B9/B10）、fail-open（B0→B2）。web/ 串行链 T-344→T-352→T-353 为 FE 面唯一互斥约束。零依赖穿插票：T-336、T-340、T-342、T-344、T-350（宽度补位候选，按 conductor 派发节奏）。

## 3. 风险登记

| # | 风险 | 缓解 |
|---|---|---|
| R1 | **Q3 trashcan 档位终裁时点**——须在 T-345 门控断言（AC3/L17）前收口 | T-345 AC1 mini 规格取证上 BOARD；conductor 在 B6 前安排裁决窗（暂行 pro+/feature-gov 不阻塞实现主体） |
| R2 | **Q4 操作族 license 门**——T-335 取证若发现门控则 T-339/T-343 需补槽三缝 | T-335 AC2 明确取证腿（folderDownload 计流量面尤查）；暂不门控不阻塞，翻则补票面不大 |
| R3 | **nuget.exe 活体可得性**（T-337 AC3） | curl 等价 + BOARD 留痕路径（M11 conan 1.x 先例）；dotnet 8 容器腿为下限 |
| R4 | **internal/storage 共写面**——T-338/T-339/T-343/T-345/T-349 五票同 area | 严格波次串行（B2/B3/B5/B6/B8 已错峰）；router/slots/main.go 接线统一交 conductor（M11 纪律延续） |
| R5 | **T-347↔T-346 时序耦合**——§15.4.1 remote 字段须 FR-113.1 终态回写 | B6→B7 排程内建；T-346 延期则 T-347 §23 段先落、§15.4.1 段顺延（票面已注明） |
| R6 | **dev-go-core 负载**——T-336/T-337/T-341/T-346（+T-357 条件） | B1/B2/B4/B6 错峰 + T-337 窗口独占条款；T-336 可再前移穿插 |
| R7 | **web/ 串行链拖尾**——T-344→T-352→T-353 三连若 FE 带宽受限影响终验 | T-353 体量极小（P2），可并入 T-352 由 conductor 裁量（FR 边界留痕） |
| R8 | **瘦身与冷启动权衡**（FR-108.3） | 归因清单先行 + <2s 三连硬门；不得以启动时延换内存 |
| R9 | **fail-open 回归面**——M6 H12~H15 迁移序列 + binstore 三链 | T-338 AC2 硬门；ADR-0040 定案前不开工（dep 已建） |
| R10 | **条件腿触发态**——Q2/Q5/Q6 全触发则票数上浮 | 上限 27 仍在 PRD §1.3 区间；未触发项 BOARD 留痕非 DoD 缺口 |

## 4. 首派建议（B0/B1）

- **B0（即派）**：**T-333**（architect，ADR-0040——解锁 FR-107 全链，docs-only 零冲突）+ **T-334**（reverse-engineer，nuget.md——解锁 P0 主线 FR-103/104；T-304 §1.1/§4.1/§7 出处直取，规格活化即可开工，无预研缺口）。两票 area 互斥（DECISIONS.md vs docs/reverse/），零依赖零交织。
- **B1（B0 任一票收口即接续）**：**T-335**（repo-operations.md——解锁 FR-105/106 生命周期域全链）+ **T-336**（FR-108 瘦身，P0 零依赖快赢——资源门红→绿尽早落，避免拖到收口期与终验挤压）。
- 穿插备位：宽度空窗时 T-340（conan/cargo 收尾，零依赖小票）或 T-342（HelmOCI）可提前，注意 T-342 与 T-340 同角色（dev-registry-adapter）不并行。
- 派单纪律（全程）：no-git 条款（共享树禁分支/禁提交，conductor 收口）；T-337 派单注明窗口独占；T-345 派单附 Q3 暂行口径与终裁时点；涉 SSRF/代理面票措辞平实化（T-313 教训）。

## 5. PRD/ROADMAP 微调登记（不直改，交 conductor/PM）

1. **FR-110 拆票粒度**：PRD §1.3 建议「拆 1~2 票」，实拆 1 票（T-340，conan+cargo 同角色异包、票内串行）。执行中若体量超预期可拆 T-340a/T-340b（conductor 裁量）。
2. **FR-113 拆票粒度**：PRD §1.3 建议「拆 2~3 票」，实拆 4 票（T-346/T-349/T-350/T-353）——原因：web 表单需 dev-frontend 角色（BE 票不可承载）、auth 与 storage 尾巴分属 dev-go-core/dev-go-storage 两角色。属角色边界驱动的细化，总数仍落 §1.3 区间。
3. **nuget v2 owner 口径**：PRD 下游消费者表将 internal/adapter/nuget v2 列于 dev-go-core 名下，而 M10 T-287 试点为 dev-registry-adapter。拆票按 PRD 口径派 dev-go-core（T-337/T-341/T-357）；conductor 若考量 T-287 连续性可改派 dev-registry-adapter，票面 area/dep 不变（备裁登记）。
4. **ROADMAP M12 立项段**与 PRD v1.0 一致无阻塞分歧；「M11 未纳入项」滚程去向与 §2.2 对账无缺——收口时按 DoD#7 建「M12 未纳入项」段（PM 职责，非本拆票动作）。
5. **T-330/T-320 票号沿用**：转正映射已入 BOARD 节首注（T-330→T-345/T-352；T-320→T-342），沿用旧号与否 conductor 收口时定——若沿用需同步改 BOARD 节映射表。
