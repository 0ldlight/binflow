# 导入/导出 REST API 行为规格（M6 — 迁移工具基座）

> 逆向基线：artifactory-pro 7.161.16（`reverse-src/artifactory/`）。反编译代码中 import/export 位于 UI REST 层（`o.a.a.ui.rest.resource.admin.importexport`），而非 `api/` 前缀下。
> 置信度标注：`高` = 代码 + JFrog 官方文档双证；`中` = 仅代码可见；`低` = 推断待动态验证。

## 1. 端点总表

所有端点 **仅 admin 角色** 可访问（`@RolesAllowed({"admin"})`），返回 JSON。

### 1.1 导出端点

| 方法 | 路径 | 语义 | 请求体 | 成功响应 | 错误响应 | 置信度 |
|---|---|---|---|---|---|---|
| POST | `/artifactexport/repository` | 导出单个仓库或全部仓库 | `ImportExportSettings` JSON | 200 + 成功消息 | 400/500 + 错误消息 | 高 |
| POST | `/artifactexport/system` | 导出整个系统（配置 + 仓库） | `ImportExportSettings` JSON | 200 + 成功消息 + 导出文件路径 | 400/500 + 错误消息 | 高 |

### 1.2 导入端点

| 方法 | 路径 | 语义 | 请求体 | 成功响应 | 错误响应 | 置信度 |
|---|---|---|---|---|---|---|
| POST | `/artifactimport/repository` | 导入单个仓库或全部仓库 | `ImportExportSettings` JSON | 200 + 成功消息 | 400/500 + 错误消息 | 高 |
| POST | `/artifactimport/upload` | 上传并导入已解压的 zip（仓库维） | `multipart/form-data`（`FileUpload`） | 200 + 成功消息 | 400/500 + 错误消息 | 中 |
| POST | `/artifactimport/system` | 导入整个系统 | `ImportExportSettings` JSON | 200 + 成功消息 | 403（SaaS 用户）、400/500 | 高 |
| POST | `/artifactimport/systemUpload` | 上传并导入已解压的系统 zip | `multipart/form-data`（`FileUpload`） | 200 + 成功消息 | 403（SaaS 用户）、400/500 | 中 |

---

## 2. 请求模型：`ImportExportSettings`

| 字段 | 类型 | 默认值 | 说明 | 置信度 |
|---|---|---|---|---|
| `path` | string | — | 导出/导入目标或来源目录的绝对路径。**导出时为必填** | 高 |
| `excludeMetadata` | boolean | false | 导出时是否排除元数据（属性）；导入时忽略 | 高 |
| `verbose` | boolean | false | 是否输出详细日志 | 高 |
| `excludeContent` | boolean | false | 是否仅导出/导入配置而不包含制品二进制内容 | 高 |
| `repository` | string | — | 仓库键名；特殊值 `"All Repositories"` 表示全部仓库。仓库导出时使用 | 高 |
| `excludeBuilds` | boolean | false | 是否排除构建（build）信息 | 高 |
| `m2` (createM2CompatibleExport) | boolean | false | 是否以 Maven2 兼容格式导出（目录结构 artifactId/version/ 风格） | 高 |
| `createArchive` | boolean | false | 是否将导出内容打包为 zip 归档（非目录形式） | 高 |
| `zip` | boolean | false | 标记导入来源为 zip 文件（UI 侧标记，传递到服务层） | 中 |
| `actOnMissionControl` | boolean | false | 是否对 Mission Control 组件执行操作（JFrog 平台特有） | 中 |

**导出/导入流程中的字段转换**（系统导出 `ExportSystemService`）：

- `excludeMetadata` = true → `settings.setIncludeMetadata(false)`
- `excludeContent` = true → `settings.setExcludeContent(true)`
- `verbose` → `settings.setVerbose(..)`
- `m2` → `settings.setM2Compatible(..)`
- `excludeBuilds` → `settings.setExcludeBuilds(..)`
- `createArchive` → `settings.setCreateArchive(..)`
- `failFast` 在系统导出时硬编码为 false
- `failIfEmpty` 在系统导出时硬编码为 true
- 仓库列表在导出时自动收集：排除 metadata 或 content 时仍导出全部本地仓库；否则使用 `repositoryService.getAllLocalRepoTypeKeys()` 过滤可导入项

---

## 3. 导出语义流程

### 3.1 路径校验（系统导出与仓库导出共用）

1. 调用 `PathValidatorUtil.isExportImportValidPath(path)` — 验证路径是否合法（非 null、非空、非相对的通用规则）（置信度：中）
2. 调用 `PathValidatorUtil.isExportPathInAllowList(path)` — 验证路径是否在已配置的允许导出白名单中（置信度：中）
3. 失败时分别返回 `"Invalid Export Directory"` 和 `"Export path is not within the configured allowed paths"`

### 3.2 Cold Instance 阻止

如果 `excludeContent` = false 且实例处于 cold instance 状态（归档制品尚未取回），导出被阻止，返回 `"Exporting binaries in not supported in cold instance"`。（置信度：中）

### 3.3 仓库导出

- `repository` 字段值为 `"All Repositories"` 时调用 `BackupService.backupRepos()` 全量导出
- 否则调用 `RepositoryService.exportRepo(sourceRepoKey, exportSettings)` 导出单一仓库
- Maven2兼容格式（`m2=true`）改变导出目录结构

### 3.4 系统导出

- 创建 `ExportSettingsImpl(exportToPath, status)`
- 调用 `context.exportTo(settings)` 统一导出
- 成功后返回包含输出文件路径的成功消息

---

## 4. 导入语义流程

### 4.1 JSON 导入（`/artifactimport/repository` 与 `/system`）

- 路径校验与导出一致（使用 `PathValidatorUtil.isExportImportValidPath`）
- 系统导入在 SaaS 模式（AOL）下被阻止 → 返回 HTTP 403，消息 `"The System Import and Export features are not available for Artifactory SaaS users"`
- 导入异步执行：`ImportServiceImpl.importAsync()` 先调用 `importSync()`（代理到 `ContextHelper.get().importFrom(settings)`），然后写入 marker 文件

### 4.2 Zip 上传导入（`/artifactimport/upload` 与 `/systemUpload`）

- `Content-Type: multipart/form-data`
- 上传文件被解压到临时目录（UUID 命名，避免冲突）
- 导入完成后临时目录被清理
- 仓库 zip 导入与系统 zip 导入使用不同的服务处理

### 4.3 Marker 文件（ImportReport）

导入完成后，在导入目录写入一个 marker JSON 文件（文件名由 `importSettings.getMarkerFile()` 指定），格式：

```json
{
  "status": "success" | "failure",
  "error": "<错误描述，仅 failure 时存在>"
}
```

（置信度：高）

---

## 5. 错误响应格式

与 Artifactory 通用错误格式一致：

```json
{
  "errors": [{
    "status": <int>,
    "message": "<错误描述>"
  }]
}
```

所有导出/导入错误走服务层 error path，不抛出 HTTP 异常，而是通过 `response.error(...)` 返回。

---

## 6. 与官方规范的差异/补充

| 项目 | 说明 | 置信度 |
|---|---|---|
| UI 层路径 vs API 路径 | 这些端点在 Artifactory 中属于 UI REST 层（`/ui/` 路径或内部映射），而非公开 API 文档中的 `/api/export` 和 `/api/import` 系列。JFrog REST API 文档中另有 `/api/export/system`、`/api/export/repo/{repoKey}`、`/api/import/system`、`/api/import/repo/{repoKey}` 等端点。本规格覆盖的是 UI 层路径。 | 中 |
| SaaS 阻止 | 系统级别导入（system / systemUpload）在 SaaS 模式下返回 403，仓库级别导入/导出不受限 | 高 |
| Cold instance 阻止 | 导出时如果实例处于 cold storage 状态且 `excludeContent=false`，系统导出和仓库导出均被阻止 | 中 |
| 异步导入 + marker 文件 | 导入为异步执行，完成后写入 marker JSON 文件以表示完成状态 | 中 |

---

## 待验证清单（低置信度）

1. `path` 字段在 JSON 导入/导出时的完整路径校验规则（允许的目录白名单配置方式未在反编译代码中直接可见）
2. `zip` 字段在服务层的准确作用（标记导入源已解压，但完整语义待验证）
3. `/api/export/` 和 `/api/import/` 公共 API 路径与服务层行为的具体差异
4. 导出文件实际目录结构（Maven2 兼容模式 vs 标准模式的具体布局差异）
5. 同仓库并发导入的锁/保护行为