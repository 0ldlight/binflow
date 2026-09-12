# T-L009-2-review-a — list 补参大票 P0+P1（Reviewer A，correctness 面）

Ticket:        L009-2 list 参数族补参 P0+P1（P0 递归语义+形态+行为差；P1 listFolders/includeRootPath）
Role:          code-reviewer (reviewer-a)
Area:          internal/httpapi（?list handler 面）
Input:         conductor 派发（两票串行）；reports/compatibility/L008-list-params-diff.md §1/§2/§3 全文；reports/agents/T-L009-2.md
Changes:       diff 全量：internal/httpapi/storage.go（190 行重写）、repositories.go（writeJSONBodyCT）、compat_test.go（TestStorageList 翻新）+ 新增 storage_list_params_test.go（357 行）；上下游追读：router.go storage 派发臂（939-1023）、splitStoragePath、storageNode/listWithNote/serveRootFolder/isFolderPath/isoMillisUTC/storageURI、internal/migrate/reader.go（跨界消费面）
Files:         storage.go 正确（递归三连修+形态五项+行为差三项均落）；repositories.go 正确（CT 助手无行为分叉）；compat_test.go 翻新与证据一致；storage_list_params_test.go 行为命名、九表臂+根组合臂+线形态臂齐
Tests:         go build ./... + go vet ./internal/httpapi/（空）；go test ./internal/httpapi/ -run 'TestStorageList|TestURLDecoderDecode|TestWithNameUnescaped|TestPermissionsV1Key|TestUserDetailFieldSet|TestPermissionsV1AdminSubjectRefusal|TestPermissionsV1AliasDetail' -count=1 → ok 6.761s；go test -race 同子集 → ok 21.032s；go test ./internal/httpapi/ -count=1 全包 → ok 282.510s
Commands:      上述五条 + 活参照 :8082 只读探针（见 L009-3 报告）；grep writeError 537 调用点波及面排查
Outputs:       本报告 reports/agents/T-L009-2-review-a.md
Compatibility: 与 L008-3 §1 十四条逐条核对：item1-4/8-14 实现逐字吻合（含 `For input string:` 信封、`/d2` 形文件夹行、前导斜杠、字母序、vendor CT、请求时刻 created、根可列、冒号文案）；item5-7（P2 三参）按票面不做、仅整数校验——正确
Security:      无新攻击面；参数校验先于目标解析（顺序契约成立）；403 匿名臂保留
Performance:   单次 List+内存投影维持；排序 O(n log n)；每文件行 blob ledger 读为既有行为；无新 IO 路径——无回退
Risks:         int32 溢出窗口（见 blocking）；`?list&deep` 空值推定 400 `For input string: ""`（Java parseInt("") 同文案，证据直接读出，认可）；重复参数名取 vals[0]（Java 侧未取证，极角）
Blockers:      无（取证全通）
Next:          ①补 reviewer-b（storage 域双审强制，本票只有 A 形态）；②修复 int32 窗口后可重审通过；③范围外发现两条交 conductor（详见结论区）

## 评审报告 T-L009-2（形态: reviewer-a）
结论: REQUEST_CHANGES

### 必须修改（blocking）
- internal/httpapi/storage.go:694（parseListOptions 内 strconv.Atoi 后） 七参整数校验未钳 Java parseInt 的 int32 界：`?list&depth=2147483648` 参照 400 `For input string: "2147483648"`（Integer.parseInt 公开规范，溢出抛 NumberFormatException 同文案），BinFlow 200（Go Atoi 64 位平台吞下）→ 状态级分歧，且落在票面「depth 钳层边界（超大）」审查点上。窗口 (2^31, 2^63)——20 位以上长串 Go 已因 ErrRange 回 400 恰好对，只有这个窄窗错。建议改法（一行）：Atoi 成功后 `if n > math.MaxInt32 || n < math.MinInt32 { return o, errors.New(`For input string: "` + vals[0] + `"`) }`，测试加 `depth=2147483648` 一臂。

### 建议改进（non-blocking）
- storage.go:836 created 手写 `time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")` 与 isoMillisUTC 的格式串重复——若 isoMillisUTC 语义可承载可复用；现状正确，仅 DRY 提示。
- Java parseInt 接受非 ASCII Unicode 数字（全角 `１` 等 Character.digit 路径）而 Go Atoi 拒——极角仅记录，LOOP 010 矩阵可补一臂定谳。

### 范围外发现（交 conductor）
- GET /api/storage?list（完全无仓段）：BinFlow 走 router.go:948-950 → notImplemented 404 信封；参照按 L008 §1.12 为 400 `Cannot list files of root.`。未入 32 臂矩阵、票面明示范围外——建议 compatibility-engineer 立账。
- docs/reverse/rest-api.md:119 与 docs/reverse/api-inventory.yaml:53 仍留「根目录 → 400 Cannot list files of root.」旧文案，已被 L008-3 §1.12 推翻（仓库根可列，400 仅属无仓段请求）——文档同步项。

### 正确性核对记录（证据）
- 尾斜杠根因修复确认：name=TrimSuffix(rel,"/") 后计段数，旧「无 listFolders 任何深度零文件夹行」死守卫（尾斜杠计入 levels 使 folder 行恒超 depth）消除；TestStorageListRecursionSemantics 9 表臂（deep=1 零泄漏/deep≠1 平/depth 单独不递归/depth=0 忽略/depth=99 平/deep=1&depth=1|2|4 钳层）与证据 §1.2-3 逐臂吻合。
- 根组合臂：node=nil 时 dir=""、prefix=""、storageURI 裸仓 key（TrimSuffix 收尾斜杠）、includeRoot 的 `/` 条目经 sort 自然居首——A26/A27 测试钉住；`/` 条目 lastModified 在根臂为 1970 零时刻（isoMillisUTC("") 防御成立不 panic）——票面 Risk ① 已如实登记。
- migrate 跨界无破坏：internal/migrate/reader.go:280 filterSourceFiles 对 uri 已做 TrimPrefix("/") 归一且注释明示两形态兼容；folder 过滤走 bool 字段不依赖尾斜杠。
