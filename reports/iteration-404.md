# Sprint 404 迭代报告 — T-215 关账（双 review + B1 闭环）+ 波 4 派发

**日期**: 2026-08-23 07:50
**上轮**: Sprint 403（B1 返修在途）；其间返修完成 → conductor 复验 → 关账 → 波 4 派发（本报告补登该序列）。

## 阶段 2 — T-215 收尾

1. **返修轮完成**：m-holder 夹具经 metadata 真缝（PutTarget + CanManage:true + 幽灵仓 PUT 可达性技术）；三断言（建仓臂 403 门源文案判别 / 详情 200 对照腿 / permissions 渲染 [r m] 含 N1）。红绿自证：删分支 → 测试以「403 来自错误的门」精确翻红（service 兜底仍在但文案暴露）→ 还原绿。
2. **conductor 复验**：`TestT215ManageHolderCreateArmStaysAdminOnly` + 守卫 -race 绿（2.7s）+ 全仓 build OK。
3. **提交 `bbbfbf2`**（httpapi + adapter/docker + config + cmd + audit + 矩阵脚本 + 日志），树干净。

## 阶段 3 — 波 4 派发

**T-217 [P0] FR-65**（manage wire + CanManageRepo + 仓库级 admin 派生），prompt 全量吸收 T-215/review 移交：三挂钩（覆盖集臂 / principals manage 字母 / usage ∨-臂）、service 门逐处放宽的零提权证明、复用 t215 夹具技术、Makefile 注释顺手修、守卫字面量不碰。

## 阶段 4 — 落盘

BOARD（波 3 done + 波 4 doing）`08cff1d` + 本报告。

## 阻塞与风险

- T-217 的 service 门放宽是提权敏感操作——AC 已含零提权证明（覆盖集三例 + 无提权链不变量 + t215 判别测试维持绿）。
- 遗留移交链保持：T-218 词表 / T-219 token handler / PM 两处回写。

## 下轮计划

T-217 完成 → review（关键模块双视角）→ 过审后波 5 三线（T-218 前端 + T-219 step-up + T-221 QA-I）。
