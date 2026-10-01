import { useEffect, useMemo, useRef, useState } from 'react'
import { App, Badge, Button, Card, Input, Select, Space, Switch, Tooltip, Typography } from 'antd'
import { DownloadOutlined, PauseCircleOutlined, PlayCircleOutlined, ClearOutlined, VerticalAlignBottomOutlined } from '@ant-design/icons'
import { useSearchParams } from 'react-router-dom'
import dayjs from 'dayjs'
import { get, put, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { DetailPanel, SplitLayout } from '../../components/DetailPanel'
import { CodeBlock, KV } from '../../components/common'

interface LogEntry { seq: number; time: string; level: string; msg: string; attrs?: Record<string, unknown> }
const rank: Record<string, number> = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 }
const MAX = 3000

function attrsInline(a?: Record<string, unknown>) {
  if (!a) return ''
  return Object.entries(a).filter(([k]) => k !== 'stack').map(([k, v]) => `${k}=${typeof v === 'object' ? JSON.stringify(v) : String(v)}`).join(' ')
}

export default function Logs() {
  const [params] = useSearchParams()
  const { message } = App.useApp()
  const [entries, setEntries] = useState<LogEntry[]>([])
  const [level, setLevel] = useState(params.get('level') ?? '')
  const [q, setQ] = useState('')
  const [live, setLive] = useState(true)
  const [follow, setFollow] = useState(true)
  const [connected, setConnected] = useState(false)
  const [serverLevel, setServerLevel] = useState('info')
  const [sel, setSel] = useState<LogEntry | null>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const pausedBuf = useRef<LogEntry[]>([])

  useEffect(() => {
    get<{ items: LogEntry[]; level: string }>('/admin/logs', { limit: 1000 }).then((r) => {
      setEntries(r.items.slice().reverse())
      setServerLevel(r.level.toLowerCase())
    }).catch((e) => message.error(errorMessage(e)))
  }, [message])

  useEffect(() => {
    const es = new EventSource('/api/admin/logs/stream')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (ev) => {
      const e = JSON.parse(ev.data) as LogEntry
      if (!live) {
        pausedBuf.current.push(e)
        return
      }
      setEntries((old) => {
        if (old.length && old[old.length - 1].seq >= e.seq) return old
        const next = old.length >= MAX ? old.slice(old.length - MAX + 1) : old.slice()
        next.push(e)
        return next
      })
    }
    return () => es.close()
  }, [live])

  useEffect(() => {
    if (live && pausedBuf.current.length) {
      const buf = pausedBuf.current
      pausedBuf.current = []
      setEntries((old) => [...old, ...buf.filter((b) => !old.length || b.seq > old[old.length - 1].seq)].slice(-MAX))
    }
  }, [live])

  const shown = useMemo(() => {
    const ql = q.trim().toLowerCase()
    return entries.filter((e) => (!level || rank[e.level] >= rank[level]) && (!ql || e.msg.toLowerCase().includes(ql) || attrsInline(e.attrs).toLowerCase().includes(ql)))
  }, [entries, level, q])

  useEffect(() => {
    if (follow && boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight
  }, [shown, follow])

  const counts = useMemo(() => {
    const c: Record<string, number> = { WARN: 0, ERROR: 0 }
    for (const e of entries) if (e.level in c) c[e.level]++
    return c
  }, [entries])

  const changeLevel = async (v: string) => {
    try {
      await put('/admin/logs/level', { level: v })
      setServerLevel(v)
      message.success(`서버 로그 레벨을 ${v}로 변경했습니다`)
    } catch (e) {
      message.error(errorMessage(e))
    }
  }

  return (
    <>
      <PageHeader
        title="서버 로그"
        description={`컨테이너에 접속하지 않고 mmcp 서버 로그를 실시간으로 확인합니다. 최근 ${MAX.toLocaleString()}줄을 보관하며, 표준 출력(JSON)에도 같은 로그가 기록됩니다.`}
        extra={<>
          <Tooltip title="서버가 기록하는 최소 레벨 (일반 설정에 저장)">
            <Select value={serverLevel} onChange={changeLevel} style={{ width: 150 }} aria-label="서버 로그 레벨"
              options={['debug', 'info', 'warn', 'error'].map((l) => ({ value: l, label: `기록 레벨: ${l}` }))} />
          </Tooltip>
          <Button icon={<DownloadOutlined />} href="/api/admin/logs/download">다운로드</Button>
        </>}
      />
      <SplitLayout panel={sel && (
        <DetailPanel open title={sel.msg} onClose={() => setSel(null)}>
          <KV items={[['시각', dayjs(sel.time).format('YYYY-MM-DD HH:mm:ss.SSS')], ['레벨', sel.level], ['순번', sel.seq]]} />
          <Typography.Paragraph strong style={{ margin: '12px 0 6px' }}>속성</Typography.Paragraph>
          <CodeBlock code={JSON.stringify(sel.attrs ?? {}, null, 2)} wrap maxHeight={520} />
        </DetailPanel>
      )}>
        <Card size="small" style={{ marginBottom: 12 }}>
          <Space wrap>
            <Badge status={connected ? (live ? 'processing' : 'warning') : 'error'} text={connected ? (live ? '실시간' : '일시정지') : '연결 끊김'} />
            <Button size="small" icon={live ? <PauseCircleOutlined /> : <PlayCircleOutlined />} onClick={() => setLive(!live)}>{live ? '일시정지' : '재개'}</Button>
            <Select size="small" value={level} onChange={setLevel} style={{ width: 130 }} aria-label="표시 레벨" options={[{ value: '', label: '모든 레벨' }, { value: 'INFO', label: 'INFO 이상' }, { value: 'WARN', label: 'WARN 이상' }, { value: 'ERROR', label: 'ERROR만' }]} />
            <Input.Search size="small" allowClear placeholder="메시지·속성 검색 (request_id 등)" value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 280 }} />
            <Space size={4}><Switch size="small" checked={follow} onChange={setFollow} /><VerticalAlignBottomOutlined /> 자동 스크롤</Space>
            <Button size="small" icon={<ClearOutlined />} onClick={() => setEntries([])}>화면 비우기</Button>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>{shown.length.toLocaleString()} / {entries.length.toLocaleString()}줄 · WARN {counts.WARN} · ERROR {counts.ERROR}</Typography.Text>
          </Space>
        </Card>
        <div className="mmcp-log-view mono" ref={boxRef} onScroll={(e) => {
          const el = e.currentTarget
          const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
          if (!atBottom && follow) setFollow(false)
        }} role="log" aria-live="off">
          {shown.length === 0 && <div style={{ padding: 16, color: '#64748b' }}>표시할 로그가 없습니다.</div>}
          {shown.map((e) => (
            <div key={e.seq} className={`mmcp-log-row${sel?.seq === e.seq ? ' sel' : ''}`} onClick={() => setSel(e)}>
              <span className="mmcp-log-time">{dayjs(e.time).format('MM-DD HH:mm:ss.SSS')}</span>
              <span className={`mmcp-log-${e.level}`}>{e.level}</span>
              <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{e.msg}<span className="mmcp-log-attrs">{attrsInline(e.attrs)}</span></span>
            </div>
          ))}
        </div>
      </SplitLayout>
    </>
  )
}
