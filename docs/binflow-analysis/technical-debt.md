# BinFlow 技术债与未知项地图（证据指针文档——正文在既有产物）

> 指针层：债务四源——已知差异台账 / UNKNOWN 队列 / absent backlog / 自认妥协。采集日 2026-09-14。

## 1. 已知差异台账（51 条）

`docs/compatibility/known-divergence.yaml`——四分类：
- **BUG 26**：待对齐修复（含 npm 域 5 条、virtual 冷负 miss、update-merge 族已收口等——逐条 authority/evidence 在文件内）。
- **UNKNOWN 17**：行为未定（如经典 security 读族门姿、readers 默认组建模级两连）。
- **INTENTIONAL 7**：裁定有意差异，必带 authority（如 npm D5 400-不复刻-500、docker remote 404 负缓存 ADR-0048）。
- **UNSUPPORTED 2**。

## 2. UNKNOWN 队列（110 条，宪章 §44 纪律）

`docs/compatibility/unknown.yaml`——P0 3 / P1 50 / P2 57；域分布 STG 11 / SEC 12 / FE 7 / LOG 15 / DIST 8 / **ENT 28（最大簇：企业许可/档位/Xray 联动）** / PROTO 10 / API 8 / CFG 5 / JOB 6。
收口纪律：禁猜；P0 项须 E4 活体或 E5 官方单源证据。

## 3. absent backlog（❌78 行 = 未实现的参照面）

主账 `docs/compatibility/matrix.yaml` 的 absent 行即差距 backlog（唯一账，不在本文件复制）。集中簇：
- D07/D08 全缺位行（build-info/release-bundle——M17 已落最小面，主账行态待契约化后翻绿）；
- D06 系统与运维 absent 11（System Logs 进程日志端点族、维护面余项——见 §4 契约缺口）；
- D03 搜索 absent 8、D04 安全 absent 12（users 回显字段级/组成员暴露面等——`docs/reverse/gap-endpoints.md` 五缺口群）。

## 4. 前端侧契约缺口（FE 摆不出的后端面）

源自 parity 锚册 as-built 注记（B 表行内登记，非本文件新增）：
- SSH key 自助增删：`internal/httpapi` 全量无 SSH 端点（Profile SSH 卡缺位登记 `profile-ssh-gap`）。
- 用户能力位三旗（Can Update Profile / Disable UI Access / Disable Internal Password）：BE decode 静默吞咽、GET 回显恒出厂档——API 漂移钉 tripwire 已埋（t453 spec）。
- 仓库表单四隐藏域（maxUniqueSnapshots/repoLayoutRef/blackedOut/archiveBrowsingEnabled）：configJSON 不转发、回显缺失——须 BE 承接票。
- 系统进程日志尾随/下载端点族：现 System Logs 载体 = 审计跟踪（降级定案）。
- 服务端令牌清单端点（§9-R6）：Access Tokens 台账转持久列表的前置。
- remoteDegraded wire note 的 BE httpapi 渲染腿（T-448 §5-2 缝，在途）。

## 5. 自认妥协台账（架构层）

`docs/design/architecture.md` §11「已知妥协」+ §12「待逆向规格确认清单」（阻塞点挂 docs/reverse/）。

## 6. absent-roadmap（不在路线图的参照面——刻意不做）

- **Xray 及一切外部产品面**（D14 ⛔；宪章与 M16 指令均豁免）。
- Federation/Lifecycle/Retention Policies/Insights 等企业面 UI（参照截图在场，BinFlow 未建——`docs/ui-parity-matrix.md` 标「未建·档位外」）。
- stay-out 八项登记：parity 锚册 §9A（A8 清单 + 仓库详情中间页/E4 测试器/快搜范围页签/结果计数一致性等）。
- 交互式 import/export UI（ADR-0015 勘误②：/api/export/** 404 有意、CLI-only）。

## 7. 测试债基线

- e2e：`web/e2e/`（m8/m9/m10/m14/m16 + a11y 双主题）——两共享红基线对照定谳机制在案（T-461 注）。
- 后端：`internal/metadata/` 60+ 测试文件（race on/off 双态）。

## 8. 缺口声明（真无证据的面）

无。债务四源各有唯一事实源；本文件仅做视图。
