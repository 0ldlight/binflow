# Sprint 386 迭代报告 — M6 完结收官（T-209 关票 + DoD #5 执行）

**日期**: 2026-08-23
**上轮**: Sprint 385（T-208/T-209 复核验收在途）
**本轮焦点**: 收两线复核结论 → T-208/T-209 双双关票 → docs 重建落盘 → 本地打 `m5-done`/`m6-done`。**M6 正式 closure。**

## 阶段 0 — 复位

在途 2 agent（review2 + qa），HEAD=`6097538`。本轮先后收到两者完成通知。

## 阶段 2 — 收尾（本轮主体）

### review2（code-reviewer → T-209 返修复核）：REQUEST_CHANGES → 已闭环

- **原三 blocker 全部确认真实修复**（① snapshot purge 并列清 `upload_sessions` + 测试钉死；② ResumeSession 锁序反转消除——全包锁审计唯一嵌套对 `s.mu→e.mu`；③ sweep 测试重写真实证明全栈落库）。
- **新 B1**（返修 commit 自引入）：`OpenEngine` sweep 任意失败即 `os.RemoveAll(uploads/)`——活实例注入 md 后一次瞬时 SQLITE_BUSY 即摧毁全部在途上传；唯一良性场景下是 no-op，纯破坏。conductor 亲核源码（`engine.go:116-118`）确认。
- **返修轮 2**（dev-go-storage）：删该行 + 注释如实化 + 钉死测试 `TestOpenSweepFailureKeepsUploads`（假 store 注入 ListExpired 恒错；三类目录字节级幸存断言；**变异验证**：放回 RemoveAll 行即红）。顺手 N1 godoc / N5 s3 注释 / N4 sql 行尾。N2 禁改（qa 并行在跑 httpapi）、N3 记债、**N6 conductor 裁定延后 M7**、N7 无害。
- **conductor 增量复验**（qa 明示其证据锚 `e7b581e`、不含 B1 增量，故复验是合入前置）：`go build ./... && go vet ./...` OK；`-race -run 'TestOpenSweepFailureKeepsUploads|Sweep|Resume'` ok 1.7s；storage+metadata lint 0 issues。提交 `db45cb2`。

### qa（qa-engineer → T-208 + T-209）：双双 PASS

- **T-208 PASS 5/5 零缺陷**（真实栈 curl 16 腿：禁用→存量 token 立即 401 且 `enabled=0` 落库核实、缺省保持双向、非 admin 四姿势 403 零副作用、session 臂 401；全仓 `-race` 23 包 exit 0）。**T-208 done**（review APPROVE + qa PASS，代码已随 `f21fd74`）。T-190 护栏③自此端到端闭环——本会话开场发现的「用户禁用无 REST seam」缺口正式补完。
- **T-209 PASS 5/5**（锚基线 `e7b581e`）：三上轮 FAIL 确认关闭；e2e 亮点——kill -9 后会话行 + 512KiB 部分数据幸存启动 sweep、过期回填全清、真·半路断开（伪造 digest 400、416 权威 offset）无完整性洞、真栈 export 证实瞬态不进备份面；S3 回归绿。观察 O-1（干净停机清会话，非回归）/O-2（重启续传无 REST 调用方 = N6 同源）→ 延后 M7。**T-209 done**（`e7b581e` + `db45cb2`）。

## 阶段 3 — 派发

无（todo 空；收官轮不派新票）。

## 阶段 4 — 落盘（本轮 commit 清单）

| commit | 内容 |
|---|---|
| `f89b55d` | chore: T-208 closed（qa 报告 ×2 + BOARD） |
| `db45cb2` | fix: T-209 review2 B1（代码 + 钉死测试 + review2/返修日志） |
| `323a434` | docs: docs-site 重建（guides 页 + 资产哈希，bed7a66 源的生成物） |
| （本轮） | chore: BOARD 终态 + 本报告 |
| tag | `m5-done`（回补，`5d13c3c`）/ `m6-done`（收官 HEAD），均 annotated |

## DoD 终态（对照 ROADMAP 68-75 行）

| # | 条件 | 判定 |
|---|---|---|
| 1 | 全票 done | ✅ M6 主体 59 票 + 收尾 3 票（T-208/209/210）全 done，doing/qa/blocked 空 |
| 2 | QA 全绿 | ✅ 全部 qa PASS；T-173/T-175 两处 FAIL 经 ADR-0025 终裁记为已知限制 |
| 3 | 部署烟测 | ✅ compose/k8s/helm/systemd/离线包矩阵 |
| 4 | 文档 | ✅ docs/user + docs-site 同步重建（`323a434`） |
| 5 | tag + 发布 | ✅ 本地 `m5-done`（补）/`m6-done`（含 m5 缺失历史遗留的补齐）；**push 须用户授权，未执行** |

## 遗留债（M7 候选清单，全部已记 BOARD 状态行）

N6/O-2 重启续传 REST 可见性（引擎能力就位、无生产调用方）· N3 ctx 取消窄窗（fail-closed）· O-1 干净停机清会话 · N2 restart 臂注释 · internal/auth 53 条既有 lint · 008/009 sql 行尾 · Q8/Q9 真实 AWS S3 / 真实 Artifactory 条件腿（MinIO/Docker 等价性收口）。

## 阻塞与风险

- **唯一待办 = push 授权**（外发红线，等用户明示；届时 `git push origin main --tags` 或分步）。
- 下一里程碑（M7 规划或新方向）待用户指令；/loop 每 10 分钟空转确认冻结态，收到指令即转阶段 1。

## 下轮计划

无新输入 → 轻量冻结确认；有 push 授权 → 执行外发；有 M7 指令 → 阶段 1 补给（PM/architect/reverse-engineer 三路并起）。
