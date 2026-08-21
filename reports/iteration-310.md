# Sprint 310 迭代报告 — T-180 收口（装配全闭环）+ .gitignore 抢修 + batch 3 + T-163/T-184 派发

**日期**: 2026-08-22
**上轮**: Sprint 309（等待回合）
**本轮焦点**: 第三处装配缺口收口；gitignore 吞文件事故修复；batch 3 提交

## 阶段 2 — 收口

### T-180 — 复制面桥接收编 ✅ 核验通过

- httpapi 四端点（T-159 契约**零差异**）+ MigrationStarter 签名收编 + SecureFromEndpoint 单点化 + cmd 引擎全装配
- 真二进制 smoke 全链（含 SIGTERM→drained）
- conductor 复核：replication 10.7s + scoped httpapi 5.7s + cmd skip 25.1s race 绿

### ⚠️ 事故与抢修：.gitignore 吞文件

T-180 遗留②实锤：`.gitignore:51` 裸名 `binflow-server` 连带忽略 `cmd/binflow-server/` 未跟踪文件——**T-178/T-179/T-180 的三个测试文件从未入库**（batch 2 提交时静默丢失，当时「binflow-server is ignored」报错即此信号，被当作 pathspec 怪癖放过了——流程教训记录）。
处置：裸名改 `/binflow-server` 锚定根路径 + 注释说明；三文件重见天日随 batch 3 入库。**复盘**：batch 2 的提交核对应包含「新增文件清单 vs 票据产出清单」比对，本轮起执行。

## 阶段 3 — 派发

- **T-163 Prometheus /metrics**（httpapi 释放后）——internal/metrics 新包 + 根级端点 + require_auth 可配
- **T-184 replication CRUD docs 回写**（architect，T-180 遗留④）
- 在途 2/4：T-163 · T-184

## 阶段 4 — 落盘

- ✅ **M6 batch 3 提交 `1d27288`**（replication 桥接 + charts oidc + gitignore 修复与三测试文件找回）
- ✅ BOARD.md：T-180 → done（done 区 **29 票**）；T-184 建票；T-163/T-184 → doing；状态行更新
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-180 ✅ 装配三缺口全闭环） |
| 提交 | M6 batch 3（1d27288） |
| 派发 | 2（T-163 · T-184） |
| 在途 | T-163 · T-184（2/4） |
| done 区 | **29 票** |

**装配三层战完成**：库层 → 端点层 → 服务端组装全通；真二进制可起 S3+OIDC+LDAP+复制全栈。

下轮重点：T-163/T-184 收口 → 剩余 todo 仅 T-165~T-175 主链（**全部卡在 T-165/T-168 重派决策**）+ QA 批次。待用户：重派两票；Q1~Q10。