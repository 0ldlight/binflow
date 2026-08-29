import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'

// 危险确认对话框（console-ux §3.5）：居中 modal，焦点圈进对话框、Tab
// 循环、Esc 关闭（= 取消）。后续票的删除仓 / GC apply / 删 manifest 都
// 走这个基座（T-88 R6：共享组件由 T-98 沉淀）；确认按钮的前置条件
// （如输入 repo key）由调用方在 body 里自管。
//
// T-344 批 B：自有 modal 壳（.modal-backdrop + 手写焦点陷阱）→ MUI
// Dialog（mui-native-visual §4.2）。焦点陷阱 / Tab 循环 / Esc / backdrop
// 点击取消 / 滚动锁定全部交 MUI；paper 续挂 `modal` 类名（§3.8 fallback
// 选择器钩子——base.css 的 .modal 族规则为 pages 残面保留，此处泄漏项
// 由 sx 显式归零）。body 内的 `.confirm-input` 规格钩子类由调用方续挂
// （T-299 纪律），pages.css 的对应规则原样生效。

export interface ConfirmOptions {
  title: string
  body?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  /** danger 时确认按钮红变体 + modal 红边（P5 危险区语义） */
  danger?: boolean
  /**
   * 确认按钮的前置条件（P5：如「输入 repo key 才可用」，§3.5）。body 里
   * 的输入事件会让对话框重渲染并重新求值（T-99 加的缝：调用方把可变
   * 状态收在闭包/holder 里，onInput/onKeyDown 时在这里读）。可选、向后
   * 兼容——不传即恒可用。
   */
  confirmDisabled?: () => boolean
}

const ConfirmContext = createContext<(opts: ConfirmOptions) => Promise<boolean>>(
  async () => false,
)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null)
  const resolverRef = useRef<((v: boolean) => void) | null>(null)

  const confirm = useCallback((next: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      // 一次只允许一个对话框：后来的请求直接否决前一个（保守）。
      resolverRef.current?.(false)
      resolverRef.current = resolve
      setOpts(next)
    })
  }, [])

  const settle = useCallback((v: boolean) => {
    resolverRef.current?.(v)
    resolverRef.current = null
    setOpts(null)
  }, [])

  useEffect(() => () => resolverRef.current?.(false), [])

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {opts && (
        <ConfirmDialog key="confirm" opts={opts} onSettle={settle} />
      )}
    </ConfirmContext.Provider>
  )
}

export function useConfirm(): (opts: ConfirmOptions) => Promise<boolean> {
  return useContext(ConfirmContext)
}

function ConfirmDialog({
  opts,
  onSettle,
}: {
  opts: ConfirmOptions
  onSettle: (v: boolean) => void
}) {
  // body 内输入（输入 key 确认等前置）触发重渲染，让 confirmDisabled
  // 重新求值（T-99 缝：body 是静态 ReactNode，靠事件冒泡刷新按钮态）。
  const [, bump] = useState(0)

  // 打开即聚焦取消按钮（安全默认，§3.5）。MUI Modal 的内容是二段式提交
  // （首 pass 渲染空、Portal 内容随后续 commit 才进 DOM）——挂载期
  // useEffect 里 ref 尚为 null；用回调 ref + 微任务在按钮真实落 DOM 的
  // 那个 commit 后聚焦（disableAutoFocus 让开 FocusTrap 的 paper 首焦）。
  // ref 必须 useCallback 稳定引用（T-344E 修正）：每次渲染新建函数会让
  // React 重挂 ref（null → node），body 内输入每敲一个字符（bump 重渲染）
  // 就把焦点抢回取消钮——fill 型 spec 不可见（一次性 input 事件），type
  // 型键盘流实测炸（键入落进按钮）。
  const focusCancel = useCallback((node: HTMLButtonElement | null) => {
    if (node) queueMicrotask(() => node.focus())
  }, [])

  // Esc 兜底：MUI Modal 的 Esc 监听在 modal root 上（事件冒泡路径内才
  // 生效）——焦点掉到 body 时（如 body 内控件卸载）Esc 到不了 root。
  // 文档级监听补位：MUI 已处理的 Esc 会 stopPropagation，不会双触发。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.preventDefault()
      onSettle(false)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onSettle])

  // paper slotProps 以变量承载：对象字面量直挂会触发 data-* 的过剩属性
  // 检查（TS 只在 JSX 属性位放行连字符属性，嵌套字面量不放行）。
  const paperProps = {
    // §3.8 钩子：.modal 类名留 DOM（m8 artifacts-tree fallback 选择器）。
    // .modal 残留规则的泄漏项（padding/border/width）由 sx 归零/覆写。
    className: 'modal',
    'data-testid': 'confirm-dialog',
    onInput: () => bump((t) => t + 1),
    onClick: () => bump((t) => t + 1),
    sx: {
      p: 0,
      width: 'min(440px, calc(100vw - 48px))',
      ...(opts.danger ? { border: '1px solid', borderColor: 'error.light' } : { border: 'none' }),
    },
  }

  return (
    <Dialog
      open
      disableAutoFocus
      onClose={(_, reason) => {
        // Esc = 取消（§3.5/§8）；backdrop 点击同语义（旧壳行为原样）
        if (reason === 'escapeKeyDown' || reason === 'backdropClick') onSettle(false)
      }}
      aria-labelledby="confirm-dialog-title"
      slotProps={{ paper: paperProps }}
    >
      <DialogTitle id="confirm-dialog-title">{opts.title}</DialogTitle>
      {opts.body && <DialogContent>{opts.body}</DialogContent>}
      <DialogActions>
        <Button
          ref={focusCancel}
          variant="outlined"
          data-testid="confirm-cancel"
          onClick={() => onSettle(false)}
        >
          {opts.cancelLabel ?? '取消'}
        </Button>
        <Button
          variant="contained"
          color={opts.danger ? 'error' : 'primary'}
          size="medium"
          data-testid="confirm-accept"
          disabled={opts.confirmDisabled ? opts.confirmDisabled() : false}
          onClick={() => onSettle(true)}
        >
          {opts.confirmLabel ?? '确认'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
