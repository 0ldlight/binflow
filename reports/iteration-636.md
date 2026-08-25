# Sprint 636 迭代报告 — 第十三次熔断恢复 + T-286 补交 + T-285 复活（合并轮，00:05~03:21 共 16 次堆积触发）

**日期**: 2026-08-26 03:30
**上轮**: Sprint 635

## 事件与处置

- **00:05** T-285（Go adapter）被 429 击落（第十三次）。死前 Real-client e2e 已通过、正跑全量 race。
- **T-286 提交事故与修复**：d9db164 只提交了 12 个 modified 文件，**16 个 untracked 新文件全部漏 add**（deployprops/properties/013 迁移等）——与 T-283 同类事故。我核实后在 CD 链拦截前自查发现，stash T-285 部分改动 → 补交 `d305e82` → build/test/不变量全绿验证**已提交状态** → 双远端推送（CD 链已接力）。
- **T-285 复活**：stash pop 恢复 + 提交清单纪律特别提醒（贴全 `git status --short` 进日志）。

## 教训升级

连续两次同类事故（T-283/T-286：modified 已提交而 untracked 新文件漏 add）——**我的提交流程缺陷**：验证跑在工作树（含未跟踪文件）而 HEAD 不含。改进：此后凡含新文件的提交，commit 后必须 `git stash -u` 式核验或直接对 HEAD 做 build 验证。

## 阶段 0

在途 ×1：T-285（复活）。HEAD=`d305e82`（双远端同步，VM 已 ci.d305e82）。M10：8/21 done。

## 下轮计划

T-285 收口 → B5（T-287 NuGet 试点 ∥ T-288 控制台 License & Add-ons 页）。
