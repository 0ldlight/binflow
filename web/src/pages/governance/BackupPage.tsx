import { CopyButton } from '../../components/CopyButton'

// 备份 / 恢复（T-102 AC③；ux R5 兜底形态）：export/import 的浏览器 UI
// **不做**（在线任务 + 产物下载面不存在）——页面收敛为 CLI 引导块 +
// 文档指引。命令与 T-96 的 CLI 契约同源（cmd/binflow-server export /
// import），警示项对齐 ADR-0015 / NFR-S22 / ADR-0012 恢复链。

interface CmdBlock {
  title: string
  text: string
  note?: string
}

const BLOCKS: CmdBlock[] = [
  {
    title: '导出（在线 export）',
    text: [
      '# 在线执行：持有 data 目录维护锁（与 GC 互斥），SQLite 快照先行，blobs 拷贝保 mtime',
      'binflow-server export -c /etc/binflow/config.yaml \\',
      '  --output /backup/binflow-$(date +%F)',
    ].join('\n'),
    note: '产物目录含口令哈希与 enc:v1: 密文——权限 0700 保管（NFR-S22）；--tar 打包为 P2 债务。',
  },
  {
    title: '恢复（停机 import）',
    text: [
      '# 一律停机执行；目标 data dir 必须为空（非空 fail-fast，无半恢复）',
      'binflow-server import -c /etc/binflow/config.yaml \\',
      '  --input /backup/binflow-2026-08-21 --verify full',
    ].join('\n'),
    note: '--verify spot（默认，size 全验 + sha256 抽验前 100）/ full（全量重哈希）；恢复后 web_sessions 不回带（需重登），remote 凭据依赖 BINFLOW_REMOTE_CREDENTIALS_KEY（ADR-0012 恢复链）。',
  },
]

export default function BackupPage() {
  return (
    <div data-testid="backup-page">
      <div className="page-header">
        <h2>备份 / 恢复</h2>
      </div>

      <section className="card section">
        <h3>CLI 引导（浏览器面不提供 export / import）</h3>
        <p className="text-2">
          备份恢复走 CLI（<span className="mono" lang="en">binflow-server export / import</span>）。
          在线 export 与 GC 共用 data 目录维护锁：一方运行时另一方被拒
          （REST GC 409 / CLI 非零退出码），不会排队等待。
        </p>
        {BLOCKS.map((b) => (
          <div className="cmd-block" key={b.title} data-testid={`backup-cmd-${b.title.startsWith('导出') ? 'export' : 'import'}`}>
            <header>
              <span>{b.title}</span>
              <CopyButton value={b.text} label={b.title} />
            </header>
            <pre lang="en">{b.text}</pre>
            {b.note && <div className="note">{b.note}</div>}
          </div>
        ))}
        <p className="field-hint" style={{ marginBottom: 0 }}>
          完整手册（mtime 保管告警、恢复链、停机强一致可选项）见 docs/user 备份恢复篇（T-107 交付）；
          上次 export.run / import.run 记录经审计日志查询。
        </p>
      </section>
    </div>
  )
}
