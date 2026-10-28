# L042 — 差分首波（新环境）：docker-remote 契约重放 + console 根路径 case #0（wave-1）

- 日期：2026-10-28；模式 **dual**（A/B 双发，v2 框架批跑）
- **A 面**：JFrog Artifactory **7.161.26** Enterprise+ @ `http://192.168.120.36:32082`（`/artifactory/api/system/version` 复验 `7.161.26 / revision 86126900 / package_handler 5.675.60`；addons 含 docker/federated/release-bundle/xray/replication，entitlements 含 REPO_REPLICATION/MULTIPUSH —— E+ 档佐证，confidence high）。凭据经 gitignored `.env.ref` source 注入（值零落盘零打印）。
- **B 面**：BinFlow `dev-9e7ea000` @ `http://192.168.120.36:32083`（intake ㉗ k8s ns `lzw` 测试实例）。凭据经 gitignored `.env.lzw` 注入。
- **面结构（本波活体测绘）**：
  | 面 | A（:32082） | B（:32083） |
  |---|---|---|
  | REST API | `/artifactory/api/*`（裸 `/api/*` 404） | `/binflow/api/*`（裸 `/api/*` → 404 `no root mirror` 文案） |
  | docker v2 | **根路径** `/v2/*`（匿名 401 挑战 / Basic 200） | **根路径** `/v2/*`（同构；`/binflow/v2/` 404） |
  | UI | 裸 `/` 200 首页 HTML；`/artifactory/` 302→绝对 `…/ui/`；`/ui/` 200 | 裸 `/` 404；`/binflow/` 301→相对 `/binflow/ui/`；`/binflow/ui/` 200 |
- **上游替代（环境约束，记录在案）**：pod 出网 docker.io 不可达 → 双端 remote 仓统一指向集群内中继 registry `http://192.168.120.36:32100`（zot 系）。该上游 Accept 头严格白名单（docker 类型与 `*/*` 均拒、仅接受 `application/vnd.oci.image.manifest.v1+json`）→ A/B 对其 OCI 单体 manifest 双端同败 404 MANIFEST_UNKNOWN（**双端同败=SAME**，错误形态逐字段同构佐证；zot 严格性属环境交互非 A-B 分歧）。schema2 夹具镜像 `difftest-docker/smoke`（t1/t2/t3，确定性字节）匿名推送至该上游供拉穿正路径使用。
- **命名空间纪律**：双端 remote 仓 `diff-docker-remote`（B 侧建仓 API `PUT /binflow/api/repositories/<key>` 200 验证在案，配置回读字段面高度镜像 Artifactory）；波末双端 DELETE 200×2、复验零残留。上游 `difftest-` 前缀镜像残留 t1/t2/t3（上游 DELETE 405 无法 API 清除，待手动 GC；不影响 CI 依赖的 `mirror/library/*`）。
- **框架适配（tools/difftest/v2，differential-qa 域内）**：
  1. `runner.py` http 腿：用例自带 Authorization（Bearer 腿）不再被默认 Basic 覆盖（一行向后兼容修复）。
  2. `_dockerlib.anon_http`：匿名腿（`ctx.http` 恒加 Basic 无法表达匿名臂）+ **不跟随 3xx**（urllib 默认静默跟到 200 会吞掉重定向面——console 契约的主断言对象）。
  3. 本批 env 约定：docker 契约域 A_BASE/B_BASE = 裸主机根（docker 面在根路径）；REST API 批沿用带前缀 base。
  4. 新增 13 用例（`cases/docker_*.py` + `cases/console_root_path.py` + `_dockerlib.py`）+ 夹具供给脚本 `docker_wave_setup.sh`。
- **批次轨迹（诚实记录）**：run1 5P/8F → run2 7P/6F → run3 9P/4F → **run4 定谳 10P/3F**。中间 FAIL 的三宗皆为用例侧 bug（非产品发现，防假发现入账）：① blob 路径丢 `sha256:` 前缀（A/B 同败，误读为 A 侧传播窗口——已证伪）；② ping 401 body 尾换行严格比较；③ catalog Link 断言放错臂（裸 GET 无 Link，截断臂才有）。run3 另暴露 A 侧熔断对工装的干扰（见 F-3，假凭据改按轮唯一）。迭代产物留存 `run/wave1-*/`，定谳在 `run/wave1/`。

---

## 计分总表（run4 定谳，`bash tools/difftest/v2/score.sh`：`{"pass":10,"fail":3,"blocked":0,"not_run":28,"total":41,"coverage":0.7692}`，本波筛选 13 例全执行）

| case | 重放契约条目（contracts/docker-remote.yaml） | verdict | 备注 |
|---|---|---|---|
| dcr-ping-anonymous | #1 remote-v2-ping-anonymous | **PASS** | 挑战头/Bearer realm=own-root/service=host 回显/compact body 双端同构 |
| dcr-ping-reject-arms | #2 ping-bad-credentials P1/P2 臂 | **PASS** | Basic realm + Bad Credentials + P2 Props-Token-not-found 双端逐字 |
| dcr-token-issue | #3 remote-token-issue（C01b） | **FAIL** | 已知分歧复现（见分歧明细 ②） |
| dcr-token-reject-arms | #4 token-bad-credentials + #5 token-anonymous（C03） | **FAIL** | bad-basic 臂双端 SAME；anonymous 臂分歧复现（见 ③） |
| dcr-manifest-get | #6 manifest-get-tag + #7 head-digest | **PASS** | body 双端逐字节；Docker-Content-Digest=sha256(body)；Etag=sha1 派生；头集窄集+超集 tolerated |
| dcr-manifest-conditional | #8 manifest-conditional-get（M1/M3/M4/M7 臂） | **PASS** | 304 裸头/IMS/非匹配 200/HEAD 恒 200 双端同姿（unquoted 臂不在本波子集） |
| dcr-manifest-unknown-404 | #13 manifest-unknown-404 | **PASS** | MANIFEST_UNKNOWN + detail.manifest=镜像名（上游非 docker.io 无 library/ 归一）双端逐字 |
| dcr-blob-get | #10 blob-get + #11 range | **PASS** | config/layer sha256(body)=digest；sha256__ 命名；206 + Content-Range 双端同 |
| dcr-blob-conditional | #12 blob-conditional（B1/B4/B6）+ #14 unknown-digest + #15 空层合成 | **PASS** | 304 完整 artifact 面无 CT/CL；BLOB_UNKNOWN blobSum 回显；空层 200/32B 双端同 |
| dcr-tags-catalog | #16 tags-list + #17 catalog + #18/#19 分页 | **PASS** | 聚合/截断/Link（tags 域路径 + catalog /v2/_catalog registry 级 quirk）/n=abc 404 双端同；tag 集双端互比等 |
| dcr-push-reject | #20/#21/#22 remote 禁推三端点 | **PASS** | 400 三文案逐字（pretty status 形态无 code/detail） |
| dcr-pull-sequence | #23 pull-sequence（REST 子集） | **PASS** | ping→token→manifest→config→layer 全链 200 + digest 链自洽 + 双端 manifest 逐字节同 |
| console-root-path | （新面，case #0） | **FAIL** | 裸根首页分歧（见分歧明细 ①，立票 T-630） |

## 分歧明细

**① 裸根路径无首页（case #0，新立 T-630）**
- A：`GET /` → **200 text/html 首页**（与 `/ui/` 同源 HTML，裸端口访问即达）；`GET /artifactory/` → **302** + 绝对 `Location: http://…:32082/ui/`。
- B：`GET /` → **404** JSON `no root mirror: BinFlow serves every endpoint under the /binflow prefix (requested path /); the legacy repository context prefix is not emulated`（自述设计决定）；`GET /binflow/` → **301** + 相对 `Location: /binflow/ui/`；`GET /binflow/ui/` → 200（控制台在，但需先验路径知识才可达）。
- 涉三个 delta：裸根可达性（主页面对照）；跳转状态码 302 vs 301；Location 绝对 vs 相对。
- 注：任务快照的「A 302→/ui/」现值为「裸 `/` 直出 200、上下文根才 302」——以本波活体为准。

**② token 签发形态（C01b 复现，known-divergence#docker/remote-token-response-shape 维持）**
- A `{token, expires_in:9000, JWT(eyJ2ZXIi…)}` vs B `{token, access_token, expires_in:2592000, issued_at, scope, 不透明 hex64}`。与旧基准观察逐项一致 → 7.161.26 基准下维持 DIVERGENT（对齐方向 9000/JWT 待 authority 裁）。

**③ 匿名 token 策略（C03 复现，known-divergence#docker/remote-anonymous-token-policy 维持）**
- A `GET /v2/token` 无凭据 → 401 `Authentication is required`；B → **200 签发匿名 token**。实例匿名姿态默认值分歧维持待裁。

## 微差（note 级，不立票）

- **N-1**：A ping 401 body 带尾部换行（87B vs 字面 86B；B 无换行）。JSON 语义等价；建议 accepted 指纹注记（类似 N3 pretty 方言先例），不动 verdict。
- **N-2**：tags/list A Jackson pretty（`: ` 前置空格）vs B Go Encoder 缩进风格——canonical JSON 等价，既有 accepted 指纹，维持。

## 新发现

- **F-1（立票 T-631，配置面分歧）`allowPrivateUpstream` 守卫**：同一份 PUT 建仓载荷（私有 HTTP 上游）→ A 7.161.26 **无此字段**（GET 配置面不回显）默认放行，建仓即拉穿；B **有此字段默认 false**，建仓 200 但运行时拒拉 `400 Cannot fetch …: upstream target refused — private or suppressed upstream`，置 `true` 后拉穿正常。对私有 HTTP 上游场景与参照活体行为分歧（该字段疑似对齐较新 JFrog 安全语义——需 compatibility-engineer 对 7.161.26 官方语义核实后裁：默认值对齐 vs 登记意图性差异）。
- **F-2（note 级）**：见微差 N-1。
- **F-3（立票 T-632，安全面分歧）recurrent-failure 熔断**：A 对同一用户名反复坏 Basic 凭据后返回 **403** `This request is blocked due to recurrent request failures, please try again in 4 second(s)`；B 无此机制（恒 401）。跨轮差分工装踩中过（假凭据用户名固定）——已改按轮唯一规避。是否对齐（实现熔断）或登记 BinFlow 侧待补安全能力，归产品/安全裁。
- **环境观察（非分歧）**：中继 registry（:32100）**匿名 PUSH 开放**（POST uploads 202）——共享设施风险提示，建议收紧；SSH docker 通道（172.16.58.130）本波 banner 超时不可用，真实 daemon 腿顺延。

## 四问口径（本波）

- **surface**：docker-remote v2 面（契约 25 条目中本波重放 12 条覆盖至臂级）+ console 根路径面（case #0）；REST-face 优先，真实 daemon 腿除外。
- **matched**：10/13 case PASS（blob/manifest/tags/catalog/push-reject/序列全链 + 404 形态族 + 条件请求族双端同姿）。
- **divergence**：3/13 FAIL = 1 新立票（T-630 裸根首页）+ 2 已知分歧复现（C01b/C03，7.161.26 基准下维持）。
- **unknown（未覆盖，不立断言）**：negative-cache TTL 窗口臂（#9/#16 上游计数判定——需实例日志面）；expired/revoked bearer 臂（P3/P4b，需参照侧 token 状态操控）；真实 docker daemon 端到端拉穿时序与缓存快路径（#23 timing 面 + C07）；过期窗重验证（#9 revalidation-serve）；schema1 拉取阻断；catalog 空列表降级臂（上游有 catalog API 未复验）；star INM 腿；`/binflow/v2/` 前缀 docker 面 404（B 挂载拓扑确认即可，非契约面）。

## Z 口径更新建议（归 compatibility-engineer）

1. `docker/remote-token-response-shape` / `docker/remote-anonymous-token-policy`：7.161.26 新基准活体复验一致 → 条目基准标注更新为 7.161.26（旧 7.161.20 结论不自动失效）。
2. 新条目提案：`console/root-path-homepage`（T-630 落账后立）；配置面 `remote/allow-private-upstream-default`（F-1）；note 级 `docker/remote-ping-body-trailing-newline`（N-1）。
3. normalize 注册提案：docker-remote 域维持 **raw**（本波全部判定未用归一即达成；尾换行以 note 承载，不进 normalize）。
4. 工装纪律（differential-qa 自留）：坏凭据臂假用户名按轮唯一（A 熔断）；404 族夹具按轮唯一命名（负缓存 missedTTL 1800s）；上游镜像 DELETE 405 需手动 GC 清单。

## 工件与清理

- 定谳产物：`tools/difftest/v2/run/wave1/results.json` + `run/wave1/evidence/<case>/checks.json`（逐检查项、无凭据）；迭代史 `run/wave1-*/`。
- 清理：双端 `diff-docker-remote` DELETE 200×2、复验仓库列表零 `diff-` 残留。上游 `difftest-docker/smoke` t1/t2/t3 残留（405）待手动 GC。
- 框架改动与报告本地提交于 `claude/lzw-followups`（未推远端——github 大传输不稳）。

---

## rerun-2026-10-28（wave-1 重放收尾：A 侧 fixture 灌装 + 8 例 dcr 重放）

- **执行域**：worktree `wave-docker-remote`（主检出与 t633-wire 并行会话零接触，未动 git）；B 侧实例已换版 `dev-bb65bb75`（wave-1 时 `dev-9e7ea000`）。凭据全程 gitignored env 文件行首锚定注入，零落盘零打印。
- **13b 根因定性修正（证据在案，裁定归 conductor/compatibility-engineer）**：13b 8 例 FAIL 的初判「A 侧缺 `difftest-docker` local 仓」与本批复核不符——
  - 13b 证据逐条回看（`run/wave1-rerun13b/`）：8 例 FAIL 检查项**全部在 B 侧**（`b/token-issued: token status 401`、`b/tags status-200: got 401` 等），A 侧同轮全 200 且 manifest sha `9530196490…` 正确；
  - case 文件寻址的 repo 是 **`diff-docker-remote`**（`_dockerlib.py` `REPO` 常量；路径形态 `/v2/diff-docker-remote/difftest-docker/smoke/manifests/t2`），**不引用 `difftest-docker` local 仓**；「`difftest-docker%2Fsmoke`」是上游中继 :32100 上的镜像路径（`IMG=difftest-docker/smoke`），非 A 侧 repo+image；
  - wave-1 波末双端 DELETE 清理后，rerun 未重跑 setup 的建仓段 → B 侧 `diff-docker-remote` 缺失，B token（scope 校验）与 manifest 全线 401。**真实根因 = setup 脚本建仓段未随重放执行（IaC 缺口在重建段），非 A 侧 fixture 仓缺失**。本会话活体：A repo 列表含 `diff-docker-remote`+`difftest-docker`（conductor 补建），B repo 列表 `diff-docker-remote`（本会话重建），双端 wave1 形态 HEAD manifest 200。
- **灌装（任务 1，A 侧 `difftest-docker` local 仓，admin Basic）**：脚本 `tools/difftest/v2/docker_a_fixture_push.sh`，字节与推送序列严格取自 `docker_wave_setup.sh` 上游段：
  - 本地复算 digest 与 wave-1 夹具逐位一致（脚本内断言）：layer `sha256:d53eda7a637c…`、config `sha256:481acf9f92ec…`、manifest `sha256:95301964901114259e985357c982f41e97657f38b36e0b5c65de8b3c6be4b558`；
  - 推送：POST `/v2/difftest-docker%2Fsmoke/blobs/uploads/` → **202**（%2F 形态 A 直接接受）；blob PUT ×2 → **201**；manifest t1/t2 PUT → **201**；
  - 推后断言：HEAD manifest t1 → **200**、t2 → **200**，`Docker-Content-Digest: sha256:9530196490…` 精确一致；HEAD by digest → **200**；
  - 残留处置：`difftest-docker/smoke`（t1/t2 + 2 blobs）为声明测试残留，留存不删（OCI DELETE 可能 405）。
- **重放（任务 2，8 例，`run/wave1-rerun13c/`）**：runner v2 双发，env 六变量注入（A/B docker face = 裸主机根 `:32082`/`:32083`）；case 文件未改一字——docker/remote-* 契约重放对象仍是 **remote 面**（`diff-docker-remote` 拓扑），A 侧 local 仓灌装不改变本批断言对象，仅兑现 conductor 补建仓的可用性。

| case | wave-1 run4 | 13b | **13c rerun** | 检查项 |
|---|---|---|---|---|
| dcr-blob-conditional | PASS | FAIL(env) | **PASS** | 46（a23/b23） |
| dcr-blob-get | PASS | FAIL(env) | **PASS** | 56（a28/b28） |
| dcr-manifest-conditional | PASS | FAIL(env) | **PASS** | 17（a8/b8/ab1） |
| dcr-manifest-get | PASS | FAIL(env) | **PASS** | 34（a16/b16/ab2） |
| dcr-manifest-unknown-404 | PASS | FAIL(env) | **PASS** | 18（a9/b9） |
| dcr-pull-sequence | PASS | FAIL(env) | **PASS** | 21（a10/b10/ab1） |
| dcr-push-reject | PASS | FAIL(env) | **PASS** | 38（a19/b19） |
| dcr-tags-catalog | PASS | FAIL(env) | **PASS** | 36（a17/b17/ab2） |

- **复跑门**：连续两轮（`run/wave1-rerun13c/`、`run/wave1-rerun13c-2/`）score 均为 `{"pass":8,"fail":0,"blocked":0,"not_run":33,"total":41,"coverage":1.0}`，结论稳定零 flake。
- **console-root-path**：本批未重跑；PASS 活体证据 = conductor 本会话已验（随 `dev-bb65bb75` 上线），票面口径归 T-630。
- **T-632 熔断臂口径**：**未覆盖**——B 侧已含熔断实现但 httpapi 接线票在途（行为未达 face），本批 8 例亦不含坏凭据臂；A 侧熔断工装纪律按 wave-1 沿用（假凭据按轮唯一）。熔断差分臂待接线票落地后另批。
- **已知分歧补充探针（非 case verdict，每端 2 curl）**：`dev-bb65bb75` 上 C01b/C03 均维持——token-issue A `{token, expires_in:9000, JWT}` vs B `{token, access_token, issued_at, scope, expires_in:2592000, hex64}`（DIVERGENT 维持）；匿名 token A 401 `Authentication is required` vs B 200 签发（DIVERGENT 维持）。token 两 case 不在本批 8 例范围，判定仍归 known-divergence 台账。
- **工件**：可复跑脚本 `tools/difftest/v2/{docker_a_fixture_push.sh,wave1_rerun13c_probe.sh,wave1_rerun13c_replay.sh}` + 产物 `run/wave1-rerun13c{,-2}/`（results.json + evidence/ 逐检查项，无凭据）。
- **遗留清单**：① B 侧无 `difftest-docker` local 仓——现 case 集不需要；若未来 local-face 重放需双侧灌装，应入 setup 脚本 IaC；② 上游中继 `difftest-docker/smoke` t1/t2/t3 残留待手动 GC（wave-1 清单结转）；③ A 侧 `difftest-docker/smoke` 残留（本批灌装，声明留存）。

---

## setup2-verify-2026-10-28（IaC 缺口修复验证：建仓路径 404 修复 + A 侧 difftest-docker 固化）

- **一行修正（根因链补全，裁定归 conductor/compatibility-engineer）**：13b/13c 根因定位补第二层——setup 建仓段用 `$BASE/api/repositories/<key>`（根别名路径），在参照 JFrog 7.161.26 上该路径 **404**（平台只认 `/artifactory/api/*`；活体实证：A `/api/repositories`→404、`/artifactory/api/repositories`→200；B `/api/repositories`→404、`/binflow/api/repositories`→200）。**修复已落地**：`tools/difftest/v2/docker_wave_setup.sh` 重写为自适应 API 前缀探测（候选 ``、`/artifactory`、`/binflow` 按 `GET <cand>/api/repositories` 首个 200 胜出，无硬编码端口）+ 全段幂等（仓存在跳过建仓、blob HEAD 命中跳传、manifest PUT 200/201 可重复）+ **A 侧 `difftest-docker` local 仓固化**（缺则建、镜像以 admin Basic 直推 `/v2/difftest-docker%2Fsmoke` face、推后 HEAD t1/t2/by-digest 全 200 且 `Docker-Content-Digest: sha256:9530196490…` 精确一致）。
- **setup 实跑（两轮全绿，`set -eu` 保持、六变量纪律不变）**：run1 裸主机根 BASE（强制走 404→回退路径：A 探测 `…/api`→404→`…/artifactory/api`→200；B 探测 `…/api`→404→`…/artifactory/api`→404→`…/binflow/api`→200）；run2 前缀 BASE（`/artifactory`、`/binflow` 直连探测一轮命中）。两轮每步断言码：repo GET 200 skip→verify 200；upstream blob HEAD 200 SKIP、manifest t1/t2 PUT 201、HEAD 200+dcd 一致；A-local 同构全绿。
- **8 例重放（`run/setup2-verify/`、复跑 `run/setup2-verify-2/`）**：**7/8 PASS，dcr-tags-catalog FAIL（两轮同态，确定性非 flake）**——失败项 `ab/tag-order-intercompare`：A `['t1','t2','t3']` vs B `['nc1a181cca3cd','nc1a181cea8bb','t1','t2','t3']`。取证：nc\* tag 由 wave2 批 `dcr-negative-cache-ttl` 于 17:17:14/17:19:26 按设计推入上游 relay（run 唯一名，声明残留，上游 DELETE 405 不可清）；13c（15:32）当时上游无 nc tag 故 PASS 成立；本批为污染后首跑。**新暴露分歧候选（登 UNKNOWN 待裁，不在本票权限）**：A 侧 remote 仓 `tags/list` 为非活体视图（上游 17:19 起 5 tag，A 至 18:13 跨多次请求仍答 3，滞后 ≥54min），B 侧活体聚合——上游静态时双端一致（wave-1/13c 全绿），上游一变即暴露。伴随观察：tags/list 带 manifest media-type Accept 时 A 406、B 200（协商宽严差，一并待裁）。修复选项（归 compatibility-engineer）：intercompare 放宽为集合包含 / A 视图刷新机制探明 / wave2 残留清理策略。**case 文件未改一字**（放宽断言=掩盖差异，需裁定后执行）。
- **回归对照**：其余 7 例两轮 score 均 `{"pass":7,"fail":1,"blocked":0,"not_run":39,"total":47,"coverage":0.875}`，与 13c 基线一致项无回退；新增差异 1（tags-catalog intercompare，环境触发型，见上）。
- **工件**：`tools/difftest/v2/docker_wave_setup.sh`（重写，旧版见 git 历史）+ `run/setup2-verify{,-2}/`（results.json + evidence/）；工作日志 `reports/agents/T-L042-setup2.md`。
