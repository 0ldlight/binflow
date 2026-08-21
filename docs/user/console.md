---
title: Web 控制台使用指南
sidebar_position: 30
---

# Web 控制台使用指南

> 适用版本：M4（Web 控制台 + 会话认证；PRD milestone-4 v1.2、ADR-0014 勘误后、console-ux v1.2）。
> 本文命令在本机 scratch 实例（commit `7593d8e` 构建，含内嵌控制台）上复跑：301 跳转、登录/whoami/登出、CSRF Origin 拒绝、保留字建仓全拒均按预期（蓝本为 T-103/T-105 验收序列，报告见 `reports/agents/T-103-qa.md` / `T-105-qa.md`）。浏览器矩阵依据 T-104（Chromium 47/47）与 T-120 修复后的跨引擎复核。

M4 起单二进制自带 Web 控制台（go:embed，零外部依赖、断网可用）：建仓、浏览制品、授权、审计、GC、配额全部有界面。控制台是**管理面**——CI 与脚本继续走 REST/token，两者同一 API、同一权限模型。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 一个本地账号（默认 `admin` / `$ADMIN_PW`）。控制台**没有匿名模式**：匿名读只作用于制品内容路径，未登录一律先到登录页。
- 浏览器：**Chromium 系**（Chrome/Edge，QA 主矩阵）；WebKit（Safari 内核）与 Firefox 支持登录/制品树/上传链（见[浏览器兼容](#浏览器兼容)）。

## 登录与会话

浏览器打开 `$BASE/binflow/`——服务端 301 到控制台 `$BASE/binflow/ui/`（控制台是嵌入二进制的 SPA，无需单独部署）：

```bash
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/binflow/
# 301 http://localhost:8080/binflow/ui/
```

登录（用户名 + 口令，无「记住我」——本地用户模型无邮件通道，改密入口在登录后的设置页）：

- 错误凭据：行内红字「用户名或密码错误」；服务端 401 文案对「口令错」与「用户不存在」**完全相同**（不泄露用户存在性），并落 `login.failed` 审计。
- 成功：服务端签发会话 cookie 并跳转 `return` 参数指定的原路由。

会话机制（服务端存储，curl/CLI 可完全等效操作）：

```bash
# 登录（JSON 或 form 均可）→ 200 {"username":"admin","admin":true} + Set-Cookie
curl -s -c jar.txt -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<口令>"}'
# whoami（前端路由守卫同一端点）
curl -s -b jar.txt $BASE/binflow/api/v1/session        # 200 {"username":...,"admin":true}
# 登出 → 204，服务端吊销会话；同 cookie 重放 → 401
curl -s -b jar.txt -X DELETE $BASE/binflow/api/v1/session -o /dev/null -w '%{http_code}\n'   # 204
```

行为要点（均经 QA 实测）：

| 面 | 行为 |
|---|---|
| cookie | `binflow_session`；`HttpOnly; Path=/binflow; SameSite=Lax`；TLS 部署（`server.base_url` 为 https 或请求为 https）自动加 `Secure`，明文 HTTP 不加 |
| TTL | `console.session_ttl_hours`（默认 **24**）；`console.session_ttl_seconds` 覆盖键（测试粒度，同给时 seconds 胜） |
| **会话必死语义** | 滑动续期被绝对 TTL **封顶吞没**：会话必死于 `登录时刻 + TTL`，**与活跃度无关**——默认配置下活跃用户 24h 也必然掉线重登（T-110 塌缩定案）。不要按「一直操作就一直在」规划长任务 |
| 重启 | 会话落元数据库，实例重启**不掉线**（重启后 whoami 仍 200） |
| 日志 | session 值不进任何日志（签发/吊销记 info 级结构化日志，不含值） |
| 等价性 | 会话 cookie 与 Basic/Token **等权**：内容路径、npm packument、pypi simple、管理面同一认证。唯一例外是 docker `/v2` 面——cookie `Path=/binflow` 作用域结构性不含根级 `/v2`（见 [FAQ](faq.md#docker-login-为什么不走控制台的会话)） |

**CSRF 防线（Origin 同源校验）**：会话 cookie 认证的**非 GET/HEAD** 写请求，携带 `Origin` 且非同源 → **403**；无 Origin（curl/CI）或同源放行。Basic/Token/Bearer 认证天然免疫——**CI 与脚本零感知**：

```bash
# cookie + 跨站 Origin 的写 → 403（E-01 信封）
curl -s -b jar.txt -X PUT $BASE/binflow/generic-local/a/f.txt \
  --data-binary @f.txt -H 'Origin: http://evil.example' -o /dev/null -w '%{http_code}\n'   # 403
# 同一请求去掉 Origin → 正常写入（201，仓库存在时）
```

## 界面走查（六组页面）

### 1. 仪表盘（`/`）

五张卡片（实例/健康/存储/仓库/最近审计），各自独立加载独立到达——健康卡慢不影响其余。空实例给「创建第一个仓库」CTA。remote 仓的 assumed-offline 状态会在此汇总（仅列非健康项）。

### 2. 仓库（`/repositories`）

- **列表**：type/packageType 过滤；remote 的 assumed-offline 黄标；virtual 行显示成员数。
- **建仓**：三步表单（类型 → 标识与来源 → 策略）+ 实时摘要。key 规则 `[a-z][a-z0-9-]{1,62}` 前端预检、服务端终裁（400 行内回显）。**保留字 `api` / `v2` / `docs` / `console` / `ui` / `assets` 建仓即 400**（与控制台/文档/资产路由冲突，六字全拒为实测行为）。
- **详情页**：客户端接入命令块（docker login / settings.xml / .npmrc / pip.conf / curl，带复制按钮——与 docs/user 各接入指南同源）+ 统计 + 危险区。
- **治理字段（仅 local 仓）**：`quotaBytes`（正整数，0=不限）与 `includesPattern` / `excludesPattern`（chips 编辑）。remote/virtual 仓这两组字段不适用（详见[治理指南](admin/governance.md)）。
- **删除**：在详情页危险区——非空仓显示「将删除 N 个制品」+ 勾选 `deleteContent` + **输入 repo key 确认**。不勾选直接删非空仓会被服务端 400 拒绝，UI 会先呈现原因而不是让用户撞墙。

### 3. 制品树与上传/下载（`/repositories/:key/tree/...`）

- 左树懒加载一层、右表当前层 children（目录/文件图标、size、修改时间、操作者、面包屑）；大目录「加载更多」客户端分页，超过 2000 条提示改用搜索。
- M4 的树对五种 packageType **统一按路径树呈现**：docker 仓可见 manifest/tag 节点；maven 仓按 GAV 目录钻取。按协议深度特化的视图（docker 两级 catalog→tag 表 + manifest 详情面板、npm 包目录、pypi 归一名项目视图、maven `maven-metadata.xml` 只读面板）为已登记的后续项；docker/npm/pypi 仓的树页同时给**接入命令块**（发布动作走命令行）。
- **上传对话框仅 generic 与 maven 仓出现**（docker/npm/pypi 的发布是协议会话，UI 以接入命令块替代；maven 为 GAV 表单——五输入实时预览生成的 layout 路径并做前端预检）。浏览器端流式计算 sha256 与服务端返回值比对，成功显示一致徽标；409 双值 / 403 权限指引 / 413 quota 文案**原样呈现**。
- 下载：详情面板给 checksums（mono + 拷贝），可与服务端 item info 对账。
- 权限过滤自然生效：无 read 权限的子树不可见；操作中 403 行内呈现并指向权限模型。

### 4. 搜索（`/search`）

名称子串检索（`GET /api/search/artifact?name=`，`⌘K` / `/` 全局快捷键），结果按调用者权限过滤——**无 read 权限的仓库不会出现在结果里**（不提示「被过滤」，空结果就是空结果）。支持按仓库收窄；maven 结果带 GAV 语义副行。checksum 精确反查 M4 未接 UI（页面引导走 REST，命令见[治理指南](admin/governance.md#搜索)）。行点击跳转制品树定位。

### 5. 安全（`/security/*`，admin）

用户 / 组 / 权限 target 编辑器。三步授权流（建组 → 建用户入组 → 建 target 双栏矩阵）、**模式测试器**（include/exclude 逐条命中 + exclude 优先最终判定，判定向量与服务端 ACL 同源）、保存前变更 diff 确认、移出组**即时生效**（下一次请求即 403）。完整操作与 curl 对账见[用户组与权限管理](admin/groups-permissions.md)。

### 6. 治理（`/governance/*`，admin）

- **审计日志**（`/audit`）：actor/action/repo/时间窗过滤 + 「加载更多」游标分页；动作值原样 mono 显示（不翻译，方便贴给同事比对日志）。
- **存储 & GC**：stats 概况 + dry-run 结果面板 + apply **输入实例名二次确认**（危险区模式）。
- **配额**：每仓 used/quota 水位条（80% 黄 / 100% 红）+ 行内编辑上限。
- **备份/恢复页不做**（M4 有意裁剪）：页面给 CLI 引导块——export/import 是高危操作，走带外通道，见[备份与恢复手册](admin/backup-restore.md)。

## 角色可见性

| 角色 | 可见 |
|---|---|
| admin | 全部入口与数据面 |
| 非 admin 已登录 | 仪表盘（实例卡 + 收敛说明）、**制品树与搜索（按自身路径 ACL）**、设置（实例信息 + 改密）。「安全」「治理」分组整体隐藏；仓库列表页为管理面（admin-only）——非 admin 看到的是无权限卡 + 搜索/直链引导，但**制品本身经树路由与搜索仍按授权可达** |
| 未登录 | 无（重定向 `/login?return=`） |

401 与 403 在界面上分流：401 一律「登录已过期」toast + 重定向登录页；403 按层级收敛（导航/按钮不渲染、页面级无权限卡、卡片级隐藏）——403 只表达权限，不用于表达「功能不存在」。

## 浏览器兼容

| 引擎 | 状态 | 依据 |
|---|---|---|
| Chromium（Chrome/Edge） | **主支持**：QA 全量矩阵 47/47（登录/建仓/树/上传/搜索/安全/审计/GC 全链） | T-104（Chromium for Testing 151） |
| WebKit（Safari 内核） | 登录/制品树/上传链可用 | T-104 发现的懒加载 CSS 资源路径缺陷（D-104-1）已由 T-120 修复，修复后跨引擎复核通过 |
| Firefox | 同 WebKit | 同上 |

已知边界：三引擎均为 QA 抽查口径（登录/树/上传三链），非全量矩阵；Chromium 是唯一全量回归引擎，生产环境推荐 Chromium 系浏览器。

## 有意不兼容与已知边界（M4 控制台）

| 项 | 说明 |
|---|---|
| Access Tokens 管理页 | M4 不做列表/吊销 UI（签发仍走 REST admin-only；页面为引导占位）——P2 债务 |
| 备份/恢复 UI | 不做（CLI only + 页面引导块），高危操作走带外 |
| docker/npm/pypi 仓 UI 上传 | 协议是发布会话，浏览器不做——以接入命令块替代。已知界面瑕疵：非 admin 打开这三类仓会看到 generic 形态的上传入口，写入会被协议面拒绝并**原样呈现错误**（无数据落盘） |
| 协议特化树视图 | M4 统一路径树（见上文）；docker 两级 catalog→tag 表、npm/pypi 包目录、maven metadata 只读面板为后续登记项 |
| virtual 仓来源成员列 | 未做（成员来源经响应头仅在内容 GET 可得，item info 无此数据） |
| 审计 CSV 导出 / 搜索 checksum 反查 UI | P2 债务：按钮不渲染 / 页面引导走 REST |
| token 签发/吊销不落审计 | M4 已知缺口（`token.issue`/`token.revoke` 在词表但签发无留痕）——登记中 |
| 非 admin 的仓库列表 | 管理面 admin-only 是定案（存在性不泄露），非缺陷；制品可达性走内容面 |

## 下一步

- 授权三步流与组语义：[用户组与权限管理](admin/groups-permissions.md)
- 审计 / GC / 配额：[治理指南](admin/governance.md)
- 备份恢复：[备份与恢复手册](admin/backup-restore.md)
- CI 与脚本不走控制台，走 [API Token](faq.md#高频场景高-qps-请用-access-token) 与各协议接入指南
