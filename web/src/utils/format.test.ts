import { describe, expect, it } from 'vitest'
import { chosung, matchScore } from './format'

describe('quick navigation matching', () => {
  it('extracts Korean initial consonants', () => {
    expect(chosung('대시보드')).toBe('ㄷㅅㅂㄷ')
    expect(chosung('API 키')).toBe('API ㅋ')
  })
  it('ranks prefix > substring > 초성 > fuzzy', () => {
    expect(matchScore('대시', '대시보드')).toBe(100)
    expect(matchScore('보드', '대시보드')).toBe(80)
    expect(matchScore('ㄷㅅㅂㄷ', '대시보드')).toBe(70)
    expect(matchScore('dsb', 'dashboard')).toBe(40)
    expect(matchScore('zzz', '대시보드')).toBe(0)
  })
  it('descriptions only match as plain substrings', () => {
    expect(matchScore('ㄱㅅ', '대시보드', '', '구성요소 상태')).toBe(0)
    expect(matchScore('구성', '대시보드', '', '구성요소 상태')).toBe(30)
  })
})
