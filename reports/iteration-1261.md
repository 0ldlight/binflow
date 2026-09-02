# Sprint 1261 迭代报告 — UAT AFTER 六标记全绿（M15 线上收口完毕）；T-434 树栈在途

**日期**: 2026-09-02 22:0x
**上轮**: Sprint 1260（m15-done 收口：PR #68 + tag）

## UAT AFTER（T-429 §6 清单——deploy_uat 已完，现网 `uat.4001701`〔PR #67 合并 sha，含 T-431/全 M15 面；close-pen 文档笔未含——纯文档无面影响〕）

| 标记 | 结果 |
|---|---|
| M1 version 翻转 | ✅ `uat.2a11096` → `uat.4001701` |
| M2 AQL | ✅ `items.find` 200 命中 Clash.Verge（envelope 全形） |
| M3 老搜索三端点 | ✅ gavc/prop/pattern 全 200 |
| M4 docker×virtual（T-431） | ✅ 矩阵格开——两次 400 均为**正确校验**（成员必填 + 同型规则 T-367），带 docker 成员 **200 建成**后清理 |
| M5 复制包 B | ✅ `/v1/system/replications` 200 + test unknown 404 |
| 清理 | ✅ 仓库基线复原双仓 |

## 状态

**M15 全线闭合**（25 票 + tag + PR #68 + UAT 六标）。M16: T-434 树栈在途（27 进程）。
