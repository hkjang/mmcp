#!/usr/bin/env node
// Captures every mmcp screen into docs/screenshots/*.webp for the guides and
// the GitHub Pages site. Run against a dev instance seeded with
// scripts/dev/seed-*.sh:
//   SCREENSHOT_USERNAME=hkjang SCREENSHOT_PASSWORD=... node scripts/capture-screenshots.mjs
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const req = createRequire(join(root, 'web', 'package.json'))
const { chromium } = req('@playwright/test')
const sharp = req('sharp')
const B = process.env.SCREENSHOT_BASE_URL ?? 'http://127.0.0.1:18088'
const OUT = resolve(process.env.SCREENSHOT_OUTPUT_DIR ?? join(root, 'docs', 'screenshots'))
const USER = process.env.SCREENSHOT_USERNAME
const PASS = process.env.SCREENSHOT_PASSWORD
if (!USER || !PASS) {
  console.error('SCREENSHOT_USERNAME and SCREENSHOT_PASSWORD are required')
  process.exit(2)
}
await mkdir(OUT, { recursive: true })

const browser = await chromium.launch()
async function save(page, file, opts = {}) {
  const png = await page.screenshot({ fullPage: false, ...opts })
  await sharp(png).webp({ quality: 82 }).toFile(join(OUT, file))
  console.log('captured', file)
}
const settle = async (page, ms = 500) => {
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(ms)
}
async function login(page) {
  await page.goto(B + '/login?sso=none')
  await page.getByLabel('아이디').fill(USER)
  await page.getByLabel('비밀번호').fill(PASS)
  await page.getByRole('button', { name: '로그인', exact: true }).click()
  await page.waitForURL(/\/(admin|me)/)
}

const desktop = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'ko-KR', colorScheme: 'light' })
const page = await desktop.newPage()
await page.addInitScript(() => localStorage.setItem('mmcp.theme', JSON.stringify('light')))
await page.goto(B + '/login?sso=none')
await settle(page, 800)
await save(page, 'login.webp')
await login(page)

const captures = [
  { file: 'dashboard.webp', path: '/admin' },
  { file: 'users.webp', path: '/admin/users', click: '.ant-table-row' },
  { file: 'mappings.webp', path: '/admin/mappings', click: '.ant-table-row' },
  { file: 'keycloak.webp', path: '/admin/integrations/keycloak', action: async (p) => { await p.getByRole('button', { name: /연결 테스트/ }).click(); await p.waitForTimeout(1200) } },
  { file: 'mattermost.webp', path: '/admin/integrations/mattermost', action: async (p) => { await p.getByRole('button', { name: /연결 테스트/ }).click(); await p.waitForTimeout(1200) } },
  { file: 'tools.webp', path: '/admin/mcp/tools', action: async (p) => { await p.locator('.ant-table-row', { hasText: 'mattermost_send_dm' }).locator('td').first().click(); await p.waitForTimeout(400) } },
  { file: 'policies.webp', path: '/admin/mcp/policies' },
  { file: 'clients.webp', path: '/admin/mcp/clients' },
  { file: 'key-management.webp', path: '/admin/security/keys' },
  { file: 'api-keys-admin.webp', path: '/admin/security/api-keys' },
  { file: 'web-sessions.webp', path: '/admin/security/sessions' },
  { file: 'access-control.webp', path: '/admin/security/access' },
  { file: 'audit.webp', path: '/admin/audit', click: '.ant-table-row' },
  { file: 'approvals-admin.webp', path: '/admin/approvals' },
  { file: 'server-logs.webp', path: '/admin/system/logs', action: async (p) => { await p.locator('.mmcp-log-row').last().click(); await p.waitForTimeout(300) } },
  { file: 'general-settings.webp', path: '/admin/system/settings' },
  { file: 'backup.webp', path: '/admin/system/backup' },
  { file: 'about.webp', path: '/admin/system/about' },
  { file: 'me-home.webp', path: '/me' },
  { file: 'connect.webp', path: '/me/connect' },
  { file: 'my-keys.webp', path: '/me/keys' },
  { file: 'my-mattermost.webp', path: '/me/mattermost' },
  { file: 'my-encryption.webp', path: '/me/encryption' },
  {
    file: 'playground.webp', path: '/me/playground?tool=mattermost_send_dm',
    action: async (p) => {
      await p.locator('#tool-mattermost_send_dm_recipient').fill('lee')
      await p.locator('#tool-mattermost_send_dm_message').fill('내일 오전 10시 회고 회의 잊지 마세요 🙂')
      await p.getByRole('button', { name: /실행/ }).click()
      await p.waitForSelector('text=실행 전 확인이 필요합니다')
      await p.waitForTimeout(300)
    },
  },
  { file: 'playground-search.webp', path: '/me/playground?tool=mattermost_search_posts', action: async (p) => { await p.getByRole('button', { name: /실행/ }).click(); await p.waitForTimeout(1500) } },
  { file: 'my-approvals.webp', path: '/me/approvals' },
  { file: 'activity.webp', path: '/me/activity' },
  { file: 'my-sessions.webp', path: '/me/sessions' },
  { file: 'profile.webp', path: '/me/profile' },
]
for (const c of captures) {
  await page.goto(B + c.path)
  await settle(page)
  if (c.click) {
    await page.locator(c.click).first().click()
    await page.waitForTimeout(500)
  }
  if (c.action) await c.action(page)
  await save(page, c.file)
}

// interaction states
await page.goto(B + '/admin')
await settle(page)
await page.getByTestId('profile-menu').click()
await page.waitForTimeout(400)
await save(page, 'profile-menu.webp')
await page.keyboard.press('Escape')
await page.keyboard.press('Control+k')
await page.waitForTimeout(300)
await page.keyboard.type('ㅁㅍ')
await page.waitForTimeout(300)
await save(page, 'command-palette.webp')
await page.keyboard.press('Escape')

// OAuth consent for a freshly registered client
const reg = await (await fetch(B + '/oauth/register', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ client_name: 'Claude Code', redirect_uris: ['http://127.0.0.1:53682/callback'] }) })).json()
await page.goto(`${B}/oauth/authorize?response_type=code&client_id=${reg.client_id}&redirect_uri=http://127.0.0.1:53682/callback&code_challenge=${'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM'}&code_challenge_method=S256&state=demo&scope=mcp`)
await page.waitForSelector('[data-testid=consent-approve]')
await page.waitForTimeout(300)
await save(page, 'oauth-consent.webp')

// dark theme
const dark = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'ko-KR', storageState: await desktop.storageState() })
const dp = await dark.newPage()
await dp.addInitScript(() => localStorage.setItem('mmcp.theme', JSON.stringify('dark')))
await dp.goto(B + '/admin')
await settle(dp, 800)
await save(dp, 'dark-dashboard.webp')
await dp.goto(B + '/me/playground?tool=mattermost_unread')
await settle(dp)
await dp.getByRole('button', { name: /실행/ }).click()
await dp.waitForTimeout(1500)
await save(dp, 'dark-playground.webp')

// mobile
const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true, locale: 'ko-KR', storageState: await desktop.storageState() })
const mp = await mobile.newPage()
await mp.addInitScript(() => localStorage.setItem('mmcp.theme', JSON.stringify('light')))
await mp.goto(B + '/admin')
await settle(mp, 800)
await save(mp, 'mobile-dashboard.webp')
await mp.getByLabel('메뉴 열기').click()
await mp.waitForTimeout(500)
await save(mp, 'mobile-nav.webp')
await mp.goto(B + '/me/connect')
await settle(mp)
await save(mp, 'mobile-connect.webp')
await browser.close()
