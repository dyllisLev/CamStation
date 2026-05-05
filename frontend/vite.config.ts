import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://10.0.0.17:8000',
      '/go2rtc': 'http://10.0.0.17:1984',
    }
  },
  test: {
    environment: 'jsdom',
    globals: true,
  }
})
