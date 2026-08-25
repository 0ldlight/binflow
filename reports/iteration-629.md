# Sprint 629 迭代报告 — T-298 关账：CD 链全形态落地并自愈（通知轮）

**日期**: 2026-08-25 23:20
**上轮**: Sprint 628；其间 T-298 完成 → **L1 紧急修复**（T-283 漏 add 的 license_gate 两文件——CD 链编译红首战立功）→ 提交 `784f3d5`/`375d856` → 双远端推送 → **CD 链自动接力实测**：VM 版本 `ci.e32c63f → ci.375d856`，演示标记 `T298-CD-DEMO-MARK` 已上 docs 站（grep binary match 确认）。

## T-298 交付

- **binflow-deploy job**（smoke SUCCESS 门控）：console+docs+build 版本注入 → VM 备份(留5)→原子换→探针 60s→失败自动回滚→烟测全链+docs 面
- **docs 站嵌入二进制**（单产物原子部署/回滚、零新组件——3.3MB docs/19.96MB 二进制）
- **双回滚臂实测**（探针臂 /bin/false + 烟测臂 docs 断言红）+ 红 smoke 拒绝接力实测
- **release 形态**走自托管镜像（dogfood）
- 数据纪律：只换二进制；/var/lib/binflow、/etc/binflow、unit 绝不触碰

## CD 链首战

漏 add 的定义文件 → main 在 VM 编译红 → deploy 门拒绝接力 → 我补提交 push → 链自愈 → **新版本+docs 标记自动上站**。管线在其存在数小时内抓到第一个真实缺陷。

## 阶段 0

在途 ×0（M10 基座+CD 全收）。HEAD=`375d856`。M10：**7/21 done + T-298**。

## 下轮计划

T-284（规格批次一）∥ T-285 或 T-286（B4 双主力——deps T-278✅/T-283✅ 全绿）。
