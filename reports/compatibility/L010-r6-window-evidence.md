# L010-4 — R-6 搭车取证窗（单窗口封顶一节，architect 排程裁定版）

> 状态: **双败定谳 ③（常量层不可覆盖）**——两发换链尝试（JVM 系统属性 / property 属性形态）均在 init 期确定性失败于同一 TLS 握手点；窗口按裁定封顶，未做第三发。运行时四问取证未达成（维持 L003 静态锚置信）；静态面备选立票建议见 §5。
> 取证环境: 参照实例 :8082（jfrog/artifactory-pro 7.161.20，docker 容器 `artifactory`）
> + 一次性 MinIO（RELEASE.2025-04-22T22-12-26Z，127.0.0.1:19000，桶 `artifactory-filestore`，AK/SK 本地随机生成、全程不落库、窗口末随容器销毁）
> 日期: 2026-09-12（UTC）
> 前置: L003-3 三次换链实录（`reports/compatibility/L003-s3-chain-evidence.md`）——本窗即其 §5 候选 1/2 的执行

## 0. 窗口终态速览

| 发次 | 注入面 | 有效性验证 | init 结果 |
|---|---|---|---|
| 第一发 | JVM 系统属性 `-Dbinary.provider.s3.https.only=false`（system.yaml `shared.extraJavaOpts` 注入）+ binarystore 双保险（`<enablePathStyleAccess>true</enablePathStyleAccess>` 子元素 + `<httpsOnly>false</httpsOnly>` 子元素） | **属性已确认入 JVM 命令行**（catalina 进程 PID 10550 cmdline 含 `-Dbinary.provider.s3.https.only=false`，matches=1） | **失败**：`Failed to initialize BinaryServiceImpl` + `S3SdkException: Remote host terminated the handshake (SDK Attempt Count: 4)` |
| 第二发 | 在第一发状态上叠加 `<property name="httpsOnly" value="false"/>` 属性形态（仅一次 XML 编辑+重启，按裁定） | grep 确认 XML 在位（子元素/property 形态/JVM 属性三面同时存在） | **失败**：同点失败，时间戳 `2026-09-12T11:37:32.531Z [ERROR] [o.j.s.b.BinaryServiceImpl:266] Failed to initialize BinaryServiceImpl`，同栈 `Remote host terminated the handshake (SDK Attempt Count: 4)` |

**定谳 ③**：TLS 强制为常量层行为，在已测三个配置面上不可覆盖——
1. 子元素形态 `<httpsOnly>false</httpsOnly>`（L003 尝试 3）：不生效；
2. JVM dotted 键 `binary.provider.s3.https.only=false`（本窗第一发，cmdline 已验证送达）：不生效；
3. property 属性形态 `<property name="httpsOnly" value="false"/>`（本窗第二发）：不生效。
   三面**同时在场**仍 TLS 强制 → 对明文 S3 兼容后端，7.161.20 的 s3-storage-v3 链在 binarystore.xml + JVM 属性可及范围内无法关闭 TLS。

## 1. 取证四问（本窗零新增——双败，运行时取证未达）

| # | 问题 | 当前置信（全部继承 L003 静态锚） | 本窗增量 |
|---|---|---|---|
| Q1 | GET 走 redirect 还是直读？ | 默认恒直读（字节码错误串高置信）；redirect 开启后运行时形态未取证 | 无（被 TLS 常量层拦截在 init） |
| Q2 | 预签名 URL 实形态（参数集/签名版本）？ | 未取证 | 无 |
| Q3 | X-Amz-Expires 有效期实测？ | 未取证（键 `signedUrlExpirySeconds`/`s3SignedUrlExpirySeconds` 存在，默认值未知） | 无 |
| Q4 | 降级阈值行为？ | 未取证（常量 `cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800，中置信；语义=小于此值不重定向直接代理） | 无 |

本窗为四问准备的取证面已就绪未消费：MinIO 起立+建桶成功、宿主/容器双侧连通探针 200、`enableSignedUrlRedirect=true` 已随链配置在位——阻塞点自始至终只有 Artifactory 侧 TLS 强制。

## 2. 双发实录（全程透明）

1. **基线**：换链前 ping 200；live `binarystore.xml` sha256 `2d1409c6124858a44b9b404e06b88ce879b1315d6644844668e7849e85e4f479`（与 L003 恢复终态逐字节一致）；`system.yaml` sha256 `29951b7c106af0754190b3616f26b56dcd0e83615f2b5daf9de0c206c3dc2afe`。双侧备份（容器 `.bak-l0104` + 宿主 `/tmp/*-l0104-host`）三份同 sha 核对。
2. **基线标记制品**：PUT `example-repo-local/restore-marker/l0104-marker.txt` 201，GET 200，sha1 `cabf2aec180a1e94c30d72bad6c4db228f37112d` 逐字节一致（走原 file-system 链）。
3. **环境件**：一次性 MinIO 起立（127.0.0.1:19000，AK 20 位/SK 40 位 openssl 随机，`/tmp/l0104-minio-creds.env` 0600 中转）；宿主探针 200、`artifactory` 容器探针（host.docker.internal:19000）200；`mc mb` 建桶 `artifactory-filestore` 成功。
4. **第一发**：binarystore 换 `chain template="s3-storage-v3"`（endpoint `host.docker.internal:19000` 无 scheme + identity/credential + bucketName + path-style/httpsOnly 子元素双保险 + `enableSignedUrlRedirect=true`）；system.yaml `shared: extraJavaOpts: -Dbinary.provider.s3.https.only=false`。重启（11:31:44Z）→ init 失败于 TLS 握手（同 L003 尝试 2/3 失败点）。**送达性验证**：catalina PID 10550 cmdline 实测含该属性——排除"属性未注入"假阳性，第一发为有效阴性。
5. **第二发**：binarystore 叠加 `<property name="httpsOnly" value="false"/>`（一次 sed 编辑），重启（11:35:53Z）→ 11:37:32.531Z 同点失败（`BinaryServiceImpl:266`，`Remote host terminated the handshake (SDK Attempt Count: 4)`；时间戳晚于重启，排除第一发日志残留）。
6. 两发失败均为 init 期确定性拒绝（Spring bean 装配失败、无数据面写入、原 filestore 零触碰）。

## 3. 恢复证明（硬要求，全部满足）

- [x] `binarystore.xml` 还原：容器内 sha256 = `2d1409c6…`，chain 回 `file-system` 模板，与 L003 终态基线逐字节一致
- [x] `system.yaml` 还原：sha256 = `29951b7c…` 与基线一致；`extraJavaOpts` 键 grep 计 0（清除确认）
- [x] 重启后 ping 200（11:38:33Z 重启 → 11:41:45Z 200，~192s 锁恢复，落在 L003 实测 ~160-210s 窗内）
- [x] 标记制品 GET 200（43B），sha1 `cabf2aec180a1e94c30d72bad6c4db228f37112d` 逐字节一致
- [x] MinIO 容器/桶/凭据清理：`l0104-minio` 已 rm（桶随容器消失）；容器侧 `.bak-l0104` ×2、宿主侧凭据文件（creds.env / 含 AK-SK 的 binarystore 草稿 / 含加密 DB 口令的 system.yaml 副本）全部删除；宿主仅留**无凭据**的 `/tmp/binarystore.xml.orig-l0104-host`（L003 同例）
- [x] 终态 ping 200；原 filestore 全程零触碰（两发均止步 init，无任何 PUT 进云链）

## 4. 残余未测假说（按可能性排序，供后续窗口/票，本窗不执行）

1. **endpoint 显式 scheme**：`<endpoint>http://host.docker.internal:19000</endpoint>`（带 scheme）。本窗两发均用无 scheme 形态（对齐 JFrog 文档惯例、由 httpsOnly 决定 scheme）；带 scheme 形态未测——若 SDK 优先取 endpoint 自带 scheme，则 TLS 常量论需修正为"scheme 决定权在 endpoint 文本"。
2. **JVM camel 键**：`-DbinaryProviderS3HttpsOnly=false`（键表收录的驼峰对键，L003 §5 已列，本窗按裁定只测 dotted 形态）。
3. **MinIO 侧 TLS**（L003 §5 替代路径）：自签证书 + artifactory truststore 注入——绕开开关问题，但同样动容器环境。
> 注：假说 1 成本最低（一次 XML 编辑+重启）；若后续窗口批准，恢复机械操作沿用本窗/L003 已三次+两次验证的配方即可。

## 5. R-6 裁定回填建议（静态面备选立票）

**四问维持 evidence-blocked，不盲裁**（ADR-0001：无运行时证据不裁兼容行为）。至此取证不确定性已彻底收敛：换链机械（备份/恢复/标记验证）五次 100% 可复现，环境件三轮就绪，唯一阻塞=TLS 常量层，且其三个可及配置面已全部试尽。

**静态面备选票（产品裁定权，建议立票）**——对齐字节码已证事实的最小实现：
1. BinFlow S3 链 GET **默认直读**（服务端代理），与 Artifactory 默认一致（`enableSignedUrlRedirect` 默认关，字节码高置信）；
2. **redirect 显式开启**：`enableSignedUrlRedirect=true` 时走 302 预签名跳转（形态未取证——BinFlow 自定SigV4 预签名即可，留 known-divergence UNKNOWN）；
3. **`signedUrlExpirySeconds` 可配**（默认值未取证——BinFlow 自定默认并写入文档）；
4. 阈值 `cloud.binary.provider.redirect.threshold.in.bytes`（默认 204800，中置信）作为 BinFlow 同名可配项预留下已知锚点，语义按"小于阈值不重定向、直接代理"实现。
> 即：BinFlow 按「默认直读、redirect 显式开启 + `signedUrlExpirySeconds` 可配」实现，redirect 面/有效期默认值/阈值运行时行为三项留 known-divergence UNKNOWN——与 L003 §6 建议一致，本窗以双败定谳将其从"备选"升为"推荐主路径"。

## 6. 原始摘录（脱敏；凭据零出现）

```
# 第一发送达性（catalina 进程 cmdline 实测）
PID=10550
-Dbinary.provider.s3.https.only=false
matches=1

# 第二发失败（artifactory-service.log，行 12506 起；时间戳晚于 11:35:53Z 重启）
2026-09-12T11:37:32.531Z [ERROR] [d96a28e04b4b5278] [o.j.s.b.BinaryServiceImpl:266 ] [art-init] - Failed to initialize BinaryServiceImpl
org.jfrog.type.s3.exception.S3SdkException: Unable to execute HTTP request:
    Remote host terminated the handshake (SDK Attempt Count: 4)
（无 UnknownHostException——path-style 生效；三面 httpsOnly 关闭形态在场仍 TLS 强制）

# 恢复终态
/var/opt/jfrog/artifactory/etc/artifactory/binarystore.xml  2d1409c6…（=基线）
/var/opt/jfrog/artifactory/etc/system.yaml                 29951b7c…（=基线，extraJavaOpts 计 0）
11:41:45 ping=200（~192s 锁恢复）
marker GET: 200 43B  sha1 cabf2aec180a1e94c30d72bad6c4db228f37112d（=PUT 时）
```
