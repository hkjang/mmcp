import { useEffect } from 'react'
import { Alert, App, Button, Card, Col, Descriptions, Form, Input, Radio, Row, Switch, Tag, Typography } from 'antd'
import { SaveOutlined, LockOutlined } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { post, put, errorMessage } from '../../api/client'
import type { User } from '../../api/types'
import { useAuth } from '../../contexts/AuthContext'
import { useTheme, type ThemeMode } from '../../contexts/ThemeContext'
import { PageHeader } from '../../components/PageHeader'
import { appVersion, fmtTime } from '../../utils/format'

export default function Profile() {
  const { user, version, refresh } = useAuth()
  const { mode, setMode, compact, setCompact } = useTheme()
  const { message } = App.useApp()
  const [profileForm] = Form.useForm()
  const [pwForm] = Form.useForm()
  useEffect(() => {
    if (user) profileForm.setFieldsValue({ display_name: user.display_name, email: user.email })
  }, [user, profileForm])
  const saveProfile = useMutation({ mutationFn: (v: { display_name: string; email: string }) => put<User>('/me/profile', v), onSuccess: () => { message.success('저장했습니다'); void refresh() }, onError: (e) => message.error(errorMessage(e)) })
  const changePw = useMutation({ mutationFn: (v: { current_password: string; new_password: string }) => post('/me/password', v), onSuccess: () => { message.success('비밀번호를 변경했습니다. 다른 브라우저 세션은 로그아웃되었습니다.'); pwForm.resetFields() }, onError: (e) => message.error(errorMessage(e)) })
  if (!user) return null
  const sso = user.source === 'keycloak'
  return (
    <>
      <PageHeader title="프로필·환경설정" />
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={14}>
          <Card title="프로필" style={{ marginBottom: 16 }}>
            {sso && <Alert type="info" showIcon style={{ marginBottom: 12 }} title="Keycloak SSO 계정입니다. 이메일과 역할은 Keycloak에서 관리됩니다." />}
            <Form form={profileForm} layout="vertical" onFinish={(v) => saveProfile.mutate(v)} name="profile">
              <Form.Item name="display_name" label="표시 이름" rules={[{ required: true, message: '이름을 입력하세요' }, { max: 100 }]}><Input /></Form.Item>
              <Form.Item name="email" label="이메일" rules={[{ type: 'email', message: '이메일 형식이 아닙니다' }]}><Input disabled={sso} /></Form.Item>
              <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={saveProfile.isPending}>저장</Button>
            </Form>
          </Card>
          {user.has_password && (
            <Card title="비밀번호 변경">
              <Form form={pwForm} layout="vertical" onFinish={(v) => changePw.mutate({ current_password: v.current_password, new_password: v.new_password })} name="change-password">
                <Form.Item name="current_password" label="현재 비밀번호" rules={[{ required: true, message: '현재 비밀번호를 입력하세요' }]}><Input.Password autoComplete="current-password" prefix={<LockOutlined />} /></Form.Item>
                <Form.Item name="new_password" label="새 비밀번호" rules={[{ required: true, message: '새 비밀번호를 입력하세요' }, { min: 8, message: '8자 이상' }]} extra="영문 대/소문자, 숫자, 특수문자 중 3종류 이상">
                  <Input.Password autoComplete="new-password" prefix={<LockOutlined />} />
                </Form.Item>
                <Form.Item name="confirm" label="새 비밀번호 확인" dependencies={['new_password']} rules={[{ required: true, message: '한 번 더 입력하세요' }, ({ getFieldValue }) => ({ validator: (_, v) => (!v || getFieldValue('new_password') === v ? Promise.resolve() : Promise.reject(new Error('새 비밀번호가 일치하지 않습니다'))) })]}>
                  <Input.Password autoComplete="new-password" prefix={<LockOutlined />} />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={changePw.isPending}>변경</Button>
              </Form>
            </Card>
          )}
        </Col>
        <Col xs={24} xl={10}>
          <Card title="화면" style={{ marginBottom: 16 }}>
            <Typography.Paragraph strong style={{ marginBottom: 6 }}>테마</Typography.Paragraph>
            <Radio.Group value={mode} onChange={(e) => setMode(e.target.value as ThemeMode)} optionType="button" options={[{ value: 'light', label: '라이트' }, { value: 'dark', label: '다크' }, { value: 'system', label: '시스템' }]} />
            <div style={{ marginTop: 16, display: 'flex', alignItems: 'center', gap: 8 }}><Switch checked={compact} onChange={setCompact} /> 촘촘한 화면 (compact)</div>
          </Card>
          <Card title="계정 정보">
            <Descriptions size="small" column={1} items={[
{ label: "아이디", children: <>@{user.username}</> },
{ label: "역할", children: <>{user.roles.map((r) => <Tag key={r}>{r}</Tag>)}</> },
{ label: "가입", children: <>{fmtTime(user.created_at)}</> },
{ label: "최근 로그인", children: <>{fmtTime(user.last_login_at)}</> },
{ label: "서비스 버전", children: <>mmcp v{version?.version && version.version !== 'dev' ? version.version.replace(/^v/, '') : appVersion}</> },
]} />
          </Card>
        </Col>
      </Row>
    </>
  )
}
