# iteration-1550 · R11：checksum 族收口 + mime 单源收敛 + ADR-0053 + L040 探针批 + PR #185 勘误与补载；台账 Z=56（BUG 12→9）（2026-09-30）

一轮六票（BIN-63..68）全 Done；双审**首过 0+0**（连续第二轮零 blocking）。本轮主轴=修复+收敛：
R10 台账批立下的六条修复面全数翻正（3 flip 票 + 枚举门 + .sha512/SET 策略面 + L040 探针），
mime 四副本收敛单源（T-581），repo SPI 可选能力段定型 ADR-0053（T-582）。**轮内重大事件：
PR #185 stale-merge 勘误**——L040 白名单 B1 面（develop 树）零塌缩暴露 R10 载荷五提交不在
develop（建 PR 时分支头未推，gh 以远端旧头建 PR）；勘误落 iteration-1549（46ffbe70），
R10+R11 载荷随本轮修正 PR #186 一并补载，合并后账实对齐。台账 Z（open）56 持平：
6 flip resolved、6 新立（BUG +3 / UNKNOWN +3）、3 修订；**open BUG 12→9**（首轮净下降）。

## PR / 合并

| PR | 内容 | 状态 |
|---|---|---|
| [#186](https://github.com/0ldlight/binflow/pull/186) | R10 载荷补载（5 提交：T-578 SPI 缝 3254c887 / T-579 nuget 渲染族 b248be9d / ADR-0052 4fa01dda / L039 fc6fc0f5 / R10 台账批 bbfed66b）+ R10 战报 885b22e5 + R11 载荷（9 提交：ADR-0053 a37e559b / T-585 a2b4b359 / T-583 a3f053df / L040 b53c576a / T-581 26f7f781 / 勘误 46ffbe70 / T-584 e16034bb / 双审报告 bee83ca3 / R11 台账批 da195727）——共 15 提交 | MERGED（合并前后双闸：headRefOid=da195727×15 核对 + develop 树 SetClientChecksums/is-ancestor 复验） |

develop→main 保鲜：#184（R9）后累计 **15 done ≥10** → 闸触发条件已满，**R12 首查项=develop→main
保鲜 PR**（main=UAT 源须含 R10+R11 载荷）。

## 轮内裁定（conductor，台账四分类口径）

L040 候选（N1-N5）+ T-583/584/585 域外发现逐条落账：

1. **六条 flip resolved**（修复票全数兑现，双轮活体证据入台账）：C1 virtual 穿透、C3 终缀
   大小写、P8 双文案（T-583）；C4 .sha512 全路径普通部署、srvgen-oc SET 无条件化（T-584）；
   C6 枚举门（T-585）。
2. **N1/N5=BUG 新立**：maven 未路由 virtual sidecar 假成功（C1 maven 平面扩展）；generic
   sidecar 1024B 守卫（限值+文案已实证钉死）。
3. **oc 键集模型=BUG 新立**（T-584 五腿 live + T-583 facet a 合并）：A=client∪{sha256}，
   B FileInfo 全 triple 兜底/envelope 空集两面各自偏离。
4. **N3/N4/N2=三修订**：C2 收窄 sha256-only 主摘要回显；C5 文件名键控路由模型钉死；C7 分面
   坐实（maven PUT 已对齐，余 generic PUT/DELETE/sidecar-GET 三面）——三票 R12 联合裁定
   （BIN-74），UNKNOWN 限期余一程。
5. **域外 UNKNOWN ×3 新立**：maven sidecar GET xsha 头回显、repo-config CT 严格度、
   update-404 文案（T-583 facet b / T-585 Risks②③）。
6. **ADR-0052 Errata-2**（审 B NB-② handoff）：决策 6.3 括注「generic 无 policy 配置面」被
   T-584 GATE 35/35 超越——机制轴无恙，事实注记过时。

## 完成票（六票全 Done，Linear BIN-63..68）

| 票 | 提交 | 一句话 |
|---|---|---|
| BIN-63/T-581 mime 表族收敛 | 26f7f781 | 69 条表提升 internal/adapter/mimetable 单源（ByPath 唯一入口、stdlib-only）；四消费方薄再导出零调用点改动；字节平价四连 IDENTICAL + 快照守卫 |
| BIN-64/T-582 ADR-0053 | a37e559b | repo SPI 可选能力段定型（三判据/六存量段零迁移收编/命名后缀/resolver 单点）；ADR-0052 Errata-1 收编 |
| BIN-65/T-583 generic 拦截族 | a3f053df | virtual 写路由穿透（成员 key 全指）+ 终缀 case-insensitive + GET 未设双文案；27 腿活体双轮全 PASS |
| BIN-66/T-584 策略面收尾 | e16034bb | .sha512 全路径普通部署（GAV+非 GAV 双 201）+ SET 无条件化校验门保策略域（双 adapter）；GATE 35/35 ×2 |
| BIN-67/T-585 枚举门 | a2b4b359 | checksumPolicyType 闭集 {client-checksums,server-generated-checksums} + 逐字 400；19 腿双轮 + negative 9 例（Security 硬门） |
| BIN-68/T-586 L040 探针批 | b53c576a | 111+2 腿双面双轮：N1/N5 BUG 形、N3/N4/N2 修订分面；B1 零塌缩坐实 PR #185 载荷缺席（勘误触发） |

## 双审（首过 0+0，连续第二轮零 blocking）

- **Reviewer A（correctness）APPROVE**：0 blocking / 6 NB / 7 范围外。T-581 字节恒等独立复核。
  关键 NB：putSha512ChecksumFile 缺 srvgen declared-drop 角落（→ L041 探针腿，BIN-73）；非 GAV
  .sha512 DELETE 空 GAV 触发器全仓 no-op（无数据风险，watch）。
- **Reviewer B（architecture）APPROVE**：0 blocking / 6 NB / 4 范围外。依赖方向零违例、ADR-0053
  符合度逐项实测（六段行号全中）、勘误 git 事实三项独立复验属实。NB handoff：virtualDeploymentTarget
  第 7 份锁步拷贝收敛票（BIN-72）、ADR-0052 Errata-2（本轮已落）、oc 键集跟进票（BIN-71）、
  remote/virtual 枚举门 probe 候选（BIN-73）。
- **合并操作纪律（#185 事故后新立两道闸）**：建 PR 前推净+核范围空；合并前核 headRefOid==本地
  tip + 提交数；合并后 is-ancestor 抽验。#186 全程执行（15/15 提交核对后合并）。

## Z 对账（raw 实枚举口径）

R11 终态（conductor 枚举，python yaml 解析复核）：**total 127 = resolved 71 + open 56**
（BUG 9 / UNKNOWN 39 / INTENTIONAL 7 / UNSUPPORTED 1）。较 R10（121=65+56）：**+6 flip
resolved**（C1/C3/P8/C4/srvgen-oc/C6）、**+6 新立**（BUG +3：N1/N5/oc 键集；UNKNOWN +3：
xsha/CT/update-404）、3 修订（C2/C5/C7 模型钉死）；open 净 56→56 持平但**结构改善：BUG
12→9（净 -3）、UNKNOWN 36→39（+3 转裁定轨道）**。

open BUG 9 枚举——存量 5（docker/remote-cache-layout、rest/properties-root-posture、
search/snapshot-row-wildcard-match、search/badchecksum-maven-metadata-exclusion、
maven/bare-directory-get-400-vs-404）+ R11 新立 3（maven/checksum-put-unrouted-virtual-plane、
generic/checksum-put-body-size-guard、httpapi/original-checksums-key-model）+ storage/deploy-201-
itemcreated-envelope-family（池存续）。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（冻结行集零增删；本轮无新行）
- **Y**= 109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12，不变）
- **coverage**= 60.50%（181 适用行口径，不变）
- **Z（open 已知偏离）**= **56**（raw 实枚举口径，open BUG 9 构成见上节）

## R12 候选池（已注册 Linear Todo 六票 BIN-69..74）

- **BIN-69/T-587**（High）：maven 拦截臂过部署路由门（N1；顺带清理 writeSidecarDigest 不可达
  sha512 分支）。
- **BIN-70/T-588**（High）：generic sidecar 1024B 守卫 + 409 逐字（N5；兼收 io.ReadAll 无上限）。
- **BIN-71/T-589**（High）：originalChecksums 键集模型单源化（FileInfo+envelope 双面，A 口径）。
- **BIN-72/T-590**（Medium）：virtualDeploymentTarget 第 7 份锁步拷贝收敛（同 T-581 手术）。
- **BIN-73/T-591**（Medium）：探针批 L041（xsha 头面/remote·virtual 枚举门/repo-404 文案族/CT
  严格度/N6 遮蔽面/srvgen declared-drop 角落）。
- **BIN-74/T-592**（Medium）：C2/C5/C7 联合裁定（UNKNOWN 限期余一程，本轮必须出裁定）。
- 候选池（未立票）：审 A NB 残项（errors.Is 断言、nil 守卫对称化、adapter 级 SET 失败测试、
  writeServiceError 横切收敛——R10 起结转）；ADR-0052 Errata-1 行号漂移注记（审 B NB-④，按
  as-built 惯例不改，可不做）。

## 环境与安全复核

- A 实例（7.161.26 @ 192.168.120.38:8082）配置零触碰；difftest-* 前缀仓用后 DELETE 复验 0
  （T-583/584/585/586 四票均附 teardown 证据）；凭据经 /tmp env 注入零落盘零打印。
- L040 B 面=develop d420bbc7（B1）与载荷树 885b22e5（B2）双面取证；/tmp 探针资产留存备查。
- `.playwright-mcp/` 未入库；reverse-src/ 只读未触碰；BOARD.md 冻结只读维持；本地 Docker 未
  启用（B 面 scratch 构建为本地 go build，无 Docker 依赖）。

## Watch items（转 R12）

- **repo test flake**（R10 审 A 首跑 FAIL 未复现；R11 复审 build+四包 test 全绿）——再显影立
  flake 票。
- 非 GAV .sha512 DELETE 空 GAV 触发器全仓 no-op（审 A NB；无数据风险，留意 DELETE 语义票）。
- develop→main 保鲜（15 done ≥10 闸已满，R12 首查项执行）。
