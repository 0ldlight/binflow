# Sprint 756 迭代报告 — T-308 收验；T-300 提前补位；三票交织裁定与 PR 化口径

**日期**: 2026-08-27 10:57
**上轮**: Sprint 755（10:23）

## T-308 收验（已验待合）

agent 10:44 回报全量交付：conan v2 17 端点 + v1 全量数据面（CN-1 终裁）+ 能力头（TL-2 逐字）+ reindex 业务体 + 门控三缝。**真实客户端双版本活体 E2E**：conan 2.31.2（upload 两修订→清缓存→install 全远端下载链）+ conan 1.66.0（user/upload --all/search/install/remove）。`-race` 双包 ok；`make test-m10-invariant` / `EXPECT=1` 均 0 deviations。conductor 复验 build/vet/addons+conan+cmd 全绿。遗留三件：dispatchAPI 两 case 波次合流时 conductor 接线（片段在 T-308.md §5-D9）；规格修订建议 2 条交 reverse-engineer；`forceConanAuthentication` 字段未落（默认 false 已备）。

## 三票全交织裁定（分支模型第 6 条）

T-311 续跑期间主动改写 main.go（rpm import）与 router.go（yum case + 第 7 个 repoManage gate）——worktree 验证抓到实时竞态（先前 grep main.go rpm=0，10 分钟后 import 出现）。接线文件 T-308/T-309/T-311 三票共写且无法按票序独立编译。裁定：**T-311 落地全绿后 B4+B5 波次单 PR 一次合入**（gh pr create → gh pr merge --merge），三票逐项归因入 PR 描述。快照保险 /tmp/snap-b45-1052/。附带发现：`go test ./internal/httpapi/` 暂红是 T-311 在途所致（t215 gate 计数 7≠6，待其落测试侧）。

## PR 化合并口径（用户 10:55 指令）

「你自己在合适的时机创建github pr」→ BOARD 分支模型第 5 条：feature→develop 与 develop→main 均经 GitHub PR（conductor gh 自建自合，--merge 保合并提交）；develop→main release PR 在 CircleCI SSH fingerprint 占位符填妥前**只建不合**。

## T-300 提前派发（宽度补位）

宽度空 1（T-308 已完，T-311 在途）。Go 侧候选票（T-310 deb 撞 repo 未提交面、T-312/T-313 撞 adapter 未提交面）全部与 B4+B5 交织冲突；**T-300 MUI 批二**（web/ 域，dep T-299/T-307 均已 done）零重叠 → 10:57 派 dev-frontend（后台）。批一边界项（session 菜单/侧栏/badge）派单裁定=迁。T-316 仍卡 NuGet 对齐捆绑未决用户裁决。

## 状态

M11：8/32 + T-308/T-309 已验待合（波次 PR 锚）。在途 ×2（T-311 rpm / T-300 MUI）。HEAD[develop]=`bf4cfa0`。
