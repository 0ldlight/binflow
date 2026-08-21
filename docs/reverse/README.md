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