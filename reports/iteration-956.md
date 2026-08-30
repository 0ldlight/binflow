# Sprint 956 迭代报告 — T-370 收口（PM 文面裁定包，`2c0d243`）；三项终裁开放；T-364 在途

**日期**: 2026-08-30 12:51
**上轮**: Sprint 955（等待轮）

## T-370 → done（develop=`2c0d243`，双远端）——M13 5/23

PM 文面裁定包全量落笔：

- **M12 PRD v1.1（十三处）**：cargo-409 双姿态四处补全（收口笔 `346485e` 仅落 2/8——「FR-110.4 已落」不实，本票补余六处）+ FR-113.5 正文对齐 AC5 + flat 八处折叠口径 + FR-107 AC2 fail-closed 加注。
- **M13 PRD v1.1（廿一处）**：重试语义五处锚定 webhook.md 官方值（4xx 不重试/固定 10s/retryCount 5/30s 超时）+「36 事件」→13 域 66 型 + envelope 时间戳断言删除 + SSRF 键落定 + Q4 取证入文 + K47~K50 回填。
- **裁定材料上板**：D-10 四臂双证对照（PM 建议=对齐 409）+ Q4 就绪（官方矩阵 Non-commercial ❌ → 建议维持 pro+）+ ADR-0041 决策 4 冲突登记（建议 architect 回填对齐官方投递参数）。

conductor 复验：双 PRD 版本行 v1.1 在案 + grep 旧措辞零残留。遗留：ROADMAP 两处版本引用滞后（PM 票内纪律禁写，下批随收口）。

## 三项终裁开放（本轮 AskUserQuestion 已呈用户）

① **Q3/D-10** NuGet 同字节幂等 409-vs-201（PM 建议 409；翻转则触发 T-378 小票）；② **Q4** webhook 槽档位（建议维持 pro+/KindFeature）；③ **ADR-0041 决策 4** 投递参数回填对齐 webhook.md 官方值（效力序 webhook.md > ADR）。

## 在途

- **T-364 投递引擎**（dev-go-storage agent `a9a9f1dc`）：B2 后半，报告未落。完成后验四 AC（outbox 幸存 kill -9/固定间隔重试+死信/HMAC 签名链/SSRF+指标族）+ §6 cmd 接缝 diff 移交。

## 状态

M13：**5/23**。在途 ×1。HEAD[develop]=`2c0d243`。
