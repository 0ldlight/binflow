# 源码分析 · 企业/Addon 面（证据指针文档——正文在既有产物）

> 指针层。行为规格正文导航。

## 1. 规格文件

| 文件 | 覆盖 |
|---|---|
| `docs/reverse/enterprise/feature-catalog.yaml` | 企业功能目录（91 条 + 外部依赖标注——inv-4） |
| `docs/reverse/enterprise/feature-gates.yaml` | 80 项 AddonType 许可门控清单 |
| `docs/reverse/enterprise/license-behavior.md` | 许可加载/档位行为 |
| `docs/reverse/enterprise/runtime-behavior.md` | 企业面运行时行为 |
| `docs/reverse/inv-4-addons.md` | 全量盘点分区4：HA/Xray/Distribution/Build-info/Projects/复制/联邦/插件/事件/许可/DB/存储后端 |

## 2. 域规格（企业链）

- Build-info：`docs/reverse/build-info.md`（端点族/数据模型/权限面两出口/webhook·AQL 联动/OSS 档位核验）——BinFlow 对位 ADR-0045。
- Release Bundle：`docs/reverse/release-bundle.md`（源侧 18 端点 + Distribution 侧 v1/v2、冲突三态 202/200/409、状态机）——ADR-0046。
- Distribution：`docs/reverse/distribution/`（README/behavior/layout/evidence）。
- 复制/联邦：`docs/reverse/replication.md`（push/pull/事件驱动、全局控制、联邦概念）——ADR-0021。
- 生命周期治理：matrix D11（2 行 ❌/⛔）。

## 3. 对账

`docs/compatibility/matrix.yaml` D07（11 行全 ❌——M17 最小面已建，主账行态滞后待契约化）/ D08（7 行）/ D14（外部产品面 ⛔）。

## 4. 档位口径

参照实例 addons 全开（pro）——门控行为须以 feature-gates.yaml + license-behavior.md 为准，不以参照实例在场推定 OSS 档可用。BinFlow 自有 license 三档（ADR-0032）与 addon 注册表（ADR-0033）是刻意异构。

## 5. 缺口声明

ENT 域是 UNKNOWN 最大簇（28/110——Xray 联动/HA/Insights 深层行为）；外部产品面（Xray/Pipelines/…）规格属 D14 ⛔，永不进 BinFlow 对齐义务。
