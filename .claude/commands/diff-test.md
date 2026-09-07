---
description: 触发差分测试批次（Artifactory 参照 × BinFlow UAT 双系统对照）
---

执行一轮差分测试（$ARGUMENTS 可指定域，如 `storage` / `docker` / 全量省略=核心集）：

1. 前置检查：Artifactory 参照实例与 BinFlow UAT 双可达（healthz/version 探针）——参照不可达则降级金样单边模式（显式标注 mode=golden-only，confidence≤medium）。
2. 派 `differential-qa-engineer`（后台）：
   - 输入：`docs/compatibility/contracts/<域>/` 契约集 + `docs/compatibility/fixtures/` + normalize 规则。
   - 执行：same request → 双发 → normalize → diff（status/headers 白名单/body/artifact sha256/metadata）。
   - 真实客户端优先（curl/docker/mvn/npm/pip/twine/go/helm/oras/crane/skopeo/nuget/cargo/conan）；自研 client 仅补 L0/L1 密集断言。
   - 产出：`reports/compatibility/<date>-<domain>.yaml`（逐 case 一致/差异+四分类建议）。
3. 收编：差异新发现 → 归 compatibility-engineer 裁定分类；矩阵翻态建议 + Score 变动入迭代报告。
