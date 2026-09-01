# Sprint 1117 迭代报告 — T-400 首红归因：T-406b 回归修复（`f4ee09f`）

**日期**: 2026-09-01 08:2x
**上轮**: Sprint 1116（等待轮②）

## T-400 终验中段红 → conductor 归因修复（worktree 双基线）

T-400 在其快照（81e528d）的 go 全量抓到两红，进入 e2e 段。conductor 归因：

- **pypi `TestRemoteRepositoryProjectPage`**：`/simple/<project>/` 索引页合法以斜杠结尾，被 T-406 folder-face 的 **childless-404 短路**截胡不回源。修：childless folder 落穿 `getRemote`；缓存面（marker 行 / 有子级材料化）维持零上游接触。
- **nuget `TestV2RemoteProxyFallback`**：原测试钉住 svc.List-拒绝-remote 的 404 降级（测试注释自认 the degradation the ticket report registers）——T-406 放开 List 后回退真正枚举落袋事实答 200。**契约更新**（行为改良非回归）。

**复验**：nuget/pypi/repo 全包绿（26.2s/7.9s/50.9s）+ adapter 全树 sweep 零红 + lint 0 + console 零改动。bisect worktree 已清。T-400 已 SendMessage 通知：两红按 D-400-x 已修复记账（引用 `f4ee09f`），拉新 HEAD 复跑两包入 AC3 证据；e2e 无需重跑。

## 状态

M14 实现面 22/22 + T-406b 收口；**T-400 终验在途**（e2e 段 + 两包复跑 + AC1/2/4/5）。HEAD[develop]=`f4ee09f`（双远端）。收口队列不变：T-400 PASS → m14-done 五步 → M15 开局。
