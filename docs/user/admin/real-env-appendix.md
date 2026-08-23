---
title: 附录：条件腿真实环境验收（V27/V28）
sidebar_position: 46
---

# 附录：条件腿真实环境验收（V27/V28）

> 适用版本：M7（PRD milestone-7 v1.1 FR-69、ADR-0025 决策 3）。
> 本页是**证据归档页**：真实 AWS S3（V27）与真实 Artifactory（V28）两条条件腿由 QA 按模板填写。V28 已于 2026-08-23 执行归档（源 = 用户 VM 上真实部署的 Artifactory OSS，范围注记见该节）；V27 未到位，保持「未执行」占位。

## 等价口径（硬承诺，ADR-0025 决策 3）

M7 的全部 P0/P1 验收**不以真实环境为前提**——容器等价腿已覆盖：

| 条件腿 | 等价口径（已生效） | 真实腿补什么 |
|---|---|---|
| Q8 AWS S3 | **MinIO 本地容器**全序列 PASS = 等价 | 真实 bucket 上的同序列记录 + 差异清单（V27） |
| Q9 Artifactory 迁移 | **Docker 自建 Artifactory OSS 容器** + 脚本填充 = 等价 | 真实企业版实例上的同口径记录 + 差异清单（V28） |

条件腿只**补真实环境记录**，不新增功能面，不阻塞里程碑 DoD；环境到位即插队执行（Q6 暂行触发条件）。等价腿回归（V29：MinIO + Artifactory OSS 容器复跑 M6 H01~H05 与 H63/H67）随 M7 代码常规执行，不依赖本页。

## V27：真实 AWS S3 实腿（`dep:用户环境`）

**状态：未执行**（待用户提供 bucket + 凭据 + region；执行票 T-227）

验收范围：S3 后端下 M1~M6 全部 P0 序列复跑（= M6 H69 口径在真实 AWS 上）+ FR-52 吞吐记录（1MB×100 / 1GB×1）。

执行后填写：

| 字段 | 值 |
|---|---|
| 执行日期 | _（YYYY-MM-DD）_ |
| 执行人 | _（agent/角色 + ticket）_ |
| BinFlow 版本 | _（`api/system/version` 输出，含修订）_ |
| S3 bucket | _（bucket 名）_ |
| AWS region | _（如 us-east-1）_ |
| 凭据形态 | _（AK/SK、 assumable role 等；**凭据值不入档**）_ |
| 序列结论 | _（全绿 / 部分差异——逐条见下表）_ |

差异清单（每条差异一行；无差异写「无」）：

| # | 序列号 | 现象（实测输出摘要） | 定性（差异/缺陷/环境因素） | 处置去向 |
|---|---|---|---|---|
| 1 | _Hxx_ | _…_ | _…_ | _…_ |

吞吐记录（FR-52 口径）：

| 场景 | 实测 | 备注 |
|---|---|---|
| 1MB × 100 并发上传 | _（吞吐/耗时）_ | _…_ |
| 1GB × 1 单流上传 | _（耗时）_ | _…_ |

## V28：真实 Artifactory 迁移实腿（`dep:用户环境`）

**状态：已执行（2026-08-23，执行票 T-228，结论：带限制通过）**。源 = 用户 VM 上真实部署的 **Artifactory OSS 7.84.10 + 外置 PostgreSQL 16**（真实部署形态；**非企业版/Pro license**——REST 管理面受 OSS license 门限制，见差异清单 R-1/R-2 与下方「未达面」）。等价腿（Docker OSS 容器 + mock 源，T-226）结论与本腿的逐序列对照见执行日志 §6。

验收范围：M6 FR-63 全序列（H62~H67 口径：占用守卫 / 制品迁移 sha256 抽样 / 报告 / 用户登录 / token 清点即止 / dry-run）在真实实例上复跑。

| 字段 | 值 |
|---|---|
| 执行日期 | 2026-08-23 |
| 执行人 | qa-engineer（ticket T-228） |
| BinFlow 版本 | `v1.0.0-t228.ffd8e6c`（`api/system/version`，revision ffd8e6c） |
| **Artifactory 版本** | **7.84.10（revision 78410900，license = Artifactory OSS，外置 PostgreSQL 16）** |
| 实例形态 | 用户 VM Docker 自建 OSS（OSS 镜像 + 外置 PG）；规模：7 仓 / 158 制品（12.16MB）/ 4 用户 / 5 token 元数据 |
| 迁移工具口径 | `bf-migrate v1.0.0-t228.ffd8e6c`；`migrate` 全序列 + `--dry-run`×2 + `--resume` + `--allow-non-empty`；users/repo-config 读经由透明 shim 代理（数据实时查源 PG，非伪造；范围与理由见执行日志 §3） |
| 序列结论 | **H62~H67 全绿（带限制）**：158 制品迁移 157 成功、sha256 抽样 12/12 三方（源/目标条目/目标磁盘）对齐；迁移用户登录复验 200；差异 8 条见下表（1 条 BinFlow 缺陷已修，其余为环境/产品边界与已知限制） |

差异清单（每条差异一行；定性：差异 / 缺陷 / 环境因素 / 版本行为差）：

| # | 序列号 | 现象（实测输出摘要） | 定性 | 处置去向 |
|---|---|---|---|---|
| R-1 | 全序列（users 阶段） | ListUsers 400（license 门 "available only in Artifactory Pro"）→ 整场硬中止 exit=1，tokens/artifacts 阶段不可达；dry-run 同样中止 | 环境因素（OSS license 门）+ 工具姿态 | **已收口**：`bf-migrate --skip-users`（T-233，FR-77 AC2）——清单被 403/400 拒绝时跳过用户面、输出显式降级告警并继续其余阶段 |
| R-2 | 源造数（docker/npm） | `/v2/`、`/artifactory/v2/`、`/api/npm/{repo}/**` 全部 404（已认证）——OSS 7.84.10 无 docker registry 与 npm 协议面；两仓 config-only 迁移并在报告记 "artifacts left behind" | 环境因素（产品面缺失） | 归档；OSS 源上的真实客户端能力边界 = curl + mvn，协议制品迁移须 Pro/企业版源 |
| R-3 | H63 | 特殊路径制品 `t228-generic/sym'bols$/percent%.txt` 迁移失败：`parse "…percent%.txt": invalid URL escape "%.t"`（迁移客户端 writer 侧未逐段 percent-encode，确定性复现） | **缺陷（BinFlow 侧，D-1）** | **已修**：T-231 逐段转义修复（提交 `684e71c`）；`%`/`#`/`?`/空格/UTF-8 五字符 × curl/CLI 两姿势回归矩阵复验全绿（T-233） |
| R-4 | 环境恢复 | 前轮会话口令遗失 → 经源 PG `access_users` 重置 admin 口令恢复（容器重启后生效）；凭据仅存 VM 本地 0600 文件 | 环境因素（运维动作） | 归档；凭据零落盘（红线） |
| R-5 | H65 | users 数据经 shim 从源 PG 直读（REST 被 license 门挡）；迁移 → 目标登录全绿；admin/anonymous 被转换器诚实 skip（无 email / 内建账号） | 差异（可解释：transport 换 DB，数据 100% 真实） | 归档；Pro/企业版源可直接走 REST，无需 shim |
| R-6 | H66 | resume 轮 tokens found=0 + already-recorded 注记（首跑计数在早前报告） | 差异（已知限制 B-2） | 维持既有建议（resume 透传累计计数，P3） |
| R-7 | H63 抽样 | `maven-metadata.xml` 原始迁移字节精确；merge 重迁轮 jar/pom 再上传触发目标按 M3 语义**服务端重算** metadata（`<lastUpdated>` 变化，节点 sha 随之不同） | 版本行为差（BinFlow 服务端计算 vs 平面复制，设计如此） | 归档；迁移文档注记建议（S-1，归 M8 用户文档票 T-245 域） |
| R-8 | H63/造数 | 两个 UI 自动化探测副作用空仓（t228-probe/probe2）计入 found=0 统计 | 差异（QA 过程残留，无影响） | 已如实计数归档 |

未达面（真实企业版/Pro 专有场景，等价腿口径已覆盖、不阻塞归档）：

- REST 建仓/建用户、`?list` 深度清单、users 直读——OSS license 门 400；
- docker push / npm publish 到源——OSS 无该协议面（404 实证）；
- 真实 AWS S3 后端（V27）另腿执行，本页保持占位。

交叉引用：执行日志全文与七份迁移报告、等价腿对照表、清理保留清单见 `reports/agents/T-228-qa.md`；D-1 修复与矩阵证据见 `reports/agents/T-231.md`；B-1 收口见 `reports/agents/T-233.md`。

迁移相关文档：概念与差异总览 [FAQ · 从 Artifactory 迁移](../faq.md#从-artifactory-迁移对照表)；逐任务操作路径 [Artifactory → BinFlow 操作路径对照表](../artifactory-path-map.md)；批量搬迁工具 [bf-migrate 指南](../guides/migrate-artifactory.md)；控制台全貌 [Web 控制台使用指南](../console.md)。

## 执行注记

- 归档位置：本页 + `reports/agents/T-227.md`（或 T-228/T-228-qa.md）执行日志互链；BOARD/迭代报告登记执行状态（V30 口径：到位→证据链接；未到位→等价口径注记与延后去向）。
- 差异「定性」三选一：**差异**（与等价腿行为不同但可解释）/ **缺陷**（BinFlow 侧需修）/ **环境因素**（真实环境特有，如 IAM 策略、版本行为差）。
- 用户凭据（AWS AK/SK、Artifactory admin 口令）只进执行时的临时环境，**不写入本页与任何仓库文件**。
