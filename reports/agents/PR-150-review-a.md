# 评审报告 PR-150（形态: reviewer-a / correctness）

## 评审对象

- PR #150 `R0 wiring: governance freeze (Linear authority) + ruflo config adoption`（分支 claude/wonderful-sanderson-ee626f → develop，OPEN）
- 提交：`1f6dcf34` chore(config)（.claude/settings.json + .gitignore）、`904b574a` docs(governance)（BOARD.md 冻结通告 + CLAUDE.md 修订 + SPRINT-LOOP v3）
- 改动面：5 文件 / 315 diff 行，纯文档+配置，无代码。本 PR 非六关键域，单实例评审（A）合规。

## 清单逐项结论

| # | 清单项 | 结论 |
|---|---|---|
| 1 | 安全：settings.json 无密钥；.gitignore 挡 secrets | **PASS**。settings.json 全文核查：env 块仅三个功能开关旗标（AGENT_TEAMS / CLAUDE_FLOW_V3 / HOOKS），permissions 为工具放行清单，无令牌/内部 URL 凭据。`.env.local`/`.env.*.local` 生效；`git ls-files -ci --exclude-standard` 为空（无 tracked 文件被误挡，`deploy/*/.env.example` 不匹配新模式）。`.claude-flow/` 仅忽略 data/logs/sessions 三个子目录，其下配置文件仍可提交。`.codex/` 整目录忽略，无 tracked 内容。 |
| 2 | 三文档口径一致（冻结语义 + binfloow 拼写） | **PASS（被查三文档内部）**。CLAUDE.md（intro + 文件地图 + 看板规则三处）、BOARD.md 冻结通告、SPRINT-LOOP v3 规则 1：语义一致（权威=Linear、BOARD 冻结只读、subagent 恒不写、conductor 仅退场流程改写）。`binfloow` 拼写四处一致（CLAUDE.md ×2、SPRINT-LOOP ×1、BOARD ×1）。**但发现第四、五处活口径打架 → A-B1（blocking）**。 |
| 3 | v3 质量门不回退（对照 v2 原文） | **PASS**。派发单点名 8 项全部在 v3 有落点：8 条硬规则（规则 1 改写为 Linear 单写者，规则 5 反而**加强**——新增凭据纪律；2-4/6-8 原文保留）；四问计量公式逐字保留（含 score.sh v2 过渡注）；双审六关键域保留；分类硬门逐字保留；DoD 十二条逐字保留；qa≥3 熔断保留且**加强**（新增「3 次验收失败→独立根因，禁第 4 次盲重试」）；clean-room 处置逐字保留；loop 集成逐字保留。v3 另有净新增：收编三铁律、故障注入 containment、Linear 协作纪律。v2 其余收敛项的落点已逐一核实（见 A-N2 落点表），「质量门一不减」在系统层面成立。 |
| 4 | BOARD.md 编辑安全 | **PASS**。`git diff 904b574a^ 904b574a -- BOARD.md` = 5 insertions / **0 deletions**，单一 hunk 落在标题下通告区，全部 `> ` 前缀的合法 blockquote，无索引切片/重排痕迹。 |
| 5 | .gitignore 无误伤 | **PASS**（同第 1 项取证：tracked-ignored 列表为空；.claude-flow 仅子目录）。 |

## 发现问题

### Blocking

- **A-B1 治理切换未收口：活指令面仍驱动旧 BOARD 体制（本 PR 改了协议手册与看板，却留下直接矛盾的现行指令）**
  - `.claude/commands/sprint.md:10`：「结束时落盘 BOARD.md 与 reports/iteration-NNN.md」——/sprint 是 conductor 每轮执行的活命令，与冻结通告「票态不再流转」及 v3 规则 1 直接冲突。
  - `.claude/commands/security-review.md:13`：「Critical/High → P0/P1 缺陷票**即时入 BOARD**」——直接违反「新票不再录入」，是五个位置中危害最大的一个（会把新缺陷票写进冻结快照）。
  - `TEAM.md:105`（第 14 节 Loop Policy，组织宪章=活口径）：仍写「AI Software Factory Loop **18 阶段**（见 SPRINT-LOOP.md）」，与 v3（六步闭环、Linear 驱动）版本矛盾。CLAUDE.md 明示「组织宪章见 TEAM.md」，读者沿引用链拿到的是被废口径。
  - 读侧陈旧（同根因，建议同批修）：`.claude/commands/team-status.md:7`（BOARD 为「实态源」——冻结后将持续产出错误状态报告）、`.claude/commands/compatibility-gap.md:8`（「BOARD 在途票」为活源）。
  - **最小修法**（每处一行，与 PR 同类 docs 改动）：sprint.md:10 改「落盘 `reports/iteration-NNN.md`（BOARD 已冻结，不落盘；Linear 回填 pending）」；security-review.md:13 改「缺陷票录 Linear（未就绪期 T-<id> 报告承载），不入已冻结 BOARD」；TEAM.md:105 改「AI Software Factory Loop v3 六步闭环（见 SPRINT-LOOP.md，Linear 驱动）」；两个读侧命令加「BOARD 为冻结快照，实态=Linear/T-<id> 报告」。硬性最低集 = 前三处（写入路径 + 宪章版本）；读侧两处可并入本 PR 或紧随票。
  - 定性依据：这不是「范围外发现」——矛盾由本 PR 自身制造（改写了 SPRINT-LOOP 与 BOARD 状态，却未更新其活引用方），等同改了函数签名不更新调用方。`.claude/agents/*.md` 无此问题（tech-lead 等契约均已是「BOARD 只读/绝不写」口径）。

### Non-blocking

- **A-N1** `BOARD.md:6`（冻结通告正下方的存量行）仍称「唯一事实来源」，与紧邻上方「权威任务源迁移至 Linear」字面相反。属被冻结的历史内容、且上方有带日期的取代性通告，读者可自明；建议不动冻结正文，在通告块内补半句「下文历史口径（含「唯一事实来源」）自冻结日起失效」。
- **A-N2** v3 对 v2 细节做了收敛，落点均已核实存在，但 v3 未给指针，「质量门一不减」的成立依赖读者知道去哪找。落点表：Playwright/axe（UI 面）→ `.claude/agents/qa-engineer.md:3,17,62,99`；INTENTIONAL 必带 authority → `docs/compatibility/known-divergence.yaml:3` 头注 + `docs/compatibility/README.md:15,25`；security-auditor 每 10 轮/里程碑周期面 → `.claude/agents/security-auditor.md:3,81` + `TEAM.md:84`；protocol_leg ×10 → CLAUDE.md 文件地图（ci/ 行「十腿」）；UAT 四面与失败→缺陷票 → DoD「UAT validated」+ 部署硬门；dispatch 模板断点快照规范 → 各角色契约 Handoff 节。建议 v3 步骤 5 末尾加一行「细则见各角色 Agent Contract 与 docs/compatibility/」，把「不减」变成可验证的声明。真正无落点的仅一处：v2 阶段 0 的「PRODUCT 空壳 → 停止请用户填写」守卫（轻微，可在步骤 1 补一句）。
- **A-N3** `.claude/settings.json:13-14`：`Bash(node .claude/*)` + `mcp__claude-flow__*` 扩大了免确认执行面（.claude 顶层任意文件可作 node 脚本直跑、claude-flow MCP 全量放行）。属 ruflo 收编的已知取舍，无凭据泄露，记录备查；若后续想收紧，可将 `node .claude/*` 换成具名脚本清单。

## 裁决

**REQUEST_CHANGES**（blocking 1 项 / non-blocking 3 项）

## 建议

- 实现方按 A-B1 最小修法补 3-5 处一行修改后即可重审（预计 10 分钟改动量，重审只看增量）。
- Next（交 conductor）：A-N2 落点表可作为后续「Linear OAuth 就绪 + 回填」票的检查清单素材；`docs/ai-engineering/target-state.md`/`current-state.md` 中的「18 阶段」为带日期的历史审计快照，不改（记录即可）。

## 取证命令

```
gh pr diff 150（全量 315 行 + --name-only）
git show 904b574a^:.claude/team/SPRINT-LOOP.md（v2 原文对照）
git diff 904b574a^ 904b574a --stat/-- BOARD.md + grep -c '^-[^-]'（=0）
git ls-files -ci --exclude-standard（=空）；git ls-files | grep -Ei '\.env|^\.claude-flow|^\.codex'
grep -rn 'binfloow' CLAUDE.md BOARD.md .claude/team/SPRINT-LOOP.md
grep -rn '每 10 轮|security-auditor|Playwright|axe|INTENTIONAL.*authority'（落点核实）
grep -rn '落盘 BOARD|入 BOARD|写入 BOARD|录入 BOARD|写 BOARD|BOARD 移|更新 BOARD' .claude/agents/ .claude/commands/ TEAM.md
grep -rn '18 阶段' --include='*.md' .（陈旧引用清查）
```
