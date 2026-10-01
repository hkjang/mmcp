import { Button, Card, Empty, List, Popconfirm, Space, Tag, Typography } from 'antd'
import { CheckOutlined, CloseOutlined } from '@ant-design/icons'
import { StatusTag, TimeText } from './common'
import { fmtTime } from '../utils/format'

export interface Approval { id: string; username: string; tool: string; summary: Record<string, unknown>; status: string; created_at: string; decided_at?: string; expires_at: string; client: string }

function Preview({ s }: { s: Record<string, unknown> }) {
  const recips = Array.isArray(s.recipients) ? (s.recipients as { username: string; display_name: string }[]).map((r) => `${r.display_name}(@${r.username})`).join(', ') : ''
  return (
    <div style={{ fontSize: 13 }}>
      <div><b>{String(s.action ?? '')}</b>{s.send_as ? <Tag style={{ marginLeft: 6 }}>{s.send_as === 'bot' ? '봇 발신' : '본인 발신'}</Tag> : null}</div>
      {recips && <div>받는 사람: {recips}</div>}
      {s.channel ? <div>채널: {String(s.channel)}</div> : null}
      {s.filename ? <div>파일: {String(s.filename)} ({String(s.bytes ?? '')} bytes)</div> : null}
      {s.message ? <Typography.Paragraph style={{ margin: '6px 0 0', padding: '8px 10px', background: 'var(--mmcp-code-bg)', borderRadius: 6, whiteSpace: 'pre-wrap' }} ellipsis={{ rows: 4, expandable: true }}>{String(s.message)}</Typography.Paragraph> : null}
      {s.after ? <div>변경 후: {String(s.after)}</div> : null}
    </div>
  )
}

export function ApprovalList({ items, loading, showUser, onDecide }: { items?: Approval[]; loading?: boolean; showUser?: boolean; onDecide?: (id: string, d: 'approve' | 'reject') => void }) {
  return (
    <Card styles={{ body: { padding: items?.length ? 0 : 24 } }}>
      {!loading && !items?.length ? <Empty description="승인 요청이 없습니다" /> : (
        <List
          loading={loading}
          dataSource={items}
          renderItem={(a) => (
            <List.Item style={{ padding: '14px 20px', alignItems: 'flex-start' }} actions={a.status === 'pending' && onDecide ? [
              <Popconfirm key="r" title="이 요청을 거절할까요? AI가 같은 요청을 실행할 수 없게 됩니다." onConfirm={() => onDecide(a.id, 'reject')}><Button size="small" icon={<CloseOutlined />}>거절</Button></Popconfirm>,
              <Button key="a" size="small" type="primary" icon={<CheckOutlined />} onClick={() => onDecide(a.id, 'approve')}>미리 승인</Button>,
            ] : undefined}>
              <List.Item.Meta
                title={<Space wrap><span className="mmcp-tool-name">{a.tool}</span><StatusTag status={a.status} />{showUser && <Tag>{a.username}</Tag>}{a.client && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{a.client}</Typography.Text>}</Space>}
                description={<>
                  <Preview s={a.summary} />
                  <div style={{ fontSize: 12, marginTop: 6 }}>요청 <TimeText value={a.created_at} /> · 만료 {fmtTime(a.expires_at)}{a.decided_at && <> · 처리 <TimeText value={a.decided_at} /></>}</div>
                </>}
              />
            </List.Item>
          )}
        />
      )}
    </Card>
  )
}
