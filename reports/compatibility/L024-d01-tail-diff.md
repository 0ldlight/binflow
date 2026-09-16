# L024-10 D01 尾差分报告（票乙 metadata 面 + 票丙 archive!/ 面——批次 2 最后差分）

- **模式**：dual（A 参照全程可达）
- **A 参照**：http://172.16.58.130:8082（Artifactory pro 7.161.15，admin）
- **B 被测**：http://172.16.58.130:8083（BinFlow dev.fbf72388，L024-8 乙/丙交付后）
- **规格基线**：docs/reverse/rest-api.md §3 表 + §3.1（L024-1 增量属性面 13 条）；docs/reverse/repo-operations.md §3.3（archive!/ 活体复核 4 条）；实现报告 reports/agents/L024-8.md（四项低置信待差分登记）
- **工具**：`tools/difftest/l02410/d01_tail_diff.py`（31 判定单元；wire 证据 `reports/compatibility/l024e-wire/{a,b}/`）；复跑门 pass1==pass2 判定集一致（SAME 14 / DIVERGENT 17）
- **normalize 提案（fixtures/normalize.yaml#d01-tail）**：D1 头 drop 集沿用；PN-ctx **contextPath 归一**（文案内插 `/artifactory` ↔ `/binflow`——BinFlow 根前缀为既定设计，repo.md §1；a03 除 context 外逐字同）；PN-hdr **Allow（405）/Cache-Control（?properties GET）头 drop**（A 带 B 缺的无语义裁决面，T7 聚合）

## 0. 摘要

| 口径 | 数 |
|---|---|
| 判定单元 | 31（metadata PATCH 13 / DELETE 4 / POST-405+动词+anon 7 / archive!/ 5 / stats 副作用 2） |
| SAME | 14（归一提案后 +2：m01b/m02b/m08b 仅 Cache-Control 差、a03 仅 contextPath 差 → **实质 SAME 18**） |
| DIVERGENT | 17（去族 **8 个 D-item**：BUG 4 / BUG-minor 2 / header 面 1 / 既有裁定 1） |
| 四项低置信定谳 | ①DELETE 失败文案 / ③!-无斜杠 generic 文案 / ④其它动词——全部 A 真值落定；②403 repoPath 分隔符未测（需受限用户构造，登记 Risks） |

## 1. 绿面（14 单元 + 归一后 4）

- **PATCH /api/metadata 九臂全绿**：新键数组（m01+verify k=[v1,v2]）/覆盖语义（m02+verify k=[v3]、seed 保留）/null 删键+幂等（m03/m03b）/`{}` → 400 `props or stats fields required` 逐字（m04）/数字值与对象值 → 400 `Failed to set properties on <repo>:<path>: Failed to parse json object while performing patch properties request.` 逐字含句号（m05/m05b）/missing item → **400 非 404** `Item <repo>:<path> does not exist` 逐字（m06）/virtual 仓 → 400 `Repository '<repo>' is not a local repository` 逐字（m07）/非文本元素静默跳过（m08+verify m=[v1,v2]）。
- **DELETE 主臂**：全删 204（m10）、无属性 204 幂等（m10b）。
- **POST /api/storage 405**：裸与 query 双臂 envelope `{"errors":[{"status":405,"message":"Method Not Allowed"}]}` 逐字（m12/m12b）；无仓段 POST → 404 `Not Found`（m12c，L010-2 面维持）。
- **archive!/ 命中族**：成员与嵌套成员 200+成员 CT+**字节同**（a01/a02）；miss 404 全文双绿（a03——full URI 含 contextPath + `; Path: '<repoKey>:<archivePath>'` 尾段逐字同，唯 context token 差=归一面）；成员 sha1 后缀 200 **内容同**（a05）。

## 2. D-item 详单（8 项）

- **T1【BUG·低置信①定谳】DELETE /api/metadata 失败臂**（m10c/m10d）：**A 对 missing item 与 virtual 仓均回 204（无守卫静默 no-op）**；B 回 400（借 PATCH 族文案 `Failed to set properties on …: Item … does not exist` / `… Repository '…' is not a local repository`）。B 修：去守卫，统一 204。
- **T2【BUG·低置信④定谳】/api/metadata 其它动词**（m14a/b/c）：PUT/GET/POST → **A=405 envelope `Method Not Allowed`（+Allow 头）**；B=404 E-26 `… is not implemented in BinFlow`。B 修：三动词 405（与 POST /api/storage 同法）。
- **T3【BUG·低置信③定谳】`!`-无斜杠 miss（generic 仓）**（a04）：**A=`File not found.; Path: '<repo>:<archive>!<entry>'`**（带句号+Path 尾段保 `!` 原样）；B=`Failed to find the requested resource '<repo>/<path>!.'`（generic resource 形）。B 修：文案对齐 A 形。
- **T4【BUG·§3.1-10 定谳（低置信升实测）】PATCH stats 腿**（m09/m09b）：PATCH `{"stats":{"downloadCount":1}}` 双端 204（m09 SAME）；**副作用 A=真 merge 落库**（downloadCount=1 写入 + `lastDownloadedBy:"import"` 标记 + `lastDownloaded:0`/`remoteLastDownloaded:0` 全字段回显、uri=repo-root 形）；**B=no-op**（downloadCount 0）+ 零值字段整体省略（lastDownloaded/lastDownloadedBy/remoteLastDownloaded 缺）+ uri=api/storage 形。B 修三面：merge 写入、零值字段回显、stats uri 形（A=repo-root 下载形）。
- **T5【BUG-minor】archive!/ 成员命中响应头族**（a01/a02）：A 带 `Accept-Ranges`/`Content-Disposition`/`X-Artifactory-Filename`/`X-Checksum-Md5/Sha1/Sha256` 六头；B 全缺（body 字节与 CT 双同）。
- **T6【BUG-minor】成员 sha1 后缀 Content-Type**（a05）：A=`application/x-checksum`；B=`text/plain`（sha1 内容同）。
- **T7【header 面·PN-hdr 提案】405 响应的 `Allow` 头 + ?properties GET 的 `Cache-Control` 头**：A 带 B 缺（m12/m12b/m01b/m02b/m08b 五单元唯差）——无客户端语义面（jf/真实客户端 blind），提案 drop；聚合登记。
- **T8【既有裁定】401 措辞族**（m13）：L020-3 广度定案在案（`Bad Credentials` vs `invalid credentials` + realm 名）。

## 3. spec 输入（docs/reverse）

1. §3.1-10 stats 腿定谳（升「高——活体」）：A=JSON-merge 落库，`downloadCount` 直写、`lastDownloadedBy` 写 `"import"` 标记、`lastDownloaded` 维持 0；?stats 回显六字段全量（uri=repo-root 形）。
2. §3 表 DELETE /api/metadata 行补：**失败臂无守卫**——missing item / virtual 仓均 204 静默 no-op（m10c/m10d 活体）。
3. §3.1 补：/api/metadata 其它动词（PUT/GET/POST）= 405 envelope（含 Allow 头）。
4. §3.3-3 定谳：`!`-无斜杠 miss 在 **generic 仓**同为 `File not found.; Path: '<repo>:<archive>!<entry>'` 形（L024-1 探针的 repo 类型疑虑关闭——generic/maven 同形）。

## 4. D01 翻态建议（落账归 conductor/compatibility-engineer）

| 行 | 建议 | 依据 |
|---|---|---|
| D01-R08 增量属性面（PATCH/DELETE/POST 405） | **候裁态**——PATCH 九臂逐字全绿 + POST 405 双绿；余 T1（DELETE 失败臂）/T2（其它动词）/T4（stats 腿）三面挂账 | §1/§2 |
| D01-R16 archive!/ 抽取 | **候裁态**——命中/miss/checksum 后缀语义与字节全绿；余 T3 文案/T5 头族/T6 CT 三小面 | §1/§2 |

## 5. 环境处置

双端 l024e-local/l024e-virt 删净（deleteContent）；零 build 产生；/tmp 夹具清除；本轮零 ssh 操作、零 jf config。

---

# §6 终确段（L024-12，2026-09-16——L024-11 后六臂重放，批次 2 末验）

- **B 被测**：dev.**0c5d2ce3**（L024-11 返工重部署；A 参照不变）
- **重放**：原驱动全量两轮（pass1==pass2 判定集一致：**SAME 23 / DIVERGENT 8**）
- **六臂对账**：**T1-T6 全部翻 SAME**——
  - T1 DELETE 失败臂：m10c/m10d 翻绿（B 去守卫，missing item/virtual 均 204 no-op 对齐 A）；
  - T2 其它动词：m14a/b/c 翻绿（PUT/GET/POST=405 envelope **含 Allow 头**对齐）；
  - T3 !-无斜杠：a04 翻绿（`File not found.; Path: '<repo>:<arch>!<entry>'` 逐字）；
  - T4 stats 腿三面：m09b **body 六字段逐字同**（merge 落库 downloadCount=1 + lastDownloadedBy="import" + 全字段回显 + uri=repo-root 形）；
  - T5 六头族：a01/a02 翻绿（Accept-Ranges/Content-Disposition/X-Artifactory-Filename/X-Checksum-Md5/Sha1/Sha256 齐）；
  - T6 x-checksum CT：a05 翻绿。
- **残余 8 单元 = 三归一面 + 一既有 + 一新微残**：PN-ctx（m09b uri 与 a03 文案内的 `/artifactory`↔`/binflow` contextPath——BinFlow 根前缀既定设计，非行为差）×2；PN-hdr Cache-Control（m01b/m02b/m08b）×3；T8 401 措辞族（m13）×1；**新微残：POST /api/storage 405 仍缺 `Allow` 头**（m12/m12b ×2）——L024-11 给 /api/metadata 动词 405 补了 Allow，**storage POST 面漏补**（BUG-minor，一字头部）。
- **四条开放项**维持不判（403 文案未测/lastDownloadedBy="import" 单点/maven 同形推论/T7+T8 归一族）。

## 6.1 D01 翻态终版建议

| 行 | 终版 | 依据 |
|---|---|---|
| D01-R08 增量属性面 | **候裁态→差一口**：PATCH 九臂/POST 405 envelope/DELETE 全语义绿；唯 `Allow` on POST /api/storage 405 微残（一字头部修后即翻 ✅）；PN-ctx/PN-hdr/T8 归归一面与既有裁定 | §6 |
| D01-R16 archive!/ 抽取 | **翻 ✅（VERIFIED）**：命中/嵌套/miss/checksum 后缀语义+字节+文案全绿；唯余 PN-ctx contextPath 归一面（实例 URL 空间既定设计，非行为差） | §6 |

## 6.2 终确环境处置

双端 l024e 两仓删净复核（A/B repos []）；零 build；/tmp 夹具清除。
