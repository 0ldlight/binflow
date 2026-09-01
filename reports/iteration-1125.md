# Sprint 1125 迭代报告 — M15 拆票落板（25 票/十三波）；B0 双锚派发

**日期**: 2026-09-01 09:2x
**上轮**: Sprint 1124（等待轮——拆票阅读期）

## 拆票收货（tech-lead → done）

`docs/M15-SPLIT.md` + BOARD「M15 票批 v1」：**25 票**（P0×8 / P1×11 / P2×6 含条件票 T-431），B0~B12 十三波全宽 2。关键路径 = AQL 主轴六波串行链（B0 锚→语言→内核→门→端点→老搜索→QA 中期→尾部）；virtual / 复制包 B / FE 链三条副线全挂主轴空侧 lane 零反压。断言反转三处预归属（SR-03/04→T-417、tree-empty-virtual→T-416、mint→T-410）。

**conductor 批复六条口径**（BOARD 留痕）：T-418 独立成票认可 / L2·列选器·双值归属照 SPLIT / ADR↔aql.md 软缝维持并行 / T-420 归运行时裁量。

## 派发

**B0 双锚并行在途**：T-407（reverse-engineer——aql.md + t226 活体核验 + 口径归一；INC-1 差集法纪律重申）+ T-408（architect——ADR-0043 + architecture 搜索域增量节）。L20 就绪 + ADR Accepted 后开 B1（T-409 语言 + T-410 mint 插空）。

## 状态

M15：**0/25**（B0 在途×2）。M14 全线闭合。HEAD[develop]=本轮推送。
