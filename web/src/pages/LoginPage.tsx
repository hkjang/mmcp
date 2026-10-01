import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Divider, Form, Input, Space, Spin, Typography } from 'antd'
import { LockOutlined, SafetyCertificateOutlined, UserOutlined, ArrowRightOutlined } from '@ant-design/icons'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ApiError, get, post } from '../api/client'
import type { AuthConfig, MeResponse } from '../api/types'
import { useAuth } from '../contexts/AuthContext'
import { LogoMark } from '../components/Logo'
import { appVersion } from '../utils/format'

const ssoMessages: Record<string, string> = {
  error: 'Keycloak 연동 설정을 확인할 수 없습니다. 관리자에게 문의하세요.',
  state: '로그인 요청이 만료되었거나 브라우저 쿠키가 차단되었습니다. 다시 시도하세요.',
  denied: 'Keycloak에서 로그인이 거부되었습니다.',
  exchange: 'Keycloak 토큰 교환에 실패했습니다. 클라이언트 설정(Secret, Redirect URI)을 확인하세요.',
  id_token: 'Keycloak ID 토큰 검증에 실패했습니다.',
  nonce: '로그인 요청 검증(nonce)에 실패했습니다. 다시 시도하세요.',
  provision: 'mmcp 계정을 만들거나 연결하지 못했습니다. 관리자에게 문의하세요.',
  config: 'Keycloak 연동이 설정되지 않았습니다.',
  session: '세션을 만들지 못했습니다.',
}

const SILENT_KEY = 'mmcp.sso.tried'

function safeNext(n: string | null) {
  return n && n.startsWith('/') && !n.startsWith('//') && !n.startsWith('/login') ? n : '/'
}

export default function LoginPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { user, setMe } = useAuth()
  const next = safeNext(params.get('next'))
  const sso = params.get('sso')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [silent, setSilent] = useState(false)
  const { data: cfg, isLoading } = useQuery({ queryKey: ['auth-config'], queryFn: () => get<AuthConfig>('/auth/config') })

  useEffect(() => {
    if (user) navigate(next, { replace: true })
  }, [user, next, navigate])

  // Keycloak silent SSO: if the browser already holds a Keycloak session,
  // a prompt=none round-trip signs the user in (and provisions the mmcp
  // account) without showing any page. Guards: once per minute, never right
  // after an explicit logout, never when Keycloak just said "login_required".
  useEffect(() => {
    if (!cfg || user || !cfg.sso_enabled) return
    let skip = false
    try {
      skip = sessionStorage.getItem('mmcp.sso.skip') === '1'
    } catch {
      /* ignore */
    }
    if (params.get('logged_out') || skip) return
    if (sso === 'none' && cfg.auto_redirect) {
      window.location.assign(`/auth/oidc/login?next=${encodeURIComponent(next)}`)
      return
    }
    if (sso || !cfg.silent_sso) return
    let last = 0
    try {
      last = Number(sessionStorage.getItem(SILENT_KEY) ?? 0)
      sessionStorage.setItem(SILENT_KEY, String(Date.now()))
    } catch {
      /* ignore */
    }
    if (Date.now() - last < 60_000) return
    setSilent(true)
    window.location.assign(`/auth/oidc/login?prompt=none&next=${encodeURIComponent(next)}`)
  }, [cfg, user, sso, next, params])

  const ssoLogin = () => {
    try {
      sessionStorage.removeItem('mmcp.sso.skip')
    } catch {
      /* ignore */
    }
    window.location.assign(`/auth/oidc/login?next=${encodeURIComponent(next)}`)
  }

  const onFinish = async (v: { username: string; password: string }) => {
    setSubmitting(true)
    setError(null)
    try {
      const me = await post<MeResponse>('/auth/login', v)
      try {
        sessionStorage.removeItem('mmcp.sso.skip')
      } catch {
        /* ignore */
      }
      setMe(me)
      navigate(next === '/' ? (me.is_admin ? '/admin' : '/me') : next, { replace: true })
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '로그인에 실패했습니다')
    } finally {
      setSubmitting(false)
    }
  }

  const notice = useMemo(() => {
    if (params.get('expired')) return { type: 'info' as const, text: '세션이 만료되었습니다. 다시 로그인하세요.' }
    if (params.get('logged_out')) return { type: 'success' as const, text: '로그아웃되었습니다.' }
    if (sso && sso !== 'none') return { type: 'error' as const, text: ssoMessages[sso] ?? 'SSO 로그인에 실패했습니다.' }
    return null
  }, [params, sso])

  const version = cfg?.version?.version && cfg.version.version !== 'dev' ? cfg.version.version.replace(/^v/, '') : appVersion
  const isConsent = next.startsWith('/oauth/consent')

  return (
    <div className="mmcp-login">
      <section className="mmcp-login-hero">
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <LogoMark size={40} />
          <div>
            <div style={{ fontWeight: 700, fontSize: 20 }}>mmcp</div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Mattermost MCP Gateway</div>
          </div>
        </div>
        <div>
          <h2>
            AI 에이전트에게 Mattermost를,
            <br />
            사용자 본인의 권한으로.
          </h2>
          <p className="hide-sm">
            Keycloak 로그인 한 번으로 Claude Code·Codex·사내 AI 포털이 Mattermost를 조회하고 DM을 보냅니다. 모든 호출은 사용자에게 매핑된 Mattermost
            계정의 권한으로 실행되고, 쓰기 작업은 승인 후에만 전송되며, 전부 감사 로그에 남습니다.
          </p>
          <div className="mmcp-login-flow hide-sm">
            <span className="node">Keycloak SSO</span>
            <ArrowRightOutlined />
            <span className="node">mmcp 계정 매핑</span>
            <ArrowRightOutlined />
            <span className="node">Mattermost REST v4</span>
          </div>
        </div>
        <div className="hide-sm" style={{ fontSize: 12, opacity: 0.6 }}>오프라인 사내망 전용 배포 · OAuth 2.1 / MCP 2025-11-25</div>
      </section>
      <section className="mmcp-login-panel">
        <Card className="mmcp-login-card" variant="borderless" style={{ boxShadow: '0 10px 40px rgba(15,23,42,.08)' }}>
          {silent || isLoading ? (
            <div style={{ textAlign: 'center', padding: '40px 0' }}>
              <Spin size="large" />
              <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
                {silent ? 'Keycloak 로그인 상태를 확인하는 중…' : '불러오는 중…'}
              </Typography.Paragraph>
            </div>
          ) : (
            <>
              <Typography.Title level={3} style={{ marginTop: 0, marginBottom: 4 }}>
                {isConsent ? 'MCP 클라이언트 연결' : '로그인'}
              </Typography.Title>
              <Typography.Paragraph type="secondary">
                {isConsent ? 'AI 클라이언트를 연결하려면 먼저 로그인하세요.' : 'mmcp에 로그인하여 MCP 연결과 키를 관리하세요.'}
              </Typography.Paragraph>
              {notice && <Alert type={notice.type} title={notice.text} showIcon style={{ marginBottom: 16 }} />}
              {cfg?.login_notice && <Alert type="info" title={cfg.login_notice} style={{ marginBottom: 16 }} />}
              {cfg?.sso_enabled && (
                <Button type="primary" size="large" block icon={<SafetyCertificateOutlined />} onClick={ssoLogin} data-testid="sso-login">
                  {cfg.sso_button_label || 'Keycloak SSO로 로그인'}
                </Button>
              )}
              {cfg?.sso_enabled && cfg?.local_login_enabled && <Divider plain style={{ fontSize: 12 }}>또는 로컬 계정</Divider>}
              {(cfg?.local_login_enabled ?? true) && (
                <Form layout="vertical" onFinish={onFinish} requiredMark={false} disabled={submitting} name="login">
                  <Form.Item name="username" label="아이디" rules={[{ required: true, message: '아이디를 입력하세요' }]}>
                    <Input size="large" prefix={<UserOutlined />} autoComplete="username" autoFocus={!cfg?.sso_enabled} placeholder="아이디" />
                  </Form.Item>
                  <Form.Item name="password" label="비밀번호" rules={[{ required: true, message: '비밀번호를 입력하세요' }]}>
                    <Input.Password size="large" prefix={<LockOutlined />} autoComplete="current-password" placeholder="비밀번호" />
                  </Form.Item>
                  {error && <Alert type="error" title={error} showIcon style={{ marginBottom: 16 }} />}
                  <Button htmlType="submit" size="large" block type={cfg?.sso_enabled ? 'default' : 'primary'} loading={submitting}>
                    로그인
                  </Button>
                </Form>
              )}
              {!cfg?.local_login_enabled && !cfg?.sso_enabled && <Alert type="warning" title="사용 가능한 로그인 방법이 없습니다." />}
            </>
          )}
          <Space style={{ marginTop: 24, width: '100%', justifyContent: 'space-between', fontSize: 12 }} wrap>
            <Typography.Text type="secondary" data-testid="login-version">
              mmcp v{version}
              {cfg?.version?.commit && cfg.version.commit !== 'unknown' ? ` · ${cfg.version.commit.slice(0, 7)}` : ''}
            </Typography.Text>
            <Typography.Text type="secondary">Mattermost MCP Gateway</Typography.Text>
          </Space>
        </Card>
      </section>
    </div>
  )
}
