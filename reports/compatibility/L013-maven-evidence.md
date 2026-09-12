# L013-4 · Maven 元数据计算取证（双系统 wire 矩阵）

- 模式: **dual**（参照 artifactory-ux :8082 7.161.x ③次重启后窗口；BinFlow UAT :8083 = `uat-l0134-bebd92b1`（revision 标签 cc55428d；`git diff bebd92b1..cc55428d -- cmd/ internal/` 为空 → Go 树等价，仅差一个 docs commit））
- 规格锚: docs/reverse/maven-npm-pypi.md §1（§1.3 snapshot 改写 / §1.4 metadata 计算 / §1.5 checksum）+ [MVN-MD]
- wire 证据: `reports/compatibility/l0134-wire/{a,b}/maven/`（a=参照 b=BinFlow；*.hdr/*.body 逐臂落盘，mvn 日志 mvn-deploy-{a,b}.log）
- 脚本: `tools/difftest/l0134/maven-evidence.sh`（矩阵）、`tools/difftest/l0134/mvn-deploy-leg.sh`（真实 CLI 腿）、`tools/difftest/l0134/build-image-offline.sh`（UAT 重建）
- difftest 命名空间: `l0134-mvn-{d,u,cc,sg}` 双端（已删净，两侧 repo 列表回到基线：ref=example-repo-local / bf=docker-local）

## 0. 矩阵判定分布（18 臂）

**一致 11 / 差异 7**（差异根因聚类：BUG 候选 ×2〔其中 unique-rewrite 缺失跨 3 臂〕、UNKNOWN ×3）。

| # | 臂 | 判定 | 分类建议 | 证据一句 |
|---|---|---|---|---|
| A1 | group metadata versions 累积（1.0.0→1.1.0→2.0.0-SNAPSHOT） | 一致 | — | a2/a3 双端 versions 逐增不丢行（a2-get-groupmeta.body 双端同构） |
| A2 | latest/release 算术（SNAPSHOT 计入 latest；release=最后非 SNAPSHOT） | 一致 | — | 双端 a3: latest=2.0.0-SNAPSHOT / release=1.1.0 |
| A3 | lastUpdated 格式 | 一致 | — | 双端 `yyyyMMddHHmmss`（20260912222739 / 20260912222033） |
| A4 | metadata XML 形态 | **差异** | UNKNOWN | 参照带 `modelVersion="1.1.0"` 属性 + **尾部 `<version>` 元素**（versioning 之后）；BinFlow 两者皆缺；snapshot 块内 lastUpdated/snapshot 子序不同（a3b 双端 body） |
| A5 | maven-metadata.xml .sha1/.md5 伴随（服务端自算） | 一致 | — | 双端 served-sha1 == shasum(metadata)（a4-sha1-check.txt 双端） |
| A6 | **默认仓 -SNAPSHOT PUT 服务端改写** | **差异** | **BUG 候选** | 参照默认仓（未设 behavior）把 `-SNAPSHOT.pom` 落为 `2.0.0-20260912.222739-1`（value 同）；BinFlow 保持 `-SNAPSHOT` 文件名与 value（a3b 双端） |
| B1 | unique 仓 raw -SNAPSHOT PUT 改写（snapshotVersionBehavior=unique 显式） | **差异** | **BUG 候选**（与 A6 同根） | 参照 b5: `probe-app-2.0.0-20260912.222753-1.jar`；BinFlow b5: 原名 `-SNAPSHOT.jar`；**BinFlow 创建仓时接受该字段（PUT 200）但行为缺位** |
| B2 | buildNumber/timestamp 跨趟递增 | **差异** | 同 B1 | 参照 jar→(ts,1)、pom 同秒同 N、二趟 jar→(ts',2)；BinFlow buildNumber 恒 1 无 timestamp |
| B3 | 客户端已 unique 文件名（mvn 默认）→ 不改写 + 算术 | 一致 | — | mvn 腿双端 ts-N 原样落盘，二趟 buildNumber=2（mvn-leg-snapdir-meta 双端） |
| C1 | 客户端手 PUT maven-metadata.xml（201）后的服务面 | **差异** | UNKNOWN | 参照逐字回放手写内容（c1b 见 9.9.9-bogus）直至下次触发重算；BinFlow 1s 后即回服务端重算形（b/c1b 无 bogus 行）——终态语义双端收敛（c1d 均无 bogus） |
| C2 | 并行 deploy 不同版本（4.0.0 ∥ 4.1.0）合并 | 一致 | — | 双端 versions 两行俱在、latest=4.1.0、无丢更新（c2c） |
| C3 | 并行同版本 unique snapshot（相异字节） | **差异** | UNKNOWN | 双端均 201+仅存一文件（同秒同 N 撞名/同名覆盖）；**参照连 snapshot metadata 一并丢失（404 稳态 2min+，c3c-late）**；BinFlow metadata 俱在（520B） |
| D1 | client-checksums 仓 + 匹配 .sha1 旁车 | 一致 | — | 双端 201 |
| D2 | client-checksums 仓 + 失配 .sha1 旁车 | 一致（文案微差） | —（normalize 候选） | 双端 409 同信封同句式；路径成分：参照 `com/l0134/...`（repo 相对）vs BinFlow `l0134-mvn-cc/com/l0134/...`（带 repo 前缀）（d3 双端 body） |
| D3 | server-generated-checksums 仓 + 失配旁车 | 一致 | — | 双端 201 容忍 |
| D4 | >1024B 旁车 | 一致 | — | 双端 409 `"Suspicious checksum file, content length of 1200 bytes is bigger than allowed."` 逐字同 |
| E1 | 真实 mvn CLI（3.9.16）deploy exit code/上传序列/算术 | 一致 | — | 双端 exit=0；4 PUT 同序（client-unique pom/jar + snapdir meta + group meta）；BUILD SUCCESS |
| E2 | mvn 腿落盘后存储面 | **差异** | **BUG 候选** | BinFlow 把 X-Checksum-* 头**物化成 .sha1/.md5 存储项**（9→15 项，连 maven-metadata.xml 也有旁车项）；参照 5 项（originalChecksums 走登记不落项）（mvn-leg-snapdir-list.json 双端） |

## 1. 差异根因聚类（给 maven 元数据计算票的实现段）

1. **服务端 unique snapshot 改写缺失**（A6/B1/B2，BUG 候选）：spec §1.3 `adjustMavenSnapshotPath` 未实现——repo 配置面接受 `snapshotVersionBehavior=unique`（200）但 PUT `-SNAPSHOT` 文件不改写、buildNumber 恒 1。**影响面**：mvn 3 默认 uniqueVersion=true 的主流路径不踩（客户端自带 ts-N 名）；踩的是 raw PUT / `uniqueVersion=false`（Maven 2 风格）/ deployer 工具链。参照默认行为=unique（A6 wire 证实，spec §1.3「unique（Maven 2 默认语义）」与配置模板默认的歧义由 wire 定案）。
2. **checksum 头物化为旁车存储项**（E2，BUG 候选）：BinFlow 在 mvn deploy（wagon 以 `X-Checksum-Sha1/Sha256` 头随 PUT）后把 .sha1/.md5 落成可见存储项；参照只登记 originalChecksums。浏览/listing 面出现幻影项，且与 §1.5「旁车只用于登记」的参照语义相反。
3. **metadata XML 形态三处偏差**（A4，UNKNOWN 待裁）：`modelVersion` 属性缺、尾部 `<version>` 元素缺、snapshot 块子序（lastUpdated 位置）不同。mvn 消费端容忍度未测（MetadataReader 对可选元素缺省容忍的可能性大，但 [MVN-MD] schema 两者都在册）。
4. **手 PUT metadata 即时重算**（C1，UNKNOWN）：BinFlow 对客户端直 PUT 的 maven-metadata.xml 立即以服务端重算覆盖（201 后 1s 内即不可见客户端字节）；参照保留客户端字节直至下次触发事件。真实 mvn 腿双端终态等价（E1），仅非 mvn 手 PUT 路径可见。
5. **并行同版本竞态的 metadata 丢失**（C3，UNKNOWN）：参照自身在竞态下丢失 snapshot metadata（404 稳态）——参照侧反常；BinFlow 反而完备。实现票无需对齐此反常，登 UNKNOWN 留裁。

## 2. 双端等价已证实面（契约断言可直接引用）

- group metadata versions/latest/release/lastUpdated 全套语义与格式（A1-A3）
- metadata .sha1/.md5 伴随=服务端自算且与内容一致（A5）
- checksum policy 三态全对齐（D1-D4，含 >1024B 409 逐字文案）
- 并行不同版本 deploy 无丢更新（C2）
- mvn 3.9.16 主流路径（客户端 unique 名）deploy 全绿：exit 0、序列同、buildNumber 算术同（B3/E1）

## 3. 环境事件记录（复跑者必读）

- 参照 :8082 在窗口内被外部两次 `docker stop`（22:18:47Z、22:32:03Z，均 exit 0——判定为并行轨道生命周期操作，非崩溃）；本票两次重启（各 ~2min 起）。
- 参照「recurrent request failures」拦截器与 admin 登录锁在并行 audit 轨道（audit-probe-user 404 流 + 内部 JFrog Access Client 自锁旋涡）叠加下多次误伤本票请求；矩阵脚本已加 403 退避（sleep 9 ×3）+ 请求间隔 2s，最终全臂落盘。
- UAT 重建走 L003-3 §2 指南；`build-release.sh` 死于 DaoCloud alpine 镜像 EOF（phase 3），按 `tools/difftest/l0072/build-image-offline.sh` 配方离线重组（phase 2 assets 镜像由失败运行残留提供，digest 钉本地 alpine:3.24）。
