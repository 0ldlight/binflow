# Sprint 375 迭代报告 — 用户终裁落地（ADR-0025）+ 三票并行派发

**日期**: 2026-08-22
**上轮**: Sprint 374（M6 冻结态复核 + 新缺口定论）
**本轮焦点**: 用户授权依过往经验终裁 → 六项开放问题落 ADR-0025 → 派生 T-208/T-209/T-210 三票并行派发，冻结态解除。

## 阶段 0 — 复位

- PRODUCT.md（69 行）/ ROADMAP.md（75 行）非空壳。
- 在途 agent：无（doing/qa/blocked 全空）。
- done 区 62 票（M6 全量 59 票全 done）。
- tag `m1-done`~`m4-done` 在，`m5-done`/`m6-done` 缺。

## 本轮核心：用户终裁落地（ADR-0025）

用户消息「你根据过往经验帮我决定」授权 conductor 代行终裁。六项裁定写入 `DECISIONS.md` 新增 **ADR-0025**（M6 收尾开放问题用户终裁）：

| # | 开放问题 | 裁定 |
|---|---------|------|
| Q6 | replica 仓库类型 | 认领 T-162 暂行（local backing + virtual 只读门面）为正式决议，不新增 rclass 枚举 |
| Q7 | 复制冲突策略 | 认领暂行（同 checksum 幂等成功 / 不一致 failed 不动目标）为正式决议，不引入 conflict 状态词 |
| Q8 | AWS S3 验收 | MinIO 等价；AWS 实腿延后 M7（`dep:用户环境`） |
| Q9 | Artifactory 迁移验收 | Docker 自建 OSS 覆盖 P2；真实企业版延后 M7 |
| Q10 | 私网目标默认放行 | 认领暂行（DenyPrivateTargets 默认 false）+ config 桥接键 → T-210 |
| Q2 | 本地 filestore sessions 统一 DB | **采纳，修订 ADR-0006 决策 2** → T-209 |
| — | 用户禁用 REST seam | P2 修复票 → T-208 |

同步回写：
- `docs/prd/milestone-6.md` §7 开放问题表 Q2/Q6/Q7/Q8/Q9/Q10 状态改「已定案（ADR-0025 …）」，去除「暂行…待终裁」措辞。
- `DECISIONS.md` ADR-0006 决策 2 与后果段会话部分标注待 T-209 改写（T-209 票内执行）。
- `BOARD.md` 状态行收敛为「终裁已落 ADR-0025」+ 三票派生。

## 阶段 3 — 派发（本轮）

三张后续票并行派发（后台），area 两两不重叠：

| 票 | 优先级 | 角色 | area | 内容 |
|----|-------|------|------|------|
| T-208 | P2 | dev-go-core | internal/httpapi + internal/auth | 用户禁用 REST seam（`enabled` wire 字段 + 护栏③端到端） |
| T-209 | P1 | dev-go-storage | internal/storage + internal/metadata | 本地 filestore session 统一 DB（修订 ADR-0006） |
| T-210 | P2 | dev-go-core | internal/config + internal/replication | `replication.allow_private_target` 显式开关 |

看板：todo 空，三票移入 doing。

## 阶段 4 — 落盘

- ✅ DECISIONS.md：新增 ADR-0025。
- ✅ docs/prd/milestone-6.md：Q2/Q6/Q7/Q8/Q9/Q10 状态回写。
- ✅ BOARD.md：状态行 + 三票 todo→doing。
- ✅ 本报告 + commit（`ADR-0025` 裁决记录与看板/PRD 回写）。

## 阶段 5 — 战报

见下方用户汇报。

## 阻塞与风险

- **无新阻塞**。三票在途，下轮收尾（review/qa 按常规流程）。
- **DoD #5 仍未决**：`git tag m6-done` + 补 `m5-done` 属本地动作，conductor 可自行打；**`git push` 外发仍须用户单独授权**——在 T-208/T-209/T-210 三票收尾并重跑 DoD 全绿前，建议不打 tag（M6 实质还没最后 closure）。
- **loop 已恢复有工作可做**：后续轮次正常走「收尾三票 → review → qa → 提交」流程。

## 下轮计划

收尾 T-208/T-209/T-210：等待后台 agent 完成通知 → 逐票 review → qa → conventional commit。三票均 done 后重跑 DoD 五条核验，再就 `m6-done`/`m5-done` tag 与 push 请示用户。