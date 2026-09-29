# R9-pr-review-a — Reviewer A（correctness）审 R9 payload（T-571/T-573/T-574/T-575/T-576 + T-572 台账面）

> 轮次：初审（2026-09-29，工作树未提交态）结论 REQUEST_CHANGES（1 blocking）→ 修复 commit
> 5774a072 → 复验（同日，分支 HEAD）结论 **APPROVE**。初审全文保留于下半部，复验记录在上。

## 复验轮（commit 5774a072，只复验 blocking 与直接关联面）

结论: **APPROVE**

blocking 1（generic 拦截臂缺仓型门）已修复且经独立取证证实：

1. **门已落**：`internal/adapter/generic/handler.go` handlePut 拦截调用点现以
   `h.class.Get(r.Context(), repoKey)` → `row.Type == repo.TypeLocal` 为门（与 maven 臂同款姿态）；
   remote/virtual 保持原链。`internal/adapter/generic/api.go` 新增 `repo.ClassReader class` 缝，
   New/NewWithClock 扩参，main.go:1362 传 `md.Repos()`，全仓 grep 无 nil-class 调用点（33 处测试
   调用点 + 1 处 main 全部实参接线；nil 将在首次受门 PUT 上 panic——无人这么接，可接受）。
2. **独立复现翻绿**：同款 `-overlay` 取证探针（零仓库写入）复跑 HEAD：
   - leg1（源在场未缓存）：PUT `<remote>/up.bin.sha1` = **405 RE-05 逐字，upstream hits Δ=0**
     （初审为 MISS 全量拉取后 405）；
   - leg2（源上游也缺）：PUT `<remote>/missing.txt.sha1` = **405 `Remote repository ... is a
     read-only proxy cache; deployments ... not accepted.`，upstream hits Δ=0**
     （初审为 404 `Target file to set checksum on doesn't exist`）。
   两条回归腿全部治愈，写路径零回源接触。
3. **仓内负测在案**：`internal/adapter/generic/remote_render_test.go` RE-05 块后新增三终缀
   （.sha1/.md5/.sha256）PUT 臂——断言 405 + `Allow: GET` + upstream hits 恒 1（零接触计数），
   `TestRemoteOutcomesRenderThroughHandler` PASS。local 面拦截行为未回退
   （TestChecksumPutSourceMissing 六臂 / FamilyNegatives / SourcePresentInterim 全 PASS）。
4. **NB-4 顺手修复确认**：maven handler.go Parse 失败臂 404 前已 `io.Copy(io.Discard, r.Body)`，
   与 generic 臂对齐。
5. **门命令复跑**：`go build ./...` OK；`go vet ./...` OK；`gofmt -l`（generic+maven）空；
   `golangci-lint run ./internal/adapter/generic/... ./internal/adapter/maven/...` = 0 issues；
   `go test -count=1 ./internal/adapter/generic/ ./internal/adapter/maven/` = ok 13.3s/30.6s。
   coordinator 侧全量（httpapi/replication/client/nuget/goproxy/pypi/cargo/deb/rpm/helm/conan/npm/
   helmoci）声称全绿，与 34 调用点编译面（build ./... 通过）一致，采信。

初审 non-blocking 1/2/3（virtual 面语义、大小写、拦截序）维持登记态——均为未取证面而非缺陷，
不阻 APPROVE；NB-5（L037 Arm 4 括注 vs mime-ownership §2 口径冲突）仍交 conductor/
reverse-engineer；NB-6/7（carve-out 无影、裸终缀边角）初审已证实无问题。

剩余风险提示（不阻断）：T-574 报告 Risks ③ 的失实表述（「拦截臂遇非 local 错误形态保持原链」）
在修复后字面上已无害（现行为确实保持原链），但报告原文未勘误——conductor 收编时可加一行。

## 初审轮（工作树未提交态，全文存档）

```
Ticket:        R9 payload：BIN-53/T-571（mime 归属翻转）+ BIN-55/T-573（license dev 旁路）
               + BIN-56/T-574（checksum PUT 拦截族）+ BIN-57/T-575（nuget 双面头）
               + BIN-58/T-576（goproxy CT 拼写）；P1 族
Role:          code-reviewer (reviewer-a)
Area:          internal/adapter/{generic,maven,goproxy,nuget} + internal/httpapi + internal/license + internal/config
Input:         conductor 派发令（worktree 全部未提交改动）；reports/agents/T-{571,573,574,575,576}.md；
               reports/compatibility/L037-probe-arms.md Arm 1/2/4/5；docs/reverse/mime-ownership.md §1/§2；
               internal/repo/{service,virtual,api}.go 上下游追读（Get/Put/resolveWriteRepo/StatusError 链）
Changes:       git diff 22 文件 + untracked 产品码 11 文件全读；上下游追读：repo.Service.Get 的
               remote/virtual 分派、resolveWriteRepo→refuseNonLocalWrite（RE-05 405）、StatusError.Unwrap、
               license Manager 全部读点（State/AddonEnabled/PackageTypeAvailable）、adapter registry 派发键
Files:         generic/{mime,handler,iteminfo}.go=翻转落地正确（local 面）；maven/{mime,put,handler}.go=同构正确
               且拦截臂有 local 限定；goproxy/handler.go=常量翻转正确；nuget/flat.go=双面头正确；
               httpapi/mime.go=carve-out 无表值影子（69 值逐一核对）；license/{manager,devtier_*}.go=正确；
               config/load.go=仅白名单名正确。**generic/handler.go 拦截臂缺 local 限定 → blocking**
Tests:         见 Commands；全部绿（含 -tags dev）；但 generic 拦截族测试无 remote/virtual 仓负测腿——
               blocking 缺陷正落在此盲区（overlay 取证证实）
Commands:      go build ./...（OK）；go vet ./...（OK）；
               GOLANGCI_LINT_CACHE=/tmp/r9rev-lintcache golangci-lint run ./internal/...（0 issues）；
               go test -count=1 ./internal/adapter/{generic,maven,goproxy,nuget}/ ./internal/license/
               ./internal/config/（ok 14.2s/34.8s/15.5s/26.3s/2.5s/1.9s）+ ./internal/httpapi/（ok 206.9s）；
               go test -tags dev ./internal/license/（ok）+ go build -tags dev ./...（OK）+ go vet -tags dev
               ./internal/license/ ./internal/config/（OK）；
               三表机械比对（python 正则逐键）：69/69/69，diff=NONE/NONE；
               /tmp raw TCP 探针：Go net/http wire 面裁剪尾随空格（"Content-Type: text/x-swift" 无尾空格）；
               go test -overlay 取证（仅 /tmp 文件，零仓库写入）TestReviewerRemoteChecksumPut：
               leg1 源在场=upstream hit+1→405；leg2 源缺=upstream hit+1→404 拦截文案
Outputs:       reports/agents/R9-pr-review-a.md（本文件）
Compatibility: 本视角不适用（契约面归 Reviewer B）；注：L037 Arm 4 括注与 mime-ownership §2 的
               .info/.mod 归属表述冲突（见 non-blocking 5）
Security:      攻击面走查：路径拼接无新增（终缀剥离只缩短）；凭据零落盘（日志复核）；**发现一项
               写路径副作用面**：remote 仓 PUT 触发上游拉取+缓存落盘（blocking 1 的 leg1）——
               已授权读权限者可用 PUT 动词预热/填充 pull-through 缓存，非越权但违反 RE-05 零副作用语义
               【复验轮已治愈】
Performance:   渲染时查表=每请求一次 ToLower+map 查，同阶；stdlib 回退删除减少一次潜在锁竞争；
               拦截臂源缺腿一次 O(1) Get 后 404 短路（免 body 落盘）——除 blocking 1 的 remote 腿
               （触发整制品上游下载）外无热 path 影响【remote 腿已治愈】
Risks:         ① blocking 1 修复时的装配缝：generic.Handler 现无 ClassReader（maven 有 h.class），
               修法需经 New() 接线 metadata.RepoStore【已按此修法落地】；② virtual 面语义翻转未取证
               （NB-1）；③ 三表 lockstep 靠三包独立测试互锁，无编译期强制（rider ⑥ 已注记收敛条件，可接受）
Blockers:      无取证障碍（全部命令可跑、结果与各报告声称一致——除 T-574 Risks ③ 一处声称与代码
               行为不符，归入 blocking 1 证据面）
Next:          ① 修复 blocking 1（建议随修复补 remote 仓 .sha1 PUT 负测腿：405 逐字 + upstream 零接触）
               【已兑现，见复验轮】；② NB-1/NB-2/NB-3 三个未取证面建议入差异池探针候选；
               ③ NB-5 文档口径冲突交 conductor/reverse-engineer 对齐；④ 本 payload 含 storage/protocol
               关键域，B 形态评审应并行在案
```

### 初审结论区（形态: reviewer-a）——结论: REQUEST_CHANGES（已被复验轮翻为 APPROVE）

#### 必须修改（blocking）

- **B1** `internal/adapter/generic/handler.go:114-121（调用点）+ :154-176（interceptMissingChecksumPut）` — 拦截臂调用 `h.svc.Get` **无仓型限定**，而同票 maven 臂有（`internal/adapter/maven/handler.go:107`）。后果（overlay 取证证实）：
  1. **RE-05 违反**：generic remote 仓上 `PUT <remote>/x.sha1`（源上游也缺）→ `repo/service.go:517-521` 远端 unfound FetchError 包 ErrNodeNotFound，`repo/api.go:793` Unwrap 使 errors.Is 命中 → 拦截臂答 **404 `Target file to set checksum on doesn't exist: <remote>:x`**，替换了写 remote 应有的 405 read-only 拒绝（契约由 `generic/remote_render_test.go:91-94`、cargo/deb/conan remote 测试与 `httpapi/deploy_refusal_family_test.go:107` 钉住）。
  2. **写动词副作用**：源上游在场未缓存时，PUT 触发完整上游拉取（cache MISS 落 blob + 节点 + artifact/cached webhook，`repo/service.go:523-536`）后仍落 405——写动词预热 pull-through 缓存，零 A 证据。
  3. **证据不符**：T-574 报告 Risks ③「拦截臂遇非 non-local 错误形态保持原链不猜」与代码行为不符。
  → 修法：generic.New() 接线 `repo.ClassReader`，拦截臂仅 TypeLocal 探测。【修复=commit 5774a072，复验通过】

#### 建议改进（non-blocking，初审清单一仍有效；NB-4 已修复）

1. generic virtual 面语义翻转未取证（聚合读面决定 404 vs 原 deployment-member 部署）；与 B1 修复后的 local-only 门一致地被排除——维持登记探针候选。
2. 终缀匹配大小写敏感（`.SHA1` 不拦截→普通部署）vs mimeByPath 不敏感——探针候选。
3. X-Checksum-Deploy 先于拦截、畸形头+源缺答 404 非 400——两个未取证序面，登记即可。
4. ~~maven 臂 404 前未排空 r.Body~~ 【已修复：commit 5774a072，io.Copy(io.Discard) 已确认在案】
5. （范围外）L037 Arm 4 括注「generic 仓 .info=octet-stream」与 `docs/reverse/mime-ownership.md` §2（v17 全表全局含 .info/.mod）及 T-571 sweep 42/42 SAME 矛盾——交 conductor/reverse-engineer 收口。
6. OCI carve-out 影子核查**证实无影**（69 值逐一核对无 vnd.docker/oci/cncf 前缀；新模型新行不可携前缀）；仅 pre-flip 旧行继续渲染存值，属已登记旧行分歧类。
7. 裸 `.sha1` 空/纯终缀边角行为正确（len 守卫排除，空源名不可达，无穿越面）；generic（201 普通部署）与 maven（400 bare-suffix）答案不同但均未取证 A。

#### 已核实为正确的关键面（取证摘要）

- 三表 lockstep：generic/maven/httpapi 各 69 键机械比对全等；`.swift` 尾随空格 FileInfo 面逐字保留、wire 面由 Go 传输层裁剪（raw TCP 实测）——与 T-571 声称的 A 双面形态一致。
- maven 臂 local 限定正确；409 去前缀文案与探针逐字一致；.sha512 族外保守不猜符合 clean-room 纪律。
- license devtier：全门经 State() 汇合；prod 臂 env 完全忽略有 `!dev` 测试钉住；config 白名单仅容忍名不读值；非法值 fail-safe community + 恰一行 WARN。
- nuget：bare 201 头恒渲染自 node.Sha256（有效值）；push 四入口去头负测 + 409 无头负测俱在。
- goproxy：拼写翻转 + 全栈精确相等断言 + .mod.gz 双动词 404 钉住路由集。
- 四门全绿属实（含 -tags dev）；各报告声称与复跑一致（除 B1-3 一处，修复后无害化）。

## 断点快照

- 已完成：初审全量 + 复验轮（blocking 修复独立取证 + 关联面四门 + 仓内负测确认）+ 结论翻转 APPROVE。
- 未完成：无。
- 断点位置：无悬挂；/tmp 取证资产（overlay 探针）复跑命令在 Commands，复验轮输出已录。
