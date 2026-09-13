# Iteration Report — LOOP 014（npm SPECIFIED 清零 / maven 双 BUG+返工 / R-15 v2.2）

- **Iteration**: 014
- **Date**: 2026-09-13
- **Goal**: K60-5 升格 / maven 双 BUG / R-15a/b 重呈裁

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L014-1 R-15 重呈裁 | product-manager | ✅ v2.2（原 501 案证据作废；PM 建议双实现——候用户） |
| L014-2 maven 双 BUG | dev-registry-adapter | ✅ 含双审返工一轮（四 blocking 全闭——F1 活参照定案） |
| L014-4 K60-5 升格 | compatibility-engineer | ✅ 16 条目终态 15V+1I、SPECIFIED 清零 |
| L014-3 Penpot 清理 | conductor | ⏸ 候用户授权 |

## Review（双审战绩 13/13）
- A：REQUEST_CHANGES ×2（**B1 权限门穿透**——write-without-read 覆盖上构建〔安全级〕；B2 virtual 盲区——同 N 无限累积）
- B：REQUEST_CHANGES ×2（**F1 pom 跟随规则与参照反编译相悖**且零观测；F2 证据链断点）
- 返工全闭：B1 走未设门 facts 推导；B2 预取写目标；F1 活参照探针定案（**pom 永不并入当前 trip**——wire 证实 B 的反编译分析）；F2 probe-log 落盘

## Differential
- maven 矩阵终态 **23/26 一致**（余 3=三 UNKNOWN 本体：XML 形态/手 PUT 可见性/参照自丢+新根因假说）
- K60-5 探针实跑（双端建仓删净）；m21-m24 补格全落

## Interruptions
- 第七波限额（返工+Review B 击落——B 报告完整送达；返工树幸存；恰逢重置点复活）
- ref 负载振荡（agent 探针压载——未重启，收尾自减压）

## Compatibility Score
- Before: ✅73/◐18（48.6%）
- After: **✅74/◐17——Coverage 89/181 = 49.2%**（D12-R08 翻✅）
- npm 契约 16 条目（15V+1I）；台账 41 条（三 maven UNKNOWN 候登记 LOOP 015）

## Fixed Gaps
- maven unique-snapshot 改写（含权限门/virtual/pom 规则三深修）+checksum 旁车真相；npm K60-5 升格

## Next Priority（LOOP 015）
1. R-15a/b 终裁驱动（P0 partial 清零钥匙——v2.2 就绪）+ 三 maven UNKNOWN 登记
2. spec build.timestamp 裁决票 + routeTarget seam 回收票
3. internal/client 存量失败票（T-231/T-233 域——worktree 复验非回归）
4. login-missing-fields UNKNOWN（gate 本轮）+ 扩章第三票候选（npm publish 族/pypi）
5. Penpot 残留清理+4 硬阻塞 gap（候用户）
