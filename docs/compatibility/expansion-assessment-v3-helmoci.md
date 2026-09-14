# 契约扩面评估 v3——helmoci（第六域候选，案后段首项复核，LOOP 020 / L020-2，2026-09-13）

- 背景：v2 评估将 helmoci 列后段首项（/v2 wire 已被 docker-remote 25 条目背书——helmoci 经 docker
  /v2 plane 单码径共享，L008-2 测试同跑）。本页按三轴复核其**增量面=index.yaml 经典 chart 仓**，一页可裁。
- 已开五域 82 条目：docker-remote 25 / conan 16 / npm 17 / pypi 10 / goproxy 4 / storage-admin 10。

## 三轴评估（增量面=经典 chart 仓：index.yaml + tgz + provenance + Helm CLI 腿）

| 轴 | 评估 | 依据 |
|---|---|---|
| ① 规格厚薄 | **厚**（helm.md 335 行——七候选中行数最多）：官方三锚（Chart Repository Guide〔index.yaml 字段义/prov 并置/无效 index 拒绝〕+ Use OCI-based registries + JFrog Helm 官方文档）+ DE 补白逐条标注（index.yaml 读写细节、**URL 改写算法**、虚仓缓存键、事件链、reindex 语义、HelmOCI↔docker v2 精确复用面） | docs/reverse/helm.md（T-292） |
| ② 差分密度预估 | **中**：经典面核心=index.yaml 计算物（生成/合并/重算——结构类比 maven-metadata 但 YAML+URL 改写）、chart tgz 上传、.prov 并置与 provenance 语义、helm repo add 对无效 index 的拒绝面；深水=virtual index 计算/relative URLs/外部依赖改写（JFrog 特有改写算法）。**真客户端腿=helm CLI**（repo add/update/show/pull/provenance verify——本机可装，与 mvn/npm/conan 腿同模式） | helm.md §1-§5 + D12-R09（helm reindex ×2 已 ✅） |
| ③ 实现缺口 | **零实现缺口**：helm adapter（经典+OCI 双面）+ reindex ×2 在册（D12-R09 ✅）；缺口纯契约/差分——经典面 0 条目（OCI 面已被 docker 域间接背书，增量仅需 classic 3-5 条目） | internal/adapter/helm + matrix D12-R09 |

## 与 v2 结论的一致性与新信息

- v2 判「增量价值最低」的依据是 /v2 面——本页聚焦后维持，但**经典面本身是未被任何域覆盖的独立 wire**
  （index.yaml 是计算物 + URL 改写涉「服务端自知对外 base」类问题——conan upload_urls 同款议题，
  goproxy/proxy 类似面已在五域中反复出现，属高复现价值模式）。
- 首票切片建议（四段闭环照旧）：probe 双端拍 `helm repo add` + index.yaml GET 形态（URL 改写实形）
  → 契约 3-5 条目（index.yaml GET 形态/reindex 触发与异步语义〔规格已载〕/chart 上传与 .prov 并置/
  无效 index 拒绝面）→ 差分（curl + helm CLI 双腿）→ matrix 新行提案（helm 经典面现无独立行——D12-R09
  仅 reindex 行；走新行提案）。
- 排程建议：**LOOP 021+ 可开**（conan 域 D2/D3/D4 已清、auth401-wording 系 LOOP 021 裁定票非差分重票
  ——helm 首票与其无资源冲突）；预估 2-3 LOOP 达到五域同等覆盖密度（经典面窄于 conan）。

## 结论

**推荐 helmoci 为第六域（案后段首项确认）**——一句话：经典 index.yaml 面是五域之外唯一未背书的
计算物 wire（URL 改写+provenance+reindex 语义厚规格在册、零实现缺口、helm CLI 腿现成），OCI 面增量
近零故总量小（预估 3-5 条目即闭环）。
