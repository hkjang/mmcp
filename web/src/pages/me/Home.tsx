import { Alert, Button, Card, Col, List, Row, Skeleton, Space, Statistic, Steps, Tag, Typography } from 'antd'
import { CheckCircleFilled, ExclamationCircleFilled, LinkOutlined, KeyOutlined, ThunderboltOutlined, RightOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { get } from '../../api/client'
import type { Mapping, User } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { StatusTag } from '../../components/common'

interface Overview { user: User; mapping?: Mapping; is_admin: boolean; stats: Record<string, number>; checks: { key: string; ok: boolean; title: string; detail: string; link?: string }[] }

export default function Home() {
  const navigate = useNavigate()
  const { data } = useQuery({ queryKey: ['me', 'overview'], queryFn: () => get<Overview>('/me') })
  if (!data) return <Skeleton active />
  const okCount = data.checks.filter((c) => c.ok).length
  const ready = data.mapping?.status === 'ok'
  return (
    <>
      <PageHeader title={`안녕하세요, ${data.user.display_name || data.user.username}님`} description="내 MCP 연결 상태와 키 보안 점검 결과입니다." />
      {!ready && (
        <Alert type="info" showIcon style={{ marginBottom: 16 }} title="시작하기: Mattermost 계정 연결이 필요합니다"
          action={<Button size="small" type="primary" onClick={() => navigate('/me/mattermost')}>연결하기</Button>}
          description="관리자가 자동 매핑을 켜 두었다면 아래 버튼만 누르면 됩니다. 아니면 Mattermost 개인 액세스 토큰을 등록하세요." />
      )}
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={14}>
          <Card title="시작 가이드" style={{ marginBottom: 16 }}>
            <Steps orientation="vertical" size="small" current={ready ? (data.stats.connected_clients > 0 || data.stats.active_api_keys > 0 ? 3 : 1) : 0} items={[
              { title: 'Mattermost 계정 연결', description: ready ? <span>@{data.mapping?.mm_username} 계정과 연결됨</span> : <Link to="/me/mattermost">계정 연결하기 →</Link> },
              { title: 'AI 클라이언트 연결', description: <span>Claude Code·Codex·Cursor에 MCP 서버를 추가하세요. <Link to="/me/connect">연결 가이드 →</Link></span> },
              { title: '도구 사용', description: <span>"어제 Text2SQL 채널 요약해줘", "kim에게 회의 일정 DM 보내줘" 처럼 요청하세요. <Link to="/me/playground">도구 실행기에서 미리 해보기 →</Link></span> },
            ]} />
          </Card>
          <Row gutter={[16, 16]}>
            {[
              ['최근 7일 도구 호출', data.stats.calls_7d, '/me/activity'], ['실패', data.stats.failures_7d, '/me/activity?result=failure'],
              ['활성 API 키', data.stats.active_api_keys, '/me/keys'], ['연결된 앱', data.stats.connected_clients, '/me/sessions'], ['승인 대기', data.stats.pending_approvals, '/me/approvals'],
            ].map(([t, v, to]) => (
              <Col xs={12} md={8} xl={8} key={String(t)}>
                <Link to={String(to)}><Card size="small" hoverable><Statistic title={t} value={Number(v)} /></Card></Link>
              </Col>
            ))}
          </Row>
        </Col>
        <Col xs={24} xl={10}>
          <Card title={<Space>보안 점검 <Tag color={okCount === data.checks.length ? 'green' : 'orange'}>{okCount}/{data.checks.length}</Tag></Space>} style={{ marginBottom: 16 }}>
            <List dataSource={data.checks} renderItem={(c) => (
              <List.Item actions={c.link ? [<Link key="l" to={c.link}><RightOutlined /></Link>] : undefined}>
                <List.Item.Meta avatar={c.ok ? <CheckCircleFilled className="mmcp-check-ok" style={{ fontSize: 18 }} /> : <ExclamationCircleFilled className="mmcp-check-bad" style={{ fontSize: 18 }} />} title={c.title} description={c.detail} />
              </List.Item>
            )} />
          </Card>
          <Card title="Mattermost 연결">
            {data.mapping ? (
              <Space orientation="vertical">
                <Space><StatusTag status={data.mapping.status} /><Typography.Text strong>@{data.mapping.mm_username || '?'}</Typography.Text>{data.mapping.mm_display_name}</Space>
                <Typography.Text type="secondary" style={{ fontSize: 12.5 }}>자격증명: {data.mapping.credential_type === 'pat' ? `PAT ····${data.mapping.token_last4} (${data.mapping.credential_source === 'auto' ? '자동 발급' : '직접 등록'})` : '없음'}</Typography.Text>
              </Space>
            ) : <Typography.Text type="secondary">아직 연결되지 않았습니다.</Typography.Text>}
            <Space style={{ marginTop: 16 }} wrap>
              <Button icon={<LinkOutlined />} onClick={() => navigate('/me/connect')}>MCP 연결</Button>
              <Button icon={<KeyOutlined />} onClick={() => navigate('/me/keys')}>API 키</Button>
              <Button icon={<ThunderboltOutlined />} onClick={() => navigate('/me/playground')}>도구 실행기</Button>
            </Space>
          </Card>
        </Col>
      </Row>
    </>
  )
}
