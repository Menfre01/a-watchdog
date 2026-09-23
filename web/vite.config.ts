import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 控制台构建配置:产物直出 Go embed 目录(pkg/admin/dist);
// 开发期由 Vite 代理管理 API 到本机网关。
export default defineConfig({
  plugins: [vue()],
  base: '/console/',
  build: {
    outDir: '../pkg/admin/dist',
    emptyOutDir: false,
  },
  server: {
    proxy: {
      '/console/api': 'http://127.0.0.1:8080',
    },
  },
})
