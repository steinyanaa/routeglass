import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    sourcemap: false,
    manifest: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('three') || id.includes('react-globe.gl') || id.includes('globe.gl')) return 'globe'
          if (id.includes('i18next')) return 'i18n'
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: { '/api': 'http://127.0.0.1:8765' },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./tests/setup.ts'],
    css: true,
  },
})
