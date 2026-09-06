# M17-kickoff — 产品域扩张专程立项日志（product-manager）

- **日期**：2026-09-06（m16-done 收口窗同日——2026-09-06 13:0x conductor 裁定后，auto-next-milestone 触发）
- **角色**：product-manager
- **产出**：`docs/prd/milestone-17.md`（PRD v1.0 立项骨架稿）+ ROADMAP.md 四处修订（§4 变更清单）
- **git**：零操作（工作树直改，conductor 统一提交）；零票号新散布（新事项一律 FR/LC/L/K/Q 编号）

---

## 1. 范围裁定依据（两层结构——本立项的核心裁定）

**输入**：ROADMAP「M16 未纳入项」备稿（T-460 起，含启用三行增补候选）+ M17 预立项段衔接 + PRODUCT.md（五条仍未修订）+ BOARD.md m16-done 总账（挂账三项 + 滚程五项：BE round-trip 缺口 / B-3.2 / NuGet symbol 七承 / webhook 触发源 / build-info dormant→wired）+ 用户方向线索（Q6 两程结构终裁「产品域全部进」；intake ⑤ Xray 仍不做）。

**裁定逻辑**：

1. **主轴候裁层（FR-152~155）**：Q6 终裁已把四主目（Build-info / Release Bundle / 洞察报表 / Federation·Lifecycles）裁「进」M17——**这是用户指令，范围不发明**；但 PRODUCT.md「明确不做」五条原样未修订（T-460 衔接核对在案），**立项不翻案产品边界**——故四主目全部走「候裁」态：候 Q0（PRODUCT.md 修订稿终裁——PM 拟稿不落笔）+ Q1（定容与分期）后才拆票。ADR-0045/0046 占位前置。
2. **确定层（FR-156~161）**：m16-done 总账滚程五项 + 「M16 未纳入项」启用段（parity 残留 / 契约漂移 / cron·i18n 遗留 / 工程票外系残余）+ M15 续滚工程债八项——**零产品边界翻案面**，审定后即可先行拆票。「连续两程滚程即升级为债」纪律：本轮收纳项不再无界续滚（做不完走「M17 未纳入项」显式登记 + 归属审计）。
3. **维持不做**：Xray 集成面（intake ⑤ 唯一排除）+ license 识别 xray_tied 族（连带 `/api/search/license` 缺位登记——Q9）+ A0 红线族。
4. **定容张力（Q1 必答的理由）**：四主目全进 = 40~48 票，超 30~40 票带宽；PM 建议态 = Build-info 主程（P0）+ Release Bundle 最小面（P1）+ 洞察报表（P1），Federation/Lifecycles 滚 M18 单列专程 → 32~40 票。依「一个域被真实客户端走通 > 四个域只有 happy path」准绳。

## 2. FR 草案表（FR-151~161）

| FR | 域 | 层 | 优先级 | 票估 | 来源（可溯源，零发明） |
|---|---|---|---|---|---|
| FR-151 | 前置锚：PRODUCT.md 修订稿 + 规格票两份（build-info.md/release-bundle.md）+ ADR-0045/0046 + 活体基线（Q10） | 前置 | P0 | ~3 | Q0/Q1 必答前置 + ADR 群门槛（M16 PRD §0.3） |
| FR-152 | Build-info 域主程：模型 + REST 全链 + docker promote + webhook wired + AQL build 三域 + Builds 页/Module ID | 主轴候裁 | P0 | 8~10 | Q6 主目① + 滚程「dormant→wired」+ A1 联动面 |
| FR-153 | Release Bundle 最小面 + Any Distribution 预置解锁 | 主轴候裁 | P1 | 4~5 | Q6 主目② + M16 B-2.16 缺位登记；深度候 Q2 |
| FR-154 | 洞察报表：聚合快照层 + 图表族（dep M16 统计单源） | 主轴候裁 | P1 | 3~4 | Q6 主目③ + ADR-0044 K69 单源契约 |
| FR-155 | Federation/Lifecycles/Repository Path Map | 主轴候裁 | 候裁 | 0 或 5~8 | Q6 主目④——**PM 倾向滚 M18 单列专程**（Q1/Q8） |
| FR-156 | BE 表单域承接与权限域收口（configJSON 四域 round-trip + Stage + K73 + 通配桶 + B-1.7 评审腿 + B-3.2 + Last Login 列） | 确定 | P1 | 3~4 | 滚程五项①② + M16 未纳入项 B-1.5/B-2.16/B-1.7/B-3.2/T-468 |
| FR-157 | 契约漂移勘误族（url 前缀 / downloadUri / System Logs 端点 / ?properties / useAsync 双计） | 确定 | P1 | 1~2 | M16 未纳入项登记族七条 |
| FR-158 | cron/i18n 遗留（gc-cron-gap 三载体 + TTL wire + 切换器位置 + faq） | 确定 | P2 | 1~2 | M16 未纳入项 cron/i18n 段 |
| FR-159 | webhook 域二程 + Replay/outbox 行级 REST 并轨（dormant→wired + 裁剪出口候 Q5 + 死信重放） | 确定 | P1 | 2~3 | 滚程五项④⑤（M16 Q11 承接）+ M13 登记 |
| FR-160 | 工程债与续滚收尾包（M15 八项 + Annotate 别名评估 + QRL 三遗留 + X-Explode staging + 文档四条核对等） | 确定 | P2 | 3~4 | M15 未纳入项续滚 + T-444/T-452/T-476/T-477 登记 |
| FR-161 | virtual 成员同型全包型推广（13 包型 × 三 rclass） | 确定（弹性位） | P2 候裁 | 1~2 | M16 容量注滚程项（候 Q6） |
| — | QA 2 / tech-writer 1~2 / release 1 / PM 1 | — | — | 5~6 | — |

**票量级**：PM 定容建议态（FR-155 裁出 + HA/全量 REST 不做）**32~40 票**；四域全进则 40~48 超带（Q1 出口②须拆程或裁确定层尾部）。契约矩阵 LC-99~LC-114 估 16 条（A 12 / C 2 / 候裁 2）；L49~L63 验收线骨架；K74~K76 候校准。

## 3. 用户裁定清单（Q0~Q12 摘要——全文见 PRD §7）

| # | 事项 | PM 倾向 | 裁点 |
|---|---|---|---|
| Q0 | PRODUCT.md 五条修订稿（HA·联邦拆行 / Xray 维持 / LDAP·SAML·OIDC 行补账 / 洞察解禁 / 全量 REST 维持） | 按建议稿 | **立项窗必答** |
| Q1 | 四主目定容分期（前三域进 M17 / Federation·Lifecycles 滚 M18） | 裁减分期 | **立项窗必答** |
| Q2 | Release Bundle 深度（最小面 vs Distribution 服务对接全量面） | 最小面 | FR-153 断言冻结前 |
| Q3 | HA 本体 | 滚 M18+ 单列专程 | 随 Q1 |
| Q4 | 全量 REST 承诺退让边界 | 维持高频子集 | 随 Q0 |
| Q5 | webhook 触发源裁剪（M16 Q11 承接——build 域 wired 后裁点已至） | 裁剪（仅注册有源域） | FR-159 拆票前 |
| Q6 | 同型推广排期 | M17 承载（连续两程滚程即升级为债） | Q1 定容后 |
| Q7 | Go/Terraform/GitLFS/AI-ML 包型域 | 滚 M18+ | 随 Q1 |
| Q8 | Cleanup-Retention 策略引擎/冷存储 | 随 Q1 联动（M18） | 随 Q1 |
| Q9 | `/api/search/license` 腿（xray_tied 无数据源） | 缺位登记不做 | FR-152 断言冻结前 |
| Q10 | 7.161 参照容器双损坏修复 | 修复（归 conductor 裁量） | FR-151 规格票启动前 |
| Q11 | B-1.7 双布尔 vs 三值枚举（ADR-0026 增补评审） | 维持现行 + tripwire 触发器 | FR-156 拆票时 |
| Q12 | NuGet symbol 七承 | 维持条件池（用户点名即翻） | 登记确认项 |

## 4. ROADMAP 变更清单（四处）

1. **当前里程碑头**切 M17（M16 m16-done 状态保留可见，沿 M15→M16 先例）。
2. **M16 章**标题补 `m16-done` 2026-09-06 标记（35/35 + T-466 PASS with notes）。
3. **M17 预立项段转正**为正式 M17 章（预立项五要素全量吸收进来源链：主目→FR-152~155 / 随域联动→各 FR 腿 / 追加候补→Q3/Q4 / 维持不做→§2.2 / 容量注→FR-159/161 + Q6/Q7）。
4. **「M16 未纳入项」启用笔三行增补**：状态行备稿→已启用（as-built 勾稽 35/35 + B-3.2 未插空归 M17 FR-156 + 挂账三项处置）+ M17 衔接行更新（M17 已立项、双源对账完成、PRODUCT.md 门槛仍在 Q0）。

## 5. 遗留与移交（非 PM 裁量）

- **Q10 7.161 参照容器修复**：conductor 决策项移入 M17（活体参照基线依赖——未修复则规格票降级 t226 单源 + 官方文档留痕）。
- **m16-done 挂账三项**（非 PM 范围，M17 章已注记）：CircleCI 平台事故期 re-run 补证（T-465 §5 清单）/ DNS+SG 两用户前置（TLS 443 就绪）/ M17 预立项窗开（**随本次立项兑现**）。
- **t381 事故残留 / CircleCI 两跟进票**：维持 conductor 登记态（M16 未纳入项在册，未重复立项）。

## 6. 状态

立项骨架稿完成，**停下等收编**（conductor 审定窗 + Q0/Q1 组织终裁）。主轴候裁层在 Q0/Q1 终裁前不拆票；确定层可先行。
