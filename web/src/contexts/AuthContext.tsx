import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ApiError, get, post } from '../api/client'
import type { BuildInfo, MeResponse, User } from '../api/types'

interface AuthState {
  user: User | null
  isAdmin: boolean
  version: BuildInfo | null
  loading: boolean
  refresh: () => Promise<User | null>
  setMe: (me: MeResponse) => void
  logout: () => Promise<void>
}

const Ctx = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMeState] = useState<MeResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const qc = useQueryClient()

  const refresh = useCallback(async () => {
    try {
      const r = await get<MeResponse | { user: null }>('/auth/me')
      if (!r.user) {
        setMeState(null)
        return null
      }
      setMeState(r as MeResponse)
      try {
        // a successful sign-in resets the silent-SSO loop guard so a later
        // session expiry can silently re-authenticate again
        sessionStorage.removeItem('mmcp.sso.tried')
      } catch {
        /* ignore */
      }
      return r.user
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 401) console.warn(e)
      setMeState(null)
      return null
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // A 401 from any API call means the session expired: go back through the
  // login page, which retries Keycloak silent SSO before showing a form.
  useEffect(() => {
    const onUnauthorized = () => {
      setMeState(null)
      const here = window.location.pathname + window.location.search
      if (!window.location.pathname.startsWith('/login')) window.location.assign(`/login?next=${encodeURIComponent(here)}&expired=1`)
    }
    window.addEventListener('mmcp:unauthorized', onUnauthorized)
    return () => window.removeEventListener('mmcp:unauthorized', onUnauthorized)
  }, [])

  const logout = useCallback(async () => {
    let redirect = ''
    try {
      const r = await post<{ redirect?: string }>('/auth/logout')
      redirect = r.redirect ?? ''
    } catch {
      /* ignore */
    }
    try {
      sessionStorage.setItem('mmcp.sso.skip', '1') // don't silently sign back in right after logout
    } catch {
      /* ignore */
    }
    qc.clear()
    setMeState(null)
    window.location.assign(redirect || '/login?logged_out=1')
  }, [qc])

  const value = useMemo<AuthState>(
    () => ({ user: me?.user ?? null, isAdmin: !!me?.is_admin, version: me?.version ?? null, loading, refresh, setMe: setMeState, logout }),
    [me, loading, refresh, logout],
  )
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useAuth() {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth outside provider')
  return v
}
