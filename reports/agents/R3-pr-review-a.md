# R3 PR Review — Reviewer A (correctness)

评审对象：`claude/r3-fixes` 相对 `origin/develop` 的 5 提交（51c1a1c4 / 93aa3573 / 9c83678d / df236376 / f21f1c09）。

```
结论: APPROVE
形态: reviewer-a (correctness)
blocking: 0
non-blocking: 6
范围外: 2（移交 conductor，见 Next）
```

---

## 15 字段模板

**Ticket:** R3 批量 PR（T-540/T-542/T-543/T-544/T-545/T-546；Linear BIN-23/25/26/27/28）— iteration-1542 R3 修复轮
**Role:** code-reviewer (reviewer-a)
**Area:** maven adapter（UA 条件合并 + 409 门）/ repo cache 投影 / replication 测试 / difftest 工具 / compatibility 账本
**Input:** conductor 派发（5 提交清单 + correctness 评审重点）；通读 docs/reverse 引用段（经代码注释锚点核对）、L030/L031 差分报告、T-54x 实现日志
**Changes:** 逐提交 git show 全量 diff；上下游追读：maven handleGet/putFile 全链（put.go:60-130 入口、serveSidecar、listRowsChecked/ResolveMeta 调用点、httpapi/storage.go 消费点 error 映射）；_npmlib.py 全文 + 3 case 全文
**Files:** 见下「逐文件结论」
**Tests:** 见「取证命令与结果」——全部 PASS / clean
**Commands:** 见「取证命令与结果」（原文）
**Outputs:** reports/agents/R3-pr-review-a.md（本文件）
**Compatibility:** known-divergence 6 新条目与两 fix 提交互指一致（T-542/T-543 条目 review_gate 已标 in-flight 闭环路径）；L031 报告如实记录 3 FAIL（B 侧 latest 重算漂移）并移交裁定，r2≡r3 稳定判据 + round-0 执行事故如实留档——无粉饰
**Security:** 409 门在校验后才落盘不存在（见正确性分析 2）；pom 解析用 encoding/xml 无 XXE 外展；difftest 凭据硬检查通过——case 文件零字面量，全部经 ctx.sides/env 注入（`${NPM_AUTH}` 仅子进程环境展开），proxy token 内存构造不落盘
**Performance:** UA 谓词为正则+字符串后缀，热 path 可忽略；pom 门新增 ≤4MiB 内存缓冲（超限流式旁路 + warn 日志）——pom 本体 KB 级，可接受；listCacheRows 比旧委托少一次 remoteBrowseRows 上游折叠——纯减法
**Risks:** ① local 面 java-agent 剥除后 body/sidecar 摘要不一致（已登记 unprobed，见 non-blocking 1）；② remote 仓自身快照 metadata 面未设门（第三暴露面，未取证未实现——符合 no-guess 纪律，建议补探针票）；③ suppress 旋钮在 virtual 路由后读 member 配置（见 non-blocking 3）
**Blockers:** 无
**Next:** 见「范围外与建议」

---

## 逐文件结论

| 文件 | 结论 |
|---|---|
| internal/replication/engine_test.go | waitAudit 形状与 waitTask 一致（10s deadline / 10ms 轮询 / t.Helper / t.Fatalf 在测试 goroutine 合法）；sink `collected()` mutex 保护 + 拷贝返回，轮询安全；3 调用点断言内容零改动（精确计数保持）。PASS |
| internal/adapter/maven/config.go | suppressPomConsistencyChecks 以 `*bool` 三态解析，缺省 false=执行校验——与 knob 语义一致。PASS |
| internal/adapter/maven/handler.go | rowType 单次取值重构行为等价（err 时 "" 同旧两处条件皆跳过）；local 剥除臂失败回退 serveFile，svc.Get 错误映射不吞。PASS（附 non-blocking 1） |
| internal/adapter/maven/put.go | 见正确性分析 1/2。PASS |
| internal/adapter/maven/virtual_metadata.go | 谓词/剥除/派生写三件套正确；writeDerivedMetadata/sidecar 摘要对派生字节计算——virtual 面自洽。PASS |
| internal/repo/virtual.go | listCacheRows 直取 ListByPrefix（绕开 browse.go:225 的 remoteBrowseRows 折叠——正是票面目标）；错误 wrap 带上下文。PASS |
| internal/repo/service.go | 两委托臂改本地语义读；ACL 门显式落在 PARENT key（复刻旧委托臂姿态，401/403 分叉保持）；degraded 恒 "" 是零上游的必然（原委托臂可能透传 parent 降级态——该变化即票面意图）。PASS |
| internal/httpapi/cache_face_rest_zero_upstream_test.go | 3 腿计数假上游（atomic 计数），断言含「必须 >0 上游命中」的控制腿——不会假绿。PASS |
| tools/difftest/v2/cases/_npmlib.py | 确定性 fixture（tar mtime=0 + gzip mtime=0）；npmrc 无秘密（${NPM_AUTH} env 展开）；proxy try/finally shutdown+server_close 三 case 俱全；timeout 显式标记。PASS |
| tools/difftest/v2/cases/npm_*.py ×3 | 判定 a/b/expected 三方比对；case 2 的 virt2 条件断言坍缩仅在双端同拒时合并标记、混局丢弃该块但 raw 留档 virt2_put_status 交批次裁定——非放松；case 3 pack 断言条件链任何分支都不可能产出 "sha256-ok" 假绿。PASS |
| docs/compatibility/known-divergence.yaml | 6 条目 authority/evidence/review_gate 齐备；79 id 无重复（grep+uniq 机器验证）。PASS |
| reports/compatibility/L031-*.md | FAIL 结论如实、漂移移交路径明确、复跑命令在案。PASS |

---

## 正确性分析（评审重点逐项）

**1. UA 谓词误伤面（virtual_metadata.go:436-446）**
- 空/空白 UA → capable（true）：与登记的参照语义（absent UA 默认支持）一致。
- 非 maven 客户端：Gradle/curl/浏览器 product token 不命中 ivy/wharf 后缀 → capable（照发全量）——不误伤。
- 中间盒改写/剥离 UA → 判为 capable → 提供合并（更宽容侧）。参照面 anchored 的谓词族即 `[Jj]ava/.+` 全匹配 + Ivy/Wharf 产品 token，多给的宽容只在「参照本会拒绝」的窄族上反向发生不可能（改写后的 UA 不落入拒绝族即视为 capable，与 absent-UA 缺省同侧）。可接受。
- 谓词只影响 serve 面（读路径剥除），不写存储、不改 calculator 重算——无持久化污染。

**2. 409 路径的部分写入**（put.go:247-273）
校验在 putFile 顶部、快照重写与 store 链之前执行；拒绝路径 `writeError(409)` 后直接 return——零落盘。测试四臂（local/虚拟路由 × GET 404）钉死「拒后无残留」。suppress 旋钮读取时机：`cfg := ParseRepoConfig(row.Config)`（put.go:69）在**每次请求**从 repo 行现场解析——非建仓快照，改旋钮即时生效。409 经 writeError 走 errors[] envelope（response.go:50），与 A 面逐字节消息由测试锚定。
- >4MiB pom 流式旁路（不再解析）+ unparseable/坐标不全放过：均有注释说明证据边界（只钉了 mismatch 拒绝形状），不猜测扩门——纪律正确。

**3. repo 投影委托臂**（virtual.go:952-998, service.go:586-596, 1733-1755）
- 空缓存：ListByPrefix 返回空 → 文件 miss 走 ErrNodeNotFound（wrap 带 projKey 上下文）→ httpapi errors.Is 映射 404（storage.go:1092）。
- 上游不可达：**不接触上游**（listCacheRows 无 remoteBrowseRows 折叠、无 pull-through）——测试 leg 2 以计数器钉死零命中。
- ResolveMeta 委托失败传播：listCacheRows wrap `"list %s/%s: %w"`、miss wrap `"node %s/%s: %w"`——上下文齐备，errors.Is 链路完整。
- 文件夹合成 `RepoKey: projKey` 与 getCacheProjection/cacheProjectionFolder 既有姿态一致（旧委托臂泄 parent key 反而是被修的瑕疵）。

**4. waitAudit**：10s/10ms 与 waitTask 逐形状一致；断言未放松（三处仍精确 ==1/==2 计数 + Action 逐项）。

**5. difftest 凭据**：全部 `ctx.sides[...]["user"]/["password"]`（runner env 注入）与 `${NPM_AUTH}` 子进程环境展开；`/tmp/r3-difftest.env` 外置；case/证据零字面量。**硬检查 PASS。**

**6. 断言变相放松排查**：无超时放宽（difftest timeout_s 480/600 为新设 case 自有值）；无 skip；replication 断言原样；snapshot_routing_test.go 仅将 trip 标记从 version 行挪到 `<name>`（适配新门，断言仍 201）——合法适配非放松。

---

## 建议改进（non-blocking）

1. **internal/adapter/maven/handler.go:135-138 + virtual_metadata.go:475-505** — local 面 java-agent 剥除后，`serveSidecar`（handler.go:151-200）仍返回**存储文档**的 ledger 摘要：body=剥除派生字节 vs `.sha1/.md5`=未剥除原文摘要——local 面自身不自洽（virtual 面经 writeDerivedSidecar 自洽）。已在提交 Carried 登记为 unprobed；建议探针票把「local sidecar java-agent 面」与下条 remote 面一并钉证据后收口。
2. **internal/adapter/maven/handler.go:135** — remote 仓（TypeRemote）自身缓存快照 metadata 的 java-agent 面是第三个暴露面，未设门（证据只钉了 virtual 合并腿 + local 成员腿）。符合 no-guess 纪律，但建议登记进同一探针票避免长期三面不一致。
3. **internal/adapter/maven/put.go:69,127** — virtual PUT 路由成功后 cfg 取 **member** 行：suppressPomConsistencyChecks 设在 virtual 仓自身时不生效（设在 member 生效）。语义可辩（落地域=member、证据两腿消息一致），建议 compatibility 契约面明确归属后固化。
4. **internal/adapter/maven/put.go:447-478** — `${revision}` 类 CI-friendly pom（坐标为属性占位符）会 409——与参照生态已知行为一致（正是 suppress 旋钮存在的原因），无需改；若后续差分显示 A 做插值再立票。
5. **tools/difftest/v2/cases/npm_cli_tarball_fidelity.py:134-138** — pack 断言条件链在「exit!=0 且文件存在（陈留）」时报 "sha_mismatch" 而非 exit 码——不产生假绿（EXPECTED="sha256-ok"），仅证据可读性小疵。
6. **internal/repo/service.go:1744-1755** — 投影臂 degraded 恒 ""：意图内（零上游无可降级），但 ListWithRemote 消费方若依赖该旗标渲染「远端层静默」状态，投影键将永不显示——行为面变化即票面目标，留档即可。

---

## 范围外与建议（交 conductor）

- **L031 唯一实质差异**（virtual packument `dist-tags.latest` 保留 vs 重算）已由报告移交 compatibility-engineer 裁定——勿遗失（BIN-28 关票≠差异关闭）。
- 提交 f21f1c09 Carried 三项（201 envelope `/binflow` 前缀 seam / mvn.metadata.version3.enabled 契约 / local sidecar+remote 面探针）需有票承接，建议并入本轮派发。
- 本 PR 覆盖 maven（协议关键域）+ repo storage 面：按双审章程属双审强制域，conductor 应已并行派 reviewer-b；若未见 `R3-pr-review-b.md` 请补实例，本报告不代行 B 形态结论。

---

## 取证命令与结果

全部在本 worktree detach 到 f21f1c09 执行（只读 + 限定包测试，无落盘变更；测试后 worktree 已切回原分支，报告文件为唯一新增）：

```
git log --oneline origin/develop..claude/r3-fixes          # 5 提交确认
git diff --stat origin/develop...claude/r3-fixes           # 22 文件 +2044/-33
git show <sha>（逐提交全量 diff）
go vet ./internal/adapter/maven/ ./internal/repo/ ./internal/replication/
  → exit 0
gofmt -l internal/adapter/maven internal/repo internal/replication internal/httpapi
  → 无输出
golangci-lint run internal/adapter/maven/... internal/repo/... internal/replication/...
  → 0 issues
go test ./internal/adapter/maven/ -run 'TestPomPathConsistency|TestSnapshotVersionsUA|TestClientSupportsM3|TestSnapshotRewriteWriteOnlyPrincipal' -count=1
  → ok 2.307s
go test ./internal/adapter/maven/ -count=1                 → ok 20.535s
go test ./internal/replication/ -run 'TestPushHappyPath|TestRetryBackoffSchedule|TestCronRevivalAfterBackoffBurnout' -count=1
  → ok 0.882s
go test ./internal/replication/ -count=1                   → ok 18.701s
go test ./internal/repo/ -run 'Cache|Projection|Virtual' -count=1
  → ok 17.495s
go test ./internal/httpapi/ -run 'TestCacheFaceRestZeroUpstream' -count=1
  → ok 0.875s
git show claude/r3-fixes:docs/compatibility/known-divergence.yaml | grep -c '^- id:'   → 79
（同上 | sort | uniq -d → 空，id 唯一）
grep 凭据扫描 4 个 difftest 新文件 → 零字面量（全部 ctx.sides/${NPM_AUTH}）
git check-ignore tools/difftest/v2/run/x → tools/difftest/v2/.gitignore:1 run/（L031 run 产物缺席=既定 gitignore 姿态，与 L030 批次先例一致，非虚假证据）
```

未取证（超出本角色/环境）：L030/L031 双端差分复跑（需 live A 实例写操作，归 differential-qa-engineer）；`-race` 全量复跑（实现日志声称 full package -race ok 108s——本评审以非 race 全量 + 焦点集复现其非 race 声称，race 声称未独立复跑，但本批改动无新增 goroutine/共享状态，waitAudit 走既有 mutex sink）。

## 评审报告 R3（形态: reviewer-a）
结论: APPROVE
### 必须修改（blocking）
- 无
### 建议改进（non-blocking）
- handler.go:135-138 + virtual_metadata.go:475-505 local 面 java-agent body/sidecar 摘要不一致（已登记 unprobed）→ 探针票收口
- handler.go:135 remote 仓自身快照 metadata 面未设门（第三暴露面）→ 同一探针票
- put.go:69,127 virtual 路由后 suppress 旋钮读 member 配置 → 契约面明确归属
- put.go:447-478 `${revision}` pom 会 409（与参照生态一致）→ 留档
- npm_cli_tarball_fidelity.py:134-138 pack 断言条件链证据可读性小疵
- service.go:1744-1755 投影臂 degraded 恒空 → 留档
