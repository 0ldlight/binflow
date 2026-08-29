import { Link } from 'react-router-dom'

import Chip from '@mui/material/Chip'
import Paper from '@mui/material/Paper'
import Typography from '@mui/material/Typography'

import { useAuth } from '../app/AuthContext'

// 占位页（T-98）：18 路由表中本批未交付的页面（仓库 T-99 / 搜索与树
// T-100 / 安全 T-101 / 治理与审计 T-102）。深链可达、壳保留、明确
// 告知交付票号——不给 404（这些是已规划路由，不是错误地址）。
// adminOnly 页面对非 admin 呈现无权限卡（§3.3 收敛）。
// T-344 批 D：.card → Paper；票号徽章 span.badge → Chip（neutral =
// filled default + mono，批 C 色彩映射口径）。

export default function PlaceholderPage({
  title,
  ticket,
  adminOnly = false,
}: {
  title: string
  ticket: string
  adminOnly?: boolean
}) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const denied = adminOnly && !admin

  return (
    <div data-testid="placeholder-page">
      <div className="page-header">
        <h2>{title}</h2>
      </div>
      <Paper component="section" className="card" elevation={1} sx={{ maxWidth: 560 }}>
        {denied ? (
          <>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
              无权限
            </Typography>
            <p className="text-2">此页面属于管理面板，需要管理员权限。当前用户 {session?.username} 不是 admin。</p>
          </>
        ) : (
          <>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
              即将交付{' '}
              <Chip
                component="span"
                size="small"
                className="badge neutral"
                label={ticket}
                lang="en"
                sx={{ fontFamily: 'var(--bf-mono)', verticalAlign: 'middle' }}
              />
            </Typography>
            <p className="text-2">「{title}」页面在控制台后续批次交付，当前版本暂未包含。</p>
            <p className="text-muted">在交付前，对应操作可通过 REST API 或 CLI 完成（见 docs/user）。</p>
          </>
        )}
        <p>
          <Link to="/">← 返回仪表盘</Link>
        </p>
      </Paper>
    </div>
  )
}
