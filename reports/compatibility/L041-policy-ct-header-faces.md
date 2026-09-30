# L041 — 收敛树首跑：策略/REST/头面六臂探针 + checksum 白名单 #1-#9 首跑（T-591 / BIN-73）

- 日期：2026-09-30；模式 **dual**（A 活体双轮 + B 单树双轮）
- A 面：JFrog Artifactory **7.161.26** Enterprise+ @ http://192.168.120.38:8082（当日 `/api/system/version` 复验 `7.161.26 / 86126900`，与 L037/L038/L039/L040 同锚）；凭据经 `/tmp/r3-difftest.env` source 注入（值零落盘零打印不进本报告；见「环境处置」grep 注记）
- **B 面 = 收敛树（本批首次）**：worktree HEAD `30e96418`；**内容恒等于 develop `1b1e3bfb`**（`git diff --stat HEAD 1b1e3bfb` = **空**，HEAD..1b1e3bfb 的 9 个提交全为 PR merge 节点零内容差；`git branch -a --contains 1b1e3bfb` = origin/develop）——**L040「B2=载荷树条件性 collapse」的条件句随本批关闭**：R10+R11 全部修复（a3f053df 拦截族扩展 / e16034bb 策略面收口 / 26f7f781 mime 收敛）均在树内。**溯源门（账实防污染）**：证据轮所用构建产自共享 worktree（构建时点 10:07 后另有 agent 在途修改 internal/adapter/maven/{handler,put}.go——非本批文件、零触碰），故另建 **detached pristine worktree @30e96418 重构建 `b-server-clean` 并双轮全量重跑**：clean r1≡r2 drift=0 且 **原始 B vs clean B 108 腿归一后 drift=0**（二进制 shasum 异仅 vcs stamp，行为零差）——B 证据对收敛树账实成立。scratch 双实例 `BINFLOW_HOME=/tmp/l041/homes/b-r{1,2}`、`BINFLOW_SERVER__LISTEN`=127.0.0.1:**18156/18157**、`BINFLOW_DEV_TIER=pro`（healthz=200×2，license `licensed:false, tier:pro` 双实例一致）
- 命名空间：双端统一 `difftest-l041-*`；A 实例配置零触碰；**A 每轮脚本 teardown residual=[] + 收尾独立复验计数=0**
- **双轮**：A r1≡r2 **drift 0**、B r1≡r2 **drift 0**（normalize：ISO-TS/RFC-1123 Last-Modified/UUID/epoch-ms/host:port + 请求级头 x-request-id 丢弃；`compare.py` 输出在案）；四证据轮 transport-error（status=0）= 0
- 探针资产（/tmp/l041 留存备查）：`probe.py`（108 腿/轮：六臂+setup/teardown/residual+cfg 全量重取）+ `round.sh`（B 轮编排）+ `compare.py`（r1≡r2 门）；raw：`probe-{a,b}-r{1,2}.json` ×4 + shakedown 5 轮（A×2/B×3，探针客户端 bug 两处在证据轮前修复：①REST PUT=create/POST=update 语义与键位互撞 ②maven artifactId 取自版本目录上级段——均为探针设计错非产品发现）
- 前序权威：L039/L040（白名单标尺+候选）、R11 台账批（六新条目）、R11 双审（A NB-1、B NB-6）、T-583/T-584/T-585 报告

---

## Arm 1 — maven/generic sidecar GET 头面（台账 `maven/sidecar-get-x-checksum-sha256-echo` UNKNOWN，review_gate「一腿头面全收」兑现）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `m1-get-jar-ctrl`（源工件 GET 对照） | 200 + **X-Checksum-{Md5,Sha1,Sha256} 全三键**（源计算值） | 200 + 同三键**同值** | 一致（头族主面） |
| `m1-get-sha1-set`（sidecar GET·已设） | 200，**无任何 X-Checksum-\* 头、无 Etag**（仅 CT/CL/Last-Modified/infra 头） | 200 + **Etag（=值）+ X-Checksum-Sha256（=源 sha256）** | **差异（条目主面坐实：B GET 面自加头）** |
| `m1-head-sha1-set`（**HEAD 动词**） | 200 + **Etag + X-Checksum-{Md5,Sha1,Sha256} 全三键**（A 条件渲染路径=**动词 HEAD**） | 200 + Etag + **仅 X-Checksum-Sha256** | **差异（反向半面：B HEAD 缺 triple 中 Md5/Sha1）** |
| `m1-get-md5-unset`（未设 md5 GET） | **404** `Checksum not found for com/diff/l041/m1/1.0.0/m1-1.0.0.jar`（指源） | **200** = 计算 md5（**按需计算**）+ Etag + xsha 头 | **差异（状态级，见 N2）** |
| `m1-get-sha256-unset`（未设 sha256 GET） | 200 = 计算 sha256（按需） | 200 = 同值（+Etag/xsha 头） | 一致（体面）；头面差异同上两行 |
| `m1-get-sha1-absent-src`（源缺 sidecar GET） | 404 `File not found.; Path: '…:…/none-9.0.0.jar'`（**指源**） | 404 同文案但 Path **指 `…jar.sha1` 终缀路径** | **差异（miss 指向面：B maven 面指终缀）** |
| `g1-get-sha1-set`（generic sidecar GET 对照） | 200，无校验头 | 200，无校验头 | 一致（B 的 xsha 自加头**仅 maven 面**） |
| `g1-get-sha256-unset`（generic 未设 sha256 GET） | **200** = 计算 sha256（按需，与 C2/N3 的 A 模型一致，延及 local 面） | **404** `Checksum not found for g1/src.bin` | **差异（反向：B generic 面不按需，见 N2）** |

**裁定输入**：A 模型钉死——(a) sidecar **GET**：零校验头零 Etag；(b) sidecar **HEAD**：Etag + 源工件校验 triple 全键（A 的「条件渲染路径」=动词，非状态/算法）；(c) 按需计算**仅 sha256**（maven/generic 两面一致），md5 未设=404 Checksum-not-found；(d) miss 文案指**源**。B 模型——GET/HEAD 均自加 Etag+X-Checksum-Sha256（maven 面）、HEAD 缺 Md5/Sha1 两键、md5 按需**过算**（generic 面却**零算**）、miss 指终缀。**分类建议：条目翻面裁定输入已齐**（B GET 自加头同 nuget/push-201 先例方向；HEAD 缺键为同条目另一半），待 conductor。

## Arm 2 — checksumPolicyType 枚举门 remote/virtual 面（R11 Review B NB-⑥ 候选）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `r2-ctrl-local-badenum`（local 对照锚） | 400 `No checksum policy type found for type: none` | 400 同文案**逐字** | 一致（T-585 门复绿） |
| `r2-create-remote-badenum` / `r2-update-remote-badenum`（remote 面 badenum） | **200 接受**（字段静默丢弃） | **200 接受** | **一致——A 对 remote 面同样无门** |
| `r2-create-virtual-badenum` / `r2-update-virtual-badenum`（virtual 面） | **200 接受**（同上） | **200 接受** | 一致 |
| `r2-get-{remote,virtual}-cfg`（cfg 回读） | checksumPolicyType **缺席** | 同缺席 | 一致（双端同丢字段） |
| `r2-create-remote-rrcp-badenum`（**remote 域字段** `remoteRepoChecksumPolicyType:"none"`） | **400** `No checksum policy type found for: none`（**注意：无 "type" 一词**，与 local 面文案异体） | **200 接受**（B 无该字段无门） | **差异（N1 新候选）** |

**结论**：NB-⑥ 候选**否定**——A 对 remote/virtual 面的 `checksumPolicyType` 同样无枚举门（字段不属该 rclass，静默丢弃），双端一致，无需对齐。但探针发现 A 的枚举门挂在**域字段**上：remote rclass 的真策略字段是 `remoteRepoChecksumPolicyType`，A 对它有门（400，文案变体「for:」无「type」），B 该字段整面缺席。→ 新候选 N1。

## Arm 3 — repo 缺失 key 404 文案方法族（台账 `rest/repo-update-missing-key-404-wording` UNKNOWN 证据增补）

| 腿（各用独立 never-created key，免 PUT 建仓污染） | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `r3-post-update-missing`（POST update→缺失 key） | 404 `No repository '<key>' was found.`（**带引号**） | 404 `Repository does not exist: repo "<key>": repository not found` | 差异（=登记条目主面，live 复现） |
| `r3-get-missing`（GET→缺失 key） | **400** `{"errors":[{"status":400,"message":"Bad Request"}]}` | **400 同形**（message 逐字 "Bad Request"） | **一致（意外但双端同形）** |
| `r3-delete-missing`（DELETE→缺失 key） | 404，**非 errors 包**：`{repoKey, statusMsg: "Cannot delete repository: '<key>', repository config does not exist", deletedArtifactsCount: 0, success: true}` | 404 errors 包 `Repository does not exist: repo "<key>": repository not found` | **差异（体形族：A statusMsg 包 vs B errors 包）** |
| `r3-put-missing-create-face`（PUT→缺失 key） | 200 建仓（create-or-replace 语义=建） | 200 建仓 | 一致（方法族收口：PUT=建） |

**裁定输入**：条目应**扩族修订**——A 模型=三形态（POST-missing 带引号 `No repository '<k>' was found.` / DELETE-missing statusMsg 包 `Cannot delete repository: '<k>', repository config does not exist` / GET-missing 400 Bad Request（双端一致无需动作））；另见 Arm 4 的**第三文案变体**（无引号）。

## Arm 4 — repo 配置写面 Content-Type 严格度（台账 `rest/repo-config-put-ct-strictness` UNKNOWN 边界探）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `r4-post-update-no-ct` / `r4-put-create-no-ct`（缺 CT） | **415** `Unsupported Media Type`（改仓/建仓同面） | **200**（全接受） | 差异（登记条目主面 live+扩面：不限于缺 CT） |
| `r4-post-update-text-plain` | **415** | 200 | 差异 |
| `r4-post-update-xml`（application/xml） | **415** | 200 | 差异 |
| `r4-post-update-vnd-json`（application/vnd.api+json） | **415** | 200 | 差异（JSON 子类型亦拒） |
| `r4-post-update-json-charset`（**application/json;charset=UTF-8**） | **404** `No repository difftest-l041-ct-local was found.`（**无引号变体**——charset 参数使 POST update 落入 repo-lookup 失败路径） | 200 | **差异（新 facet：非 415 族，404 文案族第三变体）** |
| `r4-post-update-json-exact-ctrl`（精确 application/json） | 200 `Repository … update successfully.` | 200 | 一致（控制腿） |
| `r4-put-existing-ctrl`（PUT→已存在 key） | 400 `error when validating repository name: … Repository key already exists` | 400 同文案**逐字** | 一致（PUT=纯建仓门，双端同形） |

**裁定输入**：条目修订——A 模型=**精确 `application/json` 白名单**（缺 CT / text-plain / xml / vnd.api+json 全 415；**charset 参数不是 415 而是 404 无引号文案**——A 内部异常路径，如实记录不猜成因）；B=零 CT 检查全接受。真实客户端恒带 CT，影响面低（条目 rationale 原判维持），裁定权在 compatibility-engineer。

## Arm 5 — N6 遮蔽面：实体校验文件双读面（L040 N6 注记，A 侧未探面）

载体=.sha512（两侧均可普通部署落地；.sha1 实体在 maven/generic 面双端均被拦截语义占用，**A 无 .sha1 实体落地路径**）：

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `n6-put-gav-sha512` / `n6-put-nongav-sha512`（实体落地，GAV+非GAV 两拼写） | 201 / 201 | 201 / 201 | 一致（T-584 .sha512 普通部署面复绿） |
| `n6-get-member-{gav,nongav}-sha512`（成员 local GET） | 200 = **文件字节**（CL128，Etag 同值） | 200 = 同字节同 Etag | 一致（**成员读面服务实体文件**） |
| `n6-get-virt-{gav,nongav}-sha512`（virtual 读面） | 200 = 同字节 | 200 = 同字节（+`X-Binflow-Resolved-From` 自加头） | 一致（体面）；B 自加头见 N3 |
| `n6-fileinfo-{gav,nongav}-sha512` | 200 | 200 | 一致 |
| `n6-put-sha1-set-member` + `n6-get-member-sha1-set`（sidecar 语义对照） | 201 + 200=值 | 201 + 200=值 | 一致 |
| `n6-virt-put-sha1-src-absent`（virtual 面 .sha1 源缺 PUT=.sha1 实体落地可达性） | 404 `Target file … doesn't exist: difftest-l041-`**`maven`**`…` | 404 同文案 repo 段=**difftest-l041-vmt2**（虚拟 key） | 差异（=L040 arm1b 已登录 open 面，live 不变；**.sha1 经 virtual 实体落地双端均不可达**） |

**结论**：L040 N6 的「同仓双读面不一致」在**收敛树上不再可达**（T-583 拦截族扩展后 .sha1 经 virtual 不再落实体文件；.sha512 实体双读面双端一致服务字节）——N6 注记可随 C1/C5 修复票关账。副产：GET 实体文件的头面差异（A `Content-Disposition`+`X-Artifactory-Filename` vs B 无）→ N3 候选。

## Arm 6 — srvgen declared-drop 角落（R11 Review A NB-1；.md5/.sha1 声明含全零占位）

策略=server-generated-checksums；声明头 X-Checksum-Md5=000…0(32)、X-Checksum-Sha1=000…0(40)（decl-zero 腿）或双正确计算值（decl-correct 腿）：

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `s6-put-gav-jar-decl-zero`（GAV jar+占位声明） | **201**；FileInfo **oc={sha1:000…0, md5:000…0, sha256:计算}**（**声明值无条件注册进 originalChecksums**）；checksums=计算 triple；GET 回显头=计算 triple | 201；**oc={sha1:计算, md5:计算, sha256:计算}**（**声明被丢弃**） | **差异（值级：A 保声明 / B 丢声明）** |
| `s6-put-gav-sha512-decl-zero`（GAV .sha512+占位） | 同上形（oc=占位零值存活） | 同上形（oc=计算） | 差异（同面） |
| `s6-put-nongav-sha512-decl-zero`（**非 GAV .sha512+占位**） | **201** 落地（oc=占位零值） | **409** `Checksum error for '…r.txt.sha512': received '000…0' but actual is '39f6…'`（**拒绝**，无落地：GET/FileInfo 404） | **差异（状态级：refuse 门在该臂未按策略域收口）** |
| `s6-put-*-decl-correct`（GAV jar/.sha512+非 GAV 三腿正确声明） | 201；oc=checksums=计算（=声明，不可分） | 201 同形 | 一致 |
| `s6-get-*`（GET 回显头） | 计算 triple | 计算 triple | 一致（srvgen 下 GET 回显=计算值，双端同模型） |
| `m1-fileinfo-jar`（无声明无 sidecar 的 jar oc 键集，交叉证） | oc=**{sha1(client 注册), sha256}（无 md5 键）** | oc={sha1, **md5(兜底计算)**, sha256} | 差异（=台账 `httpapi/original-checksums-key-model` BUG 面 maven 平面 live 复现） |

**裁定输入（NB-1 收口）**：A 权威模型——**部署声明头（含全零占位）无条件注册进 originalChecksums，策略只管 checksums/GET 回显取计算值**（srvgen=「计算值服务、声明值留档」，非「丢弃声明」）；B 的 putFile 在 srvgen 下**丢弃**声明（GAV 两臂）且 putSha512ChecksumFile 臂**反向收紧为 409 拒绝**——三臂两语义（GAV=丢、非GAV=拒、A=留档），NB-1 所指「两臂内部分歧」在收敛树上不仅仍在且多了状态级偏离。→ 新候选 N4（BUG 形倾向：refuse 应按 ADR-0052 策略域收口；oc 值面与 key-model 条目同族）。

## Arm 7 — checksum 白名单 #1-#9 **收敛树首跑**（L038/L039 口径，L040 B2 条件性结论转正验证）

| # | 面 | A（双轮） | B=收敛树（双轮） | 判定 |
|---|---|---|---|---|
| 1 | generic 值对 201 形态 | 201 无 CT、CL 缺席、Location→**源**、空体 | 201 无 CT、**CL=0**、Location→**源**、空体 | **collapse**（CL 显式 0 vs 缺席=既有微 facet 注记） |
| 2 | generic 值错 409 | 409 `Checksum error for 'wl/src.bin.md5': received '000…0' but actual is 'be06…'` | 409 同文案**逐字** | **collapse** |
| 3 | del3 计数 | deletedArtifactsCount=**3** | **3** | **collapse** |
| 4 | FileInfo oc 写穿 | oc.md5=**000…0**（键集 A=B={sha1,md5,sha256}——本腿 sha1 为 client 注册故键集重合） | 同值同键集 | **collapse** |
| 5 | maven 409 后 GET 回显 | =**000…0**（40 hex） | =**000…0** | **collapse** |
| 6 | nuget bare 201 CT+body | CT `ItemCreated+json;charset=UTF-8` 全 envelope | 同 CT 同结构（CL 显式 vs 缺席微 facet 同 #1 族） | **collapse** |
| 7 | nuget 409 文案族 | `Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected…ChecksumsInfo{…}` 全族，CT `application/json;charset=ISO-8859-1` | **逐字一致**（ChecksumsInfo 全族）+CT 同 | **collapse** |
| 8a | srvgen-oc（wsrv 三腿） | oc={md5:**000…0**, sha256:计算}（**无 sha1 键**）；GET .md5=计算值 | oc={md5:**000…0**, **sha1:计算(兜底)**, sha256}；GET .md5=计算值 | **条目面 collapse（R11 翻面站稳）；残余 sha1 键=open BUG `httpapi/original-checksums-key-model` 面（R12 门，live 如预期，非回归）** |
| 8b | maven 非 GAV .sha512 | 201 普通部署，CT `ItemCreated+json;charset=UTF-8`，Location→自身 | 201 同形（CT `…+json; charset=UTF-8` 空格微 facet 注记） | **collapse（R11 翻面站稳）** |
| 9a | generic GET 未设值·源在场 | `Checksum not found for wl/other.txt` | **逐字** | **collapse（L040 半面→全收）** |
| 9b | generic GET 未设值·源缺 | `File not found.; Path: '…:wl/never-seeded.txt'` | **逐字** | **collapse（保持）** |

**结论**：**九面全 collapse，零意外 live**——L040 的「B2=885b22e5 条件性 collapse」在 develop 内容树上**转正**（#1-#7/#9b 保持、#8b/#9a 随 R11 修复收齐）；唯一残余=8a 的 sha1 兜底键，归属既有 open BUG 条目（键集模型，R12 修复门），非白名单面回归。

---

## 候选台账条目草稿（新立/修订/证据齐备分列；分类建议全待 conductor，INTENTIONAL 恒不越权）

### 新立

| # | 条目 id | surface 草稿 | A 形（权威） | B 形（收敛树） | 建议分类 | raw 锚 |
|---|---|---|---|---|---|---|
| N1 | `rest/remote-domain-policy-enum-gate` | remote rclass 域字段 `remoteRepoChecksumPolicyType` 枚举门 | badenum `none` → **400** `No checksum policy type found for: none`（**文案变体：无 "type"**）；而 `checksumPolicyType`（local 域字段）在 remote/virtual 面双端同静默丢弃 | 该字段整面缺席，badenum **200 接受** | **UNKNOWN**（范围裁定：B remote 模型是否引入该字段+门；BUG 倾向注记：同 T-585 local 门族） | `r2-create-remote-rrcp-badenum` |
| N2 | `maven/sidecar-get-ondemand-matrix` | sidecar GET 未设算法按需计算矩阵 + miss 指向 | **sha256-only 按需**（maven/generic 两面一致）；md5/sha1 未设=404 `Checksum not found for <src>`；miss 文案指**源** | maven 面 md5 亦按需 **200 计算值**（过算）；generic 面 sha256 **404**（零算，与自身 maven 面内部不一致）；maven miss 指向**终缀路径** | **BUG 倾向 UNKNOWN**（A 模型已由 C2/N3 钉 sha256-only；B 需统一按需矩阵+miss 指向） | `m1-get-md5-unset` `g1-get-sha256-unset` `m1-get-sha1-absent-src` |
| N3 | `storage/artifact-get-response-headers` | 工件 GET 响应头集（Content-Disposition/infra 族 vs 自加头族） | `Content-Disposition: attachment; filename=…; filename*=UTF-8''…` + `X-Artifactory-Filename/Id/NodeId/Origin-Remote-Path` + `X-Jfrog-Version` + 校验 triple | **无 Content-Disposition/X-Artifactory-\* 全族**；自加 `X-Request-Id`（请求级）+ `X-Binflow-Resolved-From`（virtual 读面） | **UNKNOWN**（范围裁定：哪些头属契约面——Content-Disposition 影响下载文件名属客户端可见行为，BUG 倾向；infra/X-Jfrog-Version 是否入仿形待裁） | `m1-get-jar-ctrl` `n6-get-*-sha512` |
| N4 | `maven/srvgen-declared-header-drop` | server-generated-checksums 下部署声明头（X-Checksum-Md5/Sha1）处置 | **无条件注册进 originalChecksums（全零占位亦留档）**；checksums/GET 回显=计算值；非 GAV .sha512+占位=201 | GAV 臂（putFile）**丢弃**声明（oc=计算 triple）；非 GAV .sha512 臂（putSha512ChecksumFile）**409 拒绝**（refuse 门未按策略域收口）；三臂两语义 | **BUG 倾向 UNKNOWN**（refuse 域收口=ADR-0052 家族语言；oc 值面与 `httpapi/original-checksums-key-model` 同族交叠，裁定时可并票） | `s6-put-gav-*-decl-zero` `s6-put-nongav-sha512-decl-zero` `s6-fileinfo-*` |

### 修订（既有条目 A 模型扩展）

| 条目 | 修订内容 | 证据行草稿素材 |
|---|---|---|
| `rest/repo-update-missing-key-404-wording` | 方法族三形态收口：POST-missing=带引号 `No repository '<k>' was found.`（live 复现）；**DELETE-missing=A statusMsg 包 `Cannot delete repository: '<k>', repository config does not exist`（{repoKey,statusMsg,deletedArtifactsCount,success}）vs B errors 包**；GET-missing=**双端一致 400 `Bad Request` 无需动作**；PUT-missing=双端 200 建仓一致；**第三文案变体（无引号）见 CT 条目 charset facet** | `r3-{post,get,delete,put}-*` 四腿 |
| `rest/repo-config-put-ct-strictness` | A 模型=**精确 `application/json` 白名单**：缺 CT/text-plain/application/xml/application/vnd.api+json → **415**（建仓+改仓同面）；**charset 参数 → 非 415，POST update 落 404 `No repository <k> was found.`（无引号变体）**；B 零 CT 检查（全 200）。影响面维持低判（真实客户端恒带 CT） | `r4-*` 七腿 |

### 证据齐备（翻面裁定输入，待 conductor）

| 条目 | 证据增量 | 证据行草稿素材 |
|---|---|---|
| `maven/sidecar-get-x-checksum-sha256-echo` | review_gate「一腿头面全收」兑现：A **GET=零校验头零 Etag / HEAD=Etag+源校验 triple 全键**（条件渲染路径=动词）；B GET+HEAD=Etag+仅 X-Checksum-Sha256（**maven 面独有，generic 面双端无头**）。GET 自加头同 nuget/push-201 已裁 BUG 族方向；HEAD 缺 Md5/Sha1 为同条目另一半 | `m1-{get,head}-sha1-set` `m1-get-md5-unset` `g1-get-sha1-set`（双轮） |
| `httpapi/original-checksums-key-model` | live 交叉证 ×2：maven 平面 FileInfo（无注册 jar：A oc={sha1,sha256} 无 md5 键 vs B 兜底 md5）+ wsrv 面（A 无 sha1 键 vs B 兜底）；另值维度证据见 N4（B srvgen 丢声明值，A 留档） | `m1-fileinfo-jar` `wsrv-fileinfo-jar` `s6-fileinfo-gav-jar` |

## NOT_RUN / BLOCKED 如实清单

- **mvn/wagon 真实客户端腿**：沿用 T-574/L039/L040 缓期口径（探针=python3 http.client，存储/REST 面 curl 级等价；协议腿待 N1/C1-maven 落地后随全模型验证）。
- **generic 面 HEAD sidecar**：仅 maven 面 HEAD 取证（A triple 发现面）；generic HEAD 未设腿。
- **charset-CT 的 PUT 建仓腿**：仅 POST update 面实证 404 变体；PUT+charset 未探。
- `remoteRepoChecksumPolicyType` **合法值**往返腿（goodenum echo）：未设（N1 仅 badenum 门面）。
- **C3（终缀大小写）/C6（policy 枚举 local 面大小写变体）**：不在本批臂集（L039/L040 同口径未复验，如实标注）。
- **C2 virtual 读面按需计算**：本批只证 local 成员面（N2）；virtual 读面沿用 L040 C2 证据。
- **B 面 X-Request-Id 稳定性**：请求级 UUID，compare 归一丢弃（其「A 缺席」面并入 N3 候选，未单独立案）。

## 环境处置

- A：`difftest-l041-*` 每轮脚本 teardown DELETE + 四证据轮 residual 全=[] + 收尾**独立**复验前缀计数=**0**；A 配置零触碰；shakedown 期手动复现探针（ctprobe/rmtprobe 两个临时 key）用后 DELETE，终态独立计数含其为 0。
- B：双实例（18156/18157）每轮 SIGTERM graceful（`"binflow stopped"` ×2 在案）+端口释放复验（lsof 空）+pgrep 无残留；shakedown 实例（18158）同处置；clean 复验双实例（18156/18157 复用）同处置；detached pristine worktree `git worktree remove` 完毕（worktree list 零 l041 项）。
- /tmp/l041：探针脚本+raw JSON×4 证据轮+shakedown×5+双 home server.log 留存备查；凭据处置注记——**probe 不记录任何请求头（Basic auth 零落盘）**；对产物的口令 grep 命中经逐处定位=①A/B 实例 remote 配置回读体中的**字段名**（该字段名与 difftest 环境所用**文档化默认口令值**同串，字段值为空串）②B 自身启动日志的默认口令告警（产品既有行为，非本批写入）；无凭据材料落盘，处置如实在案。
