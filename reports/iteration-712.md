# Sprint 712 迭代报告 — gitflow 切换完成（用户指令即时落地）；tech-lead 拆票在途

**日期**: 2026-08-26 20:50
**上轮**: Sprint 711

## gitflow 切换（用户指令 20:45，当日三动作落地）

1. **develop 切出**（main@`95a8f9a`）→ 双远端已推；本地已切 develop，后续提交全进 develop
2. **CD 链改挂 develop**：VM Jenkins `binflow-ci-smoke` + `binflow-deploy` 容器内 config.xml `*/main → */develop`（已核实 1 处/文件）+ Jenkins 重启重载；仓库 groovy 源同步（deploy/ci/binflow-deploy.groovy）；`binflow-release` 维持 main
3. **BOARD 指令落板**：main = release-only（--no-ff 合并 + tag）、feature/T-<id> 每票分支、hotfix 双回、subagent 不提交的纪律不变、存量 tag 不动

main 停在 `95a8f9a`（含 m10-done）；下一次 develop→main 合并在 M11 首批收口。

## 在途 ×1

tech-lead M11 拆票（PRD v1.1 + 用户四裁定输入）。

## 下轮计划

拆票回收 → 录板 T-301 起 → B0 派发（先复核前置票）；gitflow 首个 feature 分支流程实战。
