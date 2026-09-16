# L025-4 D02 配置族全域差分报告

- **模式**：dual（A 参照全程可达）
- **A 参照**：http://172.16.58.130:8082（Artifactory pro 7.161.15，admin；非 admin 臂用户 `l025q-u` 用毕删）
- **B 被测**：http://172.16.58.130:8083（BinFlow dev.498683c5——票甲 7661853b + 票乙 498683c5）
- **规格基线**：docs/reverse/rest-api.md §2.1（L025-1 五面规格）；实现报告 L025-3A/3B（四臂待验证登记）
- **工具**：`tools/difftest/l0254/d02_config_diff.py`（36 判定单元 + 定向补探针；wire `reports/compatibility/l025q-wire/{a,b}/`）；复跑门 pass1==pass2 判定集一致（SAME 22 / DIVERGENT 14）
- **normalize 提案（fixtures/normalize.yaml#d02-config）**：Q1 头 drop+vendor CT 等价（沿先例）；Q2 host 占位；Q3 **config 体=结构投影比对**（分组键/组内排序/断言标量），全字段集差记「模型级漂移度量」不改判定；**Q4 batch-delete reports[]=集合序比对**（A=HashSet 不稳、B=批序——规格明载）；Q5 repolayouts `repositoryAssociations`=实例态（各实例实际用布局的仓不同——结构比三键在场即可）

## 0. 摘要

| 口径 | 数 |
|---|---|
| 判定单元 | 36（读族 18 / batch 写族 14 / 补探针 4）+ 集合化复判 3 |
| SAME | 22（集合化/隔离复判后 **+4**：w13〔隔离重探逐字同——pass1 分歧系 w12 状态级联〕、w14〔同因〕、w11〔Q4 集合同〕、v03〔显式 CT 头重探 406 同〕） |
| DIVERGENT | 14 → 实质 **9 个 D-item**：BUG 5 / 模型级大缺口 1（账实不符上报） / header 面 1 / spec 勘误 2（含在 BUG 内） |
| 四臂定谳 | 空数组 201 ✓ / blank-key **A=ghost 逐仓非预校验**（spec 证伪）/ PUT 非 admin=裸 `Forbidden` / POST 非 admin=`Only platform/project admins...`（spec 反编译文案证伪）；**真 207/全败+失败报告值域=不可安全构造维持开放**（共享参照无法按需注入真删除失败——不碰系统仓/不造破坏性并发） |

## 1. 绿面（22 单元 + 复判 4）

- **读族结构面全绿**：configurations 分组/排序/CT/no-store/过滤空集 `{}`/非 admin 403 信封 `Forbidden`（c01-c03）；existence 四臂（命中形/逗号 400 `Invalid repository type: local,remote`/重复参 OR/`project=` 静默忽略）（e01-e04）；v2 读 404 信封 `The repository <key> was not found`（v02）；**v2 CT 协商怪癖 406 `Not Acceptable` 双同**（v03 显式头重探）+ 不匹配 Accept→200（v04）；**非 admin 五键部分视图逐字同**（v05——remote 臂规格面）；批读 ghost 静默省略/逗号单 key 落空 `{}`/缺 names 400 `Repository keys are missing.`（b02/b03）；布局列表 25 **body IDENTICAL**（l01）、缺名 500 `No value present`（l03）、旧挂载双 404（l04a/b）。
- **batch 写族逐字面全绿**：批建 **201 体逐字节**（w01——尾空格+空行）；整单 400+回滚（w02/w02b 复核 404）；缺 key 400 `Repository key are missing in configuration`（w03）；**空数组 201 空体（四臂①——A 同形）**（w04）；merge 200 裸串 `Repositories updated successfully.`（w06）；ghost 404 裸文本 `No repositories found for the following keys: <k>`（w07）；空删 400 `{"statusMessage":"No repository keys were provided for deletion"}` 无 reports（w09）；全 ghost 200+去重+聚合文案（w10，Q4 集合同）；混合删 local+virtual+remote+ghost **全 success 形+三 rclass 文案（remote=local 形复证）**（w11，Q4 集合同）；非 admin 预校验 403 单报告**逐字同**（w13 隔离重探：`Cannot delete repository: 'k', Reason: User: ('u') has insufficient permission to delete repositories: k`）。

## 2. D-item 详单（9 项）

- **G1【模型级大缺口·账实不符上报】config 渲染投影 5-8 键**（c01/v01/v04/b01/w06b 五单元）：A=configurations 全量 50+ 键/v1 单仓 61 键/v2 16-18 键；**B=统一 5 键投影 `{description,key,packageType,rclass,url}`**（且 local 仓也带 `url`）——批读（b01）与 admin 读（w06b）与非 admin 五键视图（v05）**同一投影**。**实现报告 L025-3A 宣称「已建模键子集 17/18/40/9 键」与部署实例 wire 不符**（疑似票甲渲染层未达 dev.498683c5 或投影路由退化）——matrix ✅ 宣称与账实不符，上报 conductor 定因。
- **G2【BUG·四臂③定谳+spec 证伪】DELETE blank-key**（w12）：**A=按 ghost 逐仓处理**（`["k",""]` → 200，`""` 行 success:true `Cannot delete repository: '', repository config does not exist`，真仓正常删）；B=400 预校验单报告中止——**spec §2.1.7-2「blank key 整单中止」被活体证伪**。B 修：blank 落逐仓 ghost 形。
- **G3【BUG·四臂④定谳+spec 证伪】POST 批改非 admin 403**（w08）：**A=403 裸文本 `Only platform/project admins are allowed to update repositories`**（CT application/json）；B=`User is not authorized to update the following repositories: <k>`（spec §2.1.6 反编译中置信文案被证伪）。B 修文案 + spec 勘误。
- **G4【BUG·四臂②定谳】PUT 批建非 admin 403**（w05）：**A=errors 信封裸 `Forbidden`**（与 configurations 403 同文案）；B=`administrator privileges required`。B 修文案。
- **G5【BUG·跨切面新发现】v1 单仓 GET 未知 key**（w02b）：**A=400 envelope `{"errors":[{"status":400,"message":"Bad Request"}]}`**（裸！）；B=404 `Repository does not exist: repo "<k>": repository not found`。B 修：状态+文案（v2 读的 404 信封形维持不变——v02 绿）。
- **G6【header 面·PN】admin/repolayouts 的 CORS+Cache-Control 头族**（l01/l02）：A 带 `Access-Control-Allow-Headers/Methods` + `Cache-Control`；B 全缺（body IDENTICAL/实例态 associations 差除外）。提案 drop/补齐裁定。
- **G7【开放·不可构造】真 207/全败聚合 + DELETE 失败报告字段值域**：共享参照上无法安全按需注入真删除失败（不动系统仓/不造破坏性并发）——维持开放（B 单测注入证据在案：混合 207+全败首状态码+失败报告全字段集）。
- **G8【spec 勘误随 G2/G3】**：§2.1.7-2 blank-key 臂改 ghost 逐仓形；§2.1.6 非 admin 文案改 `Only platform/project admins are allowed to update repositories`。
- **G9【既有裁定】401/403 措辞族关联面**：v05/w05 等非 admin 403 文案差除 G3/G4 点名外，其余 403 面归 L020-3 广度族口径候裁。

## 3. matrix D02 批次 3 行翻态建议（落账归 conductor/compatibility-engineer）

| 行（批次 3 面） | 建议 | 依据 |
|---|---|---|
| configurations 全量读 | **不翻**（G1 投影大缺口；结构面绿） | c01-c03 |
| existence | **翻 ✅（VERIFIED）** | e01-e04 全绿 |
| v2 单仓读 | **不翻**（G1；406/404/非 admin 五键面绿） | v01-v05 |
| v2 批读 | **不翻**（G1） | b01-b03 |
| repolayouts 族 | **候裁态**（body IDENTICAL；G6 头族裁定） | l01-l04 |
| PUT 批建 | **候裁态**（201 体逐字节/回滚/空数组绿；G4 文案） | w01-w05 |
| POST 批改 | **候裁态**（merge/ghost 绿；G3 文案） | w06-w08 |
| DELETE 批删 207 状态机 | **候裁态**（状态机可达臂全绿含四臂①；G2 blank-key；G7 开放） | w09-w14 |

## 4. 环境处置

双端 l025q-* 仓（seed/virt/rem/b1/b2/t1/t2）全删复核（A/B residue []）；非 admin 用户 `l025q-u` 双端删除复核（404）；/tmp 清理；本轮零 ssh。
