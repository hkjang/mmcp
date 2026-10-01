import { Alert, Button, Card, Col, Form, Input, InputNumber, Row, Select, Skeleton, Switch } from 'antd'
import { SaveOutlined } from '@ant-design/icons'
import { PageHeader } from '../../components/PageHeader'
import { useSectionForm } from './settingsHooks'

export default function GeneralSettings() {
  const [form] = Form.useForm()
  const { values, isLoading, save } = useSectionForm('general', form)
  const origin = window.location.origin
  if (isLoading) return <Skeleton active />
  return (
    <>
      <PageHeader title="일반 설정" description="환경변수 4개를 제외한 모든 운영 설정은 이 콘솔에서 관리하며 데이터베이스에 저장됩니다." extra={<Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => form.submit()}>저장</Button>} />
      <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} name="general-settings">
        <Row gutter={16}>
          <Col xs={24} xl={14}>
            <Card title="서비스 주소" style={{ marginBottom: 16 }}>
              {!values?.public_url && (
                <Alert type="warning" showIcon style={{ marginBottom: 12 }} title="Public URL이 비어 있습니다" description={<>MCP OAuth 메타데이터와 Keycloak 리다이렉트 주소가 요청 Host로 추정됩니다. 지금 접속 주소 <code>{origin}</code>가 사용자 접근 주소라면 그대로 저장하세요. <Button size="small" type="link" onClick={() => form.setFieldsValue({ public_url: origin })}>현재 주소 사용</Button></>} />
              )}
              <Form.Item name="public_url" label="Public URL" rules={[{ type: 'url', message: 'http(s) URL을 입력하세요' }]} extra="사용자와 MCP 클라이언트가 접속하는 외부 주소 (리버스 프록시 뒤라면 프록시 주소). MCP 엔드포인트는 {Public URL}/mcp 입니다.">
                <Input placeholder="https://mcp.company.co.kr" autoComplete="off" />
              </Form.Item>
              <Form.Item name="login_notice" label="로그인 화면 공지" extra="예: 시스템 점검 안내, 사용 문의처">
                <Input.TextArea rows={2} maxLength={500} showCount />
              </Form.Item>
            </Card>
          </Col>
          <Col xs={24} xl={10}>
            <Card title="운영" style={{ marginBottom: 16 }}>
              <Form.Item name="audit_retention_days" label="감사 로그 보존 기간"><InputNumber min={1} max={3650} style={{ width: '100%' }} suffix="일" /></Form.Item>
              <Form.Item name="log_level" label="서버 로그 레벨">
                <Select options={['debug', 'info', 'warn', 'error'].map((v) => ({ value: v, label: v }))} />
              </Form.Item>
              <Form.Item name="metrics_enabled" label="Prometheus /metrics 노출" valuePropName="checked" extra="관리자 IP 허용목록이 설정되어 있으면 함께 적용됩니다."><Switch /></Form.Item>
            </Card>
          </Col>
        </Row>
      </Form>
    </>
  )
}
