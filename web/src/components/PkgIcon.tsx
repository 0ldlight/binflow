import type { CSSProperties } from 'react'

import type { PackageType } from '../lib/repos'

import './pkg-icon.css'

// 包型图标单点接线（FR-127 / T-390，资产源 docs/design/brand/package-icons
// ——UX-1/K56 生产件搬运）：30 枚（mono/brand × 13 包型 + trashcan/webhook）。
//
// [单点引用] import.meta.glob(?raw, eager) 把 SVG 文件按名内联成字符串——
//   资产与消费点之间零命名耦合：换文件/补文件零返工，也不产生 30 个独立
//   请求（gzip 后计入 SPA 预算，AC3 的 ≤10KB 门按此口径量）。构建期卫生：
//   XML 注释（源文件的形态注记）与 <title>（可及名改由消费点语境承载——
//   图标全部与可见文字同现，装饰位）在模块加载时剥除。
// [两版纪律]（README §1）：mono = currentColor 随文字色（列表/树/表单等
//   正文位）；brand = 官方品牌色（pkg-grid/smu-grid/addon 矩阵等包型身份
//   位）；门控（license 未解锁）一律 mono + 容器 opacity——品牌色置灰会
//   脏色（README §6.3），且 brand 的暗底提亮只挂 [data-variant='brand']，
//   mono 的 currentColor 不受其影响。
// [命名例外]（README §1）：deb.svg ↔ wire 'debian'（目录名取任务口径）；
//   go.svg 与 wire 值 'go' 同名（注记性例外，零映射）。
// [暗底提亮] 11 个官方深色在暗色磁贴/表格面 <3:1（AC3 抽查），提亮档见
//   pkg-icon.css 尾块 + tokens.css 的 --bf-pkgicon-*（K61 登记 README §4）。

/** 图标 id = PackageType ∪ addon 两枚（trashcan/webhook，License 矩阵用） */
export type PkgIconId = PackageType | 'trashcan' | 'webhook'

export type PkgIconVariant = 'brand' | 'mono'

const BRAND_SRC = import.meta.glob('../assets/pkg-icons/brand/*.svg', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const MONO_SRC = import.meta.glob('../assets/pkg-icons/mono/*.svg', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

/** wire id → 资产名（debian → deb 例外见文件头注；其余同名） */
const ASSET_NAME: Record<PkgIconId, string> = {
  generic: 'generic',
  docker: 'docker',
  maven: 'maven',
  npm: 'npm',
  pypi: 'pypi',
  go: 'go',
  nuget: 'nuget',
  cargo: 'cargo',
  conan: 'conan',
  helm: 'helm',
  helmoci: 'helmoci',
  rpm: 'rpm',
  debian: 'deb',
  trashcan: 'trashcan',
  webhook: 'webhook',
}

const FALLBACK_ASSET = 'generic'

/** 剥注释与 <title>（文件头注「构建期卫生」） */
function hygienize(src: string): string {
  return src.replace(/<!--[\s\S]*?-->/g, '').replace(/\s*<title>[\s\S]*?<\/title>/, '')
}

function svgSource(id: string, variant: PkgIconVariant): string {
  const dir = variant === 'brand' ? BRAND_SRC : MONO_SRC
  const name = ASSET_NAME[id as PkgIconId] ?? FALLBACK_ASSET
  return hygienize(dir[`../assets/pkg-icons/${variant}/${name}.svg`] ?? dir[`../assets/pkg-icons/${variant}/${FALLBACK_ASSET}.svg`] ?? '')
}

/**
 * 包型图标。装饰位（默认，aria-hidden——图标恒与可见文字同现）；仅当图标
 * 独立承载语义时给 label（role=img + aria-label，AC3 的「语义处」腿）。
 * size = 像素边长（24 viewBox 按比例缩放；笔宽基准 2 已按 16~24px 槽校过，
 * 不再整体缩放 svg——README §2）。
 *
 * 返回恒为元素（MUI Chip icon 等 ReactElement 位可直接消费）；资产缺席
 * （glob 未命中）时渲染空槽 span——不渲染 null 会破 flex/gap 布局节奏。
 */
export function PkgIcon({
  id,
  variant,
  size = 16,
  className,
  style,
  label,
}: {
  /** 运行时可能来自服务端 wire 值（未知型回退 generic——树/列表既有口径） */
  id: PkgIconId | (string & {})
  variant: PkgIconVariant
  size?: number
  className?: string
  style?: CSSProperties
  /** 语义位可及名（缺省 = 装饰 aria-hidden） */
  label?: string
}) {
  const html = svgSource(id, variant)
  const cls = className ? `pkg-svg ${className}` : 'pkg-svg'
  const a11y = label ? { role: 'img' as const, 'aria-label': label } : { 'aria-hidden': true }
  return (
    <span
      {...a11y}
      data-icon={id}
      data-variant={variant}
      className={cls}
      style={{ width: size, height: size, ...style }}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
