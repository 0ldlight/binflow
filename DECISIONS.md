# 架构决策记录（ADR）

> architect 维护。每条决策一条 ADR，按序号递增，只追加不删改（推翻旧决策时新增一条并标记旧条 Superseded）。

## 模板

```
## ADR-<序号>: <决策标题>
- 状态: Proposed | Accepted | Superseded by ADR-<n>
- 日期: YYYY-MM-DD
- 背景: 遇到什么问题、什么约束
- 候选方案: A / B / C 各自优劣
- 决策: 选了什么
- 理由: 为什么赢出
- 后果: 带来的影响、需要遵守的约定
```

---

## ADR-0000: 采用「AI 研发团队 + 看板单写者 + area 分区」工作流
- 状态: Accepted
- 日期: 2026-08-17
- 背景: 多个 AI agent 并行开发同一代码库，需要防冲突、可恢复、可审计的流程。
- 候选方案: A) 单 agent 串行完成所有事；B) 多 agent 自由协作；C) 主会话统一编排 + 角色分工 + 状态落盘。
- 决策: 选 C。
- 理由: 保留并行效率，同时用「BOARD.md 单写者」「ticket area 不重叠」「全部状态写文件」三条约束消除写冲突与状态丢失。
- 后果: 每轮迭代必须更新看板与报告；agent 间不直接通信，全部通过主会话中转。

## ADR-0001: 逆向采用 clean-room 流程（规格与实现分离）
- 状态: Accepted（用户指定基线）
- 日期: 2026-08-17
- 背景: BinFlow 需要对齐 Artifactory 行为，参考材料是 `reverse-src/` 下的反编译 Java 代码。直接翻译/复制反编译代码有版权与许可风险，且会把 Java 的结构包袱带进 Go 实现。
- 候选方案: A) 逐模块翻译反编译代码；B) 只看公开文档实现；C) clean-room：反编译仅用于产出行为规格，规格与实现分离。
- 决策: 选 C。reverse-engineer 产出 `docs/reverse/*.md` 行为规格（端点表、存储布局、语义流程），实现者只依据规格与官方协议文档编码；有公开规范的能力（Docker Registry v2 / Maven 2 / npm / PyPI）一律以官方规范为准。
- 后果: 每个协议/领域先有规格再有实现 ticket；规格必须标注置信度（高=代码+公开文档双证 / 中=仅代码 / 低=推测）；实现不得与反编译代码逐行对应；`reverse-src/` 永不入库。

## ADR-0002: Go 模块化单体，单二进制交付
- 状态: Accepted（用户指定基线，architect 细化）
- 日期: 2026-08-17
- 背景: 对标 Artifactory（Java/Tomcat/JVM，部署重、启动慢、内存高）。BinFlow 的差异化之一是交付形态。
- 候选方案: A) Go 微服务；B) Go 模块化单体；C) Java 同栈重写。
- 决策: 选 B。`cmd/binflow-server` 单入口；`internal/` 分包（config / storage / metadata / repo / adapter / auth / audit / httpapi / console）；包边界即并行开发的 area 边界；Web 控制台构建产物用 go:embed 打入二进制。
- 理由: 单二进制是产品差异化（零依赖、冷启动快、内存小一个数量级）；此规模上微服务是过度设计；沿用 Java 违背选 Go 的初衷。
- 后果: 模块间只通过接口依赖，禁止跨包摸内部结构；未来如需拆分保留可能（ADR 可推翻）。

## ADR-0003: 概念模型与 Artifactory 一一对齐
- 状态: Accepted（用户指定基线）
- 日期: 2026-08-17
- 背景: 产品要求架构与 Artifactory 一致，便于迁移与文档类推。
- 决策: 仓库三型 local / remote / virtual；checksum 寻址的文件存储（sha256 主键，sha1/md5 附属校验）；元数据库内嵌 SQLite（默认、零依赖）+ Postgres（可选）；users/groups/permissions/tokens 权限体系；REST 兼容 Artifactory 高频子集 + 自有 `/api/v1`。
- 后果: API 与配置命名沿用 Artifactory 术语（repo key、node、checksum…）；元数据 schema 需在 M1 由 architect 定稿。

## ADR-0004: 部署矩阵（多元化部署）
- 状态: Accepted（用户指定基线，release-engineer 落地）
- 日期: 2026-08-17
- 背景: 用户明确要求多元化部署方式；目标环境含 K8s、compose、裸机、离线网络。
- 决策: GA（M5）必须交付：① 单二进制（linux/darwin/windows × amd64/arm64，goreleaser + 校验和）② Docker multi-arch 镜像（distroless / alpine）③ docker-compose ④ Helm Chart（PVC/ingress/HPA/values）⑤ 原生 K8s 清单 ⑥ systemd 单元 + 安装脚本；另产出离线安装包（镜像 tar + Chart + 脚本 + 校验和）。
- 后果: release-engineer 常设；`deploy/` 与 `charts/` 目录纳管；M2 起每个里程碑包含部署烟测票；对外推送镜像/Chart/二进制必须经用户确认。
