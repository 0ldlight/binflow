// 详情字段族文案常量（M16 T-445 / FR-144.1~.3——「新文案集中常量」纪律：
// 本票新增的详情字段标签与无源提示集中在此；既有标签〔名称/类型/大小/
// 部署者/Created/修改时间/子项〕维持原位不churn，多出字段的去留候 Q9）。
//
// 标签语言跟随面板既有混合口径（Repository Path / File URL / Created 用
// 英文，名称/类型用中文）——Artifactory 字段族名保留英文原文，parity
// 收口审计（T-460）按字段名逐一对拍。字段序 = reverse §3.2 + 7.161.20
// 活体（m16-baseline-refresh §A2-7）：Name → Repository Path → File URL
// →（Module ID 不建）→ Deployed By → Size → Created → Last Modified →
// Downloads → Last Downloaded By → Last Downloaded → Remote Downloads，
// BinFlow 自有增强字段（类型/mimeType/校验块/tags）排在 parity 族之后。

/** 下载统计族标签（file 形态 General 页——消费 T-438 ?stats 面） */
export const STATS_LABELS = {
  downloads: 'Downloads',
  lastDownloadedBy: 'Last Downloaded By',
  lastDownloaded: 'Last Downloaded',
  remoteDownloads: 'Remote Downloads',
} as const

/** 仓视图字段族标签（repo 形态 General 页——FR-144.3） */
export const REPO_FIELD_LABELS = {
  repoLayout: 'Repository Layout',
  description: 'Description',
  created: 'Created',
  artifactCount: 'Artifact Count',
} as const

/**
 * 无源字段提示（缺位登记不伪造——值恒 '—'，title 悬浮说明为何无值）：
 *
 * - Repository Layout：BinFlow 布局由各协议 adapter 固定，repoLayoutRef
 *   无引擎承接（K70，建仓表单同款预留位口径）——不编造型名。
 * - 仓 Created：后端 metadata.Repo.CreatedAt 存在但未投影到任何 wire 面
 *   （GET /api/repositories/{key} 无该字段）——纯 FE 票不添后端面，登记
 *   待 BE 承接。
 */
export const NO_SOURCE_HINTS = {
  repoLayout: '预留位：BinFlow 布局由协议固定（maven 即 maven-2 形），repoLayoutRef 无引擎承接（K70）——无值不伪造',
  repoCreated: '仓库创建时间暂无 API 面（后端 CreatedAt 未投影）——登记待后端承接',
} as const

/** 下载统计四态文案（loading/error 的占位与错误提示） */
export const STATS_HINTS = {
  loading: '…',
  /** 统计面失败（?stats 与 item-info 同门，item 成功而 stats 失败属异常面） */
  unavailable: (status: number) => `下载统计不可用（HTTP ${status}）`,
} as const

/** 值缺席的统一占位：无源字段（K70/无 wire 面）/ 从未下载 / 非档位省略
 *  （lastDownloadedBy）——不区分占位原因，避免泄漏档位信息 */
export const EMPTY_VALUE = '—'

/**
 * 远端派生行文案（M16 T-461 / FR-147——listRemoteFolderItems on 的上游
 * 枚举 display-only 行消费面）。点击派生行 = item-info GET 触发回源
 * pull-through：成功则落地成缓存行（详情自纠为真实值），失败即降级面
 * （上游停机——已缓存内容仍可浏览，remote-browsing.md §4-1）。
 */
export const REMOTE_COPY = {
  /** 派生行回源失败的详情面板错误文案（降级呈现腿） */
  fetchFailed: (status: number | string) =>
    `远端条目拉取失败（HTTP ${status}）：上游不可达或超时，或该路径已不在上游索引中——已缓存内容仍可浏览，稍后重试或刷新。`,
} as const

/**
 * 属性页签文案（M16 T-447 / FR-144.4，B-2.9 翻正——Artifactory 属性编辑
 * 解剖：常显 Property/Value 输入 + Add + 网格搜索；7.161.20 活体实证
 * placeholder 逐字 = "Property name" / "Property value"）。
 */
export const PROPS_COPY = {
  /** 常显键输入 placeholder（7.161.20 活体同文） */
  keyPlaceholder: 'Property name',
  /** 常显值输入 placeholder（7.161.20 活体同文；多值逗号分隔） */
  valuePlaceholder: 'Property value',
  /** Add 提交钮（B-2.9 解剖要素；同名键 = 整体替换其值集〔§11.40〕） */
  addLabel: 'Add 属性',
  /** 网格搜索（B-2.9 解剖要素——键/值子串过滤既有网格） */
  searchLabel: '搜索属性',
  searchPlaceholder: '搜索键或值',
  /** 同名键替换语义的可见性提示（Add 表单的 helper 文案） */
  replaceHint: '同名键 = 整体替换其值集（其他键保留）',
  /** 行内删除的危险确认（E1 统一——Q2 出口①：删除走确认，轻交互退役） */
  deleteTitle: '删除属性',
  deleteLead: '将删除属性',
  deleteTrail: '（该节点的这一个键及其全部值）。属性删除没有撤销，需要时可在上方重新添加。',
  /** 表尾常驻说明（保存/删除语义与服务端口径——T-291 起维持） */
  footnote: 'Add = PUT（该键值集整体替换，其他键保留）；删除 = DELETE 该键（危险确认）。与服务端规则同口径：键 [A-Za-z][A-Za-z0-9_.-]{0,63}，值 ≤1KiB、无控制字符，单键 ≤32 值，节点 ≤64 键。',
} as const

/**
 * 下载形态文案（M16 T-447 / FR-144.5，B-2.12 翻正 + Q9 处置——单 24px
 * 图标钮（直接下载）+ 伴随菜单承载校验能力与 checksum/mimeType 信息）。
 */
export const DOWNLOAD_COPY = {
  /** 单图标钮（直接下载——浏览器原生落盘，Artifactory 单 24px 图标对位） */
  iconLabel: '下载',
  iconTitle: '下载（浏览器直接落盘）',
  /** 伴随菜单触发（校验能力 + checksum/mimeType 的家——Q9「收进伴随形态」） */
  menuLabel: '下载与校验',
  /** 伴随菜单内的校验动作（sha256 对账——原「下载并校验」按钮能力） */
  verifyLabel: '下载并校验（sha256 对账）',
  verifyBusy: '正在下载并计算 sha256（大文件稍慢）…',
  verifyOk: '✓ 下载落盘 sha256 与服务端一致',
  verifyBad: '✗ 不一致！下载内容与服务端登记的 checksum 不匹配',
  /** checksum/mimeType 区（Q9：mimeType 与校验徽标块自 General 页收进伴随） */
  checksumsHeader: 'Checksums',
  mimeTypeLabel: 'mimeType',
  /** 大文件指引（校验是浏览器内存路径——Blob 落盘的固有成本提示） */
  verifyHint: '大文件建议直接下载（校验经浏览器内存路径）',
} as const
