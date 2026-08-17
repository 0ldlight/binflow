# 迭代报告 046 — Sprint 046

- 日期：2026-08-18 04:05（T-13 修复回报触发的收尾轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-13 修复复审通过 → done**，提交 `913e973`：
   - PutFromBlob 落地（判权先于 blob 打开、storage.Open+blobs 台账双维校验、ErrOrphanBlob 堵死孤儿实体化与永久 sha256-only 降级）；
   - BlobOpener 注入缝删除（生产面 grep 零残留）；404 文案分动词（GET/HEAD 用规格 §1.4 文案）；控制字节拒绝；originalChecksums 上下文区分；
   - repo 5 新测试 + T-12 全绿；adapter curl 黑盒全 PASS；全仓 lint 0。
2. **T-14（httpapi 核心）派发**：派单附四项实证结论（mux `..` 归一化陷阱及 EscapedPath 对策、%2e decode 责任、匿名分层规则、PutFromBlob 契约）。**adapter 层收官**——M1 剩 T-14→T-15→T-16→T-17→T-18→T-19 六票冲刺链。
3. PutFromBlob 契约的架构 §3.3 一句话回写挂账（并入下次 architect 活动或 T-15 派单顺带）。

## 看板快照（本轮结束时）

- todo: 5（T-15~T-19）
- doing: T-14
- review / qa / blocked:（空）
- done: 22/27

## 证据与测试结果

- T-13 修复复现输出见上方（repo 13.7s + adapter 1.7s/11.0s race 绿、ErrOrphanBlob 落点、lint 0）。

## 阻塞与风险

- T-15 与 T-14 同包串行——T-14 是最后一个大票，之后 T-15~T-17 均中小票。
- 额度：窗口（02:26 起）已近 2h，T-14 大票若触顶由 loop 下窗口接力（恢复模式已验证两次）。

## 下轮计划

1. 收 T-14 → 核验（middleware 顺序/匿名矩阵/E-26/日志无认证头）→ review 单 reviewer。
2. T-14 done → 派 T-15（兼容 REST，附 v1.3 口径 + revoke 哨兵已导出 + 错误体三分层）。
3. T-15/T-16 间隙让 architect 顺手补 §3.3 PutFromBlob 一句话。
