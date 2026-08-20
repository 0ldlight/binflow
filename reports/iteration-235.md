# 迭代报告 235 — Sprint 235（批 3 全收口 + 批 4 派发轮）

- 日期：2026-08-20 21:35
- 里程碑：M4
- conductor：主会话

## 本轮动作摘要

1. **T-96（备份/恢复 CLI）收尾**：agent 完成通知确认（task 已停）→ conductor 核验通过 → 提交 `75c6d95`（12 文件 +2950/−6）：
   - `storage.AcquireDataLock`：data 目录跨进程锁（flock/LockFileEx kernel32 直调，x/sys 不升 direct）；gc CLI 已接线（W25b 双向互斥今日可验）；**T-94 消费契约已在日志申报**
   - export 顺序硬规则（锁 → VACUUM INTO 快照 → web_sessions purge → blobs 保 mtime 拷贝 → manifest 引用集=快照 nodes ∪ docker_refs）；import 空目录仅停机 + spot/full 校验 + 损坏矩阵 8 行
   - 核验证据：build/vet ✓、storage/metadata/cmd 三包测试绿（25.3/19.7/18.2s）、lint 0；**conductor 真机抽查**：fresh4 全新 import `--verify full` 6/6 rehash（216ms）、manifest metadata.sha256 与实件一致、恢复实例 GET a.bin/big.bin sha 逐字对账（2b0ecdd6…/7d0f10bc…）、bk≡fresh4 blob 清单逐 sha 一致、无钥 serve fail-fast（W30b 顺手实证——T-96 自测以外独立复现）
   - 插曲：conductor 首次抽查 GET 用了 `/api/storage` API 面拿到 JSON 描述（sha 不匹配虚警），改内容面 `/binflow/<repo>/<path>` 后对账通过——非产品问题
2. **T-96 双 review 派发**（code-reviewer 正确性 + architect 架构，重点：备份顺序硬规则/mtime 保真/锁语义/VACUUM INTO 并发一致性 + 锁原语契约/kernel32 直调/分层/ADR-0015 对齐）
3. **批 4 四线派发**（一条消息并行）：T-94（GC 管理化，消费锁原语勿重写）+ T-97（groups 域，双 reviewer 票）+ T-98（FE 基座：登录/壳/仪表盘）+ T-111（docker 413 verbatim 渲染臂）。area 互不重叠；router.go T-94/T-97 共文件不同 case（T-93/T-95 先例）
4. **T-95 review 回来（本轮内）**：REQUEST_CHANGES，1 blocking——**B1 声明 checksum 的幂等重传在配额边界误拒 413**（三处 `replaced` 在 idempotent 时置 0 → 预检 delta=全量而计量层 delta=0；带 X-Checksum-Sha256 重传/秒传重宣告/docker 重推同 digest 全中，reviewer 一次性注入用例实测复现）。修复窄：三处条件改 `existing != nil` + governance.go 注释 + 三臂回归。已 SendMessage 唤醒 T-95 原 agent 修复（含 NB2 顺手项：quota action 常量改 alias T-93 面）。6 non-blocking 登记（NB4 §4.6 键名勘误转 architect）。
5. 在途合计 8 线：T-95 修复 + T-96 双 review + 批 4 四线。

## 看板快照（本轮结束时）

- todo: 7（T-99~T-107，AC 见 T-88.md）· doing: 4（T-94/T-97/T-98/T-111）· review: 2（T-95 单、T-96 双）· done: 109 · blocked: 0

## 阻塞与风险

- 无新阻塞。T-98 前端首票，若 Playwright 环境问题会最先暴露。
- T-96 遗留：windows 锁仅编译验证（运行时验证无环境）；docker/mvn/twine 真客户端保真链归 T-103。

## 下轮计划

1. 收批 4 四线 + 三路 review（T-95/T-96 双）→ 核验提交。
2. 批 5：{T-99（FE 仓库管理页）, T-103（QA 后端面）}——T-103 前置 T-111 完成。
