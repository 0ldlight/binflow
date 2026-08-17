# 迭代报告 045 — Sprint 045

- 日期：2026-08-18 03:55（T-13 review 回报触发的裁决轮）
- 里程碑：M1 内核基座
- conductor：主会话

## 本轮动作摘要

1. **T-13 review 裁决：REQUEST_CHANGES**（1 blocker）：
   - B1：BlobOpener 直调 storage.Engine.Open 违反架构 §5.1 分层，且孤儿 blob（物理在、台账缺行）可被秒传实体化并永久 sha256-only 降级 → 修复方案：repo.Service 扩 PutFromBlob（校验 blobs 表有行，孤儿报错）+ generic 改调。已派原 agent（授权跨 area 动 internal/repo）。
   - **路径安全面全面通过**：reviewer 真机起服务 + curl --path-as-is + 原始 socket 打 30+ 变体全 400；早期「201」误报系 curl 自身丢弃末段（trace 证实），双重编码 201 属正确的单次解码语义。
   - M1（404 文案）+ m1/m2/m4 顺手修；m3/m5/m6/m7 记录。
2. **范围外事项澄清**：reviewer 工作区看到的 IMS 失败是 T-20 在途快照——T-20 提交后 conductor 现测 `If-Modified-Since -z both sides` PASS、adapter 全绿，无遗留。
3. 遗留四项裁决落定：②随本修复落地；①③④按 reviewer 意见归档（M3 再评估 / M1 不需要 / 无行动项）。

## 看板快照（本轮结束时）

- todo: 6（T-14~T-19）· doing: T-13（修复）· review: 空 · done: 21 · blocked: 0

## 证据与测试结果

- T-13-review.md 落盘（真机 30+ 变体取证、四类 panic 测试确认、干净 clone 复现）。
- IMS 澄清证据见上方（PASS 输出）。

## 阻塞与风险

- T-14 等 T-13 修复（同包）。T-13 修复含跨 area（repo），需保持 T-12 全部测试绿。

## 下轮计划

1. 收 T-13 修复 → 针对性复审（PutFromBlob 单测 + 孤儿 blob 拒绝 + 分层 grep 无 Engine.Open 直调）→ done → **派 T-14**（附 mux 只分流/%2e decode/匿名分层/PutFromBlob 契约）。
2. 之后 T-15→T-16→T-17→T-18→T-19 冲刺链。
