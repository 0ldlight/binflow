import { useTheme } from '@/app/providers'
import lockupDark from '../assets/brand/lockup-dark.svg?url'
import lockupLight from '../assets/brand/lockup-horizontal.svg?url'
import markDark from '../assets/brand/mark-dark.svg?url'

// 品牌资产单点引用层（FR-126 / T-389——候选 1「容器·双箭流」转正）。
//
// 资产真身 = web/src/assets/brand/*.svg（K56 生产件逐字拷贝，零改动：
// 换稿 = ux-designer 替换文件，本组件与全部消费位零返工——「资产参数化
// 单点引用」的票面口径）。色板已锚定 tokens.css 的 --bf-text/--bf-accent
// 双主题值：改 token 必须同步 K56 母版（docs/design/brand/logo/
// candidate-1/README.md §1 的同步纪律）——组件层不做任何颜色消费
// （assert-tokens 腿 2/3 的扫描面因此天然干净）。
//
// 变体选择规则（亮暗版各就各位）：
//   BrandMark    —— 侧栏顶专用（24px）。侧栏两主题恒为深底（tokens.css
//                   --bf-sidebar 系：亮色主题 #1b2430 / 暗色 #0b0e13），
//                   故固定 mark-dark（浅描边 + 亮双箭），不随主题切换。
//   BrandLockup  —— 登录页品牌区（48px 高档）。背景是随主题的 --bf-bg，
//                   故按 ThemeContext 取 lockup-horizontal（亮）/ lockup-dark
//                   （暗）。
//
// 尺寸均显式声明（img 的 width/height——无布局抖动）；lockup 宽度按
// 674×128 viewBox 等比推导。alt 承载可读名（lockup 内含 wordmark 图形，
// 屏幕 Reader 需要等价文本）；侧栏 mark 是文字旁的装饰件，alt=""。

interface BrandMarkProps {
  /** 渲染边长（px）；侧栏顶规格 24（console 品牌位唯一消费档） */
  size?: number
  testid?: string
}

/** mark 单形（侧栏顶 24px——深底恒定，mark-dark 固定变体） */
export function BrandMark({ size = 24, testid }: BrandMarkProps) {
  return (
    <img
      src={markDark}
      alt=""
      aria-hidden="true"
      width={size}
      height={size}
      data-testid={testid}
      style={{ display: 'block', flexShrink: 0 }}
    />
  )
}

interface BrandLockupProps {
  /** 渲染高度（px）；登录页规格 48（K56 §1 消费位口径） */
  height?: number
  testid?: string
}

/** 横版 lockup（mark + BinFlow wordmark——path 化，零字体依赖），随主题换变体 */
export function BrandLockup({ height = 48, testid }: BrandLockupProps) {
  const { theme } = useTheme()
  return (
    <img
      src={theme === 'dark' ? lockupDark : lockupLight}
      alt="BinFlow"
      height={height}
      width={Math.round((height * 674) / 128)}
      data-testid={testid}
      style={{ display: 'block' }}
    />
  )
}
