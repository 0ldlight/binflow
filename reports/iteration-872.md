# Sprint 872 迭代报告 — QA 角色 UI 测试能力入册（用户指令）；T-343/T-340 双派；本地/UAT 访问双通

**日期**: 2026-08-29 01:10
**上轮**: Sprint 871（01:05）

## QA UI 能力（用户指令「测试工程师需要具有UI测试的能力」）

`.claude/agents/qa-engineer.md` 增补（`e5a720f`）：Playwright 全量（串行绿协议）/axe 双主题/交互四态抽查（锚册对照）/scratch 实例自起；输出契约加 UI 矩阵行。后续 qa 派单即生效（L15~L17/MUI 相关与终验直接受益）。

## 环境支持（穿插）

- 本地实例 nohup 常驻（:8080 三面通，admin/password）。
- **UAT 根因定案**：服务端完全健康（SSH 内验证 200×2 + 监听 *:8080）——**AWS 安全组未放行 8080**；操作指引已给用户（一次性 inbound rule）。

## 双票派发（01:10）

- **T-343 归档族**（dev-go-core）：zip/archive!/explode + V-1 定案 + T-339 观察者缝顺手接线。
- **T-340 D-F**（dev-registry-adapter）：conan v1 `_/_` delete 状态码小票。

## 状态

M12：8/25。在途 ×2。HEAD[develop]=`e5a720f` 已推双远端。
