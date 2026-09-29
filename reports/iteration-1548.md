# iteration-1548 · R9：裸 checksum PUT 拦截族 + mime 归属翻转 + nuget/goproxy 响应面 + license dev 缝 + 探针双批（L037/L038）+ 台账 Z=54（2026-09-29）

一轮七票（BIN-53..59）+ 双审 blocking 修复轮内闭环 + 差分双批（L037 probe-first 五臂 /
L038 T-577 live 收口）。R8 遗留的 mime 两票制后腿（BIN-53 归属翻转）按裁定执行；T-574
揭出 client-checksum 持久化 SPI 缺口，已开 **BIN-60**（Backlog/High）移交 R10 首位候选。
台账 Z（open）46→54：6 flip resolved、14 新条目（BUG 5→13，全部为探针/差分显影的已知面）。

## PR / 合并

| PR | 内容 | 状态 |
|---|---|---|
| [#183](https://github.com/0ldlight/binflow/pull/183) | R9 载荷（6 提交：BIN-56 拦截族+双审 blocking 修复 / BIN-55 license 缝 / BIN-53 mime 翻转 / BIN-57 nuget 头 / BIN-58 goproxy CT / a5dd06f3 台账批+七票证据+双审报告）→ develop | MERGED 14:50:54Z（6d10fd34） |

develop→main 保鲜本轮未触发（周期合并闸 ≥10 done 或 ≥1 天；R8 #181 当日已保鲜，R9 累计
7 票未达闸）——R10 首查项。

## 轮内裁定（conductor，台账四分类口径）

1. **mime 归属**（R8 两票制后腿，BIN-53 执行）：扩展名出厂表拥有值、声明 CT 忽略——
   T-571 62 键活体 sweep 双面 SAME 后 generic/mime-ownership-model +
   generic/mime-stdlib-host-drift 双双翻 resolved；L037 错误括注（「generic
   .info=octet-stream」未实测推断）以勘误更正，台账口径不受影响。
2. **裸 checksum PUT 族**（L037 Arm 1 提案 → 裁定 1a/1b/1c）：generic 拦截缺失=BUG；
   maven 404 文案族=BUG（T-574 落地即翻 resolved）；409 写穿=BUG 挂 SPI 缝（BIN-60）。
3. **nuget X-Checksum-Sha256**（L037 Arm 2 → 裁定 2a）：bare 面 UNKNOWN→BUG 对齐
   （T-575 落地：201 恒渲染，client 头原值/缺席服务端补算同值，错值 409 无头）；
   push 面 B 多渲染的反向候选同票收口（v2/v3 两入口去头）。
4. **pypi 上传响应 CT/body**（L037 Arm 3 判决实验）：twine 7.0.0 对上传 200 只看状态码
   （body 换 text/html 垃圾仍 exit=0）——UNKNOWN→INTENTIONAL 登记。
5. **goproxy .info/.mod CT**（BIN-58/T-576）：拼写对齐 wire（`application/json+info` /
   `text/plain+mod` 去 charset）；.mod.gz 维持不路由（A api 面同 404）。
6. **L038 五候选**（T-577 收口批，conductor 逐条）：① nuget v3 push 201 CT/body=BUG
   （B 自身 v2/v3 形态不一致且与 A 分歧）；⑤ 201 envelope 渲染家族=BUG（R10 家族统一
   票池）；② nuget v2 push 根存储布局=UNKNOWN（方向未裁）；③ goproxy .mod.gz 存储面=
   UNKNOWN；④ `/api/go` 别名缺席=INTENTIONAL（authority=docs/reverse/goproxy.md 不沿用
   声明，本轮追认）。

## 完成票（七票全 Done，Linear BIN-53..59）

| 票 | 提交 | 一句话 |
|---|---|---|
| BIN-53/T-571 mime 归属翻转 | 1d4f5085 | 扩展名表拥有值/声明 CT 忽略；62 键活体 sweep 双面全 SAME |
| BIN-54/T-572 L037 探针批 | 报告 L037（随 a5dd06f3） | probe-first 五臂双轮：checksum PUT 全谱 / nuget 头取值语义 / twine 判决实验 / goproxy 相邻面 / del3 计数归因 |
| BIN-55/T-573 license dev 缝 | f2c26b5d | dev 构建专属 tier bypass（ADR-0032 Errata），解锁 nuget/go 活体 B 面 |
| BIN-56/T-574 checksum PUT 拦截族 | d900b21f | generic 拦截臂（源缺 404 逐字）+ maven 文案/路由前置/409 路径；LOCAL 门（双审 blocking 修复 amend 并入） |
| BIN-57/T-575 nuget 头 | 17e534de | bare PUT 201 恒渲染 X-Checksum-Sha256；push 两入口去头 |
| BIN-58/T-576 goproxy CT | 82541b60 | .info/.mod Content-Type 拼写对齐 |
| BIN-59/T-577 L038 收口 | 报告 L038（随 a5dd06f3） | live 双端双轮 32 PASS / 0 FAIL / 2 BLOCKED（cargo Custom Base URL 实例门）；白名单 9/9 复验零回归；环境处置复验在案 |

## 双审（v1 双 REQUEST_CHANGES 同 blocking → 修复 → v2 双 APPROVE 0+0）

- **v1 blocking（A/B 同发现）**：T-574 拦截臂在 remote 面先 svc.Get 探源——写动词触发
  回源拉穿（cache warming），上游 miss 会以 checksum 404 顶替 RE-05 的 405 只读拒绝。
- **修复（5774a072 amend 进 d900b21f）**：repo.ClassReader 缝（metadata.RepoStore 实现，
  principal-free）+ generic handlePut TypeLocal 门（remote/virtual 恒走原链）+
  remote_render_test 负测（三终缀 × 405+Allow+上游计数冻结）+ maven Parse 失败臂 body drain。
- **v2**：Reviewer A（correctness）APPROVE 0 blocking / 7 NB（自跑 `-overlay` 探针证双回归
  腿治愈、Δ0 上游命中）；Reviewer B（architecture）APPROVE 0 blocking / 4 NB（缝方向/
  门惯用法与 maven local 门同构/负测钉住）。
- **NB 转化**：A 的 virtual 面 checksum PUT 语义、终缀大小写敏感、拦截排序 → R10 探针
  候选池；B 的 mime 表族收敛触发注记、generic.New class 参数注释 → 随 BIN-60 族票顺手；
  T-574 Risks ③ 失实表述 + L037 错误括注 → 勘误已落两报告原文。

## Z 对账（raw 实枚举口径）

R9 终态（conductor 枚举）：**total 114 = resolved 60 + open 54**（BUG 13 / UNKNOWN 33 /
INTENTIONAL 7 / UNSUPPORTED 1）。较 R8（100=54+46，BUG 5/UNKNOWN 35/INTENTIONAL 5/
UNSUPPORTED 1）：**+6 flip resolved**（mime×2、maven checksum 404 文案、nuget
X-Checksum-Sha256×2、goproxy .info/.mod CT）、**+14 新条目**（11 存续 open + 3 注册即
收口）、total +14；open 净 46→54（+8）。open BUG 5→13：新增全部为 R9 探针/差分显影的
已知面，无存量恶化。

open BUG 13 枚举——存量 5（docker/remote-cache-layout、rest/properties-root-posture、
search/snapshot-row-wildcard-match、search/badchecksum-maven-metadata-exclusion、
maven/bare-directory-get-400-vs-404）+ SPI 缝 2（generic/checksum-terminal-suffix-put-routing、
maven/checksum-put-409-write-through → BIN-60）+ T-574 新钉 2（generic/checksum-get-unset-404-wording、
maven/sha512-put-non-layout-deploy）+ envelope 池 4（nuget/bare-put-201-content-type-body-envelope、
nuget/checksum-409-error-wording-family、nuget/v3-push-201-content-type-body、
storage/deploy-201-itemcreated-envelope-family）。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（冻结行集零增删；本轮无新行）
- **Y**= 109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12，不变）
- **coverage**= 60.50%（181 适用行口径，不变）
- **Z（open 已知偏离）**= **54**（raw 实枚举口径，open BUG 13 构成见上节）

## R10 候选池（已注册/待派发）

- **BIN-60（Backlog/High）**：client-checksum 持久化 SPI 缝（SetClientChecksums）——
  翻正 SPI 缝 2 BUG + GET 未设值文案族 + del3 计数收敛 + originalChecksums 渲染；
  验收=/tmp/t574-probe.py 腿集双轮复跑（预期 DIFF 清单全翻 PASS）。
- 201 envelope 渲染家族统一票（4 BUG 池：nuget bare CT/body、nuget 409 文案族、nuget v3
  push CT/body、storage/deploy ItemCreated 家族）。
- 双审 NB 探针候选：virtual 面 checksum PUT 语义、终缀大小写、拦截排序。
- 工程卫生：中间提交 d900b21f 不独立编译（1 参 mimeByPath 引用跨票序落定义）——多票
  同树并行时的分票提交序教训：收编时先全量 build 再分票 amend。
- develop→main 保鲜（本轮未达闸，R10 首查）。

## 环境与安全复核

- A 实例（7.161.26 @ 192.168.120.38:8082）配置零触碰；difftest-* 前缀仓用后 DELETE
  复验 0；凭据经 /tmp env 注入零落盘零打印。
- B scratch 实例（18080/18082/18083 系）全 SIGTERM graceful stop + 端口释放复验；
  /tmp 探针资产留存备查。
- `.playwright-mcp/` 未入库；reverse-src/ 只读未触碰；BOARD.md 冻结只读维持。
