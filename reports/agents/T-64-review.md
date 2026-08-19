# 评审报告 T-64（视角: correctness）

- ticket: T-64 [P0] repo.Service：三型仓库模型与配置校验（FR-15）+ PutLandedBlob 用例
- 评审对象: commit `63135de`（14 文件，+2228/−121）
- 结论: **APPROVE**
- 评审人: code-reviewer（单 reviewer，correctness 为主，PutLandedBlob 事务边界为关注点）
- 日期: 2026-08-19
- 备注: 评审期间工作区被 T-80/T-66 的在途未提交改动占用（internal/httpapi/*、internal/remote/*）。
  in-area 包（internal/repo、internal/adapter/docker）经 `git diff` 确认与 HEAD 一致、
  HEAD 对这两包 == 63135de；跨包验证（httpapi C26 声明）在 `git archive 63135de` 的
  /tmp 快照上执行，未触碰共享工作区。

## 必须修改（blocking）

无。

## 逐项核实（关注点 → 证据）

### 1. PutLandedBlob 事务边界 — 通过

- **顺序与原子性**（service.go:366-411 → putNode:477-536）：权限对
  （authorizeContentPut，Put/PutFromBlob/PutLandedBlob 三处共用，抽取正确、门在任何
  blob I/O 之前）→ O(1) Open 存在性/尺寸探针 → putNode 内 **blobs 台账行先落**
  （Blobs.Put ON CONFLICT DO NOTHING）→ node 行后落（nodes.sha256 FK 兜底）。
  与架构 §3.2/§3.3 blob-first 序一致；物理 blob 由调用方的 Commit 先行保证。
- **与 PutFromBlob 的差异面**：PutFromBlob 拒孤儿（ErrOrphanBlob，摘要以台账行为
  权威）；PutLandedBlob **写**台账行（会话三摘要，DO NOTHING 保既有行权威）。
  `TestPutLandedBlobVsPutFromBlobOrphan` 把同一前置状态在两方法上的相反行为钉成
  对照——契约差异被测试钉死，不是只写在注释里。
- **幂等重传分支跳过台账写**（putNode 早退）：node 存在且同 sha ⇒ FK 保证台账行
  已存在，无缺口。
- **物理缺失错误分类**：普通 error 不映射 ErrNodeNotFound（内部不一致不是客户端
  可寻址 404）——表驱动 `plain` 分支钉住。
- **docker finalize 切换**（git diff 核实）：registerBlobNode 删除
  `store.Open → svc.Put` 回读，直接 `svc.PutLandedBlob`；B4 失败语义（5xx 非 201 /
  denied 403 / busy 503+Retry-After）逐行保留；busy 注入点从 `busyOpenStore.Open`
  迁至 `busyLandedService.PutLandedBlob`，T-54 断言原样（TestWriteBlobCreatedBusyMaps503
  在本评审的全量 -race 跑中绿）。
- **O(size) 真删的机械钉板**：`failReadEngine`（Open 成功 / Read 恒败）上
  PutLandedBlob 成功、旧回读 workaround 必败——对比测试即债务可视化，真实有效。
- **并发**：`TestPutLandedBlobConcurrentSameDigest`（8 goroutine 同 digest 同 path）
  本评审 `-race -count=2` 复跑 PASS，收敛单 node；Blobs.Put DO NOTHING + Nodes.Put
  upsert 的收敛推理成立。

### 2. 三型校验矩阵 — 通过

- validate.go:31-35/63-76：local × {generic,docker,maven,npm,pypi}、
  remote/virtual × {generic,maven,npm,pypi}；唯二拒绝 remote+docker / virtual+docker
  → ErrRepoTypeNotSupported「not supported in M3 (docker is local-only; PRD Q4)」，
  与 FR-15-AC7 及 M05 实测文案一致。validateRepoType 在 config 解析之前（矩阵拒绝
  优先于字段拒绝，docker+remote 带 url 也答矩阵 400——测试钉住）。
- **remote 默认值落点**：7200/1800/15/300/false（config.go 常量 + 表驱动断言）；
  retrievalCachePeriodSecs → remote_configs.content_ttl_seconds（7200；DDL 86400
  仅 schema 兜底——ADR-0012 勘误二④「列名与数值对齐 PRD 字段名」）、metadata_ttl
  600 常量；missed/socket/assumed/hardFail 仅存 canonical JSON（003 无对应列）——
  与遗留②给 T-66 的接口说明一致，分布正确。显式 0=缺省、负值 400、未知字段丢弃。
- **password**：输入接受、canonical JSON 无此字段、remote_configs 行恒空串；
  GetRepo/ListReposFiltered 再过 maskRemoteConfig（直接注入 DB 的 password 也不回
  显，`TestGetRepoMasksInjectedPassword` 覆盖）——T-62 review 的防明文窗口提示已落实。

### 3. virtual 校验 — 通过

- 成员非空/存在/禁嵌套/禁重复/禁自引用（update 可达，单独测试）；defaultDeploymentRepo
  必须为成员中的 local（指向非成员或 remote 成员两分支各有用例）；三别名并收、
  冲突 400。与 FR-15-AC4、repo-semantics §8.2（写路由目标为 local 仓）对齐。
- SetMembers 声明序（position=声明序）断言；T-62 的 SetMembers 单事务原子换表
  （substores_remote_virtual.go:178-200 核实）承载「改序/缩员即时生效」；
  description-only 更新不动成员表有钉板。
- **crash 窗口自愈的真实性**：`TestRemoteUpdateHealsMissingConfigRow` 用 store 层
  DeleteConfig 造出「行有配置无」再经 UpdateRepo 走 UpdateConfig→ErrRemoteConfigNotFound
  （RowsAffected==0 路径核实）→CreateConfig 回落，测试真实非摆设。

### 4. DeleteRepo remote_cache 级联 — 通过

- 显式 DeleteCacheByRepo（行删前、计数入审计 removedCacheRows）；缓存行不要求
  deleteContent（仅负缓存行的 remote 仓可裸删并有测试）；remote_configs/virtual_members
  依赖 FK 级联、docker_refs 仍走行删后 DeleteRepoRefs——与 T-62/T-35 的
  DeleteRepoRefs/delete_by_repo 契约一致，无并行 teardown 重复。
- DeleteCacheByRepo 失败会阻塞删除（return error）：方向安全（行保留可重试），
  与「派生态不阻塞」的语义（存在缓存行不 demanding flag）不矛盾。

### 5. §5.6 反转的最小性 — 通过

- repo_test.go 矩阵 19 行翻新、docker_test.go 翻新、措辞断言收紧为
  "not supported in M3"；E-07 的 ErrRepoTypeNotSupported 泛化语义（remote/virtual
  内容面拒答）以 loadLocalRepo 措辞更新收口，无多余面。
- httpapi 在 63135de 零改动（grep 核实 0 处 wiring），全套件在快照上
  `go test -race ./internal/httpapi/` 绿（63.9s），C26 两子测 PASS——「无需改动
  仍全绿」声明属实。C26-virtual 目前是空转绿（400 来自 repositories-required
  而非 rclass 拒绝），实现日志已如实披露并归 T-80 翻转。

### 6. 遗留①（httpapi ~40 行接线）必要性 — 属实

- 63135de 的 httpapi 无 url/repositories 字段透传、无 ?type=/?packageType= 接线
  （0 grep hits）；T-64 area 声明为 internal/repo + internal/adapter/docker（仅
  finalize），分区规则下不可越界；conductor 已派 T-80（commit 8119ff8 + BOARD）。
  转交正当且必要。

### 7. clean-room 抽查 — 无嫌疑

- config.go 的字段集/默认值/别名与 docs/reverse/repo-semantics.md §7.1/§8.1/§8.2
  规格表及 PRD v1.2 C4 对应，是行为规格的 Go 建模（wrap error、table-driven、
  typed struct），非反编译代码的逐行翻译；无 reverse-src 引用。

## 建议改进（non-blocking，4 条）

1. **config.go:187-204 maskRemoteConfig 大小写敏感**：fast-path
   `strings.Contains(config, "password")` 与 `raw["password"]` 均只匹配小写拼写，
   手工注入 `{"Password":...}` 可绕过掩码。canonical 行任何拼写都不携带，且掩码
   注释自declare best-effort，故不 blocking；建议 T-66 动凭据链时改为大小写不敏感
   的键扫描。
2. **service.go:1286-1316 建仓 crash 窗口的恢复口径**：repositories 行已落而
   CreateConfig/SetMembers 失败时，**CreateRepo 重试**会答 ErrRepoExists，唯一自愈
   路径是 UpdateRepo 带 config（已测）；另两并发 config-carrying Update 在丢行态
   同时回落 CreateConfig 会有一方 UNIQUE 失败。M3 已接受（日志遗留 3），建议 T-66
   时代加启动一致性扫描时把「建仓重试 vs 更新自愈」写进用户文档口径。
3. **service.go:1552 审计 detail 的 configSet 判定**：`configSet && config != "{}"`
   使「合法地把 local config 更新为 `{}`」记为 configSet:false。审计精度瑕疵，
   建议直接用 configSet 布尔。
4. **api.go:240-260 PutLandedBlob 的分段位置**：放在 public use-case 段
   （PutFromBlob 之后）而非 adapter SPI 段。按 §11.13 它是内容面用例（PutFromBlob
   变体）、注释已说明 docker/maven/npm 为调用方，放置可辩护；但 T-63 确立的分段
   契约面向 M3 协议票收敛，建议 conductor 在 T-67/T-69/T-70 接线时确认它不被误
   当作协议编排方法的先例。

## 验证取证（本评审实际执行）

```
go vet ./internal/repo/ ./internal/adapter/docker/        # 通过
gofmt -l internal/repo internal/adapter/docker            # 0 文件
go test -race -count=2 ./internal/repo/ \
  -run 'TestPutLandedBlobConcurrentSameDigest|TestPutLandedBlobNeverReadsTheBytes|TestPutLandedBlobVsPutFromBlobOrphan'
  # 3 测试 ×2 轮全 PASS（10.4s）
go test -race -count=1 ./internal/repo/ ./internal/adapter/docker/
  # ok repo 154.7s / ok docker 99.0s
# 63135de 快照（/tmp，git archive，未触碰共享工作区）：
go build ./...                                            # BUILD-OK
go test -race -count=1 ./internal/httpapi/                # ok 63.9s
  -run TestRepositoriesCRUD -v                            # C26 两子测 PASS
```

## 范围外发现（交 conductor）

- 工作区当前有 T-80（httpapi wiring）/T-66（internal/remote）在途未提交改动，
  与本票无冲突；T-80 落地后需按日志遗留 1 翻转 compat_test.go C26 两用例
  （remote/virtual 建 200，docker 组合保 400）。
- T-66 接口口径（日志遗留 2）：missedRetrievalCachePeriodSecs 等四字段仅存
  repositories.config canonical JSON，fetcher 从 GetRepo(...).Config 读取——
  请在 T-66 派发时随票据带上。
