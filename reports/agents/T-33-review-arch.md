# 评审报告 T-33（视角: consistency / 架构一致性）

- reviewer: code-reviewer (arch)
- 日期: 2026-08-18
- 输入: reports/agents/T-33.md、commit e15e87a（14 文件，+1297/-8）、ADR-0010、architecture §5.1/§5.3/§7（T-30 定稿版）、T-32 票据 AC、docs/reverse/docker-registry.md
- 结论: **REQUEST_CHANGES**（blocking 2，non-blocking 5，范围外 2）

取证命令与结果：

- `go test ./internal/httpapi/ ./internal/adapter/docker/ -count=1` → ok（15.2s / 0.37s）
- `go vet ./internal/adapter/docker/ ./internal/httpapi/ ./cmd/binflow-server/` → 零输出
- 真栈裸进程 curl 探针（127.0.0.1:18123，默认配置）：
  - `GET /v2/` → 200 `{}` + `Docker-Distribution-Api-Version: registry/2.0` + X-Request-Id（通过）
  - `GET /v2/ -u baduser:badpass` → **401 + /binflow E-01 信封（`{"errors":[{"status":401,...}]}`）+ `WWW-Authenticate: Basic realm="BinFlow Realm"`，无 api-version 头**（B1 实证，见下）

---

## 一、ADR-0010 逐条对照

| 条款 | 裁定 | 证据 |
|---|---|---|
| 1. 根级例外、不剥前缀、进同一 middleware 链 | **实质符合，但链的失败分支破 /v2 域契约 → B1** | router.go:97-106 在 `/binflow` 分支之前判定；`withRootPrincipal` 原样传递（不改 URL）；dispatch 位于 baseChain+authenticate 之内（router.go:45-47）。requestID/accessLog/recover/CORS 全覆盖（TestV2MiddlewareChainShared 断言 X-Request-Id 与 access log） |
| 2. `/binflow/v2/**` 不提供 | 符合 | router.go:126-131 维持 E-26 信封 404；TestV2RootNotUnderBinflow 额外断言该响应**不带** api-version 头（域隔离可观测证据，好设计） |
| 3. name 首段=repoKey、单段 404、repo 门 NAME_UNKNOWN | 符合 | name.go:47-104（首段 repoKey、贪心尾段定位且留至少一个 image 段、`/v2/manifests/latest` 单段→404 与 ADR 单段裁定一致）；handler.go:152-159 三因同 404 NAME_UNKNOWN（不可区分性）。安全检查先于形状分析（name.go:64-70）顺序正确 |
| 4. token realm 预留（T-37 缝） | 符合 | TokenPath 常量 + challenge() 头形 `realm="<base>/v2/token",service="binflow"`（handler.go:74-83）；`/v2/token` 现 spec 体 404（handler.go:119-125）；base_url 优先 + X-Forwarded 推导（TestV2PingBaseURLOverride） |
| 5. scope→Can 映射 | T-37 范围，缝已备（challenge 的 scope 参数） | — |
| 6. 反代不禁止不必需 | 无代码面，无需落地 | — |

ADDITIONAL：repo key 保留字 `v2` 维持（repo/api.go:102-105），且 `/v2` 根例外先于 name 解析消费，`/v2/v2/...` 落 NAME_UNKNOWN，无路由混淆。

## 二、必须修改（blocking）

### B1. 共享 authenticate 的拒绝分支把 /binflow 信封泄漏上 /v2 平面（AC② 违约，实测已证）

- 位置：`internal/httpapi/middleware.go:289-306`（authenticate 硬拒：binflow 信封 401 + `Basic realm="BinFlow Realm"`）；`internal/httpapi/router.go:45-47`（/v2 与 /binflow 共用该链）。
- 问题：ADR-0010 第 1 条要求 /v2 进同一 middleware 链——实现做到了，但该链的**失败渲染**是 /binflow 风格的。凡「呈交但被拒」的凭证（过期 Bearer、错误 Basic）打到任何 /v2 路径，authenticate 在到达 docker handler 之前就以 E-01 信封 + Basic 挑战 401 收场：无 spec 体、无 api-version 头、Bearer 挑战缺失。直接违反本票 AC②「`Docker-Distribution-Api-Version: registry/2.0` **全 /v2 响应**」与 NFR-S10「/v2 域绝不出现 E-01 信封」（实现者日志自己的措辞）。TestV2ErrorEnvelopeIsolation 只打了 handler 渲染的 POST 405 分支，未覆盖此路径——这正是它漏网的原因。真实客户端影响：匿名开实例上带过期 token 的 `docker pull` 得到 Basic 挑战而非匿名放行或 spec 401；T-37 的 `/v2/token` 对错误口令的响应也会先死在这条分支上。
- 同族问题：`recoverPanic` 的 500 也是 /binflow 信封（/v2 平面 panic 同样破约），修法应一并覆盖。
- 建议改法：authenticate 不再自渲染拒绝——把「凭证已呈交且被拒」作为 context 信号下传，由路由平面定形：/binflow 各 route gate 渲染现行信封 + Basic 挑战（行为不变），/v2 分支把该信号交给 docker handler 的 `challenge()` 渲染 spec 体 + Bearer（语义正确：无效凭证=未认证）。recoverPanic 同理按平面选信封。若 conductor 裁定改法归 T-37，须先在本票登记为 T-37 的**前置阻塞项**并在 AC 补「无效凭证于 /v2 的渲染」用例；但该违约今天就可达、且是本票 AC② 的字面未满足，我的裁定是本票修。

### B2. repo 门把真实查询错误吞成 NAME_UNKNOWN，且丢 request context

- 位置：`internal/adapter/docker/handler.go:152-159`（`err != nil || row == nil || ...` 三因同 404，无日志）；`internal/adapter/docker/repolookup.go:25-37`（`Get` 用 `context.Background()`；not-found→(nil,nil) 后其余错误原样上抛却被调用端与 not-found 合并）。
- 问题：SQLite busy/locked 等瞬时故障会让全部 docker 仓表现为 NAME_UNKNOWN 404，**零日志**——DB 中断在 /v2 平面不可观测，docker 客户端会当作镜像缺失触发重推循环。对照 httpapi 同缝的 `writeRepoLookupError`（router.go:391-399：not-found→404、其余→500+ERROR 日志），docker 门丢掉了这一区分。`context.Background()` 还切断取消传播（CLAUDE.md Go 规范：显式传递 context；httpapi 的孪生缝传 `r.Context()`）。
- 建议改法：seam 保持 not-found→(nil,nil)，但真实错误上抛并在 handler 记 ERROR 日志后以 spec 体 500 回答（indistinguishability 安全面不受影响——DB 故障不泄漏仓存在性）；`RepoLookup.Get(ctx, key)` 加 ctx，`serveNameRoute` 传 `r.Context()`。

## 三、RepoTypes 空 class 键——架构裁定（conductor 点名项）

**裁定：合理过渡与架构缺口并存——本票放行该手法，缺口须以 §5.1 勘误关闭，且必须在 M3（maven/npm）之前落地。**

理由：

1. **当下是对的**。若 docker 声明 `{"local"}`，httpapi 扁平 map（server.go:91-97，protocol 键与 class 键同命名空间）里 `adapters["local"]` 会被后挂的 docker 覆盖 generic 的内容分发——立刻破坏 M1 唯一 local 服务者。空切片是今天唯一不破系统的选择，且注释（handler.go:14-23）、map 契约注释（server.go:82-90）、测试（TestRepoTypesProtocolKeyOnly）三处钉住，债务双登记。作为紧急规避：合格。
2. **但它是缺口，不是终态**：(a) 与 §5.1 草图注释「docker M2 为 {local,remote,virtual}」直接矛盾，SPI 声明开始说谎（docker 明明服务 local-class docker 仓）；(b) 手法**不可重复**——M3 maven/npm 同样服务 local class，不能也都返回 `{}`，届时 `RepoTypes` 语义归零；(c) 根因是 httpapi map 的 **class 键是零读取的死键**：全仓 grep 证实除写入外无任何按 class 的查找（读取只有 `adapters["docker"]` 与 `adapters[row.PackageType]`），死键唯一的作用就是制造碰撞。
3. **Register 空 RepoTypes panic 与 Deps 注入路径的行为差异**：裁定为**保留 panic**（空声明=装配 bug，应启动期暴露，方向正确）；不对称因 `adapter.Register/All/ForRepoType` 目前**零生产调用方**（装配全走 Deps.Adapters 注入）而暂时无害，§5.1 补丁落地后 docker 声明 `{"local"}` 即可正常 Register，不对称自然消失。生产装配维持 Deps 注入（§2 规则 3：无包级单例），Register 系的存废另记范围外。

**给 architect 的 §5.1 勘误措辞（建议原文落 ADR/architecture 勘误栏）**：

> 1. `RepoTypes()` 语义收窄为**声明性元数据**（本协议可服务的仓库 class，供校验/文档/未来 class 维度能力使用），**不得作为任何分发 map 的键**。docker M2 声明 `{"local"}`（原草图「docker M2 为 {local,remote,virtual}」随 remote/virtual docker 顺延 M3 一并勘误）。
> 2. httpapi adapters map 的键**仅** package type（≡ `Protocol()`）。现存的 class 键（"local"/"remote"/"virtual"）为零读取死键且是唯一碰撞源，删除（`New()` 不再写 `adapters[t]`）。分发语义不变：`dispatchContent` 本就只按 `row.PackageType` 查表。
> 3. `adapter.Register()` 的 `byType` 同步改为仅 package type 键；「重复 class 即 panic」条款删除（generic 与 docker 同时声明 class=local 是合法状态——class 不是分发键）；**空 RepoTypes panic 保留**。
> 4. `Layout` 错误契约两态化：客户端语法错误（dot-segment/坏编码/超长）wrap `ErrBadRequestPath`（400）；不可寻址形状（单段 name/无路由尾）允许返回 404 形错误。docker 现行为即为标准形态（见 N2）。

落点建议：随 T-35（repo docker 启用）或一张小 architect 票；不阻塞本票。

## 四、分层红线（§5.1「adapter 禁止 import storage/metadata」）

- docker 包 import 面：`adapter`/`auth`/`repo`（类型）+ `metadata`（仅 repolookup.go 的 `RepoStore.Get`，读 `PackageType` 一项路由数据）。与 M1 定案的先例一致（generic 已注入 `md.Blobs()`；T-14 终判 + ADR-0009 回写：「RepoLookup 用 metadata.Get 是有意为之的匿名读前置缝」）。**越线裁定：不越**——业务语义仍全走 `repo.Service`（PutManifest 等已备），metadata 触点限定在路由数据。B2 修掉 ctx/吞错后此缝干净。
- 授权门序（T-27/T-14 回写「路由解析位于授权门后」）：/v2 不经 httpapi `authorize()` 是 ADR-0010 第 4 条 + 信封隔离的必然（binflow 信封 401 会破 spec 协议）；等价防线成立——匿名关时 `serveNameRoute` 在 repo 门**之前** challenge（handler.go:143-150），匿名开时 repo 门只暴露路由数据，与 /binflow 内容面匿名读缝同构。一致性：通过。
- `repolookup.go` 的定位：docker 自持窄缝是**正确的依赖方向**（adapter 不得 import httpapi，无法复用其 RepoLookup）；形状上 docker 的接口形（返回窄接口 `RepoRow`）比 httpapi 的（返回 `*metadata.Repo`）更符合消费端最小面。双缝并存是分层的必然成本，可接受；修 B2 时补 ctx 即与 httpapi 缝对齐。
- `name.go` 复用 `adapter.NormalizeRelPath`——正是 layout.go:117-120 为 M2 预留的出口，SPI 公民行为良好。

## 五、R10 测试反转与覆盖

- routes_test.go：diff 恰为删 2 行（`/v2/`、`/v2/_catalog`）+ 9 行注释注明「PRD §8.1 baseline item 1 / T-32 risk R10」并指明断言所有权转移；`/binflow/v2` 两行保留（第 2 条不做双挂载）。**最小面：是；来源注明：是。**其余矩阵零变动，整套 httpapi 测试通过（15.2s）。
- docker_v2_test.go 对 AC/PRD 对应：D04（匿名开/关、头形、base_url 覆盖）、D24/DE-15/16（未定义路径、referrers）、DE-17/NFR-S10（信封隔离、detail:null、405）、NFR-S11（4 变体 + raw-TCP 无 3xx）、ADR-0010 第 2 条（无 api-version 头断言）、§6.3（health registry 字段 + 旧字段不降级）、middleware 链共享。**缺口一处**：无「呈交但被拒凭证」用例——即 B1 的漏网（见 N4）。

## 六、装配面（T-16 链一致性）

- `cmd/binflow-server/main.go:209-229`：`docker.New(svc, NewRepoLookup(md.Repos()), Options{...})` 纯 DI，无 `adapter.Register`、无包级单例；serve 与测试共用 `newAssembledServer`（tested=served 不变量保持）。Adapters 顺序 `[generic, docker]` 且 server.go 注释明确顺序无关的理由。harness 同法（harness_test.go:85-88,114）。**无全局单例回归：通过。**
- health `probeRegistry` 只答路由数据（mounted 与否），不做活性回环——与「dashboard 非 probe 目标」的 §7 语义一致。

## 七、clean-room 抽查

- realm/service 形态取 ADR-0010（`<base>/v2/token` + `service="binflow"`），**刻意不同于** Artifactory 的 `<servlet>/api/docker/<repoKey>/v2/token` + `service=<host>`（docker-registry.md §5.1）——非照抄。
- `routeTails`、贪心 name 解析、错误码表均引官方 spec（[DIST-API]），注释明言以官方文档为准（ADR-0001 合规）。结构与 reverse-src 的 per-endpoint Java handler 族无对应关系（Go 单 dispatcher）。**逐行对应嫌疑：无。**

## 八、建议改进（non-blocking）

- **N1**（=第三节）：RepoTypes 空 class 键——放行本票，§5.1 勘误须在 M3 前落地；措辞见上。
- **N2** `internal/adapter/api.go:20-25`：`Layout` 文档「Parse failures return an error wrapping ErrBadRequestPath」与 docker 的 404 形 `notANameError` 不符——按第三节补丁 4 两态化措辞修文档，不改代码。
- **N3** `/binflow/<docker-repo>/**` 内容路径姿态未定义：T-35 启用 docker 仓后，该拼写会分发到 docker handler 并在 **/binflow 平面**渲染 spec 体 404（`serveNameRoute` 的 unknown-route 分支），违反 §7.3「/binflow 全非 2xx = E-01 信封」。建议 conductor 交 T-35/T-43 裁定（个人倾向：内容路径对 docker 仓回 E-01 信封 404，协议面只在 /v2）。
- **N4** `router.go:141-147` `writeV2Unavailable` 分支无测试覆盖（生产装配恒挂 docker，仅测试装配可达该分支）；补一个不挂 docker 的 server 用例。
- **N5** `handler.go:53-58`：405 无 `Allow` 头（RFC 9110 MUST）；T-37/T-38 顺手补 `Allow: GET, HEAD`。

## 九、范围外发现（交 conductor）

- architecture §5.1「注册机制：各协议包 init() 调 Register；httpapi 启动时 Mount 全部」与 DI 装配现实（Deps.Adapters 注入，M1 起即如此）不同步——M1 遗留文本债，建议随第三节勘误一并改写。
- `adapter.Register/All/ForRepoType` 零生产调用方（`ForRepoType` 连测试调用方也无）——建议 architect 裁定：赋予真实调用方或标记 M3 前删除。

## 十、结论

ADR-0010 六条中五条干净落地，R10 反转最小面，分层与装配无可指摘，clean-room 无嫌疑。但 AC② 的「全 /v2 响应」被共享 authenticate 的拒绝分支实测证伪（B1），repo 门吞错（B2）是错误处理链与可观测性的实质缺陷——两者修复面都很小（各约 10-20 行 + 用例），修完即可 Approve。RepoTypes 裁定：合理过渡，§5.1 勘误随附。
