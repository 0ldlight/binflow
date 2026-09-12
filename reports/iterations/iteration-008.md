# Iteration Report — LOOP 008（update-merge 落地 / D3+virtual 双小票 / 票 D / R-10 呈批）

- **Iteration**: 008
- **Date**: 2026-09-12
- **Gap Before**: update-merge BUG（ADR 候选在案）；D3 终裁待实现；virtual 注记 BUG；票 D 参数族未核对；裁定项散置
- **Goal**: ADR-0050 流程+实现 / 双小票 / 参数矩阵 / R-10 包整编

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L008-1a/b ADR+实现 | architect + dev-go-core | ✅ ADR-0050 入册（+40 纯追加）+ 任务书施工（含双审返工一轮） |
| L008-2 D3+virtual | dev-registry-adapter | ✅ 四臂 0-vs-0 对齐闭环 + 注记销 |
| L008-3 票 D 前段 | differential-qa-engineer | ✅ 32 臂矩阵（26 分歧）+ 缺参清单 + users 404 实锤 |
| L008-4a R-10 包 | product-manager | ✅ v2.0：14 编号/16 席位呈批 |
| L008-4b 台账卫生批 | compatibility-engineer | ✅ 五件（mxm/stray 家族/勘误段/注释/auth-model） |

## Implementation（L008-1b，含返工）
- **update-merge**：parseRemoteConfig 基线模式（三列矩阵：省略=保留/null=清/空值分族/显式 0 存 0）+ PUT=create-only（400 逐字）+ credential 密文行保留链（RawMessage null 存活）
- **返工 B1（P0，双审独立命中）**：credential 保留臂三分流——瞬态 GetConfig 错误拒绝更新而非静默清密文（注入测试腿）
- **返工 B2**：socketTimeout 别名族先四臂取证（参照 millis:0 存 0、显式 0 胜 secs）后重写 resolveRemoteAlias（单侧显式 0=给定值；根因 b 位吞 0 修复）
- D3：blob 面检索窗豁免（standing+digest 已验→本地 HIT 零上游零行重写）；virtual：walk 终态 plain body

## Differential
- update-merge：13 硬断言臂全 SAME + B2 补 D1-D3 三臂 SAME（D4 席位差 by-design）
- D3 四臂 0-vs-0（含 10.4×TTL 与真实 CLI）；virtual 138B plain 对齐
- 票 D：26/32 分歧入账（递归三连坏含死守卫、5 形态级差作用全部 200 臂）

## Review
- A/B 双 REQUEST_CHANGES（**独立命中同一 P0**：credential 吞错臂——sealed 凭据静默丢失风险）→ 返工清偿（预授最小改法+注入测试）
- 双审战绩 9/9（本轮抓出的是静默数据丢失类——最危险的一类）
- B non-blocking：ADR-0050 对象列 A14 证伪→Errata 触发③成立（architect 勘误候办）；audit descriptionSet 微漂移；POST 尾随垃圾 400 留痕

## Interruptions
- 零（本轮 4+ agent 全程无击落）

## Compatibility Score
- matrix：✅70 维持（D02 行注记闭环）；台账 25 条目（resolved +3：update-merge/D3/virtual 注记；新 BUG×2：users 404/stray 家族；UNKNOWN 到期 4 条待 LOOP 009 首件）
- ADR：+2（0050）+ 勘误（0012-四）；契约：docker-remote blob 豁免语义入册

## Fixed Gaps
- update-merge（族级语义终案落地）；D3 三轮悬置终闭环；virtual 注记；票 D 证据面全开

## Next Priority（LOOP 009）
1. **四条 UNKNOWN 到期 gate 首件**（R-10 待批关联三条顺延+invalid-value 族——authority 阻塞合法顺延改期）
2. **list 参数补参大票**（P0 递归语义+uri 形态 → P1 listFolders/includeRootPath → P2 三元数据参 → P3 校验/根列/文案——L008-list-params-diff §5 分组就绪）
3. stray+users 体裁合并微票（permissions 边缘校验①②+users 404 envelope——文案泛化警告在账）
4. ADR-0050 对象列 Errata（architect 小件）
5. R-6 候选窗口（三候选/Mino TLS 路径）
6. 待批驱动（R-10 包 16 席位）
