# 裁决素材：spec §1.3 build.timestamp 漂移（maven unique-snapshot mint 时间戳源）

- 来源：L015-2 素材整理票（conductor 指令③）；证据=reports/compatibility/l0142-wire/probe-log.md N1 节 + T-L014-2.md Compatibility 漂移点上报（spec conflict）。
- 性质：**规格冲突**（docs/reverse/maven-npm-pypi.md §1.3 vs 7.161.20 双端 wire）——按效力序上报 conductor 裁定；本文件只出两案对比素材，不构成裁定。
- 席位建议：pending-rulings **R-17**（R-16 已被 rest/properties-root-posture 候裁占用；编号归 PM 登记）。

## 冲突面

| 侧 | 主张 |
|---|---|
| spec §1.3 | 「ts 优先取 build.timestamp property」——改写时间戳取节点 property `build.timestamp` 优先 |
| wire（7.161.20，两次实测） | mint 路径**均用服务端 now**（rows 5/9）；build.timestamp 仅落为节点 property，从不 steer mint。携带形态=**deploy 矩阵参数**（`;build.timestamp=…`，随 PUT 路径尾随）；epoch-millis 等其它携带形态未探（open set） |

BinFlow as-built（T-L014-2）：从 wire（服务端 now）——与参照活体一致、与 spec 文本相反。

## 两案对比

| | 案甲：规格勘误（按 wire 定案） | 案乙：补探针后裁（open set 取证前置） |
|---|---|---|
| 内容 | maven-npm-pypi.md §1.3 该句改为「ts=服务端 now；build.timestamp 仅作为节点 property 落盘（deploy 矩阵参数携带形态），不参与 mint」 | 先探 epoch-millis / header（X-*-style）/ property 键拼写等携带形态是否触发优先取——若某形态触发则 spec 句部分成立（条件化改写），否则归案甲 |
| 依据 | 双次活体直证 mint=now；现有形态（矩阵参数）已证不 steer | 「未探≠不存在」——参照实现对 property 优先取的逻辑可能存在但触发面窄（clean-room 不猜，反向也不猜） |
| 成本 | 一句规格勘误（reverse-engineer 域）；BinFlow 零改动（as-built 即 wire 形） | 一轮参照探针（3-5 臂：epoch-millis 矩阵参数/X-Property 头/预置 property）后仍需回案甲收尾 |
| 风险 | 若存在未探触发形态，勘误句过强（可加「已探形态内」限定对冲） | 取证成本先付；结论大概率归甲（两次实测+代码路径旁证） |

**建议立场（compatibility-engineer，供裁）**：案甲 + 限定语对冲（「在已探携带形态（deploy 矩阵参数）内，build.timestamp 不参与 mint」）——零实现成本、双证在案、open set 残余以限定语兜底；若 conductor 要求穷尽取证再走案乙。

## 关联

- BinFlow 实现：internal/adapter/maven/snapshot.go（mint(now, N)——维持不动，两案下均无需改）。
- 差分面：mint 时间戳随归一化（时间戳值不判），断言面为**改写名形态**（{baseRev}-yyyyMMdd.HHmmss-N）非 ts 值本身——两案均不触契约断言。
