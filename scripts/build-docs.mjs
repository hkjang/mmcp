#!/usr/bin/env node
// Renders docs/USER_GUIDE.md and docs/ADMIN_GUIDE.md into styled HTML pages
// for GitHub Pages (Markdown is served raw there). Mermaid blocks render in
// the browser on the public docs site only; the product itself stays offline.
import { createRequire } from 'node:module'
import { readFile, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const req = createRequire(join(root, 'web', 'package.json'))
const { marked } = req('marked')
const version = (await readFile(join(root, 'VERSION'), 'utf8')).trim()

const slug = (s) => s.toLowerCase().replace(/<[^>]+>/g, '').replace(/[^\p{L}\p{N}\s-]/gu, '').trim().replace(/\s/g, '-')
const pages = [
  { src: 'USER_GUIDE.md', out: 'user-guide.html', title: '사용자 가이드', desc: 'mmcp 사용자 가이드: Keycloak SSO 로그인, Mattermost 계정 연결, Claude Code·Codex·Cursor MCP 연결, API 키 회전, 승인 흐름, 도구 목록과 문제 해결.' },
  { src: 'ADMIN_GUIDE.md', out: 'admin-guide.html', title: '관리자 가이드', desc: 'mmcp 관리자 가이드: 오프라인 설치, Keycloak·Mattermost 연동, 계정 매핑과 충돌 처리, 도구 정책, 키 관리와 회전, 감사 로그, 운영과 문제 해결.' },
]
for (const p of pages) {
  const md = await readFile(join(root, 'docs', p.src), 'utf8')
  const toc = []
  const renderer = new marked.Renderer()
  renderer.heading = function ({ tokens, depth }) {
    const text = this.parser.parseInline(tokens)
    const id = slug(text)
    if (depth === 2 || depth === 3) toc.push({ depth, text: text.replace(/<[^>]+>/g, ''), id })
    return `<h${depth} id="${id}"><a class="anchor" href="#${id}" aria-hidden="true">#</a>${text}</h${depth}>\n`
  }
  renderer.code = function ({ text, lang }) {
    if (lang === 'mermaid') return `<pre class="mermaid">${text.replace(/</g, '&lt;')}</pre>\n`
    return `<pre class="code"><code>${text.replace(/&/g, '&amp;').replace(/</g, '&lt;')}</code></pre>\n`
  }
  renderer.image = ({ href, text }) => `<img src="${href}" alt="${text}" loading="lazy" decoding="async" class="doc-shot" />`
  let body = marked.parse(md, { renderer, gfm: true })
  // links between the two guides point to their HTML versions
  body = body.replaceAll('href="USER_GUIDE.md', 'href="user-guide.html').replaceAll('href="ADMIN_GUIDE.md', 'href="admin-guide.html')
  const html = `<!doctype html>
<html lang="ko">
<head>
  <meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>${p.title} — mmcp v${version} Mattermost MCP Gateway</title>
  <meta name="description" content="${p.desc}" />
  <link rel="canonical" href="https://hkjang.github.io/mmcp/${p.out}" />
  <link rel="icon" type="image/svg+xml" href="favicon.svg" />
  <meta property="og:title" content="mmcp ${p.title}" /><meta property="og:description" content="${p.desc}" />
  <meta property="og:image" content="https://hkjang.github.io/mmcp/og-card.png" /><meta property="og:type" content="article" />
  <link rel="stylesheet" href="styles.css" />
  <script type="application/ld+json">${JSON.stringify({ '@context': 'https://schema.org', '@type': 'TechArticle', headline: `mmcp ${p.title}`, description: p.desc, inLanguage: 'ko', version, url: `https://hkjang.github.io/mmcp/${p.out}`, about: { '@id': 'https://hkjang.github.io/mmcp/#app' }, author: { '@type': 'Person', name: 'hkjang' } })}</script>
</head>
<body class="doc">
  <header class="nav"><div class="wrap nav-inner">
    <a class="brand" href="./"><img src="favicon.svg" width="34" height="34" alt="" /><span><b>mmcp</b><small>${p.title} · v${version}</small></span></a>
    <nav aria-label="문서"><a href="./">홈</a><a href="user-guide.html">사용자 가이드</a><a href="admin-guide.html">관리자 가이드</a><a class="gh" href="https://github.com/hkjang/mmcp">GitHub</a></nav>
  </div></header>
  <div class="wrap doc-grid">
    <aside class="doc-toc" aria-label="목차"><b>목차</b>${toc.map((t) => `<a class="d${t.depth}" href="#${t.id}">${t.text}</a>`).join('')}</aside>
    <article class="doc-body">${body}</article>
  </div>
  <footer class="footer"><div class="wrap"><span>mmcp v${version} · MIT License</span><span><a href="./">홈</a> · <a href="https://github.com/hkjang/mmcp/releases">다운로드</a></span></div></footer>
  <script type="module">
    import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs'
    mermaid.initialize({ startOnLoad: true, theme: matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default' })
  </script>
</body>
</html>
`
  await writeFile(join(root, 'docs', p.out), html)
  console.log('wrote docs/' + p.out, `(${toc.length} headings)`)
}
