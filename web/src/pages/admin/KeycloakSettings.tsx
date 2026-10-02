import { useState } from 'react'
import { Alert, App, Button, Card, Col, Form, Input, Row, Space, Switch, Typography, Descriptions, Tag, Skeleton } from 'antd'
import { ApiOutlined, SaveOutlined } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { post, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { CodeBlock } from '../../components/common'
import { TagsInput, useSectionForm, useSettings } from './settingsHooks'
import { CheckCircleFilled, CloseCircleFilled } from '@ant-design/icons'

interface TestResult { ok: boolean; message: string; checks?: { name: string; ok: boolean; message: string }[]; uris?: { redirect_uri: string; post_logout_redirect_uri: string }; latency_ms?: number; jwks_keys?: number; authorization_endpoint?: string; token_endpoint?: string; end_session_endpoint?: string; pkce_methods?: string[]; jwks_uri?: string }

export default function KeycloakSettings() {
  const [form] = Form.useForm()
  const { values, isLoading, save } = useSectionForm('keycloak', form)
  const { message } = App.useApp()
  const [result, setResult] = useState<TestResult | null>(null)
  const test = useMutation({ mutationFn: () => post<TestResult>('/admin/settings/keycloak/test'), onSuccess: setResult, onError: (e) => message.error(errorMessage(e)) })
  const enabled = Form.useWatch('enabled', form)
  const { data: all } = useSettings()
  const callback = all?.keycloak_redirect_uri ?? `${window.location.origin}/auth/oidc/callback`
  const postLogout = all?.keycloak_post_logout_uri ?? `${window.location.origin}/login?logged_out=1`
  const serverBase = all?.public_base ?? window.location.origin
  if (isLoading) return <Skeleton active />
  return (
    <>
      <PageHeader
        title="Keycloak 연동"
        description="Keycloak은 mmcp 웹 로그인(사일런트 SSO 포함)과 MCP 토큰 검증(JWKS)을 담당합니다. 저장 후 '연결 테스트'로 Discovery와 JWKS를 확인하세요."
        extra={<>
          <Button icon={<ApiOutlined />} loading={test.isPending} onClick={() => test.mutate()} disabled={!values?.enabled}>연결 테스트</Button>
          <Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => form.submit()}>저장</Button>
        </>}
      />
      <Row gutter={16}>
        <Col xs={24} xl={15}>
          <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} name="keycloak-settings" scrollToFirstError>
            <Card title="연결" style={{ marginBottom: 16 }}>
              <Form.Item name="enabled" label="Keycloak SSO 사용" valuePropName="checked"><Switch /></Form.Item>
              <Form.Item name="issuer_url" label="Issuer URL" rules={[{ required: enabled, message: 'Issuer URL이 필요합니다' }, { type: 'url', message: 'http(s) URL을 입력하세요' }]} extra="예: https://sso.company.co.kr/realms/company">
                <Input placeholder="https://sso.company.co.kr/realms/company" autoComplete="off" />
              </Form.Item>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <Form.Item name="client_id" label="Client ID" rules={[{ required: enabled, message: 'Client ID가 필요합니다' }]}><Input placeholder="mmcp" autoComplete="off" /></Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item name="client_secret" label="Client Secret" extra="저장된 값은 ******** 로 표시됩니다. 바꿀 때만 입력하세요.">
                    <Input.Password placeholder="confidential client secret" autoComplete="new-password" />
                  </Form.Item>
                </Col>
              </Row>
              <Form.Item name="scopes" label="Scopes"><TagsInput placeholder="openid profile email" /></Form.Item>
              <Row gutter={12}>
                <Col xs={24} md={12}><Form.Item name="insecure_tls" label="TLS 인증서 검증 생략 (테스트 전용)" valuePropName="checked"><Switch /></Form.Item></Col>
              </Row>
              <Form.Item name="ca_pem" label="사설 CA 인증서 (PEM)" extra="사내 CA로 서명된 Keycloak을 쓰는 오프라인망이라면 루트 CA를 붙여 넣으세요.">
                <Input.TextArea rows={3} placeholder="-----BEGIN CERTIFICATE-----" style={{ fontFamily: 'monospace', fontSize: 12 }} />
              </Form.Item>
            </Card>
            <Card title="사용자·역할" style={{ marginBottom: 16 }}>
              <Row gutter={12}>
                <Col xs={24} md={12}><Form.Item name="username_claim" label="사용자명 클레임" extra="Mattermost username과 매칭할 클레임"><Input placeholder="preferred_username" /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item name="email_fallback" label="클레임이 없으면 이메일 앞부분 사용" valuePropName="checked"><Switch /></Form.Item></Col>
              </Row>
              <Row gutter={12}>
                <Col xs={24} md={12}><Form.Item name="auto_provision" label="첫 로그인 시 mmcp 사용자 자동 생성" valuePropName="checked"><Switch /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item name="sync_roles" label="로그인마다 Keycloak 역할 동기화" valuePropName="checked"><Switch /></Form.Item></Col>
              </Row>
              <Form.Item name="admin_roles" label="관리자로 승격할 Keycloak 역할" extra="이 역할을 가진 사용자는 mmcp 서비스 관리자(admin)가 됩니다."><TagsInput placeholder="mcp-admin" /></Form.Item>
              <Form.Item name="default_roles" label="모든 SSO 사용자에게 부여할 기본 역할" extra="mcp-* 이름의 Keycloak 역할(realm/client)은 그대로 mmcp 역할로 반영됩니다."><TagsInput placeholder="mcp-user" options={['mcp-user', 'mcp-writer', 'mcp-file', 'mcp-channel-admin']} /></Form.Item>
            </Card>
            <Card title="로그인 경험" style={{ marginBottom: 16 }}>
              <Row gutter={12}>
                <Col xs={24} md={12}><Form.Item name="silent_sso" label="사일런트 SSO" valuePropName="checked" extra="Keycloak에 이미 로그인되어 있으면 화면 없이 바로 로그인합니다 (prompt=none)."><Switch /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item name="auto_redirect" label="로그인 화면 대신 Keycloak으로 바로 이동" valuePropName="checked" extra="사일런트 SSO 실패 시 Keycloak 로그인 화면으로 보냅니다."><Switch /></Form.Item></Col>
              </Row>
              <Row gutter={12}>
                <Col xs={24} md={12}><Form.Item name="button_label" label="로그인 버튼 문구"><Input maxLength={40} /></Form.Item></Col>
                <Col xs={24} md={12}><Form.Item name="end_session_on_logout" label="로그아웃 시 Keycloak 세션도 종료" valuePropName="checked"><Switch /></Form.Item></Col>
              </Row>
            </Card>
            <Card title="MCP 토큰 직접 인증">
              <Form.Item name="accept_jwt_bearer" label="Keycloak 액세스 토큰을 /mcp Bearer로 허용" valuePropName="checked" extra="AI 포털처럼 이미 Keycloak 토큰을 가진 서비스가 그대로 호출할 수 있습니다. JWKS로 서명·만료·발급자를 검증합니다.">
                <Switch />
              </Form.Item>
              <Form.Item name="jwt_audiences" label="허용 audience / azp" extra="비우면 Client ID만 허용합니다. Keycloak에 Audience 매퍼를 추가해 aud에 이 값을 넣으세요."><TagsInput placeholder="mmcp" /></Form.Item>
            </Card>
          </Form>
        </Col>
        <Col xs={24} xl={9}>
          {result && (
            <Card title="연결 테스트" style={{ marginBottom: 16 }}>
              <Alert type={result.ok ? 'success' : 'error'} showIcon title={result.message} style={{ marginBottom: 12 }} />
              {result.checks?.map((c) => (
                  <div key={c.name} style={{ display: 'flex', gap: 8, alignItems: 'flex-start', marginBottom: 8, fontSize: 13 }}>
                    {c.ok ? <CheckCircleFilled style={{ color: '#16a34a', marginTop: 3 }} /> : <CloseCircleFilled style={{ color: '#dc2626', marginTop: 3 }} />}
                    <span style={{ wordBreak: 'break-all' }}>{c.message}</span>
                  </div>
                ))}
              {result.ok && (
                <Descriptions size="small" column={1} items={[
{ label: "응답", children: <>{result.latency_ms}ms</> },
{ label: "JWKS 키", children: <>{result.jwks_keys}개</> },
{ label: "PKCE", children: <>{result.pkce_methods?.map((p) => <Tag key={p}>{p}</Tag>)}</> },
{ label: "로그아웃", children: <>{result.end_session_endpoint ? '지원' : '미지원'}</> },
]} />
              )}
            </Card>
          )}
          <Card title="Keycloak 클라이언트 설정 가이드">
            <Typography.Paragraph>Realm에 OIDC 클라이언트를 만들고 아래처럼 설정합니다. 아래 주소는 mmcp가 실제로 Keycloak에 보내는 값입니다.</Typography.Paragraph>
            {!all?.settings.general.public_url && (
              <Alert type="warning" showIcon style={{ marginBottom: 12 }} title="Public URL이 비어 있습니다"
                description={<>현재 요청 기준으로 <code>{serverBase}</code> 를 사용하고 있습니다. 리버스 프록시 뒤라면 <b>일반 설정 &gt; Public URL</b>에 사용자가 접속하는 주소(https://…)를 지정해야 Keycloak 'Invalid parameter: redirect_uri' 오류와 MCP OAuth 실패를 막을 수 있습니다.</>} />
            )}
            {all?.settings.general.public_url && serverBase !== window.location.origin && (
              <Alert type="info" showIcon style={{ marginBottom: 12 }} title={`Public URL(${serverBase})과 지금 접속한 주소(${window.location.origin})가 다릅니다`} description="Keycloak에는 Public URL 기준 주소가 등록되어야 합니다." />
            )}
            <Descriptions size="small" column={1} bordered items={[
{ label: "Client authentication", children: <>ON (confidential)</> },
{ label: "Standard flow", children: <>ON · PKCE S256</> },
{ label: "Direct access grants", children: <>OFF 권장</> },
{ label: "Access token 수명", children: <>5~15분</> },
]} />
            <Typography.Paragraph style={{ marginTop: 12, marginBottom: 4 }}>Valid redirect URI</Typography.Paragraph>
            <CodeBlock code={callback} />
            <Typography.Paragraph style={{ marginTop: 12, marginBottom: 4 }}>Valid post logout redirect URI</Typography.Paragraph>
            <CodeBlock code={postLogout} />
            <Typography.Paragraph style={{ marginTop: 12, marginBottom: 4 }}>권장 Realm 역할</Typography.Paragraph>
            <Space size={[4, 4]} wrap>{['mcp-user', 'mcp-writer', 'mcp-file', 'mcp-channel-admin', 'mcp-admin'].map((r) => <Tag key={r}>{r}</Tag>)}</Space>
            <Alert style={{ marginTop: 12 }} type="info" showIcon title="사일런트 SSO는 브라우저의 최상위 리다이렉트(prompt=none)로 동작하므로 서드파티 쿠키 차단의 영향을 받지 않습니다." />
          </Card>
        </Col>
      </Row>
    </>
  )
}
