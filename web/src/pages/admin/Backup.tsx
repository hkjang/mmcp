import { useState } from 'react'
import { Alert, App, Button, Card, Col, Descriptions, Row, Space, Tag, Typography, Upload } from 'antd'
import { CloudDownloadOutlined, CloudUploadOutlined, InboxOutlined } from '@ant-design/icons'
import { useQueryClient } from '@tanstack/react-query'
import { api, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { fmtTime } from '../../utils/format'

interface Backup { format: string; exported_at: string; version: string; settings: Record<string, unknown>; tool_policies: Record<string, unknown> }

export default function Backup() {
  const { message, modal } = App.useApp()
  const qc = useQueryClient()
  const [file, setFile] = useState<Backup | null>(null)
  const [busy, setBusy] = useState(false)
  const doImport = async () => {
    if (!file) return
    setBusy(true)
    try {
      const r = await api<{ sections: string[]; tool_policies: number }>('/admin/config/import', { method: 'POST', body: file })
      message.success(`복원 완료: 설정 ${r.sections.join(', ')} · 도구 정책 ${r.tool_policies}개`)
      setFile(null)
      void qc.invalidateQueries()
    } catch (e) {
      message.error(errorMessage(e), 8)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <PageHeader title="백업·복원" description="콘솔에서 관리하는 설정과 도구 정책을 JSON으로 내보내고 다른 환경(스테이징 → 운영)으로 옮깁니다." />
      <Row gutter={16}>
        <Col xs={24} lg={12}>
          <Card title="내보내기" style={{ marginBottom: 16 }}>
            <Typography.Paragraph>일반·Keycloak·Mattermost·MCP 설정과 도구 정책을 내보냅니다.</Typography.Paragraph>
            <Alert type="info" showIcon style={{ marginBottom: 12 }} title="Client Secret, 서비스/봇 토큰 같은 비밀값은 백업 파일에 포함되지 않습니다. 복원한 뒤 해당 화면에서 다시 입력하세요." />
            <Button type="primary" icon={<CloudDownloadOutlined />} href="/api/admin/config/export">설정 백업 다운로드</Button>
          </Card>
          <Card title="데이터베이스 백업 권장 사항">
            <Typography.Paragraph style={{ fontSize: 13 }}>
              사용자·매핑·암호화된 자격증명·감사 로그는 PostgreSQL에 있습니다. <code>pg_dump</code>로 정기 백업하고, 복원할 때는 <b>같은 ENCRYPTION_KEY</b>가 필요합니다. 키를 잃으면 저장된 자격증명은 복호화할 수 없습니다 (사용자는 토큰을 다시 등록하면 됩니다).
            </Typography.Paragraph>
            <pre className="mmcp-code">pg_dump -Fc "$POSTGRES_DSN" &gt; mmcp-$(date +%F).dump</pre>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="복원">
            <Upload.Dragger accept=".json,application/json" maxCount={1} showUploadList={false} beforeUpload={(f) => {
              f.text().then((t) => {
                try {
                  const b = JSON.parse(t) as Backup
                  if (b.format !== 'mmcp-config/v1') throw new Error('mmcp-config/v1 형식이 아닙니다')
                  setFile(b)
                } catch (e) {
                  message.error(`백업 파일을 읽을 수 없습니다: ${errorMessage(e)}`)
                }
              })
              return false
            }}>
              <p className="ant-upload-drag-icon"><InboxOutlined /></p>
              <p className="ant-upload-text">백업 JSON 파일을 끌어 놓거나 클릭해 선택</p>
            </Upload.Dragger>
            {file && (
              <div style={{ marginTop: 16 }}>
                <Descriptions size="small" column={1} bordered items={[
{ label: "내보낸 시각", children: <>{fmtTime(file.exported_at)}</> },
{ label: "버전", children: <>{file.version}</> },
{ label: "설정 섹션", children: <>{Object.keys(file.settings).map((s) => <Tag key={s}>{s}</Tag>)}</> },
{ label: "도구 정책", children: <>{Object.keys(file.tool_policies ?? {}).length}개</> },
]} />
                <Space style={{ marginTop: 12 }}>
                  <Button onClick={() => setFile(null)}>취소</Button>
                  <Button type="primary" danger icon={<CloudUploadOutlined />} loading={busy} onClick={() => modal.confirm({ title: '현재 설정을 덮어쓸까요?', content: '비밀값은 유지되고 나머지 설정과 도구 정책이 백업 내용으로 바뀝니다.', onOk: doImport })}>복원</Button>
                </Space>
              </div>
            )}
          </Card>
        </Col>
      </Row>
    </>
  )
}
