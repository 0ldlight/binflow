# T-294 评审报告（视角: correctness + auth 面回归 + 偏差复核）

- 评审人：code-reviewer；日期：2026-08-26；票据：T-294（Cargo adapter：sparse 索引 + crates API local 全量）
- 输入：`reports/agents/T-294.md`、`docs/reverse/cargo.md`、`reports/agents/tl-fr91-ac3.md` §3、工作树全部改动（`internal/adapter/cargo/` 17 文件、`cmd/binflow-server/main.go`、`internal/auth/authenticator.go`、`internal/auth/auth_test.go`）
- 结论：**REQUEST_CHANGES**（1 blocker）

## 评审取证（实际执行的命令）

| 命令 | 结果 |
|---|---|
| `go build ./...` / `go vet`（cargo/auth/cmd 三点） | OK / 0 |
| `gofmt -l`（三个改动面） | 空 |
| `go test ./internal/adapter/cargo/ ./internal/auth/ -count=1` | ok（5.4s / 23.5s） |
| `go test ./... -count=1`（全仓） | exit=0，无 FAIL |
| `go test ./internal/adapter/cargo/ -count=1 -race` | ok（32.9s，0 race） |
| `go test ./internal/auth/ -count=1 -race` | ok（67.9s，0 race） |
| 并发取证探针（临时测试文件，跑后已删） | **复现 blocker B1**：8 个并发 publish 后索引文件仅剩 2 行 |
| 独立真机探针（/tmp 起最小 registry + cargo 1.98.0） | **D-1 实证成立**：`"errors":[]` 空数组 → cargo exit 101 "the remote server responded with an error:"；warnings-only 与 `{}` → exit 0 |
| `scripts/m10-tier-matrix.baseline` 全文核对 | 仅 T01–T06（generic/npm/nuget/go/license/addons），**无 cargo 行**——"无翻格"声明属实 |
| `git status` 核对 | 仅 main.go / internal/auth / internal/adapter/cargo / 本日志改动——"零分支改动 httpapi router 与其它 adapter"声明属实 |

未重跑 `make test-m10-invariant` / `test-m10-matrix`（起真实实例的脚本，重）；以 baseline 文件核对 + 全仓测试绿替代，特此声明。

## 必须修改（blocking）

### B1 — 索引整文件重写存在丢失更新竞争，并发 publish/yank 会永久丢行

- **位置**：`internal/adapter/cargo/index.go:182-225`（rewriteIndex：List → 组装 → Put，无任何同步）；触发点 `publish.go:245`、`yank.go:43`
- **问题**：同一 crate 的两个并发写请求（publish×2、publish+yank、yank+unyank）各自 List 全版本、组装后整文件 Put，last-write-wins。后写者的快照不含先写者刚落的版本/布尔翻转，该行**永久丢失**（.crate 仍在、下载可用，但索引不列出 → `cargo add`/解析不可见，直到该 crate 下一次任何 publish/yank 触发重写）。违反 AC 3"一行一版本、同 name+vers 恒一行"不变量与官方索引唯一性 must。
- **取证**：评审临时探针（8 goroutine 并发 publish 同 crate 的 8 个版本）多轮复现，一次捕获 `index rows = 2, want 8`。概率低（约 1/15~1/60 次运行），但 CI 农场并行发布是现实负载，且后果是静默数据不一致。
- **建议改法**：`rewriteIndex` 的"List→组装→Put"临界区按 `(repoKey, crateName)` 加进程内互斥（`map[string]*sync.Mutex` 带引用计数，或 `singleflight.Group` 串行化；cargo adapter 是 index/* 的唯一写者，进程内锁在当前单进程架构下充分）。或乐观路线：写后重 List 校验行数、不一致则重算重试。**随修复补并发回归测试**（N 并发 publish → 索引 N 行）——现有测试面全部是顺序用例，恰是本缺陷漏网的原因。

## 建议改进（non-blocking）

- **M1** `internal/auth/authenticator.go:572-579` + `:617-623`：无空格但含 **tab** 的 Authorization 值（如 `foo\tbar`、`Bearer\t<tok>`）改动前被 basicAuth 判 malformed 401，改动后三臂皆不认（bareTokenHeader 排除 `\t`）、**静默降级匿名**。不可利用（匿名 ≤ 任何凭据），但漂移了 presented-but-rejected 姿态。建议 bareTokenHeader 对"到达该臂的任何非空值"一律按裸 token 走 Verify（失败即拒），并补表驱动一行。
- **M2** `internal/adapter/cargo/client_e2e_test.go:48-50`：注释称 cargo 认证形态是 `Authorization: Bearer <token>`——与本票 §5 的探针结论（裸 token，无 scheme）相反，也是 auth 改动的立论依据。纯注释错误，改掉以免误导后来者。
- **M3** `publish.go:197-208`：409 重复检查是 List-then-Put，存在 TOCTOU 窗口——并发同版本重传时，同 sha256 走 svc.Put 幂等通道 200；持 DELETE 权限者可经 svc.Put 覆写臂绕过 409。顺序路径（真实客户端命中面）行为正确，登记即可。
- **M4** `publish.go:48,90`：`maxFrameLen = 1<<40` 大于 uint32 上限，永不可触发（死上界）。顺手改成 `1<<32` 或删掉留注释。
- **M5** `handler.go:272`：serveBareContent 的 `index/` 前缀拒绝是死防御——parseRoute 把一切 index/* 形态先行消费（合法→kindIndexFile 403 臂，非法→404），bare 面只见得到 `.cargo/` 活检查。无害，加一行注释说明即可。
- **M6** `yank.go:35-46` / `publish.go:224-251`：属性翻转成功而索引重写失败 → 客户端收 500 但状态已变（索引留旧值，下次重写自愈）；publish 落 crate 后 sidecar/重写失败 → cargo 重试撞 409（文案准确但体验困惑）。非事务多写的固有窗口，规格的异步索引器同样存在，登记不阻塞。
- **M7** `handler.go:143-154`：kindRoot 跳过 classOf，remote/virtual cargo 仓的根探针也答 200（其余面 404）。纯一致性瑕疵。
- **M8** search `per_page=0`/垃圾回退默认 10（crates.io 答 400）；规格未定义，可留。
- **M9** dev 日志 §6.2 登记的"仅持 write 不持 read 的 principal 在 List 步 403"复核属实（`repo/service.go:1314` allow(Read)）；dup 检查处经 writeError 映射 403、重写处映射 500，均不落脏数据。维持工单驱动。

## 七条偏差逐条复核（§4）

| # | 复核结论 | 依据 |
|---|---|---|
| D-1 | **成立（已独立实证）** | 评审方真机探针（cargo 1.98.0）：200+`"errors":[]` → exit 101 失败；200+warnings-only / `{}` → exit 0。dev 的五形态矩阵可信，规格 §2 草图确需修订，R-1 建议应随批 |
| D-2 | 成立 | CG-2 裁决文本是"统一 4xx/5xx + 信封、禁 200+errors"，400/500 拆分在伞内且 HTTP 语义更准；建议在 AC 记录补注，避免后续按 AC 字面（"解帧→500"）误判回归 |
| D-3 | 成立 | CG-3 定案 409；覆盖臂与官方唯一性 must 冲突。附 M3 竞态窗口注记 |
| D-4 | 成立（但引出 B1） | 官方允许索引延迟、零延迟是上界；同步化把重写竞争引入请求路径——D-4 本身没错，B1 是实现缺口不是路线错 |
| D-5 | 成立 | DB-3 姿态延伸合理，回填路径有测试（TestIndexLineImportBackfill）；`index/` 半为死防御（M5） |
| D-6 | 成立 | 全局匿名策略由 router `authorize` 中间件落地，TestConfigJSONAnonymousOff 经真栈验证裸取 401/带凭据 200+auth-required，与 AC 1a 一致 |
| D-7 | 成立 | RepoTypes=[local] 声明式诚实，TestRemoteVirtualRefused 覆盖 |

## internal/auth 裸 token 臂回归风险评估（重点项）

1. **臂序与互斥**：Basic → X-JFrog-Art-Api/X-Api-Key → Bearer(+OIDC 回退) → **bare（新增，无空格值）** → session cookie → 匿名。bareTokenHeader 只收"无空格无 tab"的值，与带 scheme 的两臂判据（按空格 Cut）天然互斥；`Digest xyz` 类未知 scheme 行为不变（落 api-key 臂或匿名，与旧表一致）。
2. **presented-but-rejected 姿态**：裸垃圾值 → `verifier.Verify` 拒绝 → ErrInvalidCredentials → 401，**绝不降匿名**（表驱动两行新用例 + 复跑绿）。唯一漂移是 M1 的 tab 值（降匿名，不可利用）。
3. **时序攻击面**：Verify = sha256 后按哈希查库（`token.go:32-75`），无逐字节明文比较，与既有 api-key/Basic-token-duality 臂同款；bare 臂不触发 argon2（无新 DoS 面）。
4. **既有端点语义**：nuget（Basic）、docker/npm/pypi/mvn（Bearer/Basic/无）均先于 bare 臂命中，互不影响；全仓 28 包测试 + auth/cargo 双包 -race 0 失败佐证。
5. **面加宽登记**：所有端点自此接受裸 token 形态——纯加法、语义安全，dev 的显著登记恰当。**建议 conductor 据此补 ADR 或并入 K-3 认证面登记**（dev 已提，评审背书该建议）。

## 门控接缝与 clean-room

- main.go 挂载点状属实（Register 一行 + Adapters 字面量追加，同 goproxy/nuget 模式）；`internal/httpapi`、`internal/addons`、其它 adapter 零改动（git status 核对）。三缝（gateAddonWrite / D3 建仓 overlay / RequireAddon）走既有泛型实现，cargo 槽位 T-282 先落。
- matrix baseline 无 cargo 行属实（T01–T06 全文核对）；cargo 门控行为由 `gate_test.go` 全链双测覆盖（D3 社区 400 点名 cargo/pro；pro 真文档装载→建仓+publish 200；卸载→D2 403+`X-Binflow-License-Required: cargo`+D1 读持续+D3 回 400），与 T-285/T-287 先例同构。
- **clean-room**：Go 实现与 reverse-src 无逐行对应（分层结构完全不同：handler/layout/publish/index vs Java SubResource/Indexer/MetadataExtractor）；仅协议 wire 常量（git 弃用文案、"unable to download crate"）逐字一致，二者均为 `docs/reverse/cargo.md` §1/§2 已固化的对外契约字符串，属必须复现的行为，非代码翻译。合规。

## 测试覆盖评价

解帧 table（合法/截断/错位/超限/尾随/空 meta 帧）、SemVer 解析与排序、四档路径与名字校验、索引行 schema 归一、契约 15 例（探针/config 两态+禁匿名/publish 全链+cksum 对账/409/失败族/固定 404 体/ETag/304/yank 翻转+404+keep-serving/search/未知路径/git 面/remote-virtual 拒绝/bare 面 403/匿名写 401）、门控 2 例、真机 e2e 门控 1 例——面与深度均达标。唯一实质缺口即 B1 的并发场景（无任何并发 publish/yank 用例），随修复补。

## 结论

**REQUEST_CHANGES**：修 B1（rewriteIndex 丢失更新 + 并发回归测试）后可再审；M1/M2 建议顺手带上。其余面（auth 加宽、七偏差、门控接缝、clean-room、测试深度）复核通过。
