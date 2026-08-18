# 路线图（ROADMAP）

> 由 product-manager 维护；tech-lead 据此把当前里程碑分解为 ticket。

## 当前里程碑：M2

### M0 — 团队启动（已完成）
- [x] 产品愿景 PRODUCT.md（BinFlow）
- [x] 团队工作流与看板建立

### M1 — 内核基座（已完成，`m1-done`；QA 全绿见 T-18/T-19）
目标：存储引擎 + 仓库模型 + Generic 本地仓库的最小闭环，Go 脚手架与工程化就绪。
- [x] 逆向规格：REST 表面 / 存储布局 / 配置格式 / 仓库语义（reverse-engineer → docs/reverse/）
- [x] ADR 与架构设计：模块划分、存储设计、适配器接口、部署架构（architect）
- [x] 脚手架：go module、cmd/internal 布局、Makefile、lint/test/CI（devops-engineer）
- [x] 存储引擎：checksum 寻址、去重、上传会话、原子落盘（dev-go-storage）
- [x] 元数据与仓库模型：repo 配置、node 元数据、SQLite 嵌入（dev-go-core）
- [x] Generic 本地仓库：上传/下载/删除/校验（dev-registry-adapter）
- [x] 基础认证与权限骨架：admin 用户、API Token、路径 ACL（dev-go-core）
- [x] 开发环境：docker-compose 起本地实例（devops-engineer）
- [x] QA：generic roundtrip + 存储完整性；文档：README 快速开始

### M2 — 云原生旗舰：Docker Registry v2（当前）
需求基线：docs/prd/milestone-2.md（PRD v1.0；`/v2` 路由方案待 ADR 定案，见 PRD §7 Q1）
- [ ] 逆向规格：docker-registry.md（补 spec 外空白，reverse-engineer）
- [ ] ADR-0010：`/v2` 挂载形态（根级例外 vs `/binflow/v2`+反代 rewrite，architect，依赖 PRD Q1 用户定案）
- [ ] blob upload 协议（POST/PATCH/PUT，monolithic + chunked）
- [ ] manifest schema2 / OCI 存取（by-digest / by-tag）
- [ ] `/v2/_catalog`、tags/list；docker login 的 token 认证流
- [ ] Helm OCI 承载（oras 客户端可用）
- [ ] conformance：docker / podman / crane / skopeo / oras 全过（分级见 PRD §5.3）
- [ ] M1 遗留收编：O1 断开日志定界、O3 gc 旗标、Content-Type 映射（PRD §6.4）
- [ ] 部署烟测：Docker 镜像 + compose（release-engineer）

### M3 — 多生态与代理
- [ ] Maven 2：layout 解析、deploy/resolve、maven-metadata.xml、checksum 策略
- [ ] npm：publish / tarball / metadata；PyPI：simple index / upload
- [ ] remote 仓库代理缓存（pull-through，含 SSRF 防护）
- [ ] virtual 仓库聚合与解析顺序

### M4 — 控制台与治理
- [ ] Web 控制台：登录、仓库管理、制品树浏览、上传、搜索
- [ ] 权限模型完整实现（users/groups × repo × path）+ UI
- [ ] 审计日志、GC、配额
- [ ] 备份/恢复（export/import）

### M5 — 发布矩阵与文档中心（GA）
- [ ] goreleaser 多平台二进制（linux/darwin/windows × amd64/arm64）+ 校验和
- [ ] Docker multi-arch 镜像（distroless / alpine 双变体）
- [ ] docker-compose 产物、Helm Chart（persistence/ingress/HPA）、原生 K8s 清单、systemd + 安装脚本、离线安装包
- [ ] 帮助文档中心：每种部署方式的安装指南、每协议客户端接入指南、管理指南、API 参考、FAQ
- [ ] 安全审计（security-auditor）+ 性能基准

### M6+ — 展望
S3 存储后端、复制/联邦、OIDC/LDAP、Prometheus 指标、`bf` CLI、Artifactory 迁移工具。

## 里程碑完成定义（DoD）

每个里程碑视为完成，当且仅当：
1. 该里程碑所有 ticket 处于 done（通过 review + qa）
2. qa-engineer 的兼容/验收报告全绿（含真实客户端证据）
3. release-engineer 对已交付的部署方式完成烟测（M2 起）
4. tech-writer 已产出该里程碑新增能力的用户文档
5. 主会话完成 git tag（`m<N>-done`）；对外发布任何制品先经用户确认
