# Sprint 296 迭代报告 — T-157 收口 + 第三处装配缺口建票 T-179 + T-158 派发

**日期**: 2026-08-22
**上轮**: Sprint 295（T-170 收口 + T-178 派发）
**本轮焦点**: P0 认证端点收口；装配缺口清单增至三处（T-178/T-179）

## 阶段 2 — 收口

### T-157 — OIDC/LDAP HTTP 端点挂载 ✅ 核验通过

**agent 产出**：`httpapi/oidc.go`（login 302 state+PKCE S256 / callback code→token→验证→自动建用户→session；事务 cookie 全路径清除）+ Deps.OIDC（nil→404 不暴露）+ router 挂载 + 登录豁免重构（陈旧 cookie 不锁死 SSO）+ 两组测试（mock IdP 7 组 / 真 LDAPProvider 7 例全栈）。
- AC② 说明：session handler 本就消费 AuthenticateCredentials（T-156 回退在内），以全栈测试验证 HTTP 面接通——合理。

**conductor 复核**：scoped `-run TestOIDC|TestLDAP` race 绿 6.4s + build/vet 干净；agent 自跑全量 httpapi 117.8s 绿（全量统一复跑安排在 T-178 合入后，避免同包在途干扰）。

**第三处装配缺口 → T-179 [P0] 已建**：config 无 auth.oidc/ldap 段、cmd 未构造 provider、auth 缺 userCreator 导出——阻塞 T-174 真实 IdP 验收。whoami source 硬编码也并入。dep T-178（串行避让 cmd 冲突）。
**Q5 补充**：本地/LDAP 同名用户冲突行为未定（现状 500）——入开放问题清单。

## 阶段 3 — 派发

- **T-158 SSO 登录 UI** 已派发（T-157 收口解锁；web 区空闲）。指引：探测 OIDC 启用无公开端点——务实容错方案自选，如需后端小端点报告提需、conductor 开票，不许越区写后端。
- 在途 4/4：T-158 · T-162 · T-169 · T-178

## 阶段 4 — 落盘

- ✅ BOARD.md：T-157 → done（**M6 14/26**）；T-158 → doing；T-179 新票（Batch 9）；状态行更新
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-157 ✅ P0） |
| 派发 | 1（T-158）+ 建票 1（T-179 P0） |
| 在途 | T-158 · T-162 · T-169 · T-178（4/4） |
| 已完成 | M6 **14/26** |

装配缺口累计三处（S3 引擎 /readyz+接线 T-178、认证 config+cmd T-179）——引擎与端点层已齐，缺服务端组装，符合「库先齐、装配收尾」的批次结构。

下轮重点：T-178 收口 → 全量 httpapi 统一复跑 → 派 T-179。待用户：重派 T-165/T-168；Q1~Q9（新增 Q10：同名冲突）。