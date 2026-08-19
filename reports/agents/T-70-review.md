# 评审报告 T-70（视角: correctness）

结论: **REQUEST_CHANGES**（1 blocking，其余全过）

- 日期: 2026-08-19
- 范围: commit `c59bd6b`（internal/adapter/pypi/ 8 生产 + 7 测试 + routes_test.go E-26 翻转）
- 取证命令（本机实跑）:
  - `CGO_ENABLED=1 go test -race -count=1 ./internal/adapter/pypi/` → **ok**（两次，16.4s / 58.5s）
  - `go test -count=1 -run 'TestE26' ./internal/httpapi/` → **ok**
  - `go vet ./internal/adapter/pypi/ ./internal/httpapi/` + `gofmt -l` → 零输出
  - 恶意文件名探针（临时测试文件，跑完即删，工作树已复核干净）：`<>&"'`、`#`、`?`、空格、`%`、`;`、反斜杠、正斜杠九变体走 upload→simple 页→JSON→下载回环；fd 卫生探针（30 轮重复上传）；审计保真探针（生产装配形态 `audit.New` 接线）。

## AC / 契约核对（全过项）

| 检查点 | 结果 | 证据 |
|---|---|---|
| PEP 503 三态同页 + 变体 | ✓ | TestSimpleNormalizationMatrix（含 `Demo.Pkg`/`demo__pkg` 与负例 `DEMO__PKG....1`→404）；ETag 相等证明字节同页 |
| HTML 转义完整性 | ✓ | 探针九变体：五元字符走 `htmlEscape`（`&amp;&lt;&gt;&quot;&#39;`），`#`/`?` 走 `url.PathEscape`（`%23`/`%3F`，href 不被 fragment/query 截断），空格/分号/字面 `%` 均正确；转义拼写下载回环 200 |
| JSON simple（PEP 691） | ✓ | filename 保持原文（Go JSON 的 `&` 形态任何解析器等价解码，非 HTML 双重转义）；Accept 显式才 JSON，缺省/`*/*`/`text/html` 保持 HTML |
| href fragment 仅 sha256 | ✓ | TestSimplePageShape 断言无 `#md5=` |
| api-version 头 | ✓ | 规格 §3.2 逐字（`<meta name="api-version" value="2" />`，仓级/包级同头，恰好一次） |
| ETag 稳定性 | ✓ | sha256(渲染体)，渲染确定性（条目按文件名排序、并列按全路径、固定头尾）→ 相同内容跨重启稳定；304 三拼写（精确/弱/`*`）过 |
| 302 补尾斜杠 | ✓ | 相对 Location 的论证成立：T-63 缝 `withAPIProtocolPrefix` 把 URL 重写成 `/binflow/<tail>` 后处理器看不到 api 拼写，绝对路径会换挂载面；相对目标两入口均正确解析（TestSimpleRedirect 断言最终 URL 保持 api 拼写） |
| `:action` 严格 400 | ✓ | `unknown action '<a>'` 与 PRD v1.2 M31 细节四逐字；缺失分支文案自定已注明；零残留断言 |
| md5_digest 三态 | ✓ | 缺失自算（X-Checksum-Md5 回显实证）/ 大小写不敏感正确 200 / 不一致 409 received-actual 且零 node / 宽度错 400；sha256_digest 同链 |
| 同 filename 400【暂行】 | ✓ | "already exists" 文案在；同字节重传也 400（严格防覆盖）；原始内容幸存 |
| multipart 流式 | ✓ | content 直灌 storage session（字段序颠倒用例过）；值字段 1MiB/128 上限；重复 content/错文件字段/坏 boundary 400 |
| 存储原始名不归一化 | ✓ | C7：node 落 `<name>/<version>/<filename>` 原始拼写，归一化仅索引查找 |
| 穿越防御 | ✓ | name/version 段级校验 + `adapter.NormalizeRelPath` 双层；filename 侧 mime/multipart 剥目录成分 + 反斜杠段校验 400；全量断言三段形态；`%2f` 字面名按表单数据处理正确 |
| 下载双入口 | ✓ | 两入口同 bytes 同头集（X-Checksum-Sha256/Sha1/Md5、ETag=未引号 sha1、Last-Modified、Accept-Ranges）；单区间 206/416 `bytes */total`/多区间忽略 200/弱 ETag 304/If-Modified-Since 304/HEAD 等价 |
| remote/virtual 写门 | ✓ | 405 + `Allow: GET` 在读 body 之前；remote 文案与 RE-04、virtual 文案与 C5 定案逐字比对一致（PRD §5.2/§5.5-C5） |
| E-26 翻转 | ✓ | 两行注记来源（R5）+ `TestE26PyPIMountRouting`（真挂载：未知名协议 404 / 未知仓内容面 404 / pypi-ui 永久 404）；与 T-69 npm 行共存；改动面最小 |
| MetadataProvider | ✓ | Classify（`simple/`→metadata，余 content）、PackageName（三形态→归一化名，保留端点/legacy JSON→ok=false）、Versions=nil；Register 单入口双注册（T-63 N2）有 compile+断言钉住 |
| clean-room | ✓ 无嫌疑 | 行为来源 = PEP 公开规范 + docs/reverse 行为规格；无 Java 包结构/反编译标识符残留；conditional.go 为仓内 generic 契约的 per-protocol 副本（docker range.go 先例，抽共享包留 architect 决定，已注明） |

## 必须修改（blocking）

1. **`internal/adapter/pypi/upload.go:169` — 重复 filename 探针丢弃 `svc.Get` 返回的 `io.ReadSeekCloser`（未 Close）+ 伪造 download 审计事件。**
   `switch _, _, err := h.svc.Get(ctx, p, repoKey, path)` 把第一个返回值丢掉。`repo.Service.Get` 的契约注释明写 "(caller closes)"，且 local 命中路径会真实 `storage.Open` 打开 blob 文件句柄（service.go:247）。重复上传命中分支（err==nil）每次泄漏一个打开的 fd（仅靠 os.File finalizer 兜底回收——30 轮探针 delta=1，finalizer 掩盖了累积，但代码层面违反契约）。
   同时实证（生产装配形态 `audit.New` 接线进 `repo.New`）：**每次重复上传尝试都会先落一条 `action=download path=<name>/<ver>/<file> actor=admin` 审计事件**再落 deploy 事件——被拒绝的写操作在合规审计流里被记成了一次下载，污染审计面与任何未来按 download 事件计数的统计。
   → 建议改法（最小）：
   ```go
   rc, _, err := h.svc.Get(ctx, p, repoKey, path)
   if rc != nil { _ = rc.Close() }
   switch { case err == nil, errors.Is(err, repo.ErrIsFolder): ... }
   ```
   （审计噪声的根治需要 service 面提供 metadata-only 存在性探测——见范围外。）

## 建议改进（non-blocking）

1. `simple.go:280 wantsSimpleJSON` 用子串匹配：`Accept: ...vnd.pypi.simple.v1+json;q=0` 仍出 JSON（q=0 应拒）。PRD 只要求「显式含」所以现状合规，T-75 QA 无需改；若日后收紧按 RFC 9110 解析。
2. simple 项目页 HTML/JSON 两形态共用一个 URL 但无 `Vary: Accept`（也无 Cache-Control）：中介缓存理论上可交叉服务两形态。BinFlow 自身不做共享缓存标记，实际风险低；建议 T-66 metadata TTL 层落地时一并补 `Vary: Accept`。
3. `normalize.go:43` 段校验里 `HasPrefix("../")`/`HasSuffix("/..")` 两分支不可达（前一行 `ContainsAny("/\\")` 已拒）——无害的死防御，可留可删。
4. 重复 filename 探针与 `PutLandedBlob` 之间存在 TOCTOU：并发同 filename 双上传都可通过 Get miss，后到者落入 putNode 覆盖链（有 delete 权限的调用方会覆盖而非 400）。无 AC 覆盖并发形态，记录口径即可；如需严格，PutLandedBlob 需暴露「存在即拒」参数（service 面）。
5. 值字段上限 128 × 1MiB = 单请求最多 ~128MiB 内存缓冲。写门已要求认证，风险有界；可考虑收紧 description 类字段上限或在 doc.go 注明该算术。
6. `validateUploadSegment` 的 128 字符 filename 上限：真实 wheel 文件名（长名+版本+tag 串）理论上可超，将 400。罕见，留意即可。
7. `download.go:93` 416 分支的 `hdr.Del("Content-Length")` 是死代码（该分支之前从未设置过 Content-Length）——无害。

## 范围外发现（转 conductor）

1. **N4 缝层强制仍无主**：决策记录在 `doc.go:31-40`（严格 404 姿态）；BOARD.md:208 显示 conductor 已把 N4 列入 T-69/T-70 派单要点，两票均回报「无法从协议包观察、强制点在 `dispatchAPIProtocolMount` 一行 + 翻转 T-63 钉死行」。该缝层核对在 T-69/T-70 都落地后仍无人认领——请 conductor 显式路由（建议随 T-80 接线或单开小票，npm/pypi 一并收口）。
2. **repo.Service 公共面缺 metadata-only 存在性探测**：blocking 项的审计噪声根源是 Get 兼任探测。建议 service 面增 Head/Stat（不 Open blob、不写 audit），协议适配器（maven/npm/pypi 的重复检查同款）统一切换。
3. 遗留③（remote/virtual simple 400 过渡）：现状由 T-66 工作树内 `loadLocalRepo`/`refuseNonLocalWrite` 的 `ErrRepoTypeNotSupported`（文案 "not served by the local content plane"）承载，pypi handler 映射为 400——口径与 T-70 日志 §6.3 一致，过渡合理，T-75 按终态复核（已具名）。

## 结论

代码质量高、测试面扎实（46 测试函数 + 门控真客户端矩阵）、契约逐字对齐 PRD v1.2 定案。唯一 blocking 是 upload 重复探测的一行资源/审计缺陷，修复成本一行；修掉即可转 qa。
