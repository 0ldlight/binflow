# Sprint 655 迭代报告 — T-290 关账（`6b93e7e`），T-289 修复轮在途

**日期**: 2026-08-26 09:15
**上轮**: Sprint 654

## T-290 关账链

1. 评审 **APPROVE**（0 blocker；四处自有裁定全维持；P1 三字段按 PRD 终版、missed 双拼写 canonical、按名 400 保 scenario-D、行列>JSON>legacy>默认解析点全部复核通过）
2. 修复轮清 2 minor：`resolveRemoteAlias`（nil/0 均缺席，双非零分歧仍 400，10 臂表驱动）+ `rejectM11RemoteFields` 改 Decoder 双 Decode（尾随垃圾 400，空 blob 仍走 url-required 臂）；lint 0（上轮 3 条已自清）
3. conductor 复验：`go build` ✅ / `make lint` 0 / T290 三包 ok / `make test-m10-invariant` 双腿 0 deviations
4. 提交 `6b93e7e`（19 文件点名 add 含评审档案）→ **HEAD-build stash 验证**（提交态独立编译 + repo/remote/metadata 三包绿）→ 双远端推送（CD 链接力）

**M11 台账新增**：socketTimeout/metadataRetrieval 溢出上界 + legacy secs 退役评估（评审 minor 2）。

## T-289 评审 REQUEST_CHANGES（5 blocker）→ 修复轮已派（09:15）

- B1 锁序倒置（config 失败臂持 ms.mu 取 registry.mu × evictIdle 反向）——AB-BA 全平面死锁面
- B2 411 先设 Content-Length:0 吞掉 errors[] 信封 + 该臂零测试
- B3 `partSizeMB << 20` 溢出绕 5GiB 400 门（2^43 实测击穿）
- B4 裸 status 列表无授权过滤（越权枚举在途会话）
- B5 S3 AbortMultipartUpload 从未调用——abort/sweep/错 sha 的「清理干净」主张被证伪（mc 对象列表看不见 in-progress MPU）；**conductor 裁定选「修」**，failLocked 同补，探针断言升级 ListMultipartUploads
- 五项裁定全站住；AC2 descope 已正式留痕；3 条廉价 non-blocking 顺手清（hex 大写归一/snapshot 注释/failed 态文案）

评审档案：reports/agents/T-289-review.md（评审员内联交付，conductor 归档）。

## 在途

T-289 修复轮（1 agent）。M10：**12/21 done**（T-290 ✓）。

## 下轮计划

T-289 修复轮回收 → conductor 复验（目标测试 + 探针重跑抽验 + 不变量 + HEAD-build）→ 提交 → **B6 全清** → B7 派发（T-291 首个 MUI 票 ∥ T-292 RPM/Helm 规格）。
