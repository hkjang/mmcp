import { useState, type ReactNode } from 'react'
import { Alert, App, Button, Modal, Space, Tag, Tooltip, Typography } from 'antd'
import { CheckOutlined, CopyOutlined, WarningOutlined } from '@ant-design/icons'
import { copyText, fromNow, fmtTime, statusColor, statusLabel } from '../utils/format'

export function StatusTag({ status, label }: { status: string; label?: string }) {
  return <Tag color={statusColor[status] ?? 'default'}>{label ?? statusLabel[status] ?? status}</Tag>
}

export function TimeText({ value }: { value?: string | null }) {
  if (!value) return <Typography.Text type="secondary">-</Typography.Text>
  return (
    <Tooltip title={fmtTime(value)}>
      <span>{fromNow(value)}</span>
    </Tooltip>
  )
}

export function CopyButton({ text, label, size = 'small' }: { text: string; label?: string; size?: 'small' | 'middle' }) {
  const [done, setDone] = useState(false)
  const { message } = App.useApp()
  return (
    <Button
      size={size}
      icon={done ? <CheckOutlined /> : <CopyOutlined />}
      onClick={async () => {
        await copyText(text)
        setDone(true)
        message.success('복사했습니다')
        setTimeout(() => setDone(false), 1500)
      }}
    >
      {label}
    </Button>
  )
}

export function CodeBlock({ code, wrap, maxHeight, copy = true }: { code: string; wrap?: boolean; maxHeight?: number; copy?: boolean }) {
  return (
    <div style={{ position: 'relative' }}>
      <pre className={`mmcp-code${wrap ? ' wrap' : ''}`} style={maxHeight ? { maxHeight } : undefined}>
        {code}
      </pre>
      {copy && (
        <div style={{ position: 'absolute', top: 8, right: 8 }}>
          <CopyButton text={code} />
        </div>
      )}
    </div>
  )
}

/** Shows a secret exactly once with copy and a clear warning. */
export function SecretModal({ open, title, secret, onClose, children }: { open: boolean; title: string; secret: string; onClose: () => void; children?: ReactNode }) {
  return (
    <Modal open={open} title={title} onCancel={onClose} maskClosable={false} footer={<Button type="primary" onClick={onClose}>안전하게 보관했습니다</Button>} width={600}>
      <Alert type="warning" showIcon icon={<WarningOutlined />} title="이 값은 지금 한 번만 표시됩니다. 창을 닫으면 다시 볼 수 없습니다." style={{ marginBottom: 12 }} />
      <Space.Compact style={{ width: '100%' }}>
        <Typography.Text code copyable={false} style={{ flex: 1, padding: '6px 10px', wordBreak: 'break-all', userSelect: 'all', margin: 0 }} data-testid="secret-value">
          {secret}
        </Typography.Text>
        <CopyButton text={secret} label="복사" size="middle" />
      </Space.Compact>
      {children && <div style={{ marginTop: 16 }}>{children}</div>}
    </Modal>
  )
}

export function KV({ items }: { items: [ReactNode, ReactNode][] }) {
  return (
    <dl className="mmcp-kv">
      {items.map(([k, v], i) => (
        <div key={i} style={{ display: 'contents' }}>
          <dt>{k}</dt>
          <dd>{v ?? '-'}</dd>
        </div>
      ))}
    </dl>
  )
}

export function Mono({ children }: { children: ReactNode }) {
  return <span className="mono" style={{ fontSize: 12.5 }}>{children}</span>
}
