import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  base: '/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200,
    assetsInlineLimit: 4096,
    rollupOptions: {
      output: {
        manualChunks: {
          vue: ['vue'],
          tdesign: ['tdesign-vue-next', 'tdesign-icons-vue-next'],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: { '/api': 'http://127.0.0.1:8080', '/d': 'http://127.0.0.1:8080' },
  },
})
