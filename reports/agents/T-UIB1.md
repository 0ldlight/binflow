# T-UIB1 — UI Phase 1 批 1：token 收敛（styles/tw → src/design-system）

```
Ticket:       T-UIB1 UI Phase 1 批 1 token 收敛（P0；design-system-plan §6 批 1 行）
Role:         dev-frontend（area = web/src/design-system token 层 + 接线点）
Area:         web/src/design-system/（新建）；接线：main.tsx / foundation.ts / components.json / assert-tokens.mjs
Input:        docs/design/design-system-plan.md §3（六族规格）/§6 批 1 行/§7 验证门/§9 技术债；
              现状文件 styles/tw/{tokens,tailwind}.css（142+107 行）与 styles/tokens.css（147 行，G2 双层）
Changes:
  1. token 六族分文件迁入 web/src/design-system/tokens/：color.css（族 1 亮暗双谱 + pkgicon 暗档）、
     typography.css（族 2 九级字阶+行高+字重+font-feature+字体栈）、spacing.css（族 3 sp-1~8 + ctl 三档 + 布局尺寸）、
     radius.css（族 4 五档）、shadow.css（族 5 三档 + ring-width）、motion.css（族 6 z 四层 + dur/ease + reduced-motion 降级）；
     index.css 六文件 @import 聚合（唯一入口）
  2. @theme inline 桥接层迁入 web/src/design-system/tailwind.css——@theme 块逐条原样（值零改动，
     shadcn 原语消费面零改动）；import 改 './tokens/index.css'
  3. fonts.css 骨架（只建文件不引字体——Inter 是批 3；无任何 import 接线）
  4. G2 清偿：删 styles/tokens.css + styles/tw/ 整目录（旧 tw 层为旧层超集[多 --bf-font-sans]，
     删除零变量丢失）；grep 全仓零断链（见 Tests 门 5）
  5. 旧字号名 fs-aux/body/form/h3/h2/h1 在 typography.css 尾部别名到同值新阶
     （--bf-fs-aux: var(--bf-fs-xs) 等，12/13/14/16/20/24 逐档相等——消费面零改动）
  6. 新槽位只定义不消费：--bf-ring（色，=accent 值同源）、状态 {success,warning,info}-fg 补齐、
     *-surface 软底四枚、--bf-sp-8/--bf-ctl-{sm,md,lg}/--bf-sidebar-w 240px/--bf-topbar-h 64px/
     --bf-content-max 1440px、--bf-r-xs/--bf-r-full、--bf-ring-width 2px、--bf-dur-{fast,base,slow}/
     --bf-ease + prefers-reduced-motion 降为 1ms（有消费面前惰性）——桥接层零新增映射
  7. assert-tokens.mjs 豁免谓词同步：styles/tw/ 前缀 + 任意层级 tokens.css 文件名豁免 →
     design-system/tokens/ 前缀（7 文件豁免计数验证）；桥接层零字面量故不豁免
  8. shadcn CLI alias 同步：components.json css 路径 → src/design-system/tailwind.css（§7.3 引用门）
  9. 注释清偿（路径引用漂移）：vite.config / theme-provider / aggrid.theme / BrandLogo / PkgIcon /
     pkg-icon.css / mark.svg <desc> / gen-brand-assets.mjs 中 tokens.css·styles/tw 字面引用改指新层
Files:
  新增: web/src/design-system/tokens/{color,typography,spacing,radius,shadow,motion,index}.css
        web/src/design-system/tailwind.css
        web/src/design-system/fonts.css
  修改: web/src/main.tsx（CSS 引导接线）
        web/src/app/foundation.ts（barrel import 路径）
        web/components.json（shadcn css alias）
        web/scripts/assert-tokens.mjs（豁免谓词 + 头注）
        web/vite.config.ts / src/app/providers/theme-provider.tsx / src/features/aggrid/theme.ts /
        src/components/{BrandLogo.tsx,PkgIcon.tsx,pkg-icon.css} / src/assets/brand/mark.svg /
        web/scripts/gen-brand-assets.mjs（均为注释路径清偿，零逻辑改动）
  删除: web/src/styles/tokens.css、web/src/styles/tw/tailwind.css、web/src/styles/tw/tokens.css
Tests:
  无新增 spec（批 1 是纯等价迁移——验收靠既有全量 e2e + 截图差分，批 0 golden 即本批验收面）
Commands:（实际运行，均在 repo 根或 web/ 下）
  cd web && npm run typecheck
  cd web && npm run lint
  cd web && node scripts/assert-tokens.mjs && node scripts/assert-i18n.mjs
  make lint（仓库根，Go 面）
  make console（npm ci + build + embed 拷贝 + console-size）
  grep -r "styles/tokens.css\|tokens\.css" web/src web/*.ts web/*.mjs
  BASE=… ADMIN_PW=password npx playwright test（全量，4 轮——拓扑见 Outputs）
  BASE=… ADMIN_PW=password npx playwright test e2e/design-baseline.spec.ts（截图差分门，未带 --update-snapshots）
Outputs:
  1. typecheck 绿（tsc --noEmit 零输出）
  2. web lint：0 errors / 45 warnings（react-hooks 既有存量，非本批面）；assert-i18n OK（3118 调用点/2345 键同构）
  3. assert-tokens: OK — css 14 + tsx 97 + token 定义层 7 个豁免（design-system/tokens/）
  4. make lint（Go）：0 issues
  5. make console 绿；console-size: 1,015,636 bytes（5MB 预算内；无新依赖，bundle 零增量来源）
  6. grep 门：0 残留
  7. 截图差分（批 0 golden，31 张）：29/31 像素级全等（15 页×亮暗 + login——explorer/search/builds/
     repos×4/users/groups/permissions/audit/storage/status 全过）；仅 repo-detail×2 有差——
     定性证据链：(a) 旧 bundle 自远程直取 vs golden = 0 像素差（golden 与数据均未漂）；
     (b) 新旧 bundle 全页 DOM innerText diff = 仅 4 行 Set Me Up curl 中的 origin 串
     （172.16.58.130:8083 → 本地前沿 127.0.0.1:8099，窗体 x406..989/y339..516 双主题同位）——
     纯测试载体 origin 回显，非视觉/值变更。结论：截图差分 ≈0 达成（残留 100% 归因于 harness origin）
  8. 全量 e2e（关键发现：远程 dev 容器 binflow-dev 镜像 uat-l0213-b906e7cd 内嵌的是改前 bundle——
     票面 BASE=remote 直跑测的是旧前端。为让门真实命中新代码，用本地静态前沿（serve 新 dist +
     /binflow API 反代远程，Origin/Referer 复写过 CSRF 同源校验）跑截图门；再用 bare-binary
     （make console 已 embed 新 dist 后 make build）在 /tmp 全新数据目录起本地实例跑全量：
     267 failed / 161 passed / 16 skipped。267 失败 100% 与本批无关，证据：
     (a) 251/267 = seed: PUT /binflow/api/repositories/{m8-perf-local,m9-r00,m10-legacy-generic}
         -> 400 "Repository key already exists"——ADR-0050 decision 2（L007-3，本轮）已裁 PUT=create-only
         （existing→400），而 web/scripts/seed-m8.mjs seedRepos 仍注释「Accepts 200 (replace) and 201
         (create)」且 converge 只重试 5xx/409/429——同实例第二个 spec 文件起 beforeAll 必炸（纯 API 面，
         与前端 bundle 无关；远程共用实例亦同因）；
     (b) 2 = design-baseline dashboard（本地全新实例无 demo-* 种子——环境差，非视觉）；
     (c) 其余 14 = 决定性对照：git stash 本批改动 → 重建 HEAD bundle 二进制 → 同规格全新实例跑
         同 11 个 spec 文件 → 18 failed / 26 passed，失败集与带本批改动的运行逐条相同
         （HEAD 本身即挂——树/node-detail/policy-keys i18n 文案/分页器等 14 条为存量环境态）。
     对照后已 stash pop + 重建 dist/embed/二进制复原到本批状态
Compatibility: 契约漂移：无（纯前端 token 票，REST 面零接触）。
              测试基建漂移（上报，非本批面）：ADR-0050 PUT=create-only vs seed-m8.mjs 的
              PUT-replace 假设——全量 e2e 在任何共用/二次运行实例上必然大面积红（见 Outputs 8a）
Security:      XSS 面：本批零组件/渲染改动（纯 CSS 变量层迁移，值不变）；无敏感信息进前端日志；
              本地前沿代理（/tmp 一次性脚本，已杀）仅复写 Origin/Referer 头过同源 CSRF 校验，不落密钥
Performance:  console-size 1,015,636 bytes（预算 5MB）；零新依赖；CSS 净变化 = 新增槽位声明
              （~40 行声明级，无选择器/规则增量）；dist index CSS 52,237→~51.8KB 量级持平
Risks:        1. mark.svg <desc> 的路径文字更新使 web 拷贝与 docs/design/brand K56 母版 desc 不再逐字
              相等（视觉几何零改动；母版归 ux-designer——Next 建议）
              2. 远程 dev 实例内嵌 bundle 滞后（改前）——后续 UI 批次若沿用「BASE=remote」验收，
              需先刷新该容器镜像或沿用本次的本地前沿/bare-binary 法，否则门测的是旧代码
              3. design-baseline 的 Set Me Up origin 回显无 mask——BASE 换载体即红（本次已定量归因；
              加 mask 属 spec 改动，未动）
Blockers:     无（验收门全部达成；全量 e2e 的 267 失败已用 HEAD 对照证明与本批零因果——门未降，
              证据链完整）
Next:         1. 提票修 seed-m8.mjs（ADR-0050 对齐：GET-first 或 converge 容纳 400-already-exists）
              ——恢复全量 e2e 在共用实例上的可跑性（web/scripts 需票面指向，本批未越权改）
              2. design-baseline 建议：repo-commands 卡 origin 回显段加 mask（或快照时钉 BASE），
              使门可随载体迁移；登记批 0 spec 头部「已知妥协」清单
              3. 流程建议：UI 批次验收需明确「被测 bundle 供给方式」（远程镜像刷新点 或 本地
              bare-binary 法——后者本次已验证可复用：make console && make build && /tmp 数据目录）
              4. 锚册零接触（本批未涉及 parity 行）；批 2（壳 240/64）将集中承受 G4 视觉变更并翻新基线
```

## 验收门逐项

| 门 | 结果 |
|---|---|
| typecheck | 绿 |
| make lint（Go） | 0 issues |
| make console（vite 构建 + 资产接线） | 绿 |
| console-size | 1,015,636 bytes（< 5MB） |
| assert-tokens / assert-i18n | OK / OK |
| grep 旧 tokens.css 引用 | 0 |
| 截图差分 ≈0 | 达成（29/31 像素全等；2 张 repo-detail 差已定量归因为测试载体 origin 回显，DOM 级 diff 仅 origin 串；golden 未更新） |
| 全量 e2e | 已跑 + HEAD 对照：失败集与本批零因果（251 seed 400[ADR-0050 vs seed-m8 漂移] + 2 环境 + 14 存量；旧 bundle 同实例同挂 18/18） |

## 起止

2026-09-15 02:4x ~ 04:0x（CST）
