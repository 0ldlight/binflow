---
title: Maven 接入
sidebar_position: 20
---

# Maven 接入

> 适用版本：M3（Maven 2 layout + maven-metadata.xml + remote/virtual；PRD milestone-3 v1.2）。
> 本文核心链在 M3 QA 基线（commit `0f86229`，T-74/T-76 验收产物）上复跑：release deploy、全新本地仓 resolve、mirror 全量收口、virtual 混合解析均退出码 0（复跑记录见 `reports/agents/T-77.md`）；snapshot `-U` 强刷与 checksum 两态链取自 T-74/T-76 验收记录。客户端锚定 mvn 3.9.x（3.9.9 实测）；Gradle 8.x 走 Maven 仓可用（P2 观察，见文末）。

把 BinFlow 当作 Maven 2 仓库用：`mvn deploy` 发内部构件、`settings.xml`/pom 指过来解析依赖——layout、`maven-metadata.xml`、checksum 语义与 Nexus/Artifactory 一致，迁移时 URL 前缀从 `/artifactory` 改成 `/binflow` 即可。

## 前置条件

- 运行中的 BinFlow 实例（下文 `BASE=http://localhost:8080`）。
- 管理员凭据 `admin` / `$ADMIN_PW`。
- 一个 `packageType=maven` 的仓库（第 1 步创建）。

## URL 形态

Maven 客户端直接走**内容路径**（无 API 前缀），layout 即 Maven 2 标准：

```
$BASE/binflow/<repoKey>/<groupId 点转斜杠>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<ext>
```

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
export MVN_REPO=$BASE/binflow/maven-local
```

匿名读默认开启（resolve 不需要凭据）；**deploy（PUT）必须认证**——凭据放在 `settings.xml` 的 `<server>`（下文）。

## 接入步骤

### 1. 创建 maven 仓库

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"maven"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

可选拓扑：`remote` 仓代理上游（如 Maven Central）、`virtual` 仓聚合 local + remote——见 [remote/virtual 管理指南](../admin/remote-virtual.md)。

### 2. 发布：settings.xml + altDeploymentRepository

`~/.m2/settings.xml`（或用 `-s` 指定独立文件，CI 推荐）：

```xml
<settings>
  <servers>
    <server>
      <id>binflow</id>
      <username>admin</username>
      <password>$ADMIN_PW</password>
    </server>
  </servers>
</settings>
```

工程内执行（`id` 必须与 `altDeploymentRepository` 首段一致，格式 `id::layout::url`）：

```bash
mvn -B -DskipTests deploy \
  -DaltDeploymentRepository=binflow::default::$MVN_REPO
# BUILD SUCCESS；pom/jar/旁车 checksum/metadata 全部 201
```

验证落盘（jar 逐位一致、旁车 sha1 为 40 位裸 hex）：

```bash
curl -su admin:$ADMIN_PW -o dl.jar \
  $MVN_REPO/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar
curl -su admin:$ADMIN_PW \
  $MVN_REPO/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar.sha1
# 返回 40 位 hex（无换行），应等于 sha1sum dl.jar
```

### 3. 解析：pom `<repositories>`（推荐起步形态）

消费侧 pom 声明 BinFlow 仓库，匿名即可拉取：

```xml
<repositories>
  <repository>
    <id>bf</id>
    <url>http://localhost:8080/binflow/maven-local</url>
  </repository>
</repositories>
```

```bash
mvn -B -Dmaven.repo.local=$(pwd)/fresh-repo compile    # BUILD SUCCESS
```

### 4. 解析：settings.xml `<mirror>` 全量收口（团队统一出口）

把一切 Maven 流量（含插件）收进一个 virtual 仓——这是「团队只配一个 URL」的终态形态：

```xml
<settings>
  <mirrors>
    <mirror>
      <id>binflow</id>
      <mirrorOf>*</mirrorOf>
      <url>http://localhost:8080/binflow/maven-virtual</url>
    </mirror>
  </mirrors>
  <servers>
    <server><id>binflow</id><username>admin</username><password>$ADMIN_PW</password></server>
  </servers>
</settings>
```

两个实测必知的坑：

- **`mirrorOf: *` 会连插件解析一起接管**——virtual 必须含一个代理 Maven Central 的 remote 成员，否则 `mvn compile` 在 `maven-resources-plugin` 等默认插件处直接失败（T-77 复跑实证）。先建 Central remote 成员：

  ```bash
  curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/maven-remote-central \
    -H 'Content-Type: application/json' \
    -d '{"rclass":"remote","packageType":"maven","url":"https://repo.maven.apache.org/maven2"}' \
    -o /dev/null -w '%{http_code}\n'        # 200
  ```

- **Maven 会把解析失败缓存在本地仓的 `.lastUpdated` 标记里**——修好配置后如果还报 `Could not find artifact`，清掉 `-Dmaven.repo.local` 目录或加 `-U` 重试。

## SNAPSHOT 语义

- mvn 默认 unique deploy：落盘文件名为 `demo-app-1.2.0-20260819.212603-2.jar`（`yyyyMMdd.HHmmss-buildNumber`），目录仍是 `1.2.0-SNAPSHOT/`；两次 deploy 后 version 级 `maven-metadata.xml` 的 `buildNumber` 递增。
- 全新本地仓拿最新 SNAPSHOT：消费侧 pom 声明 SNAPSHOT 依赖 + `<repositories>` 指向仓，执行 `mvn -B -U compile`——`-U` 强刷 metadata，实际解析到最新 buildNumber 的 timestamped 文件（T-74 M16b 实测拿到 `-2` 号）。注意 `dependency:get -DremoteRepositories=` 在 mvn 3.9.x 不生效（见文末不兼容表）。
- 服务端按存储事实计算 `maven-metadata.xml`（versions/latest/release/lastUpdated、snapshotVersions），并发 deploy 不丢版本；客户端 PUT metadata 被接受并触发重算，不直接成为清单来源。
- **强刷一个已缓存的 metadata**：remote 仓没有 `?refresh` 参数——对缓存路径执行 `DELETE`（删本地缓存）后下次 GET 回源，详见[管理指南](../admin/remote-virtual.md#缓存管理与强刷手法)。

仓级配置（建仓 JSON 内）：

| 字段 | 取值 | 行为 |
|---|---|---|
| `snapshotVersionBehavior` | `deployer`（默认）/ `non-unique` / `unique` | M3 三值都按上传名落盘；`unique` 的服务端改写（timestamped 重命名 + buildNumber 续号）为 P2 未实现，行为等同 `deployer` |
| `handleSnapshots` | `true`（默认）/ `false` | `false` 时 SNAPSHOT deploy → **409**（GET 不受影响） |
| `handleReleases` | `true`（默认）/ `false` | `false` 时 release deploy → **409** |

## checksum 策略

mvn deploy 自动携带 `X-Checksum-*` 头与 `.sha1`/`.md5` 旁车文件，BinFlow 按仓配置校验：

| `checksumPolicyType` | 客户端摘要与实测不一致时 | 说明 |
|---|---|---|
| `client-checksums`（默认，未配置同） | **409**，message 形如 `Checksum error for '<repo>/<path>': received '<x>' but actual is '<y>'` | 严格模式，存储不可变优先 |
| `server-generated-checksums` | 接受，落盘与回显以**服务端实测**为准 | 宽容模式（历史构件迁移场景） |

客户端一个摘要都没给时，服务端计算并接受（两模式下都不拒绝）。GET 侧继承通用下载链：`X-Checksum-Sha1/Md5/Sha256` 头、`ETag=<sha1>`、Range 206、`If-None-Match` 304；旁车 GET 返回服务端实测的裸 hex。

注意：**remote 仓的 checksum 旁车请求（`.jar.sha1` 等）不回源，404** `Checksums are not downloadable.`——Maven 3.9 resolver 对缺失 checksum 只是告警不失败，全链不受影响（T-75/T-76 实测）。

## 匿名与凭据

| 场景 | 行为 |
|---|---|
| 匿名 GET/HEAD（默认 `anonymous_access: true`） | 200——resolve、IDE 索引均免凭据 |
| 匿名 PUT（deploy） | **401** `authentication required`（写操作认证不豁免） |
| 全局关匿名（`security.anonymous_access: false`） | GET 也需凭据：settings.xml 的 `<server>` 同样覆盖解析场景 |

## 有意不兼容与差异（Maven 域）

| 行为 | BinFlow | 依据 |
|---|---|---|
| layout 严格校验 | 不合规路径（目录 < 3 层、文件名不以 `<artifactId>-<version>` 开头）→ **400**；maven 仓不可当 generic 仓误用 | PRD FR-16-AC8（400 码值暂行） |
| Maven 索引 | 不生成 `nexus-maven-repository-index.gz`；`/binflow/<repo>/.index/**` 404——IDE 依赖 `maven-metadata.xml` 即可工作 | §2.2 |
| remote 仓 checksum 旁车 | 404 `Checksums are not downloadable.`（不回源；checksum 经响应头提供） | FR-20-AC13 定案 |
| `snapshotVersionBehavior=unique` 服务端改写 | P2 未实现（按上传名落盘）；`maxUniqueSnapshots` 保留策略 M4 | FR-16-AC12 |
| `mvn dependency:get -DremoteRepositories=` | mvn 3.9.x 捆绑的 dependency-plugin 3.7.0 不认该参数覆盖——请用 pom `<repositories>` 或 settings mirror 形态 | T-75 勘误 E1 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| deploy 409 `... handling of snapshots is disabled (handleSnapshots=false).` | 仓配置关了 SNAPSHOT（或 release 对应 `handleReleases`） | 改仓配置或换正确的仓 |
| deploy/PUT 409 `Checksum error ... received '<x>' but actual is '<y>'` | 客户端声明的摘要与内容不符（client-checksums 仓） | 检查构件完整性；或对迁移仓配 `server-generated-checksums` |
| PUT 400 `maven layout: "foo.jar": ... at least 3 directories` | 路径不是 `<groupId 路径>/<artifactId>/<version>/<file>` | 用 mvn deploy 或标准 layout；通用文件请放 generic 仓 |
| PUT 400 `file name "x.jar" must start with "<artifactId>-<version>"` | 文件名与目录 GAV 不符 | 同上 |
| 401 `authentication required` | 匿名 deploy，或 settings.xml `<server>` 的 `id` 与 `altDeploymentRepository` 首段不一致 | 对齐 id；检查口令 |
| `Could not find artifact`（修好配置后仍报） | 本地仓 `.lastUpdated` 负缓存 | 清 `maven.repo.local` 或 `mvn -U` |
| mirror 形态下默认插件解析失败 | `mirrorOf: *` 接管一切流量但 virtual 无 Central 成员 | virtual 加 `maven-remote-central` 成员（上文第 4 步） |

## Gradle（P2 观察，非门槛）

Gradle 8.x 走 BinFlow maven 仓可用（T-76 实测 `gradle build` exit 0）：

```kotlin
repositories {
    maven {
        url = uri("http://localhost:8080/binflow/maven-local")
        allowInsecureProtocol = true          // 明文 HTTP 必配
    }
}
```

`.module` 元数据 404 后 Gradle 自动回落 POM，属正常行为。

## 下一步

- 代理 Maven Central / 聚合 local+remote：[remote/virtual 管理指南](../admin/remote-virtual.md)
- npm / PyPI 接入：[npm](npm.md) · [pypi](pypi.md)
- 从 Artifactory 迁移的概念对照：[faq.md](../faq.md)
