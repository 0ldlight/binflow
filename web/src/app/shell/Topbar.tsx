// 顶栏骨架（全局搜索/palette 入口/主题/语言/用户/AI 槽位——
// architecture §4）。P1 仅结构不消费：槽位为占位容器，交互 P2 接线。
import { Globe, Search, Sparkles, Sun, User } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'

export function Topbar() {
  return (
    <header
      data-slot="topbar"
      className="sticky top-0 z-[70] flex h-12 items-center gap-2 border-b border-border bg-surface-1 px-3"
    >
      <Button variant="ghost" size="sm" className="gap-2 text-muted-foreground" aria-label="Global search">
        <Search className="size-4" />
        <span className="hidden sm:inline">Search</span>
        <kbd className="rounded-sm border border-border bg-surface-2 px-1 text-aux">⌘K</kbd>
      </Button>
      <div className="flex-1" />
      <Button variant="ghost" size="icon" aria-label="Toggle theme">
        <Sun className="size-4" />
      </Button>
      <Button variant="ghost" size="icon" aria-label="Toggle locale">
        <Globe className="size-4" />
      </Button>
      <Separator orientation="vertical" className="h-5" />
      <Button variant="ghost" size="icon" aria-label="AI assistant">
        <Sparkles className="size-4" />
      </Button>
      <Button variant="ghost" size="icon" aria-label="Account menu">
        <User className="size-4" />
      </Button>
    </header>
  )
}
