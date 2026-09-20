---
title: 用户可见变化公告
sidebar_position: 5
---

# 用户可见变化公告

> 适用版本：本页所述变化的交付版本（仓库配置动词语义收紧 + 协议语义对齐批次；控制台交互对齐、计划任务、界面语言三条主线）。
> 本页面向**已在使用 BinFlow 的读者**：把最近的交付批次中**你会直接看到或需要动手适配**的变化逐一列明——包括若干**推翻此前既定裁定的翻案项**（每项注明「此前 → 现在」）。操作细节一律链到对应指南；本页零内部编号，按变化本身组织。

## 破坏性变更：仓库配置动词语义收紧（PUT 只建仓，更新只有 POST）

**此前**：`PUT /binflow/api/repositories/{key}` 对已存在的 key 走「替换更新」（200 生效）。**现在**：**PUT 收紧为只建仓（create-only）**——对已存在的 key 一律 **400**，且**零副作用**（拒绝后存量配置原样不动）：

```json
{"errors": [{"status": 400, "message": "error when validating repository name: libs-release : Repository key already exists"}]}
```

- **唯一的更新拼写是 `POST`**，且为**合并语义（三列）**：body 里**省略**的字段保留存量；**`null` / 空串**清空该字段（数组空值保留、对象 `{}` 整族复位）；**显式值**覆盖（`0` 也是显式值，不是缺省）。未知 key 404。
- **动因**：对齐 参考仓库——参照系统的更新拼写只有 POST；BinFlow 先前「PUT 也能更新」是自有偏差，本批收敛。建仓仍 admin only；更新臂对覆盖仓的 manage 持有者开放（与此前一致）。

**影响面与迁移**（repeat-PUT 更新脚本必读）：

- 「先 PUT 建仓、此后每轮 PUT 刷配置」的脚本 / CI / IaC 模板，从第二个 PUT 起拿到 400。
- 迁移 = **更新调用 PUT 改 POST**，一行替换；body 不必补全字段（省略即保留，不会丢配置）：

```bash
# 此前（现在拿到 400）：
curl -u admin:*** -X PUT "$BASE/binflow/api/repositories/libs-release" \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"新描述"}'

# 迁移后（更新走 POST，省略字段保留存量）：
curl -u admin:*** -X POST "$BASE/binflow/api/repositories/libs-release" \
  -H 'Content-Type: application/json' \
  -d '{"description":"新描述"}'
```

- upsert 形脚本改为两步：先 `GET` 探测（404 → `PUT` 建仓；200 → `POST` 更新）。
- 动词语义表与逐字段合并细则见 [API Reference · 仓库管理端点](api-reference.md)。

## 协议语义对齐批次：客户端可见面

六个协议域（npm / PyPI / Conan / Docker / Go / Helm）的客户端可见语义逐项对齐 参考仓库 口径，Maven 与存储管理面同步收敛；下列行为经真实客户端（pip / mvn / npm / conan / helm CLI / curl）在隔离实例或双端对照下验证：

- **Maven**：SNAPSHOT 部署缺省 `unique`——落盘名改写为时间戳版本（`demo-app-1.2.0-20260819.212603-2.jar`，buildNumber 跨趟递增；消费侧 `-U` 强刷解析最新）；版本目录的 `maven-metadata.xml` 有 **pom 前置**（目录存在 `.pom` 直接子文件才生成）；`deploy:deploy-file` 免 `generatePom` 的路径 404。见 [Maven 接入](integrations/maven.md)。
- **PyPI**：simple 索引的 `Requires-Python` 按三源管线取值（twine 上传表单 / wheel `*.dist-info/METADATA` / sdist `PKG-INFO`），服务端从制品字节解析并转义渲染——pip 的版本过滤直接吃到这个值；坏元数据**存而不索引**（上传成功、不进 simple 页）。见 [PyPI 接入](integrations/pypi.md)。
- **npm**：dist-tags 族语义——`latest` **删不掉**（删除请求本身 200 空体，此后两读面读时重算为最高已发布版本）；集合面 PUT/POST 与单 tag POST 一律 405（npm CLI 只走单 tag 显式端点，不受影响）；dist-tags GET 带 60 秒缓存头（刚改完 tag 立查可能读到旧值，`--prefer-online` 或等 60 秒自愈）；重发同版本按 **tarball 路径占用**判定 403（版本不可变——packument 文档携带历史版本清单不触发，ghost 版本不入索引不劫持 `latest`）。见 [npm 接入](integrations/npm.md)。
- **Conan**：v1 / v2 双协议面收口——幽灵 revision 的删除分叉、错误信封族、`q` 查询参数门。见 [Conan 接入](integrations/conan.md)。
- **Docker / Go / Helm**：docker token 流与 `/v2` 根级面、Go Modules 的 sumdb 校验链、Helm chart 的 `index.yaml` 计算面与 digest 口径（`digest` = chart `.tgz` 的裸 sha256）对齐。
- **存储管理面**：`?list` 清单族**七参数全量**（`deep` / `depth` / `listFolders` / `includeRootPath` + 元数据三参 `mdTimestamps` / `statsTimestamps` / `includePropertiesMd5`；参数出现但非整数值一律 400 `For input string: "<v>"`）；递归属性写**只对真实变更移动属性 mtime**（原值重写不再刷新时间戳）；**下载计数只计内容 GET**（`?stats` 等元数据读不计数）。见 [API Reference](api-reference.md) 与 [属性指南](properties.md)。

## 界面语言：中英双语可切换

**此前**：控制台为中文单语。**现在**：内置中英双语资源包，一步切换。

- **切换器**在侧栏底部脚注（「语言」caption + `中文` / `English` 两档单选，应用与管理两侧栏同脚注常驻）——当前语言呈选中态，点选即生效。
- 切换 = **整页重载**后按新语言渲染（与 参考仓库 同款姿态），选择持久化在浏览器本地（`localStorage`），下次打开保持；清除浏览器数据后回落中文默认。
- **中文默认不变**；英文界面为全量覆盖（控制台、仓库、制品、搜索、安全、治理、监控、Webhooks 等全部页面域）。
- **术语两包保真**：repo key、node、checksum、Deploy、Set Me Up、cron、readonly_admin 等英文术语在两种语言下原样呈现——中文界面里它们今天长什么样，英文界面里就长什么样。
- **日期与数字随语言**：结果表与审计时间列在英文界面下用 `MMM d, yyyy h:mm:ss AM/PM` 形态（中文界面维持 `dd-MM-yy HH:mm:ss` 24 小时形态，零变化）；数字千位分组随语言取义。

详见[控制台指南 · 界面语言](console.md#界面语言中英双语切换)。

## 计划任务（cron 调度）落地

**此前**：产品曾终裁**不引入**用户级 cron 计划任务（复制为纯事件驱动、备份只能靠外部 crontab 调 CLI、GC 仅手动面）。**现在**：该裁定**正式推翻**——调度域全面落地，与既有事件驱动轨并存。这是登记在案的决策翻案，不是静默变更：

- **维护**：GC 与缓存清理按表达式到点自动执行（三个调度槽）。
- **定时备份**：实例内配置到点 export——不再需要外部 crontab；恢复仍是带外 import CLI。
- **复制**：单条 push 配置可挂调度轨（到点全量对账），与事件轨、Replicate Now 并存且目标侧幂等。
- 表达式为 **Quartz 六域子集**（秒开头的 6/7 域；`L`/`nL` 收录，`W`/`#` 拒收）；全部配置有 REST 面与控制台面。

完整语义、表达式子集与 curl 对账见 **[计划任务（cron 调度）与定时备份](admin/cron-scheduling.md)**。

## 监控面重组：服务状态 / 系统日志 / 系统信息归位

管理侧栏新增**监控（服务节点）分组六页**——存储、**服务状态**（新页）、**系统日志**（新页）、系统信息、维护（GC）、备份 / 恢复：

- **服务状态**：总体状态徽标 + 存储元数据/镜像仓库三子系统行 + 版本信息 + 计划任务调度节（下次运行倒计时）。**Uptime 不呈现**（服务端无该数据，如实缺位不伪造）。
- **系统日志**：7 秒尾随刷新（可暂停 / 立即刷新）+ 窗口内子串过滤 + 当前窗口导出 `.log`。**日志源为审计跟踪**（谁在何时动了什么）——BinFlow 当前没有服务进程日志的在线查看端点，这是如实换形而非等价物；服务进程日志仍走部署层的日志面。
- **系统信息 / 维护（GC）/ 备份恢复**自旧分组**归位迁址**到监控组——旧路径自动折入新址（见下文兼容性）。
- Webhooks 移入「常规」分组；管理侧栏条目 16 → 18。

## 远端浏览可选档：remote 仓的未缓存目录可见

**此前**：remote 仓在制品树里**只显示已缓存制品**。**现在**：三类 remote 仓（**Helm / Debian / RPM**）可开启「列出远端目录条目」：

- 建仓/编辑表单 Advanced 步的 `listRemoteFolderItems` 复选（默认关——不开则行为与此前**逐字节一致**）；详情页回显开/关状态。
- 开启后：树展开可见**未缓存的远端目录与文件**（「远端」标记，大小/时间为 `—` 占位）；点击未缓存条目触发回源拉取并落地缓存（下载计数随动）。
- **上游降级不塌树**：已缓存条目始终可用；远端层故障时目录顶部给出降级注记（REST 面为 `FolderInfo` 的可选 `remoteDegraded` 字段）。
- 其它包型（generic/maven 等）不做远端目录抓取——控件不出现，服务端按名 400（明确的 HTML 抓取取舍，非待完成项）。

详见 [remote / virtual 管理 · 远端浏览可选档](admin/remote-virtual.md#远端浏览可选档listremotefolderitems)。

## 交互形态对齐与既定裁定的翻案

以下变化来自控制台与 参考仓库 交互形态的逐项对齐；其中数项**推翻了此前登记的「有意不做」裁定**，逐一明示：

### 分页：页码控件全面替换「加载更多」（翻案）

**此前**：「加载更多」增量分导是登记在案的豁免项。**现在**：全站列表与结果表统一**页码控件**——页码序列 + 首/上一页/下一页/末页 + 每页行数档 `[20, 50, 100, 200, 1000]`（缺省 100）；边界态禁置不隐藏。搜索（基本与 AQL 两模式）、审计日志、仓库 / 用户 / 组 / 权限 / token 列表全部迁移。分治例外：制品树大目录的「加载更多」**维持**（深浏览场景，与 参考仓库 同为树增量 + 表页码双轨）。AQL 模式下页码重写 `.offset()`、档位重写 `.limit()`——查询文本仍是唯一事实源。

### 用户与组：路由整页表单 + Retype Password 双录（翻案）

**此前**：用户/组创建是列表页内联展开卡，且创建态**没有**二次口令输入。**现在**：创建与编辑均为**路由整页表单**（`/admin/security/users/new`、`/admin/security/groups/new`、`/groups/:name/edit` 可直达深链）；内联卡退役。用户创建页补 **Retype Password 双录**（两次不一致挡提交——按 参考仓库 现版形态补齐）；页脚 `Cancel` | `Reset` | `Save` 双初始禁置（未改过不可复位）。表单里的能力位三开关（Can Update Profile / Disable UI Access / Disable Internal Password）为**恒禁用预留位**——服务端尚未承接，提交体零携带，承接落地后转正。

### 删除动作收敛（范围修正）

浏览器面的行内删除件退役：制品树 children 表不再有详情/下载/删除三钮——**删除统一走详情面板或右键菜单**（均过危险确认，回收站语义不变）。管理面实体列表（仓库/用户/组/权限）的删除维持既有形态：收进详情/编辑页危险区（输入名强确认）。

### 帮助菜单与 About（新面）

顶栏 `?` 帮助下拉四项：**Documentation**（本帮助文档）、**Online Training**（无对应服务——禁用占位如实标注，不伪造外链）、**Release Notes**（升级与版本说明页）、**About**（版本弹窗：版本/修订/产品）。侧栏脚注版本行同时升格为 About 入口。**编辑档案页**新增自助 Identity Token 生成（一次性明文 + 即用 curl 样例）；SSH 公钥管理因服务端无对应端点如实缺位。

### 其他可见变化（简列）

- **树头工具带**：过滤仓库 / 包类型 facet / 仓型复选 / Sort-by / Compacted 紧凑档 / **My Favorites 收藏仓**（仓库右键「收藏」，浏览器本地持久）。
- **建仓三段步进**：Basic → Advanced → Replications（`?section=` 深链）；包类型选择网格 13 型全量呈现（8 进阶型带 `pro` 徽章，档位门由服务端终裁）。
- **搜索统一结果网格**：选择列 + 批量复制路径、列选器、网格快滤；顶栏输入即查询（`?q=` 深链）。
- **详情字段族**：文件详情新增下载统计四行（Downloads / Last Downloaded By / Last Downloaded / Remote Downloads，数据源 `?stats`——「被访问次数」口径，人工浏览也计入）；下载钮收为单图标钮 + 伴随菜单（校验下载、checksums 区）。
- **权限动作域扩为五值**：`read / deploy-cache / annotate / delete / manage`（`write` 仍收为兼容别名；`annotate` 单独控制属性写门）；迁移升级自动回填，存量 write 授权零提权。
- **docker virtual 仓开闸**：`rclass × packageType` 组合门全量退役——建仓面唯一剩余门是 license 档位。
- **AQL 与闲置检索**：AQL 统计域（`stat.*`）+ `GET /api/search/usage`（N 天未下载检索），见 [AQL 搜索指南](aql.md)。

## 兼容性影响

- **本批唯一破坏臂**：仓库配置动词语义（PUT 只建仓）——repeat-PUT 更新脚本需按上文迁移；协议语义批其余条目为行为对齐，标准客户端（pip / mvn / npm / conan / helm CLI）无需改动。
- **旧路径自动折入**：本轮迁移的四个页面路径（系统信息、维护 GC、备份恢复、Webhooks）旧深链打开时**一次性 replace 到新址**，不落 404——书签无需立即更新，但建议尽早改（更早轮次移除的历史重定向窗口口径不变，见[控制台指南 · 旧路径](console.md#旧路径--新路径m9-起不再重定向)）。
- **默认行为不变**：远端浏览默认关、界面默认中文、事件驱动复制轨不变——不配置新能力时，实例行为与此前一致。
- **控制台批次 API 面纯增量**：新端点族（维护/备份/调度投影）与 `cron_exp` 字段均为新增；既有端点行为零变化。

## 下一步

- 脚本迁移：[API Reference · 仓库管理端点](api-reference.md)（PUT/POST 动词语义与合并细则）
- 新能力上手：[计划任务与定时备份](admin/cron-scheduling.md) · [远端浏览可选档](admin/remote-virtual.md#远端浏览可选档listremotefolderitems) · [界面语言](console.md#界面语言中英双语切换)
- 从 参考仓库 迁移的逐任务对照：[操作路径对照表](compatibility-path-map.md)
