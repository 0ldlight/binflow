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
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
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

export default function StyleguidePage() {
  const { theme, toggle } = useTheme()
  const [checked, setChecked] = useState(true)
  const [switchOn, setSwitchOn] = useState(true)
  const [radio, setRadio] = useState('a')
  const [retryCount, setRetryCount] = useState(0)

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

      <footer className="pb-8 text-aux text-muted-foreground">
        Styleguide route: /dev/styleguide — dev server and styleguide-mode builds only (see app/router gating).
      </footer>
    </div>
  )
}
