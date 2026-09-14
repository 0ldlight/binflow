# Maven 版本目录 metadata 的 pom 前置行为规格

> 票：T-L017-2（LOOP 017）。定谳台账假说 `maven/reference-race-metadata-loss`：参照版本目录
> metadata 生成**要求同目录 pom 存在**。本文给出可观察行为规格 + 活体证据 + 重分类建议。
> 证据等级：**E1** = 反编译定位（文件:行号，见附录 A）；**E2** = :8082 参照活体臂（附录 B）；
> **E3** = 公开规范（[MVN-MD]）。置信度：高 = E1+E2 双证；中 = 仅单源。
> 既有相关规格：`maven-npm-pypi.md` §1.4（触发时机/内容规则）与 §1.5——本文不重复，只补前置条件、
> 客户端 metadata PUT 的 202 丢弃、buildNumber 种子三个空白面。

## 0. 假说定谳（结论先行）

**证成（confirmed）**。参照（Artifactory 7.161.x）在 Maven local 仓库中：

1. **SNAPSHOT 版本目录**的 `maven-metadata.xml` 生成以**同目录存在 `.pom` 文件**为硬前置——
   无 pom 则永不生成；任何重算触发事件到达时还会**主动删除**已存在的该文件（§1.1）。
2. **版本组目录**（`{group}/{module}/maven-metadata.xml`）的 `<versions>` 只统计含 pom 的子版本目录，
   无 pom 子目录不产生组 metadata（既有规格已载，本文补活体证，§1.2）。
3. 台账 C3 的「参照竞态丢 metadata」由此**完全消解**：竞态目录 jar-only → metadata 从未生成（404 稳态
   是 pom 前置的正常输出，不是竞态丢失）；「仅存一文件」面则是 buildNumber 种子 + 同秒时间戳撞名的
   确定性结果（§1.3），双端一致（BinFlow 同形）。
4. **新发现（本票增量）**：客户端 PUT 到 `-SNAPSHOT` 版本目录的 `maven-metadata.xml`，在 snapshot
   策略非 deployer 的仓库被**丢弃式受理**——响应 202、字节不落盘、不触发重算（§1.4）。台账
   `maven/manual-put-metadata-visibility` 的「逐字回放」观测面只适用于**版本组目录**（其活体臂即组目录）。

## 1. 行为规格（当客户端…服务端…）

### 1.1 SNAPSHOT 版本目录 metadata 的 pom 前置（置信度：高）

当客户端向 Maven local 仓库（maven-2-default 布局，snapshot 策略 unique 或 non-unique）的
`{groupPath}/{module}/{baseRev}-SNAPSHOT/` 目录只部署 jar（或任何非 pom 文件）而不部署 pom 时：

- 服务端返回部署成功（201），文件按 snapshot 策略改名落盘（unique：`{ts}-{N}` 服务端时间戳编号），
  **但不生成**该目录的 `maven-metadata.xml`——GET 该文件恒 404，无超时窗口（稳态）。
- 当客户端随后（任意时刻）向同目录部署 `.pom` 文件时：服务端在部署响应前后完成父目录（=该版本目录）
  metadata 的**同步**计算并落盘，GET 立即可得（`<snapshot>` 块 buildNumber/timestamp 取最新 unique
  pom；`<snapshotVersions>` 覆盖目录内 jar+pom 全部 (ext, classifier)，共用同一 `{ts}-{N}`）。
- 当客户端删除版本目录中**最后一个 pom**（jar 仍在）时：删除触发的重算发现目录无 pom → 服务端
  **删除**该目录的 `maven-metadata.xml`（伴随 `.sha512`），GET 变 404；jar 不受影响。
- 当版本目录曾由服务端生成 metadata、其后 pom 被移走（move）时：同上，下次重算触发即删除。
- 前置判定口径：pom = 文件路径 MIME 判定为 `application/x-maven-pom+xml`（即 `.pom` 后缀，
  checksum 旁车不算）；只看**直接子文件**，不看子目录。
- 例外（沿用既有规格 RTFACT-6242）：待删内容是 snapshot 型 metadata 而路径**非** snapshot 目录时不删；
  本规格域（路径就是 `-SNAPSHOT` 目录）无此豁免——snapshot 目录内无 pom 必删。

### 1.2 版本组目录 metadata 的 pom 前置（置信度：高；补活体证）

当 `{groupPath}/{module}/` 下所有子版本目录都无 pom（例如仅 release jar-only 目录）时：服务端不生成
组目录 `maven-metadata.xml`，GET 404；出现任一含 pom 的子目录后，pom 部署触发的（默认异步）祖父目录
计算生成 `<versions>` 清单。release 版本目录本身在 Maven 布局中**没有**自己的 metadata 文件——
客户端 PUT release jar 后 GET 其版本目录 `maven-metadata.xml` 恒 404 与 pom 无关（布局语义）。

### 1.3 buildNumber 种子与同秒撞名（C3 竞态面的确定性消解）（置信度：高）

当客户端向 unique 策略仓库的空版本目录以非 unique 文件名（`{module}-{baseRev}-SNAPSHOT.jar`）部署、
未携带 `build.timestamp` 属性时：服务端改名的编号 `{N}` 取自**父目录 maven-metadata.xml 的
`<snapshot><buildNumber>` +1**；该文件不存在（例如 pom 前置未满足、从未生成）时按 0 起算 →
首个文件必为 `{N}=1`，时间戳用服务端当前时刻。由此：

- 当两个相异字节的同坐标部署**落在同一秒**且目录无 metadata 时：两者改写为**完全相同的**
  `{ts}-1` 文件名 → 后写覆盖先写（两次响应均 201），目录终态恰一个文件。此为确定性算术结果，
  非竞态异常；BinFlow 同形（台账 C3 已证双端一致）。
- 当客户端先部署 pom 再并行部署双 jar 时：pom 已领 `{ts}-1`（或已生成 metadata），jar 取后续编号，
  目录 metadata 俱在——「丢 metadata」不出现。

### 1.4 客户端手 PUT snapshot 目录 metadata：202 丢弃式受理（置信度：高；本票新发现，此条补充官方规范）

当客户端 PUT 的目标路径是 `-SNAPSHOT` 版本目录下的 `maven-metadata.xml`（父目录名以 `-SNAPSHOT`
结尾且文件名为 metadata 名），且仓库 snapshot 策略**非 deployer**（unique/non-unique）时：

- 服务端**读空请求体并返回 202**，字节**不落盘**、不触发重算——目录 metadata 状态保持原样：
  - 目录无 pom（无 metadata）→ 随后 GET 仍 404；
  - 目录有 pom（服务端 metadata 在）→ GET 返回的是既有**服务端计算形**，客户端手写字节永不可见。
- 当仓库 snapshot 策略为 **deployer** 时：不适用本条，客户端 metadata 字节按普通文件存储（201）。
- 对照（既有规格/台账 C1 边界修正）：**版本组目录**（父目录非 `-SNAPSHOT` 结尾）的
  `maven-metadata.xml` 手 PUT 走普通存储（201），字节逐字回放至下次重算触发——台账
  `maven/manual-put-metadata-visibility` 观测即此面；snapshot 目录面是 202 丢弃，两者应分列。

## 2. 触发矩阵（何时跑重算；与 maven-npm-pypi.md §1.4 对齐后增量）

| 客户端事件 | 服务端重算范围 | 计算 metadata 的 pom 前置含义 | 置信度 |
|---|---|---|---|
| PUT unique snapshot 改写落盘后的文件 | 版本目录，同步 | 无 pom → 结果为空 → 不写且删既有 | 高 |
| PUT `-SNAPSHOT` 目录内 pom | 版本目录同步 + 祖父（组）目录异步（可配同步） | pom 到位 → 两级均可产出 | 高 |
| PUT `-SNAPSHOT` 目录内 metadata（非 deployer 仓） | **无重算**（请求被 202 丢弃，无落盘无事件） | 状态保持 | 高 |
| DELETE 版本目录内 pom | 版本目录 + 组目录，异步 | 无 pom 残留 → 删 metadata（RTFACT-6242 豁免不适用 snapshot 目录） | 高 |
| PUT 非 snapshot 文件（release jar 等） | 无 Maven 重算事件（组目录清单只随 pom 事件变动） | jar-only 目录任何时刻都无 metadata | 中（观测面与「算而空」不可区分） |

## 3. 与公开规范的差异/补充

- [MVN-MD]（maven.apache.org/repositories/metadata.html）只定义三级 metadata 的结构与用途
  （V 级 snapshotVersions 为快照解析映射），**未定义**服务端生成时机与前置条件——§1.1/§1.2/§1.4
  全部为此条补充官方规范。
- Maven 官方语义中 V 级 metadata「含该 baseVersion 目录全部制品映射」与本规格的 pom 前置并不矛盾：
  前置是 Artifactory 服务端**生成**条件，规范只描述文件一旦存在后的消费语义。

## 4. 重分类建议（供 compatibility-engineer / conductor 落账；本文档不改台账）

`maven/reference-race-metadata-loss`（UNKNOWN）建议拆解重分类：

1. 「参照 snapshot metadata 404 稳态」→ **不是竞态反常**，是 §1.1 pom 前置的确定性输出。建议将该面
   改判：登记为**参照确定性行为**（本规格为 authority），BinFlow「jar-only 目录从任意文件生成 574B
   metadata」成为**唯一差异侧**——建议新开条目（如 `maven/version-metadata-pom-prerequisite`）：
   BinFlow 生成多余 metadata = 对齐缺口（建议对齐方向：jar-only 不生成、重算触发时删除），或由产品裁
   INTENTIONAL。
2. 「双端 201 + 仅存一文件」→ 已证双端一致（本票 A4 复现参照同形；台账 C3 原文亦证），随 1 消解，
   不再独立登记。
3. `maven/manual-put-metadata-visibility`（R-21）→ 建议补描述边界：snapshot 版本目录手 PUT 是
   202 丢弃（§1.4），「逐字回放」仅组目录；BinFlow 若手 PUT snapshot 目录 metadata 落盘（201）则
   构成新差异面，呈批时口径需分列。

## 5. 待验证清单（低置信度）

- 无低置信度核心条目。两个中置信度待差分复核：
  1. §2 末行「release jar PUT 无任何 Maven 重算事件」：观测面与「异步算出空结果」不可区分
     （两种实现 GET 均 404）——如需区分须看服务端任务日志，compat 契约层面按「无 metadata 产出」
     断言即可（行为等价）。
  2. §1.4 deployer 仓 snapshot 目录手 PUT 落盘（201）为反编译单源推断（E1），未活体（本票 ≤6 臂
     预算未覆盖）——下轮差分可加一臂。
- BinFlow 侧对照臂（jar-only 574B、手 PUT 落盘形态）属差分执行域，不在本规格票内。

## 附录 A：E1 反编译定位（reverse-src/artifactory/backend-maven，只读取证）

| 证据 | 文件 | 行号 | 要点 |
|---|---|---|---|
| A-1 | `artifactory-core/.../maven/MavenMetadataCalculator.java` | L251-254 | snapshot 目录计算入口：目录无 pom → 返回空结果（不产出） |
| A-2 | 同上 | L213-215 | 空结果 → 调 removeMetadataIfExist（删 `maven-metadata.xml`） |
| A-3 | 同上 | L499-509 | pom 前置判定：遍历直接子文件，MIME 判 pom |
| A-4 | 同上 | L173-179 | 目录树过滤：非 pom 且非 unique-snapshot 文件不进入计算视野 |
| A-5 | 同上 | L228-235, L321-324, L398 | 组目录 `<versions>` = AQL `name matches '*.pom'` 且 depth=dir+2 的子目录；空 → 空结果 |
| A-6 | 同上 | L512-520, L534-558 | 删除动作与豁免（snapshot 型 metadata 在非 snapshot 路径不删） |
| A-7 | `artifactory-core/.../repo/interceptor/MavenMetadataCalculationInterceptor.java` | L94-114, L128-130 | afterCreate 触发矩阵：unique snapshot 文件/snapshot 目录 pom → 同步父目录计算 |
| A-8 | 同上 | L176-186 | afterDelete → 异步重算父目录 + 组目录 |
| A-9 | `artifactory-core/.../engine/UploadServiceImpl.java` | L572-581 | 非 deployer 仓 + snapshot 目录 metadata PUT → 消费请求体直接返回，不走落盘 |
| A-10 | `artifactory-core/.../util/UploadServiceUtils.java` | L322-325, L346-350 | 策略判定；丢弃式受理返回 202（日志「Skipping deployment of maven metadata file」） |
| A-11 | `artifactory-api/.../mime/MavenNaming.java` + `mime/NamingUtils.java` | L100-105 / L60-63 | snapshot 目录 metadata 判定（父目录 `-SNAPSHOT` 结尾）；pom = MIME `application/x-maven-pom+xml` |
| A-12 | `artifactory-core/.../repo/snapshot/UniqueSnapshotVersionAdapter.java` | L120-141, L157-175 | buildNumber 取父目录 metadata `<snapshot><buildNumber>`+1；文件不存在按 0 起算 → 首个必为 1 |
| A-13 | `artifactory-core/.../maven/MavenMetadataServiceImpl.java` | L96-98 | 本版本 async 入口直接委托同步实现（对外可见性不变：部署后即可见） |

## 附录 B：E2 活体臂（:8082 参照，仓 `l0172-mvn-u`，maven local，snapshot 策略 unique；2026-09-13）

| 臂 | 操作 | 结果 |
|---|---|---|
| A1 | PUT `m1-1.0-SNAPSHOT.jar`（jar-only 版本目录）→ GET 目录 metadata | 201；GET **404** |
| A2 | 同目录再 PUT `m1-1.0-SNAPSHOT.pom` → GET metadata | 201；GET **200**：buildNumber=1，snapshotVersions 含 jar+pom，共用 `1.0-20260913.130937-1` |
| A3 | jar-only 目录 m2 手 PUT `maven-metadata.xml`（含 bogus buildNumber）→ GET；再 PUT 第二只 jar → GET | PUT 返回 **202**；GET **404**（字节未落盘）；二 jar 后仍 404 |
| A3b | pom 俱在目录 m1 手 PUT 同一 bogus metadata → GET | **202**；GET 200 为服务端计算形（无 bogus，文档未变） |
| A4 | 空目录 m3 双 jar 并行（相异字节，同秒）→ 列目录 + GET metadata | 双 201；目录恰一文件 `m3-3.0-20260913.130956-1.jar`（同秒同 N 撞名）；metadata **404** 稳态 |
| A5 | 空目录 m4 先 pom 再双 jar 并行 → GET metadata | 201/201/201；metadata **200**，buildNumber=1 |
| A6 | 目录 m5（pom ts-1 + jar ts-2，metadata 200）仅 DELETE pom 文件 → GET metadata | DELETE 204；3s 后 metadata **404**，jar `m5-5.0-20260913.131236-2.jar` 仍在 |
| S1 | release jar-only `r1/1.0/r1-1.0.jar` → GET 版本目录与组目录 metadata；对照组 m1 | 版本目录 404、组目录 **404**；对照组（含 pom）组目录 **200** |

（活体侧物证：/tmp/l0172-*.xml 与 /tmp/m3.json；测试仓 l0172-mvn-u 留存 :8082 供复核，未清理。）

## 附录 C：E3 公开规范

- [MVN-MD] https://maven.apache.org/repositories/metadata.html —— 三级 metadata 结构与用途；V 级
  snapshotVersions = baseVersion 目录内制品-时间戳映射。对服务端生成时机、pom 前置、客户端 metadata
  PUT 的受理语义**均无规定**（2026-09-13 核对）。
