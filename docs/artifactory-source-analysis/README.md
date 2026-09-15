# Artifactory 反编译源码分析（证据指针文档——正文在既有产物）

> 新宪章 Discovery 归档的**源码侧**入口：7.161.24 全量反编译的知识地图。只索引既有 EVIDENCE INDEX 与行为规格，不复制、不重扫、不引用源码路径进实现。
> clean-room 铁律（ADR-0001）：`reverse-src/` 只读；本目录全部指针指向 `docs/reverse/` 的规格与证据索引。

## 1. 反编译基线

- 版本 **7.161.24**（build 7.161.14；access 7.191.x、jfrog-commons 13.21.0、metadata 7.490.1）。
- 规模：**569 Maven 模块 / 14,702 Java 文件**（+ Go 伪码 + 前端 source-map 还原——frontend 侧见 runtime-analysis README §5 与 `docs/reverse/frontend/`）。
- 源树：`reverse-src/artifactory`（gitignored；evidence-index 的派生命令记录在各 yaml 头注，可复算）。

## 2. 机器可读证据索引（E1，五件套）

| 文件 | 规模 | 回答的问题 |
|---|---|---|
| `docs/reverse/artifactory-module-catalog.yaml` | 569 模块（access 14 / addon 65 / core 22 / commons 37 / service 425…） | 有哪些模块、什么形态 |
| `docs/reverse/domain-map.yaml` | 26 能力域 → 模块映射（147 org 模块全量对账不重不漏） | 哪些模块支撑哪个产品能力域 |
| `docs/reverse/dependency-map.yaml` | 组内编译期依赖边（pom 声明，E1） | 哪个模块依赖哪个模块 |
| `docs/reverse/api-inventory.yaml` | 357 resource 类 ≈ 1,866 方法级操作 | 对外表面有哪些端点 |
| `docs/reverse/configuration-map.yaml` | system.yaml schema + JF_* env + bootstrap 加载序 + secrets 链 | 配置体系怎么加载 |

## 3. 各面指针页

| 面 | 文件 |
|---|---|
| 模块地图 | `module-map.md` |
| API 表面 | `api-analysis.md` |
| 仓库模型 | `repository-model.md` |
| 安全模型 | `security-model.md` |
| 存储模型 | `storage-model.md` |
| 企业/Addon 面 | `enterprise.md` |

## 4. 行为规格总集

`docs/reverse/README.md`——预期清单表（60+ 文件）：四分区全量盘点（inv-1~4）、全量功能矩阵（213 条）、协议规格（docker/maven/npm/pypi/goproxy/conan/debian/rpm/helm/nuget/cargo/aql）、REST 兼容矩阵（历史冻结快照 195 行）、auth/rbac/logging/enterprise/storage/distribution/jobs/protocols/frontend 分目录。

## 5. 证据等级词汇（全库统一）

E1 静态反编译 ｜ E2 静态 + 运行时资产 ｜ E4 运行时行为实证（探针/走查）｜ E5 安装包/官方文档单源。置信度：高 = 反编译 + 公开文档双证；中 = 仅反编译；低 = 推断待验证。

## 6. 缺口声明（真无证据的面）

- BinaryProvider 存储链实现类：反编译树内无实现（外部 binary-store-* jar）——已由 L000-A 容器内定位收口（U-STG-01 resolution），残余 GC 资格窗口/云链运行时/HA 协议在 UNKNOWN 池。
- recovered 占位 pom 的组内依赖边不可抽取（dependency-map 已知局限 8 模块）。
- 前端仅 source-map 还原子集（661 文件），非全量源。
