# 团队名册与使用手册

主会话（你启动 Claude Code 的那个会话）扮演 **conductor（研发总监 / Scrum Master）**，
按 `.claude/team/SPRINT-LOOP.md` 的协议每轮"收尾上轮 → 补给看板 → 并行派发 → 汇报"。
其余角色全部是 `.claude/agents/` 下的 subagent 定义（DevOps / 云原生 / 制品仓库领域专精），由主会话按需派发。

## 角色总览（6 组 15 角色）

| 组 | 角色（subagent_type） | 职责一句话 | 模型 | 建议并行实例 |
|---|---|---|---|---|
| 产品组 | `product-manager` | 制品仓库领域 PM：PRD、兼容性验收标准、路线图 | opus | 1 |
| 产品组 | `ux-designer` | Web 控制台：信息架构、线框、交互四态、设计 token | sonnet | 1 |
| 设计组 | `architect` | Go 架构、存储设计、协议适配器 SPI、部署架构、ADR | opus | 1 |
| 设计组 | `tech-lead` | 把 PRD 拆成工程票（优先级/角色/area/依赖）、攻坚 | opus | 1 |
| 逆向组 | `reverse-engineer` | 读 reverse-src/ 反编译代码，产出 clean-room 行为规格 | opus | 1–2（按领域） |
| 开发组 | `dev-go-core` | 仓库模型、元数据层、REST API、认证权限 | sonnet | 1–2 |
| 开发组 | `dev-go-storage` | checksum 寻址存储、去重、上传会话、GC、备份 | sonnet | 1–2 |
| 开发组 | `dev-registry-adapter` | 协议适配器：Generic/Docker v2/Maven/npm/PyPI | sonnet | 1–3（按协议） |
| 开发组 | `dev-frontend` | Web 控制台（React，go:embed 打包） | sonnet | 1–2 |
| 开发组 | `devops-engineer` | Go 工具链、Makefile、CI、开发环境 compose/kind | sonnet | 1 |
| 开发组 | `release-engineer` | 多元部署矩阵：goreleaser/Docker/Helm/K8s/systemd/离线包 | sonnet | 1 |
| 质量组 | `qa-engineer` | 真实客户端兼容矩阵、存储完整性、部署烟测 | sonnet | 1–2 |
| 质量组 | `code-reviewer` | Go 代码评审（正确性/一致性），APPROVE/REQUEST_CHANGES | opus | 1–2（不同视角） |
| 质量组 | `security-auditor` | 制品仓库威胁模型：越权/路径穿越/SSRF/供应链 | opus | 里程碑节点 1 |
| 支持组 | `tech-writer` | 帮助文档中心：安装/客户端接入/管理/API/FAQ | sonnet | 1 |

> 模型可按成本调整：改 `.claude/agents/<role>.md` frontmatter 的 `model`
> （`opus` / `sonnet` / `haiku` / `inherit`）。评审、架构、逆向类建议保持 opus。

## 「一个角色多个 agent」如何实现

agent 定义是**类型**，实例是**派发**。三种玩法：

1. **并行实例**：同一种类型同时派多个。例如 M3 里 Maven、npm、PyPI 三个适配器互不重叠，
   一条消息发 3 个 `dev-registry-adapter`，各领一个协议包（`internal/adapter/maven|npm|pypi`）。
2. **视角实例**：同一类型、不同指令。存储引擎等关键模块派 2 个 `code-reviewer`，
   一个查并发与错误处理、一个查架构一致性与测试覆盖，结论由主会话裁决。
3. **领域实例**：需要更专精时复制定义文件，如
   `cp .claude/agents/dev-registry-adapter.md .claude/agents/dev-adapter-oci.md`，改 name/description 聚焦 OCI/Helm。

并行安全铁律（协议已内置）：**area 不重叠**（Go 包/页面组/部署目标互斥）、**BOARD.md 单写者**。

## 启动方式

```bash
# 1. 放入逆向参考代码（不入库；没有它 reverse-engineer 会标 blocked）
mkdir -p reverse-src && cp -r <反编译输出> reverse-src/artifactory/

# 2. 新开会话让 agent 定义生效，手动试跑一轮（首轮：reverse-engineer + PM + architect 并行）
/sprint

# 3. 确认节奏后交给 loop 自动循环（间隔按单轮耗时定，一般 15–30m）
/loop 20m /sprint
```

注意事项：

- loop 只在会话空闲时触发，上一轮没跑完不会叠加；在途 agent 由下一轮收尾。
- recurring 循环约 7 天自动过期，到期重新 `/loop` 即可。
- 自主循环建议先放行权限：会话内用 acceptEdits 模式，或跑 `/fewer-permission-prompts` 生成白名单。
- 看进度随时 `/team-status`。

## 如何调整团队

- **加角色**：`.claude/agents/` 新建 `xxx.md`（frontmatter：name/description/tools/model + 正文 system prompt）。
- **减角色**：删文件即可；协议派发的是角色名，缺失时报错可见。
- **换节奏**：`/loop <interval> /sprint`；并行度上限在 SPRINT-LOOP.md「硬性规则」里改。
- **改产品方向**：改 PRODUCT.md（必要时同步 ROADMAP.md），`/sprint` 的阶段 1 会发现 PRD 失效并重走。
