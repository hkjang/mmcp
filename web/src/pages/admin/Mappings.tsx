import { useEffect, useMemo, useState } from 'react'
import { Alert, App, AutoComplete, Button, Card, Col, Divider, Form, Input, Modal, Popconfirm, Row, Select, Space, Statistic, Steps, Table, Tag, Typography } from 'antd'
import { SyncOutlined, ReloadOutlined, UserSwitchOutlined, ThunderboltOutlined, SearchOutlined, DisconnectOutlined, StopOutlined, PlayCircleOutlined, SafetyOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { del, get, post, put, errorMessage } from '../../api/client'
import type { Mapping, Paged, User, ToolError } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { DetailPanel, SplitLayout } from '../../components/DetailPanel'
import { KV, Mono, StatusTag, TimeText } from '../../components/common'
import { fmtTime, methodLabel } from '../../utils/format'

interface MappingList extends Paged<Mapping> { stats: Record<string, number> }

export default function Mappings() {
  const [params, setParams] = useSearchParams()
  const { message, modal } = App.useApp()
  const qc = useQueryClient()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<string | null>(null)
  const [manualOpen, setManualOpen] = useState(false)
  const status = params.get('status') ?? ''
  const { data, refetch, isFetching } = useQuery({
    queryKey: ['admin', 'mappings', params.get('q'), status, page],
    queryFn: () => get<MappingList>('/admin/mappings', { q: params.get('q') ?? '', status, page, page_size: 20 }),
    placeholderData: (p) => p,
  })
  const auto = useMutation({
    mutationFn: () => post<{ result: Record<string, number>; failures: { username: string; error: string }[] }>('/admin/mappings/auto'),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: ['admin', 'mappings'] })
      modal.info({
        title: '자동 매핑 결과',
        width: 520,
        content: (
          <div>
            <p>처리 {r.result.processed}명 · 성공 {r.result.mapped}명 · 실패 {r.result.failed}명</p>
            {r.failures.length > 0 && <ul style={{ paddingLeft: 18, maxHeight: 240, overflow: 'auto' }}>{r.failures.map((f) => <li key={f.username}><b>{f.username}</b>: {f.error}</li>)}</ul>}
          </div>
        ),
      })
    },
    onError: (e) => message.error(errorMessage(e)),
  })
  useEffect(() => {
    if (params.get('auto') === '1') {
      const n = new URLSearchParams(params)
      n.delete('auto')
      setParams(n, { replace: true })
      modal.confirm({ title: '미매핑 사용자를 자동 매핑할까요?', content: 'Keycloak username과 같은 Mattermost username을 서비스 토큰으로 찾아 연결합니다.', onOk: () => auto.mutateAsync() })
    }
  }, [params, setParams, modal, auto])
  const s = data?.stats ?? {}
  const setStatus = (v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set('status', v)
    else n.delete('status')
    setParams(n, { replace: true })
    setPage(1)
  }
  return (
    <>
      <PageHeader
        title="계정 매핑"
        description={<>최초 연결은 <b>preferred_username ↔ Mattermost username</b>으로 찾고, 이후에는 <b>Keycloak sub ↔ Mattermost user_id</b>를 영구 식별자로 고정합니다. 계정이 삭제·재생성되면 충돌로 자동 차단됩니다.</>}
        extra={<>
          <Button icon={<ReloadOutlined />} onClick={() => refetch()} loading={isFetching}>새로고침</Button>
          <Button icon={<UserSwitchOutlined />} onClick={() => setManualOpen(true)}>수동 매핑</Button>
          <Button type="primary" icon={<ThunderboltOutlined />} loading={auto.isPending} onClick={() => auto.mutate()}>미매핑 사용자 자동 매핑</Button>
        </>}
      />
      <Row gutter={[12, 12]} style={{ marginBottom: 12 }}>
        {[
          ['ok', '정상', s.ok], ['pending', '대기', s.pending], ['not_found', 'Mattermost 없음', s.not_found], ['conflict', '충돌', s.conflict], ['disabled', '비활성', s.disabled], ['', '매핑 미시도 사용자', s.unmapped_users],
        ].map(([k, label, v]) => (
          <Col xs={12} sm={8} xl={4} key={String(label)}>
            <Card size="small" hoverable onClick={() => k !== '' && setStatus(status === k ? '' : String(k))} style={{ borderColor: status === k && k ? '#2563eb' : undefined }}>
              <Statistic title={label} value={Number(v ?? 0)} styles={{ content: { fontSize: 22, color: k === 'conflict' && Number(v) > 0 ? '#dc2626' : undefined } }} />
            </Card>
          </Col>
        ))}
      </Row>
      <SplitLayout panel={selected && <MappingPanel id={selected} onClose={() => setSelected(null)} />}>
        <Card size="small" style={{ marginBottom: 12 }}>
          <Space wrap>
            <Input.Search allowClear placeholder="mmcp/Keycloak/Mattermost 사용자명" value={q} onChange={(e) => setQ(e.target.value)} enterButton={<SearchOutlined />} style={{ width: 280 }}
              onSearch={(v) => { const n = new URLSearchParams(params); if (v) n.set('q', v); else n.delete('q'); setParams(n, { replace: true }); setPage(1) }} />
            <Select allowClear placeholder="상태" value={status || undefined} onChange={(v) => setStatus(v ?? '')} style={{ width: 160 }}
              options={['ok', 'pending', 'not_found', 'conflict', 'disabled'].map((v) => ({ value: v, label: <StatusTag status={v} /> }))} />
          </Space>
        </Card>
        <Card size="small" styles={{ body: { padding: 0 } }}>
          <Table<Mapping>
            rowKey="id"
            dataSource={data?.items}
            loading={isFetching && !data}
            scroll={{ x: 960 }}
            onRow={(r) => ({ onClick: () => setSelected(r.id), style: { cursor: 'pointer', background: selected === r.id ? 'rgba(37,99,235,.06)' : undefined } })}
            pagination={{ current: page, pageSize: 20, total: data?.total, onChange: setPage, showTotal: (t) => `총 ${t}건` }}
            columns={[
              { title: 'mmcp / Keycloak', key: 'k', render: (_, r) => <div><b>{r.username}</b><div style={{ fontSize: 11.5, opacity: 0.6 }}>{r.keycloak_sub ? <Mono>sub {r.keycloak_sub.slice(0, 13)}…</Mono> : '로컬 계정'}</div></div> },
              { title: 'Mattermost', key: 'm', render: (_, r) => r.mm_username ? <div>@{r.mm_username} <span style={{ opacity: 0.7 }}>{r.mm_display_name}</span><div style={{ fontSize: 11.5, opacity: 0.6 }}><Mono>{r.mm_user_id || '-'}</Mono></div></div> : '-' },
              { title: '방식', dataIndex: 'method', width: 90, render: (v) => <Tag>{methodLabel[v]}</Tag> },
              { title: '상태', dataIndex: 'status', width: 100, render: (v) => <StatusTag status={v} /> },
              { title: '자격증명', key: 'c', width: 150, render: (_, r) => r.credential_type === 'pat' ? <Tag color={r.credential_source === 'auto' ? 'cyan' : 'geekblue'}>PAT ··{r.token_last4} · {r.credential_source === 'auto' ? '자동' : '본인'}</Tag> : <Typography.Text type="secondary">없음</Typography.Text> },
              { title: '마지막 검증', dataIndex: 'verified_at', width: 120, render: (v) => <TimeText value={v} /> },
              { title: '오류', dataIndex: 'last_error', ellipsis: true, render: (v) => v && <Typography.Text type="danger" style={{ fontSize: 12 }}>{v}</Typography.Text> },
            ]}
          />
        </Card>
      </SplitLayout>
      <ManualMappingModal open={manualOpen} onClose={() => setManualOpen(false)} />
    </>
  )
}

function MappingPanel({ id, onClose }: { id: string; onClose: () => void }) {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const list = qc.getQueriesData<MappingList>({ queryKey: ['admin', 'mappings'] })
  const fromList = list.flatMap(([, d]) => d?.items ?? []).find((m) => m.id === id)
  const [m, setM] = useState<Mapping | undefined>(fromList)
  const [verifyErr, setVerifyErr] = useState<ToolError | null>(null)
  useEffect(() => { setM(fromList); setVerifyErr(null) }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  const done = (r: Mapping | { mapping: Mapping; error?: ToolError }) => {
    if ('mapping' in r) {
      setM(r.mapping)
      setVerifyErr(r.error ?? null)
      if (r.error) message.warning(r.error.message, 6)
      else message.success('검증했습니다')
    } else {
      setM(r)
      message.success('처리했습니다')
    }
    void qc.invalidateQueries({ queryKey: ['admin', 'mappings'] })
  }
  const act = useMutation({ mutationFn: (f: () => Promise<Mapping | { mapping: Mapping; error?: ToolError }>) => f(), onSuccess: done, onError: (e) => message.error(errorMessage(e), 6) })
  if (!m) return null
  const chainStatus = m.status === 'ok' ? 'finish' : m.status === 'conflict' || m.status === 'not_found' ? 'error' : 'wait'
  return (
    <DetailPanel open title={`${m.username} ↔ ${m.mm_username ? '@' + m.mm_username : '?'}`} onClose={onClose} extra={<StatusTag status={m.status} />}>
      <Steps
        orientation="vertical"
        size="small"
        items={[
          { title: 'Keycloak', status: m.keycloak_sub ? 'finish' : 'wait', description: m.keycloak_sub ? <span style={{ fontSize: 12 }}>@{m.keycloak_username} · <Mono>{m.keycloak_sub}</Mono></span> : '로컬 계정 (Keycloak 연결 없음)' },
          { title: 'mmcp 사용자', status: 'finish', description: <span style={{ fontSize: 12 }}>@{m.username}</span> },
          { title: 'Mattermost', status: chainStatus, description: m.mm_user_id ? <span style={{ fontSize: 12 }}>@{m.mm_username} · <Mono>{m.mm_user_id}</Mono></span> : (m.last_error || '아직 찾지 못함') },
        ]}
      />
      {(m.last_error || verifyErr) && <Alert type={m.status === 'conflict' ? 'error' : 'warning'} showIcon title={verifyErr?.message ?? m.last_error} style={{ margin: '8px 0 12px' }} />}
      {m.status === 'conflict' && (
        <Alert type="info" style={{ marginBottom: 12 }} title="충돌 해결" description="Mattermost 계정이 삭제 후 재생성되었거나 username이 다른 계정을 가리킵니다. 올바른 계정을 확인한 뒤 '재검증'으로 다시 찾거나 '수동 매핑'으로 지정하세요." />
      )}
      <KV items={[
        ['방식', methodLabel[m.method]],
        ['이메일', m.mm_email || '-'],
        ['매핑 시각', fmtTime(m.mapped_at)],
        ['마지막 검증', fmtTime(m.verified_at)],
        ['자격증명', m.credential_type === 'pat' ? `PAT ····${m.token_last4}` : '없음'],
        ['발급 방식', m.credential_source === 'auto' ? '서비스 토큰으로 자동 발급' : m.credential_source === 'user' ? '사용자가 직접 등록' : '-'],
        ['토큰 버전', m.token_version ? `v${m.token_version} · ${fmtTime(m.token_rotated_at)}` : '-'],
      ]} />
      <Divider plain />
      <Space wrap>
        <Button icon={<SafetyOutlined />} loading={act.isPending} onClick={() => act.mutate(() => post(`/admin/mappings/${m.id}/verify`))}>재검증</Button>
        {m.status === 'ok' && <Button icon={<SyncOutlined />} loading={act.isPending} onClick={() => act.mutate(() => post(`/admin/mappings/${m.id}/rotate-token`))}>{m.credential_type === 'pat' ? '토큰 회전' : '토큰 자동 발급'}</Button>}
        {m.status === 'disabled'
          ? <Button icon={<PlayCircleOutlined />} onClick={() => act.mutate(() => put(`/admin/mappings/${m.id}/status`, { status: 'enabled' }))}>다시 활성화</Button>
          : <Popconfirm title="이 사용자의 Mattermost 접근을 중지할까요?" onConfirm={() => act.mutate(() => put(`/admin/mappings/${m.id}/status`, { status: 'disabled' }))}><Button icon={<StopOutlined />}>비활성화</Button></Popconfirm>}
        <Popconfirm title="매핑과 저장된 자격증명을 삭제할까요? 자동 발급 토큰은 Mattermost에서도 폐기됩니다." okButtonProps={{ danger: true }}
          onConfirm={() => del(`/admin/mappings/${m.id}`).then(() => { message.success('삭제했습니다'); void qc.invalidateQueries({ queryKey: ['admin', 'mappings'] }); onClose() }).catch((e) => message.error(errorMessage(e)))}>
          <Button danger icon={<DisconnectOutlined />}>매핑 해제</Button>
        </Popconfirm>
      </Space>
    </DetailPanel>
  )
}

function ManualMappingModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [form] = Form.useForm()
  const { message } = App.useApp()
  const qc = useQueryClient()
  const [userQ, setUserQ] = useState('')
  const [mmQ, setMmQ] = useState('')
  const users = useQuery({ queryKey: ['admin', 'users-pick', userQ], queryFn: () => get<Paged<{ user: User }>>('/admin/users', { q: userQ, page_size: 20 }), enabled: open })
  const mm = useQuery({ queryKey: ['admin', 'mm-users', mmQ], queryFn: () => get<{ items: { username: string; display_name: string; email: string; position: string }[] }>('/admin/mattermost/users', { q: mmQ }), enabled: open && mmQ.length > 0, retry: false })
  const mmOptions = useMemo(() => (mm.data?.items ?? []).map((u) => ({ value: u.username, label: <span>@{u.username} <span style={{ opacity: 0.6 }}>{u.display_name} {u.position && `· ${u.position}`}</span></span> })), [mm.data])
  const m = useMutation({
    mutationFn: (v: { user_id: string; mm_username: string }) => post<Mapping>('/admin/mappings', v),
    onSuccess: () => { message.success('매핑했습니다'); void qc.invalidateQueries({ queryKey: ['admin', 'mappings'] }); form.resetFields(); onClose() },
    onError: (e) => message.error(errorMessage(e), 6),
  })
  return (
    <Modal open={open} title="수동 매핑" onCancel={onClose} onOk={() => form.submit()} okText="매핑" confirmLoading={m.isPending} destroyOnHidden>
      <Typography.Paragraph type="secondary">username이 다른 사용자(예: Keycloak <code>honggd</code> → Mattermost <code>hong</code>)를 직접 연결합니다. 이전 자격증명은 폐기됩니다.</Typography.Paragraph>
      <Form form={form} layout="vertical" onFinish={(v) => m.mutate(v)} preserve={false} name="manual-mapping">
        <Form.Item name="user_id" label="mmcp 사용자" rules={[{ required: true, message: '사용자를 선택하세요' }]}>
          <Select showSearch filterOption={false} onSearch={setUserQ} placeholder="아이디나 이름으로 검색" loading={users.isFetching}
            options={(users.data?.items ?? []).map((r) => ({ value: r.user.id, label: `${r.user.display_name || r.user.username} (@${r.user.username})` }))} />
        </Form.Item>
        <Form.Item name="mm_username" label="Mattermost username" rules={[{ required: true, message: 'username을 입력하세요' }]} extra={mm.error ? errorMessage(mm.error) : '입력하면 Mattermost 사용자를 검색합니다 (서비스 토큰 필요).'}>
          <AutoComplete options={mmOptions} onSearch={setMmQ} placeholder="hong" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
