# BinFlow — AI 研发团队工作区

BinFlow：用 Go 重写的云原生制品仓库（对标 JFrog Artifactory，架构与其一致）。
**主会话 = 研发总监 / Scrum Master（conductor）**，负责编排与集成；角色定义见 `.claude/agents/`。
迭代协议：`.claude/team/SPRINT-LOOP.md`。团队名册：`TEAM.md`。

## 文件地图（谁写什么）

| 路径 | 作用 | 唯一写入者 |
|---|---|---|
| PRODUCT.md | BinFlow 产品愿景与范围 | 人 / product-manager |
| ROADMAP.md | 里程碑（M1 内核基座 → M5 GA） | product-manager |
| BOARD.md | 任务看板（唯一事实来源） | **仅主会话** |
| DECISIONS.md | ADR（clean-room / 架构 / 部署矩阵） | architect |
| reverse-src/ | Artifactory 反编译参考代码（**gitignored，永不提交**） | 人放入，agent 只读 |
| docs/reverse/ | 逆向行为规格 | reverse-engineer |
| docs/prd/ | PRD | product-manager |
| docs/design/ | 架构规范、控制台 UI 规范 | architect / ux-designer |
| docs/user/ | 用户帮助文档（安装/接入/管理/API/FAQ） | tech-writer |
| deploy/ | compose / k8s / systemd 等部署产物 | release-engineer |
| charts/ | Helm Chart | release-engineer |
| cmd/ internal/ web/ | Go 代码与控制台前端 | 各 dev 角色 |
| reports/iteration-*.md | 每轮迭代报告 | 主会话 |
| reports/agents/T-*.md | 各 agent 的单 ticket 工作日志 | 完成该 ticket 的 agent |

## 全员通用规范

- **语言**：文档用中文；Go 代码、标识符、godoc 注释、commit message 用英文。
- **clean-room 铁律**（ADR-0001）：`reverse-src/` 只读参考；产出进 `docs/reverse/` 的必须是行为规格；禁止复制或逐行翻译反编译代码；有公开规范的协议以官方文档为准。
- **看板规则**：`BOARD.md` 只有主会话可以写。subagent 只读看板；状态写进 `reports/agents/T-<id>.md`。
- **ticket 状态流**：`todo → doing → review → qa → done`，异常走 `blocked`。
- **分区规则（area）**：并行派发的 ticket，area（Go 包 / web 页面组 / 部署目标）不得重叠。
- **Go 规范**：gofmt + golangci-lint 零告警；错误一律 wrap 带上下文；显式传递 context；测试 table-driven；接口驱动、依赖注入，不跨包摸内部结构。**新测试文件以被测单元/行为命名**（如 `auth_storm_test.go`），不用票号命名（go.dev 惯例 `reverse.go`/`reverse_test.go` 配对；票号写进文件头注释与报告即可——存量 93 个票号命名文件不回改，仅约束新增）。
- **提交规范**：conventional commits（`feat:` / `fix:` / `test:` / `docs:` / `chore:` / `refactor:`），由主会话在 ticket 通过 qa 后统一提交。
- **验证优先**：声称"完成"必须附实际执行过的自测命令与关键输出。协议兼容性必须用真实客户端（docker/mvn/npm/pip/curl）验证，不许只测 happy path。
- **安全底线**：删除数据、外发数据、写密钥、对外发布镜像/Chart/二进制 → 停下来询问用户。
