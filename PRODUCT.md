# 产品愿景（PRODUCT）— BinFlow

> 本文件是整个团队的需求源头。product-manager 由此展开 PRD，architect 由此定架构，reverse-engineer 由此定逆向范围。

## 产品名称

BinFlow

## 一句话定位

用 Go 从零实现的云原生制品仓库（Artifact Repository Manager）：单二进制交付，兼容主流包生态协议，统一管理 Docker 镜像、Maven/npm/PyPI 构件与任意二进制制品。

## 背景与对标

- 对标 **JFrog Artifactory**：架构与概念模型与其保持一致（仓库模型、存储引擎、权限体系），让使用方可以平滑迁移、文档直接类推。
- **行为参考**：`reverse-src/` 下存放 Artifactory 反编译代码（Java，不入库）。
- **clean-room 原则**（详见 DECISIONS.md ADR-0001）：逆向只产出「行为规格」（接口表、存储布局、流程语义），禁止逐行翻译或复制反编译代码；有公开规范的能力（Docker Registry v2、Maven 2、npm、PyPI 协议）一律以官方规范为准，反编译只用来补文档没写的空白。
- **差异化**：Go 单二进制（无 JVM、内嵌数据库零依赖）、云原生部署矩阵、启动与内存开销比 JVM 低一个数量级。

## 目标用户与场景

- 平台工程 / DevOps 团队：内网统一制品源，CI/CD 依赖收口
- 离线与受限网络：air-gapped 集群的镜像与依赖分发
- 从 Artifactory 迁移的团队：兼容的仓库语义与 REST 行为，降低迁移成本

## 核心能力（按优先级）

1. **存储引擎**：checksum（sha256/sha1/md5）寻址的文件存储与去重，制品不可变，上传强制校验
2. **仓库模型（对齐 Artifactory）**：local / remote（代理缓存）/ virtual（聚合）
3. **协议适配**：Generic(raw)、Docker Registry API v2（含 OCI）、Maven 2、npm、PyPI；后续 Helm OCI、Go modules
4. **REST API**：兼容 Artifactory 常用端点子集 + 自有 `/api/v1`
5. **Web 控制台**：仓库管理、制品浏览/上传/下载、搜索、用户与权限
6. **治理**：用户/组/权限（仓库×路径）、API Token、审计日志、GC、配额、备份恢复
7. **多元部署**：单二进制 / Docker / docker-compose / Helm(K8s) / 原生 K8s 清单 / systemd / 离线安装包

## 架构对齐原则

BinFlow 的概念模型必须与 Artifactory 一一对应（落地见 DECISIONS.md ADR-0003）：

| Artifactory 概念 | BinFlow 对应 |
|---|---|
| Local / Remote / Virtual repository | 同名概念，语义一致 |
| Checksum-based filestore | checksum 寻址的 blob 存储（去重） |
| Metadata DB（Derby/Postgres） | 内嵌 SQLite（默认）/ Postgres（可选） |
| Access（用户/组/权限） | auth 模块（users/groups/permissions/tokens） |
| REST `/api/` | 兼容端点子集 + `/api/v1` |
| Web UI | 内嵌 Web 控制台（go:embed） |

## 明确不做

- **不做 Xray 式漏洞扫描 / 许可证合规平台**（2026-09-06 用户终裁：唯一维持排除项）

## 范围演进记录（原「明确不做」五条的 2026-09-06 终裁翻案）

原第一版五条排除项，除 Xray 外全数解禁进产品路线（用户裁定「除了 2 不做，剩下的都做」）：

1. **HA 集群与联邦复制** → 复制（push/pull）已于 M6/M15 实现；**Federation（镜像联邦）与 HA 本体进产品路线**（M18+ 候排程，M17 立项时 Q1 已裁 Federation 滚 M18 专程；HA 同轨）
2. Xray 式扫描 → **维持不做**（唯一排除）
3. LDAP / SAML / OIDC → **已实现**（M6 FR-54~56 OIDC+LDAP、M11 FR-92 SAML 前端化——本行为滞后补账，非新翻案）
4. UI 高级分析与洞察报表 → **进产品范围**（M17 承载，FR-154）
5. Artifactory 全量 REST 兼容 → **进产品范围**（分程分批交付——按域渐进扩展兼容面，自有 `/api/v1` 并行保留；排程归里程碑规划）

## 成功标准

- 真实客户端全链路可用：`docker push/pull`、`mvn deploy/resolve`、`npm publish/install`、`pip install`（走代理）、raw 上传下载
- 单二进制 < 40MB，冷启动 < 2s，空载内存 < 100MB
- 部署矩阵每种方式按文档 15 分钟内从零跑通
- 1000 并发拉取无错误；checksum 去重生效（相同 blob 只存一份）

## 技术约束

- Go（当前稳定版），模块化单体，单二进制，Web 控制台 go:embed 打入
- 元数据：内嵌 SQLite（默认，零依赖）/ Postgres（可选）
- 协议兼容优先于功能数量：每个协议适配器必须用真实客户端验收
- 配置：单 YAML + 环境变量覆盖（12-factor 友好）
