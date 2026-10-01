import { Card, Col, Descriptions, List, Row, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { get } from '../../api/client'
import type { BuildInfo, Component } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { LogoMark } from '../../components/Logo'
import { compIcon } from './Dashboard'
import { fmtTime } from '../../utils/format'

interface SystemInfo { build: BuildInfo; started_at: string; uptime_seconds: number; database_version: string; migrations: { version: string; applied_at: string }[]; master_keys: string[]; primary_master_key: string; log_buffer: number; log_level: string; tools: number; env: string[] }

const uptime = (s: number) => `${Math.floor(s / 86400)}일 ${Math.floor((s % 86400) / 3600)}시간 ${Math.floor((s % 3600) / 60)}분`

export default function About() {
  const { data } = useQuery({ queryKey: ['admin', 'system'], queryFn: () => get<SystemInfo>('/admin/system') })
  const health = useQuery({ queryKey: ['admin', 'health'], queryFn: () => get<{ components: Component[] }>('/admin/health') })
  if (!data) return <Skeleton active />
  const base = window.location.origin
  return (
    <>
      <PageHeader title="시스템 정보" description="빌드 버전, 런타임, 데이터베이스 스키마와 구성요소 상태입니다." />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card>
            <Space align="center" size={16} style={{ marginBottom: 16 }}>
              <LogoMark size={56} />
              <div>
                <Typography.Title level={3} style={{ margin: 0 }}>mmcp {data.build.version}</Typography.Title>
                <Typography.Text type="secondary">Mattermost MCP Gateway</Typography.Text>
              </div>
            </Space>
            <Descriptions size="small" column={1} bordered items={[
{ label: "버전", children: <>{data.build.version}</> },
{ label: "커밋", children: <>{data.build.commit}</> },
{ label: "빌드 시각", children: <>{data.build.build_time}</> },
{ label: "Go", children: <>{data.build.go_version}</> },
{ label: "시작", children: <>{fmtTime(data.started_at)} ({uptime(data.uptime_seconds)})</> },
{ label: "PostgreSQL", children: <>{data.database_version}</> },
{ label: "마스터 키", children: <>{data.master_keys.map((k) => <Tag key={k} color={k === data.primary_master_key ? 'blue' : 'default'}>{k}</Tag>)}</> },
{ label: "도구 수", children: <>{data.tools}</> },
{ label: "로그", children: <>레벨 {data.log_level} · 버퍼 {data.log_buffer.toLocaleString()}줄</> },
{ label: "환경변수", children: <>{data.env.map((e) => <Tag key={e}>{e}</Tag>)}</> },
]} />
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="구성요소" style={{ marginBottom: 16 }}>
            <List size="small" loading={health.isLoading} dataSource={health.data?.components} renderItem={(c) => (
              <List.Item><List.Item.Meta avatar={compIcon(c.status)} title={c.label} description={c.message} />{c.latency_ms > 0 && `${c.latency_ms}ms`}</List.Item>
            )} />
          </Card>
          <Card title="운영 엔드포인트" style={{ marginBottom: 16 }}>
            <List size="small" dataSource={[
              ['/api/health', '라이브니스'], ['/api/ready', '레디니스 (DB + Keycloak JWKS + Mattermost ping)'], ['/metrics', 'Prometheus 메트릭'],
              ['/api/openapi.json', 'REST API 명세 (OpenAPI 3.1)'], ['/.well-known/oauth-protected-resource/mcp', 'MCP 보호 리소스 메타데이터'], ['/.well-known/oauth-authorization-server', 'OAuth 인가 서버 메타데이터'],
            ]} renderItem={([p, d]) => <List.Item><a href={`${base}${p}`} target="_blank" rel="noreferrer"><code>{p}</code></a><Typography.Text type="secondary" style={{ fontSize: 12 }}>{d}</Typography.Text></List.Item>} />
          </Card>
          <Card title="스키마 마이그레이션">
            <Table size="small" rowKey="version" pagination={false} dataSource={data.migrations} columns={[{ title: '버전', dataIndex: 'version' }, { title: '적용', dataIndex: 'applied_at', render: (v) => fmtTime(v) }]} />
          </Card>
        </Col>
      </Row>
    </>
  )
}
