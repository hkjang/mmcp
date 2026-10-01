import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'

const version = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim()
const backend = process.env.MMCP_DEV_BACKEND ?? 'http://127.0.0.1:18088'
const proxied = ['/api', '/mcp', '/oauth/authorize', '/oauth/token', '/oauth/register', '/oauth/revoke', '/auth', '/.well-known', '/metrics']

export default defineConfig({
  plugins: [react()],
  define: { 'import.meta.env.VITE_APP_VERSION': JSON.stringify(version) },
  server: { proxy: Object.fromEntries(proxied.map((p) => [p, { target: backend, changeOrigin: false }])) },
  build: {
    chunkSizeWarningLimit: 1600,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/antd') || id.includes('node_modules/@ant-design') || id.includes('node_modules/rc-')) return 'antd'
          if (id.includes('node_modules/react') || id.includes('node_modules/scheduler')) return 'react'
        },
      },
    },
  },
})
