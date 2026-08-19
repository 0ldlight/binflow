# T-65 评审报告（视角: security — SSRF/凭据/注入面）

- 票据: T-65 internal/remote：SSRF 防护链 + stdlib 出站 HTTP client
- 结论: **REQUEST_CHANGES**（blocking 2 / non-blocking 7）
- 评审人: code-reviewer（安全）
- 日期: 2026-08-19
- 取证方式: 通读 internal/remote 全部 5 文件 + 契约三方（PRD v1.2 NFR-S13 / ADR-0012 决策 3 + 勘误二 / T-79）；攻击探针以 `go test -overlay` 注入包内（/tmp，零仓库改动、零外网连接），矩阵覆盖 IPv6 过渡格式 / 数值混淆 IP / DNS 混排答案 / 重定向 Location 变体 / 凭据 A→B→A 链。

## 取证命令与关键输出

```
$ go test -race -count=1 ./internal/remote/        → ok 2.170s
$ go vet ./internal/remote/...                     → VET_OK
$ gofmt -l internal/remote/                        → (空)
$ go test -count=1 -cover ./internal/remote/       → coverage: 86.0%（与日志一致）
$ git diff --stat go.mod go.sum                    → 空（零新依赖，与日志一致）
$ go test -overlay /tmp/t65_overlay.json -run TestReview -v ./internal/remote/
```

探针命中（原文摘录）：

```
NAT64 embeds 127.0.0.1       rejected=false   (http://[64:ff9b::7f00:1]/x)
NAT64 embeds 169.254.169.254 rejected=false   (http://[64:ff9b::a9fe:a9fe]/x)
6to4  embeds 127.0.0.1       rejected=false   (http://[2002:7f00:1::]/x)
Teredo embeds 127.0.0.1      rejected=false   (http://[2001:0::7f00:1]/x)
NAT64 hop: Location: http://[64:ff9b::a9fe:a9fe]/latest → hops=2（被跟随，非拒绝）
classifyIP: fe80::1 → "link_local"；fe80::1%en0 → ""（zone 使 Contains 失配）
dial [fe80::1%25en0]:80 → rejection=false err=connect: invalid argument
dial mixed [93.184.216.34 10.0.0.5] → rejected: private_rfc1918（混排答案全拒，两序皆拒）
system-resolver CheckURL("http://127.1/x")      → rejected: loopback 127.0.0.1
system-resolver CheckURL("http://2130706433/x") → rejected: loopback 127.0.0.1
cred chain A→B: hop b.test auth=""（剥离）；回跳 a.test auth=""（不恢复，fail-closed）
```

## 必须修改（blocking）

### B1. IPv6 过渡格式（NAT64/6to4/Teredo）嵌套的违禁 IPv4 完全绕过校验链

- 位置: `internal/remote/ssrfguard.go:51-69`（blockedRanges）与 `:74-85`（classifyIP）
- 问题: `classifyIP` 只做 `Unmap()`（折 v4-mapped），不拆过渡格式。探针实证 6 个变体全部通过 CheckURL，且重定向真实跟随 NAT64 跳（第 2 跳已发出）：
  - `http://[64:ff9b::7f00:1]/x`（NAT64 嵌 127.0.0.1）→ 放行
  - `http://[64:ff9b::a9fe:a9fe]/x`（嵌 **169.254.169.254 云 metadata**）→ 放行
  - `http://[64:ff9b::a00:5]/x`（嵌 10.0.0.5）→ 放行
  - `http://[2002:7f00:1::]/x`、`http://[2002:a9fe:a9fe::]/x`（6to4）→ 放行
  - `http://[2001:0::7f00:1]/x`（Teredo 段）→ 放行
- 影响: **NAT64 well-known prefix（64:ff9b::/96）由 NAT64/DNS64 网关翻译内嵌 IPv4**——IPv6-only 集群（云上部署矩阵）下 `64:ff9b::a9fe:a9fe` 会被网关翻译成 169.254.169.254，即对 PRD 明确点名保护的目标形成完整 SSRF 绕过。6to4/Teredo 需 relay 路径，可利用性弱，但属同一洗白类。
- 规格依据: **ADR-0012 决策 3 原文显式将 `64:ff9b::` 列入清单**（"链路本地（169.254/64:ff9b::/fc00::/7）"）；勘误二列举的五个被 PRD 取代的参数（跳数/超时/体量/TTL/建仓只校验 scheme）**不含 IP 清单本身**，故 ADR 清单条文仍有效——实现者"PRD 枚举集即契约"的读法与 ADR 原文冲突，需 conductor 裁决并回写勘误；安全侧裁定：必须防。
- 建议改法（外科式，不破 DNS64 合法上游——真实公网上游在 DNS64 环境会解析进 64:ff9b::/96，不能整段封）:
  ```go
  // classifyIP 内，Unmap 之后追加：
  // NAT64 64:ff9b::/96、6to4 2002::/16（第 3-6 字节为内嵌 v4）→ 取内嵌 v4 递归过表；
  // Teredo 2001:0::/32 → 直接拒绝（已废弃，不可能为合法制品上游）。
  ```
  每类补一行表驱动测试（嵌 127.0.0.1 / 169.254.169.254 / 公网内嵌三态）。同时上报 conductor：PRD NFR-S13 ② 需勘误补过渡格式拆包规则（或 ADR 勘误明确放弃——需安全签署）。

### B2. 带 zone 的 IPv6 字面量绕过 fe80::/10 类目（三阶段全漏）

- 位置: `internal/remote/ssrfguard.go:74-85`
- 问题: `netip.Prefix.Contains` 对带 zone 的地址返回 false。`classifyIP("fe80::1%en0") = ""`；`CheckURL("http://[fe80::1%25en0]/x")` 返回 nil（无拒绝、**无 WARN**）；dial 与 Control 同逻辑同样放行，最后仅在内核层 `connect: invalid argument` 失败——连接失败是 URL 转义后 zone 名恰好不匹配接口名的**偶然结果**，不是链的强制。
- 影响: fe80::/10 是 PRD ② 明文枚举类目；一个 SSRF 形态的目标穿过全部三个阶段且不留 ⑦ 要求的 WARN 审计行。若未来某调用方（T-66/校验层）直接用 CheckURL 判定配置合法性，会得到"允许"。
- 建议改法: `classifyIP` 入口 `ip = ip.WithZone("")` 再过表（一行 + 一个测试用例）。

## 建议改进（non-blocking）

1. **CGNAT 100.64.0.0/10 不在清单**（ssrfguard.go:51-69）——PRD 枚举集即契约，实现者已在日志声明；但部分内网/云 VPC 用 CGNAT 段承载内部服务，属规格缺口而非实现缺陷。交 conductor/PM 走 PRD 勘误（表内一行即可）。
2. **scheme 拒绝的 WARN target 记完整 URL**（ssrfguard.go:198 用 `u.String()`）——URL 带 userinfo 时（如 `gopher://user:pass@host/`）凭据进日志；host 类拒绝正确地只记 `u.Host`。建议 scheme 类也只记 host+scheme，`CheckURL` parse 失败路径同理（:195 内插 rawURL）。当前无真实泄密路径（凭据是管理员自己的），日志卫生加固。
3. **A→B→A 重定向链凭据不恢复**（client.go:400-405，`header.Del` 永久删除）——安全方向正确（fail-closed），但同源往返链会匿名回源。建议在 T-66 接线文档中写明，防误判为 bug。
4. **重定向 Location 自带 userinfo 时 stdlib 会注入 Authorization**（client.go:424 建请求未剥 `next.User`）——net/http 对 URL userinfo 自动转 Basic 头；凭据是攻击者自选值，无 BinFlow 秘密泄露。可选加固：follow 内 `next.User = nil`。
5. **MaxBufferedBody 负值 = 解除 64MB 上限**（client.go:120-125）——生产 Options 结构上的测试缝，误传 -1 即关掉 DoS 防护。建议改为显式开关或仅测试可及。
6. **重试退避无 jitter**（client.go:414-443）——200ms/400ms 固定、上限 2 次、逐跳独立，放大器风险可忽略；惊群为观感问题。
7. **测试命名误导 + 用例计数口径**——ssrfguard_test.go:63 名为 "fec0:: site-local" 实测 `fc00::1`（fec0::/10 本身未拦，已废弃段，可接受但命名应改）；日志称"48 用例"，实测 28 个测试函数 / 88 个 RUN 条目（含子测试），计数不符但不影响结论。

## 核验通过项（正面清单）

- **DNS rebinding TOCTOU**: check 与 dial 两相各自独立解析并**全答案过检**（混排公私两序均拒，探针实证）；钉死 `ips[0]` 后无任何路径再走系统解析（keep-alive 复用的是已验证连接，且每请求仍过 CheckURL）；`Control` 对实际连接地址二次过检；v4-mapped 在 dial 相同样被折（探针：`[::ffff:169.254.169.254]:80` 拒）。
- **TLS 一致性**: 未覆写 `TLSClientConfig.ServerName`、自定义的是 `DialContext` 而非 `DialTLS`——SNI/证书校验取自 req.URL host，与 pinned-IP 拨号解耦，hostname 校验完好。
- **数值/八进制/十六进制 IP 混淆**（2130706433 / 127.1 / 0x7f.0.0.1 / 0177.0.0.1）: 真实系统解析器实证——resolver 规范化后全答案过检即拒（`127.1`→127.0.0.1 拒）；到达 connect() 的字符串只可能是 netip 规范形态，无 guard-vs-kernel 解析不对称逃逸。
- **重定向面**: 每跳（含初始）发包前过全链（禁跳 hop 仅调 1 次）；Location 相对/协议相对/绝对/userinfo 藏私网/大写 scheme/file:// 全部正确处置；畸形 Location 报错不发包；5 跳上限 5 成 6 拒。
- **凭据面**: Basic 编码与 stdlib 等价；跨 host 剥离实证成立、同 host 保留实证成立；host 类拒绝的 WARN target 为 host:port，无凭据字段。
- **资源面**: 64MB 双路（declared CL 快速失败 + LimitReader(limit+1) 截断检测，恰好压线放行）；Stream 不限流符合分型规格；重试仅连接级错误 + 幂等 GET/HEAD + 上限 2 + 指数退避，超时/链拒绝/状态码/ctx 取消均不重试；退避常数固定不可被放大；Fetch defer Close、重定向 body 有界 drain 后关闭、CloseIdleConnections 暴露。
- **规格静默两项的保守方向均正确**: 超时不重试（15s×3 会拖垮 assumed-offline 时延，ADR-0012 勘误一 assumed-offline 语义佐证）；跨 host 剥凭据（浏览器/Go stdlib 标准安全默认）。
- **clean-room 抽查**: reverse-src 对应物为 Java 的 `UrlVerifierRedirectStrategy`（host 白名单机制），BinFlow 为 Go IP 清单 + pinned dialer + 全答案过检，机制不同构、语言不通，无逐行对应嫌疑。
- 工程声明复核: `-race` PASS、vet/gofmt 零告警、coverage 86.0%、go.mod/go.sum 零改动——与 T-65.md 声明一致（用例计数除外，见 non-blocking 7）。

## 范围外交 conductor

- PRD NFR-S13 ② 与 ADR-0012 决策 3 在 IP 清单上的文字冲突（64:ff9b::）需勘误定案（B1 附建议文案）。
- CGNAT 100.64/10 是否入清单属 PRD 决策（non-blocking 1）。
- T-64（internal/repo 在途改动）有 1 条 errorlint（T-65 日志已自报，非本票引入，属实）。
