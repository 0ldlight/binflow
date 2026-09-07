# BinFlow Compatibility Engineering 体系

> 兼容工程的唯一入口。设计规范：`docs/ai-engineering/compatibility-engineering.md`（契约模式/状态机/L0~L12/四分类/金样/Score 公式）。
> Owner：compatibility-engineer（agent-graph owns 本目录全树）。

## 目录

| 路径 | 内容 |
|---|---|
| `matrix.yaml` | 兼容矩阵主账（四态 ✅/◐/❌/⛔+超集；迁移自 docs/reverse/rest-compat-matrix.md v1 冻结行集 195 行） |
| `contracts/` | 可执行契约（`<domain>/<feature>.yaml`，十七字段：feature/surface/request/headers/query/authentication/status/response headers/response body/artifact bytes/metadata/side effects/timing/error behavior/version/evidence/confidence/status） |
| `probes/` | 可复跑实验（curl/脚本，登记于契约 evidence） |
| `fixtures/` | 差分夹具（请求体/制品样例/normalize 规则样本） |
| `diffs/` | 差分产物登记（全文在 reports/compatibility/） |
| `known-divergence.yaml` | 已知差异台账（四分类 BUG/INTENTIONAL/UNSUPPORTED/UNKNOWN；INTENTIONAL 必带 authority） |
| `protocol/` | 每协议兼容细则（自 docs/reverse/<proto>.md 提炼的可执行面） |
| `golden/` | Golden Behavior Dataset（行为标准；更新须 compatibility-engineer 评审） |

## 契约状态机

```
DISCOVERED → SPECIFIED → IMPLEMENTED → VERIFIED
                ↘ DIVERGENT（差分不一致待裁）
                ↘ BLOCKED（参照断供等）
                ↘ INTENTIONAL（裁定有意差异，必带 authority）
```

## 现状（2026-09-08 脚手架期）

- matrix.yaml 为**迁移占位**：行集迁移票（M3-1）将把 rest-compat-matrix.md 的 195 冻结行机读化迁入。
- known-divergence.yaml 为**种子**：首批收编票（M3-2）将收敛各票「契约漂移登记」。
- 参照实例（7.161 pro + t226 OSS）**双双损坏**（Q10 悬置）——修复前差分降级金样单边模式（mode=golden-only）。
- 差分执行底盘：`ci/protocol-matrix.sh` 十腿（L5 雏形）+ `tools/difftest/`（M3-4 骨架票）。
