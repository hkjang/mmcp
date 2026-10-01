import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  define: { 'import.meta.env.VITE_APP_VERSION': JSON.stringify('test') },
  test: { environment: 'jsdom', globals: true, setupFiles: ['./src/test-setup.ts'] },
})
