import { useState } from 'react'
import { Button, Card, DatePicker, Input, Select, Space, Table, Tag, Typography } from 'antd'
import { DownloadOutlined, ReloadOutlined, FilterOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import dayjs, { type Dayjs } from 'dayjs'
import { buildQuery, get } from '../api/client'
import type { AuditEvent, Paged } from '../api/types'
import { DetailPanel, SplitLayout } from './DetailPanel'
import { CodeBlock, CopyButton, KV, Mono, StatusTag, TimeText } from './common'
import { categoryLabel, fmtTime } from '../utils/format'

export function AuditView({ endpoint, admin }: { endpoint: string; admin?: boolean }) {
  const [params, setParams] = useSearchParams()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(30)
  const [sel, setSel] = useState<AuditEvent | null>(null)
  const [q, setQ] = useState(params.get('q') ?? '')
  const filters = {
    category: params.get('category') ?? '', result: params.get('result') ?? '', action: params.get('action') ?? '',
    actor: params.get('actor') ?? '', q: params.get('q') ?? '', from: params.get('from') ?? '', to: params.get('to') ?? '',
  }
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['audit', endpoint, filters, page, pageSize],
    queryFn: () => get<Paged<AuditEvent>>(endpoint, { ...filters, page, page_size: pageSize }),
    placeholderData: (p) => p,
  })
  const set = (k: string, v?: string | null) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
    setPage(1)
  }
  const range: [Dayjs, Dayjs] | null = filters.from && filters.to ? [dayjs(filters.from), dayjs(filters.to)] : null
  return (
    <SplitLayout panel={sel && (
      <DetailPanel open title={<span><span className="mmcp-tool-name">{sel.action}</span></span>} onClose={() => setSel(null)} extra={<StatusTag status={sel.result} />}>
        <KV items={[
          ['시각', fmtTime(sel.ts)],
          ['분류', categoryLabel[sel.category] ?? sel.category],
          ['사용자', sel.actor || '-'],
          ['Mattermost', sel.mm_username ? `@${sel.mm_username}` : '-'],
          ['대상', sel.target ? <Mono>{sel.target}</Mono> : '-'],
          ['오류 코드', sel.error_code ? <Tag color="red">{sel.error_code}</Tag> : '-'],
          ['지연', `${sel.latency_ms}ms`],
          ['클라이언트', sel.client || '-'],
          ['IP', sel.ip || '-'],
          ['요청 ID', sel.request_id ? <Space size={4}><Mono>{sel.request_id}</Mono><CopyButton text={sel.request_id} /></Space> : '-'],
        ]} />
        <Space style={{ margin: '12px 0' }} wrap>
          {sel.request_id && <Button size="small" onClick={() => { setQ(sel.request_id); set('q', sel.request_id) }}>같은 요청 보기</Button>}
          {admin && sel.actor && <Button size="small" onClick={() => set('actor', sel.actor)}>이 사용자 활동</Button>}
          {sel.category === 'tool' && <Button size="small" onClick={() => set('action', sel.action)}>같은 도구</Button>}
        </Space>
        <Typography.Paragraph strong style={{ marginBottom: 6 }}>상세</Typography.Paragraph>
        <CodeBlock code={JSON.stringify(sel.details, null, 2)} wrap maxHeight={480} />
      </DetailPanel>
    )}>
      <Card size="small" style={{ marginBottom: 12 }}>
        <Space wrap>
          <Input.Search allowClear placeholder="작업, 대상, 오류 코드, 요청 ID" value={q} onChange={(e) => setQ(e.target.value)} onSearch={(v) => set('q', v)} style={{ width: 260 }} />
          <Select allowClear placeholder="분류" style={{ width: 120 }} value={filters.category || undefined} onChange={(v) => set('category', v)}
            options={['tool', 'auth', 'oauth', 'key', 'mapping', ...(admin ? ['admin'] : [])].map((c) => ({ value: c, label: categoryLabel[c] }))} />
          <Select allowClear placeholder="결과" style={{ width: 110 }} value={filters.result || undefined} onChange={(v) => set('result', v)}
            options={['success', 'failure', 'denied', 'pending'].map((c) => ({ value: c, label: <StatusTag status={c} /> }))} />
          {admin && <Input allowClear placeholder="사용자" style={{ width: 130 }} value={filters.actor} onChange={(e) => set('actor', e.target.value)} prefix={<FilterOutlined />} />}
          <DatePicker.RangePicker showTime={{ format: 'HH:mm' }} format="MM-DD HH:mm" value={range} onChange={(v) => {
            const n = new URLSearchParams(params)
            if (v?.[0] && v[1]) { n.set('from', v[0].toISOString()); n.set('to', v[1].toISOString()) } else { n.delete('from'); n.delete('to') }
            setParams(n, { replace: true })
          }} />
          {filters.action && <Tag closable onClose={() => set('action', '')}>도구: {filters.action}</Tag>}
          <Button icon={<ReloadOutlined />} onClick={() => refetch()} loading={isFetching} aria-label="새로고침" />
          {admin && <Button icon={<DownloadOutlined />} href={`/api/admin/audit/export${buildQuery(filters)}`}>CSV</Button>}
        </Space>
      </Card>
      <Card size="small" styles={{ body: { padding: 0 } }}>
        <Table<AuditEvent>
          rowKey="id" size="small" dataSource={data?.items} loading={isFetching && !data} scroll={{ x: 1000 }}
          onRow={(r) => ({ onClick: () => setSel(r), style: { cursor: 'pointer', background: sel?.id === r.id ? 'rgba(37,99,235,.06)' : undefined } })}
          pagination={{ current: page, pageSize, total: data?.total, showSizeChanger: true, pageSizeOptions: [30, 50, 100], onChange: (p, s) => { setPage(p); setPageSize(s) }, showTotal: (t) => `총 ${t.toLocaleString()}건` }}
          columns={[
            { title: '시각', dataIndex: 'ts', width: 150, render: (v) => <span style={{ fontSize: 12.5 }}>{fmtTime(v)}</span> },
            { title: '분류', dataIndex: 'category', width: 80, render: (v) => <Tag>{categoryLabel[v] ?? v}</Tag> },
            { title: '작업', dataIndex: 'action', render: (v) => <span className="mmcp-tool-name">{v}</span> },
            ...(admin ? [{ title: '사용자', key: 'a', width: 130, render: (_: unknown, r: AuditEvent) => <span>{r.actor}{r.mm_username && r.mm_username !== r.actor && <span style={{ opacity: 0.6, fontSize: 12 }}> → @{r.mm_username}</span>}</span> }] : []),
            { title: '결과', dataIndex: 'result', width: 80, render: (v) => <StatusTag status={v} /> },
            { title: '오류', dataIndex: 'error_code', width: 170, render: (v) => v && <Tag color="red" style={{ fontSize: 11 }}>{v}</Tag> },
            { title: '지연', dataIndex: 'latency_ms', width: 70, render: (v) => (v ? `${v}ms` : '') },
            { title: '클라이언트', dataIndex: 'client', width: 130, ellipsis: true },
            { title: '경과', dataIndex: 'ts', key: 'ago', width: 90, render: (v) => <TimeText value={v} /> },
          ]}
        />
      </Card>
    </SplitLayout>
  )
}
