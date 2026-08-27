# Sprint 787 迭代报告 — T-322/T-324 双派（B11 尾 + B12 半）；CircleCI API 排查进展

**日期**: 2026-08-27 23:46
**上轮**: Sprint 786（23:45 T-321 收口）

## 双票派发（23:46）

- **T-322 rpm 签名腿**（dev-registry-adapter）：复用 T-321 模式（repomd.xml.asc/.key 形态；stack.signer 缝已备——main.go 一行照 T-321 先例直改）；真实 rockylinux dnf gpg 校验链验收。
- **T-324 unused-cleanup 引擎**（dev-go-storage）：cron+审计+零孤儿；范围口径先查 BOARD/PRD/规格，歧义登记。main.go 若需动则移交 conductor（T-322 也有一行——防撞车）。

## CircleCI API 排查

token 有效（404≠401 鉴别）但 `gh/lzwzzy/binflow` 及变体均不在作用域——已请用户从 Project Settings → Overview 原样复制 slug，或直接粘失败日志尾部。

## 状态

M11：21/32。在途 ×2。HEAD[develop]=`9e2d063`。
