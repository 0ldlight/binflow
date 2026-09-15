# 源码分析 · 存储模型（证据指针文档——正文在既有产物）

> 指针层。行为规格正文导航。

## 1. 规格文件

| 文件 | 覆盖 |
|---|---|
| `docs/reverse/storage-layout.md` | filestore 目录推导、checksum 命名、元数据序列化（BinFlow 对位 ADR-0006——布局即兼容契约） |
| `docs/reverse/s3-storage-layout.md` | S3/对象存储 binarystore 配置、MPU 参数、云存储重定向（BinFlow 对位 ADR-0018/0039/0040） |
| `docs/reverse/storage/binary-provider-chain.md` | BinaryProvider 存储链 29 条行为（高置信 18；L000-A 收口——binary-store-* jar 容器内定位 + 288 类清单 + 24 模板矩阵，evidence/ 在同目录） |
| `docs/reverse/storage/prune-gc-admin.md` | prune/GC 管理面行为（Pruner optional facet + 共享删除纪律——ADR-0049 authority 锚） |
| `docs/reverse/storage/README.md` | 存储面子域导航 |

## 2. 对账与契约

- `docs/compatibility/matrix.yaml` D01（制品与存储 43 行：✅22/◐5/❌10/超集2/⛔4）。
- `docs/compatibility/contracts/storage-admin.yaml`（10 条目，E5-E10）——含冻结行集外新行提案 D01-R38..R43。
- 存储链兼容终案：ADR-0049（KEEP 16 行 / INTENTIONAL 5+1 / REFACTOR 两小面）。

## 3. 残余未知

UNKNOWN 池 STG 域 11 条（GC 资格窗口/云链运行时/HA 协议等——U-STG-12~19 族）。

## 4. 缺口声明

Azure/GCS 云后端运行时行为无活体（binarystore 模板文档单源 E5）；HA 双活存储协议零实证（单节点参照不可达）。
