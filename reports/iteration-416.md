# Sprint 416 迭代报告 — 文档/QA 双关账 + 用户支持插曲

**日期**: 2026-08-23 11:41
**上轮**: Sprint 415；其间 T-225、T-224 两通知轮 + 用户「无法登录」支持请求。

## 关账

| 票 | 判定 | commit |
|---|---|---|
| T-225 文档 II | conductor 直审（新两页 + sidebar + 全 curl 自建 IdP 实跑） | `5f01cbc`/`7c1a9fc` |
| T-224 验收 III | **PASS 8/8 零缺陷**（第二身份回跳腿/TTL 闭区间/PKCE 全链/dind 稳定；干净树 worktree 验收避开 T-220 WIP） | `830bc9d`/`9ef5ac6` |

## 用户支持：admin/password 无法登录

- **分诊**：干净 HEAD 复现测试 → 登录 200 正常，**代码无回归**。
- **根因**：8080 被用户另一项目容器 `arca-frontend` 占用 38h（unhealthy）——用户命中的非 BinFlow；附 `BINFLOW_ADMIN_PASSWORD` 仅首boot种入的说明。
- **处置**：应用户要求起本地实例——干净 worktree 构建（拷贝主树 console dist 静态资产解决 gitignore 占位问题）→ **http://127.0.0.1:18080/binflow/ui/**（admin/password，登录 200 实证，M7 新控制台）。数据 `/tmp/bf-user-data`，PID 36174 常驻。

## 阶段 0（本轮触发时）

在途 ×2：**T-220**（债务包，transcript 11:38）/ **T-226**（VM 等价回归，11:40）。用户实例健康（ping OK）。HEAD=`9ef5ac6`。M7：**17/21 done**。

## 尾声清单（收官轮待办）

T-220 → T-222（硬门槛）→ M7 主体 DoD 盘点；PM v1.2 回写 5 项；T-227/T-228 等扩盘；UI 打磨尾债 4 条。

## 下轮计划

T-220 完成 → 全仓 lint 0 + race 绿核验 → T-222 派发（worktree 干净树口径）。
