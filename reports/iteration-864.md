# Sprint 864 迭代报告 — T-336 收口（RSS 138.7→10.0MB，PR #25）；M12 5/25；宽度全空

**日期**: 2026-08-28 20:45
**上轮**: Sprint 863（20:25）

## T-336 → done（PR #25，develop=`48ba431`）

- **归因反转**：embed 无罪——启动期 argon2id 双 64MiB 派生（gctrace 落锤）；`FreeOSMemory` 一次调用归还（darwin/Linux 双机制实证）。
- conductor 复验 `make footprint --expect`：**9.9MB GREEN**（冷启动 1585ms<2s）。
- 登记：启动瞬态 peak 仍 ~139MB（非门控指标）；登录期单峰 <100MB 靠 scavenger；「任意时刻 ≤100MB」如需另立项。

## M12 现况

5/25（B0 全清 + T-336/337/338）。在途 ×0。下一批候选：T-341（v3 search，dep T-337 ✓）/ T-335（repo-operations mini 规格主线头）——下轮双派。

HEAD[develop]=`48ba431` 已推双远端。
