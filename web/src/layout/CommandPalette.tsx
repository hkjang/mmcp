import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Input, Modal, Tag, Typography, type InputRef } from 'antd'
import { BgColorsOutlined, LogoutOutlined, PlusOutlined, QuestionCircleOutlined, SearchOutlined, SwapOutlined, ClockCircleOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { adminNav, personalNav, allNav } from '../config/navigation'
import { matchScore } from '../utils/format'
import { useAuth } from '../contexts/AuthContext'
import { useTheme } from '../contexts/ThemeContext'

interface Cmd {
  id: string
  label: string
  description?: string
  icon: ReactNode
  group: string
  keywords?: string
  hint?: string
  run: () => void
}

export function readRecent(): string[] {
  try {
    return JSON.parse(localStorage.getItem('mmcp.recent') ?? '[]') as string[]
  } catch {
    return []
  }
}

export function pushRecent(path: string) {
  try {
    const list = [path, ...readRecent().filter((p) => p !== path)].slice(0, 6)
    localStorage.setItem('mmcp.recent', JSON.stringify(list))
  } catch {
    /* ignore */
  }
}

export function CommandPalette({ open, onClose, onHelp }: { open: boolean; onClose: () => void; onHelp: () => void }) {
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const navigate = useNavigate()
  const { isAdmin, logout } = useAuth()
  const { toggle } = useTheme()
  const inputRef = useRef<InputRef>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open) {
      setQ('')
      setActive(0)
      setTimeout(() => inputRef.current?.focus(), 30)
    }
  }, [open])

  const commands = useMemo<Cmd[]>(() => {
    const go = (path: string) => () => {
      navigate(path)
      onClose()
    }
    const nav = [...(isAdmin ? adminNav : []), ...personalNav].map<Cmd>((n) => ({
      id: n.path, label: n.label, description: n.description, icon: n.icon, group: n.path.startsWith('/admin') ? '관리 콘솔' : '내 공간',
      keywords: `${n.keywords ?? ''} ${n.path}`, hint: n.shortcut ? `G ${n.shortcut.toUpperCase()}` : undefined, run: go(n.path),
    }))
    const actions: Cmd[] = [
      { id: 'act:new-key', label: '새 API 키 발급', description: '내 API 키 화면에서 바로 발급 창 열기', icon: <PlusOutlined />, group: '작업', keywords: 'api key create 키 발급', run: go('/me/keys?new=1') },
      { id: 'act:connect', label: 'MCP 클라이언트 연결 설정 복사', description: 'Claude Code / Codex / Cursor 설정', icon: <SwapOutlined />, group: '작업', keywords: 'connect claude codex 연결', run: go('/me/connect') },
      { id: 'act:theme', label: '라이트/다크 테마 전환', icon: <BgColorsOutlined />, group: '작업', keywords: 'theme dark light 테마 다크', run: () => { toggle(); onClose() } },
      { id: 'act:help', label: '키보드 단축키 보기', icon: <QuestionCircleOutlined />, group: '작업', keywords: 'shortcut help 단축키 도움말', hint: '?', run: () => { onClose(); onHelp() } },
      { id: 'act:logout', label: '로그아웃', icon: <LogoutOutlined />, group: '작업', keywords: 'logout 로그아웃', run: () => void logout() },
    ]
    if (isAdmin) {
      actions.unshift(
        { id: 'act:automap', label: '미매핑 사용자 자동 매핑', description: '계정 매핑 화면으로 이동', icon: <SwapOutlined />, group: '작업', keywords: 'auto map 자동 매핑', run: go('/admin/mappings?auto=1') },
        { id: 'act:logs-error', label: '오류 로그만 보기', icon: <SearchOutlined />, group: '작업', keywords: 'error log 오류', run: go('/admin/system/logs?level=ERROR') },
      )
    }
    return [...nav, ...actions]
  }, [isAdmin, navigate, onClose, onHelp, toggle, logout])

  const results = useMemo(() => {
    if (!q.trim()) {
      const recent = readRecent()
        .map((p) => commands.find((c) => c.id === p))
        .filter((c): c is Cmd => !!c)
        .map((c) => ({ ...c, group: '최근 이동', icon: <ClockCircleOutlined /> }))
      return [...recent, ...commands.filter((c) => c.group !== '관리 콘솔' || recent.length === 0).slice(0, 12)]
    }
    return commands
      .map((c) => ({ c, s: matchScore(q, c.label, c.keywords ?? '', c.description ?? '') }))
      .filter((x) => x.s > 0)
      .sort((a, b) => b.s - a.s)
      .slice(0, 30)
      .map((x) => x.c)
  }, [q, commands])

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((a) => Math.min(a + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => Math.max(a - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      results[active]?.run()
    }
  }

  let lastGroup = ''
  return (
    <Modal open={open} onCancel={onClose} footer={null} closable={false} width={620} styles={{ body: { padding: 4 } }} destroyOnHidden>
      <Input
        ref={inputRef}
        size="large"
        prefix={<SearchOutlined />}
        placeholder="페이지, 작업 검색 (초성 검색 가능: ㄷㅅㅂㄷ)"
        value={q}
        onChange={(e) => {
          setQ(e.target.value)
          setActive(0)
        }}
        onKeyDown={onKey}
        aria-label="빠른 이동 검색"
        variant="borderless"
        style={{ fontSize: 16 }}
      />
      <div className="mmcp-palette-list mmcp-scroll" ref={listRef} role="listbox">
        {results.length === 0 && <Typography.Paragraph type="secondary" style={{ padding: 16 }}>일치하는 항목이 없습니다.</Typography.Paragraph>}
        {results.map((c, i) => {
          const header = c.group !== lastGroup ? c.group : null
          lastGroup = c.group
          return (
            <div key={`${c.group}-${c.id}`}>
              {header && <div style={{ fontSize: 11, fontWeight: 600, opacity: 0.55, padding: '10px 12px 4px' }}>{header}</div>}
              <div
                className={`mmcp-palette-item${i === active ? ' active' : ''}`}
                data-idx={i}
                role="option"
                aria-selected={i === active}
                onMouseEnter={() => setActive(i)}
                onClick={() => c.run()}
              >
                <span style={{ opacity: 0.75 }}>{c.icon}</span>
                <span>{c.label}</span>
                {c.description && <span className="desc">{c.description}</span>}
                {c.hint && <Tag style={{ marginLeft: c.description ? 8 : 'auto' }}>{c.hint}</Tag>}
              </div>
            </div>
          )
        })}
      </div>
      <div style={{ display: 'flex', gap: 14, fontSize: 11.5, opacity: 0.6, padding: '8px 12px 4px', flexWrap: 'wrap' }}>
        <span><kbd className="mmcp-kbd">↑↓</kbd> 이동</span>
        <span><kbd className="mmcp-kbd">Enter</kbd> 열기</span>
        <span><kbd className="mmcp-kbd">Esc</kbd> 닫기</span>
        <span><kbd className="mmcp-kbd">G</kbd> 다음 글자로 바로 이동</span>
      </div>
    </Modal>
  )
}

export function ShortcutHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { isAdmin } = useAuth()
  const items = allNav.filter((n) => n.shortcut && (isAdmin || !n.path.startsWith('/admin')))
  return (
    <Modal open={open} onCancel={onClose} footer={null} title="키보드 단축키" width={520}>
      <table style={{ width: '100%', fontSize: 13, borderCollapse: 'collapse' }}>
        <tbody>
          {[
            ['Ctrl/⌘ + K, /', '빠른 이동 (명령 팔레트)'],
            ['?', '단축키 도움말'],
            ['Shift + T', '라이트/다크 테마 전환'],
            ...items.map((n) => [`G → ${n.shortcut!.toUpperCase()}`, n.label]),
          ].map(([k, v]) => (
            <tr key={k} style={{ borderBottom: '1px solid var(--mmcp-border)' }}>
              <td style={{ padding: '7px 4px', width: 160 }}>
                <kbd className="mmcp-kbd">{k}</kbd>
              </td>
              <td style={{ padding: '7px 4px' }}>{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Modal>
  )
}

/** Global keyboard shortcuts: ⌘K / "/" palette, "?" help, "g x" navigation. */
export function useGlobalShortcuts(openPalette: () => void, openHelp: () => void) {
  const navigate = useNavigate()
  const { isAdmin } = useAuth()
  const { toggle } = useTheme()
  useEffect(() => {
    let gAt = 0
    const typing = (el: EventTarget | null) => {
      const t = el as HTMLElement | null
      return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)
    }
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        openPalette()
        return
      }
      if (e.metaKey || e.ctrlKey || e.altKey || typing(e.target) || document.querySelector('.ant-modal-wrap:not([style*="display: none"])')) return
      if (e.key === '/') {
        e.preventDefault()
        openPalette()
      } else if (e.key === '?') {
        openHelp()
      } else if (e.key === 'T' && e.shiftKey) {
        toggle()
      } else if (e.key.toLowerCase() === 'g') {
        gAt = Date.now()
      } else if (Date.now() - gAt < 1200) {
        const hit = allNav.find((n) => n.shortcut === e.key.toLowerCase() && (isAdmin || !n.path.startsWith('/admin')))
        if (hit) {
          e.preventDefault()
          navigate(hit.path)
        }
        gAt = 0
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate, openPalette, openHelp, isAdmin, toggle])
}
