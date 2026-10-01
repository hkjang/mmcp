import { useState } from 'react'
import { Alert, App, Button, Card, Col, Descriptions, Form, Input, Popconfirm, Result, Row, Skeleton, Space, Steps, Tag, Typography } from 'antd'
import { LinkOutlined, SyncOutlined, DeleteOutlined, SaveOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { del, get, post, put, errorMessage } from '../../api/client'
import type { Mapping, ToolError } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { Mono, StatusTag } from '../../components/common'
import { fmtTime, methodLabel } from '../../utils/format'

interface MMInfo { mapping?: Mapping; configured: boolean; site_url: string; auto_issue: boolean; auto_discovery: boolean; send_identity: string; bot_available: boolean }

export default function MyMattermost() {
  const qc = useQueryClient()
  const { message } = App.useApp()
  const [form] = Form.useForm()
  const [linkErr, setLinkErr] = useState<ToolError | null>(null)
  const { data } = useQuery({ queryKey: ['me', 'mattermost'], queryFn: () => get<MMInfo>('/me/mattermost') })
  const done = (r: { mapping?: Mapping; error?: ToolError }) => {
    setLinkErr(r.error ?? null)
    if (r.error) message.warning(r.error.message, 6)
    else message.success('처리했습니다')
    void qc.invalidateQueries({ queryKey: ['me'] })
  }
  const link = useMutation({ mutationFn: () => post<{ mapping?: Mapping; error?: ToolError }>('/me/mattermost/link'), onSuccess: done, onError: (e) => message.error(errorMessage(e)) })
  const register = useMutation({ mutationFn: (token: string) => put<{ mapping: Mapping }>('/me/mattermost/token', { token }), onSuccess: (r) => { form.resetFields(); done(r) }, onError: (e) => message.error(errorMessage(e), 8) })
  const rotate = useMutation({ mutationFn: () => post<{ mapping: Mapping }>('/me/mattermost/token/rotate'), onSuccess: done, onError: (e) => message.error(errorMessage(e), 8) })
  const clear = useMutation({ mutationFn: () => del<{ mapping: Mapping }>('/me/mattermost/token'), onSuccess: done, onError: (e) => message.error(errorMessage(e)) })
  if (!data) return <Skeleton active />
  if (!data.configured) return <Result status="info" title="Mattermost 연동이 아직 설정되지 않았습니다" subTitle="서비스 관리자에게 문의하세요." />
  const m = data.mapping
  const ok = m?.status === 'ok'
  const hasCred = m?.credential_type === 'pat'
  return (
    <>
      <PageHeader title="Mattermost 연결" description="내 mmcp 계정을 Mattermost 계정에 연결합니다. 조회·검색·DM 읽기는 항상 이 연결의 권한으로만 실행됩니다." />
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={13}>
          <Card title="연결 상태" extra={m && <StatusTag status={m.status} />} style={{ marginBottom: 16 }}>
            {m ? (
              <Descriptions size="small" column={1} bordered items={[
{ label: "Mattermost 계정", children: <>{m.mm_username ? <span>@{m.mm_username} {m.mm_display_name}</span> : '-'}</> },
{ label: "user_id", children: <>{m.mm_user_id ? <Mono>{m.mm_user_id}</Mono> : '-'}</> },
{ label: "연결 방식", children: <>{methodLabel[m.method]}</> },
{ label: "자격증명", children: <>{hasCred ? <span>개인 액세스 토큰 ····{m.token_last4} <Tag>{m.credential_source === 'auto' ? '자동 발급' : '직접 등록'}</Tag> v{m.token_version}</span> : data.auto_issue ? <Tag color="cyan">첫 사용 시 자동 발급</Tag> : <Tag color="orange">등록 필요</Tag>}</> },
{ label: "토큰 교체", children: <>{fmtTime(m.token_rotated_at)}</> },
{ label: "마지막 검증", children: <>{fmtTime(m.verified_at)}</> },
]} />
            ) : <Typography.Paragraph type="secondary">아직 연결 기록이 없습니다.</Typography.Paragraph>}
            {(linkErr || m?.last_error) && <Alert type={m?.status === 'conflict' ? 'error' : 'warning'} showIcon style={{ marginTop: 12 }} title={linkErr?.message ?? m?.last_error} />}
            <Space style={{ marginTop: 16 }} wrap>
              <Button type={ok ? 'default' : 'primary'} icon={<LinkOutlined />} loading={link.isPending} onClick={() => link.mutate()} disabled={!data.auto_discovery && !hasCred}>
                {ok ? '연결 다시 확인' : '자동으로 연결하기'}
              </Button>
              {ok && data.auto_issue && <Button icon={<SyncOutlined />} loading={rotate.isPending} onClick={() => rotate.mutate()}>{hasCred && m?.credential_source === 'auto' ? '토큰 회전' : '토큰 자동 발급'}</Button>}
              {hasCred && (
                <Popconfirm title="저장된 토큰을 삭제할까요? 자동 발급된 토큰은 Mattermost에서도 폐기됩니다." onConfirm={() => clear.mutate()}>
                  <Button danger icon={<DeleteOutlined />}>토큰 삭제</Button>
                </Popconfirm>
              )}
            </Space>
            {!data.auto_discovery && !hasCred && <Alert type="info" style={{ marginTop: 12 }} showIcon title="관리자가 자동 연결을 설정하지 않았습니다. 오른쪽 안내에 따라 개인 액세스 토큰을 등록하면 연결됩니다." />}
          </Card>
          <Card title={hasCred ? '토큰 교체 (직접 등록)' : '개인 액세스 토큰 등록'}>
            <Form form={form} layout="vertical" onFinish={(v: { token: string }) => register.mutate(v.token)} name="register-mm-token">
              <Form.Item name="token" label="Mattermost 개인 액세스 토큰" rules={[{ required: true, message: '토큰을 붙여 넣으세요' }, { min: 20, message: '토큰이 너무 짧습니다' }]}
                extra="토큰은 내 개인 데이터 키로 암호화(AES-256-GCM)되어 저장되며, 화면에 다시 표시되지 않습니다. 토큰 소유자가 내 계정과 같은지 확인한 뒤 저장합니다.">
                <Input.Password placeholder="xxxxxxxxxxxxxxxxxxxxxxxxxx" autoComplete="off" />
              </Form.Item>
              <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={register.isPending}>검증 후 저장</Button>
            </Form>
          </Card>
        </Col>
        <Col xs={24} xl={11}>
          <Card title="개인 액세스 토큰 만드는 법" style={{ marginBottom: 16 }}>
            <Steps orientation="vertical" size="small" current={-1} items={[
              { title: 'Mattermost 접속', description: <a href={data.site_url} target="_blank" rel="noreferrer">{data.site_url}</a> },
              { title: '프로필 → 보안 → 개인 액세스 토큰', description: '메뉴가 없으면 관리자에게 토큰 생성 권한을 요청하세요.' },
              { title: '토큰 만들기', description: '설명에 "mmcp"라고 적고 생성된 토큰을 복사합니다.' },
              { title: '왼쪽에 붙여 넣고 저장', description: '이전 토큰을 교체했다면 Mattermost에서 이전 토큰을 비활성화하세요.' },
            ]} />
          </Card>
          <Card title="메시지는 누구 명의로 보내지나요?">
            <Typography.Paragraph style={{ fontSize: 13.5 }}>
              {data.send_identity === 'user' && '내 Mattermost 계정 명의로 보냅니다. 자격증명이 필요합니다.'}
              {data.send_identity === 'bot' && '봇 계정(AI Assistant)이 보내고, 메시지에 "○○님의 요청" 표시가 붙습니다.'}
              {data.send_identity === 'auto' && '자격증명이 있으면 내 명의로, 없으면 봇 계정이 "○○님의 요청"으로 보냅니다.'}
            </Typography.Paragraph>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12.5, marginBottom: 0 }}>모든 발송은 기본적으로 승인 단계를 거치며, 요청자 정보가 감사 로그와 메시지 props(requested_by)에 기록됩니다.</Typography.Paragraph>
          </Card>
        </Col>
      </Row>
    </>
  )
}
