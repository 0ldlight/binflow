import { Link, useLocation } from 'react-router-dom'

import { tr } from '@/i18n'

const t = tr('console')

// 404 页（console-m8 §1.3「未匹配（保留导航壳）」——P3 新栈重写）：
// 保留导航壳；深链状态回显——展示触发 404 的原始路径（mono，未解码原文），
// 主行动回应用模式首页（/artifacts，§1.1 登录落点同源）。
// 锚族原样：not-found/not-found-path/not-found-home。

export default function NotFoundPage() {
  const { pathname } = useLocation()
  return (
    <div data-testid="not-found">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('页面不存在')}</h2>
      </div>
      <section className="card max-w-[560px] rounded-md border border-border bg-surface-1 p-4">
        <p className="text-2">{t('地址不存在或已变更。控制台路由见左侧导航。')}</p>
        <p>
          <span className="text-2">{t('请求的地址：')}</span>
          <span className="font-mono" data-testid="not-found-path" lang="en">
            {pathname}
          </span>
        </p>
        <p>
          <Link to="/artifacts" data-testid="not-found-home" className="text-primary underline underline-offset-2">
            {t('← 回主页（制品树）')}
          </Link>
        </p>
      </section>
    </div>
  )
}
