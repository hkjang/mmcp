import { Alert, Card, Col, Descriptions, Row, Skeleton, Space, Tabs, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { get } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { CodeBlock, CopyButton } from '../../components/common'

interface ConnectInfo { mcp_url: string; resource_metadata: string; authorization_server: string; oauth_mode: string; dcr_enabled: boolean; keycloak_issuer: string; keycloak_jwt: boolean; keycloak_client_id: string; protocol_versions: string[] }

export default function Connect() {
  const { data } = useQuery({ queryKey: ['me', 'connect'], queryFn: () => get<ConnectInfo>('/me/connect') })
  if (!data) return <Skeleton active />
  const url = data.mcp_url
  const tabs = [
    {
      key: 'claude', label: 'Claude Code',
      children: (
        <>
          <Typography.Paragraph>OAuth로 연결합니다. 처음 도구를 쓸 때 브라우저가 열리고, Keycloak에 이미 로그인되어 있으면 <b>허용</b> 한 번으로 끝납니다.</Typography.Paragraph>
          <CodeBlock code={`claude mcp add --transport http mmcp ${url}`} />
          <Typography.Paragraph style={{ marginTop: 12 }}>Claude Code 안에서 <code>/mcp</code> → <b>mmcp</b> → Authenticate 를 선택합니다. API 키를 쓰려면:</Typography.Paragraph>
          <CodeBlock code={`claude mcp add --transport http mmcp ${url} \\\n  --header "Authorization: Bearer mmcp_pk_…"`} />
        </>
      ),
    },
    {
      key: 'codex', label: 'Codex CLI',
      children: (
        <>
          <Typography.Paragraph><code>~/.codex/config.toml</code>에 추가하고 API 키를 환경변수로 넘깁니다.</Typography.Paragraph>
          <CodeBlock code={`[mcp_servers.mmcp]\nurl = "${url}"\nbearer_token_env_var = "MMCP_API_KEY"`} />
          <CodeBlock code={'export MMCP_API_KEY="mmcp_pk_…"\ncodex'} />
          <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>OAuth를 지원하는 Codex 버전에서는 <code>codex mcp add mmcp --url {url}</code> 후 <code>codex mcp login mmcp</code>를 사용할 수 있습니다.</Typography.Paragraph>
        </>
      ),
    },
    {
      key: 'cursor', label: 'Cursor',
      children: <><Typography.Paragraph><code>~/.cursor/mcp.json</code> (OAuth 자동 처리)</Typography.Paragraph><CodeBlock code={JSON.stringify({ mcpServers: { mmcp: { url } } }, null, 2)} /></>,
    },
    {
      key: 'vscode', label: 'VS Code',
      children: <><Typography.Paragraph><code>.vscode/mcp.json</code> (GitHub Copilot Agent 모드)</Typography.Paragraph><CodeBlock code={JSON.stringify({ servers: { mmcp: { type: 'http', url } } }, null, 2)} /></>,
    },
    {
      key: 'portal', label: 'AI 포털 · 서버 간',
      children: (
        <>
          <Typography.Paragraph>이미 Keycloak 토큰을 가진 사내 서비스(QwenPaw, AI 포털)는 그 액세스 토큰을 그대로 Bearer로 보냅니다. mmcp가 JWKS로 검증하고 <code>preferred_username</code>으로 사용자를 찾습니다.</Typography.Paragraph>
          {!data.keycloak_jwt && <Alert type="warning" showIcon style={{ marginBottom: 12 }} title="관리자가 Keycloak 토큰 직접 인증을 꺼 두었습니다." />}
          <CodeBlock code={`curl -s ${url} \\\n  -H "Authorization: Bearer $KEYCLOAK_ACCESS_TOKEN" \\\n  -H "Content-Type: application/json" \\\n  -H "Accept: application/json, text/event-stream" \\\n  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`} />
          <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>토큰의 aud 또는 azp에 <code>{data.keycloak_client_id || 'mmcp'}</code>가 포함되어야 합니다.</Typography.Paragraph>
        </>
      ),
    },
    {
      key: 'curl', label: '테스트 (curl)',
      children: (
        <CodeBlock code={`# 1) initialize\ncurl -si ${url} -H "Authorization: Bearer mmcp_pk_…" \\\n  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \\\n  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"curl","version":"1"},"capabilities":{}}}'\n\n# 2) 도구 호출\ncurl -s ${url} -H "Authorization: Bearer mmcp_pk_…" -H "Content-Type: application/json" \\\n  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mattermost_unread","arguments":{}}}'`} />
      ),
    },
  ]
  return (
    <>
      <PageHeader title="MCP 연결 가이드" description="mmcp는 MCP Streamable HTTP 서버입니다. OAuth(권장) 또는 개인 API 키로 연결합니다." />
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <Card title="MCP 서버 주소" style={{ marginBottom: 16 }}>
            <Space.Compact style={{ width: '100%' }}>
              <Typography.Text code style={{ flex: 1, padding: '6px 10px', fontSize: 15, margin: 0 }} data-testid="mcp-url">{url}</Typography.Text>
              <CopyButton text={url} label="복사" size="middle" />
            </Space.Compact>
          </Card>
          <Card><Tabs items={tabs} /></Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card title="인증 방식" style={{ marginBottom: 16 }}>
            <Descriptions size="small" column={1} items={[
{ label: "OAuth 2.1", children: <>{data.oauth_mode === 'gateway' ? <Tag color="blue">mmcp 게이트웨이{data.dcr_enabled ? ' · DCR' : ''}</Tag> : <Tag color="purple">Keycloak 직접</Tag>}</> },
{ label: "개인 API 키", children: <><Link to="/me/keys">mmcp_pk_… 발급</Link></> },
{ label: "Keycloak 토큰", children: <>{data.keycloak_jwt ? <Tag color="green">허용</Tag> : <Tag>꺼짐</Tag>}</> },
{ label: "프로토콜", children: <>{data.protocol_versions.join(', ')}</> },
]} />
          </Card>
          <Card title="메타데이터">
            <Typography.Paragraph style={{ fontSize: 12.5 }}>보호 리소스 (RFC 9728)</Typography.Paragraph>
            <CodeBlock code={data.resource_metadata} />
            <Typography.Paragraph style={{ fontSize: 12.5, marginTop: 12 }}>인가 서버 (RFC 8414)</Typography.Paragraph>
            <CodeBlock code={data.authorization_server} />
          </Card>
        </Col>
      </Row>
    </>
  )
}
