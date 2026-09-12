# L009-3-review-a — stray+users 体裁微票 + P1（Reviewer A，correctness 面）

Ticket:        L009-3 permissions %zz/NameValidator 两臂 + users 404 envelope + P1
Role:          code-reviewer (reviewer-a)
Area:          internal/httpapi（router/permissions/security/envelope + 测试）
Input:         conductor 派发（两票串行）；reports/agents/L009-3.md；台账 rest/permissions-v1-edge-validation-family、rest/users-v1-get-unknown-style；L008-list-params-diff.md §6；反编译三源（reverse-src/artifactory-7.161.20-partial）：org/jfrog/common/validator/{XSSValidator,NameValidator}.java、org/artifactory/addon/security/RestSecurityRequestHandler.java:551-640
Changes:       diff 全量：router.go（withNameUnescaped 两级解码+urlDecoderDecode）、permissions.go（双校验器+keyed 载体 6 处）、security.go（users 两臂）、envelope.go（SetEscapeHTML(false)）+ 三测试翻新 + 两新测试；上下游追读：keyed 面路由挂载、writeError 537 调用点全包波及排查
Files:         router.go 正确（两级解码+防御臂+传输层钉死）；permissions.go 分派结构正确、载体收口正确，但 XSSValidator 形态复刻在可达角与反编译模式分歧、NameValidator 缺三字面量臂（见 blocking）；security.go 正确；envelope.go 正确（波及面零断言依赖转义形）
Tests:         子集含两票全部新测试 ok 6.761s；-race 子集 ok 21.032s；全包 ok 282.510s；活参照 :8082 六臂只读探针全命中（Commands 栏）
Commands:      curl --path-as-is -u admin '.../permissions/n1%25zz' → 400 信封 `URLDecoder: Illegal hex characters in escape (%) pattern - not a hexadecimal digit: "z" = 122`（**含引号形**，逐字）；'.../n1%25' → 400 `URLDecoder: Incomplete trailing escape (%) pattern` 逐字；'.../users/l009-ghost-user' → 404 信封 `Not Found`；409 臂（PUT 路径/体名不匹配，返回先于任何写）→ 409 信封逐字；missing-repositories 臂（PUT 体仅同名，返回先于任何写）→ 400 信封逐字；裸 TCP `GET .../%zz` → `HTTP/1.1 400 Bad Request` + `Content-Type: text/plain; charset=utf-8` + 体 `400 Bad Request`（参照 Jetty 与 BinFlow net/http 同应答，L006「BinFlow 404」= 探针客户端 %25 重引工具差的结论成立）
Outputs:       本报告 reports/agents/L009-3-review-a.md
Compatibility: 台账两 entry ①②落地成立（差分子集内逐字 SAME）；③超集维持；users 双臂落地（泛化文案遵台账）。载体收口（writePlainError→writeError ×7）经 409/missing-repos 两活体臂验证为参照真实形态（JAX-RS Response.entity(String) 经 Artifactory 信封 writer 包装）
Security:      fail-closed 400；恶意名经 JSON 信封序列化；SetEscapeHTML(false) 是载体对齐非注入面（application/json + nosniff 不变）；XSSValidator 本身即安全护栏——护栏形态与参照分歧即 blocking
Performance:   每请求 O(n) 名扫描+常数级解码，无测量面——认可
Risks:         blocking 三条修复后仍需 LOOP 010 差分补 `</x>`、`</>`、换行、`.`/`&`、int32 窗口诸臂防回归；镜像含并行轨 WIP（票面 Risk ① 如实）
Blockers:      无（取证全通）

## 评审报告 L009-3（形态: reviewer-a）
结论: REQUEST_CHANGES

### 必须修改（blocking）
- internal/httpapi/permissions.go:80（xssMarkupName） XSSValidator 复刻用 `<>|<.{2,}>` 归纳 27 探针臂，与反编译模式 `(.*)<(|/|[^/>][^>]+|/[^>][^>]+)>(.*)`（reverse-src/.../org/jfrog/common/validator/XSSValidator.java，与活体 7.161.20 同源）在可达输入上**双向分歧**：
  1. `n1</x>n2`（`%3C%2Fx%3E`，普通可发送段）：参照——alt2 要求恰 `/`、alt3 首字符排除 `/`、alt4 要求 `/`+≥2 字符，`/x` 三臂皆不中→放行 201；BinFlow——`.{2,}` 吞 `/x`→400。假阳性拒答。
  2. `n1</>n2`：参照 alt2（`/` 单字符）命中→400；BinFlow 1 字符不中 `.{2,}`→放行 201。护栏漏放。
  3. `<em>` 后随换行（`n1<em>\nn2`）：参照 `.matches()` 尾部 `(.*)` 不跨行→放行；BinFlow MatchString 无锚，子串 `<em>` 命中→400。
  4. 换行在括号内（`n1<a\nb>n2`）：参照 `[^>]+` 否定类**可**匹配 `\n`→400；BinFlow `.` 不跨行→放行。
  票面 Risk ② 的口径（「Java regex `.` 默认不跨行，Go 同默认」）不成立——承载分支的是否定类 `[^/>]`/`[^>]+`（跨行）与锚定 `(.*)`（不跨行），`.{2,}` 两头都不等价。最小修复（一行）：照源移植 `regexp.MustCompile(`^.*<(|/|[^/>][^>]+|/[^>][^>]+)>.*$`)`——RE2 全支持（空分支合法，`^...$` 无 m 旗标即 matches() 全串语义，`.`/否定类跨行行为与 Java 同），isNotBlank 闸由路由构造保证（空名不达此臂）；测试补上述 4 臂。
- internal/httpapi/permissions.go:73（NameValidator 插桩） 仅复刻字符集臂，缺同调用点（RestSecurityRequestHandler.java:605→NameValidator.validate）的三字面量臂：body 携名 `.` / `..` / `&` 时参照 400 `Name cannot be empty link: '<name>'`，BinFlow 现状 201 建靶。`Name cannot be blank` 臂在 keyed 面不可达（isNotBlank 前置，反编译 600-601 行证实）无需做。修复：`nameFromPath==false` 分支在字符集检查前加 `if body.Name == "." || body.Name == ".." || body.Name == "&" { writeError(w, 400, "Name cannot be empty link: '"+body.Name+"'") ; return }`。
- （跨票重申）storage.go 七参 int32 窗口——见 T-L009-2-review-a.md blocking 条。

### 建议改进（non-blocking）
- urlDecoderDecode 按字节直写，不做 Java `new String(bytes, UTF-8)` 的畸形序列替换（`%FF`：Java→U+FFFD，Go→原样字节）——未取证角，查无实害（两侧皆查无此名），LOOP 010 矩阵补臂定谳即可。
- writeJSONBody/writeJSONBodyCT 成功体仍走 MarshalIndent 默认 HTML 转义，而错误信封已关——Jackson 载体对齐只做了一半（含 `&`/`<>` 的成功体如权限名、属性值仍转义形）。建议 D 票或 LOOP 010 收口，非本票范围。
- curl 直发 `%zz` 因环境代理语义得到绝对形请求行，不可复现——裸 TCP 才是可靠取证法；TestPermissionsV1KeyNameTransport400 的做法值得保持为该族的标准取证姿势（实现已如此，仅记录方法论）。

### 正确性核对记录（证据）
- **分派结构等价性证明**（反编译 600-619 行 vs permissions.go:419-431）：参照 entityKey-XSS 检查在 body 携名时形式上无条件执行，但可达性分析——body 名过 NameValidator（无 `<`/`>`）且 == entityKey（否则 409 先返回）⟹ entityKey 无 `<`/`>` ⟹ XSS 模式（需字面 `<...>`）必不中——故「body 携名走 NameValidator+409、无名 body 走路径键 XSS」的互斥结构与参照严格等价；实现测试 212-217 行（benign 体名 + XSS 路径键 → 409）与反编译 409-先于-XSS 次序一致。
- NameValidator 字符集 `/ \ : | ? * " < >` 9 字符与 illegalNameChars 逐字符一致；文案与反编译 `Illegal name : '/,\,:,|,?,<,>,*,"' is not allowed` 逐字节一致。
- urlDecoderDecode 与 java.net.URLDecoder 语义：`+`→空格、二轮 %XX、`i+2>=len`→Incomplete（`n1%`/`n1%2` 两臂）、c1 先于 c2 报首个非十六进制（活体 `%252g`/`%25g2` 均报 `"g" = 103`）——全对齐，含字符码十进制（z=122/g=103/空格=32）。
- 传输层：参照 Jetty 与 BinFlow net/http 对裸 `%zz` 请求行同答 `400 Bad Request` text/plain——L006 台账「BinFlow 404」确系探针客户端把 `%` 重引为 `%25` 的工具差，票据更正结论成立；防御臂（PathUnescape 失败裸 400）监听器下不可达，测试钉其不退化。
- SetEscapeHTML(false) 波及面：writeError 全包 537 调用点、50 文件；唯一携带 `<...>` 的消息 search_legacy.go:155 无测试断言其转义形；全部断言经 json.Unmarshal 或 Contains 解码值比较——零断言受影响，方向为 Jackson 对齐正确。
- users 404：参照活体 404 信封 `Not Found`（application/json）与实现 writeError(w,404,"Not Found") 逐字一致；DELETE 臂不动（台账证据外）符合范围纪律。
