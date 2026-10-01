import { Alert, Button, Card, Col, Form, InputNumber, Row, Skeleton, Switch, Typography } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { PageHeader } from '../../components/PageHeader'
import { TagsInput, useSectionForm } from './settingsHooks'

export default function AccessControl() {
  const [form] = Form.useForm()
  const { isLoading, save } = useSectionForm('general', form)
  if (isLoading) return <Skeleton active />
  return (
    <>
      <PageHeader title="접근 제어" description="IP 허용목록, CORS, 로그인 잠금 정책을 설정합니다. 허용목록을 비우면 제한하지 않습니다." extra={<Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => form.submit()}>저장</Button>} />
      <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} name="access-control">
        <Row gutter={16}>
          <Col xs={24} xl={12}>
            <Card title="IP 허용목록" style={{ marginBottom: 16 }}>
              <Alert type="warning" showIcon style={{ marginBottom: 12 }} title="관리자 허용목록을 잘못 지정하면 관리 콘솔에 접근할 수 없게 됩니다. 현재 접속 IP가 포함되는지 확인하세요." />
              <Form.Item name="admin_ip_allowlist" label="관리 콘솔·메트릭 허용 IP/CIDR" extra="예: 10.10.0.0/16, 192.168.1.20"><TagsInput placeholder="10.0.0.0/8" /></Form.Item>
              <Form.Item name="mcp_ip_allowlist" label="MCP 엔드포인트 허용 IP/CIDR"><TagsInput placeholder="10.0.0.0/8" /></Form.Item>
              <Form.Item name="trust_proxy_headers" label="리버스 프록시의 X-Forwarded-For / X-Forwarded-Host 신뢰" valuePropName="checked" extra="mmcp 앞에 Nginx/OpenResty/LB가 있을 때만 켜세요. 켜지 않으면 프록시 IP가 클라이언트 IP로 기록됩니다.">
                <Switch />
              </Form.Item>
            </Card>
            <Card title="CORS">
              <Form.Item name="cors_origins" label="MCP·OAuth 엔드포인트 허용 Origin" extra="브라우저 기반 MCP 클라이언트(사내 AI 포털 등)의 Origin. 같은 Origin은 자동 허용됩니다.">
                <TagsInput placeholder="https://ai-portal.company.co.kr" />
              </Form.Item>
            </Card>
          </Col>
          <Col xs={24} xl={12}>
            <Card title="로그인 정책" style={{ marginBottom: 16 }}>
              <Form.Item name="local_login_enabled" label="로컬 계정 로그인 허용" valuePropName="checked" extra="꺼도 부트스트랩 관리자(BOOTSTRAP_ADMIN)는 비상용으로 로그인할 수 있습니다."><Switch /></Form.Item>
              <Row gutter={12}>
                <Col span={12}><Form.Item name="login_max_failures" label="연속 실패 허용 횟수"><InputNumber min={1} max={100} style={{ width: '100%' }} suffix="회" /></Form.Item></Col>
                <Col span={12}><Form.Item name="lockout_minutes" label="잠금 시간"><InputNumber min={1} max={1440} style={{ width: '100%' }} suffix="분" /></Form.Item></Col>
                <Col span={12}><Form.Item name="session_ttl_hours" label="웹 세션 유효시간"><InputNumber min={1} max={720} style={{ width: '100%' }} suffix="시간" /></Form.Item></Col>
              </Row>
            </Card>
            <Card title="적용되는 보안 기본값">
              <Typography.Paragraph style={{ fontSize: 13, marginBottom: 0 }}>
                · 세션 쿠키 HttpOnly · SameSite=Lax · HTTPS에서 Secure<br />
                · 상태 변경 API는 X-Requested-With 헤더 필수 (CSRF 방어)<br />
                · CSP, X-Frame-Options DENY, HSTS(HTTPS), nosniff<br />
                · MCP Origin 검증 (DNS 리바인딩 방어) · OAuth PKCE S256 필수<br />
                · 리프레시 토큰 재사용 감지 시 토큰 계열 전체 폐기<br />
                · 비밀번호 bcrypt(12), API 키·토큰은 SHA-256 해시로만 저장
              </Typography.Paragraph>
            </Card>
          </Col>
        </Row>
      </Form>
    </>
  )
}
