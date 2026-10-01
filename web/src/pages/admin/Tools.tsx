import { useMemo, useState } from 'react'
import { App, Button, Card, Descriptions, Input, Segmented, Select, Space, Switch, Table, Tag, Tooltip, Typography, Alert } from 'antd'
import { ExperimentOutlined, LockOutlined, WarningOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { get, put, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { DetailPanel, SplitLayout } from '../../components/DetailPanel'
import { CodeBlock } from '../../components/common'
import { categoryLabel, fmtTime } from '../../utils/format'

export interface ToolSummary {
  name: string; title: string; description: string; category: string; read_only: boolean; destructive: boolean; sends: boolean; alias_of: string
  schema: Record<string, unknown>; enabled: boolean; mode: 'auto' | 'confirm'; roles: string[]; updated_at: string; updated_by: string
  default_enabled: boolean; default_mode: string; default_roles: string[]; blocked_reason: string; rate_bucket: string; calls_24h: number; errors_24h: number; avg_latency_ms: number
}

const catColor: Record<string, string> = { read: 'blue', search: 'geekblue', dm_read: 'cyan', dm_search: 'cyan', write: 'orange', dm_write: 'volcano', file: 'purple', channel_admin: 'magenta', destructive: 'red' }

export default function Tools() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [filter, setFilter] = useState<string>('all')
  const [q, setQ] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const { data, isLoading } = useQuery({ queryKey: ['admin', 'tools'], queryFn: () => get<{ items: ToolSummary[]; roles: string[] }>('/admin/tools') })
  const save = useMutation({
    mutationFn: (t: Pick<ToolSummary, 'name' | 'enabled' | 'mode' | 'roles'>) => put(`/admin/tools/${t.name}`, { enabled: t.enabled, mode: t.mode, roles: t.roles }),
    onSuccess: () => { message.success('도구 정책을 저장했습니다'); void qc.invalidateQueries({ queryKey: ['admin', 'tools'] }) },
    onError: (e) => message.error(errorMessage(e)),
  })
  const rows = useMemo(() => (data?.items ?? []).filter((t) => {
    if (q && !`${t.name} ${t.title} ${t.description}`.toLowerCase().includes(q.toLowerCase())) return false
    if (filter === 'read') return t.read_only
    if (filter === 'write') return !t.read_only
    if (filter === 'dm') return t.category.startsWith('dm_')
    if (filter === 'alias') return !!t.alias_of
    if (filter === 'all') return !t.alias_of
    return true
  }), [data, filter, q])
  const sel = data?.items.find((t) => t.name === selected)
  const roleOpts = (data?.roles ?? []).map((r) => ({ value: r, label: r }))
  return (
    <>
      <PageHeader title="도구 정책" description="MCP로 노출되는 Mattermost 도구별로 활성화, 실행 방식(즉시/승인), 필요 역할을 정합니다. 읽기 도구는 즉시, 쓰기 도구는 승인 기반이 기본입니다." />
      <SplitLayout panel={sel && (
        <DetailPanel open title={<span className="mmcp-tool-name">{sel.name}</span>} onClose={() => setSelected(null)}
          extra={<Button size="small" icon={<ExperimentOutlined />} onClick={() => navigate(`/me/playground?tool=${sel.alias_of || sel.name}`)}>실행해 보기</Button>}>
          <Typography.Title level={5} style={{ marginTop: 0 }}>{sel.title}</Typography.Title>
          <Typography.Paragraph type="secondary">{sel.description}</Typography.Paragraph>
          {sel.blocked_reason && <Alert type="warning" showIcon title={sel.blocked_reason} style={{ marginBottom: 12 }} />}
          <Descriptions size="small" column={1} bordered items={[
{ label: "분류", children: <><Tag color={catColor[sel.category]}>{categoryLabel[sel.category]}</Tag></> },
{ label: "MCP 힌트", children: <>{sel.read_only ? 'readOnlyHint' : 'write'}{sel.destructive && ' · destructiveHint'}</> },
{ label: "호출 한도 버킷", children: <>{sel.rate_bucket}</> },
{ label: "24시간", children: <>{sel.calls_24h}건 성공 · {sel.errors_24h}건 실패 · 평균 {sel.avg_latency_ms}ms</> },
{ label: "기본값", children: <>{sel.default_enabled ? '활성' : '비활성'} · {sel.default_mode} · {sel.default_roles.join(', ')}</> },
{ label: "최근 변경", children: <>{sel.updated_by ? `${sel.updated_by} · ${fmtTime(sel.updated_at)}` : '-'}</> },
]} />
          <Typography.Paragraph strong style={{ marginTop: 16, marginBottom: 6 }}>입력 스키마</Typography.Paragraph>
          <CodeBlock code={JSON.stringify(sel.schema, null, 2)} maxHeight={360} />
        </DetailPanel>
      )}>
        <Card size="small" style={{ marginBottom: 12 }}>
          <Space wrap>
            <Segmented value={filter} onChange={(v) => setFilter(String(v))} options={[{ label: '전체', value: 'all' }, { label: '읽기', value: 'read' }, { label: '쓰기', value: 'write' }, { label: 'DM', value: 'dm' }, { label: '호환 별칭', value: 'alias' }]} />
            <Input.Search allowClear placeholder="도구 검색" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 220 }} />
          </Space>
        </Card>
        <Card size="small" styles={{ body: { padding: 0 } }}>
          <Table<ToolSummary>
            rowKey="name"
            loading={isLoading}
            dataSource={rows}
            pagination={false}
            scroll={{ x: 980 }}
            onRow={(r) => ({ onClick: (e) => { if (!(e.target as HTMLElement).closest('.ant-switch,.ant-select')) setSelected(r.name) }, style: { cursor: 'pointer', background: selected === r.name ? 'rgba(37,99,235,.06)' : undefined } })}
            columns={[
              { title: '도구', key: 'n', render: (_, t) => <div><span className="mmcp-tool-name">{t.name}</span> {t.destructive && <Tooltip title="파괴적 작업"><WarningOutlined style={{ color: '#dc2626' }} /></Tooltip>}<div style={{ fontSize: 12, opacity: 0.65 }}>{t.title}{t.alias_of && ` → ${t.alias_of}`}</div></div> },
              { title: '분류', dataIndex: 'category', width: 100, render: (v) => <Tag color={catColor[v]}>{categoryLabel[v]}</Tag> },
              {
                title: '활성', key: 'e', width: 80, render: (_, t) => t.alias_of ? <Typography.Text type="secondary">원본 따름</Typography.Text> : (
                  <Switch size="small" checked={t.enabled} aria-label={`${t.name} 활성화`} onChange={(v) => save.mutate({ name: t.name, enabled: v, mode: t.mode, roles: t.roles })} />
                ),
              },
              {
                title: '실행 방식', key: 'm', width: 130, render: (_, t) => t.read_only || t.alias_of ? <Tag>{t.read_only ? '즉시' : t.mode === 'confirm' ? '승인 필요' : '즉시'}</Tag> : (
                  <Select size="small" value={t.mode} style={{ width: 110 }} aria-label={`${t.name} 실행 방식`} onChange={(v) => save.mutate({ name: t.name, enabled: t.enabled, mode: v, roles: t.roles })}
                    options={[{ value: 'confirm', label: <span><LockOutlined /> 승인 필요</span> }, { value: 'auto', label: '즉시 실행' }]} />
                ),
              },
              {
                title: '필요 역할 (하나 이상)', key: 'r', render: (_, t) => t.alias_of ? t.roles.map((r) => <Tag key={r}>{r}</Tag>) : (
                  <Select size="small" mode="multiple" value={t.roles} style={{ minWidth: 200, width: '100%' }} options={roleOpts} aria-label={`${t.name} 필요 역할`}
                    onChange={(v) => save.mutate({ name: t.name, enabled: t.enabled, mode: t.mode, roles: v })} maxTagCount="responsive" />
                ),
              },
              { title: '24시간', key: 'c', width: 110, render: (_, t) => <span style={{ fontSize: 12.5 }}>{t.calls_24h}{t.errors_24h > 0 && <Typography.Text type="danger"> / {t.errors_24h} 실패</Typography.Text>}</span> },
              { title: '', key: 'b', width: 30, render: (_, t) => t.blocked_reason && <Tooltip title={t.blocked_reason}><WarningOutlined style={{ color: '#f59e0b' }} /></Tooltip> },
            ]}
          />
        </Card>
      </SplitLayout>
    </>
  )
}
