// T-463 键集 / T-464 填充：bundles 域 en 目录包——键 = zh 文案原文
// （zh-as-key），值 = 逐义对译 en（术语对齐 Artifactory：repo key / node /
// checksum / Deploy / Set Me Up 等英文术语原样保留）。
import { registerEn } from '../../index'

registerEn('bundles', {
  "（空清单）": "(empty manifest)",
  "← 返回 bundle 列表": "← Back to bundles",
  "← 返回版本列表": "← Back to versions",
  "版本化发布记录（名 → 版本 → 描述符，只读查询面）": "Versioned release records (name → version → descriptor; read-only query face)",
  "创建时间": "Created",
  "创建者": "Created By",
  "读门 = 系统读权限 ∨ Any Distribution 通道（按 bundle 名授予）——403 即读门拒绝（拒绝即答案，不与不存在混同）。": "The read gate = system read capability ∨ the Any Distribution channel (granted per bundle name) — a 403 is the gate's refusal (the refusal itself is the answer, never conflated with not-found).",
  "服务端按会话可见集过滤（系统读权限 ∨ Any Distribution 通道授予面）——空集如实呈现；管理员可经 API 创建（POST /api/release/bundle）。": "The server filters by this session's visible set (system read capability ∨ the Any Distribution channel grant) — an empty set is shown as-is; admins create via the API (POST /api/release/bundle).",
  "描述符校验和": "descriptor checksum",
  "描述符校验和（HEAD）": "Descriptor checksum (HEAD)",
  "名单面按会话可见集过滤（空集如实）；版本/描述符面对无读门会话是明确的 403。读门 = 系统读权限 ∨ Any Distribution 通道（按 bundle 名授予）。": "The names face filters by this session's visible set (an empty set is shown as-is); the versions/descriptor faces answer an explicit 403 to sessions without the read gate. Read gate = system read capability ∨ the Any Distribution channel (granted per bundle name).",
  "签名（清单摘要）": "Signature (manifest digest)",
  "清单身份集摘要占位（排序后的 repo/path 行集 sha256——签名缺席适配，E6 语义）": "Manifest identity-set digest placeholder (sha256 over the sorted repo/path rows — the signature-absent adaptation, E6 semantics)",
  "清单引用的制品尚不在本实例（pending 行——bundle 因此 INPROGRESS）": "The artifact this manifest row references is not on this instance yet (pending row — hence the bundle is INPROGRESS)",
  "无权限查看此 Release Bundle": "No permission to view this release bundle",
  "暂无可见的 Release Bundle": "No visible release bundles",
  "制品清单（{v1} 项）": "Manifest ({v1} items)",
  "Bundle": "Bundle",
  "ⓘ 创建走 API：POST /api/release/bundle（显式清单形——name / version / artifacts[]，release-bundle 槽位门）；控制台创建面归后续票，本页不伪造表单。": "ⓘ Creation goes through the API: POST /api/release/bundle (explicit manifest — name / version / artifacts[], gated by the release-bundle feature slot); the console creation face lands with a later ticket — this page does not fake a form.",
  "Release Bundle {name} 不存在": "Release bundle {name} does not exist",
  "Release Bundle 不存在": "Release bundle does not exist",
  "sha256": "sha256",
})
