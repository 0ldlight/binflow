# Iteration Report — LOOP 009（list 补参 P0+P1 / stray+users / 四 gate 改期 / Errata+评估）

- **Iteration**: 009
- **Date**: 2026-09-12
- **Goal**: 26 分歧臂收敛主战场 + 开放 BUG 两条清偿 + 台账纪律件

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L009-1 四 gate | compatibility-engineer | ✅ 改期 LOOP 010（authority 阻塞合法顺延）+ R-12 候裁建议 |
| L009-2 list P0+P1 | dev-go-storage | ✅ 25/32 归一 SAME；含两轮返工（B 空值短路+A int32 界——均回归类） |
| L009-3 stray+users | dev-go-core | ✅ 26 臂逐字 SAME；含返工（XSS 源型移植+NameValidator 三臂） |
| L009-4 Errata+评估 | architect | ✅ ADR-0050 Errata 一 + R-6 四路径排程建议 |

## Implementation
- list 面重写：递归三连修（死守卫根因）、形态五项、行为差三项、listFolders/includeRootPath、空值守卫（isNotBlank 语义）、int32 界钳制
- permissions：两级解码（URLDecoder 复刻）、XSS 源型正则（`^.*<(|/|[^/>][^>]+|/[^/>][^>]+)>.*$`）、NameValidator 全臂、keyed 面 4xx envelope 收口、users 404 envelope（泛化文案）

## Differential
- 32 臂矩阵两轮：**25/32 归一 SAME，余 7 臂全落 P2 范围零越界**；26 臂 permissions/users 逐字 SAME；四角臂+三字面量臂活体双轮
- 换行角新发现：参照 %0A 路径段路由层不可达（BinFlow 可路由）——候立账

## Review
- A/B 双 REQUEST_CHANGES（四 blocking：空值臂回归、int32 窄窗、XSS 双向分歧、三字面量臂）→ 全数返工清偿（A 预授修法+B 探针先行纪律合流：源移植+活体复核）
- 双审战绩 10/10（本轮四 blocking 中两个系回归类——重写面的经典风险被系统性拦截）
- 差分单臂复验延至 LOOP 010 合并（双审认可）

## Interruptions
- 第四波限额击落双 reviewer（翻绿已先落账）→ 断点复活零损失

## Compatibility Score
- matrix：D01-R05 收窄+confidence high；台账 **30 条目**（resolved +3 累计 19；params 家族收窄三 P2 参；新账 3：vendor CT BUG/空 actions BUG/DELETE 200 UNKNOWN〔与 L007-1 相抵诚实存证+两态探针门〕）
- ADR-0050 Errata 一；LOOP 010 池：P2 三参尾款+复核两项+旁观微票+规格刷新票+转义不对称+无仓段臂

## Fixed Gaps
- list 参数族 26→7 分歧臂；开放 BUG 两条清偿；四 gate 改期；ADR-0050 软缝收口

## Next Priority（LOOP 010）
1. P2 三参尾款（mdTimestamps/includePropertiesMd5/statsTimestamps——含跨面等值性规格前置）+ 合并差分复验（含延期的单臂：空值/int32/四角）
2. 复核两项（root includeRootPath lastModified 形态 / DELETE 未知名两态探针）
3. 旁观微票（vendor CT / 空 actions 放行 / 转义不对称 MarshalIndent / 无仓段臂立账）
4. 规格刷新票（rest-api.md + api-inventory.yaml 的 ?list 行——逐字节复核前置）
5. R-6 单窗口搭车（①JVM 属性分钟级→②property→双败裁）；R-10 批复驱动
