# T-65 架构评审报告（视角: consistency / architecture）

- 票据: T-65 [P0] internal/remote：SSRF 防护链 + stdlib 出站 HTTP client
- 评审对象: commit f9fb2c9（internal/remote/ 5 文件 + T-65.md），工作区与提交一致（git status 干净）
- 评审人: code-reviewer（架构一致性）
- 日期: 2026-08-19
- 结论: **REQUEST_CHANGES**（2 blocking，均为小改；架构面整体合格）

## 一、ADR-0012 决策 3/5 + T-79 勘误对照（逐项核实）

| 契约条款 | 实现 | 判定 |
|---|---|---|
| 勘误① 重定向 5 跳、每跳重过链、拒绝 400 | `DefaultMaxRedirects=5`；`follow()` 每跳（含初始）先 `CheckURL` 再发包；5 跳边界成功 + 第 6 跳 `ErrTooManyRedirects` 双向测试 | ✓ |
| 勘误② socketTimeoutSecs 默认 15s 统一 | `DefaultSocketTimeout=15s` 驱动 Dialer.Timeout/TLSHandshake/ResponseHeaderTimeout/idle-deadline；`TestClientDefaults` 钉死 | ✓ |
| 勘误③ 缓冲型 64MB / artifact 流式不限 | `Fetch` 双路限流（declared 预检 + LimitReader 实测）；`Stream` 不限流 + 计数；见 blocking-2 的 HEAD 误报 | ✓（一处例外见 B2） |
| 勘误⑤ 建仓只校验 scheme、IP 校验全在请求时 | `NewClient` 只断言 scheme/host 在场；`http://10.0.0.1:9099` 构造成功有显式断言（`TestNewClientSchemeAssertion`）——与勘误口径一致，**未沿用 ADR 旧「配置时+连接时」双检表述** | ✓ |
| 决策 3 ③ DNS rebinding：pin 已验 IP + Control 二检 | `dial()` 自解析→全 IP 过检→pin `ips[0]`；`Control` 对实际连接地址复查（backstop 定位诚实标注）；`TestGuardDialRebinding` + `TestClientRebindingThroughTransport` 穿透 http 栈验证 | ✓ |
| 决策 5 stdlib-only、go.mod 零新增 | 非测试 import 仅 14 个 stdlib 包；f9fb2c9 未触 go.mod/go.sum，`git status` 干净 | ✓ 实证 |
| 决策 5 幂等 GET 指数退避 2 次 | `DefaultRetries=2`、200ms→400ms；GET/HEAD（HEAD 为幂等放宽，NB-6）；超时不重试为规格静默处的保守裁定且单点可翻（`retriable()`） | ✓（裁定交 conductor 备案） |

keep-alive 复用时不发生 dial、check 相是唯一逐请求防线——报告取舍表已如实声明，与「每跳重过①②③」的③按连接粒度解释合理，无契约违背。

## 二、T-66 消费面逐项核对（对照 T-61 派单 AC①②）

| T-65 报告遗留① | 导出面形态 | T-66 AC 匹配 |
|---|---|---|
| RejectionError → 400 E-01 | 导出结构体，`Category` 稳定字符串、`Error()` 文案含 category；`errors.As` 穿透 url.Error/net.OpError（有测试） | ✓ |
| ErrBodyTooLarge → 502 | 哨兵 + `fmt.Errorf %w` 包装，`errors.Is` 可达 | ✓ |
| ErrTooManyRedirects | 哨兵，`errors.Is` 验证 | ✓ |
| FinalURL | `Result/StreamResult.FinalURL`（取 `resp.Request.URL`，同源 302 链测试断言 /three） | ✓ |
| BytesRead | `StreamResult.BytesRead()`，atomic 并发安全 | ✓ |
| 按仓 client / CloseIdleConnections | `Options` per-repo 构造 + `CloseIdleConnections()`（删仓/停机） | ✓ |
| Options.Resolve 测试缝 | 存在，生产传 nil | ✓ |

T-66 AC 的「上游 401/403 视为 unfound」「5xx/超时 → assumed-offline」依赖「非 2xx 原样返回、仅传输/链错误为 error」——形态满足。**无缺失接口**。过度设计嫌疑仅一项（Guard 导出面，见 NB-1）。

## 三、包边界

- 非测试 import 严格 stdlib；未 import repo/metadata/adapter/config/storage——接线归 T-66，符合分批边界。✓
- doc.go 声明两子域（ssrfguard/client）+ fetcher/cache-state/credentials 留 T-66，与实际文件组织一致；架构 §2 remote 行（fetcher 门面 `remote.Fetch` 为 T-66 落地）不冲突。✓
- clean-room 抽查：SSRF 链为 BinFlow 自有安全面（PRD 明言非 Artifactory 对标项），实现为 stdlib 惯用模式（Dialer.Control pin/IP 前缀表）自研组织；repo-semantics §7.1 仅消费行为参数 socketTimeoutMillis=15000。未发现与 reverse-src 逐行对应嫌疑。✓

## 四、测试形态与 NFR-S13 七点覆盖

- 注入 `Resolve` + httptest loopback（豁免下）+ `hopFunc` 未导出缝：全套件零外网依赖，本机 `-race -count=3` 连跑稳定，CI 可复现。✓
- 七点映射：①scheme（矩阵+构造+302 两式）②全 IP（矩阵 34 例 + 公私混排 + v4-mapped 洗白）③rebinding（guard 级 + http 栈级）④重定向（每跳筛 6 例 + 5 成 6 拒 + 同源跟随）⑤64MB（declared/chunked/压线）⑥超时（不重试 + mid-body idle）⑦WARN（每次拒绝断言一条、attrs、无堆栈）。**七点全覆盖且带零上游命中负断言**。✓
- 实测用例数：88（含子测试）/28 顶层，报告写 48——少报非多报，勘误即可（NB-5）。

## 五、必须修改（blocking）

1. **client.go:78-82 — `Options.MaxRedirects` / `Options.MaxBufferedBody` godoc 与代码相反**。注释写「`<= 0` means Default*」，代码实际是 `== 0` → 默认、`< 0` → 钳 0（MaxRedirects=0 跳即不跟随 / MaxBufferedBody=0 即不限流）。负值语义两句注释还互相矛盾（MaxBufferedBody 首句 vs 次句）。这是 T-66 下一波直接编码的契约面：传 -1 期望默认会得到「不跟随重定向/无上限」的静默行为差异。→ 改法：注释改为与 Retries 同款准确表述（`0 means Default*; negative clamps to 0 (…)`），两处各一行。
2. **client.go:292-295 — `Fetch` 的 declared Content-Length 快速失败对 HEAD 误报**。HEAD 响应携带制品体积的 Content-Length 但 body 零字节；对大制品（>64MB）的 HEAD 探测/再验证会直接 `ErrBodyTooLarge`（T-66 映射 502）——纯粹由声明头触发，未读任何字节，违背 64MB 上限的内存保护初衷。→ 改法：快速失败仅对实际读 body 的方法生效，如 `if method != http.MethodHead && limit > 0 && resp.ContentLength > limit`（`req.Method` 在 Fetch 作用域可得；实测路径 `io.ReadAll` 对 HEAD 读 0 字节天然安全）；补一条 HEAD+大 declared CL 用例。

## 六、建议改进（non-blocking）

1. `Guard`/`NewGuard`/`GuardOptions` 导出但当前唯一消费者是 `Client`；T-66 AC 无直接消费。若 T-66 接线后仍无外部使用者，建议随 T-66 收敛为未导出（internal/ 包，风险低，仅记录）。
2. WARN `target` 字段形态不一致：scheme 拒绝记完整 URL（`u.String()`），IP 拒绝记 `host:port`（`u.Host`）。M42 取证 grep 面建议统一，一行改动。
3. 超时类测试墙钟断言（80ms 超时 / 350ms 上界）在重载 CI 有理论 flake 空间；本机 -race 3 连跑稳定，暂留观察。
4. 工程默认三处（h1.1-only / DisableCompression / 默认 UA）：**注释在案即可，M3 无需 config 面**——PRD 每仓配置面只有 socketTimeoutSecs 系；DisableCompression 是架构 §4.5 逐位一致的结构性要求，h1.1-only 是 idleTimeoutConn 的结构约束，UA 已可按请求覆盖（Request.Header）。建议 T-66 派单注明「三默认为 T-65 工程裁定，上游兼容性反馈出现时加单点 Options 缝」。
5. 报告「48 用例」勘误为 88（含子测试）/28 顶层。
6. retry 面覆盖 HEAD（ADR 原文「仅幂等 GET」）：幂等语义一致的放宽，T-66 再验证路径需要，记录在案即可。

## 七、范围外 / 交 conductor

- 【规格待验证】超时不重试（`retriable()` 单点可翻）与【规格未写】跨 host 重定向剥离 Authorization：dev 已如实标注，建议转 PM/规格面确认后回写 PRD 或 ADR 备注，无需改码。
- internal/repo/landed_blob_test.go:222 的 errorlint：属并行在途 T-64 改动，dev 已上报，本票无关。

## 八、验证取证（实际执行）

```
go test -race -count=1 ./internal/remote/   → ok 2.661s
go test -race -count=3 ./internal/remote/   → ok 4.824s（无 flake）
go vet ./internal/remote/                   → 零输出
gofmt -l internal/remote/                   → 空
git show --stat f9fb2c9                     → 仅 internal/remote/* + reports/agents/T-65.md
git status --porcelain internal/remote go.mod go.sum → 空（评审对象 == 提交态）
非测试 import 清单 → context/errors/fmt/io/log-slog/net/net-http/netip/net/url/strings/sync-atomic/syscall/time/encoding-base64（全 stdlib）
```
