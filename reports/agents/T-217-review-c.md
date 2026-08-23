# T-217 评审报告（视角：correctness — 越权/并发/错误处理）— REQUEST_CHANGES

- 日期: 2026-08-23
- 复核人: code-reviewer（按其运行约束，报告原文由 conductor 归档）
- 结论: **REQUEST_CHANGES**（blocking 1 / non-blocking 4）

## Blocking（1 条）

**B1. internal/httpapi/permissions.go:100 — POST 覆盖集臂只校验 body 的 repos，未并集存量 target 的 repos → m-holder 可用同名替换销毁覆盖集之外的授权**

实证（对抗探针，真实 REST wire）：
- 装置：carol 仅持 app-local 的 m；admin 的 `t-ent` 覆盖 `["other-local"]` 授 dave read（dave GET `other-local/ent.bin` = 200）。
- 对照：carol `DELETE /api/v1/permissions/t-ent` = **403**（DELETE 臂查存量 repos，正确）。
- 探针：carol `POST /api/v1/permissions` `{"name":"t-ent","repos":["app-local"],"principals":{"users":{"carol":["read"]}}}` = **201**；`t-ent` 被整体替换，**dave 在 other-local 的 read 随之被吊销（403）**。

判定：非获益型提权，但是**跨覆盖集的破坏性安全面写**——任一 m-holder 可按名劫持实例内任意 target，吊销其他团队全部授权、甚至剥掉另一位 repo-admin 的 m 位。与同文件三处自证矛盾（① DELETE 臂存量校验被 POST 臂旁路；② router.go 不变量注释「Manage holders edit exactly the targets inside their coverage」；③ 存在性隐藏的理由——清单不可读却能按名覆写任意行）。ADR-0026 决策 3 的「target 的 repositories ⊆ 覆盖集」在 create-or-replace 语义下应读作**存量 ∪ 新 body**。

**改法**：handlePermissionCreate 非写者臂在 `GetTarget(body.Name)` 命中存量时，对 `union(body.Repos, unmarshalStrings(existing.Repos))` 跑 `canManageAllRepos`；`TestT217CoverageMatrix` 补腿：同名越界替换（body ⊆ 覆盖集、存量 ⊄）→ 403 + 清单字节不变。修复不影响任何现有绿腿。

## non-blocking（4 条）

1. m-holder 的 principal 名字枚举面（permissions.go:127-149）：非写者 400/201 可区分 user/group 存在性（POST 臂泄漏 vs DELETE 臂隐藏）——仅名字 oracle，后续票统一或记录。
2. readonly_admin 在 POST/DELETE permissions 的 403 文案 plain-text vs envelope 不一致——无断言依赖，T-218 按服务端原文呈现即可。
3. PRD V07 骨架两处勘误（PUT/200→POST/201；「GET repositories 200」注明单仓详情）——PM v1.2 待回写。
4. repo_test.go 的 TestRepoCRUDRequiresAdmin 改直调——契约变更如实注释，REST 判别钉在 httpapi 成立；B1 修复腿落 httpapi 侧即可。

## 分项核验（七项任务全跑）

1. **覆盖集臂数学**：空集拒/部分交集/不相交全钉；DELETE 双臂 + 存在性隐藏矩阵实证；**唯同名替换边界破防 → B1**。
2. **service 门放宽零提权**：grep 全仓非测试调用点仅 httpapi/repositories.go 四处；migrate/writer 与 cmd/bf 走 HTTP client 不触 service——无无门路径；DeleteRepo 保 requireAdmin 正确；建仓臂唯一守门探针亲证（删分支 → 真实建仓 200）。
3. **usage ∨-臂**：readonly_admin 经 Can(r) 角色臂 200（有专测）；m-无-r 翻转腿 + W26b 既有 r 腿全钉。
4. **manage wire**：POST 收→CanManage 落库、GET 按 r/w/d/m 回显往返保真；无 m 位行零变更。
5. **守卫计数**：独立 grep 24 manage + 4 repoManage + 0 admin 与常量一致；族 4 例外无法掩护真漏迁（其他字面量降级会使计数偏离 24）。
6. **实跑**：build/vet 三包过；-race 三包 ok（181.9s/94.7s/66.3s）；lint 0；`EXPECT=1` 矩阵 0 deviations；C22/C27/W19/W19c/W21/W26/W26b/W27 + t215/t217 全绿。
7. **独立红绿**（cmp 精确还原）×4：删覆盖臂→4 腿红；回滚 requireAdmin→carol2 腿红；删 ∨-臂→usage m-only 红删建仓臂→真实建仓红。
8. **clean-room**：新文案/标识符 reverse-src 零命中；覆盖集臂系 ADR-0026 自有设计（Artifactory permission API 为 admin-only）；动作集与 auth-model §4 一致。
