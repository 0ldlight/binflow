# BinFlow — AI Software Factory 工作区

BinFlow：用 Go 重写的云原生制品仓库（对标 JFrog Artifactory，行为与产品参考目标）。
**组织形态（二代，2026-09-08 重组）**：AI Software Factory + Compatibility Engineering Organization。
**主会话 = Loop Engineer / conductor（总控）**，唯一目标是减少 Compatibility Gap；协议见 `.claude/team/SPRINT-LOOP.md`（v3，Linear 驱动）；
组织宪章见 `TEAM.md`；agent 依赖图与域所有权见 `docs/ai-engineering/agent-graph.yaml`；角色定义见 `.claude/agents/`（十二要素 Agent Contract）。
**任务权威源（2026-09-25 章程）**：Linear（workspace `binfloow`，注意拼写）三层结构 Project → 业务闭环 Issue → 工程 Sub-issue；BOARD.md 冻结为只读快照。

## 文件地图（谁写什么）

| 路径 | 作用 | 唯一写入者 |
|---|---|---|
| PRODUCT.md | BinFlow 产品愿景与范围 | 人 / product-manager |
| ROADMAP.md | 里程碑与尾部「完成定义」（Gap 口径停止条件） | product-manager |
| BOARD.md | 任务看板（**2026-09-25 起冻结为只读快照**；权威任务源=Linear binfloow，回填 pending；历史八态生命周期） | **仅主会话** |
| DECISIONS.md | ADR（clean-room / 架构 / 部署矩阵；只追加+Errata） | architect |
| docs/ai-engineering/ | 组织设计与审计（current/target-state、agent-graph、compatibility-engineering） | conductor / architect |
| docs/compatibility/ | 兼容工程体系（contracts/probes/fixtures/diffs/golden/matrix.yaml/known-divergence.yaml/protocol） | compatibility-engineer |
| reports/compatibility/ | 差分运行报告（双系统对照） | differential-qa-engineer |
| reverse-src/ | Artifactory 反编译参考代码（**gitignored，永不提交**） | 人放入，agent 只读 |
| docs/reverse/ | 逆向行为规格（clean-room） | reverse-engineer |
| docs/prd/ | PRD | product-manager |
| docs/design/ | 架构规范、控制台 UI 规范 | architect / ux-designer |
| docs/user/ | 用户帮助文档 | tech-writer |
| fern/ | Fern 文档站（openapi spec + pages；生成器 tools/openapi-spec/） | tech-writer / conductor（发布） |
| deploy/ | compose / k8s / systemd 等部署产物 | release-engineer |
| charts/ | Helm Chart | release-engineer |
| tools/difftest/ | 差分测试框架（双发→normalize→diff→score） | differential-qa-engineer |
| bench/, docs/performance/ | 性能基线与预算 | performance-engineer |
| cmd/ internal/ web/ | Go 代码与控制台前端 | 各 dev 角色（按 agent-graph owns） |
| ci/ .circleci/ .github/ | CI 质量闸门链（含 protocol matrix 十腿、差分 job） | devops-engineer |
| reports/iteration-*.md | 每轮迭代报告（含 Compatibility 四问） | 主会话 |
| reports/agents/T-*.md | 各 agent 单票工作日志（15 字段证据模板） | 完成该票的 agent |

## 全员通用规范

- **语言**：文档用中文；Go 代码、标识符、godoc 注释、commit message 用英文。
- **clean-room 铁律**（ADR-0001）：`reverse-src/` 只读参考；产出进 `docs/reverse/` 的必须是行为规格
  （「当客户端…服务端返回…」句式，禁类名/私有结构/逐行翻译）；行为规格→可执行契约归 compatibility-engineer；
  有公开规范的协议以官方文档为准；**禁止通过猜测补齐兼容行为**。
- **看板规则**：`BOARD.md` 2026-09-25 起冻结为只读快照（新票不再录入；权威任务源=Linear）。subagent 只读看板；状态写进 `reports/agents/T-<id>.md`。
- **ticket 状态流（八态）**：`DISCOVERY → SPECIFIED → READY → IMPLEMENTING → REVIEW → QA → DIFFERENTIAL → UAT → done`，异常走 `blocked`。
  分类硬门：协议票无差分测试 ≠ done；Storage 票无 corruption/concurrency/recovery 验证 ≠ done；Security 票无 negative test ≠ done；部署票无 UAT smoke ≠ done。
- **分区规则（area）**：并行派发的 ticket，area 不得重叠；唯一事实源= `docs/ai-engineering/agent-graph.yaml` 的 owns。
- **Go 规范**：gofmt + golangci-lint 零告警；错误一律 wrap 带上下文；显式传递 context；测试 table-driven；
  接口驱动、依赖注入，不跨包摸内部结构。**新测试文件以被测单元/行为命名**（如 `auth_storm_test.go`），
  不用票号命名（票号写进文件头注释与报告即可——存量票号命名文件不回改，仅约束新增）。
- **提交规范**：conventional commits（`feat:` / `fix:` / `test:` / `docs:` / `chore:` / `refactor:`），
  主会话在任务分支（task branch）上提交；合并走 PR（收口 develop）且须独立评审（code-reviewer）通过，
  主会话不得单人评审合入自己实现的代码。
- **验证优先**：声称"完成"必须附实际执行过的自测命令与关键输出（15 字段证据模板）。
  协议兼容性必须用真实客户端（docker/mvn/npm/pip/curl…）验证，不许只测 happy path。
- **安全底线**：删除数据、外发数据、写密钥、对外发布镜像/Chart/二进制 → 停下来询问用户。
  凭据一律经 SSH/凭据管理/环境变量注入获取，禁止写入 Git、文档、任务、截图、命令日志与报告。
- **基准锚定（2026-09-24 总令）**：兼容目标基准 = JFrog Artifactory **7.161.26 Enterprise+**
  （http://192.168.120.38:8082；版本活体复验 pending）。行为规格与差分结论必须标注认证所参照的源版本；
  旧基准（7.161.15，.130:8082）认证的结论按其标注版本理解，不自动失效。
- **测试四态**：PASS / FAIL / BLOCKED / NOT_RUN（skip≠PASS；无证据=NOT_RUN）；不得自动放宽超时/容差。
- **UAT 审批门（2026-09-24 起）**：CircleCI `deploy_uat` 前置 `uat_approval` 人工批准；UAT 换装需确认后执行。
