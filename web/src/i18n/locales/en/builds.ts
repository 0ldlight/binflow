// T-463 键集 / T-464 填充：builds 域 en 目录包——键 = zh 文案原文
// （zh-as-key），值 = 逐义对译 en（术语对齐 Artifactory：repo key / node /
// checksum / Deploy / Set Me Up 等英文术语原样保留）。
import { registerEn } from '../../index'

registerEn('builds', {
  "（无模块——append 可按 module id 增量并入）": "(no modules — append merges incrementally by module id)",
  "← 返回 run 列表": "← Back to runs",
  "← 返回构建列表": "← Back to builds",
  "本 run 无 audit 事件行（retention 是名级窗口事件，不归属单个 run）。": "No audit event rows for this run (retention is a name-level window event, not attributable to a single run).",
  "读门 = r(buildRepo, buildName) 镜像——403 即读门拒绝（拒绝即答案，不与不存在混同）。": "The read gate mirrors r(buildRepo, buildName) — a 403 is the gate's refusal (the refusal itself is the answer, never conflated with not-found).",
  "发布": "Publish",
  "服务端按会话可见集过滤（r(buildRepo, buildName) 授予面）——空集如实呈现；CI 经 PUT /api/build 发布（jf rt build-publish 同形）。": "The server filters by this session's visible set (the r(buildRepo, buildName) grant face) — an empty set is shown as-is; CI publishes via PUT /api/build (the same shape as jf rt build-publish).",
  "构建 {name} 不存在（或当前会话不可见）": "Build {name} does not exist (or is not visible to this session)",
  "构建不存在": "Build does not exist",
  "号单面按会话可见集过滤——不可读的构建名与不存在的名同形（零泄漏）；详情面 404 = run 真缺（?started= 消歧同名同号多 run）。": "The runs face filters by this session's visible set — an unreadable build name is indistinguishable from a nonexistent one (zero leakage); on the detail face a 404 is a genuinely missing run (?started= disambiguates same-name same-number runs).",
  "晋升": "Promote",
  "模块（{v1} 个）": "Modules ({v1})",
  "模块依赖（{v1} 项）": "Module dependencies ({v1} items)",
  "模块制品（{v1} 项）": "Module artifacts ({v1} items)",
  "目标仓": "Target repo",
  "时间线不可用（HTTP": "Timeline unavailable (HTTP",
  "事件时间线（audit）": "Event timeline (audit)",
  "属性（{v1} 项）": "Properties ({v1})",
  "无权限查看此构建": "No permission to view this build",
  "依赖": "Dependencies",
  "依赖数": "Dependencies",
  "暂无可见的构建": "No visible builds",
  "制品数": "Artifacts",
  "追加合并": "Append merge",
  "最新启动": "Last Started",
  "CI 构建记录（构建名 → run 号 → run 详情，只读查询面）": "CI build records (build name → run number → run detail; read-only query face)",
  "ⓘ 发布走 API：PUT /api/build（body = build info JSON，name/number 在 body）；promote 走 POST /api/build/promote/{name}/{number}——控制台动作面归后续票，本页不伪造表单。": "ⓘ Publishing goes through the API: PUT /api/build (body = the build info JSON, name/number in the body); promotion goes through POST /api/build/promote/{name}/{number} — the console action face lands with a later ticket; this page does not fake a form.",
  "ⓘ promote 走 API：POST /api/build/promote/{name}/{number}（body：status/comment/ciUser/timestamp/dryRun/sourceRepo/targetRepo/copy/artifacts/dependencies/scopes/properties/failFast）——ux 册无承载定案，控制台动作面归后续票。": "ⓘ Promotion goes through the API: POST /api/build/promote/{name}/{number} (body: status/comment/ciUser/timestamp/dryRun/sourceRepo/targetRepo/copy/artifacts/dependencies/scopes/properties/failFast) — the ux book has no settled carrier for it; the console action face lands with a later ticket.",
  "promotion 历史（{v1} 条）": "Promotion history ({v1} entries)",
  "record-only 行：上传文档的路径未解析到本实例节点（无 repo 段/节点缺/sha256 相左）——行存不冒领关联": "Record-only row: the uploaded document's path did not resolve to a node on this instance (no repo segment / node missing / sha256 disagreement) — the row is stored without claiming the association",
})
