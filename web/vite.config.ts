import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const daemonPort = process.env.PASEO_SERVICE_DAEMON_PORT ?? process.env.CAMSTATION_DEV_PORT ?? '18080'
const daemonTarget = process.env.CAMSTATION_DEV_SERVER_URL ?? `http://127.0.0.1:${daemonPort}`

// https://vite.dev/config/
export default defineConfig({
  base: process.env.CAMSTATION_BASE || '/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../cmd/camstationd/web',
    emptyOutDir: true,
  },
  server: {
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: daemonTarget,
      },
      '/player': {
        target: daemonTarget,
        ws: true,
      },
    },
  },
})
