import { useState } from 'react'
import { App, Button, Card, Popconfirm, Segmented, Space, Switch, Table, Tabs, Tag, Typography } from 'antd'
import { DeleteOutlined, StopOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { del, get, put, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { Mono, TimeText } from '../../components/common'

interface OAuthClient { client_id: string; name: string; redirect_uris: string[]; registration: string; software_id: string; created_at: string; last_used_at?: string; disabled: boolean; active_users: number; active_tokens: number }
export interface MCPSession { id: string; username: string; auth_method: string; client_name: string; client_version: string; protocol_version: string; ip: string; created_at: string; last_seen_at: string; calls: number; closed_at?: string; active: boolean }

const authLabel: Record<string, string> = { oauth: 'OAuth', api_key: 'API 키', keycloak_jwt: 'Keycloak JWT', console: '콘솔' }

export function SessionsTable({ data, loading, onClose, showUser = true }: { data?: MCPSession[]; loading?: boolean; onClose: (id: string) => void; showUser?: boolean }) {
  return (
    <Table<MCPSession>
      rowKey="id" size="small" loading={loading} dataSource={data} scroll={{ x: 900 }} pagination={{ pageSize: 20, hideOnSinglePage: true }}
      columns={[
        ...(showUser ? [{ title: '사용자', dataIndex: 'username', width: 110 }] : []),
        { title: '클라이언트', key: 'c', render: (_: unknown, s: MCPSession) => <span>{s.client_name || '-'} <Typography.Text type="secondary" style={{ fontSize: 12 }}>{s.client_version}</Typography.Text></span> },
        { title: '인증', dataIndex: 'auth_method', width: 110, render: (v: string) => <Tag>{authLabel[v] ?? v}</Tag> },
        { title: '프로토콜', dataIndex: 'protocol_version', width: 110 },
        { title: 'IP', dataIndex: 'ip', width: 120 },
        { title: '호출', dataIndex: 'calls', width: 70 },
        { title: '마지막 활동', dataIndex: 'last_seen_at', width: 110, render: (v: string) => <TimeText value={v} /> },
        { title: '상태', key: 's', width: 90, render: (_: unknown, s: MCPSession) => s.closed_at ? <Tag>종료</Tag> : s.active ? <Tag color="green">활성</Tag> : <Tag>유휴</Tag> },
        { title: '', key: 'a', width: 60, render: (_: unknown, s: MCPSession) => !s.closed_at && <Popconfirm title="세션을 종료할까요? 클라이언트는 다시 initialize 해야 합니다." onConfirm={() => onClose(s.id)}><Button size="small" type="text" icon={<StopOutlined />} aria-label="세션 종료" /></Popconfirm> },
      ]}
    />
  )
}

export default function Clients() {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const [activeOnly, setActiveOnly] = useState('active')
  const clients = useQuery({ queryKey: ['admin', 'oauth-clients'], queryFn: () => get<{ items: OAuthClient[] }>('/admin/oauth-clients') })
  const sessions = useQuery({ queryKey: ['admin', 'mcp-sessions', activeOnly], queryFn: () => get<{ items: MCPSession[] }>('/admin/mcp-sessions', { active: activeOnly === 'active' ? 1 : undefined }) })
  const act = useMutation({
    mutationFn: (f: () => Promise<unknown>) => f(),
    onSuccess: () => { message.success('처리했습니다'); void qc.invalidateQueries({ queryKey: ['admin', 'oauth-clients'] }); void qc.invalidateQueries({ queryKey: ['admin', 'mcp-sessions'] }) },
    onError: (e) => message.error(errorMessage(e)),
  })
  return (
    <>
      <PageHeader title="클라이언트·세션" description="동적 등록된 MCP OAuth 클라이언트(Claude Code, Codex, Cursor 등)와 MCP 세션을 확인하고 차단합니다." />
      <Card>
        <Tabs items={[
          {
            key: 'clients', label: `OAuth 클라이언트 (${clients.data?.items.length ?? 0})`,
            children: (
              <Table<OAuthClient>
                rowKey="client_id" size="small" loading={clients.isLoading} dataSource={clients.data?.items} scroll={{ x: 900 }}
                expandable={{ expandedRowRender: (c) => <div style={{ fontSize: 12.5 }}><div>client_id: <Mono>{c.client_id}</Mono></div><div>redirect_uris: {c.redirect_uris.map((u) => <Tag key={u}>{u}</Tag>)}</div>{c.software_id && <div>software_id: {c.software_id}</div>}</div> }}
                columns={[
                  { title: '이름', dataIndex: 'name', render: (v, c) => <span>{v} {c.registration === 'dynamic' && <Tag>DCR</Tag>}</span> },
                  { title: '사용자', dataIndex: 'active_users', width: 80 },
                  { title: '활성 토큰', dataIndex: 'active_tokens', width: 90 },
                  { title: '등록', dataIndex: 'created_at', width: 110, render: (v) => <TimeText value={v} /> },
                  { title: '마지막 사용', dataIndex: 'last_used_at', width: 110, render: (v) => <TimeText value={v} /> },
                  { title: '허용', key: 'd', width: 70, render: (_, c) => <Switch size="small" checked={!c.disabled} aria-label="클라이언트 허용" onChange={(on) => act.mutate(() => put(`/admin/oauth-clients/${c.client_id}`, { disabled: !on }))} /> },
                  { title: '', key: 'x', width: 50, render: (_, c) => <Popconfirm title="클라이언트와 발급된 토큰을 삭제할까요?" onConfirm={() => act.mutate(() => del(`/admin/oauth-clients/${c.client_id}`))}><Button size="small" type="text" danger icon={<DeleteOutlined />} aria-label="삭제" /></Popconfirm> },
                ]}
              />
            ),
          },
          {
            key: 'sessions', label: 'MCP 세션',
            children: (
              <Space orientation="vertical" style={{ width: '100%' }}>
                <Segmented value={activeOnly} onChange={(v) => setActiveOnly(String(v))} options={[{ label: '최근 24시간 활성', value: 'active' }, { label: '전체', value: 'all' }]} />
                <SessionsTable data={sessions.data?.items} loading={sessions.isLoading} onClose={(id) => act.mutate(() => del(`/admin/mcp-sessions/${id}`))} />
              </Space>
            ),
          },
        ]} />
      </Card>
    </>
  )
}
