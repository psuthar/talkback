import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

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
      // Same-origin local dev: route all backend API calls through Vite
      // so cookies + credentials work without CORS configuration.
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/artifacts': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/sessions': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/zoom': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/auth': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/admin': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/ws': {
        target: 'http://localhost:8081',
        ws: true,
        changeOrigin: true
      },
      '/health': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/healthz': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/db': {
        target: 'http://localhost:8081',
        changeOrigin: true
      }
    }
  }
})
