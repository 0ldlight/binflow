# 评审报告 T-11（视角: correctness + security）

结论: **REQUEST_CHANGES**

- 日期: 2026-08-18
- reviewer: code-reviewer
- 范围: `internal/auth/`（13 文件）、`internal/audit/`（4 文件）、`internal/metadata/password.go`（仅注释）
- 取证命令（实际执行）:
  - `go build ./...` → OK；`go vet ./internal/auth/... ./internal/audit/...` → OK
  - `go test -race -count=1 ./internal/auth/... ./internal/audit/...` → **ok auth 21.3s / ok audit 3.0s**（复现实现者自测）
  - 三组临时探针测试（`/tmp/t11probe`，跑完即删）针对 pathmatch 边界、Revoke 哨兵链、负 TTL —— 关键证据见下
- clean-room 抽查: 阅读反编译 `reverse-src/.../org/artifactory/util/PathMatcher.java` 与 `AuthorizationServiceBase.java` 做语义比对，**未发现逐行翻译嫌疑**（pathmatch.go 是独立的递归 segsMatch 实现，结构与 Spring AntPathMatcher 完全不同；authenticator 行为均出自 docs/reverse/auth-model.md 高置信条目）。合规。

---

## 必须修改（blocking，3 条）

### B-1 Revoke 的 not-found 哨兵违背自己导出的契约，T-15 无法对接

- 位置: `/Users/lzw/dev-center/internal/auth/api.go:122-128`（契约承诺）vs `internal/auth/token.go:126-139` + `internal/auth/deps.go:45-57` + `internal/auth/errors.go:9-10`（实现）
- 问题: `TokenRegistry.Revoke` 的导出 godoc 明确承诺 *"Unknown values return an error wrapping metadata.ErrTokenNotFound (the HTTP layer maps that to the idempotent "Token not found" 200)"*。但适配器把 `metadata.ErrTokenNotFound` 翻译成了 **auth 包私有镜像** `errTokenMissing`（`deps.go:49`），`Revoke` 再 wrap 这个镜像（`token.go:131`）。探针实测：

  ```
  double-revoke err chain: auth: revoke by value: auth: token not found
  errors.Is(metadata.ErrTokenNotFound) = false      ← 契约承诺为 true
  ```

  `RevokeByID` 同病（实测 `errors.Is(metadata.ErrTokenNotFound)=false`），而其 godoc 写的是 "same not-found semantics as Revoke"。auth-model.md §3.4-5（高置信）要求 revoke 幂等 200 "Token not found"，T-15 靠 `errors.Is` 判别 —— 当前形态下**判不出来**，只能再打一次库或匹配错误字符串。
- 加重情节: `internal/auth/token_test.go:105-108` 的断言是

  ```go
  if err := f.svc.Revoke(...); !errors.Is(err, metadata.ErrTokenNotFound) &&
      !strings.Contains(err.Error(), "not found") {
  ```

  `&&`+`||` 组合使 `errors.Is` 为 false 时靠字符串兜底**照样通过** —— 这个测试把契约破裂掩掉了。实现者日志 §五.2 自己承认「对上层不可见」，却与自家导出文档相矛盾，且没有按承诺「导出该哨兵（一行改动）」。
- 建议改法（一行级）: 在 `internal/auth/errors.go` 导出 `var ErrTokenNotFound = metadata.ErrTokenNotFound`（别名同一 error 实例），`deps.go`/`token.go` 直接 wrap 它；或让适配器透传 wrap `metadata.ErrTokenNotFound` 而非镜像。同步把 `token_test.go:105` 的断言改成**只认** `errors.Is(err, auth.ErrTokenNotFound)`，删掉字符串兜底。

### B-2 pathmatch 的目录裸前缀（matchStart）被无条件应用 —— 对文件路径系统性 over-grant（fail-open 相对规格）

- 位置: `/Users/lzw/dev-center/internal/auth/pathmatch.go:51-85`（`segsMatch` 的 `lastPlain` 规则）+ `internal/auth/authorizer.go:72-77`（调用点）
- 问题: 反编译侧（clean-room 语义比对）`PathMatcher.matches` 的 `useStartMatch` **仅当 `repoPath.isFolder()` 为真**才启用 `antPathMatcher.matchStart`；对文件路径只用 full match（`AuthorizationServiceBase.java:692`、`SecurityServiceImpl.java:793` 均传 `repoPath.isFolder()`）。BinFlow 的 `Can` 签名没有 folder 信息，实现选择了**对所有路径一律**应用目录前缀规则。探针实测（`lastPlain` 对任何「末段为纯字面量」的 pattern 生效）：

  ```
  match("a/*/c",            "a/b/c/d")          = true   （Ant full match = false）
  match("acme/artifact.bin","acme/artifact.bin/evil") = true（同上）
  match("**/release",       "x/release/inner")  = true   （同上）
  ```

  即：授权 `includes=["a/*/c"]` 的用户可以 deploy 到 `a/b/c/任意/深层/路径`，而 Artifactory 对该**文件**路径会拒绝。`auth-model.md` §4（高置信）的原文是「目录**命中**即覆盖其下尚未上传路径」——条件是目录命中，不是无条件。这是授权面比规格更宽的 fail-open 偏差，安全关键包里按从严处理。
- 边界确认（这些没问题）: `ci-out` 裸前缀不会 bleed 到 `ci-out-other/`、`ci-out2/`（探针实测 false，段级比较无部分段泄漏）；`*` 不跨 `/`；`**` 回溯正确；大小写敏感。
- 建议改法（三选一，需在报告回复中定案）:
  1. **最贴合上游**：给 `Can` 增加路径形态信息（如约定 `path` 以 `/` 结尾 = folder，正好对齐 repo 层 `isFolderNode` 的约定，`internal/repo/validate.go:126`）—— 仅对 folder 形态启用 `lastPlain` 规则。这不需要改接口签名，只需 godoc 写明约定并让 T-14 传参时保留尾斜杠。
  2. 收窄 `lastPlain` 规则仅作用于 `ActionRead`（folder list/read 是 matchStart 的主场景），write/delete 走 full match —— 不改签名，但与 folder delete 语义略有出入。
  3. 若 architect 认定 M1 接受该偏差，须在 `pathmatch.go` godoc + T-11.md 显式记录「比 Artifactory 宽」的决策，并补 `a/*/c` vs `a/b/c/d` 的固化测试（当前 28 行表里没有这条，`pathmatch_test.go:40` 只测了 `a/b/d/c` 不匹配的方向）。
  - 无论选哪条，请补上 `{"a/*/c", "a/b/c/d", ?}` 这一行，把预期钉死。

### B-3 「store 错误 fail-closed」是本票标题级 AC，但零测试覆盖

- 位置: `/Users/lzw/dev-center/internal/auth/authorizer.go:27-44`（两条 fail-closed 分支）；`internal/auth/authorizer_test.go`（全文件用真实 store，无失败注入）；`internal/auth/hygiene_test.go:38-41` 注释自认 *"its log path only fires on store errors, which the fixture store does not produce"*
- 问题: 「打错的权限表绝不能放行」是授权器最重要的安全属性，代码看起来是对的（两个错误分支都 `return false`），但**没有任何测试证明它**。同时该分支的 slog 输出（NFR-S3 声称已验证）也从未被真正触发过——`TestLogHygiene` 的风暴没有覆盖这条日志路径。
- 建议改法: `New(users, tokens, perms, anon)` 本来就接受小接口，写一个 `failingPerms` fake（`PrincipalsFor` 返回 error / `ListTargets` 返回 error）注入，断言：授权用户 + 已配置 target 的场景下 `Can` 返回 **false**（不是 panic、不是 true），且（可选用 slog handler 断言）日志只含 repo/user/action/error 四键。

---

## 建议改进（major，non-blocking，4 条）

### M-1 audit.Redact 脱敏键表缺协议实际使用的 camelCase 形态

- 位置: `/Users/lzw/dev-center/internal/audit/api.go:53-58`
- 问题: `credentialKeys` 只有 snake_case 小写键。而 auth-model.md §2.1（高置信）规定改密请求体字段是 `{userName, oldPassword, newPassword1, newPassword2}` —— **camelCase**。若 T-14/T-15 按协议字段名记 Detail（最自然的写法），`oldPassword`/`newPassword1` 不会被脱敏直落入库。同理缺 `Authorization`（首字母大写的 header 名）、`x-jfrog-art-api`/`api-key`（连字符形态）、`bearer`。Redact 是 NFR-S3 的最后防线，键表应当覆盖自家协议词汇。
- 建议: 键匹配改大小写不敏感（`strings.EqualFold` 遍历或归一 lower(key) 后查表），并补 `x-jfrog-art-api`、`api-key`、`bearer`。补 4 行测试（camelCase/header 形态各一）。

### M-2 ChangePassword 校验顺序与规格 §2.2 不一致

- 位置: `/Users/lzw/dev-center/internal/auth/token.go:153-159`
- 问题: 规格「按触发顺序」：旧口令校验在前，`new==old`、空新口令在后。BinFlow 的 switch 先判 `newPassword==""`/`old==new` 再验旧口令。后果：`ChangePassword(u, 错旧口令, "")` 返回 `ErrEmptyPassword`，规格应为 `Incorrect username/password`。两者都映射 400，但文案错配会让 T-15 的响应体对不上协议。同旧（新旧一致）时若旧口令本身错误，BinFlow 报 same、规格报 incorrect。
- 建议: 把旧口令校验挪到最前（用户存在 → 旧口令验证 → 空新 → 同旧）。

### M-3 Verify 热路径上每请求同步 Touch 写

- 位置: `/Users/lzw/dev-center/internal/auth/token.go:55`
- 问题: 每个带 token 的请求都对 SQLite 做一次 `UPDATE tokens SET last_used_at`。metadata 的连接池按其文档收敛为单写连接——所有认证请求会在写锁上串行化，成为吞吐瓶颈；只读副本/磁盘满时读路径行为也受写放大影响（错误虽被忽略）。
- 建议: M1 至少加节流（如距上次盖章 >60s 才写，需要读 `t.LastUsedAt` —— 已经查出来了，零额外成本）；或明确记录「接受该代价」并给 T-16 容量备注。

### M-4 query 语义边界：`match("**", "")`=true 与空 path 授权

- 位置: `/Users/lzw/dev-center/internal/auth/pathmatch.go:32-37`
- 问题: 空 path 对 `**`/`**/*`/空 pattern 放行（`{"**","",true}` 已是固化测试），对其它 pattern 拒绝。`Can(p, repo, "", r)` 对 granted 用户在有 includes 的 target 下会拒绝 —— 若 T-14 用空 path 表示 repo 根列表，行为在「includes 为空=全含」与「includes 非空」之间不一致。需要在 godoc 或 T-14 对接缝写明根路径的传入约定（建议约定根路径传 `""` 时按 includes 空表处理，或干脆禁止空 path 由调用方归一化）。

---

## 建议改进（minor / nit，6 条）

1. `internal/auth/api.go:36` — `ErrNoCredentials` 声明后**从未被返回**（nil,nil 承担语义）。死 API 会诱导调用方写 `errors.Is(err, ErrNoCredentials)` 分支然后永远走不到。建议删除，或让 `Authenticate` 在无凭据时真的返回它（更自文档化，但改语义需 T-14 认可）。二选一，别留现状。
2. `internal/audit/audit.go:81-92` — `mergeDetail` 在 Detail 为非法 JSON 且 RemoteAddr 非空时，`Unmarshal` 失败静默从空 map 起步，**原 payload 内容被丢弃**（只剩 remote_addr）。与其注释「merge must never drop the event」的意图相悖（事件没丢，但载荷丢了）。建议非法 Detail 时退化为 `{"_raw": "<原文>"}` 之类保底。
3. `internal/audit/audit.go:110-127` — Query 只有 limit，无 offset/keyset 分页；T-15 做审计页 UI 时会撞墙。M1 可接受，建议 godoc 标注 [M4+] 并在 T-15 对接缝提示。
4. `internal/audit/audit.go:48-50` — `New(st metadata.Store, ...)` 吃整个具体 Store，与 auth 的 consumer-side 小接口风格不一致（auth 的做法见 `internal/auth/deps.go`），迫使测试 fake 嵌入整个 `metadata.Store` 接口（`audit_test.go:122-124`）。风格问题，不阻塞。
5. `internal/audit/audit_test.go:116-120` — `TestBestEffortSwallowsErrors` 只验证不 panic，不断言「错误被吞且被记录」。建议用 slog handler 断言一条 error 日志写出（顺带验证不含 Detail）。
6. `internal/auth/token.go:77-95` — `Issue(ctx, user, -time.Hour)` 静默按永不过期处理（实测 `expires_in=0`）。规格 §3.1 要求负 `expires_in` → 400 `invalid_request`。auth 层不拦可以（HTTP 层职责），但建议 godoc 明示「负值同样视为 never，HTTP 层必须先校验」，给 T-15 立牌子。

---

## 测试与规格核对结论（AC 覆盖）

- 权限矩阵 19 行、pattern 28 行：断言质量总体**高**（每行具体期望，无 tautology；匿名开关 off 有差异化用例；主体一致性矩阵覆盖 CI-BOT/Ci-BoT/other/admin/空）。未发现「把 fail-open 当正确」的用例——但 B-2 的 over-match 方向 (`a/*/c` vs `a/b/c/d`) 恰好无覆盖，属于漏测而非测反。
- token 侧：sha256-only 落盘做了 db/-wal/-shm 全文件 grep（真取证，好评）；entropy、过期、吊销、owner-disabled 均有。唯一的问题是 B-1 指出的 double-revoke 断言用字符串兜底掩盖契约破裂。
- NFR-S3：错误消息不泄密测试真实有效；但见 B-3 —— fail-closed 的日志路径从未被触发。
- 复现确认：`go test -race -count=1 ./internal/auth/... ./internal/audit/...` 全绿（21.3s / 3.0s）。

## argon2 归置（技术正确性核查）

- 无 import cycle（`grep internal/auth internal/metadata/` 零命中，依赖单向 auth→metadata）；`password.go` 薄 re-export API 面干净（仅 HashPassword/VerifyPassword 两函数）；`internal/metadata/password.go` 实现一字未动（diff 只有 godoc 注释）。架构 §3.4 的字面偏离已记录并知会 architect。**技术层面无问题**，类目归属交 architect（T-25）。

## 范围外发现（交 conductor）

1. `go.sum` 处于 untracked 状态而 `go.mod` 已修改 —— 提交时必须一并纳入，否则 CI 不可复现。
2. `.golangci.yml` 新增 gosec 全局豁免（G401/G304/G204 等）—— 注释归于 storage/blobPath 场景，非本票产物；全局豁免 G401（弱哈希）值得 architect 复核是否应收窄到文件级。
3. B-2 若选「改 Can 约定/签名」路线涉及架构契约（§3.4 签名为并行开发解耦契约，不得单方修改），需 architect 介入定案。

---

### 结论

**REQUEST_CHANGES** — 3 blocking（B-1 契约破裂+测试掩蔽、B-2 ACL over-grant fail-open、B-3 fail-closed 零覆盖）/ 4 major / 6 minor。

B-1 是一行级修复；B-2 需要定案（建议路径 1：尾斜杠=folder 约定，零签名改动）；B-3 是一个 fake + 两个断言。修复量小，但都在安全面上，不应带病进 qa。
