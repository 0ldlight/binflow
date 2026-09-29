# R9-pr-review-b — Reviewer B（architecture 形态）审 R9 payload（T-571/T-573/T-574/T-575/T-576 五票合树）

> 修订记录：v1（2026-09-29 晚）REQUEST_CHANGES（1 blocking：generic 拦截臂无仓型门）；
> v2（2026-09-29 复验轮）blocking 已由 commit 5774a072 修复并经本 reviewer 独立复验 → **APPROVE**。
> v1 的 blocking 原文保留于结论区（修复对照），其余字段在 v2 增补复验证据。

```
Ticket:        R9 payload 批（T-571 mime 归属翻转 / T-573 license dev-bypass / T-574 checksum PUT
               拦截族 / T-575 nuget 双面头 / T-576 goproxy CT 拼写；T-572 探针报告无产品码不审）
Role:          code-reviewer (reviewer-b)
Area:          internal/adapter/{generic,maven,goproxy,nuget} + internal/httpapi + internal/license
               + internal/config + DECISIONS.md（docs/compatibility 台账/契约/勘误 diff 做一致性核对）
Input:         conductor 派发令（reviewer-b 形态，五项 architecture 侧重）+ 复验令（blocking 修复
               commit 5774a072）；六份票面日志 reports/agents/T-57{1..6}.md；docs/design/
               architecture.md §5.1 依赖方向；docs/reverse/mime-ownership.md（v17 全表行为规格）；
               docs/compatibility/{contracts/goproxy.yaml,known-divergence.yaml,proposals/
               mime-ownership-ruling.md}；L037-probe-arms.md
Changes:       v1：git diff（22 文件 +893/-296）逐文件 + untracked 11 个 Go 文件全文通读；上下游追读：
               repo.Service Get/Put 的 remote/virtual 分派（service.go:387-475/637-683）、getRemote
               FetchError→ErrNodeNotFound 映射（service.go:505-520）、maven ClassReader seam
               （api.go:41-52）、generic Handler 装配面（api.go:15-47）、license floorState
               （manager.go:383-385）；/tmp/t571-ext-sweep{.py,-r1.json} 探针资产复核。
               v2：git show 5774a072 全量复验（8 文件 +588/-113：api.go ClassReader 缝、handler.go
               local 门、remote_render_test.go 三终缀负测、maven 404 臂 body 排空、34 调用点重接）
Files:         internal/adapter/generic/mime.go — 69 键表转录忠实（.cs→text/x-csharp.sh、.swift 尾随
               空格、.xsl 双登记取先登记者均与 v17 §2 逐键核对）；stdlib 回退与 mimeForNode 删除干净
               internal/adapter/maven/mime.go — 同上，与 generic 表 69 键机械比对零差
               internal/httpapi/mime.go（新）— 第三拷贝 69 键零差；OCI carve-out 前缀树三 vendor
               （docker/oci/cncf）先于表查找，注释载明 T-32 R3 排除理由——次序正确
               internal/adapter/generic/api.go（5774a072）— ClassReader 缝落地：repo.ClassReader 为
               adapter SPI 既有序列（maven api.go:52 同款，metadata.RepoStore 结构性满足+编译期钉住），
               产品码唯一调用点 main.go:1362 传 md.Repos()——依赖方向合规，无 adapter→metadata 直依赖；
               New/NewWithClock 直接扩参（非 With-chain）编译器强制 34 调用点全接，无 nil class 传参
               internal/adapter/generic/handler.go（5774a072）— 拦截臂 local 门与 maven/handler.go:107
               完全同构（含 class.Get 失败容忍姿态：失败即走原链）；注释记载 R9 双审 blocking 理由；
               virtual/remote 不命中门直接落原链
               internal/adapter/maven/{handler,put}.go — 前置臂 local 门+错误形态白名单（仅
               ErrNodeNotFound/ErrIsFolder 短路）；409 路径去前缀与 A 逐字；404 臂补 body 排空
               （5774a072 附带，与 generic 臂 keep-alive 姿态对齐）
               internal/adapter/nuget/flat.go — bare 201 恒渲染 node.Sha256（有效值语义注释准确），
               push 面删头，最小 diff
               internal/adapter/goproxy/handler.go — 两常量拼写翻转，@v/list/@latest 面不动（无取证
               不猜，clean-room 纪律遵守）
               internal/license/{manager,devtier_dev,devtier_prod}.go — build-tag 双臂干净：生效面仅
               State() tier 读数（floorState+覆盖值，Licensed 恒 false 诚实）；pro 覆盖不越权开
               enterprise 槽有测试钉住
               internal/config/load.go — 保留名豁免循 BINFLOW_REMOTE_CREDENTIALS_KEY 先例，语义边界
               在 T-573 报告 Risks ① 如实登记
               internal/httpapi/{storage,repositories}.go — fileInfoOf 改 mimeForNode（carve-out+表）；
               requestBase 第 9 拷贝收敛触发注记（rider ⑥）
               DECISIONS.md — ADR-0032 Errata 只追加，边界澄清准确
               docs/compatibility/* — 台账翻面/版本锚 repoint/勘误均为 conductor 收编动作或票面授权，
               与代码现态逐条核对一致
               测试文件 — 全部行为命名（票号在头注释）、table-driven、经公共 API；源在场过渡态钉住
               有显式翻正注释——显式债务诚实充分；remote_render_test.go 负测断言三终缀 405+Allow:GET+
               upstream hits 不变（零回源实证）
Tests:         v1：go build ./... + vet 七包 OK；涉改六包默认构建 test 全 ok（httpapi 231.8s 亦 ok）；
               license -tags dev ok；gofmt 空；三表 69 键机械比对零差；sweep JSON 独立重放
               （.info/.mod/.cs/.sh/.swift/.xsl generic 仓双面 SAME）；CI 红线 grep 零 dev tag 泄漏。
               v2（5774a072 复验）：go build ./... + vet generic/maven OK；两包 test -count=1 全 ok
               （13.6s/30.7s）；点名 -v：TestRemoteOutcomesRenderThroughHandler（含三终缀 remote
               405+Allow+hits=1 零回源臂）/TestChecksumPutSourceMissing（6 臂）/FamilyNegatives/
               SourcePresentInterim 全 PASS；gofmt 空
Commands:      v1 见前版（build/vet/test/gofmt/三表比对脚本/sweep 重放/CI grep）；
               v2：git show 5774a072 --stat 及逐文件；grep -rn "generic.New(" 全仓调用点核对（产品码
               1 处传 md.Repos()，测试零 nil 传参）；go build ./... && go vet ./internal/adapter/
               {generic,maven}/；go test 两包 -count=1；go test -run 'TestRemoteOutcomes|
               TestChecksumPut' -v；gofmt -l 两目录
Outputs:       reports/agents/R9-pr-review-b.md（本报告，v2=复验轮）
Compatibility: 与 docs/compatibility/contracts/ 对照：goproxy.yaml 双形态 pattern——T-576 新值落 A 臂
               内无违例，收窄归 compatibility-engineer 后续；known-divergence 五条翻面/新条与代码
               现态逐条一致（maven/checksum-put-404-wording resolved 三面属实；write-through 与
               generic 条目维持 BUG 属实——SPI 缺口 BLOCKED 在案，BIN-60 承接）；mime-ownership-
               ruling §3-3a 勘误（rider ④）准确
Security:      攻击面走查：拦截臂终缀剥离只缩短路径无穿越；双臂 local 门防写路径回源（SSRF 姿势
               维持，5774a072 后 generic/maven 对称）；dev tag 零 CI/发布泄漏；凭据零落盘
Performance:   渲染时查表=每 GET/FileInfo 一次 map 查找（同阶）；拦截臂门=一次 O(1) 仓行读（双臂
               同构），family 命中时一次 O(1) 节点查——非热路径；dev 臂 State() 一个 bool 分支
Risks:         ① BIN-60（client-checksum 持久化 SPI）落地前，源在场臂维持显式过渡态（测试钉住+
               台账 BUG 维持）——已登记债务，非本轮风险；② 三表 lockstep 靠人工+三包独立单测互锁，
               下一轮表变更仍是漂移窗口（non-blocking 1 建议收敛触发注记）
Blockers:      无（v1 blocking 已修复复验；取证完整）
Next:          ① 范围外发现 1（L037 Arm 4 .info 表述矛盾）转 differential-qa-engineer/conductor
               勘误；② BIN-60 承接确认（T-574 移交设计具体可执行）；③ T-576 移交项（httpapi 表
               四键核对）已随 T-571 sweep 自动闭合，无需另票；④ non-blocking 1/2 收编时顺手
```

## 评审报告 R9 payload（形态: reviewer-b）
结论: APPROVE（v2 复验轮——v1 的 1 条 blocking 已由 commit 5774a072 修复并独立复验通过）

### blocking 修复复验（v1 条目 → 5774a072 处置）

- **v1-blocking：internal/adapter/generic/handler.go 拦截臂缺仓型门——remote generic 仓写路径 svc.Get 回源预热缓存 + 上游 miss 时以 404 checksum 文案顶替 RE-05 405，且 remote 面无 A 取证；与 maven 臂防护不对称。**
  修复复验（全部通过）：
  1. **ClassReader 缝的依赖方向**（api.go:16-51）——`repo.ClassReader` 为 adapter SPI 既有序列（maven api.go:52 先例，`_ repo.ClassReader = metadata.RepoStore(nil)` 编译期钉住），产品码唯一调用点 main.go:1362 传 `md.Repos()`，装配经 main 而非 adapter 直摸 metadata——architecture §5.1 合规。New/NewWithClock 直接扩参（编译器强制 34 调用点重接，优于 With-chain 的运行时可漏接），全仓 grep 无 nil class 传参。
  2. **门的姿态**（handler.go:126-137）——`if row, rerr := h.class.Get(r.Context(), repoKey); rerr == nil && row.Type == repo.TypeLocal` 与 maven/handler.go:107 逐字同构：class 查询失败容忍（失败即走原链）、virtual/remote 不命中门直接落原链、注释记载双审 blocking 理由。跨包防护对称达成。
  3. **负测钉住**（remote_render_test.go:103-119）——三终缀 PUT 臂断言 405 + `Allow: GET` + upstream `hits.Load()==1`（计数不变=零回源实证），-v 复跑 PASS。
  4. 附带 maven 404 臂 body 排空（handler.go ServeHTTP 前置臂）与 generic 臂 keep-alive 姿态对齐——合理。
  5. 只读复验门：go build ./... + vet OK；generic/maven 两包 test -count=1 全 ok（13.6s/30.7s）；gofmt 空。

### 建议改进（non-blocking，维持 v1）

- **internal/adapter/{generic/mime.go:12-14, maven/mime.go:6-9, httpapi/mime.go:11-13}** — mime 表拷贝族缺自己的收敛触发条件（requestBase 族 rider ⑥ 有先例）。建议各补一行同款触发。本轮三表 69 键机械比对零差+三包独立单测互锁，不 blocking。
- **reports/agents/T-574.md Risks ③** — 「拦截臂遇非 local 错误形态保持原链」在 v1 时与代码不符；5774a072 后代码已具备 local 门，该句语义成立，但建议顺手改写为描述现实现（门而非「错误形态」）。
- **internal/adapter/nuget/flat.go:530-536** — `node != nil` 死条件（svc.Put 成功必返 node）。无害。
- **internal/adapter/generic/api.go:38-44** — New 的 class 参数注释未写明 required（maven 先例注明 "class is required"）。无 nil 防御与 maven 姿态一致，仅建议注释补一句。

### 范围外发现（交 conductor，维持 v1）

- **reports/compatibility/L037-probe-arms.md Arm 4（line 131）「generic 仓 .info=octet-stream」与 T-571 活体 sweep 实测矛盾**：/tmp/t571-ext-sweep-r1.json 中 generic 仓 `file.info` 双面 `application/json+info`（r1≡r2 SAME）——L037 该句是无逐腿证据的推断；T-576 据此登记的「域特有归属张力」移交项实际已被化解。建议 conductor 在 L037 或 known-divergence 加勘误一句，防后续票按错误观察决策。
