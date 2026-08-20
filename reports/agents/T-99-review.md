# 评审报告 T-99（视角: consistency + 前端正确性）

- 结论: **REQUEST_CHANGES**（blocking 2 / non-blocking 9）
- 评审对象: commit `0f391ea`（web/ 树与该 commit 逐字节一致，已核 `git diff 0f391ea -- web/` 为空；当前工作树中的 internal/adapter/docker 等未提交改动与本票无关）
- 契约参照: internal/httpapi/repositories.go、usage.go、router.go、internal/repo/{api,validate,config,service}.go、internal/adapter/maven/config.go、docs/design/console-ux.md §4.3~§4.5/§5、docs/user/integrations/{npm,pypi}.md
- 复跑取证: `npm run typecheck`（0 错误）、`npm run lint`（0 告警）、`go test -count=1 ./internal/console/`（ok 0.281s）——与工作日志声明一致

## 契约对齐核查（重点区 1）

`repos.ts` 与 repositories.go/usage.go 逐字段比对**全部吻合**：

- 列表 `?type=&packageType=` → handleRepoList（TrimSpace + 精确匹配，非法值空数组）；详情 `rclass` 拼写、`configuration?` 可选性与 repoConfigOf 的 omitempty 行为一致，`cfgStr/cfgBool/cfgNum/cfgStrList` 对 undefined 安全。
- 创建 PUT / 更新 POST / 删除 `?deleteContent=true` 与 handleRepoPut/handleRepoPost/handleRepoDelete（EqualFold("true")）逐形态一致；更新走 POST 防误建的选型正确（POST 对 unknown key 404）。
- transport 字段子集：remote 臂 {url,username,password,4×TTL,hardFail,allowPrivateUpstream,priorityResolution}、virtual 臂 {repositories,defaultDeploymentRepo}、local 臂 {priorityResolution,maven 策略族,includes/excludes,quotaBytes} 与 configJSON 三臂一一对应。maven 策略合法值与 adapter/maven/config.go 常量同源（client-checksums/server-generated-checksums；deployer/non-unique/unique——unique 的 P2 注记也如实标注）。
- key 预检 `[a-z][a-z0-9-]{1,62}`（2..63）与保留段 {api,v2,docs,console,ui,assets} 与 validate.go/api.go 完全同源；url 预检与 parseRemoteConfig 同口径。
- 「编辑=全量替换的保全」策略核实成立：三类 rclass 的 buildBody 都保证 config map 非空（local 恒带 quotaBytes 指针、remote 恒带 url、virtual 恒带 repositories），UpdateRepo 的 `r.Config != ""` 分支整体重写——预填全量再整体提交的推理链与后端行为吻合，e2e 的「未动字段保全」断言（excludesPattern 往返）佐证。

**T-95 遗留④（空串语义）核实**：表单提示「留空 / **/* = 匹配全部路径（保存为全量替换）」与后端实际行为**一致且正确**——T-95 遗留④说的「空字符串=保持现状」只适用于「整个 body 无任何有效字段 → configJSON 返回 ""」的 REST 单字段场景；FE 全量替换流下留空即从存储 config 中移除该键 → governance 默认（includes `**/*`/excludes 空）生效。提示到位。建议 T-95 遗留④措辞回写时澄清这一区分，防后续误读。

## governance 表单语义（重点区 2）

quotaBytes/patterns 仅 local 呈现 ✔（step 3 按 rclass 分支渲染；remote/virtual 在详情治理卡呈「仅 local 支持」——T-95 遗留⑤收敛）。409/413 行内呈现：repo CRUD 的 400/404/403 全覆盖 form-error 原样 mono 呈现；409/413 属上传面，归 T-100（遗留 4 登记成立，e2e 已在 API 层断言 409 双 pattern message）。

## 必须修改（blocking）

1. **`web/src/pages/repositories/RepositoriesPage.tsx:210-244`（行内交互缺隔离，成员浮层实际不可用）**：`<tr onClick={() => navigate(...)}>` 之下，key 列的 `<Link>` 做了 `stopPropagation`，但同格的 `<CopyButton>`（components/CopyButton.tsx:33 无 stopPropagation）与「上游/成员」格里 virtual 成员的 `<details><summary>` 浮层（UpstreamCell，RepositoriesPage.tsx:68-84）都没有。点拷贝按钮 = 拷贝成功**同时被拽进详情页**；点 summary 想展开成员浮层 = 浮层刚开页面即被换走——工作日志宣传的「virtual 成员 details 浮层（键盘可达零 JS）」在列表页实际不可用（summary 的键盘 Enter 也会派发 click 同样触发导航）。e2e 未覆盖这两个控件所以漏网。
   **建议改法**（二选一或并用）：① CopyButton 的 onClick 加 `e.stopPropagation()`；② UpstreamCell 的 `<details>` 外包一层 `<span onClick={(e) => e.stopPropagation()}>`；③ 更简单——移除 tr 级 onClick 与 `cursor:pointer`，行导航只保留 key 主链接（ux §4.3[1] 的「行点击进详情」是次级 affordance，key 链接已承担）。补一条 e2e：列表点开成员浮层断言不跳转、点拷贝断言 URL 不变。

2. **`web/src/pages/repositories/RepositoryFormPage.tsx:519-527`（virtual 编辑态 defaultDeploymentRepo 联动缺失 → 可预防的 400）**：取消勾选成员的 onChange 只 `set('members', filter)`，不清理 `f.defaultDeploymentRepo`。若被取消的成员恰是默认部署仓：select 的选项列表（localMembers 由 f.members 派生）失去该项 → 受控 select 显示空白，但 state 仍持旧值（下方提示「经此 virtual 仓的部署将写入 X」还在），提交 → 服务端 400 `defaultDeploymentRepo "x" is not a member`（service.go:1691-1695 实证）。用户面对一个看不见的值报错。讽刺的是 `moveMember`（340-347）里有完全正确的联动清理，但重排不删成员，那段是**死代码**——需要的恰恰是 uncheck 分支。
   **建议改法**：uncheck 分支加 `if (!e.target.checked && f.defaultDeploymentRepo === o.key) set('defaultDeploymentRepo', '')`（顺带删掉 moveMember 里的死代码分支或保留作防御）。补 e2e：编辑 virtual → 取消 default deploy 成员 → select 回「（未配置）」且提交成功。

## 建议改进（non-blocking）

1. `RepositoriesPage.tsx:38-52` UsageCell 对 virtual 行仍发起 usage 请求（hook 在 early-return 之前调用，结果丢弃；virtual usage 恒 200/0）。详情页同场景已显式规避（RepoDetailPage.tsx:77-83 注释「virtual 仓无自身内容，不发起请求」）——列表/详情两处口径不一致，且每个 virtual 行每次挂载白耗一个请求。建议按 rclass 条件渲染拆分子组件。
2. `RepoDetailPage.tsx:75-83` usage 的 deps `[state.status, state.data?.rclass, routeKey]`：A→B 快速切换时 state.data 短暂为 null → usage 以 `Promise.resolve(null)` 落定为 'ok' → 详情闪一帧「用量不可用（—）」再回骨架。建议 data 为 null 时维持 loading 态，或以 `state.data?.key` 入 deps。
3. `web/src/lib/api.ts:160-162` RepoListItem 注释「local 行 M1 裸形态」已过时——T-95 后 local 行也回带 governance config。注释勘误即可。
4. `commands.ts:25-142` clientCommands 的 switch 无 default 分支，调用点 `repo.packageType as PackageType` 强转：服务端 enum 保证五值，但异常值会让 commands 为 undefined → `.map` 抛错白屏。建议加 `default: return []`（无 ErrorBoundary 是 T-98 基座缺口，见范围外）。
5. quotaBytes 前端只校验非负整数，>int64（约 9.2e18）的大数会以浮点形态序列化、服务端 JSON 解码 400（行内可恢复）。可在 stepValid 加上限预检。
6. e2e 缺口（与 blocking 1 直接相关）：未覆盖列表行拷贝/成员浮层交互；remote 编辑「留空保存=清除凭据」只断言了回显面无密码，未断言清除实际生效（GET 后 username 仍在、密码行为需二跳验证——可在后继票补 API 断言）。
7. AC③ 措辞「includes/excludes（mono chip 编辑）」交付为 mono 文本输入 + 逗号分隔提示——与服务端契约（逗号串）功能等价，ux §4.4 线框也未画 chip。请 conductor 裁定接受措辞或要求 chip 化（建议接受，登记口径）。
8. §4.5[3]「本仓最近事件」卡（audit 端点支持 `?repo=`，可实现）与统计卡的镜像/tag 计数未交付，**且未入漂移/遗留清单**——请补登记（可归 T-100 或 P2）。
9. 列表 forbidden（非 admin）态下过滤栏与「共 N 个」计数仍渲染在无权限卡上方——cosmetic，建议 forbidden 时一并收起。

## 重点区 3~7 结论

- **删除双段流（3）**：实现正确且优雅——`repo-delete-reason` 呈现服务端 400 原文（service.go:1954-1962 的 `holds N node(s)`/`docker image(s)` 文案均含 "deleteContent"，匹配条件 `includes('deleteContent')` 成立）+ 预勾选 + 重新输入 key；第二段 uncheck 重试不会递归回弹（`!presetContent` 门）。中间态：请求在飞时对话框关闭、删除按钮 disabled，可 Esc/点背板退出。ConfirmDialog 的 `confirmDisabled?: () => boolean` 缝向后兼容（不传恒可用），modal 根 onInput/onClick bump 重渲染的机制对文本输入与 checkbox 均生效（React 合成事件目标先于祖先派发，holder 先更新后求值）；T-98 登出路径由 auth-shell e2e「logout revokes…」回归覆盖。`confirmLabel: deleting ? …` 是恒 false 的死表达式（nit）。
- **路由与门控（4）**：非 admin 收敛姿态正确——列表/详情 403 → useAsync forbidden → 无权限卡（**不会**误显「还没有仓库」空态）；表单以 whoami admin 位本地门控（candidates 的只读 GET 会发一次 403，无害零写）。e2e 的零 PUT/POST 断言（request 监听）强度足够。
- **状态管理（5）**：useAsync（B2 修复后基座）各消费点 deps 形态正确（列表 [typeFilter,pkgFilter]、UsageCell [repoKey]、详情 [routeKey]、usage 含 state.status 触发 reload 后重取）；无 AbortController 属已文档化的取舍（alive 旗标丢弃晚到响应）。DashboardPage 改动即空态 hint 文案更新（原 hint 引用「T-99 待交付」已过期），非 admin 说明卡在位。
- **构建纪律（6）**：+16.4KB 构成逐项可核（三页面懒加载 chunk + repos.ts + pages.css），主 chunk 持平，预算 2.1%；commit 文件清单无 package.json/package-lock——deps 零变更属实。e2e 四例对 AC①②③ 覆盖扎实（API 对账腿用同源 fetch 是 curl 的浏览器等价，正式归 T-104）。
- **漂移①影响面（7）**：router.go 路由门 admin-only（332-357 行实证）vs ux §3.3 的分歧属实。前端不伪造、以 403 无权限卡收敛 + 仪表盘显式说明卡，是正确姿态；关键的是**不会把「无权限」误读为「无仓库」**（forbidden 与 empty 两态分离）。T-100 树页走 /api/storage children（authenticated+ACL）不受影响。维持工作日志的裁决请求：architect/PM 二选一（放宽路由门或修 ux §3.3）。
- **clean-room**：FE 为原创 React/TS，命令块与 docs/user 同源（npm 的 `//host/path/:_auth` 限定形态、pypi 的 index-url/repository 拼写逐条核对一致）；与 reverse-src（反编译 Java）无逐行对应嫌疑。

## 漂移/遗留清单核验

工作日志 5 项契约漂移、6 项遗留逐条核实**无虚报**；补充需登记：§4.5[3] 最近事件卡与统计卡镜像/tag 计数（上文 non-blocking 8）。

## 范围外发现（交 conductor）

- 无 React ErrorBoundary（任一页面渲染异常 = 整壳白屏）——T-98 基座缺口，建议小票补。
- T-95 遗留④措辞澄清（见「契约对齐核查」末段）。
- 漂移①/②/③/⑤ 的上游裁决与回写（architect/PM/tech-writer）。

## 复跑记录

```
cd web && npm run typecheck   # 通过（0 错误）
cd web && npm run lint        # 通过（0 告警）
go test -count=1 ./internal/console/   # ok 0.281s
```
