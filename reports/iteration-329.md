# Sprint 329 迭代报告 — T-185 收口（H28 闭环）+ T-167 派发

**日期**: 2026-08-22
**上轮**: Sprint 328（等待回合）
**本轮焦点**: QA 缺陷包 A 落地——H28 FAIL 修复闭环

## 阶段 2 — 收口

### T-185 — 认证缺陷修复包 A ✅

- idp_sync.go（组原子替换/稳态零写/admin 每认证刷新）+ 四路径接入 + session 臂补组 + users source/whoami groups
- **H28 闭环实证**：真 RS256 JWT mock IdP → 组落库 → session 组授权仓 **200** → 移除组重登 **403** + groups=[]；admin 降权闭环
- conductor 复核：scoped httpapi 9.7s + auth 2.1s 绿（agent 自跑全量 auth 30s / httpapi 127s）
- 遗留：replace 语义对混合来源成员影响待上游确认；LDAP session 腿建议 QA 双 IdP 复验

## 阶段 3 — 派发

- **T-167 bf-migrate 迁移工具**（T-165/T-166 齐备后首次可派；Q9 按 mock 双端+条件腿）
- 在途 3/4：T-172（QA）· T-167 · T-189

## 阶段 4 — 落盘

- ✅ BOARD.md：T-185 → done（done 区 **38 票**）；T-167 → doing
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-185 ✅ H28 闭环） |
| 派发 | 1（T-167） |
| 在途 | T-172 · T-167 · T-189（3/4） |
| done 区 | **38 票** |

T-174 的 P1 缺陷（D1/D2/D4）已全修；剩 D5/D6（T-186 待派）与 P2 词汇（T-187 待派）。

下轮重点：三票收口 → 派 T-186/T-187/T-171。