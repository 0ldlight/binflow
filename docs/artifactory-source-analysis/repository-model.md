# 源码分析 · 仓库模型（证据指针文档——正文在既有产物）

> 指针层。行为规格正文在 `docs/reverse/` 各文件，本页只给导航。

## 1. 仓库语义

- `docs/reverse/repo-semantics.md`——local/remote/virtual 三态语义、layout 解析、缓存规则、virtual 解析顺序（BinFlow 对位 ADR-0013）。
- `docs/reverse/repo-operations.md`——仓库 CRUD 与仓库级操作端点族（计算/索引/重建等）。
- `docs/reverse/remote-browsing.md`——remote 仓远端浏览语义（`listRemoteFolderItems` 可选档默认 false、官方开放五型、13 包型上游枚举矩阵、上游故障降级）。

## 2. 配置模型

- `docs/reverse/config-formats.md`——artifactory.config.xml / binarystore.xml / artifactory.system.properties 要点。
- `docs/reverse/configuration-map.yaml`——system.yaml schema 域结构 + JF_* env 映射 + bootstrap 加载序 + secrets 解析链（E1+B+C 三源）。
- 仓库配置 JSON round-trip 四域——`docs/compatibility/contracts/` 相关行 + matrix D02。

## 3. 布局与包型

- `docs/reverse/inv-3-protocols.md`——25 条内置 layout + 57 包型逐项 + 26 默认 layout 注册面（inv-2）。
- `docs/reverse/maven-metadata-pom-prerequisite.md`——Maven metadata/POM 前置条件。

## 4. 仓库域对账

`docs/compatibility/matrix.yaml` D02（13 行：✅5/❌6/超集1/⛔1——federated rclass 配置面 ⛔ 归并）。

## 5. 缺口声明

Federation 仓型行为规格仅 enterprise 目录条目级（无端点 wire——license 门内，UNKNOWN 池 ENT 域）。
