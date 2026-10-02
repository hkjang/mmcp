import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/ko'

dayjs.extend(relativeTime)
dayjs.locale('ko')

export const fmtTime = (v?: string | null) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-')
export const fmtDate = (v?: string | null) => (v ? dayjs(v).format('YYYY-MM-DD') : '-')
export const fromNow = (v?: string | null) => (v ? dayjs(v).fromNow() : '-')
export const appVersion = import.meta.env.VITE_APP_VERSION ?? '1.0.1'

const CHO = ['ㄱ', 'ㄲ', 'ㄴ', 'ㄷ', 'ㄸ', 'ㄹ', 'ㅁ', 'ㅂ', 'ㅃ', 'ㅅ', 'ㅆ', 'ㅇ', 'ㅈ', 'ㅉ', 'ㅊ', 'ㅋ', 'ㅌ', 'ㅍ', 'ㅎ']

/** 초성 문자열 ("대시보드" → "ㄷㅅㅂㄷ") for Korean initial-consonant search. */
export function chosung(s: string): string {
  let out = ''
  for (const ch of s) {
    const c = ch.charCodeAt(0) - 0xac00
    out += c >= 0 && c < 11172 ? CHO[Math.floor(c / 588)] : ch
  }
  return out
}

/** Scores a candidate against a query (0 = no match). Supports substring, 초성, and in-order fuzzy matching. */
export function matchScore(query: string, ...fields: string[]): number {
  const q = query.trim().toLowerCase().replace(/\s+/g, '')
  if (!q) return 1
  let best = 0
  // fields[0..1] are primary (label, keywords); the rest only count for plain substring hits
  for (const [idx, raw] of fields.entries()) {
    if (idx >= 2) {
      if (raw.toLowerCase().replace(/\s+/g, '').includes(q)) best = Math.max(best, 30)
      continue
    }
    const f = raw.toLowerCase().replace(/\s+/g, '')
    if (!f) continue
    if (f.startsWith(q)) best = Math.max(best, 100)
    else if (f.includes(q)) best = Math.max(best, 80)
    else if (chosung(f).includes(q)) best = Math.max(best, 70)
    else {
      let i = 0
      for (const ch of f) if (ch === q[i]) i++
      if (i === q.length) best = Math.max(best, 40)
    }
  }
  return best
}

export function copyText(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text)
  // clipboard API is unavailable on plain http (common on internal networks)
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try {
    document.execCommand('copy')
  } finally {
    document.body.removeChild(ta)
  }
  return Promise.resolve()
}

export const statusColor: Record<string, string> = {
  ok: 'success', up: 'success', active: 'success', success: 'success', approved: 'processing', executed: 'success',
  pending: 'warning', warn: 'warning', not_found: 'error', conflict: 'error', down: 'error', failure: 'error', denied: 'volcano',
  disabled: 'default', revoked: 'default', expired: 'default', retired: 'default', rejected: 'default', not_configured: 'default',
}
export const statusLabel: Record<string, string> = {
  ok: '정상', up: '정상', active: '활성', success: '성공', approved: '승인됨', executed: '실행됨', pending: '대기', warn: '주의',
  not_found: '없음', conflict: '충돌', down: '장애', failure: '실패', denied: '거부', disabled: '비활성', revoked: '폐기',
  expired: '만료', retired: '폐기됨', rejected: '거절', not_configured: '미설정',
}
export const methodLabel: Record<string, string> = { auto: '자동', manual: '수동', self: '본인 토큰' }
export const categoryLabel: Record<string, string> = {
  read: '조회', search: '검색', write: '작성', file: '파일', dm_read: 'DM 조회', dm_search: 'DM 검색', dm_write: 'DM 발송', channel_admin: '채널 관리', destructive: '위험',
  tool: '도구 호출', auth: '인증', admin: '관리', key: '키', mapping: '매핑', oauth: 'OAuth',
}
