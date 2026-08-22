# RBAC / 角色与授权分层行为规格（M7 种子 A 校准来源）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`，内嵌 access-* 客户端 7.189.4）。
> 置信度标注：`高` = 反编译代码 + JFrog 官方文档双证；`中` = 仅反编译代码；`低` = 推断待动态验证。
> 官方参考：JFrog Projects API 文档（docs.jfrog.com/projects/reference/getProjectRoles 等）、Deprecated JFrog APIs 页（Create or Replace Group / Create or Replace User 条目）、SAML SSO 文档。
> 姊妹篇：`auth-model.md`（用户 CRUD / token / permission target 概览）。本文只展开「admin 布尔之上/之外还有什么角色层」。

## 0. 结论速览（对「read-only admin / 仓库级 admin / 角色管理」三个问题的直接回答）

| # | 问题 | Artifactory 7.161 的真实答案 | 置信度 |
|---|---|---|---|
| 1 | 实例级（非项目）有没有角色闭集？ | **没有**。`/api/security/*` 的实体类型闭集 = `users` / `groups` / `permissions` 三种，任何其它 entityType 一律 400。授权的粒度完全由「admin 布尔（用户级或组级）+ permission target 的 ACE 动作」两个层次表达。 | 高（代码路由分发闭集 + 官方 Deprecated APIs 路径清单双证） |
| 2 | 有没有 "read-only admin"？ | **实例级没有**。最接近的形态是项目域的 `Viewer` 预定义角色（只读项目资源）；实例级只有 admin 布尔。组/用户上有 4 个 Xray 域的 scoped manager 布尔（§1.3），但它们不是通用只读 admin。 | 高（代码无该概念 + 官方文档无该角色名） |
| 3 | 有没有 "仓库级 admin"（repo admin）？ | **没有单独的仓库级 admin 角色**。等价能力由两层拼出：permission target 的 `manage` 动作（对该 target 覆盖的 repo/path 生效）+ Projects 域把 repo 划入 project 后由项目角色治理（`CREATE_LOCAL_REPO` 等 RoleAction）。 | 高 |
| 4 | "managed admin" 存在吗？ | 实例级代码中**无此概念**（无 scoped-admin/managed-admin 类型）。最接近的是 Projects 的 `RoleType.ADMIN` 与预定义 `PROJECT_ADMIN`（project 域 scoped admin）。用户/组是否「被外部 realm 托管、不可本地编辑」由 `realm` 字段表达，细化判定在 Access 服务内（见 §5 缺位说明）。 | 中（仅代码侧证据） |
| 5 | 角色管理 REST API 长什么样？ | 实例级 `/api/security/*` **没有 roles 端点**；角色 CRUD 在 **Access 服务的 Projects API**：`/access/api/v1/projects/{projectKey}/roles`（官方文档）。Artifactory 侧只保留 gRPC 客户端模型（`o.j.access.proto.generated.RoleResourceGrpc`），7.161 的 artifactory.war 内**未发现**该客户端的调用方（角色治理完全发生在 Access/前端直连）。 | 高（REST 形态=官方文档；proto=代码） |
| 6 | groups 的 admin 布尔存在吗？ | 存在：组模型字段 `adminPrivileges`（PUT/GET `/api/security/groups/{name}`），组成员即成为 effective admin。**禁止与 `autoJoin` 同时为 true**（400）。 | 高（代码 + 官方 Create Group 条目双证） |

---

## 1. 实例级授权模型（7.161 现状）

### 1.1 「是 admin」的判定（行为语义）

| 条目 | 行为 | 置信度 |
|---|---|---|
| effective admin | 用户为 admin 当且仅当：用户直接 `admin=true`，**或**所属任一组的 `adminPrivileges=true`。两者在 GET 用户响应里都表现为 `admin: true`（`o.a.a.security.UserConfigurationImpl#isEffectiveAdmin`）。 | 高 |
| admin 短路 | admin 不经 permission target 判定即拥有全部制品权限（auth-model.md §4 已载）。 | 高 |
| 非语义的组字段 | 组上除 `adminPrivileges` 外**没有任何权限语义字段**；组不携带 read/write 等 ACE——组权限只能通过把组名写进 permission target 的 principals 授予。 | 高 |
| MC scope | 持有 scope 含 `internal:mc:x` 的 access token 视为等同 admin 的**配置修改**豁免（MissionControl 集成），与 admin 布尔并联出现在中央配置校验里（`o.a.a.security.SecurityServiceImpl#hasMcScope`）。 | 中 |

### 1.2 组 CRUD 端点表（`/api/security/groups/*`，admin only）

| 方法 | 路径 | 成功 | 主要错误 | 置信度 |
|---|---|---|---|---|
| GET | `/api/security/groups` | 200 JSON 数组，元素 `{"name","uri"}`（无 admin 字段，列表仅名与链接） | 非admin 403 | 高 |
| GET | `/api/security/groups/{name}` | 200 JSON：`name/description/autoJoin/realm/realmAttributes/external(=realm≠internal 推导)/adminPrivileges/watchManager/policyViewer/policyManager/reportsManager/externalId`，带 `?includeUsers=true` 时另附 `userNames[]` | 404（无 body） | 高 |
| PUT | `/api/security/groups/{name}` | 201 无 body（已存在则更新） | 见 §1.2.1 校验链 | 高 |
| DELETE | `/api/security/groups/{name}` | 200 text `Group '<name>' has been removed successfully.` | 404；最后一个 admin 组保护见 §1.2.2 | 高 |

注意：7.161 实际把写操作转发给 Access 服务（`o.a.a.storage.db.security.service.access.AccessUserGroupStoreService`），Access 侧 HTTP 错误透传为「Failed to process request for group <g>. <msg>」+ 对应状态码。

#### 1.2.1 PUT 组校验链（按触发顺序）

1. body `name` 非空且 ≠ 路径名 → 409 `The group name that was provided in the request path does not match the group name in the provided group configuration object.`
2. `adminPrivileges=true` 且 `autoJoin=true` → 400 `For security reasons, automatically joining new users to a group that is granted with Admin privileges is not supported.`
3. 组名格式/XSS 校验失败 → 400（校验器消息）
4. body 带 `userNames` 时逐一必须已是小写形态（`verifyAllUserNamesAreLowerCased`），否则报错
5. Access 转发失败 → 透传状态码 + `Failed to process request for group <g>. ...`
6. 成功 → 201（新建）或更新；`userNames` 提供时全量替换组成员

以上置信度：高（代码完整；1/2/3 的文案仅代码可见，文案标中）。

#### 1.2.2 「最后一个 admin」保护（DELETE）

删除 admin 组时：若系统**再无任何 admin 用户**、且这是**最后一个 admin 组**，DELETE → 400 `Cannot delete group '<g>'. There must be at least one user configured with admin privileges.`——即 Artifactory 拒绝把自己锁在门外。非 admin 组删除无此检查。置信度：高（代码；官方文档未明说该文案，行为本身标中-高）。

### 1.3 scoped manager 布尔（Xray 域，不是通用角色）

用户与组模型上存在 4 个布尔：`watchManager` / `policyViewer` / `policyManager` / `reportsManager`。行为语义：

- 随用户/组 CRUD 一起读写（GET 单用户与 GET 组响应均含这 4 个字段）。
- 在 Artifactory 反编译码内**未见任何授权判定消费它们**——它们由 Xray/前端消费（Xray addon 不在本 war 内）。对 BinFlow 而言：这 4 个字段是**透传型元数据**，不构成权限层。
- 登录态响应（`UiLoggedInUserResponse`）还会给出 `resourcesManager`（= default project admin，见 §2.3）与 `platformAuditor` 等 UI 提示字段。

置信度：中（字段集与序列化=代码；「无消费方」为枚举式排除，标中）。

---

## 2. Projects 域：角色层真正的位置

### 2.1 角色 gRPC 服务（`reverse-src/artifactory/src/batch1-core/role.proto`，包 `com.jfrog.access.v1.role`）

服务 `RoleResource` 提供四个操作：`CreateRole` / `EditRole` / `DeleteRole` / `GetRoles`；**删除时要求角色未被使用**（proto 注释 "assert not in use"）。角色模型字段：

| 字段 | 语义 | 置信度 |
|---|---|---|
| `name` | 角色名（project 内唯一） | 高 |
| `description` | 描述 | 高 |
| `actions` | RoleAction 枚举集合（见 §2.2） | 高 |
| `type` | `ADMIN` / `PREDEFINED` / `CUSTOM` / `CUSTOM_GLOBAL` 四值闭集 | 高 |
| `environments` | 角色 allowed 的部署环境名集合（DEV/PROD 等自定义环境） | 高 |

### 2.2 预定义角色与动作闭集

预定义角色（`PredefinedRole` 枚举，全平台统一）：`PROJECT_ADMIN`、`RELEASE_MANAGER`、`VIEWER`、`CONTRIBUTOR`、`DEVELOPER`、`SECURITY_MANAGER`、`APPLICATION_ADMIN`、`APPTRUST_MANAGER`。置信度：高（proto + 官方 Projects 文档角色列表双证）。

RoleAction 是**项目资源动作**闭集（非 artifact 动作），按资源域分组（摘录代表性值，全表见 proto）：repository 域 `READ/ANNOTATE/DEPLOY_CACHE/DELETE_OVERWRITE/MANAGE_XRAY_MD_REPOSITORY`；release bundle 域、build 域、pipeline 域、security 域（含 `MANAGE_MEMBERS`/`MANAGE_RESOURCES`）、ML 域、以及**仓库生命周期动作** `CREATE_REMOTE_REPO/DELETE_REMOTE_REPO/CREATE_LOCAL_REPO/DELETE_LOCAL_REPO/CREATE_VIRTUAL_REPO/DELETE_VIRTUAL_REPO`。置信度：高。

要点：**RoleAction 与 permission target 的 ACE 动作（read/write/annotate/delete/manage/distribute/managedXrayMeta）是两套互不重叠的动作词汇**——前者治理项目资源与成员管理，后者治理 repo 内路径。BinFlow 若做仓库级 admin，Artifactory 的对应物是「项目角色 + repo 划入项目」，不是给 permission target 加新动作。

### 2.3 project admin 的判定与 Artifactory 侧消费

| 条目 | 行为 | 置信度 |
|---|---|---|
| project admin 判定 | 用户是 project admin ⟺ 用户名出现在该 project 的 `adminMembers.users`，或其所属组与 `adminMembers.groups` 相交（`o.j.access.client.project.GrpcProjectClientImpl#isAdminMember`）。**与角色名无关**—— membership 直判。 | 高 |
| resource-manager | `isResourceManager(u)` ≡ `isProjectAdmin(u, "default")`（`o.a.a.projects.ProjectsServiceImpl#isResourceManager`）——resource manager 就是 **default 项目的 project admin**。default 项目是「未启用项目化的所有 repo/用户」的隐式归属（`noProjectsConfigured()` 全部归 default）。 | 中（代码完整；官方文档未见此等价表述） |
| 允许 resource manager 建用户 | `security.allow.only.admin.create.entity` **默认 true**；为 false 时，非 admin 但 resource-manager 的调用者可以 PUT/POST `/api/security/users/*`（`RestSecurityRequestHandler#createOrReplaceSecurityEntity` 前置门）。groups/permissions 不享受该放行。 | 高（代码 + 常量默认值；官方文档以系统属性形式记载，标中-高） |
| 中央配置修改闭集 | 非 admin 且非 MC scope 的 project admin 修改 artifactory.config.xml 时，仅允许 4 个配置节：`repoLayouts` / `backups` / `localReplications` / `remoteReplications`；越界 → `Project admin is not allowed to make those Artifactory configuration changes.`（`o.a.a.projects.ProjectAdminValidation`）。 | 中（仅代码） |
| project-scope token 对 artifact fail-closed | artifact 路径授权判定遇到 project-scope token 直接拒绝（`AuthorizationServiceImpl#isGranted` 的 `isProjectScopeToken` 短路返回 false）——项目 token 不能读写制品路径之外的东西时也整体 fail-closed。 | 中（仅代码） |
| artifact 授权委托 | 7.161 的 artifact 级 `isGranted` 把（用户+组）principal 与（资源类型+路径+动作）scope 交给 Access 客户端的缓存鉴权 `principalCanDo` 判定；repo 类型（local/remote/build/release-bundle）作为 scope 属性参与判定。权限事实存储在 Access 侧。 | 中 |

### 2.4 角色 REST API（Access 服务，官方文档）

- 列表：`GET /access/api/v1/projects/{projectKey}/roles`（返回预定义 + 自定义角色）；创建/更新/删除同前缀。安全要求：Platform Admin 或 Project Admin，或带 `project:<key>/roles:r` scope 的 token。
- **reverse-src 缺位**：access.war 本体未反编译（README 已注明），角色 REST 的请求/响应字段、错误码只能以官方文档为准；本仓库代码仅证实 gRPC 模型与「角色属 project 域」。置信度：REST 形态=高（官方文档）；与 gRPC 模型字段一致性=中（推断）。

---

## 3. UI 与 API 的差异

| 维度 | 行为 | 置信度 |
|---|---|---|
| 双 REST 面 | UI 控制台走 `/ui/api/v1/*`（`o.a.a.ui.rest.*`），实体模型与 `/api/security/*` 同构但服务独立（group 仅有 `GetAllGroupNamesService`；permissions 有独立的 Create/Update/Delete/Get 系列 + 「effective permissions」查询服务）。 | 高 |
| UI admin 端点加强 | 部分 UI admin 端点（如 `GET /api/v2/security/permissions/users|groups/{name}`、`GET /ui/api/v1/validateUserPassword`）在 `@RolesAllowed` 之外追加 `assertValidAuthInCaseMfaEnabled`：用户 MFA 已验证时**只接受 Bearer token**，Basic/其它 → 403 `When multi-factor authentication is enabled only bearer token is accepted`。 | 中 |
| UI 项目管理 | UI 的 projects REST 面（`ui/rest/resource/admin/configuration/projects/ProjectsResource`）仅 `GET projects/unassigned/statistics`（admin only）；项目/角色管理页面实际直连 Access 服务的 `/access/api/v1/*`。 | 中 |

---

## 4. 与公开规范的差异/补充（此节补充官方文档）

1. 「 entityType 闭集 400 」与「最后一个 admin 组不可删」：官方 Deprecated APIs 页未记载，代码可见（本文补充）。
2. resource-manager ≡ default 项目 project admin：官方文档只提 "resource managers" 概念（用户管理场景），等价关系是代码可见的行为（本文补充）。
3. project admin 的中央配置修改闭集（4 个节）：官方文档未见记载（本文补充）。
4. 组模型 `adminPrivileges`+`autoJoin` 互斥：官方 Create Group 条目列字段但未写互斥校验（本文补充）。
5. 新旧组 API 并存：7.49.3 起官方主推 `PUT /access/api/v2/groups/{groupKey}`（字段 snake_case：`admin_privileges`、`membership_for_new_users`）；7.161 的 Artifactory 旧路径仍可用并转发 Access（官方 Deprecated APIs + 代码双证）。

## 5. BinFlow M7（种子 A）校准建议

依据本规格，Artifactory 的「细粒度 RBAC」真实形态是**两层正交**，不是单一角色表：

1. **实例级**保持简单闭集：用户/组 admin 布尔（含组 admin + effective admin 语义 + 最后一个 admin 保护）+ permission target。BinFlow 现有模型（permission targets + admin 布尔）已对齐此层，M7 不需要新增实例级角色。
2. **read-only admin / 仓库级 admin**若要引入，Artifactory 的可对齐路径是 **project 域角色**（预定义角色含 `VIEWER`；RoleAction 含仓库生命周期动作与按资源域动作），而非在 permission target 上加角色。建议 M7 评估「项目/资源组 + 角色（动作闭集）+ 成员（用户/组）」三件套；最小闭环可先做：组级 admin 已有 → 补「只读管理」预定义角色 + 仓库级 manage 语义。
3. 保持两套动作词汇分离（artifact 动作 vs 管理动作），不合并。
4. `security.allow.only.admin.create.entity` 类似的「resource manager 可建用户」放行开关，若 M7 引入角色管理可一并考虑（默认 true=仅 admin）。

## 待验证清单（低置信度，动态验证后回填）

| # | 条目 | 现状 |
|---|---|---|
| 1 | `/access/api/v1/projects/{key}/roles` 响应字段与 gRPC RoleModel 的一致性 | access.war 未反编译，仅有官方文档 |
| 2 | RoleAction 全表在 7.161.16 与官方文档的逐项一致性 | proto 73 项 vs 文档分组描述 |
| 3 | project admin 修改 4 个配置节之外的行为（UI 是否前置隐藏） | 仅服务端校验可见 |
| 4 | `platformAuditor` / `manageWebhook` 等 UI 登录态提示字段的授权语义 | 字段可见，消费方在 Access/前端 |
| 5 | Xray scoped manager 布尔的实际 gate 行为 | Artifactory war 内无消费方，需 Xray 侧验证 |
