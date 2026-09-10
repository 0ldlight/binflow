# Artifactory 全量审计（Phase 0）— 终稿

> 程序：**ARTIFACTORY FULL REIMPLEMENTATION PROGRAM**（用户 57 节总令，2026-09-10 立项）。
> 本阶段：Phase 0 — FULL AUDIT。**禁止修改核心代码**；只写 `docs/reverse/**`、`docs/compatibility/**`、`docs/ai-engineering/**`、`reports/**`。
> **状态：✅ Phase 0 收口（2026-09-11 01:0x）——§49 十六项全 ✅；入口放行 Phase 1（架构 v2 设计族，宪章 §50）**。
> 配套任务账：#1~#8 全部 done；BOARD 时序日志 2026-09-10 intake 段 + 推进段 + 2026-09-11 收口段。

## 1. 三个一级证据源验证（2026-09-10 执行，2026-09-11 复核）

| 源 | 路径 | 版本 | 状态 |
|---|---|---|---|
| **SOURCE A** 反编译源码 | `/Users/lzw/workspace/artifactory-decompiled`（`reverse-src/artifactory` 软链已就位） | **7.161.24**（构建号 7.161.14；access 7.191.x / jfrog-commons 13.21.0 / metadata 7.490.1） | ✅ 可用。569 Java 模块 / 14,702 .java / 206MB（CFR）；12 Go 服务 Ghidra 伪码 ≈84 万函数 / 1.7GB；前端 661 文件（source-map 次优还原，`sourcesContent` 已剥离）。`backend-maven/modules.json` 提供结构化模块清单。13 文件含 CFR 失败占位。只读铁律生效 |
| **SOURCE B** 运行时参照 | `http://localhost:8082`（docker 容器 `artifactory`） | **7.161.20** | ✅ 已复活并过 E4 基线（§2 末行）：ping 200、55 addons、Enterprise Plus Trial license + entitlements、全新引导实例 |
| **SOURCE C** 安装包 | `/Users/lzw/Downloads/artifactory-pro-7.161.16` | **7.161.16** | ✅ 可用。4.0G；`app/`（23 个微服务/组件目录 + bin + third-party + doc）+ `var/`（bootstrap + etc） |

### 1.1 版本偏斜注记（环境事实，非阻断）

三源分属 7.161.x 三个补丁版（A=7.161.24 / B=7.161.20 / C=7.161.16）。裁定：
- 冲突时按宪章证据序 **Runtime > Decompiled > Distribution**；
- 一切规格/契约引用**必须标注证据源与版本**（六线产物均已执行——frontend routes.yaml 标注 7.161.12/7.191.14 MFE 版本、enterprise 标注四源、logging 标注 B 实测快照）；
- C 与 A 差 8 个补丁号，安装包分析以宪章指定 7.161.16 为准，涉及与 A 冲突的结构性结论时以 A 为准并留痕（distribution behavior.md 已留）。

## 2. SOURCE B 参照实例诊断时间线（2026-09-10）

| 时刻 | 事件 |
|---|---|
| 06:43Z | 容器启动（用户侧重启；此前 M16 挂账已知「pro router 不就绪」） |
| 06:46 | 引导首错：`Database connection check failed Could not determine database type` |
| 06:48 | router「Master key is missing. Pending 215s / 5m0s timeout」；jfcfg 对 access:8046 connection refused（Retry 110）；frontend ping 150 次 ECONNREFUSED；topology HikariPool 建连失败 |
| 07:0x | conductor 接手诊断：**网络通**（artifactory 与 postgres 同在 `artifactory-net`，DNS 解析成功）；**DB 通**（psql `select 1` 于 artifactorydb 成功）；卷为**从未成功引导的空卷**（data/=176K 版本标记，无 filestore、无 master.key——旧活体实例数据不在此卷） |
| 进行中 | restart 后 Tomcat 11 开始部署 war；卡点收敛为 **access 服务未生成 master.key**（Go 服务族等待超时中）。观察窗内若 access 完成部署后仍无 master.key → 深挖 access-service.log；出路登记：空卷重建容器（无数据可失，但属删除类操作——**须用户确认**） |
| ~15:0x | **复活收口**：引导最终完成（容器 up 47min 稳定）。conductor E4 基线验证：`api/system/ping`=200；`api/system/version`=7.161.20（package_handler 5.675.40；**addons 55 项全开**；entitlements：EVENT_BASED_PULL_REPLICATION/REPO_REPLICATION/MULTIPUSH_REPLICATION=true、SMART_REMOTE_TARGET_FOR_EDGE=false）。实例为**全新引导**（repos 仅 example-repo-local；var/data 9.7M / 13 服务目录）→ 干净受控参照基线（差分用）。同时 E4 捕获：`var/log` 45+ 文件 taxonomy、`var/etc` 17 目录（system.yaml + full/basic 模板）→ 喂任务 #4/#6。**任务 #1 收口，E4/E5 腿解锁** |

> 环境要点（值脱敏）：容器 env 指向外部 postgres（`artifactory-net`，库名 artifactorydb，凭据经环境变量注入，不入库）；`JF_SHARED_RESTRICTEDMODE_ENABLED=true`。

## 3. 既有资产盘点 → 全量落地对账（2026-09-11 收口终态）

BinFlow 已是一个月的兼容工程沉淀，**不从零开始**。对账终态（全部 ✅ 落地）：

| 宪章要求产物 | 落地物 | 终态 |
|---|---|---|
| `docs/reverse/artifactory-module-catalog.yaml` | 569 模块全量（E1，evidence-index-only 头注） | ✅ |
| `domain-map.yaml` / `dependency-map.yaml` | 26 域 / 147 模块映射；138 节点 / 465 org 内边（475 去重 10） | ✅ |
| `api-inventory.yaml` | 14 域 × 106 资源 × 415 resource×操作行（聚合粒度） | ✅ |
| `configuration-map.yaml` | 22 服务段 schema + JF_* 映射规则 + secrets 解析链 + 模板差异 | ✅ |
| `protocols/registry.yaml` | 宪章 §9 全 22 字段 × 57 包型零丢失（13 P0 全字段） | ✅ |
| `docs/reverse/logging/` | taxonomy 93 文件 + format-spec 逐字段 + evidence/ 12 份 E4 因果样本 | ✅ |
| `docs/reverse/distribution/` | layout.yaml + behavior.md 九动词 + evidence/ 8 件 | ✅ |
| `docs/reverse/enterprise/` | feature-catalog（81 AddonType + 17 动态装配 + 142 能力）+ feature-gates（39 判定 + 45 entitlement）+ license-behavior + runtime-behavior | ✅（含一轮返工，见 §7） |
| `docs/reverse/frontend/` | routes.yaml 119 条 + screens.yaml 41 屏 + api-map.yaml（29 屏 + 50 SSR 端点）+ README | ✅ |
| `docs/reverse/security/` `storage/` `jobs/` | 三份 README 索引 + 缺口对账（11/10/6 条） | ✅ |
| `docs/compatibility/capability-matrix.yaml` | ⚖️ **≡ matrix.yaml 主账，不建副本**（§4 裁定） | ✅（裁定落地） |
| `docs/compatibility/divergence.yaml` | ⚖️ **≡ known-divergence.yaml，不建副本**（§4 裁定） | ✅（裁定落地） |
| `docs/compatibility/unknown.yaml` | **110 条**（3 P0 / 50 P1 / 57 P2；47 处可执行 probe；12 既有源引用计数防双账） | ✅ |
| `docs/compatibility/logging-matrix.yaml` | 23 家族 × BinFlow develop 实读（equivalent 0 / partial 13 / none 10） | ✅ |
| `docs/ai-engineering/artifactory-binflow-gap.yaml` | 26 域映射总账 + §46 14 子域覆盖率快照（REST 面 39%） | ✅ |
| `reports/artifactory-full-audit.md` | 本文件终稿 | ✅ |

## 4. 台账归属裁定（防双账——Phase 0 元决策）

1. **capability-matrix ≡ matrix.yaml**：宪章命名的 `capability-matrix.yaml` 与既有 `docs/compatibility/matrix.yaml` 功能同一。裁定：matrix.yaml 继续为唯一账（四态 + Score 计量已运营一个月），**不建并行副本**；gap 总账以引用方式消费它（已执行：gap.yaml 只引 matrix 锚点不复制行）。
2. **divergence ≡ known-divergence.yaml**：同理，不建 `divergence.yaml` 副本。
3. **模块目录族 = 证据索引而非设计输入**：宪章 §7 要求的 module-catalog 含类/包结构枚举，与 CLAUDE.md clean-room 铁律（docs/reverse 只放行为规格、禁类名）表面冲突。裁定：目录文件头强制声明 **evidence-index-only**；它服务于发现与对账（§23 映射），**禁止**作为 BinFlow 结构设计依据（宪章 §39~41 本就禁 class-to-class 翻译）。行为规格 md 的句式纪律不变。**六线 13 个 yaml 全部带头注，收编逐件验证**。
4. **commit 规范冲突**：宪章 §53 的 `[compat]` 前缀 vs 仓库 conventional commits（CI 依赖）。裁定：维持 conventional commits，域以 scope 表达（如 `docs(reverse): ...`）。
5. **既有 UNKNOWN 纪律一致性**：宪章 §44「UNKNOWN 禁止悄悄实现成猜测」与现行 Compatibility Policy（禁猜测补齐）同构，无冲突。

## 5. §49 审计完成标准勾稽（16 项）— 全 ✅

| # | 标准 | 状态 | 凭据 |
|---|---|---|---|
| 1 | Decompiled source indexed | ✅ | modules.json + catalog.yaml + domain/dependency 图谱 + inv 系列消费 |
| 2 | Distribution package indexed | ✅ | distribution/layout.yaml + evidence 8 件 |
| 3 | Runtime verified | ✅ | 2026-09-10 复活，E4 基线过（§2 末行） |
| 4 | Module catalog generated | ✅ | 569 模块，与 modules.json 对账不重不漏 |
| 5 | Capability map generated | ✅ | feature-catalog 142 能力 + gap 总账 26 域 |
| 6 | API inventory generated | ✅ | api-inventory.yaml 415 行 |
| 7 | Protocol registry generated | ✅ | registry.yaml 57 包型零丢失 |
| 8 | Enterprise features catalog generated | ✅ | 81 AddonType + 17 动态装配 + 三层门控 |
| 9 | Logging architecture mapped | ✅ | taxonomy 93 + format-spec + logging-matrix 23 家族 |
| 10 | Frontend mapped | ✅ | routes 119 / screens 41 / api-map + 三 README |
| 11 | Distribution mapped | ✅ | 九动词行为规格 |
| 12 | Storage mapped | ✅ | storage/README 索引 + 10 条缺口（BinaryProvider 顶级 UNKNOWN 入队 U-STG-01） |
| 13 | Security mapped | ✅ | security/README 索引 + 11 条缺口 |
| 14 | Async/job mapped | ✅ | jobs/README 四轴判定（有证据/UNKNOWN 逐项） |
| 15 | Binflow gap generated | ✅ | artifactory-binflow-gap.yaml 26 域 |
| 16 | Unknown queue generated | ✅ | unknown.yaml 110 条 |

## 6. 循环排程终态（Loops A~G 全收口）

1. **Loop A**：✅ 参照实例复活（任务 #1，2026-09-10）。
2. **Loop B**：✅ domain-map + dependency-map（任务 #2）。
3. **Loop C**：✅ distribution 映射（任务 #3），configuration-map 由任务 #6 并行收口。
4. **Loop D**：✅ 日志体系逆向（任务 #4，P0/P1 缺口补上）。
5. **Loop E**：✅ 企业功能目录（任务 #5，含一轮返工）。
6. **Loop F**：✅ 派生三件（任务 #6）+ 目录化索引（任务 #7）。
7. **Loop G**：✅ unknown 队列 + gap 总账 + logging-matrix（任务 #8）+ 本报告终稿。

## 7. 六线收编记录（2026-09-10/11，conductor）

| 线 | 交付 | 收编验证 |
|---|---|---|
| #2 domain/dependency | 2 yaml | 头注 ✓ YAML ✓ 零泄漏 ✓；465 边对 475 原始去重核可 |
| #3 distribution | 4 件 + evidence 8 | 同上；「rollback/restore 无工具」为确证性结论 |
| #4 logging | 3 件 + evidence 12 | 同上；测试资产删净（repo 400 / artifact 404 / storage 零残留） |
| #5 enterprise | 4 件 | **返工一轮**：初版两个 yaml 实为 markdown-in-yaml（`---`/`## `/管道表）+ `admin/JFrog@2026` 明文落盘 3 处——conductor 脱敏 + 退回 agent 重构（表格→序列、散文→字符串字段/挪 md），复验 ruby 解析过、零凭据。**教训入收编纪律：yaml 交付必须附实跑解析证据，且以落盘终态为准** |
| #6 派生三件 | 3 yaml | 头注 ✓ YAML ✓ 零泄漏 ✓；agent 自报修复过 1 处 colon-in-plain-scalar |
| #7 目录化 | 7 件 | 三 yaml ✓；版本偏斜裁定执行（7.161 access MFE 新页只登记不回改 7.84.10 旧稿） |
| #8 合成 | 3 yaml | 110/26/23 条目数 agent 自报与结构抽验一致；matrix.yaml 与 known-divergence.yaml 零改动（防双账裁定执行） |

收编共性验证：`git status` 新增面仅 docs/reverse|compatibility|ai-engineering + reports（17 路径）；全树 `JFrog@` grep 零命中；BOARD.md 仅 conductor 时序两条。

## 8. §55 十问自评（Loop 收口）

1. **本轮读取了哪些 Artifactory evidence？** 三源全量：A 569 模块反编译树（catalog→domain→dependency 图谱化 + addon/license/logging 配置走读）；B :8082 活体（E4 基线 + 日志因果样本 + MFE 路由提取 + license/addons/entitlements 实测）；C 4.0G 安装包（九动词脚本 + 模板 + addon.properties）。
2. **新发现了什么？** ① 17 项动态装配 addon 层（jar 自描述 `META-INF/addon.properties`，不在 AddonType 枚举，既往全部清单未覆盖）；② BinaryProvider 存储链实现类不在反编译 Java 树（filestore/S3 行为只能活体核对）；③ logback `%i`=UTC 轮转时刻时间戳、双触发 25MB/**23h** 隐藏默认；④ jfrt/jfac 请求日志 reqLen/resLen 字段序相反；⑤ 仓库删除产生 `D|PRS` 审计事件、创建不产生；⑥ rollback/restore 官方无工具（确证）；⑦ license hash 末位编码 10 档产品 tier；⑧ 三层门控（装配/license/entitlement）与「addon 激活≠license 允许」铁证。
3. **修复了什么 compatibility gap？** 零——Phase 0 是审计阶段（by design，禁改核心代码）。产出 gap 总账 26 域即修复地图。
4. **新增了多少 tests？** 0（同上）。沉淀 **47 处可执行 E4 probe** 于 unknown.yaml，是后续测试的种子。
5. **differential test 是否通过？** NOT APPLICABLE——无代码变更。
6. **UAT 是否通过？** NOT APPLICABLE——同上。
7. **是否引入 regression？** 否——仅新增 docs；matrix.yaml / known-divergence.yaml 主账零改动；核心代码零触碰。
8. **哪些 unknown 仍未解决？** 110 条入队（3 P0 / 50 P1 / 57 P2），另有 12 个既有「待验证」源引用计数。P0 三条：U-STG-01 BinaryProvider 存储链、U-PROTO-01 docker remote 代理 v2、U-ENT-05 entitlement 同源。
9. **哪些 divergence 是 intentional？** 本轮零新增——审计只登记 gap 与 unknown；intentional divergence 裁定权仍在 known-divergence.yaml 主账（未动）。
10. **下一轮最重要的 gap 是什么？** Phase 1 启动（宪章 §50）：架构 v2 设计族十份。设计输入已备：gap 总账 verdict 倾向（KEEP 8 / REFACTOR 8 / REWRITE 2〔build-info、release-bundle〕/ NEW-BUILD 2〔24 个 addon 包型绿地、ha-cluster〕/ DEPRECATE 候选 3 / REMOVE 候选 1〔xray-curation，待产品终裁〕）+ P0 unknown 三条 + §46 覆盖率快照（REST 面 39%，D07/D08/D10/D11/D13/D14 为 0）。

## 附录 A：运行时日志样本（E4 首批——日志体系逆向种子，2026-09-10 容器引导期捕获）

样本 1（Go 服务，console 流，ANSI 已剥离）：
```
2026-09-10T06:48:51.478Z [jftpl] [WARN ] [236b6fc8a090009fe8861683b4e50b10] [c.j.t.s.s.HealthCheckRunner:42] [topology-scheduler-2] - Service node is unhealthy
2026-09-10T06:48:56.134Z [jfrou] [INFO ] [4f841b049e5ff7f0] [common_logger.go:24] [main] [] - Master key is missing. Pending for 215 seconds with 5m0s timeout
```
样本 2（Java 服务，console 流）：
```
2026-09-10T06:48:54.198Z [jfcfg] [INFO ] [] [ingleTenantAccessThinClient:60] [main] - Cluster join: Retry 105: Service registry ping failed...
```
样本 3（结构化 JSON 行——tomcat 服务走 JSON console 形态）：
```json
{"log_name":"tomcat-catalina.log","app":{"datetime":"2026-09-10T06:46:49.263Z","service":"tomcat","loglevel":"WARNING","class":"org.apache.tomcat.util.digester.Digester","message":"..."}}
```

（逐字段终版规格已由任务 #4 体系化：见 `docs/reverse/logging/format-spec.md`；本附录保留为引导期一手存档。）
