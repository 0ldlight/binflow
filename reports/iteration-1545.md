# iteration-1545 · R6：D-3 级联修复 + maven deploy 四面 + L034 六面差分 + Location 轮内闭环 + 台账 Z=42（2026-09-29）

轮次目标：R5 首查项（五 lane CI 首跑观察）落位与红灯即时修复、D-3 裁定兑现
（T-555 非空仓 DELETE 200 静默级联）、R5 登记的四张 maven 提案面孔修复
（T-559）、envelope uri 前缀（T-561）、L034 差分六面验证（T-560）与轮内
Location 新面孔闭环（T-563）、台账收官批（8 resolved + 5 契约 VERIFIED +
4 金样组 + Z 口径统一）。

## PR 与合并记录

| PR | 内容 | 状态 |
|---|---|---|
| [#172](https://github.com/0ldlight/binflow/pull/172) | T-557 main 红灯修复（flaky map 序 + CI check 命名）→ develop | MERGED（轮首） |
| [#173](https://github.com/0ldlight/binflow/pull/173) | develop→main 保鲜（即时正当：修复须进 main 才能观察复跑） | MERGED |
| [#174](https://github.com/0ldlight/binflow/pull/174) | R6 载荷（9 提交：T-555 / T-558 / T-559 / T-561 / T-563 / L034 批 + mini-diff / 台账收官 + 评审报告集）→ develop | MERGED 05:15:56Z（8134b9f8） |
| [#175](https://github.com/0ldlight/binflow/pull/175) | develop→main 保鲜（≥1 天 + 7 票协议面密集，双触发成立） | MERGED 05:16:26Z（b013be5e，main push 五 lane CI 已触发=T-557 复跑观察面） |

双审（#174，两 reviewer 首轮即过，conductor 未自审）：Reviewer A（correctness）
**APPROVE**（0 blocking / 4 non-blocking：409 oracle 未观测面 / sidecar 入门分
类未观测 / ClassReader seam 注释漂移 / count 与实删非原子）——四维复跑全绿：
DeleteRepo 级联顺序未动（部分失败语义=既有 deleteContent=true 路径；并发双删
败者 404 无害）、时戳链零误 304、前缀拼接先转义后拼、409 模板双腿逐字节、
测试真实咬合（-count=2/3 稳、-race 干净）。Reviewer B（architecture）
**APPROVE**（0 blocking / 3 non-blocking / 4 范围外交办）：契约↔代码互证 3/3、
台账 8 条 evidence 链闭环、ErrRepoNotEmpty 全仓清零、195 冻结行集不变、DELETE
语义变更无行为性破坏面。报告：reports/agents/R6-pr174-review-a.md / -b.md
（07b98147）。

## 完成票（Linear 状态已同步）

| 票 | Linear | 内容 | 提交 |
|---|---|---|---|
| T-557 | BIN-39 ✅Done | main 红灯：deploy_refusal_family_test 建仓 map 随机序 flaky（-race -count=80 抽中 5 次，签名与 CI 逐字一致）→ 依赖序切片；ci.yml test job 显式 `name: test (<lane>)` | #172/#173 |
| T-555 | BIN-37 ✅Done | D-3 兑现：非空仓 DELETE → 200 静默级联 `{repoKey,statusMsg,deletedArtifactsCount,success}`；计数=文件+folder 全量根不计（活体定准 6/3/0/0 四臂）；deleteContent 旗冗余同形；ErrRepoNotEmpty 哨兵全仓删除 | 6812f271 |
| T-558 | BIN-40 ✅Done | 契约批：maven-virtual.yaml 4 DIVERGENT + ⑨ auto-materialize；repositories.yaml NEW（DELETE 级联契约）；known-divergence 4 UNKNOWN→BUG + 1 新 UNKNOWN | f61c321b |
| T-559 | BIN-41 ✅Done | maven 四面：ME-08 409 文案族逐字 A 模板（PUT 409 先于存在性、共享渲染器）+ 成员 GET class 门同族文案；writeDerivedSidecar 输入时戳（member=存储节点戳 / virtual=newestDocTime） | 531ea002 |
| T-561 | BIN-42 ✅Done | 201 envelope uri/downloadUri 携 `/binflow` context 前缀（put.go `productPrefix` 常量——适配器见前缀剥离后的请求，前缀必在渲染点回填；与 httpapi 同名常量双拷互注） | 23b70a82 |
| T-563 | BIN-45 ✅Done | 201 Location 头携前缀（checksum-file 注册式 → 前缀 TARGET；writeCreated Location==envelope uri 同源）——L034 轮内新账，修复+mini 差分同轮收口 | 4481359f |
| T-560 | BIN-43 ✅Done | L034 差分六面双轮 PASS×2 + Location mini-diff 六臂 r3≡r4 + forensics（喂 T-562） | 784241b6 / 601b15ac |
| （台账批） | 随各票 | 8 resolved + 5 契约 VERIFIED（⑧ confidence medium→high——A 跨秒直接腿实测 1.3s 304×3）+ 4 金样组 + D02-R05 回填 | 434a1207 |

## L034 差分验证（reports/compatibility/L034-r6.md）

- **六面双端两轮 PASS（r1≡r2）**：DELETE 级联四臂（nested 6 / flag 3 冗余 /
  empty 0 / virtual 0+成员存活，计数口径双端活体对齐）、409 措辞族（含 634 字
  符长路径——**A 无服务端截断**，300 上限系 L033 harness 截断伪象）、成员 GET
  class 门、plain-SNAPSHOT 存储面、sidecar 跨秒戳、envelope uri 前缀四臂。
- **Location 轮内闭环（R5 先例复用）**：L034 Arm 8 发现 B 裸根新面孔 → 当轮
  立账+立票 T-563 → 修复 → mini 差分六臂（含 X-Checksum-Deploy 零传输与
  checksum-file 注册式——rest-api §1.5 该面首个双端活体锚）r3≡r4 → 台账二次
  resume 翻 resolved。
- **T-562 forensics（喂票不修）**：unique 家园 plain-GET 选择=目录最大时戳
  walk，与 metadata `<snapshot>` 指针**解耦**（指针→ts4 但服务 20260929 重写
  版本）；非 unique 家园无 resolve。walk 族红半边按设计维持（T-562/BIN-44 规
  格票在池）。
- raw note 不裁：A sidecar LM 滞后 body 2s vs B 相等（值级残差，契约
  unobserved 记录）。

## Z 口径统一与对账（本轮诚实修正）

R5 战报记 Z=44（open 44 + gated 4 单列）；对 R5 收口文件实态实枚举为 **open
48**（BUG 7 / UNKNOWN 35 / INTENTIONAL **5** / UNSUPPORTED 1）——差异=4 条
INTENTIONAL「resolved-by-parity 闭态候选」在 R5 报告单列 gated、实文件无
resolved 块。**R6 起统一口径：open = 无 resolved 块的条目（python 实枚举），
不再单列 gated。**

R6 终态（枚举=台账 434a1207 逐位吻合）：**total 87 = resolved 45 + open 42**
（BUG 5 / UNKNOWN 31 / INTENTIONAL 5 / UNSUPPORTED 1）。较 R5 实枚举基线
48：净收敛 **−6**（BUG −2 / UNKNOWN −4，全为真收口）；当轮新增 2 条
（auto-materialize UNKNOWN open / Location BUG 当轮注册当轮收口）、翻
resolved 8 条（4 条 UNKNOWN→BUG→resolved 归并计入）。口径统一后两轮数字可
直接比：48 → 42。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（本批零新行；195 冻结行集零增删、零行态变更）
- **Y**= 109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12，不变）
- **coverage**= 60.50%（181 适用行口径，不变）
- **Z（open 已知偏离）**= **42**（raw 实枚举口径，见上节；open BUG 5：
  docker/remote-cache-layout、rest/properties-root-posture、
  search/snapshot-row-wildcard-match、search/badchecksum-maven-metadata-
  exclusion、maven/bare-directory-get-400-vs-404）
- 金样：4 新组（rest/repo-delete-report-body、maven/handle-policy-409-
  wording-family、maven/deploy-201-envelope-uri-location、
  maven/derived-sidecar-crosssec-304）；golden_ref_non_null 2→3、
  last_difftest_non_null 51→52。

## 诚实失败与教训

- **grep 管道退出码陷阱（conductor 自身）**：首哨兵批 `go test | grep -v
  '^ok'` 形态报告 TEST_EXIT=1——`$?` 是 grep 的（全滤空=exit 1），FAIL 行本会
  穿过滤器打印、无打印即全绿。教训：退出码必须显式捕获（`rc=$?` 后再 grep
  统计），管道尾是过滤器时 exit code 语义已换主。严谨复跑确认 GO_TEST_EXIT=0。
- **Z 口径计数漂移（R5 上报误差，本轮修正）**：R5 战报「44 open + 4 gated」
  与文件实态 48 的差是分类口径混用——本轮以实枚举为唯一口径并回溯修正基线。
  教训：跨轮对比指标必须同一脚本同一键位复算，报告数字要可由文件重derive。
- **t563 执行中自回滚事故**：agent 中途 `git checkout -- put.go` 误删自己的
  修复（perl 行号 mishap）——Edit 重放修复并全门重跑+变异自证，收编时逐文件
  对 git status 验证干净。教训（三铁律之外的补充）：票面交付前 collection 清
  单必须含「修复文件 diff 与票面声称一致」逐块核对。
- **mini-diff r3 首跑 mismatch 是 CASE 侧**：case 错误 pin 了 metadata 字节
  （T-561 口径只 pin 200；A 回显上传字节 vs B 重算=已知非面）——修 case 不修
  产品。教训：断言 pin 范围越界的红不算产品红，先归因后动刀。
- **ListAgents 视野缺口**：已完成的匿名后台 agent 不再出现在 ListAgents——
  后续微任务改派新 compatibility-engineer 实例；具名 agent 仍可 SendMessage
  resume（t560-l034 两程、r6c-ledger 两程均 resume 完成）。
- **波次纪律兑现**：render-point grep 先行（T-561/T-563 与 t559 同包
  adapter/maven → 串行）；「agent 运行中只提交无触碰路径」（L034 mini-diff
  与 r6c-ledger 并行时按不相交路径提交）。

## 哨兵证据（收编后全量，终态树）

- `go build ./...` exit=0；`go test ./... -count=1` **GO_TEST_EXIT=0，39 包全
  ok**（显式 rc 捕获复跑；首批 grep 形态 TEST_EXIT=1 证伪为过滤器伪信号）
- golangci-lint 0 issues；tsc --noEmit exit=0（本载荷无 web 产品码触碰）
- make spec-check PASS（spec 与生成器同步）；make fern-en-ratchet PASS（en=10）
- Reviewer A 独立复跑：adapter/maven ok 24.4s、httpapi 五测全 PASS、repo 四测
  全 PASS、-race 两批 ok（16.3s / 15.7s）；8 条 resolved 的 fix_ref 提交均在
  分支祖先链
- 双审凭据扫描：零字面量（评审报告无凭据面）

## R7 候选池

- **T-562/BIN-44 [P1]**：plain-SNAPSHOT walk 族（forensics 证据已备——目录最
  大时戳 walk 与 `<snapshot>` 指针解耦；规格票→裁对齐/维持→实现）。
- **BIN-46/T-564**：generic adapter 201 Location/uri 前缀泛化面（Reviewer A/B
  双双点名 internal/adapter/generic/handler.go:245、iteminfo.go:63-64——与
  T-561/T-563 同根因）。
- **BIN-47/T-565**：DELETE 语义消费面清尾（web 控制台文案 + docs/design 三处
  400 陈述 + fern repositories.mdx:110 / OpenAPI 生成器 DELETE 描述——R6 收尾
  README/fern 检查追加）。
- **auto-materialize UNKNOWN**：deploy-put-version-metadata-auto-materialize
  review_gate（取证→裁对齐/维持）。
- 观察（被动）：#175 main push 五 lane CI（T-557 修复复跑验证 + test
  (lane) 新命名落位）。
- 携带：跨秒 LM 值级残差 stored-stamp 升级微票（review_gate 已预写）；
  timestamp-spelling 臂；MySQL BIN-13；T-525/BIN-12；T-536 剩余随访；R5 B 审
  两条 non-blocking（契约头 changelog 陈旧、worktree 版本锚 repoint）。

## 用户待办（无变化）

B3 CircleCI nightly trigger（03:17 UTC, main）；部署轮次时 `uat_approval` 点
击；T-473；Jenkins 岛 VM .131 开机。
