import { Alert, Button, Card, Col, Form, Input, InputNumber, Radio, Row, Skeleton, Switch } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { PageHeader } from '../../components/PageHeader'
import { useSectionForm } from './settingsHooks'

const num = (name: string, label: string, min: number, max: number, extra?: string, suffix?: string) => (
  <Form.Item name={name} label={label} extra={extra}>
    <InputNumber min={min} max={max} style={{ width: '100%' }} suffix={suffix} />
  </Form.Item>
)
const sw = (name: string, label: string, extra?: string) => (
  <Form.Item name={name} label={label} valuePropName="checked" extra={extra}><Switch /></Form.Item>
)

export default function Policies() {
  const [form] = Form.useForm()
  const { isLoading, save } = useSectionForm('mcp', form)
  if (isLoading) return <Skeleton active />
  return (
    <>
      <PageHeader title="데이터·호출 정책" description="도구가 접근할 수 있는 데이터 범위, 호출 한도, MCP OAuth 동작, API 키 정책을 설정합니다." extra={<Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => form.submit()}>저장</Button>} />
      <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} name="mcp-policies">
        <Row gutter={16}>
          <Col xs={24} xl={12}>
            <Card title="데이터 접근 범위" style={{ marginBottom: 16 }}>
              <Alert type="info" showIcon style={{ marginBottom: 12 }} title="Mattermost 권한 위에 한 겹 더 거는 정책입니다. 사용자가 Mattermost에서 볼 수 있더라도 여기서 끄면 MCP로는 접근할 수 없습니다." />
              <Row gutter={12}>
                <Col span={12}>{sw('allow_public', '공개 채널')}</Col>
                <Col span={12}>{sw('allow_private', '비공개 채널')}</Col>
                <Col span={12}>{sw('allow_dm', 'DM (읽기·발송)')}</Col>
                <Col span={12}>{sw('allow_group', '그룹 메시지')}</Col>
                <Col span={12}>{sw('allow_dm_search', 'DM 검색', '개인정보 보호를 위해 기본 꺼짐')}</Col>
                <Col span={12}>{sw('allow_file_download', '첨부 파일 내용 조회')}</Col>
              </Row>
            </Card>
            <Card title="결과·감사" style={{ marginBottom: 16 }}>
              <Row gutter={12}>
                <Col span={12}>{num('max_message_chars', '메시지 최대 길이', 200, 100000, '초과분은 잘라서 반환', '자')}</Col>
                <Col span={12}>{num('max_results', '결과 최대 건수', 5, 200, undefined, '건')}</Col>
                <Col span={12}>{num('max_upload_kb', '파일 최대 크기', 1, 51200, undefined, 'KB')}</Col>
                <Col span={12}>{num('approval_ttl_minutes', '승인 유효시간', 1, 1440, undefined, '분')}</Col>
                <Col span={12}>{sw('untrusted_wrapping', '조회 결과에 신뢰불가 표시', '프롬프트 인젝션 방어용 provenance 래핑')}</Col>
                <Col span={12}>{sw('audit_message_body', '감사 로그에 본문 저장', '기본 꺼짐: 질의 해시·건수만 저장')}</Col>
                <Col span={24}>{sw('console_approval', '콘솔 승인 필수 (엄격 모드)', '켜면 AI 대화 속 동의만으로는 실행되지 않고, 사용자가 mmcp 콘솔 \'승인 요청\' 화면에서 직접 승인해야 합니다. 프롬프트 인젝션으로 AI가 스스로 승인하는 것을 막습니다. 위험 도구(삭제·보관)는 항상 이 방식입니다.')}</Col>
              </Row>
            </Card>
            <Card title="서버 안내문 (instructions)">
              <Form.Item name="server_instructions" extra="MCP initialize 응답의 instructions 뒤에 덧붙여 AI 에이전트에게 전달됩니다.">
                <Input.TextArea rows={4} placeholder="예: 사내 보안 등급 '대외비' 이상 채널의 내용은 요약하지 마세요." maxLength={4000} showCount />
              </Form.Item>
            </Card>
          </Col>
          <Col xs={24} xl={12}>
            <Card title="사용자별 분당 호출 한도" style={{ marginBottom: 16 }}>
              <Row gutter={12}>
                <Col span={12}>{num('rate_read', '조회', 1, 10000, undefined, '회/분')}</Col>
                <Col span={12}>{num('rate_search', '검색', 1, 10000, undefined, '회/분')}</Col>
                <Col span={12}>{num('rate_write', '작성', 1, 10000, undefined, '회/분')}</Col>
                <Col span={12}>{num('rate_file', '파일', 1, 10000, undefined, '회/분')}</Col>
              </Row>
            </Card>
            <Card title="MCP OAuth" style={{ marginBottom: 16 }}>
              <Form.Item name="oauth_mode" label="인가 서버">
                <Radio.Group style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <Radio value="gateway"><b>mmcp 게이트웨이</b> — 동적 클라이언트 등록 + PKCE. 로그인은 Keycloak 사일런트 SSO로 처리 (권장)</Radio>
                  <Radio value="keycloak"><b>Keycloak 직접</b> — 보호 리소스 메타데이터가 Keycloak을 가리킴. 클라이언트별 Keycloak 등록 필요</Radio>
                </Radio.Group>
              </Form.Item>
              <Row gutter={12}>
                <Col span={12}>{sw('dcr_enabled', '동적 클라이언트 등록(DCR)')}</Col>
                <Col span={12}>{sw('remember_consent', '동의 기억')}</Col>
                <Col span={12}>{num('access_token_ttl_minutes', '액세스 토큰 수명', 5, 1440, undefined, '분')}</Col>
                <Col span={12}>{num('refresh_token_ttl_days', '리프레시 토큰 수명', 1, 365, undefined, '일')}</Col>
              </Row>
              {sw('compat_aliases', 'Mattermost 공식 MCP 호환 도구 이름 (dm, group_message, create_post, search_users)')}
            </Card>
            <Card title="API 키·키 회전 정책">
              <Row gutter={12}>
                <Col span={12}>{num('api_key_max_days', '최대 유효기간', 1, 3650, undefined, '일')}</Col>
                <Col span={12}>{num('api_key_max_per_user', '사용자당 최대 키', 1, 100, undefined, '개')}</Col>
                <Col span={12}>{sw('api_key_require_expiry', '만료일 필수')}</Col>
                <Col span={12}>{num('key_rotation_days', '회전 권장 주기', 1, 3650, 'API 키·개인 데이터 키 공통', '일')}</Col>
              </Row>
            </Card>
          </Col>
        </Row>
      </Form>
    </>
  )
}
