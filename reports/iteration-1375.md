# Sprint 1375 迭代报告 — 双收编（T-458 腿① / T-471）+ CircleCI 双红定谳 + pypi 修复 + 复测启动（intake ⑮）

**日期**: 2026-09-04 18:4x~18:5x
**上轮**: Sprint 1374（T-455 收编）

## 一、双收编

- **T-458 腿① → done（`c6d8219`）**：docs/user 八文件 + fern 五页镜像；17 条 curl 断言复放 17/17；零票号。票两腿制——腿② 候后续 FE 票合入。**Fern 重发布挂本轮 conductor 收尾**。
- **T-471 → done（`1887311`）**：CI-only retries(2) + expect 10s；本地严格面逐项未变（env 门控双态验证）；真验证 = main 复测 e2e。

哨兵：go build 0 ✅ + tsc 0 ✅（后台完成）。清单：T-458/T-471 与 T-455 三票足迹互斥分离提交 ✅。

## 二、CircleCI 双红定谳（intake ⑮ 首查）

c4da02e commit status：build ✅ / deploy_uat ✅ / **protocol_matrix ❌** / **ci/circleci: e2e ❌**。

本地复现（同 UAT 目标）：**pypi 根因 = pip ≥25 硬性忽略未信任 plain-HTTP 主机**（"not a trusted or secure host … is being ignored" → "No matching distribution found"）→ `--trusted-host`（实际 index URL 派生）修复 → **pypi PASS**。generic/npm/go 本地全 PASS；docker 本地红 = 本机 Docker Desktop 未配 insecure-registry（CI config.yml:244 已配，非缺陷）。

## 三、复测启动（PR #91 → main `47f8385`）

携全量：T-444/T-455/T-458 腿①/T-471 + pypi 修复。双 CI 跑动中：
- GH ci：e2e **带 retries 新配置**首验
- CircleCI：deploy_uat → protocol_matrix **带 pypi 修复**首验

裁定挂下轮（同集绿 ×2 = 闭合；仍红则 GH protocol-matrix workflow_dispatch 取日志迭代）。

## 四、在途与状态

- lane：T-457（FE profile/帮助下拉）在途
- M16: **23/35**（T-458 两腿制未完不计满）
