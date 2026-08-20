# 评审报告 T-101（视角: consistency + 前端正确性）

结论: **REQUEST_CHANGES**（1 blocking，可一行修复）
日期: 2026-08-21
评审范围: 工作树未提交改动中本票面——`web/src/pages/security/`（10 文件）、`web/e2e/{security,pathmatch-parity}.spec.ts`、`web/scripts/gen-pathmatch-fixtures.mjs`、`SettingsPage.tsx` N1 收口、`main.tsx`/`AppShell.tsx` 安全相关行。governance/audit/lib/governance.ts 等为 T-102 在途面未评。

## 必须修改（blocking）

### B1. 409 文案解析在含点 target 名上截断/丢名 — W33c 解除闭环断裂

- 位置: `web/src/pages/security/api.ts:111-118`（`parseReferencedTargets`）
- 问题: 正则 `/permission target\(s\):\s*(.+?)\./` 的 `(.+?)` 非贪婪停在**第一个**句点。服务端（`groups.go:219-222`）对 permission target 名**无字符集限制**（仅非空，`permissions.go:51-53`），且本票自己的创建表单 `perm-form-name` 也只校验非空——`qa.build` 这类名字完全可达。实测：

  ```
  srv("t1, t2")      → ['t1', 't2']        # 正常
  srv("qa.build")    → ['qa']              # 截断
  srv("qa.build, plain") → ['qa']          # 截断 + 第二个 target 整个丢失
  ```

  后果：冲突面板（W33c 的「解析 target 名 → 链接直达 `/security/permissions/:name` 解除」）链接指向不存在的 `qa` → 编辑器 404 空态。原文 mono 展示仍在（用户可手工导航），但 AC① 的 W33c 解除路径对此类名失效。
- 建议改法: 尾锚定句式而非任意句点，一行修复：

  ```ts
  const m = /permission target\(s\):\s*(.+?)\.\s+Remove the group/.exec(message)
  ```

  并在 `e2e/security.spec.ts` W33c 补一条含点名 target 的 409 用例（当前用例名均为 `uniq('tgt')` 无点，正因此漏网）。

## 建议改进（non-blocking）

1. **编辑器整页 admin 位预收敛 vs §3.6.3**（`PermissionEditorPage.tsx:173-185`）：edit 模式的 early-return 以 whoami admin 位隐藏**数据呈现**（target 配置回显），而 §3.6.3 裁定数据呈现（L2/L3）一律 403 驱动、admin 位仅 L1 导航组与 L4 写入口。同向无破窗（admin 收紧面 ⊆ 403 收紧面），且 `:202-208` 已有等价 forbidden 分支——建议 early-return 收窄到 `mode === 'create'`（纯 L4 面），edit 让 403 终裁。
2. **sameSnapshot 与 buildTargetDiff 语义不一致**（`targetdiff.ts:59-68` vs `:27-30`）：数组比较用 `join('\n')` 顺序敏感，diff 是集合语义。用户「删掉 chip 再加回」（重排序）时 diff=[] 而 dirty=true → 确认框落入「新建 target（全部为新增项）」文案（编辑态误导）。建议列表也用集合比较（或排序后 join），并在 diff 为空时编辑态换文案。
3. **零动作主体静默剔除无 diff 行**（`PermissionEditorPage.tsx:263-265`）：基线若带 API 带外建的零动作主体（`users: {bob: []}`，服务端接受），保存会剔除该行而 diff 不显示。语义无害（空动作 ≡ 未授权，决策 3 成立），建议 diff 补「移除零动作主体」行或在矩阵行标注。
4. **testid 契约先落码后回写**：§10 v1.1 规定「T-100~T-102 新增锚必须先入 §10.3 再落码」，本票落了约 40 个 §10.3 之外的锚，且把 v1.1 锚 `perm-matrix-cell-<principal>-<action>` 细化为 `perm-matrix-cell-{user|group}-<principal>-<action>`（正当理由：用户/组同名碰撞；日志遗留 2 已附 v1.2 回写全文）。**conductor 动作**：T-104 冻结断言锚前必须完成 v1.2 回写，否则 §10「唯一锚源」地位失守。
5. **users 列表行级 N+1**（`UsersPage.tsx:24-63`）：日志已登记（遗留 5），管理面量级可接受；副作用（导航取消→后端 500 假象）已作为漂移 5 转交 dev-go-core。
6. **编辑态对带外已删 target 的保存会静默重建**（POST create-or-replace 语义使然）：管理面可接受，备案。

## 已核实项（证据）

- **pathmatch 同源（最大风险面，通过）**：`pathmatch.ts` 与 `internal/auth/pathmatch.go` 逐语义比对一致——everything 三形态（`""`/`**`/`**/*`）；`segsMatch` 的 `**` 回溯 `skip <= len`、rest 递归；`lastPlain` 目录前缀规则仅 `isFolder`（尾 `/`）且 `pi > 0`；段两侧 trim；`wildcardEqual` 首尾字面量锚定 + 中段有序。`evaluatePath` 与 `authorizer.targetCovers`（authorizer.go:83-98）组合规则一致：空 includes = 全部、任一 exclude 命中一票否决。`matchesAny` 空表 false 同款。
- **fixtures 双闸（通过）**：36 条与 `pathmatch_test.go` TestPathMatcherMatch 表逐条人工核对一致（含 `**/tmp` vs `**/tmp/**`、B-2 回归行）；`gen-pathmatch-fixtures.mjs --check` 为**全文件比对**（含头部），漂移 exit 1，实测 `ok: 36 fixtures in sync`；`npx playwright test e2e/pathmatch-parity.spec.ts` 实跑 **3/3 通过**（本评审独立复现）。闸强度备注：生成器按行正则解析，Go 表改多行形态会被 `<30` 行守卫拦下（响亮失败），可接受。
- **后端契约对齐（通过）**：users PUT create-or-replace 201 无 body / POST 指针语义（TS optional 字段 + JSON.stringify 丢 undefined = absent，`groups:[]` 显式清空）；groups PUT 201/200、DELETE 409 句式与 200 纯文本；permissions POST create-or-replace（repos 非空 + 存在性 + 主体存在性前置，UI 均对齐）、DELETE 204（apiJSON 204 短路处理）、无单查端点（列表过滤 + notFound 与 hydration 解耦正确）。组名/用户名预检与 groups.go/security.go 同口径。
- **N1 收口（通过）**：`SettingsPage.tsx` 健康行 `{admin && …}` → `health.status !== 'forbidden'` + `settings-health` 锚，正对 §3.6.3 行 247 的裁定原文；e2e 双向断言（非 admin `toHaveCount(0)` / admin 可见）。
- **§4.9 线框（通过）**：[1]~[4] 四段全落（name 锁定 + repo chips、双栏 chips + 测试器、矩阵 + 👥组行 + admin 隐式注、diff 确认）；列表页「更新时间」缺列为真实契约缺口（permissionBody 无时间字段），漂移 3 登记成立。
- **验证命令**：`npm run lint` 0 告警；`npm run typecheck` 4 错误**全部**在 `web/src/pages/repositories/tree/`（T-100 在途，文件 mtime 03:12 晚于本票面），security 面零错误——日志「typecheck 通过」在其时点成立，归属区分清楚；deps 零变更实测（package.json/package-lock 无 diff）。
- **e2e 证明力（通过）**：W33b 断言链强且真实（第二上下文 201 → admin UI uncheck+submit → **同一 bob 会话** PUT 403 + GET 200 自有 read 存活——即时失效无重登），所引用全部 testid 逐一在源码对上（含 confirm-dialog/confirm-accept）；W33 三步流带 API 三实体字段级对账。真后端 8/8 未在本评审环境复跑（实例已关，起后端超出只读评审边界），以日志证据 + 断言源码交叉核对采信。3 条 governance 失败归属 T-102 判断成立（错误签名在其自身 api() helper 的相对 URL fetch、任何 UI 交互之前，与本票面零交集）。
- **clean-room（通过）**：零 reverse-src 接触；pathmatch 移植源是本仓 `internal/auth/pathmatch.go`（票据 AC 明令同源），fixtures 事实源是本仓 Go 测试表。无逐行翻译反编译代码嫌疑。
- **403 四层（通过）**：L1 导航组 adminOnly；L2 三页 forbidden → 无权限卡（empty-state 缺省锚 scope 到页面根）；L4 创建/删除按钮仅 admin 渲染；数据行零 admin 位硬编码（UserRow admin badge 来自详情请求）。

## 范围外交 conductor

- 漂移 1（用户禁用/删除无 API 面）：已核 `router.go:434-449` 确无 DELETE users、禁用位恒 false——AC③「禁用/删」无后端面，UI 不伪造 + 只读说明的处理正确，需 PM/architect 裁决补端点或修订 AC。
- 漂移 2/3（建仓 200 非 201、permissions 无时间戳）与遗留 1（fixtures --check 的 CI 接线归 T-89 面）：维持日志登记，conductor 派发。
- T-100 在途 tree 文件使全树 typecheck 红——与本票无关，提请 conductor 在 T-100 评审时追。
