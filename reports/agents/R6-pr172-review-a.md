# R6 PR #172 Review — Reviewer A（correctness：并发/失败处理/边界）

```
Ticket:            PR #172（fix(httpapi): deploy-refusal test map-order flake, T-557/BIN-39）— commit 5a3c5c31 @ claude/r6-flakefix
Role:              code-reviewer (reviewer-a, correctness 视角)
Area:              internal/httpapi/deploy_refusal_family_test.go（建仓顺序确定性）+ .github/workflows/ci.yml（test job 显示名）
Input:             conductor 派发（T-id=T-557、评审清单 6 项）；通读范围=diff 全文、修复前文件
                   （git show 5a3c5c31^）、internal/repo/service.go validateVirtualMembers、putRepoStatus helper、
                   ci.yml test job 全段、CI run 36508756551 失败日志与 jobs API、T-557 报告逐条核对
Findings-Blocking: 无
Findings-NonBlocking:
                   1) T-557.md:38/Evidence 与测试注释「roughly 1 of 8 runs」的概率模型欠精确：map 迭代序由
                      per-map-instance 随机 seed（makemap fastrand）+ 随机起始 offset 共同决定，每次迭代的
                      P(virt 先于 mloc) 本身随槽位布局随机（作者实测 5/80=6.25%，本评审复现 8/60=13.3%，
                      均与机理一致但散布在 1/16~1/8 之上）——不影响结论，仅表述精度。
                   2) （范围外，交 conductor）本 worktree 正被另一会话并发编辑：internal/repo/api.go、
                      internal/repo/service.go 处于中途态（repo.ErrRepoNotEmpty 被移除但
                      internal/httpapi/repo_batch_write.go:436 / repositories.go:865 仍引用），
                      docs/compatibility/{maven-virtual.yaml,known-divergence.yaml,contracts/repositories.yaml}
                      亦 dirty——当前 worktree 脏态下 go build ./internal/httpapi 必失败。不影响 PR #172
                      （未提交内容不随 PR 走、PR 分支在 5a3c5c31 干净），但该会话收尾前本 worktree 内
                      任何构建/测试取证都会假红，且收编时须逐文件核对 status 防混入。
Commands:          git show 5a3c5c31（diff 全文）；git show 5a3c5c31^:internal/httpapi/deploy_refusal_family_test.go
                   （修复前 line 79 取证）；gh run view 36508756551 --log-failed；gh api …/runs/36508756551/jobs
                   --jq '.jobs[].name + " | " + .conclusion'；gh api repos/0ldlight/binflow/branches/main/protection
                   （→404 Branch not protected）；grep -rn 'test (' .github .circleci Makefile（仅 ci.yml 注释命中）；
                   grep -rn ErrRepoNotExist/member 产品码定位；gofmt -l；python3 yaml.safe_load；
                   凭据扫描 git show 5a3c5c31 | grep -inE 'password|secret|token|api[_-]?key|AKIA|PRIVATE|JFrog@|ghp_'（零命中）；
                   git worktree add --detach /tmp/t557-forensic 5a3c5c31（隔离取证，完已 remove）
Outputs:           修复前机理独立复现：CGO_ENABLED=1 go test -race -count=60（预修文件换入）→ 8/60 FAIL，
                   签名逐字=「deploy_refusal_family_test.go:79: create t553-virt: 400 … member "t553-mloc"
                   does not exist」，与 CI run 36508756551 现场一致；修复后（干净 worktree @5a3c5c31）：
                   -count=20 → ok 7.568s，-race -count=30 → ok 99.414s；gofmt 干净；YAML_OK
Verdict:           APPROVE
Lessons:           ① 顺序依赖修复完整性核对的锚=产品码校验点：validateVirtualMembers（internal/repo/service.go:2521）
                   证明「建仓时校验成员存在」是四仓唯一跨仓约束（remote 的 url 是外部 upstream 非仓键，
                   -cache 投影键不经 API 创建）——依赖清单以校验码为准，不靠猜；② 概率型 flake 的独立复现
                   用隔离 worktree 换入旧文件跑 -count 大数（本例 60 抽 8 中），比在共享树上 stash/checkout
                   安全——共享 worktree 有并发编辑者时尤其如此；③ check 名这类「显示面」改动仍可全取证：
                   run jobs API 给旧名 ground truth、branch protection API 给闸门依赖面，不留"应该没人引用"的假设。
```

## 评审报告 PR #172（形态： reviewer-a）
结论: APPROVE

### 逐项核对（派发清单 6 项）

1. **顺序依赖完整性 — 通过**。四仓引用关系逐对核查：t553-rem / t553-grem（remote）仅引用外部 upstream
   `http://127.0.0.1:9/upstream`（非仓键，无跨仓校验）；t553-mloc（local）无引用；t553-virt（virtual）
   `repositories:["t553-mloc"]` — 唯一约束 = mloc 先于 virt，产品码锚点 `internal/repo/service.go:2521-2541`
   `validateVirtualMembers`（`ErrRepoNotFound → "virtual repository member %q does not exist"`，即 CI 400 原文）。
   `-cache` 投影键（t553-rem-cache / t553-grem-cache）是 PUT 路径上的隐式投影，不经建仓 API，无创建序依赖；
   嵌套 virtual（service.go:2548 起）与本夹具无关。slice 序 rem→grem→mloc→virt 满足全部约束，无漏网依赖。
2. **修复引入新问题 — 无**。`for _, r := range repos` 中 r 为逐迭代 struct 值拷贝、无闭包捕获，任何 Go 版本
   均无循环变量语义风险（go.mod=`go 1.26.0`，本就 ≥1.22）；匿名 struct 字段名 key/body 无冲突；
   `t.Fatalf("create %s: %d %s", r.key, …)` 诊断格式与修复前逐字一致（失败仍点名具体仓键）。gofmt 干净。
3. **ci.yml name 行 — 通过，五 lane 展开逐一列出**：`name: test (${{ matrix.lane }})`（matrix context 在
   job-level name 可用，YAML 解析通过）。展开实际值：`test (httpapi)` / `test (repo)` / `test (auth)` /
   `test (search)` / `test (light)`，与 ci.yml:153 T-552 规格注释一致。旧名 ground truth（run 36508756551
   jobs API）：`test (httpapi, ./internal/httpapi)`[failure] / `test (repo, ./internal/repo)` /
   `test (auth, ./internal/auth)` / `test (search, ./internal/search)` / `test (light)`——漂移声明逐字属实，
   且 light lane 旧名本就无漂移（GitHub 略去空串 pkgs），T-557「light 不变」表述准确。闸门依赖面：
   main 无 branch protection（API 404 "Branch not protected"）、repo 内 .github/.circleci/Makefile 零处
   引用具体 check 名、lane 为 push-only 且 PR #172 面（base=develop）根本不跑 lane——无任何配置面可被改名破坏。
4. **机理复核 — 自洽，且独立复现**。CI 现场行号=79；修复前文件第 79 行恰为 map-range 循环内的 t.Fatalf
   （git show 5a3c5c31^ 取证）。隔离 worktree 换入修复前文件跑 `-race -count=60` → 8/60 FAIL、签名与 CI
   逐字一致（同 line 79、同 400 member does not exist）。修复后 `-count=20` 与 `-race -count=30` 全绿。
   作者 5/80 与本评审 8/60 均落在机理预期散布内。
5. **凭据扫描 — 零命中**（全 commit 消息+diff 硬扫 password/secret/token/api-key/AKIA/私钥头/JFrog@/ghp_，
   diff 内容仅 127.0.0.1:9 假 upstream 与仓键）。
6. **诚实性 — 全部声明可复核且相符**：修复前 -race 复现（独立复现✓）、修复后 -count=20 ok（两次独立
   ok：20.401s 共享树 / 7.568s 干净树✓）、gofmt（✓）、YAML_OK（✓）、check 名漂移与 light 不变（✓）、
   「无 CI 闸门依赖具体 check 名」（✓）。唯一精度瑕疵=概率模型 1/8 的表述（见 non-blocking 1），非虚证。

### 必须修改（blocking）
- 无。

### 建议改进（non-blocking）
- T-557.md:38 概率模型表述（1/8 单点值）→ 可改为「per-draw 布局相关，实测 6%~13%」量级表述；纯文档精度，不改代码。
- 范围外发现（交 conductor）：本 worktree 存在另一会话的未完成并发编辑（internal/repo 中途态致
  `go build ./internal/httpapi` 失败：repo_batch_write.go:436 / repositories.go:865 `undefined: repo.ErrRepoNotEmpty`），
  不影响 PR #172 本身，但该会话收尾前本 worktree 构建取证会假红、收编须防混入（见 Findings-NonBlocking 2）。
