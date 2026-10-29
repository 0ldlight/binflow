# T-L043-wave3-product-impact — L043 wave-3 两项新分歧候选产品影响调查

```
Ticket:       T-L043-wave3-product-impact + C-b 缓存表示污染 / C-a 匿名内容面放行 + P1（安全相关）
Role:         dev-go-core
Area:         调查票——只读走读 internal/{adapter/docker,repo,config,httpapi}，零产品码变更
Input:        差分 wave-3 活体结论（reports/compatibility/L043-candidates-deep-dive.md §2/§3：
              C-b=TRUE 定谳、C-a 环境面/代码语义不可分标注）；docs/reverse/docker-registry.md §4
              （Accept 协商与 schema2→1 转换，置信度高）；DECISIONS.md ADR-0009（匿名读默认开，
              用户定案）；docs/compatibility/known-divergence.yaml R-22b/C03 既有席位
Changes:      1) C-b 根因代码级定位：BinFlow manifest pull-through 缓存的是【协商表示】而非规范
              schema2——冷取把客户端 Accept 逐字转发上游，落库字节与 mime 均为上游协商结果，命中
              路径零 Accept 检查零转换；与活体 D2/D3 逐点吻合，排除「规范 schema2 + 服务时转换
              另有原因」分支。
              2) C-b 修复评估：第一步「缓存规范化」（低-中难度，单点改 Accept 集）可恢复对
              schema2 客户端的缓存透明性；第二步「服务时转换」（高难度）涉新序列化器 + 低置信度
              字节形态 + 推翻 M2 规格裁剪，建议另立票。
              3) C-a 根因定位：非缺认证代码，是 ADR-0009 产品裁决的「匿名读默认开」（默认值 + 三层
              门代码在案）；A 侧 401 = 参照实例匿名关闭（实例配置差）。对齐方案两案并陈，建议
              案 1（维持 B 姿态 + 同配置对照轮剥离环境差），证据并入既有 R-22b 裁定席位。
Files:        新增：reports/agents/T-L043-wave3-product-impact.md（本报告）
              修改：无产品码文件（调查票不改码）
              删除：无
Tests:        无新增/修改测试（零产品码变更；修复实施时按下述各修复案各自的测试面另行覆盖）
Commands:     git -C <wt> branch --show-current
              grep -rn -l -i 'manifest.*cache' <wt>/internal/
              grep -n 'func \|Accept\|accept' <wt>/internal/adapter/docker/remote.go
              sed -n '944,1243p' <wt>/internal/adapter/docker/remote.go（serveRemoteManifest/
              landFetchedManifest/serveRemoteManifestCopy 全文走读）
              sed -n '1594,1632p' <wt>/internal/adapter/docker/remote.go（resolveRemoteRef）
              grep -n 'AnonymousAccess && principalOf(r) == nil\|p == nil && !h.opts.AnonymousAccess'
              <wt>/internal/adapter/docker/{handler,catalog}.go
              grep -n 'AnonymousAccess' <wt>/internal/config/{config,api,load}.go
              sed -n '124,131p' <wt>/DECISIONS.md（ADR-0009）
              sed -n '112,131p' <wt>/docs/reverse/docker-registry.md（§4 协商规格）
              grep -n 'landFetchedManifest\|fetchManifest' <wt>/internal/adapter/docker/virtual.go
Outputs:      关键证据行（逐条对应下文根因行号）：
              - remote.go:195-196 注释自白："accept is the CLIENT's Accept set, forwarded verbatim
                (upstream content negotiation is the registry's business; BinFlow never converts)"
              - remote.go:1028（direct）/ virtual.go:230（virtual member walk）：
                entry.fetchManifest(ctx, ref.image, reference, r.Header.Values("Accept"), …)
              - remote.go:1168-1179：mediaType := mediaTypeOfHeader(fetched.header.Get("Content-Type"))；
                sink.land(manifestNodePath(ref.image, dgst), dgst, mediaType, bytes.NewReader(fetched.body))
              - remote.go:1205：sink.record(dgst, tag, mediaType, …)（tag/index 行记协商 mime）
              - remote.go:993-995 + 1255 + 1297：HIT → serveRemoteManifestCopy → io.Copy 原字节、
                Content-Type=存储 mime，无 Accept 分支；STALE/revalidated 臂同构（1057/1073/1089/1098）
              - config.go:18-19：DefaultAnonymousAccess = true（注释：mirrors ADR-0009）
              - handler.go:283：if !h.opts.AnonymousAccess && principalOf(r) == nil { challenge }
              - catalog.go:70：if p == nil && !h.opts.AnonymousAccess { challenge(scopeRegistryCatalog) }
              - docker-registry.md §4 置信度：高——"存储为 schema2、Accept 不含 schema2（老 docker
                客户端）→ 服务端把 manifest 转换为 schema1 signed 再返回
                （DockerSchemaProvider#convertSchema2To1）；转换失败则原样返回 schema2"；
                BinFlow 校准行："M2 不做 schema2→1 转换（2026 年无老客户端）"
              - known-divergence.yaml:920（rest/anonymous-read-default-policy）与 :93
                （docker/remote-anonymous-token-policy）——匿名同族既有 UNKNOWN 席位（R-22b / C03）
              - self-check：go build 未运行（零码变更，四门不适用；纯调查票）
Compatibility: C-b——实现与规格（docker-registry.md §4）在 remote 面的偏差结构：A 规格语义=缓存行
              永持规范形（schema2 优先存储）、服务时按 Accept 协商/转换；B 现状=缓存行持首次冷取的
              协商形、服务相位无关。local 面无此问题（schema1 push 被 manifest.go:552-553 拒收，
              本地恒 schema2）——分歧仅限 remote pull-through 与 virtual remote member（共用
              landFetchedManifest，virtual.go:253）。契约侧：docker-remote.yaml 无 manifest 冷取
              表示的 expect 条目（契约缺失），以 reverse 规格置信度（高）为准。
              C-a——代码语义与 ADR-0009 一致、与 A 出厂默认（匿名开）大概率同向；当前差分分歧是
              实例配置差。待裁面已有 R-22b 席位，非新分类。
Security:     C-b——无直接安全面（表示降级非权限问题）。第二步修复若引入 schema1 出站路径：
              prettyjws 的 JWS/JWK 块只作透传字节不解析不 eval（CVE-2017-11468 族教训），签名材料
              不得入日志（现有 internal/redact 层覆盖面届时核对）。
              C-a——匿名读默认开是 ADR-0009 已裁暴露面：仅内容 GET/HEAD，写与 /binflow/api/** 恒
              认证（handler.go:283/cataog.go:70 两门 + authorizeRoute 授权兜底 fail-closed）。
              catalog 匿名列举暴露镜像命名空间属裁决内可接受暴露（catalog.go:162-172 在开关关时
              challenge）。C-a 观测到的 200 属该裁决预期覆盖面，非代码缺陷；无需安全修复，
              仅需裁决记录。
Performance:  无（调查票）。注：第一步修复若实施，冷取恒请求规范 Accept 集不影响缓存命中率与
              上游会话复用（fetchManifest 的 session/token 缓存不受 Accept 影响）。
Risks:        1) C-b 第一步修复会把 schema1-only 老客户端的冷取从 200（拿到 prettyjws）变为
              404（acceptAllows 拒绝，remote.go:1172-1176）——对 A 是服务时转换 200。该残余差异
              需在 known-divergence 记录，或由第二步转换票闭合。
              2) A 服务时转换（convertSchema2To1）的输出字节形态无 reverse 规格（签名有无、
              envelope 字段序）——低置信度区，第二步实施前须差分取证，不得猜测补齐
              （clean-room 铁律）。
              3) C-a 的 A 侧出厂默认（全新安装匿名开关状态）未活体复验——当前仅知参照实例关闭；
              R-22b 裁决前「翻 B 默认值对齐实例配置」存在方向性风险（产品迁就实例配置）。
              4) 本报告仅静态走读 + 既有活体报告引用；未复跑差分探针（run/l043d/ 证据归
              differential-qa-engineer 域）。
Blockers:     无（调查完成；两项的裁决均不在 dev-go-core 权限内，交 conductor/产品）
Next:         1) C-b 第一步「缓存规范化」立实施票（建议 P1）：remote.go:1028 与 virtual.go:230 的
              Accept 参数改规范集（schema2/list/OCI 优先），补「冷取后 schema2 客户端命中」回归
              测试 + 契约条目；残余 schema1-only 404 差异登记 known-divergence。
              2) C-b 第二步「服务时转换」独立评估票（P2）：先差分取证 A 转换形字节，再裁是否
              推翻「M2 不做 schema2→1」规格裁剪。
              3) C-a 证据并入 R-22b（known-divergence rest/anonymous-read-default-policy 的
              evidence 字段追加 L043 wave-3 docker content 面行）；建议差分侧补一轮
              B SECURITY_ANONYMOUS_ACCESS=false 的同配置对照（环境差剥离），随 R-22b 一并呈批。
```

---

## 附：调查正文

### 一、C-b manifest 缓存表示污染——根因与修复评估

#### 1.1 结论

BinFlow 的 manifest pull-through 缓存的是**协商表示**（首次冷取时上游按 Accept 协商返回的字节与媒体类型），不是规范 schema2。命中路径对该缓存行**相位无关地原样服务**（无 Accept 检查、无转换）。活体 D2（冷 schema1 拉取后 schema2 客户端拿到 prettyjws）、D3（污染行持续服务）与该实现逐点吻合。调查任务书中的「规范 schema2 + 服务时转换另有原因」分支**不成立**。

#### 1.2 根因链（文件:行号）

写入侧（冷取落库）：

1. `internal/adapter/docker/remote.go:1028`（direct remote 面）、`internal/adapter/docker/virtual.go:230`（virtual 面的 remote member walk）——`entry.fetchManifest(ctx, ref.image, reference, r.Header.Values("Accept"), …)`：客户端 Accept 逐字转发上游。`fetchManifest` 本体在 `remote.go:205`，其注释（remote.go:195-196）明文自白设计意图："accept is the CLIENT's Accept set, forwarded verbatim … **BinFlow never converts**"。
2. `internal/adapter/docker/remote.go:1168`——`mediaType := mediaTypeOfHeader(fetched.header.Get("Content-Type"))`：落库媒体类型取上游协商结果（schema1 请求时即 prettyjws）。
3. `internal/adapter/docker/remote.go:1179`——`sink.land(manifestNodePath(ref.image, dgst), dgst, mediaType, bytes.NewReader(fetched.body))`：协商表示的**字节**以协商 mime 落入 digest 寻址的缓存节点。落库语义在 `internal/repo/dockerremote.go:241`（`LandRemoteBlob` → `landRemoteV2Core`）：blob-first content node + `RemoteCacheKindContent` 状态行——repo 层对「mime 是什么表示」无感知，全凭 adapter 传入。
4. `internal/adapter/docker/remote.go:1205`——`sink.record(dgst, tag, mediaType, …)`：tag 行与 manifest index 行同样记录协商媒体类型（`internal/repo/dockerremote.go:364` `RecordRemoteManifest`）。此后 `resolveRemoteRef`（remote.go:1601）按 tag 行解析出的就是污染的 mediaType。

服务侧（命中路径零协商）：

5. `remote.go:993-995`——`RemoteProbeHit` 直接进 `serveRemoteManifestCopy`；`remote.go:1255` 起——`io.Copy` 缓存原字节、`Content-Type` 设存储 mime（remote.go:1297），全程无 Accept 分支。STALE（remote.go:1073/1089/1098）、304-revalidated（remote.go:1057）臂同构——这正是活体「相位无关」的出处。

对照面：local/virtual 本地面不受影响——schema1 push 在 `internal/adapter/docker/manifest.go:552-553` 被拒（M2 不支持 schema1），本地存储恒 schema2。C-b 分歧面精确限定在 remote pull-through 及 virtual 的 remote member。

#### 1.3 规格依据与偏差结构

`docs/reverse/docker-registry.md` §4「manifest GET/HEAD 的 Accept 协商」（**置信度：高**）记载 A 的算法：存储优先 schema2（规范形）；「存储为 schema2、Accept 不含 schema2（老 docker 客户端）→ 服务端把 manifest 转换为 schema1 signed 再返回（`DockerSchemaProvider#convertSchema2To1`）；转换失败则原样返回 schema2」。同节 BinFlow 校准行明确「**M2 不做 schema2→1 转换**（2026 年无老客户端）」。

C-b 的本质：该历史规格裁剪砍掉的是「服务时转换」，而 remote pull-through 的实现组合（转发客户端 Accept + 协商结果入库）产生了**超出演裁剪意图**的副作用——不只是「B 不能像 A 那样转换服务 schema1」，而是「schema1 客户端的一次冷取把缓存行永久带偏，伤及后续 schema2 客户端」。前者是裁剪范围内的能力缺位；后者是缓存正确性缺陷（活体定谳 C-b=TRUE）。

#### 1.4 修复难度与方案概要

**第一步：缓存规范化（难度：低-中）——建议立票**

- 改动点：`remote.go:1028` 与 `virtual.go:230` 两处 `fetchManifest` 的 Accept 参数，从 `r.Header.Values("Accept")` 换为固定规范集（schema2 / manifest list / OCI index 优先）；`fetchManifest` 本体与 repo 层不动（mime 参数语义不变，只是恒收到规范表示）。
- 效果：缓存行恒持上游规范形——A 的「缓存透明性」对 schema2/无 Accept 客户端即恢复（活体 D2/D3 场景消失）。
- 影响面：(a) `acceptAllows` 拒绝臂（remote.go:1172-1176）——schema1-only 老客户端冷取会从 200（拿 prettyjws）变 404（存储形不在 Accept）；这是相对 A 的残余差异，需登记 known-divergence（或由第二步闭合）。(b) tag 行 digest 从 prettyjws digest 变 schema2 digest，`Docker-Content-Digest`/ETag 随之——下游 blob 引用（config/layer digest）与 manifest digest 无关，无连带；TTL revalidation 与负缓存键不涉表示语义，无需动。(c) 契约面：docker-remote.yaml 需补 manifest 冷取表示的 expect 条目（当前缺失）。
- 测试面：新增「冷取（带 schema1 Accept）→ 缓存行 → schema2 客户端命中拿 schema2」table-driven 用例（remote 面与 virtual member 面各一），既有 `manifest_cold_miss_negative_cache_test.go` 等回归。

**第二步：服务时转换（难度：高）——独立评估票**

- 内容：命中路径按 Accept 在线转换（schema2→prettyjws），对齐 A 的「缓存永持规范形 + 逐请求协商」全语义。
- 难点：(a) 需新序列化器（JWS envelope 构造）；(b) A 转换输出的**字节形态无 reverse 规格**（签名有无、字段序、createdOn 等）——低置信度区，实施前须差分取证，clean-room 铁律禁止猜测补齐；(c) 推翻「M2 不做 schema2→1」规格裁剪属产品裁决；(d) `manifest.go:535/552-553` 的 schema1 拒收姿势需为出站转换路径局部豁免。

### 二、C-a 匿名内容面放行——根因与对齐方案

#### 2.1 结论

BinFlow **有完整的匿名访问配置**（`security.anonymous_access`，env `BINFLOW_SECURITY_ANONYMOUS_ACCESS`），匿名内容读（manifest/catalog/tags 的 GET/HEAD）由该全局开关控制，**默认开**——这是 ADR-0009 的用户定案产品姿态，不是缺失认证代码。A 侧 401 = 参照实例把匿名访问关闭（实例配置），非 A 产品语义恒拒（Artifactory 匿名开启时匿名 docker pull 是官方支持场景，其出厂默认匿名开）。wave-3 深挖报告 §3 的判定一致：「部署面 allowAnonymous 配置差异与代码语义在活体上不可分——环境面记录，不作代码断言」。

#### 2.2 根因定位（文件:行号）

1. 默认值：`internal/config/config.go:18-19`——`DefaultAnonymousAccess = true`（注释：mirrors ADR-0009）。加载链 `internal/config/load.go:649/720`；配置键映射 `load.go:94`（yaml `anonymous_access`）与 `config.go:188`（env 布尔）。ADR-0009 全文见 `DECISIONS.md:124`（2026-08-17 用户定案：方案 A 匿名读默认开，仅内容 GET/HEAD；写与 `/binflow/api/**` 恒认证；配置可关）。
2. manifest/tags 名字路由门：`internal/adapter/docker/handler.go:283`——`if !h.opts.AnonymousAccess && principalOf(r) == nil { h.challenge(...) }`：开关开时匿名直通 repo 行查询与授权判定。
3. catalog 门：`internal/adapter/docker/catalog.go:70`——`if p == nil && !h.opts.AnonymousAccess { h.challenge(scopeRegistryCatalog) }`。
4. 授权兜底：`handler.go:379` 起 `authorizeRoute`——匿名 principal 经 `authz.Can`（`Authorizer.Can` 内部拥有 `Security.AnonymousAccess` 决策，`internal/httpapi/middleware.go:467-468` 注释在案）；fail-closed（门与授权两层，无认证旁路）。

#### 2.3 对齐方案（两案并陈，裁决归 conductor/产品）

- **案 1：维持 B 姿态（dev-go-core 建议）**——零产品码。动作：(a) wave-3 docker content 面匿名 200 证据并入 `known-divergence.yaml` `rest/anonymous-read-default-policy`（R-22b 席位，pending）evidence 字段；(b) 差分侧补一轮 B 翻 `BINFLOW_SECURITY_ANONYMOUS_ACCESS=false` 的同配置对照——把「环境配置差」从「代码语义差」中剥离，若同配置下双端行为一致则 C-a 灭（预期如此：三层门代码语义清晰）。理由：ADR-0009 在册背书 + A 出厂默认（匿名开）大概率与 B 同向；当前差分分歧是实例配置差，翻产品默认值属「用产品代码迁就差分环境配置」，方向存疑。
- **案 2：对齐 deny 默认**——`config.go:19` 一行翻转（难度低）+ ADR-0009 升级 Superseded/Errata + 部署矩阵全量回归（compose/k8s/systemd 默认形态匿名 pull 断，「15 分钟跑通」迁移体验回退）+ 受影响契约 expect 重录。**产品影响面大**，且存在 §Risks-3 的方向性风险，不建议在 A 出厂默认复验前执行。
- 同族既有席位（本票不重复立分类）：`docker/remote-anonymous-token-policy`（C03，LOOP 002 限期升级）——/v2/token 匿名签发面，与 C-a 同根（同一开关），R-22b 裁决时应联动定该面。

### 三、证据与置信度标注

- reverse 规格：`docs/reverse/docker-registry.md` §4（协商算法与 convertSchema2To1）——置信度**高**（规格自标）；本报告引用其判定 A 侧行为。
- 活体证据：`reports/compatibility/L043-candidates-deep-dive.md` §2/§3（wave-3，B=dev-cfa5da05）——C-b=TRUE 定谳矩阵（H1-H4/D1-D3/E2-E3 12 臂）、C-a 匿名矩阵（F1-F3）。本报告未复跑探针（证据归 differential-qa-engineer）。
- 契约：`docs/compatibility/contracts/docker-remote.yaml` 无 manifest 冷取表示条目（缺失）——按 §4 规则，实现对照以 reverse 规格置信度为准，修复票应补契约条目。
- 代码：本文所有 文件:行号 以 worktree feat/t634-count（HEAD 635b9ee8 附近工作树）为准，均为非测试码。
