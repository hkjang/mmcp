import { useMemo, useState } from 'react'
import { App, Button, Card, Input, Popconfirm, Segmented, Space, Table, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { del, get, errorMessage } from '../../api/client'
import type { ApiKey } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { Mono, StatusTag, TimeText } from '../../components/common'
import { fmtDate } from '../../utils/format'

export default function AdminApiKeys() {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const [filter, setFilter] = useState('active')
  const [q, setQ] = useState('')
  const { data, isLoading } = useQuery({ queryKey: ['admin', 'api-keys'], queryFn: () => get<{ items: ApiKey[] }>('/admin/api-keys') })
  const revoke = useMutation({ mutationFn: (id: string) => del(`/admin/api-keys/${id}`), onSuccess: () => { message.success('폐기했습니다'); void qc.invalidateQueries({ queryKey: ['admin', 'api-keys'] }) }, onError: (e) => message.error(errorMessage(e)) })
  const rows = useMemo(() => (data?.items ?? []).filter((k) => (filter === 'all' || (filter === 'due' ? k.rotation_due : k.status === filter)) && (!q || `${k.username} ${k.name} ${k.prefix}`.toLowerCase().includes(q.toLowerCase()))), [data, filter, q])
  return (
    <>
      <PageHeader title="API 키 현황" description="사용자가 발급한 MCP 개인 API 키(mmcp_pk_…) 전체입니다. 비밀값은 해시로만 저장되어 관리자도 볼 수 없습니다." />
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <Segmented value={filter} onChange={(v) => setFilter(String(v))} options={[{ label: '활성', value: 'active' }, { label: '회전 필요', value: 'due' }, { label: '만료', value: 'expired' }, { label: '폐기', value: 'revoked' }, { label: '전체', value: 'all' }]} />
          <Input.Search allowClear placeholder="사용자, 키 이름, 접두어" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 240 }} />
        </Space>
      </Card>
      <Card size="small" styles={{ body: { padding: 0 } }}>
        <Table<ApiKey> rowKey="id" loading={isLoading} dataSource={rows} scroll={{ x: 980 }} pagination={{ pageSize: 20, showTotal: (t) => `총 ${t}개` }} columns={[
          { title: '사용자', dataIndex: 'username', width: 120 },
          { title: '이름', dataIndex: 'name', render: (v, k) => <span>{v} {k.read_only && <Tag>읽기 전용</Tag>} {k.rotated_from && <Tag color="blue">회전됨</Tag>}</span> },
          { title: '접두어', dataIndex: 'prefix', width: 150, render: (v) => <Mono>{v}…</Mono> },
          { title: '상태', key: 's', width: 130, render: (_, k) => <Space size={4}><StatusTag status={k.status} />{k.rotation_due && <Tag color="orange">회전 필요</Tag>}</Space> },
          { title: '만료', dataIndex: 'expires_at', width: 110, render: (v) => (v ? fmtDate(v) : '없음') },
          { title: '마지막 사용', key: 'l', width: 150, render: (_, k) => <span><TimeText value={k.last_used_at} />{k.last_used_ip && <div style={{ fontSize: 11, opacity: 0.6 }}>{k.last_used_ip}</div>}</span> },
          { title: '생성', dataIndex: 'created_at', width: 100, render: (v) => <TimeText value={v} /> },
          { title: '', key: 'a', width: 70, render: (_, k) => k.status === 'active' && <Popconfirm title="이 키를 즉시 폐기할까요?" okButtonProps={{ danger: true }} onConfirm={() => revoke.mutate(k.id)}><Button size="small" danger>폐기</Button></Popconfirm> },
        ]} />
      </Card>
    </>
  )
}
