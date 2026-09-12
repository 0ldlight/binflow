# L013-4+5 · R-15 预授权探针 + packument 差分腿（结论件）

- 模式: **dual**；端点/重建/wire 目录同 `L013-maven-evidence.md` §0（wire: `l0134-wire/{a,b}/r15/` 与 `l0134-wire/{a,b}/npm/`）
- 脚本: `tools/difftest/l0134/r15-probe.sh`、`tools/difftest/l0134/packument-probe.sh`
- 台账源: docs/prd/pending-rulings.md §2 R-15（v2.1）+ §4-4 预授权式；docs/compatibility/known-divergence.yaml `npm/packument-latest-recompute`

## 1. R-15c/d 四象限选案判定：**参照=过滤（空集）→ 案乙落格 = 小实现翻 ✅（开实现票：`?project=<非空>` → `[]`）**

参照实测（r15-probe a，全臂 200）：

| 臂 | 参照 (:8082) | BinFlow (:8083) |
|---|---|---|
| c0 baseline | 205B 全量（1 仓） | 167B 全量（1 仓） |
| c1 `?project=l0134-nonexistent` | **`[ ]`（过滤！）** | 全量（忽略） |
| c2 `?project=`（空串） | 全量（空串=无参） | 全量（同） |
| c3 `?project=unknown&type=local` | `[ ]` | 全量 |
| c4 `?type=l0134-nosuchtype`（家族对照） | `[ ]` | `[]` |

- **「零代码翻 ✅」假说不成立**：参照对未知 project 返回空集而非全量——参照**未忽略**该参数（Projects addon 未激活态下仍按过滤语义回答空集）。
- 四象限落格：〔参照=过滤/空集 × BinFlow=空集（案乙）〕= **小实现翻 ✅ + 差分臂**；现格〔参照过滤 × BinFlow 忽略〕=「静默超集」格（PM 反对），必须移出。
- 实现票口径建议：非空 `project` 值 → 空数组（与 c4 非法 type 家族姿态一致）；**空串维持无参语义**（双端一致，c2）；组合臂随主臂（c3）。known-project 臂本环境不可测（参照无 /api/projects 端点、无项目域）——空集语义在无项目域下自洽（truthful empty），不构成阻塞。
- 契约断言素材：c0/c1/c2/c3/c4 五臂 wire 双端齐全。

## 2. R-15a propertiesXml 归属面双探：**两面皆服务（api 面=带 XML 声明；file 面=无声明）——「记载分叉」以「两面都对、形态不同」定案**

参照实测（预置属性 l0134k=l0134v 后）：

| 臂 | 参照 | BinFlow |
|---|---|---|
| p1 api 面 `GET /api/storage/{r}/{p}?propertiesXml` | **200 `application/xml`**，`<?xml version='1.0' encoding='UTF-8'?><properties>…`（94B） | **404** `{"status":404,"message":"/binflow/api/storage item query 'propertiesXml' is not implemented in BinFlow"}` |
| p2 file 面 `GET /{r}/{p}?propertiesXml` | **200**（ct `application`），`<properties>…` **无 XML 声明**（54B，与 api 面同体异头） | **200 但回原始文件字节**（参数被整体忽略） |
| p3 api 面无属性 | 404 `"No properties could be found."`（JSON） | 404 not-implemented 同 p1 |
| p4 file 面无属性 | 404 同文案（ISO-8859-1 变体） | 200 原始文件字节（无 404 臂） |
| p5 JSON 孪生对照 `?properties` | 200 `application/vnd.org.jfrog.artifactory.storage.ItemProperties+json` | 200 JSON 同构 |

裁定素材三条（呈 PM/compatibility-engineer）：

1. **rest-api.md §1.1 行 30（file 面）与 matrix D01-R03（api 面）的记载分叉实为两面并存**，且两面 wire 形态不同（声明/Content-Type）。若裁「实现」（案 A），规格需按两面两形态落；若裁「维持」（案 B·PM 现荐），登记面必须**同时覆盖两面**。
2. **案 B 前提「501 显式拒绝」与 as-built 不符**：BinFlow api 面实为 **404 + not-implemented 信封**（matrix 行注记的 501 已漂移，rest-compat-matrix §2 行 3 需复核）；更关键的是 **file 面不是拒绝而是静默吞参回文件字节**（p2/p4）——与参照（file 面出 XML/404）构成**未登记的真实 wire 差异**。维持案需把 file 面从「吞参」改为显式姿态（404/501）才达到「诚实缺位」标准，否则该案不成立。
3. 顺带取证：属性**写面**在 `/api/storage` 面（`PUT …?properties=k=v`）——file 面 PUT 带参=普通重部署（参数被吞，201 零属性）；参照 api 面 PUT 回 201 FileInfo，BinFlow 回 204（次要差异，随主裁定登记）。

## 3. packument 判定素材（known-divergence `npm/packument-latest-recompute`）：**差异成立，分类建议 BUG**

双端 npm 仓 `l0134-npm`，npm CLI publish 1.0.0（--tag l0134base）+ 1.1.0（latest）→ `DELETE /-/package/{pkg}/dist-tags/latest` → 双读面：

| 步 | 参照 | BinFlow |
|---|---|---|
| n1 packument（删前） | 200 dist-tags `{latest:1.1.0, l0134base:1.0.0}` | 200 同 |
| n2 dist-tags 端点（删前） | `{latest:1.1.0,…}` | 同 |
| n3 DELETE latest | 200 | 200 |
| n4 **packument（删后）** | **200 `latest:1.1.0` 仍在**（读时重导出） | **200 `dist-tags` 仅剩 `{l0134base:1.0.0}`——latest 消失** |
| n5 dist-tags 端点（删后） | `{latest:1.1.0,…}` | `{latest:1.1.0,…}`（端点族重算） |
| n6 PUT latest 还原 / n7 | 201 / latest=1.1.0 | 201 / latest=1.1.0 |

- **BinFlow 两读面互相矛盾**（packument 面不重算 latest，端点族重算）；参照两面一致保 latest。台账 rationale 所引 `TestDistTagRecomputeIsReadTimeOnly` 钉住的「packument 投影与端点族同源重算」在 bebd92b1 wire 上不成立——**疑似账实不符或回归，按 §12 上报**。
- 影响链：`npm install pkg`（走 packument latest）在 DELETE latest 后于 BinFlow 得 404 latest 缺位（无法解析安装目标），参照仍装 1.1.0。
- 修复口径素材：packument GET 的 dist-tags 投影复用端点族同源 latest 重算即可（一处投影改动）。

## 4. 清理与残留

- 双端 difftest 仓已删净（ref 剩 example-repo-local；bf 剩 docker-local——均基线态）；BinFlow 删除走 `?deleteContent=true` 扩展（参照不需要，200 直删）。
- /tmp/l0134 全清（含 npmrc/mvn settings/构建中间物，无凭据残留）。
- 未 commit、未写 BOARD、未改码与契约。
