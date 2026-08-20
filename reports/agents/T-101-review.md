# 评审报告 T-101（视角: consistency + 前端正确性）

结论: **REQUEST_CHANGES**（1 blocking）→ **复核后 APPROVE**（见文末复核记录）
日期: 2026-08-21
评审范围: 工作树未提交改动中本票面——`web/src/pages/security/`（10 文件）、`web/e2e/{security,pathmatch-parity}.spec.ts`、`web/scripts/gen-pathmatch-fixtures.mjs`、`SettingsPage.tsx` N1 收口、`main.tsx`/`AppShell.tsx` 安全相关行。governance/audit/lib/governance.ts 等为 T-102 在途面未评。

## 必须修改（blocking）

### B1. 409 文案解析在含点 target 名上截断/丢名 — W33c 解除闭环断裂（已修复，见复核）

- 位置: `web/src/pages/security/api.ts`（`parseReferencedTargets`）
- 问题: 原正则 `/permission target\(s\):\s*(.+?)\./` 的 `(.+?)` 非贪婪停在**第一个**句点。服务端（`groups.go:219-222`）对 permission target 名**无字符集限制**（仅非空，`permissions.go:51-53`），且本票自己的创建表单 `perm-form-name` 也只校验非空——`qa.build` 这类名字完全可达。评审实测（修复前）：

  ```
  srv("t1, t2")      → ['t1', 't2']        # 正常
  srv("qa.build")    → ['qa']              # 截断
  srv("qa.build, plain") → ['qa']          # 截断 + 第二个 target 整个丢失
  ```

  后果：冲突面板（W33c 的「解析 target 名 → 链接直达 `/security/permissions/:name` 解除`）链接指向不存在的 `qa` → 编辑器 404 空态。原文 mono 展示仍在，但 AC① 的 W33c 解除路径对此类名失效。
- 建议改法: 尾锚定句式而非任意句点 + e2e 补含点名 409 用例。

## 建议改进（non-blocking）

1. **编辑器整页 admin 位预收敛 vs §3.6.3**（`PermissionEditorPage.tsx`）：edit 模式的 early-return 以 whoami admin 位隐藏**数据呈现**，而 §3.6.3 裁定数据呈现一律 403 驱动。同向无破窗且已有等价 forbidden 分支——建议 early-return 收窄到 `mode === 'create'`。
2. **sameSnapshot 与 buildTargetDiff 语义不一致**（已修复，见复核）：数组比较顺序敏感 vs diff 集合语义——重排时 diff 空而按钮可点、确认框误显新建文案。
3. **零动作主体静默剔除无 diff 行**：语义无害（空动作 ≡ 未授权），建议 diff 补「移除零动作主体」行或矩阵行标注。
4. **testid 契约先落码后回写**：约 40 个 §10.3 之外的锚 + `perm-matrix-cell-{user|group}-` 细化（有同名碰撞正当理由，工作日志遗留 2 已附 v1.2 回写全文）。**conductor 动作**：T-104 冻结断言锚前必须完成 v1.2 回写。
5. **users 列表行级 N+1**：已登记（遗留 5），管理面量级可接受。
6. **编辑态对带外已删 target 的保存会静默重建**（create-or-replace 语义使然）：备案。

## 已核实项（证据）

- **pathmatch 同源（最大风险面，通过）**：`pathmatch.ts` 与 `internal/auth/pathmatch.go` 逐语义比对一致——everything 三形态（`""`/`**`/`**/*`）；`segsMatch` 的 `**` 回溯 `skip <= len`、rest 递归；`lastPlain` 目录前缀规则仅 `isFolder`（尾 `/`）且 `pi > 0`；段两侧 trim；`wildcardEqual` 首尾字面量锚定 + 中段有序。`evaluatePath` 与 `authorizer.targetCovers`（authorizer.go:83-98）组合规则一致：空 includes = 全部、任一 exclude 命中一票否决。`matchesAny` 空表 false 同款。
- **fixtures 双闸（通过）**：36 条与 `pathmatch_test.go` 表逐条人工核对一致（含 `**/tmp` vs `**/tmp/**`、B-2 回归行）；`gen-pathmatch-fixtures.mjs --check` 为全文件比对（含头部），漂移 exit 1；parity spec 评审独立复跑通过。闸强度备注：生成器按行正则解析，Go 表改多行形态会被 `<30` 行守卫拦下（响亮失败），可接受。
- **后端契约对齐（通过）**：users PUT create-or-replace 201 无 body / POST 指针语义（TS optional 字段 + JSON.stringify 丢 undefined = absent，`groups:[]` 显式清空）；groups PUT 201/200、DELETE 409 句式与 200 纯文本；permissions POST create-or-replace（repos 非空 + 存在性 + 主体存在性前置）、DELETE 204（apiJSON 204 短路）、无单查端点（列表过滤 + notFound 与 hydration 解耦正确）。组名/用户名预检与 groups.go/security.go 同口径。
- **N1 收口（通过）**：`SettingsPage.tsx` 健康行 403 驱动 + `settings-health` 锚，正对 §3.6.3 裁定原文；e2e 双向断言。
- **§4.9 线框（通过）**：[1]~[4] 四段全落；列表页「更新时间」缺列为真实契约缺口（permissionBody 无时间字段），漂移 3 登记成立。
- **e2e 证明力（通过）**：W33b 断言链强且真实（第二上下文 201 → admin UI 移组 → 同会话 403 + GET 200，即时失效无重登），全部 testid 逐一在源码对上；W33 三步流带 API 三实体字段级对账。真后端用例以日志证据 + 断言源码交叉核对采信。3 条 governance 失败归属 T-102 成立。
- **clean-room（通过）**：零 reverse-src 接触；pathmatch 移植源是本仓 Go 代码，属票据明令同源。
- **403 四层（通过）**：L1 导航组 adminOnly；L2 三页 forbidden 无权限卡；L4 写入口仅 admin 渲染；数据行零 admin 位硬编码。

## 范围外交 conductor

- 漂移 1（用户禁用/删除无 API 面）：已核 `router.go:434-449` 确无 DELETE users——AC③「禁用/删」无后端面，UI 不伪造处理正确，需 PM/architect 裁决。
- 漂移 2/3 与遗留 1（fixtures --check 的 CI 接线归 T-89 面）：维持登记，conductor 派发。
- T-100 在途 tree 文件曾使全树 typecheck 红——非本票，提请 T-100 评审时追。

---

## 复核记录（B1 + NB② 修复，2026-08-21）

结论: **APPROVE**

### B1 复核（通过）

- `api.ts` 正则改为 `/permission target\(s\):\s*(.+?)\.\s+Remove the group from those targets first\.$/`。评审独立对抗向量实测：
  - 含点名完整：`qa.build → ['qa.build']`、`qa.build, plain → ['qa.build','plain']`、`a.b.c, d.e, f → ['a.b.c','d.e','f']`；
  - **病态内嵌后缀**（名中含 `. Remove the group from those targets first.`）：`$` 终锚迫使回溯停在名单真结尾，两个名字均完整解析——「多锚一段防病态」的主张成立；
  - `pathmatch-parity.spec.ts` 新增纯 Node 回归（含点向量组 + 非匹配回退 `[]`），评审实跑 **4/4 通过**；
  - `security.spec.ts` 新增 W33d 真后端腿为**可判别**测试：旧正则下链接文本 `toContainText(dotted)`、编辑器 `toContainText(dotted)` + `not.toContainText('不存在')` 三处必红。
- **耦合稳健性（conductor 提问，裁定可接受）**：服务端文案漂移时正则不匹配 → 返回 `[]` → 冲突面板退化为「原文 mono + 稍后再试、无链接」——无错误链接、无崩溃，且 W33c/W33d 两条 e2e 会**响亮失败**提供 CI 信号。降级面 = 便利性损失，非正确性损失。附注（非必改）：`toApiError` 对纯文本 message 已 `trim()`，尾部空白路径不可达；如需纵深防御可把终锚放宽为 `first\.\s*$`（一字之改，可选）。
- 残余（backend 根因，仅登记）：**含逗号**的 target 名在 comma-join 文案格式下天然歧义（会被 split 成两个链接）。服务端 `strings.Join(refs, ", ")` 是根因；如未来成虑，应由后端加引号或前端解析后与 `GET /v1/permissions` 交叉校验。极边缘，交 conductor 备案。

### NB② 复核（代码通过；测试腿判别力不足，non-blocking）

- `targetdiff.ts` 新增 `sameSet`（元素互含）用于 repos/includes/excludes，与 `buildTargetDiff` 的 `!includes` 同一集合语义；principal 键序本就 sort 后比较；确认框兜底文案改为「没有字段级变更（仅顺序或重复调整）」如实。**修复本身正确**。
- **测试腿判别力缺口（non-blocking）**：`security.spec.ts` 的回归腿用**单个** include chip（`**`）删后加回——单元素数组在新旧两种实现下都判定相等，**旧代码同样通过该断言**（不可判别）。真正可判别的复现需两个 pattern 重排（基线 `[a,b]` → 删两 → 逆序加回 `[b,a]`：旧 join 法 dirty=true 且确认框误显新建文案，新集合法 dirty=false）。建议 T-104 前顺手补：parity spec 加纯 Node 断言（`import { sameSnapshot } from '../src/pages/security/targetdiff'`，swapped-order includes === true）或将 e2e 腿改成双 pattern 重排。修复代码已核实正确，不阻 APPROVE。

### 复核自测（评审独立执行）

- `npx playwright test e2e/pathmatch-parity.spec.ts` → 4 passed（含 B1 回归）；
- node 对抗向量六组实测如上（含点×3、病态内嵌、文案漂移回退、尾部空白）；
- typecheck：`pages/security` 0 错误（本时点全树亦 0——T-100 在途面已自愈）；eslint 0 告警；`gen-pathmatch-fixtures.mjs --check` → ok: 36 fixtures in sync。
- 真后端 W33d/复归腿无实例可复跑（只读评审边界），以断言源码逐条对码采信（全部锚存在、语义与断言一致）。
