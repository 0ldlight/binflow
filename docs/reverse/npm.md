# npm 认证/会话端点族（`/-/` 家族：login / whoami / ping）行为规格 —— K60 定案

- 票据：T-393（M14 B2，FR-130.1 前置规格：npm legacy login 端点族实证整理；T-374 L1 遗留承接、T-77 O-4 补课）。消费票：**T-394**（服务端落地：gate 豁免 + 真客户端全链）。
- **范围声明**：本文件只覆盖 npm 认证/会话端点族（K60）。npm 协议主面（publish/packument/tarball/dist-tags/unpublish/virtual 合并、存储布局、错误信封）仍以 `maven-npm-pypi.md` §0/§2 为准，不重复。
- **公开规范锚点（官方优先）**：npm 生态无独立 server-side 规范文档，**客户端源码即规范**——npm CLI（`lib/commands/login.js`、`lib/utils/auth.js`、`lib/utils/get-identity.js`）、npm-profile（`lib/index.js` 的 loginCouch/loginWeb）、npm-registry-fetch（`lib/index.js`/`lib/auth.js`）。本票逐文件核对了**本机安装的 npm 10.9.8 实物源码**（node 22.23.2 Homebrew 路径）并 live 抓包对拍。反编译只补客户端源码看不到的服务端行为，逐条标「此条补充」。
- 出处标记：`官方` = 上述客户端源码（本机 npm 10.9.8 实物 + 官方仓库 latest 对照）；`DE` = 反编译（`c.j.ph.npm.resource.NpmResource` / `command.authentication.NpmLoginCommand` / `NpmWhoAmICommand` / `model.login.NpmLoginAuthentication`）；`live` = 本票实测（真实 npm 10.9.8 客户端 + BinFlow HEAD 实例 + 本地抓包代理，2026-08-31）；`as-built` = BinFlow 代码现状。
- 置信度：`高` = 客户端源码 + live 双证（或 DE + 官方双证）；`中` = 单源；`低` = 推断。

---

## 1. K60 端点清单定案（T-394 实现面 == 本表）

前缀 `/binflow/api/npm/<repoKey>`（与协议主面同挂载；npm 客户端按 registry URL 追加 `/-/...`）。

| # | 方法 | 路径 | K60 定案 | 语义摘要 | 置信度 |
|---|---|---|---|---|---|
| K60-1 | PUT | `/-/user/org.couchdb.user:<username>` | **实现（修复面）**：httpapi 写认证门对该路径族豁免，使适配器 body 凭据臂可达 | legacy login：body 凭据（name/password）或 Authorization 头认证 → 201 铸 token；详见 §2 | 高 |
| K60-2 | GET | `/-/whoami` | **维持**（已实现 as-built） | 200 `{"username":"<name>"}`；匿名 401 + Basic challenge | 高 |
| K60-3 | GET/HEAD | `/-/ping` | **维持**（已实现，NE-07） | 200 `{}` | 高 |
| K60-4 | POST | `/-/v1/login` | **不实现**（现状 401/404 即正确姿态） | npm ≥9 默认 web 登录入口；任意 4xx/500 → 客户端 ENYI → 自动回落 couch（K60-1）。见 §4 | 高（live 双向验证） |
| K60-5 | GET/POST | `/-/npm/v1/user`；`GET/POST /-/npm/v1/tokens`；`DELETE /-/npm/v1/tokens/token/<key>` | **不做**（`npm profile` / `npm token` 命令族不在 BinFlow 面；等价物 = 控制台 + `POST /api/security/token`） | 404 姿态（npm 客户端如实报错） | 高（官方客户端契约；BinFlow 面 deliberate） |
| K60-6 | PUT/GET | `/-/user/org.couchdb.user:<name>/-rev/<rev>`；`...?write=true` | **不做，且设为不变量**：K60-1 **永不答 409** | npm 的 409 rev-dance 第二/三跳；服务端不答 409 则整族不可达（Artifactory 同样无此路由——`{user_id}` 单段 @PathParam 吃不下 `/`）。**若实现方让 login 答 409，npm 会走 GET ?write=true → 404 → 客户端报错** | 高 |

## 2. K60-1 login 端点 wire 语义（核心）

### 2.1 客户端真实请求（live 抓包，npm 10.9.8 `npm login --auth-type=legacy`，高）

```
PUT /binflow/api/npm/<repo>/-/user/org.couchdb.user:t393alice
headers: content-type: application/json; npm-auth-type: legacy; npm-command: login;
         accept: */*; user-agent: npm/10.9.8 node/v22.23.2 darwin x64 …
body:    {"_id":"org.couchdb.user:t393alice","name":"t393alice","password":"…",
          "type":"user","roles":[],"date":"2026-08-31T05:08:42.397Z"}
```

- **无 Authorization 头**（live 抓包逐头确认；npm-profile loginCouch 构造 body 凭据、不带 opts.auth——couchDB 惯例）。这是 T-374 L1 的根因：httpapi `contentAction(PUT)` 先要求凭据 → 401 at gate，请求到不了适配器 body 凭据臂。
- 用户名做 `encodeURIComponent`（npm-profile `couchEndpoint`）；服务端按路径段解析即可（`%2F` 等转义用户名不展开——as-built 单段路由天然如此）。
- body 字段集 = `_id/name/password/type/roles/date`；服务端只消费 `name`/`password`（DE 同——其余字段忽略，此条补充官方）。

### 2.2 响应契约（官方 + DE + as-built 三方）

| 场景 | 状态码 | 响应体 | 出处/置信度 |
|---|---|---|---|
| 认证成功 | **201** | `{"ok":true,"id":"org.couchdb.user:<name>","token":"<token>"}`；npm 只读 **`token`**（非空即可），随后写 `.npmrc` 的 `//<registry-uri>/:_authToken=` 并以 Bearer 复用 | 官方（npm-profile putCouch/newCreds）+ as-built live（201 实测）+ DE（201，`id` 为常量 `org.couchdb.user:undefined`——DE 细节，npm 不读 id） |
| 凭据错误 | **401** | BinFlow errors[] 信封（`{"errors":[{"status":401,"message":"authentication required"}]}`）；npmjs 官方 couch 形态是 `{"error":…,"reason":…}`——npm-registry-fetch 只按状态码归类（E401），body 形态不敏感（live 实测 E401 渲染正常） | 官方 + as-built + live |
| body 缺 name/password | **400** `User or Password fields are missing.` | DE 逐字（此条补充官方——官方客户端源码不定义服务端文案） | 高（DE） |
| 已存在用户 | **201 再铸**（幂等 login-mint，**永不 409**——见 K60-6 不变量） | 同成功行 | DE（无 409 分支）+ as-built（同构）；高 |

### 2.3 服务端语义流程（T-394 落地面）

1. **gate 豁免**：httpapi 内容面对 `PUT /-/user/org.couchdb.user:*` 路径族**不做写凭据前置门**（恢复 couch 惯例）；豁免仅此路径族 + PUT 动词（whoami/ping 读面不动）。
2. 认证解析（as-built 双臂，照旧）：Authorization 头 principal 优先；无头 → body `name`+`password` 经 argon2id 验证（present-but-wrong 一律 401，绝不静默匿名）。头 principal 与 body 指名**不同账户** → 403（as-built 防御臂，保留）。
3. 铸 token：复用 TokenRegistry（与 `POST /api/security/token` 同表同吊销链，NE-06 既有决策）；TTL as-built 720h 不变。
4. login 是**认证面不是制品写**：豁免后适配器只做验证 + 铸 token，零制品落盘——DE 同构（login 路由不做仓路径权限检查，此条补充官方）。

## 3. K60-2 whoami / K60-3 ping（维持面，live 复核）

- `GET /-/whoami`：带 `Authorization: Bearer <token>` → 200 `{"username":"<name>"}`（live：minted token → `t393alice`）；匿名 → **401 + `WWW-Authenticate: Basic realm="BinFlow Realm"`**。DE 为 **400** `User is not authenticated`（DE 偏差，此条补充官方）——npm 客户端任一错误码都归 ENEEDAUTH「This command requires you to be logged in.」，两可；BinFlow 取 401（官方 npmjs 惯例形态）。官方客户端细节：`.npmrc` 存 `username` 时 npm whoami **零 HTTP 直答**；仅 token 形态才发请求。
- `GET /-/ping` → 200 `{}`（NE-07 既有；本票 live 复核一致）。

## 4. 默认 `npm login`（web auth-type，npm ≥9 默认）行为链（live 全程抓包，高）

1. `POST /-/v1/login`，body `{}`，头 `npm-auth-type: web` → BinFlow 现状：匿名 401（写门）/ 带凭据 404（`npm endpoint -/v1/login is not implemented in BinFlow`）。
2. npm 客户端把**任意 4xx 或 500** 归为 ENYI（WebLoginNotSupported）→ verbose `web login not supported, trying couch` → **自动回落 legacy 提示 + K60-1 couch PUT**。
3. 结论：**不做 web 登录端点是正确姿态**——默认 `npm login` 与 `--auth-type=legacy` 最终汇聚到同一 K60-1 修复点；两链 live 均验证到 couch PUT（当前 401）。
4. （边界）官方 web 流成功路径要求 POST 答 `{loginUrl, doneUrl}` + doneUrl 轮询 202/200——BinFlow 不实现即不展开。

## 5. 客户端环境行为注记（live 抓包，非端点契约，实现/测试勿误判）

- **环境流量**：npm 任意命令（含 login/publish/install）可能对 `<registry>/npm` 发 **GET ×2**（pacote manifest 探测，自更新横幅检查，daily/weekly 限频但 404 不记账）。空仓 404 即可，非登录协议组成部分。
- **`.npmrc` 凭据键匹配**：`//<host>:<port>/<path>/:_authToken`（或 `:_auth`）的 host:port:path 必须与 registry URL **逐字符匹配**——本票 live 实证：键写 `:18910` 而请求走 `:18911`（代理口）→ 凭据不附带 → ENEEDAUTH（T-374 尾斜杠坑的姊妹坑：**端口也参与匹配**）。
- login 成功后 npm 把凭据行改写为 `_authToken` 形态（live：`.npmrc` `_auth` 行被 token 行取代）。

## 6. 与公开规范的差异/补充汇总

- **官方（客户端源码）已定义、本规格直接引用**：couch login 的请求形态（PUT 路径/无认证头/body 字段集）、201/401/400/409 语义与 409 rev-dance 全流程、web 登录 ENYI 回落链、whoami 的 token-Bearer 形态与 ENEEDAUTH、ping。
- **官方未定义、DE 补充**：400 缺字段文案 `User or Password fields are missing.`；login 幂等再铸（无 409 臂）；whoami 匿名 400；login 201 体 `id` 常量拼写。
- **DE 与 BinFlow 的有意差异**（BinFlow 取官方/自家惯例，已定案）：whoami 匿名 401（DE 400）；login 201 体 `id` = 真实 `org.couchdb.user:<subject>` + 多出 `rev` 字段（npm 不读，无害）；错误体用 BinFlow errors[] 信封（npm 不敏感）。
- **as-built 修复面**（T-394）：仅 §2.3-1 的 gate 豁免是缺口；适配器 body 凭据臂/铸 token/whoami/ping 均已实现且 live 可用（Basic 头路径 201 + token 全链 publish/install 实测绿）。

## 7. 待验证清单

| # | 项 | 现置信度 | 验证途径 |
|---|---|---|---|
| 1 | K60-1 gate 豁免后的真客户端 `npm login --auth-type=legacy` 全绿（无 Basic 前提的裸 login） | 高（契约级）/ 中（组合未跑） | T-394 AC1 首跑（本票只能以 `_auth` 等效腿代跑，live 已绿） |
| 2 | npm ≥11 / node ≥24 客户端行为漂移（npm-profile loginCouch 形态是否变动） | 中 | 升级客户端抽测（文档站客户端锚随升级窗） |
| 3 | DE whoami 匿名 400 的错误体形态（errors[] 还是裸文本） | 低 | t226 活体核对（可观测面小，npm 不敏感） |
