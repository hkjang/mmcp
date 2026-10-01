import { App, Button, Card, Popconfirm, Table, Tag, Tooltip } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { del, get, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { TimeText } from '../../components/common'

export interface WebSession { id: string; username: string; auth_method: string; ip: string; user_agent: string; created_at: string; last_seen_at: string; expires_at: string; current?: boolean }

export function browserName(ua: string) {
  const m = /(Edg|Chrome|Firefox|Safari|Whale)\/([\d.]+)/.exec(ua)
  const os = /Windows|Mac OS X|Linux|Android|iPhone/.exec(ua)?.[0] ?? ''
  return `${m ? (m[1] === 'Edg' ? 'Edge' : m[1]) : '알 수 없음'} · ${os.replace('Mac OS X', 'macOS')}`
}

export default function AdminSessions() {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const { data, isLoading } = useQuery({ queryKey: ['admin', 'sessions'], queryFn: () => get<{ items: WebSession[] }>('/admin/sessions') })
  const revoke = useMutation({ mutationFn: (id: string) => del(`/admin/sessions/${id}`), onSuccess: () => { message.success('세션을 종료했습니다'); void qc.invalidateQueries({ queryKey: ['admin', 'sessions'] }) }, onError: (e) => message.error(errorMessage(e)) })
  return (
    <>
      <PageHeader title="웹 세션" description="관리 콘솔·개인 페이지에 로그인한 브라우저 세션입니다. 의심스러운 세션은 즉시 종료할 수 있습니다." />
      <Card size="small" styles={{ body: { padding: 0 } }}>
        <Table<WebSession> rowKey="id" loading={isLoading} dataSource={data?.items} scroll={{ x: 860 }} pagination={{ pageSize: 20, hideOnSinglePage: true }} columns={[
          { title: '사용자', dataIndex: 'username', width: 120 },
          { title: '방식', dataIndex: 'auth_method', width: 110, render: (v) => <Tag color={v === 'keycloak' ? 'purple' : 'default'}>{v === 'keycloak' ? 'Keycloak' : '비밀번호'}</Tag> },
          { title: '브라우저', dataIndex: 'user_agent', render: (v) => <Tooltip title={v}>{browserName(v)}</Tooltip> },
          { title: 'IP', dataIndex: 'ip', width: 130 },
          { title: '로그인', dataIndex: 'created_at', width: 110, render: (v) => <TimeText value={v} /> },
          { title: '마지막 활동', dataIndex: 'last_seen_at', width: 110, render: (v) => <TimeText value={v} /> },
          { title: '만료', dataIndex: 'expires_at', width: 110, render: (v) => <TimeText value={v} /> },
          { title: '', key: 'a', width: 70, render: (_, s) => <Popconfirm title="이 세션을 종료할까요?" onConfirm={() => revoke.mutate(s.id)}><Button size="small" danger>종료</Button></Popconfirm> },
        ]} />
      </Card>
    </>
  )
}
