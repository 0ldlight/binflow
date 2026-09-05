import { Link, useLocation } from 'react-router-dom'

import Paper from '@mui/material/Paper'
import { tr } from '../i18n'

const t = tr('console')

// 404 页（console-m8 §1.3「未匹配（保留导航壳）」；T-239 对齐形态重排）：
// 保留导航壳；深链状态回显——展示触发 404 的原始路径（mono，未解码原文
// 与 T-231 编码矩阵同口径），主行动回应用模式首页（/artifacts，§1.1
// 登录落点同源）。
// T-344 批 D：.card 手作卡 → Paper（类名留 DOM 作 inert 渐进迁移钩子，
// base.css .card 族 :not(.MuiPaper-root) shim 排除——皮肤交还 Paper）。

export default function NotFoundPage() {
  const { pathname } = useLocation()
  return (
    <div data-testid="not-found">
      <div className="page-header">
        <h2>{t('页面不存在')}</h2>
      </div>
      <Paper component="section" className="card" elevation={1} sx={{ maxWidth: 560 }}>
        <p className="text-2">{t('地址不存在或已变更。控制台路由见左侧导航。')}</p>
        <p>
          <span className="text-2">{t('请求的地址：')}</span>
          <span className="mono" data-testid="not-found-path" lang="en">
            {pathname}
          </span>
        </p>
        <p>
          <Link to="/artifacts" data-testid="not-found-home">{t('← 回主页（制品树）')}          </Link>
        </p>
      </Paper>
    </div>
  )
}
