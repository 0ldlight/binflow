# Sprint 273 迭代报告 — M5 GA CLOSE

**日期**: 2026-08-21
**上轮**: sprint 272（T-147 在途基准测试）
**本轮焦点**: 收 T-147 ✅ → M5 DoD 终判 → tag m5-done + v1.0.0

## 阶段 0 — 复位

- BOARD.md todo/doing 均空，done=21/21（T-127~T-147 全部完成）
- T-147 基准测试 agent 报告就绪

## 阶段 2 — 收尾

### T-147 [P0] QA：性能基准 + GA 总矩阵 + 发布清单 — ✅ 完成

- **结论**: PASS（94/98 通过，3 DEFERRED + 1 BLOCKED，零阻塞缺陷）

**AC1（G27/G28）性能测试**:
- 1000 并发 REST GET 零 5xx（421.9 req/s 聚合）
- 冷启动 median 98ms（<2s），物理足迹 17.7M（<100MB）
- 4GB 流式 RSS 增量 ~6MB（<256MB）
- 7 次同内容上传 = 1 物理 blob（去重断言）

**AC2（G29/G29b）基准报告**:
- REST GET 69.7~421.9 req/s，搜索 P95 23ms
- export 1459.8 MiB/s（4.3GB，2.806s），GC 27-30ms
- vs M4 无 >20% 回归（冷启动 -21% 改善）

**AC3（G35）GA 总矩阵**:
- 94/98 PASS，3 DEFERRED（Q3 条件腿），1 BLOCKED（nginx SSL 证书）
- 6 平台 goreleaser 产物全部 CGO=0 <6MB，checksums 6/6 OK
- M5 DoD 7/7 条 PASS

**报告**: reports/agents/T-147-qa.md

## 阶段 3 — 派发

**todo 区为空，M5 全部 21 票完成，无新可派。**

## M5 DoD 终判

| # | DoD 条目 | 状态 | 证据 |
|---|---|---|---|
| 1 | 全部 P0 AC 经 QA 验证全绿 | **PASS** | 94/94 P0 PASS（T-144/T-743/T-746/T-747） |
| 2 | 五成员分级矩阵全过 | **PASS** | binary/docker/compose/k8s/helm/systemd/offline 全形态 |
| 3 | 条件腿处置 | **PASS** | G05 Windows + G15b systemd 按 Q3 降级口径记录 |
| 4 | 文档中心五类齐备 | **PASS** | T-146，Docusaurus 7 形态指南+API 参考 |
| 5 | 安全审计 + 性能基准归档，零 Critical/High | **PASS** | T-143 APPROVE + T-147 基准报告 |
| 6 | M4 8 项债务收口 | **PASS** | T-144: FR-44~FR-47 |
| 7 | 发布清单 ready | **PASS** | checksums 验证通过，镜像/Chart/二进制/离线包/文档就绪 |

**M5 DoD 结论：全部 7 条通过。**

## 看板快照

| 区域 | 票据 |
|---|---|
| todo | （空 — M5 全部完成） |
| doing | （空） |
| qa | （空） |
| done | T-127~T-147（共 21 票，M5 全部完成） |
| blocked | （空） |

## 进度

| 里程碑 | 状态 | 完成时间 |
|---|---|---|
| M1（内核基座） | ✅ m1-done | 2026-08-18 |
| M2（Docker 支持） | ✅ m2-done | 2026-08-19 |
| M3（Maven/npm/PyPI） | ✅ m3-done | 2026-08-20 |
| M4（Web 控制台） | ✅ m4-done | 2026-08-20 |
| **M5（GA 发布）** | **✅ m5-done** | **2026-08-21** |

## M5 21 票完成清单

| 批次 | 票据 | 状态 | 备注 |
|---|---|---|---|
| 1 | T-127 goreleaser+T-128 FR-44 BE+T-129 docs-site+T-130 K1/K2 终裁 | done | 双 review T-128 |
| 2 | T-131 FE 删双兜底+T-132 镜像双变体+T-133 token 审计 | done | 双 review T-132 |
| 3 | T-134 docker 视图+T-135 compose+T-136 Helm+T-137 K8s | done | |
| 4 | T-138 systemd+T-139 离线包+T-140 URI 修复+T-142 docs 内容 | done | |
| 5 | T-141 部署烟测+T-143 安全审计+T-144 QA 全链回归 | done | 双 review T-143 |
| 6 | T-145 QA deploy matrix+T-146 QA docs center | done | |
| 7 | T-147 GA closeout | done | **M5 最后一票** |

## 风险与阻塞

- **无阻塞** — M5 全部完成，所有 P0 AC 验证通过
- 3 条 Q3 条件腿（Windows/systemd 真机/停机优雅）已按 PRD §9-③ 降级记录
- 2 个 P2 缺陷（nginx SSL 证书/secret.yaml 占位符）不阻塞 GA

## 发布动作（用户确认后执行）

1. `git tag m5-done HEAD`
2. `git tag v1.0.0 HEAD`
3. `git push origin main --tags`
4. `goreleaser release --clean`（六平台二进制发布）
5. Docker 镜像推送 + Helm Chart 发布
6. 离线包发布