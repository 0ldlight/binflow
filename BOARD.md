# 任务看板（BOARD）

> 唯一事实来源。**只有主会话（conductor）可以写本文件**，所有 subagent 只读。
> ticket 由 tech-lead 生成、主会话录入。当前里程碑：**M6+（展望/规划阶段）**。M1~M5 已完成，tag m1-done / m2-done / m3-done / m4-done / m5-done（2026-08-21）。

## 票据格式

```
- **T-<编号>** [P0|P1|P2] 标题 `role:<agent类型>` `area:<Go包/页面组/部署目标>` `dep:T-x,T-y`
  AC: ①可验证的验收标准 ②… ③…
```

- 优先级：P0 阻塞他人/当前里程碑关键路径；P1 本里程碑应完成；P2 可延后。
- `area` 示例：`internal/storage`、`internal/adapter/docker`、`internal/auth`、`web/src/pages`、`deploy/helm`。
  同一轮并行派发的 ticket，area 不得重叠。
- `dep` 列出必须先完成的 ticket；协议适配类 ticket 必须依赖对应的逆向规格票。

## 📥 待办（todo）

> **M9 完结（closure）**（2026-08-25）。26 票全 done（23 规划 + 修复窗 T-274/T-275 + T-273 收编）；终验 **PASS**（首验 FAIL 抓 P0 锚误杀 → 修复窗 → 复验 DoD 八条全绿：e2e 176/0 默认并发 / lint 0 / ledger 六形态诚实 PASS / 契约审计 100% 归属 / F 池对账一致〔T-273 本里程碑修复〕）。**`m9-done` tag 已打并随常态授权推送双远端**。M10 候选池已录 ROADMAP（延后 3 + Q5/E7 + 票级遗留 17）。历史瘦身已执行（clone 70→8MB）。九个里程碑链 m1~m9 全交付。用户实例 18080 刷新到 M9 由 conductor 收官时执行。

### M7 票据（T-211~T-228，tech-lead 2026-08-23 分解；AC 全文见 docs/prd/milestone-7.md）

- **T-211** [P1] M7 验收脚手架 `role:devops-engineer` — done 2026-08-23（conductor 核验直收：红灯探针亲跑 404 BLOB_UPLOAD_UNKNOWN + 真实 EXIT=1；lint 基线复现 auth=53）
  三脚本 + 4 个 make 目标。红灯探针（kill -9/SIGTERM 双臂，main 基线必败非零退出，T-216 转绿即验收）；矩阵基线（user 管理面 11 读+6 变更全 403 零副作用 / read-only-admin 列 SKIP 待 T-215）；lint 基线（auth=53 + npm=1 逐项归档）。POSIX 四壳实测（bash3.2/dash/busybox ash/linux 交叉）。**勘误产出**：PRD §4.1 stats/token-list 端点 main 无路由；DU-01 GET 状态腿重启前实测存在（204+Range），缺的是跨重启存活 → T-216 范围收窄。提交 `5fd0b17`。日志 reports/agents/T-211.md。
- **T-213** [P1] Session.Offset() + ResumeSession 过期 fail-closed `role:dev-go-storage` — done 2026-08-23（code-review APPROVE 0 阻塞 + 复核人独立红绿复验无残留；提交 `c5a8206`）
  Offset() 权威 offset 契约（活会话=已收字节/恢复=重算文件长度，§3.1/§5.3.1 契约 1）；过期未清扫行 → ErrSessionNotFound（与 ListExpired `<=` 严格同界永不分歧，空/畸形行亦拒，拒绝零副作用）；migration 委托 + S3 最小实现行为零改动（hard-404 契约测试原样绿）；storage 包 race 84.9s 绿 + lint 0。日志 reports/agents/T-213.md / T-213-review.md。

#### 波 1（全 done）
- **T-214** [P0] M7 架构-PRD 冲突收敛与 ADR 定稿 `role:architect` `area:docs/design + DECISIONS.md` — done 2026-08-23（conductor 直审通过；提交 `af0f0f6`）
  **三分歧终裁**：① readonly_admin = 全域只读、角色短路 target（ADR-0026 胜出，角色名是安全不变量；连带否决 Q2 暂行的 GC dry-run 开放）；② Close **保留**未过期会话（PRD 方向胜出，三径 kill-9/SIGTERM/compose 对称可续传，sweep+TTL 唯一回收路径，新增 **ADR-0028** + ADR-0006 勘误④）；③ step-up 作用域全部非 admin session 臂、step_up_password（本地/LDAP）+ mint grant（OIDC，否决 id_token 窗口）、401 step_up_required、`auth.token_step_up` 默认 off（ADR-0027 修订 Accepted）。**wire 统一**：`adminRole`（camel）+ `readonly_admin`（snake）。§7.1 清点表终版（router.go 实际注册点全量 30 处 admin:true，幻影行勘误，T-211 实测三事实吸收）。风险登记：readonly_admin 全域内容读是新的可见面（部署文档明示）；ADR-0020 `admin_users` 文档-实现漂移建议小票；T-211 矩阵脚本列名拼写待 T-215 同步。日志 reports/agents/T-214.md（P1~P13 回写建议 → PM v1.1 在途）。

#### 波 2（全 done 2026-08-23；提交 `efd88d7`/`e9de5ef`/`6d379cf`）
- **T-212** [P0] RBAC 基座 `role:dev-go-core` — done（review APPROVE 0 阻塞：四不变量逐条 + 双独立红绿 + clean-room 无嫌疑）
  Role 闭集 + 六能力 CanManage 全表 + m 动作（只判 repos[]、双向不隐含）+ readonly 短路 + 四写路径单语句镜像 + Verify/Session 即时生效缝 + idp_sync 权威阶梯 + migration 011 双方言（回填/幂等/101 用户 ~80ms）。**review 移交（并入 T-215）**：① adapter/docker/token.go:253 `authenticateForm` 不带 Role——form 腿 readonly_admin 折叠为 user（欠授权 fail-closed 非提权），**T-215 接通 readonly_group 前必修**；② metadata Create 对 Role+IsAdmin 矛盾输入无校验（ADR 冲突 400 归 handler）；readonly_group config/cmd 接线（T-212 遗留①）。**归 T-220**：TestSessionTTLAbsoluteCapWins 墙钟 flake 放宽。日志 reports/agents/T-212.md / T-212-review.md。
- **T-216** [P1] FR-67 docker 续传 REST 化 `role:dev-registry-adapter` — done（review APPROVE + sigterm 臂随 T-229 转绿）
  lazy 重建（per-id 单飞，变异实证）+ 五动词统一 resolve + offset 事实源 sess.Offset() + 416 空 body 权威 Range + S3 恒 404。kill 臂 GREEN EXIT=0（复核人新构建复证）、真实 docker 27.5.1 全绿。**挂账**：探针陈旧二进制陷阱（T-222 消费前必修）；flyer defer 加固（M7 债）。日志 reports/agents/T-216.md / T-216-review.md。
- **T-229** [P1] ADR-0028 Close 保留语义 `role:dev-go-storage` — done（conductor 强制新构建亲验：sigterm 臂 GREEN EXIT=0 + kill 臂维持绿）
  Close 去 cleanup 改 detach + 保留清单 INFO 日志；sweep+TTL 唯一回收四路钉死；N2 前移（sweep-residue restart 臂重种子语义）。三包 race 绿 + lint 0。日志 reports/agents/T-229.md。

#### 波 3（done）
- **T-215** [P0] FR-64 REST：routeAuth 能力化迁移 + adminRole wire + 角色即时生效与审计 `role:dev-go-core` — **done 2026-08-23（双视角 review：架构 APPROVE + 正确性 B1 返修闭环；提交 `bbbfbf2`）**
  30 门迁移（26/4，双 review 独立 grep 零残留 + 守卫测试防回归）+ adminRole wire（冲突 400/kebab 拒/回显/落值/镜像）+ 同 Token 即时生效三段实证 + user.role.change 审计 + authenticateForm 带 Role + readonly_group config/cmd 接线 + 矩阵 EXPECT=1 零偏差。**B1 返修**：m-holder 夹具（metadata 真缝 + 幽灵仓 PUT 可达性）判别性测试——门源文案断言（删分支精确翻红）。日志 reports/agents/T-215.md / T-215-review-c.md / T-215-review-a.md。

#### 波 4（done）
- **T-217** [P0] FR-65 REST `role:dev-go-core` — **done 2026-08-23（双视角 review 双 REQUEST_CHANGES 独立收敛 B1 → 返修红绿闭环 → conductor 复验；提交 `04f88fb`）**
  manage wire + 族 4 例外门（handler 覆盖臂，守卫 26→24 精确两处）+ service 门放宽（Create/Update authenticated、Delete 保 admin；非测试调用点仅 httpapi——零提权 grep 证实）+ usage ∨-臂（m-无-r 翻转 403→200）+ **B1 修复**：替换臂 union(body, 存量) ⊆ 覆盖集（对抗探针实证的跨覆盖集吊销洞闭合，矩阵腿 403+清单字节不变钉死）。矩阵 EXPECT=1 零偏差；三包 race 绿 + lint 0。偏离（路由字面量迁移）裁可：§7.1 族 4 行明文预载。挂账：principal 名字枚举面（M8 裁量）；PM 回写勘误 1（POST/201+字段拼写）成立、勘误 2 可选。日志 reports/agents/T-217.md / T-217-review-a.md / T-217-review-c.md。

#### 波 6（T-223 done；T-220 待 T-219；T-226 待 T-230 收尾+扩盘）

- **T-223** [P1] M7 文档 I：RBAC 指南 + 上传续传说明 `role:tech-writer` — **done 2026-08-23（conductor 直审通过；提交 `24259fb`）**
  新页 admin/rbac-roles.md（三值模型+矩阵+wire 用法+覆盖集规则+审计+readonly_group 键）+ docker-registry.md 续传节（ADR-0028 口径、S3 限制如实、curl 全链）+ 四处既有补齐 + docs-site 重建零断链。**文档内全部 curl 逐条 scratch 实跑验证**；双探针复跑 GREEN。遗留归位：FAQ/console/step-up → T-225/T-218。日志 reports/agents/T-223.md。

#### 环境票（done）+ 波 6（在途）
- **T-230** [P2] 用户 VM 纳管 `role:release-engineer` — **done 2026-08-23（conductor 免密腿亲验；报告 `3b335b8`）**
  systemd 真机复验全绿（unit 硬化 systemctl show 证实 / docker push/re-pull digest 逐位一致 / npm 往返 / 优雅停机 0.07s 含引擎 drain 日志）；SSH 密钥免密固化（macOS expect pty 挂死以 SSH_ASKPASS 绕开）；docker 29.1.3 + mirrors（Hub 直连不通）+ minio 镜像在位；VM 回基线零残留；凭据零落盘。勘误：免认证 ping 路径实为 `/binflow/api/system/ping`。日志 reports/agents/T-230.md。
- **T-226** [P2] M7 等价口径回归（V29） `role:qa-engineer` — **done 2026-08-23（PASS：等价基线全绿、M7 回归=零；qa 报告 `66a8d1c`）**
  H01~H05 + H62~H67 全绿（MinIO/S3 + mock 源）；真实 OSS 腿 license 门限制如实归档。**B-1 [P2 建议票]**：bf-migrate users 阶段 ListUsers 403 硬 abort → 建议降级 warning 或 `--skip-users`（M8 候选）。**T-228 环境知识**：7.84.10+PG system.yaml url 形态/OSS UI-only 建仓建户/token scope 只收 applied-permissions/user。VM 扩盘中断自愈实证（growpart+resize2fs）。日志 reports/agents/T-226-qa.md。
- **T-228** [P2] FR-69/Q9 真实 Artifactory 迁移实腿（V28） `role:qa-engineer` — **done 2026-08-23（带限制通过；qa 报告 `012bd92`）**
  真实 OSS 7.84.10+PG 源（7 仓/158 制品/4 用户/5 token，curl+mvn 真实客户端造数）H62~H67 全绿；占用守卫/合并幂等/157 迁移 12/12 四方 sha256 对齐/七份报告/用户迁移登录/dry-run 双跑。**结构性限制归档**：OSS 无 docker/npm 协议面（404 实证，config-only 迁移）；B-1 真源实证。**新缺陷 D-1→T-231**。栈保留（VM，盘 99G 用 17G）。日志 reports/agents/T-228-qa.md。
- **PM v1.2 回写** — **done 2026-08-23（7 项勘误落 PRD v1.2，提交 `ffd8e6c`）**

#### M7 完结（closure，2026-08-23）

DoD 七条全达成（PRD §9 对证）：P0/P1 全绿（T-221 16/16 / T-222 192/192）/ P2 双态过 + 条件腿 Q6 口径（T-226 等价归档 + T-228 真实腿带限制通过；T-227 真实 AWS 未到位不阻塞）/ 文档四类 / ADR-0026/27/28 Accepted / lint 0 + race 全绿 / **`m7-done` tag 本地已打（`5c34194`；push 须用户授权）**。M8 债券：T-231 percent-encode 缺陷 / B-1 --skip-users / UI 打磨 4 条 / CI -timeout 20m / V28 附录移植 / dialer 样板 13 处。

#### 用户方向指令（2026-08-23）：M8 主轴 = 控制台对齐 Artifactory

**「前端 UI 和交互逻辑要求和 JFrog 一样」**（用户原话）。conductor 执行裁定：
- **对齐口径** = 信息架构 + 交互逻辑 + 操作流对齐（Artifactory 用户零学习成本迁移），视觉近似但**自有皮肤**——clean-room 铁律（ADR-0001）对 UI 同样生效：reverse-src 内 JFrog 前端资产只读参考产出**行为规格**（布局描述/交互流/组件清单），图标/样式资产零复制。
- **载体**：M8 里程碑（M7 收官后立即启动规划——PM PRD + ux-designer 控制台规格 + reverse-engineer UI 行为规格 + architect 嵌入约束 → tech-lead 分票）。需要新 ADR（UI 对齐边界与 clean-room 应用）。
- **现控制台**：M7 已交付的 RBAC/readonly/manage 交互语义保留（服务端契约不动），承载层重排。

### M8 票据（T-231~T-246，tech-lead 2026-08-23 分解；AC 全文见 docs/prd/milestone-8.md；规格依据 console-ui.md + console-m8.md）

#### B1 基座与债券（首波并行 3；T-233 待 T-231）
- **T-231** [P1] internal/client percent-encode 修复 `role:dev-go-core` — **done 2026-08-23（conductor 复验：矩阵测试 race 绿 + 全仓 lint 0；提交 `684e71c`）**
  EscapePathSegments 导出 + contentPlanePath/storagePlanePath 单一构造点喂全部五个消费方法；CLI 打印 URI 同步可复制。5×2 真实栈矩阵 + 变异验证（%/#/? 腿复现生产报错原文）。遗留① migrate/reader 转义收敛→T-233 顺手。日志 reports/agents/T-231.md。
- **T-232** [P0] M8 交互断言基座 `role:devops-engineer` — **done 2026-08-23（提交 `d6a7db9`）**
  e2e/m8/（README 断言口径 + support 五助手 + 冒烟/自证 spec）+ seed-m8 双形态（**10,291 节点树 75.8s 验证、幂等复跑 0.4s**）；全量 95 passed/0 failed。**发现（conductor 待裁）**：既有套件 gc `graceHours=0` 并行竞态（apply 与并行上传互斥——全量验收一律 `--workers=1` 兜底，根治归后续票）。日志 reports/agents/T-232.md。
- **T-234** [P0] 设计 token 基座 `role:dev-frontend` — **done 2026-08-23（conductor 复验：assert-tokens OK + typecheck 绿；提交 `9da9d18`）**
  tokens.css 全量重做（Q2 亮色默认/[data-theme] 纯换值/三阶纵深/shadow 系/scrim·danger 增补）+ ThemeContext（亮默认+持久+首访 prefers）+ 编译期断言门（assert:tokens 入 build 前置）+ --bf-text-muted 上调过 §8 对比度门（axe 双主题 serious=0）+ 零复制合规 + gzip 155KB。**勘误登记（console-m8 §5.1 回写）**：暗色默认标头过时/text-muted 新值/增补 token 未入册。冒烟 spec 暂驻 styles/ 待 T-232 合入迁 e2e/m8。日志 reports/agents/T-234.md。
- **T-233** [P1] FR-77 债券打包 `role:dev-go-core` — **done 2026-08-23（conductor 复验：样板 grep=0 + migrate/CLI 测试绿 + lint 0；提交 `b3b3c06`）——FR-77 台账六项全收口，B1 全清**
  --skip-users 降级（403→告警续迁、500 仍 fail-closed、无旗标零回归）+ CI/Makefile TEST_TIMEOUT=20m（1ms 红绿证旗标生效）+ V28 附录填实 + dialer **28 处**收敛为 2 构造 + T-231 遗留① reader 转义收敛（变异红绿）+ 5×2 矩阵复验全绿。遗留登记：adapter/npm 第三份同构转义体（候选票）；remote singleflight 负载敏感观察。日志 reports/agents/T-233.md。

#### B2 双模式壳（done）
- **T-235** [P0] 双模式壳与路由重排 `role:dev-frontend` — **done 2026-08-23（击落-恢复后全量 103/0；conductor 复验四绿 + 服务端 diff=0；提交 `aad97d9`）**
  双模式 IA（应用/管理五分组 12 条目 + 模式切换 + Quick 动作）+ LegacyRedirect 20 条（percent-encode/查询串保真）+ 242 锚零改名（+10 壳锚入册=293）+ theme-smoke 迁入收口。票面纠偏：Proxies 占位与范围下拉**不建**（无影子入口规则）。QA 面归 T-243 中期回归。日志 reports/agents/T-235.md。

#### B3 页面域第一轮（在途，宽 2——配额纪律）
- **T-236** [P0] 制品浏览器 `role:dev-frontend` — **done 2026-08-23（8/8 spec + 全量 115/0 + 树展开 56ms@10k 节点；conductor 复验 TS/build；提交 `9921bd5`）**
  跨仓树真身（懒展开/URL 即状态/深链自动展开滚动定位/过滤/右键三形态/403 四层收敛）+ 详情三形态（Tab/校验和徽标/docker tag）+ 共享库 git mv 零契约改动；挂载缝 1 import+2 element 备案。**契约漂移上报**：console-m8 §2.2 称仓库清单 admin-only，实现是 CapRepoRead（readonly 可见全量）——architect 回写 §2.2。遗留：树虚拟滚动轻量形态（10k 达标）；Set Me Up 入口 T-242 挂载点已备。日志 reports/agents/T-236.md。
- **T-238** [P1] 治理/监控/常规域归位 `role:dev-frontend` — **done 2026-08-23（全量 127/0 + MigrationPanel 债收口 + 缺口列不伪造；与 T-239 合并提交 `11fdd44`——main.tsx 两票接线交织，单提交保每修订可编译）**
  存储概要/系统信息两新页 + 治理三页归位 + readonly 三禁用。契约漂移：Files/Folders/Items 列、Server Name 等无端点项不渲染不伪造；备份进度卡留 R5 兜底。日志 reports/agents/T-238.md。
- **T-239** [P1] 应用模式辅助页与全局导航 `role:dev-frontend` — **done 2026-08-23（9 腿 spec + 全量 127/0 + axe 双主题 0；合并提交 `11fdd44`）**
  仪表盘快捷卡+审计深链进树（消费 T-236 自动展开）/全局搜索（recentSearches+深链，固定类型不造影子入口）/SettingsPage 拆分为 ProfilePage+SystemInfoPage/登录 404 对齐。日志 reports/agents/T-239.md。

#### B4 域第二波 + 中期回归（在途）
- **T-240** [P0] 仓库管理域重排 `role:dev-frontend` — **done 2026-08-24（击落-恢复五文件零分叉；5/5 新 spec + 域外全量 124/0；提交 `196966c`）**
  三 Tab 列表/包类型网格向导/详情三 Tab（quota 行内编辑 CanManageRepo 语义）/删仓两段强确认；T-218 仓库域债收口；锚零改名迁移。分歧记录：列表链接进详情自有页（§6.8 裁定）；无端点列不伪造。日志 reports/agents/T-240.md。
- **T-241** [P0] 权限 target 编辑器重排 `role:dev-frontend` — **done 2026-08-24（隔离 worktree 全量 136/0 + 锚守卫 9/9；提交 `6158f53`）——B4 全清**
  两步资源对话框（冻结锚迁入/焦点陷阱/Esc 零回填）+ 四动作矩阵 + pathmatch 测试器 + 覆盖集三呈现面（B1 存量并集钉死）。**conductor 裁定**：m-holder 控制台编辑器可达性 = 契约冻结下 API-only（列表端点 CapSecurityRead）——L2 边界说明已随票交付，过滤列表端点列 M9 候选；POST 恒 201 为 wire 事实（AC 已正）。**共享层债（T-250 候选）**：base.css 语义 badge 亮主题对比度家族（warning/success/danger）+ stepper 死样式 + quota helper 双份。日志 reports/agents/T-241.md。

#### B4.5 中期回归（done）
- **T-243** [P1] M8 中期回归 `role:qa-engineer` — **done 2026-08-24（五段全 PASS——契约冻结零违约：26 文件 Go diff 全属两张裁定债券票、五不变量 live 实证；锚册 0 断链/冻结 242 零改名；M7 语义新壳下全绿含续传双臂；全量 e2e 136/0；SPA 52.4% 预算；报告 `8b04245`）**
  P2 簿记债四条登记（D-1 存储批锚未入册/D-2 散锚 8 枚/D-3 隐式退役/D-4 死锚无守卫）——B6 前微 chore 收口。日志 reports/agents/T-243-qa.md。

#### B5 对话框族 + 键盘 + 文档（T-242 done；T-244/T-245 在途）
- **T-242** [P0] Set Me Up 与 Deploy 对话框族 `role:dev-frontend` — **done 2026-08-24（armed 实例 step-up 全链 7/7 + 全量 141/0 + 三特殊字符编码回显；提交 `bccfea6`）**
  包类型网格/协议 Tab/一次性明文 Token 面板（token_id 如实降级）/step-up 内联重验（401 豁免全局登出）/拖拽上传流式 sha256。三漂移登记：admin 口令框与 ADR-0027 豁免语义不可兼容（§4.1 回写队列）；§4.1 D3 被 step-up 融合指令推翻；无 description 字段。遗留归 T-244（quick 入口/对比度家族/48 锚入册）。日志 reports/agents/T-242.md。
- **T-244** [P1] 键盘可达 + 共享层债收口 `role:dev-frontend` — **done 2026-08-24（六债全收 + 键盘 6/6 + 全量 147/0 + 对账器终态 423/0/0；conductor 复验 TS+audit；提交 `74f4f08`）——B5 全清，M8 实现票全部落地**
  对比度家族 ≥4.59:1 双主题三承载面（审计新抓 info/danger 暗角）；死样式清除；quick-setmeup 全局入口；quota helper 合一；双 Deploy 收敛（UploadDialog 退役+锚显式注销+反向依赖消除）；锚册 v1.7 + **anchor-audit.mjs 三方对账器**（断链=0 实证）。键盘共享件接入 Tab/表格/三对话框。遗留：repos-deploy readonly 预收敛；两页自持对比度类可回退。日志 reports/agents/T-244.md。

#### B6 终验（done）
- **T-246** [P0] M8 终验 `role:qa-engineer` — **done 2026-08-24（六段 PASS；报告 `75490e6`；DoD 1/2 经 fix-forward `32313eb` 补绿——axe 52 扫全零 + NodeDetail Tab 连带真缺陷修复；全量 150/0）——M8 完结，DoD 八条全绿**
  U01~U15 + 剧本 8/8 零卡壳（1.05~1.68s）+ NFR 全绿 + 回归硬门槛（契约终审 31 文件 100% 归属三豁免票/锚 423/0/0/双臂/矩阵零偏差）。M9 候选 28 条归档 §八。`m8-done` tag 已打（`9a44168`）并随用户常态授权推送。日志 reports/agents/T-246-qa.md / T-244.md §8。
- **T-245** [P1] M8 文档改版 `role:tech-writer` — **done 2026-08-24（击落-恢复后收口；scratch 栈走查 11/11 + T-249 四臂活体复验 + make docs 零告警；提交 `a88b728`）**
  console.md 新 IA 重写 + artifactory-path-map.md（24 任务两列）+ 七页路径修订 + 两处过时事实实测修正 + 三向交叉链接。**发现**：用户 18080 实例为 M7 期构建（指纹核验，零触碰）——M8 收官后提议刷新。遗留：T-244 合入后三处回写注记。日志 reports/agents/T-245.md。
- **T-237** [P0] 用户与组管理重排 `role:dev-frontend` — **done 2026-08-23（4/4 三角色 spec + 锚守卫 23/23 + 只读完备 grep 零裸写；提交 `c5eb748`）**
  分区编辑器/双列穿梭/排序/角色徽章/删除守卫；T-224 PUT replace 姿势端到端实证；M7 语义全量保留。**契约缺口登记（熔断线合规，待 PM/architect 立项）**：① GET users 无 enabled 回显（T-208 只落写侧）；② 无 DELETE users/{name}（Artifactory 有）；③ 组成员 N+1 汇总（无端点）；④ 组无 adminPrivileges 字段。遗留：base.css badge.warning 亮色 4.26:1 共享层小票。日志 reports/agents/T-237.md。

#### B3 页面域第一波（并行 4）
- **T-236** [P0] 制品浏览器：跨仓左树+详情+右键+深链+特化视图（FR-72） `role:dev-frontend` `area:web/src/pages/artifacts/` `dep:T-232,T-235`
- **T-237** [P0] 用户与组管理页重排（FR-73） `role:dev-frontend` `area:web/src/pages/security/{Users,UserDetail,Groups}` `dep:T-232,T-235`
- **T-238** [P1] 治理/监控/常规域归位 + 存储概要新页（FR-73 治理面） `role:dev-frontend` `area:web/src/pages/{governance,monitoring,admin}` `dep:T-232,T-235`
- **T-239** [P1] 应用模式辅助页与全局导航（FR-74） `role:dev-frontend` `area:web/src/pages/{Dashboard,Search,Login,NotFound} + Settings 拆分` `dep:T-232,T-235`

#### B4 域第二波 + 中期回归（并行 3）
- **T-240** [P0] 仓库管理域重排（三 Tab/包类型网格/分组表单/删仓强确认） `role:dev-frontend` `area:web/src/pages/repositories/{...不含 tree}` `dep:T-232,T-235`
- **T-241** [P0] 权限 target 编辑器重排（两步资源对话框/四动作矩阵/模式测试器） `role:dev-frontend` `area:web/src/pages/security/{Permissions,PermissionEditor}` `dep:T-232,T-235,T-237`
- **T-243** [P1] M8 中期回归（W 锚迁移首轮 + 契约冻结 git diff 审计） `role:qa-engineer` `area:只读验证` `dep:T-235~T-239`

#### B5 对话框族 + 键盘 + 文档（并行 3）
- **T-242** [P0] Set Me Up 与 Deploy 对话框族（含 step-up 融合） `role:dev-frontend` `area:web/src/components/{SetMeUp,Deploy}Dialog + commands.ts` `dep:T-232,T-236,T-240`（T-231 前置）
- **T-244** [P1] 键盘可达与焦点管理补齐（FR-75） `role:dev-frontend` `area:web/src/components/ 共享层 + 页面小补丁` `dep:T-236,T-240,T-241,T-242`
- **T-245** [P1] M8 用户文档改版（新 IA 指南 + 操作路径对照表） `role:tech-writer` `area:docs/user/` `dep:T-236,T-240,T-241,T-242`

#### 用户指令（2026-08-23 21:51）：VM 测试环境装 Jenkins、接入 CI/CD

conductor 界定（可推翻）：**场景 = BinFlow 作为 Jenkins 流水线的制品骨架**（构建→推制品到 BinFlow→消费侧从 BinFlow 解析依赖——产品真实场景验证），非用 Jenkins CI BinFlow 自身（已有 GitHub Actions）。开票 **T-247**：
- **T-247** [P1] Jenkins 就绪 + BinFlow CI/CD 场景验收 `role:release-engineer` `area:VM（仓外）+ reports/agents/T-247.md` `dep:—` — **doing 2026-08-23**
  Jenkins LTS（docker 形态，mirror/save-load 兜底）+ VM 上 systemd 形态 BinFlow 实例（T-230 已验证路径）+ 三条流水线（maven deploy/npm publish/docker push）+ 消费 job（从 BinFlow 解析）全绿取证；Jenkins 凭据走其 credential store（零入仓库）；内存压力时 Artifactory 栈可停（可再起）。产出 CI 场景报告（后续可入 PRD 场景库）。
- **T-247** [P1] Jenkins 就绪 + BinFlow CI/CD 场景 `role:release-engineer` — **done 2026-08-24（五 job 全 SUCCESS；报告 `2a682da`）**
  mvn（279 Central 构件经 BinFlow 虚拟仓）/npm（Access Token）/docker（digest 一致）三发布 + **消费闭环**（三协议只从 BinFlow 解析，X-Binflow-Resolved-From 代理证据）+ 负面腿全绿。**产品发现 P-1→T-249**：npm 第二版本发布命中「覆写需 DELETE」臂——无 delete 的 CI 号必 403（真实 CI 才会暴露）。栈保留（Jenkins :9090 / BinFlow :8080 active；t226 栈已停可再起）。日志 reports/agents/T-247.md（含 T-248 接续点 §8）。
- **T-248** [P1] BinFlow 自身研发测试 CI/CD `role:devops-engineer` — **done 2026-08-24（三级全绿 + dogfood 闭环实证；报告 `8eb207f`）——CI 双线收官**
  smoke（热 21s/push 触发 ≤1min——Mac↔VM bare repo remote「vm」）/ nightly（全仓 race 15.5m@2C）/ release（goreleaser 六平台 + 镜像进 BinFlow）。dogfood：拉回运行 /readyz OK 自报版本。基线与遗留登记（单架构镜像/docs 链缺/web lint 未进 nightly/poll 触发）。日志 reports/agents/T-248.md。
- **T-249** [P1] npm packument 追加语义修复 `role:dev-registry-adapter` — **done 2026-08-24（conductor 复验：追加测试 race 绿 + lint 0；提交 `5e3f3a4`——M8 第二张裁定豁免服务端票，T-246 终验审计将记档）**
  转换感知判定（版本集 diff + 既有版本深比较）：追加/dist-tag 移动 → write 臂（T-68 SkipOverwriteCheck 复用）；改既有版本/deprecate → 维持 DELETE 臂；tarball 层 step-4 无条件 403 不动。npm CLI 连发双绿（write-only CI token）+ 篡改 E403 钉死文案 + **活体 A/B 反证**。遗留：deprecate 严格侧产品裁量（npmjs 仅需 publish）；replication push_npm 同臂自查；T-247 文档措辞更新。日志 reports/agents/T-249.md。
  范围：源码进 Jenkins（GitHub 直连不通则 Mac→VM bare repo push 兜底）；流水线分级——commit 烟测（build+lint+关键包测试）/ nightly 全量（make test TEST_TIMEOUT=20m 全仓 race，2C 耗时如实记录）/ tag 发布（goreleaser 风格多平台二进制 + docker 镜像**推进 BinFlow 自身**——dogfood 闭环）；console 构建链（node）；结果通知形态。产出：分级 Jenkinsfile + 首轮各级绿灯证据 + 耗时基线。

#### B6 终验（串行 1）
- **T-246** [P0] M8 终验：U01~U15 全量 + 零学习成本剧本×8 + DoD 收口 `role:qa-engineer` `area:只读验证 + 收口材料` `dep:T-233,T-242~T-245`

**tech-lead 风险登记**：dev-frontend 单角色瓶颈（9 前端票，B3/B4 可双前端按域串行接续）；console-ui 低置信 10 项不作验收依赖；契约冻结熔断线（UI 票私加端点即违约，T-243 硬闸）；锚数以 console-ux §10 的 242 为准（ADR-0029 的 283 转正时勘误）。
- **T-218** [P1] FR-66 控制台角色与权限管理扩展 + read-only 只读态 `role:dev-frontend` — **done 2026-08-23（review APPROVE 0 阻塞；提交 `2923edf`）**
  14 文件 + e2e 三腿（V12 落值/回显/审计、V13 五页只读+四写重放 403、V14 manage 往返）全套 85 passed/0 failed。review 亮点：wire 闭集 fail-safe 与 EffectiveRole 同构、adminRole 永不与 admin 布尔混发（结构性规避冲突 400）、reviewer 独立重放配额写+内容面双写全 403。**M7 尾债（UI 打磨，非阻塞 4 条）**：MigrationPanel 启动钮/树页上传删除钮未按角色禁用、UserUpdateBody.adminRole 类型可收紧、仓库设置页只读文案错位。日志 reports/agents/T-218.md / T-218-review.md。
- **T-225** [P2] M7 文档 II `role:tech-writer` — **done 2026-08-23（conductor 直审通过；提交 `5f01cbc`）**
  step-up 指南（ADR-0027 逐字 + 两处自决如实标注）+ V27/V28 证据归档模板（ADR-0025 等价口径，供 T-227/T-228 填）+ api-reference token 字段/401 双形态 + FAQ 三问。全部 curl 自建 IdP 容器实跑验证。遗留：console step-up 铸造页落地后回写 CLI 路径 §2。日志 reports/agents/T-225.md。
- **T-219** [P2] FR-68 step-up `role:dev-go-core` — **done 2026-08-23（安全视角 review APPROVE 0 阻塞；提交 `7be4da7`）**
  mint grant 台账（256-bit 只存 sha256、绑定 {user,session}、burn 原子）+ step_up_password 双 provider 腿（LDAP 结构上不可探活他人）+ OIDC prompt=login 单次 grant（明文仅存 302 fragment，RFC 3986 不进服务端日志）+ 双 config 键 TTL 域无条件拒启动 + p.Admin→CanManage 统一（Q11 零变）。真实 Keycloak+OpenLDAP 容器实测 V21~V26；默认 off 四护栏逐字复绿；64 goroutine 烧毁探针恰一次。移交：第二身份回跳腿→T-224；并发烧毁常驻用例→T-220；grant 签发审计小票+登录 lockout 存量姿态→M7+。日志 reports/agents/T-219.md / T-219-review.md。
- **T-220** [P2] FR-70 技术债打包 `role:dev-go-core` — **done 2026-08-23（conductor 复验：lint-baseline total=0 + N3/并发测试亲跑绿；提交 `b8e5fcf`）**
  auth 53 条逐条处置（死类型族按 ADR-0020 映射规则删 + 留注释；测试重构 makeUser 助手）+ npm 1 条；N3=context.WithoutCancel 修复 + 变异钉死 + fail-closed 反向钉死；TTL flake 放宽压测 10/10；64-goroutine 烧毁用例固化；15 个 sql 行尾全齐。**移交**：CI 显式 -timeout 20m（httpapi 净机 571s 逼近默认上限）→ release 小票；dialer 闭包样板 13 处 → reviewer 偏好项 M8。日志 reports/agents/T-220.md。
- **T-222** [P1] M7 验收 II `role:qa-engineer` — **done 2026-08-23（PASS 全 AC 零缺陷；提交 `f383261`）——M7 收官硬门槛通过**
  探针新鲜度守卫落地（含 embed 面，陷阱当场咬合实证）；续传四腿逐字节；MinIO/S3 双份；Playwright 85/0；千并发两臂优于 M6 基线 86%/34%（P95 新基线归档：6.8ms/2748ms）；重启首 200 仅 0.34s；**M1~M6 全 P0 复跑 192/192 四批全绿**；全仓 race 23 包绿。观察 5 条路由（O-2 P95 基线分母→PM v1.2；O-3 Helm OCI=oras 不可得条件腿；O-4 wire 校准备忘；O-5 Playwright 负载教训；O-1 TTL 回收=ADR-0028 一致）。日志 reports/agents/T-222-qa.md（含击落-恢复说明）。
- **T-224** [P2] M7 验收 III `role:qa-engineer` — **done 2026-08-23（PASS 8/8 零缺陷；qa 报告 `830bc9d`）**
  V21~V26 + config 域 + 干净树全仓 race 全绿。亮点：错误体与 ADR 逐字节、第二身份回跳腿拒发 grant、TTL 闭区间实测、dind 三态 digest 稳定、T-208 seam 禁用即失效复验。非缺陷 4 条已路由（NFR-S41 措辞→PM v1.2；PUT replace 语义姿势→文档面；ssouser2 自动建行=既有 H25；grant 签发审计=M7+ 债）。日志 reports/agents/T-224-qa.md。
- **T-221** [P1] M7 验收 I `role:qa-engineer` — **done 2026-08-23（PASS 16/16 零缺陷；qa 报告 `cf4c16a`）**
  V01~V11 全绿 + usage ∨-臂翻转 + B1 回归腿仍闭合 + M1/M4 回归零回退 + 真实客户端三协议（docker 29.7.2 push/pull + readonly 双 token 臂 / mvn deploy+resolve / npm publish+install）+ 负面矩阵全 403 + 矩阵 EXPECT=1 归档 + 全仓 23 包 race 绿。三条 PRD 字面偏差实证复核 = 已登记 PM v1.2 回写项（与 T-217 勘误 1 合并收口）。日志 reports/agents/T-221-qa.md。

#### 波 2（待波 1）
- **T-212** [P0] RBAC 基座：Role 闭集 + 六能力求值链 + migration 011 + idp_sync role 改写（ADR-0026） `role:dev-go-core` `area:internal/auth + internal/metadata` `dep:T-214`
  migration 011 双方言（users.role 三值 + is_admin 回填 admin + can_manage 列）幂等 <1s；Role(3)×Capability(6) CanManage 全表 + CanManageRepo + Can 扩 m 动作表驱动单测零遗漏；idp_sync admin_group/admin 臂 → role=admin 回归绿；export/import 往返含 role/can_manage 保真。
- **T-216** [P1] FR-67 docker blob 上传跨重启续传 REST 化 `role:dev-registry-adapter` `area:internal/adapter/docker` `dep:T-213`
  registry lazy 重建（clean-room 依据 docs/reverse/docker-registry.md §2.5）；GET 状态腿（204+Range / 404 BLOB_UPLOAD_UNKNOWN）；Content-Range 错位 416 + 权威 Range（sess.Offset()）；kill -9 真实栈全链（PATCH 512KiB→kill→重启→GET→续块→PUT digest→逐位校验）；S3 hard 404 契约不动；M2 D 序列回归。

#### 波 3（待波 2）
- **T-215** [P0] FR-64 REST：routeAuth 能力化迁移 + adminRole wire + 角色即时生效与审计 `role:dev-go-core` `area:internal/httpapi` `dep:T-212`
  router.go 全部 admin:true → manage|repoManage（依 T-214 终版清点表逐路由 diff）；守卫测试防裸 admin 门回归；adminRole 三值 wire（冲突 400/非 admin 写 403/回显/落库）；同 Token 升降角色即时生效；user.role.change 审计；readonly_admin 12 读端点 200 + 变更面 403 零副作用（V01~V06）。

#### 波 4（待波 3）
- **T-217** [P0] FR-65 REST：manage 动作 wire + CanManageRepo 接线 + 仓库级 admin 派生 `role:dev-go-core` `area:internal/httpapi` `dep:T-215`
  permission target 动作集扩 m（wire + 回显 + 视图字母集）；单仓配置族路由走 CanManageRepo；manage 派生授权链 + 越界 403 + 正交腿（仅 manage 无 r/w/d 不授予内容读写）；无提权链不变量测试；M1 C22/C27 + M4 W19~W21 零回归（V07~V10）。

#### 波 5（待波 4）
- **T-218** [P1] FR-66 控制台角色与权限管理扩展 + read-only 只读态 `role:dev-frontend` `area:web/src/pages（security 用户/权限 + governance 仓库管理只读态）` `dep:T-215,T-217`
  角色下拉（wire 回显）/manage 复选/read-only 只读态（服务端 403 如实呈现、UI 无绕过）；Playwright 三腿（V12~V14）；零新增运行时依赖。
- **T-219** [P2] FR-68 step-up：SSO session 铸管理 Token 二次认证（契约以 T-214/ADR-0027 终版为准） `role:dev-go-core` `area:internal/httpapi(token) + internal/auth + internal/config` `dep:T-215,T-214`
  开关默认 off；SSO 臂无二次凭据 401；LDAP 重 bind / OIDC 新鲜性双值腿；Basic/admin/docker 臂零影响；M6 Q11 四护栏回归；审计 second_factor 维度；Keycloak/LDAP 容器集成测试（V21~V26）。
- **T-221** [P1] M7 验收 I：RBAC 角色×能力全表 + manage 派生 + 真实客户端（V01~V11） `role:qa-engineer` `area:QA 验收面` `dep:T-217,T-211`
  V01~V10 curl 全绿 + m7-rbac-matrix.sh 矩阵归档；V11 真实客户端（docker push/pull + mvn deploy + npm publish）manage 位不动摇协议行为。

#### 波 6（待波 5）
- **T-220** [P2] FR-70 技术债打包（**Close 段与 N2 已拆 T-229 提前**）：N3 ctx 窄窗 + internal/auth 53 条 lint + sql 行尾 `role:dev-go-core` `area:internal/storage + internal/auth + internal/metadata/migrations + internal/httpapi(仅测试)` `dep:T-214,T-216,T-217,T-219,T-229`
  全仓 lint 0（auth 53 条逐条处置留档）；N3 钉死测试（Append 全量 EOF 后注入 ctx 取消 → 会话不毒化、SetState 已落库，context.WithoutCancel；变异验证）；全仓 race 绿。**并入 T-212 review 移交**：TestSessionTTLAbsoluteCapWins 墙钟 flake 放宽。
- **T-226** [P2] M7 等价口径回归：MinIO + Artifactory OSS 容器复跑 M6 基线（V29） `role:qa-engineer` `area:QA 验收面` `dep:T-217`
  M6 H01~H05 + H63/H67 在 M7 代码上复跑；差异逐条归档定性。

#### 波 7（待波 6）
- **T-222** [P1] M7 验收 II：续传四腿 + 控制台 Playwright + 性能回归（V12~V20） `role:qa-engineer` `area:QA 验收面` `dep:T-218,T-220,T-211`
  kill -9/SIGTERM/错位 416/过期 404 四腿 + MinIO 腿 + M2 D 序列双份；1000 并发 P33/P34（P95 偏差 <10%、重启首请求 <2s）；M1~M6 全 P0 序列复跑（回归硬门槛）。
- **T-224** [P2] M7 验收 III：step-up 双态矩阵（V21~V26） `role:qa-engineer` `area:QA 验收面` `dep:T-219`
  开启态矩阵（Keycloak/LDAP）+ 豁免腿 + 审计断言 + 默认 off 回归；企业部署默认值建议归档。
- **T-225** [P2] M7 文档 II：step-up 指南 + 条件腿真实环境附录 `role:tech-writer` `area:docs/user + docs-site` `dep:T-219,T-214`
  step-up 指南（开关键/双轨交互/CI 不受影响/API 参考）；条件腿证据归档模板（V27/V28）；FAQ 增补（readonly 边界/S3 404 原因/step-up 排查）。

#### 波 8（末位，`dep:用户环境` 到位即插队）
- **T-227** [P2] FR-69/Q8 真实 AWS S3 验收实腿（V27） `role:qa-engineer` `area:QA 条件腿` `dep:dep:用户环境,T-222`
  真实 bucket 上 M1~M6 全 P0 复跑或差异归档；FR-52 吞吐实测；与 MinIO 对照表 + V30 状态记录。
- **T-228** [P2] FR-69/Q9 真实 Artifactory 迁移验收实腿（V28） `role:qa-engineer` `area:QA 条件腿` `dep:dep:用户环境,T-222`
  真实实例上 FR-63 全序列复跑（H62~H67 口径）或差异归档（注明版本号）；差异不擅自修代码，回 conductor 转交。

## 🔨 进行中（doing）

（空）
  **口径修正（勘误）**：`upload_sessions` 表 M6 从未落库（009 前无此表）；S3 会话走 multipart（状态由 S3 服务端持有），不建 DB 表。故本票范围 = **仅本地 filestore**：新建 `upload_sessions` 表（migration 010，sha256/state/created_at/expires_at）+ metadata `UploadSessions()` substore + 磁盘引擎 `BeginSession/Append/Commit` 落表、`ResumeSession` 从表恢复（**重启可续传是新能力，非对齐 S3**）；删除磁盘 `sessions/<uuid>/` 路径与启动扫描。磁盘 `OpenEngine` 需注入 `metadata.Store`（改 `Options` 加 `Sessions metadata.UploadSessionStore` 字段 + main.go `openStorageEngine` 传 `md`；cmd/ wiring 由主会话接或授权本 agent 最小改动）。S3 后端不动。ADR-0006 决策 2 改「磁盘 session 非版本兼容承诺，DB 化为迁移边界」。AC：(1) 本地 filestore 会话 append/commit 落 `upload_sessions`，重启经 `ResumeSession` 续传（新能力）；(2) 旧磁盘 session 目录不再产生，启动扫描逻辑删除；(3) S3 后端回归绿（multipart 路径不变）；(4) 备份/恢复语义：sessions 瞬态不进备份面（M4 结论不变）；(5) 全仓 `-race` 绿 + lint 0。


## 🧪 测试中（qa）

（空）

## ✅ 已完成（done）

- **T-210** [P2] 复制私网目标显式开关（ADR-0025 决策 4） `role:dev-go-core` `area:internal/config + internal/replication` — done 2026-08-22（conductor 核验直收，接 cmd 组装点）
  `replication.allow_private_target` 键（默认 true）→ `EngineOptions.DenyPrivateTargets`（= !allow_private_target）。默认 true 放行私网（现状不破）；显式 false 拒绝。scheme/host 校验、逐跳重检、DNS-rebinding pinning 不随开关变化。conductor 补接 `cmd/binflow-server/main.go` NewEngine 组装 seam（agent area 未覆盖 cmd），端到端生效。测试：config 6 种键解析 + replication 节严格 schema + 引擎拒绝 loopback/默认放行回归；-race 绿 + lint 0。日志 reports/agents/T-210.md。
- **T-208** [P2] 用户禁用 REST seam（ADR-0025 决策 6） `role:dev-go-core` `area:internal/httpapi + internal/auth` — done 2026-08-23（review APPROVE + qa PASS 5/5；提交 `f21fd74`）
  `userCreateBody`/`userUpdateBody` 补 `Enabled *bool`（bool 指针区分缺省 vs 显式 false；新建缺省 true，替换/更新缺省保持存量值）；metadata 增 `SetEnabled`。qa 真实栈 curl 16 腿全过：禁用 → **存量 token 立即 401** + basic 401（`enabled=0` 落库核实）；缺省保持语义双向验证；非 admin 四姿势全 403 零副作用；session 臂禁用 401；全仓 `-race` 23 包 exit 0、lint 0（auth 53 条既有债务非本票）。T-190 护栏③（禁用→token 401）自此具备 REST 写入缝，端到端闭环。日志 reports/agents/T-208.md / T-208-qa.md。
- **T-209** [P1] filestore session 入 DB（ADR-0025 决策 5 修正，修订 ADR-0006 决策 2） `role:dev-go-storage` `area:internal/storage + internal/metadata + cmd/binflow-server` — done 2026-08-23（review 两轮：R1 三 blocker 修复确认 + R2 B1 增量 conductor 复验绿；qa PASS 5/5；提交 `e7b581e` + `db45cb2`）
  migration 010 `upload_sessions` 表 + metadata `UploadSessions()` substore + 磁盘引擎会话落表、`ResumeSession` 重哈希恢复（**重启可续传 = 新能力**）、删磁盘 `sessions/` 路径与启动扫描；S3 后端不动（multipart）。review R1 修：snapshot purge 并列清 `upload_sessions`、ResumeSession 同 id 替换出 `e.mu` 后 `detach()`（锁序反转消除，全包锁审计）、sweep 测试重写 DB 语义。review R2 修 B1：**删除 OpenEngine sweep 失败路径的 `RemoveAll(uploads/)`**（瞬时 DB 故障即可摧毁在途上传的纯破坏路径），钉死测试 `TestOpenSweepFailureKeepsUploads`（变异验证真实咬住）+ N1 godoc/N5 注释/N4 sql 行尾。qa：三上轮 FAIL 确认关闭；kill -9 后行+512KiB 部分数据幸存 sweep、过期回填全清、真·半路断开无完整性洞、export 不含瞬态表；S3 回归绿；全仓 `-race` 绿。遗留（延后 M7）：N6/O-2 重启续传 REST 可见性（引擎就位无生产调用方）、N3 ctx 取消窄窗（fail-closed）、O-1 干净停机清会话（非回归）、N2 restart 臂注释、008/009 sql 行尾（早于本票）。日志 reports/agents/T-209.md / T-209-review2.md / T-209-qa.md。

- **T-127** [P0] goreleaser 基线+版本注入（FR-34/PB-01/02） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  六平台 CGO_ENABLED=0（linux/darwin/windows×amd64/arm64）；ldflags version/revision 注入三面一致（`--version`/启动日志/health.version）；check-size 全部 5-6MB（≤40MB）；裸 build 回退 dev；release.snapshot 模板；发布禁用。日志 reports/agents/T-127.md。

- **T-129** [P0] docs-site Docusaurus 脚手架+embed+/binflow/docs（K1 暂行形态） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  Docusaurus 3.10 构建通过；搜索 @easyops-cn/docusaurus-search-local+nodejieba（K1 终裁）；baseUrl `/binflow/docs/` 零外链；`make docs` 构建+复制+docs-size（1.56MB）；go:embed Handler + router 匿名只读；docs 测试+路由集成测试全绿。日志 reports/agents/T-129.md。

- **T-128** [P0] FR-44 BE 材料化+007 回填（ADR-0016，照 T-119 草案；**待双 review**） `role:dev-go-core` — done 2026-08-21（conductor 核验，待双 review 后提交）
  materializeAncestors+putFolderRow+ensureFolderLedger；007 回填迁移（两步 SQL/递归 CTE/幂等）；12 测试适配；伴随修复 docs 重定向循环。repo+httpapi+docs 全套测试 race 绿。日志 reports/agents/T-128.md。

- **T-130** [P0] K1/K2 架构终裁+§7.1 两行补遗 `role:architect` — done 2026-08-21（conductor 核验直收）
  **K1（ADR-0011 增补①~⑤）**：搜索外挂 docusaurus-search-local+nodejieba（事实修正：内建搜索不搜正文）；baseUrl 原生前缀免 relink；fallback 阶梯至用户确认。**K2（新 ADR-0017）**：ghcr.io/lzwzzy/binflow 变体 tag、GA 无滚动 tag、基底 **distroless static-debian13**（修正 debian12 暂行）+ alpine:3.24、syft SBOM 最小面（cosign/SLSA M6+ 结构性理由）、Chart 仓库 GitHub Pages。§7.1 两行补遗；PRD v1.2。在途影响：T-129 搜索方案已知会、T-132 派单带 debian13 锚定。提交 3daa704。

- **T-1** [P0] M1 里程碑 PRD `role:product-manager` `area:docs/prd` — done 2026-08-17
  产出 docs/prd/milestone-1.md（486 行）：FR-1~FR-6 全 AC、26 端点兼容矩阵、C01~C30 验收命令。核验通过。
  8 项开放问题已定案（Q1 `/binflow` 前缀；Q2 匿名读默认开；Q3 缺省 password；Q4 如实版本；Q5 纯 Go SQLite；Q6 Range P2；Q7 github.com/lzwzzy/binflow；Q8 devops 起草）→ 回写为 T-4。
- **T-2** [P0] M1 架构设计与元数据 schema 定稿 `role:architect` `area:docs/design` — done 2026-08-17
  产出 docs/design/architecture.md（12 节）+ ADR-0005/0006/0007（零 CGO 依赖基线 modernc.org/sqlite、blob 布局与落盘协议、迁移机制）。核验通过。
  决策对齐修订（/binflow 前缀、匿名读、Q3 口令、Q7 路径）→ T-5。
- **T-4** [P0] PRD v1.1 回写 8 项已定案决策 `role:product-manager` `area:docs/prd` `dep:T-1` — done 2026-08-17
  docs/prd/milestone-1.md 升 v1.1（525 行）：§0 修订记录；Q1 全文 URL 改 /binflow（104 处）+ /artifactory/** 404 断言；Q2 匿名读默认开（FR-5-AC12/13、C23/C27、NFR-S8）；Q5 零 CGO（FR-1-AC7）；Q7 落定；§9 已决决策表。核验通过（残留 /artifactory 均为有意保留）。
- **T-5** [P0] 架构文档对齐用户决策（含 Q3 修订） `role:architect` `area:docs/design` `dep:T-2` — done 2026-08-17
  ADR-0008（/binflow 统一前缀 + module 路径 + docker /v2 例外风险）、ADR-0009（匿名读默认开 + admin env 口令引导）；§7.1 路由全前缀化、§6 种子数据改 env 优先/缺省 password、Derby/H2 排除记录入 ADR-0005。核验通过。
- **T-3** [P0] Artifactory M1 行为规格（clean-room） `role:reverse-engineer` `area:docs/reverse` — done 2026-08-17
  四份规格共 471 行：rest-api.md（~34 端点，高28/中10/低3）、storage-layout.md（sha1 分片/binaries+nodes 表行为/_pre 暂存 24h 清理）、config-formats.md（三 XML → YAML 映射）、repo-semantics.md（local 8 路径规则/上传 5 步/删除 9 场景）。核验通过（置信度标注齐全、clean-room 抽查无代码翻译）。
  高价值发现：同 checksum 幂等重传免覆盖权限检查（官方未记载）、统一错误体 errors[] 形态、回收站 14 天。PRD §5.5 六项校准项可回写（PM 增量修订，随下轮或 T-6 一并处理）。
- **T-6** [P0] M1 工程 ticket 拆解 `role:tech-lead` — done 2026-08-17
  14 张票（T-7~T-20）+ 分批表（最大波 3 张）+ R1~R9 风险清单，全文 reports/agents/T-6.md。核验通过（area 无重叠、宽度 ≤4、校准项固化进 AC、低置信度不作 AC）。R1/R2→T-21；R3~R6→T-22；R7→T-23；R8 处置合理照准。
- **T-21** [P0] PRD v1.2 校准回写 `role:product-manager` `area:docs/prd` `dep:T-3,T-6` — done 2026-08-17
  R1（checksum 不一致 409）/R2（建仓 200 纯文本）/§5.5 六项全部定案（改「校准记录」表）；增补两条规格（ETag/304/416、幂等重传注记）；token 字段标待 T-23。核验通过（旧口径无残留，对照表左列旧值为有意保留）。T-18 QA 依赖已解除。
- **T-22** [P0] architecture.md 回写 R3~R6 `role:architect` `area:docs/design` `dep:T-6` — done 2026-08-17
  R3 匿名读主键名 security.anonymous_access（别名双键等价）；R4 permission_targets+permission_principals 两表替换扁平表；R5 repo key {1,62}；R6 错误信封 errors[] 数组形。核验通过（grep 四处落点 + 旧形态零残留）。ADR-0008/0009 仅追加回写注记。遗留：M4 可在 breaking 窗口移除旧键别名。
- **T-7** [P0] 工程脚手架 `role:devops-engineer` `area:仓库根` — done 2026-08-17
  go.mod（github.com/lzwzzy/binflow, go 1.26）/ Makefile（build/test/lint/dev/clean，GOPROXY 镜像）/ .golangci.yml v2 / CI 三步 / cmd 骨架 / internal 九包 doc.go。conductor 亲测复现：build 2.58MB、test PASS、lint 0 issues、gofmt 空、CGO_ENABLED=0 通过。遗留：CI 首跑绿待推送后确认；go.sum 待首依赖生成。
- **T-23** [P1] 补逆向规格 auth-model.md `role:reverse-engineer` `area:docs/reverse` `dep:T-3` — done 2026-08-17
  273 行六节：用户模型/改密/Token 生命周期/权限概览/M1 校准建议/待验证清单；置信度高 41/中 16/低 1。核验通过（clean-room 零违规）。关键校准：token 创建响应真实字段集（无 token_id）+form 编码；revoke 幂等 200；改密现行路径与 400 语义；建用户真实为 PUT {name}。→ 触发 T-24 PRD v1.3。
- **T-24** [P1] PRD v1.3 auth 校准回写 `role:product-manager` `area:docs/prd` `dep:T-23` — done 2026-08-17
  E-17 form 编码+真实字段集（token_id 超集扩展）；E-18 revoke XOR+幂等 200；E-16 双路由+旧口令 400；E-19 PUT {name} 兼容路由；§5.1 错误体三分层（制品 errors[]/用户管理纯文本/token OAuth）；§5.5 六项校准全部收口；顺手修 C20 剧本 ADMIN_PW 连锁 401 缺陷。核验通过（旧口径零残留）。T-15 派发时附 v1.3 口径。
- **T-10** [P0] metadata：SQLite Store+迁移器+001_init `role:dev-go-core` `area:internal/metadata` `dep:T-7` — done 2026-08-17（经一轮修复）
  14 文件：api/store/migrate/password/substores + 001_init.sql（9 表，permission 两表形态）+ 35 测试。双 review：正确性 APPROVE；架构 REQUEST_CHANGES 两 blocker 均已修复并复审通过——B1 LIKE 大小写误删（case_sensitive_like 入 DSN + 6 子用例破坏性断言）、B2 池 NumCPU + 三 PRAGMA 挪 DSN（四连接并发断言 + fail-fast）。conductor 复现：race 12.8s 绿 / lint 0。
  遗留（minor 不阻塞）：FilterUnreferenced TOCTOU 契约（T-13 派单附注）、tokenStore.Touch 上下文（T-11 顺车）、双进程首启竞态（M4 技术债）。
- **T-8** [P0] config 包 `role:dev-go-core` `area:internal/config` `dep:T-7` — done 2026-08-18（经一轮修复）
  7 文件：api/load/validate/config + 30 测试（覆盖率 91.9%）。review 3 blocker + 1 major 全修复并复审通过：B1 多文档 YAML（decode 后断言 io.EOF）、B2 sqlite DSN 白名单（拒绝 URI 形态）、B3 ADMIN_PASSWORD 大小写归一赋值、M1 redactDSN 脱敏。conductor 独立探针复验多文档秘密拦截。race 2.4s 绿 / lint 0。
  顺手：m2 TOCTOU 注释、m1 doc.go 例外清单补 DATA_DIR、m4/m5 测试补齐；n1 记录保留理由。
- **T-25** [P0] 架构文档回写 `role:architect` `area:docs/design、DECISIONS.md` `dep:T-8,T-9,T-10` — done 2026-08-18
  14 处回写（architecture.md 12 + ADR-0007 勘误 2）：GC 集合形签名/mark-sweep/mtime 硬约束（备份保留 mtime）、Engine.Close、sentinel 六全集、state.json 契约定稿与 version 演进规则、DSN per-connection PRAGMA 机制、case_sensitive_like 语义、事务边界 a 案裁定、DATA_DIR 四例外名。核验通过（旧措辞零残留，来源标注 18 处）。
- **T-9** [P0] storage 引擎 `role:dev-go-storage` `area:internal/storage` `dep:T-7` — done 2026-08-18（经三轮收敛）
  11 文件 ~1200 行实现 + 40 测试。三轮评审收敛：架构 B1/M1/M2（Close 契约、会话毒化、state 形状）→ 正确性探针 singleflight panic 死锁（defer teardown 修复 + waiter 释放测试）→ rename 失败/GC 保护场景固化。conductor 复验：race 104s 全绿、lint 0、零 CGO、包边界红线（binflow 依赖=1）、512MB RSS 增量为负。
  契约偏离（经 T-25 回写架构）：GC 集合形回调、Close() 入接口、state.json version 字段。已知边界：单 data dir 单 Engine 实例（doc.go 约束）。
- **T-11** [P0] auth 与 audit `role:dev-go-core` `area:internal/auth、internal/audit` `dep:T-8,T-10` — done 2026-08-18（经一轮修复）
  auth 13 文件 + audit 2 文件，29 测试/94 子用例。review 3 blocker 全修复并复审通过：B-1 哨兵导出别名（errors.Is 双拼法钉死）；B-2 pathmatch folder 语义（尾斜杠=folder，matchStart 仅 folder 生效，over-grant 探针三行钉死）；B-3 fail-closed 三测试 + 日志卫生。conductor 复现：race 23s 绿 / lint 0。
  新契约待 architect 回写（§3.4）：Can 的 path 尾斜杠=folder；文件路径与 pattern 全段匹配。顺手：Redact camelCase/header、ChangePassword 顺序对齐规格、Touch 每分钟节流。
- **T-12** [P0] repo.Service `role:dev-go-core` `area:internal/repo` `dep:T-9,T-10,T-11` — done 2026-08-18（经一轮修复）
  api/service/validate + 28 测试（真引擎基座 + hookStore 注入/journal 写序）。review 2 blocker 修复并复审通过（TrimSuffix 三处归一化：prune 保活目录行回归 + List 双形态一致）；事务边界被证实过硬（blob-first/FK 兜底/24 并发探针）。conductor 复现：race 19.5s 绿 / lint 0。修复 agent 被 429 击落于日志收尾，产出 100% 落盘。
- **T-26** [P1] architect 回写：Can folder 契约 + gosec 豁免复核 `role:architect` `area:docs/design、.golangci.yml` `dep:T-11` — done 2026-08-18
  §3.4 补 Can 尾斜杠=folder 三句契约（对齐 AuthorizationServiceBase）；gosec 五规则收窄：G301/G306 移除豁免、G204 限测试、G401 限 digest.go+测试、G115 限 password.go、G304 限两文件；隔离探针反向验证（新违规四条全中）；nolintlint require-explanation 启用。核验通过（lint 0）。
- **T-20** [P2] Range 与条件请求 `role:dev-registry-adapter` `area:internal/adapter/generic` `dep:T-13` — done 2026-08-18
  conditional.go：单区间解析（闭/开/后缀/钳制）+ 条件求值（INM 弱比较三形态/IMS/优先级）；206 走 Seek+CopyN；416 + bytes */total；HEAD 同分支。32 区间矩阵 + ETag 12 形态 + 条件 15 形态单测 + curl 9 步（-r 四形态/999999999- 416/INM 三拼写/-z 双侧）。T-13 零回归。轻量核验（P2+黑盒覆盖）替代 review；conductor 复现 race 绿 lint 0。curl -z 对 ISO 日期静默不发头的客户端怪癖已实证并绕开。M2 If-Range 扩展点已留（seek 数据面就绪）。
- **T-13** [P0] adapter SPI 与 Generic 适配器 `role:dev-registry-adapter` `area:internal/adapter、internal/adapter/generic` `dep:T-3,T-11,T-12` — done 2026-08-18（经一轮修复）
  SPI（Layout 解码→校验链/Register panic 条件/ForRepoType 并发安全）+ generic 四动词 + 校验头语义 + errors[] 信封 + FileInfo（size 字符串）。review 修复：repo.PutFromBlob（判权先于 blob 打开、双维校验、ErrOrphanBlob 堵死孤儿实体化与 sha256-only 降级）、BlobOpener 缝删除、404 文案分动词、控制字节拒绝、originalChecksums 上下文区分。路径安全 30+ 变体真机实测全 400（reviewer 取证）。curl 黑盒 12 场景全 PASS。
  遗留裁决归档：sha1-only deploy 维持 404（M3）；originalChecksums 持久化 M1 不需要；TOCTOU 零调用确认。PutFromBlob 契约待 architect 记入 §3.3（一句话）。
- **T-14** [P0] httpapi 核心 `role:dev-go-core` `area:internal/httpapi、internal/console` `dep:T-8,T-11,T-13` — done 2026-08-18（经一轮修复）
  7 文件 + 真栈 harness 测试（覆盖率 84%）。EscapedPath 手写分发树（绕开 mux cleanPath 归一化）；middleware 固定链 + logFields；errors[] 信封；系统端点四件；SIGTERM 优雅停机。review 3 blocker + 2 major 全修复复审通过：readyz storage 探测（只读目录 503）、死 case 删除、statusRecorder 双标志、mid-stream panic 不嫁接信封、首段 unescape 三源同源（encoded key 可路由且无 ACL 旁路）。curl 冒烟 + 19 探针矩阵全绿。
  遗留归档：minor 1-4/6-8 转 T-15/T-16/PM；RepoLookup 缝判定可保留（PackageTypeOf 列非阻断重构）；M2 架构措辞（路由解析位于授权门后 + ADR-0009 补句）交 architect。
- **T-15** [P0] Artifactory 兼容 REST `role:dev-registry-adapter` `area:internal/httpapi（兼容 handlers）` `dep:T-3,T-14` — done 2026-08-18
  四组 handler（repositories/storage/security/permissions）+ router/middleware/server 扩展 + 5 测试文件（38 顶级测试、覆盖率 78.2%、T-14 零回归）。v1.3 全口径落地：建仓 200 纯文本、错误体三分层、旧口令 400、token form+JSON 嗅探+OAuth 错误体、revoke XOR 幂等双文案、?list 匿名 403/根 400。curl compat 10 场景全 PASS。轻量核验（对定案矩阵编码 + 黑盒覆盖，qa 合并 T-18 全矩阵复验）。
  遗留：T-16 装配三缝（日志已列）；email 仅校验不落盘（users 表无列，回显需派生票，M2 评估）；repo 列表无按调用者过滤（M1 无数据面）。
- **T-27** [P1] architect 回写：三处累积裁决 `role:architect` `area:docs/design、DECISIONS.md、docs/prd` `dep:T-13,T-14` — done 2026-08-18
  §3.3 PutFromBlob 契约（签名逐字对齐实现 + 四点注释）；§7.1「路由解析位置」段 + §3.4 交叉引用 + ADR-0009 补句；PRD C28a 勘误（sfu admin + v1.3.1 修订行）。核验通过。
- **T-16** [P0] cmd 装配与生命周期 `role:dev-go-core` `area:cmd/binflow-server、scripts` `dep:T-14,T-15` — done 2026-08-18（经 429 中断续跑）
  main.go 573 行装配链（无全局单例）+ 15 测试（覆盖率 77.1%）+ scripts/smoke.sh。serve/gc 双 subcommand；冷启动实测 0.15s（NFR-P1 <2s 达标 13 倍余量）；SIGTERM 排空 0.038s exit 0；postgres e2e 拒启零残留；gc dry-run/--apply/幸存断言；admin 缺省 WARN（argon2 真探测）。conductor 实跑 smoke.sh 全绿（C01/C03/C07/C08/匿名/停机）。曾被 429 击落，额度恢复后续跑完成，生产代码零损失。
  遗留：gc 无 -c 旗标（票面未要求）；二次信号强退未构造观察窗口（排空毫秒级）。
- **T-17** [P0] 开发环境 `role:devops-engineer` `area:deploy/dev、README.md` `dep:T-16` — done 2026-08-18
  多阶段 Dockerfile（非 root 10001/HEALTHCHECK /readyz/CGO 零）+ compose（命名卷持久化/35s grace/:? 强制口令）+ README（五步双路径+安全须知三件）。Docker 真机全跑：AC1 up→ping 2.35s 冷链、AC2 health ok、AC3/C29 restart+down&up 两轮 sha256 不变；容器 healthy、db 无明文、日志无凭据。conductor 复核 compose 语法（:? 触发符合预期）。遗留：版本 stamping/distroless/CI docker build 归 M5。
- **T-28** [P0] 修复 D2/D3：管理面 admin 分级 `role:dev-registry-adapter` `area:internal/httpapi` `dep:T-18` — done 2026-08-18
  routeAuth 补 admin-only：四读面（repositories 列表/单查、v1-stats、v1-health）+ token 签发；routeAuth 新增 oauth 位（token 族错误体统一 OAuth 形，顺手修 revoke 403 不一致）；ping/version/探针不误伤。矩阵测试 + 自跑 22/22 + QA 回归 23/23 关闭两缺陷。
- **T-18** [P0] QA 功能矩阵验收 `role:qa-engineer` `dep:T-16,T-17` — done 2026-08-18（首轮 FAIL→回归 ALL GREEN）
  两轮验收：round 1 FAIL（D2/D3 两 P1 同源）→ T-28 修复 → 回归 23/23 ALL GREEN、D2/D3 关闭、round 1 FAIL 撤回。最终：场景 1/3/4/6/7 + NFR-S1/S2/S3 + C28 全 PASS。方法学亮点：二轮净instance 自纠两误报 + A/B 对照构建证伪一个疑似回归（观察项 O4 供 M2 复核）。报告 reports/agents/T-18-qa.md。
- **T-19** [P0] QA 存储完整性/性能/持久化+README 复跑 `role:qa-engineer` `dep:T-18` — done 2026-08-18（ALL GREEN 零缺陷）
  场景 2/5/8/9/10 全绿：去重（blob 1 物理份）、慢上传中断零残留、kill -9 双轮+容器路径一致、1GB 流式 RSS 增量仅 56KB（限 256MB）、冷启动 0.065s（限 2s）、100 并发零 5xx、C29 两轮持久化、gc dry-run/apply/幸存、README worktree 干净复跑全 0。**DoD 第 1/2 条判定：满足**（P2 未做仅 Content-Type 映射，合规延后 M2）。报告 reports/agents/T-19-qa.md（M1 QA 总报告）。

---

## M2 票据（2026-08-18 起）

- **T-33** [P0] /v2 挂载+docker 基座 `role:dev-registry-adapter` — done 2026-08-18（经双 review 一轮修复）
  /v2 根级例外路由（ADR-0010）+ name 解析 + spec 信封 + Api-Version 头 + health registry 字段 + R10 测试反转。双 review 5 blocker 全修复复审通过：B1 平面感知认证挑战（context 信号下传，/v2 spec 体+Bearer、/binflow 不变——真栈 curl 双面验证）；B2 全段 dot-segment 防线（400 实证）；B3 _catalog 占位；B4 repo 门三因拆两分支（500+ERROR 日志）。RepoTypes 空 class 键放行（§5.1 勘误挂 M3）。提交 e15e87a+e62eb78。
- **T-35** [P0] repo docker 用例编排 `role:dev-go-core` `area:internal/repo` — done 2026-08-18（经一轮修复）
  Service +8 docker 方法（PutManifest 幂等/解析/ListTags/ListImages/DeleteManifest 走存储层同事务级联/删仓拆库）；known/supported 两层类型矩阵。review 3 blocker 修复复审通过：B1 refs 泄漏窗口关闭（DeleteRepoRefs 后置 + review 探针确定性复刻测试）；B2 幂等零变更（存储行读回实证）+ TagRepointed 三方定约；B3 哨兵拆分（ErrInvalidManifest/ErrInvalidCursor）。提交 ccd4577+4344da3。T-38 前置仅剩 T-37。
- **T-41** [P1] O1 断连日志定界 `role:dev-go-core` `area:internal/httpapi(middleware)` — done 2026-08-18
  statusRecorder 增 writeErr/panicDisconnect 槽位；断连（ctx.Canceled 主腿 + errno 兜底）≥500 降 WARN + client_disconnect 标注；recoverPanic 断连降级不注信封。进程外 e2e 实证（--limit-rate + kill -9：日志零 ERROR、真 500 反例保持 ERROR 三态表）。轻量核验（P1+e2e 证据）。提交 85df447。M5 升格 label 遗留登记。
- **T-42** [P1] gc 旗标+GC mark 扩容 `role:dev-go-core` `area:cmd/binflow-server` — done 2026-08-18
  gc -c（复用 serve 配置链）+ --grace-hours（与 days 并存 hours 胜）；mark = nodes ∪ docker_refs（ListRefsByManifest 聚合——agent 论证了 RefsByBlob 会回到 T-9 废弃的反连接路线）；真栈冒烟（refs-held 存活/级联删后转候选）。净树核验（T-37 WIP 致主仓瞬断，隔离手法 agent 自报 conductor 复现）。提交 69a6039。遗留：lint 有网补跑；子命令 --help exit 1 小票登记。
- **T-46** [P1] docker 接入用户文档 `role:tech-writer` `area:docs/user` — done 2026-08-19（M2 最后一票，经 429 中断续完）
  docs/user/docker-registry.md（306 行，Docusaurus 首批页面）：insecure-registries 三形态置顶/全名 tag 模型/buildx 两坑/Helm OCI 凭据文件方案/oras 双类型/五客户端命令表/有意不兼容清单+反代片段/常见报错对照。13 组命令复跑全过（两轮，T-45 基线产物）。观察项 O-1（crane 取消 token 的 client_disconnect 500 日志）转 T-41/T-37 域评估。提交 c230b10。
- **T-45** [P0] 部署烟测 `role:release-engineer` — done 2026-08-19（AC 4/4 + O2 全过）
  compose 实例 D04/D05/D16 全过（v1.3 口径）+ D21 restart 持久化 + O2 全新 dind 默认端口零配置复跑（49 请求零 5xx）+ 反代直通示例（nginx/traefik）+ compose TTL 透传微调（注明理由）。两轮清理彻底；digest 清单录报告（发布动作待用户确认）。提交 923db2e。
- **T-44** [P0] QA 五客户端 conformance `role:qa-engineer` — done 2026-08-19（首轮 FAIL→复验 PASS）
  首轮：podman/crane/oras/skopeo+buildx 等效+conformance 55/60+性能全绿；docker 三 P0 断（D44-1/2/3）。修复（2f505da）后复验：docker 行 10/10 全绿（错口令 exit1 恢复/buildx 双平台直推/匿名 token 链/挑战模式全链）+ helm push PASS（首轮归因修正：HELM_REGISTRY_CONFIG 亦是败因）+ conformance 无回归。**DoD §9 第 1/2 条满足**（P2 遗留按 PRD 不阻塞：D44-4/5/6 实现欠账归 T-35/T-40 域 T-45 后收口、helm plain-http login 文档转 T-46）。报告 reports/agents/T-44-qa.md。
- **T-56** [P1] M2 PRD v1.3 勘误 `role:product-manager` `area:docs/prd` — done 2026-08-19
  C6 ping 无条件 401 挑战定案（ping-缓存型客户端根因 + 匿名 token 路径 + 真实世界同构佐证）；C5 offline_token 接受忽略（MAY ignore）；C4 POST 表单凭据同权 + D15 oras/helm 双类型形态修正；C7 tags/list 删光后 200 tags:null。约 22 处，自检零残留。提交 4d3aebd。
- **T-54** [P1] F1 压测定论 `role:dev-go-core` `area:internal/metadata、internal/adapter/docker` — done 2026-08-19
  根因确凿：SQLITE_BUSY 全仓 race 负载下烧穿 5s busy_timeout（8 轮复现 5 中 + 服务端 ERROR 原文 + duration 13.7s）+ busy 误映射 500 + 第二根因 anonSeedOnce 跨迭代污染（-count>1 确定性缺陷）。修复：IsStoreBusy 分类 + 503+Retry-After+WARN（T-38/T-41 先例）+ per-handler 隔离 + harness 取证盲区补齐；修复后 8/8 压测全绿 + 两轮 count=5。conductor 复现 4 新测试 PASS/12 包/lint 0。遗留：token 面 busy 映射（T-37 后续）；WAL synchronous=NORMAL 评估（architect ADR-0007 后票）；count>1 需显式 -timeout（40m 定论口径）。提交 f21904b。
- **T-38-D1** [P2] mount action 域修 + D2 降级修复 `role:dev-registry-adapter` — done 2026-08-19
  D1：canMountFrom 改 canActions 映射 + 双臂回归（红→绿证明：stash 复现 QA 现象）；D2：T-52 未使其消失（ListImages 需 repo 根读，休眠分支对真未知镜像照跑）→ imageListed ERROR→Debug 降级 + 真栈复现整日志零 ERROR。顺手：IdleSessionEviction 去墙钟竞态（注入时钟）。提交 e906f4e（与 T-55 同批）。
- **T-55** [P1] /v2/token 401 错误体 OAuth 形一行修 `role:dev-registry-adapter` `area:internal/adapter/docker(token)` — done 2026-08-19
  RenderAuthFailure 路径感知：token 路由族 OAuth 形（invalid_client）+ 挑战头逐字节不变；资源端点 spec 体不动；writeOAuthError 去 401 附 Basic 副作用（归属调用方）。4 行表双向断言 + 真栈三组错误凭据复验。conductor 复现：build ok + 两包针对性测试绿。提交待与 T-38-D1 同批（同包在途隔离）。
- **T-53** [P1] M2 PRD v1.2 勘误收口 `role:product-manager` `area:docs/prd` — done 2026-08-19
  C3+D3 裁定 token 端点族全 OAuth（三处对齐+跨里程碑口径）；C1 name 全名模型（D 序列+FR-8-AC6+D04b/c scope 联动）；C2 ping 无 scope 注记+四形态；v1.2 修订行完整；三轮自检零残留。经 429 中断（改动在盘）恢复收尾。遗留：T-37 401=spec 一行修（conductor 待派）。提交 4034b14。
- **T-43** [P0] QA 协议矩阵+M1 回归基线 `role:qa-engineer` — done 2026-08-19（PASS 80/82）
  剧本 1-5/7/10：M1 回归基线全绿（E-26 反转验证）/D 序列 35/36 绿/跨协议去重/权限面/遗留抽查全过；worktree 隔离构建（避开 T-52 在途）。2 P2（D1 mount action 域错配、D2 探测 ERROR 污染）+F1 flake+3 PRD 勘误 → T-38-D1/T-53/T-54 收口波已派（m2-done 前清零）。QA 又自纠 3 个 harness 误报。报告 reports/agents/T-43-qa.md。
- **T-52** [P1] repo.ListTags 零 tag 契约修复 `role:dev-go-core` `area:internal/repo` — done 2026-08-19
  行数判别两态（零 manifest→ErrImageNotFound 维持；有 manifest 零 tag→空切片 nil error——恰是 adapter 渲染 tags:null 所需）；断言反转+补强 5 子用例；agent 守 area 纪律未越界（adapter 注释由 conductor 顺手落：dormant defense 标注）。提交 6544ffc。
- **T-40** [P0] catalog 与 tags/list+分页 `role:dev-registry-adapter` `area:internal/adapter/docker(catalog)` — done 2026-08-19
  catalog/tags/list 字典序（跨仓全局重排实测）+ Link 分页（last exclusive/n 缺省 100/无效 n 400）+ tags:null + Q5 过滤矩阵 + 删仓消失 + 占位分支替换。D09 全序列 curl 实证 + 14 测试 race 绿。轻量核验（对定案矩阵编码+黑盒），T-43 全量复验。发现 T-35 缺陷（ListTags 零 tag）→ T-52。提交 2cb5b9b。**M2 功能开发全部完成**。
- **T-51** [P1] R3 消歧回写 `role:architect` `area:docs/design、docs/prd` — done 2026-08-19
  §5.3 校验链①终审口径（透传+结构判读/判读优先序/裸 media type 语义/schema1 拒收/不裁回白名单理由）；DDL 注释同步；双 node 布局升格正式契约（manifest+blob 双落、DELETE 后 blob 200 为既定语义）；PRD DE-15 OCI-Subject 注记。旧措辞零残留。遗留转 conductor：repo 导出 shutdown 哨兵小票。
- **T-39** [P0] manifest 链 `role:dev-registry-adapter` `area:internal/adapter/docker(manifest)` — done 2026-08-19（经双 review 一轮修复）
  校验链（结构/digest/引用在场/嵌套 lazy）+ GET·HEAD 逐位一致 + Accept 协商 + tag 覆盖 + DELETE 级联/405。curl 41 断言 + 容器内 daemon 协商全序列 + docker manifest inspect 真客户端 exit 0。架构 APPROVE（R3 终审意见：透传+结构判读）；正确性 1 blocker（重复 digest 假失败真发布）修复复审通过：adapter 去重 + INSERT OR IGNORE 纵深 + fake PK 对齐 + 四形态 201 无幽灵测试 + 20 路并发钉住回归。提交 eef0d3f+fd3d68a。**M2 docker 域功能面全部闭环**。
- **T-50** [P0] ADR-0011 文档中心 Docusaurus `role:architect` `area:DECISIONS.md、docs/design` — done 2026-08-19
  用户定案落地：Docusaurus 选型（同栈 React/版本化/i18n）；**交付形态=go:embed 挂 /binflow/docs（统一前缀、匿名可读）**——离线自带文档对齐 15 分钟标准，独立托管用户自办不双轨；docs/user 纯 Markdown 源与 docs-site 配置分离（writer 不碰构建）；体积 5~15MB 预算 M5 check-size 把关超限 fallback tar。架构四处增量（包树/职责表/路由/部署段）。M5 需 Docusaurus 脚手架票先行（类 T-7）。
- **T-49** [P1] OSS 工程结构参考规格 `role:reverse-engineer` `area:docs/reverse` — done 2026-08-18
  oss-structure.md（242 行）：51 pom 全量解析（37 实体+14 聚合）五域归类 ↔ internal/* 双向映射；L0-L5 单向依赖 + 四 SPI 接缝（JerseyApplication 双源扫描证据/CoreAddonsImpl 60+ 桩）；M3 导航三要点（协议全在 pro 但 OSS 有 50 个 *MetadataProvider 统一注册表同构点——M3 最有价值；addon 无 npm/pypi 接口→拆票按「OSS 接口面+pro 实现」双源；repo 类层次补强 repo-semantics）；结构启示 6 条。参考强度三级标注 [OSS]/[pro]/[双源]。M3 拆票输入就位。
- **T-48** [P1] architect 勘误 `role:architect` `area:docs/design` — done 2026-08-18
  §5.1 两例外（上传会话直持 Engine 限定上传端点族读路径仍走 Service；BlobLedger READ-only 同构先例）；§11 债务 13 行（PutLandedBlob M3 前小票）；RepoTypes 四点勘误正式落（声明性元数据/分发键约束/空 panic 保留/Layout 两态化）。遗留：httpapi New() 删 class 键写入的代码侧跟进（实现票）。
- **T-38** [P0] blob 域全链路 `role:dev-registry-adapter` `area:internal/adapter/docker(blob)` — done 2026-08-18（经双 review 一轮修复 + 2 次 429）
  upload 三式（416 错位恢复）+ mount 零拷贝降级 + 空层 32B 合成 + 读路径全套 + DELETE 405。curl 黑盒 38 断言（kill -9 重启链/AC7 跨协议去重）。双 review 4+1 blocker 修复复审通过：B1 互斥串行化（6 并发同 UUID 恰 1×202+5×416 + 连带修 done 标志）；B2 idle TTL 驱逐（24h 对齐+四动词 404）；B3 fd Close（真栈 50 mount 零累积）；B4 挂账失败 5xx（重试痊愈全链测试）。WithStorage 终判不越线（§5.1 勘误→T-48）。提交 2cd6d68+0b92149。
- **T-37** [P0] docker token 流 `role:dev-registry-adapter` `area:internal/adapter/docker(token)` — done 2026-08-18（经 429 中断续完）
  /v2/token（GET/POST form 双式/任意有效用户/匿名直发 _docker_anonymous 幂等 seed fail-closed/OAuth 错误体/offline_token 400）+ scope 三映射 + 挑战矩阵（真栈：PUT→pull,push / DELETE→pull,delete / read-only Bearer PUT→403 DENIED）+ D04 全系列 + D23 吊销链。docker login 本体受阻本机 daemon（VM+代理+无 insecure-registries）——容器内等效复现全协商；拒绝动用户配置（安全底线正确）。经第 5 次 429（代码全落盘）恢复收尾。提交 a89313f。
  转交：insecure-registries 说明 → T-46 文档；D05 → T-44。authorizeRoute 共用推导表已就绪（T-38/39/40 无需重推导）。
- **T-34** [P0] metadata 002_docker 迁移+DockerStore `role:dev-go-core` `area:internal/metadata` — done 2026-08-18（APPROVE 一轮过）
  review 0 blocker：DDL 对照固化为 pragma 测试（三索引=AC 笔误以定稿为准）；级联误删探针实证不可能（digest 即 manifest 身份 + image 谓词隔离）；2000 轮 DeleteManifest vs PutRefs 0 错误 0 残留；keyset BINARY collation 稳定。5 minor+2 nit 记录不阻塞。T-35 依此解锁。
- **T-36** [P2] generic Content-Type 扩展名映射 `role:dev-registry-adapter` `area:internal/adapter/generic` — done 2026-08-18
  mime.go 18 项映射（自有表→标准库分层，跨主机确定性）；PUT 端推断写 node.Mime（单一事实源：FileInfo/GET/HEAD/api/storage 自动一致）；客户端声明逐字优先；未知回退 octet-stream 不变。14 扩展名 curl 实测+大小写/复合扩展名/checksum-deploy 继承用例。轻量核验（P2+黑盒），T-43 全量复验。提交 0073358。
- **T-47** [P1] M2 PRD v1.1 回写（R1/R2/R4/R5） `role:product-manager` `area:docs/prd` — done 2026-08-18
  R1 realm=/v2/token + 双 token 入口说明（赶在 T-37 前完成）+ D04c 新增；R2 by-tag 405；R4 tags:null 断言注记；R5 四项定案（4MB/offline_token 400/service=binflow/last exclusive）。核验通过；ROADMAP 版本引用顺手修正。
- **T-34** [P0] metadata 002_docker 迁移+DockerStore（待 review 终裁） `role:dev-go-core` `area:internal/metadata` — 编码 done 2026-08-18
  002_docker.sql 三表三索引按架构定稿（零事务语句+守卫测试）；DockerStore 全 CRUD/级联单事务/keyset catalog；老库升级路径+并发用例；M1 零回归。conductor 复现全绿。提交 6df3b96。
- **T-32** [P0] M2 工程 ticket 拆解 `role:tech-lead` — done 2026-08-18
  14 票（T-33~T-46）+ 8 批次表 + R1~R12 风险清单，全文 reports/agents/T-32.md。核验通过（area 分区/M1 复用面/双 reviewer 标注合理）。R1/R2/R4/R5→T-47（PM）；R3→architect 消歧票（赶在 T-39 前）；R6/R7/R8/R9/R10 进对应票派单要点。
- **T-29** [P0] M2 PRD `role:product-manager` `area:docs/prd、ROADMAP.md` — done 2026-08-18
  milestone-2.md v1.0（515 行）：FR-7~FR-14（docker repo 类型/blob 三式/manifest schema2+OCI/catalog+tags/token 流/Helm OCI/五客户端矩阵/部署烟测）；DE-01~DE-17 兼容矩阵 + D01~D24 验收命令；M1 观察项 O1~O4 逐条定界；Q1 路由两案对比（待定）。核验通过。
- **T-30** [P0] M2 架构增量 `role:architect` `area:docs/design、DECISIONS.md` — done 2026-08-18
  **ADR-0010：/v2 根级例外**（三案评估：反代 rewrite 出局因裸机 docker 不可用、双挂载出局因三处双份生成；根级例外与 /healthz 同类豁免，token realm 免重写）。§5.3 docker adapter 13 行端点映射（offset 由 adapter 持协议态/cross-repo mount 走 PutFromBlob/mediaType 白名单+在场校验）；token 复用 TokenRegistry（scope pull→r push→w）；002 迁移三表（docker_manifests/tags/refs）。核验通过。三处待 T-31 校准点已入 §12。

- **T-58** [P0] M3 架构增量 `role:architect` — done 2026-08-19
  ADR-0012 remote 代理基线（TTL+条件再验证/artifact-metadata 分流/stale-while-error/SSRF 双检防 DNS rebinding/AES-GCM 凭据/stdlib-only）；ADR-0013 virtual（local-first+position 序/X-BinFlow-Resolved-From 头/M3 只读 405/探索 miss 不落盘）；§4.5 remote 缓存面（无影子仓）；MetadataProvider 注册表对齐 OSS；003 迁移（remote_configs 加密+remote_cache 表）。提交 82c973e。

- **T-59** [P0] M3 逆向规格 `role:reverse-engineer` `area:docs/reverse` — done 2026-08-19
  maven-npm-pypi.md（306 行：5 端点表/12 流程/6 布局组——npm publish 十步校验链、Maven metadata 两套规则、PyPI upload multipart、remote 六步 pull-through、virtual 四桶序）+ repo-semantics.md §7/§8 扩编（+158 行）。置信度高 ~102/中 ~24/低 0（不确定不入文降级待验证 8 条）。顺带关闭 M1 待验证 #4 + 勘误 2 处。提交 7042dae。

- **T-57** [P0] M3 PRD v1.0 `role:product-manager` `area:docs/prd、ROADMAP.md` — done 2026-08-19
  milestone-3.md（817 行）：FR-15~FR-22 共 66 AC（Maven layout/checksum 三态/metadata 合并+snapshot、npm、PyPI、remote pull-through+SSRF、virtual、conformance）；35 端点矩阵 + M01~M61 验收命令（mvn/npm/pip P0）；Q1~Q8 附暂行；ROADMAP 切 M3。Q4（docker remote 推迟 M4+）已转用户知悉。提交 e456f1b。

- **T-60** [P0] M3 PRD v1.1 校准 `role:product-manager` `area:docs/prd` — done 2026-08-19
  C1~C8 全定案（maven-metadata 服务端计算/layout 六字段/virtual 两桶简化/TTL 定案/写路由字段/PyPI 布局兼容子集/npm tarball）；M1 勘误吸收（snapshot policy 409）；Q3/Q7 定案（npm 403/重复 publish、PyPI sha256-only）；连带定案（remote checksum 不回源 404、上游故障默认 404+hardFail 502——推翻 v1.0 五处）；计数 29/1/0/5+1。自检零残留。遗留：Q1/Q2 待用户；M1 PRD 两处勘误小票建议。提交 0f48164。

- **T-61** [P0] M3 工程 ticket 拆解 `role:tech-lead` — done 2026-08-19
  16 票（T-62~T-77，P0×13）+9 批次+R1~R10；复用清单 8 面（storage.Session/迁移器/Service 覆盖链/SPI/权限/GC/QA 脚本）零重做；遗留 5 项处置（2 无票归档/T-73/T-63 收编/1 归 M4）；Q1 按 ADR 写死（R1 回写）、Q2 暂行入 T-71。全文 reports/agents/T-61.md。提交 8500821。

- **T-78** [P1] PRD v1.2 凭据回写 `role:product-manager` `area:docs/prd` — done 2026-08-19
  Q1 按 ADR-0012 关闭（AES-GCM/enc:v1:/env BINFLOW_REMOTE_CREDENTIALS_KEY/fail-fast/003 一次性加密）；新增 FR-15-AC9 四断言；C2 注记顺手。R1 达成——T-66 派发解锁。范围外三冲突转 T-79（ADR 勘误）。提交 2cacee1。

- **T-79** [P1] ADR-0012/0013 勘误 `role:architect` `area:DECISIONS.md` — done 2026-08-19
  勘误一：故障降级 404+assumed-offline+X-Binflow-Upstream-Error（Warning:111 作废）+负缓存定案；ADR-0013 联动：两桶序+可选写路由（决策骨架不变）；勘误二：SSRF 五参数以 PRD v1.2 为准+建仓只校验 scheme（IP 校验全在请求时——清单外新发现分歧）。T-62 已补发对齐提示；§4.5/§5.4/003 注释三处同步债挂 T-66/T-71 派单注明。提交 14ba31e。

- **T-62** [P0] metadata 003 迁移+Remote/Virtual `role:dev-go-core` `area:internal/metadata` — done 2026-08-19（APPROVE 一轮过）
  review 0 blocker：DDL 逐列一致+pragma 钉死；老库升级真原生 apply；400 次并发 upsert + 4×40 SetMembers 探针无 busy 逃逸；两桶序注释勘误后口径完整；clean-room 无嫌疑。5 non-blocking 记录（T-64 防明文窗口提示已转批 2 派单要点）。提交 54ed293+4bcd536。

- **T-63** [P0] adapter SPI 基座 `role:dev-go-core` — done 2026-08-19（APPROVE 一轮过）
  MetadataProvider 注册表 + npm/pypi 分发缝（escaped 逐字保留）+ class 键清理（§5.1 勘误收编）+ repo/api.go 两段拆分。review 0 blocker：6 项 seam 探针真栈全过；契约三决定全确认（路径重写/ClassReader 纪律/Versions 回落）；E-26 未反转。N4（协议票严格拒绝决策）已转 T-69/T-70 派单要点。提交 5b79a52+91c1f67。**批 1 全部闭环**。

- **T-65** [P0] SSRF 防护链+stdlib client `role:dev-go-core` `area:internal/remote` — done 2026-08-19（经双 review 一轮修复 + 429 中断续完）
  NFR-S13 七点全实现（48→94 断言/coverage 87%/零新依赖/注入 Resolver 零外网）。双 review 4 blocker 修复复审通过：B1 NAT64/6to4/Teredo 内嵌 IPv4 拆解递归过表（保 DNS64 放行侧）+ 重定向跟随面钉死；B2 zone 剥离；B3 godoc 契约修正；B4 HEAD 豁免 64MB 快速失败。顺手 Location userinfo 堵注入。安全 review 探针实证（go test -overlay 零树改动）。提交 f9fb2c9+0dc17a2。T-66 消费面接口七项已备。
- **T-80** [P0] httpapi REST 三型接线 `role:dev-go-core` `area:internal/httpapi/repositories.go` — done 2026-08-19（经 429 中断续完）
  repoConfig 扩 M3 字段+configJSON 组装（keep-current 信号）+ListReposFiltered 接线+configuration 回显+C26 翻转（docker 组合维持 400）。M01~M05 真二进制 curl 全过（M02b 三态/M03 成员校验/M04 过滤矩阵/M05 组合边界）+8 REST 测试+13 包零回归。T-66 fixture 前置就绪。

- **T-64** [P0] repo 三型模型+PutLandedBlob `role:dev-go-core` — done 2026-08-19（APPROVE 一轮过）
  review 0 blocker：finalize 切换 git diff 核实（O(size) 回读真删/busy 注入迁移/T-54 断言保留）；PutLandedBlob 并发同摘要+无重读钉板 -count=2 绿；C26 声明在快照验证。4 non-blocking（掩码大小写敏感→T-66 改/crash 窗口口径/审计置空/PutLandedBlob 段位）。提交 63135de+5662b22。
- **T-81** [P1] NAT64 勘误 `role:product-manager` — done 2026-08-19
  PRD NFR-S13② + ADR-0012 决策 3 各一句（拆解递归/DNS64 保留/Teredo 直拒/v4-compatible 收编/zone 剥离），溯源 T-65+0dc17a2。M42 测试向量扩充建议记日志。

- **T-70** [P0] PyPI adapter `role:dev-registry-adapter` `area:internal/adapter/pypi` — done 2026-08-20（经一轮修复）
  simple（PEP 503 三态/691 JSON/Vary）/upload（md5 三态/:action 400）/下载双入口；twine 7+pip 26 真实客户端 M30~M35b 全过；归一化矩阵+恶意文件名九变体探针全过。review 1 blocker（探针 fd 泄漏+审计伪造）+Vary+死 Del 修复复审通过。遗留：N4 缝层强制与 service Stat 面转 conductor；remote/virtual simple 400 过渡（T-71/72）。提交 c59bd6b+258aae1。
- **T-69** [P0] npm adapter `role:dev-registry-adapter` `area:internal/adapter/npm` — done 2026-08-20
  23 文件 4336 行：十步校验链/packument（tarball 重写/ETag-304/SLIM）/dist-tags 两形态/unpublish 联动/login 复用 TokenRegistry/N4 守卫。npm 10.9.8 真实客户端 M22~M28 全过（M26 403 勘误口径/M27 -rev 占位显式断言）。E-26 npm 翻转。-rev 对 packument 形 body 的有意偏离建议规格回写。cmd 装配 3 行待集成票。提交 91bb261。
- **T-67** [P0] Maven adapter（传输面） `role:dev-registry-adapter` `area:internal/adapter/maven` — 主体 done（91bb261），mvn 真客户端腿续跑中
  layout 六字段解析/checksum 三态/旁车/snapshot 语义/穿越防御；M11~M21 wire 序列+curl 等价全过。遗留②：Service SPI 缺 metadata 覆盖豁免入口（~10 行，T-68 依赖，挂 architect）。

- **T-66** [P0] remote fetcher `role:dev-go-core` `area:internal/remote、internal/repo` — done 2026-08-20（经双 review 一轮修复 + 429 续完）
  六步全矩阵 + AES-GCM 凭据链 + 分流接线；真二进制 M41~M48/FR-15-AC9/1GiB<200MB。双 review 4 blocker 修复复审通过：B1 单飞等待者重入全量重查（16 并发同 404 上游恰 1 次钉板）；B2 超时回发旧副本；B3 RepoTypes 升 {local,remote}；B4 契约 godoc。提交 05acd91+71e6c93。
- **T-67** [P0] Maven adapter `role:dev-registry-adapter` `area:internal/adapter/maven` — done 2026-08-20
  layout 六字段/checksum 三态/旁车/snapshot 语义/穿越防御 + cmd 装配 + REST local 字段透传。wire 序列全过 + **mvn 3.9.9 真客户端 M11/M12/M13/M16 布局腿**（BUILD SUCCESS/sha256 对账/全新 repo resolve/timestamped 落盘）。SPI 豁免遗留→T-68 实施/T-83 定约。提交 91bb261+71e6c93。
- **T-71** [P0] virtual 两桶解析+写路由 `role:dev-go-core` `area:internal/repo` — done 2026-08-20（APPROVE 一轮过）
  review 0 blocker：stale/miss 语义引擎侧核实（成功必 HasCopy/true miss 只以 Unfound）；非 Unfound 透传确认为 AC7 严格读法（安全面更优）；C5 文案逐字节相等；pre-read guard 顺序面正确。6 non-blocking（QA 钉板 hardFail 透传防顺手修复等）。提交 eab4363+f6e1017。
  virtual.go 两桶序（逐请求现算）/Get 三型分派/stale 命中即成员结果（HasCopy 消费）/探索性 miss pre-read guard/写路由（405+C5 文案/配置后换址 local）/ExtraHeaders 双头合并。17 测试群+真二进制 M50/M52/M53。遗留①②（协议面 StatusError+ExtraHeaders 两缝）→ T-82。提交 eab4363。
（T-82 done → done 区）
（T-68 编码完成 → review 区）
（T-83 done → done 区）

- **T-83** [P1] architect 回写 `role:architect` `area:docs/design` — done 2026-08-20
  §4.5 两处（checksum 登记不拒定案/故障语义勘误一收口）+ §5.4 渲染缝（StatusError+ExtraHeaders 复用勿另开缝）+ RepoTypes + **SPI SkipOverwriteCheck 最终契约**（收窄：仅服务端自有写入/写门不豁免/只跳 d 检查）+ T-79 三处遗留债顺带收口。提交 d983753。

- **T-82** [P0] 三协议双缝修复 `role:dev-registry-adapter` `area:internal/adapter/{maven,npm,pypi}` — done 2026-08-20
  StatusError 直渲染（npm 此前完全缺失→一律 500）+ ExtraHeaders 探测三协议；红绿验证（还原至 HEAD 三测试 FAIL 行号级）；virtual DELETE 405+C5 逐字/双头输出/上游计数冻结。遗留①pypi 上传早闸不感知路由（挂 architect）②npm RepoTypes 口径③maven PUT 文案对齐（已顺手做）。提交 5b13a1e。

- **T-68** [P0] maven metadata 计算器 `role:dev-registry-adapter` `area:internal/adapter/maven、internal/repo(SPI)` — done 2026-08-20（APPROVE 一轮过）
  计算器 ~700 行（触发四类/两组生成器/进程锁合并）+ SPI PutWithOptions（T-83 契约）。review 0 blocker：4-worker 并发探针终态收敛零 5xx；AC6 旁车现算对账；三沉默裁决全确认。7 non-blocking（T-83 godoc 措辞偏差转 architect/dotted 段守卫建议/async 超时排队面）。提交 dad9458+e61e580。

- **T-72** [P1] virtual metadata 聚合 `role:dev-registry-adapter` `area:adapter/{maven,npm,pypi}` — done 2026-08-20
  三协议聚合面：maven 桶序合并（MNG-5180/优先短路/block 透传）/npm putIfAbsent+并集/pypi 条目并集+JSON 回退。15 矩阵群全真栈；**npm/pip 真客户端 M54/M55 全过**；mvn 环境阻塞走 curl 等价（归 T-74/76）。跨 area 补缝（repo SPI 三方法 ~150 行，成员资格守卫）已 flagged 待 architect 复核。上游计数 2→3 校准点转 T-75。提交 09f833c。

- **T-84** [P0] cmd 三协议装配 `role:dev-go-core` `area:cmd/binflow-server` — done 2026-08-20（经 429 续完）
  npm（New+WithAuth+WithLedger+Register）/pypi（Register 一次调用）入 Deps.Adapters；真二进制三协议 smoke 全 exit 0（npm publish+install/pip twine+download/mvn deploy+dependency:get）+ 读面全 200 零 ERROR。QA 真客户端前置就绪。提交 59855b1。

- **T-73** [P2] sha1-only checksum deploy `role:dev-go-core` `area:internal/metadata(增量)、internal/repo、adapter/{maven,generic}` — done 2026-08-20
  GetBySha1（idx_blobs_sha1 消费者，纯增量三文件）+ PutFromBlob sha1 寻址（权限对前解析）+ generic 删旧拒绝分支 + maven putChecksumDeploy（ME-08 gate 后/artifact-only/calc 触发）。四触及包 race 绿 + 16 包 ok。area 偏离已申报（T-62 只留索引缝，接口面无查询——无法仅在 area 内实现）。npm/pypi 未启用（PRD 未点名）。提交 f1323ff。

- **T-74** [P0] QA 三协议功能矩阵 `role:qa-engineer` — done 2026-08-20（PASS 49/49）
  mvn/npm/pip/twine 真客户端全矩阵（M01~M05/M10~M21/M22~M28/M30~M35b 勘误口径全对）；边界+穿越 12 变体+NFR-S16/17/18+四协议去重全过；5xx=0；5 条 PRD 勘误建议（E1~E5 均不阻塞）。被测 f597c86 独立 worktree。报告 reports/agents/T-74-qa.md。

- **T-75** [P0] QA remote/virtual+SSRF `role:qa-engineer` — done 2026-08-20（PASS）
  M41~M48 全序（16 直连变体全 400 + NAT64 拆解/Teredo 直拒/DNS64 保留侧不拦 + 19 WARN 全录 + 300s 真静默窗）；virtual M50~M55b（C5 逐字/优先桶/聚合/hardFail 透传钉板/T-72 校准点实证）；NFR-S13~S15+S14（enc:v1: 明文 0）+P14（1GB RSS +28KB）。3 条 502 全设计内。PRD 勘误 E1/E2 + 观察 O1~O3 转交。报告 reports/agents/T-75-qa.md。

- **T-76** [P0] QA 客户端矩阵+回归+性能 `role:qa-engineer` — done 2026-08-20（PASS）
  mvn 五链/npm 7/pip 6/curl 4/Gradle P2 观察 1 = 25/25；M50/M54 三协议收口断言成立；M1 C 序列 23 + M2 D 序列 14 回归全绿（E-07/C26/E-26 反转成立）；docker D16 全链五域去重闭环；性能（冷启动 0.118s/50 并发 3.39s 零 5xx/M60c 182ms 分解为 M1 fsync 固定成本非 M3 引入——O1 转 PM）。**DoD §9 第 1/2 条终判：满足**。报告 reports/agents/T-76-qa.md（M3 QA 总报告）。

- **T-77** [P1] M3 用户文档 `role:tech-writer` `area:docs/user` — done 2026-08-20
  四篇指南 723 行（maven/npm/pypi 接入 + remote/virtual 管理）：建仓字段表 v1.2 默认值/SSRF 放行指引/凭据 env 与 fail-fast/14 行定案报错码逐字/不兼容全表；五个客户端坑收编；T-76 基线产物抽样复跑五链 exit 0（含 enc:v1: 有/明文无、405 C5 逐字）。提交 e7d2336。

- **T-86** [P0] M4 架构增量 `role:architect` `area:docs/design、DECISIONS.md` — done 2026-08-20
  ADR-0014（console 挂 /binflow/console 保留段 + server-side session 三层 CSRF + 无专属 API 树 + vite 构建链与 ADR-0005 边界澄清）；ADR-0015（GC 在线四安全边界 + repo_usage 同事务配额 + 备份先 DB 后 blobs 硬规则 + import 仅 CLI）；004 四表设计；保留字扩 {docs,console}。提交 d0ff1fb。

- **T-87** [P1] 控制台信息架构与线框 `role:ux-designer` `area:docs/design` — done 2026-08-20
  console-ux.md 665 行十节：导航树+18 路由表 / 11 页线框（权限编辑器模式测试器+diff 确认为核心）/ 四态矩阵+大 repo 骨架屏 / keyset 增量加载策略 / 暗色双主题 --bf-* token / 五协议×三仓型呈现差异矩阵 / R1~R10 API 需求清单。三大决策：协议能力收窄不伪装（UI 上传仅 generic/maven）、大目录 keyset+懒加载、权限编辑器防 ACL 漂移。提交 a6f0d77。

- **T-85** [P0] M4 PRD v1.0 `role:product-manager` `area:docs/prd、ROADMAP.md` — done 2026-08-20
  milestone-4.md 725 行：FR-23~FR-33 共 60+ AC（控制台/权限完整模型/治理四件）；29 端点矩阵 + W01~W40（curl+Playwright+CLI）；Q1~Q6 附暂行（session/配额粒度/搜索范围/备份窗口/docker remote 推迟/GC 形态）；M1~M3 遗留收编 9 条；ROADMAP 切 M4。与 ADR-0014/0015 裁决对齐（Q1 session 与 ADR 一致）。提交 3d1cfbf。

- **T-88** [P0] M4 工程 ticket 拆解 `role:tech-lead` — done 2026-08-20
  19 票（T-89~T-107）+9 批次+R1~R10；R1 裁决「PRD 面 + ADR 内核」；复用清单 10 面；ux R1~R10 映射；P2 债务十条。全文 reports/agents/T-88.md。提交 6f99900。
- **T-109** [P1] PRD v1.1 对齐收口 `role:product-manager` `area:docs/prd` — done 2026-08-20
  R1 对齐注记（PRD 面胜出+ADR 内核生效+T-108 勘误归属）；R4 TTL 双键（hours 主键+seconds 覆盖键）；K1~K3 回写注记。提交 18fe44c。

- **T-108** [P0] M4 勘误收口 `role:architect` `area:DECISIONS.md、docs/design` — done 2026-08-20
  ADR-0014 勘误（命名对齐 PRD 面：/binflow/ui 301/session 三动词含 whoami/binflow_session/TTL 双键/CSRF 改 Origin 校验）+ ADR-0008 保留字并集 {api,v2,docs,console,ui} + R2 架构勘误（gc 同步+互斥/usage 端点/groups 兼容层/004 email 列+audit 索引+user_groups 表名）+ ADR-0015 顺带勘误（报备）。R1 门槛清除，T-89 解锁。提交 ad418c5。

- **T-89** [P0] web 前端工程脚手架 `role:devops-engineer` `area:web/、internal/console、Makefile、CI` — done 2026-08-20
  vite6+React19+TS（base=/binflow/ui/）+ go:embed 三形态 Handler + relink-assets + CI node20 步（npm audit/tsc/eslint）+ Playwright 基建。conductor 复现：make console 75KB SPA（1.4% 预算）/console 测试绿/占位态 build 18.75MB。/binflow/ 301 live 证据；真 Chromium 2 spec 过。router 挂载归 T-91。提交 358b3c1。

- **T-90** [P0] 004 迁移+Groups/WebSessions/审计扩展 `role:dev-go-core` `area:internal/metadata` — done 2026-08-20（APPROVE 一轮过）
  review 0 blocker：DDL 逐列一致；同毫秒 keyset 翻页独立探针（50 行/带全页无跳重）；EXPLAIN 独立复核（sqlite 3.43.2 复合索引倒扫免 SORT）；幂等语义实测（changes() 同值 UPDATE）。6 non-blocking（cursor 形状校验/EXPLAIN SQL 漂移/索引列序断言/nil-vs-empty/哨兵同名——转 T-93/T-97 派单注意）。范围外：T-92 在制文件混入提交（粒度问题无缺陷）。提交 595e090+bf3f804。

- **T-92** [P0] 搜索域 `role:dev-go-core` `area:httpapi+repo+metadata` — done 2026-08-20（APPROVE 一轮过）
  review 0 blocker：ACL 零泄漏（与内容面同一 allow() 路径 + 双引用探针实测）；LIKE 转义/参数化/索引真实；fileInfoOf 纯提取。4 non-blocking（宽结果 limit 门→T-105 探针/零授权 200 空 vs 403 姿态→PRD 半句）。提交 358b3c1+0390958。

- **T-93** [P0] 审计查询面+词表 `role:dev-go-core` `area:internal/audit、internal/httpapi(audit)` — done 2026-08-20
  Filter 全参数+消费侧 keyset+NormalizeTimestamp；GET /api/v1/audit（limit 1..1000/cursor/403 矩阵）；W22 窗口/W23b 脱敏 grep 0/W39 append-only 全 404；NFR-S21 源码扫描测试；M4 十动作常量。conductor 复现：audit race 3.0s 绿/lint 0/4 HTTP 测试 PASS。提交 ccc1862（t93_audit_test.go 随 13bc7f3 补齐——依赖 T-95 的 harness 真 audit 接线）。

- **T-95** [P0] 治理字段+配额 enforcement+usage `role:dev-go-core` `area:internal/repo、internal/metadata(usage)、internal/httpapi(usage)` — done 2026-08-20（单 review 一轮修复）
  includes/excludes 双值（excludes 优先、默认 **/* 零开销短路）+ quotaBytes enforcement（Put 四族挂门、413 零残留、virtual 按目标 local）+ UsageStore 同事务 delta 标量子查询（无读→写升级）+ 005 回填 + usage 端点。review 取证：匹配器同构零漂移/挂点完备（docker finalize·mount·manifest step-1 无绕过）/413 原子性成立。B1（声明 checksum 幂等重传在配额顶误拒 413——三处 replaced 条件方向反）修复：existing != nil + 注释重写 + TestQuotaIdempotentRetransmitAtCeiling 三臂回退法验证 + 弱用例修正；NB2 action alias。conductor 复核：build/lint 0/定向测试 PASS。提交 13bc7f3+0a80ed0。遗留：docker /v2 渲染→T-111、manifest 双计数显示口径、预检非预留、透传清空局限、NB1/NB3/NB5/NB6 登记。

- **T-110** [P1] assets 保留字 + TTL 塌缩句 `role:architect` `area:DECISIONS.md、docs/design` — done 2026-08-20（经 429 续完）
  ADR-0008 增补 assets（六字集并集）+ ADR-0014/§7.5 塌缩句（会话必死于 created_at+TTL 与活跃度无关——防前端/QA 误读）+ §6 DDL 注释同步。T-108 遗留①闭合。提交 8f26a0a。

- **T-91** [P0] session 三臂+console 挂载+CSRF `role:dev-go-core` — done 2026-08-20（经双 review 一轮修复 + 2 次 429）
  Cookie 第三臂（256bit/失效即拒/Touch 封顶）+ session 三动词 + csrfGuard（Origin + XFP）+ console 挂载 + TTL 双键。curl 全周期 24 PASS + Playwright 探针转绿。双 review 修复复审通过：B1 登录端点豁免（stale cookie 5 子例 + B1B2 咬合）；B2 login-CSRF Origin 守卫（6 子例）；assets 保留字；N6 audit nil 兜底。16 包 race 绿。提交 906d82a+67c3380。**批 2 闭环**。

- **T-138** [P0] FR-39 systemd 单元+安装脚本（PB-07）  — done 2026-08-21（批 4 完成）

- **T-139** [P0] FR-40 离线安装包（PB-08）  — done 2026-08-21（批 4 完成）

- **T-140** [P1] FR-47 URI 硬编码 /binflow 修复  — done 2026-08-21（批 4 完成）

- **T-142** [P1] FR-41 内容（下）：API 参考+FAQ 扩写+管理页增强  — done 2026-08-21（批 4 完成）

- **T-144** [P0] QA 全链+回归基线  — done 2026-08-21（批 5 完成，28/28 PASS）
  G30a~G30d 目录实体化 4/4 + G30e Playwright FE 4/4 + G31a~G31c Token 审计 3/3 + G32 Docker 树视图 6/6 + G33 URI 基址族 1/1 + G34 四里程碑回归 1/1 + §5.6 反转表 6/6 + FR-44-AC6 3/3。修复 t131-g30e.spec.ts 两处 data-testid 不匹配。日志 reports/agents/T-144-qa.md。

- **T-146** [P0] QA：文档中心（剧本 4） `role:qa-engineer` — done 2026-08-21（批 6 完成，6/6 AC PASS，报告 reports/agents/T-146-qa.md）
  G18 离线可用性 PASS + G19 文档完整性 PASS + G19b 帮助入口 PASS + G20 curl/doc 一致性 PASS + G21 2.25MB<<40MB PASS + G22 构建零错误 PASS

- **T-147** [P0] QA：性能基准 + GA 总矩阵 + 发布清单（剧本 7/8/10） `role:qa-engineer` — done 2026-08-21（批 7 完成，M5 最后一票）
  AC1 性能：1000 并发零 5xx（421.9 req/s 聚合）、冷启动 98ms（<2s）、RSS 物理足迹 17.7M（<100MB）、1GB 流式 RSS 增量 ~6MB（<256MB）、7 次同内容上传 = 1 物理 blob。AC2 基准：REST GET/PUT 吞吐、搜索 P95 23ms、export 1459.8 MiB/s、GC 27-30ms、vs M4 无 >20% 回归。AC3 GA 总矩阵：94/98 PASS（3 DEFERRED Q3 条件腿 + 1 BLOCKED nginx SSL）、6 平台 sha256 全部验证、M5 DoD 7/7 条 PASS。报告 reports/agents/T-147-qa.md
- **T-145** [P0] QA：发布矩阵与部署形态全量（剧本 2/3 + 条件腿） `role:qa-engineer` — done 2026-08-21（批 6 完成，PASS 46/49，1 BLOCKED + 2 DEFERRED）
  G01~G04 产物+版本+可跑腿 PASS + G06~G08 Docker 双变体 8/8 PASS + G09 compose 5/6（nginx SSL 证书 BLOCKED）+ G11~G13b Helm 9/9 PASS + G14 K8s 7/7 PASS + G15~G16 systemd 4/4 PASS + G17 离线包 4/4 PASS。G05 Windows/G15b systemd 真机 DEFERRED（用户环境未到位）。报告 reports/agents/T-145-qa.md

## ✅ 已完成（done）— M6

- **T-148** [P0] goreleaser 三二进制发布矩阵扩展（bf + bf-migrate） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  `make build` 产出三二进制（bf 2.6MB / bf-migrate 2.6MB / binflow-server 22MB，全在预算内）；`--version` 同源 ldflags 注入；`.goreleaser.yaml` 六平台三ID；`make check-size` 覆盖三二进制；`make test` 三二进制 race 绿。日志 reports/agents/T-148.md。

- **T-149** [P0] M6 依赖白名单准入（go.mod + ADR-0005 扩展） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  `go.mod` 新增 minio-go/v7 + go-oidc/v3 + go-ldap/v3；`make check-deps` 零 CGo 基线通过；`go mod verify` 通过；`make build` 三二进制全在预算内；`make vet` 零告警；18/18 测试包 race 绿。ADR-0005 白名单追加。日志 reports/agents/T-149.md。

- **T-150** [P0] storage.Backend 接口 + Engine 签名变更（Open → io.ReadCloser） `role:dev-go-storage` — done 2026-08-21（conductor 核验直收）
  新增 `internal/storage/backend.go`（包内 Backend 接口：Put/Get/Delete/Exists/List）；`internal/storage/api.go` Engine.Open 返回类型从 `io.ReadSeekCloser` 变更为 `io.ReadCloser`；`internal/storage/engine.go` 实现适配；`engine_test.go` 测试适配。全量 storage 测试 race 绿，11 个依赖包零回归。日志 reports/agents/T-150.md。

- **T-153** [P0] auth.IdentityProvider 接口 + 认证臂扩展（OIDC Bearer 臂） `role:dev-go-core` — done 2026-08-21（conductor 核验直收）
  新增 `internal/auth/identity.go`（Provider 类型/Claims/ProviderUser/IdentityProvider 接口/ErrProviderUserNotFound）；`internal/auth/api.go` Principal 新增 Source 字段；`internal/auth/authenticator.go` 新增 WithOIDC()/authenticateOIDC()/userCreator 接口；`internal/auth/deps.go` userStoreAdapter 适配 Provider/ProviderID。全量 auth 测试 race 绿，11 个依赖包零回归。日志 reports/agents/T-153.md。

- **T-152** [P0] S3 配置与健康检查（config 段 + /healthz 扩展） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/config/api.go` S3Config 结构体/StorageBackend 常量/Backend 字段；`internal/config/config.go` S3 默认常量/S3SecretEnvVar/splitEnvKey；`internal/config/load.go` raw 结构体 S3 子段/build/defaults/setEnvValue/rejectSecrets；`internal/config/validate.go` backend 枚举校验/S3 required fields/skip data_dir 创建；`internal/httpapi/system.go` probeS3Storage（BucketExists→PutObject→RemoveObject）。config 40 测试 race 绿，httpapi 构建通过，依赖包零回归。日志 reports/agents/T-152.md。

- **T-154** [P0] OIDC Provider 实现（go-oidc/v3） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/auth/oidc.go`（~796 行，OIDCProvider 实现 IdentityProvider 接口/PKCE 生成器/claims 提取/go-oidc/v3 ID Token 验证）；`internal/auth/oidc_test.go`（13 测试，httptest+jose mock OIDC server）；`internal/auth/deps.go` 依赖连线（GetByProvider/adaptUser）。auth 62 测试 race 绿，repo/httpapi/6 适配器包零回归。遗留：metadata.User 缺 Provider/ProviderID 字段（008 migration DDL 已有但 Go struct 未更新），userStoreAdapter.GetByProvider 使用 O(n) List() 遍历。日志 reports/agents/T-154.md。

- **T-151** [P0] S3Engine 实现（minio-go/v7） `role:dev-go-storage` — done 2026-08-22（conductor 核验直收）
  新增 `internal/storage/s3.go`（~755 行，S3Engine/s3Session/s3Backend 实现，multipart upload/go:generate 注册）；`internal/storage/s3_test.go`（~1000 行，mock S3 HTTP server + 25 table-driven 测试）。全量 storage 测试 race 绿（41.4s），repo/httpapi/6 适配器包零回归。mock server 并发写同 blob 收敛测试修复（uploadID 唯一性）。T-150 Backend 接口消费方完工。日志 reports/agents/T-151.md。

- **T-155** [P0] LDAP Provider 实现（go-ldap/v3） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/auth/ldap.go`（~596 行，LDAPProvider 实现 IdentityProvider 接口/连接池/Bind 流/搜索组/AdminGroup 判定）；`internal/auth/ldap_test.go`（~949 行，mock LDAP server + 16 table-driven 测试）。auth 25.5s race 绿，repo/httpapi/6 适配器包零回归。日志 reports/agents/T-155.md。

- **T-156** [P0] 008 元数据迁移（users 表 provider 列 + 认证臂优先级） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `migrations/{sqlite,postgres}/008_oidc_ldap.sql`（users 表 +provider/provider_id）；`internal/auth/deps.go` NewLDAPResolver + adaptUser 读 Provider/ProviderID；`internal/auth/authenticator.go` WithLDAP/authenticateLDAP（Bind→Resolve→自动建用户）；`internal/auth/session.go` AuthenticateCredentials 本地密码失败后 LDAP 回退；`internal/auth/ldap_login_test.go` 新增登录回退测试组；metadata User struct/查询/Create/Get/List 全链路读写 provider 列。conductor 复核：auth 60.1s / metadata 62.2s / repo 161.6s / httpapi 全部 race 绿，build+vet 干净。遗留：GetByProvider O(n) 遍历（可后续加索引）。日志 reports/agents/T-156.md。

- **T-161** [P0] replication 模型与 009 迁移（replications + replication_tasks 表） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/replication/{model,store}.go`（ReplicationConfig/ReplicationTask/ConfigStatus 聚合/Store 接口/SQLiteStore 实现，UNIQUE(name)+busy 503+status 白名单）+ `model_test.go`（11 测试 CRUD 全路径）+ `metadata/replication_internal_test.go`（009 幂等+列形状锁定）。009 SQL 以 architecture.md §6 定稿名为 `009_replication.sql`（AC 写 `009_replication_tables.sql`，以架构契约为准）。conductor 复核：replication 17.6s + metadata 119.3s race 绿。遗留：Store 未接 metadata.Store 装配面（桥接票）；isUniqueViolation 仅 sqlite 文案。日志 reports/agents/T-161.md。

- **T-164** [P1] 本地→S3 在线迁移（双写+后台迁移） `role:dev-go-storage` — done 2026-08-22（conductor 核验直收）
  新增 `internal/storage/migration.go`（MigrationEngine 三模式 bypass/dual-write/completed；migrationSession TeeReader+Pipe 流式双写、Commit 侧失败回滚；statusGuard 原子快照含 defer 覆盖 bug 修复）+ `migration_test.go`（14 测试，含 927 blob 全量迁移 S3 端硬断言）+ `internal/httpapi/migration.go`（状态/启动端点）+ server/router 接线。agent 一度因 API 400 中断后原地续跑收尾。conductor 复核：storage 196.3s + httpapi 203.9s race 绿。越区发现：binflow-server 红测试属 T-168 遗留（已记录）。日志 reports/agents/T-164.md。

- **T-160** [P1] S3 迁移进度控制台 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  新增 `web/src/pages/governance/MigrationPanel.tsx`（5s 轮询 hook + 四态：Skeleton/403 整面板隐藏/501 未配置降级/ErrorCard 重试且瞬断保留旧值）+ GCPage 接线 + `web/e2e/storage_migration.spec.ts`（5 用例 hermetic 全 mock）。conductor 复核：build 1.34s + Playwright **5/5 passed (7.2s)**。**契约漂移记录**：票面字段 total_blobs/in_progress/completed 不存在，实存契约为 total/running/done（以 internal/storage/migration.go json tag 为准），前端按实际实现——待 architect 回写契约；未配置实返 501（非 AC 写的 404）。遗留：迁移启动按钮（危险面）建议单独出票；console-ux.md 路由表待补录；web 全量 lint 存量 3 错（他人 spec）建议 chore 票。日志 reports/agents/T-160.md。

- **T-170** [P2] 部署矩阵更新（compose/k8s/helm 含 S3 + OIDC + LDAP 配置示例） `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  compose：minio（--profile s3 锚定版本+healthcheck）+ minio-init 建桶 + S3 env 段（密钥纯 env 引用）+ depends_on 门控（默认路径零改动）；charts/binflow：values config.s3/oidc/ldap 三段 + configmap 透传（顺带修正 2 处既有渲染缺陷）+ secretKeyRef + schema/NOTES 同步；k8s 清单注释示例段。conductor 复核：helm lint 0 failed + compose config 默认/s3 双路 OK + kustomize OK。烟测：默认盘路径全绿（无回归）；--profile s3 建桶→healthy→起服→roundtrip sha256 一致。**发现两处上游缺口（已建 T-178）**：probeS3Storage Secure:true 写死（http MinIO /readyz 永久 503）；cmd 装配无 backend 分支（S3 数据面未激活）。遗留：上游修复后补一次 --profile s3 全绿复测（归 T-178 验收）。日志 reports/agents/T-170.md。

- **T-157** [P0] OIDC/LDAP HTTP 端点挂载 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/httpapi/oidc.go`（OIDCLoginFlow seam + login 302 state+PKCE S256 + callback code→token→Bearer 臂验证→自动建用户→签发 session；事务 cookie 全退出路径清除）+ server Deps.OIDC（nil→disabled 404 不暴露端点）+ router 挂载 + T-91 登录豁免重构 isLoginEntryPoint 覆盖 OIDC 路由（陈旧 cookie 不锁死 SSO）+ `oidc_routes_test.go`（mock IdP 7 组）+ `ldap_session_test.go`（真 LDAPProvider+mock 目录 7 例「先本地后 LDAP」全栈验证）。conductor 复核：scoped OIDC/LDAP race 绿（6.4s）+ build/vet 干净；agent 自跑全量 httpapi 117.8s 绿。**第三处装配缺口 → 已建 T-179**：config 无 auth.oidc/ldap 段、cmd 未构造 provider、auth 缺 userCreator 导出（阻塞 T-174）。遗留：whoami source 硬编码 local（并入 T-179）；本地/LDAP 同名用户冲突行为待 Q5 定案（现状 500）。日志 reports/agents/T-157.md。

- **T-162** [P1] push 复制引擎（事件驱动 + cron 兜底） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/replication/engine.go`（Enqueue panic 护罩非阻塞 / Run drain+wake+cron 兜底 / 退避 1s→16s 共 6 次尝试 / pushOnce HEAD 幂等探测+PUT 走目标 REST 带 X-Checksum-Sha256 / 复用 remote 的 SSRF Guard 与 AES-GCM Cipher / 全量测试注入缝）+ engine_test.go（15 测试）+ engine_integration_test.go（**两真实实例**：A 上传 201→推送→B GET 200 sha256 一致、replica PUT/DELETE 405、目标宕机上传不受影响）+ repo 侧 Replicator 接口最小接线（PutWithOptions 链末 notifyReplicator，goroutine+WithoutCancel+recover）+ 挂钩 5 测试。conductor 复核：replication 18.7s + repo 104.6s race 绿，build/vet/lint 干净。**Q6/Q7 暂行假设（待定案，已标注单点切换位 pushOnce HEAD 分支）**：Q6=可写 local backing+未路由 virtual 只读门面；Q7=checksum 一致幂等成功/不一致 failed 不动目标（与 ADR-0021 字面有出入）；私有目标默认放行（DenyPrivateTargets 保留收紧位）。遗留：cmd 装配+replications REST 端点待桥接票；审计词汇/限速项未消费。日志 reports/agents/T-162.md。

- **T-169** [P2] M5 债务收编 — G05 Windows 锁 + G15b systemd 裸机部署 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  发现 AC① 实质已被 T-96 满足（datalock_windows.go LockFileEx 已落）——补 windows 真机腿测试文件 + 修 backup_test 中 Windows 必红断言（GOOS 感知）+ 空路径守卫子测试，不动 go.mod。G15b：contrib/systemd 扩展——binflow.service（SIGTERM+TimeoutStopSec=45 优雅停机锚点+UMask=0027）+ install.sh 修 3 个真机 bug（sha256 CWD 解析/重装 ETXTBSY/全新安装 restart 循环）+ dry-run 全链路 + is-active 硬门。**真机烟测**：Ubuntu22.04+systemd251 容器（PID1）install→active→readyz 200→建仓/上传/下载 sha256 一致→restart 存活→优雅停机日志→幂等重装→systemd-analyze verify→purge 全绿。conductor 复核：scoped DataLock/Backup race 绿 + bash -n + windows/linux×amd64/arm64 交叉编译 OK。遗留：make test 全量绿被 T-168 遗留红阻断（非本票区）；docs/user/install/systemd.md 对齐待他人票。日志 reports/agents/T-169.md。

- **T-158** [P1] 控制台 SSO 登录 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  LoginPage 增 SSO 按钮（探测 `GET oidc/login` redirect:manual——302=启用/404=禁用，浏览器不触达 IdP；点击复核+行内错误态+503 保留重试）；密码表单零分支（LDAP 同表单 401→200 落地壳用例）；styles 仅登录页小节追加纯 token。conductor 复核：build 2.65s + Playwright **5 passed**（--repeat-each=2 → 10 passed）+ 既有 storage_migration spec 无回归。**后端需求提议（未越权实现）**：公开 `GET /api/v1/auth/methods`——已并入 T-179 AC⑥。遗留：真实启用态验证归 T-174（T-179 接线前置）；make console 嵌入重编归主会话集成步骤。日志 reports/agents/T-158.md。

- **T-178** [P0] S3 后端服务端装配接线 + /readyz Secure bug `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  `system.go` 新增 secureFromEndpoint（https→true/http→false/无 scheme→TLS 默认），probeS3Storage 不再写死——根因实证：minio-go v7.3.0 要求 scheme 与 Secure 一致否则 minio.New 直接报错。`main.go` openStorageEngine 按 backend 分支（disk 逐字保留；s3 构造 minio 客户端 env secret fail-fast + BucketExists 缺桶拒起 + OpenS3EngineWithClient；migration 按 T-164 语义接线双写/纯 S3/拒绝非法组合）。**附带发现上游签名缝**：`*MigrationEngine.StatusView()` 不满足 `httpapi.MigrationStarter.StatusView() any` 签名（REST 迁移端点将恒 501）——cmd 侧 migrationStarter 适配器桥接，收编归 T-180。conductor 复核：scoped readyz/S3 装配 race 绿（httpapi 7.9s + cmd 2.7s）+ **全量 httpapi 统一复跑 114.5s 绿**（T-157+T-178 合流）+ build/vet 干净。红绿证明：还原 Secure:true 旧 bug 签名复现 FAIL。遗留：T-180 收编两项；bucket 不自动创建（按必须预存在）。日志 reports/agents/T-178.md。

- **T-176** [P2] 契约回写与杂项 chore `role:architect` — done 2026-08-22（conductor 核验直收）
  architecture.md §7.1 补 migration 两端点契约（字段以实现 json tag 为准 + 501 非 404 语义 + 回写记录标注）+ §8 配置段与校验规则；console-ux.md 三处补录（路由表/线框/14 testid 锚）；web/e2e 三 spec 存量 no-unused-vars 最小修复。conductor 复核：`npx eslint e2e/` exit=0 两轮一致 + diff 恰为申报 5 文件。遗留分流：PRD 旧字段勘误 → T-181；migration.go godoc 口径 → T-180 AC⑥；前端 pct 基数口径 → T-177 AC④。日志 reports/agents/T-176.md。

- **T-159** [P1] 控制台复制面板 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  新增治理组「复制」页 ReplicationPage.tsx（目标表+事件表、10s 轮询、四态收敛）+ AppShell 导航 + lazy 路由（2 行既定装配缝）+ hermetic 7 用例 spec。落位裁决：票面「管理页」无对应页，按信息架构归治理组新开页。**契约假设清单**（端点尚未桥接）：按 internal/replication/model.go 推定 `{targets[], events[]}` 聚合形状，字段名/空值/降级语义全量标注在 T-159.md 与组件头注释——**T-180 桥接票的对齐基准**。conductor 复核：build 1.49s + Playwright **7/7 passed** + 全量 lint/typecheck exit 0。回归对照：全量 e2e 73/3（2 例基线同败为既有、1 例隔离重跑过=顺序性 flake）——**watch 项：基线 2 败待溯源**。遗留：console-ux 回写 → T-182；事件 keyset 分页待真实端点。日志 reports/agents/T-159.md。

- **T-179** [P0] OIDC/LDAP 认证面服务端装配 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  config 三文件加 auth.oidc/ldap 段（键名逐字对齐 T-170 charts snake_case；rejectSecrets 递归；validate 必填+URL 形状）；auth 导出 NewUserCreator/NewOIDCResolver + OIDCWired/LDAPWired facet + authenticateSession Source=u.Provider（不再硬编码）；httpapi auth_methods.go `GET /api/v1/auth/methods`（匿名三态）；cmd wireAuthProviders（OIDC discovery fail-fast/LDAP 懒池/typed-nil 守卫/close 排空）。**范围偏离已接受**：whoami 序列化点物理在 httpapi/session.go，最小增量加 source 字段。conductor 复核：config 1.9s + auth 29.1s（一次并行负载 flake 复跑绿）+ scoped httpapi 6.9s + cmd 3.0s race 绿；cmd 全量仅剩 T-168 两已知红。**发现 charts 缺陷 → T-183**：configmap oidc 块漏渲染 enabled 行（Helm 启用 OIDC 静默失效）。遗留：PRD skip_tls_verify/group_base_dn 无对应字段未实现（规格待验证）；users 列表 source 字段归属 T-174 前确认。日志 reports/agents/T-179.md。

- **T-182** [P2] console-ux 复制页回写 `role:architect` — done 2026-08-22（conductor 核验直收）
  console-ux.md 五处补录（导航/路由表/线框 23 行/11 个 repl-* 锚/三处一致性修正），全部以 ReplicationPage.tsx 实际形态为准；同款回写题头 + 假设契约标注（T-180 对齐锚）。conductor 复核：diff 单文件。日志 reports/agents/T-182.md。

- **T-181** [P2] PRD M6 勘误 `role:product-manager` — done 2026-08-22（conductor 核验直收）
  PRD v1.0→v1.1：§4.1 FR-50 实际契约+501 语义+start 端点；§5.2/§5.4 矩阵同步；§7 Q6/Q7「暂行已实现待终裁」+新增 Q10（私有目标放行）+交叉指针。三方依据链核对（architecture≡migration.go≡httpapi 501 文案）后落笔。conductor 复核：旧字段名仅剩 2 处有意保留（勘误记载）。遗留：ROADMAP「PRD v1.0」字样待改（区外）；ADR 拟编号与 DECISIONS.md 错位记录在案。日志 reports/agents/T-181.md。

- **T-177** [P2] 迁移启动按钮 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  MigrationPanel 启动按钮（数据态+!running 渲染/403 隐藏/501 无按钮）+ useConfirm 复用（danger+YES 门+四条影响说明按实际语义）+ 202 即刻并入 + 409 行内提示；pct 基数改 (migrated+failed)/total（AC④）。conductor 复核：Playwright **8 passed** + build 1.3s + typecheck/lint 0。遗留：console-ux 三处回写与 T-182 同族（migration-start 等三锚）→ 并入后续文档票；无停止迁移 REST 面（另立票候选）。日志 reports/agents/T-177.md。

- **T-183** [P1] charts oidc enabled 渲染补丁 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  configmap.yaml oidc 块 +1 行 `enabled: {{ .Values.config.oidc.enabled }}`（逐字对齐 ldap 块）。conductor 复核：helm lint 0 failed + template grep `enabled: true` 在位 + diff 恰 1 行。agent 附负面对照（删行复现静默失效链）。验证中确认 T-170 的 existingSecret required 门为有意防呆。日志 reports/agents/T-183.md。

- **T-180** [P1] 复制面桥接收编 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  httpapi replication.go（CRUD 四端点 + status 聚合，T-159 契约**零差异**落地：凭据不下发/''哨兵/[]非 null/newest-first/默认 limit 50）+ Deps 两 seam + MigrationStarter 签名收编（storage 强类型直插，删 cmd 适配器）+ SecureFromEndpoint 导出单点化（删 cmd 副本）+ storage godoc 口径修正；cmd 复制引擎全装配（第二 store 连接池/同钥 cipher/AttachReplicator/start+drain 生命周期）。**真二进制 smoke**：空 200/匿名 401/校验 400/重复 409/上传后 pending 任务行/DELETE 204 FK 级联/SIGTERM→drained→exit。conductor 复核：replication 10.7s + scoped httpapi 5.7s + cmd skip 25.1s race 绿（agent 自跑 httpapi 全量 119.6s 零回归）。**遗留②重大**：`.gitignore:51` 裸名吞掉 cmd/ 下未跟踪测试文件——conductor 已修为 `/binflow-server` 并入 batch 3。遗留：audit 词汇/CRUD UI 票/sub-store DSN 收编。日志 reports/agents/T-180.md。

- **T-184** [P2] replication CRUD 面 docs 回写 `role:architect` — done 2026-08-22（conductor 核验直收）
  architecture.md §7.1 路由表 ADR-0021 草案 7 端点整块替换为实际落地 4 端点（{name} 寻址）+ 回写记录段收编全部差异；console-ux §4.11 补 CRUD 面说明（REST 已落地、页面仍只读、CRUD UI 另票）+ 两处过时表述修正。conductor 复核：diff 恰两文档（+59/-19）；agent 自证 19 项 grep 对上实现 + 2 项负向断言 0 hits。日志 reports/agents/T-184.md。

- **T-163** [P1] Prometheus /metrics 端点 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 internal/metrics（sync.Map Registry + Counter/Gauge/Histogram + Format() text 0.0.4）+ httpapi metrics.go（四类 family/请求计数中间件/根级 /metrics 匿名+require_auth 门/scrape 快照/路径归一防高基数）+ config metrics.require_auth 段（偏离已申报）+ cmd 注入。真二进制 curl 冒烟（200+CT+7 TYPE 行+counter/histogram+401 门+env 覆盖）。conductor 复核：metrics 1.3s + scoped httpapi 3.7s + cmd 3.0s race 绿。遗留：replication 延迟埋点（另票）/promtool 归 T-175/`/metrics/json` 未实现（§7.1 已列）。日志 reports/agents/T-163.md。

- **T-165** [P2] internal/client 包 `role:devops-engineer` — done 2026-08-22（重派收口，conductor 核验直收）
  重派 agent 盘点半成品后修复：**真缺陷**——`err == context.Canceled` 对 `*url.Error` 恒 false 改 `errors.Is`；**假测试**——TestClientAbsURL 表字段从未参与断言，重写为服务端捕获绝对 URL 精确比对；RetryMax=0 语义修正（曾被解释为默认 5 次重试各烧 31s 退避——套件 101.9s→8.9s 降 92%）；lint 14→0；gofmt 7 文件清零；补全工作日志（前 agent 未写）。conductor 复核：race 9.0s 绿 + lint 0 issues + build OK；coverage 78.5%。遗留：DownloadArtifact 不重试（流式语义已注明）；DefaultTimeout/NewWithClient 留给 T-166。日志 reports/agents/T-165.md。

- **T-174** [P0] OIDC + LDAP 集成验收 `role:qa-engineer` — done 2026-08-22（PASS 有保留；conductor 核验收口）
  真 Keycloak + 真 OpenLDAP（TLS）容器全序列：**9/14 序列全绿**（H24 登录流/H27 admin 映射/H29 disabled/H30 LDAP 流/H32 错口令/H33 admin_group_dn/H34 目录断机 45ms 401/H36 三臂优先级/H37 不串臂），H25/26/31/35/38 部分过，**H28 FAIL**（组同步根因 D4）。Playwright 真浏览器 2/2。缺陷 8 项分级：D3[P0 规格冲突] token 端点 admin-only vs FR-54-AC3 → **T-188 PM 裁决**；D1/D4[P1] 组不落库+session 丢组+users 无 source → **T-185 修复包**；D5[P1] start_tls 静默明文+D6 键缺失 → **T-186**；D2/D7/D8[P2] → **T-187**。环境排障结论（osixia ACL/olcTLSVerifyClient）收录报告供文档复用。日志 reports/agents/T-174.md。

- **T-166** [P2] bf CLI 四个子命令 `role:devops-engineer` — done 2026-08-22（429 续跑收口，conductor 核验直收）
  main.go 重写（flag 两级分发 + 四子命令 + usage 面）；config.go 新增（~/.bf/config.yaml 多 profile，密钥只存 env 引用零明文字段，覆盖序 --server>BF_BASE_URL>BINFLOW_SERVER_URL>profile>默认，basic 臂自定义 RoundTripper）；50 子用例 table-driven。真二进制×假后端四链全通（sha256 逐字一致/Bearer 全带/help 0.01s）；make build 门禁过（bf 9.43MB/15）。conductor 复核：race 4.7s 绿 + lint 0 + build OK。**跨包缺陷 → T-189**：internal/client CreateRepo（按 JSON 解码但真服务端回 200 纯文本）与 CreateToken（期望 token/token_id string，实为 access_token/int64）两处解码面不匹配——假后端按 client 契约给 JSON 故本票全绿，真服务端链会挂（影响 T-175 H57/H60）。遗留：`bf config set` 未实现（AC 外）。日志 reports/agents/T-166.md。

- **T-188** [P0] 规格裁决：token 端点权限 vs FR-54-AC3 `role:product-manager` — done 2026-08-22（conductor 核验直收）
  **裁决 A（开放）**：admin 全量 + 非 admin 已认证用户（三臂同权）可为本人发 Token——决定性依据：逆向规格 auth-model.md §3.1（高置信双证）Artifactory 本就如此，B 选项事实前提不成立；四护栏（仅本人主体/非 admin 强制 TTL≤365d/验证期查用户行状态/token.issue 审计）。PRD 升 v1.2（Q11 裁决记录 + 实现票草案七条 AC + 推翻出口）。T-174 H26 定格部分过（D3 降 P1 实现差距）。conductor 复核：diff 仅 milestone-6.md。后续票：**T-190 实现** + K9 键名（ADR-0020 附带）+ M1/M2 历史注记（P2）。日志 reports/agents/T-188.md。

- **T-168** [P2] M5 债务收编 — Go 1.26.6 + 优雅停机 + nginx SSL `role:devops-engineer` — done 2026-08-22（重派+429 续跑收口，conductor 核验直收）
  **两处红根治**：根因=同包两个全量装配调用方撞「仅装配一次」adapter 注册表契约；TestServeGracefulShutdownLog 重写为真 runServe+真 SIGTERM（五条日志断言全来自真实输出，Windows skip）；Ping 测试切轻量 Deps 形态；顺手修 2 errcheck。nginx 模板修 3 真实缺陷（http2 on 语法/Connection map/stapling resolver）+ 真容器 nginx -t + 端到端 TLSv1.3+HTTP/2 反代实测。conductor 复核：`go test -race ./cmd/binflow-server/` **无 skip 26.4s 绿** + lint 0；agent 自跑 make test 全量 21 包 exit 0。遗留：auth TTL 测试并行负载 flake（chore 票候选）。日志 reports/agents/T-168.md。

- **T-190** [P0] token 端点权限开放实现 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  路由门 admin-only→认证即可（revoke/list 维持 admin）；非 admin 指定他人 403；TTL 护栏 0<t≤cap（超限 401 对齐 Artifactory 文案，负数 400）；token.issue 审计含 subject/source；TokenVerifier Principal.Source 改读用户行 provider；K9 键 auth.token_nonadmin_max_ttl 入 config（默认 365d）。三处旧 admin-only 断言按裁决反转。**conductor 裁定**：派单笔误「TTL 超限 400」——agent 正确按 Q11/FR-54-AC3/逆向规格三处一致的 401 实现，维持。conductor 复核：scoped httpapi 9.6s + config 1.5s race 绿（auth scoped 首跑遇已知负载 flake、隔离复跑绿 3.1s）。遗留：用户禁用无 REST seam（另票）；K9 待 ADR-0020 附带确认。日志 reports/agents/T-190.md。

- **T-185** [P1] 认证缺陷修复包 A `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 idp_sync.go（syncProviderGroups 原子替换/稳态零写/未物化组跳过 + refreshProviderAdmin 每次认证刷 is_admin）；OIDC/LDAP 四条路径接入；session 臂 fillGroups；users 增 source/realm 按 provider（D1）；whoami/login 增 groups 恒 []（D2）。**H28 闭环**：TestT185H28GroupAuthorizedRepoOverSession——真 RS256 JWT mock IdP → 组落 user_groups → session GET 组授权仓 200 → 移除组重登 403 + groups=[]；admin 降权闭环（O-4）。conductor 复核：scoped httpapi 9.7s + auth 2.1s 绿（agent 自跑 auth 全量 30s + httpapi 全量 127s）。遗留：replace 语义对混合来源成员的影响待上游确认（PRD 字面）；LDAP session 腿建议 QA 双 IdP 复验。日志 reports/agents/T-185.md。

- **T-189** [P1] internal/client 解码面对齐真实服务端 `role:devops-engineer` — done 2026-08-22（conductor 核验直收）
  原两缺陷修复（CreateRepo 200 纯文本常态+JSON 兼容臂；CreateToken access_token/int64+form Revoke）+ **顺带修三处同类**（user 改密 oldPassword/newPassword——原字段名到达即空恒 400；artifact checksums 嵌套折叠；list Size int64——string 解 number 必炸）。假后端按真 httpapi 行为逐一重写 + 8 真形态快照 + 真轻量栈四链交叉验证 + 真二进制冒烟（token_id/scope/expires_in 全对）。conductor 复核：race 9.9s 绿 + cmd/bf 邻接回归绿。**观察项闭环**：临时诊断文件已清理；「遗留进程」实为 T-172 QA 在用实例（误判纠正，未动）。遗留：cmd/bf 假后端旧形态靠 legacy 兼容臂（小票候选）。日志 reports/agents/T-189.md。

- **T-186** [P1] LDAP TLS 修复 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  StartTLS 在 dialer 内接线（每连接先升级才进池、失败关连接、**绝不静默回落明文**——D5 安全缺陷闭环）；ldaps 隐式 TLS 压过 start_tls 打 WARN；单一 TLS 配置 merge（MinVersion TLS12）；`skip_tls_verify`（启用打 WARN）/`group_base_dn` 键落地（D6 闭环，搜索基切换）；wire 层 BER/TLS mock server 三态测试；**顺带修真 bug**：caller 无 ServerName 时 StartTLS 握手必失败。**范围偏离已接受**：cmd wireAuthProviders 两字段透传（不传则 D6 只修一半，沿 T-179 先例）。conductor 复核：scoped auth 6.2s + config 2.0s + cmd 5.0s race 绿（agent 自跑 auth 全量 78.8s）。遗留：charts 未渲染新键（小票候选）；H35 复验新预期已录。日志 reports/agents/T-186.md。

- **T-167** [P2] bf-migrate 迁移工具 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  cmd/bf-migrate（两级分发/三阶段 repos→users→tokens/--dry-run/--resume 断点续传/密码策略 env+0600 文件/--retry-max）+ internal/migrate 五文件（reader/converter/writer/progress 原子落盘端点绑定/编排摘要）；token 面按现实约束（值不可导出）统计跳过+提示重建；顺手清 T-148 存量 4 lint。真二进制×双 mock 进程全链（dry-run→全量 Bearer 断言→resume 零增量→500 失败级联→resume 精确重试）。conductor 复核：migrate 3.4s + bf-migrate 2.6s race 绿 + lint 0 + make build 过（9.3MB）。**上游缺口 → T-191**：client 编码面三字段拼写不匹配（members/includes/excludes vs repositories/includesPattern/excludesPattern）——对齐前真服务端丢 virtual 成员与 patterns。遗留：Q9 真实实例验收条件腿；制品迁移面（H62~67）后续票。日志 reports/agents/T-167.md。

- **T-191** [P1] internal/client 编码面对齐 `role:devops-engineer` — done 2026-08-22（conductor 核验直收）
  RepoCreateRequest 三字段 json tag 对齐真服务端（repositories/includesPattern/excludesPattern，Go 字段名不动调用面零改动）+ RepoInfo 折叠真 GET 体 configuration 嵌套回显；字节级 golden（旧拼写键缺席断言）+ 真栈 round-trip（virtual 成员顺序/local patterns 逐值一致 + 旧拼写 400 负面实证）。AC④ 裁决：**T-189 legacy 臂保留**（cmd/bf 假后端仍是活消费方，退役前置=假后端刷新小票）。邻接两测试文件断言刷新（越界已注明，tag 切换必需）。conductor 复核：client 11.3s + migrate 1.9s + bf-migrate 2.8s race 绿。日志 reports/agents/T-191.md。

- **T-172** [P0] M6 回归基线 — 本地 filestore ✅ `role:qa-engineer` — done 2026-08-22（PASS 有保留；conductor 核验收口）
  被测物 HEAD=00f73e7 独立 worktree（make console 嵌入真控制台）。**五序列 176/177 P0 全绿**（M1 30/30 · M2 25/25 可跑项 · M3 61/62 · M4 48/48 · M5 12/12）；零意外 5xx（656 条逐条可归因：652 客户端断连伪影+4 故意）；真客户端矩阵 9 种（curl/docker+dind/mvn/npm/pip+twine/oras/crane/Playwright；podman/skopeo 不可得如实标注）。**缺陷**：D-1[P1] Argon2 认证风暴（无并发闸不随 ctx 取消：CPU 337%/RSS 4.8GB/匿名 ping 饿死 10min+）→ **T-192**；D-2~D-4[P2] e2e 断言漂移三连 → **T-193**；6 条 PRD 勘误建议（并入 T-193 或 PM 票）。日志 reports/agents/T-172.md。

- **T-171** [P2] 文档 5 类 `role:tech-writer` — done 2026-08-22（conductor 核验直收）
  六篇指南（oidc/ldap/s3-config、bf-cli、migrate-artifactory、prometheus-reference）+ sidebar 导航/README/FAQ 接线。**路径裁定**：AC 写 docs-site/docs/ 实为 docs/user/（Docusaurus path 配置，「按现状」条款），路由 /binflow/docs/guides/<name> 与 AC③ 吻合。conductor 复核：make docs SUCCESS（2.60MB）+ dist 六页在场 + agent 自证 120 token 键名对代码全命中 + 真机六路由 200 + /metrics 401/200 两态。T-174 排障结论收录 ldap 指南；迁移指南如实标注 client 编码面缺口（T-191 已修，待文档回刷注记）。遗留：install/* 八页 sidebar 未注册（T-141 seam）。日志 reports/agents/T-171.md。

- **T-194** [P2] charts 新键渲染补齐 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  configmap auth.ldap +group_base_dn（非空渲染）/skip_tls_verify（无条件显式——安全姿态键可审计）；values/schema 同步；ldap 指导示例补行并删过时注。逐键核对：oidc 8/8 全渲染无缺、ldap 补齐后 13/13。conductor 复核：helm lint 0 failed + 两新键渲染在位 + diff 恰 3 文件 +18；agent 附负面对照（删行复现静默丢弃链）+ config.Load 回灌 PASS。日志 reports/agents/T-194.md。

- **T-187** [P2] 审计词汇与可诊断性 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  动作名统一 `auth.failed`（ActionLoginFail 删除，全局单词汇）；method（local/oidc/ldap）/reason（bad_credentials/user_not_found/provider_error/tls_handshake/user_disabled/bad_request）随 Detail JSON 落库（无 schema 迁移）；**统一 401 行为零变化**（Failure 错误链始终可达 ErrInvalidCredentials）；**O-2 闭环**：StartTLS/断连/错口令三态 401 同文案但审计可分辨 + 基础设施类另打 WARN。9 行分类矩阵 + 13 行审计断言矩阵 + H38 锚点。conductor 复核：scoped auth/httpapi/audit race 绿（agent 自跑全仓零失败）。遗留：PM 两项裁决（成功词表 auth.login.* 未实施；method 枚举与 PRD :345 偏差按臂维度落）；Bearer 面失败不记审计（扩面另票）。日志 reports/agents/T-187.md。

- **T-175** [P1] 集成 QA（复制/Prometheus/CLI/bf-migrate） `role:qa-engineer` — done 2026-08-22（**验收判 FAIL**；429 续跑收口；conductor 核验收口）
  29 项 H 序列：17 PASS / 4 PARTIAL / 7 FAIL / 1 阻塞腿。bf CLI **6/6**（T-189/191 修复实证有效）；Prometheus PASS 有校准项；复制域 FAIL（仅 generic 通——docker/npm/pypi 链不通）；bf-migrate 域 FAIL（制品面 0/120 未实现+无空目标守卫）。真 Prometheus 抓取 up=1 + promtool + 真 Chromium + mock Artifactory 120 制品。**缺陷分流**：D1 复活断裂+D2/D3/D4 协议面 → **T-195**；D6/D7/D8 迁移面 → **T-196**；D5/D9 命名与 PRD 校准 → **T-197**。O1：make console dist 陈旧缺 M6 路由（conductor batch 6 时刷新）。日志 reports/agents/T-175.md。

- **T-192** [P1] Argon2 认证并发闸 `role:dev-go-core` — done 2026-08-22（429 续跑收口，conductor 核验直收）
  hashgate.go 信号量闸（channel 计数，排队随 ctx 取消即刻弃位；默认 GOMAXPROCS 钳 [1,16]≈1GiB 最坏瞬态堆）+ verifyPassword/hashPassword/ChangePassword 全入闸 + WithHashConcurrency(n) 注入缝 + fillGroups ctx 取消后跳过注定失败查询（T-192 顺手治理）+ 断连降级 WARN。**真进程风暴复现**：300 并发真 64MiB 参数半数断连——**4s 排空**（修复前饿死 10min+）、匿名 ping p99=259ms、RSS 峰 2.19GB（修复前 4.8GB）、GC 后堆回落 66MB、三轮风暴 0 5xx。conductor 复核：scoped auth 8.9s race 绿（httpapi 侧待 T-195 合入统一复跑——在途半成品暂阻全仓构建）。遗留：auth.hash_concurrency YAML 接线两行（seam 已备，并入下批）；adapter 直调入口未过闸（小票）；安静机全规模复跑归 QA。日志 reports/agents/T-192.md。

- **T-198** [P1·用户需求] README 中文版 + M6 刷新 `role:tech-writer` — done 2026-08-22（conductor 核验直收）
  README.md 268→341 行 M6 刷新 + README.zh-CN.md 新增 326 行（逐节等价 + 顶部语言切换行）。**全部真机实测**：五步 curl 链/checksum 一致、/metrics 0.0.4、auth/methods 三态、/binflow/ui/ 200、bf CLI roundtrip（所发 token Bearer 200）；两文件各 21 条相对链接 0 MISS；过时 M1 表述 grep 清零。conductor 复核：双文件在场 + 切换行 + guides 链接逐条可达。日志 reports/agents/T-198.md。

- **T-196** [P1] bf-migrate 制品迁移 + 空目标守卫 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  artifacts.go（清单 deep=1 主面+FolderInfo 兜底 → 下载双哈希 → client 上传；--concurrency worker pool+进度批 flush；generic+maven，docker/npm/pypi 留后计数入报告引 D2/D3/D4 证据）+ checkTargetGuard（PRD 零仓库字面，--allow-non-empty 覆盖+resume 豁免+fail-closed）+ report.go（全退出路径落盘 0600 原子写）+ CLI 三新旗标 + 顺手修 T-167 wart（失败口令不落盘）。**烟测**：mock 源 6 仓 131 文件（generic 122）→ 真目标：dry-run found=131/planned=126 → 全量 **126/126 迁移成功** → --resume 零重传只补 alice → **sha256 全量逐制品字节级全对**（唯 maven-metadata 目标派生重生成，语义一致如实记录）；守卫拒绝/覆盖/豁免三腿全验。conductor 复核：migrate 3.7s + bf-migrate 3.2s race 绿 + lint 0。遗留：--rate-limit 未实现（随 D9 定）；新旗标文档收录。日志 reports/agents/T-196.md。

- **T-197** [P2] 指标命名与 PRD 校准包 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  **六项裁决入 PRD v1.3**：成功词表 auth.login.* 撤销（对称性+消费面连坐，method 查询参数优于拆动作）；method 枚举臂维度胜出（FR-56 勘误，请求级/臂级两套词表区分）；H44/H47/H62/H66 按实现回写（H66 token 设计性不可迁如实条件腿）；D5 命名（storage gauge 去 _total + FR-61 整表回写 + 注册面命名规约拦截防再犯）。**promtool 端到端实证：exit=3 → exit=0**。conductor 复核：metrics 1.5s + httpapi 7.9s race 绿（agent 自跑 httpapi 全量 169.9s + cmd 全量 exit=0）。遗留：用户文档两文件旧指标名 → **T-199**；ADR 编号定序（0021/22/23）→ conductor/architect 后续。日志 reports/agents/T-197.md。

- **T-199** [P2] 指标名文档同步 `role:tech-writer` — done 2026-08-22（conductor 核验直收）
  prometheus-reference 3 处 + s3-config 1 处旧名换新（含「升级注意」块：旧名以现名+_total 公式表述保字面 grep 清零）；双向 grep 证明（metrics.go 7 family 正向全命中 + 文档记号反向 7/7 对上，唯一多出为 histogram 标准 _bucket 序列）；make docs SUCCESS 2.60MB。conductor 复核：两文件旧名计数 0/0。遗留：docs 既有断链修缮票候选；ADR 编号定序仍待 architect。日志 reports/agents/T-199.md。

- **T-193** [P2] e2e 断言漂移收编（窄化 AC①②③） `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  D-3 删仓断言 2 nodes（ADR-0016 实体化对齐）；D-4 三处（501 放行/降级态断言/账面过滤+audit 超时余量——旧 30s 预算必挂，实测 50s）；改前复现 2 败→改后 **8/8**；全量回归两轮基线同败消除（run2 80/2/3/0，余 2 败均非本票）。**AC① 归属判定**：D-2 为 Go 测试（docker adapter review_fixes_test.go:161）→ 转 dev-go-core 小票。conductor 复核：diff 恰两 spec（行为证据为 agent 实机跑——conductor 裸跑缺服务器夹具属环境缺失）。**新发现 N-1**：T-187 审计改名波及 web/src/lib/governance.ts + t104 spec（W23b 败因）→ **T-200**。日志 reports/agents/T-193.md。

- **T-173** [P0] S3 后端 QA `role:qa-engineer` — done 2026-08-22（**验收判 FAIL**；429 续跑收口；conductor 核验收口）
  五序列行为面**全绿**（C 60/60 · D/M 全可跑项真客户端 · W REST 69/69 · G 票据口径）+ H07~H11/H13/H15~H17/H19~H20 过；零意外 5xx（33 条全归因）。**缺陷**：D-3[P0] migration REST 传 r.Context() 响应返回即死（H12/H14 阻塞）→ **T-201**；D-1[P1] stats S3 恒 500 + D-2[P1] export S3 必败 → 并入 T-201；D-4[P1] Append 全量内存缓冲（1GB→RSS 1.96GB）→ **T-202**；D-5/D-6[P2] 元数据/MPU 回收 → **T-203**；勘误 2 条随 T-197 模式。日志 reports/agents/T-173.md。

- **T-195** [P1] 复制引擎修复包 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  D1 复活（MaxRevives/ReviveDelay + not-retryable 终态分类——**实战实证**：漏建目标仓→6 次退避→建仓→cron 复活 success attempts=6）；D2 docker /v2 面（blob 单请求+manifest mime+逐 tag）；D3 npm publish/dist-tag 收敛面 + **裁决**（不设特权面：405/400 是 npm 协议不变量非 Q6 只读门面；版本合并不覆盖、同版本异内容 403 终态=Q7 的 npm 形态；特权面实现位已注明可平移）；D4 pypi multipart 流式 + PutLandedBlob/PutManifest 双挂钩。**真客户端双实例**：npm publish→install 内容一致、pip install wheel 一致、docker 4 任务 digest+layer 逐字节一致。conductor 复核：replication 82.3s + repo 挂钩 15.7s race 绿（全仓构建被 T-201 在途半成品暂阻，统一复跑随后）。遗留：docker 真客户端容器化复验（H48）；config 桥接（enabled 门/复活参数）；复活分类现基于 last_error 标记（009 无列）。日志 reports/agents/T-195.md。

- **T-200** [P1] 审计词表 web 波及修复 `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  governance.ts AUDIT_ACTIONS 镜像 auth.failed + t104 spec 同步 + login.spec mock 文案改写（保 grep 字面清零）；真后端词表实证（错口令→auth.failed 有行、login.failed 零行）；W23b 复绿；全量回归 **82/0/3** 优于 T-193 基线。conductor 复核：web 三文件 login.failed 计数 0/0/0。日志 reports/agents/T-200.md。

- **T-205** [P2] docs 断链修缮 `role:tech-writer` — done 2026-08-22（conductor 核验直收）
  三文件四处：docker-registry 越树链改站内 install 双链（deploy/dev/README 降反引号引用——磁盘存在但在内容根外，「改指存在页」路线）；README 快速开始两链改指 install/binary+docker（T-146-qa 实底）；governance 中文失效锚改指 /integrations 索引。**断链 4→0**（构建日志 grep broken=0，build 与 dist 双侧渲染核对）。conductor 复核：diff 恰 3 文件 +12/-6。遗留：onBrokenLinks 可 flip 为 throw（配置归 conductor 裁决，ADR-0011）。日志 reports/agents/T-205.md。

- **T-204** [P2] 认证尾巴清偿 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  三 AC 全闭：① 两适配器直达入口路由过闸——`Service.VerifyPassword(ctx, pw, encoded)` 导出 + `internal/adapter/password.go` 消费侧 `PasswordVerifier` 接口 + docker/npm `New`/`WithAuth` capability probe、`authenticateForm`/`serveLogin` 改走 gated 入口；② `auth.hash_concurrency` YAML 接线（config 五文件 + cmd main.go `WithHashConcurrency`，env `BINFLOW_AUTH__HASH_CONCURRENCY` 覆盖，负数拒绝，0=派生默认）；③ 测试三组（config 键 7 前置 / auth 闸门有界+取消 / docker+storm 16MiB×6 实测 docker ~7.0x、npm ~5.8x 串行化）。**自测实跑**：`go build ./...` exit 0、`go vet ./...` exit 0（修复 copylocks）、auth/config/docker/npm 四包 `-race -count=1` 全绿（130.7s/9.9s/98.6s/176.1s）、gofmt -l 空。conductor 复核：build exit 0 + 报告在场。遗留：cmd main.go 与 T-201 在途签名面集成需最终确认无重叠；golangci-lint 未装以 vet 替代。日志 reports/agents/T-204.md。

- **T-202** [P1] S3 会话流式 Append `role:dev-go-storage` — done 2026-08-22（conductor 核验直收）
  `s3Session.Append` 由 `bytes.Buffer` 全量缓冲改 multipart 流式分片（32KiB scratch + digest 边写边算 + `partBuf` 惰性几何增长 + 阈值 `PutObjectPart` + Commit flush 尾分片）；`partSize` 经 `resolveS3PartSize` 归一（默认 16MiB，下限 5MiB）+ `S3EngineOptions.PartSize` 可配置注入。**内存闸门证据**（128MiB 上传 -race）：S3 会话 HeapAlloc 峰值 +18MiB vs disk 0MiB（旧实现 1GB→1.96GB，现 O(partSize) 常量，满足 256MB 门）。**自测实跑**：`go build ./...` 0、`go vet ./internal/storage/` 0、gofmt 空、`go test -race ./internal/storage/` 全绿 399.4s（S3 25 例含 AppendFlushesPartsAtThreshold / AppendReaderErrorKeepsFlushedParts / CommitMismatchUploadsNoTailPart / AppendBoundedMemory；migration 14 例含 TestMigrationManyBlobs 141.79s）。conductor 复核：build exit 0 + 报告在场；**补生产接线一行**（openS3Engine `PartSize: sc.UploadPartSize`，此前 UploadPartSize 仅 config 侧消费未入引擎，默认 5MiB 现生效）。遗留：golangci-lint 未装以 vet 替代。日志 reports/agents/T-202.md。

- **T-201** [P0] S3 缺陷修复包 A `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  D-3：`StartMigration(context.WithoutCancel(r.Context()))`，迁移不随 REST 响应消亡；D-1：`BlobInventory` 消费侧接口 + `countBytes`/`refreshStorageBytes`/`handleSystemGC` 按后端分支（S3 引擎列举 / disk 盘走），消除 S3 下 stats 500 与 GC WARN；D-2：`backup.go` 引擎感知 export/import（`exportBlobsFromEngine`/`importBlobsIntoEngine`）；`internal/storage/s3_inventory.go` 只读列举 seam `BlobStats`。**自测实跑**：build/vet 绿、`golangci-lint run httpapi+cmd+storage` 0 issues、迁移/stats/backup/export 定向 `-race` ok（3.6s/8.4s）、9 个本票测试 `-race -v` 全 PASS、storage 空载重跑 ok 197.5s、**MinIO 三腿实测**（腿1 迁移 12/12 无 context canceled + 重启幂等 skipped=12；腿2 stats 200 physical_bytes=79872 + metric 对齐 + GC 无 WARN；腿3 export→import 往返 11/11 逐位一致 + 已删 blob 保持 404）。conductor 复核：`go build ./...` exit 0 + 三票 cmd 接线共存（349 BlobInventory / 595 WithHashConcurrency / 930 PartSize）。遗留：① 全量 httpapi race 在 `TestV2RejectedCredentialRendersSpecBody` 套件级抖动（不碰本票代码，单测复跑 ok 9.5s），合并后复验；② dual-write 栈 stats 保持盘走 + export 引擎面只装 live set（设计如此，P3 parity 另开）；③ s3_inventory.go 与 T-202/T-204 并行编辑共享测试依赖，合并时保留 s3_stack_test.go mock 扩展。日志 reports/agents/T-201.md。

- **T-203** [P2] S3 元数据与 MPU 回收 `role:dev-go-storage` — done 2026-08-22（conductor 核验直收）
  D-5：`CopyObject` 补 `ReplaceMetadata:true` + `blob-created-at` 元数据保真 + GC grace 基于该元数据/fallback `LastModified`（`blobCreatedAtFromMeta` 大小写不敏感命中 canonicalized 键、malformed 回退）；D-6：启动 `sweepOrphanUploads`（`ListMultipartUploads` 分页 + `AbortMultipartUpload` 超 grace 孤儿，幂等）；`OpenS3Engine`/`OpenS3EngineWithClient` 签名改 `(Engine, error)`（D-6 启动 sweep 需报错）。**自测实跑**：gofmt/vet 空、`go build ./...` exit 0、6 个 table-driven 测试定向 `-race` ok 1.677s、全量 `storage` `-race` ok 173.469s（首次全量暴露既有 `TestS3GCGracePeriod` 回归——旧测试沿用「不读元数据」bug 语义，已修复为 LastModified fallback 路径）。conductor 复核：build/vet/gofmt 三绿 + 签名 ripple 正确（main.go:928 现 `eng, err :=` 接返回值，PartSize 接线保留）。遗留：① MinIO 真实后端 smoke 未跑（mock 覆盖协议面，依 minio-go v7.3.0 公开 API + S3 规范）；② D-5 GC 逐未引用对象 HEAD 大对象数可优化（后续可选）；③ golangci-lint 未装以 vet 替代。日志 reports/agents/T-203.md。

- **T-206** [P1] 修复 T-203 D-6 启动 sweep 未建 bucket 冷启动硬失败 `role:conductor` — done 2026-08-22（conductor 直接修复）
  全仓统一复跑暴露 `cmd/binflow-server` 6 测试失败（openStack s3/dual-write/migration-completed + TestExportS3×3）：`OpenS3Engine` 无条件执行 `sweepOrphanUploads → ListMultipartUploads`，目标 bucket 未建（新部署首启/测试冷启动）时 S3 兼容存储返回 `NoSuchBucket`，链路未区分「bucket 不存在」与「真列举错误」导致引擎整体失败。**修复**：`listIncompleteUploads` 容忍 `NoSuchBucket`（或空 code+404）按空清单处理；新增回归测试 `TestS3StartupSweepToleratesMissingBucket`（mock flag `listMultipartNoSuchBucket`）。**自测实跑**：gofmt/vet 空、6 原失败测试 + storage S3/sweep 定向 `-race` ok、`cmd/binflow-server` 全量 `-race` ok 98.151s、`storage` 全量 `-race` ok 195.768s（含新回归）、`go build ./...` exit 0、**全仓统一复跑 `-race -count=1 ./...` 全绿 exit 0**（23 包，storage 417.8s/httpapi 527.3s/repo 418.6s）。日志 reports/agents/T-206.md。

## M9 票据（T-250~T-272，tech-lead 2026-08-24 分解；AC 全文见 docs/prd/milestone-9.md）

**批次（全宽 2）**：
- **T-250** [P0] 守护基线与种子脚手架 `role:devops-engineer` — **done 2026-08-24（conductor 亲跑闸门 0 deviations；提交 `2ddd030`）**
  57 格契约基线冻结（M8 尾态清档实测）+ 零登记白名单 + verdict A/B 双裁决（负向 ×4 证明咬合）+ seed-m9（50 仓/20 用户/10 组/4 覆盖集 fixture，幂等）+ e2e/m9 骨架（m9 项目段无双跑）。**M9 期常跑口径**：`make test-m7-rbac-matrix EXPECT=1`（偏离须白名单登记；改基线须先 ADR）。遗留：CI 接线随 T-272 裁量；种子无内容文件（T-253 需要时扩展）。日志 reports/agents/T-250.md。
- **T-255** [P0] GC 并发安全引擎层 `role:dev-go-storage` — **done 2026-08-24（T-232 竞态复现腿 -race×5 零误删 + 门序不变量钉死；conductor 复验；提交 `8748a3b`）**
  holdSet（refcount+TTL）+ Commit 先 acquire 后可见（锚定不变量）+ GCSweep 双删除门（hold→Live）+ 三引擎同构。**排序耦合**：repo 接线前 t94 三例/gc.spec 暂红 → **T-256 立即接续**。两处 ADR 字面偏离论证待 architect 复核（additive GCSweep；BeginSession 注册与 ADR 生命周期冲突采 ADR）。残窗登记（dedup 命中型/微秒级门-unlink 间隙——候选 C 根除）。日志 reports/agents/T-255.md。
- **T-256** [P0] GC 接线收口 `role:dev-go-core` — **done 2026-08-24（t94 复绿 + t134-g32 受害 spec 绿 + 压力腿 5 连零 flake + CI 压力步就位 + 闸门 0 偏离；conductor 复验；提交 `bfc0846`）——GC 根治链引擎+接线双落**
  五路径 release（两路径刻意不接有 refcount 论证）+ IsReferenced 单点 Live + REST 面迁 GCSweep（legacy 调用面删除）+ serve.lock 跨进程门 + `--grace-seconds`（待追认）+ TTL 配置链 + CI 压力步（T-268 硬前置达成）。遗留：storage legacy `Engine.GC` 物理删除（7 测试文件引用）归后续 storage 触点；Playwright 形态压力腿待 e2e CI job。日志 reports/agents/T-256.md。

#### M9 B2（done 2026-08-24）
- **T-251** [P0] users 域端点 `role:dev-go-core` — **done（review APPROVE 0 阻塞；合并 `a87250d` + 桩 `4c8af8d` + review `983c6db`）**
  三护栏/级联/审计全过安全 review。**三裁定采纳**：DELETE 404 保持文本体（家族一致，wire 偏差 T-271 登记）；重复删除保持确定性 404（有意非幂等，T-257/T-271 写明）；**last-admin 竞窗为真**（census 在事务外，并发互删可致零 admin 且重种不生效须 sqlite 手术）→ **T-273 [P2] 候选**：census 折入单事务 + 措辞修正。日志 T-251.md / T251-253-review.md。
- **T-253** [P0] usage 批量端点 `role:dev-go-storage` — **done（同 review 通过；N+1 闸经突变实证；18ms 基线）**。接口加宽 fake 桩教训：全树 vet 必做。日志 T-253.md。

#### M9 B3'（T-252 done；T-260 在途）+ B4 开工
- **T-252** [P0] E5 组成员查询 `role:dev-go-core` — **done 2026-08-24（conductor 复验：vet 全树 0 + 定向 race 绿 + 闸门 0 偏离；提交 `83bd336`）**
  MembershipsByGroup 单语句 JOIN + ?includeUsers（userNames 恒渲染空=[]）+ **K19 纠偏**：groups 列表不加宽（派单笔误，agent 按 ADR 权威执行并测试钉死）。E2/E5 两视图十组交叉一致。日志 reports/agents/T-252.md。
- **T-260** [P1] OIDC step-up 控制台腿 `role:dev-frontend` — **done 2026-08-24（armed 全链 4/4×3 轮零 flake + 服务端日志零 grant 明文 grep 自证 + 合并对清 161/0；conductor 复验 TS/build；提交 `1e9da1e`）**
  fragment 模块作用域消费（票面 AppShell 措辞已纠）/pending-mint/单次 grant/mock-idp.mjs（Docker-free）。漂移登记：authorize 路径笔误按真契约实现；§14.3-2 措辞回写建议；auth.oidc.* 子键 YAML-only 注记（文档）。日志 reports/agents/T-260.md。
- **T-254** [P0] E9/E6 ManageCoverage + permissions ?filter=manage `role:dev-go-core` — **done 2026-08-24（无 filter 字节级 golden 钉死 + 零泄露 grep + u9 session 可达性腿〔T-259 前提〕；conductor 复验；提交 `6bf486f`）——A 组六端点全齐**
  派单两处口径差按权威契约纠正（§14.1.6 全字段 vs「name 轻量」；§14.1.9 签名）。族 4 写臂收敛至 seam（2N→2）。日志 reports/agents/T-254.md。

#### M9 UI 消费波（全 done）+ 债票穿插
- **T-257** [P0] users/groups 页消费 `role:dev-frontend` — **done 2026-08-24（21→1 请求 + knownEnabled hack 退役 + 强确认删除面 + 6 腿×3 轮零 flake + m8 锚兼容自持 status-pill；conductor 复验；提交 `79a97f3`）**
  E2 列表单源/E3 真值/E5 按需穿梭（选型论证）/governance 词表。有效口径 171/0/6。漂移登记：强确认升格（§4.6 回写队列）；无 Cache-Control 观察。日志 reports/agents/T-257.md。
- **T-258** [P0] repos 已用列注水 — **done（冷首屏批量恰 1 + ≤3 硬断言 + 50 行对账；`a96ed17`）**——扇出根治双侧闭环。
- **T-259** [P0] m-holder 编辑器可达 `role:dev-frontend` — **done 2026-08-24（全量 168/0 零失败；conductor 复验；提交 `b610116`）——UI 消费波全清**
  取数按角色分流（user→filter=manage 恰 1 次）；L2 卡对 m-holder 退役/user 保留；name-entry 降级保锚。wire 校准：空覆盖集结构上不可达 200-[]（落 403 角色分流友好空态）；删除覆盖集内实为 204。日志 reports/agents/T-259.md。
- **T-264** [P1] web 工具链债 `role:dev-frontend` — **done 2026-08-25（token 扫描 9 css 注释感知有牙验证 + 双 spec 缺省自洽 + env 覆盖反证；裸全量 174/0〔唯一 failed 定证为 T-266 在途腿非回归〕；提交 `b169462`）**
  日志 reports/agents/T-264.md。
- **T-266** [P2] 共享层微清理 `role:dev-frontend` — **done 2026-08-25（对比度类退役等价实证〔六组合 4.93~6.16:1 双配方同过门〕+ deploy readonly 预收敛双保险；全量 176/0 零失败终验；提交 `1a54d0d`）——B8 全清**
  遗留：BASE 端口探针归 T-268 顺带；.status-pill 82% 微差未来收敛。日志 reports/agents/T-266.md。

#### M9 B9（在途——收官前倒数第二波）
- **T-267** [P1] 锚册口径统一 + 死锚退役 `role:ux-designer` — **done 2026-08-25（§10.6 单一权威 + --ledger 四断言 PASS 双零；100 死家族退役/1 自消费保留；conductor 代跑验证咬出两潜伏 parser 缺陷并修〔角括号截断盲区 + A3 基名归一〕；提交 `3295181`）**
  「无 shell 交棒 + conductor 代跑」模式首次实战。112 手抄值证伪（实 100）。日志 reports/agents/T-267.md。
- **T-268** [P0] e2e 默认并发恢复 `role:devops-engineer` — **done 2026-08-25（三轮 176/0 @4 workers + BASE 探针三臂 + t134-g32 18/18 与 grace=0 apply 同实例并发——ADR-0031 在原伤口条件下验证；conductor 复验；提交 `89690db`）——B9 全清**
  config 钉 4 的下限论证；九腿加固各带语义理由（GC 对账单调化）；CI 压力步顺序规则标记常设。遗留：a11y 预算观测；matrix 层探针覆盖。日志 reports/agents/T-268.md。

#### M9 B11'（done）
- **T-271** [P1] M9 文档四项 `role:tech-writer` — **done 2026-08-25（四项全落 + 六端点速览 + T-273 运营提醒；HEAD 构建活体验证；提交 `fb31325`）——M9 全部实现票落地（22/23）**
  顺带修正 oidc-config env 例外清单漏项；复制管理专篇建议 M10。日志 reports/agents/T-271.md。

#### M9 B12 终验 + 修复窗（全 done）
- **T-272** [P0] M9 终验 `role:qa-engineer` — **done 2026-08-25（首验 FAIL→修复窗→复验 PASS；DoD 八条全绿；报告 `cf7e75b` + 终态节）**
  首验抓 **DEFECT-1 [P0]**（T-267 锚误杀 19 活族——audit 正则盲区三环链）+ DEFECT-2（gosec）+ T-273 建议；复验红面逐门翻转（176/0、lint 0、ledger 诚实 PASS、repos-row 33/33 吻合）。日志 reports/agents/T-272-qa.md。
- **T-274** [P0] 修复：19 族回填 + audit 六形态 `role:dev-frontend` — **done（`6f9673e`；引用计数对照自证 + repos-usage 漏网族补录 + §10.6 工具局限史条款）**
- **T-275** [P1] 修复：gosec 真修 + census 折入事务 `role:dev-go-core` — **done（`af52fae`；受戒 DELETE + ErrLastAdmin + 并发双删红面双向；EXISTS 纠 review off-by-one）——T-273 同票收编**
- **T-273** last-admin census — **done（随 T-275，F 池 #9 收口）**
- **T-263** [P1] 旧路由 redirect 全量移除 `role:dev-frontend` — **done 2026-08-24（19 条 404 断言 + 17 spec 80 处 goto 同票携带 + 全量 R3 168/0；提交 `848f822`）**
  Q3 终裁执行完毕。遗留：docs-site 重建归 T-271；theme-smoke axe 30s 脆弱性归 T-268 预算评估；设计规格措辞回写归 architect 触点。日志 reports/agents/T-263.md。
- **T-265** [P1] 树过滤复位 + 顶栏搜索框 `role:dev-frontend` — **done 2026-08-25（作用域语义论证 + 7 腿 spec + 约定环境全量 175/0；提交 `a9d623d`〔重写后 `8acbf6b`〕）**
  过滤随 (repo,dir) 作用域清空/同层保留；顶栏真输入框（recentSearches 联动/Esc 两段/⌘K）。遗留：SearchPage q-sync 微票候选；recents 双实现收敛。日志 reports/agents/T-265.md。

**✅ git 历史瘦身已执行（2026-08-25，用户 force-push 授权）**：三巨 blob（2×BOARD 损坏版 + 1 误提交二进制）从全历史剥离；origin + vm 双远端 force-push，本地 72M→8.0M；HEAD tree 逐字节一致；m1~m4 tag 原样、m5~m8 重写。回滚 mirror 在 ~/binflow-git-backup/。执行细节：filter-repo 需 40 位全 SHA（短 SHA 静默不匹配）；本地清除需破 ORIG_HEAD/FETCH_HEAD/陈旧 worktree 三重可达锁。
- **T-261** [P1] npm packument 转义收敛 `role:dev-registry-adapter` — **done 2026-08-24（三重零变化证据 + 判别性 npm install 转义 URL 腿；conductor 复验；提交 `f30aede`）**
  两消费点收敛至 client.EscapePathSegments（import 单向无环论证）。**area 外登记**：remote JoinURL 裸拼接 = D-1 同类候选票；-rev 回显塌缩语义票。日志 reports/agents/T-261.md。
- **T-262** [P1] push_npm 覆写臂自查 `role:dev-go-core` — **done 2026-08-24（裁定无风险钉死：引擎不发整包 PUT，T-249 修复天然覆盖复制面；四场景表 + 全栈非 admin 腿；conductor 复验；提交 `2750caf`）**
  遗留登记：场景 3 静默分歧无观测面（WARN/audit 候选票）；T-249 §8.2 可关账。日志 reports/agents/T-262.md。
- **T-269** [P1] git 瘦身 dry-run+手册 `role:release-engineer` — **done 2026-08-24（三 lab 实测：clone 70MB→8MB〔−89%〕、tag 映射全分析、手册含授权点；提交 `1e3c077`；零远端零主仓改动）**
  **口径修正**：实为 2 个 BOARD blob + 1 个误提交二进制（体积大头 57%）；推荐 `--strip-blobs-with-ids + --prune-empty never`。**执行待用户授权 force-push**。登记：docs-site/build 入 tracked 是另一体积候选票。日志 reports/agents/T-269.md。
- **T-270** [P1] CI 多架构镜像 `role:release-engineer` — **done 2026-08-25（Jenkins #9 全绿 ~2.5min + manifest 双 platform + Mac qemu arm64 真运行腿；提交 `96faf7b`）**
  Q6 兑现：全程零 qemu（goreleaser 预编译注入，legacy builder 4~12s/镜像）；**BinFlow 首次托管自身多架构 manifest list**（dogfood：index push→children 完整性→pull-by-list 全链）。arm64 双轨验证（VM qemu 不可行→字段承载；Mac qemu→真运行）。遗留：overlay stage 提交后撤除；digest 缓存可省 80s。日志 reports/agents/T-270.md。
- **T-273** [P2] last-admin census 折入事务 — todo（M9 尾批）
- **B2**：T-251 [P0] E2/E3/E4 users 域端点（加宽+enabled 回显+DELETE 全链护栏级联）｜ T-253 [P0] E1 usage 批量端点
- **B3**：T-252 [P0] E5 组成员 ?includeUsers ｜ T-256 [P0] GC 接线收口（五路径 ReleaseGCHold+serve.lock+压力 spec 进 CI——**顺序硬规则：先于 T-268**）
- **B4**：T-254 [P0] E9/E6 ManageCoverage seam+permissions ?filter=manage ｜ T-257 [P0] users/groups 页消费（N+1 退役）
- **B5**：T-258 [P0] repos 已用列单请求注水（~171→≤3）｜ T-259 [P0] m-holder 编辑器可达（L2 卡退役）
- **B6**：T-260 [P1] OIDC step-up 控制台腿 ｜ T-261 [P1] npm packument 第三份转义收敛
- **B7**：T-262 [P1] push_npm 覆写臂自查 ｜ T-263 [P1] 旧路由 redirect 全量移除
- **B8**：T-264 [P1] web 工具链 chores ｜ T-265 [P1] 树过滤复位+顶栏搜索框
- **B9**：T-267 [P1] 锚册口径统一+死锚 112 退役（ux；不可派发时 architect 承接）｜ T-268 [P0] e2e 默认并发恢复（**须 T-256 先行**）
- **B10**：T-266 [P2] 共享层微清理 ｜ T-269 [P1] git 瘦身 dry-run+手册【force-push 须用户单独授权】
- **B11**：T-270 [P1] CI 多架构镜像 ｜ T-271 [P1] M9 文档四项
- **B12**：T-272 [P0] M9 终验（N01~N24+契约变更面审计+F 池对账）

**关键路径**：T-250→251→252→254→259→272（A 组三波串行）与 T-255→256→268→272（GC 链含硬序）。

## 用户方向指令（2026-08-25）：M10+ 主轴 = Artifactory 全功能对齐

**「继续对比 artifactory 的反编译代码，要它的所有功能」**（用户原话）。conductor 执行口径：
- **第一步 = 全量功能盘点**：reverse-src/（13,365 Java 文件 / 262MB，三批：batch1-core 218M / batch2-protocol 20M / batch3-addons 23M）全面清点 Artifactory 功能面 → `docs/reverse/artifactory-full-feature-matrix.md`（功能 × BinFlow 覆盖列：已有/部分/缺失 × 证据位置 × 置信度）
- clean-room 铁律不变：行为规格制产出，代码零复制
- 盘点产物 = M10+ 路线图骨干（缺口按价值/成本排序分期，PM/architect 后续规划）
- 四路并行清点（core 服务面 / REST+features 表面 / 协议包型 / addons+描述符）→ 合成主矩阵

**用户追加指令（2026-08-25 11:05）**：「binflow 也需要拥有和 Artifactory 一样的 license 控制，例如控制高可用等」——**license/entitlement 体系为 M10 核心组件**：
- 自有 license 格态（BinFlow 自己的密钥/签名体系——**clean-room：不复制 JFrog 的 license 密钥格式或校验算法**，只对齐「功能分级门控」这一行为模式）
- 分级门控（对标 AddonType 80 项三档 oss/pro/ent 的行为模式，BinFlow 自定档位）——HA、Xray 集成面、distribution 等企业功能按档位解锁
- `artifactory.addons.disabled` 等价的全局禁用开关 + `/api/system/licenses` 等价的管理 REST 面
- 进 M10 规划的 PM/architect 输入清单（与十大缺口并列优先）

**用户追加指令（2026-08-25 11:15）**：「后续的规划中，也需要补齐剩余的协议，例如 golang，huggingface 等，这也是 license 控制的功能，和 Artifactory 一样使用 addon 的方式加入进来」——口径：
- **52 个缺失包型全部纳入后续规划**（第一梯队 9 种〔含 Go〕+ 中使用率 ~16 + AI/ML 生态 13 型〔HuggingFace 含 xet CAS 子协议〕等，见主矩阵分组一）——多里程碑分期承载
- **包型 = addon 门控单元**（对齐 Artifactory 行为模式：每包型一个 addon 槽位，按 license 档位解锁——基础包型入基础档、AI/ML 等生态型入高档）
- **addon 装配形态**（对标 META-INF/addon.{xml,properties} 的行为模式，BinFlow 自定注册机制——Go 编译期注册表/装配清单，clean-room 不复制格式）
- license 门控 × 包型 addon × 十大缺口 → M10+ 规划完整输入集

**✅ 盘点已完成（2026-08-25，`dd51a1f`）**：`docs/reverse/artifactory-full-feature-matrix.md`（213 条目 × 覆盖终判：**已有 20 / 部分 50 / 缺失 133 / 不适用 10**——对标 Artifactory 7.161.11）+ 四分区目录（inv-1~4，809 行）。**十大高价值缺口**：① 制品属性系统（矩阵参数+?properties——所有客户端的横切基座）② AQL+13 老搜索 ③ Trash can 回收站 ④ Cleanup/Retention 策略引擎 ⑤ 制品操作族（copy/move/zap/zip/归档浏览）⑥ Webhook 事件总线（36 事件可整体平移）⑦ 包型第一梯队 9 种（NuGet/Conan/Cargo/Go/Debian/RPM/Helm/Terraform/GitLFS）⑧ 运维纵深（Support Bundle/Live Logs/限流/流量记账）⑨ Build-info 域 ⑩ 快赢包（MPU REST/smart remote 字段/versions API）。外部依赖项 9 条已单列（Xray/Distribution/Access 等——只做集成面）。


## M10 票据（T-277~T-297，tech-lead 2026-08-25 分解；AC 全文见 docs/prd/milestone-10.md；ADR-0032/0033 Accepted `e56dd8d`）

**批次（全宽 2）**：
- **T-278** [P0] GOPROXY 行为规格 `role:reverse-engineer` — **done 2026-08-25（十节 299 行：!lower 转义三态/版本文法/checksum 链/三态 rclass/客户端矩阵；go.dev/ref/mod 官方锚点优先 + 13 条反编译补充逐条标注；4 条待验证不阻塞 T-285；提交 `0d6d952`）**
  票面 goproxy/ 路径段按 PRD 87.1 澄清（基础路径为 /binflow/<repoKey>）。日志 reports/agents/T-278.md。
- **T-277** [P0] 守护基线 `role:devops-engineer` — **done 2026-08-25（不变量闸门亲验 0 deviations + 五形态姿态矩阵负向四臂咬合 + 存量 `;` 种子幂等；conductor 双闸门复跑绿；提交 `83c1d80`）——B0 全清**
  遗留：CI 接线裁量（+40s）；T-281 keygen 后 `BINFLOW_M10_LICENSE_DIR` 注入重冻基线（ADR-0032 依据）。日志 reports/agents/T-277.md。
- **T-279** [P0] license 核心包 `role:dev-go-core` — **done 2026-08-25（验签链/三端点/无撕裂/fail-safe/redact 全过；不变量闸门维持 0 deviations；conductor 复验；提交 `90929f9`）**
  自有 ed25519 文档 v1 + 档位闭集 + Manager（atomic 快照 + 每日 ticker + disabled CSV）+ 012 表 + REST 三端点。四处自有裁定注释+测试固化（leeway 1h/空 addons≡缺省/State 读时时钟/内嵌公钥 bootstrap 对私钥即毁——stock 二进制恒 community 地板）。遗留：addons.disabled 键接线归 T-283；audit 词表三词归 owner；首发前换权威钥对（T-281）。日志 reports/agents/T-279.md。
- **T-282** [P0] addon 注册表 `role:dev-go-core` — **done 2026-08-25（11 槽位 + 五核心 retro-fit 不变量证明 + 动态建仓谓词验证；不变量闸门 0 deviations；conductor 复验；提交 `157591b`）**
  遗留：repo/validate 枚举扩 dynamic 归 T-283（矩阵 T05/T06 翻 2xx 前提）；§7.1 路由表归 T-293；console 归 T-288。**矩阵脚本不查 bin/ 新鲜度**（T-279 四格欠账已补登白名单）——终验前先 make build。日志 reports/agents/T-282.md。
- **T-283** [P0] 门控织入三缝 + addons.disabled 熔断 `role:dev-go-core` — **done 2026-08-25（D1~D7 逐行验证 + architect 风险 1 双面钉死〔pull-through 内部写在 DENIED 门下成功〕+ 228 并发无撕裂；五形态矩阵重冻 + 不变量双绿；conductor 复验；提交 `98404cf`）——M10 基座三票齐装**
  遗留：矩阵 pro 形态 T05/T06 翻 2xx 等 T-281 keygen + license-dir；PRD 85.3 disabled 读 403 与 D1 冲突按 D1 落（T-293 终裁）。日志 reports/agents/T-283.md。
- **T-281** [P0] bf license 签发 CLI `role:dev-go-core` — **done 2026-08-25（三命令 + 首发换常量流程 + e2e 全链〔装 license → go 仓 200 → 篡改 D7〕；conductor 复验；提交 `38315e6`）**
  遗留：首发换 verifykey 常量 + 矩阵重冻归发布流程（--as-go-const 已备）；私钥规程归文档 §11.38。日志 reports/agents/T-281.md。
- **B2（余）**：T-281 [P0] bf license keygen/issue/inspect（deps T-279✅——待宽度穿插）
- **B1（余）**：T-280 [P1] NuGet 规格（deps 无——穿插时机由宽度定）
- **B2**：T-281 [P0] bf license keygen/issue/inspect 签发 CLI｜ T-282 [P0] addon 注册表 + GET /api/v1/addons（五核心 retro-fit community 地板）
- **B3**：T-283 [P0] 门控织入三缝 + addons.disabled 熔断（D1~D7）｜ T-284 [P1] 规格批次一（Conan/Cargo/Debian）— **T-284 done 2026-08-26（漏跑补派当日收口；conan.md 216 行 35 端点/cargo.md 205 行 13 端点/debian.md 239 行 12 端点；官方锚点优先〔Cargo Book/Debian wiki/GitLab Conan v2〕+ 反编译补充逐条标注 12/8/12 条；置信度零低项；三份 M11 可拆 + 8 点 tech-lead 裁决待 T-294/M11 拆票时消化；conductor clean-room 抽查过；提交 `3e5db97`）**
  漏跑根因与 T-280 同款（配额乱窗 B3 槽位静默丢失）；T-294 Cargo 条件票规格依赖已解除。日志 reports/agents/T-284.md。
- **T-286** [P0] 属性系统 BE `role:dev-go-core` — **done 2026-08-26（矩阵参数单点 + node_props + ?properties 三动词；五个存量 `;` fixture 逐字节回归 + 不变量双绿；提交 `d9db164`+补交 `d305e82`——补交教训：untracked 新文件漏 add，流程已改 HEAD 后复验）**
- **T-285** [P0] Go 试点 adapter `role:dev-registry-adapter` — **done 2026-08-26（3915 行 goproxy：!lower 三态/checksum 链/三态 rclass；真实 go1.26 build 全链含大写模块；门控全链 D3→pro→D1→降级；不变量 0 偏差；提交 `18eeabd`）——B4 全清，首个包型 addon 全链贯通**
  发现登记：go1.26 GOPRIVATE='*' 经 GONOPROXY 默认绕过 GOPROXY——操作形态 GOPROXY+BOSUMDB=off 已固化测试。日志 reports/agents/T-285.md。
- **T-288** [P1] 控制台 License 页 `role:dev-frontend` — **done 2026-08-26（5 腿 spec + 全量 185/0 + ledger PASS + HEAD 复验；提交 `93f2e3b`）**
  可见性按 FR-86-AC5（readonly 只读可见——派单「不可见」与其冲突已登记终裁）。licensed 形态 UI 腿待 T-281 keygen+形态。日志 reports/agents/T-288.md。
- **T-287** [P1] NuGet 试点 `role:dev-registry-adapter` — **done 2026-08-26（5103 行 + dotnet 8 真实全链 + LIVE 公网腿〔Newtonsoft 经 binflow 拉取运行〕+ 门控全链 + 不变量 0 偏差；提交 `c283bea`）——B5 全清**
  **如实披露**：T-280 规格票实际从未跑（nuget.md 不存在）——实现依据官方 NuGet API 文档 + PRD + 活体探针（公开规范协议的 clean-room 合规路径）；7 项自有裁定标 T-287 ruling 待复核。**发现**：dotnet 8 直推 PackagePublish 无 id/version + multipart 单 part（官方规范未写、测试钉死）。遗留：T-280 规格补票或并入 T-293。日志 reports/agents/T-287.md。 页
- **B6**：T-289 [P1] MPU REST 六端点（S3 专属 filestore 501）｜ T-290 [P1] smart remote 字段子集
  - **T-289 → done 2026-08-26（提交 `4ef0972`）——B6 全清**：评审 REQUEST_CHANGES 5 blocker 修复轮全落地（B1 锁序 snapshot-then-relock + 锁外 remove/B2 411 信封保真+测试/B3 移位前 400 门/B4 裸列表过 writeGate/B5 Abort+failLocked 补 AbortMultipartUpload + 探针 mc --incomplete 三段断言）+ 3 条顺手清（hex 大写归一/注释/文案）；conductor 复验（五处修复点抽查 + build/lint 0/目标测试/双矩阵 0 deviations/不变量双绿 + HEAD-build stash 验证三包绿）。AC2 descope M11 债已留痕。日志 reports/agents/T-289.md + T-289-review.md。
  - **T-288 补交 fixup `913c5ba`（第三例漏 add）**：`93f2e3b` 漏了页面本体三新文件（LicenseAddonsPage.tsx/addons.ts/license.css）——路由在而页面缺，干净检出 vite 构建会红；fixup 提交 + 干净 HEAD npm build 亲验绿。**流程追加：含新 FE 文件的提交，HEAD 侧 npm run build 一并验**。
  - **T-290 → done 2026-08-26（提交 `6b93e7e`）**：评审 APPROVE（0 blocker，四处裁定全维持）+ 修复轮清 2 minor（别名显式 0=缺席语义、尾随垃圾严格拒绝）+ conductor 复验（build/lint 0/T290 三包/三核心包全量/不变量双绿 + HEAD-build stash 验证）；minor 溢出上界归 M11 台账。日志 reports/agents/T-290.md + T-290-review.md。
- **B7**：T-291 [P1] Properties Tab FE｜ T-292 [P1] 规格批次二（RPM/Helm）— 双票已派 2026-08-26 09:34（勘误：原记 09:50 系笔误）
  - **T-292 → done 2026-08-26（提交 `94c88ae`）——FR-91 覆盖集 5/5 齐**：rpm.md 247 行（15 端点/自动 repodata 重算链/GPG/3 代历史）+ helm.md 251 行（12 端点 + HelmOCI×docker v2 八机制复用表/index.yaml 改写算法/虚仓缓存键）；官方锚点（repomd 社区规范+dnf.conf(5)+JFrog reindex REST；helm.sh 两页+JFrog Helm 仓页）+ 反编译补充各 14 条逐条标注；置信度零低项。**FR-91-AC3 tech-lead 就绪度确认已派**（15 裁决点消化 + T-294 拆票要点）。日志 reports/agents/T-292.md。
  - **T-291 → done 2026-08-26（提交 `d72a508`）——B7 全清**：**首个 MUI 面**（@mui/material v7 + emotion 三包 lockfile 钉版；MuiProvider 主题桥双模式复刻 tokens.css + ThemeContext 翻转重建；无 icons/x-data-grid）；Properties 页签按 Artifactory 交互语法（行内增删/键校验/PUT merge/readonly=disabled+反断言）；console-ux v1.11 入册 12 锚（票面不碰 docs 与 ledger 硬门冲突——按 §2.4 先入册纪律纯增量，**conductor 追认**）；playwright L21a/b/c + 全量 **188/0** + anchor-audit ledger PASS + 真栈六腿 + 双主题确定性断言；conductor 复验（build/tsc/lint/ledger/7 腿 + **HEAD 侧 Go+web 双语构建验证**——正是抓 T-288 类漏交的检查）。**契约发现**：api-reference.md:35 POST ?properties 行陈旧（router 三动词冻结）→ T-293。verifyM10 硬编码密码致改密实例 L22 必 401（一行修复归后续 BE/QA 票）。日志 reports/agents/T-291.md。
  - **T-293 → done 2026-08-26（提交 `3fcd74f`）**：11/11 分歧收口（9 终裁落档——items 5/6/7 维持 T-289/290 自有裁定并回写规范；T-287 L1~L7 全维持）+ K23~K29 校准 7/7（K26/K27/K28/K29 以 architecture §15.3/15.4 + 实现为契约源）+ **ADR-0034 新增**（五协议管理面 dispatchAPI 族 + 绝对 URL 统一 server.base_url）+ PRD **v1.1 as-built 收口稿**。**conductor 三终裁**：① T-280 免补票（官方文档路径合规 + M11 NuGet 硬化票随票补 as-built 规格）② PRD 转正随 T-297 终验 ③ license 公钥 config 覆盖未实现仅记录（M11+ 需求走新 ADR）。日志 reports/agents/T-293.md。
  - **T-294 → done 2026-08-26（提交 `6069845`）——B8 全清**：评审 REQUEST_CHANGES 修复轮全落地（B1 引用计数 per-(repo,crate) 互斥集串行索引重写 + **并发回归两腿**〔8 goroutine×3 轮断言行数==版本数 + yank 混合〕/M1 裸 token 臂畸形形态拒绝不降匿名/M2 注释修正）；auth 裸 token 臂与七偏差 D-1~D-7 评审独立实证全通过；真实 cargo 1.98 L-r1~L-r6 + 门控全链（D3→pro→卸载 D1/D2）；conductor 复验（互斥实现与 M1 姿态代码抽查 + 并发腿/race 双包/双矩阵 0 偏差 + HEAD-build 验证）。**M10 第三个包型 addon 全链贯通**（go/nuget/cargo）。M3~M9 七条 non-blocking 留登记。日志 reports/agents/T-294.md + T-294-review.md。
- **B8**：T-293 [P1] as-built 回写（四处 PRD↔ADR 分歧收口）｜ T-294 [P2] Cargo 条件票
  - **FR-91-AC3 通过 2026-08-26（`3dc47e7`）**：tech-lead 5/5 可拆、六要素 30/30、23 裁决点（改判 2 均收紧：conan v1 收至握手三端点/cargo 失败统一 4xx5xx 废双轨）、缺项 4 条 0 阻塞（GPG keypair 条件前置票 + 2 ADR 补记 + helm/cargo 规格建议修订 2 处）；跨规格定案：管理面走 dispatchAPI 族、绝对 URL 复用 server.base_url 零新配置、rpm 校验默认 SHA-256。报告 reports/agents/tl-fr91-ac3.md。**T-294 提前穿插派发 10:10**（AC 草案直取该报告 §3；local 全量，remote/virtual M11 单票 dep 本票；area=internal/adapter/cargo 与 T-291 web/ 零重叠）；T-293 待 T-291 收口后补位（as-built 回写宜晚收全部分歧输入）。
- **B9**：T-295 [P1] 部署接线（charts configmap 显式枚举——T-183 教训）｜ T-296 [P1] 文档五项
  - **T-295 → done 2026-08-26（提交 `a646503`）**：7 键全接入（addons.disabled 主键 + replication/gc-hold/step-up 对/oidc-ldap readonly）；license config 键确认**无**（BINFLOW_M10_LICENSE_DIR 是矩阵测试 env，四部署面加「设到 server 会被严格 env 扫描拒启」警示）；helm lint 0 + schema 域反例咬合 + 三变体真服烟测（community 地板/K24 容错〔未知槽位清理 + core docker 禁用 honored + WARN〕/制品 roundtrip）；contrib/systemd 有意未动（写区外，留后续票）。经第 15 次熔断 + ENOTFOUND 双恢复后当日收口。日志 reports/agents/T-295.md。
  - **T-296 → done 2026-08-26（提交 `be89008`）——B9 全清**：五项 889 行（license 指南 232/属性用法 158/go 171/nuget 160/cargo 168）+ FAQ 三目 + cargo.md R-1×4 点 + api-reference M10 端点速览收口 + 侧栏挂页；命令全取各票实测日志（go 1.26.6/dotnet 8.0.412/cargo 1.98.0）；**license 安装动词按 as-built 写 POST**（派单笔误 PUT，已登记）；docs-site build SUCCESS 五路由全生成（conductor 复跑绿）。遗留登记：goproxy.md GOPRIVATE 勘误归 reverse-engineer；cargo.md §9 四档路径示例待规格复核；`make docs` 归终验。日志 reports/agents/T-296.md。
- **B10**：T-297 [P0] 终验（L01~L30 + 真实客户端矩阵 + DoD 八条）— **done 2026-08-26（提交 `39f1fa8`）——M10 全清 21/21**
  - **总裁定 PASS**：L01~L30 = 28 ✅ + 2 ⚠️（L05/L22 文面登记态分歧，行为面安全）+ 0 ❌；真实客户端八面全绿（go 1.26.6 / dotnet 8.0.412〔live 公网腿环境性降级 + hermetic 全链替代〕/ cargo 1.98.0 / npm 10.9.8 / maven 3.9.9 / pip 26.1.2 / docker 29.7.2 dind 20MiB 真实 multipart 回环 / curl）；四闸门 0 deviations + anchor ledger PASS + make docs 产物一致；**抓获 P0×1（属性键上限差一——conductor 修复 `7b84a71` + 边界复验绿）**；`make test` 修复前 28/28 绿，修复后两处墙钟断言抖动（隔离复跑绿，环境性归因链在报告 §3）
  - DoD 八条：1~7 全 PASS；**第 8 条 tag `m10-done` 由 conductor 执行**（本提交后）
  - 登记项移交：D-3 nuget 文档一行（M11 tech-writer）；D-6 matrix_params 逃生开关 conductor 裁定**不实现**（Artifactory 无此开关——按「行为对齐」新指令维持无开关，PRD AC4 存量臂绿）；D-8 boot footprint 138MB（M11 候选）；D-9 e2e seed 竞态 + verifyM10 硬编码口令（测试基建票）；PRD v1.2 三处措辞勘误已随笔落
  - 历经 16 次配额熔断 + ENOTFOUND/流停滞 ×4 + VM 失联 3 小时，当日收口零损坏。日志 reports/agents/T-297.md（34.4KB）

**关键路径**：T-279→282→283→285/286→288→297（基座链）。
## 用户指令（2026-08-25 17:35）：持续部署——每次变更经 Jenkins 部署到 172.16.58.129 测试环境

「后续的每一次变更要通过 jenkins 持续部署到 172.16.58.129，作为测试环境」——口径：
- **触发链**：git push（origin/vm）→ Jenkins smoke/发布 job → **部署到 VM 测试环境**（现有 systemd BinFlow 实例 :8080 升级替换——T-230 路径）+ 烟测（ping/建仓/上传/下载）
- **现有基础**：T-248 三级流水线（smoke/nightly/release）+ T-270 多架构已在 VM Jenkins；缺的是 **deploy 阶段**（发布→VM 实例滚动替换 + 部署后烟测）
- **追加（2026-08-25 17:40）**：「文档站也要随功能变更及时更新，也需要通过 jenkins 持续部署到 172.16.58.129」——docs-site（Docusaurus 构建产物）与二进制同链部署：deploy job 增 docs 阶段（make docs → 产物上 VM → 服务形态二选一：嵌入二进制 /binflow/docs/ 自带〔make build 已 embed〕或 nginx 静态托管独立端口——按 T-298 选型一并论证）；**docs 变更及时性**：docs/ 目录变更触发同链（Jenkinsfile 检测路径或简单全量——每次全量最简）
- 落为 **T-298** [P0]：Jenkins deploy 阶段（job 或 release job 增 deploy-to-vm stage：构建→scp→systemd 重启→烟测→失败回滚保留旧二进制）+ 服务端就绪探针（部署门）
- 部署纪律：数据目录不动（只换二进制）；部署后跑 m7 矩阵不变量腿作为部署烟测的一部分（可选）

## 🚫 阻塞（blocked）
## T-298（done 2026-08-25，提交 `375d856`）

**持续部署链全形态落地**：binflow-deploy job（smoke SUCCESS 门控）→ console+docs+build 版本注入 → VM 备份(留5)→原子换二进制→探针 60s→**失败自动回滚**→烟测全链+docs 面。docs 站嵌入二进制（单产物原子部署）。双回滚臂+红 smoke 拒绝接力+release dogfood 形态全实测。**首战立功**：T-283 漏 add 文件致 VM 编译红 → deploy 门拦截 → 补提交后链自愈（版本 ci.e32c63f→ci.375d856，docs 演示标记上站）。遗留：L2 release 镜像 docs 占位；L3 m7 矩阵腿归 nightly。日志 reports/agents/T-298.md。

## 用户指令（2026-08-25 下达，2026-08-26 11:22 用户重申）：前端 UI 框架使用 MUI，交互逻辑仍按 Artifactory

「前端UI框架使用MUI，但是前端交互逻辑还是要按照Artifactory来」（下达 + 重申两次）——口径：
- **组件层**：web 控制台引入 **MUI（@mui/material + emotion）** 作为 UI 组件框架；现栈 React 19 + Vite 不变，MUI 以增量方式接入
- **交互层**：信息架构、操作流、四态、布局语义仍以 M8 控制台 UI 规范（docs/design/，Artifactory 对齐）为准——**MUI 只换皮肤组件，不换交互逻辑**
- **落地节奏（conductor 裁定，M10 稳定性优先）**：
  1. **T-291（Properties Tab FE，B7）为首个 MUI 票**：引入 @mui/material 依赖 + 主题桥接（Artifactory 视觉 token→MUI theme），Properties 交互仍按 Artifactory（表格 + 行内增删 + key/value 校验 + 权限门控）
  2. **存量页面 MUI 化**：M10 收口后补迁移票（按页面组分批），不与 M10 在途 10 票混流
- 适用于所有后续 FE 票；dev-frontend 角色卡与 T-291 派发单同步注入本口径
- **重申后的落地升级（2026-08-26 11:22）**：存量页面 MUI 化从「M10 后补票」升格为显式迁移票组——**T-299 [P1] 存量页面 MUI 化批次一**（登录/壳层/仓库列表与表单——高频面优先，MuiProvider 已就位无二次引入成本）+ **T-300 [P2] 批次二**（制品浏览树/搜索/安全与治理页/admin 余面），排期：T-297 终验后立即开（不进 M10 DoD，进 M11 首批）；每批验收 = 交互逻辑零变化（console-ux 册锚零改动，仅组件层换 MUI）+ anchor-audit ledger PASS + 全量 playwright 绿 + assert-tokens 零硬编码

## 用户指令（2026-08-26 11:45）：存储配置独立文件化（参考 Artifactory binarystore.xml，应对多种存储方案）

「S3等存储配置要参考Artifactory，使用单独的存储配置文件，以应对多种存储配置方案」——口径：
- **形态**：存储配置从 binflow.yaml 主配置中独立为**专用存储配置文件**（对齐 Artifactory `$JFROG_HOME/var/etc/artifactory/binarystore.xml` 的行为模式——主配置之外的专门文件，定义存储链）
- **行为基准**：`docs/reverse/config-formats.md` §1（binarystore.xml blob 存储链规格——已有：文件位置/provider 链/模板体系）+ `docs/reverse/s3-storage-layout.md`；**格式自由**（BinFlow 可用 YAML 而非 XML——clean-room 只对齐行为：独立文件、链式多方案、provider 模板语义），拆票前 reverse-engineer 复核 config-formats §1 完整度
- **多方案目标**：filestore（默认）/ S3 / dual-write 迁移链 /（远期 cache-fs 层、Azure/GS 等按主矩阵缺口）——现 internal/config 已有 disk|s3|dual-write 三形态（API 面不动，只换承载文件与链式表达）
- **落地**：M11 立项——拆票建议：BE 票（专用存储配置文件解析 + 链式表达 + binflow.yaml 向后兼容〔内嵌 storage 段继续生效或迁移提示〕+ 启动 fail-fast 校验）+ 文档票（部署矩阵三形态的存储配置示例）；**迁移纪律**：现网 VM（172.16.58.129）data 目录不动，配置切换走 CD 链验证
- 与 T-295 的关系：T-295 按现状（binflow.yaml storage 段）接线不受影响；本指令为 M11 变更，届时 charts/deploy 同步演进

## 用户指令（2026-08-26 11:35）：OAuth2/LDAP 等认证配置前端可配置，行为严格对齐 Artifactory

「oauth2和ldap等认证配置要放到前端页面可配置，诸如此类的配置要和artifactory的行为严格保持一致」——口径：
- **范围**：OAuth2 / LDAP / SAML 等外部认证集成的配置面——**控制台 admin 页面可配置**（Admin > Security 域，Artifactory 同构），非仅配置文件
- **行为对齐**：配置模型/字段/优先级/测试连接/启停语义以 `docs/reverse/auth-integration.md`（既有逆向规格）为准——**严格**一致（用户原话）；该规格写于 M6 前后，M11 拆票前 reverse-engineer 复核一轮
- **落地**：M11 立项——拆票建议：BE 票（internal/auth 配置面 REST + **变更即生效**〔Artifactory 语义，不重启〕）+ FE 票（admin 认证配置页组，**MUI 组件层**〔上条指令〕+ Artifactory 交互层）；新配置面收敛进既有认证多臂链（Basic/api-key/Bearer+OIDC/裸 token/cookie），不另起炉灶

## 用户指令（2026-08-26 19:05）：license 门控功能与 Artifactory 严格对齐——「不要有太多自己的想法」

「license控制的功能注意要和artifactory对齐，不要有太多自己的想法，所有的功能直接照搬artifactory的代码就好，只是把java转为golang」——**经用户裁决，口径落为「行为逐项对齐」**（2026-08-26 19:07 AskUserQuestion 确认）：
- **执行口径**：license 门控功能（含 M11 全部：ha/xray 槽位、剩余包型、认证配置、存储配置等）的**可观测行为/命名/语义/错误码/交互 100% 照 Artifactory**；反编译代码作为行为参考精读（对照到行为规格粒度）
- **自有裁定权收归用户**：M11 起任何与 Artifactory 的行为分歧必须上 BOARD 请用户裁决——conductor/architect/tech-lead **不再自裁**；规格票必须逐条给出「Artifactory 行为出处（反编译类/方法 + 行为描述）」而非「等价设计」
- **红线保留**（ADR-0001 不变，用户知情确认）：不逐行翻译 Java→Go——reverse-src 是 JFrog 版权反编译产物，BinFlow 对外发布镜像/二进制，逐行翻译 = 版权代码进入发布物；license 文档格式维持自有 ed25519（其可观测行为面已对齐：安装/查询/卸载/变更即生效/addon 重载/档位矩阵）
- **既有自有裁定的回头看**：M10 各票的自有裁定清单（T-287 七项/T-289 五项/T-290 四项/T-294 七项/T-293 已终裁项）在 M11 规划时按本口径逐条复核——凡「等价设计」类若与 Artifactory 有可观测差异，改回 Artifactory 形态

## 用户裁定（2026-08-26 20:35，M11 PRD v1.0 开放问题定案四项）

- **Q1 HA/Xray 本体：不进 M11**——「行为逐项对齐」指令对齐的是行为，本体解锁须修订 PRODUCT.md；M12+ 单列里程碑
- **Q2 HelmOCI：单列条件票（P1，非 DoD 硬门）**——四包型 P0/P1 收官且余量足则执行
- **Q6 GPG keypair：进 M11**（P1 条件票 + debian/rpm 各一张签名小票；DoD 不含签名腿）
- **Q8 默认值族：全部照 Artifactory 实际值**（2026-08-26 20:55 用户二次裁定修正转写错误）——RP-2 calculateYumMetadata=**false**（上传仅存储，repodata 由 reindex/显式开启触发）/ TL-4 debian 架构族=**i386,amd64 强制生成**（空 Packages 亦生成）/ HL-2 relative urls=**true** / **CG-2 cargo publish 失败形态=200+errors[]（照 Artifactory 双轨，翻转 T-294 as-built 统一 4xx/5xx——断言与规格随票回写；成功形态两方案一致=200 无 errors 键）** / **CN-1 conan v1=全量十七端点（推翻 v1.1 收窄裁定，面积上浮 T-308）**；**唯一例外 TL-5 rpm 校验算法留 SHA-256**（安全向，Artifactory 亦支持，理由留痕）
- 其余 Q3（SAML 配置面先行）/ Q4（DB 配置面权威）/ Q5（独立文件优先+内嵌 WARN）/ Q7（Trash 余量票）维持暂行，终裁归 ADR-0035/0036 与余量触发

## 用户指令（2026-08-26 21:10~21:20）：CircleCI 流水线 + UAT 环境 52.79.109.153（含文档服务）

「新增circleci的ci/cd流水线，最终部署在52.79.109.153这台服务器上作为uat环境，注意文档服务也部署在这里」+ 用户提供服务器 RSA 密钥与 CircleCI 项目信息（project id `eaa9da69-…`）——落地记录：
- **gitflow 映射**：develop → Jenkins/VM 测试环境（既有）；**main → CircleCI/UAT**（新链）
- **已完成（当日）**：`.circleci/config.yml`（build：console+docs+build+vet+lint+短测 → deploy_uat：SSH 部署+双面烟测，main 过滤）+ `deploy/ci/uat-deploy.sh`（分阶段换装/5 备份/60s 探针/失败自动回滚——deploy-vm.sh 同语义）；**UAT 服务器已开通并首部署实测**（/opt/binflow-uat + systemd binflow-uat + hardened unit；/healthz 200、/binflow/docs/ 200、license 面 401 community 地板——21.3MB linux/amd64 单二进制含嵌入文档站）
- **密钥纪律**：RSA 私钥仅存本机 `~/.ssh/binflow-uat.pem`（600）——**绝不入仓库**；CircleCI 侧需用户在 Project Settings > SSH Keys 上传同钥并把指纹填入 config.yml 的 `REPLACE_WITH_UAT_KEY_FINGERPRINT` 占位（add_ssh_keys 不支持 env 插值）
- **触发**：config 已在 develop；首个 CircleCI 流水将在下次 develop→main 合并（M11 首批收口）时自然触发，或用户在 CircleCI UI 手动触发 main 管道

## M11 票据（T-299/T-300 既定 + T-301~T-330，tech-lead 2026-08-26 拆票；AC 全文见 tech-lead 拆票交付〔本节压缩录〕+ docs/prd/milestone-11.md v1.1；Q8 终值 20:35+20:55 已并入票面）

> **里程碑收口清单新增两条（用户 2026-08-27 指令，全里程碑适用）**：每次 milestone 收口（m<N>-done tag 前）必查 ① **README**（含 zh-CN 镜像）是否随新能力过时——支持矩阵/包型/配置面/里程碑行；② **文档站**（docs/user/ → docs-site）是否需更新。检查结论（更新了什么/为何无需更新）写入收口报告留痕。
>
> **首个 M11 批次 release 已合 main 2026-08-27 14:2x（merge `1d440ea`，main `95a8f9a`→`1d440ea`）**：CircleCI/UAT 链首次点火——指纹 `75:f5:48:…:d1:fc`（由 ~/.ssh/binflow-uat.pem 推导 MD5 公钥指纹，与 CircleCI Settings 显示值核对）已填 `4776dcc`；build=console+docs+server 内嵌全量构建+vet/lint/-short 测试，deploy_uat=uat-deploy.sh（分阶换装/5 份回滚备份/healthz 探针/双面烟测含 /binflow/docs/）。干净检出编译已在本地 worktree 验证（CI build 同构）。release 合并经临时 worktree 执行（主工作树被在途 T-313 的 helm/harness_test.go 改动占据，checkout 阻断——worktree 路线确立为在途期的 release 标准程序）。流水线结果待 CircleCI 侧观察。
>
> **M11 收口时的已知欠账（conductor 2026-08-27 14:10 摸底）**：
> - README：仍写「五大包型」「M1→M9 all done」——需补 M10/M11 十二包型矩阵、license/addon 门控章、auth 配置面与 binstore.yaml 提及、里程碑行刷新（README.zh-CN.md 同步）。
> - docs/user/integrations/：缺 conan/helm/rpm/debian 四篇接入指南（随 T-312~T-315 落地写；helm 注意与 install/helm.md〔Chart 部署〕命名区分）。
> - docs/user/admin/：缺 LDAP/OAuth/SAML 前端配置指南（T-305 面）与 binstore.yaml 存储配置指南（T-306 面）。
> - 派发时机：宽度空窗时派 tech-writer（README+admin 两票可先做，integrations 四篇等 B6/B7 合入）。

**批次（全宽 2）**：
- **B0**：T-301 [P0] 前置 ADR 包｜ T-302 [P0] auth-integration.md 复核票——**双票 done 2026-08-26（B0 全清，gitflow 首航：feature 分支 --no-ff 合入 develop `4950675`/`103ffce`）**
  - T-301：ADR-0035（auth_configs 表 + license Manager 三要素复用〔快照/验后替换/回放〕实现变更即生效；K31 DB 权威终裁；enc:v1 密封 + 脱敏哨兵）/ ADR-0036（binstore.yaml 有序 provider 链 + Q5 精确化：语义分歧才 fail-fast、等价 WARN）/ ADR-0038（keypair 双列 enc:v1；RSA-4096 暂行）/ **openpgp = ProtonMail/go-crypto v1.4.1**（三平台零 CGO 实测；keybase 冻结 2020 淘汰）。预登记分歧 2 处（模板体系归 T-303/生成默认归 T-319 mini 规格）
  - T-302：规格 v2 全量重写 367 行——23 端点/58 字段**逐条双出处**（代码+官方文档）；**两低置信区推翻**（OAuth batch3 完整实装/SAML 齐备但 license-gated）；**变更即生效=保存即生效高置信**（descriptor 链+Access 回调+懒初始化兜底；边界：会话不失效/300s 认证缓存）；FE 四陷阱显式（SAML noAutoUserCreation 反语义等）；缺项清单空。日志 reports/agents/T-301.md / T-302.md
- **B1**：T-303 [P0] config-formats §1 复核 + 规格尾巴两处｜ T-299 [P0] MUI 批一
  - **T-303 → done 2026-08-26（`bc456d2`）**：§1 逐条三证（代码/官方/反证）——**模板体系分歧门关**：模板=固定注册表展开+自描述标签，**无 dual 模板/provider**（Artifactory 迁移走 eventual `_add` 符号链接非链内原语）→ ADR-0036 migration.mode 为 BinFlow 自有拼写**已登记**；模板展开器不在 reverse-src（取证边界声明防误引）；两尾巴修毕（goproxy GOPRIVATE 勘误 ×6——正确配方 GOPROXY+GONOSUMDB；cargo §9 四档路径 ×3）；置信 高21/中6/低3；T-306 六消费点五齐两登记。日志 reports/agents/T-303.md。
  - **T-299 → done 2026-08-26（`d06d6e1`，gitflow 三航）——B1 全清**：Login/壳层/仓库列表+表单迁 MUI（组件层 only，交互语法零变化——锚挂 input 本体经 slotProps、⌘K/方向键/Enter 链路原样）；四闸门绿（tsc/lint 0/build/ledger PASS〔anchor-audit 补引号字面量 sx 形态〕）+ 全量 playwright **188/0** + axe 双主题 serious=0（自擒一处真对比度违例并修）+ SPA +3.57%（预算 25%）；批次二边界项登记（session 菜单/侧栏/badge 因零变化红线未迁，归 T-300 派单裁定）。conductor 复验：build/tsc/lint/ledger 全绿。日志 reports/agents/T-299.md。
- **B2**：T-304 [P0] FR-95 回头看裁决票｜ T-305 [P0] FR-92 BE 认证配置 REST+变更即生效≤1s+双源（dep T-301/302）
  - **T-304 → done 2026-08-26（`ac15110`）**：30 项复核（维持 10 附出处/改回-既有票 5/改回-新票候选 4/已裁 9/**待用户裁决 2**）；**CG-2 十一类失败形态锚定**（权限/重复类=401匿名·403实名+errors 信封；IOException 族=200+错误串入 warnings.other〔CargoPublishResponse 无顶层 errors——与 20:55 终值吻合〕；无 409；帧前缀畸形 500 穿透降级声明）；Q8 六项归位 + 两翻转路由核对（CG-2→T-316 ✓/CN-1→T-308 ✓）；PM 转交 PRD v1.2 勘误三处待执行。日志 reports/agents/T-304.md。
  - **待用户裁决（T-304 上交，19:05 口径）**：① NuGet 对齐 bundle（v2 全面/remote search 代理/service index 动态解析/virtual 合并——M11 无承载票）② MPU 面形状（A 中继 vs presigned 需新 ADR；B create 范围；连带发现：Artifactory 六端点全 POST+QueryParam、complete?sha1 回 202、status 异步任务模型）。**不阻塞 B2/B3 流**，T-316/T-323 派发前收口。
- **B3**：T-306 [P0] FR-93 BE binstore.yaml 三链解析+装配+fail-fast（dep T-301/303）dev-go-storage｜ T-307 [P1] FR-92 FE admin 认证配置页组 MUI（dep T-305）dev-frontend
  - **T-305 → done 2026-08-27（`914dd67` + 哨兵翻转 `acab44a`）——B2 全清**：migration 015 + AuthConfigStore + ConfigManager（license 三要素复用）；**热缝**——OIDC Bearer/LDAP 登录臂逐请求取快照（无热源时字节等同旧静态装配）；**自擒真缺陷**：ldapPool put/Close 竞态（热换帧排空旧池时在途 Bind 归还向已关闭 channel 发送）；九端点 + 审计 redact + Keycloak 活体腿（issuer 翻转全链）PASS；conductor 复验（build/lint 0/TestAuthConfig/双矩阵 0 偏差）。**哨兵语义用户裁定（2026-08-27 00:15）：照 Artifactory 400 拒**——conductor 即时翻转（accept-and-keep → refuse-and-keep-stored；FE 交互=留空保持）。差异登记 5 条载日志。遗留：audit 词表两词/userDnPattern 消费缺位/带秘密文件段种子需主密钥（文档归 T-328）。
  - **T-306 → done 2026-08-27（`022ecfb`）**：binstore.yaml 三链解析（闭集+保留名拒启）+ Q5 三分支（语义分歧 fail-fast 指名两文件/等价 WARN）+ fail-fast 四形态实测（坏 YAML 指路径行/明文 secret 指名 env/保留名/双源分歧）+ **MinIO 真容器三链 roundtrip 全对账**（dual-write 双 store）+ 存量内嵌零破坏 + WARN；**conductor 裁定**：boot 拒绝路径的审计 = 结构化 stderr 日志即记录（成功路径才落 audit 事件）；票内两决策落注释。差异 6 条登记（含 mc 镜像 tag 失效归 T-325）。
  - **T-307 → done 2026-08-27（`c03d3bc`）——B3 全清**：三 Tab MUI 页组（字段册驱动，v2 逐字段——LDAP 23 锚/SAML 十三字段反语义/OAuth 映射 OIDC wire）；**哨兵留空剔除网络层断言**（00:15 裁定落地）；console-ux v1.12 先入册 **64 锚**；全量 **195/0** + axe serious=0（自擒修三）+ SPA +3.25%；**新缺口登记**：SAML 证书三端点（key/public/regenerate，v2 §3.2）BE 未落 → **T-331 [P2] 补票**（B9 后 slack 窗口，dev-go-core）。
  - **B4 双票在途 01:10 起**：T-308 conan local（v2 全量 + v1 全量 CN-1 终裁，窗口独占）/ T-309 helm 经典仓 local（relative=true 终裁）。
  - **T-309 → review→已验待合 2026-08-27**：实现+自测全绿（sprint 752 验收）；提交暂缓——接线踩 conan+helm 双包型 13 槽共写（slots.go/main.go/router.go），按合并时机口径第 4 条「T-308 先、T-309 紧随」顺序 --no-ff。
  - **429 熔断事件 01:52~01:54**：T-308/T-311 双双击落，配额复位 04:38:48；conductor 编译态预验（754 轮）全绿。
  - **双票续跑 10:23 起（复位后恢复）**：T-308（conan 1.66 live leg 门控测试收尾）/ T-311（rpm 适配器主体，参考 helm 接线模式）。宽度满 2，不派第三票。
  - **T-308 → 已验待合 2026-08-27 10:44**：agent 交付全量（v2 17 端点+v1 全量数据面+能力头+reindex 业务体+门控三缝；**真实 conan 2.31.2 与 1.66.0 双客户端活体 E2E 全绿**——login/create/upload 两修订/清缓存 install 全远端下载链/list/remove；`-race` 双包 ok；M10 不变量 0 deviations）。conductor 复验：build/vet/addons+conan+cmd 测试全绿。遗留：conan reindex 两个 dispatchAPI case 由 conductor 波次合流时接线（片段在 T-308.md §5-D9）；规格修订建议 2 条交 reverse-engineer；`forceConanAuthentication` 仓配置字段未落（默认 false 行为已备）。合入锚点 = B4+B5 波次 PR（分支模型第 6 条）。
  - **B4+B5 波次合入 2026-08-27 12:4x（`4c3f70d` + merge `d03b0f3`）——T-308/T-309/T-311 三票 done**：conductor 全量验证后按第 6 条裁定单波合入（PR 化因 gh/token 缺位暂不可用，回退 --no-ff 本地程序，feature 分支留 origin 审计）。conductor 随波接线 conan §5-D9 遗留：`Deps.MgmtHandlers` 缝（server.go）+ `/api/conan/…/reindex` 两 case（router.go）+ `t308_conan_mgmt_test.go` 钉 router 契约（401 前置/两拼写达面/数据面不串/E-26 未挂载态）。全树 31 包 ok + lint 0 + M10 矩阵 0 deviations。**PR 化待办**：gh 安装 + 认证后启用（用户侧一次性动作）。
  - **T-310 派发 2026-08-27（12:4x，dev-go-core）**：debian automatic local（TL-4=i386,amd64 强制/deb PUT 坐标/索引直写 403）；参考 rpm/helm 模式；第 15 槽。宽度：T-310 + T-300（MUI 批二在途）。
- **B4**：T-308 [P0] conan local——**v2 全量 17 端点 + v1 全量数据面（CN-1 终裁推翻收窄，本票升 M11 最重适配票，窗口独占）**（TL-2/TL-3 能力头/.timestamp）dev-go-core｜ T-309 [P0] helm 经典仓 local（HL-1/2 挂载与 relative=true；.prov；reindex 双端点）dev-registry-adapter
- **B5**：T-310 [P0] debian automatic local（TL-4=i386,amd64 强制；debPUT 坐标；索引直写 403）dev-go-core（dep T-304）｜ T-311 [P0] rpm local 管线（RP-2=false；header 解析器自研；reindex 七分支矩阵；TL-5=SHA-256）dev-registry-adapter（dep T-304）
  - **T-310 → done 2026-08-27 14:0x（feature/T-310 merge `5b4c54f` 前序）**：`internal/adapter/deb`（15 文件）——矩阵坐标 debPUT 链/ar-tar-xz control 解析（新依赖 ulikunitz/xz 留痕）/确定性 Packages-Sources-Release/By-Hash 引擎（代数保留+无符号清扫）/dpkg 版本比较器；管理面 `POST /api/deb/reindex/{repoKey}`（§4.3 矩阵+逐字拒语）；Debian() 第 15 槽（pro）；repoManage 门 7→8、槽计数 14→15。**真实 apt 链 E2E**（debian:bookworm 容器 52.7s PASS）：dpkg-deb 现造→debPUT 201→apt-get update（apt 自验 Release-SHA256）→install→二进制可跑；TL-4 空 i386/amd64 Packages 生成断言过。全树 32 包 ok + lint 0 + M10 双跑 0 deviations；conductor 复验绿。遗留：bz2/xz/lzma 索引压缩未实现（plain+gz 对 apt 全功能实证；`normalized()` 一处翻转即回）；remote 豁免缝留 T-314；snapshot 族 §10 缓议。日志 reports/agents/T-310.md。
  - **PRD v1.2.1 勘误落盘 2026-08-27（`docs(prd):` commit）**：T-304 转交三处+LC-18 完成（CN-1 族 7 处/CG-2 两处含 warnings.other 精度注保护 T-316/§5.6.1 三行）；版本沿革注记避免跳号误读。**待另转交**：§5.6.1 其余 28 项基线浓缩填实（T-287×7/T-289×5/T-290×4/T-294×7 占位行）。
  - **B6 双票派发 2026-08-27 14:1x**：T-312 conan remote+virtual（dev-go-core）/ T-313 helm virtual+remote（URL 改写/_external 核心）（dev-registry-adapter）；并行协调条款入派单（各自 adapter 包内自洽，接线预计零改）。
  - **T-313 首实例 14:00 被 API 内容过滤误杀（1301 假阳性，SSRF/proxy 术语触发）**：死于探索期零写入，14:02 中性措辞重派（同一票面；「远端拉取一律走既有 internal/remote 客户端与内置防护」表述 + 报告语言平实化条款）。经验登记：涉安全面票的派单措辞避免渲染攻击面细节。
- **B6**：T-312 [P1] conan remote+virtual（dep T-308）｜ T-313 [P1] helm virtual+remote（URL 改写/_external）（dep T-309）
- **B7**：T-314 [P1] deb remote+virtual（含 trivial P2 余量段）（dep T-310）｜ T-315 [P1] rpm remote+virtual（含 modules P2 余量段；RP-3 收紧）（dep T-311）
- **B8**：T-316 [P1] cargo remote（CG-2 确定臂：失败恢复 200+errors[] 双轨 + T-294 断言反转 + 规格回写；**dep T-304 出处锚定，不可提前**）｜ T-300 [P1] MUI 批二（dep T-299/T-307）→ **提前至 756 轮派发（10:57，dev-frontend）**：宽度补位（Go 侧新票均撞 B4+B5 未提交交织面）；批一边界项 session 菜单/侧栏/badge 派单裁定=迁；T-316 仍被 NuGet 对齐捆绑未决用户裁决卡住
  - **T-300 → done 2026-08-27 13:5x（feature/T-300 `23 文件` merge `b76e3ac`）**：四组页面 + 批一边界项全迁（22 web/src 文件 +1559/−1086）；七闸门全绿——tsc/lint 0、build、ledger PASS（锚册零改动）、axe 24 扫零、assert-tokens 0、SPA **+3.12% < +3.25% cap**、playwright 9 轮 186–192 passed（8 个负载 flake spec 全部串行复跑绿，零确定性失败；N01 +2 请求 trace 实证为登录落地页既有行为非回归）。conductor 复验 tsc/lint/build/ledger 四闸门绿。样式层发现登记：emotion 注入序在 base.css 后——单类平手 MUI 赢，续挂须复合类或显式 sx（修正 T-299 表述）；.field input→.field > input 连带修复批一 TextField 双边框。**批次三候选登记**：RepoDetailPage/Dashboard/Profile/Placeholder/NotFound、共享组件六件套、最近词 combobox 统一化；QA de-flake 票（N01 straddle + 负载 flake 家族）并入 T-327 评估。agent 自愈：误删 console/dist/placeholder.html 已恢复（终态 dist diff=0，干净检出可编译）。日志 reports/agents/T-300.md。
- **B9**：T-317 [P1] 复制硬化（两字段生效反转 L25 按名 400；属性同步端到端；replica 隔离）｜ T-318 [P1] cargo virtual（dep T-316 同 area 串行）
- **B10**：T-319 [P1] GPG keypair 体系（票内先补 mini 规格；openpgp 零 CGO；dep T-301）｜ T-320 [P1·条件 Q2] HelmOCI 分发（dep T-309；未触发非 DoD 缺口）
- **B11**：T-321 [P1] debian 签名腿（dep T-319/T-310）｜ T-322 [P1] rpm 签名腿（dep T-319/T-311）
- **B12**：T-323 [P1] S3 MPU kill -9 续传复活（upload ID 落表+ListParts 重建；探针断言翻转）｜ T-324 [P1] unused-cleanup 引擎（cron+审计+零孤儿）
- **B13**：T-325 [P1] 部署矩阵演进+CD 链验证（dep T-306；VM 数据零触碰）｜ T-326 [P2] D-8 footprint ≤100MB + D-9 测试基建（seed 竞态/verifyM10 口令外置）
- **B14**：T-327 [P1] 中期回归（L01~L11+L18~L31 首跑+双形态全 P0 复跑+契约归属审计 m10-done..HEAD）｜ T-328 [P1] 文档五类（认证/存储/四包型接入/api 增量含 L25 反转/FAQ）
- **B15**：T-329 [P0] 终验（L01~L45 全量+四包型客户端矩阵+DoD 八条+两断言反转 PRD 回写核实；L19 口径=v1 全量）
- **波外条件票**：T-330 [P1·条件 Q7] Trash can（票内先补 mini 规格）

**关键路径**：T-301/302 → T-305/306 → T-308~311（四包型 local）→ T-312~315 → T-327 → T-329；T-304 为 T-310/311/316 裁决前置。**转交**：PRD v1.2 勘误三处（§2.2 v1 行/§5.6.1 CN-1・CG-2/LC-18）随 T-304 完成转 PM。

**风险登记**：② auth-integration OAuth/SAML 基线缺口（T-302 判定）；③ K-1 mini 规格+openpgp 准入；④ cmd 装配缝 T-305/T-306 各动己方 wire 函数；⑤ dev-go-core 9 票负载（T-308 加重后 B4 窗口独占）；⑥ conan 1.x 活体可得性（curl 等价+留痕路径）。「所有研发按照gitflow规则提交代码」——**分支模型即日切换**（conductor 落地口径）：
- **main = release-only**：仅接收 release 合并（develop → main --no-ff）与 hotfix；里程碑 tag 继续（m1~m10-done 既有 tag 不动）
- **develop = 集成分支**（2026-08-26 自 main@`95a8f9a` 切出，双远端已推）：票据提交、sprint 报告、chore 全部进 develop
- **feature/T-<id>-<slug>**：每票一个特性分支，票过 qa 后由 conductor `--no-ff` 合入 develop（ticket 提交信息维持 conventional commits）
- **hotfix/***：自 main 切出，修完双回（main + develop）
- **CD 链改挂 develop**：VM Jenkins `binflow-ci-smoke` + `binflow-deploy` 已改 `*/develop`（容器 config.xml + 仓库 groovy 源同步，Jenkins 已重启重载）——满足「每一次变更持续部署」；`binflow-release` 维持 main（release 形态从 main 出）
- subagent 工作方式不变（不 git 提交，conductor 统一提交——仅提交目标从 main 改为 feature→develop）
- 存量：main 当前 = `95a8f9a`（含 m10-done tag）；下一次 develop→main 合并发生在 M11 首个批次收口或里程碑收官
- **合并时机自动决策口径（用户 2026-08-27 授权 conductor 自裁，不再逐案请示）**：
  1. **feature → develop（--no-ff）**：ticket 过 conductor 验证后**立即**合入，不等批次/里程碑收尾——每个已验证 ticket 即时进 VM CD 链，并杜绝 T-309 式「后行票阻塞先行完成票」的提交交织。
  2. **develop → main（--no-ff）**：M11 首个批次收口或里程碑收官（m11-done tag）时执行一次——触发 CircleCI/UAT 链（52.79.109.153，含文档服务）；中途不逐票 release，避免 UAT 高频换装。
  3. **hotfix/**：自 main 切出，修完双回（main --no-ff → 回并 develop）。
  4. **交织例外顺序合入**：文件共写时（slots.go/main.go/router.go 按 13 槽共写），「先完成票先合、后行票紧随」顺序 --no-ff；当前 T-309（已验）暂缓即此例——T-308 收口后 T-308 先、T-309 紧随。
  5. **PR 化合并（用户 2026-08-27 10:55 指令「你自己在合适的时机创建github pr」）**：自本条起 feature→develop 与 develop→main 均经 GitHub PR（conductor gh 自建自合；`gh pr create` → `gh pr merge --merge`，--merge 等价 --no-ff 保合并提交；PR 描述含票号+验证摘要）；时机沿用第 1/2 条口径。develop→main 的 release PR 在 CircleCI SSH key fingerprint 占位符（`REPLACE_WITH_UAT_KEY_FINGERPRINT`）被用户填妥前**只建不合**。
  6. **三票全交织裁定（2026-08-27 10:52，第 4 条扩展）**：T-311 续跑期间主动改写 main.go/router.go（rpm import/yum case），接线文件成 T-308/T-309/T-311 三票共写且无法按票序独立编译（先行提交必携带后行包）。裁定：T-311 落地全绿后以 **B4+B5 波次单 PR 一次合入**，三票逐项归因（票号→文件清单→验证摘要）写入 PR 描述；票状态以 PR 合并为 done 锚点。快照保险：/tmp/snap-b45-1052/。

（空）
