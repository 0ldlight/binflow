# dev-center — AI 研发团队工作区

本项目是一个由 AI subagent 组成的研发团队工作区。
**主会话 = 研发总监 / Scrum Master（conductor）**，负责编排与集成；具体角色定义见 `.claude/agents/`。
每轮迭代的执行手册：`.claude/team/SPRINT-LOOP.md`。团队名册与使用说明：`TEAM.md`。

## 文件地图（谁写什么）

| 路径 | 作用 | 唯一写入者 |
|---|---|---|
| PRODUCT.md | 产品愿景与范围 | 人 / product-manager |
| ROADMAP.md | 里程碑与优先级 | product-manager |
| BOARD.md | 任务看板（唯一事实来源） | **仅主会话** |
| DECISIONS.md | 架构决策记录 ADR | architect |
| docs/prd/ | PRD 文档 | product-manager |
| docs/design/ | 架构规范、UI 规范 | architect / ux-designer |
| reports/iteration-*.md | 每轮迭代报告 | 主会话 |
| reports/agents/T-*.md | 各 agent 的单 ticket 工作日志 | 完成该 ticket 的 agent |

## 全员通用规范

- **语言**：交流与文档用中文；代码、标识符、commit message 主体用英文。
- **看板规则**：`BOARD.md` 只有主会话可以写。所有 subagent 只读看板；状态变化写进自己的 `reports/agents/T-<id>.md` 日志，由主会话汇总更新看板。
- **ticket 状态流**：`todo → doing → review → qa → done`，异常走 `blocked`。
- **分区规则（area）**：并行派发的 ticket，其 `area`（目录/模块范围）不得重叠，防止并行 agent 互相覆盖。
- **提交规范**：conventional commits（`feat:` / `fix:` / `test:` / `docs:` / `chore:` / `refactor:`），由主会话在每个 ticket 通过 qa 后统一提交。
- **验证优先**：任何 agent 声称"完成"，必须在最终回复里附上实际执行过的自测命令与关键输出摘要。没有证据 = 未完成。
- **安全底线**：涉及删除数据库、外发数据、写入密钥、对外发布的操作，任何角色都必须停下来询问用户。
