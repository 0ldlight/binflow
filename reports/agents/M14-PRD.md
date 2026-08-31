# M14-PRD 工作日志 — UI-parity 里程碑立项（PRD v1.0 草案）

| 项 | 值 |
|---|---|
| 票据 | M14-PRD（PM 立项起草，2026-08-31） |
| 角色 | product-manager |
| 输入 | 用户指令 2026-08-30 三指令（M14 主轴定音）；UX-1 三交付；ROADMAP「M13 未纳入项」候选池；M13 PRD 体例；BOARD M13 收口节（只读） |
| 产出 | docs/prd/milestone-14.md（v1.0 草案待 conductor 审）+ ROADMAP 两处（M14 立项行 + 当前里程碑头切换——PM 职责内，沿 M13 v1.0 先例）；零 git 操作、未碰 BOARD.md |

## 输入核对（全部在盘读取）

PRODUCT.md / ROADMAP.md（含 M13 未纳入项段）/ docs/design/console-artifactory-parity.md（全文）/ brand/logo/README.md / brand/package-icons/README.md / docs/prd/milestone-13.md（体例母本）/ reports/agents/UX-1.md / BOARD.md（只读——M13 票据节 + 收口节 + 候选池汇总）。前端载体文件路径逐一 glob 核实（SetMeUpDialog/AppShell/RepositoryFormPage/PlaceholderPage 等全部在位；另见新发现）。

## PRD 结构（docs/prd/milestone-14.md）

§0 修订记录 ｜ §1 背景与目标（1.1 四股输入 / 1.2 量化门槛九行 / 1.3 依赖与分票——估 21~26 票、票号 T-381 起 / 1.4 工作方式六条款含 E1~E7 豁免常设 + 锚族冻结 + 低置信不进断言）｜ §2 范围（2.1 九行 In scope / 2.2 Non-goals 含候选池收编判定逐条留痕）｜ §3 场景 A~F ｜ §4 FR-123~FR-130 ｜ §5 兼容矩阵（LC-57~LC-66：A 7 / C 2 / 待裁 1；档位增量 0 行；回归基线零断言反转设计；L01~L18；K55~K61）｜ §6 NFR（P61~P63 + **A1~A3 a11y 专项新增** + S68~S70）｜ §7 Q1~Q7 带暂行 ｜ §8 验收剧本十段 ｜ §9 DoD 八条（含 parity 专项：矩阵逐格复评 + E1~E7 复核 + axe 双主题 + 四闸门）

## 关键 PM 判定（留痕）

1. **主轴结构**：批 1（D1 抽屉化/M1 单 Dialog/M3 创建 modal/L2 行内菜单）P0 ← parity §7 矩阵最大差距；批 2（Tokens 真身/L1/F2/N2）P1/P2；品牌两票（logo 六用例 P0、图标 30 枚 P1）。
2. **活体核验 = 前置锚 FR-123（P0）而非并行可选项**：低置信不进断言是 UX-1 宪章——批 1 细节断言（Tab 命名/宽度档/动作集/字段集）依赖 V1/V2/V4/V6 回写；设降级路径（Q1-③）防核验源不可得阻塞主形态（手势级断言均高/中高置信）。
3. **候选池收编**（§2.2 逐条理由）：入波 = docker remote 首航（K54 条件已满足 + area 零冲突）/ npm legacy login / helm PVC keep / 启动日志措辞 / v3-flat 补锚（后四者打包 FR-130）+ FE 类（hover 对比度、playwright/dind 注记）入 FR-127/128。滚 M15+ = Replay+outbox 行级 REST（运营增强非兼容面）/ D1 busy 重试预算（8 路门内零 5xx 已达承诺，24 路 0.27% 超门边角）/ 成员同型全包型推广（全型回归矩阵宜专程）。不立项 = disable 快照契约翻转（已钉死无推翻信号）。
4. **AQL 让位留痕**：M13 PRD 原列「AQL = M14 专程第一顺位」→ 用户指令主轴定音后顺延 **M15 专程候选第一顺位**（PRD §1.1-④/§2.2 + ROADMAP M14 段双留痕）。
5. **零断言反转设计**：M14 形态迁移不动语义——既有 spec 走「迁移更新」非「反转」，服务端 diff=0 复核沿 M8 先例；退役面（字符图标/◆/PlaceholderPage）grep=0 断言归豁免票。
6. **契约矩阵新口径**：LC 层级定义适配 UI-parity（A = 形态或 wire 对齐；品牌资产归 C 自有）；E1~E7 豁免族单列常设不入 LC 计数。

## 新发现（本次核对）

`web/src/pages/webhooks/SubscriptionDrawer.tsx` 已在盘（M13 T-366 交付）——**右抽屉形态已有站内先例**，D1 抽屉化（FR-124.1）可复用其组件模式；已作为分票提示补入 PRD §1.3（措辞含「M13 交付的 SubscriptionDrawer 先例」，见 PRD §1.3 area 错峰段）。

## 开放问题（Q1~Q7，均带暂行——待 conductor/用户）

Q1 活体核验源（t226 容器恢复 / 外部活体对照 / 仅凭标注降级）｜ Q2 logo 圈定窗（候选 1 工作稿起步，B2 前波未推翻即转正）+ wordmark 字体路线｜ Q3 决策项 A/B/C 形态终裁｜ Q4 Tokens 字段集（V6 后）｜ Q5 docker remote 排期确认（PM 建议入 M14 P1）｜ Q6 v3-flat 403-vs-409 终裁（补锚后）｜ Q7 D3 依赖树定案（暂行暂缓，V7 佐证后关闭）。

## 遗留

1. PRD 待 conductor 审定（Q1~Q7 终裁 + 21~26 票区间确认）→ tech-lead 拆票（建议 B1 波 = 活体核验票 + 批 1 两票 + FR-128 零依赖票）。
2. UX-1 三项遗留由 M14 消化：圈定窗（Q2）/ wordmark·npm·go 转 path（FR-126.3/127.2）/ V1~V8 未执行（FR-123）。
3. 图标低置信三枚（nuget/conan/docker 鲸腹）活体修正归 FR-123 顺带腿（PRD LC-63 注记），不阻塞接线。
4. GitHub Org 头像用例（logo README §4-⑥）远期 P2，非 DoD。
