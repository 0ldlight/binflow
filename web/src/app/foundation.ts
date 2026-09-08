// 新基座 barrel（P1 冒烟载体）：一条 import 链拉通全部新基座模块——
// styles/Tailwind 层、ui primitives、app 骨架、lib 三件、stores 五仓。
// 供 typecheck/lint/构建冒烟验证可导入性；生产入口暂不消费（P2 新壳
// 挂载时按路由拆分引用，本文件不进主包模块图）。
import '@/styles/tw/tailwind.css'

// styles 层的自证（无 TS 导出面——css 经上面的 import 覆盖）

// ui primitives（shadcn 源码进仓第一批）
export { Badge, badgeVariants } from '@/components/ui/badge'
export { Button, ButtonAsChild, buttonVariants } from '@/components/ui/button'
export { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
export { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList, CommandSeparator } from '@/components/ui/command'
export { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from '@/components/ui/context-menu'
export { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog'
export { Drawer, DrawerContent, DrawerTitle, DrawerTrigger } from '@/components/ui/drawer'
export { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
export { Input } from '@/components/ui/input'
export { Label } from '@/components/ui/label'
export { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
export { ScrollArea } from '@/components/ui/scroll-area'
export { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
export { Separator } from '@/components/ui/separator'
export { Sheet, SheetContent, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
export { Skeleton } from '@/components/ui/skeleton'
export { Toaster, toast } from '@/components/ui/sonner'
export { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

// app 骨架
export { UI_BASE, STORAGE_KEYS, POLL_MS, PAGE_SIZE_OPTIONS, DEFAULT_PAGE_SIZE } from '@/app/config'
export { AppProviders, useConfirm } from '@/app/providers'
export { ThemeProvider, useTheme } from '@/app/providers'
export { QueryProvider } from '@/app/providers'
export { ToastProvider } from '@/app/providers'
export { appRoutes, createAppRouter } from '@/app/router'
export { AppShell, Sidebar, Topbar } from '@/app/shell'

// lib 三件（api/format 显式走子模块路径：旧 lib/api.ts / lib/format.ts 仍
// 服役且在 bundler 解析序里优先于同名目录的 index.ts——裸 '@/lib/api' 会
// 静默解析到旧文件，终验退役旧文件后方可收编短路径）
export { ApiError, apiJSON, apiText, errText, eventJSON, rawRequest, setUnauthorizedListener } from '@/lib/api/client'
export type { RequestOptions } from '@/lib/api/client'
export { API_ROOT, EVENT_ROOT, contentUrl } from '@/lib/api/roots'
export { createQueryClient, POLL_INTERVALS, qk } from '@/lib/query'
export { formatAuditTime } from '@/lib/format/datetime'
export { formatBytes, dedupRatio } from '@/lib/format/bytes'
export { formatCount } from '@/lib/format/number'
export { cn } from '@/lib/utils'

// stores 五仓
export { useUiStore } from '@/stores/ui-store'
export { useCommandPaletteStore } from '@/stores/command-palette-store'
export { useSessionStore } from '@/stores/session-store'
export type { AdminRole, SessionSnapshot } from '@/stores/session-store'
export { usePreferencesStore, PREF_KEYS } from '@/stores/preferences-store'
export { useAiStore } from '@/stores/ai-store'
