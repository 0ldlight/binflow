import { tr } from '../../i18n'

const t = tr('repositories')

// T-439（FR-143.1/.2）新增文案集中常量——i18n 独占波降本纪律（M16-SPLIT
// 附则）：本票引入的全部新用户可见文案集中于此，T-463（FR-149 文案外提
// 100%）搬运时按文件收口；既有文案零挪动。T-443（FR-143.4/.5）同纪律
// 增量见文末（入口分路由 + dirty-gating + remote Test 三臂）。
//
// 命名：键与锚/字段一一对应，值 = 中文呈现（术语保真英文原词——console-ux
// §1.2 两包条款预对齐）。

export const FORM_STEPS = {
  basic: t('基础'),
  advanced: t('高级'),
  replications: 'Replications',
} as const

export const RESERVED_GROUP_BASIC_TITLE =
  t('Artifactory 对齐字段（预留位——当前无效，不提交、不存储）')

export const RESERVED_GROUP_ADVANCED_TITLE =
  t('Artifactory 对齐复选项（预留位——当前无效，不提交、不存储）')

export const RESERVED_PLACEHOLDER = t('（预留位）')

/** repoLayoutRef：Artifactory 7.161 Basic 步在场的 Repository Layout。
 *  BinFlow 布局由各协议 adapter 固定（maven = maven-2 形路径探测），无
 *  repoLayoutRef 消费方（grep internal/ 零读点）——后端承接落地后启用（K70）。 */
export const RESERVED_REPO_LAYOUT_HINT =
  t('预留位：BinFlow 布局由协议固定（maven 即 maven-2 形），repoLayoutRef 暂无引擎承接（K70）——字段不提交。')

/** Environments：7.84 审计名为 Environments，7.161.20 实测已更名 Stage
 *  （Stages & Lifecycle 域）——预留位按新名标注，双名留痕。 */
export const RESERVED_ENVIRONMENTS_HINT =
  t('预留位：BinFlow 无环境段模型（Artifactory 7.161 已由 Environments 更名 Stage）——字段不提交。')

/** notes（Internal Description）：transport 侧 repoConfig.Notes 有解码位但
 *  configJSON 不转发（decode-only，与 blackedOut 同类）——预留位。 */
export const RESERVED_INTERNAL_DESCRIPTION_HINT =
  t('预留位：后端 config 传输层未承接 notes（公开描述即上方「描述」字段）——字段不提交。')

/** blackedOut：Artifactory 7.161 标签「Disable Artifact Resolution in
 *  Repository」；拒写行为联动随 BE 承接票（票内登记）。 */
export const RESERVED_BLACKED_OUT_LABEL = t('blackedOut（Artifactory：Disable Artifact Resolution in Repository）')
export const RESERVED_BLACKED_OUT_HINT =
  t('预留位：PUT 解码后即丢弃（回显/行为均无）——true 拒写语义随后端承接票落地。')

/** archiveBrowsingEnabled：Artifactory 7.161 标签「Allow Artifact Content
 *  Browsing」；BinFlow 归档内浏览自 M1 起默认开放、无开关位。 */
export const RESERVED_ARCHIVE_BROWSING_LABEL = t('archiveBrowsingEnabled（Artifactory：Allow Artifact Content Browsing）')
export const RESERVED_ARCHIVE_BROWSING_HINT =
  t('预留位：BinFlow 归档内浏览恒开放、无引擎开关（回显/行为均无）——字段不提交。')

/** maxUniqueSnapshots（maven）：Artifactory 7.161 Advanced 步「Max Unique
 *  Snapshots」；transport 解码位存在但 configJSON 不转发。 */
export const RESERVED_MAX_UNIQUE_SNAPSHOTS_HINT =
  t('预留位：PUT 解码后即丢弃（快照去重上限行为无引擎承接）——字段不提交。')

/** suppressPomConsistencyChecks（maven）：Artifactory 7.161「Suppress POM
 *  Consistency Checks」；transport 无该字段（非 decode-only，直接未知键）。 */
export const RESERVED_SUPPRESS_POM_LABEL = t('Suppress POM Consistency Checks（suppressPomConsistencyChecks）')
export const RESERVED_SUPPRESS_POM_HINT =
  t('预留位：POM 一致性校验 BinFlow 无对位引擎行为——字段不提交。')

/** forceConanAuthentication：**实字段**（非预留位）——T-355A 起 local 臂
 *  configJSON 全收 + conan adapter 行为消费（匿名请求 401 挑战）。 */
export const FORCE_CONAN_AUTH_LABEL = t('强制认证（forceConanAuthentication——conan 匿名面一律 401 挑战）')
export const FORCE_CONAN_AUTH_HINT =
  t('默认关闭（普通内容 ACL）。开启后匿名 conan 端点请求收到 401（客户端引导挑战）——仅 conan 本地仓。')

// ---------------------------------------------------------------------------
// T-443（FR-143.4/.5）新增文案——入口分路由 + dirty-gating + remote Test 三臂
// ---------------------------------------------------------------------------

/** 入口下拉（列表页 Create a Repository 对位）：三预选各带一句描述
 *  （Artifactory 7.161.20 活体形态——el-dropdown 五型带描述行，BinFlow
 *  三型实有口径〔federated/release-bundle 为 A1 非目标域，不伪造〕）。
 *  描述语义对齐 7.161 原文：Local "Upload and resolve your own packages" /
 *  Remote "Proxy and cache packages hosted remotely" / Virtual "Access
 *  multiple repositories within a single URL"。 */
export const REPO_CREATE_ENTRY: Record<'local' | 'remote' | 'virtual', { label: string; desc: string }> = {
  local: { label: t('Local 仓库'), desc: t('上传并解析自己的制品（本地存储）') },
  remote: { label: t('Remote 仓库'), desc: t('代理并按需缓存远端托管的制品') },
  virtual: { label: t('Virtual 仓库'), desc: t('以单一 URL 聚合访问多个仓库') },
}

/** rclass 控件移除后表单内的仓型说明行（rclass 由入口分路由预选，B-3.8）。 */
export const RCLASS_ROUTE_NOTE = t('仓型由入口分路由预选（Local / Remote / Virtual），本页不可更改；创建后同样不可变。')

/** dirty-gating：进入编辑态 Save 禁用的 tooltip 原因（AC2 无变更提交不可达）。 */
export const SAVE_CLEAN_HINT = t('尚未修改任何字段——无变更不可提交（改字段后启用）。')

/** remote Test（FR-143.5，消费 T-442 端点）。 */
export const REMOTE_TEST_LABEL = t('测试连接')
export const REMOTE_TEST_HINT =
  t('对上游基址发一次只读 GET 探测（2xx/3xx/404 = 通过；401/403 = 凭据被拒）——零落盘、不触发 assumed-offline、凭据不进日志。表单已改的 url/凭据按草稿臂探测（改了 url 或用户名而未填密码 = 匿名探测）；未改动时探测已存配置（解封已存凭据）。')
export const REMOTE_TEST_CREATE_HINT = t('保存仓库后可在此测试上游连通（探测端点按已存仓寻址）。')
export const REMOTE_TEST_OK_NOTE = t('上游可达且凭据被接受')
export const REMOTE_TEST_FAIL_NOTE = t('探测未通过')
export const REMOTE_TEST_STATUS_PREFIX = t('上游应答 HTTP ')
export const REMOTE_TEST_UNREACHED_NOTE = t('未触达上游——连接层失败（DNS/拒绝/超时）')

// T-461（FR-147）新增文案——remote 远端浏览可选档（listRemoteFolderItems）
// ---------------------------------------------------------------------------

/** 批 1 支持远端枚举的包型（engine BrowseSupported 同集——helm classic
 *  index.yaml 全树 + debian/rpm 元数据臂；true 于其它包型服务端按名 400，
 *  表单只对批 1 型呈现控件——Artifactory 官方开放面 deb/generic/maven/
 *  Opkg/rpm 与 BinFlow 引擎面不同，generic/maven 不建不伪造）。 */
export const REMOTE_BROWSE_PKG_TYPES: readonly string[] = ['helm', 'debian', 'rpm']

/** 可选档标签（Artifactory 官方 UI 名 "List Remote Folder Items" /
 *  "List Remote Artifacts" 两种拼写并用——remote-browsing.md §1）。 */
export const LIST_REMOTE_FOLDER_ITEMS_LABEL = t('列出远端目录条目（listRemoteFolderItems）')
export const LIST_REMOTE_FOLDER_ITEMS_HINT =
  t('开启后目录浏览合并上游未缓存条目进树（按 metadata TTL 缓存枚举；点击未缓存条目会回源拉取）。默认关闭——树仅展示已缓存内容。')
