# 评审报告 T-92（视角: correctness + ACL 安全面）

结论: **APPROVE**

- 日期: 2026-08-20
- 评审对象: commit 358b3c1（注意：T-92 的 metadata 层实际先行混入 T-90 提交 595e090，见范围外发现）
- 代码: internal/metadata/search.go、internal/repo/search.go、internal/httpapi/search.go（+ api.go/service.go/router.go/routes_test.go/storage.go 接线面）

## 取证命令（均实际执行）

```
go vet ./internal/metadata/ ./internal/repo/ ./internal/httpapi/          # OK
gofmt -l <四个触及包>                                                       # 空
golangci-lint run <四个触及包>                                              # 0 issues
go test -race ./internal/metadata/ -run Search -count=1                    # ok 2.3s
go test -race ./internal/repo/ -count=1                                    # ok 55.0s
go test -race ./internal/httpapi/ -count=1                                 # ok 65.7s
go test -race ./internal/httpapi/ -run 'TestSearchACLW16|...W36' -v        # 全 PASS（子测试逐条见下）
```

## 六个重点逐项核验

### 1. ACL 零泄漏（NFR-S24）——通过，零 blocking

- **过滤实现形态**：repos 收窄 = SQL 侧 `repo_key IN (...)`（metadata/search.go:94-104）；路径级 ACL = 后置 `filterVisible`（repo/search.go:121-132）。关键在 filterVisible 走的是与内容面下载 Get（service.go:239）**同一个** `s.allow`（admin 短路 + `az.Can(ctx, p, repoKey, path, ActionRead)`），非重写判定。PRD NFR-S24 明文允许"谓词下推或后置过滤"，本实现合法。
- **匿名通道**：`searchGate`（repo/search.go:110-115）用 `allow(ctx, nil, "", "", read)` 探 `anonymousRead`，与 `auth.Service.Can` 对 nil principal 的判定（authorizer.go:20-25，`anonymousRead && read`）一致——门与逐节点过滤是同一决策，不存在门开滤关（或反之）的缝。
- **双引用泄漏探针**（实跑 PASS，-race）：TestSearchACLW16 构造 hidden blob 双引用（generic-local/acme 未授权 + other-local/ci-out 授权），ci-bot 查该 sha256 **恰得 1 条**（仅授权侧）；name 臂同构（同 repo 未授权 arm 的同名词零出现）。与 curl 双实例 27/27 互证。
- **repos 绕过面**：repos 只能收窄（IN 谓词），非 admin 的后置过滤无条件执行，**不存在绕过路径**。传入未授权/未知 repo 名 → 静默空结果（采 E-04 过滤先例），规格对"未授权 repo 名"沉默、对"未知 key"亦沉默；行为已在 metadata/repo/httpapi 三层测试钉死并申报待 R3 逆向校准。不构成泄漏（空页零信息量，与 Artifactory 过滤姿态一致）。
- **输出面**：仅 `writeSearchResults` 渲染返回节点；错误路径消息只含调用方自供参数（digest/name），无他人数据。

### 2. SQL 面——通过

- **LIKE 转义**：`likeSubstring`（metadata/search.go:54-57）单趟 Replacer 转义 `\ % _`，配 `ESCAPE '\'`；测试覆盖反斜杠、`_`/`%` 字面量化（myXlib/100Xct 对照臂）、大小写敏感（DSN `case_sensitive_like=1`，store.go:136 已核实）。无通配注入面。
- **注入**：`nodeQuery` 拼接的 conds 全为包内常量形状，动态部分仅占位符个数；所有值参数化。G202 nolint 附理由成立。SearchByChecksum 三臂各为字面量语句。
- **索引真实性**：`idx_nodes_blob`（migrations/sqlite/001_init.sql:54，nodes(sha256)）、`idx_blobs_sha1`（003_remote_virtual.sql:50）均实际存在；md5 无索引已如实申报（ledger 全扫，行数=内容数，可接受）。
- **边界**：仅 sha1/md5 且 ledger 零命中 → keys 空 → 提前返回，规避 `IN ()` 语法错误（search.go:161-163）；folder 排除 `path NOT LIKE '%/'` 为常量模式，直击 folder 哨兵行的探针已测。

### 3. 校验——通过

裸 hex（docker `sha256:` 前缀拒 400）、64/40/32 长度、大小写归一小写、至少一值 → 400、repos csv ≤1000（HTTP 层切分、用例层封顶）。repo/search_test.go 与 t92_search_test.go 双层矩阵覆盖。混合大小写归一到存储拼写有专测。

### 4. HTTP 面——通过

- **fileInfoOf 零行为变化**：git diff 核实为纯提取——字段构造逐字面保留，`writeFileInfo` 以相同 `requestBase(r)` 委托；测试另做搜索条目 vs `/api/storage` item info 的字段一致性断言（E-09 单一来源成立）。
- **SR-04**：六族（props/users/artifactory/pattern/badge/gavc）+ 裸 `/search`、`/search/` + 两开放路径的异动词全部 404 且带 not-implemented 措辞，实跑 PASS。
- **R5 最小面**：routes_test.go 恰删 1 行（artifact）+ 增 6 行族 + 注明去向，无顺手改别的矩阵行。
- **空结果 `[]` 非 null** 有断言（body.Results 为 nil 即 fail）。

### 5. 越界申报（docker 两测试文件 4 stub）——合理

diff 核实：fakeService 2 个 stub（返回 errUnimplementedFake）+ countingGetService 2 个委托转发，纯机械、零行为，属接口扩展强测附带损伤，有 T-71/72 先例。`go vet ./...` 若不补确实编译失败。申报属实且与其它波次改动 hunk 独立。

### 6. 性能遗留——处置合理

无索引全表扫 ~120ms@10 万（< NFR-P17 1s，正式门归 T-105）、md5 无索引、批量 ACL 缝均如实挂账并给归属。race 构建探针跳过 perf 抽验的理由（插针 ~17x 放大）成立，且非 race 路径照跑。

## clean-room 抽查

无逐行对应嫌疑：行为依据为 docs/reverse/rest-api.md §4 规格行（含"未认证 → 403"原义核对），代码为 BinFlow 惯用式 + 项目自有注释风格，未见反编译标识符/结构搬运。

### 必须修改（blocking）

无。

### 建议改进（non-blocking）

1. **无上限渲染 + N+1**：`?name=/` 一类宽片段在 10 万 node 库可单查询返回 10 万条 FileInfo，且每条经 `digestTripleOf`（storage.go:280-291）做一次 blobs 点查——实测 p95 只覆盖 100 命中，全命中渲染成本未设门。建议 T-105 前加防御性 limit 或至少在全量命中场景放一个 QA 探针（实现者已申报遗留 #4，此处补 N+1 细节）。
2. **错误优先级**：关闭实例匿名 + 非法查询 → 400（HTTP 层与用例层均在 searchGate 之前校验），即形状错误先于 403。rest-api §4 未定先后（表格反列 400 在前），建议在 api.go 注释或测试里钉一句"形状校验先于匿名门"为有意行为，防 QA 口径漂移。
3. **O4 与空页的表述**：零授权已认证用户在搜索得 200 空页（非 403）。这与 O4（内容面下载 403 姿态）不冲突——NFR-S24 是过滤语义——但建议 PRD FR-26 处加半句"搜索面零授权=空页非 403"，避免 QA 按 O4 误判。
4. **perf 造数直插 SQL**（RawConn）绕过域不变量：作为 perf 种子可接受（理由已注明），但建议在测试头注释明确"仅限 perf，禁止用作行为夹具"。

## 范围外发现（交 conductor）

- **提交粒度**：T-92 的 internal/metadata/search.go + search_test.go 已先行混入 T-90 提交 595e090；最终提交 358b3c1 又捆绑 T-89+T-92。实现者要求的"按票拆分归属"未发生（T-90 reviewer 已在 BOARD 记录同类发现，无代码缺陷，纯流程项）。
