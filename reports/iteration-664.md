# Sprint 664 迭代报告 — FR-91-AC3 通过（`3dc47e7`），T-294 提前穿插派发

**日期**: 2026-08-26 10:10
**上轮**: Sprint 663

## FR-91 验收闭环（AC1+AC2+AC3 全过）

- **AC1**：5 份规格落 docs/reverse/（1158 行，结构六要素）
- **AC2**：clean-room 抽查（conductor 两轮 + 各票评审）无翻译痕迹，官方锚点前置
- **AC3**：tech-lead 确认 **5/5 可直接拆票**、六要素 30/30、23 裁决点（改判 2 均收紧方向）、缺项 4 条 0 阻塞（GPG keypair 条件前置 + 2 ADR 补记 + 2 处规格建议修订——helm §7.3 与 §1.1 自相矛盾处、cargo yank 404 行，已登记）
- **跨规格定案**（M11 拆票直接用）：管理面 REST 走 dispatchAPI 显式路由族（非 apiProtocolMounts）；对外绝对 URL 一律复用 server.base_url 零新配置；rpm 校验默认 SHA-256

## T-294 提前穿插（10:10，宽度 2）

- B8 原对 T-293 ∥ T-294；因 AC3 刚好产出 T-294 的 AC 草案（§3 直粘）+ cargo.md 新鲜，先派 T-294（area=internal/adapter/cargo 与 T-291 web/ 零重叠）
- **T-293 延后补位**：as-built 回写宜收齐全部分歧输入（T-289 presigned/AC2、T-290 双裁决已进清单），T-291 收口后派
- T-294 范围按 AC 草案严格 local 全量（remote/virtual M11 单票 dep 链串行）；门控三缝同 goproxy/nuget 模式；真实客户端 L-r1~L-r6 + cksum 三方对账

## 在途 ×2

T-291（MUI 首票）∥ T-294（Cargo adapter）。M10：14/21。

## 下轮计划

T-291/T-294 收口（T-291 复验含 HEAD 侧 npm build + anchor-audit；T-294 复验含真实客户端与门控全链）→ T-293 派发收分歧 → B9（T-295 ∥ T-296）→ B10（T-297 终验）。
