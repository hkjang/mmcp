import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ConfigProvider, App as AntApp, theme as antTheme } from 'antd'
import koKR from 'antd/locale/ko_KR'

export type ThemeMode = 'light' | 'dark' | 'system'

interface ThemeState {
  mode: ThemeMode
  resolved: 'light' | 'dark'
  compact: boolean
  setMode: (m: ThemeMode) => void
  toggle: () => void
  setCompact: (c: boolean) => void
}

const Ctx = createContext<ThemeState | null>(null)

const read = <T,>(k: string, d: T): T => {
  try {
    const v = localStorage.getItem(k)
    return v === null ? d : (JSON.parse(v) as T)
  } catch {
    return d
  }
}
const write = (k: string, v: unknown) => {
  try {
    localStorage.setItem(k, JSON.stringify(v))
  } catch {
    /* storage may be blocked */
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => read<ThemeMode>('mmcp.theme', 'system'))
  const [compact, setCompactState] = useState<boolean>(() => read('mmcp.compact', false))
  const [systemDark, setSystemDark] = useState(() => window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false)

  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!mq) return
    const h = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])

  const resolved: 'light' | 'dark' = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode
  useEffect(() => {
    document.documentElement.dataset.theme = resolved
    document.documentElement.style.colorScheme = resolved
  }, [resolved])

  const setMode = useCallback((m: ThemeMode) => {
    setModeState(m)
    write('mmcp.theme', m)
  }, [])
  const setCompact = useCallback((c: boolean) => {
    setCompactState(c)
    write('mmcp.compact', c)
  }, [])
  const toggle = useCallback(() => setMode(resolved === 'dark' ? 'light' : 'dark'), [resolved, setMode])

  const algorithm = useMemo(() => {
    const a = [resolved === 'dark' ? antTheme.darkAlgorithm : antTheme.defaultAlgorithm]
    if (compact) a.push(antTheme.compactAlgorithm)
    return a
  }, [resolved, compact])

  const value = useMemo(() => ({ mode, resolved, compact, setMode, toggle, setCompact }), [mode, resolved, compact, setMode, toggle, setCompact])
  return (
    <Ctx.Provider value={value}>
      <ConfigProvider
        locale={koKR}
        theme={{
          algorithm,
          token: {
            colorPrimary: '#2563eb',
            colorInfo: '#2563eb',
            borderRadius: 8,
            fontFamily: "'Pretendard', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Apple SD Gothic Neo', 'Malgun Gothic', sans-serif",
            colorBgLayout: resolved === 'dark' ? '#0b1020' : '#f4f6fb',
          },
          components: {
            Layout: { siderBg: '#0f172a', headerBg: resolved === 'dark' ? 'rgba(15,23,42,.82)' : 'rgba(255,255,255,.85)' },
            Menu: { darkItemBg: 'transparent', darkSubMenuItemBg: 'transparent', darkItemSelectedBg: '#2563eb', itemBorderRadius: 8 },
            Table: { headerBg: resolved === 'dark' ? '#111a2e' : '#f8fafc' },
            Card: { borderRadiusLG: 12 },
          },
        }}
      >
        <AntApp>{children}</AntApp>
      </ConfigProvider>
    </Ctx.Provider>
  )
}

export function useTheme() {
  const v = useContext(Ctx)
  if (!v) throw new Error('useTheme outside provider')
  return v
}
