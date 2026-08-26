import { useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'
import Stack from '@mui/material/Stack'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

import { useToast } from '../../app/ToastContext'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, deleteNodeProperties, errText, getNodeProperties, putNodeProperties } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'

// 制品详情 · Properties 页签（T-291，M10 FR-89 FE 腿——控制台首个 MUI 面）。
//
// 交互按 Artifactory Artifact Properties Tab 对齐（BOARD 2026-08-26 指令：
// 组件层换 MUI，交互逻辑不变）：
//   - key → 多值集合的表格，值以逗号分隔呈现/编辑（Artifactory 同款形态；
//     wire 形态 = §15.3.3 的 map<string,string[]>，如实逐值呈现，非拍平）
//   - 逐属性操作流：行内编辑（键不可改——PUT 按 key 合并，改键 = 删旧键
//     + 新增行两步，向用户如实呈现）+ 新增行（空行待填）+ 删除（轻交互，
//     无确认弹窗，与 Artifactory 一致）
//   - 保存语义可见性：PUT 是「该键值集整体替换、其他键保留」（§11.40 合并
//     律），与 Artifactory 逐属性 add/remove 效果面一致——页签内常驻说明行
//   - 校验与服务端同口径（internal/metadata/props.go 闭集）：键
//     [A-Za-z][A-Za-z0-9_.-]{0,63}；值非空/≤1024B/无控制字节/单键 ≤32 值/
//     无重复；节点 ≤64 键——非法即时反馈，保存钮禁用
//   - 权限姿态沿树页先例（W12d）：readonly_admin 预收敛禁用（canWrite=
//     false）；普通用户保留写入口，服务端 403 行内呈现（opError 信封文案）
// 四态：loading（Skeleton）/ 空（无属性引导）/ 错误（Alert + 重试）/ 数据。

/** 键闭集（与服务端 ValidatePropKey 同口径） */
const KEY_RE = /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/
// eslint-disable-next-line no-control-regex -- 与服务端 isPropControlByte 同口径（C0 + DEL）
const CTRL_RE = /[\x00-\x1f\x7f]/

const MAX_KEYS = 64
const MAX_VALUES = 32
const MAX_VALUE_BYTES = 1024

const MONO = { fontFamily: 'var(--bf-mono)' } as const

function validateKey(key: string): string | null {
  if (!KEY_RE.test(key)) return '键须匹配 [A-Za-z][A-Za-z0-9_.-]{0,63}（字母开头，≤64 字符）'
  return null
}

/** 逗号分隔的多值输入 → 值集合（即时校验，错误文案与服务端规则同源） */
function parseValues(text: string): { values: string[]; error: string | null } {
  const values = text
    .split(',')
    .map((v) => v.trim())
    .filter((v) => v !== '')
  if (values.length === 0) return { values, error: '至少一个非空值（服务端拒绝空值）' }
  if (values.length > MAX_VALUES) return { values, error: `单键最多 ${MAX_VALUES} 个值` }
  const seen = new Set<string>()
  for (const v of values) {
    if (v.length > MAX_VALUE_BYTES) return { values, error: `值超过 ${MAX_VALUE_BYTES} 字节：${v.slice(0, 24)}…` }
    if (CTRL_RE.test(v)) return { values, error: '值不能包含控制字符' }
    if (seen.has(v)) return { values, error: `重复值：${v}（存储模型是值集合）` }
    seen.add(v)
  }
  return { values, error: null }
}

/** 编辑器态：null = 浏览；new = 新增草稿行；edit = 既有键的值集编辑（键锁定） */
type Editor = { mode: 'new' } | { mode: 'edit'; key: string } | null

export default function PropertiesTab({
  repoKey,
  path,
  canWrite,
}: {
  repoKey: string
  path: string
  /** 写姿态（readonly_admin 预收敛禁用；普通用户 true，服务端 403 兜底） */
  canWrite: boolean
}) {
  const toast = useToast()
  const propsQ = useAsync(() => getNodeProperties(repoKey, path), [repoKey, path])
  const rows: [string, string[]][] =
    propsQ.status === 'ok'
      ? Object.entries(propsQ.data ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      : []

  const [editor, setEditor] = useState<Editor>(null)
  const [newKey, setNewKey] = useState('')
  const [valuesText, setValuesText] = useState('')
  const [opError, setOpError] = useState<{ status: number; message: string } | null>(null)
  const [busy, setBusy] = useState(false)

  const closeEditor = () => {
    setEditor(null)
    setNewKey('')
    setValuesText('')
  }

  // ---- 即时校验（保存钮禁用的依据） ----
  const trimmedKey = newKey.trim()
  const keyError =
    editor?.mode !== 'new'
      ? null
      : trimmedKey === ''
        ? null
        : validateKey(trimmedKey) ?? (rows.some(([k]) => k === trimmedKey) ? `键 ${trimmedKey} 已存在（PUT 会原地替换值集，请直接编辑该行）` : null)
  const keyCountError =
    editor?.mode === 'new' && rows.length >= MAX_KEYS ? `节点最多 ${MAX_KEYS} 个属性键` : null
  const { error: valuesError } = editor ? parseValues(valuesText) : { error: null }
  const newKeyMissing = editor?.mode === 'new' && trimmedKey === ''
  const invalid = !!keyError || !!keyCountError || !!valuesError || newKeyMissing

  // ---- 写操作（逐属性：PUT 单键 merge / DELETE 单键） ----
  const save = async () => {
    if (!editor || invalid || busy) return
    const key = editor.mode === 'new' ? trimmedKey : editor.key
    const { values } = parseValues(valuesText)
    setBusy(true)
    setOpError(null)
    try {
      // PUT 单键 = 该键值集整体替换、他键保留（§11.40）；与 Artifactory
      // 逐属性 add/remove 的效果面一致
      await putNodeProperties(repoKey, path, { [key]: values })
      toast.success(`已保存属性 ${key}`)
      closeEditor()
      propsQ.reload()
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0
      setOpError({ status, message: errText(err) })
    } finally {
      setBusy(false)
    }
  }

  const remove = async (key: string) => {
    setBusy(true)
    setOpError(null)
    try {
      await deleteNodeProperties(repoKey, path, [key])
      toast.success(`已删除属性 ${key}`)
      if (editor?.mode === 'edit' && editor.key === key) closeEditor()
      propsQ.reload()
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0
      setOpError({ status, message: errText(err) })
    } finally {
      setBusy(false)
    }
  }

  const onFieldKeys = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      void save()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      closeEditor()
    }
  }

  const readonlyTitle = '只读管理员不可写（服务端 403 兜底）'

  // ---- 四态 ----
  if (propsQ.status === 'loading') return <div data-testid="node-props"><Skeleton lines={3} /></div>
  if (propsQ.status === 'error' || propsQ.status === 'forbidden') {
    return (
      <div data-testid="node-props">
        <Alert
          severity="error"
          data-testid="node-props-error"
          action={
            <Button color="inherit" size="small" onClick={propsQ.reload}>
              重试
            </Button>
          }
        >
          属性加载失败（HTTP {propsQ.error?.status ?? 0}）——{propsQ.error?.message ?? '网络错误'}
        </Alert>
      </div>
    )
  }

  return (
    <div data-testid="node-props">
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 1 }}>
        <Typography variant="body2" color="text.secondary">
          属性 · {rows.length} 个键
        </Typography>
        <Tooltip
          title={
            !canWrite
              ? readonlyTitle
              : rows.length >= MAX_KEYS
                ? `节点最多 ${MAX_KEYS} 个属性键`
                : '新增一个属性（空行待填）'
          }
        >
          <span>
            <Button
              size="small"
              variant="outlined"
              data-testid="node-props-add"
              disabled={!canWrite || busy || rows.length >= MAX_KEYS || editor?.mode === 'new'}
              onClick={() => {
                setNewKey('')
                setValuesText('')
                setOpError(null)
                setEditor({ mode: 'new' })
              }}
            >
              + 新增属性
            </Button>
          </span>
        </Tooltip>
      </Stack>

      {rows.length === 0 && editor?.mode !== 'new' ? (
        <Box data-testid="node-props-empty" sx={{ py: 3, textAlign: 'center' }}>
          <Typography color="text.secondary">此节点尚无属性</Typography>
          <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 0.5 }}>
            部署时以矩阵参数（PUT …;key=value）附带，或在此新增；属性用于检索与治理。
          </Typography>
        </Box>
      ) : (
        <Table size="small" data-testid="node-props-table" aria-label="制品属性">
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: '34%' }}>键</TableCell>
              <TableCell>值（多值以逗号分隔）</TableCell>
              <TableCell sx={{ width: 96 }} align="right">
                操作
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map(([key, values]) =>
              editor?.mode === 'edit' && editor.key === key ? (
                <TableRow key={key} data-testid={`node-props-row-${key}`}>
                  <TableCell sx={MONO} component="th" scope="row">
                    {key}
                  </TableCell>
                  <TableCell>
                    <TextField
                      size="small"
                      fullWidth
                      autoFocus
                      margin="none"
                      placeholder="值（多值逗号分隔，如 v1, v2）"
                      value={valuesText}
                      onChange={(e) => setValuesText(e.target.value)}
                      onKeyDown={onFieldKeys}
                      error={!!valuesError}
                      helperText={valuesError ?? 'Enter 保存 · Esc 取消；保存 = 该键值集整体替换，其他属性保留'}
                      slotProps={{ htmlInput: { 'data-testid': `node-props-values-input-${key}`, spellCheck: false } }}
                    />
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    <Tooltip title="保存（PUT 单键替换）">
                      <span>
                        <IconButton
                          size="small"
                          aria-label={`保存属性 ${key}`}
                          data-testid="node-props-save"
                          disabled={!!valuesError || valuesText.trim() === '' || busy}
                          onClick={() => void save()}
                        >
                          <span aria-hidden="true">✓</span>
                        </IconButton>
                      </span>
                    </Tooltip>
                    <Tooltip title="取消编辑">
                      <IconButton
                        size="small"
                        aria-label={`取消编辑属性 ${key}`}
                        data-testid="node-props-cancel"
                        disabled={busy}
                        onClick={closeEditor}
                      >
                        <span aria-hidden="true">✕</span>
                      </IconButton>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ) : (
                <TableRow key={key} data-testid={`node-props-row-${key}`}>
                  <TableCell sx={MONO} component="th" scope="row">
                    {key}
                  </TableCell>
                  <TableCell sx={MONO}>{values.join(', ')}</TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    <Tooltip title={!canWrite ? readonlyTitle : '编辑该键的值集（键不可改——改键 = 删除后新增）'}>
                      <span>
                        <IconButton
                          size="small"
                          aria-label={`编辑属性 ${key}`}
                          data-testid={`node-props-edit-${key}`}
                          disabled={!canWrite || busy || editor?.mode === 'new'}
                          onClick={() => {
                            setNewKey('')
                            setValuesText(values.join(', '))
                            setOpError(null)
                            setEditor({ mode: 'edit', key })
                          }}
                        >
                          <span aria-hidden="true">✎</span>
                        </IconButton>
                      </span>
                    </Tooltip>
                    <Tooltip
                      title={
                        !canWrite
                          ? readonlyTitle
                          : '删除该属性（DELETE 单键，无确认——轻交互与 Artifactory 一致）'
                      }
                    >
                      <span>
                        <IconButton
                          size="small"
                          aria-label={`删除属性 ${key}`}
                          data-testid={`node-props-delete-${key}`}
                          disabled={!canWrite || busy}
                          onClick={() => void remove(key)}
                        >
                          <span aria-hidden="true">🗑</span>
                        </IconButton>
                      </span>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ),
            )}
            {editor?.mode === 'new' && (
              <TableRow data-testid="node-props-row-new">
                <TableCell>
                  <TextField
                    size="small"
                    fullWidth
                    autoFocus
                    margin="none"
                    placeholder="键（如 qa / build.number）"
                    value={newKey}
                    onChange={(e) => setNewKey(e.target.value)}
                    error={!!keyError || !!keyCountError}
                    helperText={keyError ?? keyCountError ?? undefined}
                    slotProps={{ htmlInput: { 'data-testid': `node-props-key-input`, spellCheck: false } }}
                  />
                </TableCell>
                <TableCell>
                  <TextField
                    size="small"
                    fullWidth
                    margin="none"
                    placeholder="值（多值逗号分隔，如 passed, rc1）"
                    value={valuesText}
                    onChange={(e) => setValuesText(e.target.value)}
                    onKeyDown={onFieldKeys}
                    error={!!valuesError}
                    helperText={valuesError ?? undefined}
                    slotProps={{
                      htmlInput: {
                        'data-testid': `node-props-values-input-${newKey.trim() || 'new'}`,
                        spellCheck: false,
                      },
                    }}
                  />
                </TableCell>
                <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                  <Tooltip title="保存（PUT 单键替换）">
                    <span>
                      <IconButton
                        size="small"
                        aria-label="保存新属性"
                        data-testid="node-props-save"
                        disabled={invalid || busy}
                        onClick={() => void save()}
                      >
                        <span aria-hidden="true">✓</span>
                      </IconButton>
                    </span>
                  </Tooltip>
                  <Tooltip title="取消新增">
                    <IconButton
                      size="small"
                      aria-label="取消新增属性"
                      data-testid="node-props-cancel"
                      disabled={busy}
                      onClick={closeEditor}
                    >
                      <span aria-hidden="true">✕</span>
                    </IconButton>
                  </Tooltip>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      )}

      {opError && (
        <Alert
          severity="error"
          sx={{ mt: 1 }}
          data-testid="node-props-error"
          onClose={() => setOpError(null)}
        >
          <div>
            属性写入失败（HTTP {opError.status}）：<span lang="en">{opError.message}</span>
          </div>
          {opError.status === 403 && (
            <div>当前会话没有该路径的写权限（write 动作）——权限按 permission target 的路径 pattern 授予，请联系管理员。</div>
          )}
        </Alert>
      )}

      <Typography variant="caption" color="text.secondary" display="block" sx={{ mt: 1 }}>
        属性逐行独立保存：保存 = PUT（该键值集整体替换，其他键保留）；删除 = DELETE 该键。与服务端规则同口径：键{' '}
        {'[A-Za-z][A-Za-z0-9_.-]{0,63}'}
        ，值 ≤1KiB、无控制字符，单键 ≤32 值，节点 ≤64 键。
      </Typography>
    </div>
  )
}
