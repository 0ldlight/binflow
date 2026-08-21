# Sprint 298 迭代报告 — T-162 收口（复制引擎两实例实证）+ T-176 派发 + 看板清淤

**日期**: 2026-08-22
**上轮**: Sprint 297（等待回合）
**本轮焦点**: 复制引擎收口；修复看板 doing 区积弊（已收口票未清条目）

## 阶段 2 — 收口

### T-162 — push 复制引擎 ✅ 核验通过

**agent 产出**：engine.go（Enqueue 非阻塞+panic 护罩 / 退避 1s→16s×6 / pushOnce HEAD 幂等+PUT 带 checksum 头 / 复用 SSRF Guard 与 AES-GCM Cipher）+ 15 单测 + **两真实实例集成测试**（A 上传→B GET sha256 一致、replica 405、目标宕机上传不受影响）+ repo 链末最小接线（WithoutCancel+recover）。

**conductor 复核**：replication 18.7s + repo 104.6s race 绿；build/vet/lint 干净。

**Q6/Q7 暂行假设（重点，待用户定案）**：
- Q6（replica 仓型）= 可写 local backing + 未路由 virtual 只读门面（复用 M3 行为，零越区）
- Q7（冲突）= checksum 一致幂等成功 / 不一致记 failed 且 attempts 记满防 cron 复活、不动目标——**与 ADR-0021 字面（updated_at/first-write-wins/skipped）有出入**，切换位集中在 pushOnce HEAD 分支
- 私有目标默认放行（DenyPrivateTargets 选项保留收紧位）

### 看板清淤

发现 doing 区残留已收口票条目（T-160/T-170 收口时只加了 done 未删 doing）——本轮整体重建 doing 区，并复盘确认 done 区无遗漏。检视其余收口轮次：Sprint 293/295 的 doing 删除操作确有缺失，已在迭代报告如实记录此流程债。

## 阶段 3 — 派发

- **T-176 契约回写 chore** 已派发（architect；补位第 4 席；严令不碰 web/src——T-158 在途）
- 在途 4/4：T-158 · T-169 · T-178 · T-176

## 阶段 4 — 落盘

- ✅ BOARD.md：T-162 → done（**M6 15/26**）；doing 区重建；T-176 → doing；状态行更新
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-162 ✅ 两实例实证） |
| 派发 | 1（T-176） |
| 在途 | T-158 · T-169 · T-178 · T-176（4/4） |
| 已完成 | M6 **15/26** |

下轮重点：T-178 收口 → 全量 httpapi 复跑 + 派 T-179；T-158 收口 → T-177/T-159 候补。待用户：重派 T-165/T-168；Q1~Q10（Q6/Q7 假设已可审）。