# Sprint 374 迭代报告 — M6 冻结态复核（含新发现缺口）

**日期**: 2026-08-22
**上轮**: Sprint 373（gitignore 卫生 + 空转避让）
**本轮焦点**: 复核上轮「继续」调查结论，确认唯一新事实——用户禁用 REST seam 缺口；其余仍冻结待裁。

## 阶段 0 — 复位

- PRODUCT.md（69 行）/ ROADMAP.md（75 行）均非空壳，不触发停止。
- 在途 agent：无（doing/qa/blocked 全空）。
- done 区 62 票（M6 全量 59 票全 done）。
- git：HEAD=`fff42c9`（sprint 373 报告）；tag `m1-done`~`m4-done` 在，`m5-done`/`m6-done` 缺。
- `docs-site/build/` + `.docusaurus/` 的工作树 diff 为 Docusaurus 生成产物噪声，非源文件，本轮不作处理。

## 阶段 1/2/3 — 均无动作

- 阶段 1：todo 空，M6 冻结，无新里程碑 → 跳过。
- 阶段 2：无 doing/qa/blocked 票。
- 阶段 3：无票可派。

## 本轮唯一实质产出：新缺口定论

上轮「继续」调查核实 T-174 H26（普通 OIDC 用户 token→403）**已由 T-190 闭环**（Q11 A 路径裁决后落地）——iteration-371 DoD 盘点在该点过时，实际仍开着的 FAIL 仅剩 **T-173（S3）/ T-175（集成）两处**。

同时确认 T-190 日志遗留注记「用户禁用无 REST seam（另票）」为**真实兼容性缺口**，证据：

- `internal/httpapi/security.go` 的 `userCreateBody` / `userUpdateBody` 均无 `enabled` 字段；
- 新建用户硬编码 `Enabled: true`（`:698`）；
- 替换路径注释「the enabled flag is not part of the wire body and keeps its stored value」（`:707-708`）。

即：护栏③（禁用用户→token 401）在 auth 层已实现（`TokenVerifier.Verify` 每次重查用户行 `enabled`），但**无任何 REST 手段把 `enabled` 翻成 false**，与 Artifactory 允许 API 禁用用户的行为不一致。属 P2 量级（wire body 加 `enabled` + 护栏③端到端验证），是否开票待用户裁定。

## 阶段 4 — 落盘

- ✅ 本报告（BOARD.md 状态行已准确，无需改动；无 commit 触发）。

## 阶段 5 — 战报

见下方用户汇报。

## 阻塞与风险

- **M6 冻结待裁**：待用户裁定清单自上轮收敛为 6 项（Q2 / Q6-Q7-Q10 / Q8-Q9 / DoD#2 处 FAIL / DoD#5 tag+push / 新增用户禁用 seam 缺口）。这是唯一真正阻塞。
- **loop 空转**：M6 冻结期无可推进工作，后续轮次继续轻量确认冻结态，不制造性动作。

## 下轮计划

等用户裁定后：放行 → 打 tag + 补 commit + 进 M7 规划 / 或依裁定生成修复票走 review/qa。