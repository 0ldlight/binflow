# Sprint 342 迭代报告 — T-187 收口（T-174 缺陷全清偿）+ T-192 派发

**日期**: 2026-08-22
**上轮**: Sprint 341（等待回合）
**本轮焦点**: 审计词汇票落地；认证风暴修复上场

## 阶段 2 — 收口

### T-187 — 审计词汇与可诊断性 ✅

- `auth.failed` 全局单词汇；method/reason 分类落 Detail JSON（无 schema 迁移）
- **401 行为零变化**（错误链保 ErrInvalidCredentials 可达）；O-2 闭环（tls_handshake/provider_error/bad_credentials 可分辨 + 基础设施 WARN）
- conductor 复核：scoped auth/httpapi/audit race 绿
- 遗留：PM 两项裁决（成功词表/method 枚举偏差）待定

## 阶段 3 — 派发

- **T-192 [P1] Argon2 认证并发闸**（internal/auth 让区即派）：信号量闸 + ctx 取消 + 风暴回归测试 + T-147 零回归
- 在途 3/4：T-175（集成 QA）· T-173（S3 QA）· T-192

## 阶段 4 — 落盘

- ✅ BOARD.md：T-187 → done（done 区 **46 票**）；T-192 → doing
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-187 ✅） |
| 派发 | 1（T-192） |
| 在途 | T-175 · T-173 · T-192（3/4） |
| done 区 | **46 票** |

**T-174 QA 缺陷全清偿**（D1~D8 全闭环：修复或裁决）。剩余：在途 3 + T-193（断言漂移收编）+ PM 两项裁决。

下轮重点：QA 双收口 → **batch 6 大提交** → M6 DoD 盘点。