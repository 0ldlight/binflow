# 评审报告 T-66（视角: consistency — 架构一致性）

- reviewer: code-reviewer（架构视角；正确性另有在途 reviewer，本报告不重复其领域）
- 日期: 2026-08-20
- 输入: reports/agents/T-66.md、commit 05acd91 全量 diff、ADR-0012（含 T-79 勘误一/二）、architecture.md §2/§4.5/§5.1/§5.4、PRD v1.2（FR-15/FR-20/NFR-S13/S14/RE-04~06/RE-11/C4）、docs/reverse/{repo-semantics,rest-api,maven-npm-pypi}.md
- 结论: **REQUEST_CHANGES**（2 条 blocking，均为契约面真值问题、单点小改；实现本体架构面扎实，改完即过）

## 验证记录（实际执行）

```
go vet ./internal/remote/... ./internal/repo/... ./internal/adapter/generic/... ./internal/config/...   # 零告警
gofmt -l 同四包                                                                                       # 0 文件
go test -count=1 ./internal/remote/                                                                    # ok 17.1s
go test -count=1 -run "Remote" ./internal/repo/                                                        # ok
go test -count=1 ./internal/adapter/generic/                                                           # ok 10.3s
go list -f imports：remote → {adapter, metadata, storage}+stdlib（无 repo、无 config）                 # 依赖方向核实
```

## 1. ADR-0012 对照（含勘误口径）— 通过

- **六步序**：blocked_out → checksum 后缀（逐字文案，出处 docs/reverse/repo-semantics.md:128 / maven-npm-pypi.md:101，clean-room 通道合规）→ 负缓存 → 本地副本（双类 TTL）→ 回源矩阵 → 写面在 service 层拒绝。与 PRD FR-20 v1.1 六步定案逐条对应。
- **故障语义（勘误一）**：5xx/超时/传输 → assumed-offline 300s 静默 + 有副本（含过期）服务副本带 `X-Binflow-Upstream-Error` / 无副本 404（message 含 assumed offline）/ hardFail 仅改无副本分支为 502。`mapUpstreamStatus`/`mapTransportFault`/`downgrade` 实现与勘误逐字一致；SSRF 拒绝与 64MB 缓冲超限不开 offline 窗（爆炸半径正确）。
- **TTL**：content 7200（service 建仓默认）· metadata 600（repo/config.go defaultMetadataTTLSeconds）· negative 1800 / offline 300 / socket 15（remote/fetcher.go defaultPolicy）——对齐 PRD C4 与 repo-semantics §7.1。
- **凭据链（决策 4 / FR-15-AC9 / NFR-S14）**：`enc:v1:` + 随机 12B nonce + env base64-32B（坏值必炸）、有行无钥 fail-fast、明文一次性迁移、错钥拒启、日志/回显零明文。全链在场。
- **偏差一处（PRD 有据）**：ADR-0012 后果行「上游给 digest 头则强校验」→ 实现为 WARN 登记不拒。PRD v1.2 FR-20 行为规格明示「M3 只登记不拒，四值策略 M4」，PRD 为新定案，代码侧无责；文档侧回写见 §5。

## 2. 包边界与依赖方向 — 通过（附两条文档滞后）

- `internal/remote` import = adapter + metadata + storage + stdlib，**不 import repo**；`repo → remote` 为 §5.4 明文接线（Get 内分流）。
- `remote → adapter`：§5.4 明文「MetadataProvider 注册表在 adapter 包、service 层的 remote 缓存 TTL 分流消费它」——remote 即该消费方，合法且无环（adapter 基座只 import auth）。
- 滞后①：ADR-0012 后果行写 remote「依赖 config+storage+metadata」——实际无 config 依赖（env 自读，理由成立）、多了 adapter。下次架构触碰时改口径。
- 滞后②：§2 边界图未画 remote 节点的依赖边。随回写票补。
- **R6 核实**：`repo.Service` 接口方法集**零改动**（Get/Put/PutFromBlob/PutLandedBlob/Delete/List + CRUD + docker SPI 段原样）；新增为包级 `StatusError`/`NewStatusError`/`RemoteFetcher`（api.go SPI 段）。声明属实。
- **clean-room 抽查**：通过。逐字文案全部可溯源至 docs/reverse 规格行；实现为 Go 惯用结构（session/metadata store/singleflight），与 reverse-src Java 无逐行对应嫌疑。

## 3. 跨 area 两处申报 — 逐处裁决

### 3.1 internal/adapter/generic/handler.go（+~30 行）— **接受**

- 冻结的 Service 签名无法把 Code/Message/Header 送达 HTTP 面；备选（改签名 / httpapi 拦截）分别撞 R6 与破坏 adapter 错误信封归属。`writeServiceError` 顶部 `*repo.StatusError` 分支与既有 error-type switch 同构；`ExtraHeaders()` 结构化探测零 import、零仓库类知识，把 §5.4「adapter 对三型无感知」落成机械事实而非口头承诺。T-71（virtual 405 定案文案 + `X-BinFlow-Resolved-From`）复用价值真实。
- 附加要求（并入 blocking-2 的回写）：§5.4 补记两缝（措辞见 §5），防止 T-67/69/70 各自另开缝。
- 改进建议（non-blocking）：把「类型断言 + 头拷贝」提升为 `internal/adapter` 基座的助手（如 `adapter.CopyExtraHeaders(w, rc)`），避免四个协议 adapter 复制同一 10 行——基座只 import net/http，放得住。

### 3.2 internal/config/load.go + doc.go（+13 行）— **接受，必要**

- config 严格拒绝未知 `BINFLOW_*`（实测 exit 1），没有此放行 ADR-0012 指定 env 在真二进制上不可设置，FR-15-AC9 全链硬依赖。与 `BINFLOW_HOME` 同款「容忍+保留」先例，注释注明归属。
- 措辞 nit（non-blocking）：注释称「Like the admin password」——`BINFLOW_ADMIN_PASSWORD` 实际经 config 的 envSecret 分类进 `Config.AdminPassword` 字段（config.go:85/96），本变量是纯容忍+包自读，两种形态不同款；真正的机械先例是 BINFLOW_HOME。建议下一触碰时改注释为 HOME 类比，或在未来把两者统一（决策留 architect）。

## 4. 五个决策点裁决（架构视角）

| # | 决策点 | 裁决 | 理由 |
|---|---|---|---|
| 1 | panic 表达启动 fail-fast | **接受** | §5.4 规定引擎装配在 repo 分流内部（cmd/adapter 不可见）+ R6 冻结 New 签名 + §5.1 adapter.Register「装配期错误启动即炸」先例；构造器变体会留下「部分 repo.New 调用方无 remote」的装配分叉，更差。**条件**：panic 条款必须落在 api.go 的 New/NewWithClock 导出 godoc（现只在 service.go 未导出注释里）——并入 blocking-2。 |
| 2 | 无钥建仓丢弃 password + WARN | **接受（M3）** | 保住「未保护字节永不落盘」红线；拒绝会使 T-80 M02 无钥测试进程变红。保留意见：运维惊讶面（仓建成、凭据静默缺席、上游 401→404）——请 architect/PM 确认口径，tech-writer 在部署/用户文档写明「先设 env 再建带凭据 remote」。单点可翻（sealPassword）。 |
| 3 | original checksum 登记不拒 | **接受** | PRD v1.2 FR-20 行为规格明文（「M3 只登记不拒，四值策略 M4」），PRD 为产品定案、新于架构草案；代码注释已注明 supersession。**动作在文档侧**：§4.5:371 + ADR-0012 后果行回写（§5 给措辞）。 |
| 4 | 其它 4xx 不开 offline | **接受并背书** | 确定性协议应答 ≠连通性故障；offline 窗是整仓级静默，不能被单路径请求形状打开（爆炸半径最小化），且与 401/403 处理一致。§7.6 该行为中置信度、PRD 未列——BinFlow 从严合理，建议在 repo-semantics 侧记一笔 BinFlow 收紧。 |
| 5 | 负缓存与过期副本并存（窗口内次请求 404） | **接受** | 六步序字面（PRD RE-04/§7.2 步序即规范），已钉板、单点可翻（mapUpstreamStatus 404 分支）。可观察的翻转（本次 STALE 200、下次 404 而副本仍在）确属怪异但 spec-literal——请 architect 在 §7.2 精读时确认是否有意，M4 再归一。 |

## 5. 文档债回写措辞（挂 architect 票；T-66 报告只报了 §4.5:371 一处，实际 §4.5 有两处与实现相抵）

1. **§4.5:371**（checksum 强校验草案）改为：
   > 上游若给 `X-Checksum-*` 校验头则读为 original checksum 与实测值比对**登记**：不一致 WARN（含 declared/actual）**不拒绝**（PRD v1.2 定案：四值校验策略 M4 落地，M3 只登记；TLS + 落盘自算摘要为 M3 完整性基线）。实现：remote/fetcher.go land()。
2. **§4.5:374**（仍是勘误前旧口径「Warning: 111 / 无副本 502」）改为：
   > stale-while-error（勘误一口径）：上游 5xx/超时/传输失败 → 仓标记 assumed-offline（默认 300s，期内零上游流量）；有副本（含过期）→ 回吐 + `X-Binflow-Upstream-Error: <摘要>`；无副本 → 404（message 含 assumed offline）；`hardFail:true` → 502（仅无副本分支）。上游 404 → 写负缓存（默认 1800s）+ 过期副本仍回发（expired-but-serving）。
3. **§5.4 补记两缝**（StatusError / ExtraHeaders）：
   > 服务层渲染缝（T-66）：service 层 `repo.StatusError`（Code/Message/Header + Unwrap 哨兵）承载仓库类语义的精确客户端渲染，内容 adapter 的错误渲染入口对其原样渲染（Allow 等头随之）；`Service.Get` 返回的 reader 可结构化实现 `ExtraHeaders() http.Header`（remote 的 X-BinFlow-Cache / X-Binflow-Upstream-Error，T-71 的 X-BinFlow-Resolved-From 同缝），adapter 以类型断言探测——零 import、零类知识。T-67/69/70/71/72 复用此对缝，勿另开。
4. **§5.1 修订一行**：generic 的 RepoTypes 由 `{"local"}` 升 `{"local","remote"}`（对应 blocking-1 的代码修正）。
5. **顺手**：ADR-0012 后果行 remote 依赖清单改「adapter+storage+metadata（env 密钥自读，不经 config 树）」；§2 边界图补 remote 节点。

## 6. 接口面（T-71 / T-74 / RE-11）

- **FetchResult.HasCopy**：形态成立。「err==nil ⇒ HasCopy、真 miss 必 `*FetchError{Unfound}`」的不变量让字段冗余但自明；T-71 经 service.Get（StatusError + ErrNodeNotFound wrap）或 RemoteFetcher.Fetch 消费皆通，且 ADR-0013 联动记录的「stale 即成员结果、真 404 才续桶」映射干净（stale=成功；Unfound=续桶；502/400=中断传播）。
- **RemoteFetcher SPI**：Fetch/Invalidate/Forget 齐；**未含 Stats**。RE-11 REST（P2/M59）落票时需加宽本缝或在 api.go 的 adapter SPI 段加只读访问点——不能加 Service 公共面方法（R6）。记入该票前置，非本票缺陷（AC③ 允许延后）。
- **幂等/装配**：Engine 并发安全（mu + atomic），per-repo client 签名重建 + CloseIdleConnections，Forget 清理三张表。生命周期闭合。

## 必须修改（blocking）

1. **internal/adapter/generic/handler.go:37** — `RepoTypes()` 仍声明 `{"local"}`（注释「M1 generic serves local repos」），而 generic 内容面**已经**在服务 remote 仓（本票自带的 remote_render_test.go 即证据；M41~M48 全走 generic-remote）。§5.1 定位 RepoTypes 为「声明本协议可服务的仓库 class」的元数据、§5.4 明文「class 开放时升级 RepoTypes」——声明与行为相抵。→ 改 `return []string{repo.TypeLocal, repo.TypeRemote}` + 注释一句（virtual 待 T-71）。无测试钉死现值，零风险；与 §5.1 文档修订（§5-措辞 4）配套。
2. **internal/repo/api.go:218-220（Service.Get 契约注释）+ api.go New/NewWithClock godoc** — 契约标记文件未反映本次契约变化：Get 注释仍只写 local 语义，未提 remote 分派（读门先行）、`*StatusError` 渲染、返回 reader 可携带 ExtraHints；导出构造器 New/NewWithClock 未声明 panic 条款（决策点 1 的接受条件；现只在 service.go 未导出注释）。T-71/T-74/httpapi 即将按此文件编程。→ 各补一段 godoc（约 8 行，零行为改动）。

## 建议改进（non-blocking，6 条）

1. §3.1 的 `adapter.CopyExtraHeaders` 助手提升，防四协议复制漂移。
2. §3.2 的 config 注释措辞（HOME 先例而非 admin-password 类比）。
3. **单飞等待者不复查负缓存**：attempt 的 again-loop 只复查正副本与 offline 窗，不复查 winner 刚写的负缓存行——并发打不存在路径时 N 个等待者各自回源一次（负缓存只保护其后的请求）。PRD 把等待者复用列为 P1，故不 block；建议 loop 顶部补一步 GetCache negative-fresh 检查（单点），并抄送正确性 reviewer。
4. 决策点 2 的运维确认 + 用户文档（tech-writer）。
5. 决策点 5 的 architect 确认（§7.2 步序意图）。
6. RemoteFetcher/Stats 加宽预案记入 RE-11 P2 票前置。

## 范围外发现（交 conductor）

- **httpapi 无 StatusError 分支**（storage.go:400 / repositories.go:422 仍按 ErrRepoTypeNotSupported 旧映射）：REST 面 PUT/DELETE remote 仓将得到 400 形而非 405+Allow / 204。RE-05 的 405 契约定义在内容面（RE-05 = `PUT /binflow/<remote>/<path>`），不构成本票 AC 失败，但两面将不一致直至 httpapi 补同一分支（1 个 case，同 generic 模式）——建议并入 T-80 或开小票。
- 部署文档（release-engineer/tech-writer）需登记 `BINFLOW_REMOTE_CREDENTIALS_KEY` 的设置时机（先于建带凭据 remote）与 fail-fast 行为。
