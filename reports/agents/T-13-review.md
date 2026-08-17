# 评审报告 T-13（视角: correctness + security）

结论: **REQUEST_CHANGES**

- 评审对象：commit `0eac4a8`（T-13 定稿）。工作区现有 handler.go/curl_test.go 的改动属 T-20 在途工作，不在本票范围（见「范围外发现」）。
- 自验证命令（实际执行）：
  - `go vet ./internal/adapter/...` 通过；`gofmt -l` 空。
  - 干净 clone 于 `0eac4a8`：`go test -race -count=1 ./internal/adapter/...` 全绿（adapter 1.5s / generic 7.9s）。
  - 路径安全：用已提交代码起真实 HTTP 服务（127.0.0.1:18313，裸挂 Handler、无 ServeMux），以 `curl --path-as-is` + 原始 socket（nc）打 30+ 绕过变体；另以 `http.ReadRequest` 走 net/url 服务端解析路径验证 Layout。
  - 校验头矩阵 / 四动词 / ETag：同服务实测（结果见下）。

---

## 一、路径安全（最高优先）——结论：防线成立，无绕过窗口

**已验证拒绝（均 400）**：`%2e%2e`/`%2E%2E`（大小写）、`..%2f`、`%2e%2e%2f`、`.%2e`、`%2E.`、`%2e.`、裸 `..`/`.`（含尾段 `/.`、`/..`）、`..%5c`、`a%5cb`（反斜杠所有形态）、`/%61pi`（解码后判保留段）、`api%2Fv1`、`//`（空段）、`%2e` 单段、超 512。

**关键机制确认**：

1. **decode-then-validate 次序正确**（layout.go:29-37 → splitRepoPath → validateRelPath）。`EscapedPath()` 在 RawPath 与 Path 不一致时自动回退为「重新转义 Path」，故无论上游（T-14 / 测试 harness）改写的是 Path 还是剥了前缀的 RawPath，`PathUnescape` 都只发生**一次**——不存在双重解码窗口。
2. **`%252e%252e`（双重编码）→ 201 是正确行为**，不是绕过：单次解码后是字面文件名 `%2e%2e`，PUT 存储该字面名、GET 同一 URL 解码一致返回同一 node。Artifactory/net/http 同为单次解码语义。关键佐证：relPath **从不参与文件系统路径构造**——storage 侧 blob 落位是 `root/blobs/<sha256[:2]>/<sha256>`（engine.go:114-118，sha256 先过 `validSha256` 严格小写 hex 校验），上传临时目录是 uuid（engine.go:166-175），node 是 DB 主键。路径穿越没有可落地的目标。
3. **保留段判定在解码后**（layout.go:69）：`%61pi` → `api` → 400，实测确认。`API`（大写）不被拒但建仓 charset 为 `[a-z][a-z0-9-]{1,62}`，`API` 仓不可能存在 → 落到 repo 404，行为正确。
4. `..` 拒绝覆盖 `.` 单段与尾部 `/.`（`body = TrimSuffix(rel,"/")` 后逐段判 `""/"."/".."`）：`/acme/.`、`/acme/..`、`/acme/a/.` 实测全 400。
5. 前期 curl 探针中 `/acme/.` 等「201」是 **curl 自身 PUT 丢弃末段**（trace 显示实际发送 `PUT /generic-local/acme/hosts`），非服务端行为；原始 socket 复测为 400。实现者的 curl 测试用 `--path-as-is` 且断言 400，是有效的。

**遗留缺口（non-blocking，见 M4）**：NUL/CR/LF 等控制字节可入段名（`b%00.bin` → 201）。当前无穿越面（如上），且 Location 头经 `url.PathEscape`（实测 `%0D%0A`/`%00` 全转义，无头注入）。但这是唯一直接吃外部输入的层，建议 Layout 直接拒绝控制字符，避免 T-15 目录列表/HTML 渲染侧再背一次责任。

## 二、校验头语义——结论：矩阵正确

实测矩阵（真实 HTTP）：正常三算法 201；大写合法 hex 201（归一化）；宽度/非 hex → **400**（`ErrInvalidChecksum`，与 409 分级正确）；sha1-only / md5-only 不匹配 → 409（独立短路正确，storage Commit 按声明逐算法比对）；`X-Checksum-Deploy: true` + 缺专用头 → 400（逐字规格文案）；格式非法 → 404（合规格）；sha256 命中 → 201 且 body 被忽略、下载内容=既有 blob（Artifactory 语义）；`deploy: false` + body → 正常上传；`X-Explode-Archive: true` → 400（E-25）。

- **409 文案实测**：`Checksum error for 'generic-local/mismatch.bin': received '000…0' but actual is '2d71…81'` —— 与 E-11/repo-semantics §5 定案一致。
- `:properties`/`:statistics` PUT/DELETE 双侧 409，`we:ird.bin` 普通冒号名不误伤（有专门用例）。

## 三、四动词——结论：形态正确，一处文案偏差（Major-1）

- PUT 201：Location / X-Checksum-Sha256 / size 字符串 / ISO8601 毫秒（实测 `2026-08-17T19:32:30.000Z`）。
- DELETE 204 无 body → 重复 404 → GET 404，实测幂等链完整。
- GET/HEAD 头集合齐全（三 checksum + ETag 无引号 + Last-Modified + Accept-Ranges + Content-Length==size），Content-Type 缺省 octet-stream。
- ETag 无引号 sha1（实测），If-None-Match 现阶段一律 200——条件请求归 T-20（在途），非本票缺陷；T-20 需做 W/ 前缀与引号剥离，交接说明已写明。
- 405 + Allow 头正确。

## 四、SPI / registry / principal——结论：全部合格

- Register 四类 panic（nil/空 proto/空 types/重名 proto/重 claim）有测试；panic 时持锁但仅启动期，无泄漏问题。
- `ForRepoType`/`All`/`Protocols` 走 RWMutex、`All` 返回新切片——T-14 并发调用安全。
- `byType[proto]=h` 双键设计有注释说明（claim 冲突即启动 panic），语义自洽。
- principal：typed `principalBox` 消除跨包裸断言，nil=anonymous 与 absent 可区分，`PrincipalFrom` 不可能 panic。

## 五、测试质量——结论：真黑盒，达标

- curl 套件为 `os/exec` 真进程 + `--path-as-is`，非 http.Client 假黑盒；裸挂无 ServeMux 的理由（mux 会把 `..` 归一成 3xx 吃掉 400 防线）成立且已在交接中提醒 T-14。
- 慢上传：`exec.CommandContext` + `cmd.Cancel = Process.Kill`，Go 1.20+ 语义等价 SIGKILL；curl 缺失时诚实 skip。
- 61 子测试断言深度合格（信封形状、头逐项、received/actual、拒绝后无残留 node）。
- 干净 clone 上 `-race` 全绿复现。

---

# 问题清单

## Blocking（1）

### B1. BlobOpener 功能性直调 storage，违反 §5.1 依赖方向（需 architect 裁决定案）
- 位置：`internal/adapter/generic/api.go:26`（`BlobOpener` 类型）、`api.go:46`（`var _ BlobLedger = (metadata.BlobStore)(nil)` 生产代码内的编译期断言）、`handler.go:150-162`（`h.opener(ctx, sha)` → `storage.Engine.Open` 直调）。
- 问题：architecture.md §5.1 明文「adapter/* → repo.Service + auth；**禁止**直接 import storage/metadata（唯一例外：需要流式细节时**经 repo.Service 扩方法**，不得绕过）」，且「违例=拒绝合并」。`storage.BlobRef`/`*metadata.Node` 类型被迫随 repo.Service 签名流入 adapter，属不可避免；但 `BlobOpener` 是**功能性调用**绕过 repo.Service，正是该例外条款点名禁止的形态。
- 附带正确性瑕疵（支持收编的证据）：opener 只查 **filestore** 是否有该 sha256 的 blob。若目标 blob 是崩溃窗口留下的孤儿（物理 blob 在、blobs 台账行缺失），checksum-deploy 会把它实体化为 node，且 `putNode` 以「客户端声明的 sha256 + stat 的 size + 空 sha1/md5」写台账（`ON CONFLICT DO NOTHING` 不回填）→ 该内容从此永久 sha256-only 降级服务。经 repo.Service 扩方法则可先查台账、回填三元组后再落 node。
- 建议改法（二选一，交 conductor → architect）：
  1. repo.Service 扩 `PutFromBlob(ctx, p, repoKey, path, ref storage.BlobRef, mime string) (*metadata.Node, error)`（内部自开 blob、先核台账），adapter 删除 BlobOpener 注入——**推荐**，一并修掉台账降级瑕疵；
  2. architect 出 ADR 修订 §5.1 认可该窄缝（只读侧、接口注入）。
  - 无论裁决为何：`api.go:46` 的编译期断言移到 `_test.go`（生产代码不应 import 具体实现做断言，这也是把「仅类型依赖」与「实现依赖」混在一起的原因）。

## Major（1）

### M1. GET/HEAD 缺文件 404 文案用了 DELETE 侧文案，违反 rest-api.md §1.4
- 位置：`internal/adapter/generic/handler.go:312-313`（`writeServiceError` 的 `ErrNodeNotFound` 分支对全部动词统一输出 `Could not locate artifact. Path: '<repo>/<path>'.`）。
- 规格：GET/HEAD 内容路径 404 应为 `Failed to find the requested resource '<repo>/<path>'.`（rest-api.md §1.4，高置信度）；`Could not locate artifact. Path: ...` 是 repo-semantics §4 的 **DELETE** 文案。当前同一个 handler 里 folder-GET 分支（handler.go:244）用对了文案、缺文件分支用错，两条 GET 404 文案互不一致。
- 建议改法：`writeServiceError` 增加 verb 参数（或在 `handleGet` 内对 `ErrNodeNotFound` 提前分支，复用 folder 分支的文案常量）；DELETE 保持现文案。状态码本身正确，仅文案。

## Minor（7）

### m1. originalChecksums 在「上传但未声明任何算法」时回退为全三元组
- `iteminfo.go:83-98`：`declaredSet(空)` → 空 map → `originalChecksumsOf` 走 fallback 输出 sha1/md5/sha256 全量。Artifactory 语义是「只回显客户端声明过的」——上传上下文存在但零声明时应输出空对象 `{}`。fallback 的正当场景是「无上传上下文」（下载、/api/storage FileInfo），当前实现把两种场景混在同一个判据（`len(declared)==0`）上。建议：上传路径显式传「有上下文但零声明」的标记（如非 nil 空 set），仅无上下文场合回退。

### m2. Layout 接受段内控制字节（NUL/CR/LF）
- `layout.go:96-103` 只判 `""/"."/".."`。实测 `b%00.bin`、`b%0a.bin` → 201 落库。当前无穿越面（blob 路径与 relPath 无关）且 Location 经 PathEscape 转义（无头注入），但建议 `validateRelPath` 增加一条：段内含 `< 0x20 || 0x7f` 的字节 → `ErrBadRequestPath`。唯一直接暴露外部输入的层应自带这条底线，别指望 T-15 渲染层兜底。

### m3. `checksumMismatchMessage` 靠解析 storage 英文错误串重组文案
- `handler.go:336-354`：`strings.Index(msg, "sha256 received ")` 系列字符串匹配，与 `session.go:147` 的 `"%s received %s, actual %s"` 格式隐式耦合；格式一变即静默落入 fallback 分支（文案退化为裸 err）。已有 fallback 不算缺陷，但建议 storage 侧出结构化错误（如 `type MismatchError struct{ Algo, Received, Actual string }`），adapter 类型断言取值——两个 internal 包间的契约不该是正则。

### m4. mkdir 的 FolderInfo 携带 `checksums.sha256 = 64 个 0`（emptyFolderSHA 哨兵）
- 实测响应体可见 `"checksums":{"sha256":"000…0"}` 与同值 originalChecksums。Artifactory FolderInfo 无此字段值，内部哨兵泄漏到协议面。建议 `itemInfo` 对 folder node 省略 checksums 两个对象。

### m5. time.go 注释与实际输出不符
- `time.go:5-9` 注释称「the zone arm prints as +00:00」，实际 `Format("…Z07:00")` 对 UTC 输出 `Z`（实测 `2026-08-17T19:32:30.000Z`）。输出本身合法（ISO8601 UTC 指示符，与规格示例同类），仅注释误导，改注释即可。

### m6. Location 完全信任 `r.Host`、scheme 仅判 `r.TLS`
- `handler.go:422-428`：TLS 终止代理后面 Location 会变 `http://`；config 已有 `server.base_url`（空=按请求推导）语义但本层未接。建议在交接单注明由 T-14 注入 BaseURL（或 config 下传），避免每个 adapter 各写一份推导。

### m7. 测试的两个可移植性/健壮性小点
- `TestContentVerbsTable` 各行共享顺序状态（"checksum deploy hit" 依赖前面的 upload 行产生 blob）：`-run` 单跑子测试会 404。建议依赖行内自备 fixture 或注明顺序依赖。
- curl 套件用 `shasum -a 256`（macOS 名）：Linux 通常只有 `sha256sum`；curl 缺失会 skip 但 shasum 缺失会 Fatal。建议 LookPath 失败改 skip（与 curl 一致）。

## Nit（1）

- `headerBool`（handler.go:407-419）对不可识别值（如 `X-Checksum-Deploy: banana`）按「头不存在」处理，静默降级为普通上传。可接受，但建议至少与 `X-Explode-Archive` 一样显式拒绝或在注释里写明容忍决策。

---

# 遗留四项裁决输入（供 conductor → architect）

1. **sha1-only deploy 404**：维持现状。BinFlow blob 以 sha256 寻址、台账 sha256 主键，无反查索引；主流客户端 checksum-deploy 走 sha256 头完整可用；补 sha1 反查是 metadata 接口变更+索引成本，M1 无客户端需求。建议 M3（remote 镜像链路）再评估——彼时外部源摘要形态才是真实输入。
2. **BlobOpener 注入位置**：**应收上 repo.Service**（见 B1）。理由三点：§5.1 例外条款本就规定「经 repo.Service 扩方法」；收编后可先核台账、修掉孤儿 blob 秒传导致 sha1/md5 永久降级的瑕疵；并发的同 sha 双 deploy 目前靠 idempotent 分支兜住，收编后事务边界更清晰。adapter 侧保留窄接口注入形态，改动局部。若 architect 倾向保留 seam，请出 ADR 修订 §5.1 并记录台账降级瑕疵为已知限制。
3. **originalChecksums 持久化**：**M1 不需要**。已接受的上传中客户端声明值==实测值（不一致即 409 拒绝），持久化无信息增益；真正需要原始值的是 `.sha1/.md5` 伴车校验和文件机制（rest-api.md §1.5，M1 未实现）——那才是「为既有文件补登记客户端摘要」的场景。建议：实现伴车文件那个票时一并加 blobs 列（或独立 client_checksums 表），现在动 schema 是提前付费。
4. **TOCTOU 零调用确认**：属实。全仓 grep `FilterUnreferenced` 仅 metadata 包本体及其测试，adapter 零引用；GC 归 T-16。无行动项。

# 范围外发现（交 conductor）

- **当前工作区**（非 `0eac4a8`）的 `internal/adapter/generic/` 含 T-20 在途改动（handler.go 的 Range/条件请求分支、conditional*.go、dbg_test.go、curl_test.go 新增用例），`go test -race ./internal/adapter/generic` 在工作区**失败**：`TestCurlRoundtrip/If-Modified-Since_-z_both_sides — "-z future = 200, want 304"`。已确认 T-13 提交本身全绿（干净 clone 复测）。该失败属 T-20 开发中状态，请勿计入 T-13，但 T-20 需在自家验证里解决后再进 qa。
- `internal/adapter` 尚无任何生产侧 Register/挂载（cmd/、httpapi/ 零引用）——符合分工（T-14 范围），仅备忘。

# 结论重申

正确性/安全主轴（路径安全、校验头矩阵、四动词、SPI）全部实测成立；扣分项为一条包边界违规（需 architect 定案）与一条 GET 404 文案偏差。**REQUEST_CHANGES**：B1 裁决落定（或改法落地）+ M1 修复后即可通过。
