import type { ReactNode } from 'react'
import {
  DashboardOutlined, TeamOutlined, SafetyCertificateOutlined, KeyOutlined, AuditOutlined, FileSearchOutlined,
  SettingOutlined, CloudDownloadOutlined, InfoCircleOutlined, ToolOutlined, AppstoreOutlined, LinkOutlined, LockOutlined,
  DesktopOutlined, GlobalOutlined, CheckSquareOutlined, HomeOutlined, ThunderboltOutlined, HistoryOutlined, UserOutlined,
  CommentOutlined, IdcardOutlined, ClusterOutlined, FieldTimeOutlined, ControlOutlined,
} from '@ant-design/icons'

export interface NavItem {
  key: string
  path: string
  label: string
  icon: ReactNode
  group: string
  description: string
  keywords?: string
  shortcut?: string // second key after "g"
}

export const adminNav: NavItem[] = [
  { key: 'dashboard', path: '/admin', label: '대시보드', icon: <DashboardOutlined />, group: '개요', description: '호출 현황, 구성요소 상태, 경고', keywords: 'dashboard 현황 상태', shortcut: 'd' },
  { key: 'users', path: '/admin/users', label: '사용자', icon: <TeamOutlined />, group: '아이덴티티', description: 'mmcp 계정, 역할, 잠금 해제', keywords: 'users 계정 역할', shortcut: 'u' },
  { key: 'mappings', path: '/admin/mappings', label: '계정 매핑', icon: <IdcardOutlined />, group: '아이덴티티', description: 'Keycloak sub ↔ Mattermost user_id', keywords: 'mapping 매핑 충돌', shortcut: 'm' },
  { key: 'keycloak', path: '/admin/integrations/keycloak', label: 'Keycloak 연동', icon: <SafetyCertificateOutlined />, group: '아이덴티티', description: 'OIDC, 사일런트 SSO, 역할 매핑', keywords: 'keycloak sso oidc oauth' },
  { key: 'mattermost', path: '/admin/integrations/mattermost', label: 'Mattermost 연동', icon: <CommentOutlined />, group: '아이덴티티', description: 'REST API, 서비스/봇 토큰, 발신 계정', keywords: 'mattermost bot token 봇' },
  { key: 'tools', path: '/admin/mcp/tools', label: '도구 정책', icon: <ToolOutlined />, group: 'MCP', description: '도구별 활성화, 승인, 역할', keywords: 'tools 도구 승인', shortcut: 't' },
  { key: 'policies', path: '/admin/mcp/policies', label: '데이터·호출 정책', icon: <ControlOutlined />, group: 'MCP', description: 'DM/비공개 채널, 호출 한도, OAuth', keywords: 'policy 정책 rate limit dm' },
  { key: 'clients', path: '/admin/mcp/clients', label: '클라이언트·세션', icon: <AppstoreOutlined />, group: 'MCP', description: 'OAuth 클라이언트, MCP 세션', keywords: 'clients sessions oauth' },
  { key: 'keys', path: '/admin/security/keys', label: '키 관리', icon: <KeyOutlined />, group: '보안', description: '마스터 키, 데이터 키 회전, 토큰 회전', keywords: 'keys 키 회전 암호화 rotation', shortcut: 'k' },
  { key: 'api-keys', path: '/admin/security/api-keys', label: 'API 키 현황', icon: <LockOutlined />, group: '보안', description: '전체 사용자 API 키', keywords: 'api key 개인키' },
  { key: 'sessions', path: '/admin/security/sessions', label: '웹 세션', icon: <DesktopOutlined />, group: '보안', description: '로그인 세션 강제 종료', keywords: 'sessions 세션' },
  { key: 'access', path: '/admin/security/access', label: '접근 제어', icon: <GlobalOutlined />, group: '보안', description: 'IP 허용목록, CORS, 로그인 잠금', keywords: 'ip acl cors 잠금' },
  { key: 'audit', path: '/admin/audit', label: '감사 로그', icon: <AuditOutlined />, group: '감사', description: '도구 호출, 로그인, 관리 작업', keywords: 'audit 감사', shortcut: 'a' },
  { key: 'approvals', path: '/admin/approvals', label: '승인 이력', icon: <CheckSquareOutlined />, group: '감사', description: '쓰기 도구 승인 요청', keywords: 'approval 승인' },
  { key: 'logs', path: '/admin/system/logs', label: '서버 로그', icon: <FileSearchOutlined />, group: '시스템', description: '실시간 로그, 레벨 변경, 다운로드', keywords: 'logs 로그', shortcut: 'l' },
  { key: 'settings', path: '/admin/system/settings', label: '일반 설정', icon: <SettingOutlined />, group: '시스템', description: 'Public URL, 세션, 보존 기간', keywords: 'settings 설정 public url', shortcut: 's' },
  { key: 'backup', path: '/admin/system/backup', label: '백업·복원', icon: <CloudDownloadOutlined />, group: '시스템', description: '설정 내보내기/가져오기', keywords: 'backup 백업 복원 export import' },
  { key: 'about', path: '/admin/system/about', label: '시스템 정보', icon: <InfoCircleOutlined />, group: '시스템', description: '버전, 마이그레이션, 구성요소', keywords: 'about version 버전' },
]

export const personalNav: NavItem[] = [
  { key: 'me', path: '/me', label: '내 홈', icon: <HomeOutlined />, group: '내 공간', description: '연결 상태와 보안 점검', keywords: 'home 홈', shortcut: 'h' },
  { key: 'connect', path: '/me/connect', label: 'MCP 연결 가이드', icon: <LinkOutlined />, group: '내 공간', description: 'Claude Code, Codex, Cursor 연결', keywords: 'connect 연결 claude codex cursor', shortcut: 'c' },
  { key: 'my-keys', path: '/me/keys', label: '내 API 키', icon: <KeyOutlined />, group: '키·자격증명', description: '발급, 회전, 폐기', keywords: 'api key 키 회전', shortcut: 'i' },
  { key: 'my-mattermost', path: '/me/mattermost', label: 'Mattermost 연결', icon: <CommentOutlined />, group: '키·자격증명', description: '계정 매핑과 개인 토큰', keywords: 'mattermost token pat 토큰' },
  { key: 'my-encryption', path: '/me/encryption', label: '개인 암호화 키', icon: <ClusterOutlined />, group: '키·자격증명', description: '데이터 키 버전과 회전', keywords: 'encryption dek 암호화' },
  { key: 'playground', path: '/me/playground', label: '도구 실행기', icon: <ThunderboltOutlined />, group: '도구', description: 'MCP 도구를 직접 실행', keywords: 'playground 실행 테스트', shortcut: 'r' },
  { key: 'my-approvals', path: '/me/approvals', label: '승인 요청', icon: <CheckSquareOutlined />, group: '도구', description: 'AI가 요청한 쓰기 작업 승인', keywords: 'approval 승인' },
  { key: 'activity', path: '/me/activity', label: '내 활동', icon: <HistoryOutlined />, group: '계정', description: '도구 호출과 로그인 기록', keywords: 'activity 활동 기록' },
  { key: 'my-sessions', path: '/me/sessions', label: '세션·연결된 앱', icon: <FieldTimeOutlined />, group: '계정', description: '브라우저, MCP 클라이언트', keywords: 'sessions 세션 앱' },
  { key: 'profile', path: '/me/profile', label: '프로필·환경설정', icon: <UserOutlined />, group: '계정', description: '이름, 비밀번호, 테마', keywords: 'profile 프로필 비밀번호 테마', shortcut: 'p' },
]

export const allNav = [...adminNav, ...personalNav]

export function findNav(pathname: string): NavItem | undefined {
  let best: NavItem | undefined
  for (const n of allNav) {
    if (pathname === n.path || pathname.startsWith(n.path + '/')) {
      if (!best || n.path.length > best.path.length) best = n
    }
  }
  return best
}
