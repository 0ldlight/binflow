import type { ApiError } from '../lib/api'

// 错误卡（console-ux §5.1）：图标 + 一句人话 + 原始 message 折叠区
// （mono）+ 重试。原始 message 默认折叠——工程师排障需要它，但它
// 不该淹没页面。403 有专门的呈现（无权限卡 / 隐藏），不走这里。

export function ErrorCard({ error, onRetry }: { error: ApiError; onRetry?: () => void }) {
  const headline =
    error.status >= 500 || error.status === 0
      ? '服务暂不可用'
      : error.status === 404
        ? '资源不存在'
        : `请求失败（HTTP ${error.status}）`
  return (
    <div className="error-card" data-testid="error-card" role="alert">
      <div className="headline">
        <span aria-hidden="true">✗</span>
        {headline}
      </div>
      {error.message && <div className="text-2">{error.message}</div>}
      {error.raw && (
        <details>
          <summary>原始响应</summary>
          <pre lang="en">{error.raw.slice(0, 2000)}</pre>
        </details>
      )}
      {onRetry && (
        <div style={{ marginTop: 8 }}>
          <button type="button" className="btn" onClick={onRetry} data-testid="error-retry">
            重试
          </button>
        </div>
      )}
    </div>
  )
}
