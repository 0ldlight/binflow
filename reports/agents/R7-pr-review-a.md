# R7 载荷合并前评审（形态: reviewer-a / correctness）

Ticket:        R7 payload 合并批（10 提交 5a1e4356..4145b059；T-562 stage2/T-566/T-564/T-565①②③/契约与台账批）
Role:          code-reviewer (reviewer-a)
Area:          maven adapter walk + write-path 物化 / generic 201 前缀 / web 删除流 / 兼容契约与台账
Input:         conductor 派发（diff origin/develop..claude/r7-payload）；通读 walk.go 全文、handler.go/calc.go/put.go/snapshot.go/virtual_metadata.go、repo/virtual.go（getVirtual/ReadVirtualMember/probeLocalMember/withResolvedFrom）、generic handler.go/iteminfo.go、web 删除流（RepoDeleteConfirm/repos.ts/i18n/RepoDetailPage/RepositoriesPage/e2e×3）、契约 maven-virtual.yaml（⑩⑪⑫ + derived-sidecar 族）、金样 walk 四组 metadata、known-divergence.yaml
Changes:       产品码 diff 全量（maven 4 文件 + generic 2 文件 + web 8 文件）逐行；上下游追读至 repo.Service 虚拟解析层与 storage 打开路径；测试改动（4 文件改 + 2 文件新）逐条核对翻面理由
Files:         internal/adapter/maven/walk.go（新）——1 blocking（sha512 旁车 walk 腿 500）；handler.go——t8 门/挂点顺序/serveNode·serveSidecar 拆分纯迁移（等价性核实）；calc.go——pom 腿 module trigger 同步化正确；snapshot.go——versionDirPrefix 消重等价；generic handler.go/iteminfo.go——前缀改动最小且自测扎实；web 全链一致
Tests:         PASS×6（见 Commands）；变异阴性对照 1 次 FAIL 后还原复跑全绿；sha512 腿 scratch 取证 1 次（500 复现，文件已删）
Commands:      go test ./internal/adapter/maven/ -count=1 → ok 25.351s
               go test ./internal/adapter/maven/ -race -count=2 → ok 211.329s
               go test ./internal/adapter/generic/ ./internal/httpapi/ ./internal/repo/ -count=1 → ok 8.093s / 213.114s / 84.195s
               go build ./... → OK；gofmt -l internal/ cmd/ → 空；go vet ./internal/adapter/maven/ ./internal/adapter/generic/ → 空
               npm run typecheck（web）→ 通过零输出
               变异：selectWalkCandidate bn 比较改 fmt.Sprintf 字典序 → TestPlainSnapshotWalkSelection/s3 FAIL（w2s3-1.0-…-9.pom 胜出≠s3-bn10）→ git checkout 还原 → 全包复跑 ok 25.150s
               scratch 取证：unique 家园落 plain pom 后 GET …-SNAPSHOT.pom.sha512 → maven-unique 与单成员 virtual 双面 500 `{"status":500,"message":"digest sha512 is not available (ledger gap)"}`（控制组 .sha1=200；文件已删）
               台账复算：python yaml 实枚举 → total 98 = resolved(带 resolved 块) 47 + open 51 —— 与 4145b059 声称 Z=51/47 精确一致
Outputs:       reports/agents/R7-pr-review-a.md（本文件）
Compatibility: 契约⑩ surface 只列 .sha1/.md5 旁车、.sha256/.sha512 明记 unobserved——但 derived-sidecar 族契约（maven-virtual.yaml L106）钉死「.sha512 旁车双面 404（L032）」：walk 腿 500 同时违反该钉死臂与 layout.go:30 自述模型，属 blocking；⑩⑪⑫ 其余断言（触发门/四元组/选版/跨成员 mtime/tie 不立断言）与实现逐条对得上；⑨ 物化时机锚与 recalcSync 实现一致
Security:      凭据扫描：diff 与报告零字面量（difftest 探针经 ctx.sides 注入，两处 password 命中均为格式串）；无注入/穿越新面（walk 的 prefix/ListByPrefix 走既有路径校验）
Performance:   servePlainWalk miss 探测 Get+Close 后普通面二次 Get（plain 命中路径每次多开一次 blob；miss 路径 virtual 全解析跑两遍）——量级可忽略，记录不动；pom 腿 module recalcSync 使 PUT 响应阻塞面扩大至模块级 recalc（exec 锁串行化）——契约钉 A 同步姿态，接受
Risks:         ①walk Get-miss 与 ListByPrefix 之间存在 TOCTOU（并发落 plain 拼写则服务时间戳候选）——窗口不可观测、参考未取证，接受；②serveVirtualWalk 吞掉全部 ListVirtualMember 错误（含瞬时基础设施错误）→ 全员失败时走 404，普通面二次 Get 可纠为 5xx——记录
Blockers:      无（取证环境完备）
Next:          修复 blocking-1 后只需 reviewer-a 复核单点（sha512 腿 + 新增回归测试），不必重开双审全量；建议顺手把 non-blocking-2/3 一并带入修复票（同一文件三行内）

## 评审报告 R7（形态: reviewer-a）
结论: REQUEST_CHANGES

### 必须修改（blocking）
- internal/adapter/maven/walk.go:161-163（local 腿）与 walk.go:220-222（virtual 腿）：plain-SNAPSHOT `.sha512` 旁车 walk 解析后经 serveSidecarOfPath/writeSidecarDigest → digestOf("sha512") 恒 (_, false) → **500 "digest sha512 is not available (ledger gap)"**（已双面实证）。同族其余拼写全 404：直接时间戳拼写 .sha512 走 serveSidecar 的 sha512→404（handler.go:216-221，拆分时该闸留在了 serveSidecar，新出口没继承）；契约 derived-sidecar 族钉死「.sha512 旁车双面 404（L032）」；layout.go:30 自述「.sha512 sidecar GET answers 404」。5xx 出现在 miss 族 + 错误归因为假 ledger gap，双错。→ 最小改法：servePlainWalk 旁车臂入口加 `if l.Algo == "sha512" { return false }`（fall-through 由 serveSidecar 渲染既有 404）；或把 sha512 404 闸从 serveSidecar 下沉进 serveSidecarOfPath 使全部调用方共享。virtual 腿（writeSidecarDigest 直调）随之一并收口。补回归测试（plain .sha512 于 local+virtual 面 = 404；现测试 t5/t6 只覆盖 sha1/md5）。

### 建议改进（non-blocking）
- walk.go:219 + handler.go:296：serveVirtualWalk 文件腿 applyReaderHints 双次调用（Header().Add 叠值）。当前 ReadVirtualMember 仅返 probeLocalMember 裸流（无 ExtraHeaders）故为 no-op，但属潜伏重复头。→ 把 219 行挪进 sidecar 分支（或删去，serveNode 已统一施加）。
- repo/virtual.go:734 ReadVirtualMember 不做 withResolvedFrom 包裹（仅 getVirtual virtual.go:404 包）——walk 腿的 virtual GET/旁车无 X-BinFlow-Resolved-From，与普通 virtual 面不一致（诊断头、契约中立）。→ 接受或在 walk 腿显式补。
- handler.go writeSidecarDigest 500 文案丢了 repoKey/path 上下文（旧 "digest %s of '%s/%s'…"→新 "digest %s…"），违反错误带上下文惯例。→ msgPath 已在调用链上，回填即可。
- 走查确认无恙项（留痕）：fall-through 的 rc 已 Close（walk.go:138，无泄漏）；错误分类只认 ErrNodeNotFound；walkCandidates 过滤完备（folder 行/非直系/metadata 文件/解析失败/非时间戳/四元组族）；selectWalkCandidate ts 定宽字典序=时序 + bn 数值比较（变异实证测试咬合）；跨成员 tie=walk 序靠后成员与注释及金样「不立断言」一致；remote 行/remote 成员/cache facet 三层 skip 与 §3.6、filterMetadataSteps(levelSnapshot)、getVirtual 镜像一致；t8 门只动读面（PUT 400 保留）、挂点在 class gate 后 sidecar/file plane 前（409 优先）；serveNode/serveSidecar 拆分为纯迁移，digestTriple 的 ctx→r.Context() 等价（差值仅 WithDeployProps 值，ledger 不读），304/Range/416/ETag/HEAD 面保持；recalcSync log-and-continue 不会 fail 已落盘部署、调用点在 writeCreated 前（ctx 存活）、version/delete/metadata 触发面零改动；versionDirPrefix 两调用点表达式逐字相同；T-564 前缀仅 Location+uri/downloadUri 且测试断言 Location==uri 逐字节 + 可回解析；web 删除流组件/lib/i18n(zh+en)/调用点/e2e 五处一致，toast 计数条件正确；台账 Z 复算精确吻合。

## 复核附录（blocking-1 修复 61e05e53，reviewer-a 单点）

修复形态核对：闸下移至 writeSidecarDigest 顶部（签名 +repoKey/path），serveSidecar 缩成纯委托，serveSidecarOfPath 与 serveVirtualWalk 旁车腿两调用点同走共享出口——正是处方方案；n.b.① 落地（virtual 文件腿删预施加，serveNode 自带；旁车腿保留施加，无丢失）；n.b.③ 落地（ledger-gap 500 恢复 repo/path 上下文）。

独立取证（与落库回归测试不同构造：timestamped 直种目录、双成员 virtual、HEAD 腿、non-unique 存量 plain 面、.sha256 对照）：
- walk 腿 sha512（GET local / GET virtual / HEAD virtual）→ 404（原 500 消失）；
- 普通面对照（直接时间戳拼写 .sha512、non-unique 存量 plain 拼写 .sha512 ×2）→ 404 不回归；
- .sha256 walk → 200 = 解析目标摘要（闸 sha512 限定，未过捕）；
- TestPlainSnapshotWalkSha512SidecarGate PASS；全包 -count=1 ok 25.458s；walk/sidecar 族 -race ok 9.186s；go build OK、gofmt 空、vet OK。
- 顺带实证：timestamped 拼写 pom 的 GAV 一致性门（T-543）仍在位（scratch 首版错 groupId 被 409 正确拒绝）。

残余记录（non-blocking，cosmetic）：virtual 旁车腿的 sha512 404 消息携带解析后成员候选路径（best.path）而非请求拼写——与普通面 404 命名请求路径的惯例差一词；状态与消息族一致，不动。

结论: APPROVE（blocking-1 已闭合，复核证据如上；其余评审结论不变）
