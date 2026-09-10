import { toast } from '@/lib/toast'

// toast 门面桥（FE-P4 MUI 清场）：旧 Snackbar/Alert 栈退役，本文件降为
// 「旧 useToast() API → sonner」的适配层——AuthContext / RepoDeleteConfirm /
// PropertiesTab / ReplicationsSection 等存量消费面的调用形态零改点。
//
// 语义对齐（console-ux §3.5 旧契约逐条）：
// - 右下角堆叠：Toaster（app/providers/toast-provider——position
//   bottom-right）已挂载，本层零 DOM；
// - 成功 5s 自动消失、错误常驻（duration Infinity）至手动关闭（Toaster
//   开 closeButton）；
// - 动作链接（action）：sonner 原生 action 槽（点击后自动消散）；
// - aria-live 播报：sonner ol[aria-live] 家族语义承载；
// - `toast` testid：经 lib/toast 门面统一携带（70 处 spec 消费面零迁移）。
//
// useToast 不再是 context 消费（sonner 是模块级单例）——保留函数形态，
// 存量调用点零改动；ToastProvider 保留为恒等组件（main.tsx 过渡期可摘）。

export interface ToastAction {
  label: string
  onClick: () => void
}

export interface ToastApi {
  success: (message: string, action?: ToastAction) => void
  error: (message: string, action?: ToastAction) => void
}

const SUCCESS_TTL_MS = 5000

const api: ToastApi = {
  success: (message, action) => {
    void toast.success(message, {
      duration: SUCCESS_TTL_MS,
      ...(action ? { action: { label: action.label, onClick: action.onClick } } : {}),
    })
  },
  error: (message, action) => {
    // 错误常驻（旧契约）：duration Infinity + Toaster closeButton 手动关
    void toast.error(message, {
      duration: Infinity,
      ...(action ? { action: { label: action.label, onClick: action.onClick } } : {}),
    })
  },
}

/** 旧 ToastContext.useToast 的恒等桥（sonner 单例——零 context） */
export function useToast(): ToastApi {
  return api
}

/** 过渡期恒等 Provider（P4 起 main.tsx 不再需要挂载） */
export function ToastProvider({ children }: { children: React.ReactNode }) {
  return <>{children}</>
}
