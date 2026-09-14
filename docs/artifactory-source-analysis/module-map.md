# 源码分析 · 模块地图（证据指针文档——正文在既有产物）

> 指针层。正文：`docs/reverse/artifactory-module-catalog.yaml`（569 模块机读目录）+ `docs/reverse/domain-map.yaml`（26 域映射）+ `docs/reverse/dependency-map.yaml`（依赖图）。全部 E1 静态观察，派生命令在文件头注可复算。

## 1. 模块分类速览（catalog summary）

| 组 | 数量 | 说明 |
|---|---|---|
| access | 14 | Access 服务（用户/组/权限/SSO 的服务端） |
| addon | 65 | 商业 addon（artifactId 后缀即官方 addon 域：alpine/docker/replication/…） |
| artifactory-core | 22 | 单体内核（repo/security/schedule/engine 包根） |
| jfrog-commons | 37 | 共享库（common/storage/eventing…） |
| ms-client-* | 6 | 微服务客户端（federation/jfconnect/metadata 各 2） |
| service-other | 425 | 其余服务模块 |

## 2. 能力域 → 模块（domain-map 26 域）

147 个 org 名模块全量对账（不重不漏：module_index 给唯一 primary 归属）+ 13 横切模块 + 6 unknown 项。代表域：制品协议域（65 addon 包型注册表）、存储域、仓库域、安全域、调度域、HA/复制域、Build-info/Release 域、前端域（frontend-server SSR 网关 + artifactory-ui 宿主）。
逐域 supporting 列表见 `domain-map.yaml` domains 节——本文件不复制。

## 3. 依赖拓扑（dependency-map）

- 组内编译期依赖边（pom `<dependencies>` 声明级，138 模块可抽取；test 边保留标注）。
- 已知局限：8 个 recovered 占位 pom（access-server-* 6 + access-application + onemodelsdk）组内边不可抽取；BOM/optional 未展开。

## 4. OSS vs 商业面拆分

`docs/reverse/oss-structure.md`——OSS 发行面与 addon 边界；`docs/reverse/enterprise/feature-gates.yaml`——80 项 AddonType 许可门控清单。

## 5. 结构认知的下游消费方式

按 ADR-0001：模块结构**不得**作 BinFlow 结构设计输入；能力域语义经行为规格（docs/reverse/*.md）进入，BinFlow 按能力域组织（见 `docs/binflow-analysis/architecture.md` §2）。

## 6. 缺口声明

domain-map unknown_items 6 项（原 1 项 BinaryProvider 链已收口转 storage-model.md 注）；Go 伪码部分无模块级目录（catalog 仅覆盖 Maven 树）。
