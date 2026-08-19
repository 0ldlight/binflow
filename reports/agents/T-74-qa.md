# QA 报告 T-74 — M3 三协议功能矩阵（PRD §8 剧本 2~5/8）

- role: qa-engineer
- 日期: 2026-08-20
- 票据: T-74 [P0] QA：M3 三协议功能矩阵（AC 全文见 reports/agents/T-61.md T-74 节）
- 验收口径: docs/prd/milestone-3.md **v1.2**（M19 按 **409**、M26 按 **403** 勘误口径；M31 按 v1.2 四细节）
- 被测对象: commit **f597c86**（T-74 派发点，即 T-84 done 后的 HEAD），独立 git worktree 构建 `/tmp/t74/binflow-server`（19.6 MB）——**不**含在途 T-73 工作树改动
- 实例: A=`127.0.0.1:8080` 匿名开（默认配置）/ B=`127.0.0.1:8081` 匿名关（`security.anonymous_access: false`，仅 M28 双模式用）；均设 `BINFLOW_REMOTE_CREDENTIALS_KEY`；mock 上游 `python3 -m http.server 9099`（沿 M2 先例）
- 客户端: mvn 3.9.9 + Temurin JDK 21.0.12（/tmp/t67-tools）；npm 10.9.8（node 22.23.2）；pip 26.1.2 + twine 7.0.0（/tmp/t74/venv，含 build/setuptools/wheel）；curl；python3 3.14
- 实例串行: 开工前清理了 T-44 遗留孤儿实例（PID 8365，config 已不存在的 /tmp/t44 冷启动残留）；全程独占实例

## 总结论: **PASS**

- 场景/断言组 **49/49 全过**（§2 明细表逐行，行内含多子断言）；全程服务端 **5xx = 0**（A 304 条 + B 13 条 access，`"status":5xx` 计数 0；`level:ERROR/WARN` 计数 0）
- 自动化基线: `go vet ./...` 零告警；`go test ./... -count=1` **16/16 包 ok（exit 0）**
- 无缺陷票。产出 5 条 PRD 勘误建议（§4，均不阻塞，码值/断言口径已按 PRD 定案语义等效验证）

---

## 1. AC 逐条结论

| # | AC（T-74） | 结果 | 证据摘要（详见 §2/§3） |
|---|---|---|---|
| ① | 仓库模型 M01~M05（E-07 反转、docker 组合边界、url scheme 400） | ✅ | M01 三仓 200+packageType 正确；E-07 反转成立（remote/virtual 建仓 200）；M02b 三分支 400（message 含 url/scheme）；M05 remote+docker / virtual+docker 400「not supported in M3」、local+docker 200 |
| ① | Maven M10~M21 全序（snapshot `-U` 与 metadata 合并、M19 409、M20 400） | ✅ | mvn deploy 1.0.0/1.1.0/1.2.0-SNAPSHOT×2 全 BUILD SUCCESS；M16b `-U` 解析到 buildNumber 2 timestamped 文件；M15 versions 合并 latest 单调；M19 409（SnapshotPolicyException 口径）；M20 layout 400 两例；M21 206/304 |
| ① | npm M22 系~M28（M26 403、M27 `-rev` 占位、M28 匿名双模式） | ✅ | publish/install/dist-tag/scoped/unpublish 真客户端全 exit 0；M26 curl 直打 **403** `Cannot modify pre-existing version '1.0.0', aborting upload for: 'demo-pkg'`；M27 `-rev` PUT 200 `{"ok":"updated package"}` 且 packument 字节无变化；M28 双实例双模式断言全对 |
| ① | PyPI M30~M35b（M31 v1.2 四细节、M33 重复 400） | ✅ | twine 上传 wheel+sdist；M31 四细节逐条过（api-version=2 / 302 补尾斜杠 / ETag-304 / `:action=submit` 400 `unknown action 'submit'`）；M33 重复 filename → twine E400 + 服务端 400 |
| ② | 边界 M57/M58 + NFR-S16/S17/S18 + 跨协议去重 | ✅ | M57 ping `{}`；M58 五端点 404 E-01；三协议错误体全 `errors[]` 信封零 HTML；匿名写 maven/npm/pypi 全 401；穿越 12 变体全 400/404 且数据目录外无文件；同内容 blob 经 maven/npm/pypi/generic 四协议 stats `blobs` 计数不增 |
| ③ | mock 上游沿 M2 先例 + 实例串行防污染 | ✅ | mock 上游 9099 供 M02/M03 建仓（remote/virtual 成员）；全部测试资产隔离在 /tmp/t74（m2 仓/npm cache/venv 均不触用户目录）；收尾清理见 §5 |

## 2. 场景明细（M 序列逐条）

### 2.1 仓库模型 M01~M05

| 场景 | 结果 | 证据 |
|---|---|---|
| M01 三协议 local 仓 | ✅ | `PUT .../repositories/{maven,npm,pypi}-local` → `200 x3`；GET `packageType` = maven/npm/pypi |
| M02 remote 建仓+回显 | ✅ | 带 username/password 建 generic-remote → 200；GET `rclass=remote`、`configuration.url="http://127.0.0.1:9099"` 原样、默认值 7200/1800/15/300/false 全对、`password` 不回显（缺省）；DB 侧证：`binflow.db-wal` 中 `enc:v1:` ×3、明文口令 0 次（grep -a） |
| M02b url 校验 | ✅ | 缺 url → 400 `url is required (http/https upstream base URL)`；`file:///etc` → 400 `scheme must be http or https`；`ftp://x` → 400 同文；私网 url 建仓 200（请求时才拦，NFR-S13 口径） |
| M03 virtual 建仓+四错误分支 | ✅ | maven-virtual（maven-local+maven-remote-x）→ 200；缺 repositories/空数组/不存在成员/嵌套 virtual → 400×4，message 明确 |
| M04 过滤 | ✅ | `?type=remote` → {generic-remote, maven-remote-x, private-ok-remote}；`?type=virtual` → {maven-virtual}；`?packageType=maven` → {maven-local, maven-remote-x, maven-virtual}；非法值 → `[]`（P1，过） |
| M05 docker 组合边界 | ✅ | remote+docker → 400 `remote docker repositories are not supported in M3`；virtual+docker → 400（同族文案）；local+docker → 200 不回归 |

### 2.2 Maven M10~M21（mvn 3.9.9 真客户端 + curl 等价）

| 场景 | 结果 | 证据 |
|---|---|---|
| M10/M11 deploy | ✅ | `mvn -B -DskipTests deploy -DaltDeploymentRepository=binflow::default::…maven-local` → **DEPLOY_OK exit 0**（wire：pom/jar+sha1+md5+metadata 全 201） |
| M12 落盘对账 | ✅ | GET jar → 200，sha256 与本地构建产物一致（57b9db89…）；pom 逐位相同；item info `checksums.sha1` 40 hex；旁车 `.sha1` GET 裸 hex 40 字符 == 实测 |
| M13 全新仓 resolve | ✅ | 消费 pom `<repositories>` + `-Dmaven.repo.local=fresh-repo compile` → **RESOLVE_OK exit 0**，fresh-repo 落 jar/pom+服务端旁车（sha1 校验随下载通过） |
| M14 metadata+旁车 | ✅ | 版本组 XML 含 1.0.0/latest/release/lastUpdated(yyyyMMddHHmmss UTC)；`.sha1`/`.md5` 旁车均 == 对 GET XML 现算值 |
| M15 新版本合并 | ✅ | deploy 1.1.0 → versions=[1.0.0,1.1.0]、latest=1.1.0、lastUpdated 212458→212546 单调 |
| M16 snapshot×2 | ✅ | 两次 unique deploy → 落盘 `-20260819.212557-1` 与 `-20260819.212603-2` 两套 jar/pom（access log 201×2 + 直连 GET 200×2）；version 级 metadata `snapshotVersions` 2 条（jar/pom）、`buildNumber=2`、timestamp 在；artifact 级含 `1.2.0-SNAPSHOT` |
| M16b `-U` 强刷 | ✅ | 全新 repo `-U dependency:get com.acme:demo-app:1.2.0-SNAPSHOT` → BUILD SUCCESS，实际下载 `-20260819.212603-2`（buildNumber 2） |
| M16c/M17 checksum 两态 | ✅ | 伪 X-Checksum-Sha1：client-checksums 仓 **409**（`received '000…0' but actual is '5eb220…'`）；server-generated 仓 **201** 且 GET 内容 sha1 == 实测（服务端实测为准） |
| M18 旁车两态 | ✅ | 正确旁车 201 + GET 回实测；`deadbeef` 旁车 client-checksums **409**；server-generated 仓接受但 GET 回**实测值**（客户端声明不落盘） |
| M19 开关拒绝（**409 勘误口径**） | ✅ | `handleSnapshots:false` 仓 PUT 2.0-SNAPSHOT → **409** `handling of snapshots is disabled`；`handleReleases:false` 仓 PUT release → **409**；预置内容后翻 flag，GET 仍 200（开关不影响 GET） |
| M20 layout 400 | ✅ | `foo.jar` → 400 `at least 3 directories`；`zzz-1.0.0.jar` → 400 `must start with "demo-app-<version>"` |
| M21 Range/304 | ✅ | `-r 0-99` → 206；`If-None-Match`（ETag=sha1）→ 304；`X-Checksum-Md5/Sha1/Sha256` 头集齐全 |
| M12 附带跨协议去重 | ✅ | 见 §3.4（四协议 stats 不增） |

### 2.3 npm M22 系~M28（npm 10.9.8 真客户端 + curl 等价）

| 场景 | 结果 | 证据 |
|---|---|---|
| M22 publish | ✅ | `npm publish` → `+ demo-pkg@1.0.0` exit 0；`npm view` → 1.0.0 |
| M22b packument | ✅ | `dist-tags.latest=1.0.0`；`dist.tarball` 重写为 `http://localhost:8080/binflow/api/npm/npm-local/...`；`dist.shasum` == 服务端 tarball 实测 sha1（0ef0851c…）；`_attachments` 不回显；内容路径第二入口（`/binflow/npm-local/...tgz`）200 |
| M22c whoami | ✅ | `npm whoami` → `admin` |
| M23 install+缓存清空 | ✅ | 全新目录 install exit 0；`npm cache clean --force` + 删 node_modules 后重装 exit 0；package-lock 记录正确 |
| M24 dist-tags | ✅ | `dist-tag add/ls/rm` exit 0；packument `beta` 增删联动；`install @beta` exit 0；NE-04 双形态：家族端点 GET 200、旧式 `PUT .../<pkg>/<tag>` → **201 `{"ok":"created new tag"}`**、DELETE 200、缺失 404 `npm package not found with name:demo-pkg, and tag:ghost` |
| M25 scoped | ✅ | `@acme/util` publish→install 全 exit 0；`%2f`/`%2F` 双编码 200 等价（C8）；scoped tarball 布局 `@acme/util/-/@acme/util-1.0.0.tgz` 双入口 200 |
| M26 重复 publish（**403 勘误口径**） | ✅ | `npm publish` → E403 exit 1；curl 直打 → **403** E-01 `Cannot modify pre-existing version '1.0.0', aborting upload for: 'demo-pkg'` |
| M27 unpublish+`-rev` 占位 | ✅ | `-rev` PUT → **200 `{"ok":"updated package"}`** 且前后 packument `cmp` **无变化**（AC11 显式断言）；`npm unpublish@1.0.1 --force` exit 0；versions 无 1.0.1、tarball 404；install@1.0.1 exit 1 而 @1.0.0 exit 0 |
| M28 匿名双模式 | ✅ | A（匿名开）: GET packument/tarball 200、PUT 401（E-01 `authentication required`）；B（匿名关）: 匿名 GET 401、带 `_auth` GET 200；`npm install` 无凭据 fail / 带 `_auth` exit 0 |
| FR-18-AC9 integrity 同源 | ✅ | `base64(sha512(服务端 tarball))` == package-lock `integrity` digest == packument `dist.integrity`（6h644uyx…）三方一致 |
| FR-18-AC10 ETag/304 | ✅（口径注记） | `ETag`==`X-Checksum-Sha1`==**存储 packument node** sha1（storage API 交叉实证 636db9dd…）；If-None-Match → 304；文档变更 ETag 随动（tag 增删三值变化）、再命中仍 304。与**应答体** sha1 不同（应答时按 BaseURL 重写 tarball URL 并注入 `_rev`/`time` 派生字段）——功能语义正确，字面口径歧义见 §4-E3 |

### 2.4 PyPI M30~M35b（pip 26.1.2 + twine 7.0.0 真客户端 + curl 等价）

| 场景 | 结果 | 证据 |
|---|---|---|
| M30 build+upload | ✅ | `pip wheel` + `twine upload` → exit 0（服务端 200 统一响应口径） |
| M31 simple index | ✅ | 200 HTML 含 `#sha256=`；`<meta name="api-version" value="2" />`（PEP 629）；无 Artifactory 私有 `rel="internal\|external"`；条目按文件名排序（4 文件字典序验证）；`Demo_Pkg`/`demo_pkg`/`demo-pkg` 三形态 sha256(页面) 完全一致；未知名 404 E-01 |
| M31 无尾斜杠 302 | ✅ | `GET .../simple/demo-pkg` → **302**，`Location: demo-pkg/`（相对形式，解析即补尾斜杠 URL） |
| M31 ETag/304 | ✅ | ETag 存在（不透明稳定哈希 `"22b66ada…"`）；If-None-Match → 304 |
| M31 `:action` 400 | ✅ | `:action=submit` → **400** `unknown action 'submit'` |
| M32 install+hash 对账 | ✅ | 全新 venv `pip install --index-url .../simple demo-pkg` exit 0、`Version: 0.1.0`；`pip download` 落盘 sha256 == index fragment（6c577f3f…）；`packages/` 端点与内容路径双入口 200（PE-03） |
| M33 重复上传 400 | ✅ | twine 重传同 filename → `HTTPError: 400 Bad Request` exit 1；curl 等价 400 `file '…whl' already exists … overwriting is not allowed`（服务端 access log status:400 实证） |
| M34 依赖链 | ✅ | demo-lib 0.1.0 上传后，demo-pkg 0.2.0（`dependencies=["demo-lib"]`）在新 venv `pip install` 自动从同一 index 装齐两者（`Successfully installed demo-lib-0.1.0 demo-pkg-0.2.0`） |
| M35 wheel+sdist 并存 | ✅ | 0.3.0 双格式同传后 index 页两文件俱在；`--only-binary` 取 whl、`--no-binary` 取 tar.gz（access log 实证两类 packages 路径都被取用） |
| M35b PEP 691 JSON | ✅（P2） | `Accept: application/vnd.pypi.simple.v1+json` → 200，`Content-Type` 同型回、`Vary: Accept`、`files[].filename` 与 HTML 等价（4 条） |
| FR-19-AC7 匿名边界 | ✅ | 匿名 install（M32 pip 无凭据）默认可；匿名 POST upload → 401（§3.2） |

### 2.5 边界 M57/M58 + NFR

| 场景 | 结果 | 证据 |
|---|---|---|
| M57 探针 | ✅ | `GET /binflow/api/npm/npm-local/-/ping` → 200 `{}` |
| M58 不做端点 | ✅ | `.index/nexus-maven-repository-index.gz`、`/-/v1/search`、`/-/npm/v1/security/audit`、`/pypi/<pkg>/json`、`/api/pypi-ui/**` → 404×5 全 E-01 |
| NFR-S16 错误体分层 | ✅ | npm 404/400、pypi 404/400、maven 400、通用 404/401 抽查全部 `{"errors":[{status,message}]}` 信封；零 HTML 栈页、零空 200 |
| NFR-S17 匿名写 401 | ✅ | maven PUT / npm PUT / pypi POST 匿名 → 401×3（E-01 `authentication required`）；remote 仓 405 面（M48）归 T-75 |
| NFR-S18 路径穿越 | ✅ | 12 变体（`../`、`%2e%2e`、编码斜杠 `%2f`、`@scope%2F..%2F..`、`@scope%2f..`、pypi simple/packages 域、maven PUT/GET 域）→ **400×12** `dot segment ".." escapes or dilutes the repository root`；数据目录外无文件落盘；正向对照：合法 `%2f` scoped 名 200（穿越仅在含 `..` 时拒） |
| 隐藏索引目录不可见 | ✅ | `.npm/<pkg>/package.json`、`.pypi/<pkg>` 探针 404 |
| 5xx 扫描 | ✅ | A 实例 304 条 access **0 条 5xx**、B 实例 13 条 **0 条 5xx**；`level:ERROR` 与 `level:WARN` 均为 0 |

### 2.6 跨协议去重（M12 附带，FR-16-AC10）

| 步骤 | stats（blobs / logical / physical） |
|---|---|
| 基线（maven deploy 后） | 11 / 2339 / 2985 |
| 同 jar 字节 PUT 进 generic-local（新路径） | **11** / 3926 / 2985 —— blobs 不增 |
| npm tarball 字节、pypi wheel 字节、maven jar 字节再各 PUT 进 generic | **67 → 67** / 24464 / 28700 —— 三腿全部不增 |

maven/npm/pypi/generic 四协议同内容单份 blob，logical 增长而 blobs/physical 不变——去重免费获得（docker 域变体归 T-76 回归票）。

## 3. 自动化与执行环境

```
被测二进制: /tmp/t74/binflow-server（worktree @ f597c86, go build）
go vet ./...        # 零告警
go test ./... -count=1   # 16/16 包 ok, exit 0
                    # httpapi 115s / remote 120s / repo 118s / storage 118s / metadata 79s ...
实例 A: serve 127.0.0.1:8080, anonymous on（默认）; 实例 B: 8081 anonymous off
mock 上游: python3 -m http.server 9099 --directory /tmp/t74/upstream-dir（M02/M03 建仓用）
```

- mvn 全程 `-s /tmp/t74/mvn-settings.xml -Dmaven.repo.local=/tmp/t74/work/m2`（不触用户 ~/.m2）；npm 全程 `npm_config_cache=/tmp/t74/npm-cache` + 隔离 userconfig；pip/twine 用 /tmp/t74/venv。
- 公网可达（pypi/central/npmjs 均 200），但本票 remote 代理链（M41+）不在范围，mock 上游仅供建仓字段。

## 4. PRD 勘误建议（转 product-manager，均不阻塞、无码值分歧）

| # | 条目 | 现状 | 建议 |
|---|---|---|---|
| E1 | §5.4 M17 命令 | `bad.jar` 字面文件名被 M20 的 layout 严格校验先行 400（PRD 自身规则），checksum 分支不可达 | M17 样例文件名改为合法前缀（如 `demo-app-1.1.0-bad.jar`）；已按此等效验证 |
| E2 | §5.4 M16 命令 | `?list&deep=1` → `.files[].uri` 的列表 API 不存在（M1 E-09 仅文件 item；目录 node 404、repo 根 `?list` 403 为 M1 既有语义） | M16 断言载体改为「version 级 metadata snapshotVersions + access log PUT 名录 + timestamped 直连 GET」（本报告已按此验证） |
| E3 | FR-18-AC10 | 「ETag == 包文档 JSON 的 sha1」存在两种读法：存储 node（实现行为，storage API 可交叉验证）vs 应答体（因 BaseURL 重写/`_rev` 注入必不相等） | 措辞收紧为「ETag == 存储 packument node 的 sha1，随文档变更而变」；应答体派生字段差异属设计内（304/变更追踪功能已验证正确） |
| E4 | FR-18-AC2 | 「shasum == 本地 npm pack 产物 sha1」不可稳定复现：npm 10.9.8 两次 `npm pack` 字节即不同（本机实证 a01d…≠d5cf…） | 断言改为「dist.shasum == 服务端 tarball 实测 sha1（+ tarball 内容与工程一致）」，本报告已按此闭环 |
| E5 | FR-19-AC9 措辞 | 「integrity（sha512）与服务端 X-Checksum-Sha256 同源一致」并列易误读为逐字相等 | 已按语义验证：`base64(sha512(tarball))` == integrity digest；建议措辞改「同源（对同一 tarball 内容分别按 sha512/sha256 计算一致）」 |

## 5. 清理

- 实例 A/B 与 mock 上游已停止；/tmp/t74（worktree、数据、venv、日志）已删除；git worktree 已注销；无进程/端口/文件残留（含误写入仓库根的 m28.body 已即时删除）。
