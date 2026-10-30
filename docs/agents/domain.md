# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`GLOSSARY.md`** at the repo root（单上下文布局，无 `GLOSSARY-MAP.md`）。
- **`DECISIONS.md`** at the repo root：本仓库的 ADR 家（clean-room / 架构 / 部署矩阵裁定），只追加 + Errata 勘误。读与你将要工作的领域相关的 ADR-NNNN 条目。

以上文件不存在时**静默继续**：不要标记缺失、不要主动建议创建。`/domain-modeling`（经 `/grill-with-docs` 与 `/improve-codebase-architecture` 触达）会在术语真正被解决时惰性创建 `GLOSSARY.md`；ADR 由 architect 角色追加进 `DECISIONS.md`，不新建 `docs/adr/`。

## File structure

单上下文：

```
/
├── GLOSSARY.md
├── DECISIONS.md            ← ADR（只追加 + Errata）
├── docs/
│   ├── design/             ← 架构规范
│   └── ai-engineering/     ← agent-graph.yaml（域所有权）
└── ...
```

## Use the glossary's vocabulary

输出中的领域概念（票标题、重构提案、假设、测试名）用 `GLOSSARY.md` 定义的术语，不要漂移到 glossary 明确避免的同义词。概念不在 glossary 里是信号：要么你在发明项目不用的语言（重考虑），要么是真缺口（记给 `/domain-modeling`）。

## Flag ADR conflicts

产出与既有 ADR 矛盾时显式标出，不静默覆盖：

> _与 ADR-0001（clean-room 铁律）矛盾，但值得重开，因为…_

`DECISIONS.md` 是只追加 + Errata 制度：推翻既有裁定走 Errata 勘误，不原地改写。
