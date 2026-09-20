import { useTheme } from '@/app/providers'
import horizontalDark from '../assets/brand/modular/binflow-horizontal-dark.svg?url'
import horizontalPrimary from '../assets/brand/modular/binflow-horizontal-primary.svg?url'
import symbolDark from '../assets/brand/modular/binflow-symbol-dark.svg?url'
import symbolPrimary from '../assets/brand/modular/binflow-symbol-primary.svg?url'

// BinFlow modular brand system.
//
// The SVG suite is the single visual source; PNG app icons under public/brand
// are exported from the same geometry. Variants stay grayscale and follow the
// active theme: `primary` is for light surfaces, while `dark` is the light
// variant intended for dark surfaces (the supplied kit's naming).
//
// Slots are deliberately restrained:
// - BrandLockup: navigation/login surfaces that need the full wordmark.
// - BrandMark: compact symbol-only surfaces such as dialogs.
const HORIZONTAL_ASPECT = 840 / 256

interface BrandMarkProps {
  /** Rendered square edge in pixels. */
  size?: number
  testid?: string
}

export function BrandMark({ size = 24, testid }: BrandMarkProps) {
  const { theme } = useTheme()
  return (
    <img
      src={theme === 'dark' ? symbolDark : symbolPrimary}
      alt=""
      aria-hidden="true"
      width={size}
      height={size}
      data-testid={testid}
      data-brand-variant={theme === 'dark' ? 'dark' : 'primary'}
      style={{ display: 'block', flexShrink: 0 }}
    />
  )
}

interface BrandLockupProps {
  /** Rendered height in pixels; width follows the supplied 840:256 lockup. */
  height?: number
  testid?: string
}

export function BrandLockup({ height = 48, testid }: BrandLockupProps) {
  const { theme } = useTheme()
  return (
    <img
      src={theme === 'dark' ? horizontalDark : horizontalPrimary}
      alt="BinFlow"
      height={height}
      width={Math.round(height * HORIZONTAL_ASPECT)}
      data-testid={testid}
      data-brand-variant={theme === 'dark' ? 'dark' : 'primary'}
      style={{ display: 'block' }}
    />
  )
}
