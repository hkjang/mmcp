import { useEffect, useState } from 'react'
import { Alert, App, Button, Card, Checkbox, Form, Input, Modal, Popconfirm, Radio, Select, Space, Table, Tag, Typography } from 'antd'
import { PlusOutlined, SyncOutlined, DeleteOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { del, get, post, put, errorMessage } from '../../api/client'
import type { ApiKey } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { CodeBlock, Mono, SecretModal, StatusTag, TimeText } from '../../components/common'
import { fmtDate } from '../../utils/format'

interface KeyPolicy { max_days: number; require_expiry: boolean; max_per_user: number; rotation_days: number }
interface Created { id: string; name: string; secret: string; prefix: string }

export default function MyKeys() {
  const [params, setParams] = useSearchParams()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const [createOpen, setCreateOpen] = useState(false)
  const [rotateKey, setRotateKey] = useState<ApiKey | null>(null)
  const [secret, setSecret] = useState<Created | null>(null)
  const [showRevoked, setShowRevoked] = useState(false)
  const { data, isLoading } = useQuery({ queryKey: ['me', 'keys'], queryFn: () => get<{ items: ApiKey[]; policy: KeyPolicy }>('/me/api-keys') })
  useEffect(() => {
    if (params.get('new') === '1') {
      setCreateOpen(true)
      const n = new URLSearchParams(params)
      n.delete('new')
      setParams(n, { replace: true })
    }
  }, [params, setParams])
  const refresh = () => void qc.invalidateQueries({ queryKey: ['me', 'keys'] })
  const revoke = useMutation({ mutationFn: (id: string) => del(`/me/api-keys/${id}`), onSuccess: () => { message.success('폐기했습니다'); refresh() }, onError: (e) => message.error(errorMessage(e)) })
  const rename = useMutation({ mutationFn: (v: { id: string; name: string }) => put(`/me/api-keys/${v.id}`, { name: v.name }), onSuccess: refresh, onError: (e) => message.error(errorMessage(e)) })
  const policy = data?.policy
  const items = (data?.items ?? []).filter((k) => showRevoked || k.status === 'active')
  const due = (data?.items ?? []).filter((k) => k.rotation_due).length
  return (
    <>
      <PageHeader title="내 API 키" description="OAuth를 지원하지 않는 클라이언트나 자동화 스크립트에서 쓰는 개인 키입니다. 키는 내 권한으로만 동작하며, 비밀값은 발급 순간 한 번만 표시됩니다."
        extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>새 키 발급</Button>} />
      {due > 0 && <Alert type="warning" showIcon style={{ marginBottom: 12 }} title={`${due}개 키가 회전 권장 주기(${policy?.rotation_days}일)를 넘었습니다. '회전'으로 새 키를 받고 클라이언트 설정을 교체하세요.`} />}
      <Card size="small" styles={{ body: { padding: 0 } }} extra={<Checkbox checked={showRevoked} onChange={(e) => setShowRevoked(e.target.checked)}>폐기·만료 키 보기</Checkbox>} title={`키 ${items.length}개 · 정책: 최대 ${policy?.max_days ?? '-'}일, 사용자당 ${policy?.max_per_user ?? '-'}개`}>
        <Table<ApiKey> rowKey="id" loading={isLoading} dataSource={items} pagination={false} scroll={{ x: 860 }} locale={{ emptyText: '발급된 키가 없습니다' }} columns={[
          { title: '이름', key: 'n', render: (_, k) => <Typography.Text editable={k.status === 'active' ? { onChange: (v) => v && v !== k.name && rename.mutate({ id: k.id, name: v }), tooltip: '이름 변경' } : false}>{k.name}</Typography.Text> },
          { title: '키', dataIndex: 'prefix', width: 160, render: (v, k) => <span><Mono>{v}…</Mono>{k.read_only && <Tag style={{ marginLeft: 6 }}>읽기 전용</Tag>}</span> },
          { title: '상태', key: 's', width: 140, render: (_, k) => <Space size={4}><StatusTag status={k.status} />{k.rotation_due && <Tag color="orange">회전 권장</Tag>}</Space> },
          { title: '만료', dataIndex: 'expires_at', width: 110, render: (v) => (v ? fmtDate(v) : <Typography.Text type="secondary">없음</Typography.Text>) },
          { title: '마지막 사용', dataIndex: 'last_used_at', width: 120, render: (v) => <TimeText value={v} /> },
          { title: '생성', dataIndex: 'created_at', width: 100, render: (v) => <TimeText value={v} /> },
          {
            title: '', key: 'a', width: 170, render: (_, k) => k.status === 'active' && (
              <Space size={4}>
                <Button size="small" icon={<SyncOutlined />} onClick={() => setRotateKey(k)}>회전</Button>
                <Popconfirm title="키를 즉시 폐기할까요? 이 키를 쓰는 클라이언트는 바로 끊깁니다." okButtonProps={{ danger: true }} onConfirm={() => revoke.mutate(k.id)}>
                  <Button size="small" danger icon={<DeleteOutlined />}>폐기</Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]} />
      </Card>
      <CreateKeyModal open={createOpen} policy={policy} onClose={() => setCreateOpen(false)} onCreated={(c) => { setCreateOpen(false); setSecret(c); refresh() }} />
      <RotateKeyModal k={rotateKey} onClose={() => setRotateKey(null)} onRotated={(c) => { setRotateKey(null); setSecret(c); refresh() }} />
      <SecretModal open={!!secret} title={`API 키 발급됨 — ${secret?.name ?? ''}`} secret={secret?.secret ?? ''} onClose={() => setSecret(null)}>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 6 }}>Claude Code에 등록 예시</Typography.Paragraph>
        <CodeBlock code={`claude mcp add --transport http mmcp ${window.location.origin}/mcp \\\n  --header "Authorization: Bearer ${secret?.secret ?? ''}"`} wrap />
      </SecretModal>
    </>
  )
}

const expiryOptions = (max: number, required: boolean) => [
  ...[30, 90, 180, 365].filter((d) => d <= max).map((d) => ({ value: d, label: `${d}일` })),
  ...(required ? [] : [{ value: 0, label: '만료 없음 (권장하지 않음)' }]),
]

function CreateKeyModal({ open, policy, onClose, onCreated }: { open: boolean; policy?: KeyPolicy; onClose: () => void; onCreated: (c: Created) => void }) {
  const [form] = Form.useForm()
  const { message } = App.useApp()
  const m = useMutation({ mutationFn: (v: { name: string; read_only: boolean; expires_days: number }) => post<Created>('/me/api-keys', v), onSuccess: (c) => { form.resetFields(); onCreated(c) }, onError: (e) => message.error(errorMessage(e)) })
  return (
    <Modal open={open} title="새 API 키 발급" onCancel={onClose} onOk={() => form.submit()} okText="발급" confirmLoading={m.isPending} destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={(v) => m.mutate(v)} initialValues={{ read_only: false, expires_days: Math.min(90, policy?.max_days ?? 90) }} preserve={false} name="create-key">
        <Form.Item name="name" label="이름" rules={[{ required: true, message: '어디에 쓰는 키인지 적어 주세요' }, { max: 60 }]}>
          <Input placeholder="예: 노트북 Claude Code" autoFocus maxLength={60} />
        </Form.Item>
        <Form.Item name="expires_days" label="만료"><Select options={expiryOptions(policy?.max_days ?? 365, !!policy?.require_expiry)} /></Form.Item>
        <Form.Item name="read_only" valuePropName="checked" extra="조회·검색 도구만 사용할 수 있습니다. 공유 환경이나 대시보드용으로 권장합니다.">
          <Checkbox>읽기 전용 키</Checkbox>
        </Form.Item>
      </Form>
    </Modal>
  )
}

function RotateKeyModal({ k, onClose, onRotated }: { k: ApiKey | null; onClose: () => void; onRotated: (c: Created) => void }) {
  const [grace, setGrace] = useState(60)
  const { message } = App.useApp()
  const m = useMutation({ mutationFn: () => post<Created>(`/me/api-keys/${k?.id}/rotate`, { grace_minutes: grace }), onSuccess: onRotated, onError: (e) => message.error(errorMessage(e)) })
  return (
    <Modal open={!!k} title={`키 회전 — ${k?.name ?? ''}`} onCancel={onClose} onOk={() => m.mutate()} okText="회전" confirmLoading={m.isPending} destroyOnHidden>
      <Typography.Paragraph>같은 설정(이름, 권한, 만료 기간)으로 새 키를 발급합니다. 이전 키는 아래 유예 시간이 지나면 만료됩니다.</Typography.Paragraph>
      <Radio.Group value={grace} onChange={(e) => setGrace(e.target.value)} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Radio value={0}>즉시 폐기 (유출이 의심될 때)</Radio>
        <Radio value={60}>1시간 유예</Radio>
        <Radio value={1440}>24시간 유예 (권장: 여러 기기 교체)</Radio>
        <Radio value={10080}>7일 유예</Radio>
      </Radio.Group>
    </Modal>
  )
}
