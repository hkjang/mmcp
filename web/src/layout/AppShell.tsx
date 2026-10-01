import { useCallback, useEffect, useMemo, useState } from 'react'
import { Avatar, Badge, Breadcrumb, Button, Drawer, Dropdown, Grid, Layout, Menu, Segmented, Space, Tag, Tooltip, Typography, theme, type MenuProps } from 'antd'
import {
  BellOutlined, BulbOutlined, MenuOutlined, MoonOutlined, SearchOutlined, LogoutOutlined, UserOutlined, KeyOutlined, HomeOutlined,
  SettingOutlined, QuestionCircleOutlined, InfoCircleOutlined, DesktopOutlined, SunOutlined, MenuFoldOutlined, MenuUnfoldOutlined,
} from '@ant-design/icons'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { adminNav, personalNav, findNav, type NavItem } from '../config/navigation'
import { useAuth } from '../contexts/AuthContext'
import { useTheme } from '../contexts/ThemeContext'
import { LogoMark } from '../components/Logo'
import { CommandPalette, ShortcutHelp, pushRecent, useGlobalShortcuts } from './CommandPalette'
import { get } from '../api/client'
import { appVersion } from '../utils/format'

const { Sider, Header, Content } = Layout

function groupItems(items: NavItem[]): MenuProps['items'] {
  const groups: Record<string, NavItem[]> = {}
  for (const n of items) (groups[n.group] ??= []).push(n)
  return Object.entries(groups).map(([g, list]) => ({
    type: 'group' as const,
    key: g,
    label: g,
    children: list.map((n) => ({ key: n.path, icon: n.icon, label: <Link to={n.path}>{n.label}</Link> })),
  }))
}

function SiderBody({ section, selected, collapsed, onNavigate }: { section: 'admin' | 'me'; selected: string; collapsed: boolean; onNavigate?: () => void }) {
  const { isAdmin, version } = useAuth()
  const navigate = useNavigate()
  const items = useMemo(() => groupItems(section === 'admin' ? adminNav : personalNav), [section])
  return (
    <>
      <Link to={section === 'admin' ? '/admin' : '/me'} className="mmcp-brand" onClick={onNavigate} aria-label="mmcp 홈">
        <LogoMark size={34} />
        {!collapsed && (
          <span>
            <div className="mmcp-brand-title">mmcp</div>
            <div className="mmcp-brand-sub">Mattermost MCP Gateway</div>
          </span>
        )}
      </Link>
      {isAdmin && !collapsed && (
        <div className="mmcp-section-switch">
          <Segmented
            block
            size="small"
            value={section}
            onChange={(v) => {
              navigate(v === 'admin' ? '/admin' : '/me')
              onNavigate?.()
            }}
            options={[
              { label: '관리 콘솔', value: 'admin' },
              { label: '내 공간', value: 'me' },
            ]}
          />
        </div>
      )}
      <nav className="mmcp-sider-menu" aria-label="주 메뉴">
        <Menu theme="dark" mode="inline" selectedKeys={[selected]} items={items} onClick={onNavigate} inlineCollapsed={collapsed} />
      </nav>
      <div className="mmcp-sider-fade" />
      {!collapsed && (
        <div className="mmcp-sider-footer">
          <div>mmcp v{version?.version && version.version !== 'dev' ? version.version.replace(/^v/, '') : appVersion}</div>
          <div style={{ opacity: 0.75 }}>
            <kbd className="mmcp-kbd" style={{ background: 'transparent', color: 'inherit', borderColor: 'rgba(148,163,184,.3)' }}>Ctrl K</kbd> 빠른 이동
          </div>
        </div>
      )}
    </>
  )
}

export function AppShell() {
  const { user, isAdmin, version, logout } = useAuth()
  const { mode, setMode, resolved, toggle } = useTheme()
  const location = useLocation()
  const navigate = useNavigate()
  const screens = Grid.useBreakpoint()
  const { token } = theme.useToken()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem('mmcp.collapsed') === '1'
    } catch {
      return false
    }
  })
  const section: 'admin' | 'me' = location.pathname.startsWith('/admin') ? 'admin' : 'me'
  const current = findNav(location.pathname)
  const mobile = !screens.lg

  useEffect(() => {
    if (current) pushRecent(current.path)
    document.title = `${current?.label ?? 'mmcp'} · mmcp`
  }, [current])

  const openPalette = useCallback(() => setPaletteOpen(true), [])
  const openHelp = useCallback(() => setHelpOpen(true), [])
  useGlobalShortcuts(openPalette, openHelp)

  const { data: approvals } = useQuery({
    queryKey: ['me', 'approvals', 'pending'],
    queryFn: () => get<{ items: unknown[] }>('/me/approvals', { status: 'pending' }),
    refetchInterval: 60_000,
  })
  const pending = approvals?.items.length ?? 0

  const ver = version?.version && version.version !== 'dev' ? version.version.replace(/^v/, '') : appVersion
  const profileMenu: MenuProps = {
    items: [
      {
        key: 'who',
        disabled: true,
        style: { cursor: 'default' },
        label: (
          <div style={{ padding: '4px 0', minWidth: 230 }}>
            <Typography.Text strong style={{ display: 'block' }}>{user?.display_name || user?.username}</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              @{user?.username} · {user?.source === 'keycloak' ? 'Keycloak SSO' : '로컬 계정'}
            </Typography.Text>
            <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {user?.roles.map((r) => (
                <Tag key={r} color={r === 'admin' ? 'gold' : 'blue'} style={{ marginInlineEnd: 0, fontSize: 11 }}>{r}</Tag>
              ))}
            </div>
          </div>
        ),
      },
      { type: 'divider' },
      { key: '/me', icon: <HomeOutlined />, label: '내 홈' },
      { key: '/me/keys', icon: <KeyOutlined />, label: '내 API 키' },
      { key: '/me/profile', icon: <UserOutlined />, label: '프로필·환경설정' },
      ...(isAdmin ? [{ key: section === 'admin' ? '/me' : '/admin', icon: <SettingOutlined />, label: section === 'admin' ? '내 공간으로 전환' : '관리 콘솔로 전환' }] : []),
      { type: 'divider' },
      {
        key: 'theme',
        icon: resolved === 'dark' ? <MoonOutlined /> : <SunOutlined />,
        label: '테마',
        children: [
          { key: 'theme:light', icon: <SunOutlined />, label: `라이트${mode === 'light' ? ' ✓' : ''}` },
          { key: 'theme:dark', icon: <MoonOutlined />, label: `다크${mode === 'dark' ? ' ✓' : ''}` },
          { key: 'theme:system', icon: <DesktopOutlined />, label: `시스템 설정 따름${mode === 'system' ? ' ✓' : ''}` },
        ],
      },
      { key: 'help', icon: <QuestionCircleOutlined />, label: '키보드 단축키' },
      {
        key: 'version',
        icon: <InfoCircleOutlined />,
        label: (
          <span data-testid="profile-version">
            버전 v{ver}
            {version?.commit && version.commit !== 'unknown' && <Typography.Text type="secondary" style={{ fontSize: 11, marginLeft: 6 }}>({version.commit.slice(0, 7)})</Typography.Text>}
          </span>
        ),
      },
      { type: 'divider' },
      { key: 'logout', icon: <LogoutOutlined />, danger: true, label: '로그아웃' },
    ],
    onClick: ({ key }) => {
      if (key === 'logout') void logout()
      else if (key === 'help') setHelpOpen(true)
      else if (key === 'version') navigate(isAdmin ? '/admin/system/about' : '/me/profile')
      else if (key.startsWith('theme:')) setMode(key.slice(6) as 'light' | 'dark' | 'system')
      else if (key.startsWith('/')) navigate(key)
    },
  }

  const siderWidth = 248
  return (
    <Layout style={{ minHeight: '100vh' }}>
      {!mobile && (
        <Sider
          className="mmcp-sider"
          width={siderWidth}
          collapsedWidth={72}
          collapsed={collapsed}
          style={{ position: 'sticky', top: 0, height: '100vh', overflow: 'hidden' }}
          trigger={null}
        >
          <SiderBody section={section} selected={current?.path ?? ''} collapsed={collapsed} />
        </Sider>
      )}
      {mobile && (
        <Drawer open={mobileOpen} onClose={() => setMobileOpen(false)} placement="left" width={siderWidth + 16} closable={false} styles={{ body: { padding: 0, display: 'flex', flexDirection: 'column' } }} rootClassName="mmcp-mobile-nav">
          <div className="mmcp-sider" style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
            <SiderBody section={section} selected={current?.path ?? ''} collapsed={false} onNavigate={() => setMobileOpen(false)} />
          </div>
        </Drawer>
      )}
      <Layout>
        <Header className="mmcp-header" style={{ background: token.Layout?.headerBg }}>
          {mobile ? (
            <Button type="text" icon={<MenuOutlined />} onClick={() => setMobileOpen(true)} aria-label="메뉴 열기" />
          ) : (
            <Tooltip title={collapsed ? '메뉴 펼치기' : '메뉴 접기'}>
              <Button
                type="text"
                icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                aria-label="메뉴 접기/펼치기"
                onClick={() => {
                  setCollapsed(!collapsed)
                  try {
                    localStorage.setItem('mmcp.collapsed', collapsed ? '0' : '1')
                  } catch {
                    /* ignore */
                  }
                }}
              />
            </Tooltip>
          )}
          {!mobile && (
            <Breadcrumb
              items={[{ title: section === 'admin' ? '관리 콘솔' : '내 공간' }, ...(current ? [{ title: current.group }, { title: current.label }] : [])]}
              style={{ flexShrink: 0 }}
            />
          )}
          <div style={{ flex: 1 }} />
          <button type="button" className="mmcp-search-trigger" onClick={openPalette} aria-label="빠른 이동 열기 (Ctrl+K)">
            <SearchOutlined />
            <span className="label" style={{ flex: 1, textAlign: 'left' }}>빠른 이동…</span>
            <kbd className="mmcp-kbd">Ctrl K</kbd>
          </button>
          <Space size={4}>
            <Tooltip title={pending ? `승인 대기 ${pending}건` : '승인 대기 없음'}>
              <Badge count={pending} size="small" offset={[-4, 4]}>
                <Button type="text" icon={<BellOutlined />} onClick={() => navigate('/me/approvals')} aria-label="승인 요청" />
              </Badge>
            </Tooltip>
            <Tooltip title={resolved === 'dark' ? '라이트 테마' : '다크 테마'}>
              <Button type="text" icon={resolved === 'dark' ? <BulbOutlined /> : <MoonOutlined />} onClick={toggle} aria-label="테마 전환" />
            </Tooltip>
            <Dropdown menu={profileMenu} trigger={['click']} placement="bottomRight">
              <Button type="text" style={{ height: 40, padding: '0 6px' }} aria-label="프로필 메뉴" data-testid="profile-menu">
                <Space size={8}>
                  <Avatar size={30} style={{ background: 'linear-gradient(135deg,#4f46e5,#06b6d4)', fontWeight: 600 }}>
                    {(user?.display_name || user?.username || '?').slice(0, 1).toUpperCase()}
                  </Avatar>
                  {screens.md && <span style={{ maxWidth: 140, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{user?.display_name || user?.username}</span>}
                </Space>
              </Button>
            </Dropdown>
          </Space>
        </Header>
        <Content>
          <div className="mmcp-content">
            <Outlet />
          </div>
        </Content>
      </Layout>
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} onHelp={openHelp} />
      <ShortcutHelp open={helpOpen} onClose={() => setHelpOpen(false)} />
    </Layout>
  )
}
