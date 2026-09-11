# L003-3 — Artifactory s3-storage-v3 云链运行时取证（U-STG-15 / R-6 证据票）

> 状态: **BLOCKED 终态（conductor 批准的重跑窗口已消耗，第三次换链同点失败）**——三次换链尝试均在 init 期确定性失败并三次完整恢复+验证；`httpsOnly` 的**子元素形态同样不生效**，TLS 强制的真实开关未定位（下一切入点=JVM 系统属性形态 `binary.provider.s3.https.only`，未验证）。静态锚（§1）与三次失败-恢复取证（§2）完整。
> 取证环境: 参照实例 :8082（jfrog/artifactory-pro 7.161.20，docker 容器 `artifactory`）
> + 一次性 MinIO（RELEASE.2025-04-22T22-12-26Z，127.0.0.1:19000，桶 `artifactory-filestore`，AK/SK 本地生成不入库）
> 日期: 2026-09-11（UTC）

## 0. 取证四问（结论速览——答到证据为止）

| # | 问题 | 结论 | 证据 |
|---|---|---|---|
| Q1 | GET 走 redirect 还是直读（代理）？ | **默认恒直读**：`enableSignedUrlRedirect` 默认关（字节码错误串：未设 true 时 `cannot generate signed url`）；redirect 是显式开启后的可选路径。开启后的运行时形态 **未取证**（换链 BLOCKED） | §1 |
| Q2 | 预签名 URL 形态（参数集/签名版本）？ | 静态面：机器类族为 AWS SDK v2 `S3Presigner`（`PresignRequestGenerator`/`S3GeneratePresignedUrlSupplier`/`S3InternalPresignedUrlRequestGenerator`）；另有 HA 内部签名 URL 面（`INTERNAL_SIGNED_URL_PREFIX/MARKER` + `remoteMemberRedirectAllowed`）。URL 实形态 **未取证** | §1 |
| Q3 | 有效期多少？ | 配置键存在：`signedUrlExpirySeconds`（S3 专键 `s3SignedUrlExpirySeconds`、CloudFront 变体 `s3SignatureExpirySeconds`）；默认值未从常量池读出，X-Amz-Expires 实证 **未取证** | §1 |
| Q4 | 降级/阈值行为？ | 阈值常量 `cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800（binary-provider-chain §4.4-C2，中置信）；运行时阈值上下/降级路径 **未取证** | §1 + reverse 规格交叉 |
| Q0 | （附）换链配置面本身的运行时事实 | **路径形态**：SDK v2 默认虚拟主机式寻址（`bucket.endpoint` 域名拼接）；`<enablePathStyleAccess>true</enablePathStyleAccess>` **子元素形态生效**（property 属性形态不生效——尝试 2/3 过 DNS 证明）。**传输安全**：自定义 endpoint 下 SDK 强制 TLS；`<httpsOnly>false</httpsOnly>` 子元素形态**不生效**（尝试 3 实证：加上后仍在 TLS 握手层被断，`Remote host terminated the handshake` ×4 重试）——明文 S3 兼容后端在 binarystore.xml 的两种已试形态下均不可达，真实开关未定位 | §2 |

## 1. 静态锚（jar 字节码，binary-store-filestore/core/api 4.372.5，容器内提取）

- **redirect 开关**: `enableSignedUrlRedirect` 是 provider 级参数，**默认关**。字节码错误串原文：
  `Provider {} cannot generate signed url since 'enableSignedUrlRedirect' parameter is not set to 'true' in provider configuration`
  （`CloudBinaryProvider`，binary-store-core）。即：不开此参数，链恒走服务端代理读。
- **有效期键**: `signedUrlExpirySeconds`（provider 参数，`BinaryStoreProperties$Key` 全键表收录；S3 专键 `s3SignedUrlExpirySeconds`、CloudFront 变体 `s3SignatureExpirySeconds`）。默认值未从常量池读出——**以运行时 X-Amz-Expires 实证为准**。
- **redirect 机器类族**: `S3SignedUrlRedirectService` / `SignedUrlParameters` / `PresignRequestGenerator` / `S3GeneratePresignedUrlSupplier`；另有 `INTERNAL_SIGNED_URL_PREFIX/MARKER/USER_NAME` 与 `remoteMemberRedirectAllowed`（HA 节点间内部签名 URL 面）。
- **path-style 寻址**: `S3ClientV2Builder.configureMiscProperties` 读 `getBooleanParam("enablePathStyleAccess")` → 为真时 `forcePathStyle(true)` / `pathStyleAccessEnabled(true)`。参数经 `StorageTenantProperties.getProperty(providerId, key)` 按 **provider id** 键取。
- **XML 形态**: `ProviderAdapter`（JAXB）把 provider **子元素**解析为 param（元素名=参数名，文本=值）；`<property name= value=>` 是另一模型（`Property`，XmlAttribute）。实证：`<property name="enablePathStyleAccess" value="true"/>` 形态**未生效**（见 §2 首败根因），子元素形态待臂 1 验证。

## 2. 换链-失败-恢复实录（两次尝试，全程透明）

1. 备份：容器内 `binarystore.xml` → `binarystore.xml.bak-l0033` + 宿主 `/tmp/binarystore.xml.orig-l0033-host-backup`；sha256 `2d1409c6124858a44b9b404e06b88ce879b1315d6644844668e7849e85e4f479` 双侧一致。
2. 恢复基线标记制品：PUT `example-repo-local/restore-marker/l0033-marker.txt`（sha1 `8bad62a610dde9ebad1cf6736b754133e3bfd4c7`，GET 200）。
3. **尝试 1**（`<property name="enablePathStyleAccess" value="true"/>` 属性形态）→ 重启 → init 失败：`java.net.UnknownHostException: artifactory-filestore.host.docker.internal`（`S3AwsBinaryProvider.initialize → shouldCreateBucket → doesContainerExist → headBucket`）。判定：SDK v2 虚拟主机式寻址，property 形态未入 `StorageTenantProperties`；MinIO 连通性无问题（容器内探针 200）。**立即恢复**（sha 校验一致）→ ping 200 + 标记制品 sha1 逐字节一致。
4. **尝试 2**（子元素形态 `<enablePathStyleAccess>true</enablePathStyleAccess>`）→ 重启 → init 失败（较尝试 1 前进一层）：`S3SdkException: Unable to execute HTTP request: Remote host terminated the handshake (SDK Attempt Count: 4)` + SSL 栈（`SSLSocketInputRecord.decode`…）。判定：**path-style 已生效**（无 UnknownHost，DNS/寻址正确），但 SDK 对自定义 endpoint 仍强制 TLS——`httpsOnly` 默认 true，明文 MinIO（http://…:19000）握手即断。**立即恢复**（sha 校验一致）→ 重启验证见 §4。
5. **尝试 3（conductor 批准的重跑窗口，evidence §5 配方：子元素 path-style + `<httpsOnly>false</httpsOnly>`）** → 重启 → init 失败，**同尝试 2 的失败点**：`S3SdkException: Unable to execute HTTP request: Remote host terminated the handshake (SDK Attempt Count: 4)`——`httpsOnly` 子元素形态不生效，TLS 强制的真实开关在 provider XML 之外（下一切入点：JVM 系统属性 `binary.provider.s3.https.only` / `binaryProviderS3HttpsOnly`，`BinaryStoreProperties$Key` 键表收录了这对 dotted/camel 键；未验证）。**立即恢复**（sha 校验一致）→ ping 200（~210s 锁恢复）+ 标记制品 GET 200 sha1 逐字节一致。按硬规则终态 BLOCKED，不再重试。
6. 三次失败均为 init 期确定性拒绝（Spring bean 装配失败、无数据面写入、原 filestore 零触碰）。根因链：DNS 寻址（→子元素 path-style 修正，已证有效）→ TLS 强制（→httpsOnly 子元素修正，**已证无效**）。剩余不确定性：TLS 开关的真实形态。

## 3. 运行时取证（未达——三次换链均止步于 init，两臂 BLOCKED 终态）

- 3.1 臂 1（默认链直读）：未取证。
- 3.2 臂 2（enableSignedUrlRedirect=true 的 302/预签名/有效期/阈值）：未取证。
- 环境件就绪度已两轮验证：MinIO 起立/建桶/连通性探针/`mc admin trace` 管道（含 query 全捕获）全部可用；阻塞点仅在 Artifactory 侧 TLS 强制（§2 尝试 2/3）。

## 4. 恢复证明（最终态，硬要求）

- [x] binarystore.xml 还原 ×3（每次恢复后容器内 sha256 = `2d1409c6…` 与备份逐字节一致；换链前 live/容器备份/宿主备份三份同 sha 核对）
- [x] 第一次恢复后：ping 200 + 标记制品 GET 200 sha1 `8bad62a6…` 逐字节一致
- [x] 第二次恢复后：ping 200（~160s 锁恢复后）+ 标记制品 GET 200 sha1 `8bad62a610dde9ebad1cf6736b754133e3bfd4c7` 逐字节一致
- [x] 第三次恢复后（终态）：ping 200（~210s 锁恢复后，实测 `ping OK after ~210s`）+ 标记制品 GET 200 sha1 `8bad62a610dde9ebad1cf6736b754133e3bfd4c7` 逐字节一致（实测 `marker GET: 200 89B`）
- [x] MinIO 容器/桶/凭据清理 ×2 轮（l0033-minio/l0033-mctrace 已 rm，桶随容器消失，creds 文件与含凭据 XML 已删；宿主侧仅留无凭据的 binarystore 备份与构建日志）
- 原 filestore 全程零触碰（三次换链窗口内无任何 PUT 进云链；标记制品始终走原 file-system 链）

## 5. 下一切入点（未验证假说——供后续票，不再由本轨执行）

TLS 强制开关的候选形态（按可能性排序）：
1. JVM 系统属性：`-Dbinary.provider.s3.https.only=false`（`BinaryStoreProperties$Key` 键表收录 dotted/camel 对键；注入面=JAVA_OPTIONS 环境变量或 setenv）。
2. `<property name="httpsOnly" value="false"/>` 属性形态（本轨两种 XML 形态只试了子元素与 property 的 pathStyle 变体；httpsOnly 的 property 形态未单独验证）。
3. 常量默认：`BinaryStoreConstValues` 层面的出厂值（键表有 `defaultValue` 迹象），可能只能经系统属性覆盖。
替代路径（零换链风险）：给 MinIO 配 TLS（自签证书 + artifactory 信任面注入 `JAVA_OPTS=-Djsse.enableSNIExtension=...`/truststore）——绕开开关问题，但 truststore 注入同样动容器环境。两条路都需新批准窗口；换链-恢复机械操作与风险面已被本轨三次验证（恢复配方 100% 可复现）。

## 6. R-6 裁定回填建议

**维持 evidence-blocked，不裁**。理由：四问中仅 Q1 的「默认恒直读」达到字节码级高置信；预签名实形态/有效期默认值/阈值运行时行为均无运行时证据——盲裁即猜测兼容行为（ADR-0001 红线）。取证的剩余不确定性已从「换链是否可行」收敛为「TLS 开关形态」（§5 三候选）。若后续窗口仍拿不到，备选路径：BinFlow 按「默认直读、redirect 显式开启 + `signedUrlExpirySeconds` 可配」实现（对齐字节码已证事实），redirect 面留 known-divergence UNKNOWN——属产品裁定权，本票不越权。

## 7. 原始摘录（脱敏）

```
# 尝试 1 根因（artifactory-service.log）
Caused by: java.net.UnknownHostException: artifactory-filestore.host.docker.internal
	at java.base/java.net.InetAddress$CachedLookup.get(InetAddress.java:909)
Suppressed: software.amazon.awssdk.core.exception.SdkClientException: Request attempt 2 failure:
	Unable to execute HTTP request: artifactory-filestore.host.docker.internal
调用栈：S3AwsBinaryProvider.initialize:149 → shouldCreateBucket:488 → doesContainerExist:205
	→ S3StorageServiceV2SyncClient.doesBucketExist:114 → DefaultS3Client.headBucket

# 尝试 2/3 根因（artifactory-service.log；两次同点，尝试 3 = 加 <httpsOnly>false</httpsOnly> 后）
2026-09-11T02:57:53.517Z [ERROR] [o.j.s.b.BinaryServiceImpl:266] - Failed to initialize BinaryServiceImpl
org.jfrog.type.s3.exception.S3SdkException: Unable to execute HTTP request:
	Remote host terminated the handshake (SDK Attempt Count: 4)
	at sun.security.ssl.SSLSocketInputRecord.decode(SSLSocketInputRecord.java:160)
（无 UnknownHostException——path-style 子元素形态生效；httpsOnly 子元素形态在尝试 3 中未改变行为）

# path-style 机制（binary-store-filestore-4.372.5.jar 字节码，S3ClientV2Builder.configureMiscProperties）
8: ldc_w  #846   // String enablePathStyleAccess
11: invokevirtual #401 // BinaryProviderBase.getBooleanParam:(Ljava/lang/String;)Z
14: ifeq 28
22: invokeinterface #848 // S3BaseClientBuilder.forcePathStyle:(Ljava/lang/Boolean;)…

# redirect 默认关（binary-store-core-4.372.5.jar，CloudBinaryProvider 字符串常量）
"Provider {} cannot generate signed url since 'enableSignedUrlRedirect' parameter is not set to 'true' in provider configuration"
键表（BinaryStoreProperties$Key）：enableSignedUrlRedirect / signedUrlExpirySeconds / s3SignedUrlExpirySeconds /
	s3SignatureExpirySeconds / remoteMemberRedirectAllowed / cloudFront{DomainName,KeyPairId,PrivateKey}
TLS 相关键（同键表）：httpsOnly / s3HttpsOnly / dotted 形态 binary.provider.s3.https.only
```
