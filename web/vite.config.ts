import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

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
      '/api': 'http://127.0.0.1:18080',
      '/live': {
        target: 'http://127.0.0.1:18080',
        ws: true,
      },
    },
  },
})
