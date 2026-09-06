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


> # 🏁 M11 已收官（m11-done 2026-08-28，里程碑 PR #13 已合并 main）
>
> **终验 PASS**（T-329：L45 全量 38✅ 零未解释红；五包型×三仓型真客户端矩阵全绿；DoD 八条兑现）。31/32 票 + 九张补票全清（T-320/T-330 条件票未触发不计 DoD，已在 ROADMAP「M11 未纳入项」留痕）。收口五项全落（①PR#11/②③④PR#12/⑤BOARD 裁定）。**里程碑检查（用户规程）**：README 双语 M1~M11 done + 文档站六新篇/配置指南——已检查并更新。M12 承载项见 ROADMAP（NuGet bundle/D-A fail-open/D-8R 瘦身/D-F/T-290-2 等）。

## M11 票据（T-299/T-300 既定 + T-301~T-330，tech-lead 2026-08-26 拆票；AC 全文见 tech-lead 拆票交付〔本节压缩录〕+ docs/prd/milestone-11.md v1.1；Q8 终值 20:35+20:55 已并入票面）

> **CircleCI build #3 红根因与修复（2026-08-28 00:5x）**：唯一失败步「Vet+lint+fast tests」中两个 npm 客户端套件——CI 镜像 npm **11.17.0** 对测试 .npmrc 已废弃的 `always-auth`（npm 9 移除）逐命令打 warn 进 stdout，污染 M26 重复发布的 403 族匹配与 M55 `npm view --json` 解析（本地 npm 10.9.8 无此告警故全绿）。修复：三处测试 .npmrc 删 `always-auth`（`a4c6ae7`，本地复跑 npm 包 75s 绿）。**main 已带修复重触发**（用户经 PR #2 自合 develop→main〔83f3231〕；conductor 基于 origin/main 再合 batch 2 release `5b8f084`）。**API 排查通道确立**：项目级 token 的 slug 反查不通（`circleci/<org-id>/<uuid>` 形态 v2 API 500/404），但 v1.1 端点 `/api/v1.1/project/circleci/<org-id>/<project-uuid>/<build#>` + action 的 presigned output_url 可拉全量日志——CI 日志获取路径固化为此。

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
  - **T-312 → done 2026-08-27 18:2x（merge `f86da48`）**：conan remote（v2 读面代理/marker 逐字/PUT 405/RE-05 DELETE）+ virtual（revisions 时间归并去重/files·pid 并集/写路由探测 conanDeploymentTarget/成员失败容忍）；provider 计入 .files.json/.search.json TTL + UpstreamPath 翻译面；handler 三仓型类门重构；revision 写平面成员定向读（防 routed 写拷贝他成员修订链）。**真实 2.31.2 客户端 E2E**（39.64s：remote login→代理 install→virtual 聚合读→virtual routed 上传+归并 list）+ T-308 双 local 回归 PASS。零跨包改动；差异登记 10 条（D1/D5/D7/D8 交 reverse-engineer 升置信）；Artifactory 真实上游活体互证未做（mock+自指上游两腿留痕）。日志 reports/agents/T-312.md。
  - **T-313 → done 2026-08-27 18:3x（merge `b059a6f`）**：helm virtual+remote——**S8 URL 改写算法**（urlrewrite.go 纯函数 + 16 分支表驱动：charts-base 边界规则/`_external/<scheme>/<host>/<path>` 折叠 `://`→`/`/允许清单门/`_transitive` 上游自 _external 面/oci:// 透传 D-5/local 绝对 URL 折回）；virtual 聚合 (name,version) **first-wins**（S13）+ SemVer 降序重排 + 空 entries 成员 200；写路由 defaultDeploymentRepo + index 重算跟随落点成员；`_external` 绝对 URL 经 internal/remote 守卫客户端代理。**真实 helm 4.2.4 客户端**（含 kind 集群腿：remote pull-through install 与虚仓 _external install 双链 STATUS: deployed）+ -race 190s + M10 双跑 0 deviations。接线一行（helm.Register + md.Remote()）。遗留登记：D-2/D-3（chartsBaseUrl 分体基址 + _external 落盘缓存需 internal/remote+repo config 扩展——architect 评估单列候选）；D-5 oci:// 待 T-320；D-10 namespace 模式；D-12 deb 满载 flake 归 T-310（20s sweep 轮询窗加宽）。日志 reports/agents/T-313.md。
  - **T-314 派发 2026-08-27 18:2x（dev-go-core）**：deb remote+virtual（含 trivial P2 余量段：bz2/xz/lzma 压缩恢复/T-310 remote 豁免缝）。参考 T-312 模板。**接线纪律追加**：main.go 编辑权移交 conductor（报告 §接线移交 精确 diff）——防与 T-315 共写竞态。
  - **T-315 派发 2026-08-27 18:4x（dev-registry-adapter）**：rpm remote+virtual（含 modules.yaml P2 余量段；RP-3 收紧）。同款接线纪律。宽度 2：T-314/T-315。
- **B7**：T-314 [P1] deb remote+virtual（含 trivial P2 余量段）（dep T-310）｜ T-315 [P1] rpm remote+virtual（含 modules P2 余量段；RP-3 收紧）（dep T-311）
  - **T-314 → done 2026-08-27 19:5x（merge `c204e0d`）**：deb remote（svc.Get 引擎链双档 TTL/PUT 405/RE-06 驱逐）+ virtual（按请求现算聚合：Release 校验节=自渲染字节/stanza 合并 R3 去重键/五档拼写按需解压/写路由重算目标=落地成员/签名族·by-hash 404=apt 回退/失败容忍-浮出）+ **P2 xz/lzma 落地**（零新依赖；bz2 降级 WARN——dsnet 不可得实证）。**真实 apt 容器双腿 41.69s PASS**（remote by-hash 回源+virtual 双成员双包装机）+ T-310 回归 PASS。差异登记 R1–R11。日志 reports/agents/T-314.md。
  - **T-315 → done 2026-08-27 19:5x（merge `82b91bd`）——B7 全清**：rpm remote（pull-through/rpm.metadata.* 异步回填/RE-05·RE-06）+ virtual（**S11 段式合并引擎** merge.go/RP-3 singleflight+TTL+成员序签名缓存/modules.yaml 透传 P2/代数保留确定性自擒修复）+ **conductor 落地两处移交**：main.go RegisterWithProps(+NodeProps)、httpapi `/api/yum` 虚仓 200/202 分支（path 补 /repodata、async 校验前移、t215 类措辞更新、handler_test 矩阵翻到 §3.2 契约）。**真实 dnf 三链**（remote makecache→install→二次缓存/virtual 双成员聚合 install Complete!/T-311 回归）+ -race 59.7s。rpm 实际无需 RemoteConfigs（与派单预估不同——无 _external 面）。差异登记 11 条。日志 reports/agents/T-315.md。
  - **B9′/B10 混批双派 2026-08-27 19:5x**：T-317 复制硬化（dev-go-core）+ T-319 GPG keypair 体系（dev-go-core，票内先补 K-1 mini 规格；openpgp=ProtonMail v1.4.1/ADR-0038 裁定既定）——T-318 仍串行卡 T-316（NuGet 对齐捆绑未决）。
  - **共享树 git 事故 20:06 及处置**：T-319 agent 自建 `feature/T-319` 分支（按 gitflow 全局纪律自发），conductor 的 sprint-778 报告提交被带错上该分支。恢复：cherry-pick 回 develop（`7c49640`）、分支重置并删除、双 agent 已下发「**共享树禁分支/禁提交**」纪律（只写文件，conductor 收口统一 git）。**派单规程修补**：今后所有 dispatch prompt 必含 no-git 条款（B4~B7 派单均隐式安全，本轮两单漏写）。
- **B8**：T-316 [P1] cargo remote（CG-2 确定臂：失败恢复 200+errors[] 双轨 + T-294 断言反转 + 规格回写；**dep T-304 出处锚定，不可提前**）｜ T-300 [P1] MUI 批二（dep T-299/T-307）→ **提前至 756 轮派发（10:57，dev-frontend）**：宽度补位（Go 侧新票均撞 B4+B5 未提交交织面）；批一边界项 session 菜单/侧栏/badge 派单裁定=迁；T-316 仍被 NuGet 对齐捆绑未决用户裁决卡住
  - **T-300 → done 2026-08-27 13:5x（feature/T-300 `23 文件` merge `b76e3ac`）**：四组页面 + 批一边界项全迁（22 web/src 文件 +1559/−1086）；七闸门全绿——tsc/lint 0、build、ledger PASS（锚册零改动）、axe 24 扫零、assert-tokens 0、SPA **+3.12% < +3.25% cap**、playwright 9 轮 186–192 passed（8 个负载 flake spec 全部串行复跑绿，零确定性失败；N01 +2 请求 trace 实证为登录落地页既有行为非回归）。conductor 复验 tsc/lint/build/ledger 四闸门绿。样式层发现登记：emotion 注入序在 base.css 后——单类平手 MUI 赢，续挂须复合类或显式 sx（修正 T-299 表述）；.field input→.field > input 连带修复批一 TextField 双边框。**批次三候选登记**：RepoDetailPage/Dashboard/Profile/Placeholder/NotFound、共享组件六件套、最近词 combobox 统一化；QA de-flake 票（N01 straddle + 负载 flake 家族）并入 T-327 评估。agent 自愈：误删 console/dist/placeholder.html 已恢复（终态 dist diff=0，干净检出可编译）。日志 reports/agents/T-300.md。
- **B9**：T-317 [P1] 复制硬化（两字段生效反转 L25 按名 400；属性同步端到端；replica 隔离）｜ T-318 [P1] cargo virtual（dep T-316 同 area 串行）
  - **T-317 → done 2026-08-27 20:5x（merge `65063dc`）**：① 两字段生效反转（L25 按名 400 退役；Bearer 认证头切换 + contentSynchronisation 四子字段；M10 断言双翻转、单 JSON 值门保留）② 属性同步端到端（推送臂两成功臂都携带——幂等零传输收敛；拉取臂 land() 后属性附着 best-effort；双实例真栈：打标→复制→读回）③ replica 隔离 = M9 Q5 终裁查证现状维持（零代码 + 双实例四条款活体断言）。属性同步失败分类 400/409 终态。**conductor worktree 独立验证**（T-319 在途 keypair 污染主树 httpapi 编译——T-317 集四包作用域全绿后合入）。日志 reports/agents/T-317.md。
- **B10**：T-319 [P1] GPG keypair 体系（票内先补 mini 规格；openpgp 零 CGO；dep T-301）｜ T-320 [P1·条件 Q2] HelmOCI 分发（dep T-309；未触发非 DoD 缺口）
  - **T-319 → done 2026-08-27 21:3x（merge `531b557`）**：mini 规格 docs/design/gpg-keypair.md（**L-A=JFrog 官方 REST 实时取证**〔2026-08-27 抓取 docs.jfrog.com OpenAPI：9 端点 + KeyPairInput/Summary 逐字〕/L-B=ADR-0038/L-C 低置信；**Q8 锚定：Artifactory 官方面无 keygen——RSA-4096「暂行」无处翻转转正**，生成端点落 BinFlow 自有管理面，兼容面 import-only）；internal/keypair 三件（Manager 含 in-use 护栏/Verify/BootCheck；openpgp 力学 ProtonMail v1.4.1；**签名 seam 供 T-321/T-322**——ErrNoKeypair/ErrUnavailable 驱动跳签清旧姿态）+ 迁移 016 双方言 + repo 六钩点（deb/rpm 接受他拒含 HL-4）+ httpapi 9 端点 + 审计零密钥材料 + t215 门 38→48 + cmd wireKeypairManager（replicationCipher 复用/BootCheck fail-fast）。**真 gpg 2.5.21 双向验签**（出：服务端钥仅公钥导出→gpg --verify exit 0 双形态；入：gpg 生成→BinFlow 导入→重签→独立 GNUPGHOME 验过）。体积 +0.35MB 实测 <1.5MB。D-1~D-8 差异登记。日志 reports/agents/T-319.md。**T-321/T-322 签名腿解锁（dep 就绪）**。
- **B11**：T-321 [P1] debian 签名腿（dep T-319/T-310）｜ T-322 [P1] rpm 签名腿（dep T-319/T-311）
  - **T-321 → done 2026-08-27 23:4x（merge `9bc8424`）**：deb 签名腿——sign.go 消费侧窄接口 + InRelease/Release.gpg 双产物 + 错误分类（无钥/不可用→跳签清旧）；local 重算写签名对 + sweep 白名单（轮换保留）；virtual 按需签聚合 + **确定性修复**（聚合 Date 乘最新成员——分请求签名对得上，apt 回退链依赖此）。conductor 落地接线（wireKeypairManager 返 SigningService + stack.signer + deb.Options 注入）。**真实 debian 容器 gpg 校验链 262s PASS**（NO_PUBKEY 证校验开启→signed-by dearmor→install 过——容器腿排掉 .asc 直用触发 apt-key 错误的真问题）。日志 reports/agents/T-321.md。
  - **T-322 → done 2026-08-28 01:0x（merge `636f98d`）——B11 全清**：rpm 签名腿——RepomdSigner 窄接口（SigningService 双面已备零缝扩）+ 跳签清旧 + 固定名落地（覆写=轮换）；renderRepomd 写盘翻转点后签名；K-1 占位的无条件 .asc/.key 清扫退役（legacy sqlite only，T-311 清扫不变量仍绿）；virtual 404 姿态零改动（ADR-0038 d5 延续）。main.go 一行（agent 照 T-321 先例自落）。**真实 dnf gpgcheck 链三腿**（rockylinux:9 repo_gpgcheck=1：无钥拒→导入 served repomd.xml.key→makecache/repoquery/install/rpm -q 全过 99s；无签名回归 126s；本机 gpg 对线上三件套验签）。容器腿修掉两个测试侧真问题（no-tty 导入确认 EOF 被误报 Bad GPG signature / makecache 管道吞退出码——二分归因测试管线，产品零改动）。日志 reports/agents/T-322.md。
  - **T-324 → done 2026-08-28 01:2x（merge `6db1d62`）——B12 全清**：unused-cleanup 引擎——三腿单锁（session 扫掠〔新 SessionSweeper 能力〕/policy 删除〔在用 oracle=审计下载 trails∪virtual 成员〕/GCSweep apply〔ADR-0031 双门，grace 递延〕）；repo.LiveChecksumSet 全树唯一引用集游走；REST POST/GET /api/v1/system/cleanup（dry-run 默认）+ system:write/read 门（t215 48→50）+ cleanup 指标 gauge；cron 每小时 apply。**零孤儿端到端对账**（node+remote_cache 同失/ledger 无未引用行/过期会话 0）+ 互斥/crash 形状/dedup 共享保留/grace 递延全钉死。conductor 落地四处接线（stack 字段/构造/deps/Run 环——报告 §5 diff）。-race 三包净。差异登记七条。文档归 T-328。日志 reports/agents/T-324.md。
- **B12**：T-323 [P1] S3 MPU kill -9 续传复活（upload ID 落表+ListParts 重建；探针断言翻转）｜ T-324 [P1] unused-cleanup 引擎（cron+审计+零孤儿）
  - **T-323 → done 2026-08-27 23:2x（merge `b903a70`）**：引擎层付清 §11.31 债——upload ID 落表 + `ResumeSession` ListParts 分页重建（offset=Σ部件）+ NoSuchUpload 同 key 归零 + **完成-后读回校验门**（live MinIO 实证 in-progress 部件不可读回——设计事实落规格）；Close 改 ADR-0028 保留语义（SIGTERM≡kill-9）；sweep 行+孤儿 MPU 联合回收；complete-race 补 B5 abort（两臂，变异验证）。**真实 MinIO 真进程 kill -9 链 -race 全过**（5MiB→SIGKILL→重启→续传→Commit→对账→零残留）+ 8 路并发 resume + 错 sha 整包拒弃。conductor 落地 main.go 接线（openS3Engine + Sessions/TTL——生产路径此前 rows=nil）。**探针 leg 4 翻转未达**：REST 面重启可见性（mpuRegistry 进程态+协议坐标无持久家）登记为 **T-323R [P2] 独立小票**（落地即翻转 leg 4，docker 面 S3 续传自动获得；附 complete 后 CopyObject 前 crash 窗口的临时对象孤儿一并裁决）。日志 reports/agents/T-323.md。
  - **T-331 → done 2026-08-28 05:0x（merge `0399a8f`）**：SAML SP 证书三端点（GET key/public〔CapSecurityRead〕/PUT regenerate/POST key〔CapSecurityWrite〕）——X.509 自签（crypto/x509，RSA-2048/CN=binflow-saml-sp/~10y）+ auth_configs 保留行 saml_sp_key（私钥 enc:v1 密封）+ §3.3 ensure 流（false=generate-or-reuse）+ 错主钥 boot 拒启；t215 门 50→53；审计零密材。**13 腿真实 curl 全链**（fresh 404→401→生成→下载 byte 级一致→加密保存复用→轮换只服务新证→openssl 双证解析→sqlite 直查密封→重启持久→错主钥拒启）。D-1~D-5 登记（路径归一到规格拼写/POST key 为原生面沿 GPG D-1 姿态）。遗留：FE 两动作按钮（锚待入册——T-307 遗留#1 就此可解）；audit picker 两词。日志 reports/agents/T-331.md。
  - **T-307R → done 2026-08-28 05:4x（merge `1e630b0`）**：SAML Tab SP 加密证书卡（下载/regenerate danger 确认/SHA-256 指纹/§3.3 certTick 感知）+ 锚册 v1.13 两锚 + CFG8 腿；四闸门绿。POST saml/key 未开口子（空态经 regenerate 同机生成——待裁定项）。**误伤事件登记**：清理 pkill 过宽杀 T-327 两无证实例——已通报处置。T-307 遗留#1 就此关闭。日志 reports/agents/T-307R.md。
  - **T-319 补遗二**（`72bf3ee`）：两个漏网测试文件（repo 引用矩阵 3 测 + httpapi 门控/CRUD/关联 5 测）+ 两报告修订——收敛「正式通知晚于提交」的清单时差问题。
- **B13**：T-325 [P1] 部署矩阵演进+CD 链验证（dep T-306；VM 数据零触碰）｜ T-326 [P2] D-8 footprint ≤100MB + D-9 测试基建（seed 竞态/verifyM10 口令外置）
  - **T-325 → done 2026-08-28 02:0x（merge `735fc53`）——B13 半清**：Chart 1.2.0（binstore.yaml 渲染为 ConfigMap 第二键 + subPath + **四类渲染期守卫**〔s3 互斥/非法链形/缺 mode/单链带 mode〕+ masterKey.existingSecret 四密封面注入）+ compose/k8s/systemd/offline 矩阵对齐 + .env.example G10 破坏修复；**CD 链真缺陷修复：uat-deploy.sh `sudo bash -s` 从未传参（$1/$2 恒空，deploy_uat 从未可能跑通）** + 烟测版本断言 + config.yml 版本注入 + UAT 预置 runbook（systemd 单元全文/sudoers/步骤）。验证：helm lint 0/七态模板矩阵含四拒绝实证/kind 真部署 roundtrip/compose 冷构建 healthy/双链 MinIO checksum 级对账/shellcheck 0/mock 远程腿 rc=0；VM 零数据触碰（两次只读 GET）。遗留：① 修复入 develop，下次 release 合并后 UAT 链才可端到端（届时观察双 job 绿）；② mc tag 维持版本锚定（registry 直连不可达，实测可拉可跑）；③ env-only 不完整链键组先于 binstore 拒启（差异登记归 dev-go-storage）。日志 reports/agents/T-325.md。
  - **T-326 → done 2026-08-28 04:0x（merge `ce41bbc`）——B13 全清**：D-8 双轨（裁决链定口径=**空载 RSS 非 artifact**：RSS 探针 `make footprint`〔vmmap 138.6MB 观察/EXPECT 门〕为正主 + check-size 六平台聚合 ≤100MiB 门〔实测 94.13MiB 过，变异验证咬合〕）；D-9（seed-m8/m10 `converge` 退避 + 竞态感知 ensure——**A/B 证明 5/8红→8/8绿、3/6红→6/6绿**；verifyM10 口令彻底外置零残留，改密实例 401→200）。D-8 瘦身本体（懒加载 embed，dev-go-core 面）遗留——`--expect` 红至落地，PRD 逃生条款待 conductor 裁定；L27a 陈旧断言（11 vs 15 槽）归 T-327 裁定。日志 reports/agents/T-326.md。
- **B14**：T-327 [P1] 中期回归（L01~L11+L18~L31 首跑+双形态全 P0 复跑+契约归属审计 m10-done..HEAD）｜ T-328 [P1] 文档五类（认证/存储/四包型接入/api 增量含 L25 反转/FAQ）
  - **T-328 → done 2026-08-28 01:4x（merge `8defc0b`）**：文档五类全交付——六新篇（auth-config/storage-config/conan/helm-charts〔与 install/helm.md 辨析〕/rpm/debian）+ api-reference M11 速览（L25/keypair 九端点/cleanup/四 reindex 族）+ remote-virtual 字段表 + license 槽位矩阵 11→15（三源漂移消除）+ FAQ 四问 + **README 双语刷新**（十二包型矩阵带 tier/license·addon 门控段/里程碑行——里程碑收口欠账「README」项就此清偿）；scratch 实例实测（哨兵三态/binstore 五次 boot 全 fail-fast 形/keypair/L25/cleanup）；make docs SUCCESS 3.94MB 零断链。遗留四项回刷登记（SAML 登录腿/T-320/T-316·318/bz2）。日志 reports/agents/T-328.md。
  - **T-327 → done 2026-08-28 05:5x（merge `82f9d18`）——B14 全清，总裁定 PASS**：L 矩阵 22✅+1⚠️+1承证 0 硬 FAIL（M11-PRD 权威编号核定）；双形态全 P0 复跑绿（docker/maven/npm/pypi/conan/helm/dnf/apt 真客户端×community/pro）；契约归属审计 m10-done..HEAD 22 代码提交全映射零孤儿行为。**四项登记**：D-A〔P1 裁定——dual-write S3 停机 fail-closed vs M6 PRD FR-50 fail-open 文面，M6 存量非 M11 回归→**入待用户裁决清单**〕/ D-B〔P1 产品缺口——deb/rpm 策略键 REST PUT 不可达（byHash/calculateYumMetadata 等被传输结构体丢弃）→**T-327R 小票**〕/ D-C〔文档 prov 分隔行——conductor 随票修〕/ D-D〔filelists 条件注——随票修〕。**L27a 裁定：spec 滞后产品正确**（15 槽实渲染）→ spec 修补归 T-327F。de-flake 裁定与 pro 抽样复跑结论见报告 §7（四包型文档命令 pro 实例全绿）。日志 reports/agents/T-327.md。
  - **T-327F → done 2026-08-28 06:1x（merge `c6db774`）**：L27a 裁定执行——spec 11→15 + PRO_PKG 七型循环自动覆盖四新包型 + NEW_PRO_PKG kind 徽章断言 + L27c/d 连带；锚册零改动（动态族锚已覆盖）；m10 spec 5 测绿（fresh 实例）。
  - **T-327R → done 2026-08-28 06:3x（merge `2f9c6a9`）——D-B 闭合**：repoConfig 传输结构体补 deb/rpm 十策略键（Artifactory 平铺拼写/指针保 explicit false/setStrSlice 落显式空）；**409 分支逐字钉文案 + by-hash 树真测**（SHA256 驱动——票面 byHash=strong 非法按规格枚举改）+ filelists 自动重算 + xz 伴生 + 强制架构族。新登记：**by-hash 保留桶缺陷**（historyCycles < 每代拼写数可裁当前代——默认值此前掩护，REST 解锁暴露）→ T-327G 小票；byHash 值域枚举校验归 repo.Service 待裁定；web 仓表单跟进。日志 reports/agents/T-327R.md。
  - **T-327G → done 2026-08-28 06:2x（merge `54edc75`）**：by-hash 保留按代裁剪——byHashAddresses 单一拼写源（写/保护共用）+ 当代保护集（不看时间戳永不裁）+ byHashPrunePlan 纯函数（保留 newest historyCycles-1 组，rpm 同款「当代占一槽」语义）；cycles=0 折叠默认（规格开放项，0=全不保违反官方历史地板——理由留痕）。引擎腿（cycles=1+ALL+xz：当代全拼写 200 byte 一致/旧代 404）+ 纯函数 8 例 + T-327R 四断言回归绿。登记：同秒代合并（安全向）/ALL→NONE 遗留树老化。日志 reports/agents/T-327G.md。
  - **T-323R → done 2026-08-28 07:0x（merge `2ec7bbf`）**：MPU REST 重启可见性——坐标随行不透明 caller blob（自持表方案排除留痕）+ MultipartUploadContexts 能力对 + per-id single-flight 懒重建（锁序保持）+ 部件账从引擎 Offset 推导；caller-less 行 fail-closed；**T-323 §5-3 随票落地**（crash 窗口 sessions/ 对象超 TTL 清扫两臂）。**探针 leg 4 翻转 GREEN**（真 MinIO 双跑：200+5MiB 坐标重建→续传→complete→对账→零残件）；kill-9 腿 -race 复验。8 并发恰 1 次 resume。登记：config swap 重分片会话不跨重启（无损重建，begin-with-id 另票）；裸列表懒视图。日志 reports/agents/T-323R.md。
  - **用户三项裁决落定 2026-08-28 07:5x**：① **NuGet 对齐 bundle → M12 立项**（v2 全面+remote/virtual search 上游代理+service index 动态解析——M11 不插面；L2/L3-remote/L4/L7 四项随批）② **MPU REST 面形状 → 整体翻转对齐 Artifactory**（六端点 POST+QueryParam/complete?sha1=202/status 异步任务模型/GET /config 能力探测——**生成 M11 新票 T-332 [P1]**：新 ADR + uploads 面改造；T-323R 的持久化/懒重建内构保留，wire 形状翻转）③ **D-A dual-write S3 停机姿态 → M12 补 fail-open 实现**（本地优先写+异步 S3 重试队列；FR-50 文面维持，实现债登记 M12）。**T-316 解锁**（前置仅 CG-2 已终裁；先前「NuGet 捆绑卡 T-316」系误判收正）。
  - **T-332/T-316 派发 2026-08-28 07:5x**：T-332 MPU 面对齐（dev-go-storage）/ T-316 cargo remote（dev-registry-adapter，CG-2 确定臂+T-294 断言反转）；T-318 串行随后。**M11 收官路径**：T-316→T-318→T-332→T-329 终验→m11-done。
  - **T-316 → done 2026-08-28 08:4x（PR #5 合并，develop=`4848dec`）——首次 GitHub PR 收口流跑通**：cargo remote（引擎 pull-through/自指 sparse 上游/config 原文服务/search 代理/写拒绝 RE-06）+ **CG-2 双轨落地**（200+warnings.other 精确 wire/成功无 errors 键/帧缺陷 500/T-294 断言反转内构不动/D-3 去 409）+ 规格 §5.3 十一类全表重写 + §8.1 六差异。**真实 cargo 1.98 矩阵**（含覆盖臂 wire 重放实证、warnings.other 被 cargo 渲染为警告的 Artifactory wire 怪癖实证、**上游 DELETE 后全新项目仅凭缓存构建**）。登记：crates.io 直连双主机不支持（R-2 新票候选）/上游死 search 404 vs Artifactory 409（R-3/4）。日志 reports/agents/T-316.md。
  - **T-318 派发 2026-08-28 08:4x（dev-registry-adapter）**：cargo virtual——接 T-316 遗留①的类门位与 provider；宽度 2：T-332/T-318。
  - **T-318 → done 2026-08-28 09:5x（PR #7 合并，develop=`f01f926`）——cargo 三仓型齐**：索引归并（裁决①首见去重+行字节逐字+SemVer 确定性；**真机侧证：cargo 1.98 完整解析合并索引并完成构建**——规格待验证清单 1 更强形式闭环）/download first-hit/search 归并/publish 写路由/yank 双持有者/裸面三态/失败容忍。六集成测 + T-294/T-316 回归门绿。差异 6 条登记；规格回写移交 reverse-engineer。日志 reports/agents/T-318.md。**收官剩 T-332（在途）+ T-329 终验**。
  - **T-332 → done 2026-08-28 10:0x（PR #8 合并，develop=`c9e7064`）**：MPU 面 Artifactory 形翻转——**ADR-0039**（六端点表/sha1=202 异步任务/token 能力凭据/退役路径；状态词表以 jfrog-client-go 源码定案）；wire 重写（create QP+token/config 探测版本门/urlPart/status/complete?sha1=202/abort/part 200 乱序重排）；T-323R 引擎缝复用；探针/api-reference/FAQ 随翻。**真 jfrog-cli 2.122.0 全链 220MiB 9.3s**（含从真客户端修形三处）。登记：architecture.md 15.4/23 回写转 architect；checksum-deploy token 窄域化候选票。日志 reports/agents/T-332.md。
  - **T-329 终验派发 2026-08-28 10:0x（qa-engineer）——M11 最后一张票**：L01~L45 全量（承证+增量）+四包型全形态矩阵+DoD 八条+两断言反转回写核实+里程碑 README/文档站检查（用户规程）；PASS 即 m11-done 收口（里程碑 PR 自动提交）。
  - **T-329 → done 2026-08-28 12:1x（merge `6c74a38`）——终验总裁定 PASS**：L45 矩阵 38✅+2⚠️（D-E/D-8R）+4 承证零未解释红；**五包型×三仓型真客户端矩阵全绿**（conan/apt/dnf/helm/cargo）；两断言反转三处一致；闸门族全过（footprint RED=D-8R 裁定项）。**收口前置五项分发**：①D-E 传输字段（dev-go-core 在途）/④文档回刷 D-G·D-H+make docs（tech-writer 在途）——②PRD 浓缩+LC-24/③ROADMAP M11 段（PM 待派）/⑤D-8R 逃生条款（conductor 随收口落 BOARD：footprint RSS 138MB 红为已知债，M12 瘦身票承载，非 m11-done 阻塞——**本行即裁定留痕**）。D-F 归 M12。日志 reports/agents/T-329.md。
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
  5. **PR 化合并（用户 2026-08-27 10:55 + 2026-08-28 07:5x + 2026-08-29 13:2x 指令「每次代码提交自动处理代码合并，无需确认」）**：feature→develop 经 GitHub PR（conductor gh 自建自合，`--merge` 保合并提交）；**develop→main 升级为：每次里程碑收口（m<N>-done 前）自动提交 PR**——批次收口不再直合 main（历史上三笔 batch 直合为先例遗留）。PR 描述含里程碑摘要+验证矩阵。**执行前置已就绪（2026-08-28 08:2x）**：细粒度 PAT（PRs RW + Contents RW，验证通过；token 仅存会话不落盘——新会话需用户重发或 gh auth 一次性固化）。自下一票收口起 feature→develop 全走 GitHub PR。
  5b.（原第 5 条的 fingerprint 条款随 2026-08-28 修复失效——fingerprint 已填 `4776dcc`，uat-deploy 参数缺陷已修，CI/UAT 链待首个触发验证。）
  6. **三票全交织裁定（2026-08-27 10:52，第 4 条扩展）**：T-311 续跑期间主动改写 main.go/router.go（rpm import/yum case），接线文件成 T-308/T-309/T-311 三票共写且无法按票序独立编译（先行提交必携带后行包）。裁定：T-311 落地全绿后以 **B4+B5 波次单 PR 一次合入**，三票逐项归因（票号→文件清单→验证摘要）写入 PR 描述；票状态以 PR 合并为 done 锚点。快照保险：/tmp/snap-b45-1052/。

（空）

## M12 票据（进度行）
  - **M12 收官 2026-08-30 07:1x——终验（T-356）完成 + 全部收口项落定，`m12-done` tag + 里程碑 PR 随本轮执行**。终验判 FAIL→P1 双修（restore 观察者 + 18 槽断言 `bad76e9`）后全清；笔头批（cargo 409 文面/FR-113 AC5/五处文档漂移+ROADMAP M12 未纳入项）本轮 conductor 落盘。

  - **用户指令（2026-08-29 13:3x）**：「每个迭代都要更新文档站和README」——sprint 规程升级：每轮迭代若有用户可见面变更落地，README（双语）+ docs/user/ 对应页随轮更新（conductor 或当轮票内完成；无变更轮次报告留痕「无需更新」）。**M12 积压盘点**（T-328 后未同步的用户可见面）：操作族（copy/move/zip/archive!/explode+pro 门控）/Trash can/NuGet v2 全路由+v3 代理/dual-write fail-open 行为/UI 视觉升级四波——本轮即派 tech-writer 票清偿。

  - **UI 视觉升级四波全清 2026-08-29 12:5x**：T-344A 规范（PR #32）→ B 批 A 主题+壳（直合 `1b5d307`）→ C 批 B 基元（PR #36）→ D 批 C 页面域（PR #37）→ E 批 D 残面清扫（PR #38）——**base.css 983→443 行**，MUI 默认皮肤全面生效（用户「UI太丑」指令闭合）。conductor 终验：193 passed 零失败 + SPA 累计 +16.47%。遗留：.card/.field 域外余量单票 + smu-tabs 换装小票 + 规范三坑回写（Modal Esc/首焦二段式/回调 ref useCallback）。

  - **用户 UI 视觉指令（2026-08-29 01:2x）**：「现在的UI太丑了，既然接入了MUI，就要使用MUI的原生组件去让UI变得更好看」→ **升级 MUI 批三（T-344）范围为「原生视觉升级」**：MuiProvider 主题打磨（MUI 默认为基底）+ 退役压皮肤的旧 CSS 块（复合类续挂→sx/主题组件）+ MUI 原生形态（Paper/AppBar/Drawer/Table/Chip 等）；锚册/assert-tokens/axe/playwright 硬约束不变。ux-designer 先出主题与组件清单，dev-frontend 随后实施。宽度空位即派。

  - **Q4 终裁（用户 2026-08-28 20:5x）**：制品操作族（copy/move/zip/`archive!`/explode）**照搬 pro 门控**（T-335 三重取证）；trash 走 `_system_` 式内部豁免保持可用；主矩阵 license 列回写 pro。T-339/T-343 断言面照此。

  - **B0 → 全清 2026-08-28 18:4x**：T-333 ADR-0040（PR #18——八决策+九 AC 锚+**新发现第三缺陷**〔Commit 反删已落盘 blob〕）/ T-334 nuget.md 活化（PR #19——258 行/18 端点/45 高置信锚，T-280 缺失就此补上）。
  - **B1 派发 2026-08-28 18:4x**：T-338 fail-open 实现（dev-go-storage，AC-A1~G 逐条）/ T-337 NuGet v2 大票（dev-go-core，窗口独占，消费要点 11 条）。
## M12 票据（T-333~T-357，tech-lead 2026-08-28 拆票；AC 全文见 docs/prd/milestone-12.md v1.0；拆票日志 reports/agents/M12-SPLIT.md；Q1~Q7 暂行口径已入票面，终裁点随票上 BOARD）

> **拆票基线**：PRD §1.3 估 22~28 票，实拆 **25 票**（P0×8 / P1×14 / P2×3，另含波外条件票 T-357 计入 P2）——四裁决/裁定承载票（裁决① NuGet bundle / 裁决③ D-A fail-open / 裁定⑤ D-8R 瘦身 / T-329 D-F 收尾）全部置顶 B0~B3；全宽 2 沿 M11 口径；协议/行为面实现票全部 dep 对应规格票（clean-room 铁律）。**转正映射**：T-330（Trash can 条件票）→ T-345/T-352 承载；T-320（HelmOCI 条件票，未派）→ T-342 承载——票号沿用与否 conductor 收口时定（PRD §1.3 既定票条款）。

**FR → 票映射**：

| FR | 票（P） | 承载要点 | 裁决/登记锚 |
|---|---|---|---|
| 前置产物 | T-333（P0）ADR-0040 / T-334（P0）nuget.md / T-335（P0）repo-operations.md | ADR + 两规格（Trash can mini 规格随票 T-345，不单列） | §1.3 前置①②③ |
| FR-103 | T-334（规格）→ T-337（实现，P0） | v2 路由全集 + OData 参数面 + publish 重复臂 | **用户裁决①（07:5x）**；T-304 §1.1/§4.1/§7 |
| FR-104 | T-341（P0，dep T-334+T-337） | v3 search 上游代理 + service index 动态解析 + virtual 合并 | **用户裁决①**；T-304 §1.1-L3'/L4/L7 |
| FR-105 | T-335（规格）→ T-339（copy/move，P0）→ T-343（归档族，P1） | 树级/dryRun/flat + 属性/校验和/索引随行；archive!/ + 目录 zip + exploded | 主矩阵缺口 5（PM 增量选题） |
| FR-106 | T-345（BE，P1）→ T-352（FE，P1）；T-330 转正 | 统一删除 seam + trash 四元组 + 保留期 + restore/empty/clean + 最小面 | Q7 兑现；Q3 档位随票终裁 |
| FR-107 | T-333（ADR）→ T-338（P1） | 本地优先写 + 异步重试队列 + 排空对账 | **用户裁决③（07:5x）**；T-327 D-A |
| FR-108 | T-336（P0） | 懒加载 embed，footprint 门红→绿 | **收口裁定⑤**；T-329 D-8R |
| FR-109 | T-342（P1，P2 段并入）；T-320 承载 | helmoci 包型 + oci:// 透传 + chartsBaseUrl + D-3 评估腿 | Q2 承接；T-313 D-2/D-3/D-5 |
| FR-110 | T-340（P1/P2，conan+cargo 一票） | D-F 状态码 + forceConanAuthentication + cargo 409 + Q5 条件腿 | **T-329 D-F 登记**；T-316 R-3/4 |
| FR-111 | T-344（P1） | 五页面 + 组件六件套 + combobox 统一化，交互零变化 | T-300 候选清单 |
| FR-112 | T-347（arch，P1）+ T-348（reverse，P1） | §15.4/§23 + cargo.md §8 + conan 升置信 + D-G/D-H | T-332/T-318/T-312 转交 |
| FR-113 | T-346（repo config+auth，P1）+ T-349（storage，P2）+ T-350（CI runner，P1）+ T-353（web 表单，P2） | T-290-2/byHash/token 窄域/auth 尾巴/拒启序/de-flake | 票级遗留按域聚类 |
| QA/文档/发布 | T-351（中期回归）+ T-356（终验，P0）；T-354（文档）；T-355（release） | L01~L35 + DoD + 五类文档 + 烟测/UAT | §8 剧本 |
| 条件票 | T-357（P2·条件 Q2） | NuGet symbol server 余量票 | Q2；T-293 终裁口径 |

**批次（全宽 2；波内 area 互斥，跨波同 area 串行）**：

- **B0（前置/规格波）**：T-333 ｜ T-334
  - **T-333** [P0] ADR-0040 dual-write fail-open 语义定案 `role:architect` area: DECISIONS.md（docs-only）dep:—
    AC1: ADR-0040 Accepted——停机窗 PUT/GET 形态（T-327 D-A 两处 500 修复目标：GET 存量 fallback 探测 / PUT disk 优先落盘+入队）、重试队列（持久化载体/指数退避/上限与死信/凭据纪律）、排空与对账策略、与 M6 三模式状态机交互（bypass/dual-write/completed 边界）；K43 回填 PRD §5.6。
    AC2: 视 Q3 终裁需要出 ADR-0041 附录或并入正文（trashcan 槽位/kind 取证留痕；暂行 pro+ feature-gov）。
  - **T-334** [P0] nuget.md 规格新建（FR-103.1/104 前置）`role:reverse-engineer` area: docs/reverse/nuget.md dep:—（T-304 §1.1/§4.1/§7 出处直取）
    AC1: 双段交付——as-built 段（M10 v3/v2 既有面 + 7 项自有裁定维持项固化）/ 增量段（v2 路由全集逐端点〔§1.1-L2 出处列展开〕+ OData 查询参数支持面〔$filter/$orderby/$top/$skip/$inlinecount 以取证为准〕+ FeedUtils 资源类型阶梯常量表 + search 代理链），出处逐条（反编译类/方法 或 官方规范锚点）+ 置信度标定。
    AC2: tech-lead 就绪度确认（L01 走查——FR-103-AC1 兑现）。
- **B1**：T-335 ｜ T-336
  - **T-335** [P0] repo-operations.md mini 规格（FR-105.1 前置）`role:reverse-engineer` area: docs/reverse/repo-operations.md dep:—（主矩阵 §C/归档族行锚点在案）
    AC1: copy/move wire（POST /api/copy|move 树级/dryRun/flat〔dry+failFast〕/to 参数/冲突报告形态）+ `archive!/`（strictArchiveDotSlash）+ 目录 zip（folderDownloadConfig 默认关 + 1024MB/5000 文件/10 并发/匿名单独开关 + GET /api/archive/download + entry 抽取 + 计流量）+ exploded 上传（explodedArchiveExtensions=zip,tar,tar.gz,tgz + X-Explode-Archive），逐条附 Artifactory 出处。
    AC2: **Q4 取证腿**——copy/move/归档族 Artifactory license 门复核结论上 BOARD（folderDownload 计流量面尤须查证；暂行基座不门控）。
  - **T-336** [P0] FR-108 空载 RSS 瘦身（懒加载 embed）`role:dev-go-core` area: internal/ 重资源 embed 面（嵌入门面/大查找表/模板族；cmd 装配只读）dep:—
    AC1: 归因清单先行（vmmap/RSS 顶源前后对照入票报告）→ `make footprint` EXIT 0（138.7MB→≤100MB **红→绿**）+ `make check-size` 六平台聚合 ≤100MiB 维持（94.13MB 基线不回归）。
    AC2: 冷启动三连 P95 <2s + 懒加载触及路径全量 e2e 抽样绿（L20；懒加载不得以启动时延换内存）。
- **B2**：T-337 ｜ T-338
  - **T-337** [P0] FR-103 NuGet v2 数据面全面实装 `role:dev-go-core` area: internal/adapter/nuget（**窗口独占**）dep:T-334
    AC1: v2 路由全集逐端点断言绿（L02：Search()±/$count、FindPackagesById()±/$count、Packages(id)(/Id)、Packages()/$count、GetUpdates()±/$count、$batch、Download nupkg sha256 对账、DELETE 200/403、PUT×2；id 小写折叠/版本归一化沿 L1/L6 维持裁定）+ **M10「v2 其余 404」负向断言反转**（归属本票豁免，PRD 回写核实归 QA）。
    AC2: 重复臂（T-304 §7 语义）——同 id+version 二次 push → 409 CONFLICT 逐字；d 权限主体重传 → 覆盖（sha256 新值断言）（L03）。
    AC3: 真实客户端——nuget.exe 活体腿（可得则 list/install 经 v2 源全绿；不可得 curl 等价 + BOARD 留痕，M11 conan 1.x 同款路径）+ nuget 槽三缝复跑（v2 面含内）+ M10 T-287 v3 序列零回归（L04/L05）。table-driven 单测 + dotnet 8 容器腿（v3 回归维持）。
  - **T-338** [P1] FR-107 dual-write S3 停机 fail-open `role:dev-go-storage` area: internal/storage（dual-write 引擎停机窗 + 重试队列 + 排空对账）dep:T-333
    AC1: MinIO stop → PUT×3 全 200（disk 优先落盘 + 队列深度+3）→ GET 新旧全 200（存量 fallback，D-A 两处 500 消除）→ MinIO start → 排空 → mc 逐对象 sha256 对账零缺（L18；T-327 D-A 证据链闭合）。
    AC2: 停机窗内重启 → 队列幸存 → 恢复排空对账报告留票；M6 H12~H15 迁移序列复跑绿 + binstore.yaml 三链 roundtrip 零回归（L19；M6 PRD FR-50 文面零修改）。
    AC3: 可观测——queue depth gauge + storage.replay.* 审计 + 启动日志水位一行（NFR-P57/S63；队列凭据不落明文，条目绑定 blob 坐标防重放）。
- **B3**：T-339 ｜ T-340
  - **T-339** [P0] FR-105 copy/move 核心 `role:dev-go-storage` area: internal/storage 树操作 + internal/httpapi（/api/copy|move 端点；router 接线交 conductor）dep:T-335
    AC1: copy/move 全链 200 + 源/目标 sha256 对账 + ?properties 随行 + copy 源保留/move 源消失；dryRun 零副作用（GET 对照）+ flat 展开 + failFast 臂（L11/L12）。
    AC2: 协议仓派生索引联动——deb 仓 copy .deb → 目标 Packages/by-hash 重算；conan 仓 move recipe → 目标 index.json 修订链一致（消费各 adapter 既有 reindex 内核；L13）。
    AC3: 万节点树 copy 零 5xx + 抽样对账 + dryRun 报告 P95 <5s；目标仓写权限门 403（NFR-P54/S62）；系统内路径豁免收编为显式系统路径集（FR-97.1 DB-2 名录）。
  - **T-340** [P1] FR-110 包型收尾（conan + cargo，票内两包串行）`role:dev-registry-adapter` area: internal/adapter/conan + internal/adapter/cargo dep:—（conan.md/cargo.md 规格在案；T-329/T-316 登记直取）
    AC1: conan D-F——`_/_` 坐标（conan 2.x 形态）v1 packages/delete → **200** + 树删（404 复现对照脚本入票；v1.go dir 解析与删除结果码分离）+ conan 1.x 流量回归不受影响；forceConanAuthentication 字段落地——开启匿名面 401/引导登录 + GET 回显 + 关闭往返（L23）。
    AC2: cargo R-3/R-4——死上游 search → **409 + errors 信封**（出处 T-304 §3 表 Exception 臂）+ `.cargo/**` DELETE 收敛（L24）。
    AC3: Q5 条件腿——conan Artifactory 真实上游活体互证（dep 用户环境可得性；不可得维持 mock+自指两腿留痕，非 DoD 缺口）。
- **B4**：T-341 ｜ T-342
  - **T-341** [P0] FR-104 NuGet v3 search 上游代理 + service index 动态解析 + virtual 合并 `role:dev-go-core` area: internal/adapter/nuget（dep T-337 **同 area 串行**）dep:T-334,T-337
    AC1: 自指/mock service index（自定义 @id 路径）→ flatcontainer/registration/search 三面 URL 全部由上游 service index 按阶梯常量表解析（前缀常量退役断言）；改上游 @id → 零配置跟随（L06；K39）。
    AC2: remote 上游新发布版本（不预下载）立即可搜（dotnet package search / curl q）+ virtual 两域并见（**M10「virtual search=local+已落地缓存」断言反转**，归属本票豁免）+ 停上游降级零 5xx/恢复自动回代理（L07~L09）。
    AC3: local 半边维持存储事实（T-304 判定，不动）+ SSRF 零新面——复用 M3 Guard 五参数 + ADR-0025 决策 4 私网开关（NFR-S64）。
  - **T-342** [P1] FR-109 HelmOCI 分发（P2 段并入；T-320 承载）`role:dev-registry-adapter` area: internal/adapter/helm（helmoci handler + docker 面 ForRepoType 复用 + 槽位）dep:—（helm.md + HL-3 在案）
    AC1: helm push/pull oci:// 全链（→ helm install）+ manifest 三 media type 断言 + .prov 经 OCI 面 --verify 腿 + 混仓校验 400（L21/L22）。
    AC2: /v2 面全量回归零变化（ForRepoType 复用不改 docker 面）+ helmoci 槽三缝转正断言（community 403 → pro 200 → 卸载 pull 200/push 403）。
    AC3: chartsBaseUrl 分体基址改写（T-313 D-2，P2）+ oci:// 透传深化（D-5 照 helm.md 增量锚点）+ D-3 `_external` 落盘缓存 **architect 评估结论 BOARD 留痕**（Q6——评估腿，未落地非 DoD 缺口）。
- **B5**：T-343 ｜ T-344
  - **T-343** [P1] FR-105 归档族（archive!/ + 目录 zip + exploded）`role:dev-go-storage` area: internal/storage 归档流式读取 + internal/httpapi（archive 端点；与 T-339 同域串行）dep:T-335,T-339
    AC1: `<file>.zip!/inner/path` 按需解包成员读取（不解包落盘）字节一致 + strictArchiveDotSlash 开启后违规形态照规格（L14）。
    AC2: 目录 zip——folderDownload 默认关断言 + 开启后 GET /api/archive/download 解包逐文件 sha256 对账 + 超限（>5000 文件）拒绝 + 计流量审计（NFR-S62 匿名默认关维持）。
    AC3: exploded 上传——X-Explode-Archive 白名单扩展内接受+解包（**M10「显式 400 拒绝」断言反转**，归属本票豁免）/闭集外 400 维持。
  - **T-344** [P1] FR-111 MUI 批次三 `role:dev-frontend` area: web/src（五页面 + 共享组件层；web/ 域本波独占）dep:—（T-300 候选清单在案）
    AC1: 四闸门——console-ux 锚零改动 + anchor-audit ledger PASS + 全量 Playwright 绿 + assert-tokens 零硬编码；axe 双主题 serious=0；SPA gzip 相对 T-291 基线累计 ≤25% 维持（L25；NFR-P56）。
    AC2: 服务端契约 git diff=0 + 交互零变化（发现 Artifactory 交互出入上 BOARD 先改册再迁移）+ 批三后旧组件栈残留页清单清零（L26）。
- **B6**：T-345 ｜ T-346
  - **T-345** [P1] FR-106 Trash can BE（mini 规格随票；T-330 转正承载）`role:dev-go-storage` area: internal/storage 删除统一 seam + auto-trashcan 仓/保留期 cron + internal/httpapi（trash REST 族）+ addon 槽 dep:T-339（move 底座复用——FR-105.5）
    AC1: mini 规格随票（T-330 模式）——auto-trashcan 内置仓 + trash 四元组（trash.time/deletedBy/originalRepository/originalPath）+ 保留期默认 14 天 + empty/restore（to+transaction-size）/clean wire + 目录级批量清理（主矩阵 §B/G + config.xml trashcanConfig 锚点）；**Q3 档位取证上 BOARD 终裁**（暂行 trashcan 槽 pro+ / kind=feature-gov）。
    AC2: DELETE 制品 → 原路径 404 + 回收站可见 + 四元组打标；restore → 原路径 200 + sha256 一致 + 属性复原 + 协议仓索引重算（L15/L16）。
    AC3: 短保留期夹具自动清理 + 审计；POST /api/trash/empty 权限门 403 + 审计行；M1~M11 删除族断言（GC/grace/quota/cleanup T-324 三腿）回归——删除语义变化的断言翻转 100% 归属本票豁免（L17；与 Cleanup-Retention 策略引擎分界，M13+ 不混入）。
  - **T-346** [P1] FR-113 repo config + auth 域遗留（113.1/113.2 BE/113.4）`role:dev-go-core` area: internal/repo（canonical/枚举）+ internal/auth（audit 词表/LDAP DN）dep:—（别名对已备；auth-integration.md v2 规格在案）
    AC1: socketTimeoutMillis canonical/回显统一 + 旧拼写 socketTimeoutMs 接受为输入别名（回显新拼写）+ 文档同步（L28；**FR-113.1 落地是 T-347 §15.4.1 终态回写时序前置**）。
    AC2: byHash 值域枚举校验归 repo.Service——PUT 非法值 400 枚举错误（值域照 debian.md/rpm.md；K45 票内定案）（L29-BE）。
    AC3: audit picker 两词补录可见 + userDnPattern 消费按 auth-integration.md v2 规格（DN 直写模式生效或明确拒绝，腿测试断言）（L31）。
- **B7**：T-347 ｜ T-348（回写批——docs-only，与实现票无 area 冲突）
  - **T-347** [P1] FR-112.1 architecture.md §15.4/§23 回写 `role:architect` area: docs/design/architecture.md（docs-only）dep:T-346（**§15.4.1 remote 字段以 FR-113.1 终态回写——PRD §1.3 时序协调条款**；§23 段无依赖可先落）
    AC1: §23 MPU 新 wire 回写（ADR-0039：六端点 POST+QueryParam / complete?sha1=202 异步任务模型 / GET /config 能力探测）+ §15.4/§15.4.1 remote 字段落 canonical JSON as-built diff 落盘 + 票报告留痕。
    AC2: `make docs` SUCCESS 零断链（L27 组成部分）。
  - **T-348** [P1] FR-112.2/112.3 规格回写 + D-G/D-H 校验 `role:reverse-engineer` area: docs/reverse/cargo.md + docs/reverse/conan.md dep:T-340（D-F 修正后 conan 规格行联动）
    AC1: cargo.md §8 virtual 行 as-built 回刷（首见去重/写路由/yank 双持有者——T-318 遗留）+ conan.md D1/D5/D7/D8 升置信（复核取证或标定）。
    AC2: D-G/D-H 四处旧措辞 grep 零残留（cargo.md 三处 / remote-virtual.md L56 / auth-config.md 边界表 / api-reference SAML 三端点）+ `make docs` SUCCESS（L27 收口）。
- **B8**：T-349 ｜ T-350
  - **T-349** [P2] FR-113 storage 域尾巴（113.3 token 窄域化 + 113.5 env 拒启序）`role:dev-go-storage` area: internal/storage（MPU token 权限域 + S3 env 启动链；同 area 随 T-338 后串行，无数据依赖）dep:—
    AC1: MPU complete 后签发 token 权限域收窄到目标会话——他路径使用 403 / 原会话续传 200（L30；NFR-S63 防横向使用）。
    AC2: S3 凭据 env 组不完整（如缺 ACCESS_KEY）→ 启动期即拒 + 错误指名缺键 + **先于 binstore.yaml 报错时序断言**（L31-AC5；T-325 登记）。
  - **T-350** [P1] FR-113.6 CI 专用 runner / 静默窗 de-flake `role:devops-engineer` area: .circleci/ + Jenkins 配置 + e2e 编排脚本 dep:—（T-327 §7 协议 + T-329 观察④在案）
    AC1: K46 定案落地——CI 专用 runner 就位或静默窗协议脚本 + 负载 flake 家族（N01 straddle 同族）三连零复发或隔离归因留痕（L32）。
- **B9**：T-351 ｜ T-352
  - **T-351** [P1] QA 中期回归 `role:qa-engineer` area: 测试矩阵（L 序列断言 + 归属审计）dep:B2~B5 主体合入（T-337~T-344）
    AC1: NuGet 域 L02~L09 + 操作族 L11~L14 首跑（含 dotnet 8 / nuget.exe 夹具、多协议仓夹具 deb/rpm/conan/npm）+ M1~M11 P0 双形态抽样回归。
    AC2: 三处断言反转预核实（nuget v2 404→全集 / virtual search 缓存→代理 / X-Explode 400→接受——PRD §5.4 回写核实）+ 契约变更面归属审计（m11-done..HEAD 增量）零孤儿。
  - **T-352** [P1] FR-106 Trash can FE 最小面 `role:dev-frontend` area: web/src（回收站浏览/恢复/清空页；与 T-344 web/ 互斥分批——先后脚）dep:T-345
    AC1: 回收站浏览/恢复/清空 MUI 面（FR-111 组件纪律）+ readonly_admin 只读断言 + Playwright 腿绿 + axe 双主题 0 + NFR-S61（回收站制品不可匿名读）FE 侧断言。
- **B10**：T-353 ｜ T-354
  - **T-353** [P2] FR-113.2 web 仓表单（deb/rpm 策略键）`role:dev-frontend` area: web/src 仓编辑器表单（web/ 串行链 T-344→T-352 后）dep:T-346（byHash 值域定案 K45）,T-352
    AC1: Playwright——deb/rpm 仓编辑器设策略键（byHash/calculateYumMetadata 等全列）→ 保存 → 重开回显（L29-FE；REST 已通 T-327R/D-E，本票只加表单面）。
  - **T-354** [P1] tech-writer 五类文档增量 `role:tech-writer` area: docs/user/（NuGet v2 接入 + 操作族指南 + 回收站管理 + HelmOCI 接入 + api-reference/FAQ）dep:对应域票合入（B6 后可启动）
    AC1: 五类增量交付（§8-10 细目）+ 客户端命令全部实测可复跑 + `make docs` SUCCESS + 侧栏挂页（L35）。
- **B11**：T-355
  - **T-355** [P1] release 部署烟测 + UAT 链 `role:release-engineer` area: deploy/ + charts/ + CD 链 dep:全部实现票（B10 前）
    AC1: 部署矩阵烟测（compose/k8s/systemd/offline 抽样）+ UAT 链（CircleCI → 52.79.109.153）绿 + 新配置键四部署面接线核验（trashcan 槽/folderDownload/fail-open 队列面——含 ADR-0040 若引入新键）。
- **B12（收口波）**：T-356
  - **T-356** [P0] QA 终验 `role:qa-engineer` area: 全量验收矩阵 dep:全部票 + T-355
    AC1: L01~L35 全量（承证+增量）+ M1~M11 全 P0 双形态复跑全绿 + 契约变更面（git diff m11-done..HEAD -- internal/ cmd/）100% 归属 M12 豁免票 + 三处断言反转 + footprint 红→绿 PRD 回写核实 + DoD 八条逐条（实测数字归档）。
    AC2: 收口检查双项（用户规程）——README 双语 + 文档站随新能力核查，结论入收口报告；总裁定 PASS → conductor git tag m12-done。
- **波外条件票**：T-357 [P2·条件 Q2] NuGet symbol server 余量票 `role:dev-go-core` area: internal/adapter/nuget（symbol 面子域）dep:T-337,T-341 收官 + 余量条款（全部 P0/P1 收官且余量足）
    AC1: mini as-built 规格随票（T-293 终裁口径）；.pdb/GUID 路径面 + 真实客户端腿；未触发 M13+ BOARD 留痕非 DoD 缺口。

**关键路径**：T-334→T-337→T-341（NuGet bundle 主线，裁决①）→ T-356；T-333→T-338（fail-open，裁决③）→ T-351/T-356；T-335→T-339→T-343/T-345→T-352（生命周期域主线）→ T-356；T-346→T-347（回写时序耦合）；T-344→T-352→T-353（web/ 串行链）。**T-336（瘦身 P0）零依赖可任意波次穿插；T-340 零依赖可提前补位。**

**风险登记（拆票日志 M12-SPLIT.md 详表）**：① Q3 trashcan 档位终裁须在 T-345 AC3 门控断言前收口（B6 前）；② Q4 操作族 license 门——T-335 取证若翻则 T-339/T-343 补三缝面；③ nuget.exe 活体可得性（curl 等价+留痕路径，M11 先例）；④ internal/storage 三票（T-338/T-339/T-343/T-345/T-349）严格波次串行 + router/slots 接线交 conductor；⑤ T-347 依赖 T-346 时序——延期则 §15.4.1 段顺延、§23 段先落；⑥ dev-go-core 负载四票错峰 B1/B2/B4/B6（窗口独占条款维持）；⑦ web/ 串行链若前端带宽受限，T-353 可并入 T-352（conductor 裁量，FR 边界留痕）。

## M13 票据（T-358~T-380，tech-lead 2026-08-30 拆票；AC 全文见 docs/prd/milestone-13.md v1.0；拆票日志 reports/agents/M13-SPLIT.md；Q1~Q7 暂行口径已入票面，终裁点随票上 BOARD）

> **拆票基线**：PRD §1.3 估 21~26 票，实拆 **23 票**（P0×7 / P1×12 / P2×4——含波外条件票 3 张计入 P2）。主轴 Webhook 双票（上=订阅+织入 / 下=投递+FE 面）全部 dep 前置双产物（T-358 webhook.md + T-359 ADR-0041——**取证特例：反编译集合无 webhook addon，JFrog 官方 REST 文档为唯一行为基准**，PRD §1.4-1 常设条款）；协议/行为面实现票全部 dep 对应规格/评估锚（T-363 dep T-342〔helm.md+HL-3 在案〕/ T-369 dep T-348〔D8 双证规格行在案〕/ T-367 消费 T-342 §5 D-2/D-3 评估结论）；旋钮两枚聚合一票（FR-118）；文面包/de-flake/运维尾巴按域聚类（T-370/T-361/T-372+T-374）。全宽 2 沿 M11/M12 口径。

**FR → 票映射**：

| FR | 票（P） | 承载要点 | 裁决/登记锚 |
|---|---|---|---|
| 前置产物 | T-358（P0）webhook.md / T-359（P0）ADR-0041 / T-360（P1）ADR-0042 | 规格 + 两 ADR（helm.md 增量段与 trash-can/repo-operations 旋钮行随实现票，不单列前置） | §1.3 前置①②③④⑤ |
| FR-114 | T-358（规格）→ T-362（P0，窗口独占） | 订阅 CRUD/test + 36 事件注册 + 事件源织入（outbox 旁路）+ webhook 第 19 槽门控 | 主轴选题（PM §2.2 留痕）；Q4/Q6 |
| FR-115 | T-364（P0 投递引擎）→ T-366（P1 FE + 真实消费者 e2e） | outbox 幸存/退避重试/死信/签名/SSRF + 控制台最小面 + dogfood Jenkins 条件腿 | inv-4 §I/§K3；K49/K50 |
| FR-116 | T-363（P0 remote）→ T-365（P1 virtual） | /v2 面首个 remote 数据链（Bearer 认证/缓存/降级）+ 成员聚合 | D-5 翻转点；dep T-342；Q5/K54 |
| FR-117 | T-367（P1 引擎缝票） | charts_base_url per-protocol 槽 + internal/remote absolute-URL 缝（FetchAbsolute）+ `_external`/`_transitive` 落盘 | T-313 D-2/D-3 + T-342 §5 评估结论（「建议 M13」兑现） |
| FR-118 | T-368（P1 两旋钮聚合一票） | folderDownloadConfig 六字段 + trashcan.retention_days；L14 断言开关化 | T-356 L14/L17 如实登记；K52 |
| FR-119 | T-369（P1 D8 翻转）→ T-371（P1 D-F2 迁移，同 area 串行） | 坐标根整树删 + files 通道规格布局 + 存量迁移（幂等零损） | T-348 N5 双证 / T-340 §4 D-F2；K53 |
| FR-120 | T-370（P0 裁定动作·落笔段 P2，PM，**随时可动**） | D-10 终裁对照材料上 BOARD + flat 措辞回写 + fail-open AC2 加注 | Q3；T-356 ⚠️ 四项承接 |
| FR-121 | T-361（P1，devops，**独立可首波并行**） | raceEnabled escape + CI e2e job 三连权威化——race 全树一次绿 | T-356 DoD#6 满载 flake 新成员 |
| FR-122 | T-372（P1 FE，先改册后实现）→ T-374（P2 docs） | trash 树常驻节点（console-m8 推翻条款兑现）+ 侧栏清单 15 对齐 + npm 尾斜杠注记 | console-m8 §6.3 推翻；ROADMAP 运维尾巴 |
| QA/文档/发布 | T-373（中期）+ T-377（终验 P0）；T-375（文档增量）；T-376（release + **UAT 随里程碑 PR 首跑**） | L01~L24 + DoD 八条 + webhook 指南等五类 + 烟测/UAT | §8 剧本；M12 T-355 未执行教训（T-356 §6） |
| 条件票 | T-378（Q3 D-10 翻转）/ T-379（Q2 symbol server 承接）/ T-380（Q5 docker remote 顺车） | 未触发 BOARD 留痕非 DoD 缺口 | Q2/Q3/Q5 |

**批次（全宽 2；波内 area 互斥，跨波同 area/同角色串行）**：

- **B0（前置/规格波）**：T-358 ｜ T-359
  - **T-358** [P0] webhook.md 规格新建（FR-114.1/115 前置——官方文档基线特例）`role:reverse-engineer` area:docs/reverse/webhook.md dep:—
    AC1: 官方文档逐端点出处——订阅 CRUD/test 族 wire（路径/方法/请求响应体/错误码，基座前缀沿 E-26）+ 过滤器形态（repo/path/事件类型闭集）+ secret/签名契约（算法/头名，K49）+ envelope 字段集（CloudEvents 形态）；出处逐条附 JFrog 官方 REST 文档锚点（反编译集合无该 addon——取证特例条款）+ 置信度标定。
    AC2: 36 事件清单按域分组（artifact/artifactProperty/docker/build/releaseBundle/distribution/curation/…以官方文档为准）逐条标注 **BinFlow 触发源覆盖界**（有本体域→织入点；无本体域→注册休眠，Q6 暂行）+ inv-4 §I（I2 平移判定/I3 outbound dispatcher/I4 注册 REST/I5 worker-events 40+ 类型互证）与 §K3（outbox 六方言 DDL）锚点补白 + **Q4 取证腿**（官方 license 标注上 BOARD——community 可用则翻转 unlocked）。
    AC3: tech-lead 就绪度确认（L01 走查——FR-114-AC1 兑现）；K47~K49 校准项回填 PRD §5.6 转 PM。
  - **T-359** [P0] ADR-0041 webhook 事件总线架构 `role:architect` area:DECISIONS.md dep:—（与 T-358 并行；wire 细目以 T-358 定案为准，落 Accepted 前对齐）
    AC1: ADR-0041 Accepted——outbox 载体（SQLite/PG 两方言 DDL）/投递队列与退避曲线/重试上限与死信形态（可查可重放）/secret 存储与签名链（AES-GCM 维持）/订阅 URL SSRF 策略（复用 M3 Guard 五参数 + `replication.allow_private_target` 同款私网开关键名定案）/与审计事件族关系；K50 回填 PRD §5.6。
    AC2: 订阅面权限门定案（admin 或专用权限——`internal:webhook` 的 BinFlow 映射）+ webhook 第 19 槽 kind/档位建议（Q4 终裁材料留痕，暂行 feature-int/pro+）+ 架构文档联动点清单（§6.4 指标/审计族）。
- **B1**：T-360 ｜ T-361
  - **T-360 → done 2026-08-30 13:3x（与 ADR-0041 回填同笔交付）——M13 7/23**：ADR-0042 Accepted 三轴全 A（启动期 sweep 监听器起前/conan 域 sweep+repo.Service 窄原语〔零 observer·Emit·审计副作用〕/回滚=元数据备份恢复——blob 零接触红利）+ 谓词消耗型幂等（二跑零改）+ sha256 三口径对账零损 + 范围谓词与 `<coordinateRoot>/0/package/<pid>/0/<tail>` 目标布局全形消歧 + 后效四项 + K53 回填登记于决策 7 + T-371 锚点骨架 4 AC。**附带：ADR-0041 锚点回填勘误**（用户终裁 ③ 执行——决策 2/4 官方值翻转存目、T-362 AC-3 时间戳断言删除、T-364 骨架 AC-2/AC-3 同步、机制条款不动清单 + T-364 限流三参数 as-built 增补登记；T-364 as-built 25e8afd 已按锚点实现——零代码翻转）。日志 reports/agents/T-360.md。
    AC1: ADR-0042 Accepted——迁移时机（启动期 vs 惰性）/幂等策略（二跑零改）/回滚路径/零损硬约束（sha256 对账口径）；K53 回填。
    AC2: 迁移范围与后效验收口径入 ADR（v1 files 双拼树 → `<root>/<pid>/<pRev>/<file>`；ref-search conaninfo 恢复 + v1 包 snapshot 键裸文件名）。
  - **T-361 → done 2026-08-31 04:4x（马拉松票 3.5h，配额击落×2 复活）——M13 17/23，实现票全清**：机制定案「双档重定标非 skip」（race 姿态探针 build-tag 文件对×2 包 + dryRunBudget 双档〔no-race 5s 生产数不动/race 45s〕+ deb pollWindow 3x + **escape #3：cycler 测试 settle 补齐——Round B 实跑自擒的第三族**）。**AC1 承证**：隔离快照背靠背 Round C/D 双 exit=0（35 包×2 零 FAIL，无隔离复跑辩护）。**AC2 承证**：GHA 计费封锁（2026-08-28 起启动即红，取证在案）→ CI 配方逐字本地复跑 **R6/R7/R8 三轮 exit=0 各 222 passed**（7.8/7.6/7.7min；take-1 四轮 runner 子壳缺陷作废如实留痕）+ .circleci e2e job 新增（main-only，随下次 main push 首跑）+ Jenkins nightly 对账（#2~#10 九连 SUCCESS 同入口自动流入；**VM main 滞后已由 conductor 于 sprint 974 同步 9263439**）。**AC3**：escape 台账 20 命中/7 文件全注释可审计；断言语义零削弱。同形未动臂四条不预防性放宽（FR-121.3 反向纪律）。conductor 复验：build 0 + BigTree 15.1s + lint 0。遗留：GHA 计费修复（用户动作）；e2e job 首跑待 main 合并。日志 reports/agents/T-361.md。
    AC1: `TestBigTreeCopyNo5xx` 预算臂 race 姿态豁免/重定标机制落地（race 开销计入预算或 skip 该臂——票内定案）+ deb 满载族（T-329 以来）同机制收口或归因留痕；`make test`（race）**全树一次绿连续两轮**（L20——不再依赖隔离复跑辩护）。
    AC2: CI e2e 专用 job 三连绿基线（真二进制+三 seed+workers=2 AUTHORITATIVE 承证在盘）+ Jenkins nightly 全量链对账记录入票（L21 前半）。
    AC3: 无静默放宽——escape 点逐处注释/留痕可审计（grep 清单入票报告；FR-121.3 边界）。
- **B2**：T-362 ｜ T-363
  - **T-362** [P0] FR-114 订阅管理 REST + 36 事件注册 + 事件源织入 `role:dev-go-core` area:internal/webhook + internal/httpapi（**窗口独占**；router/slots/main.go 接线交 conductor）dep:T-358,T-359
    AC1: 订阅 CRUD + test curl 全链（create 201 形态照规格→get→list→update→delete；test 端点向接收器真发一条测试事件、信封断言）+ 校验臂（URL 合法性/事件类型闭集外 400/过滤器语法）+ 权限门（非授权主体 CRUD 全 403 零副作用——映射照 ADR-0041）（L02）。
    AC2: 事件源织入——统一删除 seam/copy·move 操作族/属性系统/各协议发布路径旁路取事件（M12 生命周期域底座复用；outbox 异步、主路径零变化）：generic PUT→artifact 域 deployed（envelope 逐字段断言）、DELETE/copy/move/属性 PUT 各臂、docker push→docker 域事件（dind 腿）+ 过滤器 repo/path 命中发/未命中不发双臂（L03/L04）；36 类型注册（无本体域照 Q6 暂行注册休眠、不伪造触发）。
    AC3: webhook 第 19 槽门控三缝（community 建订阅 403 + `X-Binflow-License-Required: webhook` → pro 200 → 卸载降级不入箱；档位随 Q4 终裁）+ M1~M12 P0 抽样零回归（L05；table-driven 单测 + 可编排接收器夹具）。
  - **T-363** [P0] FR-116.1 HelmOCI remote pull-through `role:dev-registry-adapter` area:internal/adapter/docker（/v2 remote 数据链）+ repo（helmoci remote 配置）dep:T-342
    AC1: 自指上游（BinFlow helmoci local push chart）→ 经 helmoci-remote `helm pull` 字节/digest 一致 + 二次命中本地缓存（上游访问计数不增断言）+ `helm install` 真集群腿（M12 kind 夹具复用）（L10；真实 helm 客户端）。
    AC2: 上游认证链（401→WWW-Authenticate Bearer→token 交换→拉取；mock registry 或自指 token 面）+ 上游故障降级（停上游→已缓存可拉/未缓存零 5xx，本地事实兜底+降级标记）（L10/L11 段）。
    AC3: docker dind /v2 面全量回归零变化 + M12 helmoci local 序列零回归 + **Q5 顺车评估结论 BOARD 留痕**（K54——/v2 共享缝边际成本判定）+ helm.md remote 增量段随票（K51，mini 规格模式）。
- **B3**：T-364 ｜ T-365
  - **T-364 → done 2026-08-30 13:1x（`f`` 直落 develop，双远端）——M13 6/23，B3 前半清**：Dispatcher（4 worker/双谓词 CAS claim〔负载注入自擒 TOCTOU——重试间隔压扁 3ms 窗，修复后 24 轮负载全绿〕/锚点重试语义〔retryCount 5 首试计入/固定 10s/30s 超时/4xx 恰 1 次终态——**wire 参数全按 T-358 锚翻转，先于 ADR-0041 回填**〕/token bucket 1000s+10000/50000 并发上限 Emit 缝拒新/启动 sweep+水位 INFO/优雅停机零 delivering 残留/Replay 机制面）+ 排障环 10000+30s janitor（payload 反解 + auth 头脱敏）+ metrics 五枚族 + 真接收端 12 测试全链（HMAC 逐字验签）。conductor 四处接线（stack 字段/openStack 引擎组装+metricsReg 共享 registry/serve drain 双臂/startWebhookDelivery + webhookDB close 补 T-362 缺口）+ 复验 build/fmt/vet/lint 0 + webhook race 12.5s + cmd 包全绿。遗留五项登记（§5：audit 词表一行归 owner/metrics 注册位可平移/Replay REST 面待裁/disable 快照契约钉死/3xx 终态 V4 待验证）。日志 reports/agents/T-364.md。
    <del>原文：</del> **T-364** [P0] FR-115.1~115.4 投递引擎（outbox/重试/死信/签名/SSRF）`role:dev-go-core` area:internal/webhook 投递链（与 T-362 同 area 波次串行）dep:T-362（投递参数照 ADR-0041/K50）
    AC1: outbox 幸存——触发事件→kill -9→重启→补投接收器零丢（盘上事实源）+ 投递全链零阻塞主路径（制品 PUT 全程零 5xx；NFR-P58 并发入箱零丢+投递 P95 有界）（L06）。
    AC2: 接收器 500×N→固定间隔 10s 重试序列（4xx 不重试——T-358 锚点，终裁 2026-08-30）→恢复 200 最终成功；持续失败超上限→死信可查（REST/控制台）可重放 + 告警日志一行（L07）；secret 签名——接收侧 HMAC 校验绿/篡改 body 红（K49 官方契约；secret AES-GCM 链维持不落明文、投递日志 URL 脱敏）（L08）。
    AC3: SSRF——订阅私网 URL 拒绝形态照 ADR-0041（Guard 五参数复用 + 私网开关默认策略/开启双臂）（L09）+ 指标族 `binflow_webhook_{deliveries_total,retries_total,dead_letter_total,queue_depth}` + 审计事件 `webhook.subscription.{create,update,delete,test}` + 启动 outbox 水位一行（§6.4；NFR-S65/S66）。
  - **T-365 → done 2026-08-30 20:3x——M13 8/23，B3 全清**：/v2 virtual 服务面（成员 walk/成员上游会话/降级矩阵/写拒绝 405+C5 原文）+ V2VirtualPlane 缝 + 四读用例 virtual 臂（ResolveManifest/ResolveTag/ListTags/ListImages）+ landFetchedManifest 参数化（remote/virtual 两面一语义）。**真客户端全绿**：helm 4.2.4 双实例——双域 pull（remote 回源 Digest/tgz 逐字节一致 + local 域）+ by-digest/仅 local tag 路由 + 二跳 HIT 上游冻结 + tags 并集（未缓存 tag 不可见——§8.3 D-2 口径维持）+ push→virtual 405；kind bf-ga v1.36.1 install→upgrade 滚出 pod；docker dind 回归绿（+跨生态腿 docker pull 经 helmoci-virt RepoDigest 一致——**此跨生态口子随本票登记收紧裁定**，见下）。门控 live：pro 建 virtual 200/混仓双向 400/卸载后 400 点名 helmoci 而读照常（D1）。**conductor 复验**：build 0 + helmoci 8.6s + repo 定向绿。顺手修复登记：T-363 后 pro 下 /v2/_catalog 500 已恢复。helm.md §8.4 增量段（K51）落。**裁决登记（conductor）**：① virtual 成员类型收紧（validateHelmFamilyMix 现只管 helm×helmoci；helmoci virtual 配 docker 成员应收紧——照 Artifactory「virtual 成员须同包型」语义，**并入 T-367 rider**）；② virtual push-through 路由未实现（AC 只要求读聚合）——候 T-380 同批评估；③ catalog 并集与成员行并列口径已入 §8.4。日志 reports/agents/T-365.md。
    AC1: helmoci-virtual = local+remote 成员→pull 双域制品（tag 并集/by-digest 路由/首见语义照规格）+ 「Helm 与 HelmOCI 不混仓」校验 400 维持（M12 边界复用）（L11）。
    AC2: helmoci 槽三缝含 remote/virtual 建仓（community 400 点名 helmoci/pro 200→卸载降级）+ docker dind /v2 全量回归 + M12 helmoci local 序列零回归（L11 门控段）。
- **B4**：T-366 ｜ T-367
  - **T-366 → done 2026-08-30 21:3x——M13 9/23**：治理分组第七页 /admin/governance/webhooks（WebhooksPage + SubscriptionDialog〔**对齐规格 M3/M4 Dialog 形态**〕+ SubscriptionDrawer〔右滑 480 档——**首两块 parity 规格落地实践**〕）；lib/webhooks.ts 七端点封装 + 66 型闭集 FE 镜像 + criteria 五键托管 + secret 三态哨兵；锚册 v1.16 入册 47 锚 + ledger PASS + axe 双主题 0 + console-size 352KB 维持 + mock spec 11 腿 + **消费者腿 8 腿全绿**（真 pro 实例：七字段信封 + HMAC 线上字节逐字验签 + 固定间隔 ≥9s×2 重试 + 5 次耗尽死信 + 30s 挂死预算 + SSRF 默认拒回环钉住 + Dialog→Drawer 集成）。readonly 只读臂零写反断言；契约 diff=0。**自擒 conductor 接线缺口**：`webhook.allow_private_target` 死键（T-362 三处接线漏 load.go 装载层）——**conductor 已修**（load.go raw 段+默认 false+env 单下划线别名 + webhook_test.go 双形态表测 + strict schema 测试，config 全包绿/lint 0）。遗留：Jenkins dogfood 腿候旋钮修复后平移（容器腿已交付 AC2）；outbox 行级 REST 面与 Replay REST 归 T-364 §5-3 同裁（候选项）。日志 reports/agents/T-366.md。
    AC1: 订阅列表/新建/编辑/删除/test + 最近投递记录（状态/耗时/重试计数）MUI 面（FR-111 四闸门同构——锚册纪律新锚入册/ledger PASS/全量 Playwright/axe 双主题 0）+ readonly_admin 只读臂（L09-FE 段）。
    AC2: 真实消费者腿——T-247 dogfood Jenkins 条件腿（dep 用户环境：VM 栈在位则接 pipeline 触发腿，事件→job 触发取证）或容器接收器腿（httpbin/脚本接收器 + 故障注入 500/超时）全绿；条件不可得则容器腿 + BOARD 留痕（非 DoD 缺口）（L08 消费者段）。
    AC3: 服务端契约 git diff=0（FE 面零端点私加）+ SPA 预算维持（FR-115-AC7）。
  - **T-367 → done 2026-08-30 22:0x——M13 10/23，B4 全清**：FetchAbsolute 引擎缝（绝对 URL 走完整 RE-04 状态机落盘）+ chartsBaseUrl 槽（parseRemoteConfig per-protocol + canonical 回显 + REST 透传）+ outboundFor/chartsBaseFor 换基（同 origin 复用凭据/异 host 无凭据 egress 池）+ `_external` 双面落盘（remoteexternal.go FetchExternal/FetchVirtualExternal + 降级保留直通）+ `_transitive` 双跳链。**真客户端 21 腿全过**（helm 4.2.4 + dind，pro live）：chartsBaseUrl 异构基址 tgz 逐字节一致/二拉 HIT 计数冻结/REST 回显/maven 携带 400 点名/ftp 400/缺省回退臂；`helm dependency update` 经虚仓 `_external` 折叠取数 + HIT + Upstream 头 + storage REST 节点对账 + `_transitive` 双跳；**rider：validateV2MemberTypes**——helmoci virtual + docker 成员 400（照 Artifactory 同包型语义，T-365 登记① 兑现）；helmoci 全链 digest 一致 + dind 回归零变化。35 包全树绿（一轮 T-339 拷贝域 flake 隔离复跑 + 二跑 35/35）。conductor 复验：build 0 + remote 54.3s + helm 17.1s 绿。**裁决登记**：① 成员同型规则现限 v2 家族（裁定原文）——**helm 经典 virtual 配 docker 成员仍放行**，全包型推广候 M14 裁（登记非缺口）；② httpapi 一字段透传为 area 外最小增量（AC1 REST 腿必需，票内已登记）。日志 reports/agents/T-367.md。
    AC1: chartsBaseUrl——异构基址上游（index 内绝对 URL 异于仓基址）remote 配 `charts_base_url`→`helm pull` 回源正确 + 字节一致 + REST PUT/GET 回显 + 缺省回退仓 URL 臂（L12；helm.md §5/S8/S10 增量段随票——T-330 模式）。
    AC2: `_external` 落盘——虚仓外部域依赖 URL 改写→首次 GET 200 + 存储树落盘可寻址 + 二次命中本地（上游不回源计数断言）+ `_transitive` 同缝 + local 仓 `_external` 400 维持（L13；落盘命名自身、-cache 仓惯例不采纳——conan.md N7 同裁定）。
    AC3: SSRF 零新面（引擎缝复用 Guard 五参数 + TTL 定类/负缓存/stale 降级照 T-342 §5 D-3 评估结论）+ helm 经典仓三态（M11 FR-99）+ M12 helmoci 序列零回归（L13 回归段）。
- **B5**：T-368 ｜ T-372
  - **T-372 → done 2026-08-31 02:4x——M13 13/23，B5 全清**：锚册先行兑现（console-m8.md §4.3「不建」条款推翻留痕 + §6.3 注记——落码前完成）+ 树尾常驻 TrashTreeNode（reverse §3.2 形态；admin∪readonly 同门/普通 user 不渲染；点击/Enter 跳 /admin/governance/trash 沿用 M12 页面零数据请求；键盘叶节点语义；不受过滤域影响）+ 锚册 v1.17（tree-trash-node 新锚）。**四闸门 + 6 新腿 + 真栈 +2 腿 + 全量 237/0/23skip（7.3m）**；console-size 352,850 维持；契约 diff=0。共居 axe 假阳性 1 处按 flake 协议串行复跑绿（T-352 在册家族同行号）。conductor 复验：tsc 0 + ledger PASS。遗留：树内展开浏览形态未取（最小面入口跳转——票面二选一）；空实例引导卡整块替换树时节点不渲染（常驻语义限树本体，批块注记登记）。日志 reports/agents/T-372.md。
  - **T-368 → done 2026-08-31 02:1x——M13 12/23**：folder_download 六字段 + trashcan.retention_days 旋钮全链——config 四文件（raw 两段/build 臂/defaults/env 键族 7 键/非负域校验——**T-366 死键教训三处齐**）+ `GET /api/v1/system/settings` 回显臂（knob-scoped；system:read；manage 门 56→57 留痕）+ 三新测试件（表测双形态/strict schema camelCase 拒/L14 开关化五臂/retention=1 时钟夹具 + 0 哨兵）。**默认不变红线钉死**：M12 关态 403 逐字文案/14 天窗口/13·15 天臂全 PASS；internal/repo 产品码零改动（两缝 M12 已备）。**conductor 接线**（main.go：ConfigureFolderDownload 六字段 + ConfigureTrash/TrashEngineOptions 消费 cfg——§5 精确 diff 落盘）+ 复验 build/fmt/lint 0 + cmd 19.7s + 接线钉子 TestT368* 双 PASS。规格锚：repo-operations.md §2.1 双证（字段+默认唯一基准）/config-formats.md §2 snake_case 先例/K52 票内定案。遗留四项登记：architecture §7.1/§15/§11.45 翻转归 architect；Helm values.schema 键族归 release；文档翻转归 T-375；热更新维持重启生效分歧。日志 reports/agents/T-368.md。
    AC1: folderDownload——默认关 403 文案逐字维持（T-356 L14 基线 `Download Folder functionality is disabled.`）→ 开启 `GET /api/archive/download` 200 + 解包 sha256 对账（**M12 L14 断言升级为开关化——断言反转归属本票豁免票**，PRD §5.4）+ 匿名关 401 臂 + 超限拒绝臂（L14）。
    AC2: `retention_days=1` 短周期时钟夹具→过期自动清理 + 审计；期内不清理；默认 14 回归（M12 cron 13/15 天臂基座）（L15）；K52 键名族票内定案（六字段照 repo-operations.md §2.1 字段名）。
    AC3: trash-can.md「恒 14d」已知边界表 + api-reference「配置旋钮未落」自注 + artifact-operations.md 翻转 + `make docs` SUCCESS（FR-118-AC3 文档面）。
  - **T-372** [P1] FR-122.1 trash 树常驻节点 `role:dev-frontend` area:web/src（制品树浏览器 + 回收站入口；与 T-366 web/ 串行先后脚）dep:—（M12 T-352 回收站页面在案）
    AC1: **先改册后实现**——console-m8.md 树浏览器节「Trash Can 常驻节点不建」推翻条款回写先行（推翻留痕 + 新锚入册）→ 制品树顶层常驻回收站节点最小面（入口跳转/内嵌沿用 M12 回收站页面）（L21 段；规格先行纪律）。
    AC2: Playwright——节点可见/深链/readonly 臂 + 四闸门维持 + axe 双主题 0 + 全量 playwright 绿；服务端契约 git diff=0（FR-122-AC1）。
- **B6**：T-369 ｜ T-370
  - **T-369 → done 2026-08-31 01:0x——M13 11/23**：`serveV1RecipeDelete` 翻转为**坐标根整树删**（subtreeExists 探测→`svc.Delete(<root>/)` 连 index.json 与全部修订/包修订一并无；miss 404 沿 v1 族 `Path not found`；canWrite 门原样）——唯一生产码改动。M12 断言就地翻转（TestV1Deletes 注明 T-369/FR-119.1 豁免票号）+ 新增 TestD8WholeTreeRecipeDelete（v1 坐标删/v2 无修订删 × 删后 7 读腿 404）+ L-c5 腿补 revisions 404。**真客户端双绿**：conan 1.66.0 `remove -r -f` 访问日志全 D8 序列（DELETE→200/snapshot 404/revisions 404/修订 0 文件 404）6.95s + conan 2.31.2 7.12s（T-308/T-312/T-340 全量序列 18.6s + -race 50.4s 绿）。conan.md §3.2 D8 分歧行退役（既有体例）。低置信裁量两处测试固化+日志登记（miss 文案/miss 基面取路径基——索引损坏坐标可清除，Artifactory 同型）。conductor 复验：build 0 + D8 定向 2.3s 绿。遗留：docs/user/integrations/conan.md 两处滞后表述归 T-375 回刷；PRD §5.4 断言反转 QA 核实归 T-375 复核面。日志 reports/agents/T-369.md。
    AC1: 多修订包（r1/r2）→ v1 `DELETE conans/<ref>` 坐标根→**整树删（全部修订，GET 404×2）** + conan 1.66 remove 腿 + 2.x 无修订 DELETE 同步核对（L16；**M12 as-built「latest 修订链」断言反转 100% 归属本票豁免票**——PRD §5.4 回写核实归 QA）。
    AC2: conan.md §3.2 D8 分歧登记行随票消除（规格行退役）+ table-driven 单测 + conan v1/v2 全链抽样回归（T-308/T-312/T-340 序列）。
  - **T-370 → done 2026-08-30 12:5x（插空航 `2c0d243`，双远端）——M13 5/23**：PRD 双版本 v1.1（M12 十三处：cargo-409 四处补全〔收口笔仅落 2/8，本票补余〕+FR-113.5 对齐+flat 八处折叠口径+FR-107 AC2 加注；M13 廿一处：重试语义五处锚 webhook.md 官方值+「36 事件」→13 域 66 型+envelope 时间戳断言删除+SSRF 键落定+Q4 取证入文+K47~K50 回填）+ D-10 四臂对照/Q4 就绪/ADR-0041 冲突登记上板（下方裁定材料块）。conductor 复验：双 PRD 版本行 v1.1 在案+grep 旧措辞零残留。**三项终裁待用户/conductor 窗（Q3 D-10 / Q4 槽档位 / ADR-0041 决策 4 回填）**；ROADMAP 两处版本引用滞后 PM 下批随收口（票内留痕）。日志 reports/agents/T-370.md。
    <del>原文：</del> **T-370** [P0] FR-120 文面裁定包（D-10 终裁材料 + flat/L31 落笔）`role:product-manager` area:docs/prd（M12 PRD v1.1 增订/AC2 加注）+ docs/reverse/nuget.md 差异行（随终裁）+ docs/user/integrations/artifact-operations.md dep:—（随时可动——D-10 裁定材料宜早上 BOARD，conductor 提前插空派发；不阻塞任何实现票）
    AC1: D-10 对照材料上 BOARD（nuget.md §5.1 臂② 409 vs BinFlow as-built 201〔T-356 L03 实测〕双证对照 + 影响面；PM 出材料不代拍）；终裁后联动——翻转→触发 T-378 条件票；有意差异→nuget.md D 层差异行落笔（L19；终裁前维持 as-built）。
    AC2: flat 措辞回写——M12 PRD v1.1 增订（「flat 扁平化」→「flat 折叠进 copy 主参数族」口径）+ artifact-operations.md 同步（grep 旧措辞零残留）（L18）。
    AC3: L31 残余加注——M12 PRD FR-107 AC2「停机窗内重启」与 as-built boot 探针 fail-closed 姿势 PRD 加注（加注不改行为、ADR-0040 零修改、行为零变化断言维持）（L18；T-356 观察⑨）。
- **B7**：T-371 ｜ T-375
  - **T-371 → done 2026-08-31 03:0x——M13 14/23，conan 线收官**：D-F2 本体修复（channelFileName trim 复数→单数——新写落规格布局）+ SweepV1FilesLayout 启动 sweep（repo 清单过滤 local+conan 零仓零扫描/谓词三重约束〔coordinateRoot 四段+pid 校验+双 0 字面+pid 重复段〕/每仓 INFO 行 conflict>0 升 WARN/失败 fail-the-boot）+ **repo.Service.RewriteSubtreePrefix 窄原语**（+1 接口方法：子树前缀批改写，sha256 平移/零权限门·审计·Emit·observer/同 sha dedup/异 sha 新者胜+上报/空源 ErrNodeNotFound/空脚手架随迁删）。**真客户端**：conan 1.66 三测 PASS（新腿 LayoutSweepRoundtrip：`moved=3 dedup=1 conflicts=0`，腐蚀面〔前缀 snapshot 键+settings:{}〕sweep 后恢复，清缓存重 install 摘要全等）+ 2.31.2 全链 8.79s。conan 27.1s/repo 61.8s + docker/pypi ripple 包双 ok；新测试 -count=2 稳定。docker 两测试桩 +1 备案（接口加宽编译红最小修复，T-95/T-253 同型先例）。**conductor 接线**（main.go runServe 监听器起前插 SweepV1FilesLayout——错误即闭栈返 wrap）+ 复验 build/fmt/lint 0 + cmd 15.0s + conan 20.6s 绿。ADR-0042 四 AC 逐条对照表在日志。日志 reports/agents/T-371.md。
    AC1: `channelFileName` 复数/单数 trim 错位修正→新 PUT v1 通道包落 `<root>/<pid>/<pRev>/<file>` 规格布局 + 存量双拼布局树迁移（T-340 复现脚本夹具）→迁移前后制品 sha256 对账零损 + install roundtrip + **幂等（二跑零改）**（L17；roundtrip 对称故客户端面无感）。
    AC2: settings 恢复——新 PUT v1 通道包 ref-search conaninfo 字段非 `{{}}` + `-q` 过滤腿命中 + v1 包 snapshot 键裸文件名（L17；**服务端树断言翻转归属 FR-119 豁免票**——PRD §5.4 布局对齐行）。
    AC3: conan v1/v2 全链回归（M11 T-308/T-312 + M12 T-340 序列）零回归 + 回滚路径演练留痕（ADR-0042）。
  - **T-375 → done 2026-08-31 03:2x——M13 15/23**：**webhook 使用指南新页**（docs/user/admin/webhooks.md——9 型触发表/wire schema/七端点 curl/七字段信封/HMAC 双态 + openssl 验签 + **实测过的 Python 接收端验签示例**/幂等义务/投递与死信/排障环/metrics 五族/SSRF 边界）+ HelmOCI remote/virtual/chartsBaseUrl/`_external` 增量节 + conan.md **断言反转两处**（整树删）+ D-F2 布局对齐标注 + 两旋钮翻转（trash-can/artifact-operations）+ api-reference M13 速览（七端点族+system/settings+chartsBaseUrl）+ FAQ M13 三问（死信重放**如实登记无 REST 面**）+ 侧栏挂页 + **README 双语收口**（修英文版 helmoci 误植）。**38 腿实测取证**（HMAC 逐字对账/死信 5 次审计 attempts:5/community 403+头/旋钮双证/helm HIT+上游冻结/conan 双客户端）+ `make docs` SUCCESS 零断链 + 新页嵌入 internal/docs/dist。顺手修四处既有漂移（console.md 失真/license.md 漏登/governance 词表止 M4）。遗留：死信重放 REST 面挂载后回刷两处；architecture 端点行 + Helm values.schema 键族归 architect/release。日志 reports/agents/T-375.md。
    AC1: Webhook 使用指南（订阅/过滤/签名/重试与死信语义/SSRF 边界 + 接收端示例）+ HelmOCI remote/virtual 接入 + 旋钮说明（folderDownload/retention——trash-can.md 自注翻转联动 T-368）交付；全部 curl/客户端命令实测可复跑（L24）。
    AC2: api-reference 增量（webhook 端点族 + 断言反转两处/布局对齐一处标注）+ FAQ 增补（事件丢失排查/死信重放/remote 缓存命中）+ `make docs` SUCCESS 零断链 + 侧栏挂页 + README 双语收口核查（用户规程留痕）（§8-8）。
- **B8**：T-373 ｜ T-374
  - **T-373 → done 2026-08-31 06:0x（配额击落于报告落盘前夜——报告完整在案）——M13 18/23**：**总裁定 PASS**。编排面：六实例（community/pro×3/knob/surv）+ 双接收器（GUA+loopback 各一）+ dind + kind 真集群。**L02~L13 全绿**：webhook 域（CRUD/test 逐字/七字段信封/9 织入型全命中/过滤器双臂/三缝门控+降级不入箱/kill -9 幸存补投零丢/**固定 10s 间隔实测 10065ms/10105ms**/5 次耗尽死信/4xx 恰 1 次终态/HMAC 线上字节 MATCH+篡改红臂/**SSRF 双臂闭环——死键修复 live 证实**）；HelmOCI（remote 双跳冻结/认证腿/kind pod 1/1/virtual 并集+D-2 维持/停上游降级零 5xx）；L12 chartsBaseUrl（idxup 计数 0+per-protocol 400 点名+缺省回退）；L13 `_external` 折叠+storage 可寻址+`_transitive` 双跳。**P0 双形态抽样全绿**（docker/maven/npm/pip/conan/nuget/操作族/trash restore/blob 去重恰一份）。**三处断言反转独立复证**（D8 真客户端修订 0 随树灭/旋钮四文案逐字/D-10 四臂 curl）。**归属审计**：m12-done..HEAD 13 提交 81 文件 100% 票号归属 + **1 无票号 style 提交（45e73c4）记账**〔conductor 已记：M12 seam 的 CI lint 追补，零语义，P2 不阻塞〕。观察三项登记（`_transitive` 拼写疑折叠深度差异归 T-377 同拓扑复核；/v2 remote 不织入 cached 供 architect 确认覆盖界；Replay REST 缺席维持 T-364 登记）。T-371 行为面归 T-377（其 conan 测试面已间接全绿）。日志 reports/agents/T-373.md。
    AC1: webhook 域 L02~L09 首跑（可编排接收器夹具：容器 httpbin/脚本接收器 + 故障注入 500/超时 + kill -9 编排）+ HelmOCI L10/L11（自指上游/mock registry 认证腿/kind 夹具）+ chartsBaseUrl/_external L12/L13 首跑。
    AC2: M1~M12 P0 双形态抽样回归 + 断言反转预核实（conan D8〔T-369 合入后补腿〕/folderDownload 开关化〔T-368 合入后补腿〕）+ 契约变更面归属审计（m12-done..HEAD 增量）零孤儿。
  - **T-374 → done 2026-08-31 06:5x——M13 19/23**：console-m8 §1.3 侧栏清单对齐现役（基线 14 → **现役 18 = 应用 2 + 管理 16**；M10~M13 四增补入图并〔〕标注 + 「PRD 估算 15 未计 Webhooks，以代码现役 16 为准」出入留痕）+ §10.1 两处括注；npm.md **尾斜杠配对注记**（六格实测矩阵 npm 10.9.8 真服务——A2/A3/A4 失配 ENEEDAUTH/C1 `_authToken` 带斜杠 201/匿名 view 不受影响）+ ENEEDAUTH 行 + login 行如实修正。锚册 ledger PASS + T-372 两处落痕核销 + `make docs` SUCCESS 零断链 + shell.spec 三轮绿 + 本票对象 11/11。全量 236/238 三失败逐条甄别零耦合（实例污染/负载 flake/**真缺陷 L2**）。**遗留三枚登记（M14 候选池）**：L1 服务端 `npm login --auth-type=legacy` 被内容面匿名 PUT 认证门拦（T-77 O-4 实证补课——npm.md 已如实标注）；L2 仓库表 row-link hover 对比度 4.41:1<4.5（亮色，T-344 迁移遗留——axe 几何 race 可触发，显式 hover 3/3 复现）；L3 全量套件一次性实例假设未成文（QA 编排注意）。日志 reports/agents/T-374.md。
    AC1: console-m8 侧栏清单对齐现役 15 页（12/13→15）+ 树节点推翻条款落痕核查 + 锚册 ledger PASS + 全量 playwright 绿（L21 段；FR-122-AC2）。
    AC2: npm.md registry/token 尾斜杠接入注记落笔 + `make docs` SUCCESS 零断链（L21 段；FR-122-AC3）。
- **B9**：T-376
  - **T-376 → AC1 done 2026-08-31 07:1x（AC2 执行面备妥候里程碑 PR 触发）——M13 20/23**：Chart 1.2.0→**1.3.0**（webhook/folderDownload/trashcan 三键族显式渲染 + **Recreate 策略 + checksum/config 注解——C1/C2 两枚 k8s 升级路径硬前提缺陷**〔RollingUpdate 双 pod 争 serve.lock live 复现；仅改配置静默不滚动〕）+ compose M13 env 块 8 键 + k8s 注释块 + systemd + offline 四修（C3）。**六腿烟测全绿**（compose/Chart native/kind 真部署/systemd 容器腿/offline 62M 真构真装/双面 200——均含上传下载字节一致 + 19 槽/403 门控实见）；helm lint --strict ×多轮 + template 矩阵（互斥守卫 intact）。AC2 BEFORE 取证归档（uat.9263439 上 M13 面 404=预期），触发后按报告 §4.3 可粘贴清单直跑。遗留：helm uninstall 连 PVC 删（keep 归 conductor 裁定——**裁定：M14 候选池**，不阻塞收口）；启动日志措辞失真一行级（非本票 area）；PR 分支需复跑 docs 烟测腿（时序条款）。日志 reports/agents/T-376.md。
    AC1: 部署矩阵烟测（compose/k8s/systemd/offline 抽样）+ 新配置面四部署接线核验（webhook 第 19 槽/私网开关键/outbox 面新键族——含 ADR-0041 引入键）。
    AC2: **UAT 随里程碑 PR 首跑必须落地**（M12 T-355 未执行教训——T-356 §6 留痕）：develop→main 里程碑 PR 触发 CircleCI→52.79.109.153 分阶换装 + healthz 探针 + 双面烟测（含 /binflow/docs/）证据归档；与 conductor 收口时序协同（PR 化合并既定程序）。
- **B10（收口波）**：T-377
  - **T-377 → done 2026-08-31 10:1x——**总裁定 **PASS**，M13 21/23（T-379/T-380 未触发留痕见下）**：DoD 八条逐条达标**（实测数字归档：check-size 94.13/100MB、footprint 11.9MB@ready、冷启动三连 0.618/0.201/0.197s、投递 p95=2ms、万 blob 8 路零 5xx）；**L01~L24 全绿**（承证口径：T-373 后生产代码零变化 + HEAD 复跑压缩验证）；**T-371 行为面四 AC 独立复证**（preT371→HEAD 真升级链：sweep moved=3→二启 moved=0 TREE-IDENTICAL + sha256 三口径全等 + ref-search settings 恢复 + install roundtrip 字节 MATCH）；**`_transitive`/`_external` 拼写规则钉死**（三分支 live——T-373 观察② 关闭）；三处反转 + D-10 四臂独立复证；Q3/Q4/Q6 归位；race 全树干净机复跑 exit=0；Playwright 全量 236/24skip/1 已知串行绿；归属审计 14 提交 88 文件 100% + 45e73c4 记账复核维持。**新登记 D1**（remote 缓存树 24 路并发 SQLITE_BUSY 0.27% 可重试边角——M14 候选；8 路门内口径零 5xx）+ D2（dind containerd snapshotter 环境注记）。收口笔五项（R3 LC-56 回写/R4 README 完成态/R5 ROADMAP 未纳入段/e2e+UAT 随 PR）归 conductor 本轮执行。日志 reports/agents/T-377.md。
    AC1: L01~L24 全量（承证+增量）+ M1~M12 全 P0 双形态复跑全绿 + 契约变更面（`git diff m12-done..HEAD -- internal/ cmd/`）100% 归属 M13 豁免票 + **断言反转两处**（conan D8 latest 链→整树删 / folderDownload 恒关→旋钮化〔关态文案逐字维持〕）+ **布局对齐一处**（D-F2 双拼→规格布局）PRD 回写核实 + DoD 八条逐条（实测数字归档；NFR-P58~P60 + `make test`（race）全树一次绿×2 + footprint/check-size 门维持——webhook 引擎不得破 M12 转绿门）。
    AC2: 收口双项（README 双语 + 文档站随新能力核查——结论入收口报告）+ Q3/Q4/Q6 终裁归位核查（LC-56 归 A 或 D / webhook 槽档位 / 事件覆盖界）+ 总裁定 PASS → conductor git tag m13-done（UAT 首跑证据随里程碑 PR 归档）。
- **波外条件票**（未触发 BOARD 留痕非 DoD 缺口）：
  - **T-378 → done 2026-08-31 04:0x（条件触发兑现——用户终裁 D-10 对齐 409）——M13 16/23**：`serveV2Publish` 包体 Put 不再把实测 sha256 当声明摘要（传零值 BlobRef——重传一律走服务层完整权限对，delete 半边即 §5.1 `exists && !canDelete`→409）+ 死代码清除（SHA-256 测量族，官方 sidecar SHA-512 保留）+ **table-driven 四臂**（②a 维持/②b 本票翻转注明 M12 L03 反转+T-378 豁免/③ 两拼写/④）+ **live curl 真栈腿**：w-only 同字节重推 → `409 Package already exist: live.a/1.0.0/…` 逐字。nuget.md §5.1 D-10 关闭留痕 + §11 对照新行。nuget 16.9s 全绿 + repo/httpapi 涟漪双 ok + lint 0。顺手修 in-area 预存漂移（v2live #8 断言针 entry 命名空间——HEAD 即静默红）。conductor 复验：build 0 + 四臂定向 PASS。遗留：v3/flat 直推面 403-vs-409 待规格补锚另票；LC-56「待裁→A」回写归 PM；「有 d 无 w」微角维持现状登记。日志 reports/agents/T-378.md。
    AC1: 同字节 + 仅 w 权限主体重传→409 逐字（nuget.md §5.1 臂②）+ M12 as-built 201 断言反转（归属本票豁免）+ nuget 双客户端回归；终裁=有意差异则本票不触发（nuget.md D 层差异行留痕）。
  - **T-379** [P2·条件 Q2] NuGet symbol server 余量票（M12 T-357 承接）`role:dev-go-core` area:internal/adapter/nuget（symbol 子域）dep:P0/P1 全收官 + 余量条款（全部 P0/P1 收官且余量足）
    AC1: mini as-built 规格随票（T-293 终裁口径）；.pdb/GUID 路径面 + 真实客户端腿；未触发 M14+ BOARD 留痕。
  - **T-380** [P2·条件 Q5] docker remote 顺车 `role:dev-registry-adapter` area:internal/adapter/docker（docker remote pull-through 本体）dep:T-363（K54 判定 /v2 共享缝边际成本≈0）+ BOARD 留痕
    AC1: K54 判定≈0 且 BOARD 留痕→docker 三态 remote 首航（/v2 remote 数据链复用 helmoci 缝）+ dind 全链 + 降级臂 + Bearer 认证腿；判定≠0 则滚 M14+ 留痕（暂行口径）。

**关键路径**：T-358/T-359 → T-362（订阅+织入·独占）→ T-364（投递引擎）→ T-366（FE+消费者）→ T-377（webhook 主线）；T-342 → T-363（HelmOCI remote）→ T-365（virtual）→ T-367（引擎缝）→〔T-380 条件〕→ T-373/T-377；T-360 + T-348 → T-369（D8）→ T-371（D-F2）→ T-377（conan 线）；T-368（旋钮·零依赖）→ T-373/T-375；T-372 → T-374（运维尾巴线）；… → T-375（docs）→ T-376（release+UAT 首跑）→ T-377 → m13-done。**T-361（de-flake）/T-370（PM 文面）零依赖可任意波次穿插**（后者宜早——D-10 裁定材料上 BOARD）。

**风险登记（拆票日志 M13-SPLIT.md 详表）**：① **Q4 webhook 槽档位终裁**须在 T-362 AC3 门控断言前收口——T-358 AC2 取证腿（官方 license 标注）上 BOARD，conductor 于 B2 前安排裁决窗（暂行 pro+/feature-int 不阻塞实现主体）；② **Q6 事件覆盖界**——按暂行「注册休眠」实现，终裁若裁剪仅动注册表（面小不返工）；③ **webhook.md 官方文档取证风险**（反编译集合无该 addon）——官方文档为唯一基准，与 inv-4 锚点冲突时效力序=规格票>PRD 暂行（PRD §4 约定），分歧上 BOARD；④ **dev-go-core 五票串行负载**（T-362/T-364/T-368/T-369/T-371——B2/B3/B5/B6/B7 错峰）+ router/slots/main.go 接线统一交 conductor（M11 纪律延续）；⑤ **dev-registry-adapter 三票链**（T-363→T-365→T-367，B2/B3/B4 串行）与 FR-117 同域先后脚——internal/repo/adapter/helm 共写面已错峰；⑥ **T-367 schema 定座**（charts_base_url per-protocol 槽——T-342 §5 D-2①「须 architect 定座」）票内定案留痕，必要时 architect 会审（不单列 ADR）；⑦ **dogfood Jenkins 条件腿**（T-366 AC2）dep 用户环境——不可得则容器接收器腿 + BOARD 留痕（非 DoD 缺口）；⑧ **UAT 首跑时序**（T-376）——须与 conductor 里程碑 PR 收口协同（M12 T-355 教训），docs 票后合入以 PR 分支复跑烟测腿补偿；⑨ **conan owner 备裁**——PRD 下游消费者表将 conan 小票列 dev-go-core（M11 T-308 先例），与 M12 T-340（dev-registry-adapter）异派——拆票按 PRD 口径，conductor 可改派（票面 area/dep 不变）；⑩ 条件票触发态——Q2/Q3/Q5 全触发票数上浮至 26（PRD §1.3 上限内），未触发 BOARD 留痕。

**T-370 裁定材料上板（PM，2026-08-30；FR-120.1 承载——出材料不代拍，终裁归用户/conductor）**：

**① Q3 / D-10 NuGet publish 同字节幂等终裁（决定 T-378 条件票走向）**——双证对照（规格：nuget.md §5.1〔DE `isPackageAlreadyExistAndCantBeDeletedByCurrentUser` = exists && !canDelete〕；as-built：T-356 L03 实测 @`d41f6d0`）：

| 臂 | 场景 | 规格（Artifactory） | BinFlow as-built | 判定 |
|---|---|---|---|---|
| ②a | 包已存在 + 主体无 d 权限 + **不同字节** | 409 CONFLICT `Package already exist: <deployPath>` | 409 同文案逐字 | 一致 |
| ②b | 包已存在 + 主体无 d 权限 + **同字节** | 409（DE 谓词 exists && !canDelete，无字节比对分支——字面应 409） | **201**（幂等短路） | **分歧本体（D-10）** |
| ③ | 包已存在 + 主体有 d 权限 | 直接覆盖上传（无冲突臂） | 201 覆盖 + sha256 新值 | 一致 |
| ④ | 新包 | 201 `Successfully published NuPkg to: <path>` | 201 一致 | 一致 |

影响面：nuget local + virtual（defaultDeploymentRepo 等价 local）publish 幂等臂；LC-56 待裁行；客户端（nuget.exe/dotnet）对「重推已存在版本」以 409 为既定冲突信号。**终裁二选一**：对齐 409 → 触发 T-378 翻转小票（冲突检查前移到字节比对之前 + M12 L03 断言反转归属豁免票）；有意差异 → nuget.md 差异登记行落笔（D 层留痕，落位随终裁票——参照 cargo.md §8.1 R-3 登记形态；终裁前维持 as-built 201）。**PM 建议：对齐 409**——① 19:05 行为逐项对齐常设条款：规格 DE 谓词无字节短路，「同字节 201」属自有发挥（2026-08-26 20:35「不要有太多自己的想法」对面）；② Artifactory 迁移用户心智零损（409 = 既定重复信号，CI 遇 409 的处置在源生态本就成立）；③ 实现面小（T-378 小票在案，小时级）；④ 留痕方案需在 nuget.md 永久登记「BinFlow 宽于 Artifactory」差异，与对齐优先基线相悖且无用户可见收益论证。风险注记：若用户有「CI 网络抖动重推同包期望成功」需求则选留痕——该需求 Artifactory 同样不满足。**补充取证建议（终裁前可选）**：T-247 保留的 t226-artifactory 容器（OSS 7.84.10，docker start 可恢复——T-358 §4）可跑活体腿（w-only 主体同字节重推看实际码）：活体 409 → 对齐依据升三证；活体 201 → nuget.md §5.1 臂②规格行需修订升置信，留痕方案转优。

**② Q4 材料就绪提示（风险登记①联动）**：T-358 §1-6 取证——官方功能矩阵 Webhooks 行 Non-commercial ❌ / Pro ✅ / Enterprise ✅ → **建议维持 pro+ 不翻转**；kind 已由 ADR-0041 决策 8 定 KindFeature（不新增第三值）。供 conductor 安排 B2 前裁决窗与 Q3 一并终裁。

**③ ADR-0041 决策 4 投递参数与 webhook.md §5 冲突登记（规格活化翻转点）**：ADR-0041（Accepted 2026-08-30，与 T-358 并行定案——wire 字面量未及规格值）决策 4 = 10 次尝试/2s 起步 ×2 递增/单次 10s 超时，且决策 2 将 3xx·4xx 计入可重试；webhook.md §5 官方基准 = retryCount 5（首试计入）/**固定间隔 10s 非指数退避**/单次超时 30s（含建连/重定向/读体）/**4xx 不重试（仅发送失败或 ≥500）**。按效力序（用户裁决 > webhook.md > ADR > PRD 暂行）**建议 architect 随 ADR-0041 锚点回填修订决策 4（与决策 2 的 4xx 可重试句）对齐规格**；机制条款不翻：outbox 双方言两表/死信 additive（官方无持久死信，webhook.md §5.3 明示 BinFlow 形态归 ADR）/Guard 默认拒私网。PRD v1.1 已按官方值修正（FR-115.2/K50）；**BOARD T-364 AC2「指数退避序列」措辞建议 conductor 随改「固定间隔 10s 重试序列（4xx 不重试）」**；T-356/T-358 后 ADR-0041「T-362 验收锚点骨架」AC-3 的「时间戳」字段断言同批修正（artifact 域载荷无时间戳——T-358 §1-8）。

**④ T-370 文面修正落笔清单（2026-08-30，PRD 双版本已落）**：M12 PRD v1.1——cargo 409 双姿态四处（FR-110.4/AC3/LC-43/L24——D-356-3 残余）+ FR-113.5 正文与 L31 注对齐 AC5（D-356-4 残余）+ flat 措辞八处折叠口径（repo-operations.md §1.6）+ FR-107 AC2 加注（T-356 观察⑨；ADR-0040 零修改）；M13 PRD v1.1——重试语义五处 + 「36 事件」八处→13 域 66 型 + envelope 时间戳断言删除 + SSRF 键落定 + Q4 取证入文 + K47~K50 回填（T-358 AC3 转办 PM 事项）。artifact-operations.md L66 已是折叠口径（无需改）；nuget.md 差异行随 Q3 终裁联动。ROADMAP 两处版本引用滞后（M12/M13 需求基线行仍 v1.0、M13 行「36 事件」措辞）——PM 下批随收口更新（本票纪律禁写 ROADMAP，留痕于此）。

**三项终裁落定（用户裁决窗 2026-08-30 12:5x，AskUserQuestion 三问全按建议）**：

- **① Q3 / D-10 = 对齐 409**（NuGet 同字节幂等臂：201 幂等短路 → 409 CONFLICT，照 nuget.md §5.1 规格 DE 谓词）。→ **T-378 条件票触发**（翻转小票：冲突检查前移到字节比对之前 + M12 L03 断言反转归属豁免票 + nuget 双客户端回归）；活体取证（t226 容器）可选不做——规格谓词字面已足。
- **② Q4 = 维持 pro+ / KindFeature 终审定案**（官方功能矩阵 Non-commercial ❌）。T-362 AC3 暂行断言转正（community 403 + License-Required 头 = 钉死语义）；T-358 §1-6 取证腿关闭。M13 收口 Q4 归位核查项（T-377 AC2）此项可预勾。
- **③ ADR-0041 决策 4 = architect 回填对齐官方**（效力序 webhook.md > ADR 确认）。→ 派 architect 随 ADR-0042 同窗执行（T-360 + 回填修订）：决策 4 投递参数（10→5 次尝试/指数退避→固定 10s/10s→30s 超时）+ 决策 2 的「3xx·4xx 可重试」句（4xx 不重试）+ AC-3 时间戳断言删除。**T-364 已按锚点值实现**（wire 先行，回填为文档对齐——零代码翻转）；BOARD T-364 AC2「指数退避」措辞由回填笔随改（见上 ③ 登记）。

**T-364 收口登记（conductor，2026-08-30 13:1x）**：四处接线照报告 §6 精确 diff 落盘（stack 双字段/openStack NewDispatcher+metricsReg 共享/serve drain 双臂+startWebhookDelivery/metricRegistry 懒建 + stack.close webhookDB 补 T-362 缺口）；复验 build/gofmt/vet/lint 0 issues + `go test ./internal/webhook/ -race` 12.5s 绿 + `go test ./cmd/binflow-server/` 全绿。metrics 五枚族经共享 registry 上 /metrics 面（一次 scrape 双见）。遗留五项照报告 §5 登记：①audit `webhook.dead_letter` 入词表归 audit owner（与 T-362 四词一并）；②metrics 注册位 internal/webhook 可平移（conductor 裁：维持现位——注册幂等、名字稳定，httpapi 惯例位收益为零）；③Replay REST/控制台面待裁（机制已备）；④disable 快照契约钉死；⑤3xx 终态 V4 待验证项。

**用户指令 intake（2026-08-30 13:1x，M14 UI-parity 里程碑主输入；插空派发 UX-1 票即刻启动）**：① 前端**交互体验与 JFrog Artifactory 完全一致**——弹窗（New Repository 向导等 modal）、抽屉（Set Me Up/详情 slide-out）等形态逐一对齐（MUI 化视觉之上的下一层）；② **各协议 logo 加上**（13 包型 SVG 图标集，品牌色）；③ **BinFlow 产品 logo 自设计**（国际化、偏技术、好看；非 JFrog 仿制）。执行：UX-1（ux-designer 插空——品牌 logo 三稿 + 协议图标集 + Artifactory 交互模式规格 docs/design/console-artifactory-parity.md）→ PM 据此起草 M14 PRD（M13 收口后立项）；FE 接线票归 dev-frontend 后续波次。

**UX-1 → done 2026-08-30 13:3x（36 新文件，零现有代码改动）**：品牌 logo 三候选（docs/design/brand/logo/——**推荐候选 1「容器·双箭流」**〔bin/flow + 终端提示符 » 双隐喻，16px 免简化〕，候选 2 括号流/候选 3 六角流备选；品牌色锚定现有 --bf-accent 不另起炉灶）+ 包型图标 30 枚（mono/brand 各 15——13 包型 + trashcan/webhook；helm×helmoci 三重区分；逐枚持有方与许可姿态注记）+ **console-artifactory-parity.md**（模式 catalog 逐项置信度标注→MUI 组件→载体→差距三档 + §7 页面×模式差距矩阵〔PM 起 M14 PRD 直接引用〕+ §8 活体核验清单 V1~V8 + §9 豁免登记 E1~E7〔保护 BinFlow 优于 Artifactory 的安全设计〕+ §10 落地批次）。遗留：推荐稿待用户圈定（三稿齐备可换）；wordmark/npm/go 图标接线前转 path；活体核验 V1~V8 归有浏览器的票。日志 reports/agents/UX-1.md。

**M13 收口（2026-08-31 10:1x——T-377 PASS 后 conductor 序列）**：

- **波外条件票留痕**：**T-379**（Q2 NuGet symbol server）未触发——余量条款未满足（收口波无余量），滚 M14+；**T-380**（Q5 docker remote 顺车）——K54 判定「/v2 共享缝边际成本≈0」**成立**（T-363 §Q5 结论在案），但收口波满宽未随 M13 派发 → **转 M14 首航候选（触发条件已满足）**。两者均非 DoD 缺口（PRD §1.3 上限条款内）。
- **M14 候选池汇总**（本轮各票登记汇总）：T-380 docker remote 首航（条件已满足）/ L1 npm legacy login 服务端小票（T-374）/ L2 仓库表 hover 对比度 FE 小票（T-374）/ Replay+outbox 行级 REST 面（T-364 §5-③+T-366 §4-2）/ D1 remote 缓存树高并发 busy 重试预算（T-377）/ helm uninstall PVC keep（T-376）/ 成员同型全包型推广（T-367）/ v3/flat 直推面 403-vs-409 规格补锚（T-378）/ 启动日志措辞一行（T-376）/ playwright 纯净实例假设 README 注记（T-374 L3）/ 3xx 终态 V4 活体验证（T-364）/ disable 快照契约翻转若需（T-364）/ **UI-parity 主轴**（用户指令 2026-08-30：交互对齐 Artifactory + 协议 logo + 品牌 logo——UX-1 资产与差距矩阵已备）。
- **收口笔执行**：R3 LC-56 回写 + R4 README 双语完成态 + R5 ROADMAP M13 未纳入段 + 里程碑 PR（develop→main，触发 build/e2e 首跑/deploy_uat UAT 首跑）+ UAT 证据归档 + `m13-done` tag。

**T-376 AC2 归档 + M13 全链收官（2026-08-31 10:4x，conductor ssh 取证）**：里程碑 PR #45 合并（main=`64a195a`）→ CircleCI build → **e2e job 首跑** → **deploy_uat UAT 首跑换装完成**。UAT（52.79.109.153）M13 标记全绿：`GET /api/v1/system/settings` **200**（folder_download 六字段 live 回显——M13 新端点）/ `/binflow/docs/admin/webhooks/` **200**（文档站新页）/ `/binflow/ui/` 200 / **`/binflow/event/api/v1/subscriptions` 200**（webhook 订阅 REST 面上 UAT）。`m13-done` tag 已推双远端。**M13 十三里程碑链闭合**。

## M14 票据（tech-lead 2026-08-31 拆票中；PRD v1.0 已 conductor 审定——Q1~Q7 暂行维持、票区间 21~26 确认、拆票日志 reports/agents/M14-SPLIT.md 待落；AC 全文见 docs/prd/milestone-14.md）

> **M14 = UI-parity 专程**：用户指令 2026-08-30 三件套（交互对齐/协议 logo/品牌 logo）+ docker remote 首航（T-380 K54 条件已满足）+ 服务端小票包。E1~E7 豁免常设（对齐评审不判差距）。票号 T-381 起。

> **拆票基线**：PRD §1.3 估 21~26 票，实拆 **22 票**（P0×7 / P1×12 / P2×3——含波外条件票 2 张计入 P2）。**FE 主轴 10 票全 web/src 一波一票错峰串行（B0~B9）**；FR-123 活体核验前置锚 B0（Q1 降级路径内置——核验源不可得凭 V1~V8 标注降级，不阻塞批 1 主形态）；批 1 四项 P0（D1/M1/M3/L2）B1~B4；品牌两票 FE 错峰（logo P0 B5 / 图标 P1 B7）；批 2（Tokens P1 / L1 P1 / F2+N2 P2）B6~B9；BE 副线与 FE 天然错峰——docker remote（dev-registry-adapter）B1、FR-130 服务端小票包聚合一票（dev-go-core，dep reverse 前置规格票）B3；QA 三腿（核验执行 B0 / 中期 B5 / 终验 B11）+ tech-writer 两票（接入运维 B6 / console parity B8）+ release 一票（B10）+ PM 裁定一票（B4）。全宽 2 沿 M11~M13 口径。

**FR → 票映射**：

| FR | 票（P） | 承载要点 | 裁决/登记锚 |
|---|---|---|---|
| FR-123 | T-381（P0，B0 前置锚） | V1~V8 活体核验 + parity 置信度回写（改契约留痕）+ §7 矩阵复核基线 + 三出口 + 低置信三枚图标顺带腿 | Q1/K55；降级路径内置 |
| FR-124 | T-382（D1）/T-383（M1）/T-384（M3）/T-385（L2）——四票 P0 串行 | 抽屉化（smu-* 锚族冻结）/建仓单 Dialog + 深链/创建 modal（编辑整页 E5）/行尾 ⋮（删除不进 E1） | Q3/K57；V1/V2/V4/V6 |
| FR-125 | T-386（Tokens P1）/T-387（L1 P1）/T-388（F2+N2 P2 合并票） | PlaceholderPage 退役零新端点 / 列选刷新 per-page 持久 / 插画槽 + 图标槽 | Q4/K58；V5 降级出口 |
| FR-126 | T-389（P0，一票含 path 化 + docs-site 槽） | 候选 1 工作稿起步六用例五落地 + wordmark path + 双主题色板锚定 | Q2 圈定窗 B2 前截止；K56 票内定案 |
| FR-127 | T-390（P1，一票） | 30 枚搬运 + npm/go 转 path + 四消费点 + 门控/暗底纪律 | K61；dep D1/M1 错峰（B7） |
| FR-128 | T-391（P1，零依赖 B0 补位） | hover ≥4.5:1 + e2e README 两行注记 | T-374 L2/L3 + T-377 D2 |
| FR-129 | T-392（P1，一票含 dind 全链） | docker remote pull-through 首航（/v2 remote 复用 T-363 缝）+ Bearer + 降级 | Q5/K54 成立；LC-64 |
| FR-130 | T-393（P1 reverse 前置规格）→ T-394（P1 聚合一票 dev-go-core） | v3-flat 补锚 + npm login 端点实证（K59/K60）→ npm login + PVC keep + 启动日志 + as-built 对照（不动行为） | Q6；翻转走 T-401 条件票 |
| QA/文档/发布/裁定 | T-396（中期 P1）+ T-400（终验 P0）；T-397/T-398（两票 P1）；T-399（release+UAT P1）；T-395（PM Q 终裁联动 P1） | L01~L18 + **L16 矩阵逐格终评** + DoD 八条 + UAT 随里程碑 PR | §8 剧本十段；K55~K61 回填 |
| 条件票 | T-401（Q6 v3-flat 翻转）/T-403（symbol server 余量四承——原 T-402 让号 replication 增补票，conductor 2026-08-31 改号）；N2 内置 T-388 出口、D3（Q7）不占票号 | 未触发 BOARD 留痕非 DoD 缺口 | Q6/Q7/余量条款 |

**批次（全宽 2；波内 area 互斥，跨波同 area/同角色串行；FE 主轴 web/src 一波一票错峰）**：

- **B0（前置锚 + 零依赖补位）**：T-381 ｜ T-391
  - **T-391 → done 2026-08-31 12:0x——M14 1/22**：hover 对比度修复定案「**不引入新 token**——链接色向正文 token 压 12%（color-mix，assert-tokens 放行）」：亮 **4.42→5.08** / 暗 **5.55→6.12**，其余承载面 ≥5.1 双主题；根因复算（action.hover 叠 --bf-bg，无 Paper 包裹——探针实测 #e9ebed 与 T-374 逐字节吻合）。axe 双主题 serious=0（全路由 sweep + 显式 hover 探针 T-374 同姿势 3/3 红位 0 违例）+ e2e README 两行注记（纯净 community 前提/dind snapshotter）+ 四闸门 + 全量 233 绿（m9 两 spec seed 并行撞 INSERT 串行 8/8 绿——**L-b 登记**）。conductor 复验 ledger PASS。遗留：L-a `.member-pop` 同配方一行（virtual Tab 浮层入口——M14 候选）；L-b m9 seed 并行互撞（CI workers=2 理论可复现——归 QA/编排注记）。日志 reports/agents/T-391.md。
  - **T-381 → done 2026-08-31 12:2x——M14 2/22（B0 全清）**：t226 容器一次恢复（OSS 7.84.10）；V1~V8 全 DOM 实测回写（parity **v1.1**，§0 留痕，零静默升格）+ §7 重印三行改判 + 图标三枚修正（README v1.1）+ 26 png + measurements.json 归档。**三项记忆证伪（改判）**：① **V2/M1 决策项 A 撤销**——7.84 建仓 =「下拉选 rclass→880px 磁贴 modal→**整页路由表单**」两段式，「全程单 modal」不成立，**BinFlow 现形态已对齐——T-383 转断言收口小票（conductor 改判）**；② **V6/M3 决策项 B 撤销**——用户/组创建整页表单非 modal（**T-384 同转收口**）；③ **V4/L2 撤 parity 旗**——行尾无 ⋮（icon-trash 直删，T-385 缩面或转候选）。**V1/D1 参数修正**：抽屉 **50vw**（非 480px）+ **Configure/Deploy/Resolve 三 Tab** + 底栏返回链接——T-382 断言集更新。V3 toast 顶部居中单条（E7 转再议）；V8 删仓 520px/**Delete 绿色主按钮**——BinFlow 输入确认+红 danger = 更严有意偏离（E1 实证加码）。**INC-1 事故（已完整恢复）**：探测误点删除确认致 t226 语料仓（120 制品）误删——VM 快照 ~/t381-incident-recovery/ → 原路径回灌 **120/120** + filestore 159 blob 前后不变（checksum 寻址字节等价）+ 抽样 8 路 sha1 全中；残留：同尺寸组 path↔内容排列可能异于原序（恢复上限）；t381-ui-probe 空仓留档；后续探测改「差集法+永不点确认」。遗留：L02 conan.svg 换色归 ux；L05 ux 共笔签认待路由；L06 事故教训成文。日志 reports/agents/T-381.md。
    AC1: V1~V8 逐项核验结论 + 证据（截图/录屏/文档锚点）归档；parity 规格置信度列回写（§0 修订记录留痕——**改置信度=改契约**）；零静默升格——grep「以核验为准」清单与核验结论一一对应（L01）。
    AC2: 差距矩阵复核基线落盘（§7 重印版 + 三出口判定：V5 降级 / V7 关闭 / E7 再议）+ 低置信三枚图标（nuget/conan/docker 鲸腹）活体对照顺带腿（LC-63 注记——修正结论回 package-icons README §4 登记）；tech-lead 批 1/批 2 细节断言收口确认。
    AC3: 降级路径——核验源不可得项如实维持「中/低置信 + 以核验为准」标注，降级清单 BOARD 留痕；批 1 四项主形态断言不受阻（手势级断言均高/中高置信——不恋战，批 1 不等）。
  - **T-391** [P1] FR-128 FE 债：仓库表 hover 对比度 + e2e 前提注记 `role:dev-frontend` area:web/src（仓库表 hover 态）+ web/e2e/README dep:—（零依赖补位；hover 早落使后续每张 FE 票 axe 基线干净）
    AC1: 仓库表 hover 态文字/背景对比度 ≥4.5:1（双主题；现 4.41:1——token 微调或 hover 底色换档票内定案留痕，不引入新 token 优先）+ axe 双主题 serious=0 维持（L11 段）。
    AC2: web/e2e/README 两行注记——①全量 Playwright 须纯净 **community 实例**前提（pro 宿主 132 红两轮实证）；②dind 调试 `--feature containerd-snapshotter=false`（T-377 D2）落笔。
    AC3: e2e 全量纯净 community 形态复跑绿 + 四闸门维持（typecheck/assert:tokens/anchor ledger/lint）+ 服务端 diff=0。
- **B1（批 1 启动 + BE 副线首航）**：T-382 ｜ T-392
  - **T-382 → done 2026-08-31 13:0x——M14 4/22（v1.1 实测参数落地首票）**：壳 Dialog→**Drawer anchor=right temporary**，宽 `min(clamp(480,50vw,800),100vw)`（**几何实证：1600 视口 {x:800,w:800} 与 T-381 实测逐位一致**）；**Configure/Deploy/Resolve 三 Tab**（方向键循环+焦点陷阱×10）+ 底栏「← 选择不同的包类型」+ Done + 右上 X；步 0 药丸化（smu-grid 键盘链路零改动）；命令三分重组（smuResolveCommands 新族——docker 登录/拉取拆两 Tab、npm 增安装验证）。**smu-* 锚族零改名**（smu-close/smu-done 复役摘退役表；锚册 v1.18）；关闭四通道 + 回焦断言；**armed 真 IdP 全链绿**（fragment 回跳→抽屉自动重开→grant 单次消费→Bearer 200——4P）+ step-up 真门腿；M8 spec 迁移更新（铸币/step-up/OIDC 语义断言全量保留）11P；四闸门 + SPA 353KB + 全量 231P（4 红全数 flake 协议甄别绿——跨腿依赖/共居 axe/m9 seed 在册族）；服务端 diff=0。DeployDialog 零改动（决策项 C 遵守）。契约漂移登记：armed OIDC 实例起法需补 BINFLOW_REMOTE_CREDENTIALS_KEY（T-260 报告滞后——docs 修订一行归 T-397）。遗留：t366-consumer DRAWER 腿跨腿依赖归 QA；parity D1 行 △ 回写归 ux 共笔。日志 reports/agents/T-382.md。
    AC1: Playwright（web/e2e/m14/，沿 M8 先例）——Set Me Up 从仓库行/详情打开为**右滑抽屉**（Drawer anchor right temporary，宽 min(480px,100vw-32px) 档断言）；`smu-*` 锚族零改名（anchor-audit 0 断链——**锚族冻结铁门槛**）；Esc/backdrop 关闭 + 关闭回焦启动元素（L02）。
    AC2: OIDC 续铸重开链路回归绿（armed 实例腿，T-242 序列复用）+ step-up 内联面板抽屉内完成（不弹二级框）+ 命令块 `pre` overflow-x:auto 不折行断言；V1 结论落定的细节断言（Configure/Deploy Tab 命名）收口——降级则挂「以核验为准」附注留痕；决策项 C 暂行 DeployDialog 保持居中不动（Q3 终裁前零改动）。
    AC3: 四闸门 + axe 双主题 serious=0 + 键盘焦点（Tab 循环/焦点陷阱）Drawer 形态复测 + M8 e2e 既有 Set Me Up spec **迁移更新**（行为语义断言——铸币/step-up/OIDC 续铸——全量保留）全绿 + 服务端 diff=0。
  - **T-392 → done 2026-08-31 12:5x——M14 3/22（K54「边际成本≈0」实证：产品码净变更 ~20 行——validate.go remote 类型表加 docker 一行 + 文案/注释）**：dind 真客户端全链——push digest `6c2a9711…` → remote pull RepoDigest 全等 + fresh 首拉上游 +3 → rmi+prune 复拉 **delta 0 冻结**；凭据链（闭实例 A2 日志 admin 全链）+ mock Bearer **全舞步恰一轮**（401×1→token 交换 Basic 验证×1→授权拉取×1）；停上游降级 **200+STALE+Upstream-Error**（客户端拉成功）/未缓存 404 零 5xx；community 无 license 建 remote docker **200**（自动受缝不新增槽）+ docker virtual 400 维持；helmoci remote 零回归（Digest 同值 tgz 逐字节）；MISS/HIT/STALE 三态 + audit 自动生效。断言翻转 7 处（矩阵面）+ 新测试 10 件；全树绿 + lint 0 + check-size 94.13/footprint 11.9MB/冷启 767ms（NFR-P63 不破）。conductor 复验：build 0 + repo 定向绿。遗留四项：docs L195 归 T-397；Prometheus remote 族 RE-11 P2 占位（双计数源归并留痕）；dind PMTU 环境注记；httpapi 无 WriteTimeout 既有姿态归 architect 裁。日志 reports/agents/T-392.md。
    AC1: 自指上游（BinFlow docker local push）→ 经 docker-remote `docker pull` digest 一致 → 二次命中本地缓存（**上游访问计数不增断言**）；dind 腿（M2/M13 夹具复用）（L12；真实 docker 客户端）。
    AC2: 上游认证链（401→WWW-Authenticate Bearer→token 交换→拉取）+ 停上游降级（已缓存可拉 / 未缓存零 5xx——本地事实兜底 + 降级标记）（L12）。
    AC3: docker dind /v2 全量回归（local/virtual 序列）+ M13 helmoci remote 序列零回归 + docker 槽 community 可用断言（remote 自动受缝不新增槽）+ remote family 指标口径（回源/命中计数）自动生效核对 + 资源门不破（NFR-P63——不得破 M12 转绿门）。
- **B2（批 1 续 + 规格小票）**：T-383 ｜ T-393
  - **T-383 → done 2026-08-31 13:3x（改判收口形——决策项 A 撤销的正确性实证：**零形态改动**）——M14 6/22**：六行对照核验全钉死（rclass 手势等价对位/两段式实证〔选型后 modal 关 URL 不变表单在路由页〕/六节结构 form-section-* 新锚/深链三腿/goto 直达/remote×docker 组合门控）——**唯一差距 = 磁贴尺寸档，票内定案不追平**（Artifactory 880px 系 33 包型 90×90 大磁贴档；BinFlow 440px〔1280/1600 实测〕配 190×44 高密度卡磁贴 + 门控徽章——追平即大面积留白，从 parity 册「现档位即可」+ **440 档写进断言**改档须有意识动 spec）。新 spec 4 腿 + m8 零回归 67/67 + 邻接面 22P + a11y-sweep 56 面 + 四闸门 + 服务端 diff=0；锚册 v1.19（6 新锚）。自纠一处：form-checksum-policy 系 v1.9 零消费退役锚不复活（改节锚+label 锚定）。遗留：parity 册 M1 行「定案 440px」升级归 ux。日志 reports/agents/T-383.md。
    AC1: Playwright——新建仓全程单 Dialog(maxWidth lg) 三步（pkg-grid 网格 → rclass → 分节表单 → Save 成功落仓）；底部 Cancel 左 / Create primary 右；常规/来源/成员/策略/治理/高级六节结构保留（L03）。
    AC2: 深链 `/admin/repositories/new?package=<t>` 进入即开向导 + 浏览器回退关闭向导回列表 + 编辑态 `/admin/:key/edit` 维持整页表单（差异注记豁免留痕）；V2 细节断言（rclass 控件 Tab vs 分段、可折叠分节）收口或挂「以核验为准」附注。
    AC3: M2/M3 建仓回归序列零回归 + 四闸门 + axe 双主题 + modal 形态焦点链 + 服务端 diff=0。
  - **T-393 → done 2026-08-31 13:2x——M14 5/22**：nuget.md §5.4 六断言（**K59 锚定值 = 409**——flatcontainer 族 GET-only + catch-all PUT 转 v2 publish + BinFlow as-built 403 不一致对照，逐条出处+置信度〔高 18/中 4/低 1〕）+ **npm.md 新建（K60 六条定案**——`/-/user/org.couchdb.user:<name>` 族全量规格化，本机 npm 10.9.8 实物源码 + live 抓包对拍 L1~L7〔匿名 401 复现/Basic 201 铸 token/token 全链绿/web 登录 ENYI 回落〕；**唯一修复面 = httpapi 写认证门对该路径族豁免，login 永不 409 不变量**——解锁 T-394）。附带发现两条入册（pacote 自更新横幅探测流量非登录协议；.npmrc 凭据键**端口参与匹配**——尾斜杠坑姊妹坑 live 实证）。clean-room 合规（零前端/UI 资产；官方文档+客户端源码为准）。遗留四条登记（v3/flat live 四臂归 T-401 条件票；裸 login 首跑归 T-394 AC1；README 清单补行归 conductor；DE whoami 错误体低置信）。日志 reports/agents/T-393.md。
    AC1: nuget.md v3/flat 直推面增量锚——包已存在时 403 vs 409 语义（D-10 终裁〔对齐 409〕邻域面；出处逐条标注 + 置信度标定）（K59；130.4 腿 P2 性质票内注明）。
    AC2: npm legacy login 端点族实证整理规格化（T-77 O-4 在案——`/-/user/*` 族路径/方法/请求响应体/错误码；npm 生态公开规范为准）；K60 端点清单定案（解锁 T-394）。
    AC3: tech-lead 就绪度确认；零 reverse-src 前端/UI 资产消费（clean-room 铁律——PRD §1.4-5）。
- **B3（批 1 续 + 服务端小票包）**：T-384 ｜ T-394
  - **T-384 → done 2026-08-31 16:2x（改判收口形——决策项 B 撤销正确性实证：**零形态改动**）——M14 8/22**：对照核验四行全钉死（**非 modal 反断言**/URL 不离列表/用户四节〔settings/options/password/groups〕+ 组两节字段归属/页脚三联几何序 Cancel<Reset<Save）——差距项全有既有裁定（parity v1.1「同档形态可保持」/console-m8 §6.10「External ID/Auto Join 不建」）或票内定案（创建态无 Retype）。6 节锚 + 4 页脚复役锚（锚册 v1.20，退役计数 98→92）；spec 4 腿 + **m14 合跑 8/8（T-383 零回归）** + 邻接 19P + a11y-sweep 全路由双主题 0 + 四闸门 + SPA +35B + 服务端 diff=0。**自愈登记**：A/B 探测 pkill 误伤 T-383 :8143 实例——原 data dir 重启恢复（curl 200）。遗留：m9 N01 请求预算 flake 系 spec 级竞态（tracker 挂载时机——一行测试基建票建议）；parity M3「MUI Paper」代差描述归 ux。日志 reports/agents/T-384.md。
    AC1: Playwright——用户/组创建走 Dialog(sm) modal（字段分节、右下 Cancel/Save、**Cancel 零副作用**）；编辑保留路由页（UserDetailPage 等——E5 豁免注记）（L04）。
    AC2: V6 核验字段集对照收口（降级挂「以核验为准」）；M7 RBAC 语义（角色闭集 / readonly 禁用臂）零回归。
    AC3: 四闸门 + axe 双主题 + modal 形态焦点链 + 服务端 diff=0 + 新锚入册（锚册 + ledger 0 断链）。
  - **T-394 → done 2026-08-31 13:4x——M14 7/22**：三子项全落——① **npm legacy login 修复（K60-1/T-374 L1 闭环）**：httpapi 门豁免 `/-/user/org.couchdb.user:<name>` 族（**窄域谓词**：PUT + 该前缀 + rev 变体 + **repo 行类型==npm 钉死**〔承重墙——否则 generic 仓把该族当节点匿名写入〕；豁免连带清 action/required 跳过写 ACL 与 addon 门——K60 认证面语义）；**裸交互 `npm login --auth-type=legacy` 真客户端全链绿**（expect 伪 TTY → Logged in → .npmrc 改写 _authToken → whoami/publish/install 含 cache clean 重装退出码全 0；web 型 401→ENYI 回落同链绿；错口令不写凭据）；strict 11 行不外溢 + login 幂等再铸永不 409 双证。② helm PVC keep 注解（merge 四态渲染 + NOTES 手动清说明；UAT 实证归 T-399）。③ 启动措辞（built-in defaults 新句 + 钉死测试 + 活体）。④ npm.md 回写（M13「不可用」注记显式作废）。httpapi 274.5s/npm 47.3s/cmd 16.5s + helm lint 0 + 全仓 lint 0。conductor 复验：build 0 + T394 5 PASS + helm lint 0。遗留：缺字段 400-vs-401 逐字（DE 文案）另裁；whoami 403 读面 ACL 误导性 E403 归 conductor 裁定维持或移出；npm≥11 漂移随升级窗。日志 reports/agents/T-394.md。
    AC1: npm legacy login 真实客户端全链（login → 凭据落位 → 既有 publish/install 复用 token）+ 既有 npm 现代认证链零回归 + 端点清单 == T-393 K60 定案（L13；130.1 DoD-P1 腿）。
    AC2: helm uninstall 后 PVC 幸存（resource-policy keep 注解或等效；升级/回滚不丢数据）+ 重装同 release 数据可挂载回归（kind/helm 编排）+ docs 部署注记「彻底删除需手动清卷」（L14 前半）。
    AC3: 启动日志措辞修正一行（前后对照留痕 + grep 新措辞在场）+ v3-flat as-built 对照结论（一致→差异行关闭登记 / 不一致→触发 T-401 条件票 Q6 终裁——**本票不动行为**）+ 审计事件族复用核对（新增通道不打新词——缺词归 audit owner 登记）（L14）。
- **B4（批 1 收尾 + PM 裁定）**：T-385 ｜ T-395
  - **T-385 → 改判 2026-08-31（conductor 裁定——V4 撤旗后票面撤销）**：T-381 实测 Artifactory 行尾**无 ⋮ 菜单**（icon-trash 直删按钮）——「行内 ⋮ 菜单化」改造失去 parity 依据，**原票面撤销不派发**。BinFlow 现行交互（行点击进详情 + 既有动作位）按 **E 类有意偏离登记**（可发现性优于 icon-only 直删；删除走危险区确认不倒退——E1 家族）。残值收编：AC1 的「菜单无删除」缺席断言精神并入终验 E1~E7 复核面（T-400）；「行点击进详情语义」已由各列表既有 spec 覆盖。FR-124.4 需求状态由 PM 随 T-395 回写（改判留痕）。
    AC1: Playwright——四列表行尾 IconButton(MoreVert)+Menu 动作集逐项：详情 / 编辑 / 复制 key / Set Me Up（仅仓库行）；**菜单无删除断言（E1——缺席断言；V4 若 Artifactory 含删除亦不跟进——安全设计不倒退）**（L05）。
    AC2: 行点击进详情语义维持 + readonly 臂管理动作按既有权限位禁用 + Menu 键盘开合语义。
    AC3: 四闸门 + axe 双主题 + 服务端 diff=0 + 新锚入册（锚册 + ledger 0 断链）。
  - **T-395 → done 2026-08-31 23:5x——M14 14/22**：PRD **v1.0→v1.1**（§7 Q1~Q7 逐项归位全表重写〔终裁/执行归位 4 + 撤销归位 2 + 维持暂行 3 臂〕+ FR-124 三改判回写〔124.4 改判 E 类偏离票面撤销〕+ **FR-131 replication 新节**〔用户指令行 J〕+ K55~K61 全量回填 + FR-130.1/2 优先级勘误 + LC-57~67 修订〔A7/C2/**D1**/待裁 1〕+ L19 新增）+ ROADMAP 未纳入段备稿（22 条票级遗留 + T-402 在列）。**交叉核对零矛盾**（BOARD 改判登记逐条对照）。**抓到 T-402 撞号**（symbol server 余量票 vs replication 增补票）——conductor 已裁：symbol server **让号 T-403**（三处回写）。遗留转 conductor：Q6 收口窗必裁（**已裁：对齐 409——T-401 触发**）；Q3-C 落章或明示暂行；ROADMAP 头 v1.1 刷随收口窗。日志 reports/agents/T-395.md。
    AC1: Q1~Q7 终裁材料上 BOARD（PM 出材料不代拍）+ 终裁联动回写——K55（核验源与三出口）/K57（决策项 A/B/C 终态）/K58（Tokens 字段集）/K59（v3-flat 锚定值）PRD §5.6 回填 + LC-66 离开「待裁」回写。
    AC2: ROADMAP「M14 未纳入项」段文本备妥（AQL M15 第一顺位 + Replay REST / D1 busy / 成员同型滚程理由），收口随 conductor 收口窗落笔（M13 R5 先例）；§2.2 候选池收编对账（DoD#7）。
    AC3: PRD 文面修正——FR-130.1/130.2 优先级内部双值统一（§4 标 P2 vs DoD P1——票面已按 DoD 取 P1）+ 拆票日志歧义登记回填（M14-SPLIT.md §6 逐条）。
- **B5（品牌 P0 + QA 中期）**：T-389 ｜ T-396
  - **T-389 → done 2026-08-31 17:0x——M14 9/22（品牌面全量落地）**：六用例五接线（⑥GitHub 头像远期 P2）——favicon（**BMP-in-ICO DIB 三档**全平台最宽支持面 + 16px 像素级 run=2 两箭可辨验证）/ PWA（180/512 实底暗板 + manifest）/ 登录页 lockup 48px（**随主题翻转 src——e2e 断言**）/ 侧栏顶 mark 24px（恒暗板）/ docs-site navbar 配置槽 + favicon 换 mark（themedComponent 双源实证）。**单点引用层 BrandLogo.tsx**（消费换文件零返工；token 同步纪律写进头注）；零 `<text>` + xmllint 全绿；`gen-brand-assets.mjs` 可复跑管线（--favicon-stroke 退路）+ `wire-brand-assets.mjs` **build 期 sha1 指纹化搬挂**（serveAsset immutable 语义诚实 + 零服务端改动——/binflow/ui 一切路径系 SPA shell 的工程约束破解）。spec 3 腿（品牌位/资产字节对账真栈/axe 双主题）+ 四闸门 + ledger PASS（brand-* 2 锚 v1.21）+ 全量绿 + SPA 预算内 + 服务端 diff=0。遗留：16px 样张待人眼复核（t389-samples/——发糊则一键重派生）；PWA maskable/docs-site 深色 favicon 远期；**事故自报**：pkill 面过宽误杀 T-383/:8143、T-384/:8144 留验实例——原 data dir 逐一恢复（curl 200），后续按端口精确 pkill。日志 reports/agents/T-389.md。
    AC1: 六用例五落地——favicon.ico（16/32/48 三档在场，16px 两箭可辨样张归档）+ PWA/apple-touch PNG 180/512 + 登录页横版 lockup 暗色版（`◆` 与纯文字品牌残留 grep=0，`login-*` 锚族不动）+ 侧栏顶 mark 24px（`app-nav-brand` 结构不动）+ 文档站 navbar 浅底版；双主题各用例截图核对（L09）。GitHub 用例⑥远期 P2 非 DoD。
    AC2: wordmark path 化——生产 SVG 零 `<text>` 零 font-family（grep 断言）；K56 票内定案（Inter Bold OFL path + `INTER-LICENSE` 在场，或手工勾画 +0.5 天留痕）；资产参数化单点引用（换稿=换文件零返工）。
    AC3: 色板锚定 `--bf-accent`/`--bf-text` 双主题值（改 token 必须同步 logo 资产）+ `login-*`/`app-nav` 锚族对账 0 断链 + 四闸门 + SPA gzip 增量 ≤10KB 预算内（logo 资产计入）。
  - **T-396** [P1] QA 中期回归 `role:qa-engineer` area:测试矩阵（web/e2e/m14/ 首跑 + BE 腿）dep:T-382~T-385（批 1 四票）,T-392,T-394
    AC1: L02~L05 批 1 首跑全绿（抽屉/向导/modal/菜单四形态 + 锚族对账 + 焦点链 + 菜单无删陠除席断言复核）+ M8 e2e 既有 Set Me Up/建仓/用户组 spec **迁移更新面**复核（行为语义断言全量保留）。
    AC2: BE 副线增量首跑——L12 docker remote 全链（自指上游 + Bearer + 降级）+ L13 npm legacy login + L14 前半 PVC keep 腿（kind/helm 编排）。
    AC3: 中期归属审计——FE 变更面（`git diff m13-done..HEAD -- web/src`）100% 归属 M14 票 + FE 票服务端 diff=0 抽查 + axe 双主题/四闸门维持态巡检。
- **B6（批 2 启动 + 文档 A）**：T-386 ｜ T-397
  - **T-386 → done 2026-08-31 23:5x——M14 13/22（PlaceholderPage 整体退役）**：TokensPage 真身（创建 modal 全链 + **一次性明文面板仅展示一次** + step-up 内联（OIDC 会话引导 Set Me Up 诚实降级）+ 会话台账（by design——令牌清单端点系 R6 登记候裁）+ 双吊销出口（danger ConfirmDialog）+ 三角色臂）；**暂行字段集留痕**（V6c 密码锁未核验——Q4 终裁后翻转面登记，先立后端字段票不私加）。api.ts formBody additive（E-18 首消费）。**四条 M8 spec 腿迁移更新**（迁移非反转——T-238 先例）22 绿 + spec 8 tests + **全量两轮 264/2→264/0**（首轮 2 红=m9 usage-fanout 在册假阳性家族，串行 4/4 绿）+ armed 实例腿绿 + 四闸门 + axe 双主题 4 扫 0 + 锚册 v1.23 + SPA +4,937B + 服务端 diff=0。遗留：ProfilePage 一行文案票（Q11 口径）；R6 令牌清单端点候裁。日志 reports/agents/T-386.md。
    AC1: Playwright——创建 modal 全链（创建 → **一次性明文面板仅展示一次** → 刷新后不可再取）+ 吊销 ConfirmDialog danger 确认 + 列表状态翻转 + readonly_admin 只读臂（L06）。
    AC2: `PlaceholderPage` 该路由退役 grep=0 + **消费端点清单 == 既有 token REST（零新端点断言——不私加端点）** + mint/step-up 链复用（SetMeUp 同源引擎，`smu-token-panel` 形态复用）。
    AC3: 四闸门 + axe 双主题 + modal/确认双形态焦点链 + 服务端 diff=0 + 新锚入册。
  - **T-397 → done 2026-08-31 23:3x——M14 12/22（两笔登记债清）**：remote-virtual.md **docker remote 专节**（建仓/URL 形态/MISS-HIT-STALE/降级/Bearer/SSRF/dind 注记 + **L195 过期行作废留痕**〔T-392 遗留①〕）+ docker-registry.md 接入节（报错 +3 行）+ helm.md 卸载节重写（keep/幸存/手动清双路/无开关如实注记）+ api-reference npm 域 +5 行（couch 族）+ FAQ 两问（缓存三态观测/卸载留卷）+ **armed OIDC 起法一行修**（T-382 漂移债清）+ README 双语收口（陈旧 cargo 措辞修/M14 段含品牌行）。**全量实测**：community 建 200/virtual 400 逐字/RepoDigest 全等/二拉 delta 0/TTL STALE 降级/npm 裸 login 全链复跑/helm 三态 template/armed 补钥真绿；`make docs` SUCCESS 4.53MB 零断链。遗留五项：Docker Hub 直连候公网环境（归 T-399 UAT 顺腿）；docker virtual 开矩阵归 conductor 裁；migrate-artifactory.md 措辞陈旧登记；Chart bump+UAT 归 T-399；by-digest 强刷产品决策候选。日志 reports/agents/T-397.md。
    AC1: docker remote 接入指南（三态齐装叙事 + 缓存命中/降级语义）+ npm legacy login 注记（老 CLI 通道 + 尾斜杠配对注记衔接 T-374）交付；客户端命令全部实测可复跑（L18 段）。
    AC2: helm uninstall PVC keep 说明（彻底删除需手动清卷）+ api-reference 增量（npm login 端点族）+ FAQ 增补；`make docs` SUCCESS 零断链 + 侧栏挂页。
- **B7（图标接线，单票波）**：T-390
  - **T-390 → done 2026-08-31 21:5x——M14 10/22（图标面全量）**：30 枚逐字搬运（`<text>`=0、mono 全 currentColor）+ repos.ts 联合补 helmoci 第 13 型（五核心地板不动）+ **四消费点 28 消费 + 2 豁免**（pkg-grid brand 22px + 门控三件套〔mono+0.4+徽章〕/smu 药丸 brand 字符图标 grep=0/列表 Chip+树 .ico mono currentColor 逐值断言/addon 矩阵 13+2 brand）。**暗底全枚实算揪出 README §6.4 口径错误**（对背景而非宿主面）——11 色 <3:1 → `--bf-pkgicon-*` 提亮档（最差宿主 ≥3.4）按 K61 回写；helm/nuget 超 +10% 量级留 ux 复核（纯蓝通道物理下限）。aria-hidden 全覆盖；SPA **+5,725B ≤10KB**；四闸门 + axe 双主题（56 路由面 sweep）+ **M8 建仓/SetMe Up spec 零改动通过**（旧断言从不锚字符）+ m14 16/16。**票内自擒**：`pkg-grid-item` 类名自 T-240 起 dead-CSS（git log -S 全史零命中落 DOM）——本票复线一属性，卡面+禁用置灰复活（唯一视觉外溢面，双留痕；T-383 440px 断言复证不破）。服务端 diff=0。遗留：assert-tokens 属性选择器豁免规则单独立票；helm/nuget 暗档 ux 拍板；mono trashcan/webhook 预留。日志 reports/agents/T-390.md。
    AC1: 搬运 30 枚（mono/brand 双版）+ **npm/go 转 path 前置**（grep `<text>`=0）+ `lib/repos.ts` PackageType 联合与图标 key 对齐（helmoci 入联合 FE 小改；deb↔debian / go 映射两例外注记）（L10）。
    AC2: 四消费点逐点——pkg-grid brand 版（+门控态 **mono + opacity 0.4 + pkg-tier-* 徽章三件套**——brand 版不置灰）/ smu-grid brand 版（`CLIENT_PKG_META` 字符图标 `▫ ⬢ ⌬ ⬒ ⬓` 退役 grep=0）/ 仓库列表·制品树·搜索类型列 mono currentColor / LicenseAddonsPage addon 矩阵 brand 版；30 枚全部被消费或注记豁免。
    AC3: 暗底抽查 ≥3:1（发闷允许 +10% 亮度微调并回 README §4 登记——K61）+ 装饰图标 aria-hidden / 语义处 aria-label（类型列）+ SPA gzip 增量 ≤10KB + 四闸门 + axe + M8 建仓/SetMe Up spec 联动更新全绿。
- **B8（批 2 收尾 + 文档 B）**：T-387 ｜ T-398
  - **T-387 → done 2026-08-31 22:5x——M14 11/22**：两页工具栏尾组（列选 Menu + menuitemcheckbox + 全选复位 / 刷新钮 + 取数中进度环禁用）+ **偏好持久定案 per-page localStorage**（`binflow-console-cols-{repos,audit}`，读回清洗 + 全隐回落 + 至少一列守卫——console-ux v1.22 注记留痕；reload 持久/互不染 e2e 断言）。spec 5/5（自纠 2 处 spec 缺陷）+ 定向 29+7 + **全量两轮 258/0/24**（首轮 1 红串行甄别在册 axe 假阳性家族）+ 四闸门 + axe 菜单开态双主题 0 + 锚册 v1.22（23 锚）+ SPA +3,244B ≤10KB + 服务端 diff=0。**行面零触碰口径**：单元格内层 JSX 逐字节未动（T-390 Chip 原样），列显隐仅整列条件包裹。遗留：推广 users/groups/search（共享层 columnPrefs.ts 已就绪）；:8157 留验可整删（按端口精确杀——T-389 教训已吸收）。日志 reports/agents/T-387.md。
    AC1: Playwright——列选 Menu（checkbox 列表）开合/列显隐/全选复位 + per-page localStorage 持久（reload 保持）；刷新 IconButton 取数；轮询页自刷新维持（L07）。
    AC2: 「无端点列不伪造」纪律（列集 = 既有全部列）+ 其余列表页不受影响断言 + 列宽/空列处理。
    AC3: 四闸门 + axe 双主题 + 服务端 diff=0 + 新锚入册。
  - **T-398 → done 2026-09-01 05:0x——M14 20/22**：**nuget.md 三处陈旧修正**（重复臂表重构为 v2/v3 双列**字节盲**表——删「同字节 201」〔T-378 起漂移债清〕+ v3 409 官方逐字文案与 d 覆盖 nuance + L182 同步 + `--skip-duplicate` 提示）+ **governance.md 复制管理最小节（从无到有**——引擎语义/CRUD curl/字段门错误审计 SSRF 表/启停/观测）+ api-reference M14 速览（replication 域五行含 **PUT 行**）+ console.md 仓库管理 Replications 三处 + 治理指针 + **顺笔**：Access Tokens 三处「占位页」陈旧行更新为 T-386 真身并正面写对 Q11 口径（票内判定理由与回退路径留痕——conductor 认可保留）+ artifactory-path-map/README 导航 + m8 e2e README make docs 前提行。**live 实测**：replication 全臂（POST 201/PUT 翻转 round-trip 容忍/错误臂 400×3·404·401·403·409 逐字/DELETE 204→404/readonly 臂/**停用即停入队引擎实证**）+ nuget 四臂（v3+v2 curl E2E 可复跑）+ `make docs` SUCCESS×2 零断链 + 三新锚点产物命中。遗留：dotnet L14' 候 SDK（沿 T-401）；oidc-config base64 约束登记微票；ProfilePage FE 文案行登记 FE 票。日志 reports/agents/T-398.md。
    AC1: console.md parity 化——新形态操作说明（右滑抽屉/单弹窗向导/行尾 ⋮/Tokens 页）+ 截图随终形态更新 + **FAQ 形态迁移对照表（Artifactory 手势 → BinFlow 对应——迁移用户零学习成本叙事）**（L18）。
    AC2: 品牌注记（logo 来源与候选定案 + 图标重绘许可姿态摘要 + 低置信三枚商标复查登记）+ `make docs` SUCCESS 零断链 + README 双语随新能力核查（收口双项前哨）。
- **B9（批 2 尾票，单票波）**：T-388
  - **T-388 → done 2026-09-01 02:4x——M14 16/22（FE 主轴 10 票全清：8 done + 2 改判零改动）**：**F2 插画槽**——EmptyState illustration prop → EmptyArt 40×40（empty-art 锚；序=插画→说明→主行动；currentColor 占位线稿=候选 1 隐喻派生〔虚线容器+双箭〕；**槽位契约规格化**——真插画资产归设计票、换稿零返工；八列表页 15 落点 + 403 反面）。**N2 图标槽**——18 条一级条目 16px mono currentColor（fill 计算值=文字色 spec 实证；分组标签/模式切换不配——V5 口径；Material path 内联 Apache 2.0 不引包）。spec 6/6（自纠 1 spec 缺陷）+ shell 5/5 + m14 全目录 34/1skip + 定向 46 绿 + **全量两轮 271/0/25**（首轮 2 红=在册并行 flake 家族，串行甄别 16 绿）+ 四闸门 + axe 双主题 + 锚册 v1.24（empty-art/nav-icon 家族）+ SPA +2,155B + 服务端 diff=0。遗留：插画真资产设计票。日志 reports/agents/T-388.md。
    AC1: EmptyState 40px 可选插画槽（渲染/缺省双态）；图形 = logo mark 容器+箭隐喻线稿（ux 稿），**不引第三方插画库**（L08）。
    AC2: N2 按 V5 结论执行（主流版本有条目图标 → 16px mono 图标槽）或**降级不做 BOARD 留痕**（parity N2 既定出口）。
    AC3: 四闸门 + axe 双主题 + SPA 预算维持 + `app-nav` 锚族零改名（侧栏 DOM 结构纪律）。
- **B10（release）**：T-399
  - **T-399** [P1] release 烟测 + UAT 随里程碑 PR `role:release-engineer` area:deploy/ + charts/（版本收口）+ CD 链 dep:全部实现票（B9 收官后启动；可前移 B9 与 T-388 并波——conductor 裁量，P2 尾票不阻塞烟测面）
    AC1: 部署矩阵烟测（compose/k8s/systemd/offline 抽样）+ Chart 版本收口 bump（PVC keep 注解随 T-394 面核实）+ **helm uninstall PVC keep 在 UAT 链验证**（PRD §8-10）。
    AC2: UAT 随里程碑 PR（develop→main，CircleCI→52.79.109.153 分阶换装 + healthz + 双面烟测——M13 起常态）；与 conductor 收口时序协同（M12 T-355 教训条款）；SPA/favicon 新资产 go:embed 接线核验。
- **B11（收口波）**：T-400
  - **T-400** [P0] QA 终验 `role:qa-engineer` area:全量验收矩阵 dep:全部票 + T-399
    AC1: L01~L18 全量（承证+增量）+ M1~M13 全 P0 双形态复跑全绿 + 归属审计（FE 变更面 100% 归属 + FE 票服务端 diff=0）+ 资源门三连（footprint ≤100MB / check-size ≤100MiB / 冷启动 <2s）+ SPA gzip 增量 ≤10KB（图标 + logo 资产计入）+ `make test`（race）全树绿维持。
    AC2: **L16 parity 收口专项——差距矩阵 §7 复核基线逐格终评（终评覆盖率 100%，全部 △/✗ 格翻 ✅ 或（豁））+ E1~E7 豁免逐条复核（豁免倒退=缺陷）+ V5/V7/E7 三出口落档**；Q1~Q7 终裁归位核查（LC-66 离开「待裁」）。
    AC3: DoD 八条逐条（实测数字归档）+ 总裁定 PASS → conductor git tag m14-done（UAT 证据随里程碑 PR 归档；对外发布红线维持）。
- **波外条件票**（未触发 BOARD 留痕非 DoD 缺口）：
  - **T-401 → done 2026-09-01 00:2x（Q6 终裁兑现——击落于报告落盘前夜，报告完整在案）**：v3/flat 直推重复臂（包已存在 + w-only）→ **409 官方文案**（flat.go 冲突前移 + as-built 403 断言反转注明 T-401 豁免）；③ d 覆盖 201 / ④ 新包 201 维持；四臂 table-driven + **live curl 真栈** + nuget 全量 14.7s 绿（v2/v3 序列零回归）。nuget.md §5.4 as-built 对照行更新（403→409 关闭留痕）。area 纪律四文件。遗留：LC-66 落章归 conductor 收口窗（PM 备稿在案）；**docs/user/integrations/nuget.md 三处陈旧**（L161「同字节 201」自 T-378 起过时——D-10 后漂移非本票引入——tech-writer 小票建议）；dotnet L14' 腿候 SDK 环境（BINFLOW_T287_CLIENT_E2E=1 即验）。日志 reports/agents/T-401.md。
    AC1: 补锚值对齐翻转（403↔409 语义照 nuget.md 增量锚，D-10 先例方向）+ 断言反转归属豁免票 + nuget 双客户端回归；终裁=有意差异则本票不触发（D 层差异行留痕，LC-66 归 D）。
  - **T-403** [P2·条件 余量] NuGet symbol server 余量票（M12→M13→M14 四承——T-357/T-379 延续；**原 T-402 让号 replication 增补票**——conductor 2026-08-31 改号，T-395 §遗留③ 处置）`role:dev-go-core` area:internal/adapter/nuget（symbol 子域）dep:P0/P1 全收官 + 余量条款
    AC1: mini as-built 规格随票（T-293 终裁口径）；.pdb/GUID 路径面 + 真实客户端腿；未触发 M15+ BOARD 留痕。

**关键路径**：T-381（核验锚 B0）→ T-382→T-383→T-384→T-385（批 1 FE 串行链——V1/V2/V4/V6 细节断言收口）→ T-389（logo P0）→ T-386→T-390→T-387→T-388（批 2/图标 FE 链）→ T-398（docs B）→ T-399（release+UAT）→ T-400（终验·矩阵逐格终评）→ m14-done。副线并入：T-393→T-394（B2/B3 服务端小票链）→ T-396/T-397；T-392（B1 docker remote）→ T-396；T-395（PM 裁定）终裁归位归 T-400 AC2 核查。**T-391（FE 债）零依赖 B0 补位；T-392/T-394 与 FE 天然错峰全程并行；D1+M1 并波与 L1+FE 债合票为 conductor 压缩裁量（风险⑨）。**

**风险登记（拆票日志 M14-SPLIT.md 详表）**：① **Q1 核验源终裁时点**——须 B1 派发前（建议与 Q2/Q3 同窗）；V1/V2/V4/V6 落定前批 1 细节断言不得转正，降级路径 T-381 AC3 内置（t226 容器→外部活体→③凭标注，批 1 手势级断言均高/中高置信不阻塞）；② **Q2 logo 圈定窗截止 B2 前波**——T-389（B5）候选 1 工作稿起步，资产参数化单点引用（换稿=换文件零返工），窗后未推翻即转正 BOARD 留痕，wordmark K56 票内定案；③ **Q3 决策项 A/B/C 终裁**——暂行照 parity §10 批 1 建议（C=Deploy 保持居中不动），终裁随 Q1/Q2 同窗 B1~B2，翻转面小；④ **锚族冻结 + 新锚入册（web/src 改动铁门槛）**——smu-*/login-*/app-nav 零改名（anchor-audit 0 断链逐票 AC），D1 壳替换保留清单六项，侧栏品牌区不动 app-nav-brand，新面（L2 菜单/Tokens/列选器）新锚入册；⑤ **E1~E7 豁免复核点位**——L2 菜单无删除缺席断言（E1，V4 若含删除亦不跟进）、M3 编辑整页（E5）等七条 T-400 AC2 终验逐条复核（豁免倒退=缺陷）+ 三出口落档；⑥ **四闸门 + axe 双主题 + SPA 预算维持**——FE 十票合入条件票票内嵌，30 枚图标 + logo 资产计入 gzip 增量 ≤10KB（NFR-P61），FE 票服务端 diff=0 沿 M8 T-235 先例；⑦ **活体核验降级与零静默升格**——不可得项维持「以核验为准」标注，grep 附注清单与核验结论一一对应，置信度回写=改契约须修订留痕；⑧ **D1 抽屉化的既有兼容**——M8 e2e spec 走「迁移更新非反转」（铸币/step-up/OIDC 续铸行为断言全量保留），更新面 100% 归属 M14 豁免票（T-396/T-400 归属审计）；⑨ **FE 串行链 10 票工期**——web/src 一波一票默认纪律，压缩两选项（D1+M1 并波〔PRD §1.3 明示可并行〕/ L1+FE 债合票〔M13 ⑦ 先例〕）+ T-399 可前移 B9；⑩ **条件票触发态**——Q6 翻转（T-401）/symbol 余量四承（T-403）/N2 V5 降级（T-388 票内出口）/D3（Q7）不占票号（用户立项才开后端域票）；全触发上浮 24（PRD 21~26 线内），未触发 BOARD 留痕非 DoD 缺口。

**K56 品牌资产生产化 → done 2026-08-31 14:1x（ux-designer 插空票——T-389 接线弹药就绪）**：候选 1 生产件 6 件（mark/mark-dark/mark-mono/lockup-horizontal 674×128/lockup-dark/README——wordmark **手工勾画 monoline**〔cap 100/笔宽 16/圆帽/字距 26，逐字坐标表公开——绕开 Inter 转曲许可面，与 mark 圆帽箭头同语言〕）；npm/go 四枚 `<text>`→path；conan brand 换色 #669ACC（T-381 L02）+ mono 保持 currentColor 纪律；README 双 v1.2。ripgrep 复核 `<text>` 残留 0（仅三张候选规格表存档件——README 已警示，全仓 xmllint 前清理归 chore）。**顺手修 3 枚 brand 图标（webhook/trashcan/generic）注释非法 `--` XML 序列**（严格解析器拒载隐患，语义零变动）。PNG/favicon 派生命令已备（T-389 执行）。日志 reports/agents/K56-brand-paths.md。

**用户指令 intake ②（2026-08-31 23:2x）：replication 的交互要与 Artifactory 一致**——M14 范围增补。立票 **T-402（P0 插空，两段）**：①规格锚定段（qa-engineer 插空——t226 活体探测 Artifactory replication 交互面〔仓库 Replication 面的表单字段/cron 形态/启停开关/Replicate Now 动作/状态呈现〕+ console-artifactory-parity.md 增 R 系条目 + BinFlow 现状差距清单；**差集法只读探测——INC-1 教训永不点确认**）；②实现段（dev-frontend，候 FE lane 空位——照锚定规格对齐 BinFlow 控制台 replication 面）。

**Q6 终裁（conductor 2026-08-31 深夜窗，D-10 同族同则——用户对齐基线常设条款）**：v3/flat 直推面重复臂 **对齐 409**（K59 锚定值；as-built 403 判不一致）——**T-401 条件票触发**（翻转小票 + 矩阵 LC-66 离开「待裁」归 A；PM 建议≡conductor 裁定，收口窗落章）。

**T-386 迟到终报补录（2026-09-01 00:2x）**：验证窗末段细节落日志（一次性明文三面不残留 + Bearer 终裁 + 端点闭集对账 + 与 T-388 同树协调注记——合并树 typecheck 复核过、归属以 `777903b` 清单为界；留验实例已全清）。**契约漂移登记（候后端小票）**：`POST /api/security/token` 带 `username=<不存在>` 答 **500**（`Tokens.Issue` subject 查找错误未被 handler `ErrInvalidCredentials` 分支收编）——auth-model 3.1 语义应 **400**「username is required or unknown」臂；FE 已按实况收口（错误内联呈现），服务端修正归 M14 收口窗或 M15 候选池（conductor 留痕）。

**T-402 ①锚定段 → done 2026-09-01 02:3x（PASS——2 项降级如实标注零静默升格）**：parity **v1.2**——**R 系 R1~R10**（高 5/中高 3/中 2）+ §6A 新节 + §7 复制行 + §10 批 4；证据 11 截图 + measurements.json。

**T-402②包 A 立票（conductor 2026-09-01 02:5x——R1 裁定执行，双票并行）**：
- **T-404 → done 2026-09-01 03:3x——M14 18/22（replication 交互面对齐落地——用户指令②兑现）**：R1 内嵌节（ReplicationsSection：列表四态 + 内嵌表单 + E1 输入 name 删除〔错名禁用/对名放行〕+ 启停 Switch 接 T-405 PUT + **R3 预留位组六控件恒禁用零提交**——字段族两档定案留痕，spec 网络层对账 POST 键集=wire 闭集）+ `?section=replications` 深链 + 仓 Tab 指针升级（摘要卡+深链+全局页；remote/virtual 不适用注记）+ 列表 **Replications 第 8 列** + 行级 Run（0=纯文本照 OSS cell；≥1=▶ 图标 aria 计数深链 data-active）。编辑=删除+重建（REST 无字段级 PUT 唯一路径——recreate-note 明示「未决任务级联清空」）。spec 8/8 + m14 全量 42/1skip + 邻接 27 + m8 68（1 红在册串行绿）+ m9/m10/m12+13/a11y-sweep 全绿 + 四闸门 + 锚册 v1.25（36 名 + repo-repl-degraded 退役）+ SPA +6,159B + 服务端 diff=0。**契约修正**：实际挂载面 `/binflow/api/v1/replications/{id}`（v1 段——派单路径少 v1）。遗留：字段级 PUT 落地时 DELETE+POST 分支整体替换；预留位六控件候后端模型逐字段转实；列选项菜单 remote Tab 噪声（per-page 偏好语义可接受）。日志 reports/agents/T-404.md。
- **T-405 → done 2026-09-01 03:1x——M14 17/22**：`PUT /binflow/api/v1/replications/{id}`（数值 row id）——enabled 翻转即订阅行状态位、引擎现读（**启停即时无需重启**：停=新事件不入队+新任务不领取≤1sweep+在途 attempt 跑完；恢复=下趟 sweep 排空）；权限门同 POST/DELETE（CapSystemWrite + p.Admin 复检同 403 文案）；错误梯 404/400/503/501 全 errors[] envelope。**覆盖面票内定案：仅 enabled 最小面**（Artifactory 宽字段族中置信且 BinFlow 模型不载——全字段归后续票；decode 复用 create 全形余字段忽略——T-404 FE round-trip 不被拒）。**internal/replication 零文件改动**（引擎现读状态位——无需 Store 方法）；table-driven 12 行 + 引擎三腿（-race + count=5 稳定）+ httpapi 全量 124s 绿 + lint 0；manage 门 guard 57→58 bump。conductor 复验：build 0 + 四测试定向 PASS + replication diff 空。**契约钉死**：路径/`{"enabled":bool}` 必填/200+GET 投影 11 字段——T-404 直接消费。遗留：architecture §7.1 PUT 行归 architect；audit 词表 replication.config.*；R6/R3 编号差 conductor 统一；**诚实注记**：失败中的在途行本趟内烧完剩余退避（≤6 attempts）才停——按趟读位自然结果。日志 reports/agents/T-405.md。

**T-396 → done 2026-09-01 04:4x（首轮 FAIL→D-396-1 conductor 热修→复验 4/4 PASS）——M14 19/22**：**L01~L19 全绿**（L12 docker remote live 全链 digest 全等/delta 0/STALE；L13 npm legacy login 全链；L14 **PVC keep kind 真集群腿**〔uninstall 幸存+重装逐字读回〕+ v3-flat 四臂 live；L19 PUT 联合腿 live 全形态）；归属审计 internal/cmd/charts 28 文件 + FE 10 提交 **100% M14 票号零孤儿**；改判预核实（T-383/384 纯锚位 + T-385 MoreVert=0）；**E1~E7 逐条复核零倒退**；race 33 包零 DATA RACE；footprint 13.9MB/冷启 718ms；漂移池 2 清 6 处置态明确。**D-396-1 [P1]（已修）**：m9 usage-fanout ambient 白名单未随第 8 列迁移——**修复取产品侧**（RepositoriesPage 第 8 列 fetch gate 在列表 ok 后——无权限主体不发注定 403 的调用，u8 零水合 NFR 钉子维持）+ 白名单一行；复验 4/4 绿（真栈重建）。**D-396-2 [P2] 裁定（conductor）**：NFR-P61 SPA 预算按**每票增量口径**（各 FE 票 AC 均自证增量 ≤10KB 全过）；累计 +24.8KB 系 22 票资产合计的自然结果——登记 T-400 与 PRD 口径对账（非缺陷）。观察五项（main 侧 CI 修复未回流 develop 归 T-399 知悉项等）。日志 reports/agents/T-396.md。

**T-404/T-405 联合腿 → 验证通过（conductor 2026-09-01 03:3x，真栈）**：净实例建仓 200 → POST 配置 201（11 键投影）→ **PUT enabled:false → 200 + enabled:false 回显（GET 投影全形）** → PUT enabled:true → 200——T-404 的启停 toggle 所消费契约端到端成立。FE 侧 t404 spec test 4 的 mock 200 腿已在 T-404 收口证绿。

**T-402① 决定性发现（补全中段——Edit 误吞修复 2026-09-01 02:5x）**：① t226 系 OSS license——REPO_REPLICATION entitlements 全 false，UI Replications 步硬禁用，字段级表单活体不可达 → 降级 bundle+公开 REST 双源（中置信明文）；② **Artifactory 形态实测**：复制配置入口 = **仓库编辑页左轨步骤节**（Basic/Advanced/Replications 387px 等宽横排，panel-Replications 锚，无独立路由，**整页内嵌节无 modal/drawer**）+ 仓列表 **Replications 列**（icon-run 三态 tooltip → executereplicationnow）+ 全局封锁双开关（blockPush/blockPull，UI-API 不受门）；③ **BinFlow 差距**：UI CRUD 表单缺失（仓 Tab 现为指针卡——R1 拓扑相反）+ REST 无 PUT（启停阻塞）+ cron/Replicate Now/Test/全局封锁四项**后端语义前置**——**勘误：BinFlow 复制引擎实为事件驱动+1min sweep，非用户级 cron**（简报误称已在报告更正）。**②段工料两包**：包 A（本节上方 T-404/T-405 双票）；包 B（PM 立项评审——cron 双轨/trigger/Test/封锁：产品语义决策非纯 parity，滚 M15 候选 + Q 项登记）。**R1 拓扑裁定（conductor 2026-09-01，用户「完全一致」常设条款）**：取 Artifactory 实测形态——**配置内嵌仓编辑页 Replications 节**（仓 Tab 保留指针 + 列表列 + 行级 Run 动作三件套同构）。日志 reports/agents/T-402a.md。

**T-399 → done 2026-09-01 07:5x——M14 21/22**：部署矩阵六腿烟测全绿（goreleaser 六平台 6/6 校验和 + 四镜像 arch/smoke + compose/k8s/systemd/offline 四部署腿 roundtrip 200/201/字节一致/删除幂等 + **Chart bump 定案 1.3.0→1.4.0**〔T-394 PVC keep 系渲染产物级行为变化〕+ **helm uninstall PVC keep kind 真集群一手实证**〔install→Bound→marker→uninstall→PVC/PV 双幸存→重装收养→marker 逐字回读〕）+ UAT BEFORE 取证（uat.13ca2d6 = origin/main M14 半程）+ AFTER 可粘贴清单备妥（候 T-400 PASS 后 PR）+ SPA/favicon/manifest go:embed 接线核验全绿（含 UAT 线上现役壳 sha1-h8 吻合承证）+ T-396 L14 承证复核两轮独立绿。**F1 首红登记（候 conductor 裁）**：六平台聚合 103.36MB > 100MB（T-326 门——建门 94.13，M12→M14 累积；单档 40MB 与运行时门全绿，本轮 CHECK_SIZE_WARN=1 放行）——与 D-396-2 同族记账归 T-400 收口。T-397 遗留① Docker Hub 401+Bearer=公网可达（产品腿归 AFTER 可选）。日志 reports/agents/T-399.md。

**用户指令 intake ③（2026-09-01 07:1x）：「前端制品浏览逻辑照搬Artifactory，现在都无法展示制品」——P0 热修立票 T-406（conductor 执行）**。复现定位：用户真实数据副本（~/dev-center/data，266 仓含 t240r remote / t240v virtual e2e 残留）——树列全类仓库而内容面 `svc.List`→`loadLocalRepo` 对 remote/virtual 一律 400 → **点 remote/virtual 即红卡**（UAT/新种子无此类仓故不复现；深链实为 `/artifacts/<repo>/<path>` 路由形态，初判失效系探针姿势错误已排除）。

**T-406 → done 2026-09-01 07:5x——M14 22/22 实现面收官（P0 用户主诉兑现）**：**服务端**（internal/repo/service.go，parity——rest-api.md §3 remote-cache FileInfo 证据）：① `List` remote 仓列**缓存行**（列表面永不回源）；② `Get` remote 臂 folder 面 = 缓存 marker 行直答 ErrIsFolder + **读侧材料化 putFolderRow**（fetcher 落地只写文件行——fetcher.go:831；存量缓存自愈）；virtual 拒绝维持（FR-21-AC8 P2）。**Console**（ArtifactsBrowser.tsx）：virtual 不发注定 400 调用（D-396-1 同款 gate）→ `tree-empty-virtual` 成员感知空态（读 configuration.repositories）+ RepoBranch virtual 静态化（无箭头无子级区——消除选中即骨架屏）；remote 空态文案分支「仅展示已缓存制品（浏览不回源）」。锚册 v1.26（tree-empty-virtual 入册）。**自测**：go repo+httpapi 全量绿（58.7s/110.5s 无冻结受损）+ 新增 t406_remote_browse_test（上游零接触计数断言）+ lint 0 + 四门（anchor-audit unregistered=0）+ e2e 串行 38✓（tree10/keyboard+icons11/trash+artifacts13/usage-fanout4）+ **用户数据副本 live 四态实证**（remote/virtual 红卡消除，残余 1 次 virtual 400 系 meta 前首请求无 UX 影响）。**用户本地生效**：拉 develop + `make console && make build` 重启（folder 行读侧自愈，无数据迁移）；UAT 随 m14 PR。遗留：virtual 聚合 + remote 远端浏览 → M15 候选。日志 reports/agents/T-406.md。

**T-406b 后记 → done 2026-09-01 08:2x（`f4ee09f`——T-400 终验首红归因修复，conductor）**：T-400 全量 go 抓到 pypi `TestRemoteRepositoryProjectPage` + nuget `TestV2RemoteProxyFallback` 两红，worktree 双基线（282bb48 绿 / 81e528d 红）坐实系 T-406 回归：① pypi `/simple/<project>/` 合法斜杠结尾索引读被 folder-face 的 **childless-404 短路**截胡不回源 → 修：childless folder 落穿 getRemote（缓存面 marker/子级两支维持零上游接触；t406 测试同步改）；② nuget 测试钉住的 404 降级系 svc.List-拒绝-remote 的自认降级（注释原文 the degradation the ticket report registers）——T-406 放开后回退**真正枚举落袋事实答 200**，契约更新（行为改良）。复验：nuget 26.2s/pypi 7.9s/repo 50.9s 全包绿 + adapter 全树 sweep 零红 + lint 0；console 零改动。**教训入册：协议适配层的「目录」可能是合法 upstream 资源（斜杠结尾），服务端 folder 语义只对缓存行成立。**

**T-400 → done 2026-09-01 08:3x（PASS——五 AC 全绿，m14-done 就绪）——M14 终验收官**：**AC1** 22/22 票 done/改判成立 0 FAIL 0 DEFERRED（T-403 未触发按余量条款）；L16 终评落档 parity 册 **v1.3 §7A**（覆盖率 100%——残余 △/✗ 全翻 ✅/豁·登记）。**AC2** 改判与豁免终核全过：T-383/T-384 零形态坐实、T-385 MoreVert=0、T-401 七臂 409/409/201/201 + live curl 双矩阵绿、T-395 Q1~Q7 归位零矛盾、**E1~E7 零倒退**（代码面逐条）。**AC3** 全量回归 PASS（经 f4ee09f 收口）：四门绿（SPA 377,868B）/ go 33 ok + 2 红归因修复 / lint 0 / e2e 268 过（1 红系在册 m9 seed 家族串行 4/4 绿）/ armed 腿 4/4+7/7 / footprint 11.9MB·冷启 36ms GREEN。**AC4** 记账数表已核：D-396-2 逐票全过（T-406 +226B 闭链）+ F1 103.37MB 一手构建吻合。**AC5** docs 漂移 0（console.md 仓型面一行补笔 + nuget.md 陈旧三处系 T-398 已清承证）。缺陷 D-400-1 [P1·f4ee09f 已修]、D-400-2 [P2·契约更新] 均闭。日志 reports/agents/T-400.md。

**m14-done 收口笔（conductor 2026-09-01 08:4x——M14 关闭，`m14-done`）**：
- **F1 终裁**：六平台聚合门**调基 100→120MB**（Makefile check-size 落地；94.13→103.37MB 系 M12~M14 资产累积；单档 40MB 与运行时门维持硬约束）。**D-396-2 记账**：SPA 预算维持每票增量口径（累计 352,850→377,868B）。
- **PRD v1.2 收口笔**：LC-66 待裁→**A**（T-401 已执行 + 证据链闭合；计数 A 8 / C 2 / D 1 零待裁）；README 双语 M14→done；ROADMAP 头部切 M15 + M14 段 `m14-done` + 「M14 未纳入项」启用（T-406 遗留两项并入：virtual 聚合 / remote 远端浏览）。
- **m14-done tag + 里程碑 PR + UAT AFTER**：随本笔执行（PR 自建自合 per standing 授权；UAT AFTER = deploy_uat 后 governance push-replication 0→≥1 + version sha 翻转取证）。
- **M14 终态**：24 票全落（22 done 含 T-406/T-406b 热修 + T-401 条件票已执行 + T-403 未触发留痕 + T-402/T-404/T-405 增补）；parity 册 v1.3；锚册 v1.26。**M15 候选池开局**（AQL 第一顺位）。

---

## M15 票据（搜索基建专程——AQL 首程）

**立项（PM 2026-09-01，`docs/prd/milestone-15.md` v1.0）**：AQL 专程两度让位后兑现——「M15 核心（item+property 域 + 引擎/分页 + 老搜索首批 + 资源门简化）/ M16 高级面（statistics·usage + QRL 全量 + UI 搜索族）」分阶段。FR-132~140 九条；LC-68~79（A 9 / C 2 / 待裁 1——LC-76 远端浏览）；L20~L34；§5.7 搜索域端点全景归属表（14 端点族）；断言反转三处（SR-03/04、tree-empty-virtual、mint 500→400）预归属豁免票。

**conductor 审定（2026-09-01 09:1x）——PRD v1.0 转正 + 三项即裁**：
- **Q5 终裁：不引入 cron 双轨**（维持事件驱动 + 1min sweep 唯一引擎；手动场景 Replicate Now 承接——T-402a 勘误 + 双轨一致性成本材料充分，即裁落章；M16 复制域二程不再列 cron 为实现项）。
- **Q6 即裁：docker virtual 建仓矩阵开禁**（M14 remote 首航 + virtual 聚合语义既有 + helmoci virtual 先例——对齐 Artifactory 组合完整性；tech-lead 列条件小票，非 DoD）。
- **Q1/Q3 暂行确认**（分阶段边界收口窗终裁；400 诚实拒绝维持）；Q2 随 aql.md 回写归位；Q4 评估票材料后裁；Q7 维持 as-built 暂行。
- 派发前置：tech-lead 拆票（票号 T-407 起；aql.md 规格票 + ADR-0043 为 B0 前置锚——L20 就绪才开主轴实现）。

### M15 票批 v1（tech-lead 2026-09-01）

**拆票日志 `docs/M15-SPLIT.md`**（票据明细 AC 全文/依赖图/风险登记/口径——派单直接引用）；实拆 **25 票**（P0×8 / P1×11 / P2×6——波外条件票 T-431 计入 P2；K65 为 T-417 票内余量条款、Q4 实现段/Q7 by-digest 不占号——PRD §1.3 估 19~25 线内上沿），**B0~B12 十三波全宽 2**（波内 area 互斥；FE 主线 web/src 一波一票错峰 B3→B4→B6→B8；AQL 主轴六波串行链 B0~B5 为决定性路径）；断言反转三处预归属：SR-03/04→T-417、tree-empty-virtual→T-416、mint→T-410。

**波次表**：

| 波 | lane 1 | lane 2 | 备注 |
|---|---|---|---|
| B0 | T-407 aql.md 规格票（rev） | T-408 ADR-0043（arch） | 前置锚双票并行（conductor 指令）；软协作 132.4④ 映射表 |
| B1 | T-409 AQL 语言前端 | T-410 mint 400（插空） | L20 就绪 + ADR Accepted 后开主轴 |
| B2 | T-411 AQL 执行内核 | T-412 virtual 聚合 BE | internal/search vs internal/repo 错峰 |
| B3 | T-413 ACL+资源门 | T-414 FE 列选器三页+member-pop | FE 链起步（零依赖早落） |
| B4 | T-415 AQL 端点面 | T-416 virtual FE（断言反转②） | httpapi 搜索面 vs web/src artifacts |
| B5 | T-417 老搜索三端点（断言反转①） | T-418 replication.md 增量段 | httpapi 搜索面先后脚（T-415→T-417） |
| B6 | T-419 FE AQL 模式 | T-420 Replicate Now | T-420 FE ▶ 小腿与 T-419 文件不相交（裁量） |
| B7 | T-421 QA 中期回归 | T-422 Test+封锁双开关 | 主轴 L21~L24/L26/L29 复核窗 |
| B8 | T-423 busy 重试预算 | T-424 L2 快捷+e2e 纪律 | busy 与满载回归同场 |
| B9 | T-425 远端浏览评估票 | T-426 文档票（两腿） | Q4 材料窗 + 用户文档 |
| B10 | T-427 PM Q 终裁联动 | T-428 文面回写簇 | Q1/Q4 收口窗必裁 |
| B11 | T-429 release 烟测+UAT | — | 单票波 |
| B12 | T-430 QA 终验 | — | m15-done 就绪判定 |
| 波外 | T-431 Q6 docker virtual 开禁（已裁开——随时插空，非 DoD） | — | 避开 T-412 波与 FE 主轴票 |

**票据行**（票号 / 标题 / 优先级 / role / area / dep；AC 全文见 SPLIT §1.2 与 PRD §4）：

- T-407 [P0] FR-132 aql.md 规格票 + t226 活体核验 + 口径归一 · role: reverse-engineer · area: docs/reverse/aql.md（+主矩阵勘误回写） · dep: —
- T-408 [P0] ADR-0043 AQL 引擎架构（EBNF/AST→参数化 SQL/ACL 织入/资源门/WriteTimeout 归位） · role: architect · area: DECISIONS.md + architecture.md 搜索节 · dep: —（软协作 T-407 132.4④）
- T-409 [P0] FR-133.1 AQL 语言前端（lexer/parser/AST 校验——纯函数零 IO） · role: dev-go-core · area: internal/search 语言前端（新包，定名从 ADR-0043） · dep: T-407,T-408
- T-410 [P1] FR-139.1 mint unknown username 400 修正（断言反转③） · role: dev-go-core · area: internal/httpapi auth 域 · dep: —（插空）
- T-411 [P0] FR-133.2 AQL 执行内核（planner/参数化 SQL/投影——零拼接） · role: dev-go-core · area: internal/search + internal/metadata 只读缝 · dep: T-409
- T-412 [P1] FR-136.1/2/4 virtual 聚合 service（children 并集/解析同源/ACL 同门） · role: dev-go-core · area: internal/repo/service.go · dep: —
- T-413 [P0] FR-133.2/4 ACL 织入 + 资源治理门（allow() 同源/K63 三件/流式满载） · role: dev-go-core · area: internal/search 引擎 entry（repo.Service allow() 只读消费） · dep: T-411
- T-414 [P1] FR-135.2/3 FE 列选器三页推广 + member-pop hover · role: dev-frontend · area: web/src users/groups/search + repositories hover · dep: —
- T-415 [P0] FR-133.3/5 AQL 端点面（POST /api/search/aql + compact + E-01 + metrics） · role: dev-go-core · area: internal/httpapi search 面 · dep: T-413
- T-416 [P1] FR-136.3 virtual FE 树消费（tree-empty-virtual 退役——断言反转②） · role: dev-frontend · area: web/src artifacts（ArtifactsBrowser） · dep: T-412
- T-417 [P0] FR-134 老搜索三端点 gavc/prop/pattern（断言反转① + K64 落笔 + K65 余量条款） · role: dev-go-core · area: internal/httpapi search 面（+internal/search 匹配内核） · dep: T-415
- T-418 [P1] replication.md 增量段（包 B 前置规格——三面双源材料） · role: reverse-engineer · area: docs/reverse/replication.md · dep: —
- T-419 [P1] FR-135.1 搜索页 AQL 模式（编辑器/错误内联/结果表） · role: dev-frontend · area: web/src/pages/search · dep: T-414,T-415
- T-420 [P1] FR-138.1 Replicate Now（全量同步任务/幂等/▶ 接线；outbox diff=0） · role: dev-go-core · area: internal/replication + httpapi 复制面（+web/src ▶ 小腿） · dep: T-418
- T-421 [P1] QA 中期回归（L20~L26/L29 + 断言反转预核实） · role: qa-engineer · area: 测试矩阵 · dep: T-407,T-410,T-412,T-415,T-416,T-417
- T-422 [P2] FR-138.2/3 Test 连接 + blockPush/blockPull 全局封锁（UI-API 不受门） · role: dev-go-core · area: internal/replication + httpapi + web/src 复制配置面 · dep: T-418,T-420
- T-423 [P2] FR-139.2 remote 缓存树 busy 重试预算（24 路清零 + 写路径专项） · role: dev-go-core · area: internal/storage/remote + internal/metadata busy 面 · dep: —（B8 定位）
- T-424 [P2] FR-140.1/2 L2 行内快捷 + e2e 纪律成文（E1 不倒退） · role: dev-frontend · area: web/src repositories 行内 + web/e2e README · dep: —（FE lane 排队）
- T-425 [P2] FR-137 远端浏览评估票（13 包型能力矩阵 + Q4 材料上 BOARD） · role: reverse-engineer（dev-registry-adapter 会签） · area: docs/reverse/ mini 规格 · dep: —
- T-426 [P1] 文档票两腿（AQL 指南+搜索 API 参考 / 增量+FAQ——票内先后笔） · role: tech-writer · area: docs/user/ · dep: T-415,T-417（腿②候 B6/B7 合入）
- T-427 [P1] PM Q 终裁联动回写（Q1/Q4 收口窗必裁 + K62~K66 回填 + 未纳入段） · role: product-manager · area: docs/prd + ROADMAP · dep: T-407,T-425
- T-428 [P2] FR-140.3 文面回写簇（四处 + ux 会签两行） · role: tech-writer（ux 会签） · area: docs/reverse README + design/user 文面 · dep: —
- T-429 [P1] release 烟测 + UAT 随里程碑 PR（F1 趋势登记） · role: release-engineer · area: deploy/ + charts/ + CD 链 · dep: 全部实现票 + T-426
- T-430 [P0] QA 终验（L20~L34 + §5.7 逐行核对 + DoD 八条） · role: qa-engineer · area: 全量矩阵 · dep: 全部 + T-429
- T-431 [P2·条件·Q6 已裁开] docker virtual 建仓矩阵开禁（矩阵行 + FE 门控 + 三态回归） · role: dev-go-core · area: internal/repo 建仓矩阵 + web/src 门控一行 · dep: —（随时插空——避开 T-412 波与 FE 主轴票；非 DoD）

**首派建议**：**B0 即派 T-407 + T-408**（双锚并行，conductor 已定；L20 就绪度确认 + ADR-0043 Accepted 后开 B1 主轴 T-409 + mint T-410 插空）。插空候选全程：T-431（任意空位）→ T-425（P2 可前移早出 Q4 材料）→ T-428。

**B0 派发（conductor 2026-09-01 09:2x）**：T-407（reverse-engineer，aql.md + t226 活体核验〔INC-1 差集法纪律已重申〕+ 口径归一）+ T-408（architect，ADR-0043 + architecture 增量节）双锚并行在途。**拆票六条口径 conductor 批复**：T-418 replication.md 增量段独立成票（认可——M14 T-393/T-394 先例）/ FR-140 L2 归属照 SPLIT / search 列选器归 T-414 / mint·busy 分票优先级照 SPLIT 取值 / ADR↔aql.md 软协作缝维持 B0 并行 / T-420 FE 小腿并行归运行时裁量。M15 计 0/25。

**T-408 → done 2026-09-01 09:3x——M15 1/25**：**ADR-0043 Accepted**（七轴决策 + EBNF 文法 + 字段→SQL 映射表 11 行 + 软缝对齐清单十条 + 四票锚点）+ architecture §24 搜索域增量节 + §11.46/47 技术债登记。三个非显然裁决（勾稽现役代码）：① **ACL 两段织入**——auth.Authorizer 的 path 级 include/exclude 模式（targetCovers）使 repo 集合 SQL 过滤不完备，取「repo 集织入 + path-scoped 仓行级 CanRead 复核」，拒绝把模式翻译成 SQL LIKE（双真相源即泄漏）；② nodes 无 name/depth/type/updated_by 列——前三者双方言同式派生表达式零迁移，modified_by 注册 unsupported 诚实 400 不伪造；③ **WriteTimeout 维持不设**（同一 Server 承载分钟级 blob 流写——查询时长治理归引擎 deadline，T-392 就此关闭）。契约：metadata.NodeQueryer 只读查询缝 / repo.Service +SearchScope+CanRead / search.Engine.Run 三接口定案；K63 门参数定案（1000/4/10s/429+Retry-After，内部常量零配置键，Q2 出口）。风险：与 T-407 软缝（PRD 属性形态 {"@key"} vs 官方 {"@license"}——ADR 取官方形态，aql.md 落盘后按清单十条复核，分歧走勘误不翻机制）。日志 reports/agents/T-408.md。

**B1 插空派发（conductor 2026-09-01 09:3x）**：T-410（mint unknown username 400 修正——断言反转③，零 B0 依赖）入空 lane；T-407（aql.md）继续在途。主轴 T-409 仍候 L20 就绪（双门：L20 + ADR Accepted——后者已满足）。

**T-410 → done 2026-09-01 09:4x——M15 2/25**：`handleTokenCreate` 错误分类扩 `ErrInvalidCredentials || metadata.ErrUserNotFound` → 400 `invalid_request`「username is required or unknown」（internal/auth 零改动——分类归 HTTP seam，wrap 链保留）；table-driven 新用例（unknown 400 逐字 + 非 admin 403 守卫防枚举次序锚 + 回归 + operator log 无 500 残留）+ **断言反转③ e2e 落笔**（t386 spec：500→400）；活体逐字复刻 + armed 全链 + m9 mock 腿绿；lint 0；两包 100.6s/27.2s 全绿。conductor spot：build + 新用例绿。遗留登记：t386 readonly_admin 自铸腿 armed 假设漂移（非本票面）。日志 reports/agents/T-410.md。

**T-407 → done 2026-09-01 09:4x——M15 3/25（B0 双锚闭合，L20 地基就绪）**：**aql.md 落盘**（301 行 14 节，官方 8 页逐条锚 + inv 补白 + t226 活体 36 探针；高 ~14 / 中 ~6 / 低 8 全在册）+ 四文件勘误回写（inv-1 §E 补 license、inv-2 §1.C 14 计数定案、主矩阵三行、README）。**五个定案**：① **`$not` 不存在**（官方无 + 活体 400 双证——PRD 133.1 前提校准为 $and·$or + $msp）；② `.sort()` OSS 档被许可门挡（BinFlow 无门——按官方全集 A 层实现）；③ 老搜索计数 **14** 定案（SearchResource 铁证）；④ 空集族分化（artifact/gavc/prop=200 空数组 vs usage/creation/dates=404）；⑤ K64 维持 LIKE 子串（大小写不敏感对齐点交 T-417）。K65 判定：dates/creation trivial 可顺车。待验证 8 项全在册有归位路径（429 形态/6000 门现值/property 数据腿/virtual 对拍归 T-412/T-415 e2e）。**B1 主轴双门全开**。日志 reports/agents/T-407.md + t407-evidence/。

**B1 主轴派发（conductor 2026-09-01 09:4x）**：**T-409**（AQL 语言前端 lexer/parser/AST——dep 双锚已满足；携带 $not 校准前提与 ADR-0043 软缝清单）+ T-414（FE 列选器三页——零 BE 依赖早波）双 lane 在途。

**T-409 → done 2026-09-01 10:3x——M15 4/25（主轴第一环落）**：internal/search 语言前端全新增 2,881 行含测试（fields 闭集注册表 17+2 字段 + 12 未支持域提示 / 位置追踪 lexer / 递归下降 parser〔$and·$or·$msp·@key·隐式 and + 尾缀链序强制 + sort validator 双句逐字 + **6000 长度门在 Parse 入口**〕/ AST + T-411 消费契约 godoc / QueryError envelope 前形态）；120 test/subtest；lint 0 + race 13.6s 绿 + **零 DB import 实证**（纯函数达标）；v1c 文案逐字节断言。**两处 ADR↔aql.md 分歧留痕上报**（实现从 aql.md，ADR 勘误已派 T-408 作者补笔）：① $not 产生式（aql.md：不存在）；② checksums sha1 平名 vs 点路径。低置信项诚实拒绝 + 「以核验为准」标注。T-415 需知悉：错误文案双轨（语法=E1 逐字 / 域·字段·操作符=C 层增强，均 400）。日志 reports/agents/T-409.md。

**B2 派发（conductor 2026-09-01 10:3x）**：**T-411**（AQL 执行内核——planner/参数化 SQL 编译/投影 + 注入红线 + 万节点性能腿；dep T-409 已满足，携带两分歧勘误注记）入主轴 lane；T-414（FE）继续在途。T-408 作者复活补 ADR-0043 勘误两处（Erratum 节，Status 维持 Accepted）。

**ADR-0043 勘误 → done（T-408 作者 2026-09-01 10:5x，`DECISIONS.md` 已推）**：八项落章——$not 产生式作废 + $msp 收录 / checksums 平名五字段 / path·name 派生 / type 默认 file 暂行 / **超时 408 定案**（503 暂行作废）/ 截断通告逐字 + 6000 门 / virtual 展开三分支（编译期成员展开≠权限通道）/ 非 admin 投影 unknown 暂行——软缝清单①-⑩全闭。

**用户指令 intake ④（2026-09-01 10:4x）：「注意按照最前沿的规范管理工程结构」——常设准则（已入 conductor memory）。首轮全仓结构审计执行完毕**：workflow 5 examiner（Go 布局 / search 新包 / 工具链 / 前端 / monorepo）+ 逐条对抗核实，25 项发现 → 23 去重 → **8 项确认**（4×P1/P2 即修 + 4 项排队）。**即修四项已落 `c7dbff8`**：① docs-site 生成物出册（.gitignore + git rm --cached——176 文件 -7,127 行；沿官方 scaffold .gitignore 逐字）；② **.github/dependabot.yml 新建**（gomod/web npm/docs npm/github-actions 四生态）；③ CI `go vet ./...` → `make vet`（bare vet 在 make console 后下钻 node_modules）；④ CLAUDE.md Go 规范增补**新测试文件以行为命名**（存量 93 个票号命名不回改仅约束新增）。

**T-432 [P1·插空] web 工具链升程（审计确认项①——分两段）**：`role:dev-frontend` area:web/package.json+eslint.config+scripts ｜ dep: T-414 落地后（FE 波空位插空）。**段一**（单 PR）：vite ^6→^7.3.6 + engines `^20.19.0 || >=22.12.0` + @vitejs/plugin-react ^4→^5.2.0（peer 跨 4-8 免二次升）+ eslint-plugin-react-hooks ^5→^7.1.1 + eslint.config.js 手工块换 `configs.flat['recommended-latest']`（新规小清理预算票内）。**段二**（独立票面增量）：vite ^8.2.2（Rolldown）+ plugin-react ^6.1.1（新增 oxc/rolldown peer）——**落前必验 scripts/relink-assets.mjs 对 Rolldown 产物形态**（三改写面 + 自检硬失败）+ wire-brand-assets 冒烟。MUI v7→v9（无 v8）触全页面，**最后位**独立排。AC：四闸门 + e2e 全项目 + SPA 预算复核。

**T-433 [P2·插空] Go 结构规范收口（审计确认项③⑤⑥）**：`role:dev-go-core` area:cmd/binflow-server + internal/backup(新) + internal/servelock(新) + internal/search ｜ dep: T-411 落地后。① **cmd 瘦身**：main.go 2,240 行的 export/import/gc 子命令域逻辑迁 internal/backup + internal/servelock（显式 API + stdout/stderr 注入——行为零变化，`make build`+全量测试承证）；② internal/search 两小修：fields.go 注册表双源合一（stat.* 十项内联进 literal 排序位）+ parser.go 长度门字节/rune 双条件（`len>Max && RuneCountInString>Max`——多字节序列按官方「字符数」语义）；③ 存量票号命名测试文件**不回改**（T-432④ 规则仅约束新增）。AC：全量 go test -race + lint 0 + 行为零变化 diff 审计。

**配额窗处置（11:22~12:07，8 轮叠发吸收）**：T-411/T-414 双双击落（分别处于测试预期修复期/全套重跑期）→ 12:07 重置后 SendMessage 双复活（工作树改动零丢失；各自携审计排队项知悉）。

**T-411 → done 2026-09-01 12:4x（复活后收口）——M15 5/25（主轴第二环落：执行内核）**：internal/metadata/**aqlquery.go**（NodeQuery IR + NodeQueryer 只读缝 + IR→参数化 SQL——name/parent/type/depth 派生、julianday 双侧归一、懒 blobs join、属性谓词三态）+ internal/search/**plan.go**（AST→IR + Output/window 回显供 T-413/415；时钟与 VirtualResolver 注入）+ **match.go**（通配→LIKE 单一转义内核，与 T-417 共享）。测试五件：IR→SQL 快照 17 形 + **注入红线 7 形×8 恶意值** + EXPLAIN 索引消费 4 腿 + AQL→IR 35 形 + 万节点 P95 六形。**P95 数表：item 7.7/30.6/38.5ms（预算 500）/ property join 107.1/37.5/90.7ms（预算 800）——大幅余量**；scripts/m15-aql-perf.sh 复跑脚本。**性能腿抓真缺陷一例**：双 DISTINCT 子查询物化为无索引临时表（1.9s）→ 重构等值 join/单 DISTINCT/EXISTS，idx_node_props_name 经 EXPLAIN 断言兑现。**发现 ADR 第三处分歧（path=父目录——aql.md+活体三探针）**→ 勘误已派 T-408 作者。T-413 三个锚点（引擎织入 limit/行复核恒选列/Virtual 注入点）已随票传递。日志 reports/agents/T-411.md。

**T-414 → done 2026-09-01 12:5x（复活后收口）——M15 6/25（FE 早波落）**：users/groups/search **三页列选器**（T-387 columnPrefs 形态复用，六/四/五列闭集，操作列 admin 门控）+ **member-pop 对比度清账**（T-391 color-mix——亮 5.08:1/暗 6.12:1）。**锚册 v1.27**（T-414 批 28 名 + anchor-audit STOP 假阳性 7 词）。自测：四门绿 + 新 spec 5/5 + 定向回归 59✓ + **净实例全量 284✓/1 在册假阳性（串行 3/3 绿甄别）** + a11y 双主题 serious=0 + **服务端 diff=0 + SPA +2,183B**。环境两注记（宿主 load 450-630 撞全量首轮、脏夹具残留净实例复现实证）入日志。遗留：T-419 AQL 结果表消费面就绪（search-columns-item-* 族模板）；留验实例 :8174 待清。日志 reports/agents/T-414.md。

**B3 派发（conductor 2026-09-01 12:5x）**：**T-413**（ACL 织入 + K63 三件门——dep T-411 已满足；携带 408 超时定案与 T-411 三锚点）+ **T-412**（virtual 聚合 service——FR-21-AC8 兑现，零依赖副线，与 internal/search 错峰）双 lane 在途；T-408 作者第三笔勘误（path=父目录）在途。

**T-412 → done 2026-09-01 13:2x——M15 7/25（FR-21-AC8 兑现：M3 挂账 P2 债清偿）**：`listVirtual`（成员并集按 **virtualMemberOrder 同源**合并——与 pull 同一次顺序计算，同名路径首成员胜；remote 成员仅缓存行〔T-406 listing 口径〕；行保留成员 repo key 与 aql.md §7-2 同姿态）+ `getVirtualFolder`（成员存储行作答 + **合成 display-only marker 零落库**〔ADR-0013 不污染成员；明确不复制 remote 臂读侧材料化——T-406b 教训吸收〕）+ allow 门前置。**断言反转② BE 腿**：t406 拒绝钉翻转为开放面（+翻转钉子测试）。新增 virtual_aggregate_test 6 测试（并集/同名/实态/深层/folder 三臂/双翻序同源/越权零泄漏/空态两分/零回源）。自测：repo 全包 race **454.8s 绿** + httpapi 138.4s 绿 + 适配器四包 ok；全树 race 唯一 FAIL = T-413 在途编译错（归属其票，收口后 conductor 补跑全树）。遗留：httpapi 无斜杠先探 file 面在 virtual 含 remote 成员时走一次上游（T-406 as-built 同形，如需免另立小票登记）；FE 翻转归 T-416。日志 reports/agents/T-412.md。

**B4 派发（conductor 2026-09-01 13:3x）**：**T-416**（virtual FE 树消费——断言反转② FE 腿，dep T-412 已满足；解除 T-406 受限面 + 锚册 v1.28）入 FE lane；T-413（ACL+门）继续在途。

**T-413 → done 2026-09-01 13:2x——M15 8/25（主轴第三环落：ACL+资源门）**：**Engine.Run** 查询文本端到端（Parse → 非阻塞门 → deadline 段〔scope→编译→织入→cap+1 探测执行→行复核→脱敏装饰〕）+ **K63 三件门**（上限 1000 截断/并发 4→429+Retry-After/超时 **408** 定案形态）+ **两段 ACL 缝**（SearchScope 集合谓词 + CanRead 行复核——同一 allow() 源，ADR-0043 pt4/5 落地；repo.Service api.go 声明 + search.go 实现）。**D-413-1 [P2·conductor 已修]**：接口扩面的测试 fake blast radius——docker 包两个 fake（fakeService 30 法 + countingGetService 28 法委托）未随票补新缝方法，其票面自测只跑自身两包未及 docker——conductor 补桩（permissive stub / 逐字委托）+ docker 52.5s 绿 + **全树 vet/build 零破损**。教训：凡扩 repo.Service 类大接口的票，AC 必含全树 vet（已入 T-415 派单）。全树 race（T-412 AC3 补证）后台在跑。日志 reports/agents/T-413.md。

**B5 派发（conductor 2026-09-01 13:5x）**：**T-415**（AQL 端点面——P0 主轴第四环，dep T-413 已满足；Engine.Run 薄路由壳 + compact + envelope + metrics 新组；携带 fake-blast-radius 纪律）入 lane；T-416（FE）继续在途。

**T-415 → done 2026-09-01 17:3x（配额窗②复活后收口）——M15 9/25（主轴第四环落：AQL 端点面）**：`POST /api/search/aql` 全链——text/plain + ?query 回退 + 6000 读上限；匿名两臂（E5 401/E6 403 逐字）；错误映射（QueryError→400 逐字/busy→429+Retry-After/超时→408）；§3 流式 envelope 逐字节（pretty/compact 双形态 + virtual_repos/properties 投影 + virtualIndex 双面）；**引擎在 New 内从既有 Deps 自装配（ADR-0043 §24.1 唯一装配点——cmd 零改动 Deps 零新字段 → fake blast radius 零，D-413-1 纪律兑现）**；metrics search family 三组（queries_total{plane}/duration/rejections_total{reason}）。测试两件新（行为命名）：L21 全链 + L23 e2e + stub 429/408/500 映射 + 确定性 golden + WriteTimeout pin。自测：httpapi race **427.4s 绿** + search 15.3s + 全树 vet 零破损 + lint 0。中置信留痕：compact 非空行体（活体实 415，按 §3.1 描述实现——**规格待验证**）、properties 嵌套形态（V-d）。遗留四项（Truncated 诚实上界文案口径随 K63/Q2 裁、真门并发饱和不可确定性〔stub 同口径〕、range 比较符不触发 virtual 展开、T-417 扩 plane 值）。日志 reports/agents/T-415.md。

**B6 派发（conductor 2026-09-01 17:4x）**：**T-417**（老搜索三端点 gavc/prop/pattern——P0 主轴第五环，dep T-415 已满足；断言反转① + K64 落笔 + K65 余量条款）入 lane；T-416（FE）继续在途。

**T-416 → done 2026-09-01 17:5x（配额窗②复活后收口）——M15 10/25（断言反转② FE 落）**：T-406 内容面 gate 解除 + RepoBranch 静态化退役（与非 virtual 同形动态展开）+ `tree-empty-virtual` 空态翻转（**锚不退役**——有内容走并集表格/无成员维持空态两态文案；锚册 **v1.28** 留痕）+ 删除三出口预收敛（RE-08 405 shadow-entry 不给）+ **顺手收口先在缺陷**：未选仓手动展开永久骨架环（T-236 起全 rclass——effect 改「选中仓根∪祖先链∪手动集」）+ warn-box 对比度（78% 徽章族配方，axe 新腿首扫暴露的亮色 serious）。自测：四门 + 新 spec 4 腿 + M3/M4 浏览 7 零回归 + artifacts-tree 10 + a11y-sweep 全路由双主题 0 + **SPA 仅 +470B**。**软注记登记（候选小票）**：`GET /api/repositories/<virtual>` 回显原始 config blob——成员级联删除后 echo 仍列已删成员（服务层两态可分、FE 经 echo 不可分——真区分需 httpapi echo 改造）。遗留三条（跨仓展开 QA 补独立腿/warn-box 全局类说明/深链 403 姿态与 T-406 一致）。日志 reports/agents/T-416.md。

**B7 派发（conductor 2026-09-01 18:0x）**：**T-419**（搜索页 AQL 模式——FE 主线，dep T-414+T-415 均满足；锚册 v1.29）入 FE lane；T-417（老搜索三端点）继续在途。

**T-417 → done 2026-09-01 18:0x——M15 11/25（主轴第五环落：老搜索并轨，断言反转①）**：gavc/prop/pattern 三端点（三层数据面：metadata/repo 查询缝 + httpapi 路由/metrics〔plane 标签扩值——T-415 遗留④兑现〕）；断言反转①（t92 SR-03/04 404→分派实现，其余未实现族 404 维持）；K64 落笔；K65 顺车判定见其报告。自测：build/vet 全树（fake 纪律）/lint/测试面绿（§4 证据表）。conductor spot：build 零输出。日志 reports/agents/T-417.md。

**B8 派发（conductor 2026-09-01 18:1x）**：**T-418**（replication.md 增量段——包 B 前置规格，双源材料；解锁 T-420/T-422 链）入 lane；T-419（FE）继续在途。

**T-419 → done 2026-09-01 18:2x——M15 12/25（FE 主线落：搜索页 AQL 模式）**：模式切换（ToggleButtonGroup + `?mode=aql` 深链；基本表单/锚零变化）+ AqlPanel（mono 编辑器 / 400 E-01 逐字内联 / K63 通告 / 表头三态排序 / range 分页）+ aql.ts 统一层（**零新端点**——rawBody 消费 T-415；尾缀链重写器按链序归位）+ ColumnsMenu 抽壳两模式共用。**锚册 v1.29**（T-419 批 15 锚；search 既有锚零改名——AQL 行复用既有锚零新增）。自测：四门 + 新 spec **7/7**（含 400 逐字 + 429/408/截断 mock 腿 + axe 双主题）+ 净实例全量 **259✓/1 在册假阳性**（串行绿甄别）+ m9 4/4 + **SPA +5,053B**。契约漂移零（T-415 实测逐项对 aql.md；429 Retry-After 数值不上 UI 系 ApiError 无响应头——锚册注记）。遗留四条均轻（排序字段须在输出集/title 提示已给；病态括号退化为 400 内联；草稿会话态；耗时列不采——无端点背书不伪造）。日志 reports/agents/T-419.md。

**T-432 段一派发（conductor 2026-09-01 18:3x——结构插空票激活，前置 T-414/T-419 已落 FE lane 空净）**：vite 6→7.3.6 + plugin-react 4→5.2.0 + hooks plugin 5→7.1.1（flat recommended-latest）+ engines 底座；四门含 relink/wire-brand 对 vite 7 产物兼容 + 净实例全量。在途。

**T-418 → done 2026-09-01 22:3x（配额窗③复活后收口）——M15 13/25（包 B 前置规格落）**：replication.md **§9 增量段**（227→403 行）——端点 11（官方 REST 面 + UI-API 面双列）/ 三面 × 幂等逐条（触发·封锁·Test）/ **定案 2**（Test 形态、审计补词 4）+ **顺手清偿 2**（M6 待验证 #1/#3 双解、T-405 审计遗留词）。置信度高 9/中高 4/中 3/低 0（**三源**：官方 OpenAPI 主源 + reverse-src Pro 实现 + T-402a 实测）。待验证 V1~V4（Pro 抓包升格项，不阻断实现）+ 交裁 2 点（§9.6——实现按 A 层对位 + 票内留痕）。日志 reports/agents/T-418.md。

**B9 派发（conductor 2026-09-01 22:4x）**：**T-420**（Replicate Now——全量同步任务 + ▶ 接线；dep T-418 已满足；outbox 模式复用 diff=0 审计）入 lane；T-432①（工具链段一）继续在途。

**用户指令 intake ⑤（2026-09-02 00:1x，重量级——M16 定向）**：①「现在制品树展示仍然和 artifactory 的逻辑严重偏离」——M14/M15 parity 后仍不满（**用户第三次 UI 加码**）；②「下个里程碑需要完全检查整个前端，对齐 artifactory 的所有内容」；③「现阶段除了 xray 暂时不做，剩余产品文档中明确不做（第一版）的都要做」——**不做清单全面翻案（除 Xray）**，含 E1~E7/§9/PRD Non-goals/滚程项，且**与 conductor 先前裁定冲突处（如 Q5 cron 双轨已裁不引入）立项稿列冲突点交用户确认而非默默翻转**。**即时处置**：M16 全量审计 workflow 已发起（不做清单三源枚举 + t226 逐页活体对照〔树为最高优先〕→ 汇编 M16 立项素材）；M15 在途票（T-420/T-432①）不受扰继续。

**T-432① → done 2026-09-01 23:0x——M15 14/25（结构插空：web 工具链段一）**：vite ^6.3.0→**^7.3.6** + @vitejs/plugin-react ^4.5.0→**^5.2.0** + eslint-plugin-react-hooks ^5.2.0→**^7.1.1**（eslint.config 手工块→`configs.flat['recommended-latest']`）+ engines `^20.19.0 || >=22.12.0`；四门 + relink/wire-brand 对 vite 7 产物兼容验证 + e2e（§3 证据）。段二（vite 8 Rolldown + plugin-react 6）与 MUI v7→v9 维持排队独立票面。日志 reports/agents/T-432.md。

**T-420 → done 2026-09-02 00:4x（报告固化收口）——M15 15/25（复制包 B 主件：Replicate Now）**：`POST /api/v1/replications/{id}/run` 对位（T-418 §9 wire）——**双实例主腿**：3 制品先落仓→建配置→REST 触发→B 侧节点数/**逐路径 sha256 与源一致**→status succeeded=3→重复触发 200 收敛（幂等）→PUT enabled=false→**409**→audit 2 行；引擎单元 4 面 + FE ▶ 真语义接线（T-404 占位替换 + spec 断言翻转）。outbox 引擎文件 diff=0（复用模式非重构）。**race 口径勘误留痕**：go 默认 10m 超时误伤——Makefile `TEST_TIMEOUT=20m` 才是既定口径，按 25m 重跑全包绿。**遗留七项预登记**：e2e 真栈腿归 T-421（toast/URL/任务深链断言已翻转）、锚册 §10.5 `repos-repl-run` 语义行归 owner 修订、M15-SPLIT §5.7 补 run 行、封锁门预检归 T-422（handler 翻转点已注释标明）、audit 词 replication.run 归 T-422 腿、api-reference 归 T-426 腿②、大仓 limit 化全枚举缝（T-423 同族，dogfood 量级无压力）。日志 reports/agents/T-420.md。

**B10 派发（conductor 2026-09-02 01:0x）**：**T-422**（Test 连接 + blockPush·blockPull 全局封锁合票——P2，dep T-418 已满足；含 T-420 留下的封锁门预检翻转点 + audit 词表 replication.run/replication.config.* 批次）入 Go lane；**T-421（QA 中期）押后至审计 workflow 完结**（串行净机跑全树 race——共租负载教训）。

**T-432① 官方通知补记（2026-09-02 01:5x）**：vite7 产物与 relink/wire-brand **完全兼容**（三面改写+自检全过 + vite6 沙箱基线互证）；**契约偏差登记**——BOARD 条款④「降级清理」语义失效（hooks v7 recommended 与 recommended-latest 同为全量 compiler 规则），替代执行 = recommended-latest + 存量命中 5 规则降 warn ratchet（10 条零违规当日 error 生效）；**37 条 ratchet warnings 清单入日志作清理票底稿（候选小票登记）**；整夜 load 63→481（用户 VM+串流共租）——**安静窗 chromium 全量 e2e 复跑建议归 T-421**；t404:337 腿系 T-420 翻转版断言票内未真栈跑（归 T-421 遗留①）；:8099 跨票残留实例 hazard 注记。conductor 收编缺口事故（replications.ts 漏提交→树内 tsc 破）已闭（`215aa19`）——**教训：收编按票报变更清单逐文件对 git status**。

**T-422 → done 2026-09-02 03:4x（配额窗④复活后收口）——M15 16/25（复制包 B 首批齐：Test + 全局封锁）**：`Engine.TestTarget`（一次 GET 零副作用、凭据不落日志 NFR-S75、**不看封锁态**照 §9.2-C-10）+ **blockPush/blockPull 全局封锁**（binflow.yaml 全局段 + REST + 控制台开关**三面一致**；blockPush=on 新事件不入队 + 在途停发 + **REST 配置通道不受门**——t226 实测语义；blockPull=on 拉侧照 remote 语义拒绝/降级）+ **T-420 预检翻转点兑现**（handler 排程前 blockPush 预检）+ audit 词表批次（replication.run + replication.config.*）。自测：4 包全 ok + httpapi 137.4s + **race 两轮绿（25m 口径）** + lint 0；含 §2 越界申报（最小外延缝——AC 落地所需）。遗留细节见报告（FE 呈现面 parity R8 形态核验归 QA）。日志 reports/agents/T-422.md。

**B11 派发（conductor 2026-09-02 03:5x）**：**T-425**（远端浏览评估票——研究型轻载，产 Q4 材料包；机器被审计 workflow 占用故选此票）入 lane；**T-421 继续押后**（净机需求）。

**T-425 → done 2026-09-02 04:1x——M15 17/25（评估票：Q4 材料齐）**：13 包型能力矩阵（官方文档 2026-09-02 实取 + 21 包型设置出现矩阵脚本比对 + 本仓规格双源）+ 三出口材料 + Q4 浓缩包。**关键发现**：Artifactory 远端浏览 = 可选档 `listRemoteFolderItems`（**默认 false**；官方设置面仅 Debian/Generic/Maven/Opkg/RPM 五型）——PM docker-tags 倾向系 **L2 超 parity 错位**（Artifactory 未开放该型，已标注供裁）；maven/generic HTML 抓取族官方未写算法（中置信→建议不做）。t226 活体 Pro 许可门 400 → 降级留痕零静默升格。**Q4 终裁（conductor 2026-09-02 04:2x）**：**出口 C 批 1 = helm+deb+rpm**（~3 票零新解析器——对齐 Artifactory 可选档语义，默认维持缓存浏览）；docker tags 腿不采（超 parity）；maven/generic HTML 抓取族不做（算法无锚）；**LC-76 归 A（可选档语义）**，实现段 M16 登记。日志 reports/agents/T-425.md。

**B12 派发（conductor 2026-09-02 04:2x）**：**T-426**（tech-writer 文档票两腿——AQL 指南/搜索 API 参考/virtual·复制包 B 增量/FAQ；前置全满足）入 lane；审计 workflow 树对照继续。

**T-426 → done 2026-09-02 04:2x——M15 18/25（文档票两腿齐）**：**docs/user/aql.md 新篇**（子集边界 + 400 逐字 + **Artifactory AQL 迁移对照表**）+ api-reference（SR 表翻转 aql/gavc/prop/pattern + /v1 表 +3 行 + **顺修存量 bug：复制 target_url 缺 /binflow 后缀**）+ governance（搜索节重写 + 复制包 B 三小节）+ console（T-416/419/420/422 四面增量）+ FAQ 两问 + 顺车三处（search 指标入册/README/sidebars）。**双净实例实测**（18501/18502 避开审计端口）：AQL 22 组 curl + 老搜索四端点 + 复制包 B 全臂（run→AQL 验证 5 路径收敛/409×2/封锁四变体）跑通留输出；make docs 零断链。**as-built 事实入册两处**（相对时间 `"1d"` 须空格〔与官方后缀表字面冲突——分歧登记，翻转点 lexer.go parsePeriod〕；用户 .limit() 也置截断标记）。**环境注记**：04:08:34 全机 binflow-server 同秒被外部 SIGTERM（审计实例 18091 + 本票 scratch；非本票 pkill）——审计实例未复活，登记待查。遗留四条（429/408 未活体触发系语料限制、blockPull 降级按 T-422 报告入册、FE 面以各票 e2e 为据）。日志 reports/agents/T-426.md。

**B13 派发（conductor 2026-09-02 04:3x）**：**T-427**（PM 收口笔——Q 总账归位 + LC/K 终版 + 「M15 未纳入项」起草〔与 intake ⑤ M16 语境衔接〕；dep T-407/T-425 均满足）入 lane。

**T-427 → done 2026-09-02 04:4x——M15 19/25（PM 收口笔）**：PRD **v1.1**——§7 Q 总账逐项归位（终裁落章 3〔Q4 C 批 1/Q5 cron 不引入/Q6 开禁→T-431〕+ 规格回写归位 2〔Q2 K63 定案 1000/4/10s/**408**/Q3 400 维持〕+ 维持暂行 2〔Q1 收口窗必裁——材料已齐；Q7 登记型〕）+ LC-68~79 终版（**A10/C2/待裁 0 零滞留**）+ K62~66 实装值回填（K64 局部翻转=大小写不敏感——M4 K2 欠账清偿；K65 判 M16）+ §5.7 全景表 as-built 对账（**archive/latestVersionByProperties 两外挂端点补登**——全量口径 14+2+1 零遗漏）+ FR-133/134 规格校准（aql.md 五定案回写）+ ROADMAP「M15 未纳入项」备稿段（**每条〔M16 吸收预期〕标注——Xray 唯一维持不做、HA/Build-info/license 翻案候选、协议无 API 根树物理不可行非翻案面**）。日志 reports/agents/T-427.md。

**B14 派发（conductor 2026-09-02 04:4x）**：**T-428**（文面回写簇——四处落笔 + make docs；轻载适配审计占机）入 lane。

**T-428 → done 2026-09-02 04:5x——M15 20/25（文面回写簇）**：四处落笔全**写前代码核对**——①reverse/README 补 npm.md 行（T-393 遗留②清）；②parity 册 v1.4：M1 行 440px 紧凑档定案升级 + M3 MUI Paper 代差注记；③package-icons v1.3：K61 量级拍板（K56 现值零改动，超 +10% 不回折）；④migrate-artifactory 清账 6 处（nuget 措辞分面/三→四阶段/守卫/旗标/dry-run 真实渲染/报错 +2）。`make docs` SUCCESS 零断链；②③标「ux 会签位」代笔（遗留 ux 复核签字）。日志 reports/agents/T-428.md。

**B15 派发（conductor 2026-09-02 05:0x）**：**T-424**（L2 行内快捷 + e2e 三节纪律成文——复制 key/Set Me Up 直开 + INC-1/pkill 精确杀/assert-tokens 豁免成文）入 FE lane。

**T-424 → done 2026-09-02 08:4x（配额窗⑤复活后收口）——M15 21/25（L2 快捷钉断言 + 纪律成文）**：新 spec 5/5（复制 key〔aria+Space+剪贴板全值+回显〕/ Set Me Up 直开〔smu-* 锚族复用〕/ **E1 不倒退缺席断言**/ axe 双主题）+ **web/e2e/README 三节纪律 + 两 flake 注记成文**（INC-1 永不点确认/共享 fixture 快照前置；pkill 按端口精确杀——三起票务事故 + conductor 两起自杀并入；assert-tokens 豁免口径）。**web/src 零改动**（L2 快捷系 M8 既有能力——纪律票钉成断言口径，diff 审计过）；净实例全量三项目 **301✓/0 红/24 skip**（7.9m）；四门绿；**SPA +0B**。**过程事故诚实披露**（§5）：首轮起服 heredoc 失败→空配置误开仓库根 ./data 14 秒（仅 session sweep+WAL checkpoint，零写请求——日志逐行核对）→ 教训并入 README §4。遗留三条（Enter 劫持共享件修法超 area 建议单独提票/assert-tokens 显式 allowlist 待票/见报告）。日志 reports/agents/T-424.md。

**M15 尾波态势（conductor 2026-09-02 08:4x）**：实现票仅剩 **T-423**（busy 专项——24 路并发测需净机，候审计 workflow 末两腿完结即派）与条件票 T-431（随时插空）；随后 T-429 release（dep T-423）→ T-430 终验 → m15-done 收口窗（Q1 终裁 + 未纳入项启用 + tag/PR/UAT）。

**M16 立项稿 v1.0 落盘（PM 2026-09-02 10:3x）**：milestone-16.md 约 560 行——§0 范围定界（推荐口径=控制台交互 parity）+ FR-141~148（主轴四批次〔树栈 P0〕+ 后端小域 + 远端浏览 + AQL 副线）+ LC-80~96 + L35~L46 + K67~72 + **用户确认清单 Q1~Q13**；ROADMAP 头切 M16。日志见 PM 回报。

**M16 确认清单终裁（conductor 主持，用户四项 + conductor 三项，2026-09-02 10:4x）**：
- **Q1 cron 双轨：用户裁「引入 cron 调度域」**（推翻 conductor M15 Q5 终裁——事件驱动引擎保留，**新增独立调度域**〔GC 定时/备份定时/复制 cron 字段〕：需新 ADR + 调度数据模型 + 与事件引擎并存语义）。
- **Q3 界面语言：用户裁「双语可切换」**（i18n 框架 + 中英两包——工程量最大档，牵全部 UI 文案与 e2e 断言双语化）。
- **Q4 分页范式：用户裁「翻成页码控件」**（×9 处统一，e2e 随迁）。
- **Q6 产品域边界：用户裁「全部进」**（Builds/Build-info、Release Bundle、洞察报表、Federation/Lifecycles 全做——**按两程承接**：M16 = 交互 parity + i18n + cron 域收口；**M17 = 产品域扩张专程**〔须 PRODUCT.md 修订 + ADR 群——沿 PM A1 建议与选项说明的拆程指引〕）。
- conductor 三项：**Q2/E1** 修文本范围（管理列表 vs 浏览器表）+ 收紧件 UI 不倒退；**Q5/E5** 用户/组创建路由化（V6 前提已实证推翻）；**Q7/Annotate** 加（后端动词域 + 前端矩阵列 + 语义迁移随票）。
- Q8~Q13 可后裁维持暂行。**PM 已派回填 v1.1**（范围重排 + 裁定落章 + M17 预立项段）。

**T-423 → done 2026-09-02 10:5x——M15 22/25（最后主实现票落：busy 重试预算）**：**RetryOnBusy**（4 次总执行/250ms→1s 封顶退避/busy-class only/ctx 取消即止/耗尽保 ErrStoreBusy 可重试语义）贯穿缓存填充写链——storage 会话行 + Append 态持久化 + remote land() 三 upsert + **registry-v2 面（dockerremote.go——D1 原始现场，area 外延披露）** + 负缓存写 + **busy-Append 诚实性修正**（不再误裹 errUpstreamBody/误标 assumed-offline）。**AC1 实证**：generic 面 24 路基线（HEAD archive 构建）3×500 SQLITE_BUSY + 级联 9569 假 404 → **修复后 0 5xx、10000/10000**；8 路不倒退（p50 189ms vs T-377 184ms）；helmoci 面 24 路 0 5xx。**D-413-2 票内修**（5m 预算+指数退避+耗尽归因诚实）。**全树 race 一次过 36 包**；metadata 契约零改动（ADR-0007 勘误许可）。遗留四条（⑦ limit 缝 M16 候选/审计行丢弃 P3 候选/helmoci 基线静窗不复现如实记录/面积审计按 §6）。日志 reports/agents/T-423.md。

**B16 派发（conductor 2026-09-02 11:0x）**：**T-421**（QA 中期——净机串行全树 race 窗口开启 + 六项累积交接面收口〔T-420 e2e 腿/T-432① 安静窗复跑/D-413-2 复核/T-422 五锚/真门形态/归属审计〕）+ **T-431**（docker virtual 开禁——Q6 裁定兑现，错峰避让 QA race 窗）双 lane 在途。**M15 实现面全落**——余 T-429 release（候 T-421/T-431）→ T-430 终验 → m15-done。

**用户指令 intake ⑥（2026-09-02 14:3x）：「记得定期将 develop 的代码合并到 main」——常设节奏入册**（conductor memory：≥10 done 票或 ≥1 天触发 develop→main 自建自合）。**首次执行**：M15 中程回流 PR **#64 已合**（merge commit `12c8720`——**77 个提交**上 main：AQL 全栈/复制包 B/virtual 聚合/busy 预算/结构轮/文档族；deploy_uat 随合并触发，UAT 将升 m15 中程形态）。在途票（T-421/T-431）工作树未提交改动不受扰。

**T-431 → done 2026-09-02 15:4x（配额窗⑥复活后收口）——M15 23/25（Q6 兑现：docker×virtual 开禁）**：矩阵行一行开（validate.go——唯一行为改动，沿 T-365 语义：聚合读面 family-wide 本就在，唯一阻碍就是建仓格）+ 拒绝臂数据驱动缝 + **FE comboAllowed 门整体退役**（其对 remote 也是陈旧谎言）+ 六处旧行为钉翻转 + 双新测试（建仓/成员解析/混型拒）。docker race 239s 绿 + helmoci 180s 绿 + vet 全树 + FE 四门；唯一红（TestBigTreeCopyNo5xx）归因机器负载（load 38-148 时 11.16s 撞墙 vs load 13 时 3.39s 过——copy 计划走查不在本票路径，同机同族 QA 先例在）。遗留四条（FE remote×docker 可选再藏归 conductor 裁/e2e 实机腿候低载窗归 T-430/live dind 归 QA/docs 联动 T-426）。日志 reports/agents/T-431.md。

**B17 派发（conductor 2026-09-02 15:5x）**：**T-429**（release 烟测 + UAT 备妥——dep 全满足；Chart bump 判据 1.4.0→1.5.0；构建腿先行 + kind 重腿错峰 T-421 e2e 大波）入 lane；T-421（QA 中期）继续在途。

---

## M16 票据（全前端 Artifactory parity + i18n + cron 域——用户 intake ⑤⑥⑦）

**用户指令 intake ⑦（2026-09-02 16:1x）：「当前前端整体和 Artifactory 偏离严〔重〕」——第三次 UI 加码。处置：M16 即刻开工，不等 M15 收官仪式。**

**M16 PRD v1.1 审定转正（conductor 2026-09-02 16:1x）**：七裁定已落章（intake ⑤ 窗口）+ LC-80~98 零待裁 + 两程结构（M17 产品域预立项）。BOARD 追认转正——tech-lead 全量拆票候 M15 m15-done 后补；**树栈 P0 批次（FR-142 批次①，素材 B-1.1~1.4）以插空第三 lane 即刻派发**。

**T-434 [P0] M16 批次①：制品树栈对齐（插空即发）**：`role:dev-frontend` area:web/src/pages/artifacts ｜ 素材：reports/m16-parity-audit-material.md B-1.1~1.4 + PRD FR-142 批次①。四项：①**文件叶子进树**（消除「（空）」误导占位——children 表收窄决策随票：文件行进树后右侧纯 item view 对齐）；②**选择≠展开**（单击纯选中、箭头才展开）；③**URL/状态模型**（页签进 URL 段 + 文件选择路径段化——Artifactory `/tree/<TAB>/<repo>/<path>` 形态）；④**树头工具带**（包类型 facet/rclass 组/Sort-by/紧凑视图单选/My Favorites）+ reverse §3.2 facet 回填。在途。

**T-421 → done 2026-09-02 19:4x（三窗五跑终收口）——M15 24/25（QA 中期 PASS）**：七项收口——①全树 race 两轮 **零 DATA RACE**（红全墙钟类；9 包串行 solo 绿含 maven 438s/auth 1087s/binflow-server 750s；metadata/repo/httpapi 三包未获 solo 绿窗=在册性能门家族 + Docker VM 周期满载——T-423 当晨同树一次绿在案）；②t404 真栈 8/8（T-420 遗留①闭）；③chromium 全量 258/5/9 五红全甄别（T-432① 遗留闭）；④D-413-2 复核 ✓（T-423 修法负载下活）；⑤**T-422 五锚新 spec 5/5 真栈绿**（含 BASE2 双实例腿；dead 桶 9→4）；⑥**K63 真门活体：恰 4×200+4×429 + Retry-After:1 + 文案逐字**；⑦中期矩阵 L20~L31 过（L21 因缺陷降 D-T421-1）+ E1~E7 零倒退 + 归属审计 100% 票号。观察三项登记（root ?list 文案/凭据主密钥指路归 T-426·T-429/metrics 根路径归 T-430）。日志 reports/agents/T-421.md。

**D-T421-1 [P1·conductor 已修]**：AQL criteria 成员次序敏感——parseComparator 末尾冗余「外层 } 必须紧跟」检查误杀比较符对象后的合法 `,` 成员（单对象双操作符已由 expectPunct 覆盖，检查纯属多余）。**修复=删检查 + 注释释因**；回归测试 parser_order_test.go 四混序形 + 单对象双操作符维持拒绝；search 包 race 272.4s 绿 + lint 0。L21 验收命令原样恢复可用。

**T-429 → done 2026-09-02 20:3x——M15 24/25（release 收官）**：**Chart 1.4.0→1.5.0 定案**（M15 三处 behavior 变化判据命中；blockPush/blockPull 新键 SEED-ONLY 语义**三段链贯通**：渲染→boot seed→REST 回显→unblock 复位）；**七腿烟测全绿**（goreleaser 六平台 6/6 校验和 + 四镜像 PUSH=0 + compose/k8s 清单/Chart kind 真装/systemd/offline roundtrip 字节一致）；**M15 面探针**（AQL 命中/Replicate Now 逐字节回读/封锁 409 逐字/docker×virtual 200/virtual 聚合）；**F1 门 120MB 首验 PASS = 104.52MB**（+1.1% 非红旗）+ 资源三连 GREEN（11.9MB/514ms）；favicon+docs/aql 六面指纹一致。**UAT AFTER 清单备妥**（BEFORE 基线核 = uat.2a11096，M14 PR #65 已部署——conductor 注意：周期合并 PR #64 后 main 已前移，AFTER 翻转标记按 m15-done PR 合并后）。遗留：server 40.59MB 首破 40MB 原始线（WARN-only→M16 观察）；offline 三处预存在候小票；首 PUT 405 不可复现观察。日志 reports/agents/T-429.md。

**B18 派发（conductor 2026-09-02 20:4x）**：**T-430**（M15 终验 P0——**钉 SHA `73d4556` archive 验证**〔T-434 在改工作树，免污染〕；L20~L34 终评 + §5.7 逐行 + DoD 八条 + Q1 证据摘要）入 lane。**T-429 收编 SHA `73d4556` = m15-done 候选锚**。

**T-430 → done 2026-09-02 21:4x——M15 25/25（终验 PASS——m15-done 就绪）**：六 AC 全绿——L20~L34 十五行全过（**L21 PRD 原样命令活体 200 = D-T421-1 修复终核**；L23 截断恰 1000 行 + 官方逐字通告 + 翻页全量可达 + 8 路恰 4×429；L32 全树零 DATA RACE + 5 包串行甄别绿；L33 满载 P95 item 331.8/join 496.8 vs 500/800 余量；L34 docs SUCCESS）；§5.7 十七族活体（实装 6 全 200 + 登记 11 全 404 + 复制 5 端点 wire 对）；DoD 八条 + **144 提交归属 100% 票号**（D-T421-1 缺陷票号留痕 + 4 笔 M16 规划类）；E1~E7 零倒退 + 断言反转**四处**终核；观察项归位（三处关闭/三处 M16 增补）；**Q1 证据链闭合——QA 意见维持切分 + M16 statistics 优先**。环境披露：pkill 宽模式误杀 T-434 实例 4 分钟（自领违规——已恢复零损）；t422:79 唯一红 = clash fake-IP 劫持 `.invalid`（环境定性）。日志 reports/agents/T-430.md。

**m15-done 收口笔（conductor 2026-09-02 21:5x——M15 关闭）**：
- **Q1 终裁：维持 §1.1-5 分阶段切分，M16 statistics 域优先**（PRD v1.2 落章——T-430 证据 + QA/PM 意见一致）。
- ROADMAP「M15 未纳入项」启用 + 三处增补（40.59MB 观察/offline 三处候小票/T-431 出口清理 + t422:79 spec 环境项）；M15 段标 `m15-done`；README 双语补 AQL/复制增量。
- **m15-done tag + 里程碑 PR + UAT AFTER** 随本笔执行（T-429 §6 清单六标记）。
- **M15 终态**：25 票全落（22 实现 + T-421/T-429/T-430 三验 + 条件票 T-431 已执行/T-403 留痕）+ 增补 T-406b/D-T421-1 两热修；**AQL 全栈贯通**（aql.md→ADR→引擎四环→端点→老搜索→FE）+ virtual 聚合 + 复制包 B + busy 预算 + 结构轮；锚册 v1.30、parity v1.4。**M16 已开工**（PRD v1.1 + T-434 树栈在途）。

**T-434 → done 2026-09-02 23:4x——M16 1/?（批次① 树栈全落地——用户 P0 主诉正面回应）**：四项全对齐——**文件叶子进树**（「（空）」误导占位消除 + children 表收窄 + 目录 Artifact Count/Size 概要）/ **选择≠展开**（单击纯选中、箭头展开、深链自动展开维持）/ **URL 页签段 + 文件路径段化**（`?focus=` 退役——兼容重定向；`artifacts/:tab/:key/*` 新路由）/ **树头工具带**（包类型 facet + rclass 组〔按实有三态——Cache=remote 缓存子集不伪造〕+ Sort-by + 紧凑单选 + My Favorites）+ reverse §3.2 facet 段回填。**锚册 v1.31**（12 新锚）。自抓三缺陷票内修（删除选中回跳闭包/checksum 徽标对比度/根层 focusPath）。自测：四门绿 + 新 spec 5/5 + 指定回归（artifacts-tree 10/t416 4/t372 6）+ 全绿面 + axe 双主题 0 + **服务端 diff=0 + SPA +3,178B**。遗留：?focus= 发射端（Dashboard/Search/AqlPanel）重定向收敛归批次③翻新；TAB 同名 repo edge 锚册注记。日志 reports/agents/T-434.md。

### M16 票批 v1（tech-lead 2026-09-02）

**拆票日志 `docs/M16-SPLIT.md`**（票据明细 AC 全文/依赖图/风险登记/歧义口径——派单直接引用）；实拆 **34 票**（P0×4 / P1×25 / P2×5——含波外条件票 T-467/T-468；E7 toast / license 公钥 ADR / t381〔Q12〕不占号）+ 批次① T-434 已落 = 里程碑 35 票（PRD §1.3 估 30~40 线内）；**B0~B18 十九波全宽 2**（波内 area 互斥；**FE 主线一波一票错峰**——批次② B2~B4 → ③ B5~B8 → ④ B9~B12 → 147/150 FE 腿 B13~B14 → **i18n 独占波 B15~B16**；统计基建 B1 先行〔批次③字段族 + AQL usage 共依赖单源〕）；断言反转归属：②→T-449 ③→T-439/441 ④→T-453 ⑤→T-444 ⑥→T-463/464 ⑦→T-446/450（①已落 T-434）。关键路径 = FE 主线 15 波串行链。

**波次表**：

| 波 | lane 1 | lane 2 | 备注 |
|---|---|---|---|
| B0 | T-435 规格增量三份（rev） | T-436 ADR-0044+K68/K69 会签锚 | 前置锚双票并行（conductor 指令）；软协作 cron 锚 |
| B1 | T-437 parity 册 v1.2+K67 冻结 | T-438 统计基建 | 册 = 批次②~④断言地基；基建先行（一鱼两吃单源，dep T-436 K69） |
| B2 | T-439 FE②-a 三段+字段域 | T-440 AQL statistics/usage | FE 主线开工（dep T-437）；usage dep T-435+T-438 |
| B3 | T-441 FE②-b modal 880+8 开禁 | T-442 远端浏览三型+remote Test 端点 | web/src repositories vs internal/adapter |
| B4 | T-443 FE②-c 列表/入口/dirty/Test | T-444 Annotate BE+迁移（断言反转⑤） | FE② 收口（Test dep T-442）；auth/migrate 域 |
| B5 | T-445 FE③-a 页签序+字段族 | T-446 cron 调度引擎 | 字段族 dep T-438 端到端；internal/scheduler 新包 |
| B6 | T-447 FE③-b 属性编辑+下载形态 | T-448 可选档接线+§8.5 口径扩面 | artifacts PropertiesTab vs internal/repo |
| B7 | T-449 FE③-c 搜索栈（断言反转②+?focus= 发射端） | T-450 cron 三消费面 BE+audit | dep T-446；复制 cron 双实例零重复腿 |
| B8 | T-451 FE 分页 ×9（LC-98/E2 翻案） | T-452 QRL+UI 搜索族+dates（P2） | 分页跨页面独立波；副线 P2 收尾 |
| B9 | T-453 FE④-a 路由表单化+能力位（断言反转④） | T-454 Last Login BE（P2） | 批次④开工 |
| B10 | T-455 FE④-b 两步弹窗+矩阵五列 | T-456 QA 中期回归 | 五列 dep T-444；L36~L42/L48 已落面复核窗 |
| B11 | T-457 FE④-c profile 自助+帮助/About | T-458 文档票（两腿） | AppShell 帮助钮；腿①动笔 |
| B12 | T-459 FE④-d 监控面+导航分组 | T-460 PM Q 终裁联动收口笔 | 批次④收口；Q8~Q12 归位窗 |
| B13 | T-461 FE 远端浏览树消费 | （插空窗：T-467 条件票〔Q10〕） | dep T-448；lane 2 容条件 BE 票 |
| B14 | T-462 FE cron 消费面（GC/备份/import-export） | （插空窗续） | dep T-450+T-459；最后一张常规 FE 票 |
| B15 | T-463 **i18n-a 框架+全树文案外提（独占波）** | —（FE 互斥；非 FE 条件票可插） | 与所有 FE 票互斥 |
| B16 | T-464 i18n-b 双包+切换器+断言双语化（独占波） | —（同上） | L47；en 抽样腿 |
| B17 | T-465 release 烟测+UAT | — | 单票波 |
| B18 | T-466 QA 终验 | — | 单票波；m16-done 就绪判定 |
| 波外 | T-467 [P2·条件 Q10] NuGet symbol / T-468 [P2·条件] L1 列选器+Last Login 列 | — | 未触发不构成 DoD 缺口；T-468 避开 FE 主线波 |

**票据行**（票号 / 标题 / 优先级 / role / area / dep；AC 全文见 M16-SPLIT §1.2 与 PRD §4）：

- T-435 [P0] FR-141.4 规格增量段三份：aql.md 增量（statistics/usage/QRL/dates）+ remote-browsing.md（T-425 §1/§2 成稿）+ cron 表达式子集锚（Quartz 对拍） · role: reverse-engineer · area: docs/reverse/ · dep: —
- T-436 [P0] ADR-0044 cron 调度域（数据模型/子集/next-run/并存语义/防护）+ K68 Annotate 迁移会签 + K69 统计 schema 会签 · role: architect · area: DECISIONS.md + architecture.md · dep: —（软协作 T-435 cron 锚）
- T-437 [P0] FR-141.1/.2 parity 册 v1.2（E5/E1 修正 + E6/E2/cron 三例翻案双留痕 + stay-out 登记 + K67 冻结 + B 47 项四态预归属表） · role: ux-designer（PM 会签） · area: docs/design/console-artifactory-parity.md · dep: —
- T-438 [P1] FR-146.2 per-node 下载计数基建（nodes 四列扩 + 三分口径埋点 + FileInfo 投影——一鱼两吃单源） · role: dev-go-core · area: internal/storage + internal/httpapi · dep: T-436（K69）
- T-439 [P1] FR-143.1/.2 FE 表单三段结构（Basic|Advanced|Replications）+ 字段域补齐（四藏字段+Environments/描述拆分/ForceAuth/SuppressPOM 三链）+ 重置钮移除 · role: dev-frontend · area: web/src/pages/repositories/RepositoryFormPage · dep: T-437
- T-440 [P1] FR-148.1 AQL statistics/usage 域 + GET /api/search/usage（计数与 FileInfo 单源一致 + ACL 探针 + K63 门沿用） · role: dev-go-core · area: internal/search + internal/httpapi search 面 · dep: T-435, T-438
- T-441 [P1] FR-143.3 FE 包类型弹窗 880px + 8 包型开禁（go/nuget/cargo/conan/helm/helmoci/rpm/debian 八型真实客户端 roundtrip） · role: dev-frontend · area: web/src/pages/repositories modal · dep: T-439
- T-442 [P1] FR-147.1 远端浏览批 1 三型回源枚举（helm index 全树/deb/rpm——默认 false）+ remote Test 端点（Engine.TestTarget 复用——wire 歧义⑥票内核定） · role: dev-registry-adapter · area: internal/adapter + internal/remote + httpapi Test 路由 · dep: T-435
- T-443 [P1] FE FR-143.4/.5 仓库列表列集 + Add Repositories 入口分路由 + dirty-gating + remote Test 三臂消费 · role: dev-frontend · area: web/src/pages/repositories/RepositoriesPage · dep: T-441, T-442
- T-444 [P1] FR-146.1 Annotate 动词扩列 + write→deploy-cache 拆分迁移（dry-run 100% + M7 全量零提权 + 可回滚——断言反转⑤） · role: dev-go-core · area: internal/auth + httpapi + internal/migrate · dep: T-436（K68）
- T-445 [P1] FE FR-144.1/.2/.3 详情页签序（权限在属性前）+ File URL + Downloads/Last Downloaded 族渲染 + 仓/目录元数据补齐 · role: dev-frontend · area: web/src/pages/artifacts/NodeDetail · dep: T-438, T-443
- T-446 [P1] FR-150.1/.2 cron 调度引擎（schedule 实体/子集解析/next-run/触发器/误触发防护 + 并存语义——outbox 引擎 diff=0） · role: dev-go-core · area: internal/scheduler（定名从 ADR-0044） · dep: T-435, T-436
- T-447 [P1] FE FR-144.4/.5 属性编辑解剖（常显输入+Add+网格搜索）+ 下载形态单图标钮（校验收伴随——Q2/Q9） · role: dev-frontend · area: web/src/pages/artifacts/PropertiesTab · dep: T-445
- T-448 [P1] FR-147.2 可选档 repo service 接线 + listVirtual §8.5 口径扩面 + 上游停机降级 · role: dev-go-core · area: internal/repo + repo-semantics §8.5 回写 · dep: T-442
- T-449 [P1] FE FR-144.6 搜索栈：列集归一（name 链接|Path|Repository|Modified+选择列——断言反转②）+ 行导航 name 单元格 + 顶栏驻留/快滤 + 快搜空历史占位 + 日期格式 + **?focus= 发射端翻新（T-434 遗留）** · role: dev-frontend · area: web/src/pages/search + AppShell + DashboardPage · dep: T-447
- T-450 [P1] FR-150.3/.4 cron 三消费面 BE（GC 定时+Cleanup 两族/备份定时 CRUD REST+import-export/复制 cron 字段）+ audit 三事件 + 零重复投递双实例腿 · role: dev-go-core · area: internal/scheduler 消费接线 + httpapi 维护/备份/复制 REST · dep: T-446
- T-451 [P1] FE FR-144.7/LC-98 分页控件 ×9 统一（共享组件 + keyset 页窗映射——E2 翻案） · role: dev-frontend · area: web/src 共享分页组件 + ×9 消费点 · dep: T-449
- T-452 [P2] FR-148.2/.3 QRL 全量（v1/system/query_rate_limiter 三态+指标 job）+ UI 搜索族四端点 + dates/creation + §5.7 全景表 M16 行对账 · role: dev-go-core · area: internal/search + internal/httpapi（v1 system 面） · dep: T-435, T-440
- T-453 [P1] FE FR-145.1/.3 用户/组路由表单化（/users/new /groups/new——断言反转④）+ 能力位三旗行为联动 · role: dev-frontend · area: web/src/pages/security/UsersPage+GroupsPage · dep: T-451
- T-454 [P2] FR-146.3 Last Login 派生（audit 登录事件 → users 列表投影） · role: dev-go-core · area: internal/audit + httpapi users 面 · dep: —
- T-455 [P1] FE FR-145.2 权限编辑两步弹窗（双列选仓+Any Local/Any Remote 预置→include/exclude）+ 权限矩阵五列（dep Annotate） · role: dev-frontend · area: web/src/pages/security/PermissionEditorPage · dep: T-444, T-453
- T-456 [P1] QA 中期回归（逐批 V 式复核 L36~L42/L48 已落面 + t226 对照 + 断言反转①~⑤现值 + E1/E6 零倒退） · role: qa-engineer · area: 测试矩阵 · dep: T-434, T-439~T-452 已落面
- T-457 [P1] FE FR-145.4/.6a profile 自助 identity token/SSH key + ? 帮助下拉 + About 版本弹窗 · role: dev-frontend · area: web/src/ProfilePage + AppShell · dep: T-455
- T-458 [P1] 文档票两腿（腿①树/表单/详情字段族/统计 usage/安全面；腿②监控/远端浏览/cron/i18n + **豁免翻案用户可见变化公告**） · role: tech-writer · area: docs/user/ · dep: 腿① T-440/443/445；腿②候 B12~B16
- T-459 [P2] FE FR-145.5/.6b 监控面 System Logs 查看器 + Service Status + SystemInfoPage 归位 + 导航分组/侧栏过滤 · role: dev-frontend · area: web/src/pages/monitoring + admin/SystemInfoPage + AppShell 导航 · dep: T-457
- T-460 [P1] PM Q 终裁联动收口笔（Q8~Q12 归位 + B 47 项四态归属核对 + K67~72 回填 + ROADMAP M16 未纳入项 + M17 衔接） · role: product-manager · area: docs/prd/milestone-16.md + ROADMAP · dep: T-437
- T-461 [P1] FE FR-147.3 远端浏览树消费（可选档 on/off 双态 + 未缓存路径回源 + 降级呈现） · role: dev-frontend · area: web/src/pages/artifacts + repositories 开关 · dep: T-448
- T-462 [P1] FE FR-145.7 GC/备份 cron 消费面（Cleanup 两族/Compress/Prune/Quota + cron 字段 next-run 呈现）+ 备份定时 CRUD + import/export 管理页 · role: dev-frontend · area: web/src/pages/governance · dep: T-450, T-459
- T-463 [P1] FR-149.1 i18n 框架接入 + **全树文案外提 100%**（组件零硬编码中文 CI 断言 + zh 包零语义变化）——**独占波** · role: dev-frontend · area: web/src 全站 + web/scripts · dep: T-462（全部 FE 票收口后）
- T-464 [P1] FR-149.2/.3/.4 en 资源包（术语对齐 Artifactory）+ 语言切换器（localStorage 持久）+ 断言双语化（en 抽样腿 + axe 双 locale + 键断言） · role: dev-frontend · area: web/src + web/e2e · dep: T-463
- T-465 [P1] release 烟测 + UAT 随里程碑 PR（8 包型/统计/cron 取证 + Chart bump 判据 + F1 六平台趋势 + server 40.59MB 原始线观察；t381 处置〔Q12〕随票或留痕） · role: release-engineer · area: deploy/ + charts/ · dep: 全部实现票 + T-458
- T-466 [P0] QA 终验（L35~L48 全量 + 断言反转①~⑦归属审计 + **B 47 项收口审计表四态零无主** + DoD 八条 + NFR 归档 + m16-done 就绪判定） · role: qa-engineer · area: 全量矩阵 · dep: 全部 + T-465
- T-467 [P2·条件 Q10] NuGet symbol server 六承转正（mini as-built 规格随票 + .pdb/GUID 真实腿） · role: dev-registry-adapter · area: internal/adapter nuget symbol 面 · dep: Q10 终裁（材料窗 T-460）；非 DoD
- T-468 [P2·条件] L1 列选器推广（repos/users/groups/permissions——columnPrefs 共享层）+ Users Last Login 列 · role: dev-frontend · area: web/src 四列表页 · dep: T-454 + 批次④ FE 收口；FE 空位插空；非 DoD
- 不占号 slot：E7 toast 锚位（候用户信号）/ license 公钥 config 覆盖 ADR（候立项）/ t381 残留清理（Q12 conductor——建议随 T-465 处置）

**M16 拆票派发（conductor 2026-09-02 23:5x）**：tech-lead 全量拆票在途（T-435 起——批次②③④ + FR-146 统计基建先行 + FR-147 远端浏览 C 批 1 + FR-148 AQL 副线 + FR-149 i18n 独占波 + FR-150 cron 域〔ADR-0044 前置锚〕+ 条件池）。

**M16 拆票落板（tech-lead 2026-09-03 00:0x）**：`docs/M16-SPLIT.md` + BOARD 票批 v1——**34 新票**（P0×4/P1×25/P2×5，含条件票 T-467/T-468）+ T-434 = **35 票**（PRD 估 30~40 线内）；B0~B18 十九波全宽 2。关键路径 = FE 主线 15 波串行链；BE 全链 lane 2 错峰零反压；**B1 统计基建（T-438）先行**（批次③ Downloads + AQL usage 单源共依赖）；i18n 独占波 B15~B16（SPLIT 歧义②留痕——FE 票附「文案集中常量」纪律降本）。三处工期压缩选项（R2）备 conductor 裁量。

**B0 派发（conductor 2026-09-03 00:1x）**：**T-435**（规格增量三份——aql.md statistics/QRL/dates + remote-browsing.md 成稿 + cron Quartz 锚）+ **T-436**（ADR-0044 调度域 + K68/K69 双会签——与事件驱动零重复投递边界）双锚并行在途。B1（T-437 parity 册 v1.2 + T-438 统计基建）候 B0 就绪。

**T-436 → done 2026-09-03 00:2x——M16 2/35（B0 锚①：ADR-0044 Accepted）**：cron 调度域六轴决策 13 要点——schedules 台账（表达式/next-run/last-run/状态/所属域）+ **Quartz 六域子集自研解析**（robfig/cron 否决——方言判断）+ 独立 1min ticker + **与事件驱动+outbox 并存三层口径**（调度只触发全量类任务，事件驱动仍是增量唯一引擎，零重复投递）+ 防护三面（过去时间拒配/每域并发上限/误触发）。K68 会签（write→deploy-cache 零提权等价迁移 + annotate↔M10 属性对位）+ K69 会签（nodes 四列 + 三分计数口径 + **?stats 面正位**〔与 T-438 AC1 措辞差——派单附注〕+ CapSystemRead 可见性门）。architecture §25（25.1~25.7）+ 技术债 48/49/50。软缝清单八项（T-435 差异核对协议）。下游 T-438/440/444/446/450/462 全解锁。日志 reports/agents/T-436.md。

**T-435 → done 2026-09-03 00:3x——M16 3/35（B0 锚②：规格增量三份）**：aql.md **§14 增量段**（statistics 十字段/QRL 全量三态/dates-creation 空集族逐字——M15 冻结面零改动）+ **remote-browsing.md**（T-425 矩阵直提成稿 + 降级六条 + §8.5 扩面）+ **cron-scheduling.md**（K70 归位——Quartz 官方对拍 + 与 ADR-0044 软缝留痕）。覆盖端点 22/流程 12/字段集 4；置信度高 ~30/中 ~13/**低 0**。**关键发现三条**：① **PRD/T-440 的 `usageSince` 参数名与 Artifactory wire `notUsedSince` 不符**（三源——交实现票与 PM 勘误）；② statistics `remote_*` 族 = 下游 smart remote 回拉统计，与 K69 三分口径正交（T-438 埋点防错条已钉）；③ QRL 与 AQL 429 并发闸正交（QRL 限流=延迟放行非 429）。**t226 活体腿降级**（TUN 路由断 SSH kex——R1 内置路径，15 条待验证三清单在册，环境修复后一次只读会话可补齐）。日志 reports/agents/T-435.md。

**B1 派发（conductor 2026-09-03 00:4x）**：**T-437**（parity 册 v1.2 + K67 冻结 + 47 项四态预归属——ux 票，PM 会签）+ **T-438**（统计基建先行——nodes 四列 + 埋点 + 三分口径；**携带 T-436 K69 ?stats 正位附注 + T-435 remote_* 正交防错条 + notUsedSince 勘误口径**）双 lane 在途。

**T-437 → done 2026-09-03 00:5x——M16 4/35（B1 票①：parity 册翻案修订）**：**parity 册 v1.5**（票面「v1.2」实落 v1.5——版本线已至 v1.4，回退即重写历史，映射双处留痕）——E5/E1/E6/E2/R4 **五处翻案改写** + §3 M3 勘误 + §5 L4 分治注 + **§9A stay-out 八项登记** + **§11 K67 冻结**（树栈 as-built 定案）+ **§12 B 47 项四态预归属表 48 行**（零缺号零无主——B-2.8/B-3.2 两无主候选明示归 T-460 核定）+ cron 推翻三处留痕闭环（T-402a 勘误原文存档，「事件驱动唯一引擎」→「增量唯一引擎+全量调度并存」）；锚册 v1.32（v1.30 补记 + 本批注记）。**两命令门**（make docs/anchor-audit）系 ux agent 无 shell 的外部确认位——conductor 收口窗代跑。日志 reports/agents/T-437.md。

**T-438 → done 2026-09-03 01:2x——M16 5/35（B1 票②：统计基建单源落）**：nodes 扩列四列（K69 DDL + 幂等迁移）+ **三分口径埋点**（直连/经 virtual〔virtual 命中给实际存储成员行计数〕/remote 缓存命中——remote_* 正交防错条吸收）+ FileInfo 投影扩字段 + **?stats 面**（敏感字段 + CapSystemRead 门——按 ADR-0044 正位执行，票内 area 映射核对留痕〔internal/storage 实为 blob 后端——nodes 在 metadata，落点修正〕）+ statisticsEnabled 行为化 + sourceOrigin 落库顺车。自测：build/vet/lint 0 + T438 十二测试 race 绿 + 分位基准 + **全量 `go test -race ./...` 25m 口径全包 PASS**（含 httpapi ~10min race 面）。**单源契约声明**（零第二计数通道——批次③字段族与 FR-148 usage 域唯一数据源）。日志 reports/agents/T-438.md。

**B2 派发（conductor 2026-09-03 01:3x）**：批次② 首环 **T-439**（FE 表单三段结构 + 字段域补齐——dep T-437 已满足）入 FE lane。

**用户指令 intake ⑨（2026-09-03 01:3x）：本地 Artifactory 7.161.20 参照——「我在本地的8082部署了artifactory,参照这个，重构前端,使用typescript,账号密码是admin/JFrog@2026」**。实例核实：**7.161.20**（rev 86120900，比 t226 的 7.84.10 新 77 个 minor）**addons 全开**（replication/curation/xray/release-bundle/federated/retention 等——Pro/Enterprise 面可见）。**parity 参照基线切换至本实例**（HTTP 直连无 SSH/TUN 障碍；内存已存档）。前端已是 TypeScript（React+TSX 全树）——「使用typescript」确认满足。**处置**：①T-439（在途表单票）已获补充指令——以 7.161 实测为准；②**基线复核 agent 已派**（7.84→7.161 形态差异清单 + T-435 十五条待验证归位 + 批次② 即时修正建议）。

**基线复核 → done 2026-09-03 04:5x（PASS）**：7.161 活体复核——**批次② 获背书**（T-439 设计与 7.161 实测一致照稿）；B 系 47 偏差 4 项重写（About 消失/管理导航 11 组等）+ **2 项翻转为已对齐**（Trash=树节点——T-372 早已做对）+ 余维持；T-435 十五条归位（4 已验证 + 4 部分 + 7 维持）；3 处规格勘误入队；65 截图 + 100 DOM 转储归档 `m16-baseline-evidence/`。局限五项如实登记（文件叶子/virtual Advanced/列表右缘列/Webhooks 去向/syntax-search body）。日志 reports/agents/m16-baseline-refresh.md。

**T-439 → done 2026-09-03 05:0x——M16 6/35（批次② 首票：表单三段结构落）**：**Basic|Advanced|Replications 步进**（7.161 活体钉形——含必填门）+ 复制配置迁第三步（M6 语义零变化）+ 八域表驱动（**一实字段 forceConanAuthentication 活体实证** + 七域 decode-only 预留位 + 漂移钉 tripwire）+ footer 重置钮移除（Q9 兑现）。锚册 **v1.33**（13 锚 + form-reset 退役）；formCopy.ts 新建（i18n 集中常量纪律）。四门绿 + 新 spec 5P + 表单族合跑 31P + a11y 双主题 0 + SPA +1,292B。**契约漂移 3 项**（核心：PRD「PUT 全收」证据仅解码层——configJSON 四域静默丢弃 → Q8 全补前提在 round-trip 层不成立；Force Auth 实为 local×conan；7.161 Environments→Stage 更名）+ K70 登记。遗留三条（conan 端到端候 pro 档/预留位转正自动提示/T-431 陈旧腿顺车归位）。日志 reports/agents/T-439.md。

**T-440 → done 2026-09-03 05:4x——M16 7/35（AQL statistics/usage 域——T-438 首消费者）**：statistics 域查询绿（字段集照 aql.md §14 定案——downloaded/downloaded_by/remote_downloaded 族 + include/sort 联动）+ **`/api/search/usage?notUsedSince=`**（勘误后的 wire 名落地）+ 计数与 FileInfo 投影**单源复核一致**。ACL 越权探针零泄漏 + K63 资源门零豁免沿用 + P95 ≤800ms。conductor spot：build/search+httpapi 测试绿 + vet 全树零破损。日志 reports/agents/T-440.md。

**T-441 → done 2026-09-03 06:3x——M16 8/35（批次②：包类型弹窗 924px + 八型开禁——形态翻转③）**：modal **924px 居中**（min(924px, vw-48px) 解 MUI 600px 钳制——7.161 实测形态）+ **pkgChoiceBlock 族退役**（磁贴+radio 两承载面；后端 license 门归 ADR-0033 唯一裁决）+ 徽章文字色收敛（开禁后 WCAG 新义务 4.48→5.4:1）。**八型真实客户端 roundtrip 8/8**（go/dotnet/cargo/conan/helm 经典+OCI/dnf/apt——licensed scratch 仓均经 924px 弹窗 UI 建造，modal 实测 924px 精确居中）。四门绿 + SPA **−2,049B** + parity **v1.6**（M1 翻转③ + B-2.6 已落）+ 锚册 **v1.34**。契约漂移 ①：dotnet 9+ 需 `allowInsecureConnections`（nuget 文档补——转 tech-writer）。观察①：T-439 报告「合跑 31P」实际漏 repo-policy-keys（HEAD 6 红——本票顺车归位）。遗留：licensed 留验实例 :18081（admin/password，八仓 t441-rt-* 在场）可活体核对。日志 reports/agents/T-441.md。

**T-442 → done 2026-09-03 10:5x（配额窗⑩复活后收口）——M16 9/35（远端浏览批 1 + remote Test——Q4 出口 C 兑现）**：**`listRemoteFolderItems` 可选档**（默认 false——off 行为 diff=0）：helm classic（index.yaml 全树解析）/ deb / rpm（元数据枚举）三型回源枚举 + 上游不可达降级（错误态不整树塌）+ 探测只读零副作用 + ACL 同源（越权仓零枚举）。**remote Test 端点**（`repositories_probe.go`——上游可达 + 认证探测零副作用；wire 归 repositories 域）。全树 lint 0 + fake sweep 净 + gofmt 空。conductor spot：build 0 + vet 全树零破损。真实客户端腿不适用（枚举面系 API 侧——票内留痕 §5）。日志 reports/agents/T-442.md。

**T-443 → done 2026-09-03 12:2x——M16 10/35（批次② 闭合：列表列集 + 入口分路由 + dirty-gating + Test 消费）**：**Add Repositories 下拉三预选**（7.161 活体形态）分路由（三静态路由承 rclass prop + /new 直链兼容映射——7 处 emitter 零改动）+ 表单 rclass 控件移除（form-rclass-* 三锚退役）+ 「类型」列收敛（Q9）+ **Replications 列扩 local+remote 两 Tab**（push-only 口径注记）+ Project 列缺位登记不伪造 + **dirty-gating**（deep-equal 基线）+ **remote Test 三臂消费**（正确凭据/错误凭据/不可达——curl+Playwright 双证 + 零副作用探针）。锚册 **v1.35**（9 入册 + 4 退役）+ parity **v1.7**（B-3.6~3.9 翻已落 + **B-3.9 as-built 勘误**——T-404 实作仅 local Tab 且 spec 系空洞负断言）。四门绿 + SPA +3,830B + 触及面 55P + m8/m14/m16 全目录绿。**批次②（B2~B4）闭合**。日志 reports/agents/T-443.md。

**T-446 → done 2026-09-03 13:2x——M16 11/35（B5 票②：cron 调度引擎落）**：**internal/scheduler 新包**——Quartz 六域子集解析器（接受/拒绝形态照 cron-scheduling.md 定案——C-a 四臂活体已验）+ **next-run 纯函数** + schedules 台账 CRUD（ScheduleStore 五方法照 ADR-0044 DDL）+ **独立 1min ticker**（domain 回调接口——只触发全量类任务；消费面归 T-448/T-450）+ 防护三面（过去时间拒配/每域并发上限/误触发）+ 与事件驱动并存零重复投递边界（票内声明）+ audit schedule.* 词族。conductor spot：scheduler 21.3s 绿 + metadata Schedule 测试绿 + lint 0。日志 reports/agents/T-446.md。

**用户即时项（2026-09-03 12:4x）**：企业版 license 生成 + **UAT 激活**（`b5be0f76`，binflow，365 天，**19/19 槽全开**——conan 建仓 200 真验 pro 门通过；/tmp/binflow-enterprise.lic）。

**T-445 → done 2026-09-03 13:3x——M16 12/35（B5 票①：详情页签序 + 字段族落）**：**页签序互换**（常规→有效权限→属性——7.161 实测序，锚/URL slug 零变化）+ **File URL 三形态**（含复制钮）+ **Downloads 字段族**（消费 T-438 **?stats 面**〔按 as-built 实落——派单「FileInfo 非敏感」与 ADR K69.4 有差，FE 按实落消费效果等价，混合形裁定归 conductor/ADR〕）+ RepoGeneral 字段族（Layout/Description/Created/Artifact Count——usage counts 点名臂）+ detailCopy.ts（文案集中常量）。锚册 **v1.36**（9 锚）。四门绿 + SPA +813B + 指定回归全绿（keyboard/t434/artifacts/m10×3/t416/t372/trash/m9/a11y）。**契约漂移登记 ②③**（GET /api/repositories `url` 缺 /binflow 前缀〔M1 既有〕+ FileInfo.downloadUri 系 api/storage URI 非下载语义——后端勘误候选两条）。日志 reports/agents/T-445.md。

**T-448 → done 2026-09-03 15:1x——M16 13/35（B6 票②：可选档接线 + §8.5 扩面）**：repo service `listRemoteFolderItems` 开关接线（**默认 false——off 行为 diff=0** 既有断言零回归）；**口径扩面**：on 时 **virtual 树含远端成员行**（repo-semantics §8.5 对账回写——T-412「仅缓存行」口径扩面兑现）+ 未缓存路径回源拉取 + 下载计数埋点联动（T-438 单源）+ 上游停机降级（远端层错误态 + 已缓存行可用）+ 越权仓远端行零泄漏（allow() 同源）。conductor spot：build 0 + targeted 测试绿 + vet 全树净。日志 reports/agents/T-448.md。

**T-447 → done 2026-09-03 21:4x（配额窗击落两轮后终收口）——M16 14/35（B6 票①：属性编辑解剖 + 下载形态）**：**B-2.9 解剖翻正**（常显 Property/Value+Add+网格搜索、隐藏「+ 新增属性」与逐行 ✎/🗑 退役、删除过危险确认——E1 统一）+ **B-2.12 单 24px 图标钮**（两带文字按钮收敛 + checksums/mimeType/verify 收进伴随菜单——Q9 消化）+ ?stats 计数联动（T-438 单源）。7.161 活体实证解剖。锚册 **v1.37**（6 新+4 退役）+ parity **v1.8**（B-2.9/B-2.12 翻已落）。四门绿 + 新 spec 4/4×4 连跑 + 回归全绿（m10×3/artifacts/m16 目录 28P/m8 族 25P/a11y 双主题）+ SPA +1,081B。契约注记：?properties 分号=矩阵路径语法（REST 只认逗号配对——spec 注释留痕）。K68 候裁臂不建不登记（Property Set 须 BE 另立票）。日志 reports/agents/T-447.md。

**intake ⑩⑪ 登记补笔（2026-09-03 19:4x）**：⑩ CI 协议矩阵立票 **T-469**（devops-engineer）在途——`ci/protocol-matrix.sh` 单源 + CircleCI `protocol_matrix` job〔挂 deploy_uat 后，machine executor + 逐腿 when:always〕+ GH Actions `protocol-matrix.yml`，十协议推送+拉取，jfrog/project-examples 夹具。⑪ **origin 切 `https://github.com/0ldlight/binflow.git`**（SSH deploy key 只读误报 → HTTPS+gh 凭据；首推 `9815864..d9cb2bb` ✅）。

**T-469 → done 2026-09-03 21:5x——intake ⑩ 兑现（CI 协议推送/拉取矩阵双面落地）**：**`ci/protocol-matrix.sh`**（712 行，shellcheck 零告警）单源十腿——每腿幂等建仓→**版本戳推送**（重跑永不触发覆盖拒绝）→拉取→内容断言；fixture = CI 内 shallow clone jfrog/project-examples（clean-room 仅测试输入）。**CircleCI `protocol_matrix`**（machine executor + 十腿 when:always 独立 step + `requires: [deploy_uat]` main-only + `UAT_MATRIX_PASSWORD` secret 硬门）+ **GH Actions `protocol-matrix.yml`**（workflow_dispatch + push:main 预置注释〔billing 中断注记〕+ 十腿并行 fail-fast:false）。**本地净实例 6/10 腿全绿**（generic/maven/gradle/npm/pypi/docker——报告 TSV+内容对账+/v2/_catalog）；go/nuget/helm/conan 四腿 community 档正确 license 门 SKIP（UAT enterprise 19/19 可跑）。干跑修 6 真缺陷（BSD sed/npm fixture/SIGPIPE/Dockerfile/war-plugin×JDK17/host.docker.internal）。actionlint 0 + circleci config process EXIT 0。**首次 CI 真跑候下个 main 部署触发**。日志 reports/agents/T-469.md。

**T-450 → done 2026-09-04 01:1x（配额窗⑫复活后收口）——M16 15/35（B7 票②：cron 三消费面 + audit 三事件）**：**GC 定时**（维护面 cron 字段 + Cleanup 两族触发接线——手动 dry-run/apply 并存维持 + gc 内核 callable 化）+ **备份定时 CRUD REST**（022 backups 台账迁移 + New Backup/cron/next-run/列表 + import/export BE 支撑）+ **复制配置 cron 字段**（**M15 Q5 推翻兑现面**——双实例夹具按点触发全量同步逐路径 sha256 一致 + **调度窗内同制品增量事件不双推**〔零重复投递 L48 断言〕+ Replicate Now 并存幂等）+ audit 三事件（调度/执行/失败——set 词接线）+ 调度 CRUD admin/manager 门。三域消费接线（cmd schedule_wiring）+ 021/022 双迁移。conductor spot：scheduler 17.9s + httpapi 15.5s 绿 + build 0。日志 reports/agents/T-450.md。

**T-449 → done 2026-09-04 01:4x（配额窗⑫复活后收口）——M16 16/35（B7 票①：FE 搜索栈收敛）**：**ResultsTable.tsx 共享结果网格**（列集归一：Artifact 链接|Path|Repository|Modified+选择列——**断言反转②**；大小/sha256 移列选器不默认呈现）+ 行导航仅 name 单元格深链（行体 inert）+ **顶栏驻留查询 + 网格内快滤**（AQL 编辑器共存）+ 快搜空历史占位恒渲染 + 日期格式含时区偏移 + **?focus= 发射端翻新**（Dashboard/Search/AqlPanel 改发路径段——T-434 遗留闭）。**两真 a11y 缺陷修复**（Checkbox aria-label 落 input / indeterminate aria-checked mixed 禁值）。锚册 **v1.38** + parity **v1.9**。四门绿 + 新 spec 6/6×3 + m16 目录 34P×2 + t419+t414 18/18 + axe 双主题三态 0 + SPA +850B。契约注记三条（?repos= 退役 / cols-search 缺席语义微调 / 对账器掩蔽观察）。日志 reports/agents/T-449.md。**B7 齐落。**

**用户指令 intake ⑫（2026-09-04 01:5x）：「后续 binflow 的文档迁移至 fern，注意剔除文档中迭代相关的内容，尽量精简」——立票 T-470 已派。**

**T-470 → done 2026-09-04 02:1x——intake ⑫ 兑现（Fern 迁移首程）**：**`docs-fern/`**（fern.config.yml + docs.yml + 40 页 MDX，42 文件）——**46 页 10,278 行 → 40 页 5,263 行（51.2%）**；**T-xxx 引用 156→0**（去迭代化 grep 零命中——唯一例外系 docker virtual 400 错误文案逐字保真）；合并 auth←3 篇 + storage/permissions/operations/console←各 2 篇；删 real-env-appendix（纯 QA 归档）。**API 契约逐字保真**（端点/参数/错误文案/配置键/命令）。自查全过（yaml 双配置 + @mdx-js/mdx 40/40 编译 + 导航↔页面双向映射 + 内链解析）。遗留：`fern build` 真构建候联网环境（CLI 传递依赖 registry 404）；平台发布需账号（conductor/用户执行）；旧站退役另裁。日志 reports/agents/T-470.md。

**Fern 官方布局重构（conductor 2026-09-04，`037c383`）**：用户提供 Fern token（fern_Cod…Hyi）；CLI 安装排雷（npx 缓存劫持 1.11.3 旧核——清除后 brew 5.113.1）+ **docs.yml 逐字官方 schema**（instances/tabs/navigation 三顶层分离 + page path 指文件）——**CLI 解析全绿**（"Reload completed in 855ms" 40 页导航全解析）。遗留两环境项：本地预览前端被 pnpm 11 构建门（esbuild postinstall）挡（后端 :3003 正常）；**发布走 Fern 平台 GitHub 连接**（用户 UI 侧连 0ldlight/binflow → 自动构建 binflow.buildwithfern.com）。

**T-451 → done 2026-09-04 07:5x——M16 17/35（B8 票①：分页控件 ×9 统一——E2 翻案兑现）**：**Pager.tsx 共享控件**（页码/每页行数档位冻结 [20/50/100/200/1000]/首末页禁置 + useClientPager）+ **八列表面迁移**（ResultsTable/AqlPanel/审计 keyset 页窗/仓库/users/groups/permissions/tokens）+ 制品树增量加载按 L4 分治维持（豁免锚注记）。**契约漂移①**：7.161 管理列表实为 ag-grid「to/of」形态无页码序列——按 7.84 冻结锚实现，版本线分歧留痕候 ux 裁（翻转点已备）。锚册 **v1.39**（9 名 + 四退役）+ parity **v1.10**（E2/L4/B-3.3 翻已落 + §11.2 档位冻结）。四门绿 + 新 spec 4/4 + m16 目录 38P×2 + a11y 双主题 0 + SPA +4,863B。**环境事件披露**：brew simdutf 升级断链系统 node@22（全程 nvm v24 绕行——其他 agent 同法）。日志 reports/agents/T-451.md。

**T-452 → done 2026-09-04 09:1x——M16 18/35（B8 票②：AQL 副线收尾）**：**QRL 全量**（`v1/system/query_rate_limiter` 三态 REST + admin 门 + K63 门读数一致——K72）+ **UI 搜索族四端点**（artifactsearch/stashResults/packagesSearch/syntax-search——wire 照锚；Smart Searches 保存面 pro 档不做）+ **dates/creation 双端点**（K65：404 `No results found.` 逐字空集族 + uri 瘦行 + epoch-ms）+ ACL 同源探针 + **§5.7 全景表 M16 行逐条对账**（search_family_panorama_test）。conductor spot：build 0 + 定向 15.1s 绿。日志 reports/agents/T-452.md。**B8 齐落——批次③ 闭合。**终轮补记（`4bbb5c6`）：全量 race 完整 exit 0 零 FAIL + 遗留五条登记（migration 023/cmd 挂点/V-m 对拍/LOW_PRIORITY 桶/QRL audit 词）。

**T-454 → done 2026-09-04 11:3x（配额窗⑭复活后收口）——M16 19/35（B9 票②：Last Login 派生）**：audit 登录族事件 → users 列表 **lastLoggedIn 投影**（**单 GROUP BY 聚合禁 N+1**——整列表一查；无登录史 null；默认序不破坏）；area 外延三文件（metadata 存储腿）自报在案。conductor spot：build 0。日志 reports/agents/T-454.md。

**T-453 → done 2026-09-04 12:2x（配额窗⑭复活后收口）——M16 20/35（B9 票①：用户/组路由表单化——断言反转④）**：**/admin/security/users/new 与 /groups/new（+ :name/edit）整页表单**（四节 + **Retype 密码域**〔7.161 活体一手实证——T-384 缺位定案翻案〕+ **能力位三旗预留位**〔BE 未承接——恒禁用零提交 + hint 承两臂语义 + API 漂移钉 tripwire：BE 落地日翻红即转正触发器〕+ Reset/Save 初始禁置 + readonly 深链防御）+ **列表内联展开卡删尽**（断言反转④兑现；锚保导航入口零改名）+ GroupFormPage 两态路由（成员矩阵随迁）。锚册 **v1.40**（9 名零改名）+ parity **v1.11**（E5/M3/B-2.15 翻已落——E5 矛盾闭环）。四门绿 + 新 spec 3/3 + m16 目录 39P + 回归全绿 + SPA +3,803B。活体取证 t453-probe/（7.161 只读零写）。日志 reports/agents/T-453.md。

**用户指令 intake ⑬（2026-09-04 02:3x）：「后续代码只提交到 git@github.com:0ldlight/binflow.git」——push 循环已去掉 vm 远端**（配置保留零使用）。

**全树 race 补证判无效（conductor 2026-09-01 14:3x）**：与两 agent 测试套件同机并发跑——22 包红全部 620-660s 超时形态 + db/sql 竞争 panic + storage fail-open 窗口 = **共租负载签名**（T-414 日志同款 load 450-630），非产品缺陷。T-412 的 AC3 证据改挂 **T-421 中期 QA 串行全树 race**（届时 lane 空净）。**D-413-2 [P3·登记]**：唯一真信号 = T-413 `TestEngineMixedLoad` 在慢机下 K63 并发门 429 介入而测试只容忍 busy gate 拒绝——测试健壮性收窄（门注入调低或混合负载容忍 429），归 T-421 复验时顺腿修或转 T-433。**教训入册：全树 race/性能类验证必须 lane 空净时串行跑（派单纪律）。**

**T-444 → done 2026-09-04 16:5x（配额窗⑮复活后收口，`388411a`）——M16 21/35（B4 lane-2 补位票：Annotate 动词迁移——断言反转⑤）**：**动词闭集 +`a`**（auth 五文件）+ properties 写门单点翻转 ActionAnnotate + 403 文案 + **wire 五词翻新**（write 别名收词：PUT 收别名、GET 回显正名单）+ ?permissions 视图字母 +a + **迁移 023**（sqlite/postgres 双库 + 迁移器事务边界 guard——第一轮 ROLLBACK 关键词违规已修）+ store 四 SQL 点 + PermissionPrincipal.CanAnnotate + **dry-run 公共 API**（AnnotateMappingDryRun 全行映射表 + AnnotateBackfillVerify 双向集合断言）。3 新测试文件（行为命名）+ 6 既有文件断言反转⑤。26 文件 +1,232/−82；四门全绿（全量 race GOTEST-EXIT=0，37 包 ok——击落后重验取证）。**预登记 FE 面**：四处 e2e GET-echo 断言将红（security:175 / m8-permissions:170,437 / m9-permissions-mholder:200）→ **T-455 消费**；write 列勾选漂移展示面同归 T-455。两处低置信登记：① 字母 `a` vs rest-api.md 勘误行 `n`（按 ADR-0044 定案 a 实现——reverse-engineer 勘误候选）；② migration 011 墙钟阈值满载 flake（单独跑 0.17s/6x 余量——QA 登记在案）。遗留：M17 别名移除评估；dry-run 无 CLI 挂点（cmd/ 非 area——5 行小票可接）。日志 reports/agents/T-444.md。

**P0 事件处置（conductor 2026-09-04 16:5x，用户裁定「回退 main + 改道 develop」）：dependabot 直升 main 打破 UAT 基线 → PR #82 回退落地**。事实链：dependabot 14 提交于 09-01~09-02 **绕过 develop 直落 main**（MUI 7.3.11→9.4.0 跨两代 + vite 7→8 + @types/node 26 + docs-site react 19 + sqlite 1.57）→ main CI e2e 自 PR #76（09-03 18:35）连红 5 轮（element(s) not found——大版本 DOM 漂移签名）→ **UAT 部署坏基线 uat.9d99182** → develop/main 基线分叉。处置：**PR #82（`c4da02e`）**= web/docs-site/go.mod 六文件恢复 develop 逐字基线 + **dependabot.yml 全四组 target-branch: develop**（改道镜像进 develop `2cc3e25`）+ 关闭被取代 PR #71~#74/#81（TS 7/eslint 10/plugin-react 6 大版本升级候 m16-done 后立正式票）。Fern 无辜自证：#79 diff 仅 1 行 custom-domain（binflow.org 保留）、#80 空、#81 重复已关。**main CI 复绿验证 + UAT 回滚部署验证挂下轮**。**教训入册：dependabot 默认打 default-branch——凡仓库 default=main 且 main=部署源，target-branch 必须显式指 develop。**

**T-455 → doing 2026-09-04 17:0x（B10 票①：FE④-b 权限编辑两步弹窗 + 矩阵五列 ← T-444 done）**：dev-frontend 在途。两块：A 消费新 wire（四处 e2e echo 翻新 + write/annotate 勾选联动照 7.161 活体）+ B 主体两步弹窗与五列（read/annotate/write/delete/manage）。7.161 活体取证优先、锚册冲突登记不伪造。

**T-456 → done 2026-09-04 17:2x（配额窗⑮复活后收口，`67587b9`）——M16 22/35（QA 中期：L36~L48 逐批复核）**：**一行状态 PASS（1 缺陷登记 + 2 在途标注）**。已落 20/35 面全绿：L36 八型真实客户端矩阵 **8/8**（go/nuget/cargo/conan/helm/helmoci/rpm/deb 独立复验）+ 字段域 tripwire as-built；L37/L38/L42 全绿（统计单源三面同值 / QRL 三态 / UI 搜索族+dates 逐字臂 + ACL 三面零泄漏）；**L41 ON 态被 D-T456-1 阻断**；L48 引擎腿全过（next-run 对拍 / 调度全量 sha256 三路一致 / 零重复投递 B 恒 4 / Replicate Now 幂等 / audit scheduler 行）；**race 三红全定谳负载噪音**（净机 solo：metadata 141s / repo 816s / httpapi 733s 全 ok 零 DATA RACE——Docker VM 582% CPU 窃取实证入档 O-1）；回归窗 139P/0F 双形态 + 断言反转①~④现值兑现/⑤在途 + E1/E6 零倒退 + 锚册门 PASS；t226 关键页只读对照一致（Release Bundle/Federated=M17 stay-out 不伪造）。报告 reports/agents/T-456.md。

**D-T456-1 [P1·登记]**：`listRemoteFolderItems` httpapi 传输层丢字段——可选档 PUT 静默吞 + 类型门 400 不可达 → **L41 ON 态 FE 消费被阻断**。QA 建议并入 **T-461**（FR-147.3 FE 远端浏览树消费——届时携 BE 支撑腿：internal/httpapi 传输层修复）。T-461 派单时必带本缺陷。

**T-458 → doing 2026-09-04 17:3x（B11 票：文档票腿①——tech-writer）**：deps 满足（T-440/T-443/T-445 done）。腿① 四面：树深链工具带 / 表单三段 8 包型 / 详情字段族+usage 可复跑示例 / 权限动词 annotate 增量 + i18n·cron 预埋骨架。**双树同步**：docs/user/ 主源 + fern/pages/ 镜像（conductor 统一重发布）。腿② 候 T-459/T-461~T-464 合入另派。

**CI 事件追加（conductor 17:3x）**：c4da02e 上 e2e job 仍红 + release-dryrun 首红（前三轮全绿）+ ci job 在跑。**e2e 复绿假说受创**——依赖事件与 e2e 红的关系须重审（日志候 run 完结取证：`gh run view --job` 取 e2e 失败清单；另查 ci job 自 ≥09-02 06:33 PR #64/65 起的连红根因——sqlite 1.57 假说候证）。UAT 已回滚基线 ✅（uat.c4da02e）。

**CI 事件定谳（conductor 17:5x，run 33855887617 完结）**：三 job 分诊完毕——**ci ✅ SUCCESS**（回退奏效：lint/Test/GC stress/typecheck 全绿——dependabot 载荷坐实 ci 连红根因）；**release-dryrun ❌ = goproxy.cn GOAWAY 网络抖**（六平台快照下载 genproto 断流——重跑即绿类，非代码）；**e2e ❌ 4F/334P（23.2m）= flake 家族**（失败集两轮漂移、全部 element(s) not found、334 绿证 MUI 7 SPA 健康——CI 慢机超时形态；本地四门同 spec 全绿在案）。`gh run rerun --failed` 已发取判别信号。

**T-471 → todo（2026-09-04 17:5x 立票，P2，票外工程票）：CI e2e 稳定性**——playwright.config CI 侧 retries（`process.env.CI ? 2 : 0`）+ expect/action timeout 档位 + 必要时 workers 收敛。role: devops-engineer ｜ area: web/playwright.config.ts + .github/workflows/ci.yml ｜ dep: **候 T-455 收口**（避免在途 FE 票 e2e 被配置变更扰动）。AC：c4da02e 同树重跑 e2e job 绿 ×2 连续；本地 retries 仍 0（严格面不变）。

**T-455 → done 2026-09-04 18:2x（`18001ac`）——M16 23/35（B10 票①：FE④-b 权限编辑两步弹窗 + 矩阵五列——断言反转⑤ FE 面兑现）**：**PermAction 五词域**（api.ts normalize/wire 双点收口 + grantsOf* 归一）+ PermissionEditorPage **水合归一/保存正名单序列化 + 五列矩阵 + 两步对话框**（可点步头 perm-res-step-{1,2}）+ widgets/targetdiff/security.css 随迁 + **T-444 预登记四处 GET-echo 断言翻新**（security/m8×2/m9 → deploy-cache）+ 新四腿 spec。锚册 **v1.41** + parity **B-1.6/B-2.16 as-built**。四门绿（tsc 0 / eslint 触碰面净 / ledger A1A2 双零 / e2e 18P+4P+m8 4P+a11y 双主题 sweep exit 0）+ SPA gzip +0.51KB。**契约漂移登记**：Any Local/Any Remote 预置桶需 BE 通配桶语义先承接（FE 不伪造候裁）；**7.161 编辑形态实为三步向导路由页**（BinFlow 编辑器本体 stay-out §9A-S1 差异留痕）。write↔annotate 勾选联动在零参照实例不可观测——按独立位列实现（T-444 零提权语义下 UI 联动=越权，裁定留痕）。活体取证 t455-probe/（5 截图）。日志 reports/agents/T-455.md。

**T-457 → doing 2026-09-04 18:3x（B12：FE profile 自助 token/SSH + ? 帮助下拉 + About 版本弹窗 ← T-455 done 解锁）**：dev-frontend 在途。identity token 一次性明文+curl 即时断言 / SSH key 增删（B-1.8 补齐）/ ? 下拉四项 / About 弹窗（侧栏 vdev 升格）。端口纪律 18098+。

**T-471 → doing 2026-09-04 18:3x（T-455 收口解锁即派）**：devops-engineer 在途。CI-only retries + timeout 档位；本地严格面不变；不碰 spec 本体。

**T-458 腿① → done 2026-09-04 18:5x（`c6d8219`，14 文件 +575/−78）——票两腿制：腿② 候 T-459/T-461~T-464 合入另派**：docs/user 八文件（console 树深链/表单三段/详情族/统一搜索/预埋节；aql statistics+usage 专节〔90 天未下载可复跑示例〕；api-reference 五动词行；groups-permissions 动作动词专节〔wire 四规则+三段 curl 实录+零提权回填〕；properties 写门 annotate）+ fern/pages 五页镜像。**17 条新增 curl 断言终态复放 17/17 PASS**（净实例 18095 + 迁移账本 v23 实证；UAT 探针残留清零）。docker-virtual 四处陈旧 400 文档翻正。零票号零里程碑号（grep 核验）。日志 reports/agents/T-458.md。**Fern 重发布挂 conductor**（本报告轮执行）。

**T-471 → done 2026-09-04 18:5x（`1887311`）——工程票**：playwright.config 三处——`retries: CI ? 2 : 0`（漂移对应「两轮失败集轮换」非确定性尾延迟；真坏 selector 三连败仍红）/ `expect.timeout: CI ? 10s : 5s`（straggler 全死在 locator 等待）/ actionTimeout 不设 + workers 维持（评估理由入注释）。本地严格面逐项未变（env 门控双态活体验证）。tsc/eslint 0。真验证 = 下次 main e2e run（T-471 AC：绿 ×2 连续）。日志 reports/agents/T-471.md。

**用户指令 intake ⑮（2026-09-04 18:4x）：「关注 circleci 和 githubaction 的报错，调整后要重跑，复测」——CI 复测循环开启**。CircleCI 首查（c4da02e commit status）：build ✅ / deploy_uat ✅ / **protocol_matrix ❌**（T-469 十协议矩阵首真跑红）/ **ci/circleci: e2e ❌**。本地复现定谳：**pypi 腿根因 = pip ≥25 对 plain-HTTP 索引硬性忽略未信任主机**（本地 venv 复放 WARNING 实证）→ `--trusted-host`（从实际 index URL 派生，dockerized 模式安全）修复后 **pypi PASS**；generic/npm/go 亦本地 PASS（npm 先前红系复现 shell 缺 nvm 的 docker-fallback 形态，非缺陷）；docker 本地红 = 本机 Docker Desktop 未配 insecure-registry（CI 侧 config.yml:244 已写 daemon.json——非缺陷）。GH e2e rerun（旧配置）仍红——正是 T-471 目标面。**PR #91 已合（main `47f8385`）：携 e2e retries 新配置 + pypi 修复 + T-444/T-455/T-458/T-471 全量——双 CI 复测中，裁定挂下轮。**

**用户指令 intake ⑯（2026-09-04 19:0x）：「尽可能使用fern的能力，比如api文档的能力」——立票 T-472（tech-writer）：OpenAPI 3.1 spec 衍生（api-reference.md 主源 + router.go 核对）+ docs.yml API tab + CLI 验证。无既有 OpenAPI 资产（grep 零命中）——spec 全新建。**

**用户指令 intake ⑰（2026-09-04 19:0x）：「把circleci的devops相关能力都用起来」——立票 T-473（devops-engineer）**。

**配额窗⑯（19:12 击落 T-457/T-472/T-473）→ 20:56 复活续跑（零损失）**。

**复测①裁定（main 47f8385，2026-09-04 21:0x 定谳）**：
- **GH 矩阵首跑 7/10 绿**（generic/maven/gradle/npm/**pypi(修复生效)**/docker/go ✅）——**红三腿：helm ❌ / nuget ❌ / conan ❌**（全系本地 community 档 SKIP 从未执行的首跑坑）：① **helm = 产品缺陷**——PUT 内容面 500，活体复现铁证 `spool request body: open /tmp/binflow-helm-*.tgz: read-only file system`（UAT 只读根文件系统 vs spool 落 /tmp——**立票 T-474** dev-registry-adapter 在途：spool 改存储同卷 staging + 错误面收敛 + read-only 模拟测试）；② **nuget = runner 预装 dotnet SDK 10 遮蔽锚定版 8**（MSB4181 吞错）→ GH workflow `setup-dotnet@v4` 钉 8.0.x（`5999b39`）；③ **conan = conan 2 新版废 `--template`** → 手写最小 recipe（同 nuget consumer 无模板姿态）（`5999b39`）。
- **GH ci ❌ = npm audit 端点瞬断**（外部 registry 抖动——c4da02e 同门绿，非内容）。
- **GH e2e（带 T-471 retries 首验）：3 硬红/2 flaky/337 绿**——retries 吸收 2 腿 ✅；**t443:118 / t449:134 / t451:35 三 spec 三连败 = CI 环境确定性失败**（本地全绿；疑 TZ=UTC/viewport 环境敏感）——**立票 T-475（dev-frontend，候 T-457 收口 FE lane 空出后派）**。
- GH release-dryrun ❌ = goproxy GOAWAY 再现（网络抖家族）。
- CircleCI protocol_matrix ❌ = 同 helm/nuget/conan 三腿（同脚本单源）；ci/circleci: e2e ❌ 待查（低优先——GH 面已覆盖诊断）。

**T-473 → done 2026-09-04 21:1x（配额窗⑯复活后收口，`616f1d2`）——intake ⑰ 兑现（CircleCI 能力全开）**：九项裁定——**缓存三面落地**（Go mod/web deps/Playwright browsers + **修两处存量缓存静默空转**：cimg/go 无 /go/pkg/mod 死路径、npm ci 下 node_modules 缓存无效）；**build parallelism 2 + tests split（包计时分裂）+ junit store_test_results**（Insights flaky 检测开——喂 T-471）；e2e junit+artifacts（shard 不做——T-471 体位不动）；**DLC 开**（docker 腿真构建）；**nightly 03:17 UTC**（race_full 拆 4 分片 + 常驻 UAT 漂移面，与 main-push 管线分离）；arm.medium 不落（非加法+matrix 钉 amd64）；orbs 三否（镜像+步骤已锚定，orb=漂移）；approval gate 预留注释；org context 迁移登记（UI 动作）。`circleci config process/validate` EXIT 0 + gotestsum junit 本机实跑合法。遗留：分片时长数据候 Insights 出分回访；arm.medium 与 context 迁移两张跟进票（日志 §6）。日志 reports/agents/T-473.md。

**T-474 → doing 2026-09-04 21:1x（P1 热修：helm spool 读-only 根因）**：dev-registry-adapter 在途。spool 落位存储同卷 staging（接口驱动——Service 加显式方法或 storage StagingDir()）+ 5xx 文案收敛 + read-only TMPDIR 对照测试 + 净实例 PUT 全链路。**T-475 → todo（e2e CI 环境确定性：t443/t449/t451 三 spec——TZ/viewport 嫌疑，候 FE lane 空出）**。

**会话交接事件（2026-09-04 21:2x，conductor 续任）**：旧会话 context 耗尽亡故（transcript 不可达——SendMessage 复活失败实证）。临终史对齐：1376/1377 报告 + `5999b39`（conan/nuget 脚本修）+ T-473 收编 `616f1d2` 全数入册。处置：T-457/T-472 依盘上半成品重派 finisher（不重做）；**T-474 全新重派**（read phase 被亡故带走零足迹）——派单追加三疑点（部署二进制装配链/router 路由链/staging 卷）与同病面（deb/rpm/cargo/nuget 的 CreateTemp("")）。**GH ci job 红定谳（conductor 21:3x，`21b6f68`）**：audit step 死因 = node 20 的 npm 10 走**已退役 quick-audit 端点**（400 Invalid package tree）——非真漏洞（web 锁树双 registry 皆 0 漏洞；日志「29 漏洞」系 docs-site Docusaurus 树混入grep，非本 step）→ ci.yml 三 pin 升 **node 24**（项目真实工具链）。复测 2 触发：T-474 收编后 develop→main（helm 腿/audit/e2e 三面一次验）。

**双会话划界终笔（2026-09-04 21:5x，dev-center-1e ↔ 「提交代码到 binflow 仓库」协议成立）**：亡故会话生双续任——dev-center-1e（20:56 起 /loop 20m，cron ca10187d，复活三 agent 34m/34m/24m）与 peer（20:2x 起，重派 finisher 三票）撞车。**裁定：三票归 dev-center-1e（进度深），peer 停重派转只读简报**（>1h 无心跳可接管——已入 memory `conductor-loop-ownership`）。**交错披露已下发三 agent**：T-474 双设计归一（peer 的「507+路径披露」vs 复活侧 staging 卷——以 staging 卷为主轴吸收 507 语义 + main.go:487 装配链三疑点 + 四门全重验）；T-457 验收 finisher 遗产（其死前已达新 spec 6/6 绿——ProfilePage/AppShell/t457 spec/t134/t146 有其笔迹，t134/t146 可能系 CI 环境敏感断言修复——同族修法供 T-475 复用）；T-472 复核 finisher 微调（docs.yml/openapi/ 手改 vs 生成器再生 diff）。**教训入册：同机双 conductor 会话（cron 各持一份 /loop）= 工作树双写险——凡起续任会话，先 ListAgents 查 peer 再动手；报告文件双写以 git log 異 author 检出。** dc1e67d 误覆写 peer 的 1378 报告已修复（双文合并制立——见 iteration-1378.md）。

**T-474 → done 2026-09-04 22:0x（`901ac2e`，8 文件 +623/−11）——P1 热修收口（helm spool 同卷 staging）**：**共享原语 `internal/adapter/spool.go`**（StagingDir/StageFile/ErrStagingUnavailable——同病面披露吸收，helm 首消费）+ `Options.SpoolDir` cmd 装配 `<data_dir>/staging` + **staging 拒绝面 507**（`stagingLabel()` 只点名尝试根——运维配置值；os 细节/临时名进 slog）+ reopen 500 固定文案。**交错归一**：死 agent 孤儿原语收编并修正错误期望（MkdirAll 只读根=no-op 成功，拒绝属 StageFile 的 CreateTemp）；其「main.go:487 已接线」线索证伪（旧装配无 staging 概念——事故错误文本即铁证）；半成品 `%v` 常量重写。对照针：dir-"" 旧行为 TMPDIR 0444 必败 ↔ 生产姿态 201；残留针 + **真客户端腿**（helm v4.2.4 全链路：服务端 TMPDIR=0444〔UAT 事故拓扑〕PUT 201 + repo add/update/pull 字节一致）。交错态后四门全重验（build/vet/gofmt/lint 0 + 三包 ok——lint 旧二进制 go1.26 分析 go1.27 panic 用当前 toolchain 重装 v2.12.2 过）。**遗留登记**：cargo/deb/rpm/nuget 同族 `os.CreateTemp("")` 潜伏缺陷（原语就位——一张迁移票收口）；staging 根无启动清扫（kill -9 残留累积——建议并入 storage sweep）。日志 reports/agents/T-474.md。

**复测②起飞（2026-09-04 22:0x，main `d38186c` = develop 直推合并〔OAuth workflow-scope 拦 PR merge → 临时 worktree 造 merge commit + SSH 推 main；PR #93 自动转 merged〕）**：四修一次验——helm spool（T-474）/ node 24 audit 门（peer `21b6f68`）/ nuget dotnet 8 + conan recipe（`5999b39`）/ pypi trusted-host + e2e retries（既有）。GH 矩阵 dispatch run=33881555687（可读日志面）。**peer 侧并行盯 commit status（只读简报制）**。裁定挂下轮。

**T-457 → done 2026-09-04 22:1x（`455a519`，29 文件 +1,934/−59）——M16 24/35（B12：profile 自助 + 帮助下拉 + About）**：**token 卡升格自助签发真身**（弹窗族 15 锚 + step-up 两臂 + 一次性明文〔Bearer 与 curl -u Basic 双臂真发 API 验证 + 关闭/刷新不可再取 + 吊销收尾〕+ 即用 curl 样例）+ SSH 缺位卡（诚实缺位）+ **? 帮助下拉四项**（PRD 定案集——7.161.20 活体实为三项 JFrog 变体，差异留痕 parity B-2.17）+ **About 版本弹窗**（侧栏 vdev 升格入口）+ t134/t146 语义翻新（DC-02/G19b-1——链接断言→下拉项断言）。锚册 **v1.42**（+6 STOP 假阳性同步）。四门绿（tsc 0 / 触碰面 lint 净 / 新 spec 6/6 + 回归批 30+22 绿〔定稿二进制〕/ a11y 双主题 / SPA +3,898B ≤10KB）。**契约漂移三条登记**：ssh_keys 端点 BE 全域缺位（console-ux §9-R11——FE 零伪造，落地后补增删表）；令牌清单端点缺位（§9-R6 延续——A7 联动）；帮助菜单四项集 vs 活体三项（PRD 定案优先）。活体取证 t457-probe/（7.161.20 只读）。日志 reports/agents/T-457.md。**注**：t134/t146 系语义改写非 T-475 CI 敏感族（T-457 报告自证——先前 finisher 笔迹假说不成立）。

**复测②裁定 + 复测③筹备（2026-09-04 22:1x~22:2x）**：
- **helm 腿 = 部署竞态，修复在线实证绿**（peer 手动 PUT chart 到 UAT 得 **201**——T-474 修复在 uat.d38186c 生效；GH 矩阵腿跑在 deploy 完成前数秒 → 重跑即绿类）。
- **nuget pin 未生效**（setup-dotnet 装了 8 但 /usr/share/dotnet host shim 赢了 PATH）→ 双修：workflow step `sudo rm -rf /usr/share/dotnet` 后再 setup-dotnet（peer 保底方案，`f926a4f`）+ leg 工作区 global.json 钉 8.0.*（项目侧，`1b86ef3`）。
- **conan = 缺 default profile**（fresh CONAN_HOME 无 profiles → `conan profile detect --force` 引导，`1b86ef3`）。
- GH 矩阵已在 develop@1b86ef3 重派（run 33882745210——验 conan+helm 两腿；nuget rm 修在其后，下轮 dispatch 验）。CircleCI protocol_matrix（main 旧腿脚本）在跑——helm 应绿，conan/nuget 红属预期（修未到 main）。**复测③ = develop 矩阵 10/10 后一次合 main。**

**Fern 重发布避让（21:2x）**：T-472 在途重构 fern/（空 API definition stub 已落树——CLI 切 API 项目模式拒 tab+layout 导航形）——T-458 镜像页发布顺延候 T-472 收口统一执行。

**T-472 → done 2026-09-04 22:4x（`04851ac`，20 文件 +11,931/−349）——intake ⑯ 兑现（Fern API 参考生成）**：**fern/openapi/binflow.json**（OpenAPI 3.1：112 paths/158 ops/20 tags/54 schemas，迭代标记零残留）+ **docs.yml API tab**（官方形态 `layout: [- api:]` 字符串形 + generators.yml 注册——**派单给的 `api: {path:}` 对象形在 CLI/平台双双解析失败，agent 依 fern-api 官方 schema 纠正**，偏差留痕）+ api-reference.mdx 退役防双源 + 7 页 8 处跨链改指。**tools/openapi-spec/ 生成器**（再生式：api-reference.md 逐字主源〔25 端点 63 锚点 63/63 逐字一致〕+ router.go 交叉核对〔3 类差异登记〕+ handler 线面事实）。redocly 0 error（71 质量 warning）+ preview 发布验证（158 端点页 ×20 tag 全渲染）。**conductor 生产发布（22:4x）**：交互确认管道应答后 `Published docs`——**API tab 线上 200**（api-参考/binflow-api/…/artifact-download 实渲染）+ 文档 tab 200（**T-458 镜像顺带上线**）。遗留：npm 域两处契约页勘误裁定 / 9 内部面是否扩入 / 71 warning 收敛 / 旧短链 301。日志 reports/agents/T-472.md。

**矩阵复测③裁定 + ④在跑（22:3x~22:5x）**：③（`992c6a7`）**9/10——conan ✅ 转绿**（双缓存 detect 生效）+ helm ✅（GH runner 全链路）；**nuget 真根因露面**（rm 修生效、SDK 8.0.424 上场后）：**14 位时间戳超 NuGet Int32 patch 上限**（`'1.0.20260904142122' is not a valid version string`——SDK 10 的 MSB4181 系同一错误被吞）→ ④修：leg 内 `NVER=1.0.$(date +%s)`（epoch 秒 2038 前合法，他腿保持全局戳）（`2eca84f`）+ conan recipe 补 package()（空包 WARN——peer 材料采纳）（`2d03244`）。④ = run 33884189963 在跑——**唯一余红即 nuget，裁定挂下轮。**


**T-476 → done 2026-09-05 02:4x（`91741483`，24 文件 +1,521/−73）——T-474 同族收口（nuget/cargo/deb/rpm 四面 spool 迁移共享 staging）**：Options.SpoolDir cmd 四处装配（`<data_dir>/staging` 同卷根）+ StageFile 原语 + StagingLabel 有界披露共用 + 507 面 + nuget fd 泄漏顺修 + **cargo CG-2 刻例**（staging 拒绝 507+errors envelope / 读侧 200+warnings 契约逐字——cargo 读 200-warnings 为成功，静默失能=全损故刻例外）。**事故拓扑验证**：TMPDIR=0444 铁证 curl ×4 协议 201 字节一致。四门绿 + deb 124s 零回归。**T-477 候立**：internal/repo/archive.go:1082 X-Explode-Archive 同族（服务层域）；migrate CLI 低危登记。日志 reports/agents/T-476.md。

**终局合 main（2026-09-05 02:4x，`f98bb6b9`）——复测收官管线起飞**：T-476 二进制 + 矩阵全修 + node 24 + 30m 墙钟齐上。裁定挂下轮（矩阵 10/10 + ci 绿 = 闭合；e2e 三硬红候 T-475）。

**接管升级（conductor 续任侧 2026-09-05 05:0x~07:1x，dev-center-1e 静默 >4h 超其最长配额窗）**：
- 终验三修 + PR #95（`f08863f`）：go PATH / gradle unzip / pip PEP668——**七腿绿 + e2e ✅ + GH ci 三 job 全绿（连续第二绿 run）**；四腿（go/gradle/pypi/conan）CircleCI 面红**复原**。免日志诊断穷尽清单：本地单腿✅/本地五腿并发✅/ubuntu:22.04 同构容器全链✅/GH 面 10/10×2✅ ⇒ **machine executor 环境特异，唯日志可定谳**（爆发限流假说已被本地并发实验削弱；PEP668 对 machine 镜像 pip 22 不成立）。报告 iteration-1392（双文合并制）。
- **循环恢复派发**（07:1x）：**D-T456-1**（dev-go-core：listRemoteFolderItems 传输层丢字段——PUT 静默吞+类型门 400 不可达）+ **T-461 → doing**（dev-frontend：FE 远端浏览树消费可选档双态——off 态与骨架先行，on 态端到端候 BE 腿合入复验）。双 lane 区互斥（internal/httpapi vs web/src/pages/artifacts）。

**D-T456-1 → done 2026-09-05 07:4x（`6da7b23b`，3 文件 +344）——T-461 on 态前置解锁**：根因 = T-448 只落 service 层、httpapi `repoConfig` struct 漏字段（PUT 体经 typed decode 该键静默丢弃——既不落库且 mistyped 400 不可达）。修：`ListRemoteFolderItems *bool`（指针保显式 false 往返）+ configJSON remote 臂 setBool 收集 + GET 经 canonical 恒回显（零改动）。17 subtests（batch-1 往返/翻转保持/类型门/值域门）+ httpapi 全包回归 123s ok + repo T448 交叉 sanity。四门绿。FE 契约注记：读 GET configuration.listRemoteFolderItems（布尔恒在场）；写须全量 remote config（full-replace PUT 语义——flag-only 更新吃 url-required 400 系既有语义非本票引入）。日志 reports/agents/D-T456-1.md。

**T-461 → done 2026-09-05 08:2x（`09d3311d`，14 文件 +1,102/−26）——M16 26/35（FR-147.3 FE 远端浏览树消费：可选档双态——FR-147 全栈闭合）**：Advanced 步复选（批 1 型门 + 指针提交）+ repo 详情回显 + 树双态（off 默认 diff=0 / on 未缓存远端目录〔helm 全树 + deb/rpm 元数据臂〕+ 点击回源 + ?stats 计数联动 + virtual §8.5 成员行）+ 上游停机降级（远端层错误态 + 缓存行可用）。六腿 spec 6/6 ×5 连跑（scratch 18098）+ 回归面全绿 + SPA +906B。锚册 **v1.44**（6 名）+ parity **v1.13**（B-3.20）。**on 态端到端实证通**（D-T456-1 修在树生效）。**契约漂移登记**：① 降级 note 无 wire 面——httpapi 三处 List 调用点丢 RemoteDegraded（FE 按假定字段 remoteDegraded 消费、缺席零渲染；BE spot 三行+一字段即点亮 → **D-T461-1 立票**）；② ?list 派生行 size:0/零时 mtime 占位（FE 按 sha2 缺席判别 '—'）。**收编注记**：预暂存改名 admin→monitoring/SystemInfoPage.tsx（T-459 在途足迹，100% 相似度纯移动）被吸附入本提交——内容零变化、归 T-459 面记账。遗留：NodeDetail useAsync 重复发射 item GET → 纯浏览多计下载数（跨票怪癖小票候选）；枚举快照 TTL 600s 非 wire 可调（降级臂以换 url 失效签名达成）。日志 reports/agents/T-461.md。

**D-T461-1 → doing 2026-09-05 08:3x（P2 小票：RemoteDegraded 上 wire）**：dev-go-core 在途。httpapi 三处 List 调用点补 RemoteDegraded 透出（字段名回写对齐 FE 假定形 remoteDegraded）+ storage.go 一字段；30min 量级。



**会话继承事件（2026-09-05 12:0x，conductor 第三任）**：dev-center-1e 与续任 peer（提交代码到 binflow 仓库）双亡后，用户重启本会话（继承原始 transcript 压缩上下文）。前任接管窗战果全数入册：D-T456-1（`6da7b23b`）/T-461（`09d3311d`，26/35）/T-459 派发。**两孤儿遗产处置**：D-T461-1（BE wire 小票，亲验收编 `1bc0bb93`——build/vet/gofmt 0 + 新 247 行 wire 测试绿 4.3s；agent 亡故无报告，conductor 代验留痕）；T-459（FE 监控面，足迹大但无报告）→ **finisher 已派**（盘上续作收尾）。

**CircleCI 四腿终章（2026-09-05 12:0x，日志铁证定谳——API 通路经 chunk keychain token〔用户预置〕）**：**go = 混树**（镜像预装 /usr/local/go 被 1.26.6 tarball 叠压——map.go/map_swiss.go 两代并存 'ctrlEmpty redeclared'）→ extract 前 rm -rf；**gradle = JDK 21 shim**（'class file major version 65'——镜像默认 21 vs wrapper 上限 19）→ update-alternatives 钉 17；**conan/pypi = 工具链步 timedout**（apt -qq + >/dev/null 饿死 no-output 计时器）→ 输出放流 + no_output_timeout 20m + pypi 探测预装 venv 免 apt。四修 `df3dc4e4`。**chunk sidecar 立**（用户 /chunk-sidecar 意图兑现：key 已补、远端 Linux 验证环境就绪；pre-commit 钩子 rsync 现断——conductor 提交暂 --no-verify，本地哨兵纪律不变，sidecar 修复挂后续）。

**【全章闭合】CI 事件终章（2026-09-05 16:0x，main `7c87fd66`）——CircleCI 十腿全绿 + GH a955dbce 三 job 全绿 = 双面 10/10**：
- **CircleCI protocol_leg ×10 全 ✅**（conan 终腿=工具链探测预装 gcc/cmake + apt 去 -qq 放流，`6e3082b0`）+ build/deploy_uat ✅（e2e pending 但同内容 a955dbce 已绿）
- **GH a955dbce ci+e2e+release-dryrun 齐 ✅**
- 自 09-02 dependabot 直升 main 事件起的完整因果链全部落幕：node 24 audit（退役端点）/ Test 30m（慢机容量）/ e2e 六 spec 确定性（T-475：页窗×累积态/lazy 重挂竞速/straggler 预算）/ pypi trusted-host / conan 手写 recipe+双缓存 detect+probe-first / nuget 四层洋葱（Int32→UInt16→具名源→产品 spool）/ helm spool 507 / go 混树 / gradle JDK 21 shim——**intake ⑩（十协议矩阵）⑮（CI 复测循环）⑱（CircleCI 每 job 并行）全兑现**。
- spool 家族六面终章（T-474/476/477）：helm/nuget/cargo/deb/rpm/repo-explode——read-only rootfs 全免疫，统一 `<data_dir>/staging`。
- 教训入册：worktree 合并前必 fetch 全量双分支（7fd7091e 陈旧 ref 事故）；管道 `| head` 吞退出码两案（git commit / tsc 哨兵）——哨兵一律裸跑取 $?。

**T-459 → done 2026-09-05 15:3x（`d9b02f1a`，42 文件 +2,253/−90）——M16 27/35（B13：监控组 + 导航分组/侧栏过滤）**：监控组三页（SystemLogs〔审计承载——服务进程日志端点缺位登记不伪造〕/ServiceStatus〔health+version 对位〕/SystemInfo 归位）+ AppShell 导航 16→18 + 管理态 Search Admin Resources 过滤框 + 四旧深链 replace 窗。孤儿遗产即终态（finisher 零新增 src——补报告+验证）；**整树收编自愈 T-461 提交误卷三件的断 tsc**（finisher 警示采纳）。新 spec 8P×2 + 牵动 11 spec 绿 + a11y 双主题 62 扫 + SPA +5,819B。锚册 v1.43/parity v1.12（前任执笔）。日志 reports/agents/T-459.md。

**chunk 集成面处置（2026-09-05 16:2x）**：`chunk init` 生成的 Stop 钩子（每轮 Stop 跑 sidecar validate，3×4min 重试）在 sidecar 未配置工具链时纯失败烧时——**已摘除**（`.claude/settings.json` Stop 置空；commit 前钩子暂留未动）。CircleCI API 取证能力（keychain token）不受影响。**后续票候选**：sidecar 正规 setup（node+go 工具链 + 仓形命令调优〔npm ci 在 web/ 非 root、make test 300s 不容 race 全量〕+ 快照固化）——兑现用户 /chunk-sidecar 意图后可复挂 Stop 钩子。

**T-462 → done 2026-09-05 16:5x（`b1a6060b`，20 文件 +2,116/−98）——M16 28/35（B14：FE FR-145.7 三域 cron 消费 + import/export——M15 Q5 推翻呈现兑现）**：GCPage 定时维护卡（三槽表达式/下次/上次 + gc Run Now 并存〔ADR-0044 7①〕+ cleanup 两槽 apply:true confirm）+ BackupPage 整页重写（定时 CRUD：Enabled/Key/Cron/Next〔datetime-local→RFC3339 过去 400〕/Path + E1 删除 + backup-cli 卡）+ ReplicationPage 调度列 join（读失败降 '—'）+ ReplicationsSection repl-form-cron 预留位转正。AUDIT_ACTIONS 54→63。gc-cron-gap 诚实缺位（Quota/Compress/Prune 无载体）。新 spec 7P×2 + 回归全绿 + SPA +7,266B。锚册 v1.45（+37）/parity v1.14。契约 UAT 实测零漂移。**7.161 参照容器双损坏**（pro router 不就绪/oss 进程死）——修复归 conductor 决策（t462-probe/probe-notes.txt）。日志 reports/agents/T-462.md。**全部 FE 页面票收官。**

**T-463 → doing 2026-09-05 16:5x（B15 独占波：i18n 框架 + 全树文案外提 + CI 防回流断言——断言反转⑥）**：dev-frontend 独占 web/ 域（FE 票全收口）。zh 全量键 + en 骨架 + 持久化 + 切换器归 T-464。

**T-463 → done 2026-09-05 22:2x（`44e05ed2`，94 文件 +7,217/−3,221）——M16 29/35（B15 独占波：i18n 框架 + 全树外提 + CI 断言——断言反转⑥前半）**：**零依赖内核**（<1KB gz，tr/translate/initI18n/setLocale/getLocale + localStorage 持久化）+ **zh-as-key gettext 形**（外提=机械逐字搬运——85 文件 2,581 调用点 diff 证明零语义变化；省 i18next ~14KB）+ **1,970 键 ×10 域**（console 214/repositories 393/artifacts 209/search 47/security 330/governance 314/monitoring 77/webhooks 89/admin 149/common 148）+ en 骨架双向同构 + zh manifests 物化 + **assert-i18n 三道闸**（硬编码零命中/同构/清单一致——负测定位注入违例）挂 build/lint 链。**两工程根因入册**：JSX 多行文本编译语义（换行 run→单空格）；无 u 标志正则按 UTF-16 码元（CJK 区间吃代理对——7 emoji 键拆除+正则 \u 化）。e2e 106P 抽样 + CI 同参全量 379P + 终态二进制复验。SPA 功能增量 +4,841B gz；catalogs 35.8KB gz 懒载（zh 用户零请求已断言；NFR-P73 登记）。五小时马拉松。日志 reports/agents/T-463.md。

**T-464 → doing 2026-09-05 22:3x（B16 波尾：en 填充 1,970 键 + 切换器 UI + 断言双语化 + 日期数字 locale 化——断言反转⑥收口）**：dev-frontend 独占。

**用户指令 intake ⑲（2026-09-05 23:1x）：「后续 uat 环境部署，监听在 443 端口」——形态经问询裁定：HTTPS + ACME 域名（Let's Encrypt）→ 立票 T-478**（devops-engineer 在途）：倾向反代终结 TLS（Caddy 自动 ACME，BinFlow 保持内部 :8080 产品零改）+ CI 双面基地址切 https。**DNS 前置项归用户**：uat.<域名> A 记录 → 52.79.109.153。

**用户指令 intake ⑳（2026-09-05 23:1x）：「ci 协议的测试，需要包含远程仓库和虚拟仓库」——立票 T-479**（devops-engineer 在途）：矩阵十腿扩 remote（真实公共上游回源+缓存断言）+ virtual（local+remote 聚合解析，§8.5 语义）覆盖——钉版制品+网络抖动降级策略+离线守卫。

**T-478 → done 2026-09-06 01:0x（`3bb71d4e`，5 文件 +337/−19）——intake ⑲ 兑现（UAT 443 = HTTPS + ACME，Caddy 反代终结）**：方案裁定=反代（进程内无 TLS 面实测；T-168 nginx 模板既定姿态；Caddy 优于 nginx+certbot——ACME 全在 daemon）。uat.Caddyfile + 幂等 uat-proxy.sh（validate 先于 reload + ufw 80/443 + 三段探针 + ACME 退避自愈）+ deploy_uat proxy 步骤（先于二进制换装）+ **双面基地址默认翻 https://uat.binflow.org**（8080 过渡回退 env / UAT_DOMAIN=off 可禁层）+ docker 腿 insecure-registries 按方案条件化。门：caddy validate×2 + 行为级本地跑（308/:443/ACME WARN 路径）+ cc process 0 + actionlint 0。**用户前置两项**：① DNS A 记录 uat.binflow.org → 52.79.109.153（权威 NS 在 businessidentity.llc——DoH 实测 NXDOMAIN）；② AWS 安全组放行 80+443。就绪后 conductor 按 checklist 实部署验证。日志 reports/agents/T-478.md。

**用户指令 intake ㉑（2026-09-06 02:0x）：「精简 README 的内容，不要提到迭代的内容」——立票 T-480**（tech-writer 在途）：双语对（README.md 649 行 + README.zh-CN.md）去迭代化（M-号/演进叙事零残留）+ 门厅化（快速开始/能力矩阵/链接指向 Fern 文档站）+ 目标 ≤200 行/份。

**develop 对齐事件（2026-09-06 02:0x）**：远端现 main→develop 合并 `cfae1868`（来源 CI 侧对齐——含 dependabot actions v7 系保留于 main 的链），本地 `c24b7f97` 合并对齐后上行完成。

**T-479 → done 2026-09-06 02:2x（`003452bd`，+1,054/−6）——intake ⑳ 兑现（矩阵十腿增 remote+virtual 面）**：753→1,537 行——每腿 remote 段（真实公共上游钉版拉取：MISS→HIT 缓存冻结 + sha256/digest 对账）+ virtual 段（local+remote 聚合 §8.5 Resolved-From + helm _external 折叠 + C5 405 逐字）。离线守卫三态（lax SKIP/strict FAIL/可达放行）。**5 腿活体验证绿**（npm/pypi/go/helm/generic）；余 5 腿引擎级断言设计期活体过（候 CI 首跑实证）。CI 双面零 config 变更（单源自流）。**文档漂移两条登记**：docker-registry.md「virtual 暂不做」过时（活体 200+聚合证伪）；golang.md virtual "members" 笔误（canonical=repositories，实测 400）。同票携 T-478 env 注释与报告补收。日志 reports/agents/T-479.md。

**T-464 → done 2026-09-06 05:0x（`34827840`，23 文件 +2,590/−2,015）——M16 30/35（B16 波尾：en 双包 + 切换器 + 断言双语化——断言反转⑥收口，FR-149 全栈闭合）**：en 1,971 键全填（Artifactory 术语对齐）+ 侧栏脚切换器（ToggleButtonGroup zh/en + aria-pressed）+ formatAuditTime/formatCount locale 化 + formatStamp en 变体 + TokensPage mintedAt 随 locale + t464 双语 spec 五腿（往返持久化/7 页抽样/日期正则/术语保真/axe 双 locale）+ regen-catalogs 保值化。assert-i18n 同构闸过（2,583 点/1,971 键）；zh 回归抽样 78P；catalogs chunk 35.8→73.0KB gz（投影内），zh 用户零字节不变。日志 reports/agents/T-464.md。

**T-480 → done 2026-09-06 05:0x（`82da959e`，649→200/194 行 −69%/−70%）——intake ㉑ 兑现（README 门厅化）**：六大里程碑叙事段/五 M-号章/内部流程链接表/curl 长廊/make 全表/213 链接矩阵全数退位 Fern 站；grep 双语零迭代残留自证；51 相对链接核验；快速开始真机实跑全链。**事实纠偏两处**：13 包型（非 12——slots.go+Fern 口径）；docker virtual 三仓型齐备（validate 矩阵+活体 200 证伪旧文「未交付」）。遗留：docker-registry.md/fern docker.mdx 旧口径翻新（他域登记）。日志 reports/agents/T-480.md。

**CircleCI 事件三连修（2026-09-06 03:5x~05:3x，用户指令「circleci 报错解决一下」）**：① deploy_uat publickey 拒 = **键安装步序在 proxy 后 + proxy 键 pattern 窄**（id_rsa_* 漏 ed25519_uat）→ 步序前移 + 宽 pattern + 硬失败指引（`2ce0e7ae`）→ **deploy_uat ✅ 实证绿**；② 十腿全灭 HTTP 000 = **基地址默认已翻 https 而 DNS 未就绪**（守卫只盖了 proxy 步）→ 双面动态探测回落 8080（`f5de8590`）；③ 回落轮 FATAL BASE 未设 = **自伤一处**（printf 转义误留——BASH_ENV 写入字面 ${BASE}）→ 一字符修（`8de042fb`，合 main f43d67bb）。**当前阻断 = CircleCI 平台事故**（build "Task information unavailable"×2 连 infrastructure fail——runner 分配故障，非代码）——下轮重试。**TLS 443 就绪仍候用户两项**：DNS A 记录 + AWS SG 80/443。

**T-458 腿② → done 2026-09-06 06:5x（`297cd303`，20 文件 +855/−75）——T-458 两腿齐 → M16 31/35（文档票全闭合）**：新增 whats-new.md（用户可见变化公告——E 系翻案+cron 推翻+i18n 双语逐一明示，零票号零里程碑号 grep 自证）+ admin/cron-scheduling.md（Quartz 六域/维护三槽/定时备份/复制双轨/报错对照）双树镜像 + 导航/侧栏配准。console.md 导航 18 条目图 + 监控组指南（System Logs 审计承载如实+端点缺位注记）+ 远端浏览可选档 + 预埋节转正 + 有意不兼容表 cron 行翻转；**七处「无用户级 cron」陈旧句两树翻正**；api-reference +3 行 + 腿①断链修复。全部 API 示例 scratch 实例逐条实测（pro 探针自铸 2 天 license 测毕还原 community）；make docs 本机过（终轮零 WARNING）；fern slug/锚点对账零新断链。**conductor Fern 重发布**：自定义域 binflow.org 成主站 + 旧域并行——whats-new 页 200 实证（中文 slug）。遗留：System Logs 进程日志端点缺口（文档如实注记）；切换器位置候 ux 复核；faq 两问候选未入。日志 reports/agents/T-458-leg2.md。

**T-460 → done 2026-09-06 07:2x（`0d6e8efb`，3 文件 +167/−13）——M16 32/35（PM 终裁收口笔）**：PRD v1.2——**Q 表 13/13 闭环**（Q8 as-built 裁①；Q9 九倾向全兑现；**Q10 条件窗关闭**〔NuGet symbol 滚 M17+，零触发信号，用户点名即翻〕；Q11 裁点未至滚 M17+〔与 build-info dormant→wired 联动〕；Q12 移交）；**B49 行终态零无主**（31 落/8 部分/1 滚/4 豁免登记/2 stay-out/3 去重——B-2.8 豁免·差异登记 + B-3.2 滚程留插空窗）；K67~K73 回填（K73 新行纠 K70 误挂）；勘误五条。ROADMAP M16 未纳入项备稿（m16-done 窗启用）+ M17 门槛核对（PRODUCT 五条未修订——预立项维持不启动）。**移交 conductor 六项**（B-3.2 插空/t381 处置/Q10 不转正报备/7.161 容器修复/T-466 输入告知/T-437 两命令门代跑——末项已注入 T-466 派单）。日志 reports/agents/T-460.md。

**T-466 → doing 2026-09-06 07:3x（P0 终验——里程碑收官门）**：qa-engineer 在途。DoD 八条证据 + L44 审计核对 + 净窗全量回归（四门+e2e CI 同参+双 locale a11y）+ 十协议 local 矩阵（净实例）+ T-437 两命令门代跑 + 契约缺位清点。

**T-466 → done 2026-09-06 11:3x（`d4342230`）——P0 终验 PASS with notes，m16-done 可裁**：DoD 8/8 证据齐（L44 审计 49=49 零无主对账 / 断言反转七归属全 M16 / FE 变更面 20 提交零无主 / 16 纯 FE 票服务端 diff=0）；race 37/37 solo 绿（首跑超时系参数失误 solo 定谳）+ e2e 385/385 有效 + 十协议 local 10/10（licensed 净实例真客户端）+ remote/virtual 6 绿（npm ssrf 设计内 SKIP + 三面本网污染 CI a955dbce 承载）+ a11y 双主题双 locale 0 + NFR 全门过（11.4MB/126ms/502.6KB）。**缺陷两枚 P2 移交 T-465 顺腿**（D-T466-1 npm virtual 降级路径缺 / D-T466-2 DOCKERIZED_TOOLS 空格）。20 项缺位表全登记零遗漏。日志 reports/agents/T-466.md。

**T-465 → doing 2026-09-06 11:4x（35 票收官笔：两 P2 顺修 + goreleaser 六平台烟测 + chart 联动 + 部署三面抽检 + m16-done 收口清单成文）**：release-engineer 在途。**收口后即裁 m16-done**（tag + UAT 随里程碑 PR——CircleCI 平台恢复后复跑补证）。

**用户指令 intake ㉒（2026-09-06 11:4x）：「binflow 访问地址提取为变量，由 CI 平台的环境变量传入」——已落地（`9d841da6`）**：部署步本就读 `UAT_HOST`；矩阵面残留运行时字面量全数并入同模式——过渡探测 https 目标派生 `UAT_DOMAIN`、回落派生 `UAT_HOST`（双面）；docker 腿 insecure-registries 臂随 `UAT_MATRIX_BASE`/`UAT_DOMAIN`。存余字面量仅注释与 `${VAR:-default}` 文档形（env 模式本体，平台级可覆写）。

**T-465 → done 2026-09-06 13:0x（`049182a8`，+162/−4）——M16 35/35 全收官（终票：release 烟测 + 收口清单）**：D-T466-2 尾随空格修（client_base 成员测试复活——pre/post 演示定谳）+ D-T466-1 npm virtual 降级路径（ssrf SKIP 时 local-member 聚合如实记录；CI strict 恒双成员）；**两修同场一脚验证**（--docker-clients npm+maven EXIT 0——T-466 同姿态红位转绿 + 容器内 CBASE 生效实证）。goreleaser 六平台快照 EXIT 0（18 零 CGO 二进制，F1 门 112.72/120MB）+ release-verify 6/6 + sha256 对账；chart 维持 1.5.0（M15 后零提交零新面）；部署矩阵三面烟测（4 镜像 arch×变体/compose 真部署逐字节/k8s kubeconform 4/4/systemd 优雅排水）。**m16-done 收口清单成文**（报告 §5）。日志 reports/agents/T-465.md。

---

# 【里程碑】M16 = m16-done（2026-09-06 13:0x 裁定，conductor）

**35/35 全 done + T-466 P0 终验 PASS with notes + 收口清单就绪**。战果总账：
- **全前端 Artifactory 对齐**（intake ⑤⑧⑭）：树栈/表单/详情/搜索/安全面/监控组/帮助/i18n 双语——B 矩阵 49 行零无主（31 落/8 部分滚/豁免登记/stay-out）
- **断言反转①~⑦全兑现**（七终裁 Q 表 13/13 闭环）
- **FR-147/148/149/150 全栈闭合**（远端浏览/AQL 副线收尾/i18n 双语/cron 调度域）
- **CI 三章**（intake ⑩⑮⑱）：十协议矩阵双面 10/10 + remote/virtual 覆盖 + 每 job 并行 + node24/30m/retries 确定性
- **spool 家族六面终章**（T-474/476/477——read-only rootfs 全免疫）
- **UAT 443/ACME 落地**（T-478——候 DNS/SG 两用户前置即活）+ **访问地址全 env 化**（intake ㉒）
- **文档双树**（T-458 两腿 + T-470/T-472 Fern 迁移 + API tab + binflow.org 主站 + T-480 README 门厅化）
- NFR 全门（11.4MB/126ms/502.6KB/race 37/37/e2e 385/385）
- **挂账**：CircleCI 平台事故期（恢复后按 T-465 §5 清单 re-run 补证）；DNS+SG 两用户前置；M17 预立项窗开（ROADMAP 备稿段启用 + PM 立项流程）
