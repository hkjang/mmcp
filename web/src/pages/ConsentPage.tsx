import { useState } from 'react'
import { Alert, Button, Card, Checkbox, Descriptions, Result, Space, Spin, Tag, Typography } from 'antd'
import { ApiOutlined, CheckCircleOutlined, StopOutlined } from '@ant-design/icons'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { get, post, errorMessage } from '../api/client'
import { useAuth } from '../contexts/AuthContext'
import { LogoMark } from '../components/Logo'
import { fmtTime } from '../utils/format'

interface RequestInfo { client_id: string; client_name: string; redirect_host: string; scope: string; registration: string; client_created_at: string; expires_at: string; remember_available: boolean }

export default function ConsentPage() {
  const [params] = useSearchParams()
  const id = params.get('request') ?? ''
  const { user } = useAuth()
  const [remember, setRemember] = useState(true)
  const [busy, setBusy] = useState<'approve' | 'deny' | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const { data, error, isLoading } = useQuery({ queryKey: ['oauth-request', id], queryFn: () => get<RequestInfo>(`/oauth/requests/${encodeURIComponent(id)}`), enabled: !!id, retry: false })

  const decide = async (approve: boolean) => {
    setBusy(approve ? 'approve' : 'deny')
    setErr(null)
    try {
      const r = await post<{ redirect: string }>(`/oauth/requests/${encodeURIComponent(id)}`, { approve, remember })
      window.location.assign(r.redirect)
    } catch (e) {
      setErr(errorMessage(e))
      setBusy(null)
    }
  }

  const loopback = data?.redirect_host.startsWith('http://127.0.0.1') || data?.redirect_host.startsWith('http://localhost')
  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', padding: 16 }}>
      <Card style={{ width: '100%', maxWidth: 520, boxShadow: '0 10px 40px rgba(15,23,42,.08)' }}>
        <Space align="center" style={{ marginBottom: 12 }}>
          <LogoMark size={36} />
          <Typography.Title level={4} style={{ margin: 0 }}>MCP 클라이언트 연결 요청</Typography.Title>
        </Space>
        {isLoading && <Spin />}
        {(error || !id) && <Result status="warning" title="인가 요청이 없거나 만료되었습니다" subTitle={error ? errorMessage(error) : 'MCP 클라이언트에서 연결을 다시 시도하세요.'} />}
        {data && (
          <>
            <Typography.Paragraph>
              <Typography.Text strong>{data.client_name}</Typography.Text>이(가) <Typography.Text strong>{user?.display_name || user?.username}</Typography.Text> 계정으로 mmcp의 Mattermost 도구를 사용하려고 합니다.
            </Typography.Paragraph>
            <Descriptions size="small" column={1} bordered style={{ marginBottom: 16 }} items={[
{ label: "클라이언트", children: <><Space><ApiOutlined />{data.client_name}<Tag>{data.registration === 'dynamic' ? '자동 등록' : '수동 등록'}</Tag></Space>{data.registration === 'dynamic' && <div style={{ fontSize: 12, opacity: 0.65, marginTop: 4 }}>이름은 클라이언트가 스스로 등록한 값입니다. 돌아갈 주소가 내가 쓰는 앱인지 확인하세요.</div>}</> },
{ label: "돌아갈 주소", children: <><Typography.Text code>{data.redirect_host}</Typography.Text> {loopback && <Tag color="green">로컬 앱</Tag>}</> },
{ label: "권한", children: <>{data.scope.split(' ').map((s) => (
                  <Tag key={s} color={s === 'mcp' ? 'blue' : 'default'}>{s === 'mcp' ? 'mcp — 조회 + 승인 기반 작성' : 'mcp:read — 조회 전용'}</Tag>
                ))}</> },
{ label: "요청 만료", children: <>{fmtTime(data.expires_at)}</> },
]} />
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              title="허용하면 이 클라이언트는 내 Mattermost 권한 범위 안에서만 동작합니다. 메시지 전송 같은 쓰기 작업은 매번 확인을 거치며, 언제든 '세션·연결된 앱'에서 연결을 끊을 수 있습니다."
            />
            {data.remember_available && (
              <Checkbox checked={remember} onChange={(e) => setRemember(e.target.checked)} style={{ marginBottom: 16 }}>
                이 클라이언트를 기억하고 다음부터 묻지 않기
              </Checkbox>
            )}
            {err && <Alert type="error" title={err} style={{ marginBottom: 12 }} />}
            <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
              <Button icon={<StopOutlined />} onClick={() => decide(false)} loading={busy === 'deny'} disabled={!!busy}>거부</Button>
              <Button type="primary" icon={<CheckCircleOutlined />} onClick={() => decide(true)} loading={busy === 'approve'} disabled={!!busy} data-testid="consent-approve">허용</Button>
            </Space>
          </>
        )}
      </Card>
    </div>
  )
}
