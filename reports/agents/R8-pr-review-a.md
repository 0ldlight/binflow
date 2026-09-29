# R8 PR 评审报告（形态: reviewer-a / correctness）

评审区间：`49cb5cae..HEAD`（6 commits：f54333bf docs、23c628ff docs、749cb5c1 BIN-50、4d18e3aa BIN-49、dfff3e1f BIN-52、777049cf 台账批）
工作目录：`.claude/worktrees/clever-grothendieck-a3fa4f`（git worktree）

```
Ticket:        BIN-49/T-567 + BIN-50/T-568 + BIN-52/T-570 + 台账批 777049cf（附 23c628ff/T-569 提案 docs-only、f54333bf docs-only）
Role:          code-reviewer (reviewer-a / correctness)
Area:          internal/adapter/{deb,rpm,helm,nuget,cargo,pypi,generic,maven} + docs/compatibility/known-divergence.yaml
Input:         conductor 派发（R8 载荷、四焦点清单）；L036 六点裁定引文（代码注释内引）；docs/reverse/mime-ownership.md §2；
               reports/agents/T-56{7,8,9,0}.md；httpapi router.go/layout.go/middleware.go（Location 回读链追读）
Changes:       六渲染点 Location 改绝对形 + nuget v3 push 去头（4d18e3aa，5 产品文件 + 5 新测试 + 2 旧测试调断言）；
               pypi 上传 200 补 Location + X-Checksum-Sha256（749cb5c1，2 文件）；
               generic/maven mime 表对齐工厂表 v17 群1/群2（dfff3e1f，4 文件）；台账 7 resolved + 2 新 UNKNOWN（777049cf）
Files:         deb/handler.go:549-588 ✓；rpm/handler.go:577-615 ✓；helm/handler.go:569-607 ✓；nuget/flat.go:356-364+519-522+537-567 ✓；
               cargo/handler.go:344-347+365-397 ✓；pypi/upload.go:249-289 ✓；generic/mime.go ✓（注释有一处事实错误，见 non-blocking 1）；
               maven/mime.go ✓；generic/mime_test.go ✓（注释同错）；maven/mime_table_test.go ✓；known-divergence.yaml ✓（抽查三条与代码一致）
Tests:         go test ./internal/adapter/{deb,rpm,helm,nuget,cargo,pypi,generic,maven}/ -count=1 → 8 包全 ok
               （deb 91.1s / rpm 79.3s / helm 55.0s / nuget 74.2s / cargo 64.6s / pypi 60.5s / generic 25.6s / maven 93.1s，exit 0）；
               go vet 八包 → 无输出；gofmt -l internal/adapter/ → 空；T-567/T-568/T-570 报告声称的测试结果与本次复跑一致（无虚假证据）
Commands:      git log/diff --stat 49cb5cae..HEAD；grep 全仓 Set("Location" 扫漏点（7 处全收敛，pypi/simple.go:116 为 302 相对形，正当）；
               GOROOT/src/mime/type.go builtinTypesLower 核对（.csv 在 builtin，.md/.yaml/.swift 不在）；
               /tmp/r8review 一次性 go run 验证 url.PathEscape 逐段编码面（空格→%20、%→%25、#→%23、?→%3F、中文→UTF-8 percent、
               +/&/=/: 留裸〔pchar 合法且 PathUnescape 原样回读〕、;→%3B、,→%2C）
Outputs:       reports/agents/R8-pr-review-a.md（本文件）
Compatibility: 台账 resolved 块抽查 deb/pypi/nuget-v3 三条与代码逐点一致（deb 三调用点 handler.go:324/414/464→587、
               pypi upload.go:262-266、nuget flat.go:356-364）；nuget bare 的 X-Checksum-Sha256 拆条与新 UNKNOWN 两条均为诚实拆分
               （代码确未渲染/确维持 text/plain）。.csv 残差归因在 T-570 报告与两处代码注释中写错机制（darwin/linux 宿主漂移），
               实为 Go builtin 宿主一致——见 non-blocking 1，不影响 resolved/UNKNOWN 判定本身
Security:      pypi Location 三段（name/version/filename 客户端可控）先经 validateUploadSegment + NormalizeRelPath 双层校验
               （upload.go:317-330，CR/LF 控制字节在 validateRelPath 拒绝）再 escapePath 渲染，无注入面；
               五族 Location 的 rel 均为已校验存储路径，repoKey 来自路由层；无越权/穿越新增面
Performance:   pypi sha256 零重算属实：sess.Append 流式喂 digesters（session.go:78 MultiWriter）、Commit 返回 actual
               （session.go:183），失败腿 poison 不可能产出半摘要（session.go:58-61）；响应路径零额外 I/O。
               Location 渲染为纯字符串拼接，无锁无分配热点
Risks:         1) 反代/TLS 终止场景 requestBase 只认 r.TLS，Location scheme 会渲染成 http://（httpapi 自己的 requestScheme
               〔router.go:299，X-Forwarded-Proto 感知〕仅用于 /v2 realm 回退——同产品两套 scheme 推导，形态不一致，现状记录）；
               2) productPrefix="/binflow" 已复制进 8 个 adapter 包（+httpapi），ADR-0008 前缀若变更有 9 处漂移面（B 形态应裁）；
               3) cargo 族为推断级对齐（A 面建仓被 Custom Base URL 门挡），注释已如实标注，实证推翻则勘误
Blockers:      无（测试全绿、A 端证据链在案、无 clean-room 嫌疑）
Next:          1) BIN-53 票面应修正预期：.csv/.pdf/.svg 是 Go builtin（mime/type.go builtinTypesLower），裸 linux 容器同样答
               text/csv|application/pdf|image/svg+xml 而非 octet-stream——残差是宿主一致面不是 darwin 漂移面，删回退前无腿会自动收敛；
               2) 建议任一族 created_location_test 补一条需转义文件名腿（空格/%/中文）——escapePath 八份副本目前零测试覆盖
               （本次以推演+GOROOT 核验证实正确性）；3) X-Forwarded-Proto 与 Location scheme 的反代形态建议开部署域票统一；
               4) 交 reviewer-b：productPrefix 九处复制的归属裁定 + generic 表缺 .mf/.css 等 20+ 工厂表行的范围外发现（BIN-53 族）
```

## 评审报告 R8-pr（形态: reviewer-a）
结论: **APPROVE**

### 逐焦点取证（correctness）

1. **escapePath 逐段 PathEscape 正确性**：url.PathEscape 按 RFC3986 pchar 逐段编码（实测：空格→%20、`%`→%25、`#`→%23、`?`→%3F、中文→UTF-8 percent、`;`→%3B、`,`→%2C；`+ & = : @` 留裸）。回读链：httpapi 以 EscapedPath() 裸拼写分发（router.go:110），withStrippedPrefix 原样保 RawPath（router.go:2446-2464），adapter.ResolveContent 整串 PathUnescape（layout.go:55）后过 SplitMatrixParams。关键不变式成立：**writeCreated 渲染的 rel 是存储值（写入时已过同一 peel+validate），GET 重放同一 decode+peel 幂等**——含 `;` 的遗留路径回落同判、含 k=v 矩阵形的路径写入时已被剥，Location 不携带矩阵参数且 GET 回读一致。文件名含字面 `%2F` 亦无歧义（编码为 %252F）。推演结论：可原样 GET 回读（deb/rpm/nuget/cargo 测试附 GET 200 锚点佐证 ASCII 面）。
2. **requestBase 反代取值面（如实记录）**：五族+pypi 的 requestBase 只看 r.TLS，**不处理 X-Forwarded-Proto**。httpapi 存在 XFP 感知的 requestScheme（router.go:299-307）但仅服务 /v2 realm 回退构建。TLS 终止反代后所有 201/200 Location scheme=误渲染 http。与既有 generic/maven（T-563/T-564）同形态，非本批引入的回归；记录为 follow-up 候选（non-blocking 3）。
3. **pypi X-Checksum-Sha256 来源**：ref.Sha256 = storage.BlobRef.Sha256 = Append 流式增量摘要（session.go:78），Commit 先对声明值校验后发布（session.go:188-211），失败/partial 一律 poison 不可提交（session.go:58-61、71-77）——摘要严格等于上传 content part 字节。中断/重试腿稳定：Append 失败→400+defer Abort；Commit 失败→writeServiceError，两腿均不写头；并发同 blob 走 singleflight（key=actual.Sha256），各得自身 actual，同值。CT 维持 text/plain：A 面为 vendor 私有 ItemCreated+json，维持 B 现值=票面裁定第 3 条，回归风险=登记 UNKNOWN（pypi/upload-response-content-type-body），无掩盖。
4. **maven .sha512 删条目副作用**：sidecar GET 面走 writeSidecarDigest→sidecarContentType 常量（handler.go:36/247），不经 extensionMimes；sha512 sidecar 本身按 L032 裁定 404（handler.go:236-239，含 walk 腿门）。表删仅影响无声明 CT 的 .sha512 body PUT 推断——mime_table_test.go:69-84 resolver 级钉 octet-stream 且 stored-wins 臂在案（stdlib 无 .sha512 builtin，实测 GOROOT 表无此键）。.csv 删条目：wire 零变化（旧表值与 Go builtin 值同为 text/csv; charset=utf-8），但注释归因错误（见 non-blocking 1）。
5. **created_location_test 断言强度**：五族均表驱动 + 绝对字面值断言 + **GET 回读 200 锚点**；deb 显式覆盖矩阵坐标腿（`;deb.distribution=…` 三段，断言矩阵不入 Location，created_location_test.go:39+62）；nuget 覆盖 v3 push 双 URL 形态断言**无头**（负断言）+ 旧测试 flat_test.go:130 改无头断言；helm 含 .prov 腿；cargo 含 index/sidecar 族腿。唯一缺口=无 percent-encoding 腿（non-blocking 2）。
6. **台账一致性抽查**：deb（三调用点+矩阵剥离+GET 锚=代码与测试事实）、pypi（通用形 Location+会话摘要+CT 维持=upload.go:262-266）、nuget-v3（去头=flat.go:356-364）三条 resolved 与代码逐点相符；两条拆分 UNKNOWN（nuget bare X-Checksum-Sha256、pypi CT/body）与代码现状相符，属诚实拆分非藏账。
7. **虚假证据核查**：T-567（五包 62.6/58.1/40.1/50.0/46.0s）、T-568（pypi 9.5s）、T-570（generic 11.1s/maven 28.3s）声称的绿测与本次全量复跑（91.1/79.3/55.0/74.2/64.6/60.5/25.6/93.1s）方向一致，全绿复现。T-568 的 twine 活体 wire 取证（Location 可跟随、sha256 逐字一致）与代码推演一致。
8. **clean-room 抽查**：六渲染点修法源自 L036 活体探针+裁定引文，mime 表源自出厂配置文件 mimetypes.xml 的行为规格（docs/reverse/mime-ownership.md 行为句式），无逐行对应 reverse-src 嫌疑。

### 必须修改（blocking）

- 无。

### 建议改进（non-blocking）

1. **generic/mime.go:16-17 注释 + generic/mime_test.go:20-25 注释 + T-570 报告 .csv 归因——事实错误**：注释称 .csv「falls through to octet-stream」「bare linux: likely ""」，T-570 称「Go builtin 无 .csv」。实测 GOROOT/src/mime/type.go:73 builtinTypesLower 含 `.csv → "text/csv; charset=utf-8"`（toolchain go1.26+），**裸 linux 容器同样答 text/csv; charset=utf-8**。残差是宿主一致分歧（≠A 的 octet-stream），不是 darwin 漂移；BIN-53 若按错误归因排期会预期落空（.csv/.pdf/.svg 三腿删回退前在任何宿主都不收敛）。建议改两处注释并把归因更正带给 BIN-53 票面。wire 行为本批零变化（旧表值=builtin 值），故不 blocking。
2. **escapePath 零测试覆盖**：八份副本（本批新增五份+pypi 复用旧份+generic/maven 既有）无任何一族测过需转义文件名。正确性本次已由推演+stdlib 核验背书，建议任一族（如 deb plain 面）补一条 `loc pkg 1.0%.deb` 腿断言 Location 字面值+GET 回读。
3. **requestBase 不认 X-Forwarded-Proto**（deb:553/rpm:581/helm:573/nuget:539/cargo:367/pypi:274 + 既有 generic:554/maven:565）：TLS 终止反代后 Location scheme 误渲染 http；httpapi 的 requestScheme（router.go:299）已有 XFP 感知实现但仅用于 /v2 realm。建议部署域统一票（八渲染点+realm 一并裁定），非本批回归。
4. **productPrefix="/binflow" 九处复制**（8 adapter 包 + httpapi router.go:33）：有意为之（adapter 是叶子、不 import httpapi），但漂移面在扩张——建议 reviewer-b 裁定是否提共享常量到 adapter 包。
5. pypi upload.go:264 `if ref.Sha256 != ""` 为死守卫（Commit 恒返非空计算摘要），无害可留。
6. 范围外发现（交 conductor，不入本票结论）：generic 表缺 .mf（工厂表 text/plain 族）及 .css/.java/.py/.xz/.bz2/.7z/.apk/.gem 等 20+ 工厂表行，均落 stdlib/builtin 宿主一致面（部分带 charset 差异），BIN-53 族收口时应按工厂表全量核对。
```
