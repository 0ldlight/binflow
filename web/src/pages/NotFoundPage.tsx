import { Link } from 'react-router-dom'

// 404 页（console-ux §3.2）：保留导航壳，给出返回仪表盘链接。

export default function NotFoundPage() {
  return (
    <div data-testid="not-found">
      <div className="page-header">
        <h2>页面不存在</h2>
      </div>
      <section className="card" style={{ maxWidth: 560 }}>
        <p className="text-2">地址不存在或已变更。控制台路由见左侧导航。</p>
        <p>
          <Link to="/">← 返回仪表盘</Link>
        </p>
      </section>
    </div>
  )
}
