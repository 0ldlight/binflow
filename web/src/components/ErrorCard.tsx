import { useState } from 'react'
import Alert from '@mui/material/Alert'
import AlertTitle from '@mui/material/AlertTitle'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Collapse from '@mui/material/Collapse'
import Link from '@mui/material/Link'

import type { ApiError } from '../lib/api'
import { tr } from '../i18n'

const t = tr('console')

// 错误卡（console-ux §5.1）：一句人话 + 原始 message 折叠区（mono）+ 重试。
// T-344 批 B：div.error-card 手作卡 → MUI Alert severity=error + Collapse
// （mui-native-visual §4.2）；原始 message 默认折叠——工程师排障需要它，
// 但它不该淹没页面。403 有专门的呈现（无权限卡 / 隐藏），不走这里。

export function ErrorCard({ error, onRetry }: { error: ApiError; onRetry?: () => void }) {
  const [rawOpen, setRawOpen] = useState(false)
  const headline =
    error.status >= 500 || error.status === 0
      ? t('服务暂不可用')
      : error.status === 404
        ? t('资源不存在')
        : t('请求失败（HTTP {v1}）', { v1: error.status })
  return (
    <Alert severity="error" data-testid="error-card" sx={{ alignItems: 'flex-start' }}>
      <AlertTitle>{headline}</AlertTitle>
      {error.message && <div className="text-2">{error.message}</div>}
      {error.raw && (
        <>
          <Link
            component="button"
            variant="body2"
            underline="hover"
            onClick={() => setRawOpen((v) => !v)}
            aria-expanded={rawOpen}
          >{t('原始响应')}          </Link>
          <Collapse in={rawOpen} timeout="auto" unmountOnExit>
            <Box
              component="pre"
              lang="en"
              sx={{
                m: 0,
                mt: 1,
                p: 1,
                fontFamily: 'var(--bf-mono)',
                fontSize: 12,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                maxHeight: 160,
                overflow: 'auto',
                bgcolor: 'action.hover',
                borderRadius: 1,
              }}
            >
              {error.raw.slice(0, 2000)}
            </Box>
          </Collapse>
        </>
      )}
      {onRetry && (
        <Button size="small" variant="outlined" color="error" onClick={onRetry} sx={{ mt: 1 }}>{t('重试')}        </Button>
      )}
    </Alert>
  )
}
