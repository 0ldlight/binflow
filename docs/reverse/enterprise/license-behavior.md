# Artifactory License 生命周期行为规格（license-behavior）

> **证据索引**：本文件是 `docs/reverse/enterprise/` 系列中的行为规格文档。
> - A = 反编译源码 `/Users/lzw/workspace/artifactory-decompiled/backend`（7.161.24，只读）。
> - B = 运行时参照实例 `http://localhost:8082`（7.161.20 Enterprise Plus Trial，凭据经本地环境变量（不入库），E4 实测）。
> - C = 安装包 `/Users/lzw/Downloads/artifactory-pro-7.161.16`。
> - E 等级：E1=A+B/C 双源；E3=A 单源（代码可读但实例不可观察）；UNKNOWN=需降级实例 E4 验证。
> - 行为句式：「当 license 为 X 时，对 Y 的请求返回 Z」。
> - 参照实例 license 全开（Enterprise Plus Trial），故所有「拒绝路径」无法直接观察，标注 UNKNOWN 并说明代码依据。

---

## 1. License 数据模型

### 1.1 产品类型（Product.Type）

当 license 被解析后，每个产品（artifactory/xray）携带一个类型。类型全集：
`OSS | COMMERCIAL | COMMERCIAL_XWE | ARCHIVE_TRIAL | ARCHIVE | EDGE_TRIAL | EDGE | TRIAL | TRIAL_XWE | ENTERPRISE | ENTERPRISE_PLUS_TRIAL | ENTERPRISE_PLUS`（E1：A=`Product.java:150-206`）。

类型谓词：
- 当类型为 `TRIAL / ENTERPRISE_PLUS_TRIAL / EDGE_TRIAL / ARCHIVE_TRIAL` 时，`isTrial()` 为真。
- 当类型为 `ENTERPRISE / ENTERPRISE_PLUS / ENTERPRISE_PLUS_TRIAL` 时，`isEnterprise()` 为真。
- 当类型为 `EDGE / EDGE_TRIAL` 时，`isEdge()` 为真。
- 当类型为 `ENTERPRISE_PLUS / ENTERPRISE_PLUS_TRIAL` 时，`isEnterprisePlus()` 为真。
- 当类型为 `ARCHIVE / ARCHIVE_TRIAL` 时，`isArchive()` 为真。

### 1.2 License key hash 编码

当服务端计算 license key hash 时，返回 `<sha1 前缀><类型码>`——类型码为末位单数字：0=OSS、1=TRIAL、2=COMMERCIAL、3=ENTERPRISE、4=ENTERPRISE_PLUS、5=ENTERPRISE_PLUS_TRIAL、6=EDGE、7=EDGE_TRIAL、8=ARCHIVE、9=ARCHIVE_TRIAL（E1：A=`ArtifactoryLicenseProvider.getLicenseKeyHash():412-446`）。
- 当 excludeNewLicensesSuffix=true 时，新类型（4~9）一律返回后缀 "3"（兼容旧版 Artifactory 对端解析）。
- 当 key 为空时返回 `da39a3ee5e6b4b0d3255bfef95601890afd807090`（空串 sha1）。
- B 实测：`/api/system/version.license = "26a15afeb8ddffa50e93bc1ad6f20817574b77275"`，末位 5 = Enterprise Plus Trial，与 `/api/system/license.type` 一致（E4）。

### 1.3 License 文件存储布局

- 单机（非 HA）：当服务启动时从 `$ARTIFACTORY_HOME/etc/artifactory.lic` 读取；若该文件不存在且 `artifactory.cluster.license` 存在则回退读取后者。单机安装 license 时写回 `artifactory.lic`（E3：A=`e.java:106-133`）。
- HA：license 池存 DB，每节点 hash 写入 `artifactory_servers.license_key_hash` 列（E3：A=`a.java`、`ArtifactoryLicenseProvider.a(String):242-245`）。
- 文件格式：base64 编码的签名 license 对象（含 products map：artifactory/xray 各带 type/expires/owner/termedLicense 属性）。B 实测容器 `/opt/jfrog/artifactory/var/etc/artifactory/artifactory.lic` 10760 字节，base64 PEM 形态（E4）。
- Partner license（AWS）：环境变量触发 `PartnerLicenseProvider`，license 文件可经 `artifactory.vmware.license` 属性重定向（E3）。

### 1.4 Termed license（期次许可）

当 license 的 properties 中 `termedLicense=true` 时，`isTermedLicense()` 为真。此时：
- `isReadOnly()` = termedLicense && 已过期。即 termed license 过期后实例转为只读（非 termed license 过期后行为见 §4）。
- 当 `ConstantValues.termLicenseDynamicExpiryDateEnabled=true` 且 EntitlementService 可用时，有效期取 max(license expires, JFConnect 下发动态过期日)（E3：A=`AddonsManagerImpl.getLicenseValidUntil():452-460`）。

---

## 2. License 安装/激活流程

### 2.1 安装（POST /api/system/licenses）

当客户端以 admin 或 ha 角色向 `/api/system/licenses` 提交 license key（单个字符串或 `[{licenseKey:...}]` JSON 数组，HA 多 license）时：

1. 当实例为 AOL（云端）时，返回 400 "Artifactory Online does not require license, Please contact support@jfrog.com..."。
2. 当实例为 partner license 时，返回 400 "Artifactory is running with a partner license, this feature isn't supported"。
3. 当请求体无法解析出任何 key 时，返回 400 "No license key supplied."。
4. 当单机模式提交多个 license 时，返回 400 "Only Artifactory High Availability setup support uploading multiple licenses."（UnsupportedOperationException 包装）。
5. 每个 key 先离线验证签名/类型/版本：
   - 已存在（hash 重复）→ `licenseExists`；
   - 非本版本 → `notForThisVersion`；
   - 签名无效 → `invalidKey`。
6. 验证通过的 key 写入 license 池（文件或 DB）→ 立即触发 activateLicense → 成功则 `activateAddons`（全部非 DISABLED addon 置 ACTIVATED）→ 通知所有 LicenseEventListener → HA 下向其它节点传播（propagateLicenseChanges）。
7. 当激活成功时返回 200，body 含每个 key 的验证消息；当全部无效时返回 400。
（E1：A=`ArtifactoryLicensesResource.java:93-119`、`LicenseAddonsManagerImpl.addLicenses():418-428`；D=JFrog REST 文档 licenses 端点。）

在线验证（`validateOnline`）：当不跳过在线验证时，key 提交 JFrog 服务端核验（`OnlineLicenseValidationServiceImpl`）；实例离线时验证降级本地（E3）。

### 2.2 查询（GET /api/system/license）

- 单机：返回 `{type, validThrough, licensedTo}`。B 实测返回 `{"type":"Enterprise Plus Trial","validThrough":"Nov 3, 2026","licensedTo":"TEST JFrog Ltd."}`（E4）。
- HA：返回 `{licenses:[{type, validThrough, licensedTo, licenseHash, expired, nodeId, nodeUrl}]}` 列表（E3：A=`getHaLicensesDetails():204-212`）。
- AOL：该端点对 AOL 实例返回 400（assertApiAllowedInternally）。
- 角色要求：admin/ha；scope `internal:mc:r` 或 `system:info/licenses:r`。

### 2.3 激活/重载

- `POST /api/system/licenses/activate`：当传入 ignoredLicenses（hash 列表）时重新走激活流程，忽略指定 license（用于集群冲突后重试）（E3）。
- `POST /api/system/licenses/licenseChanged`：HA 其它节点 license 变更后的回执端点，清缓存重载（E3）。
- 运行时 license 文件变更（单机）：每 2 秒轮询文件 mtime（`hasChange()` 节流），变更时重载并重激活，无需重启（E3：A=`e.java:89-104`）。

### 2.4 删除（DELETE /api/system/licenses?licenseHash=...）

- 当非 HA 模式时，返回 403 "This call is only available for Artifactory High Availability"。
- 当请求者非 admin 时，返回 403 "Only Artifactory admin user is allowed to use this call"。
- HA 下逐 key 检查：key 在其它活跃节点使用中 → `licenseInUse`（若用在本节点则本地重激活，在别的节点则远程传播激活）；无效 hash → `invalidKey`；通过后从池中删除并传播（E1：A=`LicenseAddonsManagerImpl.removeLicenses():472-533`）。

---

## 3. License 选择算法（多 license 时）

当 license 池中有多个 license 时，服务端按以下顺序选取当前使用 license（E3：A=`ArtifactoryLicenseProvider.acquireLicense():165-202`��：

1. 按 Product.Type.ordinal **降序**（Enterprise Plus > Enterprise > Commercial > Trial > ...），同类型按 expires **降序**——优先级：更高 tier、更晚过期。
2. 对每个候选：取 `license.lock` ConflictGuard（120s 超时）→ 检查集群活跃成员 hash 表，被其它节点占用则跳过 → `isLicenseCanBeActivated` 验证（含版本、runningMode 一致性、license 不重复）→ 通过则占用并写 DB。
3. 全部不可用时 `isLicenseInstalled()` 为 false（进入无 license lockdown）。

License 过期后自动重取（`shouldReAcquireLicense`）：当当前 license 过期且池中存在更晚过期的可用 license 时，返回 true 触发重取（E3：A=:209-221）。

---

## 4. 过期/降级/无 license 的 API 行为

### 4.1 无 license（isLicenseInstalled=false）

| 场景 | 行为 | E |
|------|------|---|
| 任意上传/下载请求 | lockdown=true → 503 "No license installed!"（经 interceptResponse 拦截） | E3（A=`LicenseAddonsManagerImpl.a(boolean):710-713`） |
| GET /api/system/ping | 503（isLockdown 检查） | E3 |
| /api/system/version | 正常返回（version 端点不 lockdown），addons 列表为空，license=null | E3 |
| OSS/JCR/ConanCE 版本 | 无 license 也可运行（OssAddonsManager.lockdown 恒 false，仅 JCR EULA 未签时 503） | E3 |
| UI admin 页面 | 当 !isLicenseInstalled 时 admin 页面对所有用户开放（引导装 license） | E3（A=`isAdminPageAccessible():248-252`） |

### 4.2 Trial 过期（trialLicenseExpired=true）

| 场景 | 行为 | E |
|------|------|---|
| 任意上传/下载请求 | 503 "Trial license expired!" | E3 |
| REST 调用（路径前缀在 trialExpirationRestCallToBlock 24 项中） | 503（LicenseRestFilter 拦截） | E3 |
| UI footer | 警告 "Your Artifactory Trial license has expired. To purchase a license..." | E3 |

trialExpirationRestCallToBlock 路径前缀：`nuget, npm, yum, deb, docker, gems, pypi, vcs, cran, conda, composer, conan, puppet, chef, pub, cargo, terraform, pods, lfs, helm, alpine, release, blob/info, security/keys/trusted`（E1：A=`AddonsManager.java:48`）。

### 4.3 商业 license 过期（非 termed）

当 license 为 COMMERCIAL/ENTERPRISE 等**非 termed** 类型且已过期时：
- `isReadOnly()` 为 false（readOnly 仅针对 termed license）。
- `isLicenseValidForThisVersion` 检查 `expires > 1274803121468L`（2010-07-25，宽底线）——实际过期 license 此检查为 false → 触发 4.1 的 "No license installed!" lockdown 路径（503）。
- UI footer 提示 "Your Artifactory Enterprise license has expired. To renew your license please contact sales@jfrog.com."（E3：A=`FooterMessageUtils.a(Product.Type):196-213`）。

### 4.4 Termed license 过期（isReadOnly=true）

当 license 为 termed 且已过期、且动态过期日未延长时：
- 上传请求：503 "Unable to upload to Artifactory due to an expired license. You may want to check the expiration of your current JFrog Platform License..."（E3：A=`a(ArtifactoryRequest,boolean):722-725`）。
- 下载请求：放行（下载走 checkReadOnly=false 路径）。
- 远程仓库回源下载：`isRemoteDownloadAllowed` 返回 false → 远程拉取被拒（警告日志）——本地缓存制品仍可下载（E3：A=`isRemoteDownloadAllowed():1056-1062`）。
- Edge 混合集群上传：403 "Deployment to an Edge Artifactory node is not supported."（例外 repo：`_intransit`、`jfrog-support-bundle`、release-bundles v2 仓、`artifactory-edge-uploads`）（E3：A=`AddonsManagerImpl.isUploadRequestBlocked():241-258`）。

### 4.5 版本不匹配（isInstalledLicenseValidForThisVersion=false）

当 license 签发的适用版本低于当前 Artifactory 版本时：503 "No license installed!"（与无 license 同消息）（E3）。

### 4.6 AOL 流量超限

当 AOL 实例免费订阅月流量超限时：429 "You have reached your monthly quota for Data Transfer. To continue and start enjoying unlimited transfer, upgrade your subscription"（Pipelines 内部调用豁免）（E3：A=`:698-703`）。

### 4.7 集群 license 冲突

当 HA 集群出现以下 VerificationResult 时（verifyAllArtifactoryServers）：
- `duplicateServerIds / converting / notSameVersion / runningModeConflict` → 本节点 `context.setOffline()` → 全部请求 503 "Artifactory is in offline state. For more details please check the logs!"。
- `duplicateLicense` → 本节点 releaseLicense（让出 license，进入无 license lockdown）。
- `haConfiguredNotHaLicense` → 非 HA family license 无法在 HA 集群激活（E3：A=`LicenseAddonsManagerImpl.b(VerificationResult,boolean):913-930`）。

---

## 5. License 类型对 API 的可见影响

### 5.1 /api/system/version

当 license 变化时该端点返回的 `addons`（ACTIVATED addon 名列表）、`license`（hash）、`entitlements`（4 项复制相关布尔值）随之变化（E1：A=`VersionResource.java:79-114`；E4=B 实测 55 addons + entitlements）。
- 当请求 User-Agent 为旧版 `Artifactory/<6.0.0` 时，license hash 使用 excludeNewLicensesSuffix 变体（新类型后缀折叠为 3）（E3：A=`shouldIgnoreNewLicenses():123-132`）。

### 5.2 AQL 查询超时按 license 分档

当实例为 AOL（SaaS）时，AQL 查询超时按订阅档位：FREE/FREE_XRAY=5min、PRO 档=15min、ENT 档=30min、ENTERPRISE_PLUS=45min；自托管取 `aql.query.timeout.seconds` 常量；`jfrog-worker` UA 走 workers 专用超时（E3：A=`ArtifactoryLicenseProvider.getAqlQueryTimeoutByLicense():786-838`、`AqlQueryTimeout.java`）。

### 5.3 UI footer 消息矩阵

| license 状态 | footer 消息 | 可见性 |
|--------------|-------------|--------|
| 无 license（单机） | "No license found. If you have a license click here to activate..." | all |
| Partner license | "Licensed to AWS" / AWS License Server 取不到时警告 | all |
| 即将过期（trial<10 天 / 其它<30 天） | "Your Artifactory [Trial] license will expire in N day(s)..." | admin |
| 已过期 | 按类型："...has expired. To purchase a license..." | trial→user, oss/ent→admin/info |
| 集群 mixed entPlus/edge | "One or more of the cluster nodes is not activated with an Enterprise+/Edge license which may lead to limited functionality..." | all |
| 集群部分节点无 license | "N of the cluster nodes do(es) not have a license..." | all |
（E3：A=`FooterMessageUtils.java:96-243`。）

---

## 6. JFConnect / Entitlement 服务交互

- 当 `EntitlementService.isAvailable()`（JFConnect client 已建立）时，`isEntitled(type)` 优先询问 JFConnect 服务端（gRPC），命中即返回。
- 当 `isEnforced()`（服务端开启强制）时，本地 license 判定**完全旁路**——JFConnect 返回 false 即 false，即使本地 license 允许（E3：A=`AddonsManagerImpl.a(EntitlementType,boolean):414-430`）。
- 当 JFConnect 不可用时，回退 `LicenseAddonsManagerImpl.isEntitled()` 的本地 switch（§feature-gates 三）。
- Termed license + JFConnect 可用时：每 5 分钟身份校验（Alibaba 环境专有），blocked 时上传禁用（E3）。

---

## 7. OSS / JCR / ConanCE 特殊版本行为

- 当运行模式为 OSS 时：`isLicenseInstalled()` 恒 false、`getEnabledAddonNames()` 恒空、lockdown 恒 false（不因 license 拦截任何请求）；安装 license 端点仍可调用但 `addAndActivateLicenses` 抛 UnsupportedOperationException（E3：A=`OssAddonsManager.java:145-233`）。
- 当运行模式为 JCR（容器注册版）时：DOCKER/PROPERTIES/SMART_REPO/S3/DISTRIBUTION/AQL/AOL 7 个 addon 视为已支持（JCR 激活集）；EULA 未签署时所有请求 503 "In order to use Artifactory you must accept the EULA first"；安装 license 返回 "Operation is not supported on Artifactory JCR"（E3：A=:98-99, 435-450）。
- 当运行模��为 ConanCE（C/C++ 社区版）时：仅 CONAN addon 视为已支持；安装 license 返回 "Operation is not supported on Artifactory Free"（E3：A=:143-147, 211-215）。
- OSS 版本检测到 DB 中存在活跃 HA 成员时：启动即失败（IllegalStateException "Found active HA servers in DB, OSS is not supported in by active HA environment. Shutting down Artifactory."）（E3：A=:396-404）。

---

## 8. 待验证清单（需降级实例 E4 验证的 UNKNOWN 项）

以下行为在当前全激活参照实例上无法直接观察，仅有代码依据（E3）。**需用无 license / 过期 trial / edge / termed 实例做差分验证**：

1. 无 license 503 的实际响应体格式（是否含错误 envelope、Content-Type）——A 证据仅给状态码与消息字符串。
2. Trial 过期时 24 个路径前缀的逐一拦截验证（LicenseRestFilter 的 path 匹配是 URI 全路径还是相对路径）。
3. Termed license 过期的 readOnly 上传 503 与下载放行的实测确认。
4. `haConfiguredNotHaLicense` 在 HA 集群装非 HA license 时的实际 API 表现。
5. JCR EULA 未签署时 503 的触发范围（是否包含 /api/system/ping）。
6. License 安装时在线验证失败（网络不通 JFrog）的具体错误码。
7. License hash 后缀在新旧客户端互操作（6.0 前后）下的实际值。
8. 集群 mixed license 的具体功能降级范围（footer 之外哪些 API 返回错误）。
9. `addons.disabled` 运行时修改后是否需要重启生效。
10. Partner（AWS）license 的 license API 返回形态。
