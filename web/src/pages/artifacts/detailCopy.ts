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
