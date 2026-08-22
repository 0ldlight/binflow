# Sprint 339 迭代报告 — T-171 收口（六篇指南）+ T-194 派发

**日期**: 2026-08-22
**上轮**: Sprint 338（T-172 收口 + T-173 派发）
**本轮焦点**: 文档链落地；charts 新键小票补位

## 阶段 2 — 收口

### T-171 — 文档 5 类 ✅

- 六篇指南（OIDC/LDAP/S3/bf-cli/迁移/Prometheus）+ sidebar/导航/FAQ 接线
- **路径裁定**：AC 的 docs-site/docs/ 实为 docs/user/（Docusaurus path 配置——「按现状」条款适用）
- conductor 复核：make docs SUCCESS + dist 六页在场；agent 自证 120 token 键名对代码全命中 + 真机六路由 200 + T-174 排障知识收录
- 遗留：install/* 八页 sidebar 未注册（T-141 seam）；迁移指南的 client 缺口注记待随 T-191 提交回刷

## 阶段 3 — 派发

- **T-194 charts 新键渲染**（T-186 的 skip_tls_verify/group_base_dn + oidc 全键核对 + 指南示例同步）
- 在途 4/4：T-187（审计词汇）· T-175（集成 QA）· T-173（S3 QA）· T-194

## 阶段 4 — 落盘

- ✅ BOARD.md：T-171 → done（done 区 **44 票**）；T-194 建票并派发
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-171 ✅） |
| 派发 | 1（T-194） |
| 在途 | T-187 · T-175 · T-173 · T-194（4/4） |
| done 区 | **44 票** |

剩余：在途 4 + T-192（等 T-187）+ T-193 + install sidebar 小缝。**M6 收官冲刺**。

下轮重点：收口潮 → T-192 → batch 6 大提交 → M6 DoD 盘点。