import { App, Button, Card, Empty, List, Popconfirm, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { DisconnectOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { del, get, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { TimeText } from '../../components/common'
import { SessionsTable, type MCPSession } from '../admin/Clients'
import { browserName, type WebSession } from '../admin/AdminSessions'

interface AppGrant { client_id: string; name: string; authorized_at: string; last_used_at?: string; active_tokens: number; remembered: boolean }

export default function MySessions() {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const sessions = useQuery({ queryKey: ['me', 'sessions'], queryFn: () => get<{ web: WebSession[]; mcp: MCPSession[] }>('/me/sessions') })
  const apps = useQuery({ queryKey: ['me', 'apps'], queryFn: () => get<{ items: AppGrant[] }>('/me/oauth-clients') })
  const act = useMutation({ mutationFn: (f: () => Promise<unknown>) => f(), onSuccess: () => { message.success('처리했습니다'); void qc.invalidateQueries({ queryKey: ['me'] }) }, onError: (e) => message.error(errorMessage(e)) })
  return (
    <>
      <PageHeader title="세션·연결된 앱" description="로그인한 브라우저, 연결된 MCP 클라이언트(OAuth), 진행 중인 MCP 세션을 확인하고 끊을 수 있습니다." />
      <Card title="연결된 앱 (OAuth)" style={{ marginBottom: 16 }}>
        {apps.data?.items.length ? (
          <List dataSource={apps.data.items} renderItem={(a) => (
            <List.Item actions={[<Popconfirm key="d" title={`${a.name} 연결을 끊을까요? 이 앱의 토큰이 즉시 폐기됩니다.`} onConfirm={() => act.mutate(() => del(`/me/oauth-clients/${a.client_id}`))}><Button danger size="small" icon={<DisconnectOutlined />}>연결 끊기</Button></Popconfirm>]}>
              <List.Item.Meta title={<Space>{a.name}{a.remembered && <Tag>동의 기억됨</Tag>}</Space>} description={<span style={{ fontSize: 12.5 }}>승인 <TimeText value={a.authorized_at} /> · 마지막 사용 <TimeText value={a.last_used_at} /> · 활성 토큰 {a.active_tokens}</span>} />
            </List.Item>
          )} />
        ) : <Empty description="OAuth로 연결된 앱이 없습니다" />}
      </Card>
      <Card title="브라우저 세션" style={{ marginBottom: 16 }}>
        <Table<WebSession> rowKey="id" size="small" dataSource={sessions.data?.web} pagination={false} scroll={{ x: 700 }} columns={[
          { title: '브라우저', dataIndex: 'user_agent', render: (v, s) => <Space><Tooltip title={v}>{browserName(v)}</Tooltip>{s.current && <Tag color="blue">현재 세션</Tag>}</Space> },
          { title: '방식', dataIndex: 'auth_method', width: 100, render: (v) => (v === 'keycloak' ? 'Keycloak' : '비밀번호') },
          { title: 'IP', dataIndex: 'ip', width: 130 },
          { title: '마지막 활동', dataIndex: 'last_seen_at', width: 110, render: (v) => <TimeText value={v} /> },
          { title: '', key: 'a', width: 80, render: (_, s) => !s.current && <Button size="small" onClick={() => act.mutate(() => del(`/me/sessions/${s.id}`))}>로그아웃</Button> },
        ]} />
      </Card>
      <Card title="MCP 세션">
        <Typography.Paragraph type="secondary" style={{ fontSize: 12.5 }}>AI 클라이언트가 initialize 할 때마다 생깁니다. 24시간 동안 활동이 없으면 자동 종료됩니다.</Typography.Paragraph>
        <SessionsTable data={sessions.data?.mcp} loading={sessions.isLoading} showUser={false} onClose={(id) => act.mutate(() => del(`/me/mcp-sessions/${id}`))} />
      </Card>
    </>
  )
}
