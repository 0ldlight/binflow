# T-33 评审报告（视角: correctness）

结论: REQUEST_CHANGES

范围：internal/adapter/docker/（6 文件）+ internal/httpapi/（router/system/routes_test/harness_test + 新 docker_v2_test.go）+ cmd/binflow-server/main.go（commit e15e87a）。
对照契约：DECISIONS.md ADR-0010、architecture §5.3/§7、docs/reverse/docker-registry.md §0/§1。

## 取证记录（全部实际执行）

- `go test -race -count=1 ./internal/adapter/docker/ ./internal/httpapi/` — ok（1.761s / 17.235s）。
- `go vet`（三包）通过；`gofmt -l` 空。
- 真栈裸进程双场景（匿名开 127.0.0.1:18193 / 匿名关+base_url 127.0.0.1:18194）：
  - raw-TCP + `curl --path-as-is` 混合编码探针：`/v2/%2e%2e/`、`/v2/a/..%2f`、`/v2/%2f/etc`、`/v2/team1/%2e%2e/...`、`/v2/team1//app/...`、`/v2/x/..` 全部 **400 直答**；`/v2//x`、`/v2/../etc/passwd`、`/v2/./x` **404 直答**；**零 3xx**（T-14 红线成立），响应全带 spec 体 + api-version 头；数据目录零写入。
  - `/binflow/v2/**` 维持 E-01 信封 404 且不带 Docker-Distribution-Api-Version 头；`/binflow/api/system/ping` 等 generic 面不受 /v2 分支影响（互不干扰成立：`/v2` 判定在 `/binflow` 分支之前，`/binflow` 前缀测试不会吞掉 `/v2`，反之 `/v2` 前缀测试也不吞 `/binflow`——两者前缀互斥）。
  - 匿名关：ping 401 + `Bearer realm="https://reg.example.com/v2/token",service="binflow"`（base_url 优先实测成立）；name 路由 401 挑战先于 repo 门。
  - 单段 name、`/v2/_catalog`、token 缝 404 spec 体。
  - **缺陷取证**：匿名关实例上 `-u admin:wrongpass` 或 `Authorization: Bearer staletoken` 打 `/v2/` → 401 + `WWW-Authenticate: Basic realm="BinFlow Realm"` + **E-01 信封**（`{"errors":[{"status":401,...}]}`），见 blocking B1。

## 必须修改（blocking）

### B1. /v2 上「带凭据但校验失败」泄漏 E-01 信封 + Basic 挑战，破坏 spec 体协议与 docker 客户端握手

- 位置：`/Users/lzw/dev-center/internal/httpapi/middleware.go:292-297`（`authenticate` 的 `err != nil` 分支）+ `/Users/lzw/dev-center/internal/httpapi/router.go:45-47`（/v2 进入同一 authenticate 且无法定制其失败渲染）。
- 实测（匿名关实例，真栈）：
  ```
  GET /v2/  Authorization: Bearer staletoken
  → 401, WWW-Authenticate: Basic realm="BinFlow Realm"
    {"errors":[{"status":401,"message":"invalid credentials"}]}
  ```
- 为什么 blocking：
  1. 直接违反本票 AC②/DE-17/NFR-S10 与实现者日志第 45 行的自断言「/v2 域内**绝不**出现 E-01 信封」——`TestV2ErrorEnvelopeIsolation` 只测了匿名无凭据与 method 405 两条路径，漏掉了「有凭据、验失败」这条中间件层路径，断言面不完整。
  2. docker 认证流必然走到这里：token 过期/撤销后，客户端带过期 Bearer 重打 `/v2/`，收到的是 `Basic realm="BinFlow Realm"` 挑战——docker 客户端在 Bearer 协商中收到 Basic 挑战会按 Basic 重试或直接失败，`docker login`/后续 pull 的续期路径（T-37 依赖的握手基座）被破坏。
  3. `anonymous_access: true`（默认）实例上更糟：匿名 name 路由本会被 adapter challenge（若走那条路），但凭据失败在中间件层就拦截——同样输出 Basic 挑战。凭据失败不应降级为匿名是正确原则，但渲染必须按域分流。
- 建议改法（任选其一，倾向 a）：
  - a) `authenticate` 失败分支按路径分流渲染：`/v2` 前缀（或更通用：由 router 提供一个 `authFailureRenderer` 缝）→ 调 docker 域的 spec 体 + `Bearer realm=.../v2/token,service="binflow"`（challenge 逻辑已在 adapter，可把 `challenge()`/`writeSpecError` 提为 docker 包导出小函数或经 adapters["docker"].(interface{ RenderAuthFailure(...) }) 调用）；其余路径维持现状。
  - b) authenticate 失败时不硬 401，而是把「凭据被拒」记入 context、principal 置 nil 继续进 dispatch，由各域的门决定（/v2 → adapter challenge；/binflow → authorize 的 401）。此改动波及 generic/api 面语义（凭据失败 vs 匿名需可区分），面更大，若走此路需补 `TestAuthenticateRejectNoDowngrade` 系列回归。
  - 无论哪种：补 table-driven 用例「/v2 + 错 Basic / 过期 Bearer → 401 spec 体 UNAUTHORIZED + Bearer realm 挑战，无 status 字段」进 docker_v2_test.go。
- 注：此缝在 ADR-0010 第 1 条「进同一 middleware 链」的执行层定义里未被 T-33 的测试覆盖矩阵枚举——修复时建议在 T-37（token 流）ticket 里同步登记「凭据失败渲染分流」的契约测试。

### B2. `parseV2Name` 的 dot-segment 防线不覆盖 repoKey 段——`/v2/../binflow` 类路径把 `..` 当 repoKey 放行到 repo 行查询

- 位置：`/Users/lzw/dev-center/internal/adapter/docker/name.go:68`（防线只跑 `strings.Join(segments[1:], "/")`，即 image 部分）。
- 实测推演（与真栈行为一致，`/v2/x/..` 的对照证明了防线确实只看余段）：
  - `/v2/..` → segments=[".."]，len<2 → 404（shape）✓
  - `/v2/../etc/passwd` → segments=["..","etc","passwd"]，**防线跳过 repoKey=".."**，余段 "etc/passwd" 合法 → tailIdx<1 → 404（shape）——路径里的 `..` 没被 400 审判。
  - `/v2/../binflow` 同理，`..` 作为 repoKey 进入 `h.repos.Get("..")` 的查询路径（被 NAME_UNKNOWN 404 兜住，**今天**无文件系统后果——adapter 不拼路径；但这是防线不对称，明天 T-38/T-39 落盘时 image/tail 已有防线、repoKey 侧靠巧合）。
- 为什么 blocking：实现者自报修复了「dot-segment 防线先于路由尾形状判定」的顺序 bug，但修复不完整——同一防线对 name 的**第一段**根本不生效。generic 层的 `splitRepoPath`（layout.go:63-70）对 key 段有独立的空/长度/保留字检查后再 `validateRelPath(rest)`，docker 侧却把 key 段的路径安全检查整体遗漏。NFR-S11 的语义是「dot-segment 请求必须 400」，现在 `/v2/../x/y` 是 404。
- 建议改法：`parseV2Name` 在 dot-segment 防线处先对**全段**（或至少 segments[0]）跑一次检查，例如在 line 68 之前加：
  ```go
  if segments[0] == "." || segments[0] == ".." {
      return nameRef{}, fmt.Errorf("%w: dot segment %q in repository key", adapter.ErrBadRequestPath, segments[0])
  }
  ```
  并补 table 用例：`/v2/../etc/passwd` → 400（当前 404）、`/v2/./x/manifests/latest` → 400、`/v2/x/../y/manifests/latest` → 400（这个已覆盖）。
- 附带核对：`adapter.NormalizeRelPath(image)`（name.go:99）与 line 68 的 `Join(segments[1:])` 对同一字符串跑了两遍——第二遍是死代码（同输入同结果），non-blocking 顺带清理。

### B3. `/v2/_catalog` 落进「未定义路由」404 而非 name 路由——首段 `_catalog` 被当作 repoKey 查库

- 位置：`/Users/lzw/dev-center/internal/adapter/docker/handler.go:117-141`（`serveNameRoute` 对 `_catalog` 无特判）+ name.go 解析。
- 实测：`/v2/_catalog` → 404 spec 体（真栈）。行为观察上 OK，但语义路径是：`parseV2Name("/v2/_catalog")` → 单段 → `notANameError`（404 shape）。**问题在两段以上时**：`/v2/_catalog/x`、`/v2/_catalog?n=10` 的未来实现（architecture §5.3 明确列了 `/v2/_catalog` GET 端点，M2 范围）会把 `_catalog` 当名字段处理，且当前 `_catalog` 作为「registry 级端点」与「name 首段=repoKey」的优先级从未被裁定——它不是保留字（adapter.IsReservedSegment 只有 api/v2），repo.Service 也不拒绝用户建名为 `_catalog` 的仓。
- 定性：这不是当前的错误响应（404 合理，catalog 属 T-3x 后续），但它是**与 B2 同类的「首段身份未审判」问题**：首段目前承担 repoKey、`token`（handler 有特判）、未来的 `_catalog` 三种身份，其中只有 `token` 有显式分支。`_catalog` 落到 repo 门后，用户建一个叫 `_catalog` 的 docker 仓就能劫持该端点的语义（spec 保留 `_` 前缀给 registry 级端点）。
- 建议改法（最小）：在 `serveNameRoute` 的 TokenPath 特判旁加 `_catalog` 的显式 404 占位分支（与 token 同款注释「T-3x 落地」），并在 repo.Service 的保留字校验侧（T-35 area，登记给 conductor 转交）把 `_` 前缀首段加入建仓禁用名单——与 ADR-0010「repo key 保留字 v2 维持」同源的补充。若裁定为「不修，等 catalog ticket」，请在本票日志把这个裁定写明（当前日志未提 `_catalog`）。

## 建议改进（non-blocking）

1. `handler.go:174-176`：`sessionRegistry` 是零字段的空壳类型，`newSessionRegistry()` 每次构造分配。R6 登记可以，但空 struct + 工厂函数在没有行为前是纯占位——建议留注释指明 T-38 将持有的字段（UUID→Session/received），否则下次读代码的人无法确认它是遗漏还是预留（注释在 T-33.md 有、代码里没有）。
2. `repolookup.go:26`：`l.store.Get(context.Background(), key)`——丢掉了请求的 ctx（调用方 `serveNameRoute` 有 `r` 可传）。repo 行查询挂死时取消不传播，且 trace/request_id 断链。建议 `RepoLookup.Get` 接口加 ctx（T-35 演 metadata 缝时顺路）。
3. `handler.go:157-158`：NAME_UNKNOWN 的 message 回显 `%q(repoKey)`——把用户输入（percent-decode 后的任意字节，包括控制字符？不会——ParseV2Name 已 decode，`NormalizeRelPath` 只跑 image 段，repoKey 段没有控制字符检查）嵌进 JSON body。JSON 编码器会转义控制字符，注入面不存在，但 repoKey 段可含 `%zz` 之外的任意 decoded 字节（如换行）——建议对 repoKey 也跑一遍 `isControlByte` 类检查（与 B2 同一处修）。
4. `errors.go:50-52`：`_ = enc.Encode(...)` 忽略编码错误——与 httpapi.writeError 同款既有风格，一致即可，不改。
5. `docker_v2_test.go:296` 起的 encoded-slash 用例注释很好；但 `TestV2PingAnonymousClosed` 用 `h.srv.URL` 断言 realm，依赖 harness 的 URL 拼接——若 harness 未来改 listener 形态会假失败，可改为只断言后缀 `/v2/token` 与 service 值（低优先）。
6. routes_test.go 的 E-26 矩阵移除 `/v2` 两项的注释（R10 来源标注）完整规范——好。

## 对重点检查项的逐项结论

1. **路由例外互不干扰**：成立（见取证）。EscapedPath 传递正确：`withRootPrincipal` 不碰 URL，raw spelling 原样进 adapter；真栈混合编码探针全 400/404 直答、零重定向。`/binflow/v2` 404 信封 + 无 api-version 头回归有测试有实证。**但中间件层的 authenticate 失败渲染漏网（B1）。**
2. **name 解析**：切分边界（嵌套名 acme/team/app、贪心尾定位、单段 404）测试与实测均正确；自报修复的顺序 bug（防线先于形状）在**余段**上成立且有测试，**但 repoKey 段不在防线内（B2）**——正是「还有没有类似顺序耦合」问题的答案：有，第一段。
3. **错误信封隔离**：adapter 自渲染的所有路径（ping 405/name 404/400/challenge/token 404/无 adapter 404）全部 spec 体，实测一致；`TestV2ErrorEnvelopeIsolation` 的判别式（无 "status" 字段）合理。**唯一漏网是 B1 的中间件路径。**
4. **中间件缝**：principal 经 `adapter.WithPrincipal` 正确传递（T-33 风格与 dispatchContent 的 withStrippedPrefix 一致）；realm base 来源（base_url 优先、X-Forwarded-* 、原生 Host、缺省 http）实测与代码一致。**认证后 /v2 的授权路径**：M2 基座无 scope/授权判定（ADR-0010 第 5 条归 T-37），当前已认证即过 repo 门——与基座范围相符，无缺陷。**B1 是本项的实缺陷。**
5. **RepoTypes 空切片裁定**：裁定安全。实证路径：`adapters["docker"]` 唯一消费点是 dispatch 的 /v2 分支与 probeRegistry；docker 声明 "local" 确会覆盖 generic（map 后写胜出，server.go:91-97），实现者判断正确。`adapter.Register()` 空 RepoTypes panic 的行为差异已在两侧注释写明，契约归属清晰。**adapters map 并发注册**：map 在 `New()` 构造期一次性填充、之后只读，无并发写，无竞争（-race 通过）。
6. **health registry 字段**：只增不破成立——healthResponse 新增 Registry 字段，storage/metadata 字段与既有测试零变化，TestV2HealthRegistryField 同时断言新字段与旧字段不降级。probeRegistry 只读 routing 数据、不引进新依赖，聚合逻辑（任一子系统 error → 顶层 error）正确。

## clean-room 抽查

对 docker 包 6 文件与 reverse-src 做结构比对：代码组织按 BinFlow 既有分层（adapter SPI + errors + handler + name + lookup），注释引用的是官方 [DIST-API]/[OCI] 规范与 docs/reverse/docker-registry.md 的行为规格条目（§0 api-version 头、§1 错误体形态），**未发现**类名/方法名/字面量与 `o.j.repomd.docker.v2.rest.*` 反编译代码的逐行对应；Artifactory 特有偏离（`<uuid>.patch`、`.jfrog` 索引文件、SIZE_INVALID 等非官方码）均未照抄，且 §10 的「按官方实现」校准建议被采纳（UUID 纯 UUID、错误体官方码）。通过。

## 范围外发现（交 conductor）

- `internal/repo` 的在途修改（T-35，工作区未提交）致 `go test ./...` 该包失败，与本票无关（实现者已 stash 验证，本次未复核 stash 实验，但失败测试文件均为 T-35 area）。
- B3 的保留字扩展（`_` 前缀）落在 repo.Service 校验，属 T-35 area，需转交。
- `adapter.Register()` 与 Deps.Adapters 注入两条装配路径的行为分叉（panic vs 静默空键）是架构债，architect review 视角应再看。
