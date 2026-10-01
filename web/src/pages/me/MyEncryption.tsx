import { Alert, App, Button, Card, Col, Popconfirm, Row, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { SyncOutlined, LockOutlined, KeyOutlined, SafetyCertificateOutlined, ArrowRightOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { get, post, errorMessage } from '../../api/client'
import type { DataKey } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { Mono, TimeText } from '../../components/common'
import { fmtTime } from '../../utils/format'

interface EncInfo { keys: DataKey[]; protected_items: string[]; rotation_days: number; algorithm: string }

export default function MyEncryption() {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const { data } = useQuery({ queryKey: ['me', 'encryption'], queryFn: () => get<EncInfo>('/me/encryption-key') })
  const rotate = useMutation({ mutationFn: () => post<{ version: number }>('/me/encryption-key/rotate'), onSuccess: (r) => { message.success(`개인 데이터 키를 v${r.version}으로 회전했습니다`); void qc.invalidateQueries({ queryKey: ['me'] }) }, onError: (e) => message.error(errorMessage(e)) })
  if (!data) return <Skeleton active />
  const active = data.keys.find((k) => k.status === 'active')
  const age = active ? dayjs().diff(dayjs(active.created_at), 'day') : 0
  return (
    <>
      <PageHeader title="개인 암호화 키" description="내 자격증명은 나만의 데이터 키로 암호화됩니다. 키를 회전하면 새 키가 만들어지고, 저장된 자격증명이 즉시 재암호화된 뒤 이전 키는 폐기됩니다."
        extra={<Popconfirm title="개인 데이터 키를 회전할까요?" description="진행 중인 작업에 영향은 없습니다." onConfirm={() => rotate.mutate()}><Button type="primary" icon={<SyncOutlined />} loading={rotate.isPending}>지금 회전</Button></Popconfirm>} />
      {active && age >= data.rotation_days && <Alert type="warning" showIcon style={{ marginBottom: 16 }} title={`현재 키가 ${age}일 경과해 권장 주기(${data.rotation_days}일)를 넘었습니다.`} />}
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={14}>
          <Card title="키 버전">
            <Table<DataKey> rowKey="id" size="small" pagination={false} dataSource={data.keys} locale={{ emptyText: '아직 데이터 키가 없습니다. 자격증명을 저장하면 자동 생성됩니다.' }} columns={[
              { title: '버전', dataIndex: 'version', width: 80, render: (v, k) => <Tag color={k.status === 'active' ? 'green' : 'default'}>v{v}</Tag> },
              { title: '상태', dataIndex: 'status', width: 90, render: (v) => (v === 'active' ? '사용 중' : '폐기됨') },
              { title: '생성', dataIndex: 'created_at', render: (v) => fmtTime(v) },
              { title: '폐기', dataIndex: 'retired_at', render: (v) => (v ? <TimeText value={v} /> : '-') },
              { title: '감싼 마스터 키', dataIndex: 'master_key_id', render: (v) => <Mono>{v}</Mono> },
            ]} />
          </Card>
        </Col>
        <Col xs={24} xl={10}>
          <Card title="보호 구조" style={{ marginBottom: 16 }}>
            <Space wrap style={{ fontSize: 13 }}>
              <Tag icon={<SafetyCertificateOutlined />} color="purple">마스터 키 (ENCRYPTION_KEY)</Tag><ArrowRightOutlined />
              <Tag icon={<KeyOutlined />} color="blue">내 데이터 키 v{active?.version ?? '-'}</Tag><ArrowRightOutlined />
              <Tag icon={<LockOutlined />} color="green">자격증명</Tag>
            </Space>
            <Typography.Paragraph type="secondary" style={{ marginTop: 12, fontSize: 12.5 }}>{data.algorithm}. 암호문에는 소유자 정보가 연관 데이터(AAD)로 묶여 있어 다른 사용자의 레코드로 옮겨 붙여도 복호화되지 않습니다.</Typography.Paragraph>
          </Card>
          <Card title="이 키가 보호하는 항목">
            {data.protected_items.length ? data.protected_items.map((p) => <Tag key={p} color="green">{p}</Tag>) : <Typography.Text type="secondary">현재 저장된 항목이 없습니다.</Typography.Text>}
          </Card>
        </Col>
      </Row>
    </>
  )
}
