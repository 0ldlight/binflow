# Sprint 299 迭代报告 — 双收口（T-169/T-158）+ T-159 派发

**日期**: 2026-08-22
**上轮**: Sprint 298（T-162 收口 + T-176 派发）
**本轮焦点**: systemd 真机烟测票与 SSO 登录 UI 票收口；web 区释放后派复制面板

## 阶段 2 — 收口

### T-169 — Windows 锁 + systemd 裸机 ✅ 核验通过

- 关键发现：AC① 实质已被 **T-96** 满足（datalock_windows.go LockFileEx 已存在）——agent 补测试与 GOOS 感知修复而非重复造轮，不动 go.mod（避让 T-168 在途）
- G15b 真机烟测：Ubuntu22.04+systemd PID1 容器全链路（install→active→readyz→roundtrip sha256→restart→优雅停机日志→幂等重装→systemd-analyze verify→purge）；install.sh 修 3 个真机首跑复现 bug
- conductor 复核：scoped DataLock/Backup race 绿 + bash -n + 双平台×双架构交叉编译 OK + build OK
- 遗留：make test 全量绿被 T-168 遗留红阻断（非本票区，在案）

### T-158 — SSO 登录 UI ✅ 核验通过

- SSO 按钮探测方案：`fetch(oidc/login, redirect:manual)` 读 302/404（浏览器不触达 IdP）；密码表单零分支；纯 token 样式追加
- conductor 复核：build 2.65s + Playwright **5 passed**（repeat-each=2 → 10）+ storage_migration spec 无回归
- 后端提议 `GET /api/v1/auth/methods` → **已并入 T-179 AC⑤**；T-174 dep 增补 T-179（真实 IdP 验收的硬前置）

## 阶段 3 — 派发

- **T-159 复制面板 UI** 已派发（T-162✅ + T-158✅ 释放 web 区）。指引：replication/status 端点尚未桥接——以 `internal/replication/model.go` ConfigStatus 真实形状推契约 + 报告标注假设（供桥接票对齐）
- 在途 3/4：T-159 · T-178 · T-176。第四席无免冲突票（T-177 web 冲突 T-159 / T-163 httpapi 冲突 T-178 / T-179 dep T-178）——**留空是正确决策**

## 阶段 4 — 落盘

- ✅ BOARD.md：T-169/T-158 → done（**M6 17/26**）；T-159 → doing；T-179 增 AC⑤；T-174 dep 增 T-179；状态行更新
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 2（T-169 ✅ 真机 systemd 全链路 · T-158 ✅） |
| 派发 | 1（T-159） |
| 在途 | T-159 · T-178 · T-176（3/4，第 4 席按冲突规则留空） |
| 已完成 | M6 **17/26** |

下轮重点：T-178 收口（S3 数据面激活）→ 派 T-179（认证装配）+ 全量复跑；T-176 收口。待用户：重派 T-165/T-168；Q1~Q10。