export class ApiError extends Error {
  status: number
  code: string
  details?: unknown
  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message)
    this.status = status
    this.code = code
    this.details = details
  }
}

type Options = { method?: string; body?: unknown; query?: Record<string, string | number | boolean | undefined | null>; signal?: AbortSignal }

export function buildQuery(query?: Options['query']): string {
  if (!query) return ''
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== null && v !== '') p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

export async function api<T = unknown>(path: string, opts: Options = {}): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (method !== 'GET') headers['X-Requested-With'] = 'mmcp'
  let body: BodyInit | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  const res = await fetch(`/api${path}${buildQuery(opts.query)}`, { method, headers, body, credentials: 'same-origin', signal: opts.signal })
  const text = await res.text()
  let data: unknown = undefined
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  if (!res.ok) {
    const err = (data as { error?: { code?: string; message?: string; details?: unknown } } | undefined)?.error
    if (res.status === 401 && !path.startsWith('/auth/')) window.dispatchEvent(new CustomEvent('mmcp:unauthorized'))
    throw new ApiError(res.status, err?.code ?? `HTTP_${res.status}`, err?.message ?? res.statusText ?? '요청에 실패했습니다', err?.details)
  }
  return data as T
}

export const get = <T,>(path: string, query?: Options['query']) => api<T>(path, { query })
export const post = <T,>(path: string, body?: unknown) => api<T>(path, { method: 'POST', body: body ?? {} })
export const put = <T,>(path: string, body?: unknown) => api<T>(path, { method: 'PUT', body: body ?? {} })
export const del = <T,>(path: string) => api<T>(path, { method: 'DELETE' })

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return String(e)
}
