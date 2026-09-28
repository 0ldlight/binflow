# L031 — difftest v2 npm 批次：Facet 过滤 A 面差分臂（BIN-28 / T-545）

双发双系统：A = JFrog Artifactory **7.161.26**（参照实例，live 认证 2026-09-28）；B = **当前 worktree（claude/r3-fixes, 51c1a1c4）构建的 binflow-server**（127.0.0.1:18245，BINFLOW_HOME=/tmp/r3-bhome-t545 全新 scratch）。双客户端：curl（packument/tarball 面）+ **npm CLI 11.19.0 / node v24.20.0**（view/install/pack 面）。模式 **dual**。凭据全程经 `/tmp/r3-difftest.env` 环境变量注入，case 文件与证据零字面量（Authorization/NPM_AUTH 恒脱敏或仅在子进程环境存在）。

上游拓扑（受控假 npm 源，全部 `difftest-` 命名空间）：

- 假源 = A 实例上的 local npm 仓（`difftest-npm{1,2,3}-src`），以确定性预构 tarball（tar mtime=0 + gzip mtime=0，双轮字节恒定）经真实 npm publish 播种；dist-tags 刻意回拨 `latest→1.0.0`（上游同时存在 2.0.0）以压测 latest 语义。
- A 腿 remote 上游 = `http://192.168.120.38:8081/artifactory/api/npm/<src>`（**直连端口 8081**——路由端口 8082 的自引用 remote 拉取 404，2026-09-28 探针实证；上游凭据经 remote repo username/password 运行时注入）。B 无法被 A 回访（VPN utun4 无入向路由，探针实证），故假源必须栖身 A 上。
- B 腿 remote 上游 = 本机布局翻译代理（`<name>/packument.json` → A 的 `/<name>`；其余路径透传；运行时注入 A 侧 Basic 认证）——适配 BinFlow npm remote 存储布局 identity 映射的既有 wire 漂移（T-538 漂移点 A 同款 posture），**双端消费同一物理上游字节**。B remote `allowPrivateUpstream=true`（difftest 先例，SSRF 仓级豁免）。
- virtual 成员声明序 [remote, local]（remote 在前——压测 §1 locals 恒先）。

## 轮次与稳定性

| 轮 | 目录 | 结果 |
|---|---|---|
| round-0（作废） | run/l031-npm-r2（被覆盖） | 用例生命周期缺陷：a 腿 finally 误删共享假源，b 腿上游全 404——判 BLOCKED 级执行事故，修复后作废重跑（教训入 T-545 日志） |
| r2 | `tools/difftest/v2/run/l031-npm-r2/` | 3 case 全 FAIL，唯一差异 = latest 标签语义（逐 case 一致） |
| r3 | `tools/difftest/v2/run/l031-npm-r3/` | 与 r2 **全断言值逐项相同**（a/b 两面、3 case、每键比对脚本通过）——**r2≡r3 稳定判据成立** |

机读产物：`run/l031-npm-r{2,3}/results.json`（json 解析通过；`score.sh` 口径 PASS=0 / FAIL=3 / BLOCKED=0 / NOT_RUN=7——NOT_RUN 为未选中的 maven/demo case，非漏跑）。

## Case 1 — npm-virtual-packument-merge（curl 臂）：FAIL（B 违例，1 键）

| 面 | A（7.161.26 实测） | B（当前树实测） | 对照 |
|---|---|---|---|
| rmt 版本并集（3 版） | `1.0.0\|1.0.1\|2.0.0` | 同 | 一致 |
| rmt `latest`（上游回拨至 1.0.0） | **1.0.0（保留 base 成员标签）** | **2.0.0（重算为最大版本）** | **差异** |
| rmt `stable` 标签 | 2.0.0 | 2.0.0 | 一致 |
| shadow 同名同版本（local 在 [rem,loc] 声明序下） | shasum/description/tarball 字节 = **local**（fb09dc12…，locals 恒先 + putIfAbsent） | 同（同一 sha1 锚） | 一致 |
| local-only 包 | 1.0.0 经 virtual 可见 | 同 | 一致 |
| tarball 字节保真（shadow=local / rmt@1.0.1=上游） | 200+sha256-ok ×2 | 同（另 `X-Binflow-Resolved-From: difftest-npm1-rem`） | 一致 |
| 不存在的包 | 404 | 404 | 一致 |

## Case 2 — npm-cache-member-layout（cache 投影布局）：FAIL（B 违例，1 键，与 Case 1 同根）

| 面 | A | B | 对照 |
|---|---|---|---|
| 显式把 `<rem>-cache` 列入 virtual 成员（PUT virt2） | **400**「Repository difftest-npm2-rem-cache does not exist」 | **400**「invalid repository config: virtual repository member …」 | **一致（双端同拒）** |
| 重复 GET virt packument（聚合缓存窗口内） | 语义恒同（versions/dist-tags/shasum） | 同 | 一致 |
| SLIM 变体（`Accept: application/vnd.npm.install-v1+json`） | 200 + Content-Type 原样回 | 200 + 同 | 一致 |
| virt 基线 latest | 1.0.0 | 2.0.0 | **差异（同根）** |

## Case 3 — npm-cli-tarball-fidelity（真实 npm 客户端臂）：FAIL（B 违例，1 键，同根）

| 面 | A | B | 对照 |
|---|---|---|---|
| `npm view <rmt> versions --json` | 3 版并集 | 同 | 一致 |
| `npm view <rmt> dist-tags --json` | `latest=1.0.0\|stable=2.0.0` | `latest=2.0.0\|stable=2.0.0` | **差异（同根）** |
| `npm view <shadow>@1.0.0 dist.shasum` | local 副本 sha1 | 同 | 一致 |
| `npm install`（rmt@1.0.1+shadow+lonly 固定 pin） | exit 0，node_modules 三包 index.js 字节=fixture 锚 | 同 | 一致 |
| `npm pack rmt@2.0.0` / `npm pack shadow@1.0.0` | tarball sha256=上游/local fixture 逐字节 | 同 | 一致 |

## 裁定问题：A 面在 remote+cache 成员布局下滤 cache 投影吗？——**是（结构上无从不滤），且 cache 投影不是可显式声明的成员**

1. **`<K>-cache` 在 A 不是一等仓键**：显式列入 virtual 成员被 400 拒（"does not exist"）；`GET /api/repositories/<K>-cache` 亦 400。它只在两处可达——**存储层**（`GET /<K>-cache/.npm/<pkg>/package.json` → **200 + 缓存的上游 packument 全文**，本轮 live 实证 remote-cache-projection §2.1 + maven-npm-pypi §2.2 布局）与搜索域（§7.3）。B 面对同一 PUT 同样 400（不同错误文案）——**双端一致拒绝显式 cache 成员**。
2. 因此 §6 filterCacheRepositoriesDuplication（四桶序含任一 remote 本体 → 滤全部 cache 仓）在 A 是**纯内部装配行为**：外部无法构造「remote 本体 + 显式 cache 成员」布局来制造双查。其外在可观测面——合并正确、版本恰一次、locals 恒先、tarball 保真、重复读稳定——在受控布局下双端全绿；B 面（T-538 FacetCache 去重实现）与 A 在全部可观测维度一致，`X-Binflow-Resolved-From` 头证明 B 的解析落 remote 本体成员。
3. **`<virt>-cache` 聚合缓存**（§6，npm 特有、TTL 600s）：A 的 `.npm/` 隐藏布局不可经 `GET /api/storage/<virt>-cache/.npm` 列出（404 "Unable to find item"）；B 无该投影键（404 "Unable to find repository"）。存储布局层差异——maven-npm-pypi §5 已明示 BinFlow 以元数据表为中心、无需逐字复刻隐藏文件布局；对外协议面（重复读语义稳定）双端一致，判**非差分面**，登记顺带观察。

## 批次汇总

| case | 层 | verdict | 差异分类建议 |
|---|---|---|---|
| npm-virtual-packument-merge | L7 | FAIL | BUG 倾向（1 键：latest 重算，见下） |
| npm-cache-member-layout | L7 | FAIL | 同根（latest 键）；cache 成员布局本身双端一致 |
| npm-cli-tarball-fidelity | L5 | FAIL | 同根（view dist-tags）；字节保真双端全绿 |

**唯一实质差异（三 case 一致复现，r2≡r3）**：virtual packument 合并的 `dist-tags.latest` 语义——上游 latest 指向非最大版本时，A **保留产出成员（base）的标签原值**（无排除模式时不重算；§2.6「被排除模式过滤后……重算」的条件性读法与 live 行为吻合），B **无条件重算为最大版本**（T-538 实现注释「latest 重算」的读法）。建议分类 **BUG**（B 偏离参照面）但严重度低：真实 registry 的 latest 恒为最大版本，两语义仅在手工维护/时间窗发布的内部源上分叉。**规格侧**：maven-npm-pypi §2.6 的「latest 标签重算」措辞需要 compatibility-engineer 细化（条件性 vs 无条件），本报告不自行改 known-divergence.yaml / 规格文档。其余 tag（stable）的并集保留双端一致。

## 顺带观察（未裁定，移交后续）

- A 的 virtual packument 响应**无 ETag 头**（couch `_rev` 形态的 If-None-Match/304 面未在本批断言；npm CLI 全链已绿，协议影响未见）；B 同样无——记录为后续差分候选（ETag/304 面）。
- 合并 packument 的顶层 description = base 成员 packument 的 description（A/B 一致，随 base 的 latest 指向取 1.0.0 版文案）。
- npm publish 打到 **repo 直连路径**（`/<repo>/<name>`，非 `api/npm`）时 A 存成裸文件且 npm CLI 侧仍报成功——本轮 harness 探针踩坑记录（后改走 `api/npm` 协议路径），不构成差分面但值得 UI/文档关注。

## 遗留与建议处置

1. latest 重算差异：交 compatibility-engineer 裁定（BUG vs INTENTIONAL）+ §2.6 措辞细化提案；若裁 BUG 归 dev-package-npm 修 `internal/adapter/npm` 聚合的 latest 规则（条件性重算：仅排除模式生效时）。
2. T-538 漂移点 A（npm remote 上游 wire 布局 identity 映射）在本批继续以翻译代理适配——**参照直连形态已验可用**（A 的 `api/npm` 路径双形均可作上游），归属裁定仍待 architect/compatibility-engineer（UpstreamPath facet 候选）。
3. B remote 上游 300s assumed-offline 熔断 + 删除 virtual 方可清负缓存：difftest 布局纪律已固化进 case（proxy 先起、virt+rem 删重建）；产品面是否对齐 A 的负缓存语义，候选后续差分票。

## 证据与复跑

- case 定义：`tools/difftest/v2/cases/npm_virtual_packument_merge.py` / `npm_cache_member_layout.py` / `npm_cli_tarball_fidelity.py`（共享 `_npmlib.py`，fixture 锚内嵌确定性摘要）
- 双轮产物：`tools/difftest/v2/run/l031-npm-r{2,3}/results.json` + `evidence/<case-id>/{a,b}-leg.json,summary.json`（结论可反查双端原始断言值与 observatory 原文）
- 复跑：`set -a; source /tmp/r3-difftest.env; set +a; export A_BASE=… B_BASE=…; python3 tools/difftest/v2/runner.py --out <dir> --case npm-virtual-packument-merge --case npm-cache-member-layout --case npm-cli-tarball-fidelity`
- 双端实例处置：difftest- 命名空间 repo 双端已清空（A/B 各 `api/repositories` 过滤核查 = none）；B 进程（18245）与代理测试后回收（见 T-545 日志）。
