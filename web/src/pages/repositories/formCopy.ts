// T-439（FR-143.1/.2）新增文案集中常量——i18n 独占波降本纪律（M16-SPLIT
// 附则）：本票引入的全部新用户可见文案集中于此，T-463（FR-149 文案外提
// 100%）搬运时按文件收口；既有文案零挪动。
//
// 命名：键与锚/字段一一对应，值 = 中文呈现（术语保真英文原词——console-ux
// §1.2 两包条款预对齐）。

export const FORM_STEPS = {
  basic: '基础',
  advanced: '高级',
  replications: 'Replications',
} as const

export const RESERVED_GROUP_BASIC_TITLE =
  'Artifactory 对齐字段（预留位——当前无效，不提交、不存储）'

export const RESERVED_GROUP_ADVANCED_TITLE =
  'Artifactory 对齐复选项（预留位——当前无效，不提交、不存储）'

export const RESERVED_PLACEHOLDER = '（预留位）'

/** repoLayoutRef：Artifactory 7.161 Basic 步在场的 Repository Layout。
 *  BinFlow 布局由各协议 adapter 固定（maven = maven-2 形路径探测），无
 *  repoLayoutRef 消费方（grep internal/ 零读点）——后端承接落地后启用（K70）。 */
export const RESERVED_REPO_LAYOUT_HINT =
  '预留位：BinFlow 布局由协议固定（maven 即 maven-2 形），repoLayoutRef 暂无引擎承接（K70）——字段不提交。'

/** Environments：7.84 审计名为 Environments，7.161.20 实测已更名 Stage
 *  （Stages & Lifecycle 域）——预留位按新名标注，双名留痕。 */
export const RESERVED_ENVIRONMENTS_HINT =
  '预留位：BinFlow 无环境段模型（Artifactory 7.161 已由 Environments 更名 Stage）——字段不提交。'

/** notes（Internal Description）：transport 侧 repoConfig.Notes 有解码位但
 *  configJSON 不转发（decode-only，与 blackedOut 同类）——预留位。 */
export const RESERVED_INTERNAL_DESCRIPTION_HINT =
  '预留位：后端 config 传输层未承接 notes（公开描述即上方「描述」字段）——字段不提交。'

/** blackedOut：Artifactory 7.161 标签「Disable Artifact Resolution in
 *  Repository」；拒写行为联动随 BE 承接票（票内登记）。 */
export const RESERVED_BLACKED_OUT_LABEL = 'blackedOut（Artifactory：Disable Artifact Resolution in Repository）'
export const RESERVED_BLACKED_OUT_HINT =
  '预留位：PUT 解码后即丢弃（回显/行为均无）——true 拒写语义随后端承接票落地。'

/** archiveBrowsingEnabled：Artifactory 7.161 标签「Allow Artifact Content
 *  Browsing」；BinFlow 归档内浏览自 M1 起默认开放、无开关位。 */
export const RESERVED_ARCHIVE_BROWSING_LABEL = 'archiveBrowsingEnabled（Artifactory：Allow Artifact Content Browsing）'
export const RESERVED_ARCHIVE_BROWSING_HINT =
  '预留位：BinFlow 归档内浏览恒开放、无引擎开关（回显/行为均无）——字段不提交。'

/** maxUniqueSnapshots（maven）：Artifactory 7.161 Advanced 步「Max Unique
 *  Snapshots」；transport 解码位存在但 configJSON 不转发。 */
export const RESERVED_MAX_UNIQUE_SNAPSHOTS_HINT =
  '预留位：PUT 解码后即丢弃（快照去重上限行为无引擎承接）——字段不提交。'

/** suppressPomConsistencyChecks（maven）：Artifactory 7.161「Suppress POM
 *  Consistency Checks」；transport 无该字段（非 decode-only，直接未知键）。 */
export const RESERVED_SUPPRESS_POM_LABEL = 'Suppress POM Consistency Checks（suppressPomConsistencyChecks）'
export const RESERVED_SUPPRESS_POM_HINT =
  '预留位：POM 一致性校验 BinFlow 无对位引擎行为——字段不提交。'

/** forceConanAuthentication：**实字段**（非预留位）——T-355A 起 local 臂
 *  configJSON 全收 + conan adapter 行为消费（匿名请求 401 挑战）。 */
export const FORCE_CONAN_AUTH_LABEL = '强制认证（forceConanAuthentication——conan 匿名面一律 401 挑战）'
export const FORCE_CONAN_AUTH_HINT =
  '默认关闭（普通内容 ACL）。开启后匿名 conan 端点请求收到 401（客户端引导挑战）——仅 conan 本地仓。'
