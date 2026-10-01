import { lazy, Suspense, type ReactNode } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Result, Spin, Button } from 'antd'
import { useAuth } from './contexts/AuthContext'
import { AppShell } from './layout/AppShell'
import LoginPage from './pages/LoginPage'
import ConsentPage from './pages/ConsentPage'

const Dashboard = lazy(() => import('./pages/admin/Dashboard'))
const Users = lazy(() => import('./pages/admin/Users'))
const Mappings = lazy(() => import('./pages/admin/Mappings'))
const KeycloakSettings = lazy(() => import('./pages/admin/KeycloakSettings'))
const MattermostSettings = lazy(() => import('./pages/admin/MattermostSettings'))
const Tools = lazy(() => import('./pages/admin/Tools'))
const Policies = lazy(() => import('./pages/admin/Policies'))
const Clients = lazy(() => import('./pages/admin/Clients'))
const KeyManagement = lazy(() => import('./pages/admin/KeyManagement'))
const AdminApiKeys = lazy(() => import('./pages/admin/AdminApiKeys'))
const AdminSessions = lazy(() => import('./pages/admin/AdminSessions'))
const AccessControl = lazy(() => import('./pages/admin/AccessControl'))
const Audit = lazy(() => import('./pages/admin/Audit'))
const AdminApprovals = lazy(() => import('./pages/admin/AdminApprovals'))
const Logs = lazy(() => import('./pages/admin/Logs'))
const GeneralSettings = lazy(() => import('./pages/admin/GeneralSettings'))
const Backup = lazy(() => import('./pages/admin/Backup'))
const About = lazy(() => import('./pages/admin/About'))
const MeHome = lazy(() => import('./pages/me/Home'))
const Connect = lazy(() => import('./pages/me/Connect'))
const MyKeys = lazy(() => import('./pages/me/MyKeys'))
const MyMattermost = lazy(() => import('./pages/me/MyMattermost'))
const MyEncryption = lazy(() => import('./pages/me/MyEncryption'))
const Playground = lazy(() => import('./pages/me/Playground'))
const MyApprovals = lazy(() => import('./pages/me/MyApprovals'))
const Activity = lazy(() => import('./pages/me/Activity'))
const MySessions = lazy(() => import('./pages/me/MySessions'))
const Profile = lazy(() => import('./pages/me/Profile'))

const Loading = () => (
  <div style={{ display: 'grid', placeItems: 'center', minHeight: 320 }}>
    <Spin size="large" />
  </div>
)

function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth()
  const loc = useLocation()
  if (loading) return <Loading />
  if (!user) return <Navigate to={`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />
  return <>{children}</>
}

function RequireAdmin({ children }: { children: ReactNode }) {
  const { isAdmin } = useAuth()
  if (!isAdmin)
    return <Result status="403" title="관리자 전용 화면" subTitle="서비스 관리자 권한이 필요합니다." extra={<Button href="/me" type="primary">내 공간으로</Button>} />
  return <>{children}</>
}

function Home() {
  const { isAdmin } = useAuth()
  return <Navigate to={isAdmin ? '/admin' : '/me'} replace />
}

const admin = (el: ReactNode) => <RequireAdmin>{el}</RequireAdmin>

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/oauth/consent" element={<RequireAuth><ConsentPage /></RequireAuth>} />
      <Route element={<RequireAuth><Suspense fallback={<Loading />}><AppShell /></Suspense></RequireAuth>}>
        <Route index element={<Home />} />
        <Route path="/admin" element={admin(<Suspense fallback={<Loading />}><Dashboard /></Suspense>)} />
        <Route path="/admin/users" element={admin(<Suspense fallback={<Loading />}><Users /></Suspense>)} />
        <Route path="/admin/mappings" element={admin(<Suspense fallback={<Loading />}><Mappings /></Suspense>)} />
        <Route path="/admin/integrations/keycloak" element={admin(<Suspense fallback={<Loading />}><KeycloakSettings /></Suspense>)} />
        <Route path="/admin/integrations/mattermost" element={admin(<Suspense fallback={<Loading />}><MattermostSettings /></Suspense>)} />
        <Route path="/admin/mcp/tools" element={admin(<Suspense fallback={<Loading />}><Tools /></Suspense>)} />
        <Route path="/admin/mcp/policies" element={admin(<Suspense fallback={<Loading />}><Policies /></Suspense>)} />
        <Route path="/admin/mcp/clients" element={admin(<Suspense fallback={<Loading />}><Clients /></Suspense>)} />
        <Route path="/admin/security/keys" element={admin(<Suspense fallback={<Loading />}><KeyManagement /></Suspense>)} />
        <Route path="/admin/security/api-keys" element={admin(<Suspense fallback={<Loading />}><AdminApiKeys /></Suspense>)} />
        <Route path="/admin/security/sessions" element={admin(<Suspense fallback={<Loading />}><AdminSessions /></Suspense>)} />
        <Route path="/admin/security/access" element={admin(<Suspense fallback={<Loading />}><AccessControl /></Suspense>)} />
        <Route path="/admin/audit" element={admin(<Suspense fallback={<Loading />}><Audit /></Suspense>)} />
        <Route path="/admin/approvals" element={admin(<Suspense fallback={<Loading />}><AdminApprovals /></Suspense>)} />
        <Route path="/admin/system/logs" element={admin(<Suspense fallback={<Loading />}><Logs /></Suspense>)} />
        <Route path="/admin/system/settings" element={admin(<Suspense fallback={<Loading />}><GeneralSettings /></Suspense>)} />
        <Route path="/admin/system/backup" element={admin(<Suspense fallback={<Loading />}><Backup /></Suspense>)} />
        <Route path="/admin/system/about" element={admin(<Suspense fallback={<Loading />}><About /></Suspense>)} />
        <Route path="/me" element={<Suspense fallback={<Loading />}><MeHome /></Suspense>} />
        <Route path="/me/connect" element={<Suspense fallback={<Loading />}><Connect /></Suspense>} />
        <Route path="/me/keys" element={<Suspense fallback={<Loading />}><MyKeys /></Suspense>} />
        <Route path="/me/mattermost" element={<Suspense fallback={<Loading />}><MyMattermost /></Suspense>} />
        <Route path="/me/encryption" element={<Suspense fallback={<Loading />}><MyEncryption /></Suspense>} />
        <Route path="/me/playground" element={<Suspense fallback={<Loading />}><Playground /></Suspense>} />
        <Route path="/me/approvals" element={<Suspense fallback={<Loading />}><MyApprovals /></Suspense>} />
        <Route path="/me/activity" element={<Suspense fallback={<Loading />}><Activity /></Suspense>} />
        <Route path="/me/sessions" element={<Suspense fallback={<Loading />}><MySessions /></Suspense>} />
        <Route path="/me/profile" element={<Suspense fallback={<Loading />}><Profile /></Suspense>} />
        <Route path="*" element={<Result status="404" title="페이지를 찾을 수 없습니다" extra={<Button href="/" type="primary">홈으로</Button>} />} />
      </Route>
    </Routes>
  )
}
