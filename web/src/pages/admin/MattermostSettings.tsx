import { useState } from 'react'
import { Alert, App, Button, Card, Col, Form, Input, InputNumber, Radio, Row, Space, Switch, Typography, List, Skeleton } from 'antd'
import { ApiOutlined, SaveOutlined, CheckCircleFilled, CloseCircleFilled } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { post, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { useSectionForm } from './settingsHooks'

interface TestResult { ok: boolean; message: string; latency_ms?: number; server_version?: string; checks?: { name: string; ok: boolean; message: string }[] }

export default function MattermostSettings() {
  const [form] = Form.useForm()
  const { values, isLoading, save } = useSectionForm('mattermost', form)
  const { message } = App.useApp()
  const [result, setResult] = useState<TestResult | null>(null)
  const test = useMutation({ mutationFn: () => post<TestResult>('/admin/settings/mattermost/test'), onSuccess: setResult, onError: (e) => message.error(errorMessage(e)) })
  const sendIdentity = Form.useWatch('send_identity', form)
  const template = Form.useWatch('attribution_template', form) as string | undefined
  if (isLoading) return <Skeleton active />
  return (
    <>
      <PageHeader
        title="Mattermost 연동"
        description="mmcp는 Mattermost REST API v4를 호출합니다. 조회는 항상 사용자 본인의 자격증명으로, 발송은 아래에서 고른 계정으로 실행됩니다."
        extra={<>
          <Button icon={<ApiOutlined />} loading={test.isPending} onClick={() => test.mutate()} disabled={!values?.url}>연결 테스트</Button>
          <Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => form.submit()}>저장</Button>
        </>}
      />
      <Row gutter={16}>
        <Col xs={24} xl={15}>
          <Form form={form} layout="vertical" onFinish={(v) => save.mutate(v)} name="mattermost-settings" scrollToFirstError>
            <Card title="서버" style={{ marginBottom: 16 }}>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <Form.Item name="url" label="API Base URL" rules={[{ type: 'url', message: 'http(s) URL을 입력하세요' }]} extra="/api/v4 는 붙이지 않아도 됩니다.">
                    <Input placeholder="https://mattermost.company.co.kr" autoComplete="off" />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item name="site_url" label="Site URL (퍼머링크용)" rules={[{ type: 'url', message: 'http(s) URL을 입력하세요' }]} extra="비우면 API URL을 사용합니다.">
                    <Input placeholder="https://chat.company.co.kr" autoComplete="off" />
                  </Form.Item>
                </Col>
              </Row>
              <Row gutter={12}>
                <Col xs={12} md={8}><Form.Item name="timeout_seconds" label="타임아웃(초)"><InputNumber min={1} max={120} style={{ width: '100%' }} /></Form.Item></Col>
                <Col xs={12} md={8}><Form.Item name="cache_ttl_minutes" label="매핑 검증 캐시(분)"><InputNumber min={1} max={1440} style={{ width: '100%' }} /></Form.Item></Col>
                <Col xs={24} md={8}><Form.Item name="insecure_tls" label="TLS 검증 생략 (테스트 전용)" valuePropName="checked"><Switch /></Form.Item></Col>
              </Row>
              <Form.Item name="ca_pem" label="사설 CA 인증서 (PEM)"><Input.TextArea rows={2} placeholder="-----BEGIN CERTIFICATE-----" style={{ fontFamily: 'monospace', fontSize: 12 }} /></Form.Item>
            </Card>
            <Card title="서비스 계정 (조회·매핑·토큰 발급)" style={{ marginBottom: 16 }}>
              <Alert type="info" showIcon style={{ marginBottom: 16 }} title="서비스 토큰은 사용자 찾기(username → user_id)와 개인 토큰 자동 발급에만 쓰이며, 메시지 조회에는 절대 사용되지 않습니다. 시스템 관리자 계정의 PAT를 권장합니다." />
              <Form.Item name="service_token" label="서비스 계정 액세스 토큰" extra="저장된 값은 ******** 로 표시됩니다.">
                <Input.Password autoComplete="new-password" placeholder="system admin personal access token" />
              </Form.Item>
              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <Form.Item name="auto_issue_tokens" label="사용자 개인 토큰 자동 발급" valuePropName="checked" extra="사용자가 PAT를 직접 만들 필요 없이 첫 호출 때 사용자 명의 토큰을 발급·암호화 저장합니다.">
                    <Switch />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item name="email_fallback" label="username이 없으면 이메일로 매칭" valuePropName="checked"><Switch /></Form.Item>
                </Col>
              </Row>
              <Form.Item name="token_description" label="자동 발급 토큰 설명"><Input maxLength={80} /></Form.Item>
            </Card>
            <Card title="발신 계정 (메시지·DM 전송)">
              <Form.Item name="send_identity" label="누구 명의로 보낼까요?">
                <Radio.Group style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  <Radio value="user"><b>사용자 본인</b> — Mattermost에 실제 사용자(hkjang)가 보낸 것으로 표시. 사용자 자격증명 필요.</Radio>
                  <Radio value="bot"><b>봇 계정</b> — "AI Assistant"가 보내고 요청자를 메시지와 props에 기록. 사용자 비밀번호·PAT 불필요.</Radio>
                  <Radio value="auto"><b>자동</b> — 사용자 자격증명이 있으면 본인, 없으면 봇으로 발송 (권장).</Radio>
                </Radio.Group>
              </Form.Item>
              {sendIdentity !== 'user' && (
                <>
                  <Form.Item name="bot_token" label="봇 계정 액세스 토큰" extra="Integrations > Bot Accounts에서 만든 봇의 토큰. DM 외 채널에 보내려면 post:channels 또는 post:all 권한이 필요합니다.">
                    <Input.Password autoComplete="new-password" placeholder="bot access token" />
                  </Form.Item>
                  <Form.Item name="bot_dm_channel" label="봇 DM을 어느 대화방에 넣을까요?">
                    <Radio.Group style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      <Radio value="bot_pair"><b>봇 ↔ 수신자</b> DM (일반 봇 권한으로 가능, 권장)</Radio>
                      <Radio value="requester_pair"><b>요청자 ↔ 수신자</b> DM에 봇이 게시 (서비스 토큰 system_admin + 봇 post:all 필요)</Radio>
                    </Radio.Group>
                  </Form.Item>
                  <Form.Item name="attribution_template" label="요청자 표시 문구" extra="{display_name}, {username}, {mmcp_username} 치환. 비우면 문구 없이 props에만 기록합니다.">
                    <Input />
                  </Form.Item>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>미리보기</Typography.Text>
                  <div style={{ border: '1px solid var(--mmcp-border)', borderRadius: 8, padding: '10px 12px', marginTop: 4 }}>
                    <b>AI Assistant</b> <Typography.Text type="secondary" style={{ fontSize: 11 }}>BOT</Typography.Text>
                    <div style={{ fontStyle: 'italic', opacity: 0.75 }}>{(template ?? '').replaceAll('{display_name}', '장형국').replaceAll('{username}', 'hkjang').replaceAll('{mmcp_username}', 'hkjang').replace(/^_|_$/g, '')}</div>
                    <div>서버 점검 완료됐습니다.</div>
                  </div>
                </>
              )}
            </Card>
          </Form>
        </Col>
        <Col xs={24} xl={9}>
          {result && (
            <Card title="연결 테스트" style={{ marginBottom: 16 }}>
              <Alert type={result.ok ? 'success' : 'error'} showIcon title={result.message} description={result.server_version && `서버 ${result.server_version.split('.').slice(0, 3).join('.')} · ${result.latency_ms}ms`} style={{ marginBottom: 12 }} />
              <List size="small" dataSource={result.checks ?? []} renderItem={(c) => (
                <List.Item><Space align="start">{c.ok ? <CheckCircleFilled style={{ color: '#16a34a' }} /> : <CloseCircleFilled style={{ color: '#dc2626' }} />}<span style={{ fontSize: 13 }}>{c.message}</span></Space></List.Item>
              )} />
            </Card>
          )}
          <Card title="권한 모델">
            <List size="small" dataSource={[
              ['조회·검색·DM 읽기', '사용자 본인 토큰 — Mattermost 채널 멤버십 그대로 적용'],
              ['사용자 찾기·매핑', '서비스 토큰 (사용자 디렉터리 조회만)'],
              ['개인 토큰 자동 발급', '서비스 토큰 (system_admin) → 사용자 명의 PAT'],
              ['메시지·DM 발송', '위에서 고른 발신 계정 + 승인 정책'],
            ]} renderItem={([a, b]) => <List.Item><div><b>{a}</b><div style={{ fontSize: 12.5, opacity: 0.75 }}>{b}</div></div></List.Item>} />
            <Alert style={{ marginTop: 12 }} type="warning" showIcon title="mmcp는 Mattermost 비밀번호를 받거나 저장하지 않습니다. 사용자 비밀번호 기반 세션 로그인은 지원하지 않습니다." />
          </Card>
        </Col>
      </Row>
    </>
  )
}
