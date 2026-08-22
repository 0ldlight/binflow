# Sprint 397 迭代报告 — 波 2 全清关账 + 波 3 派发

**日期**: 2026-08-23 06:50
**上轮**: Sprint 396（在途确认）；其间 T-229 完成 + T-212 review APPROVE 两通知齐至，本轮完成波 2 全部关账。

## 阶段 2 — 波 2 收尾（三票关账）

| 票 | 判定链 | commit |
|---|---|---|
| T-212 RBAC 基座 | agent 全绿 → review APPROVE（四不变量逐条 + 独立红绿×2：短路/镜像 + clean-room 无嫌疑） | `efd88d7` |
| T-216 docker 续传 | agent 全绿 → review APPROVE（单飞变异实证 + kill 臂新构建复证）→ sigterm 臂随 T-229 转绿 | `e9de5ef` |
| T-229 Close 保留 | agent 全绿 → **conductor 强制新构建亲验**：sigterm 臂 GREEN EXIT=0（204+Range→续传→PUT 201→逐位等）+ kill 臂维持绿 | `6d379cf` |

conductor 合入前全仓 build+vet 绿；三 commit 按依赖序、严守文件归属（T-229 在 resume_test.go 的两处注释随 T-216 走并在 commit message 注明）。

**FR-67 至此端到端达成**：kill -9 / SIGTERM / compose 三径对称续传，探针双臂全绿。

## 阶段 3 — 波 3 派发

**T-215 [P0]**（独占 httpapi + config/cmd 接线 + 矩阵脚本同步），prompt 吸收全部 review 移交项：
- T-212 review 移交①：`adapter/docker/token.go:253` authenticateForm 不带 Role（form 腿 readonly 折叠 user——接通 readonly_group 前必修）
- 移交②：Create 矛盾输入 handler 侧 400
- T-212 遗留①：readonly_group config 键 + cmd 接线
- T-214 风险③：矩阵脚本列名拼写同步 `readonly_admin`
- 施工纪律：严格照 §7.1 清点表逐路由迁移 + 守卫测试防裸 admin 门回归 + 30 处 diff 清单入日志（T-221 复核依据）

## 阶段 4 — 落盘

BOARD（波 2 done 区三票 + 波 3 doing）+ 本报告；commits `efd88d7`/`e9de5ef`/`6d379cf`/`e314479`。

## 阻塞与风险

- T-215 是本里程碑最险面（30 处路由门迁移，漏一处 = 静默 403）——三重钉死已写进 AC（清点表逐路由 diff / 守卫测试 / T-221 矩阵复核）。
- 挂账不变：探针新鲜度校验（T-222 前）、flyer defer、TTL flake（T-220）。

## 下轮计划

T-215 完成 → review（关键模块双视角候选）→ 过审后波 4（T-217 FR-65 REST 面）。
