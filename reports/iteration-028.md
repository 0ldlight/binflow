# 迭代报告 028 — Sprint 028

- 日期：2026-08-18 01:05（T-9 二轮修复回报触发的收尾轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-9 二轮修复复验通过 → done**（基础三件套全员 done 🎉）：
   - singleflight panic 死锁修复：teardown 移入 defer（map delete → unlock → wg.Done 顺序保证）；panic 传播但状态干净；两个 panic 注入测试 PASS（follow-up 不挂死 + 已阻塞 waiter 释放）。
   - 场景固化：rename EACCES 五点断言、GC grace 保护（8 并发 vs hammering GC → 8/8 存活）+ 小 grace 孤儿确被回收。
   - 跨引擎单实例约束入 doc.go。
   - conductor 复验：4 针对性测试全 PASS、lint 0。提交 `3a19549`。
2. **T-12（repo.Service）派发**：T-9 done 解锁关键路径下一环。派单附 T-25 回写后的契约（事务边界 a 案、sentinel 全集、blob-first）与 repo-semantics 语义（幂等重传免覆盖）。auth 未就绪部分授权本地最小接口过渡。
3. 在途：T-11（auth，编码 ~1h——auth 面较大属正常）、T-12。

## 看板快照（本轮结束时）

- todo: 7（T-13~T-20 减 T-25）
- doing: T-11、T-12
- review / qa / blocked:（空）
- done: T-1~T-10, T-21~T-25（done 16）

## 证据与测试结果

- T-9 复验输出见上方（4 测试 PASS、defer 落地、lint 0）。
- 全仓状态：基础层（config/storage/metadata）race 全绿 + lint 0 + 零 CGO + 架构文档对齐。

## 阻塞与风险

- T-11 运行时间偏长（auth 面含认证/授权/token/审计四模块），若再 30 分钟无产出下轮探活。
- T-12 依赖 auth 类型面——已授权最小接口过渡，T-11 合流时需对齐（记入两票日志）。

## 下轮计划

1. 收 T-11 → review（单 reviewer）→ done；收 T-12 → review（正确性 reviewer 重点 Put 事务边界）。
2. T-12 done 后派 T-13（adapter SPI + Generic——M1 首个对外可见行为票，真实客户端测试）。
3. 关注 CI 首跑状态（多轮 push 已触发多次，无红灯通知即绿）。
