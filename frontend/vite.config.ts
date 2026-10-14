import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 4000,
    sourcemap: false,
    target: 'es2022',
  },
  server: {
    proxy: {
      '/api': { target: process.env.SKIFF_DEV_URL || 'http://127.0.0.1:8484', ws: true, changeOrigin: false },
    },
  },
})
