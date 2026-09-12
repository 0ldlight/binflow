# L012-2b — npm dist-tags 面实现票 · 双腿正式批跑终版（LOOP 012 轨道 2b）

- 票：LOOP 012 / L012-2b（dev-registry-adapter，npm 域唯一实例）
- 前序：L012-1 取证 `reports/compatibility/L012-dist-tags-evidence.md`（六项 conductor 裁定 D2/D3/D4/D5/D6/D7 全按参照对齐收 + 文案级 4 格 m05/m07/m08/m09）
- 模式：**dual**（参照 + BinFlow 双端活体）
- 双端：
  - A = Artifactory 7.161.20（:8082，admin）
  - B = BinFlow **uat-l0122b-l0122**（:8083；从**工作树**构建：a81cd053 + 未提交改动 = L012-3 httpapi + 本票 npm adapter；数据卷保留滚动）
- 客户端：npm CLI 腿 = **npm 10.9.8**（评估票钉的版本锚，/tmp/l0121/npm10 rig）；Go 测试内嵌腿 = npm 11.19.0（client_test 真客户端）
- 原始 wire：`reports/compatibility/l0122-wire/{a,b}/`（本票产物；L012-1 原始证据 `l0121-wire/` 未动）；curl 矩阵 `l0122-wire/curl-matrix-{a,b}.txt`

## 1. 六项裁定实现态（internal/adapter/npm）

| # | 裁定 | 实现 | 锚 |
|---|---|---|---|
| D2 | DELETE latest 后 GET 读时重算（「重算永生」） | `serveDistTags` GET/HEAD：tags 无 `latest` 键时以 `latestVersion(versions)` 现算补入；**已存在的 latest 永不覆盖**（客户端回滚指针合法）；纯读时投影，存储文档不动 | disttag.go GET 分支；`disttag_recompute_test.go` 三测 |
| D3 | PUT 不存在版本 404 文案版本位 | 新 `msgTagVersionNotFound = "npm package not found with name:%s, and version:%s"`（真版本值） | errors.go；`disttag_wire_alignment_test.go` |
| D4 | 幽灵包 404 = 参照裸文案 | `writeTagsLookupError` → `Not found`（dist-tag 面专属；packument 面的 `Package '<n>' not found` 不动） | 同上 |
| D5 | 坏 body → 400 + 中性文案（修 Go 内幕泄漏） | 单 tag PUT / legacy PUT 解码失败 → 400 `invalid dist-tag body`（不再拼 `err.Error()`）；测试断言无 `invalid character`/`cannot unmarshal` 泄漏 | 同上 |
| D6 | bulk 面全族 405（wire 对齐） | 集合 PUT/POST、单 tag POST → 405 `Method Not Allowed` + Allow 头（集合 `GET, HEAD`；单 tag `PUT, DELETE`）；原 bulk 实建分支删除 | `disttag_test.go` 翻转测试 |
| D7 | GET dist-tags 补 `Cache-Control: max-age=60` | 成功 GET 响应头直钉 | `disttag_wire_alignment_test.go` |

文案级 4 格：m05（`Not found`）/ m07（`and version:9.9.9`）/ m08+m09（`invalid dist-tag body`）全部落锚。

## 2. npm CLI 腿（15 用例，npm 10.9.8 经 wiretap）

**双端 case 退出码向量逐 case 相同**（`diff _summary` 零差）：p1-p4 publish 全 0；ls1/add1/add2/ls2/rm1/scope-ls/scope-add/scope-rm/notag-ls 全 0；rm2-nonexistent/ghost-ls/badver-add 全 1（两端同因：客户端短路/E404/版本不存在 404）。

关键 wire 逐项：

| 用例 | A | B | 判 |
|---|---|---|---|
| notag-ls（D2 判别） | 200 `{"latest":"1.1.0"}` + `Cache-Control: max-age=60` | 200 `{"latest":"1.1.0"}` + `Cache-Control: max-age=60` | **D2+D7 双绿**；重算语义与参照一致（npm ls 不再报 No dist-tags found） |
| badver-add | PUT 404 `…name:l0121-probe-pkg, and version:9.9.9` | PUT 404 同文案逐字（仅 pretty/compact 差） | **D3 绿** |
| ghost-ls | 404 `Not found` | 404 `Not found` | **D4 绿** |
| rm1/rm2/scope 族 | 200 空 / 客户端短路 / 201 | 同 | 持平（K60 不变量保持） |
| 响应头 | `X-Artifactory-*` 族 | `X-Request-Id` | 产品头差异（非协议面） |

## 3. curl 20 格矩阵终判分布（归一口径）

归一规则组（本票口径）：(a) JSON pretty/compact 与 `{"ok": "…"}` 空格归一；(b) 已裁定差异不计 drift——m02 实例配置臂（A 匿名全局关/B `ANONYMOUS_ACCESS=true`，差分以认证臂为准）、m08/m09 D5 裁定（B 保持 400+中性文案，A 的 500 是参照自身瑕疵，建议契约钉 400 并记 known-divergence）、m10/m18 401 文案大小写（K60 §6 已裁定 B errors[] 小写风格）。

| 终判 | 格 | 说明 |
|---|---|---|
| **归一后逐字全同（15/20）** | m01, m03, m04, m05, m06, m07, m11, m12, m13, m14, m15, m16, m17, m19, m20 | m05 幽灵文案、m07 版本位文案、m12 再删 404、m13-m15 405 文案、m19/m20 清理探针全部逐字对齐 |
| **裁定内差异（5/20）** | m02, m08, m09, m10, m18 | 全部有既定裁定背书（见上归一规则 b），零未解释 drift |

对照 L012-1 基线：当时 8 格实差（m05/m07/m08/m09/m13/m14/m15 + D2 客户端可见分叉）→ 本票后 0 格未裁定差。

## 4. 参照自证：max-age=60 的客户端陈旧窗是参照自身行为

D7 落地后 Go 测试 M24（npm 11.19.0）出现 `dist-tag add` 后立即 `dist-tag rm` 报 `beta is not a dist-tag`——npm 将 add 预检的 GET（beta 建立前）按 `max-age=60` 缓存、rm 复读旧值。**在 Artifactory 参照上同序列复现同样失败**（本票 live 实证，:8082，npm 11.19.0：add `+beta: …@1.1.0` 后立即 rm → `npm error beta is not a dist-tag on …`）。结论：该陈旧窗是「参照头面 × npm 缓存」的组合行为，非 BinFlow 缺陷；M24 以 `--prefer-online` 顶掉（与 L012-1 取证 rig E0-2 同一缓解），并在测试注释中记录参照复现事实。

## 5. K60 族零补证（D12-R03 转写建议）

本批 m16（`GET /-/ping` → 200 `{}`）、m17（whoami 认证 200 `{"username":"admin"}`）、m18（whoami 匿名 401 + `WWW-Authenticate: Basic realm="BinFlow Realm"`）三格与 L012-1 §5 抽核一致（第二次独立复核）。**建议**：compatibility-engineer 落 `docs/compatibility/contracts/npm.yaml` 时将 K60-2/K60-3/§5-1 三条从 npm.md 规格零补证转写为契约条目（两次独立 wire 批跑同形，证据充分），不再另开取证票。

## 6. 遗留与建议

1. **packument 面的 latest 重算未裁定**：D2 证据只覆盖 dist-tags 端点族；`GET /<name>` packument 在 latest 被删后仍按存储文档渲染（无 latest）。参照行为未取证（`npm install pkg` 解析 latest 的分叉面）。已用 `TestDistTagRecomputeIsReadTimeOnly` 钉住现状——若后续裁定 packument 也重算，翻该断言即可。建议 compatibility-engineer 补一条 packument 臂取证。
2. **契约票素材就绪**：`docs/compatibility/contracts/npm.yaml` 首文件可直接以本票 §1 表 + §3 归一规则组为蓝本（GET dist-tags 200 map+CC:60/404 `Not found`、PUT 单 tag 201/404 版本位、DELETE 200 空/404 tag 位、%2f/%2F 等价、bulk 全族 405、坏 body 400+中性文案）。
3. 金样采集候选不变（L012-1 §3 提金清单 + 本票 m07/m13-m15 新逐字对齐格）。
4. 预存在无关失败（非本票）：`internal/adapter/rpm` `TestRestPolicyKeysDriveReindexBranches` 与 `internal/adapter/helm` `TestRestEnforceSwitchesDriveUploadHook` 在本票改动 stash 后仍失败——指向工作树中 L012-3 未提交的 `internal/httpapi/{properties,storage}.go` 配套测试未同步，交 conductor 转 L012-3 收尾。

---
*证据索引：`tools/difftest/l0121/{wiretap.py,disttag-family.sh,curl-matrix.sh}`（rig 未改动；family 以 sed 重定向 LOGS 至 l0122-wire）；实现 `internal/adapter/npm/{disttag.go,errors.go}`；测试 `disttag_{recompute,wire_alignment}_test.go` + `disttag_test.go` + `client_test.go`（M24 --prefer-online）。*
