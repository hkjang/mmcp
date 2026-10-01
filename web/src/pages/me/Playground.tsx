import { useEffect, useMemo, useState } from 'react'
import { Alert, App, Button, Card, Col, Empty, Form, Input, InputNumber, List, Row, Segmented, Select, Space, Switch, Tag, Tooltip, Typography } from 'antd'
import { PlayCircleOutlined, CheckOutlined, CloseOutlined, LockOutlined, HistoryOutlined } from '@ant-design/icons'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { get, post, errorMessage } from '../../api/client'
import type { ToolResult, ToolError } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { CodeBlock } from '../../components/common'
import { categoryLabel } from '../../utils/format'

interface ToolInfo { name: string; title: string; description: string; category: string; read_only: boolean; destructive: boolean; mode: string; schema: { properties?: Record<string, Prop>; required?: string[] }; available: boolean; reason: string; roles: string[] }
interface Prop { type: string; description?: string; default?: unknown; enum?: string[]; minimum?: number; maximum?: number; items?: { type: string } }
interface Run { at: string; tool: string; args: Record<string, unknown>; result: ToolResult; ms: number }

const samples: Record<string, Record<string, unknown>> = {
  mattermost_search_posts: { query: '장애', limit: 10 },
  mattermost_send_dm: { recipient: 'kim', message: '내일 오전 10시 회의 잊지 마세요.' },
  mattermost_recent_posts: { channel: 'town-square', limit: 10 },
}

function cleanArgs(v: Record<string, unknown>) {
  const out: Record<string, unknown> = {}
  for (const [k, x] of Object.entries(v)) {
    if (x === undefined || x === null || x === '' || (Array.isArray(x) && x.length === 0)) continue
    out[k] = x
  }
  return out
}

export default function Playground() {
  const [params, setParams] = useSearchParams()
  const { message } = App.useApp()
  const [form] = Form.useForm()
  const [q, setQ] = useState('')
  const [mode, setMode] = useState<'form' | 'json'>('form')
  const [json, setJson] = useState('{}')
  const [runs, setRuns] = useState<Run[]>([])
  const [current, setCurrent] = useState<Run | null>(null)
  const { data } = useQuery({ queryKey: ['me', 'tools'], queryFn: () => get<{ items: ToolInfo[] }>('/me/tools') })
  const name = params.get('tool') ?? 'mattermost_me'
  const tool = data?.items.find((t) => t.name === name)
  useEffect(() => {
    form.resetFields()
    const init = samples[name] ?? {}
    form.setFieldsValue(init)
    setJson(JSON.stringify(init, null, 2))
    setCurrent(null)
  }, [name, form])

  const run = useMutation({
    mutationFn: async (args: Record<string, unknown>) => {
      const t0 = performance.now()
      const result = await post<ToolResult>(`/me/tools/${name}/call`, { arguments: args })
      return { at: new Date().toISOString(), tool: name, args, result, ms: Math.round(performance.now() - t0) }
    },
    onSuccess: (r) => { setCurrent(r); setRuns((old) => [r, ...old].slice(0, 20)) },
    onError: (e) => message.error(errorMessage(e)),
  })
  const reject = useMutation({ mutationFn: (id: string) => post(`/me/approvals/${id}/reject`), onSuccess: () => { message.info('요청을 거절했습니다'); setCurrent(null) } })

  const submit = () => {
    if (mode === 'json') {
      try {
        run.mutate(JSON.parse(json || '{}'))
      } catch {
        message.error('JSON 형식이 올바르지 않습니다')
      }
    } else {
      form.validateFields().then((v) => run.mutate(cleanArgs(v)))
    }
  }

  const grouped = useMemo(() => {
    const list = (data?.items ?? []).filter((t) => !q || `${t.name} ${t.title}`.toLowerCase().includes(q.toLowerCase()))
    return list
  }, [data, q])

  const err = (current?.result.structuredContent as { error?: ToolError } | undefined)?.error
  const approval = err?.code === 'APPROVAL_REQUIRED' ? (err.details as { confirm_token: string; preview: Record<string, unknown>; expires_at: string }) : null
  const approveAndRun = async (token: string) => {
    try {
      // record the approval first: required when the admin enabled console approval
      await post(`/me/approvals/${token}/approve`)
    } catch {
      /* already decided; the re-call reports the real state */
    }
    if (current) run.mutate({ ...current.args, confirm_token: token })
  }
  const candidates = err?.code === 'AMBIGUOUS_RECIPIENT' ? (err.details as { username: string; display_name: string; position: string; email: string }[]) : null
  const props = tool?.schema.properties ?? {}
  const required = new Set(tool?.schema.required ?? [])

  return (
    <>
      <PageHeader title="도구 실행기" description="AI 클라이언트가 쓰는 MCP 도구를 내 권한으로 직접 실행해 봅니다. 정책·승인·감사가 실제 MCP 호출과 똑같이 적용됩니다." />
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8} xl={6}>
          <Card size="small" title="도구" styles={{ body: { padding: 8 } }}>
            <Input.Search allowClear size="small" placeholder="도구 검색" value={q} onChange={(e) => setQ(e.target.value)} style={{ marginBottom: 8 }} />
            <div className="mmcp-scroll" style={{ maxHeight: 'calc(100vh - 260px)', overflowY: 'auto' }}>
              <List size="small" dataSource={grouped} renderItem={(t) => (
                <Tooltip title={t.available ? t.description : t.reason} placement="right">
                  <List.Item onClick={() => setParams({ tool: t.name }, { replace: true })} style={{ cursor: 'pointer', padding: '6px 8px', borderRadius: 6, background: t.name === name ? 'rgba(37,99,235,.1)' : undefined, opacity: t.available ? 1 : 0.45 }}>
                    <div style={{ width: '100%' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
                        <span style={{ fontSize: 13 }}>{t.title}</span>
                        <span>{t.mode === 'confirm' && !t.read_only && <LockOutlined style={{ fontSize: 11, opacity: 0.6 }} />}</span>
                      </div>
                      <div className="mmcp-tool-name" style={{ fontSize: 11, opacity: 0.6 }}>{t.name}</div>
                    </div>
                  </List.Item>
                </Tooltip>
              )} />
            </div>
          </Card>
        </Col>
        <Col xs={24} lg={16} xl={18}>
          {!tool ? <Card><Empty description="도구를 선택하세요" /></Card> : (
            <>
              <Card style={{ marginBottom: 16 }} title={<Space wrap><span>{tool.title}</span><Tag className="mmcp-tool-name">{tool.name}</Tag><Tag>{categoryLabel[tool.category]}</Tag>{!tool.read_only && tool.mode === 'confirm' && <Tag color="orange" icon={<LockOutlined />}>승인 필요</Tag>}</Space>}
                extra={<Segmented size="small" value={mode} onChange={(v) => { if (v === 'json') setJson(JSON.stringify(cleanArgs(form.getFieldsValue()), null, 2)); setMode(v as 'form' | 'json') }} options={[{ label: '폼', value: 'form' }, { label: 'JSON', value: 'json' }]} />}>
                <Typography.Paragraph type="secondary">{tool.description}</Typography.Paragraph>
                {!tool.available && <Alert type="warning" showIcon style={{ marginBottom: 12 }} title={`실행할 수 없음: ${tool.reason}`} />}
                {mode === 'json' ? (
                  <Input.TextArea value={json} onChange={(e) => setJson(e.target.value)} rows={8} style={{ fontFamily: 'monospace', fontSize: 12.5 }} aria-label="도구 인자 JSON" />
                ) : (
                  <Form form={form} layout="vertical" name={`tool-${name}`} onFinish={submit}>
                    {Object.keys(props).length === 0 && <Typography.Text type="secondary">인자가 없는 도구입니다.</Typography.Text>}
                    <Row gutter={12}>
                      {Object.entries(props).filter(([k]) => k !== 'confirm_token').map(([k, p]) => (
                        <Col xs={24} md={p.type === 'string' && (k === 'message' || k === 'content') ? 24 : 12} key={k}>
                          <Form.Item name={k} label={<span><span className="mono">{k}</span>{p.description && <Typography.Text type="secondary" style={{ fontSize: 12, marginLeft: 6 }}>{p.description}</Typography.Text>}</span>}
                            rules={required.has(k) ? [{ required: true, message: `${k}은(는) 필수입니다` }] : undefined} valuePropName={p.type === 'boolean' ? 'checked' : 'value'}>
                            {p.type === 'boolean' ? <Switch /> : p.type === 'integer' ? <InputNumber min={p.minimum} max={p.maximum} placeholder={p.default !== undefined ? String(p.default) : undefined} style={{ width: '100%' }} />
                              : p.type === 'array' ? <Select mode="tags" tokenSeparators={[',']} placeholder="여러 값 입력 (Enter)" open={false} suffixIcon={null} />
                                : p.enum ? <Select allowClear options={p.enum.map((e) => ({ value: e, label: e }))} />
                                  : k === 'message' || k === 'content' ? <Input.TextArea rows={3} /> : <Input autoComplete="off" />}
                          </Form.Item>
                        </Col>
                      ))}
                    </Row>
                  </Form>
                )}
                <Button type="primary" icon={<PlayCircleOutlined />} onClick={submit} loading={run.isPending} disabled={!tool.available} style={{ marginTop: 8 }}>실행</Button>
              </Card>
              {approval && current && (
                <Card style={{ marginBottom: 16, borderColor: '#f59e0b' }} title={<span><LockOutlined /> 실행 전 확인이 필요합니다</span>}>
                  <Typography.Paragraph>AI 클라이언트에서는 이 미리보기를 사용자에게 보여 주고 동의를 받은 뒤 같은 인자 + <code>confirm_token</code>으로 다시 호출합니다.</Typography.Paragraph>
                  <CodeBlock code={JSON.stringify(approval.preview, null, 2)} wrap />
                  <Space style={{ marginTop: 12 }}>
                    <Button icon={<CloseOutlined />} onClick={() => reject.mutate(approval.confirm_token)}>거절</Button>
                    <Button type="primary" icon={<CheckOutlined />} loading={run.isPending} onClick={() => void approveAndRun(approval.confirm_token)}>승인하고 실행</Button>
                  </Space>
                </Card>
              )}
              {candidates && (
                <Alert type="warning" showIcon style={{ marginBottom: 16 }} title={err?.message} description={
                  <Space wrap style={{ marginTop: 8 }}>{candidates.map((c) => (
                    <Button key={c.username} size="small" onClick={() => {
                      const field = 'recipient' in props ? 'recipient' : 'user' in props ? 'user' : 'recipient'
                      form.setFieldsValue({ [field]: c.username })
                      message.info(`@${c.username}(으)로 지정했습니다. 다시 실행하세요.`)
                    }}>{c.display_name} @{c.username} · {c.position}</Button>
                  ))}</Space>
                } />
              )}
              {current && (
                <Card size="small" title={<Space>결과 <Tag color={current.result.isError ? 'red' : 'green'}>{current.result.isError ? err?.code ?? '오류' : '성공'}</Tag><Typography.Text type="secondary" style={{ fontSize: 12 }}>{current.ms}ms</Typography.Text></Space>}>
                  <CodeBlock code={JSON.stringify(current.result.structuredContent ?? current.result.content, null, 2)} maxHeight={520} />
                </Card>
              )}
              {runs.length > 1 && (
                <Card size="small" title={<span><HistoryOutlined /> 이번 세션 실행 기록</span>} style={{ marginTop: 16 }}>
                  <List size="small" dataSource={runs} renderItem={(r) => (
                    <List.Item onClick={() => { setParams({ tool: r.tool }, { replace: true }); setTimeout(() => setCurrent(r), 0) }} style={{ cursor: 'pointer' }}>
                      <span className="mmcp-tool-name">{r.tool}</span>
                      <Space><Tag color={r.result.isError ? 'red' : 'green'}>{r.result.isError ? '오류' : '성공'}</Tag><span style={{ fontSize: 12, opacity: 0.6 }}>{new Date(r.at).toLocaleTimeString()}</span></Space>
                    </List.Item>
                  )} />
                </Card>
              )}
            </>
          )}
        </Col>
      </Row>
    </>
  )
}
