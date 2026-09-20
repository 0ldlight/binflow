import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
// Styleguide 页（design-system-plan §6 批 4 / §4.2）：P0 原语状态矩阵的
// 可视化网格——七态（light/dark × hover/active/disabled/loading/error）
// 逐组件陈列，双主题经页内 toggle（useTheme）即时切换。
//
// 构建态门控（负证口径）：本页路由仅 (a) vite dev server（import.meta.env.DEV）
// 与 (b) `npm run build:styleguide`（vite build --mode styleguide，e2e 腿
// 的隔离实例产物）注册——生产构建不注册，lazy chunk 随死分支 tree-shake，
// 生产 dist 无本 chunk（见 app/router/index.tsx 门控注释）。
//
// 文案策略：dev 工具面（生产不可达），标签为英文技术名词直书，不进 i18n
// 目录（assert-i18n 的 CJK 硬闸零接触——本页无中文文案）。
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Moon, Search, Sun } from 'lucide-react'

import { useTheme } from '@/app/providers'
import { Badge } from '@/components/ui/badge'
import {
  Breadcrumb,
  BreadcrumbEllipsis,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
import { Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle, DrawerTrigger } from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { CopyButton } from '@/components/layout/copy-button'
import { TransferBox } from '@/components/layout/transfer-box'
import { ErrorCard, EmptyState, StateSkeleton } from '@/components/layout/states'
import { toast } from '@/lib/toast'

/** 单态格：态标签 + 载体（§4.2 矩阵单元格的可视化形态） */
function Cell({ label, children }: { label: string; children: ReactNode }) {
  return (
    <figure className="flex w-44 shrink-0 flex-col gap-1.5">
      <figcaption className="text-aux text-muted-foreground">{label}</figcaption>
      <div className="flex min-h-14 flex-wrap items-center gap-3">{children}</div>
    </figure>
  )
}

/** 组件区：h2 锚 + 矩阵格容器 */
function Section({ id, title, desc, children }: { id: string; title: string; desc?: string; children: ReactNode }) {
  return (
    <section id={id} className="scroll-mt-20">
      <h2 className="text-h3 font-semibold">{title}</h2>
      {desc && <p className="mt-0.5 text-dense text-muted-foreground">{desc}</p>}
      <div className="mt-3 flex flex-wrap items-start gap-4 rounded-md border border-border bg-surface-1 p-4">{children}</div>
    </section>
  )
}

/** ArtifactTree 行配方复刻（§4.2 行态——真实件在 TreePanel.tsx，此处为
 * styleguide 陈列面：无虚拟化/懒加载语义，皮肤类逐字同源） */
function TreeDemoRow({
  depth,
  selected = false,
  onChain = false,
  onSelect,
  icon,
  testid,
  children,
}: {
  depth: number
  selected?: boolean
  onChain?: boolean
  onSelect?: () => void
  icon: string
  testid: string
  children: ReactNode
}) {
  return (
    <div
      data-testid={testid}
      role="treeitem"
      aria-selected={selected || undefined}
      aria-level={depth + 1}
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (onSelect && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault()
          onSelect()
        }
      }}
      className={`relative flex cursor-pointer items-center gap-1 rounded-sm px-1.5 py-1 transition-colors duration-fast ease-standard ${
        selected ? 'bg-primary/10 font-semibold' : onChain ? 'bg-surface-2' : ''
      } hover:bg-surface-2 focus-visible:outline-2 focus-visible:-outline-offset-1 focus-visible:outline-ring`}
      style={{ paddingLeft: depth * 14 + 6 }}
    >
      {Array.from({ length: depth }, (_, i) => (
        <span key={i} aria-hidden="true" className="absolute inset-y-0 w-px bg-border" style={{ left: i * 14 + 6 }} />
      ))}
      {selected && <span aria-hidden="true" className="absolute inset-y-0 left-0 w-0.5 rounded-full bg-primary" />}
      <span aria-hidden="true" className="w-6 shrink-0 text-muted-foreground">
        {icon}
      </span>
      <span className="truncate font-mono" lang="en">
        {children}
      </span>
    </div>
  )
}

/** ChecksumBlock 行配方复刻（截断+reveal+缺失「—」；copy 走真 CopyButton） */
function ChecksumDemoRow({ algo, value }: { algo: string; value: string | null }) {
  const [full, setFull] = useState(false)
  return (
    <div className="kv mb-1 flex gap-2 text-dense">
      <span className="k w-16 shrink-0 text-muted-foreground" lang="en">
        {algo}
      </span>
      <span className="min-w-0 break-all font-mono tabular-nums" lang="en">
        {value ? (
          <>
            {value.length > 24 ? (
              <Button
                type="button"
                data-testid={`sg-checksum-reveal-${algo}`}
                aria-expanded={full}
                title="Click to expand or collapse the full checksum value"
                onClick={() => setFull((f) => !f)}
                className="rounded-xs underline decoration-border-strong decoration-dotted underline-offset-2 hover:decoration-primary"
              >
                {full ? value : `${value.slice(0, 20)}…${value.slice(-8)}`}
              </Button>
            ) : (
              value
            )}
            <CopyButton value={value} label={algo} />
          </>
        ) : (
          '—'
        )}
      </span>
    </div>
  )
}

export default function StyleguidePage() {
  const { theme, toggle } = useTheme()
  const [checked, setChecked] = useState(true)
  const [switchOn, setSwitchOn] = useState(true)
  const [radio, setRadio] = useState('a')
  const [retryCount, setRetryCount] = useState(0)
  // 批 6 域件/P2 demo 态（tree 选中迁移 / transfer 勾选）
  const [treeSel, setTreeSel] = useState(2)
  const [transferSel, setTransferSel] = useState<Set<string>>(new Set(['release']))

  return (
    // min-h-screen + bg-background：壳外页面自带画布（AppShell 同款纪律
    // ——body 无背景，暗谱下白画布会打翻文字 token 对比度）
    <div data-testid="styleguide-root" className="mx-auto flex min-h-screen w-full max-w-content flex-col gap-8 bg-background px-6 py-8 text-foreground">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-h2 font-semibold">BinFlow Design System</h1>
          <p className="mt-1 text-dense text-muted-foreground">
            State matrix (design-system-plan §4.2) — light/dark × hover/active/disabled/loading/error. Dev-only route,
            not registered in production builds.
          </p>
        </div>
        <Button variant="outline" size="sm" data-testid="styleguide-theme-toggle" onClick={toggle}>
          {theme === 'light' ? <Moon /> : <Sun />}
          {theme === 'light' ? 'Dark theme' : 'Light theme'}
        </Button>
      </header>

      <Section id="button" title="Button" desc="6 variants × 3 sizes; loading = spinner + text kept + real disabled (aria-busy)">
        <Cell label="default">
          <Button>Deploy</Button>
        </Cell>
        <Cell label="secondary">
          <Button variant="secondary">Cancel</Button>
        </Cell>
        <Cell label="outline">
          <Button variant="outline">Filter</Button>
        </Cell>
        <Cell label="ghost">
          <Button variant="ghost">Clear</Button>
        </Cell>
        <Cell label="destructive">
          <Button variant="destructive">Delete</Button>
        </Cell>
        <Cell label="link">
          <Button variant="link">Details</Button>
        </Cell>
        <Cell label="sizes sm / lg">
          <Button size="sm">Small</Button>
          <Button size="lg">Large</Button>
        </Cell>
        <Cell label="loading (aria-busy)">
          <Button data-testid="sg-btn-loading" loading>
            Deploying…
          </Button>
        </Cell>
        <Cell label="loading destructive">
          <Button variant="destructive" loading>
            Deleting…
          </Button>
        </Cell>
        <Cell label="disabled">
          <Button disabled>Disabled</Button>
        </Cell>
      </Section>

      <Section id="spinner" title="Spinner" desc="Loading-state carrier shared by Button loading and inline busy slots">
        <Cell label="default">
          <Spinner />
        </Cell>
        <Cell label="inline with text">
          <span className="flex items-center gap-2 text-dense text-muted-foreground">
            <Spinner className="size-3.5" /> Loading…
          </span>
        </Cell>
      </Section>

      <Section id="input" title="Input" desc="h-8 dense; error = aria-invalid → danger border; affix = prefix/suffix slots">
        <Cell label="default">
          <Label htmlFor="sg-input" className="sr-only">
            Repository key
          </Label>
          <Input id="sg-input" placeholder="repo key" className="w-40" />
        </Cell>
        <Cell label="error (aria-invalid)">
          <Label htmlFor="sg-input-error" className="sr-only">
            Invalid input
          </Label>
          <Input
            id="sg-input-error"
            data-testid="sg-input-error"
            defaultValue="maven-hosted!"
            aria-invalid
            aria-describedby="sg-input-error-desc"
            className="w-40"
          />
          <span id="sg-input-error-desc" className="text-aux text-destructive">
            Lowercase letters, digits, hyphen only
          </span>
        </Cell>
        <Cell label="disabled">
          <Label htmlFor="sg-input-disabled" className="sr-only">
            Disabled input
          </Label>
          <Input id="sg-input-disabled" defaultValue="read only value" disabled className="w-40" />
        </Cell>
        <Cell label="affix (icon + mono unit)">
          <Label htmlFor="sg-input-affix" className="sr-only">
            Retention
          </Label>
          <Input
            id="sg-input-affix"
            data-testid="sg-input-affix"
            defaultValue="30"
            prefix={<Search />}
            suffix={<span className="font-mono">days</span>}
            className="w-44"
          />
        </Cell>
      </Section>

      <Section id="textarea" title="Textarea" desc="min-h-16; same error/disabled semantics as Input">
        <Cell label="default">
          <Textarea placeholder="description" aria-label="Description" className="w-52" />
        </Cell>
        <Cell label="error (aria-invalid)">
          <Textarea data-testid="sg-textarea-error" defaultValue="not valid json" aria-invalid aria-label="Invalid JSON" className="w-52" />
        </Cell>
        <Cell label="disabled">
          <Textarea defaultValue="locked content" aria-label="Locked content" disabled className="w-52" />
        </Cell>
      </Section>

      <Section id="checkbox" title="Checkbox" desc="Radix: space toggles, mixed state via checked='indeterminate'">
        <Cell label="unchecked / checked">
          <Checkbox id="sg-checkbox" data-testid="sg-checkbox" checked={checked} onCheckedChange={(v) => setChecked(v === true)} />
          <Label htmlFor="sg-checkbox">Controlled (checked={String(checked)})</Label>
        </Cell>
        <Cell label="indeterminate">
          <Checkbox defaultChecked="indeterminate" aria-label="partially selected" />
        </Cell>
        <Cell label="disabled (checked)">
          <Checkbox defaultChecked disabled aria-label="disabled checked" />
        </Cell>
        <Cell label="error (aria-invalid)">
          <Checkbox aria-invalid aria-label="must accept" />
        </Cell>
      </Section>

      <Section id="switch" title="Switch" desc="default h-4.5×w-8 / sm h-3.5×w-6; thumb slides on motion token">
        <Cell label="default on/off">
          <Switch id="sg-switch" data-testid="sg-switch" checked={switchOn} onCheckedChange={setSwitchOn} />
          <Label htmlFor="sg-switch">Mirror to remote</Label>
        </Cell>
        <Cell label="sm">
          <Switch size="sm" defaultChecked aria-label="compact switch" />
          <Switch size="sm" aria-label="compact switch off" />
        </Cell>
        <Cell label="disabled">
          <Switch defaultChecked disabled aria-label="disabled switch" />
        </Cell>
        <Cell label="error (aria-invalid)">
          <Switch aria-invalid aria-label="invalid switch" />
        </Cell>
      </Section>

      <Section id="radio-group" title="RadioGroup" desc="Radix roving focus; arrow keys move within group">
        <Cell label="group (2 options + 1 disabled)">
          <RadioGroup data-testid="sg-radio" value={radio} onValueChange={setRadio}>
            <div className="flex items-center gap-2">
              <RadioGroupItem value="a" id="sg-radio-a" />
              <Label htmlFor="sg-radio-a">Local</Label>
            </div>
            <div className="flex items-center gap-2">
              <RadioGroupItem value="b" id="sg-radio-b" />
              <Label htmlFor="sg-radio-b">Remote</Label>
            </div>
            <div className="flex items-center gap-2">
              <RadioGroupItem value="c" id="sg-radio-c" disabled />
              <Label htmlFor="sg-radio-c">Virtual (disabled)</Label>
            </div>
          </RadioGroup>
        </Cell>
      </Section>

      <Section id="badge" title="Badge" desc="Solid default/destructive; soft = *-surface slots + status text; dot variant">
        <Cell label="solid">
          <Badge>default</Badge>
          <Badge variant="destructive">danger</Badge>
        </Cell>
        <Cell label="soft (surface slots)">
          <Badge variant="success">success</Badge>
          <Badge variant="warning">warning</Badge>
          <Badge variant="info">info</Badge>
          <Badge variant="destructive-soft">danger</Badge>
        </Cell>
        <Cell label="dot">
          <Badge variant="success" dot>
            Healthy
          </Badge>
          <Badge variant="warning" dot>
            Degraded
          </Badge>
        </Cell>
        <Cell label="outline / secondary">
          <Badge variant="outline">outline</Badge>
          <Badge variant="secondary">secondary</Badge>
        </Cell>
      </Section>

      {/* 批 6 徽章两族收敛呈裁（T-UIB6 任务 5）：tint 族（批 5 旧配方等值
          复刻——color-mix 15%/18% 软底 + 78% 文字收敛，28 消费面在役）与
          soft 族（批 4 *-surface 语义槽）同屏并列，各带语义标签供 conductor
          封票终裁——本批不删任何一族（golden 翻新归 conductor）。 */}
      <Section
        id="badge-convergence"
        title="Badge — two-family convergence (ruling pending)"
        desc="RESOLVED (conductor, batch-6 seal): both families stay with a semantic split — tint = tier/semantic tag recipes (legacy replica, color-mix 15%/18% bg + 78% text convergence; 28 consumer files in service); soft = new neutral/status semantics on *-surface slots (batch 4). New badges pick by meaning, not by habit."
      >
        <Cell label="tint — legacy recipe (batch 5)">
          <Badge variant="tint-neutral" data-testid="sg-badge-tint-neutral">
            neutral
          </Badge>
          <Badge variant="tint-info" data-testid="sg-badge-tint-info">
            info
          </Badge>
          <Badge variant="tint-success" data-testid="sg-badge-tint-success">
            success
          </Badge>
          <Badge variant="tint-warning" data-testid="sg-badge-tint-warning">
            warning
          </Badge>
          <Badge variant="tint-danger" data-testid="sg-badge-tint-danger">
            danger
          </Badge>
          <Badge variant="tint-pro">pro</Badge>
          <Badge variant="tint-enterprise">enterprise</Badge>
        </Cell>
        <Cell label="soft — new semantic surface (batch 4)">
          <Badge variant="secondary">neutral</Badge>
          <Badge variant="info">info</Badge>
          <Badge variant="success">success</Badge>
          <Badge variant="warning">warning</Badge>
          <Badge variant="destructive-soft">danger</Badge>
        </Cell>
        <Cell label="same status, both families">
          <span className="flex items-center gap-1.5">
            <Badge variant="tint-success">healthy</Badge>
            <Badge variant="success">healthy</Badge>
          </span>
          <span className="flex items-center gap-1.5">
            <Badge variant="tint-warning">degraded</Badge>
            <Badge variant="warning">degraded</Badge>
          </span>
        </Cell>
      </Section>

      <Section id="toast" title="Toast" desc="Soft *-surface backgrounds + status text + left edge bar; theme follows the app theme">
        <Cell label="four severities">
          <Button variant="outline" size="sm" data-testid="sg-toast-success" onClick={() => toast.success('Artifact deployed')}>
            success
          </Button>
          <Button variant="outline" size="sm" data-testid="sg-toast-error" onClick={() => toast.error('Deploy failed: 500')}>
            error
          </Button>
          <Button variant="outline" size="sm" data-testid="sg-toast-warning" onClick={() => toast.warning('Quota at 90%')}>
            warning
          </Button>
          <Button variant="outline" size="sm" data-testid="sg-toast-info" onClick={() => toast.info('GC scheduled')}>
            info
          </Button>
        </Cell>
      </Section>

      <Section id="tabs" title="Tabs" desc="Active = accent underline (scale-in on motion token) + semibold; hover = surface-2; arrows move (Radix)">
        <Tabs data-testid="sg-tabs" defaultValue="general" className="w-full max-w-md">
          <TabsList>
            <TabsTrigger value="general">General</TabsTrigger>
            <TabsTrigger value="permissions">Permissions</TabsTrigger>
            <TabsTrigger value="advanced">Advanced</TabsTrigger>
            <TabsTrigger value="locked" disabled>
              Locked
            </TabsTrigger>
          </TabsList>
          <TabsContent value="general" className="pt-3 text-dense text-muted-foreground">
            General settings content
          </TabsContent>
          <TabsContent value="permissions" className="pt-3 text-dense text-muted-foreground">
            Permissions content
          </TabsContent>
          <TabsContent value="advanced" className="pt-3 text-dense text-muted-foreground">
            Advanced content
          </TabsContent>
        </Tabs>
      </Section>

      <Section id="breadcrumb" title="Breadcrumb" desc="Last segment solid, middle hover accent, overflow via ellipsis slot">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <a href="#breadcrumb">Application</a>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbEllipsis />
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <a href="#breadcrumb">libs-release</a>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>artifact-1.0.0.tgz</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
      </Section>

      <Section id="states" title="EmptyState / ErrorCard / Skeleton" desc="Unified four-state carriers (layout/states.tsx); anchors empty-state / error-card unchanged">
        <Cell label="EmptyState">
          <EmptyState
            className="w-64"
            testid="sg-empty"
            message="No artifacts yet"
            hint="Deploy your first artifact or create this repository from a template."
            illustration
            action={
              <Button size="sm" onClick={() => toast.info('Demo action')}>
                Deploy artifact
              </Button>
            }
          />
        </Cell>
        <Cell label="ErrorCard + retry">
          <ErrorCard
            className="w-72"
            error={{ status: 500, message: 'storage: context deadline exceeded while listing tree nodes' }}
            onRetry={() => setRetryCount((n) => n + 1)}
          />
          <span data-testid="sg-retry-count" className="text-aux text-muted-foreground">
            retries: {retryCount}
          </span>
        </Cell>
        <Cell label="Skeleton">
          <StateSkeleton lines={4} className="w-56" />
        </Cell>
      </Section>

      {/* ---- 批 6 域件行（T-UIB6 任务 1/任务 6）：ArtifactTree / ChecksumBlock
          / PathBreadcrumb / PropertiesTable 的皮肤配方陈列——demo 为配方
          等价复刻（真实件在 Explorer 页，虚拟化/懒加载语义不在本面）。 ---- */}
      <Section
        id="domain-tree"
        title="ArtifactTree rows"
        desc="hover=surface-2 / on-chain=surface-2 / selected=accent 2px left bar + bg-primary/10 + semibold; per-depth indent guides (border ticks); motion token row transition. Click a row to move selection."
      >
        <div
          data-testid="sg-tree-demo"
          role="tree"
          aria-label="ArtifactTree demo"
          className="w-72 rounded-md border border-border bg-surface-1 py-1"
        >
          <TreeDemoRow depth={0} selected={treeSel === 0} onSelect={() => setTreeSel(0)} icon="▣" testid="sg-tree-row-repo">
            libs-release
          </TreeDemoRow>
          <TreeDemoRow depth={1} onChain={treeSel >= 1} selected={treeSel === 1} onSelect={() => setTreeSel(1)} icon="▾◻" testid="sg-tree-row-dir">
            acme
          </TreeDemoRow>
          <TreeDemoRow depth={2} onChain={treeSel >= 2} selected={treeSel === 2} onSelect={() => setTreeSel(2)} icon="▾◻" testid="sg-tree-row-sub">
            platform
          </TreeDemoRow>
          <TreeDemoRow depth={2} selected={treeSel === 3} onSelect={() => setTreeSel(3)} icon="◾" testid="sg-tree-row-file">
            app-1.0.0.tgz
          </TreeDemoRow>
        </div>
      </Section>

      <Section
        id="checksum"
        title="ChecksumBlock"
        desc="mono + tabular-nums; long hashes truncated with click-to-reveal (copy stays full-value); missing algorithm renders an em-dash placeholder, never a forged value"
      >
        <div className="w-full max-w-md rounded-md border border-border bg-surface-1 p-3" data-testid="sg-checksum-demo">
          <ChecksumDemoRow algo="sha256" value="3f9a1c8e5b2d74f06a1c9e8b7d6f5a4e3c2b1a0987654321fedcba9876543210" />
          <ChecksumDemoRow algo="sha1" value="a94a8fe5ccb19ba61c4c0873d391e987982fbbd3" />
          <ChecksumDemoRow algo="md5" value={null} />
        </div>
      </Section>

      <Section
        id="path-breadcrumb"
        title="PathBreadcrumb"
        desc="Batch-4 Breadcrumb primitives + path separators ('/'); mono segments, last segment solid, middle hover accent; whole-path copy button"
      >
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <a href="#path-breadcrumb" className="font-mono" lang="en">
                  libs-release
                </a>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator className="font-mono">/</BreadcrumbSeparator>
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <a href="#path-breadcrumb" className="font-mono" lang="en">
                  acme
                </a>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator className="font-mono">/</BreadcrumbSeparator>
            <BreadcrumbItem>
              <BreadcrumbPage className="font-mono" lang="en">
                app-1.0.0.tgz
              </BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <CopyButton value="libs-release/acme/app-1.0.0.tgz" label="path" />
      </Section>

      <Section id="properties" title="PropertiesTable" desc="Key mono / values mono; row hover surface-2; empty state via the unified EmptyState carrier">
        <Table aria-label="Properties demo" className="w-full max-w-md border-collapse text-dense">
          <TableHeader>
            <TableRow className="border-b border-border text-left text-aux text-muted-foreground">
              <TableHead className="w-[34%] px-2 py-1.5 font-medium">Key</TableHead>
              <TableHead className="px-2 py-1.5 font-medium">Values</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow className="border-b border-border/60 transition-colors duration-fast ease-standard hover:bg-surface-2">
              <TableHead scope="row" className="px-2 py-1.5 text-left font-mono font-normal">
                license
              </TableHead>
              <TableCell className="px-2 py-1.5 font-mono">apache-2.0</TableCell>
            </TableRow>
            <TableRow className="border-b border-border/60 transition-colors duration-fast ease-standard hover:bg-surface-2">
              <TableHead scope="row" className="px-2 py-1.5 text-left font-mono font-normal">
                build.name
              </TableHead>
              <TableCell className="px-2 py-1.5 font-mono">ci-release</TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <EmptyState testid="sg-props-empty" className="w-64" message="No properties yet" hint="Add via the form above, or matrix params on deploy." illustration />
      </Section>

      {/* ---- 批 6 motion 消费面（T-UIB6 任务 3）：fade=fast(120ms) modal /
          pop=base(160ms) anchored / slide=slow(240ms) drawer+toast——时长
          直取 --bf-dur-*（prefers-reduced-motion 时 token 层整组降 1ms，
          e2e 以 computed animation/transition duration 断言）。 ---- */}
      <Section
        id="motion"
        title="Motion tokens"
        desc="fade (--bf-dur-fast, 120ms) modal; pop (--bf-dur-base, 160ms) anchored overlays; slide (--bf-dur-slow, 240ms) drawer + toast. Emulating prefers-reduced-motion collapses all three to 1ms."
      >
        <Cell label="dialog — fade fast">
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="outline" size="sm" data-testid="sg-motion-dialog-open">
                Open dialog
              </Button>
            </DialogTrigger>
            <DialogContent data-testid="sg-motion-dialog" className="max-w-sm">
              <DialogHeader>
                <DialogTitle>Delete artifact?</DialogTitle>
                <DialogDescription>Overlay and content fade with the fast motion token (120ms).</DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <DialogClose asChild>
                  <Button variant="outline" size="sm">
                    Close
                  </Button>
                </DialogClose>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </Cell>
        <Cell label="sheet right — slide slow">
          <Sheet>
            <SheetTrigger asChild>
              <Button variant="outline" size="sm" data-testid="sg-motion-sheet-open">
                Open sheet
              </Button>
            </SheetTrigger>
            <SheetContent data-testid="sg-motion-sheet">
              <SheetHeader>
                <SheetTitle>Detail panel</SheetTitle>
                <SheetDescription>Slides in over 240ms; the overlay fades fast.</SheetDescription>
              </SheetHeader>
            </SheetContent>
          </Sheet>
        </Cell>
        <Cell label="drawer bottom (vaul) — slide slow">
          <Drawer>
            <DrawerTrigger asChild>
              <Button variant="outline" size="sm" data-testid="sg-motion-drawer-open">
                Open drawer
              </Button>
            </DrawerTrigger>
            <DrawerContent data-testid="sg-motion-drawer">
              <DrawerHeader>
                <DrawerTitle>Bottom drawer</DrawerTitle>
                <DrawerDescription>vaul slide keyframes re-timed to the token (240ms).</DrawerDescription>
              </DrawerHeader>
            </DrawerContent>
          </Drawer>
        </Cell>
        <Cell label="popover — pop base">
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm" data-testid="sg-motion-popover-open">
                Open popover
              </Button>
            </PopoverTrigger>
            <PopoverContent data-testid="sg-motion-popover" className="w-56" align="start">
              <p className="text-dense text-muted-foreground">Anchored overlay, 160ms scale+fade.</p>
            </PopoverContent>
          </Popover>
        </Cell>
        <Cell label="toast — transition slow">
          <Button variant="outline" size="sm" data-testid="sg-motion-toast" onClick={() => toast.info('Motion token leg')}>
            Fire toast
          </Button>
        </Cell>
      </Section>

      {/* ---- 批 6 P2 件最小陈列（T-UIB6 任务 4）：Card/Separator/ScrollArea
          原语（token 基座已是成品，此面为矩阵登记）+ TransferBox 重皮后
          形态；AG Grid 主题是嵌入面皮肤（非交互原语），矩阵归 Explorer/
          Search e2e——不在 styleguide 拉起 grid 实例。 ---- */}
      <Section id="p2" title="P2 pieces" desc="Card / Separator / ScrollArea primitives (token base); TransferBox after the batch-6 reskin (legacy .transfer-* CSS retired into semantic classes)">
        <Cell label="card">
          <Card className="w-56">
            <CardHeader>
              <CardTitle>Storage</CardTitle>
              <CardDescription>Card primitive on token base</CardDescription>
            </CardHeader>
            <CardContent className="text-dense text-muted-foreground">Body slot</CardContent>
          </Card>
        </Cell>
        <Cell label="separator + scroll area">
          <div className="flex h-24 w-56 items-stretch gap-3">
            <ScrollArea className="w-40 rounded-md border border-border p-2">
              {Array.from({ length: 12 }, (_, i) => (
                <p key={i} className="text-dense text-muted-foreground">
                  line {i + 1}
                </p>
              ))}
            </ScrollArea>
            <Separator orientation="vertical" />
          </div>
        </Cell>
        <Cell label="transfer box (reskinned)">
          <TransferBox
            items={[
              { name: 'devs', label: 'devs', note: 'read' },
              { name: 'release', label: 'release' },
              { name: 'qa', label: 'qa' },
              { name: 'admins', label: 'admins' },
            ]}
            selected={[...transferSel]}
            onToggle={(name, next) =>
              setTransferSel((prev) => {
                const s = new Set(prev)
                if (next) s.add(name)
                else s.delete(name)
                return s
              })
            }
          />
        </Cell>
      </Section>

      <footer className="pb-8 text-aux text-muted-foreground">
        Styleguide route: /dev/styleguide — dev server and styleguide-mode builds only (see app/router gating).
      </footer>
    </div>
  )
}
