# 迭代报告 234 — Sprint 234

- 日期：2026-08-20 21:40（T-95 完成触发的收尾轮）
- 里程碑：M4
- conductor：主会话

## 本轮动作摘要

1. **T-95（治理字段+配额 enforcement+usage）收尾**：conductor 核验通过 → 选择性提交 `13bc7f3`（21 文件 +2456/−31，避开 T-96 在制面 cmd/storage/snapshot）：
   - includes/excludes 双值（excludes 优先、默认 `**/*` 短路——未配置仓与 M1~M3 逐字节一致）；上传 409 / 下载 404；
   - quotaBytes enforcement：Put 四族挂门（virtual 写按目标 local 仓）、413 StatusError、被拒零残留；PutManifest 双 node 例外（注释载明）；
   - UsageStore 同事务 node+counter（delta 标量子查询避开读→写升级死锁）+ 005 回填迁移；GET /api/v1/storage/usage/{repo}；
   - harness 接真 audit logger（对齐 cmd 装配）；t93_audit_test.go 补齐进库。
   - 核验证据：build/vet ✓、repo+metadata+httpapi 三包测试绿（41.7/11.2/57.7s）、lint 0 issues、真机 curl 矩阵（W12a/W26/W26b/W27）见 T-95.md。
2. **T-95 review 派发**（单 reviewer，正确性+并发视角，重点：UsageStore 事务边界/匹配器同构漂移/挂点完备性/413 原子性）。
3. **看板手术**：T-93 补 done 行（sprint 232 落盘遗漏）；M3 残留行清理（todo/doing/review 区 9 行——done 区均有完整记录）；**新票 T-111**（docker adapter StatusError-verbatim 渲染臂，T-95 遗留①，W26c 前置）。
4. 批 4 计划更新：{T-94, T-97, T-98, **T-111**} 四线——T-94 派单附注「消费 storage.AcquireDataLock（T-96 已落），勿重写」。

## 看板快照（本轮结束时）

- todo: 8（T-94/T-97/T-98/T-111 + T-99~T-107）· doing: T-96 · review: T-95 · done: 109 · blocked: 0

## 阻塞与风险

- T-96 在途（cmd 命令级测试段），其锁原语（datalock.go 系）已在盘未提交——批 4 的 T-94 依赖其提交后派发。
- docker /v2 面配额拒绝渲染 500 UNKNOWN（T-95 遗留①）→ 已立 T-111，须在 T-103 QA（W26c 断言）前完成。

## 下轮计划

1. 收 T-96 → 核验 + 双 review（正确性+架构）→ 提交。
2. 派批 4 四线：T-94（GC，消费锁原语）+ T-97（groups，双 reviewer）+ T-98（FE 基座）+ T-111（docker 413 渲染臂）。
3. 收 T-95 review → 视结果 done 或退回修复。
