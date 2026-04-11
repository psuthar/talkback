import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Same-origin dev: proxy to cmd/api — use root `.env` PORT=8080 (default).
const apiTarget = 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.js'],
  },
  server: {
    port: 3000,
    open: true,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/artifacts': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/sessions': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/zoom': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/auth': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/admin': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/ws': {
        target: apiTarget,
        ws: true,
        changeOrigin: true,
      },
      '/health': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/healthz': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/db': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
})
