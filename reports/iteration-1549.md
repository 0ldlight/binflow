# iteration-1549 · R10：client-checksum 持久化 SPI（ADR-0052）+ generic/maven 全模型翻正 + nuget 渲染族收口 + L039 相邻探针批 + 台账 Z=56（2026-09-30）

一轮三票（BIN-60..62）+ ADR-0052 落地；双审**首过 0+0**（本轮零 blocking 轮，R9 曾双
REQUEST_CHANGES）。R9 移交的 SPI 缝票 BIN-60 首位执行：SetClientChecksums 持久化缝 +
OriginalChecksums 渲染单源，generic/maven 五条 BUG 全模型翻正；nuget envelope 家族三票
收口（byte 级钉测）；L039 相邻探针批显影 7 候选**全数落账**（含审 A 范围外 srvgen-oc
补遗——台账完整性原则：双轮钉住的分歧必须入账）。台账 Z（open）54→**56**：5 flip
resolved、7 新立（BUG +4 / UNKNOWN +3）、2 修订。

## PR / 合并

| PR | 内容 | 状态 |
|---|---|---|
| [#185](https://github.com/0ldlight/binflow/pull/185) | R10 载荷（5 提交：T-578 SPI 缝+翻正 3254c887 / T-579 nuget 渲染族 b248be9d / ADR-0052 4fa01dda / L039 fc6fc0f5 / 台账批 bbfed66b；R9 战报 d9bae6b4 搭车）→ develop | MERGED 02:04:04+08:00（d420bbc7） |

develop→main 保鲜：PR #184 已于本轮开始前（09-30 00:28）把 main 保鲜至 R9——iteration-1548
的 R10 首查项闭环。R10 合并后累计 3 done < 10 且间隔 < 1 天，闸不触发——R11 首查项。

## 轮内裁定（conductor，台账四分类口径）

L039 七候选（C1-C7）+ P8/C4 精化逐条落账：

1. **C1 generic/checksum-put-virtual-plane=BUG**：virtual 面 checksum PUT 穿透（generic
   R9 修了 LOCAL 面，virtual 半面未收口）。
2. **C3 generic/checksum-suffix-case-sensitivity=BUG**：终缀大小写敏感（.SHA1 大写不拦）。
3. **C6 maven/checksum-policy-type-enum-validation=BUG**：checksumPolicyType 建仓面
   照单全收（A 仅收两枚举 + 逐字 400）。
4. **srvgen-oc 补遗 maven/checksum-oc-write-under-srvgen-policy=BUG**：server-generated
   策略下 declared 仍入 oc（A 注册与校验解耦；B SET 门限 CLIENT 策略）——审 A 范围外①③
   收编为台账条目，ADR-0052 已预留解耦位（seam 不读 checksumPolicyType）。
5. **C2/C5/C7=UNKNOWN×3**：virtual GET 按需计算 / maven-metadata.xml.sha1 专用路由 /
   remote 面 deploy 拒绝形态——方向未裁，L040（BIN-68）补取证。
6. **P8 修订**（generic/checksum-get-unset-404-wording 保持 open）：A 模型精化为双态文案
   （源缺=`File not found.; Path:` / 源在场未设=`Checksum not found for <src>`）——修复票
   BIN-65。
7. **C4 修订**（maven/sha512-put-non-layout-deploy 保持 open）：补 GAV 腿分歧（A=201
   普通部署 / B=404 旧文案）——修复票 BIN-66。

## 完成票（三票全 Done，Linear BIN-60..62）

| 票 | 提交 | 一句话 |
|---|---|---|
| BIN-60/T-578 SPI 缝+全模型翻正 | 3254c887 | SetClientChecksums 单语句条件列更新（w 门在缝内、SET 先行于渲染）；OriginalChecksums 渲染单源（httpapi/generic/maven 三消费面）；generic/maven 五条 BUG 翻正（201 CL0+Location 指源、409 写穿、GET 回显、del3 收敛）；28 腿活体双轮 |
| BIN-61/T-579 nuget 渲染族 | b248be9d | bare-PUT 201 ItemCreated envelope（CT+Jackson 字段序 byte 钉）；v3 push 201 body；409 CLIENT 文案逐字零 session 泄露（四负测）；originalChecksums 接单源；7 腿活体双轮 |
| BIN-62/T-580 L039 探针批 | 报告 L039（随 fc6fc0f5/bbfed66b） | checksum-PUT 相邻面 8 臂双轮：7 候选（4 BUG+3 UNKNOWN）+2 精化；白名单 9/9 零回归（develop 6d10fd34 干净树——**证 A 模型稳定，非修复验证**：修复证据=T-578/579 载荷树双轮）；R9 修复面复验 |

ADR-0052（4fa01dda）：SetClientChecksums SPI 六点全落地；两处自报偏差（可选接口段
ClientChecksumWriter / metadata 专用方法）经审 B 裁定 ACCEPTED——架构裁定票 BIN-64。

## 双审（首过 0+0，无 blocking 轮）

- **Reviewer A（correctness）APPROVE**：0 blocking / 6 NB / 3 范围外。取证自跑 build/
  vet/gofmt 全绿、三 adapter 3×ok、`-race -count=2` 新测试族全 PASS。NB 转候选池：
  generic io.ReadAll 无上限（勿猜限值，L040 探后加守卫）、writeBareCreated nil 守卫不对
  称、mime 第四副本、errors.Is 弱断言、adapter 级 SET 失败测试缺、writeServiceError
  err.Error() 直出（横切票）。
- **Reviewer B（architecture）APPROVE**：0 blocking / 4 NB / 3 handoff。§5.1 依赖方向、
  ADR-0052 符合度（两偏差 ACCEPTED）、渲染单源四消费面全确认、L039 零越界。handoff：
  批准合入 / 台账翻面 / mime 收敛票 / architect 裁定票 / L039 候选落账——全数执行。
- 范围外发现（审 A ①③）→ 台账 srvgen-oc 条目 + BIN-66 修复票；② → BIN-65。
- **注记（watch item）**：审 A 首跑 `internal/repo` 一次未捕获 FAIL（复跑 5 次 +
  `-race -count=2` 全 ok）——非稳定复现，持续观察；再显影则立 flake 票。

## Z 对账（raw 实枚举口径）

R10 终态（conductor 枚举，python yaml 解析复核）：**total 121 = resolved 65 + open 56**
（BUG 12 / UNKNOWN 36 / INTENTIONAL 7 / UNSUPPORTED 1）。较 R9（114=60+54）：**+5 flip
resolved**（generic/checksum-terminal-suffix-put-routing LOCAL 面、maven/checksum-put-409-
write-through、nuget/bare-put-201-content-type-body-envelope、nuget/checksum-409-error-
wording-family、nuget/v3-push-201-content-type-body）、**+7 新立**、total +7；open 净
54→56（+2：BUG −5+4、UNKNOWN +3）。

open BUG 12 枚举——存量 5（docker/remote-cache-layout、rest/properties-root-posture、
search/snapshot-row-wildcard-match、search/badchecksum-maven-metadata-exclusion、
maven/bare-directory-get-400-vs-404）+ L039 新钉 4（virtual-plane C1、suffix-case C3、
policy-enum C6、srvgen-oc）+ 修订存续 2（checksum-get-unset-404-wording P8 双文案、
sha512-put-non-layout-deploy C4 GAV 腿）+ storage/deploy-201-itemcreated-envelope-family
（池存续，R9 立项面本轮 nuget 三票收口其 nuget 腿）。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（冻结行集零增删；本轮无新行）
- **Y**= 109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12，不变）
- **coverage**= 60.50%（181 适用行口径，不变）
- **Z（open 已知偏离）**= **56**（raw 实枚举口径，open BUG 12 构成见上节）

## R11 候选池（已注册 Linear Todo 六票）

- **BIN-63/T-581**（High）：mime 表族收敛——四份 lockstep 副本提升共享包单源。
- **BIN-64/T-582**（Medium）：architect 裁定 ClientChecksumWriter 可选段 vs Service
  接口收编（ADR-0052 偏差①，审 B handoff）。
- **BIN-65/T-583**（High）：generic 拦截族扩展——virtual 穿透（C1）+ 终缀大小写不敏感
  （C3）+ GET 未设值双文案（P8）；差分硬门。
- **BIN-66/T-584**（High）：maven .sha512 全路径普通部署（C4 两臂）+ SET 无条件化
  （srvgen-oc）；差分硬门。
- **BIN-67/T-585**（Medium）：checksumPolicyType 建仓面枚举校验（C6）+ cfg-read echo
  复验腿；negative test 硬门。
- **BIN-68/T-586**（Medium）：探针批 L040——maven virtual/remote sidecar PUT 腿（审 A
  范围外①）+ C2/C5/C7 裁定取证 + io.ReadAll 超限 A 面（勿猜限值）+ **白名单九腿
  develop 复跑**（R10 载荷合并后预期 #1-#7 collapse——首查项）。
- 候选池（未立票）：审 A NB 残项（errors.Is 断言、nil 守卫对称化、adapter 级 SET 失败
  测试、writeServiceError err.Error() 横切收敛）；审 B NB-3 GET 遮蔽存量 .sha1 实文件
  → docs/user 迁移说明（tech-writer 面）。

## 环境与安全复核

- A 实例（7.161.26 @ 192.168.120.38:8082）配置零触碰；difftest-* 前缀仓用后 DELETE
  复验 0；凭据经 /tmp env 注入零落盘零打印。
- L039 B 面 origin/develop 6d10fd34 干净树运行（与载荷分支隔离）；/tmp 探针资产留存备查。
- `.playwright-mcp/` 未入库；reverse-src/ 只读未触碰；BOARD.md 冻结只读维持。
