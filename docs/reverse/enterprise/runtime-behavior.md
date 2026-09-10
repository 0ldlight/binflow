# E4 实测记录 — 企业功能目录与 license 门控（runtime-behavior）

> 参照实例：`http://localhost:8082`（docker `artifactory`，Artifactory Pro 7.161.20 rev 86120900，license=Enterprise Plus Trial，全部 addon 激活）。
> 探测时间：2026-09-11。凭据已脱敏（admin / <密码>）。
> 命令均为只读 GET（无安装/删除 license 操作）。

---

## 1. GET /artifactory/api/system/version（admin）

```bash
curl -s -u admin:'<密码>' http://localhost:8082/artifactory/api/system/version
```

```json
{
  "version" : "7.161.20",
  "revision" : "86120900",
  "servicesVersions" : { "package_handler_version" : "5.675.40" },
  "addons" : [ "docker", "helmoci", "oci", "packages-archive", "vagrant", "replication",
    "filestore", "curation", "plugins", "worker", "gems", "composer", "bower", "nuget",
    "debian", "opkg", "rpm", "cocoapods", "conan", "vcs", "release-bundle", "jf-connect",
    "jf-event", "keys", "alpine", "analytics", "cargo", "chef", "federated", "git",
    "observability", "onboarding", "pub", "rest", "swift", "lead-artifact-detector",
    "terraform", "license", "package-reroute", "puppet", "ldap", "sso", "layouts",
    "properties", "search", "securityresourceaddon", "filtered-resources", "p2", "watch",
    "webstart", "support", "xray", "retention", "policies", "archive" ],
  "license" : "26a15afeb8ddffa50e93bc1ad6f20817574b77275",
  "entitlements" : {
    "EVENT_BASED_PULL_REPLICATION" : true,
    "SMART_REMOTE_TARGET_FOR_EDGE" : false,
    "REPO_REPLICATION" : true,
    "MULTIPUSH_REPLICATION" : true
  }
}
```

**解读**（与 A 源码对账）：
- addons 共 **55 项** = installer 56 个已装配 addon（54 个 `artifactory-addon-*` jar + `binary-store-filestore`(filestore) + `packages-archive`）减去 `ha`（非 HA 部署强制 DISABLED）。
- `license` 末位 `5` → Product.Type.ENTERPRISE_PLUS_TRIAL（Enterprise Plus Trial），与 §2 license API 的 type 一致。
- `entitlements` 仅 4 项（复制相关），`SMART_REMOTE_TARGET_FOR_EDGE=false` 对应 `LicenseAddonsManagerImpl.isEntitled` switch 的 default-false 分支——非 Edge 节点即使 Enterprise Plus Trial 也返回 false。

## 2. GET /artifactory/api/system/license（admin）

```bash
curl -s -u admin:'<密码>' http://localhost:8082/artifactory/api/system/license
```

```json
{
  "type" : "Enterprise Plus Trial",
  "validThrough" : "Nov 3, 2026",
  "licensedTo" : "TEST JFrog Ltd."
}
```

（单机形态：`{type, validThrough, licensedTo}`；HA 形态应为 `{licenses:[...]}`，本实例非 HA 未观察到。）

GET `/api/system/licenses`（复数，同目录的另一 resource）返回与上完全相同的单机 license 详情。

## 3. 匿名访问（negative probe）

| 端点 | 匿名响应 |
|------|----------|
| GET /artifactory/api/system/version | 401（无 body） |
| GET /artifactory/api/system/license | 401 `{"errors":[{"status":401,"message":"Authentication is required"}]}` |
| GET /artifactory/api/system/licenses | 401 |

## 4. GET /artifactory/api/system/ping（匿名可达）

```
200 OK
```

license 正常时 ping 不受 lockdown 影响（`isLockdown` 仅在 lockdown=true 时返回 503）。

## 5. License 文件布局（docker exec 只读）

```bash
docker exec artifactory sh -c 'ls -la /opt/jfrog/artifactory/var/etc/artifactory/'
```

- 路径：`/opt/jfrog/artifactory/var/etc/artifactory/artifactory.lic`
- 大小：10,760 字节
- 格式：base64 文本（起始 `bGljZW5zZTog...`，即 `license: <base64>` 行式结构；首块解码后再嵌套 base64 的签名 license 对象）
- 该路径即 A 源码 `e.java` 读取的 `$ARTIFACTORY_HOME/etc/artifactory.lic`（etcDir=var/etc/artifactory）。

## 6. 实测结论与代码对账摘要

| 观察 | 代码依据（A） | 一致性 |
|------|--------------|--------|
| addons 55 项、无 ha | `SpringConfigResourceLoader.loadEnabledAddons` 非 HA 强制禁 ha；`activateAddons` 全激活 | 一致 |
| license hash 末位 5 = EP Trial | `ArtifactoryLicenseProvider.getLicenseKeyHash` 类型码表 | 一致 |
| entitlements 4 项、SMART_REMOTE_TARGET_FOR_EDGE=false | `VersionResource.replicationEntitlements` 固定 4 项；switch default false | 一致 |
| license API 返回三字段 | `ArtifactoryLicensesResource.getNonHaLicenseDetails` | 一致 |
| ping 200（license 有效） | `PingResource.isLockdown` 仅 lockdown 时 503 | 一致 |
| version/license 401（匿名） | `@RolesAllowed({admin,user})`/`{admin,ha}` | 一致 |

## 7. 未实测项（需降级实例）

本实例 license 全开，以下路径未能观察（详见 license-behavior.md §8）：无 license 503、trial 过期 24 前缀拦截、termed readOnly 上传拒绝、HA 集群冲突路径、JCR EULA 503。
