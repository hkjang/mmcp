import type { ReactNode } from 'react'
import { Button, Drawer, Grid, Typography, theme } from 'antd'
import { CloseOutlined } from '@ant-design/icons'

/**
 * Right-hand detail panel. On wide screens it sits beside the list, sticky
 * and independently scrollable; on narrow screens it becomes a drawer.
 * Use inside <SplitLayout>.
 */
export function DetailPanel({ open, title, extra, onClose, children, width = 440 }: { open: boolean; title: ReactNode; extra?: ReactNode; onClose: () => void; children: ReactNode; width?: number }) {
  const screens = Grid.useBreakpoint()
  const { token } = theme.useToken()
  if (!open) return null
  if (!screens.xl) {
    return (
      <Drawer open title={title} extra={extra} onClose={onClose} width={Math.min(width + 80, window.innerWidth)} destroyOnHidden styles={{ body: { paddingTop: 12 } }}>
        {children}
      </Drawer>
    )
  }
  return (
    <aside className="mmcp-detail-panel" style={{ width, background: token.colorBgContainer }} aria-label="상세 정보">
      <div className="mmcp-detail-panel-head" style={{ background: token.colorBgContainer }}>
        <Typography.Text strong ellipsis style={{ fontSize: 15 }}>
          {title}
        </Typography.Text>
        <span style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          {extra}
          <Button type="text" size="small" icon={<CloseOutlined />} onClick={onClose} aria-label="닫기" />
        </span>
      </div>
      <div className="mmcp-detail-panel-body">{children}</div>
    </aside>
  )
}

export function SplitLayout({ children, panel }: { children: ReactNode; panel: ReactNode }) {
  return (
    <div className="mmcp-split">
      <div className="mmcp-split-main">{children}</div>
      {panel}
    </div>
  )
}
