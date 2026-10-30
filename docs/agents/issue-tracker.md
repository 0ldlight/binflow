# Issue tracker: Linear

本仓库的 issue 权威源是 **Linear**，不是 GitHub Issues。GitHub remote（`0ldlight/binflow`）只承载代码与 PR。

- **入口**：https://linear.app/binfloow/team/BIN1/overview （workspace `binfloow`，注意拼写；团队 BIN1）
- **访问方式**：linear-server MCP（`https://mcp.linear.app/mcp`，user scope）。若当前会话没有 linear 工具，提示用户完成 OAuth；不要改用 GitHub Issues 替代。

## 结构约定（2026-09-25 章程）

三层结构，逐层分解：**Project**（里程碑/主题容器）→ **业务闭环 Issue**（一个完整业务闭环）→ **工程 Sub-issue**（可派发的最小工作单元，即 ticket，编号 `T-<id>`）。

`BOARD.md` 自 2026-09-25 起冻结为只读快照：新票不再录入，也不要从它派生新工作；权威任务源只有 Linear。

## 票状态流（八态）

`DISCOVERY → SPECIFIED → READY → IMPLEMENTING → REVIEW → QA → DIFFERENTIAL → UAT → done`，异常走 `blocked`。

分类硬门（不满足不得进 done）：协议票无差分测试 ≠ done；Storage 票无 corruption/concurrency/recovery 验证 ≠ done；Security 票无 negative test ≠ done；部署票无 UAT smoke ≠ done。

## 仓库侧落盘约定

- 每张完成票的工作日志（15 字段证据模板）：`reports/agents/T-<id>.md`
- 每轮迭代报告（含 Compatibility 四问）：`reports/iteration-*.md`
- subagent 只读 BOARD.md；状态写进报告与 Linear，不改看板。

## Pull requests as a triage surface

**PRs as a request surface: no.**（`/triage` 读此 flag。）外部 PR 不是 triage 请求面；PR 评审走章程双审制度（code-reviewer A/B），不进 triage 状态机。

## When a skill says "publish to the issue tracker"

在 Linear 对应业务闭环 Issue 下创建工程 Sub-issue（`T-<id>` 编号，中文票面）。

## When a skill says "fetch the relevant ticket"

按 `T-<id>` 在 Linear 检索；票面细节以 Linear 为准，`reports/agents/T-<id>.md` 只补执行证据。

## Wayfinding operations

Used by `/wayfinder`. The **map** is a Linear 业务闭环 Issue（或轻量 Project）；**child tickets** 是其下的工程 Sub-issue。

- **Map**: 业务闭环 Issue，Notes / Decisions-so-far / Fog 写在 description 或置顶评论。
- **Child ticket**: map 下的工程 Sub-issue，打 `wayfinder:<type>`（`research`/`prototype`/`grilling`/`task`）标签。
- **Blocking**: 用 Linear 的 blocked-by 关系表达依赖；frontier = 无 blocker 且无 assignee 的 open sub-issues，按 map 顺序取第一个。
- **Claim**: 把 assignee 标为驱动会话（或注明会话名）；claim 是该会话的第一次写操作。
- **Resolve**: 结论评论到票上后关闭，并把 context 指针回填到 map 的 Decisions-so-far。
