import { Alert, Card, Col, Empty, List, Progress, Row, Skeleton, Space, Statistic, Table, Tag, Typography, Button, Tooltip } from 'antd'
import { ReloadOutlined, CheckCircleFilled, CloseCircleFilled, ExclamationCircleFilled, MinusCircleFilled } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { get } from '../../api/client'
import type { Component } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { HourlyBars, type HourPoint } from '../../components/MiniBars'
import { TimeText } from '../../components/common'

interface DashboardData {
  counts: Record<string, number>
  today: { calls: number; errors: number; by_bucket: Record<string, number> }
  hourly: HourPoint[]
  top_tools: { tool: string; calls: number; avg_latency_ms: number }[]
  top_users: { user: string; calls: number }[]
  recent_failures: { id: number; ts: string; category: string; action: string; actor: string; error_code: string }[]
  components: Component[]
  warnings: string[]
}

export const compIcon = (s: string) =>
  s === 'up' ? <CheckCircleFilled style={{ color: '#16a34a' }} /> : s === 'down' ? <CloseCircleFilled style={{ color: '#dc2626' }} /> : s === 'warn' ? <ExclamationCircleFilled style={{ color: '#f59e0b' }} /> : <MinusCircleFilled style={{ color: '#94a3b8' }} />

export default function Dashboard() {
  const { data, isLoading, refetch, isFetching } = useQuery({ queryKey: ['admin', 'dashboard'], queryFn: () => get<DashboardData>('/admin/dashboard'), refetchInterval: 30_000 })
  if (isLoading || !data) return <Skeleton active paragraph={{ rows: 12 }} />
  const c = data.counts
  const mappedPct = c.users ? Math.round((c.mapped / Math.max(1, c.active_users)) * 100) : 0
  const maxTool = Math.max(1, ...data.top_tools.map((t) => t.calls))
  return (
    <>
      <PageHeader
        title="대시보드"
        description="MCP 호출 현황과 연동 구성요소 상태를 한눈에 봅니다. 30초마다 자동 갱신됩니다."
        extra={<Button icon={<ReloadOutlined />} loading={isFetching} onClick={() => refetch()}>새로고침</Button>}
      />
      {data.warnings?.length > 0 && (
        <Alert type="warning" showIcon style={{ marginBottom: 16 }} title="확인이 필요한 항목" description={<ul style={{ margin: 0, paddingLeft: 18 }}>{data.warnings.map((w) => <li key={w}>{w}</li>)}</ul>} />
      )}
      <Row gutter={[16, 16]}>
        {[
          { t: '사용자', v: c.users, s: `활성 ${c.active_users} · SSO ${c.sso_users}`, to: '/admin/users' },
          { t: '매핑 완료', v: c.mapped, s: <Progress percent={mappedPct} size="small" showInfo={false} />, to: '/admin/mappings' },
          { t: '매핑 오류', v: c.mapping_errors, s: '없음 · 충돌 · 대기', to: '/admin/mappings?status=conflict', danger: c.mapping_errors > 0 },
          { t: '오늘 도구 호출', v: data.today.calls, s: `실패 ${data.today.errors}`, to: '/admin/audit?category=tool' },
          { t: 'MCP 클라이언트', v: c.mcp_clients, s: `활성 세션 ${c.active_sessions}`, to: '/admin/mcp/clients' },
          { t: '승인 대기', v: c.pending_approvals, s: `활성 API 키 ${c.api_keys}`, to: '/admin/approvals' },
        ].map((x) => (
          <Col xs={12} md={8} xl={4} key={x.t}>
            <Link to={x.to}>
              <Card className="mmcp-stat" hoverable size="small">
                <Statistic title={x.t} value={x.v} valueStyle={x.danger ? { color: '#dc2626' } : undefined} />
                <div style={{ fontSize: 12, opacity: 0.65, marginTop: 4 }}>{x.s}</div>
              </Card>
            </Link>
          </Col>
        ))}
        <Col xs={24} xl={16}>
          <Card title="최근 24시간 도구 호출" extra={<Space size={12} style={{ fontSize: 12 }}><Tag color="blue">성공</Tag><Tag color="red">실패</Tag><Tag color="gold">거부·승인대기</Tag></Space>}>
            <HourlyBars data={data.hourly} />
            <Row gutter={16} style={{ marginTop: 16 }}>
              {['read', 'search', 'write', 'file'].map((b) => (
                <Col span={6} key={b}>
                  <Statistic title={{ read: '조회', search: '검색', write: '작성', file: '파일' }[b]} value={data.today.by_bucket[b] ?? 0} suffix="건" styles={{ content: { fontSize: 20 } }} />
                </Col>
              ))}
            </Row>
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card title="구성요소 상태" extra={<Link to="/admin/system/about">자세히</Link>}>
            <List
              size="small"
              dataSource={data.components}
              renderItem={(cp) => (
                <List.Item>
                  <List.Item.Meta avatar={compIcon(cp.status)} title={cp.label} description={<Typography.Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: cp.message }}>{cp.message}</Typography.Text>} />
                  {cp.latency_ms > 0 && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{cp.latency_ms}ms</Typography.Text>}
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col xs={24} lg={12} xl={8}>
          <Card title="많이 쓰는 도구 (7일)">
            {data.top_tools.length === 0 ? <Empty description="호출 기록이 없습니다" /> : data.top_tools.map((t) => (
              <div key={t.tool} style={{ marginBottom: 10 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12.5 }}>
                  <span className="mmcp-tool-name">{t.tool}</span>
                  <span>{t.calls}건 · {t.avg_latency_ms}ms</span>
                </div>
                <Progress percent={(t.calls / maxTool) * 100} showInfo={false} size="small" />
              </div>
            ))}
          </Card>
        </Col>
        <Col xs={24} lg={12} xl={6}>
          <Card title="활발한 사용자 (7일)">
            {data.top_users.length === 0 ? <Empty description="기록 없음" /> : (
              <List size="small" dataSource={data.top_users} renderItem={(u, i) => <List.Item><span>{i + 1}. {u.user}</span><Tag>{u.calls}</Tag></List.Item>} />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={10}>
          <Card title="최근 실패·거부" extra={<Link to="/admin/audit?result=failure">감사 로그</Link>}>
            <Table
              size="small"
              rowKey="id"
              pagination={false}
              scroll={{ x: 420 }}
              dataSource={data.recent_failures}
              locale={{ emptyText: '최근 실패가 없습니다' }}
              columns={[
                { title: '시각', dataIndex: 'ts', width: 90, render: (v) => <TimeText value={v} /> },
                { title: '작업', dataIndex: 'action', render: (v, r) => <Tooltip title={r.category}><span className="mmcp-tool-name">{v}</span></Tooltip> },
                { title: '사용자', dataIndex: 'actor', width: 90, ellipsis: true },
                { title: '코드', dataIndex: 'error_code', render: (v) => v && <Tag color="red">{v}</Tag> },
              ]}
            />
          </Card>
        </Col>
      </Row>
    </>
  )
}
