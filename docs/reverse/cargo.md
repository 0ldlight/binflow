# Cargo 包型（sparse 索引 + crates API 协议）行为规格

- 票据：T-284（FR-91.1 覆盖集第 2 份）；M11 Cargo 适配票与 T-294（Cargo 条件票）的前置规格。
- **公开规范锚点（官方优先）**：The Cargo Book 两页——[Registry Index](https://doc.rust-lang.org/cargo/reference/registry-index.html)（config.json / 索引路径规则 / 索引行 JSON schema / sparse 协议）与 [Registry Web API](https://doc.rust-lang.org/cargo/reference/registry-web-api.html)（publish/yank/unyank/search wire 契约）。反编译（`reverse-src/artifactory/src/{batch2-protocol/org/jfrog/repomd/cargo, batch3-addons/org/artifactory/addon/cargo}`）只补规范空白（存储布局、yank 的属性化实现、remote 翻译层、git 索引弃用闸门），逐条标注「此条补充官方规范」。
- 置信度：`高` = 官方规范 + 反编译双证（或纯官方规范定义）；`中` = 仅反编译；`低` = 推断。低置信不作验收依赖。

---

## 1. 基础路径形态

| 系统 | 形态 | 说明 | 置信度 |
|---|---|---|---|
| BinFlow（实现口径，建议） | `$BASE/binflow/<repoKey>/...` | 与 goproxy.md §1 同构。config.json 中 `dl`/`api` 必须自指回 BinFlow：`dl = $BASE/binflow/<repoKey>/v1/crates`，`api = $BASE/binflow/<repoKey>`。 | —（BinFlow 决策，与 LC-10 对齐） |
| Artifactory（参考对照） | `$BASE/artifactory/api/cargo/<repoKey>/...` | config.json 合成 `{"dl":"<base>/api/cargo/<repo>/v1/crates","api":"<base>/api/cargo/<repo>"}`。 | 高 |

git index 卹议（`cargoGitIndexEnabled`）**已弃用**：仓未开 sparse（cargoInternalIndex）且系统未开 git 索引时，index 请求 404 + 文案 `Cargo's Git index is deprecated and no longer supported. Please migrate this repository to the sparse HTTP index (cargoInternalIndex=true).`。**BinFlow 决策：只实现 sparse，不做 git 面**（与官方"不建议双协议"一致）。高（代码 + 官方双证）。

## 2. 端点表（相对 `$BASE/binflow/<repoKey>/`）

| 方法 | 路径 | 语义 | 成功响应 | 错误响应 | 置信度 |
|---|---|---|---|---|---|
| GET | `/`（仓根） | 仓可用性探针 | 200 空体 | — | 中（仅代码） |
| GET | `index/config.json`（Artifactory 另映射仓根 GET 同义） | sparse 入口配置 | 200 JSON（§3.1） | git 弃用闸门 404 | 高（官方 + 代码） |
| GET | `index/{pkgPath}` | 索引文件（每版本一行 JSON，NDJSON） | 200 `text/plain` 索引行流；应支持 `ETag`/`Last-Modified` + `If-None-Match`/`If-Modified-Since` → 304（官方缓存语义） | 不存在 404/410/451（官方允许三码；BinFlow 取 404） | 高（官方 + 代码） |
| GET | `v1/crates/{name}/{version}/download` | 下载 `.crate` | 200 crate 流 | 404 `{"errors":[{"detail":"unable to download crate"}]}` | 高（官方 + 代码） |
| PUT | `api/v1/crates/new` | publish（wire 见 §5.1） | 200 `{"warnings":{...},"errors":[]}`（无 warnings 可省） | 见 §5.3 | 高（官方 + 代码） |
| DELETE | `api/v1/crates/{name}/{version}/yank` | yank | 200 `{"ok":true}` | 401（匿名）/ 403（无权限）`{"errors":[{"detail":"unauthorized user"\|"forbidden"}]}`；**crate 或版本不存在 → 404 + errors 信封**（TL-6 定案，T-293 补行 2026-08-26——官方未列该分支，BinFlow 取 404 而非幂等 200） | 高 |
| PUT | `api/v1/crates/{name}/{version}/unyank` | unyank | 200 `{"ok":true}` | 同上（401/403 同形；不存在 → **404 + errors 信封**，TL-6） | 高 |
| GET | `api/v1/crates?q=<q>&per_page=<n>` | search（**官方：不带 Authorization**） | 200 `{"crates":[{"name","max_version","description"}],"meta":{"total":N}}` | — | 高（官方；Artifactory 亦不鉴权） |
| GET | `api/v1/crates/{name}/owners` 族 | owners 管理（官方四端点） | — | — | **M11 不做**（§9）；收到按未知路径 404 |

管理端点（Artifactory `/api/cargo/<repoKey>/...`；BinFlow 归自有 API 面）：`POST reindex`（重建索引，MANAGE 权限）、`POST init`（git 遗留，不做）。中。

错误信封统一（官方）：`{"errors":[{"detail":"<msg>"}]}`；**200 + errors 体也会被 cargo 展示为失败**（官方明文）。高。

## 3. sparse 索引协议

### 3.1 config.json（高——官方 + 代码双证）

```json
{"dl": "<api>/v1/crates", "api": "<api>"}
```

- BinFlow 合成规则：`dl`/`api` 指向自身仓面；无 `{crate}` 等占位符 → cargo 自动追加 `/{crate}/{version}/download`。**base 取值 = `server.base_url`（空则按请求 scheme+host 推导，X-Forwarded-Proto 认可）——ADR-0034/TL-1 统一定案（npm packument / nuget service index 同款先例），不新增 per-protocol 配置键（T-293 合入 2026-08-26）**。
- `auth-required: true`：当该仓禁匿名（读需认证）时输出——cargo 会先裸取 config.json，收到 401 后带凭据重试（官方 sparse 认证握手）。BinFlow 默认匿名可读 → 不输出该键。
- 服务端应支持 config.json 的条件请求（cargo 每会话首取）。

### 3.2 索引路径推导（高——官方定义，代码一致）

| 名字长度 | 路径（相对 `index/`） |
|---|---|
| 1 | `1/{name}` |
| 2 | `2/{name}` |
| 3 | `3/{首字符}/{name}` |
| ≥4 | `{前两字符}/{第3-4字符}/{name}` |

- 官方注意：索引**文件名**用小写；索引行内 `name` 保持原大小写。服务端路由正则须接受 `[A-Za-z0-9_\-]` 与路径斜杠（Artifactory 路由正则 `[A-Za-z0-9][A-Za-z0-9_/\-]{0,127}`，此条补充官方规范）。BinFlow 校验 crate 名：ASCII 字母数字 + `-`/`_`、首字符字母、长度 ≤64（官方 crates.io 限制集，建议采纳）。
- **同一 name+version 只能出现一行**（官方 must：SemVer build metadata 不区分）。

### 3.3 索引行 JSON（字段集官方；生成时机为代码补充）

每行字段：`name`、`vers`、`deps[]`（官方 schema：name/req/features/optional/default_features/target/kind/registry/package）、`cksum`（**服务端实测 .crate 的 sha256**——官方明确 cksum 由 registry 计算写入）、`features`、`yanked`、`links`（可选）、`v`（可选 schema 版本，建议 2 + `features2` 合并策略：BinFlow 直接全放 `features`，`v` 可省）。高。

- 写入时机（此条补充官方规范）：publish 接受后由**异步索引器**生成/重写 `index/<pkgPath>` 整文件（对该 crate 全版本重算，逐版本一行）；官方允许 index 延迟更新——cargo 发布后会轮询索引短窗口，未见则告警不失败。高。
- yank/unyank 不改历史行内容，仅翻转该版本行的 `yanked` 布尔（重写索引文件）。高。

## 4. 存储布局（此节为反编译补充官方规范；官方不定义服务端存储）

```
<repoKey>/
  crates/<name>/<name>-<version>.crate          ← blob（唯一内容实体）
  .cargo/crates/<name>/<name>-<version>.json    ← 长元数据（deps/features 全量 JSON，供索引重算与搜索回填）
  index/1|2|3/c|ab/cd/<name>                    ← 索引行文件（§3.2）
```

- `.crate` 节点属性：`crate.name`、`crate.version`、`crate.description`、`crate.keywords`（`;` 连接）、`crate.categories`（`;` 连接）、`crate.yanked`（yank 时加、unyank 时删）。高（代码）。
- 索引 `cksum` 取存储后实测 sha256（即下载校验链闭环：索引声明 = 存储实际）。高。
- 物理 blob 走 BinFlow 既有 filestore 寻址；上述为逻辑路径。
- 长元数据缺失时（外部导入场景）可从属性回填 deps/features（`;` 拆分约定）。中。

## 5. 上传/下载/校验链

### 5.1 publish wire 格式（高——官方 + 代码逐字节双证）

```
[4 字节小端 uint32 = JSON 长度][元数据 JSON（官方 publish schema，含 vers/deps/authors/description/readme/...）]
[4 字节小端 uint32 = crate 长度][.crate gzip tarball]
```

### 5.2 服务端处理序（行为描述）

1. 解帧 → 元数据与 crate 分离。
2. 目标路径 `crates/<name>/<name>-<version>.crate`：已存在且无 DELETE 权限 → 覆写保护 401（匿名）/403；已存在且有 DELETE 权限 → 按覆盖语义重传。高（代码，与 repo-semantics §3 一致）。
3. 无写权限 → 401/403（错误信封）。
4. 落盘 crate + 属性（§4）+ `.cargo` 长元数据；存储层实测 sha256/sha1/md5 入库。
5. 异步：生成/重写索引行（§3.3）。
6. 响应 200；**处理失败（如解帧 IO 错误）时 Artifactory 返回 200 + errors 体**（cargo 侧按失败展示）——BinFlow 建议对齐官方：4xx/5xx + errors 信封为主，IO 类可保留 200+errors 形态以最大兼容（登记拆票裁决）。中。

### 5.3 校验链

| 时机 | 行为 | 置信度 |
|---|---|---|
| publish PUT | 支持 BinFlow 统一 checksum 头？——cargo 客户端**不带** checksum 头（官方未定义），服务端实测入库即可 | 高 |
| 下载 | cargo 按索引行 `cksum` 校验 .crate 的 sha256 → **索引 cksum 必须等于存储实测 sha256**（BinFlow AC 对账点） | 高 |
| 索引刷新 | cargo 用 `If-None-Match`/`If-Modified-Since` → 服务端 304；BinFlow 索引文件响应必须带 `ETag`（建议 = sha256）或 `Last-Modified` | 高（官方）；BinFlow 实现建议 |
| 版本合法性 | `vers` 必须合法 SemVer 2.0；同 name+vers（忽略 build metadata）重复 → 拒绝（官方 must）；拒绝码建议 409/400 + errors 信封 | 高（官方）/ —（码为 BinFlow 裁决） |
| 特性门 | `rust_version`（可选，无操作符的版本要求）随元数据透传；cargo 1.51 以下不识别 `v` 字段会被跳过——服务端不需处理 | 高（官方） |

## 6. yank / unyank 语义（高——官方语义 + 代码实现双证）

- 官方语义：yank ≠ 删除——只把索引行 `yanked` 置 true；已列入 Cargo.lock 的项目仍可构建，新解析不选该版本。
- 服务端实现（此条补充官方规范）：yank = 在 `.crate` 节点**加属性 `crate.yanked=true`** + 重写索引行（需对 crate 有 DELETE 权限）；unyank = **删属性** + 重写（需写权限）。响应统一 `{"ok":true}`。
- 搜索结果排除 yanked 版本（§7）。

## 7. search 端点行为（官方契约 + 代码实现细节）

- `q` 对 `crate.name` 与 `crate.description` 做通配匹配（`*<q>*`），扫描上限 1000 个制品；按 crate 名去重取最高版本（版本取自文件名解析 `(.+)-(\d+\.\d+\.\d+...).crate`）；排除 yanked。
- `per_page` 默认 10、上限 100；响应 `{"crates":[...≤per_page],"meta":{"total":<去重后总数>}}`。
- 置信度：契约高（官方）；匹配/去重细节中（代码）。

## 8. rclass 三态行为

| rclass | 行为 | 置信度 |
|---|---|---|
| local | 全量（§2–§7）。 | 高 |
| remote | sparse pull-through：`index/{pkg}` 与 `v1/crates/.../download` 未命中回源上游 registry URL；上游 config.json 首次拉取后**原文缓存为 cache 仓根的 `config.original.json`**（用于定位上游 dl/api）；对外 config.json 仍合成自指（dl/api 指向 BinFlow remote 仓，下载经代理）。search 直接代理上游 `api/v1/crates`（透传 q/per_page，错误透传上游码，上游异常 → 409 errors 信封）。publish/yank/unyank：不路由（对齐 BinFlow remote 写拒绝）。 | 中（代码走读；config 翻译链完整可见） |
| virtual | `index/{pkg}`：按成员序**拼接各成员索引文件全文**（行级追加，不去重——同名版本多成员时依赖下载侧 first-found 兜底；BinFlow 建议：按 (name,vers) 去重取首见成员，避免重复行违反官方唯一性 must，登记拆票裁决）；download：first-found（含 remote 成员 pull-through）；search：跨成员归并；config.json：任一成员可读即可读；publish：路由 `defaultDeploymentRepoRef`，未配置/不存在 → 400（错误体含仓 key）。 | 中（merger 类 + 虚仓 handler 走读；去重行为为推断 → 低，已单列待验证） |

匿名下载（Artifactory `cargoAnonymousAccess`）：仓配置允许时，download 与 index 对匿名放行（系统身份执行）。BinFlow 沿用全局匿名策略即可，可不设按仓开关。中。

## 9. 真实客户端命令清单（cargo 1.8x；本机无 cargo，供 qa 环境执行）

```bash
# 安装参考：rustup（1.70+ 默认 sparse；1.8x 全支持）
REG="$BASE/binflow/cargo-local"                    # 例 http://localhost:8080/binflow/cargo-local

# L-r1 手工探针
curl -s "$REG/index/config.json"
curl -s "$REG/index/3/m/mycrate"                    # 404 或索引行
curl -s -o /dev/null -w '%{http_code}\n' "$REG/v1/crates/mycrate/0.1.0/download"

# L-r2 ~/.cargo/config.toml
cat >> ~/.cargo/config.toml <<EOF
[registries.binflow]
index = "sparse+$REG/index/"
EOF
cargo login --registry binflow                      # 输入 token（BinFlow 用户 token）
# 或环境变量：CARGO_REGISTRIES_BINFLOW_INDEX / CARGO_REGISTRIES_BINFLOW_TOKEN

# L-r3 发布
cargo new mycrate --lib && cd mycrate
echo 'description = "demo"' >> Cargo.toml
cargo publish --registry binflow                    # PUT api/v1/crates/new
sleep 2 && curl -s "$REG/index/3/m/mycrate"         # 索引行出现（cksum 对账）

# L-r4 消费（隔离 CARGO_HOME）
cd .. && cargo new consumer && cd consumer
cargo add mycrate --registry binflow
CARGO_NET_GIT_FETCH_WITH_CLI=false cargo build      # 走 sparse 索引 + download

# L-r5 yank/unyank
cargo yank --registry binflow mycrate@0.1.0
curl -s "$REG/index/3/m/mycrate"                    # 行内 "yanked":true
cargo unyank --registry binflow mycrate@0.1.0

# L-r6 search
curl -s "$REG/api/v1/crates?q=mycrate&per_page=10"

# L-r7 remote pull-through（仓 cargo-remote 上游 crates.io）
curl -s "$BASE/binflow/cargo-remote/index/config.json"
curl -s -o /dev/null -w '%{http_code}\n' "$BASE/binflow/cargo-remote/v1/crates/serde/1.0.200/download"   # 二次执行提速断言
```

注：sparse URL 必须带 `sparse+` 前缀；http 明文实例需 `CARGO_NET_HTTP_CHECK_REVOKE=false` 无关，实际需要的是允许 http（cargo 对 http sparse 会告警但不阻断，qa 如遇阻断改用 https 或 `[net] retry` 检查）。

## 10. 与公开规范的差异/补充清单

| # | 条目 | 来源 | 置信度 |
|---|---|---|---|
| S1 | 存储布局 `crates/<name>/<name>-<ver>.crate` + `.cargo/...json` 长元数据 + `index/...` | 反编译 | 高 |
| S2 | yank = 节点属性 + 索引整文件重写（非 git commit） | 反编译 | 高 |
| S3 | 索引路由正则（接受路径斜杠）与 crate 名限制采纳 | 反编译 + 官方建议 | 高 |
| S4 | remote 的 `config.original.json` 翻译链与 search 代理 | 反编译 | 中 |
| S5 | virtual 索引拼接/下载 first-found/publish 路由 defaultDeploymentRepoRef | 反编译 | 中（去重行为低） |
| S6 | publish 解帧后逐条错误处理返回 200+errors 的兼容形态 | 反编译 | 中 |
| S7 | 仓根 GET 探针、reindex 管理端点 | 反编译 | 中 |
| S8 | 索引行文件响应需带 ETag/Last-Modified（官方缓存协议）→ BinFlow 实现义务 | 官方 + 推断 | 高（义务）/ —（实现建议） |

## 11. 显式不做（M11 登记建议）

1. **git index 协议面**（官方已弃用 + Artifactory 已加弃用闸门）：`init` 端点、git smart-http 均不做。
2. **owners 四端点**：官方可选能力，BinFlow 无用户-所有权模型，不做（未知路径 404）。
3. **`/me` login 页**：无控制台 token 页面联动，M11 不做。

## 12. M11 拆票就绪度自评

- **可直接拆票**：端点表（§2）、config.json 合成（§3.1）、索引路径与行 schema（§3.2/§3.3）、存储布局（§4）、publish wire 与校验链（§5）、yank 语义（§6）、search（§7）、local 全量、客户端命令（§9）。与 LC-10（`api/v1/crates` + sparse 索引，crates.io 规范为准）完全对齐。
- **拆票时需裁决三点**：① 虚仓索引去重策略（S5 低置信项，建议首见成员去重）；② publish IO 类错误 200+errors vs 4xx/5xx；③ 重复版本拒绝码（建议 409）。
- **依赖提示**：索引行生成依赖制品属性系统（FR-89 已落）；`cksum` 必须取存储实测 sha256（复用既有内容面实测链）。

### 待验证清单

1. virtual 索引重复行在真实 cargo 解析下的行为（S5）——驱动 ① 裁决。
2. http 明文 sparse 实例的真实 cargo 告警/阻断边界（qa 环境）。
3. `features2`/`v` 字段省略时旧版 cargo（<1.51）兼容性——BinFlow 目标 cargo 1.8x，低风险。
