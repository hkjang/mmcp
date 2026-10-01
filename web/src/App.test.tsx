import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from './contexts/ThemeContext'
import { AuthProvider } from './contexts/AuthContext'
import App from './App'

describe('login page', () => {
  it('shows the service version and local login form', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      const body = url.includes('/auth/config')
        ? { local_login_enabled: true, sso_enabled: false, silent_sso: false, auto_redirect: false, sso_button_label: '', login_notice: '', version: { version: 'v9.9.9', commit: 'abcdef123456' } }
        : { user: null }
      return new Response(JSON.stringify(body), { status: 200 })
    }))
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ThemeProvider>
          <MemoryRouter initialEntries={['/login?sso=none']}>
            <AuthProvider>
              <App />
            </AuthProvider>
          </MemoryRouter>
        </ThemeProvider>
      </QueryClientProvider>,
    )
    expect(await screen.findByTestId('login-version')).toHaveTextContent('mmcp v9.9.9 · abcdef1')
    expect(screen.getByLabelText('아이디')).toBeInTheDocument()
  })
})
