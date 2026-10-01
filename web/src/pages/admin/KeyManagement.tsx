import { useState } from 'react'
import { Alert, App, Button, Card, Col, Input, Popconfirm, Row, Space, Statistic, Steps, Table, Tag, Typography } from 'antd'
import { SyncOutlined, SafetyOutlined, KeyOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post, errorMessage } from '../../api/client'
import type { DataKey } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { CodeBlock, Mono, TimeText } from '../../components/common'

interface KeysOverview {
  master_keys: { id: string; data_keys: number; primary: boolean; loaded: boolean }[]
  primary_master_key: string
  system_keys: DataKey[]
  user_keys: { user_id: string; username: string; display_name: string; version?: number; created_at?: string; master_key_id?: string; retired_versions: number; age_days: number; rotation_due: boolean; has_mm_token: boolean; mm_token_source?: string; mm_token_rotated_at?: string }[]
  rotation_days: number
  auto_tokens: number
  auto_issue_enabled: boolean
}

export default function KeyManagement() {
  const qc = useQueryClient()
  const { message, modal } = App.useApp()
  const [q, setQ] = useState('')
  const { data, isLoading } = useQuery({ queryKey: ['admin', 'keys'], queryFn: () => get<KeysOverview>('/admin/keys') })
  const act = useMutation({
    mutationFn: (f: () => Promise<Record<string, number>>) => f(),
    onSuccess: (r) => { message.success(`완료: ${Object.entries(r).map(([k, v]) => `${k} ${v}`).join(', ')}`); void qc.invalidateQueries({ queryKey: ['admin', 'keys'] }) },
    onError: (e) => message.error(errorMessage(e), 6),
  })
  const stale = data?.master_keys.filter((m) => !m.primary && m.data_keys > 0).reduce((n, m) => n + m.data_keys, 0) ?? 0
  const due = data?.user_keys.filter((u) => u.rotation_due && u.version).length ?? 0
  const users = (data?.user_keys ?? []).filter((u) => !q || `${u.username} ${u.display_name}`.toLowerCase().includes(q.toLowerCase()))
  return (
    <>
      <PageHeader title="키 관리" description="봉투 암호화 구조: ENCRYPTION_KEY(마스터 키) → 시스템/사용자별 데이터 키(AES-256-GCM) → 자격증명·비밀 설정. 데이터 키는 사용자별로 독립 회전됩니다." />
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={12} lg={6}><Card size="small"><Statistic title="주 마스터 키" value={data?.primary_master_key ?? '-'} styles={{ content: { fontSize: 16, fontFamily: 'monospace' } }} /></Card></Col>
        <Col xs={12} lg={6}><Card size="small"><Statistic title="재래핑 필요 데이터 키" value={stale} styles={{ content: { color: stale ? '#ea580c' : undefined } }} /></Card></Col>
        <Col xs={12} lg={6}><Card size="small"><Statistic title={`회전 주기(${data?.rotation_days ?? 90}일) 초과 사용자 키`} value={due} styles={{ content: { color: due ? '#ea580c' : undefined } }} /></Card></Col>
        <Col xs={12} lg={6}><Card size="small"><Statistic title="자동 발급 Mattermost 토큰" value={data?.auto_tokens ?? 0} /></Card></Col>
      </Row>
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card title="마스터 키 (ENCRYPTION_KEY)" style={{ marginBottom: 16 }} extra={
            <Popconfirm title="모든 데이터 키를 주 마스터 키로 다시 감쌀까요?" onConfirm={() => act.mutate(() => post('/admin/keys/rewrap'))}>
              <Button size="small" icon={<SafetyOutlined />} loading={act.isPending}>재래핑</Button>
            </Popconfirm>}>
            <Table size="small" rowKey="id" pagination={false} loading={isLoading} dataSource={data?.master_keys} columns={[
              { title: '키 ID', dataIndex: 'id', render: (v) => <Mono>{v}</Mono> },
              { title: '데이터 키', dataIndex: 'data_keys', width: 80 },
              { title: '상태', key: 's', width: 140, render: (_, m) => <Space size={4}>{m.primary && <Tag color="blue">주 키</Tag>}{m.loaded ? <Tag color="green">로드됨</Tag> : <Tag color="red">없음</Tag>}</Space> },
            ]} />
            <Typography.Paragraph strong style={{ marginTop: 16 }}>마스터 키 회전 절차 (무중단)</Typography.Paragraph>
            <Steps orientation="vertical" size="small" current={-1} items={[
              { title: '새 키 생성', description: <CodeBlock code="docker run --rm mmcp:v1.0.0 gen-key" /> },
              { title: '새 키를 앞에, 이전 키를 뒤에 두고 재시작', description: <CodeBlock code={'ENCRYPTION_KEY="<새 키>,<이전 키>"'} /> },
              { title: '이 화면에서 "재래핑" 실행', description: '모든 데이터 키가 새 키로 다시 감싸집니다.' },
              { title: '이전 키 제거 후 재시작', description: '"재래핑 필요 데이터 키"가 0인지 확인한 뒤 진행하세요.' },
            ]} />
          </Card>
          <Card title="시스템 데이터 키" extra={
            <Popconfirm title="시스템 데이터 키를 회전하고 비밀 설정을 다시 암호화할까요?" onConfirm={() => act.mutate(() => post('/admin/keys/system/rotate'))}>
              <Button size="small" icon={<SyncOutlined />}>회전</Button>
            </Popconfirm>}>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12.5 }}>Keycloak Client Secret, Mattermost 서비스/봇 토큰을 보호합니다.</Typography.Paragraph>
            {data?.system_keys.map((k) => (
              <div key={k.id} style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, padding: '4px 0' }}>
                <span><Tag color={k.status === 'active' ? 'green' : 'default'}>v{k.version}</Tag><Mono>{k.master_key_id}</Mono></span>
                <TimeText value={k.created_at} />
              </div>
            ))}
          </Card>
        </Col>
        <Col xs={24} xl={14}>
          <Card title="사용자 데이터 키 · Mattermost 토큰" extra={
            <Space wrap>
              <Popconfirm title={`회전 주기를 넘긴 사용자 키 ${due}개를 회전할까요?`} disabled={!due} onConfirm={() => act.mutate(() => post('/admin/keys/users/rotate-stale'))}>
                <Button size="small" icon={<KeyOutlined />} disabled={!due}>주기 초과 키 일괄 회전</Button>
              </Popconfirm>
              <Button size="small" icon={<SyncOutlined />} disabled={!data?.auto_tokens} onClick={() => modal.confirm({
                title: '자동 발급 Mattermost 토큰 일괄 회전', content: '새 토큰을 발급해 저장한 뒤 이전 토큰을 Mattermost에서 폐기합니다. 사용자 작업은 끊기지 않습니다.',
                onOk: () => act.mutateAsync(() => post('/admin/keys/mm-tokens/rotate-auto')),
              })}>MM 토큰 일괄 회전</Button>
            </Space>}>
            {!data?.auto_issue_enabled && <Alert type="info" showIcon style={{ marginBottom: 12 }} title="토큰 자동 발급이 꺼져 있어, 사용자가 직접 등록한 토큰은 사용자 본인이 Mattermost에서 새로 만들어 교체해야 합니다." />}
            <Input.Search allowClear placeholder="사용자 검색" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 220, marginBottom: 12 }} />
            <Table size="small" rowKey="user_id" loading={isLoading} dataSource={users} scroll={{ x: 720 }} pagination={{ pageSize: 15, hideOnSinglePage: true }} columns={[
              { title: '사용자', key: 'u', render: (_, u) => <span>{u.display_name || u.username} <Typography.Text type="secondary" style={{ fontSize: 12 }}>@{u.username}</Typography.Text></span> },
              { title: '데이터 키', key: 'v', width: 150, render: (_, u) => u.version ? <span><Tag color={u.rotation_due ? 'orange' : 'green'}>v{u.version}</Tag>{u.age_days}일{u.retired_versions > 0 && <Typography.Text type="secondary" style={{ fontSize: 11 }}> (+{u.retired_versions})</Typography.Text>}</span> : <Typography.Text type="secondary">미생성</Typography.Text> },
              { title: 'MM 토큰', key: 't', width: 150, render: (_, u) => u.has_mm_token ? <span><Tag color={u.mm_token_source === 'auto' ? 'cyan' : 'geekblue'}>{u.mm_token_source === 'auto' ? '자동' : '본인'}</Tag><TimeText value={u.mm_token_rotated_at} /></span> : '-' },
              { title: '', key: 'a', width: 90, render: (_, u) => u.version && <Popconfirm title={`${u.username}의 데이터 키를 회전하고 자격증명을 재암호화할까요?`} onConfirm={() => act.mutate(() => post(`/admin/keys/users/${u.user_id}/rotate`))}><Button size="small">키 회전</Button></Popconfirm> },
            ]} />
          </Card>
        </Col>
      </Row>
    </>
  )
}
