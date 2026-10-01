import { useState } from 'react'
import { App, Button, Card, Divider, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography, Alert, List, Empty } from 'antd'
import { PlusOutlined, ReloadOutlined, UnlockOutlined, LogoutOutlined, DeleteOutlined, KeyOutlined, SearchOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { del, get, post, put, errorMessage } from '../../api/client'
import type { ApiKey, AuditEvent, DataKey, Mapping, Paged, User } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { DetailPanel, SplitLayout } from '../../components/DetailPanel'
import { KV, StatusTag, TimeText, Mono } from '../../components/common'
import { fmtTime, methodLabel } from '../../utils/format'

interface UserRow { user: User; mapping_status?: string; mm_username?: string; has_credential: boolean; api_keys: number; locked: boolean }
interface UserDetail { user: User; mapping?: Mapping; api_keys: ApiKey[]; data_keys: DataKey[]; sessions: { id: string; ip: string; auth_method: string; last_seen_at: string; user_agent: string }[]; activity: AuditEvent[]; locked: boolean; locked_until?: string }

const ROLE_HELP: Record<string, string> = {
  admin: '서비스 관리자 (관리 콘솔)', 'mcp-user': '조회 도구', 'mcp-writer': '메시지/DM 작성', 'mcp-file': '파일 첨부·다운로드',
  'mcp-channel-admin': '채널 생성·멤버 추가', 'mcp-admin': '모든 도구 (위험 도구 포함)',
}
export const roleOptions = Object.entries(ROLE_HELP).map(([value, label]) => ({ value, label: `${value} — ${label}` }))

function genPassword() {
  const a = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*'
  const b = new Uint32Array(16)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => a[x % a.length]).join('') + 'aA1!'
}

export default function Users() {
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [selected, setSelected] = useState<string | null>(params.get('id'))
  const [createOpen, setCreateOpen] = useState(false)
  const filters = { q: params.get('q') ?? '', status: params.get('status') ?? '', source: params.get('source') ?? '', role: params.get('role') ?? '', mapping: params.get('mapping') ?? '' }
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin', 'users', filters, page, pageSize],
    queryFn: () => get<Paged<UserRow>>('/admin/users', { ...filters, page, page_size: pageSize }),
    placeholderData: (p) => p,
  })
  const setFilter = (k: string, v: string) => {
    const n = new URLSearchParams(params)
    if (v) n.set(k, v)
    else n.delete(k)
    setParams(n, { replace: true })
    setPage(1)
  }
  return (
    <>
      <PageHeader
        title="사용자"
        description="mmcp 계정과 역할을 관리합니다. Keycloak 사용자는 첫 로그인(또는 사일런트 SSO) 시 자동 생성되고 역할이 동기화됩니다."
        extra={<>
          <Button icon={<ReloadOutlined />} onClick={() => refetch()} loading={isFetching}>새로고침</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>로컬 사용자 추가</Button>
        </>}
      />
      <SplitLayout
        panel={selected && <UserPanel id={selected} onClose={() => setSelected(null)} onChanged={() => refetch()} />}
      >
        <Card size="small" style={{ marginBottom: 12 }}>
          <Space wrap>
            <Input.Search allowClear placeholder="아이디, 이름, 이메일" value={q} onChange={(e) => setQ(e.target.value)} onSearch={(v) => setFilter('q', v)} style={{ width: 240 }} enterButton={<SearchOutlined />} />
            <Select allowClear placeholder="상태" style={{ width: 110 }} value={filters.status || undefined} onChange={(v) => setFilter('status', v ?? '')} options={[{ value: 'active', label: '활성' }, { value: 'disabled', label: '비활성' }]} />
            <Select allowClear placeholder="출처" style={{ width: 130 }} value={filters.source || undefined} onChange={(v) => setFilter('source', v ?? '')} options={[{ value: 'keycloak', label: 'Keycloak' }, { value: 'local', label: '로컬' }]} />
            <Select allowClear placeholder="역할" style={{ width: 170 }} value={filters.role || undefined} onChange={(v) => setFilter('role', v ?? '')} options={Object.keys(ROLE_HELP).map((r) => ({ value: r, label: r }))} />
            <Select allowClear placeholder="매핑" style={{ width: 130 }} value={filters.mapping || undefined} onChange={(v) => setFilter('mapping', v ?? '')} options={[{ value: 'ok', label: '정상' }, { value: 'not_found', label: '없음' }, { value: 'conflict', label: '충돌' }, { value: 'pending', label: '대기' }, { value: 'none', label: '미시도' }]} />
          </Space>
        </Card>
        <Card size="small" styles={{ body: { padding: 0 } }}>
          <Table<UserRow>
            rowKey={(r) => r.user.id}
            size="middle"
            loading={isFetching && !data}
            dataSource={data?.items}
            scroll={{ x: 900 }}
            onRow={(r) => ({ onClick: () => setSelected(r.user.id), style: { cursor: 'pointer', background: selected === r.user.id ? 'rgba(37,99,235,.06)' : undefined } })}
            pagination={{ current: page, pageSize, total: data?.total, showSizeChanger: true, showTotal: (t) => `총 ${t}명`, onChange: (p, s) => { setPage(p); setPageSize(s) } }}
            columns={[
              { title: '사용자', key: 'u', render: (_, r) => <div><Typography.Text strong>{r.user.display_name || r.user.username}</Typography.Text><div style={{ fontSize: 12, opacity: 0.65 }}>@{r.user.username}{r.user.email ? ` · ${r.user.email}` : ''}</div></div> },
              { title: '출처', key: 's', width: 100, render: (_, r) => <Tag color={r.user.source === 'keycloak' ? 'purple' : 'default'}>{r.user.source === 'keycloak' ? 'Keycloak' : '로컬'}</Tag> },
              { title: '역할', key: 'r', render: (_, r) => <Space size={[4, 4]} wrap>{r.user.roles.map((x) => <Tag key={x} color={x === 'admin' ? 'gold' : 'blue'}>{x}</Tag>)}</Space> },
              { title: '상태', key: 'st', width: 110, render: (_, r) => <Space size={4}><StatusTag status={r.user.status} />{r.locked && <Tag color="red">잠김</Tag>}</Space> },
              { title: 'Mattermost', key: 'm', width: 150, render: (_, r) => r.mapping_status ? <Space size={4}><StatusTag status={r.mapping_status} />{r.mm_username && <span style={{ fontSize: 12 }}>@{r.mm_username}</span>}</Space> : <Typography.Text type="secondary">미연결</Typography.Text> },
              { title: '최근 로그인', key: 'l', width: 120, render: (_, r) => <TimeText value={r.user.last_login_at} /> },
            ]}
          />
        </Card>
      </SplitLayout>
      <CreateUserModal open={createOpen} onClose={() => setCreateOpen(false)} onCreated={(id) => { setCreateOpen(false); void refetch(); setSelected(id) }} />
    </>
  )
}

function CreateUserModal({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (id: string) => void }) {
  const [form] = Form.useForm()
  const { message } = App.useApp()
  const m = useMutation({
    mutationFn: (v: Record<string, unknown>) => post<User>('/admin/users', v),
    onSuccess: (u) => { message.success('사용자를 만들었습니다'); form.resetFields(); onCreated(u.id) },
    onError: (e) => message.error(errorMessage(e)),
  })
  return (
    <Modal open={open} title="로컬 사용자 추가" onCancel={onClose} onOk={() => form.submit()} okText="추가" confirmLoading={m.isPending} destroyOnHidden>
      <Alert type="info" showIcon style={{ marginBottom: 16 }} title="Keycloak 사용자는 따로 만들 필요가 없습니다. 같은 아이디로 미리 만들어 두면 첫 SSO 로그인 때 자동으로 연결됩니다." />
      <Form form={form} layout="vertical" onFinish={(v) => m.mutate(v)} initialValues={{ roles: ['mcp-user'] }} preserve={false} name="create-user">
        <Form.Item name="username" label="아이디" rules={[{ required: true, message: '아이디를 입력하세요' }, { pattern: /^[a-z0-9._@-]{2,64}$/, message: '영문 소문자/숫자/._-@ 2~64자' }]} normalize={(v: string) => v?.toLowerCase()}>
          <Input autoComplete="off" placeholder="hkjang" />
        </Form.Item>
        <Form.Item name="display_name" label="표시 이름"><Input placeholder="장형국" /></Form.Item>
        <Form.Item name="email" label="이메일" rules={[{ type: 'email', message: '이메일 형식이 아닙니다' }]}><Input placeholder="user@company.co.kr" /></Form.Item>
        <Form.Item label="초기 비밀번호" extra="비워 두면 SSO 전용 계정이 됩니다.">
          <Space.Compact style={{ width: '100%' }}>
            <Form.Item name="password" noStyle rules={[{ min: 8, message: '8자 이상' }]}>
              <Input.Password autoComplete="new-password" placeholder="8자 이상, 3종류 이상 조합" />
            </Form.Item>
            <Button onClick={() => form.setFieldsValue({ password: genPassword() })}>생성</Button>
          </Space.Compact>
        </Form.Item>
        <Form.Item name="roles" label="역할"><Select mode="multiple" options={roleOptions} /></Form.Item>
      </Form>
    </Modal>
  )
}

function UserPanel({ id, onClose, onChanged }: { id: string; onClose: () => void; onChanged: () => void }) {
  const { message, modal } = App.useApp()
  const qc = useQueryClient()
  const { data, refetch } = useQuery({ queryKey: ['admin', 'user', id], queryFn: () => get<UserDetail>(`/admin/users/${id}`) })
  const [roles, setRoles] = useState<string[] | null>(null)
  const refresh = () => { void refetch(); onChanged(); void qc.invalidateQueries({ queryKey: ['admin', 'users'] }) }
  const act = useMutation({
    mutationFn: (f: () => Promise<unknown>) => f(),
    onSuccess: () => { message.success('처리했습니다'); refresh() },
    onError: (e) => message.error(errorMessage(e)),
  })
  if (!data) return <DetailPanel open title="불러오는 중" onClose={onClose}><Empty /></DetailPanel>
  const u = data.user
  const curRoles = roles ?? u.manual_roles
  const resetPassword = () => {
    let pw = genPassword()
    modal.confirm({
      title: `${u.username} 비밀번호 재설정`,
      content: (
        <div>
          <p>새 비밀번호를 사용자에게 안전한 경로로 전달하세요. 기존 세션은 모두 종료됩니다.</p>
          <Input.Password defaultValue={pw} onChange={(e) => { pw = e.target.value }} autoComplete="new-password" />
        </div>
      ),
      okText: '재설정',
      onOk: () => act.mutateAsync(() => post(`/admin/users/${id}/reset-password`, { password: pw })),
    })
  }
  return (
    <DetailPanel open title={u.display_name || u.username} onClose={onClose} extra={<StatusTag status={u.status} />}>
      <KV items={[
        ['아이디', <Mono>@{u.username}</Mono>],
        ['출처', u.source === 'keycloak' ? 'Keycloak SSO' : '로컬 계정'],
        ['이메일', u.email || '-'],
        ['Keycloak sub', u.oidc_sub ? <Mono>{u.oidc_sub}</Mono> : '-'],
        ['생성', fmtTime(u.created_at)],
        ['최근 로그인', fmtTime(u.last_login_at)],
        ['잠금', data.locked ? <Tag color="red">{fmtTime(data.locked_until)}까지</Tag> : '없음'],
      ]} />
      <Divider titlePlacement="start" plain>역할</Divider>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>Keycloak에서 동기화된 역할: {u.idp_roles.length ? u.idp_roles.map((r) => <Tag key={r} color="purple">{r}</Tag>) : '없음'}</Typography.Paragraph>
      <Select mode="multiple" style={{ width: '100%' }} value={curRoles} onChange={setRoles} options={roleOptions} placeholder="수동 부여 역할" aria-label="수동 부여 역할" />
      <Space style={{ marginTop: 8 }}>
        <Button type="primary" size="small" disabled={roles === null} loading={act.isPending} onClick={() => act.mutate(() => put(`/admin/users/${id}`, { roles: curRoles }).then(() => setRoles(null)))}>역할 저장</Button>
        {roles !== null && <Button size="small" onClick={() => setRoles(null)}>취소</Button>}
      </Space>
      <Divider titlePlacement="start" plain>계정 상태</Divider>
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Space>
          <Switch checked={u.status === 'active'} loading={act.isPending} onChange={(on) => act.mutate(() => put(`/admin/users/${id}`, { status: on ? 'active' : 'disabled' }))} />
          <span>{u.status === 'active' ? '활성' : '비활성 (로그인·MCP 호출 차단)'}</span>
        </Space>
        <Space wrap>
          {u.source === 'local' && <Button size="small" icon={<KeyOutlined />} onClick={resetPassword}>비밀번호 재설정</Button>}
          {data.locked && <Button size="small" icon={<UnlockOutlined />} onClick={() => act.mutate(() => post(`/admin/users/${id}/unlock`))}>잠금 해제</Button>}
          <Popconfirm title="모든 웹 세션과 OAuth 토큰을 종료할까요?" onConfirm={() => act.mutate(() => post(`/admin/users/${id}/revoke-sessions`))}>
            <Button size="small" icon={<LogoutOutlined />}>세션·토큰 강제 종료</Button>
          </Popconfirm>
          <Popconfirm title="사용자를 삭제할까요? 매핑, 키, 자격증명이 함께 삭제되며 되돌릴 수 없습니다." okButtonProps={{ danger: true }} onConfirm={() => act.mutate(() => del(`/admin/users/${id}`).then(onClose))}>
            <Button size="small" danger icon={<DeleteOutlined />}>삭제</Button>
          </Popconfirm>
        </Space>
      </Space>
      <Divider titlePlacement="start" plain>Mattermost 매핑</Divider>
      {data.mapping ? (
        <KV items={[
          ['상태', <Space><StatusTag status={data.mapping.status} />{methodLabel[data.mapping.method]}</Space>],
          ['Mattermost', data.mapping.mm_username ? `@${data.mapping.mm_username} (${data.mapping.mm_display_name || '-'})` : '-'],
          ['user_id', <Mono>{data.mapping.mm_user_id || '-'}</Mono>],
          ['자격증명', data.mapping.credential_type === 'pat' ? `PAT ····${data.mapping.token_last4} (${data.mapping.credential_source === 'auto' ? '자동 발급' : '사용자 등록'}, v${data.mapping.token_version})` : '없음'],
          ['오류', data.mapping.last_error || '-'],
        ]} />
      ) : <Typography.Text type="secondary">아직 매핑되지 않았습니다.</Typography.Text>}
      <div style={{ marginTop: 8 }}><Link to={`/admin/mappings?q=${u.username}`}>매핑 화면에서 관리 →</Link></div>
      <Divider titlePlacement="start" plain>API 키 ({data.api_keys.filter((k) => k.status === 'active').length})</Divider>
      <List size="small" dataSource={data.api_keys.slice(0, 8)} locale={{ emptyText: '없음' }} renderItem={(k) => (
        <List.Item extra={<StatusTag status={k.status} />}>
          <List.Item.Meta title={<span>{k.name} {k.read_only && <Tag>읽기 전용</Tag>}</span>} description={<span style={{ fontSize: 12 }}><Mono>{k.prefix}…</Mono> · 마지막 사용 <TimeText value={k.last_used_at} /></span>} />
        </List.Item>
      )} />
      <Divider titlePlacement="start" plain>개인 데이터 키</Divider>
      {data.data_keys.length === 0 ? <Typography.Text type="secondary">아직 생성되지 않음</Typography.Text> : data.data_keys.map((k) => (
        <div key={k.id} style={{ fontSize: 12.5, marginBottom: 4 }}><Tag color={k.status === 'active' ? 'green' : 'default'}>v{k.version}</Tag>{fmtTime(k.created_at)} · {k.master_key_id}</div>
      ))}
      <Divider titlePlacement="start" plain>최근 활동</Divider>
      <List size="small" dataSource={data.activity} locale={{ emptyText: '기록 없음' }} renderItem={(a) => (
        <List.Item>
          <span style={{ fontSize: 12.5 }}><TimeText value={a.ts} /> · <span className="mmcp-tool-name">{a.action}</span></span>
          <StatusTag status={a.result} />
        </List.Item>
      )} />
      <Link to={`/admin/audit?actor=${u.username}`}>감사 로그 전체 보기 →</Link>
    </DetailPanel>
  )
}
