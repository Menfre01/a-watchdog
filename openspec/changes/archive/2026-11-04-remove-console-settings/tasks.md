# Tasks

## 1. 移除前端入口

- [x] 1.1 `web/src/App.vue` 移除 settings 的 tab 项、类型分支与视图映射，并移除 Settings 的 import。验证:`pnpm typecheck` 通过。
- [x] 1.2 移除 `web/src/views/Settings.vue`,并移除 `web/src/api.ts` 中仅它使用的 `Setting` 类型。验证:`grep -r "Settings.vue\|type Setting\|api/settings" web/src` 无输出。

## 2. 验证

- [x] 2.1 `pnpm typecheck` 与 `make build` 通过。验证：退出码 0,`bin/awatchdog` 生成。
- [x] 2.2 重启服务后侧栏只有四项，构建产物不含「全局配置」。验证：页面可见四项,`grep -c "全局配置" pkg/admin/dist/assets/*.js` 为 0。
