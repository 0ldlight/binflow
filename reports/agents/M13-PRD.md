# M13-PRD 工作日志 — M13 立项 PRD 起草（product-manager）

- 角色：product-manager；日期：2026-08-30；状态：done
- 产出：`docs/prd/milestone-13.md`（PRD v1.0 草案，待 conductor 审）+ `ROADMAP.md` 两处（当前里程碑头 M12→M13 切换 + M13 立项行插入）
- 纪律：共享工作树零 git 操作（conductor PR 收口）；只动上述两文件 + 本日志；BOARD.md 只读未改

## 1. 输入通读（出处可溯）

| 输入 | 关键收获 |
|---|---|
| ROADMAP「M12 未纳入项」（刚建，`346485e`）+「M11 未纳入项」M12+ 主轴候选行 | 范围基线全量：票级遗留 7 项聚类 / P2 登记 4 项 / 运维尾巴 2 项 / 主轴候选 8 选 1 |
| docs/prd/milestone-12.md | 体例基准（FR 接续编号/LC 矩阵/§2.2 滚程/§5.5 L 清单/DoD 八条）；FR-110.4/AC3 旧措辞仍在（§1.2 已修）——flat/L31/D-10 三项 M13 承载依据 |
| reports/agents/T-356.md（M12 终验） | ⚠️ 4 维持登记（L03 D-10 / L12 flat / L14 folderDownload / L17 retention）= M13 FR-118/FR-120 直取；观察③ flake 新成员、观察⑨ fail-open AC2 加注；DoD#1 条件票留痕（symbol server 未触发/D-3 评估建议 M13）；§6 T-355 未执行教训 → M13 UAT 必做 |
| reports/agents/T-348.md | conan D8「latest 链→坐标根整树删」双证新取证（反编译 + 参考实现）——FR-119.1 直取；§4 D-F2 仍开放归 FR-110.1 邻域 |
| reports/agents/T-340.md §4 | D-F2 双拼布局细节（channelFileName 复数/单数 trim 错位；roundtrip 对称不破、settings 短路）——FR-119.2 范围与 AC 依据 |
| docs/reverse/artifactory-full-feature-matrix.md | 缺口 6 Webhook（36 事件 + inv-4「可整体平移」）；§待验证「Webhook addon 反编译集合缺失 → 官方文档为唯一来源」；缺口 2 AQL / 缺口 9 Build-info（滚程依据） |
| docs/reverse/inv-4-addons.md | §I I2~I5（管理在 Access 的平移判定 / outbound dispatcher / 事件注册 REST）+ §K3 outbox 模式 + L186 官方文档基准声明——webhook.md 前置规格票的取证路径 |
| docs/reverse/helm.md | chartsBaseUrl 回源链（§5/S8/S10 高置信）、`_external`/`_transitive` 端点表（local 400）、D-5——FR-117 行为面 |
| docs/reverse/repo-operations.md §2.1 | folderDownloadConfig 六字段双证——FR-118.1 |
| docs/reverse/nuget.md §5.1 | 臂② 409 vs as-built 201（D-10）——LC-56 待裁行 + Q3 |
| reports/iteration-942.md | m12-done 程序：笔头批 `346485e`（五处漂移 + PRD 两处文面修正 + M12 未纳入项段）——背景段收官事实与 FR-120.3「AC5 主措辞已对齐、剩 AC2 加注」的界定依据 |
| docs/design/console-m8.md | 树浏览器「Trash Can 常驻节点不建（BinFlow 无回收站）」条款（被 M12 推翻）——FR-122.1 先改册后实现 |
| internal/adapter/docker 代码面 | remote 仓在 /v2 面只读无代理（`ErrRepoTypeNotSupported`）——FR-116「翻转点」定性依据（D-5 首航 docker 面 remote 数据链） |

## 2. PM 裁量记录（可推翻）

1. **主轴选题 = Webhook 统一事件总线**。理由：① inv-4 判定「可整体平移、无需模拟 Access 拆分」——候选中实现风险最优；② CI 集成刚需且有 T-247 dogfood Jenkins 真实消费者可验收（产品「客户端真实可用」准绳）；③ 消费 M12 生命周期域底座（统一删除 seam/操作族/属性），事件织入为 outbox 旁路、主路径零变化。**落选**：AQL+老搜索（搜索基建专程，体量与 HA 同级——M14 第一顺位）；Build-info（CI「入向」域，先出后入，M14+）；HA（Q1 前置 PRODUCT.md 修订解禁未发生，不排任何工作项）。
2. **票级遗留全收编为 FR-116~FR-119**；deb bz2 维持 Q7 推翻通道不立项（LC-45 D 留痕沿 M12）；npm 尾斜杠并入 FR-122 docs 票。
3. **断言反转预算**：两处（conan D8 / folderDownload 开关化）+ 一处服务端布局对齐（D-F2）——全部 PRD §5.4 回写在案，QA 归属审计有锚。
4. **UAT 首跑定为 M13 必做**（M12 T-355 未执行教训，T-356 §6 留痕直取）。

## 3. 产物结构

- PRD：§0 修订记录 / §1 背景·量化门槛·分票提示（估 21~26 票）·工作方式条款五条（含 webhook 官方文档取证特例）/ §2 范围 + Non-goals 滚程表（HA 前置显式注记）/ §3 场景六则 / §4 FR-114~FR-122（九条，全部 AC 可执行）/ §5 兼容矩阵（LC-46~LC-56 = A 10 / 待裁 1；档位增量 webhook 第 19 槽 Q4；§5.5 L01~L24）/ §6 NFR（P58~P60、S65~S67）/ §7 Q1~Q7 / §8 剧本 / §9 DoD 八条。
- ROADMAP：当前里程碑头切换 + M13 立项行（14 条目，含「M12 未纳入项」对账与 M14+ 第一顺位预登记）。

## 4. 开放问题（需用户/conductor 终裁，PM 未代拍）

Q1 HA 前置（PRODUCT.md 修订解禁）/ Q2 symbol server 余量 / Q3 D-10 终裁 / Q4 webhook 槽档位 / Q5 docker remote 顺车 / Q6 无本体域事件处置 / Q7 deb bz2 维持。

## 5. 自查

- FR 编号接续 M12（FR-114 起）；LC 接续（LC-46 起）；NFR-P/S 接续（P58/S65 起）；L 序列每里程碑重启（M12 同例）。
- 只写 docs/prd/milestone-13.md、ROADMAP.md、本日志三个文件；未触 BOARD.md（trash 树常驻节点等 FE 面留待 tech-lead 拆票后入板）。
