# M4 DoD 核查报告（2026-08-21）

依据 `docs/prd/milestone-4.md` §9 六条逐项核查：

| # | DoD 条目 | 判定 | 证据 |
|---|---|---|---|
| 1 | §4 全部 P0/P1 AC 经 qa 验证全绿（P2 延后 BOARD 记录） | ✅ | QA 三部曲：T-103 后端矩阵 **217/217**（零 P0/P1 缺陷）· T-104 浏览器矩阵 **Chromium 47/47** + 三引擎修复后 9/9（T-120）· T-105 回归+性能 **279/279**；开发期 review 全闭环（双 reviewer：T-96/T-97；单 review 一轮修复：T-95/T-98/T-99/T-100/T-111 等）。P2 延后在 BOARD：M55b/M59（基线即 P2）、五协议特化视图（T-100 AC 标注 P1 之内包 P2 子项，T-104 核对不阻）、D-1/D-2（T-103 P2）、D-104-2（token 审计 P2） |
| 2 | §8 剧本全绿 + §5.3 矩阵 Chromium+curl+CLI 三 P0 成员全过 | ✅ | T-104 Chromium CFT151 全链 47/47（W09→W35）· T-103 curl 全序列（W01~W32 后端面）· T-105 CLI 真客户端矩阵（mvn/npm/pip/twine/docker + podman/crane/oras/skopeo 扩展）；非预期 5xx=0、panic=0 |
| 3 | §5.5 K1~K3 逆向回写定案；§7 六项开放问题定案回写 | ✅ | K1~K3：T-109 回写注记（PRD v1.1）；Q1 session（ADR-0014+T-108 勘误）、Q2 配额（T-95 落地+T-97 权限面）、Q3 搜索（T-92+T-113 形态定案）、Q4 备份窗口（ADR-0015+T-112 勘误）、Q5 docker remote 推迟（T-85 转用户知悉）、Q6 GC 形态（ADR-0015 勘误①+T-94）；PRD 演进 v1.0→v1.3（T-109/T-113/T-117）+ M1~M3 勘误（T-121） |
| 4 | tech-writer 四篇文档（含 NFR-S22 保管告警） | ✅ | T-107：console.md / admin/groups-permissions.md / admin/governance.md / admin/backup-restore.md（NFR-S22 0700 保管告警在篇）+ FAQ；全部命令同源抽验通过 |
| 5 | release-engineer 烟测报告（compose 控制台链）归档 | ✅ | T-106-qa.md：三形态（bare/compose/nginx 反代）全绿，compose 控制台链 login→建仓→上传→树浏览 exit 0；271 请求 0 5xx；Dockerfile console 构建阶段 + 反代片段修复 |
| 6 | 主会话完成 m4-done tag（对外发布先经用户确认） | ⏳ | **待用户确认后执行** |

追加项：M4 期间发现并修复的最后一项 P1（D-106-1 folder 占位破坏 export）已由 T-124 闭环（红绿锚+真机往返）；M5 债务清单已在 BOARD 登记（ADR-0016 实现草案、docker 树数据源裁决、windows 锁运行时验证、token 审计、docker blob GC 候选、§7.1 两行补遗等）。

**结论：DoD 1~5 全部满足；第 6 条（tag）请用户确认。**
