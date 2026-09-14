# L021-1 helmoci 扩章首票 — 经典 chart 仓双端 wire 取证 + 契约蓝本（六域收官）

- 日期：2026-09-14（LOOP 021 / L021-1 / differential-qa-engineer）
- 模式：**dual**（A=Artifactory ref :8082 7.161.20 pro 全开〔baseUrl 未配置、匿名全局关——两项实例配置背景影响取证口径〕；B=BinFlow UAT :8083 `uat-l0213-b906e7cd`）
- **版签偏差注记**：派发令指定重建至 70061946；执行时并行轨道 L021-3（maven faces）共用工作树并覆写
  .env.uat 版签，实际构建落 uat-l0213-b906e7cd（含其未提交 maven 改动）。**helm 码径同一性已核**：
  `git diff 70061946..b906e7cd -- internal/adapter/helm internal/repo cmd` 为空 + `git status
  internal/adapter/helm` 无未提交改动——B 侧 helm 面行为可归 70061946。不与并行轨道争用共享 UAT 实例。
- 纪律：全程串行；臂预算 ≤12（实跑 12 + 同臂补发/修正不另计）；ref 健康门触发 3 次（见 §0）
- 客户端：**真实腿 helm 4.2.4**（brew 本机钉版；经典仓面与 helm 3 同族协议）；GPG 签名链用专用
  throwaway key（L021 DiffTest，ed25519，GNUPGHOME=/tmp/l021ws/gnupg——取证后已删）
- wire 捕获：curl 腿 `.hdr/.body` 逐部落盘 + helm CLI 腿经 `tools/difftest/l0211/wireproxy.py`
  （19021→:8082 / 19022→:8083，逐交换 JSON wire，Authorization 脱敏）——CLI 腿 wire 覆盖
  repo add/update（A）、pull --verify（B prov 取数交换）
- 证据：`reports/compatibility/l021-wire/{a,b}/`（helm/ 102 文件〔.hdr/.body/.err 三件套〕+ client/ 13 文件〔.log/.wire〕，轮询中间态已清）+ `arms.txt`；
  脚本 `tools/difftest/l0211/{helmoci-evidence.sh,wireproxy.py}`；夹具 l021chart
  0.1.0（signed）/0.2.0/0.10.0（sha256 见 §3）
- 规格/种子锚：`docs/reverse/helm.md`（335 行）+ `expansion-assessment-v3-helmoci.md`；
  matrix D12-R09（helm reindex ×2 在册）；helm 经典主面无独立行——新行建议见 §7
- 命名空间：`l021-helm`（helm local）双端建删净（deleteContent=true 双端 200；残留复核 A=400/B=404
  即无此仓）；helm repo l021a/l021b 已 remove；/tmp/l021ws 已清

## 0. 环境事故与处置（影响时序，不影响判定）

1. **Docker Desktop 第 6/7 次僵死**（L018 runbook 同款：docker CLI 挂起、端口 000/503）：构建途中
   第 6 次（杀僵尸 build + kill backend + 全量重启 + 手工 `docker start postgres artifactory`）；
   探针中段第 7 次（A 端口 wedge 全 503、binflow-ga unhealthy——同 runbook 再来一轮）。处置均沿
   runbook；artifactory 硬杀后曾 wedged 启动（router :8046 注册环），`docker restart artifactory`
   干净重启后 468s 恢复。**建议 conductor 知会 devops：频次 7 次/5 天，立案门槛已过。**
2. **探针脚本两轮作废重跑**：首轮 bash 数组展开 bug（`$ARR` 只取首元素——`-u` 吞 `-d` 致 curl
   误行）全部 401/URL-reject；修复 `${ARR[@]}` + wait_entry 改显式 user/pass 后全量重跑。次轮 A 侧
   全 503（第 7 次僵死窗口）作废，B 侧数据有效但为保同态重跑第三轮（teardown B 仓后干净双端）。
   最终轮 = 本报告数据。
3. **A 侧 transient 403**：首轮 bug 轰出 `recurrent request failures` 登录锁（403 2s 窗）——重跑前
   已自然过期，最终轮零出现。

## 1. 探针矩阵（12 臂；判定=归一提案 §6 应用前的原始差分）

夹具：`l021chart` 三版本（0.1.0 含 .prov 签名 / 0.2.0 / 0.10.0——排序判别子：SemVer 降序 0.10.0>0.2.0>0.1.0
与字符串降序 0.2.0>0.10.0 可区分）。A 上传面=`/artifactory/l021-helm/<path>`（curl -T 形态）、下载面=
`/artifactory/api/helm/l021-helm/`；B 上传/下载主面=`/binflow/l021-helm/`、只读别名=`/binflow/api/helm/l021-helm/`。

| # | 臂 | A（ref 7.161.20，baseUrl 未配） | B（UAT b906e7cd） | 判定 |
|---|---|---|---|---|
| a01 | PUT chart 0.1.0.tgz | **201** | **201** | **一致** |
| a02 | index.yaml（上传后；形态面） | 200：apiVersion v1 / entries.l021chart 单条目 / digest=`b6ecbe48…`（=tgz sha256 裸 hex）/ version+appVersion 恒引号 / **urls=`local://l021chart-0.1.0.tgz`** / created `…T04:25:29.694555524Z`（纳秒无引号）/ CT **text/plain** | 200：同骨架 / **digest 逐字同** / 引号同 / **urls=`l021chart-0.1.0.tgz`（relative）** / created `"…T04:25:28Z"`（秒级带引号）/ CT **text/yaml** | 骨架**一致**；**差异 D1（urls）/D2（CT）**；时间戳+布局=归一候选 N1/N2 |
| a03 | 版本排序（+0.2.0/+0.10.0） | 条目序 **0.10.0, 0.2.0, 0.1.0**（SemVer 降序）；三 digest=三夹具 sha256 逐字同 | **同序同 digest** | **一致**（SemVer 降序双端双证） |
| a04 | provenance PUT/GET | PUT prov **201**；GET **200** sha256=`5328fcf2…`==夹具；CT=application/octet-stream；index 无 prov 条目 | PUT **201**；GET **200** 同 sha256；CT=text/plain; charset=utf-8；index 同无 | **一致**（字节平价+不进 index）；CT 子面归 D2 |
| a05 | chart tgz 下载 | 200；sha256==夹具；CT application/x-gzip；X-Checksum-Md5/Sha1/Sha256 三头**值逐字同 B** | 200；主面+**别名面**同字节同头 | **一致**（含 B 别名面双证） |
| a06 | HEAD index | 200；CT text/plain | 200；CT text/yaml | 状态**一致**；CT 归 D2 |
| a07 | helm repo add + update（CLI） | **exit 0 ×2**（wire：GET index 200 → YAML 解析过） | **exit 0 ×2** | **一致**（add/update 不触 urls） |
| a08 | helm show chart + pull（CLI） | show **exit 1** / pull **exit 1**：`Error: scheme "local" not supported` | show **exit 0**（chart 元数据全输出）/ pull **exit 0**：产物 sha256==index digest==夹具 | **差异（D1 连带）**——B 相对 urls 按 repo base 解析成功（helm.md 待验证清单①定谳） |
| a09 | helm pull --verify（CLI） | **exit 1**（同因） | **exit 0**：`Chart Hash Verified: sha256:b6ecbe48…`；wire 亲证 GET `<tgz>.prov`→200→验签+哈希 | **差异（D1 连带）**；B 侧 provenance 全链活体双证 |
| a10 | auth 门（anon + 坏凭据） | anon **401**（实例匿名关）；badcred **401** `Bad Credentials` envelope | anon **200**（index 全文——B 匿名读开）；badcred **401** `invalid credentials` | 401 状态**一致**；措辞=跨域族 #auth401-wording；anon=实例姿态 D5 |
| a11 | reindex | POST **200** `Recalculating index for helm repository l021-helm scheduled to run`；后台完成后 3 条目保全 | POST **200** `Helm chart index calculation for repository 'l021-helm' has been scheduled.`；3 条目保全 | 语义**一致**（异步受理+条目保全）；**措辞差异 D3** |
| a12 | DELETE 0.10.0 → 条目移除 | **204**；index 移除 0.10.0（实测 ≤2s，异步快） | **204**；立即移除 | **一致**（删改状态机） |

同臂补发（不另计臂）：a08/a09 B 腿 destination 目录缺失修正重跑（脚本 bug 非 B 缺陷）；a02/a03 轮询
grep 引号形态 bug 致 "NOT visible" 假阴性——实际 index 双端 ≤10s 可见（a02-index 落档即证）。

## 2. index.yaml urls 形态对照（D1 核心）

| 面 | A（baseUrl 未配置） | B | 说明 |
|---|---|---|---|
| urls[0] | `local://l021chart-0.1.0.tgz` | `l021chart-0.1.0.tgz` | A=伪 scheme fallback（baseUrl 缺失时 7.161.20 实测行为——**helm.md §5.1 未载此形态**）；B=relative（**helm.md §5.1/HL-2 在册 BinFlow 定案**） |
| helm 4 客户端 | `Error: scheme "local" not supported`（show/pull/verify 全拒） | 五腿全绿（相对路径按 repo base 解析） | B 侧客户端可用性反超参照配置态 |
| DE 口径 absolute 模式 | 本实例不可测（需配 baseUrl——实例配置变更越红线，§12） | B 有 reserved Options 席（`absoluteURLs`，无配置键暴露） | absolute 对拍臂=后段票（见 §7） |

## 3. 制品字节平价（三方对拍：夹具 × A GET × B GET）

| 制品 | 夹具 sha256 | A GET | B GET | 结论 |
|---|---|---|---|---|
| l021chart-0.1.0.tgz | `b6ecbe48331b216b39c751851d3eae5425f4134cc2c6fa29ba39dbe00fd55bfc` | 同 | 同（+别名面同） | 字节平价；=index digest 双端逐字同 |
| l021chart-0.2.0.tgz | `1f586b7a90f6231d07ea41376d918d630711ea824a4ef8e3952a13b9575d3dd4` | 同（index digest） | 同 | 同上 |
| l021chart-0.10.0.tgz | `3fc51d3f208cd50fe845db65a03984bc3600a1fc5878ce06c5134ac07e9c9732` | 同（index digest） | 同 | 同上 |
| l021chart-0.1.0.tgz.prov | `5328fcf221d1f5b3b48a135b6c67214d3d9cd548e2a36b0df35818fbf719741b` | 同 | 同 | 字节平价；helm --verify 验签通过 |
| tgz 响应三校验和头 | — | Md5 `0dcbd241…`/Sha1 `7eb06c75…`/Sha256 `b6ecbe48…` | **逐字同** | 头面孪生（a05 双 hdr 对拍） |

## 4. CLI 腿汇总（真实 helm 4.2.4；退出码+关键输出）

| 腿 | A | B |
|---|---|---|
| repo add（经 wireproxy） | exit 0（l021a 注册） | exit 0（l021b 注册） |
| repo update | exit 0（wire：GET index 200） | exit 0（同） |
| show chart --version 0.1.0 | exit 1（local:// 拒绝） | exit 0（`type: application` / `version: 0.1.0`） |
| pull --version 0.1.0 | exit 1（同因） | exit 0（产物 sha256==digest） |
| pull --verify --keyring | exit 1（同因） | exit 0（`Chart Hash Verified: sha256:b6ecbe48…`） |

wire：`a/client/cli-a.wire`（2 交换：add+update 各 GET index 200，含 A 三条目 index 全文 1032B）；
`b/client/cli-b.wire` + `cli-b-fix.wire`（B 各腿交换；fix 腿含 prov GET 交换——`GET /binflow/l021-helm/l021chart-0.1.0.tgz.prov` → 200，X-Checksum-Sha256=`5328fcf2…`）。

## 5. 差异清单与分类建议（4+1 项；INTENTIONAL 终局裁定权不在本角色）

| D# | 面 | A | B | 分类建议 | 一句证据 |
|---|---|---|---|---|---|
| D1 | index urls[0] 形态 | `local://<path>`（baseUrl 未配置 fallback——DE 未载） | `<path>` relative（HL-2 在册决策） | **UNKNOWN（INTENTIONAL 候裁）** | helm 4 对 local:// 三腿全拒 / relative 五腿全绿——B 客户端可用性反超；absolute 臂不可测（配置越权）留后段 |
| D2 | Content-Type 族 | index=text/plain、prov=application/octet-stream | index=text/yaml、prov=text/plain; charset=utf-8 | **UNKNOWN（client-blind）** | helm 双端均解析（CT 不参与）；**helm.md §2 表「text/yaml」对 A 真身不成立——规格勘误候选**（B 反而合表） |
| D3 | reindex 受理措辞 | `Recalculating index for helm repository <key> scheduled to run` | `Helm chart index calculation for repository '<key>' has been scheduled.` | **UNKNOWN（措辞族）** | 状态/异步语义/条目保全双端同；纯 message 串差 |
| D4 | 401 措辞（坏凭据） | `Bad Credentials`（errors envelope） | `invalid credentials` | **UNKNOWN——跨域已知族 #auth401-wording** | conan D2②/npm m18 同款（L020-3 裁定材料在途），本域不另立 |
| D5 | 匿名可达性（记录级） | 401（实例匿名全局关） | 200（匿名读开） | **记录级（实例配置姿态）** | 双端均 as-configured；协议断言=认证后可达（绿）；BinFlow 私有仓默认匿名读归 storage-admin/auth 域口径 |

回归对照：helmoci 域首轮差分（无上轮清单）。跨域族比对：D4 与 conan/npm 域 401 措辞族**同款再现**
（B 侧统一 `invalid credentials` 渲染点——auth 中间层，非 helm adapter）；D3 措辞族与 conan D2 envelope 族
同性质（状态同、串异）。

## 6. normalize 提案（helmoci 域新章 `fixtures/normalize.yaml#helmoci`——归 compatibility-engineer 裁定，本轮不动文件）

| # | 规则 | 内容 | 理由 |
|---|---|---|---|
| N1 | `index.timestamps: iso8601_equivalence` | entries[].created 与根 generated：A 纳秒无引号（`2026-…T04:25:29.694555524Z`）≡ B 秒级带引号（`"2026-…T04:25:28Z"`）——归一为时间 instant 比对（UTC+精度截断到秒+quote 剥离）；**值本身=服务端写时刻非断言面** | §2 双端形态 |
| N2 | `yaml.layout: normalize_layout` | index.yaml 序列化布局（字段序：A Java map 序 urls 先于 type / B 字典序；列表缩进：A 2-space 挂 `- ` / B 4+6-space；引号风格）——parse-tree 比对，布局不比 | 双端合法 YAML 且 helm 双解析；version/appVersion 恒引号为**双端同**不属本条 |
| N3 | `headers: drop/keep` | drop：Date/X-Request-Id/X-Artifactory-*/X-Jfrog-Version/Etag/Last-Modified/Content-Disposition/Accept-Ranges/Proxy-Connection/Keep-Alive；keep_semantic：X-Checksum-Md5/Sha1/Sha256（值=断言面）+ Content-Type（**断言面非 drop——D2 待裁**） | A 存储文件头族 B 缺位；三校验和+CT 是协议面 |
| 反向门 | **不归一清单** | urls[0] 形态（local:// vs relative——D1）、Content-Type 值（D2）、reindex/401 message 措辞（D3/D4）、匿名可达性（D5）——**全部为待裁差异面，禁止归一掩盖** | 分类先行 |

## 7. 回流清单

- **契约蓝本**：`docs/compatibility/contracts/helmoci.yaml`（5 条目：index-generation[DIVERGENT-D1] /
  chart-download / provenance / repo-cli-roundtrip[DIVERGENT-by-D1] / auth-gate——机读门
  `yaml.safe_load` 5 entries 通过、DIVERGENT 2 均带 divergence_ref）——待 compatibility-engineer 评审入册。
- **normalize 提案**：§6 三规则+反向门（不改 normalize.yaml，提案在案）。
- **提金候选**（A 侧逐字落档 l021-wire/a）：index.yaml 全形（a02-index.body——apiVersion/entries 骨架/
  digest/引号/纳秒时间戳；urls 段需标注 baseUrl 未配置背景）、tgz 下载三校验和头（a05-get-tgz.hdr）、
  reindex 受理文案（a11-reindex.body）、401 envelope（a10-badcred-index.body）、prov GET 头
  （a04-get-prov.hdr）、HEAD index 200（a06）。
- **状态机反馈**：matrix 建议**新行**（helm 经典面现无独立行——D12-R09 仅重索引；`contract_ref=
  contracts/helmoci.yaml`、`last_difftest=本报告`）。
- **规格勘误提案**（docs/reverse/helm.md 归 reverse-engineer）：①§2 GET index 成功响应「text/yaml」
  对 A 真身不成立（live=text/plain，本实例）；②§5.1 urls 生成规则漏 baseUrl 未配置时的 `local://`
  fallback 形态（A 实测）且「默认 absolute」在本实例不可复现；③§9 待验证清单①（helm 3+ 对 relative
  urls 的 repo update 解析）活体定谳：**支持**（helm 4.2.4 五腿绿）。
- **UNKNOWN 升 conductor**：D1（urls 形态——INTENTIONAL 候裁：HL-2 在册+B 客户端可用性反超+absolute
  臂后段补）、D2（CT 族）、D3（reindex 措辞）；D4 归 L020-3 在途裁定。
- **环境上报**：Docker Desktop 第 6/7 次僵死（§0——建议 devops 立案）。
- **实例配置上报**：B 侧 pro license 沿用 L018 装载（DB 持久化存活，89 天余量）——helm/helmoci 均
  TierPro，conan 域先例已报，无需新动作。

## 8. 六域收官确认

helmoci 契约文件落盘（`docs/compatibility/contracts/helmoci.yaml` 5 条目）后，contracts/ 目录 =
docker-remote(25) / conan(16) / npm(17) / pypi(10) / goproxy(4) / storage-admin(10) /
**helmoci(5——本轮新增)** ——expansion-assessment-v3 所列扩章域全部有契约文件在册，**六域（评估口径
第六域=helmoci）收官**。OCI 面（helmoci 的 oci:// 子面）经 docker 域 25 条目背书不重复立条（评估口径维持）。
