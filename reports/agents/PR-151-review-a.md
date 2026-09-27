# PR-151 评审报告（形态: reviewer-a · correctness）

```
Ticket:        PR #151（develop 收口）= edb3114c (T-520) + 0818159e (T-521) + 6d5f4d1a (T-522) + 84fab27d (T-523)
Role:          code-reviewer (reviewer-a)
Area:          docs/design + DECISIONS.md / fern en 站 / Makefile + .circleci + .github + ci/ + tools/openapi-spec / tools/difftest/v2（T-523 中途扩入）
Input:         conductor 派发（三提交清单 + 五项重点；T-523 扩票指令：凭据纪律/断言可证伪性/BLOCKED 语义/python 正确性/README ctx.sides）
Changes:       git diff develop...HEAD 全量 31 文件通读；上下游追读：internal/httpapi 路由（server/router/system/console）、internal/repo（virtual/dockervirtual/api/service/operations/remoteexternal）、internal/remote/fetcher、internal/metadata/migrations、charts/ 与 deploy/、fern/docs.yml 与 en 导航、tools/difftest/v2/runner.py 全文、docs/reverse/ 四份规格锚点
Files:         逐文件结论见下
Tests:         见 Commands 段（全部只读取证；含 standing UAT 6×GET、双 mock 两轮差分框架复跑、Maven Central 上游哈希活体核验）
Commands:      见下方原文
Outputs:       reports/agents/PR-151-review-a.md（本文件）
Compatibility: docker virtual 表述与 as-built 一致；ADR-0051 引用锚全实存；T-523 四例 EXPECTED 逐一对照 docs/reverse/ 规格原文核验（非捕获即真相）
Security:      凭据纪律逐路径核验（见 T-523 段）；ui-smoke.sh 无注入面；openapi-spec 仅文案；无凭据/内部地址落盘
Performance:   spec-check <1s；ui_smoke nightly 单 job；difftest poll 预算固定不放宽
Risks:         cimg/go python3 存在性（fail-loud）；nightly 休眠待用户建 trigger；case 2/4 双发补跑时 B 面预期红（已登记缝/裁定）
Blockers:      无
Next:          见结论区后「范围外发现 + 交 conductor 事项」
```

## 结论: APPROVE

四提交全部按各自 AC 交付，关键声明独立复现成立，无 blocking 发现。分级发现：Critical 0 / High 0 / Medium 0 / Low 7。

### 必须修改（blocking）

无。

### 建议改进（non-blocking，按重要性排序）

**T-523 域（tools/difftest/v2）**

- **[Low-1] cases/maven_virtual_delete_passthrough.py:35-42** — `EXPECTED["delete_via_virtual"]="404"` 在 BinFlow 面**必红**：as-built 是恒 405（`internal/repo/virtual.go:72-84` refuseVirtualDelete，`internal/repo/service.go:1403-1410` 自注 "BinFlow's deliberate incompatibility"），且该语义正是本 PR 内登记的 **T-524 未裁定项**（INTENTIONAL 候选 vs 对齐，BLOCKED 待参照活体验证）。规格-of-record（repo-semantics §8.2 L220 已按 virtual-resolution §7.5 勘误为 404 ITEM_NOT_FOUND，高置信但反编译单源自记高风险、建议差分腿）支持 404——EXPECTED 推导方法论正确，用例恰是裁定 T-524 的仪器。缺的是交叉引用：用例 docstring 与 T-523 报告均未链接 T-524/service.go 蓄意不相容注记，将来双发 FAIL 需要对 open ruling 解释，且若裁定落 INTENTIONAL 本例 EXPECTED 须同步翻新（或标 known-divergence）。→ docstring 加一行 T-524 链接即可。
- **[Low-2] cases/maven_resolve_remote_cache.py:47-51** — `EXPECTED["cache_projection_artifact"]="200"` 在 BinFlow 面必红：`GET /<K>-cache/<path>` 直访是 T-520 设计文档 §9 **F1 未实现缝**（internal/ 无 -cache 路由，grep 证实）。规格锚（remote-cache-projection §2.1 L56「可用，高置信」）成立，T-523 报告风险 ⑫-1 泛化预告了「未实现 → FAIL=合法 DIFFERENT」，但未点名 F1。→ docstring 注明 F1 缝关联，避免双发后重复立项。
- **[Low-3] cases/_mavenlib.py:161** — DOCTYPE 拒绝只扫 `body[:2048]`，而 docstring 声称 "rejecting any DOCTYPE"。前置长注释的 prolog 可把 DOCTYPE 推过窗口；stdlib ET/expat 不拦**内部**实体展开（billion laughs），威胁模型（敌意被测实例）下是 DoS 面。→ 改扫全 body（`b"<!DOCTYPE" in body`），一行修。
- **[Low-4] cases/_mavenlib.py:257-282（judge）** — 库函数层面：若某边断言 dict 为空/缺键，`a_viol/b_viol/ab_div` 全空会误判 PASS（空集∩期望=无违例）。现有四例在所有分支都填满全部 EXPECTED 键（逐例验证）且 SetupError/TransportError 均先走 BLOCKED，结构性无恙；这是留给后续用例作者的潜伏脚枪。→ 可加每边键集 === EXPECTED 键集断言（违反记 BLOCKED instrument error）。

**T-522 域（CI/工具链）**

- **[Low-5] ci/ui-smoke.sh:36-38** — 头注释称基址解析「mirrors ci/protocol-matrix.sh's transition probe」，但 protocol-matrix.sh 本体无此探针（无 BINFLOW_BASE 直接 FATAL，:156-159）；被镜像的探针在 `.circleci/config.yml` protocol_leg step（:491-498）。行为正确（CI 无参形态实跑 6/6 绿），仅注释归因错一个文件。
- **[Low-6] ci/ui-smoke.sh:50** — 回退基址硬编码 `52.79.109.153:8080`，与 config.yml:307 既有默认一致（非新暴露）；迁 UAT 主机时是第三处要同步的 IP 常量。
- **[Low-7] Makefile:243-250（spec-check）** — `git diff --quiet` 对 untracked 边缘形态盲、本地浅克隆可能误报（T-522 报告风险段已自记，失败信息含修复指引，CI 两面全克隆不受影响）。可接受现状。

### 分文件结论

| 文件 | 结论 |
|---|---|
| **T-523：tools/difftest/v2/** | |
| cases/_mavenlib.py | 凭据纪律闭环：settings.xml 只含 `${env.DIFFTEST_MVN_USER/PASS}` 占位（:35-41），凭据仅进 subprocess env（:225-227），argv（cmd 列表 :214-224）无凭据，scratch 目录 finally 清理（含 SetupError 传播路径）；grep 无凭据值/参照地址；mock 实跑 evidence 检视无泄漏。poll_until 预算固定不放宽、超预算返回末态可证伪；mvn FileNotFoundError→SetupError→BLOCKED、TimeoutExpired→记录 timeout 值→FAIL 不悬挂；parse_metadata 命名空间宽容 + DOCTYPE 拒绝（见 Low-3 窗口 nit） |
| cases/maven_virtual_deploy.py | 断言全集在所有分支填满（10/10 键）；规格锚逐条核验：§8.2 L217 写路由、§1.1 PUT 201/GET 200、§1.3 unique→timestamped、§1.4 异步计算+版本组/快照目录规则+Maven 比较器（latest 含 SNAPSHOT、release=末位非 SNAPSHOT）、§5.2 discard_active_reference 默认策略对无 repositories 的 fixture 为 no-op（pom 字节可比）——全部与规格原文一致，非捕获即真相。group metadata `versions==[1.0.0, 1.0.1-SNAPSHOT]` 精确序符合比较器 |
| cases/maven_resolve_remote_cache.py | 上游 sha256 常量活体核验：repo1.maven.org 实拉 `javax.annotation-api-1.3.2.pom` = `46a4a251…f06b97` 与内嵌常量逐字节一致。删仓清缓存前置（§1.4）成立；HIT 判据诚实降级（投影持有+字节精确，wire 级证明声明为局限）；`cache_projection_artifact` 见 Low-2 |
| cases/maven_virtual_metadata_merge.py | EXPECTED 与 virtual-resolution §5.1 L84-93 逐行对应：并集重排/latest=排序末位/release=末位非 SNAPSHOT/合并不写 `<virtual>-cache`（区别 npm）；成员 metadata 异步→先 poll 成员再读 virtual，序正确；`<VIRT>-cache` 探测双面均应 404（B 面无此仓 ✓、A 面合并不落缓存 ✓） |
| cases/maven_virtual_delete_passthrough.py | 断言链（PUT 201/解析 200/virtual DELETE 404+成员存活/直删 204/再解析 404）全部规格推导（§1.1 DELETE 204 + §8.2 L220/§7.5 404）；见 Low-1 的 T-524 耦合注记 |
| _smoke_mock.py | 定位明确（接线自检非 oracle）；线程化 stdlib；DELETE=200 等哑行为被用例如实记为规格违例 |
| README.md（+17/-1） | ctx.sides 纠错与 runner.py 实际形态吻合（side_config 返回 plain dict，旧文档 `.base` 属性访问会 AttributeError——T-523 报告偏差①如实记录了 mock 首轮暴露该错的经过）；Maven 批次表格的规格锚与用例 docstring 一致 |
| reports/agents/T-523.md | 声明全部复现：--list 5 例 ✓、dry-run NOT_RUN=5 ✓、无 env BLOCKED=5 逐例六变量 reason ✓、双 mock PASS=1/FAIL=4 连续两轮稳定 ✓、mvn 四腿 exit=0 落 evidence ✓、真实差分结论 0（诚实）✓、run/ gitignored ✓ |
| **T-522：Makefile / CI / ui-smoke.sh / openapi-spec** | |
| Makefile | spec/spec-check 逻辑正确（重生成→git diff 门→exit 1 传播，实跑验证）；.PHONY 补齐；dev 前置门纯加法 |
| ci/ui-smoke.sh | set -euo pipefail；curl 失败归约为可判状态；六腿独立不互相遮蔽（死端口实跑 6×FAIL+exit 1）；grep -m1+`\|\|true` 规避 pipefail 陷阱（注释自记）；六条路由对代码事实逐一核验（/healthz server.go:548、301 console.go:99、id="root" 双 shell 俱在故以 bundle script 为判别、docs server.go:325、version 匿名 router.go:351-355 routeAuth{}）；CI 无参形态对 standing UAT 实跑 6/6 绿 |
| .circleci/config.yml | `circleci config validate` 通过；漂移门挂 build 双 shard；nightly 增 ui_smoke 与既有两腿并行；env 注释修正实证（add_ssh_keys 读 $SSH_KEY_FINGERPRINT_UAT :270-274，UAT_SSH_FP 全仓唯一残留即更正注本身） |
| .github/workflows/ci.yml | 漂移门在 checkout 后 setup-go 前，ubuntu-latest 自带 python3/git/make，定位正确 |
| tools/openapi-spec/ 3 文件 | 4 行纯描述文案对齐；`make spec` 重生成与提交 artifact 字节一致（324268 bytes，git diff 零输出）——存量漂移归零成立，门不落地即红 |
| **T-520：DECISIONS.md / 设计文档** | |
| DECISIONS.md | +15/-0 只追加 ✓；ADR-0051 追加于 0050 后（0037 缺口在档预留 :802）；四段序三处表述一致；I1-I12 逐条溯源 §2.2/§2.3/§3；O1-O5 与软缝协议同款且差分可观测；0013 勘误正确废止联动记录①两桶序并逐列维持项；0012 勘误五严格限引用面注记 |
| docs/design/virtual-four-bucket.md | 内部自洽（展开规则↔I5-I8、三遍扫描↔I2/I4、投影零存储面承诺贯穿）；引用代码锚点全实存（virtual.go:110/242/424/134-139、service.go:2407、api.go:506-512、fetcher.go:77/938、operations.go:521、dockervirtual.go:62/595、remoteexternal.go:66、repo_config_render.go:219/330、validate.go 在位） |
| **T-521：fern en 9 页 + 2 index** | |
| fern/translations/en/pages/** | en/zh 全 9 页反引号内容逐字节一致（diff 验证）；零内部票号/里程碑/组织内幕（grep 零命中）；无真实凭据；docker virtual 新表述与代码吻合（读聚合可用、推送 405 指向 local——dockervirtual.go:327-355 同源文案）；部署事实抽验全对（Dockerfile 双变体、healthcheck-probe、/var/lib/binflow、charts 20Gi+replicaCount≤1+resource-policy keep、binflow-ga/dev 容器名、systemd 加固清单）；en 导航（e0061292 既有）与新页 H1 对齐 |

### Commands（取证原文与结果）

```
$ git diff --stat develop...HEAD                          → 31 文件（含 84fab27d 扩票后）
$ make spec-check                                          → in sync，exit=0（重生成字节一致）
$ bash -n / shellcheck ci/ui-smoke.sh                      → 双绿
$ BINFLOW_BASE=http://127.0.0.1:1 bash ci/ui-smoke.sh      → 6×FAIL，exit=1（腿不互蔽）
$ bash ci/ui-smoke.sh                                      → https://uat.binflow.org 6×OK，exit=0
$ circleci config validate .circleci/config.yml            → valid
$ python3 -m py_compile tools/difftest/v2/{cases/_mavenlib.py, cases/maven_*.py, _smoke_mock.py}
                                                           → py-compile-OK
$ python3 tools/difftest/v2/runner.py --list               → 5 例全发现
$ python3 tools/difftest/v2/runner.py --dry-run            → NOT_RUN=5
$ python3 tools/difftest/v2/runner.py（无 env）             → BLOCKED=5，逐例 "missing required env: A_BASE, B_BASE, A_USER, A_PASSWORD, B_USER, B_PASSWORD"
$ python3 _smoke_mock.py 18091/18092 + runner 双轮          → PASS=1 FAIL=4 BLOCKED=0 ×2，(status,reason) 逐例稳定；
                                                             deploy 用例 mvn 腿 exit=0/0 落 evidence；credential grep 零命中
$ curl -fsS repo1.maven.org/.../javax.annotation-api-1.3.2.pom | shasum -a 256
                                                           → 46a4a251…f06b97（与 case 2 内嵌常量一致）
$ grep -n "func refuseVirtualDelete" -A13 internal/repo/virtual.go → 恒 405 + Allow: GET（case 4 B 面预期红坐实）
（规格锚/代码锚/ADR 序号/gitignore 的定位命令见上表，全部命中）
```

### 范围外发现（交 conductor，不影响本 PR 结论）

1. **docs/user/docker-registry.md:127 存量过时**：仍写「docker … virtual 聚合暂不做」，与 as-built 及本 PR 修正后的 fern zh/en 表述相抵 → tech-writer 跟进小票。
2. **fern install/upgrade 迁移表停在 007**：仓库实际 28 个 SQLite 迁移（internal/metadata/migrations/sqlite/001…028）。zh 存量过时、本批如实镜像到 en → 双语同步刷新票。
3. （微）zh index `/api-参考/binflow-api` vs en `/api-reference/binflow-api` slug 分叉——e0061292 既有形态，随 T-521 登记的「en 首批平台渲染预览」一并确认。

### 交 conductor 事项（源自 T-523 复核）

- case 4（delete-passthrough）与 **T-524 open ruling** 耦合：双发补跑时 B 面 `delete_via_virtual` 预期记 `status=405` → FAIL。这是 T-524 的裁定素材（A 面实测值即活体证据），不是用例缺陷；建议 T-524 裁定时同步处理本例 EXPECTED（对齐 404 / 翻新为 INTENTIONAL 标注）。
- case 2 的 `cache_projection_artifact` 与 **F1 缝**（`<K>-cache` 直访未实现）耦合：B 面预期 `status=404` → FAIL，即四桶实现票（dev-go-core/storage）落地前的合法红。
- T-523 报告建议保持 READY、凭据注入后补跑——本评审认同（mock 接线两轮稳定 + 全部断言规格可溯源，补跑只差环境）。

### 评审取向说明

- CI 真实首轮运行（cimg/go 内 python3、nightly 触发形态）与 difftest 真双发（A 参照凭据）同为环境依赖项，无法在本评审内取证；两者的本地可证面（脚本/目标/框架逻辑、mock 双轮）已全证，fail-loud 形态确认。
- tools/openapi-spec 跨域 4 行：conductor 已裁定接受，复核确认纯文案、生成物字节一致、无行为面。
- difftest 用例对参照实例的写入（建 `difftest-mvn-*` 临时仓 + mvn 部署 + 指向 Maven Central 的 remote 仓）发生在**未来双发补跑**时，T-523 本票未对参照实例做任何写入（报告 ⑭ 声明 + 本地无凭据事实一致）。
