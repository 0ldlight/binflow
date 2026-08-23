import { Link, useLocation } from 'react-router-dom'

// 404 页（console-m8 §1.3「未匹配（保留导航壳）」；T-239 对齐形态重排）：
// 保留导航壳；深链状态回显——展示触发 404 的原始路径（mono，未解码原文
// 与 T-231 编码矩阵同口径），主行动回应用模式首页（/artifacts，§1.1
// 登录落点同源）。

export default function NotFoundPage() {
  const { pathname } = useLocation()
  return (
    <div data-testid="not-found">
      <div className="page-header">
        <h2>页面不存在</h2>
      </div>
      <section className="card" style={{ maxWidth: 560 }}>
        <p className="text-2">地址不存在或已变更。控制台路由见左侧导航。</p>
        <p>
          <span className="text-2">请求的地址：</span>
          <span className="mono" data-testid="not-found-path" lang="en">
            {pathname}
          </span>
        </p>
        <p>
          <Link to="/artifacts" data-testid="not-found-home">
            ← 回主页（制品树）
          </Link>
        </p>
      </section>
    </div>
  )
}
