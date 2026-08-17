# 团队名册与使用手册

主会话（你启动 Claude Code 的那个会话）扮演 **conductor（研发总监 / Scrum Master）**，
按 `.claude/team/SPRINT-LOOP.md` 的协议每轮"收尾上轮 → 补给看板 → 并行派发 → 汇报"。
其余角色全部是 `.claude/agents/` 下的 subagent 定义，由主会话按需派发。

## 角色总览（5 组 12 角色）

| 组 | 角色（subagent_type） | 职责一句话 | 模型 | 建议并行实例 |
|---|---|---|---|---|
| 产品组 | `product-manager` | 需求分析、PRD、用户故事与验收标准、路线图 | opus | 1–2（按功能域） |
| 产品组 | `ux-designer` | 信息架构、页面线框、交互状态、设计 token | sonnet | 1 |
| 设计组 | `architect` | 技术选型、系统架构、接口契约、ADR | opus | 1 |
| 设计组 | `tech-lead` | 把 PRD 分解为工程 ticket、划定 area、攻坚疑难 | opus | 1 |
| 开发组 | `dev-frontend` | 前端实现（组件/状态/路由/样式） | sonnet | 1–3 |
| 开发组 | `dev-backend` | 后端实现（API/数据库/服务层） | sonnet | 1–3 |
| 开发组 | `dev-fullstack` | 端到端小特性、脚手架补齐、小 bug 修复 | sonnet | 1–2 |
| 开发组 | `devops-engineer` | 环境搭建、脚本、CI/CD、容器、部署 | sonnet | 1 |
| 质量组 | `qa-engineer` | 从验收标准生成测试计划与用例、执行验证、报缺陷 | sonnet | 1–2 |
| 质量组 | `code-reviewer` | 代码评审（正确性/一致性/测试覆盖），出 APPROVE/REQUEST_CHANGES | opus | 1–2（不同视角） |
| 质量组 | `security-auditor` | 安全审计（注入/越权/敏感数据/依赖） | opus | 里程碑节点 1 |
| 支持组 | `tech-writer` | README、用户文档、CHANGELOG、API 文档 | sonnet | 1 |

> 模型可按成本调整：改 `.claude/agents/<role>.md` frontmatter 的 `model` 字段
> （可选 `opus` / `sonnet` / `haiku` / `inherit`）。评审、架构类建议保持 opus。

## 「一个角色多个 agent」如何实现

Claude Code 的 agent 定义是**类型**，实例是**派发**。三种玩法：

1. **并行实例**：同一种类型同时派多个。例如 3 个互不重叠的后端 ticket，
   一条消息里发 3 个 `Agent(subagent_type="dev-backend")`，各自领一个 ticket。
2. **视角实例**：同一类型、不同指令。例如评审关键模块时派 2 个 `code-reviewer`，
   一个专注正确性、一个专注简洁性与一致性，结论由主会话裁决。
3. **功能域实例**：产品复杂时可复制定义文件做领域专精，如
   `cp .claude/agents/dev-backend.md .claude/agents/dev-backend-data.md`，改 name/description/职责为数据方向。

并行安全的两条铁律（协议已内置）：**area 不重叠**、**BOARD.md 单写者**。

## 启动方式

```bash
# 1. 填写产品愿景（已内置一份可直接跑的示例，替换成你的产品即可）
$EDITOR PRODUCT.md

# 2. 重启 / 新开一个会话让 agent 定义生效，先手动试跑一轮
/sprint

# 3. 确认节奏没问题后，交给 loop 自动循环（间隔按单轮耗时定，一般 15–30m）
/loop 20m /sprint
```

注意事项：

- loop 只在会话空闲时触发，上一轮没跑完不会叠加；后台 agent 的完成通知由下一轮收尾处理。
- 若循环基于系统调度器，recurring 任务约 7 天后自动过期，到期重新 `/loop` 即可。
- 自主循环建议提前放行权限：会话内用 acceptEdits 模式，或运行 `/fewer-permission-prompts`
  生成常用命令白名单；否则每轮会停在权限确认上。
- 想看进度随时运行 `/team-status`。

## 如何调整团队

- **加角色**：在 `.claude/agents/` 新建 `xxx.md`（frontmatter：name/description/tools/model + 正文为 system prompt），参考现有文件。
- **减角色**：删文件即可；协议派发的是角色名，缺失时报错可见。
- **换节奏**：`/loop <interval> /sprint`；迭代协议本身的并行度上限在 SPRINT-LOOP.md 的"硬性规则"里改。
- **换产品**：改 PRODUCT.md，然后让 product-manager 重走 M0（`/sprint` 会自动发现 PRD 失效）。
