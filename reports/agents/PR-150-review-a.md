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

---

# 增量重审（2026-09-25，针对 commit 33fecb29）

## 增量范围

仅 33fecb29 一个提交、8 个文件（67+/8-）；worktree 中 T-520..T-523 在途 agent 的未提交分区不计入 PR 面。

## 处置核对（对照首轮报告）

| 项 | 处置 | 结论 |
|---|---|---|
| A-B1 sprint.md | 行 4 重锚 v3 六步闭环；行 8 复位改读 Linear/T-*.md；行 10 「只落盘 reports/iteration-NNN.md（BOARD.md 已冻结只读，不再写入）」 | **修复到位**，写入路径矛盾消除 |
| A-B1 security-review.md:13 | 「即时录 Linear（未就绪期 T-<id>.md 承载；BOARD.md 已冻结只读不再入票）」 | **修复到位**（原危害最大处） |
| A-B1 TEAM.md:105 | 「Loop v3 六步闭环（Linear 驱动，见 SPRINT-LOOP.md）」 | **修复到位** |
| A-B1 读侧 ×2 | team-status.md:7（BOARD=冻结历史快照 + T-*.md 过渡载体）、compatibility-gap.md:8（BOARD 仅作冻结历史参照） | **修复到位** |
| A-N1 | BOARD.md 冻结通告内追加失效声明半句，冻结正文未动（单行 amend，仍 blockquote，无结构改动） | **修复到位**，符合「不动冻结体」建议 |
| A-N2 | v3 step 5 追加「质量门口径核对清单：六关键域 A/B 双审 + AC 与真实客户端矩阵 + 差分（或金样降级显式记账）+ 基线比对 + negative test + UAT smoke——任一回退即阻断合并」 | **超出建议**（内联核对清单强于指针行，且新增「任一回退即阻断合并」硬句） |
| A-N3 | 知悉不改 | 符合处置建议 |

## 首轮记录修正

- 首轮 A-N2 称「PRODUCT 空壳守卫无落点」为漏看：该守卫一直存活于 `.claude/commands/sprint.md`（/sprint 命令面，本增量未动、已核实仍在）。据此 v2 收敛项**全部**有落点，「质量门一不减」无例外成立。

## 残留清查（针对 33fecb29 提交树，避开在途 agent 未提交内容）

- `git grep -E '落盘 BOARD|入 BOARD|写入 BOARD|录入 BOARD|写 BOARD|BOARD 移|更新 BOARD' 33fecb29 -- .claude/ TEAM.md CLAUDE.md` → 仅 tech-lead.md:37/137 两处**禁止性**表述（「绝不写 BOARD.md」「想直接写 BOARD→停」），与冻结一致，非矛盾。
- `git grep '18 阶段' 33fecb29 -- '*.md' ':!reports/' ':!BOARD.md' ':!docs/ai-engineering/'` → **零残留**（排除项均为带日期历史快照/日志）。
- sprint.md 提交树全文复核：五处改动到位，PRODUCT 空壳守卫存活。
- 增量 diff 无新密钥/新配置面；markdown 结构（缩进/blockquote）正确；commit message conventional 且引报告路径。

## 增量裁决

**APPROVE**

- A-B1 五处全部修复且与建议修法逐字吻合；A-N1/A-N2 处置到位；A-N3 知悉记录。
- 五项原清单结论不变（增量未触及 settings.json/.gitignore；BOARD.md 仅通告区单行 amend，仍零删除冻结正文）。
- Next（交 conductor）：无阻塞遗留。A-N3（settings.json 免确认执行面）作为 ruflo 取舍已记录在案，后续如收紧另开票。
