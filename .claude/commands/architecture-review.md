---
description: 架构一致性巡检（ADR 纪律 + agent-graph owns 违规 + 软缝勘误欠账）
---

执行架构一致性巡检，只读不写（$ARGUMENTS 可限定范围）：

1. **ADR 纪律**：DECISIONS.md 是否只追加无改写；Errata 段格式（原行保留作废-替换）合规；开放 Errata 清单。
2. **域所有权违规**：近 N 轮 git log 中，各票改动路径 vs `docs/ai-engineering/agent-graph.yaml` owns——越权写入围点。
3. **依赖方向**：import 禁令抽检（如 internal/build 禁 import webhook/search/httpapi 等 ADR-0045 条款）；适配器显式装配无 init() 自注册。
4. **软缝协议**：效力序（用户裁决>规格票>ADR>PRD）执行情况；勘误欠账清单（ADR-0044 勘误行等待办）。
5. **文档-实现漂移**：docs/design/architecture.md 与 internal/ 包结构的显著漂移点。

派 `architect` 执行（或 conductor 亲执小范围），产出巡检报告（问题清单+严重度+补救建议）。
