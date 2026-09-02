# 逆向行为规格（docs/reverse/）

reverse-engineer 在此产出 **clean-room 行为规格**——让实现者不需要看反编译代码就能完成 Go 实现。

## 规则（ADR-0001）

- 原始材料：`reverse-src/`（gitignored，永不提交），agent 只读。
- 这里只放行为规格：端点表、存储布局、配置格式、语义流程；**不放代码翻译**。
- 有公开规范的协议（Docker Registry v2 / Maven 2 / npm / PyPI）以官方规范为准，反编译只补空白。
- 每份规格标注置信度：高 = 反编译代码 + 公开文档双证；中 = 仅代码；低 = 推测待验证。

## 预期规格清单（随里程碑产出）

| 文件 | 内容 | 里程碑 |
|---|---|---|
| `rest-api.md` | REST 端点表（路径/方法/参数/响应码/示例） | M1 |
| `storage-layout.md` | filestore 目录推导、checksum 命名、元数据序列化 | M1 |
| `config-formats.md` | artifactory.config.xml / binarystore.xml 要点 → BinFlow 配置映射 | M1 |
| `repo-semantics.md` | local/remote/virtual 语义、layout 解析、缓存规则 | M1–M3 |
| `auth-model.md` | 用户/组/权限模型、token 行为 | M1–M4 |
| `docker-registry.md` | Registry v2 端点行为细节（补官方规范空白处） | M2 |
| `maven-npm-pypi.md` | 各协议仓库交互细节（补官方规范空白处） | M3 |
| `import-export-api.md` | 导入/导出 REST API（系统/仓库级，含 marker 文件） | M6 |
| `replication.md` | 复制 push/pull/事件驱动、全局控制、联邦概念 | M6 |
| `auth-integration.md` | LDAP 配置模型、OAuth stub 状态、用户自动创建 | M6 |
| `s3-storage-layout.md` | S3/对象存储 binarystore 配置、MPU 参数、云存储重定向 | M6 |
| `metrics.md` | 内部指标框架、可观测性日志服务、Prometheus 集成现状 | M6 |
| `rbac-model.md` | 实例级/Projects 域两层授权模型、角色闭集、组 CRUD 与 effective admin | M7 |
| `console-ui.md` | 控制台 UI 行为规格：全局 IA、页面骨架、交互流、状态矩阵、OSS 缺位（活体 7.84.10 取证） | M8 |
| `gap-endpoints.md` | M9 服务端缺口群：users 回显字段级（无 enabled 布尔，status 枚举）、DELETE user 级联与守卫、组成员暴露面（includeUsers / UI 扇出 / Access v2 members）、permission 列表无过滤面、仓库用量走 /api/storageinfo 扇出 | M9 |
| `inv-1-core.md` | 全量功能盘点·分区1（batch1-core 核心服务面）：安全信任/存储/仓库模型/配置集群/搜索/元数据/生命周期治理/运维观测/集成面，约 120 条功能目录 + BinFlow 覆盖对照 | 全量盘点 |
| `inv-2-surface.md` | 全量功能盘点·分区2（REST + features + 描述符表面）：357 resource 类 ≈1,866 方法级操作清点、80 项 AddonType 许可门控、26 默认 layout、≈120 system.properties 开关 | 全量盘点 |
| `inv-3-protocols.md` | 全量功能盘点·分区3（协议与包型）：57 包型逐项、横切协议能力（矩阵参数/checksum 部署三头/路径归一化中枢等 22 项）、25 条内置 layout | 全量盘点 |
| `inv-4-addons.md` | 全量功能盘点·分区4（Addon/企业功能）：HA/Xray/Distribution/Build-info/Projects/复制/联邦/插件/事件/许可/DB/存储后端等 91 条 + 外部依赖标注 | 全量盘点 |
| `goproxy.md` | Go 包型（GOPROXY 协议）行为规格：端点表、`!lower` 转义布局、校验链、rclass 三态、真实客户端矩阵（FR-87 前置，T-278） | M10 |
| `npm.md` | npm 认证/会话端点族（`/-/` 家族）行为规格：K60 六条定案（login 修复面 / whoami / ping / `-/v1/login` ENYI 回落 / profile·tokens 404 姿态 / rev-dance 不变量）、login wire 语义、`.npmrc` 键匹配坑；「客户端源码即规范」+ live 抓包对拍。范围仅认证族——协议主面仍在 `maven-npm-pypi.md` §0/§2（FR-130.1 前置，T-393；**索引行 T-428 补齐——T-393 遗留 #2 登记**） | M14 |
| `aql.md` | AQL 与搜索域行为规格：语言子集（域/字段/操作符/尾缀链）、envelope 与错误文案逐字、资源治理 K63 校准、virtual 仓语义、老搜索 14 端点族 OSS 可用性矩阵、基座映射表（FR-132/133/134 前置锚，T-407；官方文档为唯一行为基准 + t226 活体核验） | M15 |
| `artifactory-full-feature-matrix.md` | **主矩阵（M10+ 路线图骨干）**：四分区去重合并的全量功能对照——213 条、十大高价值缺口、依赖外部产品项单列、待验证清单汇总 | 全量盘点 |
